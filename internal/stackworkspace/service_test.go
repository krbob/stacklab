package stackworkspace

import (
	"context"
	"errors"
	"os"
	"path/filepath"
	"strings"
	"testing"
	"time"

	"stacklab/internal/config"
	"stacklab/internal/fsmeta"
	"stacklab/internal/workspacerepair"
)

func TestServiceTreeListsAuxiliaryFilesAndIncludesCanonicalDefinitionFilesForRedirect(t *testing.T) {
	t.Parallel()

	service, stackRoot := newTestService(t, "demo")
	mustMkdirAll(t, filepath.Join(stackRoot, "app"))
	mustWriteFile(t, filepath.Join(stackRoot, "compose.yaml"), "services:\n  app:\n    image: nginx:alpine\n")
	mustWriteFile(t, filepath.Join(stackRoot, ".env"), "TAG=alpine\n")
	mustWriteFile(t, filepath.Join(stackRoot, "Dockerfile"), "FROM alpine:3.20\n")
	mustWriteFile(t, filepath.Join(stackRoot, "app", "config.yaml"), "port: 8080\n")

	response, err := service.Tree(context.Background(), "demo", "")
	if err != nil {
		t.Fatalf("Tree(root) error = %v", err)
	}
	if response.CurrentPath != "" || response.ParentPath != nil {
		t.Fatalf("unexpected root tree navigation payload: %#v", response)
	}
	if len(response.Items) != 4 {
		t.Fatalf("Tree(root) items = %d, want 4", len(response.Items))
	}
	if got := []string{response.Items[0].Name, response.Items[1].Name, response.Items[2].Name, response.Items[3].Name}; got[0] != "app" || got[1] != ".env" || got[2] != "compose.yaml" || got[3] != "Dockerfile" {
		t.Fatalf("Tree(root) sort order = %#v", got)
	}
}

func TestServiceFileDetectsTextAndBinaryAndRejectsCanonicalFiles(t *testing.T) {
	t.Parallel()

	service, stackRoot := newTestService(t, "demo")
	mustWriteFile(t, filepath.Join(stackRoot, "Dockerfile"), "FROM alpine:3.20\n")
	utf8BoundaryContent := strings.Repeat("a", 8191) + "é\n"
	mustWriteFile(t, filepath.Join(stackRoot, "utf8-boundary.conf"), utf8BoundaryContent)
	mustWriteFile(t, filepath.Join(stackRoot, "compose.yaml"), "services: {}\n")
	if err := os.WriteFile(filepath.Join(stackRoot, "blob.bin"), []byte{0x00, 0x01, 0x02}, 0o644); err != nil {
		t.Fatalf("WriteFile(blob.bin) error = %v", err)
	}

	textFile, err := service.File(context.Background(), "demo", "Dockerfile")
	if err != nil {
		t.Fatalf("File(Dockerfile) error = %v", err)
	}
	if textFile.Type != EntryTypeTextFile || textFile.Content == nil || *textFile.Content != "FROM alpine:3.20\n" {
		t.Fatalf("unexpected text file payload: %#v", textFile)
	}
	if textFile.RepairCapability.Supported {
		t.Fatalf("expected repair capability to be disabled by default, got %#v", textFile.RepairCapability)
	}

	utf8BoundaryFile, err := service.File(context.Background(), "demo", "utf8-boundary.conf")
	if err != nil {
		t.Fatalf("File(utf8-boundary) error = %v", err)
	}
	if utf8BoundaryFile.Type != EntryTypeTextFile || utf8BoundaryFile.Content == nil || *utf8BoundaryFile.Content != utf8BoundaryContent {
		t.Fatalf("unexpected utf8 boundary file payload: %#v", utf8BoundaryFile)
	}

	binaryFile, err := service.File(context.Background(), "demo", "blob.bin")
	if err != nil {
		t.Fatalf("File(blob.bin) error = %v", err)
	}
	if binaryFile.Type != EntryTypeBinaryFile || binaryFile.Content != nil || binaryFile.Writable {
		t.Fatalf("unexpected binary file payload: %#v", binaryFile)
	}

	if _, err := service.File(context.Background(), "demo", "compose.yaml"); !errors.Is(err, ErrReservedPath) {
		t.Fatalf("File(compose.yaml) error = %v, want %v", err, ErrReservedPath)
	}
}

