#!/usr/bin/env python3
import argparse
import asyncio
import logging
import os
import signal
import sys
from typing import Optional

from grpc import aio

from mlx_lm import load, stream_generate
from mlx_lm.sample_utils import make_sampler

from prometheus_client import Counter, Histogram, start_http_server

# Metrics must be defined before they are used inside the service handlers.
grpc_requests_total = Counter(
    "grpc_requests_total",
    "Total number of gRPC Generate requests.",
)
generated_chunks_total = Counter(
    "generated_chunks_total",
    "Total number of generated chunks sent to clients.",
)
request_latency_seconds = Histogram(
    "grpc_request_latency_seconds",
    "End-to-end gRPC request latency in seconds.",
)

# Allow importing generated protobufs in worker/pb.
WORKER_PB = os.path.join(os.path.dirname(__file__), "pb")
sys.path.append(WORKER_PB)

import llm_service_pb2
import llm_service_pb2_grpc


class LLMService(llm_service_pb2_grpc.LLMServiceServicer):
    def __init__(self, model, tokenizer):
        self.model = model
        self.tokenizer = tokenizer

    async def Generate(self, request, context):
        # Stream tokens from MLX to the client. We use a queue to bridge the
        # blocking generator into the async gRPC stream.
        queue: asyncio.Queue[Optional[str]] = asyncio.Queue(maxsize=128)
        loop = asyncio.get_running_loop()
        request_start = loop.time()
        grpc_requests_total.inc()

        def producer():
            try:
                sampler = None
                if request.temperature and request.temperature > 0:
                    # mlx-lm 0.30.6: temperature is configured via sample_utils.make_sampler(temp=...)
                    try:
                        sampler = make_sampler(temp=request.temperature)
                    except TypeError:
                        sampler = make_sampler(temperature=request.temperature)
                for chunk in stream_generate(
                    self.model,
                    self.tokenizer,
                    request.prompt,
                    max_tokens=request.max_tokens or 256,
                    sampler=sampler,
                ):
                    # Critical: stop immediately if the client disconnects.
                    if hasattr(context, "is_active") and not context.is_active():
                        break
                    if hasattr(context, "cancelled") and context.cancelled():
                        break
                    # Only enqueue the generated text token, not the debug-rich repr.
                    token_text = getattr(chunk, "text", str(chunk))
                    # Backpressure is handled by awaiting queue.put from the thread.
                    asyncio.run_coroutine_threadsafe(queue.put(token_text), loop).result()
                    generated_chunks_total.inc()
            finally:
                asyncio.run_coroutine_threadsafe(queue.put(None), loop).result()

        # Run the blocking producer in a thread to keep the gRPC event loop responsive.
        producer_task = asyncio.create_task(asyncio.to_thread(producer))

        while True:
            chunk = await queue.get()
            if chunk is None:
                break
            yield llm_service_pb2.GenerateResponse(generated_text=chunk)

        request_latency_seconds.observe(loop.time() - request_start)
        await producer_task


async def serve(host: str, port: int, model_name: str):
    logging.info("loading model: %s", model_name)
    model, tokenizer = load(model_name)

    server = aio.server()
    llm_service_pb2_grpc.add_LLMServiceServicer_to_server(
        LLMService(model, tokenizer), server
    )
    server.add_insecure_port(f"{host}:{port}")

    stop_event = asyncio.Event()

    def _signal_handler(*_):
        stop_event.set()

    loop = asyncio.get_running_loop()
    for sig in (signal.SIGINT, signal.SIGTERM):
        try:
            loop.add_signal_handler(sig, _signal_handler)
        except NotImplementedError:
            # Windows / limited event loops
            signal.signal(sig, lambda *_: stop_event.set())

    await server.start()
    logging.info("worker listening on %s:%d", host, port)

    await stop_event.wait()
    logging.info("shutting down gRPC server")
    await server.stop(grace=5)


def main():
    parser = argparse.ArgumentParser(description="MLX LLM gRPC worker")
    parser.add_argument("--host", default="0.0.0.0")
    parser.add_argument("--port", type=int, default=50051)
    parser.add_argument("--metrics-port", type=int, default=9108)
    parser.add_argument(
        "--model",
        default="mlx-community/Meta-Llama-3-8B-Instruct-4bit",
        help="Model name or local path for mlx-lm",
    )
    args = parser.parse_args()

    logging.basicConfig(
        level=logging.INFO,
        format="%(asctime)s %(levelname)s %(message)s",
    )

    # Start a lightweight Prometheus HTTP endpoint for worker metrics.
    start_http_server(args.metrics_port)
    logging.info("metrics listening on 0.0.0.0:%d", args.metrics_port)

    asyncio.run(serve(args.host, args.port, args.model))


if __name__ == "__main__":
    main()
