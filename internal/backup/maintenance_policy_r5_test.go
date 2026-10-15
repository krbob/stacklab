package backup

import (
	"context"
	"errors"
	"fmt"
	"reflect"
	"strings"
	"testing"
	"time"
)

func TestMaintenanceRetryUsesCurrentRetentionPolicy(t *testing.T) {
	for _, tc := range []struct {
		name       string
		policy     Retention
		wantRemove int
	}{
		{"keep_all", Retention{}, 0},
		{"expanded", Retention{Daily: 30}, 0},
		{"reduced", Retention{Daily: 1}, 2},
	} {
		t.Run(tc.name, func(t *testing.T) {
			s, _, runner := newTestService(t)
			d := addDestination(t, s, "Current retention")
			p := addPlan(t, s, d)
			run := retentionFailureRun(t, s, runner, p)
			original := clone(s.state.Runs[run.ID].PlanSnapshot)
			p.Destinations[0].Retention = tc.policy
			if _, err := s.SavePlan(context.Background(), p.ID, p); err != nil {
				t.Fatal(err)
			}
			future := run.ExpiresAt.Add(30 * 24 * time.Hour)
			s.now = func() time.Time { return future }
			retention := &classifiedRetentionRunner{}
			for index := 0; index < 3; index++ {
				id := fmt.Sprintf("%064x", index+1)
				at := future.Add(-time.Duration(index) * 24 * time.Hour)
				retention.snapshots = append(retention.snapshots, Snapshot{ID: id, Time: at})
				s.rememberSnapshotClassificationLocked(d.ID, id, "complete", future)
			}
			s.runner = retention
			job, _, err := s.StartRetry(context.Background(), run.ID, RunRequest{}, "test")
			if err != nil {
				t.Fatal(err)
			}
			s.ExecuteRun(context.Background(), job, run.ID)
			done := s.state.Runs[run.ID]
			if done.State != "succeeded" || len(retention.deleted) != tc.wantRemove {
				t.Fatalf("wrong current retention: state=%s removed=%v error=%s", done.State, retention.deleted, done.Copies[0].RetentionError)
			}
			if !reflect.DeepEqual(done.PlanSnapshot, original) || !reflect.DeepEqual(transferEvidence(done.Copies[0]), transferEvidence(run.Copies[0])) {
				t.Fatal("current retention rewrote capture policy or transfer evidence")
			}
		})
	}
}

func TestRetentionBlockReportsRequiredCheckScope(t *testing.T) {
	for _, tc := range []struct {
		name        string
		destination Destination
		want        string
	}{
		{"metadata corruption", Destination{MetadataCheck: CheckEvidence{Status: "failed"}}, "successful metadata check"},
		{"data corruption", Destination{DataCheck: CheckEvidence{Status: "failed"}}, "successful full data check"},
		{"metadata corruption with all hold", Destination{MetadataCheck: CheckEvidence{Status: "failed"}, CheckHold: &CheckHold{Scope: "all"}}, "successful full data check"},
		{"unknown legacy failure", Destination{CheckStatus: "failed"}, "successful full data check"},
		{"metadata hold", Destination{CheckHold: &CheckHold{Scope: "metadata"}}, "successful metadata check"},
		{"all hold", Destination{CheckHold: &CheckHold{Scope: "all"}}, "successful full data check"},
	} {
		t.Run(tc.name, func(t *testing.T) {
			err := retentionBlockedError(tc.destination)
			if !errors.Is(err, ErrBusy) || !strings.Contains(err.Error(), tc.want) {
				t.Fatalf("wrong check scope: %v; want %q", err, tc.want)
			}
		})
	}
}

func TestMaintenanceKeepAllResolvesFailureWithoutRepositoryCommandsOrClearingHold(t *testing.T) {
	s, _, runner := newTestService(t)
	d := addDestination(t, s, "Keep all")
	p := addPlan(t, s, d)
	run := retentionFailureRun(t, s, runner, p)
	p.Destinations[0].Retention = Retention{}
	if _, err := s.SavePlan(context.Background(), p.ID, p); err != nil {
		t.Fatal(err)
	}
	d.CheckHold = &CheckHold{Scope: "all", CheckedAt: s.timePointer(), Error: "unresolved check"}
	d.CheckStatus = "failed"
	s.state.Destinations[d.ID] = d
	before := len(runner.calls)
	job, _, err := s.StartRetry(context.Background(), run.ID, RunRequest{}, "test")
	if err != nil {
		t.Fatal(err)
	}
	s.ExecuteRun(context.Background(), job, run.ID)
	if done := s.state.Runs[run.ID]; done.State != "succeeded" || done.Copies[0].RetentionError != "" || len(runner.calls) != before {
		t.Fatalf("keep-all called repository or retained maintenance error: %+v", done)
	}
	if !reflect.DeepEqual(s.state.Destinations[d.ID], d) || protectionFor(t, s).Copies[0].EvidenceUsable {
		t.Fatal("keep-all cleared repository hold or revived protection")
	}
}

