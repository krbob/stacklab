package lifecycle

import (
	"context"
	"errors"
	"testing"
	"time"
)

func TestManagerShutdownCancelsAndWaitsForWorkers(t *testing.T) {
	manager := New(context.Background())
	started := make(chan struct{})
	finished := make(chan struct{})
	if !manager.Go(func(ctx context.Context) {
		close(started)
		<-ctx.Done()
		close(finished)
	}) {
		t.Fatal("Go() rejected worker before shutdown")
	}
	<-started

	shutdownCtx, cancel := context.WithTimeout(context.Background(), time.Second)
	defer cancel()
	if err := manager.Shutdown(shutdownCtx); err != nil {
		t.Fatalf("Shutdown() error = %v", err)
	}
	select {
	case <-finished:
	default:
		t.Fatal("Shutdown() returned before worker finished")
	}
	if manager.Go(func(context.Context) {}) {
		t.Fatal("Go() accepted worker after shutdown")
	}
}

func TestManagerShutdownHonorsDeadline(t *testing.T) {
	manager := New(context.Background())
	release := make(chan struct{})
	if !manager.Go(func(context.Context) {
		<-release
	}) {
		t.Fatal("Go() rejected worker before shutdown")
	}

	shutdownCtx, cancel := context.WithTimeout(context.Background(), 10*time.Millisecond)
	defer cancel()
	err := manager.Shutdown(shutdownCtx)
	if !errors.Is(err, ErrShutdownTimedOut) || !errors.Is(err, context.DeadlineExceeded) {
		t.Fatalf("Shutdown() error = %v, want shutdown timeout and context deadline", err)
	}
	close(release)
	if err := manager.Wait(context.Background()); err != nil {
		t.Fatalf("Wait() after release error = %v", err)
	}
}
