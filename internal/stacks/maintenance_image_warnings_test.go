package stacks

import (
	"encoding/json"
	"os"
	"path/filepath"
	"slices"
	"strings"
	"testing"
	"time"

	"stacklab/internal/config"
	"stacklab/internal/store"
)

func TestFailedMaintenanceKeepsServiceSpecificImageEvidence(t *testing.T) {
	ctx := t.Context()
	dir := t.TempDir()
	stack := filepath.Join(dir, "stacks", "demo")
	if err := os.MkdirAll(stack, 0700); err != nil {
		t.Fatal(err)
	}
	write := func(path, data string, mode os.FileMode) {
		t.Helper()
		if err := os.WriteFile(path, []byte(data), mode); err != nil {
			t.Fatal(err)
		}
	}
	const raw = "name: demo_project\nservices:\n  web: {image: \"${WEB}\"}\n  mixed: {image: web:latest}\n  worker: {image: jobs:latest}\n  untouched: {image: other:latest}\n  excluded: {image: web:latest}\n"
	resolved := strings.Replace(raw, "${WEB}", "web:latest", 1)
	write(filepath.Join(stack, "compose.yaml"), raw, 0600)
	write(filepath.Join(dir, "resolved"), resolved, 0600)
	old := "sha256:" + strings.Repeat("1", 64)
	alsoOld := "sha256:" + strings.Repeat("5", 64)
	newImage := "sha256:" + strings.Repeat("2", 64)
	localManifest := "sha256:" + strings.Repeat("3", 64)
	remoteManifest := "sha256:" + strings.Repeat("4", 64)
	script := `#!/bin/sh
case "$1" in
compose) case "$*" in *" config") cat "$FIXTURE/resolved";; esac;;
ps) printf 'a web False\nb worker False\nc untouched False\nd mixed False\ne mixed False\nf mixed False\nx excluded False\n';;
inspect) cat "$FIXTURE/runtime";;
image)
  case "$6" in
    web:latest) printf '%s null\n' "$NEW_ID";;
    jobs:latest|other:latest) printf '%s null\n' "$OLD_ID";;
    "$OLD_ID") printf '%s ["jobs@%s"]\n' "$OLD_ID" "$LOCAL_MANIFEST";;
    *) exit 1;;
  esac;;
*) exit 1;;
esac
`
	write(filepath.Join(dir, "docker"), script, 0700)
	t.Setenv("PATH", dir+string(os.PathListSeparator)+os.Getenv("PATH"))
	t.Setenv("FIXTURE", dir)
	t.Setenv("NEW_ID", newImage)
	t.Setenv("OLD_ID", old)
	t.Setenv("LOCAL_MANIFEST", localManifest)
	ResetComposeCLICacheForTests()
	t.Cleanup(ResetComposeCLICacheForTests)
	services := []string{"web", "worker", "untouched", "mixed"}
	replicas := []string{"web", "worker", "untouched", "mixed", "mixed", "mixed"}
	containers := make([]dockerInspectContainer, len(replicas))
	for i, name := range replicas {
		c := maintenanceContainer(string(rune('a'+i)), "running", "healthy")
		c.Config.Labels = map[string]string{"com.docker.compose.project": "demo_project", "com.docker.compose.service": name}
		c.Image = old
		if i == 4 {
			c.Image = alsoOld
		} else if i == 5 {
			c.Image = newImage
		}
		containers[i] = c
	}
	b, err := json.Marshal(containers)
	if err != nil {
		t.Fatal(err)
	}
	write(filepath.Join(dir, "runtime"), string(b), 0600)
	db, err := store.Open(filepath.Join(dir, "state.db"))
	if err != nil {
		t.Fatal(err)
	}
	defer db.Close()
	reader := NewServiceReader(config.Config{RootDir: dir}, nil)
	reader.AttachStore(db)
	job := store.Job{ID: "job_failure", Action: "update_stacks", State: "failed", RequestedBy: "test", RequestedAt: time.Now().UTC().Add(-time.Hour)}
	if err := db.CreateJob(ctx, job); err != nil {
		t.Fatal(err)
	}
	for _, status := range []store.ImageUpdateStatus{
		{ImageRef: "jobs:latest", State: "available", LocalDigest: localManifest, RemoteDigest: remoteManifest, CheckedAt: job.RequestedAt},
		{ImageRef: "other:latest", State: "available", LocalDigest: remoteManifest, RemoteDigest: localManifest, CheckedAt: job.RequestedAt},
	} {
		if err := db.UpsertImageUpdateStatus(ctx, status); err != nil {
			t.Fatal(err)
		}
	}
	read := func() map[string]store.ImageUpdateWarning {
		t.Helper()
		rows, err := db.ListImageWarnings(ctx)
		if err != nil {
			t.Fatal(err)
		}
		found := map[string]store.ImageUpdateWarning{}
		for _, w := range rows["demo"] {
			found[w.ServiceName] = w
		}
		return found
	}
	baseline, _, err := reader.CaptureMaintenanceHealth(ctx, "demo", services)
	if err != nil {
		t.Fatal(err)
	}
	if err := reader.RecordMaintenanceImageFailure(ctx, job, baseline, MaintenanceImageRecovery{}, false); err != nil {
		t.Fatal(err)
	}
	if w := read(); len(w) != 2 || w["worker"].ServiceName != "" {
		t.Fatalf("local-only deployment reported a remote-only candidate: %+v", w)
	}
	if err := reader.RecordMaintenanceImageFailure(ctx, job, baseline, MaintenanceImageRecovery{}, true); err != nil {
		t.Fatal(err)
	}
	w := read()
	if len(w) != 3 || w["web"].ImageRef != "${WEB}" || w["web"].ProjectName != "demo_project" || w["web"].CandidateImageID != newImage || w["web"].Outcome != "not_applied" || w["worker"].CandidateRemoteDigest != remoteManifest || w["worker"].CandidateImageID != "" || w["worker"].JobID != job.ID {
		t.Fatalf("unexpected warning evidence: %+v", w)
	}
	if !slices.Equal(w["mixed"].PreviousImageIDs, []string{old, alsoOld}) || w["mixed"].CandidateImageID != newImage {
		t.Fatalf("mixed deployment must identify only the running old image IDs: %+v", w["mixed"])
	}
	if !w["web"].AttemptedAt.After(job.RequestedAt) || !w["web"].AttemptedAt.Equal(w["worker"].AttemptedAt) {
		t.Fatal("warnings must share the time of this stack's attempt")
	}
	if err := reader.RecordMaintenanceImageFailure(ctx, job, baseline, MaintenanceImageRecovery{restored: map[string]string{"web": newImage}}, false); err != nil {
		t.Fatal(err)
	}
	if w := read(); w["web"].Outcome != "rolled_back" || w["worker"].Outcome != "not_applied" {
		t.Fatalf("rollback evidence applied outside the verified group: %+v", w)
	}
	report := MaintenanceHealthReport{verified: map[string]maintenanceServiceHealth{
		"web":    {runningImageIDs: []string{newImage}, ready: true, containerCount: 1, instances: 1},
		"mixed":  {runningImageIDs: []string{newImage, alsoOld}, ready: true, containerCount: 3, instances: 3},
		"worker": {runningImageIDs: []string{old}, ready: true, containerCount: 1, instances: 1},
	}}
	if err := reader.ClearMaintenanceImageWarnings(ctx, "demo", report); err != nil {
		t.Fatal(err)
	}
	if w := read(); len(w) != 2 || !slices.Equal(w["worker"].PreviousImageIDs, []string{old}) || w["mixed"].ServiceName != "mixed" {
		t.Fatalf("verified no-pull retry cleared the still-old container: %+v", w)
	}
}

