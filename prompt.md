#### 1. 设定背景与角色 (System/Context)

> **Role:** You are a Senior System Architect and DevOps Engineer specializing in LLM Observability and High-Performance Computing.
> **Objective:** Build a production-grade LLM inference system using `mlx-lm`, decoupling the Inference Engine (Python) from the API Gateway (Go) via gRPC Streaming, with full **Prometheus Observability** implementation.

#### 2. 核心指令 (Main Prompt)

**Task:** Create a complete, modular project scaffold for an LLM inference system based on the following specifications:

**1. Technology Stack & Architecture:**

- **Protocol:** gRPC (Server-Side Streaming) with Proto3.
- **Observability:** **Prometheus**.
  - Both services must expose a `/metrics` endpoint.
  - Use `github.com/prometheus/client_golang` for Go.
  - Use `prometheus_client` for Python.
- **Inference Worker (Python):**
  - **Manager:** **`uv`** (by Astral) for dependency management/execution.
  - **Library:** `mlx-lm`, `grpcio`, `prometheus_client`.
  - **Role:** Model inference with specific LLM metrics (Tokens per second, Active requests).
- **API Gateway (Go/Golang):**
  - **Framework:** Standard `net/http` (No Gin/Fiber).
  - **Config/Log:** `flag`, `gopkg.in/yaml.v3`, `log/slog` (JSON).
  - **Role:** Routes requests, handles auth (mock), streams responses, and tracks HTTP/gRPC latency.

**2. Detailed Implementation Steps:**

**Step A: Project Structure**

- Files: `/proto`, `/server`, `/worker`, `/config.yaml`, `/pyproject.toml`, `/prometheus.yml` (for local docker).

**Step B: Protocol Definition (`proto/llm_service.proto`)**

- `rpc Generate(GenerateRequest) returns (stream GenerateResponse);`

**Step C: Python Worker (`worker/main.py`)**

- **Metrics:** Implement a separate thread to run `start_http_server(8001)` (since gRPC occupies the main port).
- **Instrumentation:**
  - `llm_active_requests` (Gauge): Track concurrent requests.
  - `llm_generated_tokens_total` (Counter): Update this _inside_ the generation loop for every token yielded.
  - `llm_inference_duration_seconds` (Histogram): Measure time to generate response.
- **Logic:** Standard `mlx_lm.generate` with gRPC streaming and Context cancellation checks.

**Step D: Go Server (`server/main.go`)**

- **Metrics Endpoint:** Expose `/metrics` on the standard HTTP port using `promhttp.Handler()`.
- **Middleware:** Create a custom `middleware(next http.Handler)` to record:
  - `http_requests_total` (Counter, labels: method, status).
  - `http_request_duration_seconds` (Histogram).
- **Handler:** Stream gRPC response to HTTP client via `http.Flusher`.

**Step E: Production Readiness (DevOps)**

- **Configuration:** `config.yaml` should define ports for App and Metrics.
- **Makefile (Using `uv`):**
  - `run-python`: `uv run worker/main.py`
  - `run-go`: `go run ./server`
  - `run-prom`: (Optional) Command to run a local Prometheus docker container using `prometheus.yml`.
- **README:** Explain how to view metrics (e.g., "Open localhost:9090 and query `llm_generated_tokens_total`").

**3. Output Requirements:**

- **Code Completeness:** Provide full, runnable files.
- **Dependency Files:** Include a valid `pyproject.toml` (listing `prometheus_client`) and a sample `prometheus.yml` configured to scrape targets `host.docker.internal` or `localhost`.
- **Rationale:** Add comments explaining why the Python metrics server runs on a separate thread/port.

---

### 提示词设计深度解析 (Why this works)

为了确保 Codex 输出的代码不仅能跑，而且具备“生产级”素质，我在提示词中嵌入了以下关键点：

1.  **明确的架构解耦**：
    - 强制分离 Go（处理高并发网络请求）和 Python（处理计算密集型任务）。这是 LLM 部署的工业界标准模式，避免了 Python GIL 在处理网络并发时的瓶颈。

2.  **流式传输 (Streaming) 的技术细节**：
    - 普通的提示词只会说“用流式”，导致 AI 可能会写出“生成完所有文本再一次性发回”的伪流式。
    - 本提示词明确要求：`Chunked Transfer Encoding` (HTTP层) 和 `Server-Side Streaming` (gRPC层)，并强调了 `http.Flusher`，确保实现真正的打字机效果。

3.  **Context 传播与取消 (Context Cancellation)**：
    - 这是生产环境最容易被忽略的点。如果用户关闭了浏览器，后端还在继续跑模型，会极大地浪费昂贵的算力。
    - 提示词强制要求检查 context cancellation，确保 Go 断开时 Python 立刻停止推理。

4.  **特定库的指定**：
    - 指定 `mlx-lm`，避免 AI 使用通用的 PyTorch 或 HuggingFace Transformers 代码，从而无法利用 Apple Silicon 的 NPU 加速。

5.  **DevOps 思维**：
    - 要求 `Makefile` 和 `Project Structure`，确保生成的不仅仅是代码片段，而是一个可维护的工程。

### 建议后续交互流程

在使用上述提示词得到初始代码后，您可以使用以下追问（Follow-up Prompts）来进一步完善项目：

- **优化配置管理：** "Refactor the code to use environment variables for configuration (Model Path, Port, Host) using `viper` for Go and `pydantic-settings` for Python."
- **增加健壮性：** "Add a retry mechanism in the Go gRPC client with exponential backoff in case the Python worker is temporarily unavailable."
- **前端集成：** "Generate a simple HTML/JS frontend using `fetch` and `TextDecoder` to demonstrate the streaming output in the browser."
