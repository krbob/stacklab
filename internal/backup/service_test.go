package backup

import (
	"context"
	"encoding/json"
	"errors"
	"fmt"
	"os"
	"path/filepath"
	"strings"
	"testing"
	"time"

	"stacklab/internal/config"
	"stacklab/internal/jobs"
	"stacklab/internal/store"
)

type fakeCapture struct {
	calls int
	err   error
}

func (c *fakeCapture) Recover(context.Context) error { return nil }
func (c *fakeCapture) Prepare(ctx context.Context, p Plan, id, dir string) (Manifest, error) {
	c.calls++
	if c.err != nil {
		return Manifest{}, c.err
	}
	if err := os.MkdirAll(dir, 0700); err != nil {
		return Manifest{}, err
	}
	if err := os.WriteFile(filepath.Join(dir, "payload"), []byte("recover me"), 0600); err != nil {
		return Manifest{}, err
	}
	return Manifest{Version: 1, RunID: id, PlanID: p.ID, CapturedAt: time.Now().UTC(), Consistency: p.Consistency, Sources: []ManifestSource{}}, nil
}

type recordingRunner struct {
	calls         [][]string
	repositories  []string
	failRepo      string
	failRetention bool
	priorSnapshot string
}

func (r *recordingRunner) Run(ctx context.Context, name string, args, env []string, dir string) ([]byte, error) {
	r.calls = append(r.calls, append([]string(nil), args...))
	repo := ""
	if len(args) > 1 {
		repo = args[1]
	}
	r.repositories = append(r.repositories, repo)
	for _, arg := range args {
		if arg == "snapshots" {
			if r.priorSnapshot != "" {
				return []byte(`[{"id":"` + r.priorSnapshot + `"}]`), nil
			}
			return []byte(`[]`), nil
		}
		if arg == "backup" {
			if repo == r.failRepo {
				return nil, errors.New("simulated destination unavailable")
			}
			return []byte(`{"message_type":"summary","snapshot_id":"aabbccddeeff0011"}`), nil
		}
		if arg == "forget" && r.failRetention {
			return nil, errors.New("retention unavailable")
		}
	}
	return []byte(`{}`), nil
}
func (r *recordingRunner) count(command, repository string) int {
	n := 0
	for i, args := range r.calls {
		if repository != "" && r.repositories[i] != repository {
			continue
		}
		for _, arg := range args {
			if arg == command {
				n++
			}
		}
	}
	return n
}

