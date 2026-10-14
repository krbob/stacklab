package backup

import (
	"archive/tar"
	"context"
	"fmt"
	"os"
	"os/exec"
	"path/filepath"
	"testing"
	"time"
)

// TestNativeRepositoryRoundTrip exercises the actual encrypted restic format,
// staging-relative layout, integrity check and independent fresh-service restore.
func TestNativeRepositoryRoundTrip(t *testing.T) {
	if _, err := exec.LookPath("restic"); err != nil {
		t.Skip("restic is not installed")
	}
	s, capture, _ := newTestService(t)
	s.runner = nativeRunner{}
	s.capture = &nativeArchiveCapture{fakeCapture: capture}
	d := addDestination(t, s, "Native")
	p := addPlan(t, s, d)
	job, err := s.StartRepositoryJob(context.Background(), d.ID, "backup_repository_init", "tester")
	if err != nil {
		t.Fatal(err)
	}
	if _, err = s.ExecuteRepositoryJob(context.Background(), job, d.ID, "backup_repository_init", CheckRequest{}, RestoreRequest{}); err != nil {
		t.Fatal(err)
	}
	job, run, err := s.StartRun(context.Background(), p.ID, RunRequest{}, "tester")
	if err != nil {
		t.Fatal(err)
	}
	s.ExecuteRun(context.Background(), job, run.ID)
	view, err := s.Overview(context.Background())
	if err != nil {
		t.Fatal(err)
	}
	run = view.Runs[0]
	if run.State != "succeeded" {
		t.Fatalf("native run: %+v", run)
	}
	if capture.calls != 1 {
		t.Fatal("capture count")
	}
	snapshots, err := s.Snapshots(context.Background(), d.ID)
	if err != nil || len(snapshots) != 1 {
		t.Fatalf("snapshots: %#v %v", snapshots, err)
	}
	if delta := snapshots[0].Time.Sub(*run.CapturedAt); delta < -time.Second || delta > time.Second {
		t.Fatalf("snapshot time differs from actual capture: %s vs %s", snapshots[0].Time, *run.CapturedAt)
	}
	job, err = s.StartRepositoryJob(context.Background(), d.ID, "backup_check", "tester")
	if err != nil {
		t.Fatal(err)
	}
	if _, err = s.ExecuteRepositoryJob(context.Background(), job, d.ID, "backup_check", CheckRequest{ReadData: "all"}, RestoreRequest{}); err != nil {
		t.Fatal(err)
	}
	// Restore with a new service/database holding only independently supplied
	// credentials; the original plan database and local stage are not consulted.
	fresh, _, _ := newTestService(t)
	fresh.runner = nativeRunner{}
	imported, err := fresh.SaveDestination(context.Background(), "", DestinationInput{Name: "Recovered repository", Kind: "local", Repository: d.Repository, ExpectedFilesystem: d.ExpectedFilesystem, Secrets: &Secrets{Password: "test-secret-do-not-log"}})
	if err != nil {
		t.Fatal(err)
	}
	target := filepath.Join(t.TempDir(), "recovered")
	job, err = fresh.StartRepositoryJob(context.Background(), imported.ID, "backup_restore", "tester")
	if err != nil {
		t.Fatal(err)
	}
	result, err := fresh.ExecuteRepositoryJob(context.Background(), job, imported.ID, "backup_restore", CheckRequest{}, RestoreRequest{SnapshotID: snapshots[0].ShortID, Target: target, Test: true})
	if err != nil {
		t.Fatal(err)
	}
	if result.Manifest == nil || result.Manifest.PlanID != p.ID {
		t.Fatalf("recovered manifest=%#v", result.Manifest)
	}
	// Exact snapshot restore-test evidence survives a process restart and is
	// exposed on repository inventory without implying tests of other snapshots.
	reopened := NewService(fresh.cfg, fresh.jobs, fresh.capture)
	reopened.runner = nativeRunner{}
	testedSnapshots, proofErr := reopened.Snapshots(context.Background(), imported.ID)
	if proofErr != nil || len(testedSnapshots) != 1 || testedSnapshots[0].RestoreTestedAt == nil || result.SnapshotID != snapshots[0].ID {
		t.Fatalf("restore test evidence was not persisted: %#v %v", testedSnapshots, proofErr)
	}
	data, err := os.ReadFile(filepath.Join(target, "payload"))
	if err != nil || string(data) != "recover me" {
		t.Fatalf("recovered data=%q %v", data, err)
	}
	recoveredFile, readErr := os.ReadFile(filepath.Join(target, "files", "source-a", "data.txt"))
	if readErr != nil || string(recoveredFile) != "archived content" {
		t.Fatalf("extracted data=%q err=%v", recoveredFile, readErr)
	}
	// Ordinary source-selection restores also provide extracted files, while
	// retaining the archive and manifest needed by reviewed in-place recovery.
	selectedTarget := filepath.Join(t.TempDir(), "selection")
	job, err = fresh.StartRepositoryJob(context.Background(), imported.ID, "backup_restore", "tester")
	if err != nil {
		t.Fatal(err)
	}
	_, err = fresh.ExecuteRepositoryJob(context.Background(), job, imported.ID, "backup_restore", CheckRequest{}, RestoreRequest{SnapshotID: snapshots[0].ID, Target: selectedTarget, Includes: []string{"/sources/source-a.tar"}})
	if err != nil {
		t.Fatal(err)
	}
	if _, err = os.Stat(filepath.Join(selectedTarget, "files", "source-a", "data.txt")); err != nil {
		t.Fatal(err)
	}
	if _, err = os.Stat(filepath.Join(selectedTarget, "sources", "source-a.tar")); err != nil {
		t.Fatal(err)
	}
	// The second installation has distinct retention ownership, so its policy
	// cannot remove adopted snapshots from the original installation.
	if fresh.state.InstanceID == s.state.InstanceID {
		t.Fatal("fresh host reused installation ownership")
	}
}

