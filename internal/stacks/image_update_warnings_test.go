package stacks

import (
	"encoding/json"
	"os"
	"path/filepath"
	"reflect"
	"testing"
	"time"

	"stacklab/internal/store"
)

func TestStackListShowsOnlyAffectedLiveReplicas(t *testing.T) {
	reader := newTestServiceReader(t)
	ctx := t.Context()
	db, err := store.Open(filepath.Join(t.TempDir(), "test.db"))
	if err != nil {
		t.Fatal(err)
	}
	defer db.Close()
	reader.AttachStore(db)
	paths := stackPaths(reader.cfg.RootDir, "demo")
	if err := os.MkdirAll(paths.RootPath, 0700); err != nil {
		t.Fatal(err)
	}
	const compose = "name: my_project\nservices:\n  web:\n    image: 'demo:${TAG}'\n"
	env := "TAG=latest\n"
	if err := os.WriteFile(paths.ComposeFilePath, []byte(compose), 0600); err != nil {
		t.Fatal(err)
	}
	if err := os.WriteFile(paths.EnvFilePath, []byte(env), 0600); err != nil {
		t.Fatal(err)
	}
	shimDir := t.TempDir()
	inventory := filepath.Join(shimDir, "inventory")
	shim := "#!/bin/sh\ncase \"$1\" in\n  ps) echo fixture ;;\n  inspect) /bin/cat \"$INVENTORY\" ;;\n  *) exit 1 ;;\nesac\n"
	if err := os.WriteFile(filepath.Join(shimDir, "docker"), []byte(shim), 0700); err != nil {
		t.Fatal(err)
	}
	t.Setenv("PATH", shimDir)
	t.Setenv("INVENTORY", inventory)
	container := func(name, project, service, image, state, oneOff string) dockerInspectContainer {
		var c dockerInspectContainer
		c.Name, c.ID, c.Image, c.State.Status = "/"+name, name, image, state
		c.Config.Labels = map[string]string{"com.docker.compose.project": project, "com.docker.compose.service": service, "com.docker.compose.oneoff": oneOff}
		return c
	}
	containers := []dockerInspectContainer{
		container("still-old", "my_project", "web", "old", "running", ""),
		container("also-old", "my_project", "web", "older", "running", ""),
		container("already-new", "my_project", "web", "new", "running", ""),
		container("unrelated-image", "my_project", "web", "unknown", "running", ""),
		container("stopped", "my_project", "web", "old", "exited", ""),
		container("one-off", "my_project", "web", "old", "running", "True"),
		container("other-service", "my_project", "db", "old", "running", ""),
		container("other-project", "other", "web", "old", "running", ""),
	}
	writeRuntime := func(containers []dockerInspectContainer) {
		t.Helper()
		body, err := json.Marshal(containers)
		if err != nil {
			t.Fatal(err)
		}
		if err := os.WriteFile(inventory, body, 0600); err != nil {
			t.Fatal(err)
		}
	}
	writeRuntime(containers)
	now := time.Now().UTC()
	w := store.ImageUpdateWarning{ServiceName: "web", ProjectName: "my_project", ImageRef: "demo:${TAG}", PreviousImageIDs: []string{"old", "older"}, CandidateImageID: "new", Outcome: "not_applied", JobID: "job_update", AttemptedAt: now,
		DefinitionFingerprint: contentSHA256(compose) + ":" + contentSHA256(env)}
	if err := db.CreateJob(ctx, store.Job{ID: w.JobID, Action: "update_stacks", State: "failed", RequestedAt: now, RequestedBy: "test"}); err != nil {
		t.Fatal(err)
	}
	if err := db.UpdateImageWarnings(ctx, "demo", []store.ImageUpdateWarning{w}, nil); err != nil {
		t.Fatal(err)
	}
	check := func(want []ImageUpdateWarning) {
		t.Helper()
		result, err := reader.List(ctx, ListQuery{Search: "demo"})
		if err != nil || len(result.Items) != 1 || !reflect.DeepEqual(result.Items[0].UpdateWarnings, want) {
			t.Fatalf("stack list = %+v, err = %v, want warnings %+v", result, err, want)
		}
	}
	want := []ImageUpdateWarning{{ServiceName: "web", ContainerNames: []string{"also-old", "still-old"}, Outcome: "not_applied", JobID: "job_update", AttemptedAt: now}}
	check(want)
	writeRuntime(nil)
	check(nil)
	writeRuntime(containers)
	check(want)
	if err := os.WriteFile(paths.EnvFilePath, []byte("TAG=different"), 0600); err != nil {
		t.Fatal(err)
	}
	check(nil)
	if err := os.WriteFile(paths.EnvFilePath, []byte(env), 0600); err != nil {
		t.Fatal(err)
	}
	containers[0].Image = "new"
	writeRuntime(containers)
	want[0].ContainerNames = []string{"also-old"}
	check(want)
	containers[1].Image = "new"
	writeRuntime(containers)
	check(nil)
}

func TestImageUpdateWarningFollowsCurrentServiceDefinition(t *testing.T) {
	ref := "demo:latest"
	service := Service{Name: "app", Mode: ServiceModeImage, ImageRef: &ref}
	old := store.ImageUpdateWarning{ServiceName: "app", ImageRef: ref, PreviousImageIDs: []string{"old"}, DefinitionFingerprint: "compose:env"}
	built := old
	built.ImageRef = ""
	runtime := map[string][]Container{"demo": {{ServiceName: "app", Status: "running", ImageID: "old", Name: "demo-app-1"}}}
	newRef := "demo:replacement"
	for _, test := range []struct {
		name        string
		service     Service
		warning     store.ImageUpdateWarning
		composeHash string
		visible     bool
	}{
		{"same image after unrelated edit", service, old, "edited", true},
		{"changed image", Service{Name: "app", Mode: ServiceModeImage, ImageRef: &newRef}, old, "edited", false},
		{"removed service", Service{Name: "other", Mode: ServiceModeImage, ImageRef: &ref}, old, "edited", false},
		{"unchanged build-only target", Service{Name: "app", Mode: ServiceModeBuild}, built, "compose", true},
		{"changed build inputs", Service{Name: "app", Mode: ServiceModeBuild}, built, "edited", false},
	} {
		t.Run(test.name, func(t *testing.T) {
			definition := definitionSnapshot{Services: []Service{test.service}, ComposeSHA256: test.composeHash, EnvSHA256: "env"}
			got := activeImageUpdateWarnings("demo", definition, runtime, []store.ImageUpdateWarning{test.warning})
			if (len(got) == 1) != test.visible {
				t.Fatalf("warnings = %+v, want visible %v", got, test.visible)
			}
		})
	}
}
