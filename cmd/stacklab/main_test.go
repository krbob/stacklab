package main

import (
	"context"
	"io"
	"log/slog"
	"net/http"
	"net/http/httptest"
	"os"
	"path/filepath"
	"reflect"
	"sync"
	"testing"
	"time"

	"stacklab/internal/config"
	"stacklab/internal/jobs"
	"stacklab/internal/lifecycle"
	"stacklab/internal/store"
)

type recordingJobNotificationDispatcher struct {
	calls chan recordedJobNotification
}

type recordedJobNotification struct {
	job        store.Job
	contextErr error
}

func (d *recordingJobNotificationDispatcher) DispatchJob(ctx context.Context, job store.Job) error {
	d.calls <- recordedJobNotification{job: job, contextErr: ctx.Err()}
	return nil
}

type blockingShutdownComponent struct {
	name    string
	mu      *sync.Mutex
	order   *[]string
	started chan struct{}
	release chan struct{}
}

func (c *blockingShutdownComponent) Shutdown(context.Context) error {
	c.mu.Lock()
	*c.order = append(*c.order, c.name)
	c.mu.Unlock()
	if c.started != nil {
		close(c.started)
	}
	if c.release != nil {
		<-c.release
	}
	return nil
}

func TestShutdownApplicationStopsServerBeforeRuntime(t *testing.T) {
	var mu sync.Mutex
	var order []string
	serverStarted := make(chan struct{})
	releaseServer := make(chan struct{})
	server := &blockingShutdownComponent{name: "server", mu: &mu, order: &order, started: serverStarted, release: releaseServer}
	runtime := &blockingShutdownComponent{name: "runtime", mu: &mu, order: &order}
	done := make(chan error, 1)
	go func() {
		done <- shutdownApplication(context.Background(), server, runtime)
	}()

	select {
	case <-serverStarted:
	case <-time.After(time.Second):
		t.Fatal("server shutdown did not start")
	}
	mu.Lock()
	beforeRelease := append([]string(nil), order...)
	mu.Unlock()
	if !reflect.DeepEqual(beforeRelease, []string{"server"}) {
		t.Fatalf("shutdown order before server release = %v, want [server]", beforeRelease)
	}

	close(releaseServer)
	select {
	case err := <-done:
		if err != nil {
			t.Fatalf("shutdownApplication() error = %v", err)
		}
	case <-time.After(time.Second):
		t.Fatal("shutdownApplication() did not finish")
	}
	mu.Lock()
	gotOrder := append([]string(nil), order...)
	mu.Unlock()
	if !reflect.DeepEqual(gotOrder, []string{"server", "runtime"}) {
		t.Fatalf("shutdown order = %v", gotOrder)
	}
}

func TestApplicationExplicitlyOwnsWorkersAndClosesStoreAfterDrain(t *testing.T) {
	appStore, err := store.Open(t.TempDir() + "/stacklab.db")
	if err != nil {
		t.Fatalf("store.Open() error = %v", err)
	}
	t.Cleanup(func() { _ = appStore.Close() })

	workers := lifecycle.New(context.Background())
	workerStarted := make(chan struct{})
	workerStoreResult := make(chan error, 1)
	app := &application{
		store:   appStore,
		workers: workers,
		background: []backgroundWorker{{
			name: "test worker",
			run: func(ctx context.Context) {
				close(workerStarted)
				<-ctx.Done()
				workerStoreResult <- appStore.Ping(context.Background())
			},
		}},
	}

	select {
	case <-workerStarted:
		t.Fatal("background worker started during application construction")
	default:
	}
	if err := app.Start(); err != nil {
		t.Fatalf("application.Start() error = %v", err)
	}
	if err := app.Start(); err != nil {
		t.Fatalf("second application.Start() error = %v", err)
	}
	select {
	case <-workerStarted:
	case <-time.After(time.Second):
		t.Fatal("background worker did not start")
	}

	shutdownCtx, cancel := context.WithTimeout(context.Background(), time.Second)
	defer cancel()
	if err := app.Shutdown(shutdownCtx); err != nil {
		t.Fatalf("application.Shutdown() error = %v", err)
	}
	select {
	case err := <-workerStoreResult:
		if err != nil {
			t.Fatalf("store was unavailable before worker drain: %v", err)
		}
	case <-time.After(time.Second):
		t.Fatal("worker did not finish before application shutdown returned")
	}
	if err := appStore.Ping(context.Background()); err == nil {
		t.Fatal("store remained open after application shutdown")
	}
}

