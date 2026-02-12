import argparse
import asyncio
import threading
import logging
from typing import Iterable

import grpc

from pb import llm_service_pb2, llm_service_pb2_grpc

# mlx-lm 0.30.6: stream_generate yields incremental text chunks (response.text)
from mlx_lm import load, stream_generate
from mlx_lm.sample_utils import make_sampler

try:
    import mlx.core as mx
except Exception:  # pragma: no cover - optional for metrics only
    mx = None


def build_prompt(tokenizer, messages: Iterable[llm_service_pb2.ChatMessage]) -> str:
    chat = [{"role": m.role, "content": m.content} for m in messages]
    if hasattr(tokenizer, "apply_chat_template"):
        try:
            return tokenizer.apply_chat_template(
                chat, tokenize=False, add_generation_prompt=True
            )
        except TypeError:
            return tokenizer.apply_chat_template(chat, add_generation_prompt=True)
    # Fallback: simple role-prefixed prompt
    prompt_lines = [f"{m['role']}: {m['content']}" for m in chat]
    prompt_lines.append("assistant:")
    return "\n".join(prompt_lines)


def get_metal_memory_info():
    """更新为最新的 MLX API"""
    if mx is not None:
        # 使用 mx.get_xxx 而不是 mx.metal.get_xxx
        return {
            "active": mx.get_active_memory() / 1024 / 1024,
            "peak": mx.get_peak_memory() / 1024 / 1024,
            "cache": mx.get_cache_memory() / 1024 / 1024,
        }
    return None


def log_mem(prefix="Memory"):
    info = get_metal_memory_info()
    if info:
        logging.info(
            f"{prefix} -> Active: {info['active']:.2f}MB, "
            f"Peak: {info['peak']:.2f}MB, "
            f"Cache: {info['cache']:.2f}MB"
        )


class LLMService(llm_service_pb2_grpc.LLMServiceServicer):
    def __init__(self, model, tokenizer):
        self.model = model
        self.tokenizer = tokenizer
        self._lock = asyncio.Lock()

    async def ChatStream(self, request, context):
        loop = asyncio.get_running_loop()
        log_mem("Request Start")

        # 清理缓存 API 也建议更新
        if mx and mx.get_cache_memory() > 2 * 1024 * 1024 * 1024:
            mx.metal.clear_cache()  # 这个目前依然在 metal 下

        prompt = build_prompt(self.tokenizer, request.messages)
        queue = asyncio.Queue()

        def _run_generation():
            try:
                sampler = None
                if request.temperature and request.temperature > 0:
                    sampler = make_sampler(temp=request.temperature)

                for response in stream_generate(
                    self.model,
                    self.tokenizer,
                    prompt,
                    max_tokens=request.max_tokens or 256,
                    sampler=sampler,
                ):
                    # --- 修复 1: hasattr 修正为 2 个参数 ---
                    if hasattr(response, "text"):
                        loop.call_soon_threadsafe(
                            queue.put_nowait, ("chunk", response.text)
                        )

                loop.call_soon_threadsafe(queue.put_nowait, ("done", None))
            except Exception as exc:
                logging.error(f"Generation error: {exc}")
                loop.call_soon_threadsafe(queue.put_nowait, ("error", exc))

        async with self._lock:
            thread = threading.Thread(target=_run_generation, daemon=True)
            thread.start()
            try:
                while True:
                    kind, payload = await queue.get()
                    if kind == "chunk":
                        yield llm_service_pb2.ChatResponse(text_chunk=payload)
                    elif kind == "done":
                        break
                    elif kind == "error":
                        # --- 修复 2: 必须 await context.abort ---
                        logging.error(
                            f"Aborting RPC due to generation error: {payload}"
                        )
                        await context.abort(grpc.StatusCode.INTERNAL, str(payload))
            except Exception as e:
                if not context.done():
                    logging.error(f"Stream exception: {e}")
                raise

        log_mem("Request End")


async def serve(host: str, port: int, model_id: str) -> None:
    logging.info("loading model: %s", model_id)
    model, tokenizer = load(model_id)
    server = grpc.aio.server(
        options=[
            ("grpc.max_send_message_length", 128 * 1024 * 1024),
            ("grpc.max_receive_message_length", 128 * 1024 * 1024),
        ]
    )
    llm_service_pb2_grpc.add_LLMServiceServicer_to_server(
        LLMService(model, tokenizer), server
    )
    server.add_insecure_port(f"{host}:{port}")

    await server.start()
    logging.info("worker listening on %s:%d", host, port)

    await server.wait_for_termination()
    logging.info("shutting down gRPC server")


def main() -> None:
    parser = argparse.ArgumentParser(description="MLX-LM gRPC worker")
    parser.add_argument("--host", default="0.0.0.0")
    parser.add_argument("--port", type=int, default=50051)
    parser.add_argument(
        "--model", type=str, default="mlx-community/Qwen2.5-Coder-14B-Instruct-4bit"
    )
    args = parser.parse_args()

    logging.basicConfig(
        level=logging.INFO,
        format="%(asctime)s %(levelname)s %(message)s",
    )

    asyncio.run(serve(args.host, args.port, args.model))


if __name__ == "__main__":
    main()
