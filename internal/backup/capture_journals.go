package backup

import (
	"context"
	"encoding/json"
	"errors"
	"fmt"
	"os"
	"path/filepath"
	"sort"
	"strings"
	"time"

	"stacklab/internal/atomicfile"
	"stacklab/internal/limitedio"
)

type RecoveryReplacement struct {
	ContainerID string `json:"container_id"`
	StackID     string `json:"stack_id,omitempty"`
	Service     string `json:"service,omitempty"`
	State       string `json:"state"`
}
type RecoveryWriterObservation struct {
	State        string                `json:"state"`
	Error        string                `json:"error,omitempty"`
	Replacements []RecoveryReplacement `json:"replacements,omitempty"`
}
type RecoveryJournalStatus struct {
	RunID                 string                               `json:"run_id"`
	HoldStopped           bool                                 `json:"hold_stopped"`
	Writers               []Writer                             `json:"writers"`
	Phases                map[string]string                    `json:"phases,omitempty"`
	Observations          map[string]RecoveryWriterObservation `json:"observations,omitempty"`
	CanAcknowledgeMissing bool                                 `json:"can_acknowledge_missing"`
	HasPreparedRestore    bool                                 `json:"has_prepared_restore"`
	Error                 string                               `json:"error,omitempty"`
}

func containerRecoveryState(container captureContainer) string {
	if container.State.Paused {
		return "paused"
	}
	if container.State.Running {
		return "running"
	}
	return "stopped"
}
func observeJournalWriters(journal stopJournal, containers []captureContainer, discoveryErr error) map[string]RecoveryWriterObservation {
	observations := map[string]RecoveryWriterObservation{}
	for _, writer := range journal.Writers {
		observation := RecoveryWriterObservation{State: "missing"}
		if discoveryErr != nil {
			observation.State, observation.Error = "unknown", "Docker state could not be verified; retry after restoring Docker access."
		} else {
			for _, container := range containers {
				if container.ID == writer.ContainerID {
					observation.State = containerRecoveryState(container)
					continue
				}
				// Labels are hints for operator inspection, never authority to start a new
				// container or to assume it has the same mounts/data as the original.
				if writer.StackID != "" && writer.Service != "" && container.Config.Labels["com.docker.compose.project"] == writer.StackID && container.Config.Labels["com.docker.compose.service"] == writer.Service {
					observation.Replacements = append(observation.Replacements, RecoveryReplacement{ContainerID: container.ID, StackID: writer.StackID, Service: writer.Service, State: containerRecoveryState(container)})
				}
			}
			sort.Slice(observation.Replacements, func(i, j int) bool {
				return observation.Replacements[i].ContainerID < observation.Replacements[j].ContainerID
			})
		}
		observations[writer.ContainerID] = observation
	}
	return observations
}
func canAcknowledgeMissing(journal stopJournal, observations map[string]RecoveryWriterObservation) bool {
	if journal.HoldStopped {
		return false
	}
	missing := false
	for _, writer := range journal.Writers {
		phase := journal.Phases[writer.ContainerID]
		if !writer.Running || phase == "planned" || phase == "resumed" {
			continue
		}
		if observations[writer.ContainerID].State != "missing" {
			return false
		}
		missing = true
	}
	return missing
}
func (c *NativeCapture) readRecoveryJournal(runID string) (stopJournal, error) {
	file := c.journalPath(runID)
	info, err := os.Lstat(file)
	if errors.Is(err, os.ErrNotExist) {
		return stopJournal{}, ErrNotFound
	}
	if err != nil {
		return stopJournal{}, err
	}
	if info.Mode()&os.ModeSymlink != 0 {
		return stopJournal{}, fmt.Errorf("%w: recovery journal must not be a symlink", ErrInvalid)
	}
	data, err := limitedio.ReadFile(file, 1<<20)
	if err != nil {
		return stopJournal{}, err
	}
	var journal stopJournal
	if json.Unmarshal(data, &journal) != nil || !c.validJournal(journal, filepath.Base(file)) {
		return stopJournal{}, fmt.Errorf("%w: invalid recovery journal requires direct operator inspection", ErrInvalid)
	}
	return journal, nil
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
	var containers []captureContainer
	var discoveryErr error
	discovered := false
	for _, entry := range entries {
		if err = ctx.Err(); err != nil {
			return nil, err
		}
		if entry.IsDir() || !strings.HasSuffix(entry.Name(), ".json") {
			continue
		}
		status := RecoveryJournalStatus{RunID: entry.Name(), Writers: []Writer{}, Error: "invalid journal; inspect the recovery journal directory before modifying it"}
		data, readErr := limitedio.ReadFile(filepath.Join(directory, entry.Name()), 1<<20)
		var journal stopJournal
		if readErr == nil && entry.Type()&os.ModeSymlink == 0 && json.Unmarshal(data, &journal) == nil && c.validJournal(journal, entry.Name()) {
			if !discovered {
				discoveryCtx, cancel := context.WithTimeout(ctx, 10*time.Second)
				containers, discoveryErr = c.containers(discoveryCtx)
				cancel()
				discovered = true
			}
			observations := observeJournalWriters(journal, containers, discoveryErr)
			if journal.Writers == nil {
				journal.Writers = []Writer{}
			}
			status = RecoveryJournalStatus{RunID: journal.RunID, HoldStopped: journal.HoldStopped, Writers: journal.Writers, Phases: journal.Phases, Observations: observations, CanAcknowledgeMissing: canAcknowledgeMissing(journal, observations), HasPreparedRestore: len(journal.PreparedTokens) > 0}
		}
		result = append(result, status)
	}
	return result, nil
}

