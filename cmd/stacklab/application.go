package main

import (
	"context"
	"errors"
	"fmt"
	"log/slog"
	"sync"
	"time"

	"stacklab/internal/audit"
	"stacklab/internal/auth"
	"stacklab/internal/config"
	"stacklab/internal/configworkspace"
	"stacklab/internal/dockeradmin"
	"stacklab/internal/dockerregistryauth"
	"stacklab/internal/gitworkspace"
	"stacklab/internal/hostinfo"
	"stacklab/internal/httpapi"
	"stacklab/internal/imageupdates"
	"stacklab/internal/jobs"
	"stacklab/internal/lifecycle"
	"stacklab/internal/maintenance"
	"stacklab/internal/maintenancejobs"
	"stacklab/internal/notifications"
	"stacklab/internal/retention"
	"stacklab/internal/scheduler"
	"stacklab/internal/selfupdate"
	"stacklab/internal/servicemetrics"
	"stacklab/internal/stacks"
	"stacklab/internal/stackworkspace"
	"stacklab/internal/store"
	"stacklab/internal/terminal"
	"stacklab/internal/workspacerepair"
)

type backgroundWorker struct {
	name string
	run  func(context.Context)
}

type jobNotificationDispatcher interface {
	DispatchJob(context.Context, store.Job) error
}

// application is the composition root and lifecycle owner for one Stacklab
// process. Every service instance is created once here and injected into its
// consumers; constructors below do not start goroutines.
type application struct {
	handler    *httpapi.Handler
	store      *store.Store
	workers    *lifecycle.Manager
	auth       *auth.Service
	terminals  *terminal.Service
	background []backgroundWorker

	startOnce sync.Once
	startErr  error

	shutdownOnce sync.Once
	shutdownErr  error
}

func newApplication(parent context.Context, cfg config.Config, logger *slog.Logger) (*application, error) {
	if parent == nil {
		parent = context.Background()
	}
	if logger == nil {
		return nil, errors.New("application logger is required")
	}
	if err := store.EnsureDataDirectory(cfg.DataDir); err != nil {
		return nil, fmt.Errorf("prepare data directory: %w", err)
	}
	appStore, err := store.Open(cfg.DatabasePath)
	if err != nil {
		return nil, fmt.Errorf("open sqlite store: %w", err)
	}
	closeStore := true
	defer func() {
		if closeStore {
			_ = appStore.Close()
		}
	}()

	workers := lifecycle.New(parent)
	authService := auth.NewService(cfg, appStore)
	auditService := audit.NewService(appStore)
	jobService := jobs.NewService(appStore)
	notificationService := notifications.NewService(appStore, logger)
	stackReader := stacks.NewServiceReader(cfg, logger)
	stackReader.AttachStore(appStore)
	statsCollector := stacks.NewStatsCollector(logger)
	stackReader.AttachStatsCollector(statsCollector)

	imageUpdateService := imageupdates.NewService(logger, appStore)
	if err := imageUpdateService.Load(parent); err != nil {
		logger.Warn("load image update status failed", slog.String("err", err.Error()))
	}
	stackReader.AttachUpdateStatus(func() map[string]stacks.ImageUpdateState {
		cached := imageUpdateService.StatusByImage()
		result := make(map[string]stacks.ImageUpdateState, len(cached))
		for ref, status := range cached {
			result[ref] = stacks.ImageUpdateState{State: status.State, CheckedAt: status.CheckedAt}
		}
		return result
	})
	stackReader.AttachUpdateStatusCacheUpdater(imageUpdateService.CacheStatuses)

	hostInfoService := hostinfo.NewServiceWithStore(cfg, time.Now().UTC(), appStore)
	notificationService.SetStackInspector(stackReader)
	notificationService.SetStacklabLogReader(hostInfoService)
	maintenanceService := maintenance.NewService()
	maintenanceRunner := maintenancejobs.NewService(logger, jobService, auditService, stackReader, maintenanceService)
	schedulerService := scheduler.NewService(appStore, auditService, maintenanceRunner, stackReader, logger)
	selfUpdateService := selfupdate.NewService(cfg, appStore, jobService, auditService, notificationService, logger)
	retentionService := retention.NewService(appStore, logger)
	workspaceRepairer := workspacerepair.NewService(cfg)
	serviceMetrics := servicemetrics.New(time.Now().UTC())

	terminalService := terminal.NewService(logger, terminal.Config{
		MaxSessionsPerOwner: 5,
		IdleTimeout:         30 * time.Minute,
		DetachGracePeriod:   time.Minute,
	}, func(event terminal.LifecycleEvent) {
		details := map[string]any{"container_id": event.ContainerID}
		if event.Reason != "" {
			details["reason"] = event.Reason
		}
		_ = auditService.RecordTerminalEvent(
			context.Background(),
			event.StackID,
			event.SessionID,
			event.ContainerID,
			"local",
			"terminal_"+event.Type,
			"succeeded",
			details,
		)
	})
	authService.SetSessionTerminationHook(func(termination auth.SessionTermination) {
		reason := "auth_" + string(termination.Reason)
		if termination.All {
			terminalService.CloseAll(reason)
			return
		}
		terminalService.CloseOwner(termination.SessionID, reason)
	})
	if err := authService.Bootstrap(parent); err != nil {
		if errors.Is(err, auth.ErrNotConfigured) {
			logger.Warn("authentication password not initialized; set STACKLAB_BOOTSTRAP_PASSWORD to create the first password")
		} else {
			_ = authService.Shutdown(context.Background())
			return nil, fmt.Errorf("bootstrap authentication: %w", err)
		}
	}
	reconcileInterruptedJobs(parent, jobService, auditService, logger)
	jobService.SetMetricsObserver(serviceMetrics)
	configureJobNotificationDispatch(jobService, workers, notificationService, logger)

	handler, err := httpapi.NewHandler(cfg, logger, httpapi.Dependencies{
		RuntimeContext:  workers.Context(),
		Workers:         workers,
		Auth:            authService,
		Audit:           auditService,
		Jobs:            jobService,
		Terminals:       terminalService,
		StackReader:     stackReader,
		ImageUpdates:    imageUpdateService,
		HostInfo:        hostInfoService,
		DockerAdmin:     dockeradmin.NewService(cfg),
		DockerRegistry:  dockerregistryauth.NewService(cfg),
		ConfigFiles:     configworkspace.NewServiceWithRepairer(cfg, workspaceRepairer),
		StackFiles:      stackworkspace.NewServiceWithRepairer(cfg, workspaceRepairer),
		GitStatus:       gitworkspace.NewService(cfg),
		Maintenance:     maintenanceService,
		MaintenanceJobs: maintenanceRunner,
		Notifications:   notificationService,
		Schedules:       schedulerService,
		SelfUpdate:      selfUpdateService,
		ReadinessChecks: httpapi.DefaultReadinessChecks(cfg, appStore, workers.Context()),
		ServiceMetrics:  serviceMetrics,
	})
	if err != nil {
		_ = authService.Shutdown(context.Background())
		return nil, fmt.Errorf("initialize HTTP handler: %w", err)
	}

	closeStore = false
	return &application{
		handler:   handler,
		store:     appStore,
		workers:   workers,
		auth:      authService,
		terminals: terminalService,
		background: []backgroundWorker{
			{name: "stack stats", run: statsCollector.Run},
			{name: "host metrics", run: hostInfoService.RunMetrics},
			{name: "notifications", run: notificationService.RunBackground},
			{name: "scheduler", run: schedulerService.RunBackground},
			{name: "self-update", run: selfUpdateService.RunBackground},
			{name: "retention", run: retentionService.RunBackground},
		},
	}, nil
}

