package backup

import (
	"context"
	"errors"
	"os"
	"os/exec"
	"path/filepath"
	"strings"
	"testing"

	"stacklab/internal/jobs"
)

const shortUpdateSnapshot = "aabbccddeeff0011"

type updateCaptureFixture struct {
	*fakeCapture
	omitAlias bool
}

func (c *updateCaptureFixture) Prepare(ctx context.Context, p Plan, id, dir string) (Manifest, error) {
	m, err := c.fakeCapture.Prepare(ctx, p, id, dir)
	if err != nil {
		return m, err
	}
	item := ManifestSource{Source: p.Sources[0], Archive: true, SavedPath: "payload"}
	if !c.omitAlias {
		item.Aliases = p.Sources[1:]
	}
	m.Sources = []ManifestSource{item}
	return m, nil
}

type updateRepositoryFixture struct{ *recordingRunner }

func (r updateRepositoryFixture) Run(ctx context.Context, name string, args, env []string, dir string) ([]byte, error) {
	if commandIn(args, "snapshots") && commandIn(args, shortUpdateSnapshot) {
		return []byte(`[{"id":"` + shortUpdateSnapshot + strings.Repeat("0", 48) + `"}]`), nil
	}
	return r.recordingRunner.Run(ctx, name, args, env, dir)
}

func updateBackupPlan(t *testing.T, s *Service) (Plan, []string) {
	t.Helper()
	d := addDestination(t, s, "target")
	p := Plan{Name: "before update", Enabled: true, Consistency: "stop", Sources: []Source{
		{ID: "data-a", Kind: "service_mount", StackID: "app", Service: "frontend", Mount: "/data", Path: "app_data"},
		{ID: "data-b", Kind: "service_mount", StackID: "app", Service: "worker", Mount: "/shared", Path: "app_data"},
	}, Destinations: []Assignment{{Enabled: true, DestinationID: d.ID}}}
	p, err := s.SavePlan(context.Background(), "", p)
	if err != nil {
		t.Fatal(err)
	}
	return p, []string{"data-b", "data-a"}
}

func TestVerifiedBackupGatesUpdateAndRetainsStableReference(t *testing.T) {
	s, base, runner := newTestService(t)
	s.capture = &updateCaptureFixture{fakeCapture: base}
	s.runner = updateRepositoryFixture{runner}
	ctx := context.Background()
	p, ids := updateBackupPlan(t, s)
	fingerprint, err := s.ValidateUpdateBackup(ctx, "app", p.ID, ids)
	if err != nil {
		t.Fatal(err)
	}
	parent, err := s.jobs.StartWithResources(ctx, "", "update_stacks", "operator", jobs.StackResource("app"))
	if err != nil {
		t.Fatal(err)
	}
	result, err := s.CreateUpdateBackup(ctx, parent, "app", p.ID, ids, fingerprint)
	if err != nil {
		t.Fatal(err)
	}
	if result.RunID == "" || result.DestinationID != p.Destinations[0].DestinationID || result.SnapshotID != shortUpdateSnapshot+strings.Repeat("0", 48) {
		t.Fatalf("unexpected reference: %#v", result)
	}
	actual := s.state.Runs[result.RunID]
	if actual.JobID == parent.ID || !verifiedCopy(actual.Copies[0]) || base.calls != 1 || runner.count("backup", "") != 1 {
		t.Fatal("update did not use the native verified run")
	}
	if _, err = s.jobs.Start(ctx, "app", "restart", "another"); !errors.Is(err, jobs.ErrResourceConflict) {
		t.Fatalf("parent lost stack lease: %v", err)
	}
	// A capture must account for each approved source even when all data
	// physically resides in one archive. It must fail before upload if not.
	s.capture = &updateCaptureFixture{fakeCapture: base, omitAlias: true}
	result, err = s.CreateUpdateBackup(ctx, parent, "app", p.ID, ids, fingerprint)
	if err == nil || result.SnapshotID != "" || result.RunID == "" || runner.count("backup", "") != 1 {
		t.Fatalf("omitted alias permitted: %#v %v", result, err)
	}
	if s.state.Runs[result.RunID].State != "failed" {
		t.Fatal("failed capture lost run history")
	}
}

