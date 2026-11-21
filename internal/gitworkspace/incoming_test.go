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

func incomingFixture(t *testing.T) (*Service, string, config.Config) {
	t.Helper()
	base := t.TempDir()
	remote, laptop, checkout := filepath.Join(base, "remote.git"), filepath.Join(base, "laptop"), filepath.Join(base, "checkout")
	runGit(t, base, "init", "--bare", "--initial-branch=main", remote)
	runGit(t, base, "clone", remote, laptop)
	for _, dir := range []string{laptop} {
		runGit(t, dir, "config", "user.name", "Test")
		runGit(t, dir, "config", "user.email", "test@example.invalid")
	}
	writeIncomingFile(t, laptop, "config/demo/app.conf", "old\n")
	writeIncomingFile(t, laptop, "README.md", "readme\n")
	writeIncomingFile(t, laptop, ".gitignore", "config/demo/private.txt\n")
	runGit(t, laptop, "add", ".")
	runGit(t, laptop, "commit", "-m", "Initial")
	runGit(t, laptop, "push", "-u", "origin", "main")
	runGit(t, base, "clone", remote, checkout)
	runGit(t, checkout, "config", "user.name", "Test")
	runGit(t, checkout, "config", "user.email", "test@example.invalid")
	cfg := config.Config{RootDir: checkout, DataDir: filepath.Join(base, "state")}
	return NewService(cfg), laptop, cfg
}
func writeIncomingFile(t *testing.T, root, path, value string) {
	t.Helper()
	target := filepath.Join(root, path)
	if err := os.MkdirAll(filepath.Dir(target), 0755); err != nil {
		t.Fatal(err)
	}
	if err := os.WriteFile(target, []byte(value), 0644); err != nil {
		t.Fatal(err)
	}
}
func pushIncoming(t *testing.T, laptop string) {
	t.Helper()
	runGit(t, laptop, "add", "-A")
	runGit(t, laptop, "commit", "-m", "External configuration change")
	runGit(t, laptop, "push")
}
func TestIncomingPreviewPullAndDurableReceipt(t *testing.T) {
	t.Parallel()
	s, laptop, cfg := incomingFixture(t)
	writeIncomingFile(t, laptop, "config/demo/app.conf", "new\n")
	writeIncomingFile(t, laptop, "README.md", "updated readme\n")
	writeIncomingFile(t, laptop, "config/demo/[literal].conf", "literal\n")
	pushIncoming(t, laptop)
	ctx := context.Background()
	before := strings.TrimSpace(gitOutput(t, cfg.RootDir, "rev-parse", "HEAD"))
	review, err := s.Incoming(ctx)
	if err != nil || !review.CanPull || review.BehindCount != 1 || len(review.Files) != 3 {
		t.Fatalf("review=%+v err=%v", review, err)
	}
	if strings.TrimSpace(gitOutput(t, cfg.RootDir, "rev-parse", "HEAD")) != before {
		t.Fatal("fetch changed HEAD")
	}
	if b, _ := os.ReadFile(filepath.Join(cfg.RootDir, "config/demo/app.conf")); string(b) != "old\n" {
		t.Fatal("preview wrote files")
	}
	for _, path := range []string{"config/demo/app.conf", "config/demo/[literal].conf", "README.md"} {
		diff, err := s.IncomingDiff(ctx, IncomingDiffRequest{Path: path, ExpectedState: review.ExpectedState})
		if err != nil || diff.Diff == "" {
			t.Fatalf("diff %+v: %v", diff, err)
		}
	}
	if _, err := s.IncomingDiff(ctx, IncomingDiffRequest{Path: "../outside", ExpectedState: review.ExpectedState}); !errors.Is(err, ErrNotFound) {
		t.Fatalf("unexpected path accepted: %v", err)
	}
	marker := filepath.Join(t.TempDir(), "hook-executed")
	hook := filepath.Join(cfg.RootDir, ".git/hooks/post-merge")
	if err := os.WriteFile(hook, []byte("#!/bin/sh\ntouch '"+marker+"'\n"), 0755); err != nil {
		t.Fatal(err)
	}
	result, err := s.Pull(ctx, PullRequest{ExpectedState: review.ExpectedState})
	if err != nil || !result.Updated || result.Receipt.HeadCommit != review.TargetCommit {
		t.Fatalf("pull=%+v err=%v", result, err)
	}
	if _, err := os.Stat(marker); !errors.Is(err, os.ErrNotExist) {
		t.Fatal("pull ran a Git hook")
	}
	if b, _ := os.ReadFile(filepath.Join(cfg.RootDir, "config/demo/app.conf")); string(b) != "new\n" {
		t.Fatal("configuration was not downloaded")
	}
	after, err := NewService(cfg).Incoming(ctx)
	if err != nil || after.BehindCount != 0 || after.LastPull == nil || after.LastPull.HeadCommit != review.TargetCommit {
		t.Fatalf("restarted review=%+v err=%v", after, err)
	}
	// Recover a receipt written just before checkout when completion was interrupted.
	if err := s.saveSyncJournal(syncJournal{Pending: &result.Receipt}); err != nil {
		t.Fatal(err)
	}
	after, err = NewService(cfg).Incoming(ctx)
	if err != nil || after.LastPull == nil {
		t.Fatalf("pending receipt not reconciled: %v", err)
	}
}

