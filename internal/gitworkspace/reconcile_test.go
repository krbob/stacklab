package gitworkspace

import (
	"context"
	"errors"
	"os"
	"path/filepath"
	"strings"
	"testing"

	"stacklab/internal/config"
)

func divergedFixture(t *testing.T, conflict bool) (*Service, string, config.Config, IncomingResponse) {
	t.Helper()
	s, laptop, cfg := incomingFixture(t)
	writeIncomingFile(t, cfg.RootDir, "config/demo/local.conf", "local only\n")
	if conflict {
		writeIncomingFile(t, cfg.RootDir, "config/demo/app.conf", "local\n")
	}
	runGit(t, cfg.RootDir, "add", "-A")
	runGit(t, cfg.RootDir, "commit", "-m", "Local edits")
	writeIncomingFile(t, laptop, "config/demo/app.conf", "remote\n")
	writeIncomingFile(t, laptop, "config/demo/remote.conf", "remote only\n")
	pushIncoming(t, laptop)
	review, err := s.Incoming(context.Background())
	if err != nil || !review.CanReconcile {
		t.Fatalf("review=%+v err=%v", review, err)
	}
	return s, laptop, cfg, review
}
func draftRequest(d ReconcileResponse) ReconcileRequest {
	return ReconcileRequest{ID: d.ID, Revision: d.Revision}
}

func TestReconcileNonconflictingMergePreservesHistoryAndCheckoutUntilApproval(t *testing.T) {
	t.Parallel()
	s, _, cfg, review := divergedFixture(t, false)
	ctx := context.Background()
	beforeIndex, _ := os.ReadFile(filepath.Join(cfg.RootDir, ".git/index"))
	d, err := s.PrepareReconcile(ctx, PullRequest{ExpectedState: review.ExpectedState})
	if err != nil || !d.Ready || len(d.Conflicts) != 0 {
		t.Fatalf("draft=%+v err=%v", d, err)
	}
	if got := strings.TrimSpace(gitOutput(t, cfg.RootDir, "rev-parse", "HEAD")); got != review.HeadCommit {
		t.Fatal("preparation changed HEAD")
	}
	if got, _ := os.ReadFile(filepath.Join(cfg.RootDir, "config/demo/app.conf")); string(got) != "old\n" {
		t.Fatal("preview changed runtime files")
	}
	afterIndex, _ := os.ReadFile(filepath.Join(cfg.RootDir, ".git/index"))
	if string(beforeIndex) != string(afterIndex) {
		t.Fatal("preview changed real index")
	}
	file, err := s.ReconcileFile(ctx, ReconcileFileRequest{ReconcileRequest: draftRequest(d), Path: "config/demo/app.conf"})
	if err != nil || !strings.Contains(file.Diff, "+remote") {
		t.Fatalf("diff=%+v err=%v", file, err)
	}
	// Restart the service, retaining the exact draft and history.
	s = NewService(cfg)
	resumed, err := s.Reconcile(ctx)
	if err != nil || resumed.Draft == nil || resumed.Draft.ID != d.ID {
		t.Fatalf("resume=%+v %v", resumed, err)
	}
	result, err := s.ApplyReconcile(ctx, draftRequest(d))
	if err != nil || !result.Updated {
		t.Fatalf("apply=%+v %v", result, err)
	}
	parents := strings.Fields(gitOutput(t, cfg.RootDir, "show", "-s", "--format=%P", "HEAD"))
	if len(parents) != 2 || parents[0] != review.HeadCommit || parents[1] != review.TargetCommit {
		t.Fatalf("parents=%v", parents)
	}
	for path, value := range map[string]string{"app.conf": "remote\n", "local.conf": "local only\n", "remote.conf": "remote only\n"} {
		got, _ := os.ReadFile(filepath.Join(cfg.RootDir, "config/demo", path))
		if string(got) != value {
			t.Fatalf("%s=%q", path, got)
		}
	}
	status, err := s.Incoming(ctx)
	if err != nil || status.BehindCount != 0 || status.AheadCount != 2 || status.LocalChanges != 0 || status.LastPull == nil {
		t.Fatalf("after=%+v %v", status, err)
	}
	if _, err = s.ApplyReconcile(ctx, draftRequest(d)); !errors.Is(err, ErrDraftStale) {
		t.Fatalf("repeated approval=%v", err)
	}
	resumed, err = s.Reconcile(ctx)
	if err != nil || !resumed.Draft.Applied {
		t.Fatalf("applied=%+v %v", resumed, err)
	}
	if err = s.DiscardReconcile(ctx, draftRequest(d)); err != nil {
		t.Fatal(err)
	}
	resumed, err = s.Reconcile(ctx)
	if err != nil || resumed.Draft != nil {
		t.Fatal("draft not discarded")
	}
}