func TestServiceSaveFileCreatesUpdatesAndRejectsUnsafeWrites(t *testing.T) {
	t.Parallel()

	service, stackRoot := newTestService(t, "demo")
	mustMkdirAll(t, filepath.Join(stackRoot, "app"))
	configPath := filepath.Join(stackRoot, "app", "config.yaml")
	mustWriteFile(t, configPath, "old: true\n")
	if err := os.Chmod(configPath, 0o600); err != nil {
		t.Fatalf("Chmod(config.yaml) error = %v", err)
	}
	mustWriteFile(t, filepath.Join(stackRoot, "compose.yaml"), "services: {}\n")
	if err := os.WriteFile(filepath.Join(stackRoot, "blob.bin"), []byte{0x00, 0xFF}, 0o644); err != nil {
		t.Fatalf("WriteFile(blob.bin) error = %v", err)
	}

	saved, err := service.SaveFile(context.Background(), "demo", SaveFileRequest{
		Path:    "app/config.yaml",
		Content: "old: false\n",
	})
	if err != nil {
		t.Fatalf("SaveFile(existing text) error = %v", err)
	}
	if !saved.Saved || saved.AuditAction != "save_stack_file" || saved.StackID != "demo" {
		t.Fatalf("unexpected save response: %#v", saved)
	}
	content, err := os.ReadFile(configPath)
	if err != nil {
		t.Fatalf("ReadFile(config.yaml) error = %v", err)
	}
	if string(content) != "old: false\n" {
		t.Fatalf("saved content = %q, want %q", string(content), "old: false\n")
	}
	if info, err := os.Stat(configPath); err != nil || info.Mode().Perm() != 0o600 {
		t.Fatalf("config.yaml mode = %v, %v; want 0600", infoMode(info), err)
	}

	created, err := service.SaveFile(context.Background(), "demo", SaveFileRequest{
		Path:    "app/new.conf",
		Content: "listen: 80\n",
	})
	if err != nil {
		t.Fatalf("SaveFile(new file) error = %v", err)
	}
	if created.Path != "app/new.conf" || created.ModifiedAt.Before(time.Now().Add(-time.Minute)) {
		t.Fatalf("unexpected created file response: %#v", created)
	}
	if info, err := os.Stat(filepath.Join(stackRoot, "app", "new.conf")); err != nil || info.Mode().Perm() != 0o644 {
		t.Fatalf("new.conf mode = %v, %v; want 0644", infoMode(info), err)
	}

	if _, err := service.SaveFile(context.Background(), "demo", SaveFileRequest{
		Path:    "../etc/passwd",
		Content: "oops\n",
	}); !errors.Is(err, ErrPathOutsideWorkspace) {
		t.Fatalf("SaveFile(path traversal) error = %v, want %v", err, ErrPathOutsideWorkspace)
	}

	if _, err := service.SaveFile(context.Background(), "demo", SaveFileRequest{
		Path:    "blob.bin",
		Content: "oops\n",
	}); !errors.Is(err, ErrBinaryNotEditable) {
		t.Fatalf("SaveFile(binary) error = %v, want %v", err, ErrBinaryNotEditable)
	}

	if _, err := service.SaveFile(context.Background(), "demo", SaveFileRequest{
		Path:    "compose.yaml",
		Content: "services: {}\n",
	}); !errors.Is(err, ErrReservedPath) {
		t.Fatalf("SaveFile(compose.yaml) error = %v, want %v", err, ErrReservedPath)
	}
}

