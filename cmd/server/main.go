package main

import (
	"context"
	"errors"
	"log/slog"
	"net/http"
	"os"
	"os/signal"
	"syscall"
	"time"

	"github.com/lllmml/agenthub/internal/agent"
)

func main() {
	// Dependency wiring: MemoryRepository -> Service -> Handler -> ServeMux.
	repo := agent.NewMemoryRepository()
	service := agent.NewService(repo)
	handler := agent.NewHandler(service)

	mux := http.NewServeMux()
	handler.RegisterRoutes(mux)

	// Default per the API contract; PORT overrides for environments
	// where :8080 is unavailable (e.g. this dev machine runs nginx on it).
	addr := ":8080"
	if p := os.Getenv("PORT"); p != "" {
		addr = ":" + p
	}

	server := &http.Server{
		Addr:              addr,
		Handler:           mux,
		ReadHeaderTimeout: 5 * time.Second,
	}

	// Stop on SIGINT (Ctrl+C) or SIGTERM.
	ctx, stop := signal.NotifyContext(context.Background(), os.Interrupt, syscall.SIGTERM)
	defer stop()

	errCh := make(chan error, 1)
	go func() {
		errCh <- server.ListenAndServe()
	}()
	slog.Info("server listening", "addr", server.Addr)

	select {
	case err := <-errCh:
		// The server exited on its own (e.g. failed to bind).
		if err != nil && !errors.Is(err, http.ErrServerClosed) {
			slog.Error("server failed", "error", err)
			os.Exit(1)
		}
	case <-ctx.Done():
		// Termination signal received: drain in-flight requests, then exit.
		slog.Info("shutdown signal received")
		shutdownCtx, cancel := context.WithTimeout(context.Background(), 10*time.Second)
		defer cancel()
		if err := server.Shutdown(shutdownCtx); err != nil {
			slog.Error("graceful shutdown failed", "error", err)
		}
		if err := <-errCh; err != nil && !errors.Is(err, http.ErrServerClosed) {
			slog.Error("server exited with error", "error", err)
			os.Exit(1)
		}
		slog.Info("server stopped")
	}
}
