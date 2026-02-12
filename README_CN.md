<div align="right">
  <span>[<a href="./README.md">English</a>]<span>
  </span>[<a href="./README_CN.md">简体中文</a>]</span>
</div>

# GoMLX: 高性能 AI 模型服务网关

GoMLX 是一个生产级的 AI 模型服务框架，采用 **Go + Python** 的混合架构：使用 Go 语言构建高性能的 OpenAI 兼容网关，通过 gRPC 协议驱动后端的 Python AI 推理引擎。

## 🌟 核心特性

- **OpenAI 兼容接口**：无缝对接现有 OpenAI 客户端生态（支持 `/v1/chat/completions`）。
- **流式传输 (Streaming)**：基于 Server-Sent Events (SSE) 的原生流式响应，支持打字机效果。
- **高性能 gRPC 通信**：网关与推理后端采用 protobuf 协议，低延迟、高吞吐。
- **并发控制与限流**：内置信号量（Semaphore）保护后端推理资源，防止流量激增压垮 GPU。
- **健壮性设计**：支持优雅停机、上下文（Context）级级联取消、以及针对 Nginx 优化的流式传输头。
- **生产级监控**：集成结构化日志 `slog`，支持多级日志配置。

## 🏗 系统架构

```text
[ Client ] <--- HTTP/JSON (SSE) ---> [ Go Gateway ] <--- gRPC (Protobuf) ---> [ Python Worker ]
    |                                     |                                       |
  客户端                          并发控制/协议转换                        GPU 模型推理 (PyTorch/vLLM)
```

## 📂 项目结构

```text
.
├── gateway/                # Go 语言实现的网关
│   ├── main.go             # 入口函数与 HTTP 路由
│   ├── config.go           # 配置加载逻辑
│   └── pb/                 # gRPC 生成的 Go 代码
├── worker/                 # Python 语言实现的推理后端
│   ├── server.py           # gRPC 服务端实现
│   └── model.py            # 模型加载与推理逻辑
└── proto/                  # 接口定义文件
    └── llm_service.proto   # 定义 ChatStream 等接口
```

## 🚀 快速开始

### 1. 环境依赖

- **Go**: 1.21+
- **Python**: 3.9+
- **Protobuf**: `protoc` 编译器

### 2. 定义与生成接口 (Protobuf)

```bash
# 生成 Go 代码
protoc --go_out=. --go-grpc_out=. proto/llm_service.proto

# 生成 Python 代码
python -m grpc_tools.protoc -I./proto --python_out=./worker --grpc_python_out=./worker proto/llm_service.proto
```

### 3. 启动推理后端 (Python)

```bash
cd worker
pip install grpcio grpcio-tools transformers torch
python server.py --port 50051
```

### 4. 启动网关 (Go)

```bash
cd gateway
go build -o gateway
./gateway --config config.yaml
```

## ⚙️ 配置文件 (`config.yaml`)

```yaml
http_port: 8080
worker_address: "localhost:50051"
max_concurrent_requests: 50 # 并发请求限制
read_timeout: 30s
write_timeout: 300s # 流式输出建议设置较长
log_level: "info"
```

## 📝 接口调用示例

使用标准 `curl` 验证 OpenAI 兼容性：

```bash
curl http://localhost:8080/v1/chat/completions \
  -H "Content-Type: application/json" \
  -d '{
    "model": "gpt-3.5-turbo",
    "messages": [{"role": "user", "content": "你好，请介绍一下你自己"}],
    "stream": true
  }'
```

## 🛠 开发建议

1.  **Tokenizer 同步**：目前的网关使用 `strings.Fields` 估算 Token。在生产环境中，建议集成 `tiktoken-go` 以实现与 OpenAI 一致的计费统计。
2.  **GPU 负载均衡**：当有多个 Python Worker 时，可在 Go 网关层引入负载均衡策略或使用服务发现（如 Consul/Etcd）。
3.  **安全防护**：建议在网关层增加 API Key 校验及敏感词过滤逻辑。

## 📄 开源协议

[MIT License](LICENSE)
