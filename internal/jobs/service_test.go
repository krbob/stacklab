package jobs

import (
	"context"
	"testing"

	"stacklab/internal/store"
)

func TestTerminalHookRunsOnFinishSucceeded(t *testing.T) {
	t.Parallel()

	jobStore := openJobsTestStore(t)
	service := NewService(jobStore)

	var terminal store.Job
	service.SetTerminalHook(func(job store.Job) {
		terminal = job
	})

	job, err := service.Start(context.Background(), "demo", "up", "local")
	if err != nil {
		t.Fatalf("Start() error = %v", err)
	}

	job, err = service.FinishSucceeded(context.Background(), job)
	if err != nil {
		t.Fatalf("FinishSucceeded() error = %v", err)
	}

	if terminal.ID != job.ID || terminal.State != "succeeded" {
		t.Fatalf("unexpected terminal hook payload: %#v", terminal)
	}
}

func openJobsTestStore(t *testing.T) *store.Store {
	t.Helper()

	s, err := store.Open(t.TempDir() + "/stacklab.db")
	if err != nil {
		t.Fatalf("store.Open() error = %v", err)
	}
	t.Cleanup(func() {
		_ = s.Close()
	})
	return s
}
