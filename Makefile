PROTO_DIR=proto
PY_OUT=worker/pb
GO_OUT=server/pb

.PHONY: run-worker-1 run-worker-2 run-gateway gen-proto

run-worker:
	uv run worker/main.py --port 50051 --model ~/Models/Qwen2.5-Coder-14B-Instruct-4bit

build-server:
	go build -ldflags="-w -s" -trimpath -o server ./server

run-gateway:
	go run ./server --config config.yaml

gen-proto:
	mkdir -p $(PY_OUT) $(GO_OUT)
	protoc -I $(PROTO_DIR) --go_out=$(GO_OUT) --go-grpc_out=$(GO_OUT) $(PROTO_DIR)/llm_service.proto
	protoc -I $(PROTO_DIR) --python_out=$(PY_OUT) --grpc_python_out=$(PY_OUT) $(PROTO_DIR)/llm_service.proto

# Benchmark tests - run one service at a time
test-benchmark:
	go test -run TestBenchmarkSingleService -v -timeout 10m ./server

test-benchmark-high-load:
	go test -run TestBenchmarkHighLoad -v -timeout 20m ./server

test-benchmark-long-text:
	go test -run TestBenchmarkLongText -v -timeout 20m ./server

test-benchmark-low-latency:
	go test -run TestBenchmarkLowLatency -v -timeout 10m ./server

test-benchmark-stress:
	go test -run TestBenchmarkStress -v -timeout 30m ./server
