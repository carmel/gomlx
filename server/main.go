package main

import (
	"context"
	"encoding/json"
	"errors"
	"flag"
	"fmt"
	"io"
	"net/http"
	"os"
	"os/signal"
	"strings"
	"syscall"
	"time"

	"log/slog"

	"github.com/prometheus/client_golang/prometheus"
	"github.com/prometheus/client_golang/prometheus/promhttp"
	"google.golang.org/grpc"
	"google.golang.org/grpc/credentials/insecure"

	"gomlx/server/pb"
)

type chatMessage struct {
	Role    string `json:"role"`
	Content string `json:"content"`
}

type openAIChatRequest struct {
	Model       string        `json:"model"`
	Messages    []chatMessage `json:"messages"`
	Prompt      string        `json:"prompt,omitempty"`
	MaxTokens   int32         `json:"max_tokens,omitempty"`
	Temperature float32       `json:"temperature,omitempty"`
	TopP        float32       `json:"top_p,omitempty"`
	Stream      bool          `json:"stream,omitempty"`
}

type openAIChatMessage struct {
	Role    string `json:"role"`
	Content string `json:"content"`
}

type openAIChatChoice struct {
	Index        int               `json:"index"`
	Message      openAIChatMessage `json:"message"`
	FinishReason string            `json:"finish_reason"`
}

type openAIUsage struct {
	PromptTokens     int `json:"prompt_tokens"`
	CompletionTokens int `json:"completion_tokens"`
	TotalTokens      int `json:"total_tokens"`
}

type openAIChatResponse struct {
	ID      string             `json:"id"`
	Object  string             `json:"object"`
	Created int64              `json:"created"`
	Model   string             `json:"model"`
	Choices []openAIChatChoice `json:"choices"`
	Usage   openAIUsage        `json:"usage"`
}

type openAIStreamDelta struct {
	Role    string `json:"role,omitempty"`
	Content string `json:"content,omitempty"`
}

type openAIStreamChoice struct {
	Index        int               `json:"index"`
	Delta        openAIStreamDelta `json:"delta"`
	FinishReason *string           `json:"finish_reason"`
}

type openAIStreamChunk struct {
	ID      string               `json:"id"`
	Object  string               `json:"object"`
	Created int64                `json:"created"`
	Model   string               `json:"model"`
	Choices []openAIStreamChoice `json:"choices"`
}

