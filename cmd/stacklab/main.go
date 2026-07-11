package main

import (
	"context"
	"errors"
	"log/slog"
	"net/http"
	"os"
	"os/signal"
	"stacklab/internal/audit"
	"stacklab/internal/auth"
	"stacklab/internal/config"
	"stacklab/internal/hostinfo"
	"stacklab/internal/httpapi"
	"stacklab/internal/jobs"
	"stacklab/internal/lifecycle"
	"stacklab/internal/maintenance"
	"stacklab/internal/maintenancejobs"
	"stacklab/internal/notifications"
	"stacklab/internal/retention"
	"stacklab/internal/scheduler"
	"stacklab/internal/selfupdate"
	"stacklab/internal/stacks"
	"stacklab/internal/store"
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

	if err := store.EnsureDataDirectory(cfg.DataDir); err != nil {
		logger.Error("failed to prepare data directory", slog.String("err", err.Error()))
		return 1
	}
	authStore, err := store.Open(cfg.DatabasePath)
	if err != nil {
		logger.Error("failed to open sqlite store", slog.String("err", err.Error()))
		return 1
	}
	defer func() {
		if err := authStore.Close(); err != nil {
			logger.Error("failed to close sqlite store", slog.String("err", err.Error()))
		}
	}()

	authService := auth.NewService(cfg, authStore)
	auditService := audit.NewService(authStore)
	jobService := jobs.NewService(authStore)
	notificationService := notifications.NewService(authStore, logger)
	stackReader := stacks.NewServiceReader(cfg, logger)
	stackReader.AttachStore(authStore)
	notificationService.SetStackInspector(stackReader)
	notificationService.SetStacklabLogReader(hostinfo.NewService(cfg, time.Now().UTC()))
	maintenanceService := maintenance.NewService()
	maintenanceRunner := maintenancejobs.NewService(logger, jobService, auditService, stackReader, maintenanceService)
	schedulerService := scheduler.NewService(authStore, auditService, maintenanceRunner, stackReader, logger)
	selfUpdateService := selfupdate.NewService(cfg, authStore, jobService, auditService, notificationService, logger)
	retentionService := retention.NewService(authStore, logger)
	if err := authService.Bootstrap(context.Background()); err != nil {
		if errors.Is(err, auth.ErrNotConfigured) {
			logger.Warn("authentication password not initialized; set STACKLAB_BOOTSTRAP_PASSWORD to create the first password")
		} else {
			logger.Error("failed to bootstrap authentication", slog.String("err", err.Error()))
			return 1
		}
	}

	reconciledJobs, err := jobService.ReconcileInterrupted(context.Background())
	if err != nil {
		logger.Warn("failed to reconcile interrupted jobs", slog.String("err", err.Error()))
	} else {
		for _, job := range reconciledJobs {
			if err := auditService.RecordJob(context.Background(), job, map[string]any{
				"reconciled_on_startup": true,
				"interrupted_reason":    "stacklab_restart",
			}); err != nil {
				logger.Warn("record reconciled job audit failed", slog.String("job_id", job.ID), slog.String("err", err.Error()))
			}
			attrs := []any{
				slog.String("job_id", job.ID),
				slog.String("action", job.Action),
				slog.String("state", job.State),
			}
			if job.StackID != "" {
				attrs = append(attrs, slog.String("stack_id", job.StackID))
			}
			logger.Warn("reconciled interrupted job", attrs...)
		}
	}

	signalCtx, stop := signal.NotifyContext(context.Background(), os.Interrupt, syscall.SIGTERM)
	defer stop()
	appCtx, cancelApp := context.WithCancel(context.Background())
	defer cancelApp()
	background := lifecycle.New(appCtx)
	jobService.SetTerminalHook(func(job store.Job) {
		background.Go(func(ctx context.Context) {
			dispatchCtx, cancel := context.WithTimeout(context.WithoutCancel(ctx), 5*time.Second)
			defer cancel()
			if err := notificationService.DispatchJob(dispatchCtx, job); err != nil {
				logger.Warn("dispatch notification failed", slog.String("job_id", job.ID), slog.String("err", err.Error()))
			}
		})
	})

	handler, err := httpapi.NewHandlerWithContext(appCtx, cfg, logger, authService, auditService, jobService, notificationService, schedulerService, selfUpdateService)
	if err != nil {
		logger.Error("failed to initialize HTTP handler", slog.String("err", err.Error()))
		return 1
	}

	srv := &http.Server{
		Addr:              cfg.HTTPAddr,
		Handler:           handler,
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

	background.Go(func(ctx context.Context) { notificationService.RunBackground(ctx) })
	background.Go(func(ctx context.Context) { schedulerService.RunBackground(ctx) })
	background.Go(func(ctx context.Context) { selfUpdateService.RunBackground(ctx) })
	background.Go(func(ctx context.Context) { retentionService.RunBackground(ctx) })

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

	shutdownErr := shutdownApplication(shutdownCtx, srv, cancelApp, handler, background)
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

func shutdownApplication(ctx context.Context, stopAccepting shutdownComponent, cancelRuntime context.CancelFunc, components ...shutdownComponent) error {
	var shutdownErrors []error
	if stopAccepting != nil {
		if err := stopAccepting.Shutdown(ctx); err != nil {
			shutdownErrors = append(shutdownErrors, err)
		}
	}
	if cancelRuntime != nil {
		cancelRuntime()
	}
	for _, component := range components {
		if component == nil {
			continue
		}
		if err := component.Shutdown(ctx); err != nil {
			shutdownErrors = append(shutdownErrors, err)
		}
	}
	return errors.Join(shutdownErrors...)
}