func (c *NativeCapture) validateRecoveryResolution(ctx context.Context, runID string, input RecoveryResolution) (stopJournal, map[string]RecoveryWriterObservation, error) {
	journal, err := c.readRecoveryJournal(runID)
	if err != nil {
		return journal, nil, err
	}
	switch input.Action {
	case "retry":
		if journal.HoldStopped {
			return journal, nil, fmt.Errorf("%w: inspect partially restored data before acknowledging the hold; automatic restart is unsafe", ErrBusy)
		}
	case "acknowledge_hold":
		if !journal.HoldStopped {
			return journal, nil, fmt.Errorf("%w: journal is not a held restore; retry recovery or inspect missing writers", ErrInvalid)
		}
	case "acknowledge_missing":
		if !input.Inspected || len(strings.TrimSpace(input.Reason)) < 8 || len(input.Reason) > 1000 {
			return journal, nil, fmt.Errorf("%w: confirm inspection and provide a reason of 8 to 1000 characters", ErrInvalid)
		}
		discoveryCtx, cancel := context.WithTimeout(ctx, 10*time.Second)
		defer cancel()
		containers, discoveryErr := c.containers(discoveryCtx)
		observations := observeJournalWriters(journal, containers, discoveryErr)
		if !canAcknowledgeMissing(journal, observations) {
			return journal, observations, fmt.Errorf("%w: all unfinished original writers must be confirmed missing; retry recovery for existing writers and inspect Docker state", ErrBusy)
		}
		return journal, observations, nil
	default:
		return journal, nil, fmt.Errorf("%w: unsupported recovery resolution", ErrInvalid)
	}
	return journal, nil, nil
}
func (c *NativeCapture) CheckRecoveryResolution(ctx context.Context, runID string, input RecoveryResolution) error {
	_, _, err := c.validateRecoveryResolution(ctx, runID, input)
	return err
}

// Persist the reviewed identities and historical phases before clearing the
// recovery block. Helper capabilities and rollback data stay private and intact.
func (c *NativeCapture) acknowledgeRecovery(journal stopJournal, input RecoveryResolution, observations map[string]RecoveryWriterObservation) error {
	record := struct {
		RunID        string                               `json:"run_id"`
		Action       string                               `json:"action"`
		Reason       string                               `json:"reason,omitempty"`
		InspectedAt  time.Time                            `json:"inspected_at"`
		Writers      []Writer                             `json:"writers"`
		Phases       map[string]string                    `json:"phases,omitempty"`
		Observations map[string]RecoveryWriterObservation `json:"observations,omitempty"`
	}{journal.RunID, input.Action, strings.TrimSpace(input.Reason), time.Now().UTC(), journal.Writers, journal.Phases, observations}
	data, err := json.Marshal(record)
	if err != nil {
		return err
	}
	directory := filepath.Join(c.cfg.DataDir, "backup", "recovery-history")
	if err = os.MkdirAll(directory, 0700); err != nil {
		return err
	}
	if err = atomicfile.WriteBytesMode(filepath.Join(directory, filepath.Base(c.journalPath(journal.RunID))), data, ".recovery-*", 0600); err != nil {
		return err
	}
	return c.removeJournal(journal.RunID)
}

// ResolveRecovery preserves the original retry/held acknowledgement interface.
func (c *NativeCapture) ResolveRecovery(ctx context.Context, runID, action string) error {
	return c.ResolveRecoveryWithInput(ctx, runID, RecoveryResolution{Action: action})
}
func (c *NativeCapture) ResolveRecoveryWithInput(ctx context.Context, runID string, input RecoveryResolution) error {
	journal, observations, err := c.validateRecoveryResolution(ctx, runID, input)
	if err != nil {
		return err
	}
	if input.Action == "retry" {
		return c.resume(ctx, journal)
	}
	return c.acknowledgeRecovery(journal, input, observations)
}
