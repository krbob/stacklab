package gitworkspace

import (
	"context"
	"os"
	"os/exec"
	"path/filepath"
	"strings"
	"testing"

	"stacklab/internal/config"
)

func TestServiceStatusUnavailableWhenWorkspaceIsNotGitRepo(t *testing.T) {
	t.Parallel()

	service, _ := newTestService(t)

	status, err := service.Status(context.Background())
	if err != nil {
		t.Fatalf("Status() error = %v", err)
	}
	if status.Available {
		t.Fatalf("Status().Available = true, want false")
	}
	if status.Reason != "not_a_git_repository" {
		t.Fatalf("Status().Reason = %q, want %q", status.Reason, "not_a_git_repository")
	}
}

func TestServiceStatusAndDiffForManagedWorkspace(t *testing.T) {
	t.Parallel()

	service, root := newTestService(t)
	runGit(t, root, "init", "-b", "main")
	runGit(t, root, "config", "user.name", "Stacklab Test")
	runGit(t, root, "config", "user.email", "stacklab@example.com")

	mustWriteFile(t, filepath.Join(root, "stacks", "demo", "compose.yaml"), "services:\n  app:\n    image: nginx:alpine\n")
	mustWriteFile(t, filepath.Join(root, "config", "demo", "app.conf"), "server_name old.local;\n")
	mustWriteFile(t, filepath.Join(root, "config", "shared_config", "global.yml"), "global: true\n")
	mustWriteFile(t, filepath.Join(root, "notes.txt"), "outside managed roots\n")
	runGit(t, root, "add", ".")
	runGit(t, root, "commit", "-m", "initial")

	mustWriteFile(t, filepath.Join(root, "config", "demo", "app.conf"), "server_name demo.local;\n")
	mustWriteFile(t, filepath.Join(root, "config", "demo", "new.env"), "FEATURE_FLAG=true\n")
	mustRename(t, filepath.Join(root, "config", "shared_config", "global.yml"), filepath.Join(root, "config", "shared_config", "global-renamed.yml"))
	runGit(t, root, "add", "-A", "config/shared_config")
	mustWriteFileBytes(t, filepath.Join(root, "config", "demo", "blob.bin"), []byte{0x00, 0x01, 0x02})
	mustWriteFile(t, filepath.Join(root, "notes.txt"), "still outside managed roots\n")

	status, err := service.Status(context.Background())
	if err != nil {
		t.Fatalf("Status() error = %v", err)
	}
	if !status.Available || status.Branch != "main" || status.Clean {
		t.Fatalf("unexpected status payload: %#v", status)
	}
	if len(status.Items) != 4 {
		t.Fatalf("Status().Items = %d, want 4; items=%#v", len(status.Items), status.Items)
	}
	if status.Items[0].Path != "config/demo/app.conf" || status.Items[0].Status != FileStatusModified {
		t.Fatalf("unexpected first status item: %#v", status.Items[0])
	}
	if status.Items[1].Path != "config/demo/blob.bin" || status.Items[1].Status != FileStatusUntracked {
		t.Fatalf("unexpected second status item: %#v", status.Items[1])
	}
	if status.Items[2].Path != "config/demo/new.env" || status.Items[2].Status != FileStatusUntracked {
		t.Fatalf("unexpected third status item: %#v", status.Items[2])
	}
	if status.Items[3].Path != "config/shared_config/global-renamed.yml" || status.Items[3].Status != FileStatusRenamed {
		t.Fatalf("unexpected fourth status item: %#v", status.Items[3])
	}
	if status.Items[3].OldPath == nil || *status.Items[3].OldPath != "config/shared_config/global.yml" {
		t.Fatalf("expected rename old path, got %#v", status.Items[3].OldPath)
	}

	modifiedDiff, err := service.Diff(context.Background(), "config/demo/app.conf")
	if err != nil {
		t.Fatalf("Diff(modified) error = %v", err)
	}
	if modifiedDiff.IsBinary || modifiedDiff.Diff == nil || !strings.Contains(*modifiedDiff.Diff, "+server_name demo.local;") {
		t.Fatalf("unexpected modified diff payload: %#v", modifiedDiff)
	}

	untrackedDiff, err := service.Diff(context.Background(), "config/demo/new.env")
	if err != nil {
		t.Fatalf("Diff(untracked) error = %v", err)
	}
	if untrackedDiff.IsBinary || untrackedDiff.Diff == nil || !strings.Contains(*untrackedDiff.Diff, "+FEATURE_FLAG=true") {
		t.Fatalf("unexpected untracked diff payload: %#v", untrackedDiff)
	}
	if strings.Contains(*untrackedDiff.Diff, root) {
		t.Fatalf("untracked diff leaked absolute path: %q", *untrackedDiff.Diff)
	}

	binaryDiff, err := service.Diff(context.Background(), "config/demo/blob.bin")
	if err != nil {
		t.Fatalf("Diff(binary) error = %v", err)
	}
	if !binaryDiff.IsBinary || binaryDiff.Diff != nil {
		t.Fatalf("unexpected binary diff payload: %#v", binaryDiff)
	}
}

