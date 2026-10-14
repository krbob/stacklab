package backup

import (
	"context"
	"encoding/json"
	"errors"
	"fmt"
	"sort"
	"time"
)

// Only recovery sets whose authenticated manifest and objects were verified
// compete for retention buckets. Partial, unknown, legacy and future snapshots
// are preserved, and cannot displace a usable recovery point. Explicit IDs keep
// deletion independent of restic's arbitrary ordering of equal capture times.
func (s *Service) retain(ctx context.Context, d Destination, planID string, policy Retention) error {
	if policy.Daily == 0 && policy.Weekly == 0 && policy.Monthly == 0 {
		return nil
	}
	ctx, cancel := context.WithTimeout(ctx, 30*time.Minute)
	defer cancel()
	output, err := s.restic(ctx, d, "", "snapshots", "--tag", "stacklab,instance:"+s.state.InstanceID+",plan:"+planID)
	if err != nil {
		return err
	}
	var snapshots []Snapshot
	if err := json.Unmarshal(output, &snapshots); err != nil {
		return fmt.Errorf("invalid retention snapshot inventory")
	}
	sort.Slice(snapshots, func(i, j int) bool { return snapshots[i].Time.After(snapshots[j].Time) })
	verified := []Snapshot{}
	remainingChecks := 64
	for _, snapshot := range snapshots {
		if !snapshotID.MatchString(snapshot.ID) || snapshot.Time.IsZero() || snapshot.Time.After(s.now().Add(clockSkewTolerance)) {
			continue
		}
		key := snapshotEvidenceKey(d.ID, snapshot.ID)
		s.mu.Lock()
		_, known := s.state.VerifiedSnapshots[key]
		s.mu.Unlock()
		if !known {
			if remainingChecks == 0 {
				continue
			}
			remainingChecks--
			if err := s.verifySavedSnapshot(ctx, d, snapshot.ID); err != nil {
				if errors.Is(err, errIncompleteSnapshot) || errors.Is(err, errLegacySnapshot) {
					continue
				}
				// Availability failures are not proof of incompleteness. Do not
				// run destructive retention with a partially inspected inventory.
				return fmt.Errorf("retention verification unavailable: %w", err)
			}
			s.mu.Lock()
			rememberSnapshotEvidence(&s.state.VerifiedSnapshots, key, s.now())
			s.mu.Unlock()
		}
		verified = append(verified, snapshot)
	}
	keep := retainedSnapshotIDs(verified, policy)
	s.mu.Lock()
	// The current protection checkpoint remains addressable even if two fully
	// valid snapshots happen to share a capture timestamp.
	if checkpoint, exists := s.state.CopyCheckpoints[checkpointKey(planID, d.ID)]; exists {
		keep[checkpoint.SnapshotID] = true
	}
	err = s.persist(context.Background())
	s.mu.Unlock()
	if err != nil {
		return err
	}
	remove := []string{}
	for _, snapshot := range verified {
		if !keep[snapshot.ID] {
			remove = append(remove, snapshot.ID)
		}
	}
	// Bound argv size while retaining the exact inspected IDs. Never pass only
	// filters to forget: an empty deletion list means no command at all.
	removed := len(remove) > 0
	for len(remove) > 0 {
		n := min(128, len(remove))
		if _, err := s.restic(ctx, d, "", append([]string{"forget"}, remove[:n]...)...); err != nil {
			return err
		}
		remove = remove[n:]
	}
	if removed && (d.PruneSchedule.Frequency == "" || d.PruneSchedule.Frequency == "manual") {
		_, err = s.restic(ctx, d, "", "prune")
	}
	return err
}

// Keep the newest verified recovery point in the last N populated calendar
// periods (UTC days, ISO weeks and months), taking their union. Like restic,
// retain the oldest point too when fewer periods exist than requested.
func retainedSnapshotIDs(snapshots []Snapshot, policy Retention) map[string]bool {
	ordered := append([]Snapshot(nil), snapshots...)
	sort.Slice(ordered, func(i, j int) bool {
		if ordered[i].Time.Equal(ordered[j].Time) {
			return ordered[i].ID < ordered[j].ID
		}
		return ordered[i].Time.After(ordered[j].Time)
	})
	keep := map[string]bool{}
	for _, bucket := range []struct {
		count int
		key   func(time.Time) string
	}{
		{policy.Daily, func(t time.Time) string { return t.UTC().Format("2006-01-02") }},
		{policy.Weekly, func(t time.Time) string { y, w := t.UTC().ISOWeek(); return fmt.Sprintf("%04d-%02d", y, w) }},
		{policy.Monthly, func(t time.Time) string { return t.UTC().Format("2006-01") }},
	} {
		if bucket.count <= 0 {
			continue
		}
		periods := map[string]bool{}
		for _, snapshot := range ordered {
			period := bucket.key(snapshot.Time)
			if periods[period] {
				continue
			}
			if len(periods) >= bucket.count {
				break
			}
			periods[period] = true
			keep[snapshot.ID] = true
		}
		if len(periods) < bucket.count && len(ordered) > 0 {
			keep[ordered[len(ordered)-1].ID] = true
		}
	}
	return keep
}