func TestIncomingRejectsChangedReviewAndLocalChanges(t *testing.T) {
	for _, scenario := range []string{"tracked outside roots", "untracked outside roots", "staged", "diverged", "new fetch", "new local commit", "unfinished merge", "assume unchanged", "skip worktree"} {
		t.Run(scenario, func(t *testing.T) {
			t.Parallel()
			s, laptop, cfg := incomingFixture(t)
			writeIncomingFile(t, laptop, "config/demo/app.conf", "new\n")
			pushIncoming(t, laptop)
			review, err := s.Incoming(context.Background())
			if err != nil {
				t.Fatal(err)
			}
			switch scenario {
			case "assume unchanged", "skip worktree":
				flag := "--assume-unchanged"
				if scenario == "skip worktree" {
					flag = "--skip-worktree"
				}
				runGit(t, cfg.RootDir, "update-index", flag, "config/demo/app.conf")
				writeIncomingFile(t, cfg.RootDir, "config/demo/app.conf", "hidden local data\n")
			case "tracked outside roots":
				writeIncomingFile(t, cfg.RootDir, "README.md", "local\n")
			case "untracked outside roots":
				writeIncomingFile(t, cfg.RootDir, "notes.txt", "local\n")
			case "staged":
				writeIncomingFile(t, cfg.RootDir, "config/demo/app.conf", "staged\n")
				runGit(t, cfg.RootDir, "add", ".")
			case "diverged", "new local commit":
				writeIncomingFile(t, cfg.RootDir, "README.md", "local\n")
				runGit(t, cfg.RootDir, "add", ".")
				runGit(t, cfg.RootDir, "commit", "-m", "Local commit")
			case "new fetch":
				writeIncomingFile(t, laptop, "README.md", "next\n")
				pushIncoming(t, laptop)
			case "unfinished merge":
				writeIncomingFile(t, cfg.RootDir, ".git/MERGE_HEAD", review.TargetCommit+"\n")
			}
			if scenario == "new fetch" || scenario == "diverged" {
				updated, err := s.Incoming(context.Background())
				if err != nil {
					t.Fatal(err)
				}
				if scenario == "diverged" {
					if updated.CanPull || updated.BlockedReason != "diverged" {
						t.Fatalf("not blocked: %+v", updated)
					}
					review = updated
				}
			}
			before := gitOutput(t, cfg.RootDir, "rev-parse", "HEAD")
			if _, err := s.Pull(context.Background(), PullRequest{ExpectedState: review.ExpectedState}); err == nil {
				t.Fatal("unsafe download accepted")
			}
			if gitOutput(t, cfg.RootDir, "rev-parse", "HEAD") != before {
				t.Fatal("rejected operation moved HEAD")
			}
		})
	}
}