func TestUpdateBackupApprovesOnlyExactReviewedScope(t *testing.T) {
	s, base, runner := newTestService(t)
	ctx := context.Background()
	p, ids := updateBackupPlan(t, s)
	fingerprint, err := s.ValidateUpdateBackup(ctx, "app", p.ID, ids)
	if err != nil || len(fingerprint) != 64 {
		t.Fatalf("fingerprint %q: %v", fingerprint, err)
	}
	if _, err = s.ValidateUpdateBackup(ctx, "other", p.ID, ids); !errors.Is(err, ErrInvalid) {
		t.Fatalf("other stack: %v", err)
	}
	for _, invalid := range [][]string{nil, {ids[0]}, {ids[0], ids[0]}, {ids[0], "other"}} {
		if _, err = s.ValidateUpdateBackup(ctx, "app", p.ID, invalid); !errors.Is(err, ErrInvalid) {
			t.Fatalf("accepted %v: %v", invalid, err)
		}
	}
	renamed := clone(p)
	renamed.Name = "new name"
	renamed.Destinations[0].Schedule = Schedule{Frequency: "interval", IntervalHours: 6}
	renamed.UpdateBackupFingerprint = "client spoof"
	if _, err = s.SavePlan(ctx, p.ID, renamed); err != nil {
		t.Fatal(err)
	}
	got, err := s.ValidateUpdateBackup(ctx, "app", p.ID, ids)
	if err != nil || got != fingerprint || s.state.Plans[p.ID].UpdateBackupFingerprint != "" {
		t.Fatal("display edits changed review or accepted client fingerprint")
	}
	overview, err := s.Overview(ctx)
	if err != nil || overview.Plans[0].UpdateBackupFingerprint != got {
		t.Fatalf("plan response missing review: %v", err)
	}
	parent, err := s.jobs.Start(ctx, "app", "update_stacks", "operator")
	if err != nil {
		t.Fatal(err)
	}
	for _, change := range []func(*Plan){
		func(q *Plan) { q.Enabled = false },
		func(q *Plan) { q.Sources[0].Kind = "stacklab" },
		func(q *Plan) { q.Sources[0].StackID = "unrelated" },
		func(q *Plan) { q.Sources[0].Path = "" },
		func(q *Plan) {
			q.Sources[0].Kind = "managed_config"
			q.Sources[0].Path = filepath.Join(s.cfg.RootDir, "config", "unrelated")
		},
		func(q *Plan) { q.Destinations[0].Enabled = false },
		func(q *Plan) { q.Sources[0].Excludes = []string{"*.sqlite"} },
		func(q *Plan) { q.Sources[0].Path = "other_data" },
		func(q *Plan) { q.Consistency = "files" },
		func(q *Plan) { q.Destinations[0].DestinationID = addDestination(t, s, "other").ID },
	} {
		q := clone(p)
		change(&q)
		if _, err = s.SavePlan(ctx, p.ID, q); err != nil {
			t.Fatal(err)
		}
		if _, err = s.CreateUpdateBackup(ctx, parent, "app", p.ID, ids, fingerprint); !errors.Is(err, ErrInvalid) {
			t.Fatalf("changed scope permitted: %v", err)
		}
	}
	if base.calls != 0 || runner.count("backup", "") != 0 || len(s.state.Runs) != 0 {
		t.Fatal("rejected policy left a capture or run")
	}
}

func TestUpdateBackupManagedConfigRejectsSymlinkToDifferentStack(t *testing.T) {
	s, _, _ := newTestService(t)
	dir := filepath.Join(s.cfg.RootDir, "config", "other")
	if err := os.MkdirAll(dir, 0700); err != nil {
		t.Fatal(err)
	}
	path := filepath.Join(s.cfg.RootDir, "config", "app")
	if err := os.Symlink(dir, path); err != nil {
		t.Fatal(err)
	}
	c := &NativeCapture{cfg: s.cfg}
	ctx := context.WithValue(context.Background(), updateBackupStackKey{}, "app")
	_, err := c.resolveSource(ctx, Source{Kind: "managed_config", StackID: "app", Path: path}, nil)
	if err == nil {
		t.Fatal("capture crossed stack config boundary")
	}
}

