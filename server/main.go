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

	"google.golang.org/grpc"
	"google.golang.org/grpc/credentials/insecure"

	pb "gomlx/server/pb"
)

type Gateway struct {
	// cfg    Config
	client pb.LLMServiceClient
	pool   *Semaphore
}

func (g *Gateway) chatCompletions(w http.ResponseWriter, r *http.Request) {

	var req ChatCompletionRequest
	if err := json.NewDecoder(r.Body).Decode(&req); err != nil {
		http.Error(w, "invalid JSON", http.StatusBadRequest)
		return
	}

	if err := g.pool.Acquire(r.Context()); err != nil {
		http.Error(w, "too many requests", http.StatusTooManyRequests)
		return
	}
	defer g.pool.Release()

	if len(req.Messages) == 0 {
		http.Error(w, "messages required", http.StatusBadRequest)
		return
	}

	if req.MaxTokens == 0 {
		req.MaxTokens = 1024
	}
	if req.Temperature == 0 {
		req.Temperature = float32(0.7)
	}

	grpcReq := &pb.ChatRequest{
		MaxTokens:   req.MaxTokens,
		Temperature: req.Temperature,
	}
	for _, msg := range req.Messages {
		grpcReq.Messages = append(grpcReq.Messages, &pb.ChatMessage{
			Role:    msg.Role,
			Content: msg.Content,
		})
	}

	// Use the HTTP request context so client disconnects cancel gRPC streams.
	stream, err := g.client.ChatStream(r.Context(), grpcReq)
	if err != nil {
		slog.Error("failed to start gRPC stream", "error", err)
		http.Error(w, "failed to start stream", http.StatusBadGateway)
		return
	}

	start := time.Now()
	if req.Stream {
		flusher, ok := w.(http.Flusher)
		if !ok {
			http.Error(w, "streaming unsupported", http.StatusInternalServerError)
			return
		}
		w.Header().Set("Content-Type", "text/event-stream")
		w.Header().Set("Cache-Control", "no-cache")
		w.Header().Set("Connection", "keep-alive")
		w.Header().Set("X-Accel-Buffering", "no") // 禁用 Nginx 缓存

		w.WriteHeader(http.StatusOK)
		flusher.Flush()
		streamID := fmt.Sprintf("chatcmpl-%d", start.UnixNano())
		firstChunk := true

		for {
			resp, err := stream.Recv()
			if errors.Is(err, io.EOF) {
				break
			}
			if err != nil {
				slog.Warn("stream recv error", "error", err)
				return
			}

			delta := ChatMessage{Content: resp.GetTextChunk()}
			if firstChunk {
				delta.Role = "assistant"
				firstChunk = false
			}

			chunk := ChatCompletionChunk{
				ID:      streamID,
				Object:  "chat.completion.chunk",
				Created: time.Now().Unix(),
				Model:   req.Model,
				Choices: []ChatCompletionChunkChoice{{
					Index: 0,
					Delta: delta,
				}},
			}

			payload, err := json.Marshal(chunk)
			if err != nil {
				slog.Error("failed to marshal chunk", "error", err)
				return
			}
			if _, err := fmt.Fprintf(w, "data: %s\n\n", payload); err != nil {
				slog.Warn("client write error", "error", err)
				return
			}
			flusher.Flush()
		}

		// Send final done signal
		doneChunk := ChatCompletionChunk{
			ID:      streamID,
			Object:  "chat.completion.chunk",
			Created: time.Now().Unix(),
			Model:   req.Model,
			Choices: []ChatCompletionChunkChoice{{
				Index:        0,
				Delta:        ChatMessage{},
				FinishReason: new("stop"),
			}},
		}
		if payload, err := json.Marshal(doneChunk); err == nil {
			fmt.Fprintf(w, "data: %s\n\n", payload)
		}
		fmt.Fprint(w, "data: [DONE]\n\n")
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
				slog.Error("stream recv error", "error", err)
				http.Error(w, "error reading stream", http.StatusInternalServerError)
			}

			chunk := resp.GetTextChunk()
			sb.WriteString(chunk)
			completionTokens += len(strings.Fields(chunk)) // 粗略估算
		}

		fullText := sb.String()
		finishReason := "stop"
		if req.MaxTokens > 0 && completionTokens >= int(req.MaxTokens) {
			// Approximate length stop if we hit the requested max tokens.
			finishReason = "length"
		}
		chatResp := ChatCompletionResponse{
			ID:      fmt.Sprintf("chatcmpl-%d", time.Now().UnixNano()),
			Object:  "chat.completion",
			Created: time.Now().Unix(),
			Model:   req.Model,
			Choices: []ChatCompletionChoice{{
				Index: 0,
				Message: ChatMessage{
					Role:    "assistant",
					Content: fullText,
				},
				FinishReason: finishReason,
			}},
			Usage: &Usage{
				PromptTokens:     len(strings.Fields(fullText)),
				CompletionTokens: completionTokens,
				TotalTokens:      len(strings.Fields(fullText)) + completionTokens,
			},
		}

		w.Header().Set("Content-Type", "application/json")
		if err := json.NewEncoder(w).Encode(chatResp); err != nil {
			slog.Error("failed to encode response", "error", err)
		}
	}
}

