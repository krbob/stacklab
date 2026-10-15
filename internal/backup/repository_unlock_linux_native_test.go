//go:build linux

package backup

import (
	"context"
	"encoding/json"
	"errors"
	"io"
	"os"
	"os/exec"
	"path/filepath"
	"reflect"
	"strings"
	"syscall"
	"testing"
	"time"
)

// Docker creates a root-owned restic process in the host PID/UTS namespaces.
// Stacklab still executes as the non-root test user: signal 0 must return EPERM.
// This fixture owns its repository and never changes the installed service.
func TestNativeUnlockRefusesForeignUIDOwner(t *testing.T) {
	if os.Getenv("STACKLAB_TEST_FOREIGN_UID_UNLOCK") != "1" {
		t.Skip("set STACKLAB_TEST_FOREIGN_UID_UNLOCK=1 on disposable Linux with Docker")
	}
	if os.Getuid() == 0 {
		t.Fatal("fixture must run as the non-root service/test user")
	}
	ctx, cancel := context.WithTimeout(context.Background(), 60*time.Second)
	defer cancel()
	s, _, _ := newTestService(t)
	s.runner = nativeRunner{}
	d := addDestination(t, s, "Foreign UID")
	if _, err := s.restic(ctx, d, "", "init"); err != nil {
		t.Fatal(err)
	}
	name := "stacklab-root-lock-" + newID("")
	cmd := exec.CommandContext(ctx, "docker", "run", "--rm", "-i", "--name", name, "--label", "stacklab.fixture=foreign-uid-unlock", "--pid=host", "--uts=host", "--network=none", "-v", "/:/host", "debian:trixie-slim", "chroot", "/host", "/usr/bin/restic", "--repo", d.Repository, "--password-file", filepath.Join(s.secretDirectory(d.ID), "password"), "backup", "--stdin", "--stdin-filename", "held-input")
	stdin, err := cmd.StdinPipe()
	if err != nil {
		t.Fatal(err)
	}
	defer stdin.Close()
	cmd.Stdout = io.Discard
	cmd.Stderr = io.Discard
	if err := cmd.Start(); err != nil {
		t.Fatal(err)
	}
	defer func() {
		cleanup, stop := context.WithTimeout(context.Background(), 15*time.Second)
		defer stop()
		_ = exec.CommandContext(cleanup, "docker", "rm", "-f", name).Run()
		_ = cmd.Wait()
	}()
	var ids []string
	deadline := time.Now().Add(15 * time.Second)
	for time.Now().Before(deadline) {
		entries, err := os.ReadDir(filepath.Join(d.Repository, "locks"))
		if err != nil {
			t.Fatal(err)
		}
		for _, e := range entries {
			if len(e.Name()) == 64 {
				ids = append(ids, e.Name())
			}
		}
		if len(ids) > 0 {
			break
		}
		time.Sleep(30 * time.Millisecond)
	}
	if len(ids) != 1 {
		t.Fatalf("root process did not hold exactly one lock: %v", ids)
	}
	// Root-created lock files are 0600. Grant the fixture user read access only,
	// so this exercises EPERM on the process rather than unreadable metadata.
	output, err := exec.CommandContext(ctx, "docker", "exec", name, "chroot", "/host", "chmod", "0644", filepath.Join(d.Repository, "locks", ids[0])).CombinedOutput()
	if err != nil {
		t.Fatalf("fixture lock readability: %v %s", err, output)
	}
	output, err = s.restic(ctx, d, "", "cat", "lock", ids[0])
	if err != nil {
		t.Fatal(err)
	}
	var lock repositoryLock
	if err := json.Unmarshal(output, &lock); err != nil {
		t.Fatal(err)
	}
	if lock.UID != 0 || !errors.Is(syscall.Kill(lock.PID, 0), syscall.EPERM) {
		t.Fatalf("fixture is not a live foreign UID owner: %+v", lock)
	}
	job, err := s.StartRepositoryJob(ctx, d.ID, "backup_unlock", "fixture")
	if err != nil {
		t.Fatal(err)
	}
	_, err = s.ExecuteRepositoryJob(ctx, job, d.ID, "backup_unlock", CheckRequest{}, RestoreRequest{})
	var diagnostic *repositoryCommandError
	if !errors.As(err, &diagnostic) || diagnostic.Code != "repository_locked" || !strings.Contains(err.Error(), "still exists") {
		t.Fatalf("foreign owner not protected: %v", err)
	}
	after, err := s.repositoryLockIDs(ctx, d)
	if err != nil || !reflect.DeepEqual(ids, after) {
		t.Fatalf("active root lock changed: %v %v", after, err)
	}
	// Once the owning process really exits, the exact same lock is removable.
	if output, err := exec.CommandContext(ctx, "docker", "kill", "--signal", "KILL", name).CombinedOutput(); err != nil {
		t.Fatalf("fixture owner kill: %v %s", err, output)
	}
	deadline = time.Now().Add(5 * time.Second)
	for time.Now().Before(deadline) && !errors.Is(syscall.Kill(lock.PID, 0), syscall.ESRCH) {
		time.Sleep(20 * time.Millisecond)
	}
	job, err = s.StartRepositoryJob(ctx, d.ID, "backup_unlock", "fixture")
	if err != nil {
		t.Fatal(err)
	}
	if _, err = s.ExecuteRepositoryJob(ctx, job, d.ID, "backup_unlock", CheckRequest{}, RestoreRequest{}); err != nil {
		t.Fatal(err)
	}
	after, err = s.repositoryLockIDs(ctx, d)
	if err != nil || len(after) != 0 {
		t.Fatalf("dead owner's lock remains: %v %v", after, err)
	}
	t.Log("live UID 0 owner was refused on EPERM; killed owner's stale lock was removed")
}
