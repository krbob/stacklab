package gitworkspace

import (
	"context"
	"errors"
	"os"
	"os/exec"
	"path/filepath"
	"strings"
	"testing"
	"time"

	"stacklab/internal/config"
)

func TestMain(m *testing.M) {
	if filepath.Base(os.Args[0]) == "fake-git" {
		fakeGitMain()
		return
	}
	os.Exit(m.Run())
}

func fakeGitMain() {
	args := os.Args[1:]
	if len(args) >= 2 && args[0] == "-C" {
		args = args[2:]
	}
	if len(args) == 2 && args[0] == "rev-parse" && args[1] == "--show-toplevel" {
		if os.Getenv("LC_ALL") == "C" && os.Getenv("LANG") == "C" {
			_, _ = os.Stderr.WriteString("fatal: not a git repository (or any of the parent directories): .git\n")
		} else {
			_, _ = os.Stderr.WriteString("fatal: to nie jest repozytorium git\n")
		}
		os.Exit(128)
	}
	_, _ = os.Stderr.WriteString("unexpected args: " + strings.Join(args, " ") + "\n")
	os.Exit(1)
}

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

func TestServiceStatusUnavailableWhenGitOutputWouldBeLocalized(t *testing.T) {
	t.Parallel()

	service, _ := newTestService(t)
	fakeGit := filepath.Join(t.TempDir(), "fake-git")
	testBinary, err := os.Executable()
	if err != nil {
		t.Fatalf("Executable() error = %v", err)
	}
	if err := os.Symlink(testBinary, fakeGit); err != nil {
		t.Fatalf("Symlink(fake git) error = %v", err)
	}
	service.gitBinary = fakeGit

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

func TestServiceDiffUsesEmptyTreeForUnbornHead(t *testing.T) {
	t.Parallel()

	service, root := newTestService(t)
	runGit(t, root, "init", "-b", "main")
	mustWriteFile(t, filepath.Join(root, "config", "demo", "app.conf"), "server_name demo.local;\n")
	runGit(t, root, "add", ".")

	diff, err := service.Diff(context.Background(), "config/demo/app.conf")
	if err != nil {
		t.Fatalf("Diff(unborn HEAD) error = %v", err)
	}
	if diff.IsBinary || diff.Diff == nil || !strings.Contains(*diff.Diff, "+server_name demo.local;") {
		t.Fatalf("unexpected unborn HEAD diff payload: %#v", diff)
	}
}

func TestServiceStatusDoesNotMutateWorkspaceRoot(t *testing.T) {
	t.Parallel()

	tempDir := t.TempDir()
	realRoot := filepath.Join(tempDir, "real")
	linkRoot := filepath.Join(tempDir, "link")
	if err := os.MkdirAll(realRoot, 0o755); err != nil {
		t.Fatalf("MkdirAll(realRoot) error = %v", err)
	}
	if err := os.Symlink(realRoot, linkRoot); err != nil {
		t.Fatalf("Symlink(linkRoot) error = %v", err)
	}
	runGit(t, realRoot, "init", "-b", "main")

	service := NewService(config.Config{RootDir: linkRoot})
	originalRoot := service.workspaceRoot

	status, err := service.Status(context.Background())
	if err != nil {
		t.Fatalf("Status() error = %v", err)
	}
	if !status.Available {
		t.Fatalf("Status().Available = false, want true: %#v", status)
	}
	if service.workspaceRoot != originalRoot {
		t.Fatalf("workspaceRoot mutated from %q to %q", originalRoot, service.workspaceRoot)
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

func TestServiceCommitAndPushSelectedPaths(t *testing.T) {
	t.Parallel()

	service, root := newTestService(t)
	runGit(t, root, "init", "-b", "main")
	runGit(t, root, "config", "user.name", "Stacklab Test")
	runGit(t, root, "config", "user.email", "stacklab@example.com")

	mustWriteFile(t, filepath.Join(root, "config", "demo", "app.conf"), "server_name old.local;\n")
	mustWriteFile(t, filepath.Join(root, "config", "demo", "other.env"), "FEATURE_FLAG=false\n")
	runGit(t, root, "add", ".")
	runGit(t, root, "commit", "-m", "initial")

	remoteDir := filepath.Join(t.TempDir(), "origin.git")
	runGit(t, root, "init", "--bare", remoteDir)
	runGit(t, root, "remote", "add", "origin", remoteDir)
	runGit(t, root, "push", "-u", "origin", "main")

	mustWriteFile(t, filepath.Join(root, "config", "demo", "app.conf"), "server_name new.local;\n")
	mustWriteFile(t, filepath.Join(root, "config", "demo", "other.env"), "FEATURE_FLAG=true\n")

	commitResponse, err := service.Commit(context.Background(), CommitRequest{
		Message: "Update app config",
		Paths:   []string{"config/demo/app.conf"},
	})
	if err != nil {
		t.Fatalf("Commit() error = %v", err)
	}
	if !commitResponse.Committed || commitResponse.Commit == "" || commitResponse.RemainingChanges != 1 {
		t.Fatalf("unexpected Commit() payload: %#v", commitResponse)
	}

	status, err := service.Status(context.Background())
	if err != nil {
		t.Fatalf("Status() after commit error = %v", err)
	}
	if !status.Available || len(status.Items) != 1 || status.Items[0].Path != "config/demo/other.env" {
		t.Fatalf("unexpected status after commit: %#v", status)
	}
	if status.AheadCount != 1 {
		t.Fatalf("Status().AheadCount after commit = %d, want 1", status.AheadCount)
	}

	pushResponse, err := service.Push(context.Background())
	if err != nil {
		t.Fatalf("Push() error = %v", err)
	}
	if !pushResponse.Pushed || pushResponse.Remote != "origin" || pushResponse.UpstreamName != "origin/main" {
		t.Fatalf("unexpected Push() payload: %#v", pushResponse)
	}
	if pushResponse.AheadCount != 0 {
		t.Fatalf("Push().AheadCount = %d, want 0", pushResponse.AheadCount)
	}
}

func TestServiceCommitRenamedFileIncludesOldPath(t *testing.T) {
	t.Parallel()

	service, root := newTestService(t)
	runGit(t, root, "init", "-b", "main")
	runGit(t, root, "config", "user.name", "Stacklab Test")
	runGit(t, root, "config", "user.email", "stacklab@example.com")

	mustWriteFile(t, filepath.Join(root, "config", "demo", "old.conf"), "server_name demo.local;\n")
	runGit(t, root, "add", ".")
	runGit(t, root, "commit", "-m", "initial")
	mustRename(t, filepath.Join(root, "config", "demo", "old.conf"), filepath.Join(root, "config", "demo", "new.conf"))
	runGit(t, root, "add", "-A", "config/demo")

	status, err := service.Status(context.Background())
	if err != nil {
		t.Fatalf("Status() error = %v", err)
	}
	if len(status.Items) != 1 || status.Items[0].Status != FileStatusRenamed || status.Items[0].OldPath == nil || *status.Items[0].OldPath != "config/demo/old.conf" {
		t.Fatalf("unexpected rename status: %#v", status.Items)
	}

	commitResponse, err := service.Commit(context.Background(), CommitRequest{
		Message: "Rename app config",
		Paths:   []string{"config/demo/new.conf"},
	})
	if err != nil {
		t.Fatalf("Commit(rename) error = %v", err)
	}
	if !commitResponse.Committed || commitResponse.RemainingChanges != 0 {
		t.Fatalf("unexpected Commit(rename) payload: %#v", commitResponse)
	}

	status, err = service.Status(context.Background())
	if err != nil {
		t.Fatalf("Status() after rename commit error = %v", err)
	}
	if !status.Clean || len(status.Items) != 0 {
		t.Fatalf("unexpected status after rename commit: %#v", status)
	}
}

func TestServiceCommitRemovesStaleIndexLock(t *testing.T) {
	t.Parallel()

	service, root := newTestService(t)
	runGit(t, root, "init", "-b", "main")
	runGit(t, root, "config", "user.name", "Stacklab Test")
	runGit(t, root, "config", "user.email", "stacklab@example.com")

	mustWriteFile(t, filepath.Join(root, "config", "demo", "app.conf"), "server_name old.local;\n")
	runGit(t, root, "add", ".")
	runGit(t, root, "commit", "-m", "initial")
	mustWriteFile(t, filepath.Join(root, "config", "demo", "app.conf"), "server_name new.local;\n")

	lockPath := filepath.Join(root, ".git", "index.lock")
	mustWriteFile(t, lockPath, "stale lock\n")
	staleTime := time.Now().Add(-(staleGitIndexLockAge + time.Minute))
	if err := os.Chtimes(lockPath, staleTime, staleTime); err != nil {
		t.Fatalf("Chtimes(index.lock) error = %v", err)
	}

	commitResponse, err := service.Commit(context.Background(), CommitRequest{
		Message: "Update app config",
		Paths:   []string{"config/demo/app.conf"},
	})
	if err != nil {
		t.Fatalf("Commit() error = %v", err)
	}
	if !commitResponse.Committed {
		t.Fatalf("Commit().Committed = false, want true")
	}
	if _, err := os.Stat(lockPath); !os.IsNotExist(err) {
		t.Fatalf("index.lock still present after commit, err=%v", err)
	}
}

func TestServiceCommitRejectsOperationInProgress(t *testing.T) {
	t.Parallel()

	service, root := newTestService(t)
	runGit(t, root, "init", "-b", "main")
	runGit(t, root, "config", "user.name", "Stacklab Test")
	runGit(t, root, "config", "user.email", "stacklab@example.com")

	mustWriteFile(t, filepath.Join(root, "config", "demo", "app.conf"), "server_name old.local;\n")
	runGit(t, root, "add", ".")
	runGit(t, root, "commit", "-m", "initial")
	mustWriteFile(t, filepath.Join(root, "config", "demo", "app.conf"), "server_name new.local;\n")
	mustWriteFile(t, filepath.Join(root, ".git", "MERGE_HEAD"), strings.Repeat("0", 40)+"\n")

	if _, err := service.Commit(context.Background(), CommitRequest{
		Message: "Update app config",
		Paths:   []string{"config/demo/app.conf"},
	}); err != ErrOperationInProgress {
		t.Fatalf("Commit(operation in progress) error = %v, want %v", err, ErrOperationInProgress)
	}
}

func TestServiceCommitAndPushValidation(t *testing.T) {
	t.Parallel()

	service, root := newTestService(t)
	runGit(t, root, "init", "-b", "main")
	runGit(t, root, "config", "user.name", "Stacklab Test")
	runGit(t, root, "config", "user.email", "stacklab@example.com")
	mustWriteFile(t, filepath.Join(root, "config", "demo", "app.conf"), "server_name old.local;\n")
	runGit(t, root, "add", ".")
	runGit(t, root, "commit", "-m", "initial")
	mustWriteFile(t, filepath.Join(root, "config", "demo", "app.conf"), "server_name changed.local;\n")

	if _, err := service.Commit(context.Background(), CommitRequest{Message: "", Paths: []string{"config/demo/app.conf"}}); err != ErrValidation {
		t.Fatalf("Commit(empty message) error = %v, want %v", err, ErrValidation)
	}
	if _, err := service.Commit(context.Background(), CommitRequest{Message: "Update", Paths: nil}); err != ErrValidation {
		t.Fatalf("Commit(empty paths) error = %v, want %v", err, ErrValidation)
	}
	if _, err := service.Commit(context.Background(), CommitRequest{Message: "Update", Paths: []string{"../etc/passwd"}}); err != ErrPathOutsideWorkspace {
		t.Fatalf("Commit(path traversal) error = %v, want %v", err, ErrPathOutsideWorkspace)
	}
	if _, err := service.Commit(context.Background(), CommitRequest{Message: "Update", Paths: []string{"config/demo/missing.conf"}}); err != ErrNotFound {
		t.Fatalf("Commit(missing path) error = %v, want %v", err, ErrNotFound)
	}
	if _, err := service.Push(context.Background()); err != ErrUpstreamNotConfigured {
		t.Fatalf("Push(no upstream) error = %v, want %v", err, ErrUpstreamNotConfigured)
	}
}

func TestClassifyGitCommitErrorPreservesStderr(t *testing.T) {
	t.Parallel()

	err := classifyGitCommitError([]byte("Author identity unknown\n\nRun git config user.name"), errors.New("exit status 128"))
	if err == nil || !strings.Contains(err.Error(), "Author identity unknown") {
		t.Fatalf("classifyGitCommitError() = %v, want stderr in error", err)
	}
}

func TestServiceStatusDiffAndCommitDetectUnreadableFile(t *testing.T) {
	t.Parallel()

	if os.Geteuid() == 0 {
		t.Skip("permission diagnostics test requires non-root user")
	}

	service, root := newTestService(t)
	runGit(t, root, "init", "-b", "main")
	runGit(t, root, "config", "user.name", "Stacklab Test")
	runGit(t, root, "config", "user.email", "stacklab@example.com")

	protectedPath := filepath.Join(root, "config", "demo", "secret.conf")
	mustWriteFile(t, protectedPath, "token=old\n")
	runGit(t, root, "add", ".")
	runGit(t, root, "commit", "-m", "initial")
	mustWriteFile(t, protectedPath, "token=new\n")
	if err := os.Chmod(protectedPath, 0o000); err != nil {
		t.Fatalf("Chmod(secret.conf) error = %v", err)
	}
	t.Cleanup(func() {
		_ = os.Chmod(protectedPath, 0o644)
	})

	status, err := service.Status(context.Background())
	if err != nil {
		t.Fatalf("Status() error = %v", err)
	}
	if len(status.Items) != 1 {
		t.Fatalf("Status().Items = %d, want 1", len(status.Items))
	}
	item := status.Items[0]
	if item.Permissions == nil || item.Permissions.Readable || item.DiffAvailable || item.CommitAllowed {
		t.Fatalf("unexpected protected status item: %#v", item)
	}
	if item.BlockedReason == nil || *item.BlockedReason != "not_readable" {
		t.Fatalf("unexpected blocked reason: %#v", item.BlockedReason)
	}

	diff, err := service.Diff(context.Background(), "config/demo/secret.conf")
	if err != nil {
		t.Fatalf("Diff(secret.conf) error = %v", err)
	}
	if diff.DiffAvailable || diff.Diff != nil || diff.BlockedReason == nil || *diff.BlockedReason != "not_readable" {
		t.Fatalf("unexpected protected diff payload: %#v", diff)
	}

	if _, err := service.Commit(context.Background(), CommitRequest{
		Message: "Update protected file",
		Paths:   []string{"config/demo/secret.conf"},
	}); err != ErrPermissionDenied {
		t.Fatalf("Commit(secret.conf) error = %v, want %v", err, ErrPermissionDenied)
	}
}

func TestServiceStatusMarksPermissionDeniedStatItemBlocked(t *testing.T) {
	t.Parallel()

	if os.Geteuid() == 0 {
		t.Skip("permission diagnostics test requires non-root user")
	}

	service, root := newTestService(t)
	runGit(t, root, "init", "-b", "main")
	runGit(t, root, "config", "user.name", "Stacklab Test")
	runGit(t, root, "config", "user.email", "stacklab@example.com")

	mustWriteFile(t, filepath.Join(root, "config", "demo", "app.conf"), "server_name old.local;\n")
	runGit(t, root, "add", ".")
	runGit(t, root, "commit", "-m", "initial")

	protectedDir := filepath.Join(root, "private")
	if err := os.MkdirAll(protectedDir, 0o700); err != nil {
		t.Fatalf("MkdirAll(private) error = %v", err)
	}
	mustWriteFile(t, filepath.Join(protectedDir, "secret.conf"), "token=secret\n")
	if err := os.Symlink("../../private/secret.conf", filepath.Join(root, "config", "demo", "secret-link.conf")); err != nil {
		t.Fatalf("Symlink(secret-link.conf) error = %v", err)
	}
	if err := os.Chmod(protectedDir, 0o000); err != nil {
		t.Fatalf("Chmod(private) error = %v", err)
	}
	t.Cleanup(func() {
		_ = os.Chmod(protectedDir, 0o700)
	})

	status, err := service.Status(context.Background())
	if err != nil {
		t.Fatalf("Status() error = %v", err)
	}
	if len(status.Items) != 1 {
		t.Fatalf("Status().Items = %d, want 1; items=%#v", len(status.Items), status.Items)
	}
	item := status.Items[0]
	if item.Path != "config/demo/secret-link.conf" || item.DiffAvailable || item.CommitAllowed {
		t.Fatalf("unexpected blocked status item: %#v", item)
	}
	if item.BlockedReason == nil || *item.BlockedReason != "not_readable" {
		t.Fatalf("unexpected blocked reason: %#v", item.BlockedReason)
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
