package main

import (
	"context"
	"errors"
	"log/slog"
	"net/http"
	"os"
	"os/signal"
	"stacklab/internal/config"
	"stacklab/internal/httpapi"
	"syscall"
	"time"
)

func main() {
	cfg := config.Load()
	logger := slog.New(slog.NewTextHandler(os.Stdout, &slog.HandlerOptions{
		Level: cfg.LogLevel,
	}))

	srv := &http.Server{
		Addr:              cfg.HTTPAddr,
		Handler:           httpapi.NewHandler(cfg, logger),
		ReadHeaderTimeout: 5 * time.Second,
	}

	logger.Info("starting stacklab",
		slog.String("http_addr", cfg.HTTPAddr),
		slog.String("root", cfg.RootDir),
		slog.String("data_dir", cfg.DataDir),
		slog.String("frontend_dist", cfg.FrontendDistDir),
	)

	ctx, stop := signal.NotifyContext(context.Background(), os.Interrupt, syscall.SIGTERM)
	defer stop()

	go func() {
		<-ctx.Done()

		shutdownCtx, cancel := context.WithTimeout(context.Background(), 10*time.Second)
		defer cancel()

		logger.Info("shutting down stacklab")
		if err := srv.Shutdown(shutdownCtx); err != nil {
			logger.Error("graceful shutdown failed", slog.String("err", err.Error()))
		}
	}()

	if err := srv.ListenAndServe(); err != nil && !errors.Is(err, http.ErrServerClosed) {
		logger.Error("stacklab exited with error", slog.String("err", err.Error()))
		os.Exit(1)
	}
}
