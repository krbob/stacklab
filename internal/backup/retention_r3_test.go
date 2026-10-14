package backup

import (
	"context"
	"encoding/json"
	"fmt"
	"reflect"
	"strings"
	"testing"
	"time"
)

type classifiedRetentionRunner struct {
	snapshots []Snapshot
	statuses  map[string]string
	listings  map[string]int
	dumps     map[string]int
	deleted   []string
	failID    string
}

func (r *classifiedRetentionRunner) Run(_ context.Context, _ string, args, _ []string, _ string) ([]byte, error) {
	for index, arg := range args {
		switch arg {
		case "snapshots":
			return json.Marshal(r.snapshots)
		case "ls":
			id := args[len(args)-1]
			if r.listings == nil {
				r.listings = map[string]int{}
			}
			r.listings[id]++
			if id == r.failID {
				return nil, commandContextError(context.DeadlineExceeded)
			}
			if r.statuses[id] == "incomplete" {
				return []byte("{\"path\":\"/\",\"type\":\"dir\"}\n"), nil
			}
			return []byte("{\"path\":\"/manifest.json\",\"type\":\"file\",\"size\":100}\n{\"path\":\"/source.tar\",\"type\":\"file\",\"size\":1000}\n"), nil
		case "dump":
			id := args[index+1]
			if r.dumps == nil {
				r.dumps = map[string]int{}
			}
			r.dumps[id]++
			switch r.statuses[id] {
			case "invalid":
				return []byte(`{"version":1,"sources":[{"source":{"id":"../unsafe"},"saved_path":"source.tar"}]}`), nil
			case "legacy":
				return []byte(`{"version":1,"sources":[{"source":{"id":"source-a"},"saved_path":"source.tar","size_bytes":1000,"archive":false}]}`), nil
			case "unsupported":
				return []byte(`{"version":2}`), nil
			default:
				return []byte(`{"version":1,"sources":[{"source":{"id":"source-a"},"saved_path":"source.tar","size_bytes":1000,"archive":true}]}`), nil
			}
		case "forget":
			r.deleted = append(r.deleted, args[index+1:]...)
			return []byte(`{}`), nil
		case "prune":
			return []byte(`{}`), nil
		}
	}
	return nil, fmt.Errorf("unexpected command: %v", args)
}

func TestRetentionCachesNegativeClassificationsAndRetainsValidSnapshots(t *testing.T) {
	s, _, _ := newTestService(t)
	d := addDestination(t, s, "Classifications")
	p := addPlan(t, s, d)
	now := time.Now().UTC()
	s.now = func() time.Time { return now }
	r := &classifiedRetentionRunner{statuses: map[string]string{}}
	for index, status := range []string{"incomplete", "legacy", "unsupported", "invalid", "complete", "complete"} {
		id := fmt.Sprintf("%064x", index+1)
		r.statuses[id] = status
		r.snapshots = append(r.snapshots, Snapshot{ID: id, Time: now.Add(-time.Duration(index) * 24 * time.Hour)})
	}
	s.runner = r
	if err := s.retain(context.Background(), d, p.ID, Retention{Daily: 1}); err != nil {
		t.Fatal(err)
	}
	wantDeleted := r.snapshots[len(r.snapshots)-1].ID
	if !reflect.DeepEqual(r.deleted, []string{wantDeleted}) {
		t.Fatalf("retention deleted unsafe snapshot: %v", r.deleted)
	}
	firstListings, firstDumps := clone(r.listings), clone(r.dumps)
	fresh := NewService(s.cfg, s.jobs, s.capture)
	fresh.runner, fresh.now = r, s.now
	if err := fresh.retain(context.Background(), d, p.ID, Retention{Daily: 1}); err != nil {
		t.Fatal(err)
	}
	if !reflect.DeepEqual(firstListings, r.listings) || !reflect.DeepEqual(firstDumps, r.dumps) {
		t.Fatalf("immutable negative evidence revalidated: listings %v -> %v, dumps %v -> %v", firstListings, r.listings, firstDumps, r.dumps)
	}
	listed, err := fresh.Snapshots(context.Background(), d.ID)
	if err != nil {
		t.Fatal(err)
	}
	for _, snapshot := range listed {
		if snapshot.RecoveryStatus != r.statuses[snapshot.ID] || snapshot.ClassifiedAt == nil {
			t.Fatalf("snapshot classification missing: %+v", snapshot)
		}
	}
	if !reflect.DeepEqual(firstListings, r.listings) || !reflect.DeepEqual(firstDumps, r.dumps) {
		t.Fatal("snapshot listing eagerly verified recovery objects")
	}
}

func TestRetentionPersistsProgressBeforeTransportFailure(t *testing.T) {
	s, _, _ := newTestService(t)
	d := addDestination(t, s, "Interrupted classification")
	p := addPlan(t, s, d)
	r := &classifiedRetentionRunner{statuses: map[string]string{"aaaaaaaa": "unsupported", "bbbbbbbb": "complete"}, failID: "bbbbbbbb", snapshots: []Snapshot{{ID: "aaaaaaaa", Time: s.now()}, {ID: "bbbbbbbb", Time: s.now()}}}
	s.runner = r
	if err := s.retain(context.Background(), d, p.ID, Retention{Daily: 1}); err == nil || !strings.Contains(err.Error(), "unavailable") {
		t.Fatalf("transport error: %v", err)
	}
	fresh := NewService(s.cfg, s.jobs, s.capture)
	if evidence, found := fresh.snapshotClassificationLocked(d.ID, "aaaaaaaa"); !found || evidence.Status != "unsupported" {
		t.Fatal("classification before failure was not persisted")
	}
	if _, found := fresh.snapshotClassificationLocked(d.ID, "bbbbbbbb"); found {
		t.Fatal("transient timeout cached as permanent incompleteness")
	}
	if fresh.state.RetentionCursors[checkpointKey(p.ID, d.ID)] != "bbbbbbbb" {
		t.Fatal("failed transport did not advance durable scan cursor")
	}
	r.failID = ""
	fresh.runner = r
	if err := fresh.retain(context.Background(), d, p.ID, Retention{Daily: 1}); err != nil {
		t.Fatal(err)
	}
	if r.listings["aaaaaaaa"] != 1 || len(r.deleted) != 0 {
		t.Fatalf("preserved classification reread or deleted: %+v", r)
	}
}