func main() {
	configPath := flag.String("config", "config.yaml", "path to config file")
	flag.Parse()

	cfg, err := LoadConfig(*configPath)
	if err != nil {
		fmt.Fprintf(os.Stderr, "%v\n", err)
		os.Exit(1)
	}

	ll := parseLevel(cfg.LogLevel)
	handler := slog.NewJSONHandler(os.Stdout, &slog.HandlerOptions{Level: ll})
	logger := slog.New(handler)
	slog.SetDefault(logger)

	grpcConn, err := grpc.NewClient(
		cfg.WorkerAddress,
		grpc.WithTransportCredentials(insecure.NewCredentials()),
	)
	if err != nil {
		slog.Error("failed to connect to worker", "error", err)
		os.Exit(1)
	}
	defer grpcConn.Close()

	client := pb.NewLLMServiceClient(grpcConn)
	gw := &Gateway{
		// cfg:    cfg,
		client: client,
		pool:   NewSemaphore(cfg.MaxConcurrentRequests),
	}

	mux := http.NewServeMux()

	mux.HandleFunc("/healthz", func(w http.ResponseWriter, _ *http.Request) {
		w.Header().Set("Content-Type", "text/plain; charset=utf-8")
		w.WriteHeader(http.StatusOK)
		_, _ = w.Write([]byte("ok"))
	})

	mux.HandleFunc("POST /v1/chat/completions", gw.chatCompletions)

	addr := fmt.Sprintf(":%d", cfg.HTTPPort)
	slog.Info("gateway listening", "addr", addr)

	server := &http.Server{
		Addr:         addr,
		Handler:      mux,
		ReadTimeout:  cfg.ReadTimeout,
		WriteTimeout: cfg.WriteTimeout,
		IdleTimeout:  cfg.IdleTimeout,
	}

	ctx, stop := signal.NotifyContext(context.Background(), syscall.SIGINT, syscall.SIGTERM)
	defer stop()

	go func() {
		if err := server.ListenAndServe(); err != nil && !errors.Is(err, http.ErrServerClosed) {
			slog.Error("http server error", "error", err)
			stop()
		}
	}()

	<-ctx.Done()
	shutdownCtx, cancel := context.WithTimeout(context.Background(), 10*time.Second)
	defer cancel()

	slog.Info("shutting down")
	if err := server.Shutdown(shutdownCtx); err != nil {
		slog.Error("shutdown error", "error", err)
	}
}

func parseLevel(level string) slog.Level {
	switch strings.ToLower(level) {
	case "debug":
		return slog.LevelDebug
	case "info":
		return slog.LevelInfo
	case "warn", "warning":
		return slog.LevelWarn
	case "error":
		return slog.LevelError
	default:
		return slog.LevelInfo
	}
}
