package main

import (
	"context"
	"errors"
	"log/slog"
	"net/http"
	"os"
	"os/signal"
	"stacklab/internal/auth"
	"stacklab/internal/config"
	"stacklab/internal/httpapi"
	"stacklab/internal/store"
	"syscall"
	"time"
)

func main() {
	cfg := config.Load()
	logger := slog.New(slog.NewTextHandler(os.Stdout, &slog.HandlerOptions{
		Level: cfg.LogLevel,
	}))

	authStore, err := store.Open(cfg.DatabasePath)
	if err != nil {
		logger.Error("failed to open sqlite store", slog.String("err", err.Error()))
		os.Exit(1)
	}
	defer func() {
		if err := authStore.Close(); err != nil {
			logger.Error("failed to close sqlite store", slog.String("err", err.Error()))
		}
	}()

	authService := auth.NewService(cfg, authStore)
	if err := authService.Bootstrap(context.Background()); err != nil {
		if errors.Is(err, auth.ErrNotConfigured) {
			logger.Warn("authentication password not initialized; set STACKLAB_BOOTSTRAP_PASSWORD to create the first password")
		} else {
			logger.Error("failed to bootstrap authentication", slog.String("err", err.Error()))
			os.Exit(1)
		}
	}

	handler, err := httpapi.NewHandler(cfg, logger, authService)
	if err != nil {
		logger.Error("failed to initialize HTTP handler", slog.String("err", err.Error()))
		os.Exit(1)
	}

	srv := &http.Server{
		Addr:              cfg.HTTPAddr,
		Handler:           handler,
		ReadHeaderTimeout: 5 * time.Second,
	}

	logger.Info("starting stacklab",
		slog.String("http_addr", cfg.HTTPAddr),
		slog.String("root", cfg.RootDir),
		slog.String("data_dir", cfg.DataDir),
		slog.String("database_path", cfg.DatabasePath),
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