func TestRetentionCursorProgressesBeyondClassificationCacheLimit(t *testing.T) {
	s, _, _ := newTestService(t)
	d := addDestination(t, s, "Large repository")
	p := addPlan(t, s, d)
	now := s.now()
	r := &classifiedRetentionRunner{statuses: map[string]string{}}
	for index := 0; index < maxSnapshotEvidence+65; index++ {
		id := fmt.Sprintf("%064x", index+1)
		r.snapshots = append(r.snapshots, Snapshot{ID: id, Time: now.Add(-time.Duration(index) * time.Minute)})
		r.statuses[id] = "unsupported"
		if index < maxSnapshotEvidence {
			s.rememberSnapshotClassificationLocked(d.ID, id, "unsupported", now.Add(-time.Hour))
		}
	}
	s.runner = r
	for iteration := 0; iteration < 2; iteration++ {
		if err := s.retain(context.Background(), d, p.ID, Retention{Daily: 1}); err != nil {
			t.Fatal(err)
		}
	}
	last := r.snapshots[len(r.snapshots)-1].ID
	if r.listings[last] != 1 {
		t.Fatal("cache eviction starved oldest unclassified snapshot")
	}
	if len(s.state.SnapshotClassifications) != maxSnapshotEvidence {
		t.Fatalf("unbounded classification cache: %d", len(s.state.SnapshotClassifications))
	}
	checks := 0
	for _, count := range r.listings {
		checks += count
	}
	if checks != 128 || len(r.deleted) != 0 {
		t.Fatalf("classification budget=%d deleted=%v", checks, r.deleted)
	}
}

func TestRetentionRevalidatesUnsupportedClassificationVersion(t *testing.T) {
	s, _, _ := newTestService(t)
	d := addDestination(t, s, "Versioned classification")
	p := addPlan(t, s, d)
	id := "aaaaaaaa"
	s.state.SnapshotClassifications = map[string]snapshotClassification{snapshotEvidenceKey(d.ID, id): {Version: snapshotClassificationVersion + 1, Status: "complete", ClassifiedAt: s.now()}}
	r := &classifiedRetentionRunner{statuses: map[string]string{id: "unsupported"}, snapshots: []Snapshot{{ID: id, Time: s.now()}}}
	s.runner = r
	if err := s.retain(context.Background(), d, p.ID, Retention{Daily: 1}); err != nil {
		t.Fatal(err)
	}
	if r.listings[id] != 1 || s.state.SnapshotClassifications[snapshotEvidenceKey(d.ID, id)].Status != "unsupported" || len(r.deleted) != 0 {
		t.Fatal("future classification trusted without revalidation")
	}
}

func TestRetentionFailurePreservesVerifiedCopyAndRetriesMaintenance(t *testing.T) {
	s, capture, runner := newTestService(t)
	d := addDestination(t, s, "Retention failure")
	p := addPlan(t, s, d)
	runner.failRetention = true
	job, run, err := s.StartRun(context.Background(), p.ID, RunRequest{}, "test")
	if err != nil {
		t.Fatal(err)
	}
	s.ExecuteRun(context.Background(), job, run.ID)
	failed := s.state.Runs[run.ID]
	copy := failed.Copies[0]
	if copy.State != "succeeded" || copy.Verification != recoveryObjectsVerified || copy.Retention != "failed" || copy.RetentionError == "" || copy.Error != "" || failed.State != "failed" || !strings.Contains(failed.Error, "retention requires attention") {
		t.Fatalf("retention failure poisoned copy: %+v", failed)
	}
	protection := protectionFor(t, s)
	if protection.State != "attention" || !protection.Copies[0].EvidenceUsable || protection.Copies[0].LastError != copy.RetentionError {
		t.Fatalf("retention maintenance lost usable evidence: %+v", protection)
	}
	runner.failRetention = false
	s.policy.Paused = true
	job, _, err = s.StartRetry(context.Background(), run.ID, RunRequest{}, "test")
	if err != nil {
		t.Fatal(err)
	}
	s.ExecuteRun(context.Background(), job, run.ID)
	if paused := s.state.Runs[run.ID]; paused.State != "failed" || paused.Copies[0].Retention != "failed" || paused.Copies[0].RetentionError == "" {
		t.Fatalf("skipped paused retention falsely resolved maintenance: %+v", paused)
	}
	s.policy.Paused = false
	job, _, err = s.StartRetry(context.Background(), run.ID, RunRequest{}, "test")
	if err != nil {
		t.Fatal(err)
	}
	s.ExecuteRun(context.Background(), job, run.ID)
	if done := s.state.Runs[run.ID]; done.State != "succeeded" || done.Copies[0].RetentionError != "" || runner.count("backup", d.Repository) != 1 || capture.calls != 1 {
		t.Fatalf("maintenance retry recaptured, uploaded, or retained failure: %+v", done)
	}
}
