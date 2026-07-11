package main

import (
	"context"
	"errors"
	"log/slog"
	"net/http"
	"os"
	"os/signal"
	"stacklab/internal/config"
	"stacklab/internal/stacks"
	"syscall"
	"time"
)

func main() {
	os.Exit(run())
}

func run() int {
	cfg := config.Load()
	logger := slog.New(slog.NewTextHandler(os.Stdout, &slog.HandlerOptions{
		Level: cfg.LogLevel,
	}))

	app, err := newApplication(context.Background(), cfg, logger)
	if err != nil {
		logger.Error("failed to initialize application", slog.String("err", err.Error()))
		return 1
	}

	signalCtx, stop := signal.NotifyContext(context.Background(), os.Interrupt, syscall.SIGTERM)
	defer stop()
	if err := app.Start(); err != nil {
		logger.Error("failed to start application", slog.String("err", err.Error()))
		shutdownCtx, cancelShutdown := context.WithTimeout(context.Background(), 25*time.Second)
		defer cancelShutdown()
		_ = app.Shutdown(shutdownCtx)
		return 1
	}

	srv := &http.Server{
		Addr:              cfg.HTTPAddr,
		Handler:           app.handler,
		ReadTimeout:       30 * time.Second,
		ReadHeaderTimeout: 5 * time.Second,
	}

	logger.Info("starting stacklab",
		slog.String("version", stacks.AppVersion),
		slog.String("commit", stacks.AppCommit),
		slog.String("http_addr", cfg.HTTPAddr),
		slog.String("root", cfg.RootDir),
		slog.String("data_dir", cfg.DataDir),
		slog.String("database_path", cfg.DatabasePath),
		slog.String("frontend_dist", cfg.FrontendDistDir),
	)

	serveResult := make(chan error, 1)
	go func() {
		serveResult <- srv.ListenAndServe()
	}()

	var serveErr error
	select {
	case serveErr = <-serveResult:
	case <-signalCtx.Done():
	}

	logger.Info("shutting down stacklab")
	shutdownCtx, cancelShutdown := context.WithTimeout(context.Background(), 25*time.Second)
	defer cancelShutdown()

	shutdownErr := shutdownApplication(shutdownCtx, srv, app)
	if shutdownErr != nil {
		logger.Error("graceful shutdown failed", slog.String("err", shutdownErr.Error()))
	}
	if serveErr == nil {
		serveErr = <-serveResult
	}
	if serveErr != nil && !errors.Is(serveErr, http.ErrServerClosed) {
		logger.Error("stacklab exited with error", slog.String("err", serveErr.Error()))
		return 1
	}
	if shutdownErr != nil {
		return 1
	}
	return 0
}

type shutdownComponent interface {
	Shutdown(context.Context) error
}

func shutdownApplication(ctx context.Context, stopAccepting, runtime shutdownComponent) error {
	var shutdownErrors []error
	if stopAccepting != nil {
		if err := stopAccepting.Shutdown(ctx); err != nil {
			shutdownErrors = append(shutdownErrors, err)
		}
	}
	if runtime != nil {
		if err := runtime.Shutdown(ctx); err != nil {
			shutdownErrors = append(shutdownErrors, err)
		}
	}
	return errors.Join(shutdownErrors...)
}
