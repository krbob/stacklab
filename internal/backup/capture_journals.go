package backup

import (
	"context"
	"encoding/json"
	"errors"
	"fmt"
	"os"
	"path/filepath"
	"strings"

	"stacklab/internal/limitedio"
)

type RecoveryJournalStatus struct {
	RunID          string            `json:"run_id"`
	HoldStopped    bool              `json:"hold_stopped"`
	Writers        []Writer          `json:"writers"`
	Phases         map[string]string `json:"phases,omitempty"`
	PreparedTokens map[string]string `json:"prepared_tokens,omitempty"`
	Error          string            `json:"error,omitempty"`
}

func (c *NativeCapture) RecoveryJournals(ctx context.Context) ([]RecoveryJournalStatus, error) {
	result := []RecoveryJournalStatus{}
	directory := filepath.Join(c.cfg.DataDir, "backup", "journals")
	entries, err := os.ReadDir(directory)
	if errors.Is(err, os.ErrNotExist) {
		return result, nil
	}
	if err != nil {
		return nil, err
	}
	for _, entry := range entries {
		if err = ctx.Err(); err != nil {
			return nil, err
		}
		if entry.IsDir() || !strings.HasSuffix(entry.Name(), ".json") {
			continue
		}
		status := RecoveryJournalStatus{RunID: entry.Name(), Error: "invalid journal; inspect the recovery journal directory before modifying it"}
		data, readErr := limitedio.ReadFile(filepath.Join(directory, entry.Name()), 1<<20)
		var journal stopJournal
		if readErr == nil && entry.Type()&os.ModeSymlink == 0 && json.Unmarshal(data, &journal) == nil && c.validJournal(journal, entry.Name()) {
			status = RecoveryJournalStatus{RunID: journal.RunID, HoldStopped: journal.HoldStopped, Writers: journal.Writers, Phases: journal.Phases, PreparedTokens: journal.PreparedTokens}
		}
		result = append(result, status)
	}
	return result, nil
}

// ResolveRecovery retries the recorded identities, or explicitly acknowledges a
// held restore after operator inspection. Acknowledgement never starts services
// or deletes archive/rollback data maintained by the privileged helper.
func (c *NativeCapture) ResolveRecovery(ctx context.Context, runID, action string) error {
	file := c.journalPath(runID)
	info, err := os.Lstat(file)
	if err != nil {
		return err
	}
	if info.Mode()&os.ModeSymlink != 0 {
		return errors.New("journal must not be a symlink")
	}
	data, err := limitedio.ReadFile(file, 1<<20)
	if err != nil {
		return err
	}
	var journal stopJournal
	if json.Unmarshal(data, &journal) != nil || !c.validJournal(journal, filepath.Base(file)) {
		return errors.New("invalid recovery journal requires direct operator inspection")
	}
	switch action {
	case "retry":
		if journal.HoldStopped {
			return errors.New("inspect partially restored data before acknowledging the hold; automatic restart is unsafe")
		}
		return c.resume(ctx, journal)
	case "acknowledge_hold":
		if !journal.HoldStopped {
			return errors.New("journal is not a held restore; retry the recorded recovery or inspect the journal manually")
		}
		return c.removeJournal(runID)
	default:
		return fmt.Errorf("unsupported recovery resolution %q", action)
	}
}