func TestNativeRcloneTransport(t *testing.T) {
	if _, err := exec.LookPath("restic"); err != nil {
		t.Skip("restic unavailable")
	}
	if _, err := exec.LookPath("rclone"); err != nil {
		t.Skip("rclone unavailable")
	}
	s, _, _ := newTestService(t)
	s.runner = nativeRunner{}
	repo := filepath.Join(t.TempDir(), "repository")
	d, err := s.SaveDestination(context.Background(), "", DestinationInput{Name: "Rclone fixture", Kind: "rclone", Repository: "rclone:fixture:" + repo, Secrets: &Secrets{Password: "rclone-test-password", RcloneConfig: "[fixture]\ntype = local\n"}})
	if err != nil {
		t.Fatal(err)
	}
	job, err := s.StartRepositoryJob(context.Background(), d.ID, "backup_repository_init", "tester")
	if err != nil {
		t.Fatal(err)
	}
	if _, err = s.ExecuteRepositoryJob(context.Background(), job, d.ID, "backup_repository_init", CheckRequest{}, RestoreRequest{}); err != nil {
		t.Fatal(err)
	}
	p := addPlan(t, s, d)
	job, run, err := s.StartRun(context.Background(), p.ID, RunRequest{}, "tester")
	if err != nil {
		t.Fatal(err)
	}
	s.ExecuteRun(context.Background(), job, run.ID)
	view, _ := s.Overview(context.Background())
	if view.Runs[0].State != "succeeded" {
		t.Fatalf("rclone backup: %#v", view.Runs[0])
	}
	snapshots, err := s.Snapshots(context.Background(), d.ID)
	if err != nil || len(snapshots) != 1 {
		t.Fatalf("snapshots=%v err=%v", snapshots, err)
	}
	job, err = s.StartRepositoryJob(context.Background(), d.ID, "backup_restore", "tester")
	if err != nil {
		t.Fatal(err)
	}
	result, err := s.ExecuteRepositoryJob(context.Background(), job, d.ID, "backup_restore", CheckRequest{}, RestoreRequest{SnapshotID: snapshots[0].ID, Test: true})
	if err != nil {
		t.Fatal(err)
	}
	if result.Manifest == nil {
		t.Fatal("rclone restore did not recover manifest")
	}
}

// TestNativeSFTPTransport is opt-in because it requires an independently
// configured SSH server and pinned host key. It creates a new repository only
// at STACKLAB_TEST_SFTP_REPOSITORY and never removes remote files.
func TestNativeSFTPTransport(t *testing.T) {
	repository := os.Getenv("STACKLAB_TEST_SFTP_REPOSITORY")
	if repository == "" {
		t.Skip("set STACKLAB_TEST_SFTP_REPOSITORY, STACKLAB_TEST_SFTP_KEY_FILE, STACKLAB_TEST_SFTP_HOSTS_FILE")
	}
	key, err := os.ReadFile(os.Getenv("STACKLAB_TEST_SFTP_KEY_FILE"))
	if err != nil {
		t.Fatal(err)
	}
	hosts, err := os.ReadFile(os.Getenv("STACKLAB_TEST_SFTP_HOSTS_FILE"))
	if err != nil {
		t.Fatal(err)
	}
	exerciseNativeTransport(t, DestinationInput{Name: "SFTP fixture", Kind: "sftp", Repository: repository, Secrets: &Secrets{Password: "sftp-integration-password", SSHPrivateKey: string(key), SSHKnownHosts: string(hosts)}})
}

