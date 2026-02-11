# MLX gRPC LLM Inference Scaffold

这是一个基于 Apple Silicon 的 `mlx-lm` 推理 Worker（Python）与 HTTP API Gateway（Go）解耦的项目脚手架。通信使用 gRPC Server-Side Streaming，HTTP 侧以 `net/http` + chunked 方式把 token 实时推送给客户端。

## 目录结构
- `proto/`：Protobuf 定义
- `worker/`：Python gRPC 推理服务（MLX）
- `server/`：Go HTTP API Gateway
- `config.yaml`：Go 服务器配置
- `Makefile`：常用命令

## 依赖
- Python 3.10+
- Go 1.21+
- `uv`
- `protoc`

## 安装与生成
1. 安装 Python 依赖（使用 `uv`）

```bash
uv venv .venv
source .venv/bin/activate
uv pip install -r worker/requirements.txt
```

2. 生成 Protobuf 代码

```bash
make proto-gen
```

3. 安装 Go 依赖

```bash
go mod tidy
```

## 运行
1. 启动 Python Worker（在 Apple Silicon 上运行）

```bash
make run-python
```

2. 启动 Go API Gateway

```bash
make run-go
```

3. 访问 Prometheus 指标

```bash
curl http://localhost:8080/metrics
curl http://localhost:9108
```

## 测试

```bash
curl -N -X POST http://localhost:8080/v1/chat/completions \
  -H "Content-Type: application/json" \
  -d '{"prompt":"Hello","max_tokens":64,"temperature":0.7}'
```

`-N` 会关闭 curl 的缓冲，确保你能看到实时流式输出。

## 说明
- Go 端使用 `r.Context()` 直接传递给 gRPC 流，客户端断开会立即取消后端生成。
- Python 端在生成循环中检查 `context.is_active()`，一旦失活就停止生成。
- HTTP 使用 `http.Flusher` 立刻把 token 写回客户端，实现打字机效果。
- Go 端暴露 `metrics_path`（默认 `/metrics`）用于 Prometheus 抓取。
- Python Worker 在 `--metrics-port` 上启动 Prometheus HTTP 端点（默认 `9108`）。
