package backup

import (
	"context"
	"encoding/json"
	"os/exec"
	"strings"
	"testing"
	"time"
)

func TestProgressBufferBoundsLongStreamsAndPublishesOnlyNumbers(t *testing.T) {
	updates := []transferProgress{}
	buffer := progressBuffer{callback: func(p transferProgress) { updates = append(updates, p) }}
	secret := strings.Repeat("private-path-", 100)
	line := []byte(`{"message_type":"status","bytes_done":64,"total_bytes":128,"files_done":1,"total_files":2,"current_files":["` + secret + `"]}` + "\n")
	// More than the response cap remains safe because intermediate status lines
	// are consumed, not retained alongside the terminal snapshot summary.
	for index := 0; index < 20000; index++ {
		if _, err := buffer.Write(line); err != nil {
			t.Fatal(err)
		}
	}
	summary := `{"message_type":"summary","snapshot_id":"aabbccdd","total_bytes_processed":128,"total_files_processed":2,"data_added":32}`
	_, _ = buffer.Write([]byte(summary[:33]))
	_, _ = buffer.Write([]byte(summary[33:]))
	buffer.finish()
	if buffer.output.overflow || strings.Contains(buffer.output.String(), "private-path") || buffer.output.String() != summary {
		t.Fatalf("retained output=%s overflow=%v", buffer.output.String(), buffer.output.overflow)
	}
	if len(updates) < 2 {
		t.Fatal("missing live status or terminal summary")
	}
	last := updates[len(updates)-1]
	if last.BytesProcessed != 128 || last.BytesTotal != 128 || last.FilesProcessed != 2 || last.BytesAdded != 32 {
		t.Fatalf("summary=%#v", last)
	}
	encoded, err := json.Marshal(updates)
	if err != nil {
		t.Fatal(err)
	}
	if strings.Contains(string(encoded), "private-path") {
		t.Fatal("private subprocess field escaped allow-list")
	}
}
func TestNativeRunnerProgressStreamAndStderrIsolation(t *testing.T) {
	shell, err := exec.LookPath("sh")
	if err != nil {
		t.Skip("shell unavailable")
	}
	observed := transferProgress{}
	output, err := nativeRunner{}.RunProgress(context.Background(), shell, []string{"-c", `printf '%s\n' '{"message_type":"status","bytes_done":10,"total_bytes":20}' '{"message_type":"summary","snapshot_id":"aabbccdd","total_bytes_processed":20,"total_files_processed":2,"data_added":3}'; printf 'private diagnostic' >&2`}, cleanProcessEnvironment(), "", func(p transferProgress) { observed = p })
	if err != nil {
		t.Fatal(err)
	}
	if observed.BytesProcessed != 20 || strings.Contains(string(output), "private diagnostic") || strings.Contains(string(output), `"status"`) {
		t.Fatalf("progress=%#v output=%s", observed, output)
	}
}
func TestRunPersistsSummaryAndPhaseTimings(t *testing.T) {
	s, _, runner := newTestService(t)
	destination := addDestination(t, s, "A")
	plan := addPlan(t, s, destination)
	s.runner = summaryRunner{wrapped: runner}
	job, run, err := s.StartRun(context.Background(), plan.ID, RunRequest{}, "tester")
	if err != nil {
		t.Fatal(err)
	}
	s.ExecuteRun(context.Background(), job, run.ID)
	view, err := s.Overview(context.Background())
	if err != nil {
		t.Fatal(err)
	}
	completed := view.Runs[0]
	if completed.Phase != "finished" || completed.CompletedAt == nil || completed.DurationSeconds <= 0 || completed.CaptureDurationSeconds <= 0 {
		t.Fatalf("run timings=%#v", completed)
	}
	copy := completed.Copies[0]
	if copy.BytesProcessed != 128 || copy.FilesProcessed != 4 || copy.BytesAdded != 32 || copy.CompletedAt == nil || copy.StartedAt == nil || copy.DurationSeconds <= 0 {
		t.Fatalf("copy progress=%#v", copy)
	}
	events, err := s.jobs.ReplayEvents(context.Background(), job.ID)
	if err != nil {
		t.Fatal(err)
	}
	found := false
	for _, event := range events {
		if event.Progress != nil && event.Progress.Phase == "backup_upload" {
			found = true
			if event.Progress.Completed != 128 || event.Progress.Total != 128 || event.Progress.Unit != "bytes" {
				t.Fatalf("event=%#v", event)
			}
		}
	}
	if !found {
		t.Fatal("missing structured job progress")
	}
}

type summaryRunner struct{ wrapped *recordingRunner }

func (r summaryRunner) Run(ctx context.Context, name string, args, env []string, dir string) ([]byte, error) {
	for _, arg := range args {
		if arg == "backup" {
			return []byte(`{"message_type":"summary","snapshot_id":"aabbccddeeff0011","total_bytes_processed":128,"total_files_processed":4,"data_added":32}`), nil
		}
	}
	return r.wrapped.Run(ctx, name, args, env, dir)
}

type liveProgressRunner struct {
	wrapped *recordingRunner
	entered chan struct{}
}

func (r liveProgressRunner) Run(ctx context.Context, name string, args, env []string, dir string) ([]byte, error) {
	return r.wrapped.Run(ctx, name, args, env, dir)
}
func (r liveProgressRunner) RunProgress(ctx context.Context, name string, args, env []string, dir string, callback progressCallback) ([]byte, error) {
	for _, arg := range args {
		if arg == "backup" {
			close(r.entered)
			for index := int64(1); index <= 100; index++ {
				callback(transferProgress{BytesProcessed: index, BytesTotal: 100, FilesProcessed: index, FilesTotal: 100})
				time.Sleep(time.Millisecond)
			}
			return []byte(`{"message_type":"summary","snapshot_id":"aabbccddeeff0011","total_bytes_processed":100,"total_files_processed":100}`), nil
		}
	}
	return r.Run(ctx, name, args, env, dir)
}
func TestLiveProgressAndOverviewOwnIndependentRunSnapshots(t *testing.T) {
	s, _, runner := newTestService(t)
	destination := addDestination(t, s, "Live")
	plan := addPlan(t, s, destination)
	entered := make(chan struct{})
	s.runner = liveProgressRunner{wrapped: runner, entered: entered}
	job, run, err := s.StartRun(context.Background(), plan.ID, RunRequest{}, "tester")
	if err != nil {
		t.Fatal(err)
	}
	done := make(chan struct{})
	go func() { defer close(done); s.ExecuteRun(context.Background(), job, run.ID) }()
	select {
	case <-entered:
	case <-time.After(5 * time.Second):
		t.Fatal("upload did not start")
	}
	queries := 0
	for {
		select {
		case <-done:
			if queries < 10 {
				t.Fatalf("insufficient concurrent observations: %d", queries)
			}
			return
		default:
			view, err := s.Overview(context.Background())
			if err != nil {
				t.Fatal(err)
			}
			if len(view.Runs) != 1 {
				t.Fatalf("missing run: %#v", view.Runs)
			}
			queries++
		}
	}
}