// A returned maintenance reference can be opened directly using the normal
// isolated recovery path, including after the staged capture has been removed.
func TestNativePreUpdateSnapshotSupportsIsolatedRecovery(t *testing.T) {
	requireGNUTar(t)
	if _, err := exec.LookPath("restic"); err != nil {
		t.Skip("restic unavailable")
	}
	ctx := context.Background()
	s, _, _ := newTestService(t)
	s.runner = nativeRunner{}
	s.capture = &NativeCapture{cfg: s.cfg, stacks: captureReaderFixture{}, command: captureCommand}
	dir := filepath.Join(s.cfg.RootDir, "config", "app")
	if err := os.MkdirAll(dir, 0700); err != nil {
		t.Fatal(err)
	}
	source := filepath.Join(dir, "app.conf")
	if err := os.WriteFile(source, []byte("before update"), 0600); err != nil {
		t.Fatal(err)
	}
	d := addDestination(t, s, "native")
	job, err := s.StartRepositoryJob(ctx, d.ID, "backup_repository_init", "tester")
	if err != nil {
		t.Fatal(err)
	}
	if _, err = s.ExecuteRepositoryJob(ctx, job, d.ID, "backup_repository_init", CheckRequest{}, RestoreRequest{}); err != nil {
		t.Fatal(err)
	}
	plan, err := s.SavePlan(ctx, "", Plan{Name: "app", Enabled: true, Consistency: "files", Sources: []Source{{ID: "app-config", Kind: "managed_config", StackID: "app", Path: dir}}, Destinations: []Assignment{{Enabled: true, DestinationID: d.ID}}})
	if err != nil {
		t.Fatal(err)
	}
	ids := []string{"app-config"}
	fingerprint, err := s.ValidateUpdateBackup(ctx, "app", plan.ID, ids)
	if err != nil {
		t.Fatal(err)
	}
	parent, err := s.jobs.Start(ctx, "app", "update_stacks", "tester")
	if err != nil {
		t.Fatal(err)
	}
	ref, err := s.CreateUpdateBackup(ctx, parent, "app", plan.ID, ids, fingerprint)
	if err != nil {
		t.Fatal(err)
	}
	if err = os.WriteFile(source, []byte("after update"), 0600); err != nil {
		t.Fatal(err)
	}
	recovered, err := s.StartRepositoryJob(ctx, ref.DestinationID, "backup_restore", "tester")
	if err != nil {
		t.Fatal(err)
	}
	target := filepath.Join(t.TempDir(), "recovery")
	_, err = s.ExecuteRepositoryJob(ctx, recovered, ref.DestinationID, "backup_restore", CheckRequest{}, RestoreRequest{SnapshotID: ref.SnapshotID, Test: true, Target: target})
	if err != nil {
		t.Fatal(err)
	}
	data, err := os.ReadFile(filepath.Join(target, "files", ids[0], "app.conf"))
	if err != nil || string(data) != "before update" {
		t.Fatalf("isolated data = %q, error %v", data, err)
	}
	live, err := os.ReadFile(source)
	if err != nil || string(live) != "after update" {
		t.Fatalf("recovery touched live file: %v", err)
	}
}

func TestUpdateBackupBlocksDeploymentWhenAnApprovedDestinationFails(t *testing.T) {
	ctx := context.Background()
	s, base, runner := newTestService(t)
	s.capture = &updateCaptureFixture{fakeCapture: base}
	s.runner = updateRepositoryFixture{runner}
	plan, ids := updateBackupPlan(t, s)
	second := addDestination(t, s, "second")
	plan.Destinations = append(plan.Destinations, Assignment{DestinationID: second.ID, Enabled: true})
	plan, err := s.SavePlan(ctx, plan.ID, plan)
	if err != nil {
		t.Fatal(err)
	}
	parent, err := s.jobs.Start(ctx, "app", "update_stacks", "operator")
	if err != nil {
		t.Fatal(err)
	}
	fingerprint, err := s.ValidateUpdateBackup(ctx, "app", plan.ID, ids)
	if err != nil {
		t.Fatal(err)
	}
	s.policy.Paused = true
	if _, err = s.CreateUpdateBackup(ctx, parent, "app", plan.ID, ids, fingerprint); !errors.Is(err, ErrBusy) || len(s.state.Runs) != 0 {
		t.Fatalf("paused policy started a capture: %v", err)
	}
	s.policy.Paused = false
	runner.failRepo = second.Repository
	result, err := s.CreateUpdateBackup(ctx, parent, "app", plan.ID, ids, fingerprint)
	if err == nil || result.RunID == "" || result.SnapshotID != "" {
		t.Fatalf("partial backup released gate: %+v %v", result, err)
	}
	stored := s.state.Runs[result.RunID]
	if stored.State != "failed" || !verifiedCopy(stored.Copies[0]) || stored.Copies[1].State != "failed" {
		t.Fatal("per-destination evidence was not retained")
	}
}