func TestReconcileTextConflictResolutionAndStaleBrowser(t *testing.T) {
	t.Parallel()
	s, _, cfg, review := divergedFixture(t, true)
	ctx := context.Background()
	d, err := s.PrepareReconcile(ctx, PullRequest{ExpectedState: review.ExpectedState})
	if err != nil || d.Ready || len(d.Conflicts) != 1 {
		t.Fatalf("draft=%+v %v", d, err)
	}
	if _, err = s.PrepareReconcile(ctx, PullRequest{ExpectedState: review.ExpectedState}); !errors.Is(err, ErrDraftExists) {
		t.Fatalf("replaced draft: %v", err)
	}
	request := ReconcileFileRequest{ReconcileRequest: draftRequest(d), Path: "config/demo/app.conf"}
	file, err := s.ReconcileFile(ctx, request)
	if err != nil || !file.Editable || *file.Base.Text != "old\n" || *file.Local.Text != "local\n" || *file.Remote.Text != "remote\n" {
		t.Fatalf("file=%+v %v", file, err)
	}
	if _, err = s.ApplyReconcile(ctx, draftRequest(d)); !errors.Is(err, ErrDraftStale) {
		t.Fatalf("applied unresolved: %v", err)
	}
	resolved, err := s.ResolveReconcile(ctx, ResolveRequest{ReconcileFileRequest: request, Choice: "edit", Content: "combined\n"})
	if err != nil || !resolved.Ready || resolved.Revision != 2 {
		t.Fatalf("resolved=%+v %v", resolved, err)
	}
	if _, err = s.ResolveReconcile(ctx, ResolveRequest{ReconcileFileRequest: request, Choice: "remote"}); !errors.Is(err, ErrDraftStale) {
		t.Fatalf("stale resolution accepted: %v", err)
	}
	if _, err = s.ApplyReconcile(ctx, draftRequest(d)); !errors.Is(err, ErrDraftStale) {
		t.Fatalf("stale approval accepted: %v", err)
	}
	s = NewService(cfg)
	request.ReconcileRequest = draftRequest(resolved)
	file, err = s.ReconcileFile(ctx, request)
	if err != nil || file.Content != "combined\n" || !strings.Contains(file.Diff, "+combined") {
		t.Fatalf("saved result=%+v %v", file, err)
	}
	result, err := s.ApplyReconcile(ctx, draftRequest(resolved))
	if err != nil || !result.Updated {
		t.Fatal(err)
	}
	got, _ := os.ReadFile(filepath.Join(cfg.RootDir, request.Path))
	if string(got) != "combined\n" {
		t.Fatalf("result=%q", got)
	}
}

