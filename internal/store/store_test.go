package store

import (
	"context"
	"path/filepath"
	"testing"
	"time"
)

func TestListAuditEntriesPaginatesAndFilters(t *testing.T) {
	t.Parallel()

	ctx := context.Background()
	testStore := openTestStore(t)
	baseTime := time.Date(2026, 4, 3, 12, 0, 0, 0, time.UTC)

	insertAuditEntry(t, testStore, AuditEntry{
		ID:          "audit-1",
		StackID:     stringPtr("alpha"),
		Action:      "up",
		RequestedBy: "local",
		Result:      "succeeded",
		RequestedAt: baseTime.Add(1 * time.Minute),
		TargetType:  "stack",
		TargetID:    stringPtr("alpha"),
	})
	insertAuditEntry(t, testStore, AuditEntry{
		ID:          "audit-2",
		StackID:     stringPtr("beta"),
		Action:      "pull",
		RequestedBy: "local",
		Result:      "failed",
		RequestedAt: baseTime.Add(2 * time.Minute),
		TargetType:  "stack",
		TargetID:    stringPtr("beta"),
	})
	insertAuditEntry(t, testStore, AuditEntry{
		ID:          "audit-3",
		StackID:     stringPtr("alpha"),
		Action:      "restart",
		RequestedBy: "local",
		Result:      "succeeded",
		RequestedAt: baseTime.Add(3 * time.Minute),
		TargetType:  "stack",
		TargetID:    stringPtr("alpha"),
	})

	firstPage, err := testStore.ListAuditEntries(ctx, AuditQuery{Limit: 2})
	if err != nil {
		t.Fatalf("ListAuditEntries(first page) error = %v", err)
	}
	if len(firstPage.Items) != 2 {
		t.Fatalf("ListAuditEntries(first page) len = %d, want 2", len(firstPage.Items))
	}
	if firstPage.Items[0].ID != "audit-3" || firstPage.Items[1].ID != "audit-2" {
		t.Fatalf("unexpected first page order: got %q, %q", firstPage.Items[0].ID, firstPage.Items[1].ID)
	}
	if firstPage.NextCursor == nil || *firstPage.NextCursor == "" {
		t.Fatalf("expected non-empty next cursor")
	}

	secondPage, err := testStore.ListAuditEntries(ctx, AuditQuery{Limit: 2, Cursor: *firstPage.NextCursor})
	if err != nil {
		t.Fatalf("ListAuditEntries(second page) error = %v", err)
	}
	if len(secondPage.Items) != 1 || secondPage.Items[0].ID != "audit-1" {
		t.Fatalf("unexpected second page items: %#v", secondPage.Items)
	}

	alphaOnly, err := testStore.ListAuditEntries(ctx, AuditQuery{StackID: "alpha", Limit: 10})
	if err != nil {
		t.Fatalf("ListAuditEntries(alpha) error = %v", err)
	}
	if len(alphaOnly.Items) != 2 {
		t.Fatalf("ListAuditEntries(alpha) len = %d, want 2", len(alphaOnly.Items))
	}
	for _, item := range alphaOnly.Items {
		if item.StackID == nil || *item.StackID != "alpha" {
			t.Fatalf("expected alpha-only filter, got %#v", item.StackID)
		}
	}
}

func TestLatestAuditEntriesByStackIDsReturnsNewestPerStack(t *testing.T) {
	t.Parallel()

	ctx := context.Background()
	testStore := openTestStore(t)
	baseTime := time.Date(2026, 4, 3, 13, 0, 0, 0, time.UTC)

	insertAuditEntry(t, testStore, AuditEntry{
		ID:          "audit-alpha-old",
		StackID:     stringPtr("alpha"),
		Action:      "pull",
		RequestedBy: "local",
		Result:      "failed",
		RequestedAt: baseTime,
		TargetType:  "stack",
		TargetID:    stringPtr("alpha"),
	})
	insertAuditEntry(t, testStore, AuditEntry{
		ID:          "audit-alpha-new",
		StackID:     stringPtr("alpha"),
		Action:      "up",
		RequestedBy: "local",
		Result:      "succeeded",
		RequestedAt: baseTime.Add(2 * time.Minute),
		TargetType:  "stack",
		TargetID:    stringPtr("alpha"),
	})
	insertAuditEntry(t, testStore, AuditEntry{
		ID:          "audit-beta",
		StackID:     stringPtr("beta"),
		Action:      "restart",
		RequestedBy: "local",
		Result:      "succeeded",
		RequestedAt: baseTime.Add(1 * time.Minute),
		TargetType:  "stack",
		TargetID:    stringPtr("beta"),
	})

	lastEntries, err := testStore.LatestAuditEntriesByStackIDs(ctx, []string{"alpha", "beta"})
	if err != nil {
		t.Fatalf("LatestAuditEntriesByStackIDs() error = %v", err)
	}
	if len(lastEntries) != 2 {
		t.Fatalf("LatestAuditEntriesByStackIDs() len = %d, want 2", len(lastEntries))
	}
	if entry := lastEntries["alpha"]; entry.ID != "audit-alpha-new" {
		t.Fatalf("latest alpha audit id = %q, want %q", entry.ID, "audit-alpha-new")
	}
	if entry := lastEntries["beta"]; entry.ID != "audit-beta" {
		t.Fatalf("latest beta audit id = %q, want %q", entry.ID, "audit-beta")
	}
}

func openTestStore(t *testing.T) *Store {
	t.Helper()

	databasePath := filepath.Join(t.TempDir(), "stacklab.db")
	testStore, err := Open(databasePath)
	if err != nil {
		t.Fatalf("Open() error = %v", err)
	}
	t.Cleanup(func() {
		if err := testStore.Close(); err != nil {
			t.Fatalf("Close() error = %v", err)
		}
	})

	return testStore
}

func insertAuditEntry(t *testing.T, testStore *Store, entry AuditEntry) {
	t.Helper()

	if err := testStore.CreateAuditEntry(context.Background(), entry); err != nil {
		t.Fatalf("CreateAuditEntry() error = %v", err)
	}
}

func stringPtr(value string) *string {
	return &value
}