func configureJobNotificationDispatch(jobService *jobs.Service, workers *lifecycle.Manager, dispatcher jobNotificationDispatcher, logger *slog.Logger) {
	jobService.SetTerminalHook(func(job store.Job) {
		dispatch := func(ctx context.Context) {
			dispatchCtx, cancel := context.WithTimeout(context.WithoutCancel(ctx), 5*time.Second)
			defer cancel()
			if err := dispatcher.DispatchJob(dispatchCtx, job); err != nil {
				logger.Warn("dispatch notification failed", slog.String("job_id", job.ID), slog.String("err", err.Error()))
			}
		}
		if workers.Go(dispatch) {
			return
		}

		// A running job may reach its terminal state after shutdown has stopped
		// worker admission. Dispatch synchronously so the final notification is
		// drained before shared persistence is closed.
		dispatch(workers.Context())
	})
}

func reconcileInterruptedJobs(ctx context.Context, jobService *jobs.Service, auditService *audit.Service, logger *slog.Logger) {
	reconciledJobs, err := jobService.ReconcileInterrupted(ctx)
	if err != nil {
		logger.Warn("failed to reconcile interrupted jobs", slog.String("err", err.Error()))
		return
	}
	for _, job := range reconciledJobs {
		if err := auditService.RecordJob(ctx, job, map[string]any{
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

func (a *application) Start() error {
	if a == nil || a.workers == nil {
		return errors.New("application runtime is not initialized")
	}
	a.startOnce.Do(func() {
		for _, worker := range a.background {
			if !a.workers.Go(worker.run) {
				a.startErr = fmt.Errorf("start %s background worker: runtime is stopping", worker.name)
				return
			}
		}
	})
	return a.startErr
}

func (a *application) Shutdown(ctx context.Context) error {
	if a == nil {
		return nil
	}
	a.shutdownOnce.Do(func() {
		if a.workers != nil {
			a.workers.Stop()
		}
		var shutdownErrors []error
		if a.handler != nil {
			if err := a.handler.Shutdown(ctx); err != nil {
				shutdownErrors = append(shutdownErrors, err)
			}
		}
		if a.auth != nil {
			if err := a.auth.Shutdown(ctx); err != nil {
				shutdownErrors = append(shutdownErrors, err)
			}
		}
		if a.terminals != nil {
			a.terminals.Shutdown("server_shutdown")
		}
		if a.workers != nil {
			if err := a.workers.Wait(ctx); err != nil {
				shutdownErrors = append(shutdownErrors, err)
			}
		}
		if len(shutdownErrors) == 0 && a.store != nil {
			if err := a.store.Close(); err != nil {
				shutdownErrors = append(shutdownErrors, err)
			}
		}
		a.shutdownErr = errors.Join(shutdownErrors...)
	})
	return a.shutdownErr
}
