package backup

import (
	"context"
	"errors"
	"path/filepath"
	"testing"
)

func TestApplyMappingsRequireRegisteredResultBeforeAdmission(t *testing.T) {
	s, _, _ := newTestService(t)
	d := addDestination(t, s, "Admission")
	err := s.ValidateRepositoryRequest(context.Background(), d.ID, "backup_restore", CheckRequest{}, RestoreRequest{SnapshotID: "aabbccddeeff0011", Mappings: []RestoreMapping{{SourceID: "source", Target: "/srv/stacklab/data/app"}}})
	if !errors.Is(err, ErrInvalid) {
		t.Fatalf("unregistered mappings accepted: %v", err)
	}
	active, err := s.jobs.Store().ListActiveJobs(context.Background())
	if err != nil || len(active) != 0 {
		t.Fatalf("invalid input created active jobs: %v %v", active, err)
	}
}
func TestLegacyRecoveryPathObservesDeletionReservation(t *testing.T) {
	s, _, _ := newTestService(t)
	r := registerCatalogFixture(t, s, []catalogFixtureEntry{{name: "data", content: "test"}}, nil)
	s.mu.Lock()
	s.restoreDeletes = map[string]bool{filepath.Clean(r.Target): true}
	s.mu.Unlock()
	for _, request := range []RestoreRequest{{RestoreID: r.ID}, {RestoredDirectory: r.Target}} {
		if _, err := s.normalizeRecoveryRequest(context.Background(), r.DestinationID, request); !errors.Is(err, ErrBusy) {
			t.Fatalf("deletion reservation bypassed by %+v: %v", request, err)
		}
	}
}