func main() {
	configPath := flag.String("config", "config.yaml", "path to config file")
	flag.Parse()

	cfg, err := LoadConfig(*configPath)
	if err != nil {
		fmt.Fprintf(os.Stderr, "%v\n", err)
		os.Exit(1)
	}

	logger := newLogger(cfg.LogLevel)
	logger.Info("starting server", "http_port", cfg.HTTPPort, "grpc_address", cfg.GRPCAddress)

	grpcConn, err := grpc.NewClient(
		cfg.GRPCAddress,
		grpc.WithTransportCredentials(insecure.NewCredentials()),
	)
	if err != nil {
		logger.Error("failed to connect to worker", "error", err)
		os.Exit(1)
	}
	defer grpcConn.Close()

	client := pb.NewLLMServiceClient(grpcConn)

	mux := http.NewServeMux()
	registerMetrics(mux, cfg)
	mux.HandleFunc("/healthz", func(w http.ResponseWriter, _ *http.Request) {
		w.Header().Set("Content-Type", "text/plain; charset=utf-8")
		w.WriteHeader(http.StatusOK)
		_, _ = w.Write([]byte("ok"))
	})

	mux.HandleFunc("POST /v1/chat/completions", func(w http.ResponseWriter, r *http.Request) {
		requestsTotal.Inc()

		var reqBody openAIChatRequest
		if err := json.NewDecoder(r.Body).Decode(&reqBody); err != nil {
			http.Error(w, "invalid JSON", http.StatusBadRequest)
			return
		}

		prompt := buildPrompt(reqBody)
		if strings.TrimSpace(prompt) == "" {
			http.Error(w, "prompt/messages required", http.StatusBadRequest)
			return
		}

		if reqBody.MaxTokens == 0 {
			reqBody.MaxTokens = 256
		}
		if reqBody.Temperature == 0 {
			reqBody.Temperature = 0.7
		}

		flusher, ok := w.(http.Flusher)
		if !ok {
			http.Error(w, "streaming unsupported", http.StatusInternalServerError)
			return
		}

		// Use the HTTP request context so client disconnects cancel gRPC streams.
		stream, err := client.Generate(r.Context(), &pb.GenerateRequest{
			Prompt:      prompt,
			MaxTokens:   reqBody.MaxTokens,
			Temperature: reqBody.Temperature,
		})
		if err != nil {
			logger.Error("failed to start gRPC stream", "error", err)
			http.Error(w, "failed to start stream", http.StatusBadGateway)
			return
		}

		start := time.Now()
		if reqBody.Stream {
			w.Header().Set("Content-Type", "text/event-stream")
			w.Header().Set("Cache-Control", "no-cache")
			w.Header().Set("Connection", "keep-alive")

			streamID := fmt.Sprintf("chatcmpl-%d", start.UnixNano())
			firstChunk := true

			for {
				resp, err := stream.Recv()
				if errors.Is(err, io.EOF) {
					break
				}
				if err != nil {
					logger.Warn("stream recv error", "error", err)
					return
				}

				delta := openAIStreamDelta{Content: resp.GetGeneratedText()}
				if firstChunk {
					delta.Role = "assistant"
					firstChunk = false
				}

				chunk := openAIStreamChunk{
					ID:      streamID,
					Object:  "chat.completion.chunk",
					Created: time.Now().Unix(),
					Model:   reqBody.Model,
					Choices: []openAIStreamChoice{{
						Index: 0,
						Delta: delta,
					}},
				}

				payload, _ := json.Marshal(chunk)
				if _, err := fmt.Fprintf(w, "data: %s\n\n", payload); err != nil {
					logger.Warn("client write error", "error", err)
					return
				}
				flusher.Flush()
				streamedChunksTotal.Inc()
			}

			// Send final done signal
			doneChunk := openAIStreamChunk{
				ID:      streamID,
				Object:  "chat.completion.chunk",
				Created: time.Now().Unix(),
				Model:   reqBody.Model,
				Choices: []openAIStreamChoice{{
					Index:        0,
					Delta:        openAIStreamDelta{},
					FinishReason: ptr("stop"),
				}},
			}
			payload, _ := json.Marshal(doneChunk)
			_, _ = fmt.Fprintf(w, "data: %s\n\n", payload)
			_, _ = fmt.Fprint(w, "data: [DONE]\n\n")
			flusher.Flush()
		} else {
			// Non-stream: buffer full text then return OpenAI-style response.
			var sb strings.Builder
			completionTokens := 0
			for {
				resp, err := stream.Recv()
				if errors.Is(err, io.EOF) {
					break
				}
				if err != nil {
					logger.Warn("stream recv error", "error", err)
					return
				}
				sb.WriteString(resp.GetGeneratedText())
				completionTokens += len(strings.Fields(resp.GetGeneratedText()))
				streamedChunksTotal.Inc()
			}

			fullText := sb.String()
			finishReason := "stop"
			if reqBody.MaxTokens > 0 && completionTokens >= int(reqBody.MaxTokens) {
				// Approximate length stop if we hit the requested max tokens.
				finishReason = "length"
			}
			chatResp := openAIChatResponse{
				ID:      fmt.Sprintf("chatcmpl-%d", time.Now().UnixNano()),
				Object:  "chat.completion",
				Created: time.Now().Unix(),
				Model:   reqBody.Model,
				Choices: []openAIChatChoice{{
					Index: 0,
					Message: openAIChatMessage{
						Role:    "assistant",
						Content: fullText,
					},
					FinishReason: finishReason,
				}},
				Usage: openAIUsage{
					PromptTokens:     len(strings.Fields(prompt)),
					CompletionTokens: completionTokens,
					TotalTokens:      len(strings.Fields(prompt)) + completionTokens,
				},
			}

			w.Header().Set("Content-Type", "application/json")
			if err := json.NewEncoder(w).Encode(chatResp); err != nil {
				logger.Warn("client write error", "error", err)
				return
			}
		}

		requestLatency.Observe(time.Since(start).Seconds())
	})

	server := &http.Server{
		Addr:         fmt.Sprintf(":%d", cfg.HTTPPort),
		Handler:      mux,
		ReadTimeout:  cfg.ReadTimeout,
		WriteTimeout: cfg.WriteTimeout,
		IdleTimeout:  cfg.IdleTimeout,
	}

	ctx, stop := signal.NotifyContext(context.Background(), syscall.SIGINT, syscall.SIGTERM)
	defer stop()

	go func() {
		if err := server.ListenAndServe(); err != nil && !errors.Is(err, http.ErrServerClosed) {
			logger.Error("http server error", "error", err)
			stop()
		}
	}()

	<-ctx.Done()
	shutdownCtx, cancel := context.WithTimeout(context.Background(), 10*time.Second)
	defer cancel()

	logger.Info("shutting down")
	if err := server.Shutdown(shutdownCtx); err != nil {
		logger.Error("shutdown error", "error", err)
	}
}