func TestPullPreservesIgnoredFileAndExistingIndexLock(t *testing.T) {
	for _, scenario := range []string{"ignored collision", "index lock"} {
		t.Run(scenario, func(t *testing.T) {
			t.Parallel()
			s, laptop, cfg := incomingFixture(t)
			writeIncomingFile(t, laptop, "config/demo/private.txt", "remote\n")
			runGit(t, laptop, "add", "-f", "config/demo/private.txt")
			pushIncoming(t, laptop)
			review, err := s.Incoming(context.Background())
			if err != nil {
				t.Fatal(err)
			}
			path := "config/demo/private.txt"
			if scenario == "index lock" {
				path = ".git/index.lock"
			}
			writeIncomingFile(t, cfg.RootDir, path, "local private data\n")
			if _, err := s.Pull(context.Background(), PullRequest{ExpectedState: review.ExpectedState}); err == nil {
				t.Fatal("collision accepted")
			}
			if b, _ := os.ReadFile(filepath.Join(cfg.RootDir, path)); string(b) != "local private data\n" {
				t.Fatal("local file overwritten")
			}
			if got := strings.TrimSpace(gitOutput(t, cfg.RootDir, "rev-parse", "HEAD")); got != review.HeadCommit {
				t.Fatal("HEAD advanced after collision")
			}
		})
	}
}

func TestIncomingSerializesMutationsAndRedactsDiagnostics(t *testing.T) {
	s, _, _ := incomingFixture(t)
	s.mutationMu.Lock()
	_, err := s.Incoming(context.Background())
	s.mutationMu.Unlock()
	if !errors.Is(err, ErrOperationInProgress) {
		t.Fatalf("not serialized: %v", err)
	}
	for _, diagnostic := range []string{"fatal: unable to access https://user:secret@example.invalid/repo", "fatal: Authentication failed for https://user:secret@example.invalid/repo"} {
		err := safeSyncError([]byte(diagnostic), errors.New("exit status 128"))
		if strings.Contains(err.Error(), "secret") || strings.Contains(err.Error(), "example.invalid") {
			t.Fatal("diagnostic leaked")
		}
	}
}

func TestPullUsesReviewedCommitWithoutDownloadingANewerRemoteTip(t *testing.T) {
	t.Parallel()
	s, laptop, cfg := incomingFixture(t)
	writeIncomingFile(t, laptop, "config/demo/app.conf", "reviewed\n")
	pushIncoming(t, laptop)
	review, err := s.Incoming(context.Background())
	if err != nil {
		t.Fatal(err)
	}
	writeIncomingFile(t, laptop, "config/demo/app.conf", "not reviewed\n")
	pushIncoming(t, laptop)
	if _, err := s.Pull(context.Background(), PullRequest{ExpectedState: review.ExpectedState}); err != nil {
		t.Fatal(err)
	}
	if data, _ := os.ReadFile(filepath.Join(cfg.RootDir, "config/demo/app.conf")); string(data) != "reviewed\n" {
		t.Fatal("download silently used a newer remote commit")
	}
	next, err := s.Incoming(context.Background())
	if err != nil || !next.CanPull || next.BehindCount != 1 {
		t.Fatalf("next update missing: %+v %v", next, err)
	}
}

func TestIncomingBlocksSubmoduleCheckout(t *testing.T) {
	t.Parallel()
	s, laptop, _ := incomingFixture(t)
	commit := strings.TrimSpace(gitOutput(t, laptop, "rev-parse", "HEAD"))
	runGit(t, laptop, "update-index", "--add", "--cacheinfo", "160000,"+commit+",vendor/dependency")
	runGit(t, laptop, "commit", "-m", "Add submodule")
	runGit(t, laptop, "push")
	review, err := s.Incoming(context.Background())
	if err != nil || review.CanPull || review.BlockedReason != "submodules" {
		t.Fatalf("submodule not blocked: %+v %v", review, err)
	}
}
