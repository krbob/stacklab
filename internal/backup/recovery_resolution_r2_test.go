package backup

import (
	"context"
	"encoding/json"
	"errors"
	"os"
	"path/filepath"
	"strings"
	"testing"
)

func journalInventoryCommand(t *testing.T, containers *[]captureContainer, unavailable *bool, mutations *[]string) func(context.Context, string, ...string) ([]byte, error) {
	t.Helper()
	return func(_ context.Context, _ string, args ...string) ([]byte, error) {
		if *unavailable {
			return nil, errors.New("daemon unavailable")
		}
		switch args[0] {
		case "ps":
			ids := []string{}
			for _, item := range *containers {
				ids = append(ids, item.ID)
			}
			return []byte(strings.Join(ids, "\n")), nil
		case "inspect":
			return json.Marshal(*containers)
		default:
			*mutations = append(*mutations, strings.Join(args, " "))
			return nil, errors.New("unexpected mutation")
		}
	}
}
func TestMissingWriterReconciliationAuditsInspectionWithoutStartingReplacement(t *testing.T) {
	capture, _, _ := captureFixture(t)
	journal := newStopJournal("missing", []Writer{{ContainerID: "original", StackID: "database", Service: "postgres", Running: true}})
	journal.Phases["original"] = "stop_requested"
	journal.PreparedTokens = map[string]string{"data": "secret-helper-token"}
	if err := capture.saveStopJournal(journal); err != nil {
		t.Fatal(err)
	}
	replacement := captureContainer{ID: "replacement"}
	replacement.State.Running = true
	replacement.Config.Labels = map[string]string{"com.docker.compose.project": "database", "com.docker.compose.service": "postgres"}
	containers := []captureContainer{replacement}
	unavailable, mutations := false, []string{}
	capture.command = journalInventoryCommand(t, &containers, &unavailable, &mutations)
	statuses, err := capture.RecoveryJournals(context.Background())
	if err != nil || len(statuses) != 1 {
		t.Fatal(statuses, err)
	}
	status := statuses[0]
	observation := status.Observations["original"]
	if !status.CanAcknowledgeMissing || observation.State != "missing" || len(observation.Replacements) != 1 || observation.Replacements[0].ContainerID != "replacement" || status.Phases["original"] != "stop_requested" {
		t.Fatalf("missing observed state: %+v", status)
	}
	encoded, _ := json.Marshal(status)
	if strings.Contains(string(encoded), "secret-helper-token") || strings.Contains(string(encoded), "prepared_tokens") {
		t.Fatal("public journal exposed helper capability")
	}
	input := RecoveryResolution{Action: "acknowledge_missing", Inspected: true, Reason: "Inspected recreated postgres mounts and health."}
	if err = capture.ResolveRecoveryWithInput(context.Background(), journal.RunID, input); err != nil {
		t.Fatal(err)
	}
	if len(mutations) > 0 {
		t.Fatalf("acknowledgement started replacement: %v", mutations)
	}
	if _, err = os.Stat(capture.journalPath(journal.RunID)); !errors.Is(err, os.ErrNotExist) {
		t.Fatalf("journal not cleared: %v", err)
	}
	history, err := os.ReadFile(filepath.Join(capture.cfg.DataDir, "backup", "recovery-history", filepath.Base(capture.journalPath(journal.RunID))))
	if err != nil || !strings.Contains(string(history), input.Reason) || !strings.Contains(string(history), "original") || !strings.Contains(string(history), "replacement") || strings.Contains(string(history), "secret-helper-token") {
		t.Fatalf("incomplete/private audit: %s %v", history, err)
	}
}
func TestMissingWriterAcknowledgementRejectsUnverifiedStateAndInvalidJournal(t *testing.T) {
	for _, test := range []string{"docker_unavailable", "original_exists", "partial_missing", "held", "unconfirmed", "no_reason", "invalid", "audit_unwritable"} {
		t.Run(test, func(t *testing.T) {
			capture, _, _ := captureFixture(t)
			journal := newStopJournal("missing", []Writer{{ContainerID: "original", Running: true}})
			journal.Phases["original"] = "stopped"
			input := RecoveryResolution{Action: "acknowledge_missing", Inspected: true, Reason: "Inspected replacement and shared mounts."}
			containers := []captureContainer{}
			unavailable, mutations := false, []string{}
			switch test {
			case "docker_unavailable":
				unavailable = true
			case "original_exists":
				containers = append(containers, captureContainer{ID: "original"})
			case "partial_missing":
				journal.Writers = append(journal.Writers, Writer{ContainerID: "present", Running: true})
				journal.Phases["present"] = "stopped"
				containers = append(containers, captureContainer{ID: "present"})
			case "held":
				journal.HoldStopped = true
			case "unconfirmed":
				input.Inspected = false
			case "no_reason":
				input.Reason = " "
			}
			if err := capture.saveStopJournal(journal); err != nil {
				t.Fatal(err)
			}
			if test == "invalid" {
				if err := os.WriteFile(capture.journalPath(journal.RunID), []byte("malformed"), 0600); err != nil {
					t.Fatal(err)
				}
			}
			if test == "audit_unwritable" {
				if err := os.WriteFile(filepath.Join(capture.cfg.DataDir, "backup", "recovery-history"), []byte("not a directory"), 0600); err != nil {
					t.Fatal(err)
				}
			}
			capture.command = journalInventoryCommand(t, &containers, &unavailable, &mutations)
			if err := capture.ResolveRecoveryWithInput(context.Background(), journal.RunID, input); err == nil {
				t.Fatal("unsafe acknowledgement succeeded")
			}
			if _, err := os.Stat(capture.journalPath(journal.RunID)); err != nil {
				t.Fatal("unresolved journal removed", err)
			}
			if len(mutations) != 0 {
				t.Fatal("acknowledgement mutated Docker", mutations)
			}
		})
	}
}
func TestJournalShowsObservedStateSeparatelyFromHistoricalPhase(t *testing.T) {
	capture, _, _ := captureFixture(t)
	journal := newStopJournal("state", []Writer{{ContainerID: "original", Running: true}})
	journal.Phases["original"] = "stopped"
	if err := capture.saveStopJournal(journal); err != nil {
		t.Fatal(err)
	}
	original := captureContainer{ID: "original"}
	original.State.Running = true
	containers := []captureContainer{original}
	unavailable, mutations := false, []string{}
	capture.command = journalInventoryCommand(t, &containers, &unavailable, &mutations)
	status, err := capture.RecoveryJournals(context.Background())
	if err != nil || status[0].Phases["original"] != "stopped" || status[0].Observations["original"].State != "running" || status[0].CanAcknowledgeMissing {
		t.Fatal(status, err)
	}
}

