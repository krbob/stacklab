package store

import (
	"context"
	"path/filepath"
	"testing"
	"time"
)

// Regression: the image_update_status table must exist on a freshly migrated
// database (it was once appended to the migration list after execution).
func TestImageUpdateStatusRoundtripFreshDatabase(t *testing.T) {
	s, err := Open(filepath.Join(t.TempDir(), "test.db"))
	if err != nil {
		t.Fatalf("Open() error = %v", err)
	}
	defer s.Close()

	ctx := context.Background()
	status := ImageUpdateStatus{
		ImageRef:     "nginx:stable",
		LocalDigest:  "sha256:aaa",
		RemoteDigest: "sha256:bbb",
		State:        "available",
		CheckedAt:    time.Now().UTC(),
	}
	if err := s.UpsertImageUpdateStatus(ctx, status); err != nil {
		t.Fatalf("Upsert() error = %v", err)
	}

	status.State = "up_to_date"
	if err := s.UpsertImageUpdateStatus(ctx, status); err != nil {
		t.Fatalf("Upsert() update error = %v", err)
	}

	items, err := s.ListImageUpdateStatus(ctx)
	if err != nil {
		t.Fatalf("List() error = %v", err)
	}
	if len(items) != 1 || items[0].State != "up_to_date" || items[0].ImageRef != "nginx:stable" {
		t.Fatalf("items = %+v", items)
	}
}
