<div align="right">
  <span>[<a href="./README.md">English</a>]<span>
  </span>[<a href="./README_CN.md">简体中文</a>]</span>
</div>

# GoMLX: High-Performance AI Model Serving Gateway

GoMLX is a production-grade AI model serving framework built with a **Go + Python** hybrid architecture. It leverages the high-concurrency capabilities of Go to provide an OpenAI-compatible API gateway, while driving high-performance Python-based AI inference engines via the gRPC protocol.

## 🌟 Key Features

- **OpenAI API Compatibility**: Seamlessly integrates with existing OpenAI client ecosystems (supports `/v1/chat/completions`).
- **Native Streaming (SSE)**: Built-in support for Server-Sent Events (SSE) to enable real-time "typewriter" effects.
- **Efficient gRPC Communication**: Low-latency, high-throughput communication between the Gateway and Inference Worker using Protobuf.
- **Concurrency & Rate Limiting**: Integrated Semaphore-based traffic control to protect GPU resources from being overwhelmed by request spikes.
- **Production-Ready Robustness**: Features graceful shutdown, cascaded context cancellation, and proxy-optimized headers (e.g., for Nginx).
- **Structured Logging**: Powered by Go's `slog` for high-performance, JSON-formatted observability.

## 🏗 System Architecture

```text
[ Client ] <--- HTTP/JSON (SSE) ---> [ Go Gateway ] <--- gRPC (Protobuf) ---> [ Python Worker ]
    |                                     |                                       |
  Apps/SDKs                       Concurrency Control                      GPU Inference
                                 & Protocol Translation                  (PyTorch/vLLM/MLX)
```

## 📂 Project Structure

```text
.
├── gateway/                # Go implementation of the API Gateway
│   ├── main.go             # Entry point, HTTP routing, and Middleware
│   ├── config.go           # Configuration management
│   └── pb/                 # Generated Go code from gRPC Protobuf
├── worker/                 # Python implementation of the Inference Backend
│   ├── server.py           # gRPC server implementation
│   └── model.py            # Model loading and inference logic
└── proto/                  # Interface definition files
    └── llm_service.proto   # Service definitions (e.g., ChatStream)
```

## 🚀 Quick Start

### 1. Prerequisites

- **Go**: version 1.21 or higher
- **Python**: version 3.9 or higher
- **Protobuf**: `protoc` compiler installed

### 2. Generate Interfaces (Protobuf)

```bash
# Generate Go code
protoc --go_out=. --go-grpc_out=. proto/llm_service.proto

# Generate Python code
python -m grpc_tools.protoc -I./proto --python_out=./worker --grpc_python_out=./worker proto/llm_service.proto
```

### 3. Start the Inference Worker (Python)

```bash
cd worker
pip install grpcio grpcio-tools transformers torch
python server.py --port 50051
```

### 4. Start the Gateway (Go)

```bash
cd gateway
go build -o gateway
./gateway --config config.yaml
```

## ⚙️ Configuration (`config.yaml`)

```yaml
http_port: 8080
worker_address: "localhost:50051"
max_concurrent_requests: 50 # Limit concurrent GPU tasks
read_timeout: 30s
write_timeout: 300s # Set long enough for slow LLM generation
log_level: "info"
```

## 📝 API Usage Example

Verify the OpenAI compatibility using standard `curl`:

```bash
curl http://localhost:8080/v1/chat/completions \
  -H "Content-Type: application/json" \
  -d '{
    "model": "gpt-3.5-turbo",
    "messages": [{"role": "user", "content": "Tell me a short story about a robot."}],
    "stream": true
  }'
```

## 🛠 Development Roadmap

1.  **Token Counting**: Currently, the gateway uses a rough estimation for Token usage. For production billing, integration with `tiktoken-go` is recommended.
2.  **Load Balancing**: For multi-GPU setups, the Gateway can be extended to support Round-Robin or Least-Load balancing across multiple Python Workers.
3.  **Content Moderation**: Integration of safety filters and sensitive word filtering within the Gateway layer.

## 📄 License

[MIT License](LICENSE)
