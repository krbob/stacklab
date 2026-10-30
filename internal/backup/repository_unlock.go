package backup

import (
	"context"
	"encoding/json"
	"errors"
	"fmt"
	"os"
	"slices"
	"strings"
	"syscall"
	"time"

	"stacklab/internal/store"
)

// restic's Unix stale-owner probe uses SIGHUP and treats EPERM as absence.
// Guard it with signal 0 first, without elevating restic or signalling an owner.
// This is deliberately conservative and is not atomic with external clients.
type repositoryLock struct {
	Time     time.Time `json:"time"`
	Hostname string    `json:"hostname"`
	PID      int       `json:"pid"`
	UID      uint32    `json:"uid,omitempty"` // restic omits UID 0
}

const minimumUnlockAge = 30 * time.Minute

func unlockRefused(reason string) error {
	return &repositoryCommandError{Code: "unlock_refused", message: "stale-lock removal refused: " + reason + "; stop or coordinate all repository clients before retrying (unlock_refused)"}
}

func unlockIncomplete(reason string) error {
	return &repositoryCommandError{Code: "unlock_incomplete", message: "ordinary unlock ran but recovery is incomplete: " + reason + " (unlock_incomplete)"}
}

func validateUnlockOwner(lock repositoryLock, hostname string, now time.Time, probe func(int) error) error {
	if hostname == "" || lock.Hostname == "" || lock.Time.IsZero() || lock.PID <= 0 || int64(lock.PID) > 1<<31-1 {
		return unlockRefused("lock ownership metadata is incomplete or invalid")
	}
	if lock.Hostname != hostname {
		return unlockRefused("a lock belongs to another host; confirm its owner has stopped and resolve it on that host, or follow the operator guide lost-host procedure if that host no longer exists")
	}
	err := probe(lock.PID)
	switch {
	case err == nil, errors.Is(err, syscall.EPERM):
		return unlockRefused(fmt.Sprintf("local PID %d (recorded UID %d) still exists, regardless of lock age", lock.PID, lock.UID))
	case errors.Is(err, syscall.ESRCH):
		// A matching hostname does not identify a PID namespace. A fresh lock
		// can still belong to a live process outside this process's namespace.
		if now.Sub(lock.Time) < minimumUnlockAge {
			return unlockRefused("lock is less than 30 minutes old or has a future timestamp; an absent local PID does not prove that its owner has stopped")
		}
		return nil
	default:
		return unlockRefused("the local lock owner could not be checked reliably")
	}
}

func (s *Service) repositoryLockIDs(ctx context.Context, d Destination) ([]string, error) {
	output, err := s.restic(ctx, d, "", "list", "locks", "--no-lock")
	if err != nil {
		return nil, err
	}
	ids := strings.Fields(string(output))
	if len(ids) > 128 {
		return nil, unlockRefused("too many locks for a bounded ownership review")
	}
	seen := map[string]bool{}
	for _, id := range ids {
		if len(id) != 64 || !snapshotID.MatchString(id) || seen[id] {
			return nil, unlockRefused("lock inventory could not be interpreted")
		}
		seen[id] = true
	}
	return ids, nil
}

func (s *Service) unlockRepository(ctx context.Context, job store.Job, d Destination) error {
	ctx, cancel := context.WithTimeout(ctx, 2*time.Minute)
	defer cancel()
	hostname, err := os.Hostname()
	if err != nil || hostname == "" {
		return unlockRefused("the local hostname is unavailable")
	}
	s.event(ctx, job, "Inspecting lock owners before ordinary unlock. External repository clients must remain stopped during this operation.")
	ids, err := s.repositoryLockIDs(ctx, d)
	if err != nil {
		return err
	}
	for _, id := range ids {
		output, err := s.restic(ctx, d, "", "cat", "lock", id, "--no-lock")
		if err != nil {
			if ctx.Err() != nil {
				return commandContextError(context.Cause(ctx))
			}
			var diagnostic *repositoryCommandError
			if errors.As(err, &diagnostic) {
				switch diagnostic.Code {
				case "cancelled", "timeout", "unavailable", "credentials_rejected", "permission_denied", "repository_missing", "unlock_refused":
					return err
				}
			}
			return unlockRefused("lock metadata could not be read; verify access and refresh the lock inventory")
		}
		var lock repositoryLock
		if err := json.Unmarshal(output, &lock); err != nil {
			return unlockRefused("lock metadata could not be interpreted")
		}
		if err := validateUnlockOwner(lock, hostname, s.now(), func(pid int) error { return syscall.Kill(pid, 0) }); err != nil {
			return err
		}
	}
	current, err := s.repositoryLockIDs(ctx, d)
	if err != nil {
		return err
	}
	slices.Sort(ids)
	slices.Sort(current)
	if !slices.Equal(ids, current) {
		return unlockRefused("lock inventory changed during ownership review")
	}
	if _, err := s.restic(ctx, d, "", "unlock"); err != nil {
		var diagnostic *repositoryCommandError
		if errors.As(err, &diagnostic) {
			switch diagnostic.Code {
			case "cancelled", "timeout", "unavailable", "credentials_rejected", "permission_denied", "repository_missing", "repository_locked", "storage_full":
				return err
			}
		}
		return unlockIncomplete("unlock outcome could not be established; refresh the lock inventory before retrying")
	}
	remaining, err := s.repositoryLockIDs(ctx, d)
	if err != nil {
		return unlockIncomplete("remaining locks could not be verified; restore access and refresh the lock inventory")
	}
	s.event(ctx, job, fmt.Sprintf("Ordinary unlock finished. Remaining locks: %d. Repository integrity evidence is unchanged.", len(remaining)))
	if len(remaining) != 0 {
		return unlockIncomplete("locks remain; inspect owners before retrying repository work")
	}
	return nil
}
