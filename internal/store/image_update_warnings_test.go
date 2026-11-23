package store

import (
	"path/filepath"
	"reflect"
	"testing"
	"time"
)

func TestImageUpdateWarningsSurviveRestartAndJobRetention(t *testing.T) {
	ctx := t.Context()
	path := filepath.Join(t.TempDir(), "state.db")
	db, err := Open(path)
	if err != nil {
		t.Fatal(err)
	}
	at := time.Now().UTC()
	want := ImageUpdateWarning{StackID: "demo", ServiceName: "app", ProjectName: "my_project", ImageRef: "app:latest", DefinitionFingerprint: "compose:env",
		PreviousImageIDs: []string{"old", "older"}, CandidateRemoteDigest: "manifest", Outcome: "not_applied", JobID: "job_update", AttemptedAt: at}
	insertJob(t, db, Job{ID: want.JobID, Action: "update_stacks", State: "failed", RequestedAt: at, RequestedBy: "test"})
	if err := db.UpdateImageWarnings(ctx, "demo", []ImageUpdateWarning{want}, nil); err != nil {
		t.Fatal(err)
	}
	if err := db.Close(); err != nil {
		t.Fatal(err)
	}
	db, err = Open(path)
	if err != nil {
		t.Fatal(err)
	}
	defer db.Close()
	check := func(want ImageUpdateWarning) {
		t.Helper()
		got, err := db.ListImageWarnings(ctx)
		if err != nil || !reflect.DeepEqual(got["demo"], []ImageUpdateWarning{want}) {
			t.Fatalf("read warnings = %+v, err = %v", got, err)
		}
	}
	check(want)
	replacement := want
	replacement.Outcome = "rolled_back"
	replacement.CandidateImageID = "new"
	sibling := replacement
	sibling.ServiceName = "worker"
	if err := db.UpdateImageWarnings(ctx, "demo", []ImageUpdateWarning{replacement, sibling}, nil); err != nil {
		t.Fatal(err)
	}
	invalid := replacement
	invalid.Outcome = "unexpected"
	if err := db.UpdateImageWarnings(ctx, "demo", []ImageUpdateWarning{invalid}, []string{"worker"}); err == nil {
		t.Fatal("accepted an invalid outcome")
	}
	for _, ids := range [][]string{nil, {""}, {"old", "old"}, {"old", replacement.CandidateImageID}} {
		invalid := replacement
		invalid.PreviousImageIDs = ids
		if err := db.UpdateImageWarnings(ctx, "demo", []ImageUpdateWarning{invalid}, []string{"worker"}); err == nil {
			t.Fatalf("accepted invalid previous image IDs %v", ids)
		}
	}
	got, err := db.ListImageWarnings(ctx)
	if err != nil || !reflect.DeepEqual(got["demo"], []ImageUpdateWarning{replacement, sibling}) {
		t.Fatalf("failed batch changed evidence: %+v, %v", got, err)
	}
	other := replacement
	other.StackID = "other"
	if err := db.UpdateImageWarnings(ctx, "other", []ImageUpdateWarning{other}, nil); err != nil {
		t.Fatal(err)
	}
	if err := db.UpdateImageWarnings(ctx, "demo", nil, []string{"worker"}); err != nil {
		t.Fatal(err)
	}
	check(replacement)
	policy := OperationalRetentionPolicy{AuditEntryRetention: time.Hour, JobRetention: time.Hour, JobEventRetention: time.Hour, ExpiredSessionRetention: time.Hour}
	if _, err := db.PruneOperationalData(ctx, at.Add(2*time.Hour), policy); err != nil {
		t.Fatal(err)
	}
	replacement.JobID = ""
	check(replacement)
	if err := db.RemoveStackDefinitionState(ctx, "demo"); err != nil {
		t.Fatal(err)
	}
	got, err = db.ListImageWarnings(ctx)
	if err != nil || len(got["demo"]) != 0 || len(got["other"]) != 1 {
		t.Fatalf("remove definition warnings = %+v, %v", got, err)
	}
}