func newTestService(t *testing.T) (*Service, *fakeCapture, *recordingRunner) {
	t.Helper()
	dir := t.TempDir()
	st, err := store.Open(filepath.Join(dir, "state", "stacklab.db"))
	if err != nil {
		t.Fatal(err)
	}
	t.Cleanup(func() { _ = st.Close() })
	capture := &fakeCapture{}
	service := NewService(config.Config{RootDir: filepath.Join(dir, "workspace"), DataDir: filepath.Join(dir, "state"), DatabasePath: filepath.Join(dir, "state", "stacklab.db")}, jobs.NewService(st), capture)
	if service.initErr != nil {
		t.Fatal(service.initErr)
	}
	runner := &recordingRunner{}
	service.runner = runner
	return service, capture, runner
}
func addDestination(t *testing.T, s *Service, name string) Destination {
	t.Helper()
	parent := t.TempDir()
	identity, err := FilesystemIdentity(parent)
	if err != nil {
		t.Fatal(err)
	}
	d, err := s.SaveDestination(context.Background(), "", DestinationInput{Name: name, Kind: "local", Repository: filepath.Join(parent, "repository"), ExpectedFilesystem: identity, Secrets: &Secrets{Password: "test-secret-do-not-log"}})
	if err != nil {
		t.Fatal(err)
	}
	return d
}
func addPlan(t *testing.T, s *Service, destinations ...Destination) Plan {
	t.Helper()
	p := Plan{Name: "Test", Enabled: true, Consistency: "files", Sources: []Source{{ID: "source-a", Kind: "managed_config", Path: "app"}}}
	for _, d := range destinations {
		p.Destinations = append(p.Destinations, Assignment{DestinationID: d.ID, Enabled: true, Schedule: Schedule{Frequency: "manual"}, Retention: Retention{Daily: 7}})
	}
	p, err := s.SavePlan(context.Background(), "", p)
	if err != nil {
		t.Fatal(err)
	}
	return p
}
func TestPartialFailureRetryAndPersistence(t *testing.T) {
	s, capture, runner := newTestService(t)
	a := addDestination(t, s, "A")
	b := addDestination(t, s, "B")
	p := addPlan(t, s, a, b)
	runner.failRepo = b.Repository
	job, run, err := s.StartRun(context.Background(), p.ID, RunRequest{}, "tester")
	if err != nil {
		t.Fatal(err)
	}
	if _, err = s.jobs.StartWithResources(context.Background(), "other", "stack_restart", "tester", jobs.StackResource("other")); !errors.Is(err, jobs.ErrResourceConflict) {
		t.Fatalf("mutation conflict = %v", err)
	}
	s.ExecuteRun(context.Background(), job, run.ID)
	view, err := s.Overview(context.Background())
	if err != nil {
		t.Fatal(err)
	}
	run = view.Runs[0]
	if run.State != "failed" || run.CaptureState != "succeeded" || run.Copies[0].State != "succeeded" || run.Copies[1].State != "failed" {
		t.Fatalf("run = %#v", run)
	}
	// Restart retains the immutable original plan and already successful copy.
	s = NewService(s.cfg, s.jobs, capture)
	s.runner = runner
	runner.failRepo = ""
	job, _, err = s.StartRetry(context.Background(), run.ID, RunRequest{}, "tester")
	if err != nil {
		t.Fatal(err)
	}
	s.ExecuteRun(context.Background(), job, run.ID)
	view, _ = s.Overview(context.Background())
	if view.Runs[0].State != "succeeded" {
		t.Fatalf("retry = %#v", view.Runs[0])
	}
	if capture.calls != 1 || runner.count("backup", a.Repository) != 1 || runner.count("backup", b.Repository) != 2 {
		t.Fatalf("capture=%d uploads A=%d B=%d", capture.calls, runner.count("backup", a.Repository), runner.count("backup", b.Repository))
	}
	for _, args := range runner.calls {
		for i, arg := range args {
			if arg == "forget" {
				joined := strings.Join(args, " ")
				if !strings.Contains(joined, "stacklab,instance:"+s.state.InstanceID+",plan:"+p.ID) || !strings.Contains(joined, "--group-by host") {
					t.Fatalf("unscoped retention: %v (%d)", args, i)
				}
			}
		}
	}
}
func TestRunSelectedDestinationsDoesNotExpandAfterMatch(t *testing.T) {
	s, _, _ := newTestService(t)
	a := addDestination(t, s, "A")
	b := addDestination(t, s, "B")
	p := addPlan(t, s, a, b)
	job, run, err := s.StartRun(context.Background(), p.ID, RunRequest{DestinationIDs: []string{a.ID}}, "tester")
	if err != nil {
		t.Fatal(err)
	}
	defer s.finish(context.Background(), job, nil)
	if len(run.Copies) != 1 || run.Copies[0].DestinationID != a.ID {
		t.Fatalf("selected copies = %#v", run.Copies)
	}
}
func TestCredentialsNotExposedAndAbsentFilesystemFailsClosed(t *testing.T) {
	s, _, runner := newTestService(t)
	d := addDestination(t, s, "local")
	view, _ := s.Overview(context.Background())
	data, _ := json.Marshal(view)
	if strings.Contains(string(data), "test-secret") || strings.Contains(string(data), "password") {
		t.Fatalf("credentials leaked: %s", data)
	}
	for _, name := range []string{"credentials.json", "password", "ssh_key", "known_hosts", "rclone.conf"} {
		info, err := os.Stat(filepath.Join(s.secretDirectory(d.ID), name))
		if err != nil || info.Mode().Perm() != 0600 {
			t.Fatalf("secret %s mode/error=%v %v", name, info, err)
		}
	}
	d.ExpectedFilesystem = "missing-device"
	if _, err := s.restic(context.Background(), d, "", "init"); err == nil {
		t.Fatal("absent filesystem accepted")
	}
	if len(runner.calls) != 0 {
		t.Fatal("restic invoked before mount check")
	}
}
func TestRetentionFailureRetryDoesNotUploadAgain(t *testing.T) {
	s, _, r := newTestService(t)
	d := addDestination(t, s, "A")
	p := addPlan(t, s, d)
	r.failRetention = true
	job, run, err := s.StartRun(context.Background(), p.ID, RunRequest{}, "tester")
	if err != nil {
		t.Fatal(err)
	}
	s.ExecuteRun(context.Background(), job, run.ID)
	r.failRetention = false
	job, _, err = s.StartRetry(context.Background(), run.ID, RunRequest{}, "tester")
	if err != nil {
		t.Fatal(err)
	}
	s.ExecuteRun(context.Background(), job, run.ID)
	if r.count("backup", d.Repository) != 1 {
		t.Fatal("retention retry uploaded a duplicate snapshot")
	}
}
func TestExpiredRetryAndInterruptedReconciliation(t *testing.T) {
	s, _, _ := newTestService(t)
	d := addDestination(t, s, "A")
	p := addPlan(t, s, d)
	_, run, err := s.StartRun(context.Background(), p.ID, RunRequest{}, "tester")
	if err != nil {
		t.Fatal(err)
	}
	if err = s.Reconcile(context.Background()); err != nil {
		t.Fatal(err)
	}
	view, _ := s.Overview(context.Background())
	if view.Runs[0].State != "failed" || view.Runs[0].CaptureState != "failed" {
		t.Fatalf("reconciled run = %#v", view.Runs[0])
	}
	if _, _, err = s.StartRetry(context.Background(), run.ID, RunRequest{}, "tester"); !errors.Is(err, ErrExpired) {
		t.Fatalf("retry = %v", err)
	}
}
func TestScheduleCalendarAndMissedRuns(t *testing.T) {
	schedule := Schedule{Frequency: "daily", Time: "09:30", Timezone: "Europe/Warsaw"}
	for _, value := range []string{"2026-03-29T08:00:00Z", "2026-10-25T10:00:00Z"} {
		now, _ := time.Parse(time.RFC3339, value)
		due, ok := latestDue(schedule, now)
		if !ok || due.Hour() != 9 || due.Minute() != 30 || due.After(now) {
			t.Fatalf("due(%s)=%v,%v", value, due, ok)
		}
	}
	schedule.Frequency = "weekly"
	schedule.Weekday = 1
	now, _ := time.Parse(time.RFC3339, "2026-10-18T12:00:00Z")
	due, _ := latestDue(schedule, now)
	if due.Weekday() != time.Monday || due.Day() != 12 {
		t.Fatal(due)
	}
}
func TestRepositoryValidationRejectsCredentialURLsAndSSHInjection(t *testing.T) {
	tests := []DestinationInput{{Name: "x", Kind: "s3", Repository: "s3:https://key:secret@example.com/bucket"}, {Name: "x", Kind: "sftp", Repository: "sftp://-oProxyCommand=x@example.com/repo"}, {Name: "x", Kind: "sftp", Repository: "sftp://alice:secret@example.com/repo"}, {Name: "x", Kind: "rclone", Repository: "rclone:--config /secret:repo"}, {Name: "x", Kind: "local", Repository: "/tmp/repo"}}
	for _, input := range tests {
		if err := validateDestination(input); err == nil {
			t.Fatalf("accepted unsafe destination: %+v", input)
		}
	}
	for _, repository := range []string{"sftp://alice@example.com/repo", "sftp://alice@example.com:2222/repo"} {
		if err := validateDestination(DestinationInput{Name: "x", Kind: "sftp", Repository: repository}); err != nil {
			t.Fatal(fmt.Sprintf("%s: %v", repository, err))
		}
	}
}