// TestNativeS3Transport is opt-in and expects a dedicated, empty HTTPS bucket
// prefix. A private fixture CA may be supplied through SSL_CERT_FILE.
func TestNativeS3Transport(t *testing.T) {
	repository := os.Getenv("STACKLAB_TEST_S3_REPOSITORY")
	if repository == "" {
		t.Skip("set STACKLAB_TEST_S3_REPOSITORY, STACKLAB_TEST_S3_ACCESS_KEY, STACKLAB_TEST_S3_SECRET_KEY")
	}
	exerciseNativeTransport(t, DestinationInput{Name: "S3 fixture", Kind: "s3", Repository: repository, Region: os.Getenv("STACKLAB_TEST_S3_REGION"), Secrets: &Secrets{Password: "s3-integration-password", AccessKeyID: os.Getenv("STACKLAB_TEST_S3_ACCESS_KEY"), SecretAccessKey: os.Getenv("STACKLAB_TEST_S3_SECRET_KEY")}})
}

func exerciseNativeTransport(t *testing.T, input DestinationInput) {
	t.Helper()
	if _, err := exec.LookPath("restic"); err != nil {
		t.Skip("restic unavailable")
	}
	s, _, _ := newTestService(t)
	s.runner = nativeRunner{}
	d, err := s.SaveDestination(context.Background(), "", input)
	if err != nil {
		t.Fatal(err)
	}
	job, err := s.StartRepositoryJob(context.Background(), d.ID, "backup_repository_init", "tester")
	if err != nil {
		t.Fatal(err)
	}
	if _, err = s.ExecuteRepositoryJob(context.Background(), job, d.ID, "backup_repository_init", CheckRequest{}, RestoreRequest{}); err != nil {
		t.Fatal(err)
	}
	p := addPlan(t, s, d)
	job, run, err := s.StartRun(context.Background(), p.ID, RunRequest{}, "tester")
	if err != nil {
		t.Fatal(err)
	}
	s.ExecuteRun(context.Background(), job, run.ID)
	view, _ := s.Overview(context.Background())
	if view.Runs[0].State != "succeeded" {
		t.Fatalf("transport backup failed: %s", view.Runs[0].Error)
	}
	snapshots, err := s.Snapshots(context.Background(), d.ID)
	if err != nil || len(snapshots) != 1 {
		t.Fatalf("snapshots=%v err=%v", snapshots, err)
	}
	job, err = s.StartRepositoryJob(context.Background(), d.ID, "backup_check", "tester")
	if err != nil {
		t.Fatal(err)
	}
	if _, err = s.ExecuteRepositoryJob(context.Background(), job, d.ID, "backup_check", CheckRequest{ReadData: "all"}, RestoreRequest{}); err != nil {
		t.Fatal(err)
	}
	job, err = s.StartRepositoryJob(context.Background(), d.ID, "backup_restore", "tester")
	if err != nil {
		t.Fatal(err)
	}
	result, err := s.ExecuteRepositoryJob(context.Background(), job, d.ID, "backup_restore", CheckRequest{}, RestoreRequest{SnapshotID: snapshots[0].ID, Test: true})
	if err != nil {
		t.Fatal(err)
	}
	if result.Manifest == nil {
		t.Fatal("transport restore did not recover manifest")
	}
	payload, err := os.ReadFile(filepath.Join(result.Target, "payload"))
	if err != nil || string(payload) != "recover me" {
		t.Fatalf("transport payload verification failed: %v", err)
	}
}

type nativeArchiveCapture struct{ *fakeCapture }

func (c *nativeArchiveCapture) Prepare(ctx context.Context, p Plan, id, dir string) (Manifest, error) {
	manifest, err := c.fakeCapture.Prepare(ctx, p, id, dir)
	if err != nil {
		return manifest, err
	}
	if err = os.MkdirAll(filepath.Join(dir, "sources"), 0700); err != nil {
		return manifest, err
	}
	archivePath := filepath.Join(dir, "sources", "source-a.tar")
	file, err := os.Create(archivePath)
	if err != nil {
		return manifest, err
	}
	archive := tar.NewWriter(file)
	content := "archived content"
	if err = archive.WriteHeader(&tar.Header{Name: "data.txt", Mode: 0640, Typeflag: tar.TypeReg, Size: int64(len(content))}); err != nil {
		return manifest, err
	}
	if _, err = fmt.Fprint(archive, content); err != nil {
		return manifest, err
	}
	if err = archive.Close(); err != nil {
		return manifest, err
	}
	if err = file.Close(); err != nil {
		return manifest, err
	}
	info, err := os.Stat(archivePath)
	if err != nil {
		return manifest, err
	}
	manifest.Sources = []ManifestSource{{Source: Source{ID: "source-a", Kind: "managed_config"}, SavedPath: "sources/source-a.tar", Archive: true, SizeBytes: info.Size()}}
	return manifest, nil
}
