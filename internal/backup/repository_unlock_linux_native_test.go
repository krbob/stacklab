//go:build linux

package backup

import (
	"context"
	"encoding/json"
	"errors"
	"fmt"
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
	testNativeUnlockExternalOwner(t, false)
}

func TestNativeUnlockRefusesFreshPIDNamespaceOwner(t *testing.T) {
	testNativeUnlockExternalOwner(t, true)
}

func testNativeUnlockExternalOwner(t *testing.T, privatePID bool) {
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
	args := []string{"run", "--rm", "-i", "--name", name, "--label", "stacklab.fixture=foreign-uid-unlock", "--uts=host", "--network=none", "-v", "/:/host"}
	if privatePID {
		// The high PID is local to the container's private PID namespace. Keep
		// the host's hostname, exactly the misleading-identity case under review.
		args = append(args, "--privileged", "debian:trixie-slim", "sh", "-ec", `echo 1000000 > /proc/sys/kernel/ns_last_pid; "$@"`, "--")
	} else {
		args = append(args, "--pid=host", "debian:trixie-slim")
	}
	args = append(args, "chroot", "/host", "/usr/bin/restic", "--repo", d.Repository, "--password-file", filepath.Join(s.secretDirectory(d.ID), "password"), "backup", "--stdin", "--stdin-filename", "held-input")
	cmd := exec.CommandContext(ctx, "docker", args...)
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
	waited := false
	defer func() {
		cleanup, stop := context.WithTimeout(context.Background(), 15*time.Second)
		defer stop()
		_ = exec.CommandContext(cleanup, "docker", "rm", "-f", name).Run()
		if !waited {
			_ = cmd.Wait()
		}
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
	if privatePID {
		if lock.PID <= 1000000 || !errors.Is(syscall.Kill(lock.PID, 0), syscall.ESRCH) {
			t.Fatalf("private PID is not absent on host: %+v", lock)
		}
		if err := exec.CommandContext(ctx, "docker", "exec", name, "test", "-e", fmt.Sprintf("/proc/%d/status", lock.PID)).Run(); err != nil {
			t.Fatal("private PID owner is not alive")
		}
	} else if lock.UID != 0 || !errors.Is(syscall.Kill(lock.PID, 0), syscall.EPERM) {
		t.Fatalf("fixture is not a live foreign UID owner: %+v", lock)
	}
	job, err := s.StartRepositoryJob(ctx, d.ID, "backup_unlock", "fixture")
	if err != nil {
		t.Fatal(err)
	}
	_, err = s.ExecuteRepositoryJob(ctx, job, d.ID, "backup_unlock", CheckRequest{}, RestoreRequest{})
	var diagnostic *repositoryCommandError
	reason := "still exists"
	if privatePID {
		reason = "30 minutes"
	}
	if !errors.As(err, &diagnostic) || diagnostic.Code != "unlock_refused" || !strings.Contains(err.Error(), reason) {
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
	// Reap the actual docker/container process, not the namespace-local PID
	// which was deliberately absent on the host even while the owner lived.
	_ = cmd.Wait()
	waited = true
	// The owner has exited; simulate the additional minimum age without changing
	// repository bytes or waiting thirty minutes in the integration test.
	s.now = func() time.Time { return time.Now().UTC().Add(minimumUnlockAge + time.Minute) }
	job, err = s.StartRepositoryJob(ctx, d.ID, "backup_unlock", "fixture")
	if err != nil {
		t.Fatal(err)
	}
	if _, err = s.ExecuteRepositoryJob(ctx, job, d.ID, "backup_unlock", CheckRequest{}, RestoreRequest{}); err != nil {
		t.Fatal(err)
	}
	s.now = func() time.Time { return time.Now().UTC() }
	after, err = s.repositoryLockIDs(ctx, d)
	if err != nil || len(after) != 0 {
		t.Fatalf("dead owner's lock remains: %v %v", after, err)
	}
	t.Logf("live external owner (private PID namespace=%t) refused; killed owner's lock removed after simulated 31-minute age", privatePID)
}