func TestServiceDiffRejectsInvalidPaths(t *testing.T) {
	t.Parallel()

	service, root := newTestService(t)
	runGit(t, root, "init", "-b", "main")
	runGit(t, root, "config", "user.name", "Stacklab Test")
	runGit(t, root, "config", "user.email", "stacklab@example.com")
	mustWriteFile(t, filepath.Join(root, "config", "demo", "app.conf"), "server_name old.local;\n")
	runGit(t, root, "add", ".")
	runGit(t, root, "commit", "-m", "initial")

	if _, err := service.Diff(context.Background(), "../etc/passwd"); err != ErrPathOutsideWorkspace {
		t.Fatalf("Diff(path traversal) error = %v, want %v", err, ErrPathOutsideWorkspace)
	}
	if _, err := service.Diff(context.Background(), "README.md"); err != ErrInvalidManagedPath {
		t.Fatalf("Diff(outside managed roots) error = %v, want %v", err, ErrInvalidManagedPath)
	}
	if _, err := service.Diff(context.Background(), "config/demo/missing.conf"); err != ErrNotFound {
		t.Fatalf("Diff(missing changed path) error = %v, want %v", err, ErrNotFound)
	}
}

func newTestService(t *testing.T) (*Service, string) {
	t.Helper()

	root := t.TempDir()
	cfg := config.Config{RootDir: root}
	service := NewService(cfg)
	return service, root
}

func runGit(t *testing.T, dir string, args ...string) {
	t.Helper()

	cmd := exec.CommandContext(context.Background(), "git", append([]string{"-C", dir}, args...)...)
	cmd.Env = append(cmd.Environ(), "GIT_PAGER=cat", "TERM=dumb")
	output, err := cmd.CombinedOutput()
	if err != nil {
		t.Fatalf("git %s failed: %v\n%s", strings.Join(args, " "), err, string(output))
	}
}

func mustWriteFile(t *testing.T, path, content string) {
	t.Helper()
	mustWriteFileBytes(t, path, []byte(content))
}

func mustWriteFileBytes(t *testing.T, path string, content []byte) {
	t.Helper()
	if err := os.MkdirAll(filepath.Dir(path), 0o755); err != nil {
		t.Fatalf("MkdirAll(%s) error = %v", filepath.Dir(path), err)
	}
	if err := os.WriteFile(path, content, 0o644); err != nil {
		t.Fatalf("WriteFile(%s) error = %v", path, err)
	}
}

func mustRename(t *testing.T, oldPath, newPath string) {
	t.Helper()
	if err := os.MkdirAll(filepath.Dir(newPath), 0o755); err != nil {
		t.Fatalf("MkdirAll(%s) error = %v", filepath.Dir(newPath), err)
	}
	if err := os.Rename(oldPath, newPath); err != nil {
		t.Fatalf("Rename(%s -> %s) error = %v", oldPath, newPath, err)
	}
}