func TestRecoveryResolutionChecksAdmissionAndRechecksDockerAtExecution(t *testing.T) {
	s, _, _ := newTestService(t)
	capture, _, _ := captureFixture(t)
	s.capture = capture
	journal := newStopJournal("missing", []Writer{{ContainerID: "original", Running: true}})
	journal.Phases["original"] = "stopped"
	if err := capture.saveStopJournal(journal); err != nil {
		t.Fatal(err)
	}
	containers := []captureContainer{}
	unavailable, mutations := false, []string{}
	capture.command = journalInventoryCommand(t, &containers, &unavailable, &mutations)
	invalid := RecoveryResolution{Action: "acknowledge_missing", Inspected: false, Reason: "Inspected current service state."}
	if _, err := s.StartRecoveryResolution(context.Background(), journal.RunID, invalid); !errors.Is(err, ErrInvalid) {
		t.Fatalf("invalid request admitted: %v", err)
	}
	if _, err := s.StartRecoveryResolution(context.Background(), "unknown", RecoveryResolution{Action: "retry"}); !errors.Is(err, ErrNotFound) {
		t.Fatalf("missing journal admitted: %v", err)
	}
	input := RecoveryResolution{Action: "acknowledge_missing", Inspected: true, Reason: "Inspected current service state."}
	job, err := s.StartRecoveryResolution(context.Background(), journal.RunID, input)
	if err != nil {
		t.Fatal(err)
	}
	unavailable = true
	s.ExecuteRecoveryResolution(context.Background(), job, journal.RunID, input)
	if _, err := os.Stat(capture.journalPath(journal.RunID)); err != nil {
		t.Fatalf("journal removed after Docker state changed: %v", err)
	}
	if len(mutations) > 0 {
		t.Fatal("resolution mutated containers", mutations)
	}
}
