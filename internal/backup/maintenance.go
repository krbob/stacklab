package backup

import (
	"context"
	"sort"
	"time"
)

func (s *Service) timePointer() *time.Time { value := s.now(); return &value }

// Maintenance is independent of backup capture. A conflict remains due; a
// started (including failed) occurrence is recorded once and is never replayed
// in a tight loop. Both actions use the same global admission lock as backups.
func (s *Service) runMaintenance(ctx context.Context) {
	s.mu.Lock()
	destinations := make([]Destination, 0, len(s.state.Destinations))
	for _, destination := range s.state.Destinations {
		destinations = append(destinations, destination)
	}
	s.mu.Unlock()
	sort.Slice(destinations, func(i, j int) bool { return destinations[i].ID < destinations[j].ID })
	for _, destination := range destinations {
		for _, action := range []string{"backup_check", "backup_prune"} {
			if ctx.Err() != nil {
				return
			}
			schedule, last := destination.CheckSchedule, destination.LastCheckScheduledAt
			if action == "backup_prune" {
				schedule, last = destination.PruneSchedule, destination.LastPruneScheduledAt
			}
			due, ok := latestDue(schedule, s.now())
			if !ok || last != nil && !last.Before(due) {
				continue
			}
			job, err := s.StartRepositoryJob(ctx, destination.ID, action, "scheduler")
			if err != nil {
				continue
			}
			s.mu.Lock()
			current := s.state.Destinations[destination.ID]
			if action == "backup_check" {
				current.LastCheckScheduledAt = &due
			} else {
				current.LastPruneScheduledAt = &due
			}
			s.state.Destinations[destination.ID] = current
			persistErr := s.persist(context.Background())
			if persistErr != nil {
				s.repositoryJob = ""
			}
			s.mu.Unlock()
			if persistErr != nil {
				s.finish(ctx, job, persistErr)
				continue
			}
			_, _ = s.ExecuteRepositoryJob(ctx, job, destination.ID, action, CheckRequest{ReadData: destination.CheckReadData}, RestoreRequest{})
		}
	}
}