func TestNativeRunnerRedactsOutputAndCancelsProcessGroup(t *testing.T) {
	_, err := (nativeRunner{}).Run(context.Background(), "sh", []string{"-c", "echo TOP_SECRET_PASSWORD >&2; exit 3"}, cleanProcessEnvironment(), "")
	if err == nil || strings.Contains(err.Error(), "TOP_SECRET_PASSWORD") {
		t.Fatalf("unsafe process error: %v", err)
	}
	ctx, cancel := context.WithTimeout(context.Background(), 100*time.Millisecond)
	defer cancel()
	started := time.Now()
	_, err = (nativeRunner{}).Run(ctx, "sh", []string{"-c", "sleep 60 & wait"}, cleanProcessEnvironment(), "")
	if !errors.Is(err, context.DeadlineExceeded) || time.Since(started) > 2*time.Second {
		t.Fatalf("process group cancellation took %v: %v", time.Since(started), err)
	}
}

func TestMetadataRetentionPreservesRetryableRunsAndRepositorySnapshots(t *testing.T) {
	s, _, runner := newTestService(t)
	now := time.Now().UTC()
	s.now = func() time.Time { return now }
	for index := 0; index < 205; index++ {
		id := fmt.Sprintf("run_%03d", index)
		s.state.Runs[id] = persistedRun{Run: Run{ID: id, State: "failed", CreatedAt: now.Add(-time.Duration(index) * time.Hour), ExpiresAt: now.Add(-time.Hour)}}
	}
	s.state.Runs["run_retryable"] = persistedRun{Run: Run{ID: "run_retryable", State: "failed", ExpiresAt: now.Add(time.Hour)}}
	s.state.Runs["run_running"] = persistedRun{Run: Run{ID: "run_running", State: "running", ExpiresAt: now.Add(-time.Hour)}}
	for index := 0; index < 205; index++ {
		path := fmt.Sprintf("/restored/%d", index)
		s.state.Restores[path] = RestoreResult{Target: path}
		s.state.RestoreOrder = append(s.state.RestoreOrder, path)
	}
	s.pruneMetadataLocked()
	if len(s.state.Runs) != 202 || len(s.state.Restores) != 200 {
		t.Fatalf("retention runs=%d restores=%d", len(s.state.Runs), len(s.state.Restores))
	}
	if _, ok := s.state.Runs["run_retryable"]; !ok {
		t.Fatal("retryable recovery set discarded")
	}
	if _, ok := s.state.Runs["run_running"]; !ok {
		t.Fatal("active run discarded")
	}
	if len(runner.calls) > 0 {
		t.Fatal("metadata pruning touched repository snapshots")
	}
}
func TestLocalRepositorySymlinkCannotBypassPrivateStateGuard(t *testing.T) {
	s, _, _ := newTestService(t)
	alias := filepath.Join(t.TempDir(), "private-alias")
	if err := os.Symlink(s.directory, alias); err != nil {
		t.Fatal(err)
	}
	device, err := FilesystemIdentity(alias)
	if err != nil {
		t.Fatal(err)
	}
	_, err = s.SaveDestination(context.Background(), "", DestinationInput{Name: "unsafe", Kind: "local", Repository: filepath.Join(alias, "repo"), ExpectedFilesystem: device, Secrets: &Secrets{Password: "secret"}})
	if !errors.Is(err, ErrInvalid) {
		t.Fatalf("private state alias accepted: %v", err)
	}
}

func TestCommittedSnapshotRecoveredByRunTagWithoutDuplicateUpload(t *testing.T) {
	s, _, runner := newTestService(t)
	d := addDestination(t, s, "A")
	p := addPlan(t, s, d)
	job, run, err := s.StartRun(context.Background(), p.ID, RunRequest{}, "tester")
	if err != nil {
		t.Fatal(err)
	}
	runner.priorSnapshot = "aabbccddeeff0011"
	s.ExecuteRun(context.Background(), job, run.ID)
	view, _ := s.Overview(context.Background())
	if view.Runs[0].State != "succeeded" || runner.count("backup", d.Repository) != 0 {
		t.Fatalf("committed snapshot uploaded again: %#v", view.Runs[0])
	}
	found := false
	for _, args := range runner.calls {
		if strings.Contains(strings.Join(args, " "), "stacklab,instance:"+s.state.InstanceID+",plan:"+p.ID+",run:"+run.ID) {
			found = true
		}
	}
	if !found {
		t.Fatal("snapshot reconciliation was not scoped to immutable run identity")
	}
}
