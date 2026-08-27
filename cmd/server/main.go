package main

import (
	"context"
	"errors"
	"fmt"
	"log/slog"
	"net/http"
	"os"
	"os/signal"
	"syscall"
	"time"

	"github.com/jackc/pgx/v5/pgxpool"
	"github.com/lllmml/agenthub/internal/agent"
	"github.com/redis/go-redis/v9"
)

func main() {
	if err := run(); err != nil {
		slog.Error("server exited with error", "error", err)
		os.Exit(1)
	}
}

func run() error {
	// Database configuration is mandatory: runtime persistence uses
	// PostgreSQL only. There is deliberately no fallback to in-memory
	// storage, which would silently lose data on restart.
	dsn := os.Getenv("DATABASE_URL")
	if dsn == "" {
		return errors.New("DATABASE_URL is required (e.g. postgres://agenthub:<your-password>@localhost:5432/agenthub?sslmode=disable)")
	}

	// Stop on SIGINT (Ctrl+C) or SIGTERM.
	ctx, stop := signal.NotifyContext(context.Background(), os.Interrupt, syscall.SIGTERM)
	defer stop()

	// One application-level pool, reused by every request. pgxpool
	// defaults apply; the pool bounds database concurrency.
	pool, err := pgxpool.New(ctx, dsn)
	if err != nil {
		return fmt.Errorf("parse database configuration: %w", err)
	}
	defer pool.Close()

	// Bounded startup context so an unreachable database cannot hang
	// the process. Runtime requests use their own contexts, not this.
	pingCtx, cancel := context.WithTimeout(context.Background(), 5*time.Second)
	if err := pool.Ping(pingCtx); err != nil {
		cancel()
		return fmt.Errorf("database unreachable: %w", err)
	}
	cancel()

	// Redis is optional and only a performance layer. An absent or
	// broken REDIS_URL must never prevent startup: the cache degrades
	// to a no-op and PostgreSQL keeps serving every request.
	var cache agent.AgentCache = agent.NewNoopAgentCache()
	if url := os.Getenv("REDIS_URL"); url != "" {
		opts, err := redis.ParseURL(url)
		if err != nil {
			slog.Warn("invalid REDIS_URL; Redis caching disabled", "error", err)
		} else {
			// Bound Redis operations so a slow or broken cache fails
			// fast enough for the Service to fall back to PostgreSQL.
			opts.DialTimeout = 1 * time.Second
			opts.ReadTimeout = 1 * time.Second
			opts.WriteTimeout = 1 * time.Second

			client := redis.NewClient(opts)

			// Bounded startup connectivity check: unlike PostgreSQL,
			// an unreachable Redis is a warning, not a failure.
			pingCtx, cancel := context.WithTimeout(context.Background(), 3*time.Second)
			err := client.Ping(pingCtx).Err()
			cancel()
			if err != nil {
				_ = client.Close()
				slog.Warn("Redis unreachable at startup; Redis caching disabled, continuing with PostgreSQL", "error", err)
			} else {
				// LIFO defer ordering closes Redis before the pool.
				defer client.Close()
				cache = agent.NewRedisAgentCache(client, agent.DefaultCacheTTL)
				slog.Info("Redis caching enabled", "addr", opts.Addr)
			}
		}
	}

	// Dependency wiring: Repository + Cache -> Service -> Handler -> ServeMux.
	repo := agent.NewPostgresRepository(pool)
	service := agent.NewService(repo, cache)
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

	errCh := make(chan error, 1)
	go func() {
		errCh <- server.ListenAndServe()
	}()
	slog.Info("server listening", "addr", server.Addr)

	select {
	case err := <-errCh:
		// The server exited on its own (e.g. failed to bind).
		if err != nil && !errors.Is(err, http.ErrServerClosed) {
			return err
		}
		return nil
	case <-ctx.Done():
		// Termination signal received: stop accepting requests, drain
		// in-flight requests within the timeout, then close the pool.
		slog.Info("shutdown signal received")
		shutdownCtx, cancel := context.WithTimeout(context.Background(), 10*time.Second)
		defer cancel()
		if err := server.Shutdown(shutdownCtx); err != nil {
			slog.Error("graceful shutdown failed", "error", err)
		}
		if err := <-errCh; err != nil && !errors.Is(err, http.ErrServerClosed) {
			return err
		}
		slog.Info("server stopped")
		return nil
	}
}