func TestReconcileRejectsChangedCheckoutAndPreservesFiles(t *testing.T) {
	for _, scenario := range []string{"dirty", "staged", "untracked outside", "new head", "new fetch", "index flags", "index lock", "ignored collision", "merge in progress"} {
		t.Run(scenario, func(t *testing.T) {
			t.Parallel()
			s, laptop, cfg, review := divergedFixture(t, false)
			if scenario == "ignored collision" {
				writeIncomingFile(t, laptop, "config/demo/private.txt", "remote secret\n")
				runGit(t, laptop, "add", "-f", "config/demo/private.txt")
				pushIncoming(t, laptop)
				review, _ = s.Incoming(context.Background())
			}
			ctx := context.Background()
			d, err := s.PrepareReconcile(ctx, PullRequest{ExpectedState: review.ExpectedState})
			if err != nil {
				t.Fatal(err)
			}
			switch scenario {
			case "dirty":
				writeIncomingFile(t, cfg.RootDir, "config/demo/app.conf", "unsaved local\n")
			case "staged":
				writeIncomingFile(t, cfg.RootDir, "config/demo/app.conf", "staged local\n")
				runGit(t, cfg.RootDir, "add", "-A")
			case "untracked outside":
				writeIncomingFile(t, cfg.RootDir, "notes.txt", "keep me\n")
			case "new head":
				runGit(t, cfg.RootDir, "commit", "--allow-empty", "-m", "New commit")
			case "new fetch":
				writeIncomingFile(t, laptop, "config/demo/remote.conf", "newer\n")
				pushIncoming(t, laptop)
				_, _ = s.Incoming(ctx)
			case "index flags":
				runGit(t, cfg.RootDir, "update-index", "--assume-unchanged", "config/demo/app.conf")
			case "index lock":
				writeIncomingFile(t, cfg.RootDir, ".git/index.lock", "owned elsewhere")
			case "ignored collision":
				writeIncomingFile(t, cfg.RootDir, "config/demo/private.txt", "private bytes")
			case "merge in progress":
				writeIncomingFile(t, cfg.RootDir, ".git/MERGE_HEAD", review.TargetCommit+"\n")
			}
			before := gitOutput(t, cfg.RootDir, "rev-parse", "HEAD")
			if _, err = s.ApplyReconcile(ctx, draftRequest(d)); err == nil {
				t.Fatal("unsafe apply accepted")
			}
			if got := gitOutput(t, cfg.RootDir, "rev-parse", "HEAD"); got != before {
				t.Fatal("failed apply changed HEAD")
			}
			if scenario == "index lock" {
				got, _ := os.ReadFile(filepath.Join(cfg.RootDir, ".git/index.lock"))
				if string(got) != "owned elsewhere" {
					t.Fatal("removed another process lock")
				}
			}
			if scenario == "ignored collision" {
				got, _ := os.ReadFile(filepath.Join(cfg.RootDir, "config/demo/private.txt"))
				if string(got) != "private bytes" {
					t.Fatal("overwrote ignored file")
				}
			}
		})
	}
}

func TestReconcileBinaryAndDeletionChoices(t *testing.T) {
	for _, choice := range []string{"local", "remote", "delete"} {
		t.Run(choice, func(t *testing.T) {
			t.Parallel()
			s, laptop, cfg := incomingFixture(t)
			path := "config/demo/[binary].dat"
			writeIncomingFile(t, cfg.RootDir, path, "local\x00bytes")
			runGit(t, cfg.RootDir, "add", "-A")
			runGit(t, cfg.RootDir, "commit", "-m", "Local binary")
			writeIncomingFile(t, laptop, path, "remote\x00bytes")
			pushIncoming(t, laptop)
			ctx := context.Background()
			review, _ := s.Incoming(ctx)
			d, err := s.PrepareReconcile(ctx, PullRequest{ExpectedState: review.ExpectedState})
			if err != nil {
				t.Fatal(err)
			}
			r := ReconcileFileRequest{ReconcileRequest: draftRequest(d), Path: path}
			file, err := s.ReconcileFile(ctx, r)
			if err != nil || file.Editable || file.Local.Text != nil {
				t.Fatalf("binary=%+v %v", file, err)
			}
			if _, err = s.ResolveReconcile(ctx, ResolveRequest{ReconcileFileRequest: r, Choice: "edit", Content: "replacement"}); !errors.Is(err, ErrResolutionInvalid) {
				t.Fatalf("edited binary: %v", err)
			}
			d, err = s.ResolveReconcile(ctx, ResolveRequest{ReconcileFileRequest: r, Choice: choice})
			if err != nil {
				t.Fatal(err)
			}
			if _, err = s.ApplyReconcile(ctx, draftRequest(d)); err != nil {
				t.Fatal(err)
			}
			got, err := os.ReadFile(filepath.Join(cfg.RootDir, path))
			if choice == "delete" {
				if !errors.Is(err, os.ErrNotExist) {
					t.Fatal("file not deleted")
				}
			} else if string(got) != choice+"\x00bytes" {
				t.Fatalf("binary=%q", got)
			}
		})
	}
}