func TestServiceSaveFileRejectsStaleModifiedAt(t *testing.T) {
	t.Parallel()

	service, stackRoot := newTestService(t, "demo")
	mustMkdirAll(t, filepath.Join(stackRoot, "app"))
	configPath := filepath.Join(stackRoot, "app", "config.yaml")
	mustWriteFile(t, configPath, "old: true\n")

	loaded, err := service.File(context.Background(), "demo", "app/config.yaml")
	if err != nil {
		t.Fatalf("File(config.yaml) error = %v", err)
	}
	expected := loaded.ModifiedAt

	mustWriteFile(t, configPath, "external: true\n")
	newTime := expected.Add(2 * time.Second)
	if err := os.Chtimes(configPath, newTime, newTime); err != nil {
		t.Fatalf("Chtimes(config.yaml) error = %v", err)
	}

	_, err = service.SaveFile(context.Background(), "demo", SaveFileRequest{
		Path:               "app/config.yaml",
		Content:            "old: false\n",
		ExpectedModifiedAt: &expected,
	})
	if !errors.Is(err, ErrConflict) {
		t.Fatalf("SaveFile(stale modified_at) error = %v, want %v", err, ErrConflict)
	}
	content, err := os.ReadFile(configPath)
	if err != nil {
		t.Fatalf("ReadFile(config.yaml) error = %v", err)
	}
	if string(content) != "external: true\n" {
		t.Fatalf("stale save overwrote content: %q", string(content))
	}
}

func TestServiceSaveFileRejectsSymlinkedStackRoot(t *testing.T) {
	t.Parallel()

	rootDir := filepath.Join(t.TempDir(), "root")
	stacksRoot := filepath.Join(rootDir, "stacks")
	mustMkdirAll(t, stacksRoot)
	externalRoot := t.TempDir()
	externalPath := filepath.Join(externalRoot, "secret.conf")
	mustWriteFile(t, externalPath, "original\n")
	if err := os.Symlink(externalRoot, filepath.Join(stacksRoot, "demo")); err != nil {
		t.Fatalf("Symlink(stack root) error = %v", err)
	}

	service := NewService(config.Config{RootDir: rootDir})
	_, err := service.SaveFile(context.Background(), "demo", SaveFileRequest{
		Path:    "secret.conf",
		Content: "changed\n",
	})
	if !errors.Is(err, ErrPathOutsideWorkspace) {
		t.Fatalf("SaveFile(symlinked stack root) error = %v, want %v", err, ErrPathOutsideWorkspace)
	}
	content, readErr := os.ReadFile(externalPath)
	if readErr != nil {
		t.Fatalf("ReadFile(external target) error = %v", readErr)
	}
	if string(content) != "original\n" {
		t.Fatalf("external target content = %q, want unchanged", content)
	}
}

func TestServicePermissionDiagnostics(t *testing.T) {
	t.Parallel()

	if os.Geteuid() == 0 {
		t.Skip("permission diagnostics test requires non-root user")
	}

	service, stackRoot := newTestService(t, "demo")
	protectedPath := filepath.Join(stackRoot, "secret.conf")
	mustWriteFile(t, protectedPath, "token=secret\n")
	if err := os.Chmod(protectedPath, 0o000); err != nil {
		t.Fatalf("Chmod(secret.conf) error = %v", err)
	}
	t.Cleanup(func() { _ = os.Chmod(protectedPath, 0o644) })

	tree, err := service.Tree(context.Background(), "demo", "")
	if err != nil {
		t.Fatalf("Tree(demo) error = %v", err)
	}
	if len(tree.Items) != 1 {
		t.Fatalf("Tree(demo) items = %d, want 1", len(tree.Items))
	}
	if tree.Items[0].Permissions.Readable || tree.Items[0].Permissions.Writable {
		t.Fatalf("expected protected entry to be unreadable and unwritable, got %#v", tree.Items[0].Permissions)
	}

	file, err := service.File(context.Background(), "demo", "secret.conf")
	if err != nil {
		t.Fatalf("File(secret.conf) error = %v", err)
	}
	if file.Readable || file.Content != nil {
		t.Fatalf("expected blocked file response without content, got %#v", file)
	}
	if file.BlockedReason == nil || *file.BlockedReason != "not_readable" {
		t.Fatalf("unexpected blocked reason: %#v", file.BlockedReason)
	}
}

