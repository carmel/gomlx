## Benchmarking

- `make test-benchmark`

```txt
go test -run TestBenchmarkSingleService -v -timeout 10m ./server
=== RUN   TestBenchmarkSingleService
    benchmark_test.go:51: Starting benchmark for http://127.0.0.1:8090
    benchmark_test.go:52: Config: concurrency=4, requests=20, max_tokens=128
    benchmark_test.go:262:
        ========== Benchmark Results ==========
    benchmark_test.go:263: Service URL:         http://127.0.0.1:8090
    benchmark_test.go:264: Total Requests:      20
    benchmark_test.go:265: Successful:          20
    benchmark_test.go:266: Failed:              0
    benchmark_test.go:267: Duration:            73.94s
    benchmark_test.go:268: Success Rate:        100.0%
    benchmark_test.go:275:
        --- Latency (ms) ---
    benchmark_test.go:276: Min:                 4129.60
    benchmark_test.go:277: Avg:                 13687.11
    benchmark_test.go:278: P50:                 14748.96
    benchmark_test.go:279: P95:                 14910.21
    benchmark_test.go:280: P99:                 14910.21
    benchmark_test.go:281: Max:                 14910.21
    benchmark_test.go:283:
        --- Throughput ---
    benchmark_test.go:284: Requests/sec:        0.27
    benchmark_test.go:285: Total Tokens:        1480
    benchmark_test.go:286: Avg Tokens/Request:  74
    benchmark_test.go:287: Avg Tokens/sec:      5.41
    benchmark_test.go:289:
        ========================================
--- PASS: TestBenchmarkSingleService (73.94s)
PASS
ok  	gomlx/server	74.937s
```
