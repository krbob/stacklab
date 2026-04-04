package configworkspace

import (
	"context"
	"errors"
	"os"
	"path/filepath"
	"testing"
	"time"

	"stacklab/internal/config"
)

func TestServiceTreeListsSortedEntriesWithStackIDs(t *testing.T) {
	t.Parallel()

	service, root := newTestService(t)
	mustMkdirAll(t, filepath.Join(root, "traefik", "dynamic"))
	mustMkdirAll(t, filepath.Join(root, "nextcloud"))
	mustWriteFile(t, filepath.Join(root, "misc.env"), "DEBUG=true\n")
	mustWriteFile(t, filepath.Join(root, "nextcloud", "nginx.conf"), "server {}\n")

	response, err := service.Tree(context.Background(), "")
	if err != nil {
		t.Fatalf("Tree(root) error = %v", err)
	}

	if response.CurrentPath != "" || response.ParentPath != nil {
		t.Fatalf("unexpected root tree navigation payload: %#v", response)
	}
	if len(response.Items) != 3 {
		t.Fatalf("Tree(root) items = %d, want 3", len(response.Items))
	}

	if got := []string{response.Items[0].Name, response.Items[1].Name, response.Items[2].Name}; got[0] != "nextcloud" || got[1] != "traefik" || got[2] != "misc.env" {
		t.Fatalf("Tree(root) sort order = %#v", got)
	}
	if response.Items[0].Type != EntryTypeDirectory || response.Items[2].Type != EntryTypeTextFile {
		t.Fatalf("unexpected item types: %#v", response.Items)
	}
	if response.Items[0].StackID == nil || *response.Items[0].StackID != "nextcloud" {
		t.Fatalf("expected stack_id on top-level stack directory, got %#v", response.Items[0].StackID)
	}
	if response.Items[2].StackID != nil {
		t.Fatalf("unexpected stack_id on non-stack file: %#v", response.Items[2].StackID)
	}
}

func TestServiceFileDetectsTextAndBinary(t *testing.T) {
	t.Parallel()

	service, root := newTestService(t)
	mustMkdirAll(t, filepath.Join(root, "nextcloud"))
	mustWriteFile(t, filepath.Join(root, "nextcloud", "app.conf"), "APP_ENV=prod\n")
	if err := os.WriteFile(filepath.Join(root, "nextcloud", "cert.p12"), []byte{0x00, 0x01, 0x02, 0x03}, 0o644); err != nil {
		t.Fatalf("WriteFile(binary) error = %v", err)
	}

	textFile, err := service.File(context.Background(), "nextcloud/app.conf")
	if err != nil {
		t.Fatalf("File(text) error = %v", err)
	}
	if textFile.Type != EntryTypeTextFile || textFile.Content == nil || *textFile.Content != "APP_ENV=prod\n" {
		t.Fatalf("unexpected text file payload: %#v", textFile)
	}
	if !textFile.Writable {
		t.Fatalf("expected text file to be writable")
	}

	binaryFile, err := service.File(context.Background(), "nextcloud/cert.p12")
	if err != nil {
		t.Fatalf("File(binary) error = %v", err)
	}
	if binaryFile.Type != EntryTypeBinaryFile || binaryFile.Content != nil || binaryFile.Writable {
		t.Fatalf("unexpected binary file payload: %#v", binaryFile)
	}
}

func TestServiceSaveFileCreatesUpdatesAndRejectsUnsafeWrites(t *testing.T) {
	t.Parallel()

	service, root := newTestService(t)
	mustMkdirAll(t, filepath.Join(root, "nextcloud"))
	mustWriteFile(t, filepath.Join(root, "nextcloud", "app.conf"), "OLD=1\n")
	if err := os.WriteFile(filepath.Join(root, "nextcloud", "blob.bin"), []byte{0x00, 0xFF}, 0o644); err != nil {
		t.Fatalf("WriteFile(blob.bin) error = %v", err)
	}

	saved, err := service.SaveFile(context.Background(), SaveFileRequest{
		Path:    "nextcloud/app.conf",
		Content: "NEW=2\n",
	})
	if err != nil {
		t.Fatalf("SaveFile(existing text) error = %v", err)
	}
	if !saved.Saved || saved.AuditAction != "save_config_file" {
		t.Fatalf("unexpected save response: %#v", saved)
	}
	content, err := os.ReadFile(filepath.Join(root, "nextcloud", "app.conf"))
	if err != nil {
		t.Fatalf("ReadFile(app.conf) error = %v", err)
	}
	if string(content) != "NEW=2\n" {
		t.Fatalf("saved content = %q, want %q", string(content), "NEW=2\n")
	}

	created, err := service.SaveFile(context.Background(), SaveFileRequest{
		Path:    "nextcloud/new.conf",
		Content: "listen 80;\n",
	})
	if err != nil {
		t.Fatalf("SaveFile(new file) error = %v", err)
	}
	if created.Path != "nextcloud/new.conf" || created.ModifiedAt.Before(time.Now().Add(-time.Minute)) {
		t.Fatalf("unexpected created file response: %#v", created)
	}

	_, err = service.SaveFile(context.Background(), SaveFileRequest{
		Path:    "../etc/passwd",
		Content: "oops\n",
	})
	if !errors.Is(err, ErrPathOutsideWorkspace) {
		t.Fatalf("SaveFile(path traversal) error = %v, want %v", err, ErrPathOutsideWorkspace)
	}

	_, err = service.SaveFile(context.Background(), SaveFileRequest{
		Path:    "nextcloud/blob.bin",
		Content: "not allowed\n",
	})
	if !errors.Is(err, ErrBinaryNotEditable) {
		t.Fatalf("SaveFile(binary) error = %v, want %v", err, ErrBinaryNotEditable)
	}
}

func TestServiceTreeAndFileRejectTypeMismatches(t *testing.T) {
	t.Parallel()

	service, root := newTestService(t)
	mustMkdirAll(t, filepath.Join(root, "nextcloud"))
	mustWriteFile(t, filepath.Join(root, "nextcloud", "app.conf"), "APP_ENV=prod\n")

	if _, err := service.Tree(context.Background(), "nextcloud/app.conf"); !errors.Is(err, ErrPathNotDirectory) {
		t.Fatalf("Tree(file path) error = %v, want %v", err, ErrPathNotDirectory)
	}
	if _, err := service.File(context.Background(), "nextcloud"); !errors.Is(err, ErrPathNotFile) {
		t.Fatalf("File(directory path) error = %v, want %v", err, ErrPathNotFile)
	}
}

func newTestService(t *testing.T) (*Service, string) {
	t.Helper()

	tempDir := t.TempDir()
	cfg := config.Config{RootDir: filepath.Join(tempDir, "root")}
	root := filepath.Join(cfg.RootDir, "config")
	mustMkdirAll(t, root)
	return NewService(cfg), root
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