func TestServiceRepairPermissionsUsesRepairerAndReturnsCapability(t *testing.T) {
	t.Parallel()

	service, stackRoot := newTestService(t, "demo")
	targetPath := filepath.Join(stackRoot, "Dockerfile")
	mustWriteFile(t, targetPath, "FROM alpine:3.20\n")
	resolvedTargetPath, err := filepath.EvalSymlinks(targetPath)
	if err != nil {
		t.Fatalf("EvalSymlinks(targetPath) error = %v", err)
	}
	service.repairer = fakePermissionRepairer{
		capability: workspacerepair.Capability{Supported: true, Recursive: true},
		repair: func(ctx context.Context, target string, recursive bool) (workspacerepair.Result, error) {
			if target != resolvedTargetPath {
				t.Fatalf("repair target = %q, want %q", target, resolvedTargetPath)
			}
			return workspacerepair.Result{
				ChangedItems:            1,
				TargetPermissionsBefore: fsmeta.Permissions{Mode: "0400"},
				TargetPermissionsAfter:  fsmeta.Permissions{Mode: "0600", Readable: true, Writable: true},
			}, nil
		},
	}

	response, err := service.RepairPermissions(context.Background(), "demo", RepairPermissionsRequest{
		Path:      "Dockerfile",
		Recursive: false,
	})
	if err != nil {
		t.Fatalf("RepairPermissions() error = %v", err)
	}
	if !response.Repaired || response.StackID != "demo" || response.AuditAction != "repair_stack_workspace_permissions" {
		t.Fatalf("unexpected repair response: %#v", response)
	}
	if !response.RepairCapability.Supported {
		t.Fatalf("expected repair capability to be supported, got %#v", response.RepairCapability)
	}
}

func TestNewServiceWithRepairerUsesProvidedRepairer(t *testing.T) {
	t.Parallel()

	repairer := &fakePermissionRepairer{
		capability: workspacerepair.Capability{Supported: true, Recursive: true},
		repair: func(ctx context.Context, targetPath string, recursive bool) (workspacerepair.Result, error) {
			return workspacerepair.Result{}, nil
		},
	}
	service := NewServiceWithRepairer(config.Config{RootDir: t.TempDir()}, repairer)

	if service.repairer != repairer {
		t.Fatalf("NewServiceWithRepairer did not use provided repairer")
	}
}

type fakePermissionRepairer struct {
	capability workspacerepair.Capability
	repair     func(ctx context.Context, targetPath string, recursive bool) (workspacerepair.Result, error)
}

func (f fakePermissionRepairer) Capability(ctx context.Context) workspacerepair.Capability {
	return f.capability
}

func (f fakePermissionRepairer) Repair(ctx context.Context, targetPath string, recursive bool) (workspacerepair.Result, error) {
	return f.repair(ctx, targetPath, recursive)
}

func newTestService(t *testing.T, stackID string) (*Service, string) {
	t.Helper()

	tempDir := t.TempDir()
	cfg := config.Config{RootDir: filepath.Join(tempDir, "root")}
	stackRoot := filepath.Join(cfg.RootDir, "stacks", stackID)
	mustMkdirAll(t, stackRoot)
	return NewService(cfg), stackRoot
}

func mustMkdirAll(t *testing.T, path string) {
	t.Helper()
	if err := os.MkdirAll(path, 0o755); err != nil {
		t.Fatalf("MkdirAll(%s) error = %v", path, err)
	}
}

func mustWriteFile(t *testing.T, path, content string) {
	t.Helper()
	mustMkdirAll(t, filepath.Dir(path))
	if err := os.WriteFile(path, []byte(content), 0o644); err != nil {
		t.Fatalf("WriteFile(%s) error = %v", path, err)
	}
}

func infoMode(info os.FileInfo) os.FileMode {
	if info == nil {
		return 0
	}
	return info.Mode().Perm()
}
