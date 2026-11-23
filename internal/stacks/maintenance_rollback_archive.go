package stacks

import (
	"context"
	"crypto/sha256"
	"encoding/hex"
	"errors"
	"fmt"
	"io"
	"os"
	"os/exec"
	"path/filepath"
	"syscall"

	"stacklab/internal/atomicfile"
)

const (
	maxRollbackArchiveBytes    int64 = 16 << 30
	rollbackFreeReserve        int64 = 512 << 20
	rollbackSpaceCheckInterval int64 = 4 << 20
)

type rollbackArchiveWriter struct {
	path      string
	output    io.Writer
	written   int64
	checkedAt int64
	err       error
}

func (w *rollbackArchiveWriter) Write(data []byte) (int, error) {
	if int64(len(data)) > maxRollbackArchiveBytes-w.written {
		w.err = errors.New("rollback archive exceeds 16 GiB")
		return 0, w.err
	}
	if w.written == 0 || w.written-w.checkedAt >= rollbackSpaceCheckInterval {
		var stat syscall.Statfs_t
		if err := syscall.Statfs(w.path, &stat); err != nil {
			return 0, err
		}
		if uint64(stat.Bavail)*uint64(stat.Bsize) < uint64(rollbackFreeReserve+rollbackSpaceCheckInterval+int64(len(data))) {
			w.err = errors.New("insufficient free space for rollback archive (512 MiB reserve required)")
			return 0, w.err
		}
		w.checkedAt = w.written
	}
	n, err := w.output.Write(data)
	w.written += int64(n)
	return n, err
}

func (r MaintenanceRollback) saveArchive(ctx context.Context) (string, error) {
	path := filepath.Join(r.dir, "images.tar.partial")
	file, err := os.OpenFile(path, os.O_CREATE|os.O_EXCL|os.O_WRONLY, 0600)
	if err != nil {
		return "", err
	}
	defer file.Close()
	hash := sha256.New()
	writer := &rollbackArchiveWriter{path: r.dir, output: io.MultiWriter(file, hash)}
	args := []string{"image", "save"}
	for _, pin := range r.pins() {
		args = append(args, pin.Tag)
	}
	cmd := exec.CommandContext(ctx, "docker", args...)
	cmd.Stdout = writer
	if err := cmd.Run(); err != nil {
		if writer.err != nil {
			return "", writer.err
		}
		return "", fmt.Errorf("save local rollback archive: %w", err)
	}
	if writer.written == 0 {
		return "", errors.New("Docker returned an empty rollback archive")
	}
	if err := file.Sync(); err != nil {
		return "", err
	}
	if err := file.Close(); err != nil {
		return "", err
	}
	if err := os.Rename(path, filepath.Join(r.dir, "images.tar")); err != nil {
		return "", err
	}
	if err := atomicfile.SyncDir(r.dir); err != nil {
		return "", err
	}
	return hex.EncodeToString(hash.Sum(nil)), nil
}