func newLogger(level string) *slog.Logger {
	lvl := slog.LevelInfo
	if parsed, err := parseLevel(level); err == nil {
		lvl = parsed
	}

	handler := slog.NewJSONHandler(os.Stdout, &slog.HandlerOptions{Level: lvl})
	logger := slog.New(handler)
	slog.SetDefault(logger)
	return logger
}

func parseLevel(level string) (slog.Level, error) {
	switch strings.ToLower(level) {
	case "debug":
		return slog.LevelDebug, nil
	case "info":
		return slog.LevelInfo, nil
	case "warn", "warning":
		return slog.LevelWarn, nil
	case "error":
		return slog.LevelError, nil
	default:
		return slog.LevelInfo, fmt.Errorf("unknown level: %s", level)
	}
}

var (
	requestsTotal = prometheus.NewCounter(prometheus.CounterOpts{
		Name: "http_requests_total",
		Help: "Total number of HTTP requests received.",
	})
	streamedChunksTotal = prometheus.NewCounter(prometheus.CounterOpts{
		Name: "streamed_chunks_total",
		Help: "Total number of streamed response chunks.",
	})
	requestLatency = prometheus.NewHistogram(prometheus.HistogramOpts{
		Name:    "request_latency_seconds",
		Help:    "End-to-end request latency in seconds.",
		Buckets: prometheus.DefBuckets,
	})
)

func registerMetrics(mux *http.ServeMux, cfg Config) {
	prometheus.MustRegister(requestsTotal, streamedChunksTotal, requestLatency)
	mux.Handle(cfg.MetricsPath, promhttp.Handler())
}

// buildPrompt converts OpenAI chat/completions payload to a single prompt string
// that the backend model expects. Fallback to legacy `prompt` field if provided.
func buildPrompt(req openAIChatRequest) string {
	if req.Prompt != "" {
		return req.Prompt
	}

	var b strings.Builder
	for _, m := range req.Messages {
		role := strings.ToLower(strings.TrimSpace(m.Role))
		switch role {
		case "system":
			b.WriteString("[SYSTEM] ")
			// continue
		case "user":
			b.WriteString("[USER] ")
		case "assistant":
			b.WriteString("[ASSISTANT] ")
		default:
			b.WriteString("[UNKNOWN] ")
		}
		b.WriteString(m.Content)
		b.WriteString("\n")
	}
	return b.String()
}

func ptr[T any](v T) *T { return &v }
