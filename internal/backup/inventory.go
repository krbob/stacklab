package backup

import (
	"context"
	"encoding/json"
	"strings"
	"sync"
	"time"
)

// Reconcile durable evidence only after a successful, complete inventory. A
// failed connection is not evidence that snapshots have been deleted.
func (s *Service) reconcileInventory(ctx context.Context, d Destination, snapshots []Snapshot) error {
	present := map[string]bool{}
	for _, snapshot := range snapshots {
		present[snapshot.ID] = true
	}
	s.mu.Lock()
	checkpoints := map[string]copyCheckpoint{}
	for key, checkpoint := range s.state.CopyCheckpoints {
		if strings.HasSuffix(key, "/"+d.ID) {
			checkpoints[key] = checkpoint
		}
	}
	s.mu.Unlock()
	now := s.now()
	for key, checkpoint := range checkpoints {
		checkpoint.SnapshotStatus = "missing"
		if present[checkpoint.SnapshotID] {
			checkpoint.SnapshotStatus = "present"
			if checkpoint.Verification != recoveryObjectsVerified {
				if err := s.verifySavedSnapshot(ctx, d, checkpoint.SnapshotID); err == nil {
					checkpoint.Verification = recoveryObjectsVerified
				} else {
					checkpoint.SnapshotStatus = "unknown"
				}
			}
		}
		checkpoint.SnapshotCheckedAt = &now
		checkpoints[key] = checkpoint
	}
	s.mu.Lock()
	defer s.mu.Unlock()
	for key, checkpoint := range checkpoints {
		if current := s.state.CopyCheckpoints[key]; current.SnapshotID == checkpoint.SnapshotID {
			s.state.CopyCheckpoints[key] = checkpoint
		}
	}
	current := s.state.Destinations[d.ID]
	current.LastInventoryAt, current.LastInventoryAttemptAt = &now, &now
	current.InventoryError = ""
	current.Status, current.Error = "available", ""
	s.state.Destinations[d.ID] = current
	return s.persist(context.Background())
}

func (s *Service) refreshInventory(ctx context.Context, d Destination) error {
	output, err := s.restic(ctx, d, "", "snapshots", "--tag", "stacklab")
	if err != nil {
		s.recordInventoryError(d, err)
		return err
	}
	var snapshots []Snapshot
	if err := json.Unmarshal(output, &snapshots); err != nil {
		s.recordInventoryError(d, err)
		return err
	}
	return s.reconcileInventory(ctx, d, snapshots)
}

func (s *Service) recordInventoryError(d Destination, err error) {
	s.mu.Lock()
	defer s.mu.Unlock()
	current, exists := s.state.Destinations[d.ID]
	if !exists {
		return
	}
	current.LastInventoryAttemptAt = s.timePointer()
	current.InventoryError = "Repository inventory could not be refreshed: " + err.Error()
	s.state.Destinations[d.ID] = current
	_ = s.persist(context.Background())
}

func (s *Service) refreshInventories(ctx context.Context) {
	s.mu.Lock()
	ids := []string{}
	now := s.now()
	for id, d := range s.state.Destinations {
		due := d.LastInventoryAt == nil || d.LastInventoryAt.After(now.Add(clockSkewTolerance)) || now.Sub(*d.LastInventoryAt) >= time.Hour
		retryReady := d.LastInventoryAttemptAt == nil || d.LastInventoryAttemptAt.After(now.Add(clockSkewTolerance)) || now.Sub(*d.LastInventoryAttemptAt) >= 5*time.Minute
		if due && retryReady {
			ids = append(ids, id)
		}
	}
	s.mu.Unlock()
	tasks := make(chan string)
	var workers sync.WaitGroup
	for i := 0; i < min(2, len(ids)); i++ {
		workers.Add(1)
		go func() {
			defer workers.Done()
			for id := range tasks {
				if ctx.Err() != nil {
					continue
				}
				d, release, err := s.beginRepositoryRead(ctx, id)
				if err != nil {
					continue
				}
				_ = s.refreshInventory(ctx, d)
				release()
			}
		}()
	}
	for _, id := range ids {
		select {
		case <-ctx.Done():
			close(tasks)
			workers.Wait()
			return
		case tasks <- id:
		}
	}
	close(tasks)
	workers.Wait()
}
