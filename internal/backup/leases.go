package backup

import (
	"context"
	"crypto/sha256"
	"encoding/hex"
	"fmt"
	"net/url"
	"strings"
	"time"

	"stacklab/internal/jobs"
)

func repositoryKey(d Destination) string {
	address := strings.TrimSuffix(d.Repository, "/")
	if d.Kind == "s3" || d.Kind == "sftp" {
		prefix := ""
		if d.Kind == "s3" {
			prefix = "s3:"
			address = strings.TrimPrefix(address, prefix)
		}
		if parsed, err := url.Parse(address); err == nil {
			parsed.Host = strings.ToLower(parsed.Host)
			if parsed.Scheme == "https" && parsed.Port() == "443" || parsed.Scheme == "sftp" && parsed.Port() == "22" {
				parsed.Host = parsed.Hostname()
				if strings.Contains(parsed.Host, ":") {
					parsed.Host = "[" + parsed.Host + "]"
				}
			}
			address = prefix + parsed.String()
		}
	}
	sum := sha256.Sum256([]byte(d.Kind + "\x00" + address))
	return hex.EncodeToString(sum[:])
}

func (s *Service) repositoryResourcesLocked(copies []CopyResult) ([]jobs.Resource, error) {
	resources := []jobs.Resource{}
	seen := map[string]bool{}
	for _, copy := range copies {
		if copy.State != "pending" {
			continue
		}
		d, exists := s.state.Destinations[copy.DestinationID]
		if !exists {
			return nil, ErrNotFound
		}
		key := repositoryKey(d)
		if seen[key] {
			return nil, fmt.Errorf("%w: selected destinations reference the same repository", ErrInvalid)
		}
		seen[key] = true
		if s.repositoryJobs[key] != "" || s.repositoryReads[key] > 0 {
			return nil, ErrBusy
		}
		resources = append(resources, jobs.BackupRepositoryResource(key))
	}
	return resources, nil
}

func (s *Service) reserveRepositoriesLocked(jobID string, copies []CopyResult) {
	if s.repositoryJobs == nil {
		s.repositoryJobs = map[string]string{}
	}
	for _, copy := range copies {
		if copy.State == "pending" {
			s.repositoryJobs[repositoryKey(s.state.Destinations[copy.DestinationID])] = jobID
		}
	}
}

func (s *Service) releaseRepositories(jobID string) {
	s.mu.Lock()
	defer s.mu.Unlock()
	for key, holder := range s.repositoryJobs {
		if holder == jobID {
			delete(s.repositoryJobs, key)
		}
	}
}

// Called only while the failed capture/apply still owns host exclusion. An
// unresolved writer journal disables subsequent automatic work immediately,
// rather than waiting for the next application restart to surface the problem.
func (s *Service) recordRecoveryFailure(cause error) {
	inspector, ok := s.capture.(interface {
		RecoveryJournals(context.Context) ([]RecoveryJournalStatus, error)
	})
	if !ok {
		return
	}
	ctx, cancel := context.WithTimeout(context.Background(), 5*time.Second)
	defer cancel()
	journals, err := inspector.RecoveryJournals(ctx)
	if err == nil && len(journals) == 0 {
		return
	}
	s.mu.Lock()
	s.recoveryError = "Service recovery requires attention: " + cause.Error()
	s.mu.Unlock()
}

// Release each finished destination while its siblings continue transferring.
// The final reservation remains until job finalization and staging cleanup, so
// self-update drain still observes the job throughout its filesystem work.
func (s *Service) releaseCompletedRepository(jobID, destinationID string) error {
	s.mu.Lock()
	defer s.mu.Unlock()
	d, exists := s.state.Destinations[destinationID]
	if !exists {
		return nil
	}
	completed := repositoryKey(d)
	if s.repositoryJobs[completed] != jobID {
		return nil
	}
	remaining := []jobs.Resource{}
	for key, holder := range s.repositoryJobs {
		if holder == jobID && key != completed {
			remaining = append(remaining, jobs.BackupRepositoryResource(key))
		}
	}
	if len(remaining) == 0 {
		return nil
	}
	if err := s.jobs.ReplaceResources(jobID, remaining...); err != nil {
		return err
	}
	delete(s.repositoryJobs, completed)
	return nil
}
