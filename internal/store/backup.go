package store

import (
	"context"
	"fmt"
	"os"
	"path/filepath"
)

// Snapshot creates a transactionally consistent standalone SQLite database.
// VACUUM INTO includes committed WAL data without copying the live database or
// holding a write transaction while files are uploaded to a repository.
func (s *Store) Snapshot(ctx context.Context, target string) error {
	if !filepath.IsAbs(target) {
		return fmt.Errorf("snapshot target must be absolute")
	}
	if _, err := os.Lstat(target); !os.IsNotExist(err) {
		return fmt.Errorf("snapshot target must not exist")
	}
	if err := os.MkdirAll(filepath.Dir(target), 0o700); err != nil {
		return err
	}
	// Pre-create with private permissions; SQLite accepts an empty target.
	f, err := os.OpenFile(target, os.O_CREATE|os.O_EXCL|os.O_WRONLY, 0o600)
	if err != nil {
		return err
	}
	if err = f.Close(); err != nil {
		return err
	}
	if _, err := s.db.ExecContext(ctx, `VACUUM INTO ?`, target); err != nil {
		_ = os.Remove(target)
		return fmt.Errorf("snapshot SQLite: %w", err)
	}
	f, err = os.Open(target)
	if err != nil {
		return err
	}
	defer f.Close()
	return f.Sync()
}