func TestMaintenanceExecutionReadsCurrentPolicyAfterAdmission(t *testing.T) {
	s, _, runner := newTestService(t)
	d := addDestination(t, s, "Execution policy")
	p := addPlan(t, s, d)
	run := retentionFailureRun(t, s, runner, p)
	job, _, err := s.StartRetry(context.Background(), run.ID, RunRequest{}, "test")
	if err != nil {
		t.Fatal(err)
	}
	// Normal configuration writes are excluded by the repository reservation.
	// Replacing the authoritative state directly demonstrates that execution
	// nevertheless reloads policy instead of keeping the admission snapshot.
	p.Destinations[0].Retention = Retention{}
	s.state.Plans[p.ID] = p
	before := len(runner.calls)
	s.ExecuteRun(context.Background(), job, run.ID)
	if done := s.state.Runs[run.ID]; done.State != "succeeded" || len(runner.calls) != before {
		t.Fatalf("execution used the admission policy: %+v", done)
	}
}

func TestMaintenanceRetryRejectsInactiveCurrentPolicyAtAdmissionAndExecution(t *testing.T) {
	for _, when := range []string{"admission", "execution"} {
		for _, change := range []string{"deleted_plan", "disabled_plan", "removed_assignment", "disabled_assignment", "metadata_hold", "data_hold", "integrity_failure"} {
			t.Run(when+"/"+change, func(t *testing.T) {
				s, _, runner := newTestService(t)
				d := addDestination(t, s, "Guarded retention")
				p := addPlan(t, s, d)
				run := retentionFailureRun(t, s, runner, p)
				mutate := func() {
					switch change {
					case "deleted_plan":
						delete(s.state.Plans, p.ID)
					case "disabled_plan":
						p.Enabled = false
						s.state.Plans[p.ID] = p
					case "removed_assignment":
						p.Destinations = nil
						s.state.Plans[p.ID] = p
					case "disabled_assignment":
						p.Destinations[0].Enabled = false
						s.state.Plans[p.ID] = p
					case "metadata_hold", "data_hold":
						scope := "metadata"
						if change == "data_hold" {
							scope = "all"
						}
						d.CheckHold = &CheckHold{Scope: scope, CheckedAt: s.timePointer(), Error: "unresolved check"}
						s.state.Destinations[d.ID] = d
					case "integrity_failure":
						d.CheckStatus = "failed"
						s.state.Destinations[d.ID] = d
					}
				}
				before := len(runner.calls)
				if when == "admission" {
					mutate()
					_, _, err := s.StartRetry(context.Background(), run.ID, RunRequest{}, "test")
					want := ErrInvalid
					if strings.Contains(change, "hold") || change == "integrity_failure" {
						want = ErrBusy
					}
					if !errors.Is(err, want) || len(s.repositoryJobs) != 0 {
						t.Fatalf("invalid maintenance admitted: err=%v reservations=%v", err, s.repositoryJobs)
					}
				} else {
					job, _, err := s.StartRetry(context.Background(), run.ID, RunRequest{}, "test")
					if err != nil {
						t.Fatal(err)
					}
					if _, err := s.SavePlan(context.Background(), p.ID, p); !errors.Is(err, ErrBusy) {
						t.Fatalf("policy was mutable during reserved maintenance: %v", err)
					}
					// Simulate a changed authoritative state independently of the
					// admission snapshot to exercise the second guard directly.
					mutate()
					s.ExecuteRun(context.Background(), job, run.ID)
					copy := s.state.Runs[run.ID].Copies[0]
					if copy.Retention != "failed" || copy.RetentionError == "" || !reflect.DeepEqual(transferEvidence(copy), transferEvidence(run.Copies[0])) {
						t.Fatalf("execution guard lost copy evidence: %+v", copy)
					}
				}
				if len(runner.calls) != before {
					t.Fatal("blocked maintenance called the repository")
				}
			})
		}
	}
}

func TestMaintenanceMixedRetryAllowsExplicitHealthySubset(t *testing.T) {
	s, _, runner := newTestService(t)
	a, b := addDestination(t, s, "Healthy"), addDestination(t, s, "Held")
	p := addPlan(t, s, a, b)
	run := retentionFailureRun(t, s, runner, p)
	b.CheckHold = &CheckHold{Scope: "metadata", Error: "unresolved metadata check", CheckedAt: s.timePointer()}
	s.state.Destinations[b.ID] = b
	if _, _, err := s.StartRetry(context.Background(), run.ID, RunRequest{}, "test"); !errors.Is(err, ErrBusy) {
		t.Fatalf("implicit subset accepted blocked repository: %v", err)
	}
	if !reflect.DeepEqual(s.state.Runs[run.ID].Copies, run.Copies) || len(s.repositoryJobs) != 0 {
		t.Fatal("atomic admission partially changed the failed run")
	}
	beforeB := runner.count("snapshots", b.Repository)
	runner.failRetention = false
	job, _, err := s.StartRetry(context.Background(), run.ID, RunRequest{DestinationIDs: []string{a.ID}}, "test")
	if err != nil {
		t.Fatal(err)
	}
	s.ExecuteRun(context.Background(), job, run.ID)
	done := s.state.Runs[run.ID]
	if done.Copies[0].Retention != "succeeded" || !reflect.DeepEqual(done.Copies[1], run.Copies[1]) || runner.count("snapshots", b.Repository) != beforeB {
		t.Fatalf("healthy subset touched blocked sibling: %+v", done.Copies)
	}
}