func TestFailedPullUsesRegistryTargetWhenReplicasDisagreeWithLocalTag(t *testing.T) {
	oldImage := "sha256:" + strings.Repeat("1", 64)
	appliedImage := "sha256:" + strings.Repeat("2", 64)
	staleLocalTag := "sha256:" + strings.Repeat("3", 64)
	localManifest := "sha256:" + strings.Repeat("a", 64)
	remoteManifest := "sha256:" + strings.Repeat("b", 64)
	images := map[string]maintenanceImageIdentity{
		"app:latest": {id: staleLocalTag},
		oldImage:     {id: oldImage, repoDigests: []string{"app@" + localManifest}},
		appliedImage: {id: appliedImage, repoDigests: []string{"app@" + remoteManifest}},
	}
	status := store.ImageUpdateStatus{State: "available", LocalDigest: localManifest, RemoteDigest: remoteManifest}
	lagging, candidate, remote, outcome := failedImageEvidence([]string{oldImage, appliedImage}, "app:latest", "", status, func(ref string) maintenanceImageIdentity { return images[ref] })
	if !slices.Equal(lagging, []string{oldImage}) || candidate != "" || remote != remoteManifest || outcome != "not_applied" {
		t.Fatalf("incorrect registry candidate evidence: lagging %v candidate %q remote %q outcome %q", lagging, candidate, remote, outcome)
	}
}