func TestReconcileFailedApprovalLeavesAnEditableDraft(t *testing.T) {
	t.Parallel()
	s, _, cfg, review := divergedFixture(t, true)
	ctx := context.Background()
	d, err := s.PrepareReconcile(ctx, PullRequest{ExpectedState: review.ExpectedState})
	if err != nil {
		t.Fatal(err)
	}
	d, err = s.ResolveReconcile(ctx, ResolveRequest{ReconcileFileRequest: ReconcileFileRequest{ReconcileRequest: draftRequest(d), Path: "config/demo/app.conf"}, Choice: "local"})
	if err != nil {
		t.Fatal(err)
	}
	lock := filepath.Join(cfg.RootDir, ".git/index.lock")
	if err = os.WriteFile(lock, []byte("test-owned lock"), 0600); err != nil {
		t.Fatal(err)
	}
	if _, err = s.ApplyReconcile(ctx, draftRequest(d)); err == nil {
		t.Fatal("apply ignored index lock")
	}
	if err = os.Remove(lock); err != nil {
		t.Fatal(err)
	}
	d, err = s.ResolveReconcile(ctx, ResolveRequest{ReconcileFileRequest: ReconcileFileRequest{ReconcileRequest: draftRequest(d), Path: "config/demo/app.conf"}, Choice: "remote"})
	if err != nil {
		t.Fatal(err)
	}
	if _, err = s.ApplyReconcile(ctx, draftRequest(d)); err != nil {
		t.Fatal(err)
	}
	got, _ := os.ReadFile(filepath.Join(cfg.RootDir, "config/demo/app.conf"))
	if string(got) != "remote\n" {
		t.Fatalf("applied old candidate: %q", got)
	}
}

func TestReconcileDoesNotExecuteConfiguredMergeDriversOrHooks(t *testing.T) {
	t.Parallel()
	s, laptop, cfg := incomingFixture(t)
	marker := filepath.Join(t.TempDir(), "executed")
	writeIncomingFile(t, laptop, ".gitattributes", "config/demo/app.conf merge=custom\n")
	pushIncoming(t, laptop)
	runGit(t, cfg.RootDir, "pull", "--ff-only")
	writeIncomingFile(t, cfg.RootDir, "config/demo/app.conf", "local\n")
	runGit(t, cfg.RootDir, "add", ".")
	runGit(t, cfg.RootDir, "commit", "-m", "Local")
	runGit(t, cfg.RootDir, "config", "merge.custom.driver", "touch '"+marker+"'")
	writeIncomingFile(t, laptop, "config/demo/app.conf", "remote\n")
	pushIncoming(t, laptop)
	hook := filepath.Join(cfg.RootDir, ".git/hooks/post-merge")
	if err := os.WriteFile(hook, []byte("#!/bin/sh\ntouch '"+marker+"'\n"), 0755); err != nil {
		t.Fatal(err)
	}
	ctx := context.Background()
	review, err := s.Incoming(ctx)
	if err != nil {
		t.Fatal(err)
	}
	d, err := s.PrepareReconcile(ctx, PullRequest{ExpectedState: review.ExpectedState})
	if err != nil || len(d.Conflicts) != 1 {
		t.Fatalf("draft=%+v %v", d, err)
	}
	d, err = s.ResolveReconcile(ctx, ResolveRequest{ReconcileFileRequest: ReconcileFileRequest{ReconcileRequest: draftRequest(d), Path: "config/demo/app.conf"}, Choice: "remote"})
	if err != nil {
		t.Fatal(err)
	}
	if _, err = s.ApplyReconcile(ctx, draftRequest(d)); err != nil {
		t.Fatal(err)
	}
	if _, err = os.Stat(marker); !errors.Is(err, os.ErrNotExist) {
		t.Fatal("executed configured merge driver or hook")
	}
}
