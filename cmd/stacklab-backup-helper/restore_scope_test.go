package main

import (
	"context"
	"os"
	"path/filepath"
	"strings"
	"testing"
)

func TestHelperRefusesFilteredAndUnknownReplacementBeforeMutation(t *testing.T) {
	target, directory := canonicalTemp(t), canonicalTemp(t)
	if err := os.WriteFile(filepath.Join(target, "excluded"), []byte("only copy"), 0600); err != nil {
		t.Fatal(err)
	}
	for _, scope := range []string{"filtered", "", "unknown"} {
		state := preparedRestore{Target: target, Mode: "replace", SourceScope: scope, Phase: "prepared"}
		if err := applyPreparedRestore(context.Background(), directory, "not-opened.tar", &state); err == nil {
			t.Fatalf("scope %q accepted", scope)
		}
		data, err := os.ReadFile(filepath.Join(target, "excluded"))
		if err != nil || string(data) != "only copy" {
			t.Fatalf("data lost: %s %v", data, err)
		}
		if state.Phase != "prepared" {
			t.Fatalf("mutation began for filtered source: %+v", state)
		}
	}
}

func TestHelperReplacementCannotTargetAuthorizationBoundary(t *testing.T) {
	root := canonicalTemp(t)
	for _, target := range []string{root, filepath.Dir(root)} {
		if err := validateRestoreScope(target, "replace", "complete", []string{root}); err == nil {
			t.Fatalf("boundary %s accepted", target)
		}
	}
	if err := validateRestoreScope(filepath.Join(root, "individual"), "replace", "complete", []string{root}); err != nil {
		t.Fatal(err)
	}
	if err := validateRestoreScope(root, "merge", "filtered", []string{root}); err != nil {
		t.Fatal(err)
	}
}

func TestHelperProbeDoesNotAcceptOtherArgumentsOrActions(t *testing.T) {
	for _, args := range [][]string{{"archive", "--probe"}, {"shell", "--probe"}, {"apply-restore", "--probe", "--token", strings.Repeat("a", 48)}} {
		if err := run(args); err == nil {
			t.Fatalf("unsafe probe accepted: %v", args)
		}
	}
}

func TestReplacementKeepsRollbackWhenRootMetadataFails(t *testing.T) {
	requireHelperTar(t)
	target, directory := canonicalTemp(t), canonicalTemp(t)
	archive := helperArchive(t, directory, map[string]string{"recovered": "new"})
	if err := os.WriteFile(filepath.Join(target, "original"), []byte("last live copy"), 0600); err != nil {
		t.Fatal(err)
	}
	// O_EXCL prevents replacing a preexisting metadata file. This fails after
	// content extraction, at the root-metadata boundary before rollback cleanup.
	if err := os.WriteFile(filepath.Join(directory, "source-root.tar"), []byte("conflict"), 0600); err != nil {
		t.Fatal(err)
	}
	state := preparedRestore{Target: target, Mode: "replace", SourceScope: "complete", Phase: "prepared"}
	if err := applyPreparedRestore(context.Background(), directory, archive, &state); err == nil {
		t.Fatal("metadata failure hidden")
	}
	data, err := os.ReadFile(filepath.Join(state.Rollback, "original"))
	if err != nil || string(data) != "last live copy" {
		t.Fatalf("rollback discarded before metadata success: %s %v", data, err)
	}
	if state.Phase != "writing" {
		t.Fatalf("failed replacement marked safe: %s", state.Phase)
	}
}