func TestJobNotificationDispatchFallsBackAfterWorkerAdmissionStops(t *testing.T) {
	appStore, err := store.Open(filepath.Join(t.TempDir(), "stacklab.db"))
	if err != nil {
		t.Fatalf("store.Open() error = %v", err)
	}
	t.Cleanup(func() { _ = appStore.Close() })

	jobService := jobs.NewService(appStore)
	workers := lifecycle.New(context.Background())
	dispatcher := &recordingJobNotificationDispatcher{calls: make(chan recordedJobNotification, 1)}
	logger := slog.New(slog.NewTextHandler(io.Discard, nil))
	configureJobNotificationDispatch(jobService, workers, dispatcher, logger)

	job, err := jobService.Start(context.Background(), "demo", "up", "test")
	if err != nil {
		t.Fatalf("jobs.Start() error = %v", err)
	}
	workers.Stop()
	finishedJob, err := jobService.FinishSucceeded(context.Background(), job)
	if err != nil {
		t.Fatalf("jobs.FinishSucceeded() error = %v", err)
	}

	select {
	case call := <-dispatcher.calls:
		if call.job.ID != finishedJob.ID || call.job.State != "succeeded" {
			t.Fatalf("dispatched job = %#v, want terminal job %#v", call.job, finishedJob)
		}
		if call.contextErr != nil {
			t.Fatalf("dispatch context was already cancelled: %v", call.contextErr)
		}
	default:
		t.Fatal("terminal notification was dropped after worker admission stopped")
	}

	shutdownCtx, cancel := context.WithTimeout(context.Background(), time.Second)
	defer cancel()
	if err := workers.Wait(shutdownCtx); err != nil {
		t.Fatalf("workers.Wait() error = %v", err)
	}
}

func TestNewApplicationBuildsHandlerWithoutStartingRuntime(t *testing.T) {
	tempDir := t.TempDir()
	cfg := config.Config{
		RootDir:                 filepath.Join(tempDir, "root"),
		DataDir:                 filepath.Join(tempDir, "var"),
		DatabasePath:            filepath.Join(tempDir, "var", "stacklab.db"),
		FrontendDistDir:         filepath.Join(tempDir, "frontend"),
		BootstrapPassword:       "test-password",
		SessionCookieName:       "stacklab_session",
		SessionIdleTimeout:      30 * time.Minute,
		SessionAbsoluteLifetime: 24 * time.Hour,
	}
	if err := os.MkdirAll(cfg.FrontendDistDir, 0o755); err != nil {
		t.Fatalf("MkdirAll(frontend) error = %v", err)
	}
	if err := os.WriteFile(filepath.Join(cfg.FrontendDistDir, "index.html"), []byte("<!doctype html>"), 0o644); err != nil {
		t.Fatalf("WriteFile(index.html) error = %v", err)
	}
	logger := slog.New(slog.NewTextHandler(io.Discard, nil))
	app, err := newApplication(context.Background(), cfg, logger)
	if err != nil {
		t.Fatalf("newApplication() error = %v", err)
	}
	t.Cleanup(func() {
		shutdownCtx, cancel := context.WithTimeout(context.Background(), 5*time.Second)
		defer cancel()
		if err := app.Shutdown(shutdownCtx); err != nil {
			t.Errorf("application.Shutdown() error = %v", err)
		}
	})

	if app.handler == nil || app.store == nil || app.workers == nil {
		t.Fatalf("incomplete application composition: %#v", app)
	}
	if len(app.background) != 6 {
		t.Fatalf("background worker count = %d, want 6", len(app.background))
	}
	select {
	case <-app.workers.Context().Done():
		t.Fatal("runtime context stopped during construction")
	default:
	}

	request := httptest.NewRequest(http.MethodGet, "http://stacklab.test/api/live", nil)
	response := httptest.NewRecorder()
	app.handler.ServeHTTP(response, request)
	if response.Code != http.StatusOK {
		t.Fatalf("GET /api/live status = %d, want %d", response.Code, http.StatusOK)
	}
	if response.Header().Get("X-Request-ID") == "" {
		t.Fatal("GET /api/live omitted X-Request-ID")
	}
}
