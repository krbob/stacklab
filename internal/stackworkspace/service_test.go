package stackworkspace

import (
	"context"
	"errors"
	"os"
	"path/filepath"
	"testing"
	"time"

	"stacklab/internal/config"
)

func TestServiceTreeListsAuxiliaryFilesAndFiltersCanonicalDefinitionFiles(t *testing.T) {
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
	if len(response.Items) != 2 {
		t.Fatalf("Tree(root) items = %d, want 2", len(response.Items))
	}
	if got := []string{response.Items[0].Name, response.Items[1].Name}; got[0] != "app" || got[1] != "Dockerfile" {
		t.Fatalf("Tree(root) sort order = %#v", got)
	}
}

func TestServiceFileDetectsTextAndBinaryAndRejectsCanonicalFiles(t *testing.T) {
	t.Parallel()

	service, stackRoot := newTestService(t, "demo")
	mustWriteFile(t, filepath.Join(stackRoot, "Dockerfile"), "FROM alpine:3.20\n")
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
	mustWriteFile(t, filepath.Join(stackRoot, "app", "config.yaml"), "old: true\n")
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
	content, err := os.ReadFile(filepath.Join(stackRoot, "app", "config.yaml"))
	if err != nil {
		t.Fatalf("ReadFile(config.yaml) error = %v", err)
	}
	if string(content) != "old: false\n" {
		t.Fatalf("saved content = %q, want %q", string(content), "old: false\n")
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
