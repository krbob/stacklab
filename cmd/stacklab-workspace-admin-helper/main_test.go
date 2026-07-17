package main

import (
	"fmt"
	"os"
	"path/filepath"
	"strings"
	"testing"
)

func TestLoadStacklabRootReadsRootOwnedEnvFile(t *testing.T) {
	tempDir := t.TempDir()
	root := filepath.Join(tempDir, "root")
	withStacklabEnv(t, "STACKLAB_ROOT="+root+"\n")

	got, err := loadStacklabRoot()
	if err != nil {
		t.Fatalf("loadStacklabRoot() error = %v", err)
	}

	want, err := filepath.Abs(root)
	if err != nil {
		t.Fatalf("filepath.Abs(%q) error = %v", root, err)
	}
	if got != want {
		t.Fatalf("loadStacklabRoot() = %q, want %q", got, want)
	}
}

func TestLoadStacklabRootIgnoresEnvironmentOverride(t *testing.T) {
	tempDir := t.TempDir()
	root := filepath.Join(tempDir, "root")
	evilRoot := filepath.Join(tempDir, "evil")
	t.Setenv("STACKLAB_ROOT", evilRoot)
	withStacklabEnv(t, "STACKLAB_ROOT="+root+"\n")

	got, err := loadStacklabRoot()
	if err != nil {
		t.Fatalf("loadStacklabRoot() error = %v", err)
	}

	want, err := filepath.Abs(root)
	if err != nil {
		t.Fatalf("filepath.Abs(%q) error = %v", root, err)
	}
	if got != want {
		t.Fatalf("loadStacklabRoot() = %q, want %q", got, want)
	}
}

func TestLoadStacklabRootFallsBackToSystemdEnvironment(t *testing.T) {
	tempDir := t.TempDir()
	root := filepath.Join(tempDir, "root")
	t.Setenv("STACKLAB_ROOT", filepath.Join(tempDir, "evil"))
	withStacklabEnv(t, "STACKLAB_DATA_DIR=/var/lib/stacklab\n")
	restore := replaceSystemctlShow(func(unit string) ([]byte, error) {
		if unit != defaultStacklabUnit {
			t.Fatalf("systemctl unit = %q, want %q", unit, defaultStacklabUnit)
		}
		return []byte("STACKLAB_DATA_DIR=/var/lib/stacklab STACKLAB_ROOT=" + root + "\n"), nil
	})
	defer restore()

	got, err := loadStacklabRoot()
	if err != nil {
		t.Fatalf("loadStacklabRoot() error = %v", err)
	}

	want, err := filepath.Abs(root)
	if err != nil {
		t.Fatalf("filepath.Abs(%q) error = %v", root, err)
	}
	if got != want {
		t.Fatalf("loadStacklabRoot() = %q, want %q", got, want)
	}
}

func TestRunProbeRejectsUnexpectedPositionalArguments(t *testing.T) {
	err := runProbe([]string{"--strategy", "ownership", "extra"})
	if err == nil || !strings.Contains(err.Error(), "unexpected positional") {
		t.Fatalf("runProbe() error = %v, want unexpected positional rejection", err)
	}
}

func TestRunRepairRejectsUnexpectedPositionalArguments(t *testing.T) {
	err := runRepair([]string{"--path", filepath.Join(t.TempDir(), "root", "config", "demo"), "extra"})
	if err == nil || !strings.Contains(err.Error(), "unexpected positional") {
		t.Fatalf("runRepair() error = %v, want unexpected positional rejection", err)
	}
}

func TestRepairIdentityUsesSudoCaller(t *testing.T) {
	withSudoIdentity(t, 104, 111)

	uid, gid, err := repairIdentity()
	if err != nil {
		t.Fatalf("repairIdentity() error = %v", err)
	}
	if uid != 104 || gid != 111 {
		t.Fatalf("repairIdentity() = %d:%d, want 104:111", uid, gid)
	}
}

func TestRepairIdentityUsesEffectiveIdentityWithoutSudo(t *testing.T) {
	withoutEnv(t, "SUDO_UID")
	withoutEnv(t, "SUDO_GID")

	uid, gid, err := repairIdentity()
	if err != nil {
		t.Fatalf("repairIdentity() error = %v", err)
	}
	if uid != os.Geteuid() || gid != os.Getegid() {
		t.Fatalf("repairIdentity() = %d:%d, want %d:%d", uid, gid, os.Geteuid(), os.Getegid())
	}
}

func TestRepairIdentityRejectsInvalidSudoCaller(t *testing.T) {
	for _, value := range []string{"", "invalid", "-1", "4294967295", "4294967296"} {
		t.Run(value, func(t *testing.T) {
			t.Setenv("SUDO_UID", value)
			t.Setenv("SUDO_GID", "111")

			if _, _, err := repairIdentity(); err == nil || !strings.Contains(err.Error(), "invalid SUDO_UID") {
				t.Fatalf("repairIdentity() error = %v, want invalid SUDO_UID", err)
			}
		})
	}
}

func TestRepairIdentityRejectsIncompleteSudoCaller(t *testing.T) {
	t.Run("missing gid", func(t *testing.T) {
		t.Setenv("SUDO_UID", "104")
		withoutEnv(t, "SUDO_GID")
		if _, _, err := repairIdentity(); err == nil || !strings.Contains(err.Error(), "incomplete") {
			t.Fatalf("repairIdentity() error = %v, want incomplete sudo identity", err)
		}
	})
	t.Run("missing uid", func(t *testing.T) {
		withoutEnv(t, "SUDO_UID")
		t.Setenv("SUDO_GID", "111")
		if _, _, err := repairIdentity(); err == nil || !strings.Contains(err.Error(), "incomplete") {
			t.Fatalf("repairIdentity() error = %v, want incomplete sudo identity", err)
		}
	})
}

func TestRunRepairACLGrantsReadWriteWithoutExecuteToRegularFile(t *testing.T) {
	tempDir := t.TempDir()
	root := filepath.Join(tempDir, "root")
	stackRoot := filepath.Join(root, "stacks", "demo")
	if err := os.MkdirAll(stackRoot, 0o755); err != nil {
		t.Fatalf("MkdirAll(stackRoot) error = %v", err)
	}
	targetPath := filepath.Join(stackRoot, "secret.txt")
	if err := os.WriteFile(targetPath, []byte("secret\n"), 0o400); err != nil {
		t.Fatalf("WriteFile(target) error = %v", err)
	}

	withStacklabEnv(t, "STACKLAB_ROOT="+root+"\n")
	withSudoIdentity(t, 4242, 4343)
	var calls [][]string
	restore := replaceACLCommand(func(name string, args ...string) ([]byte, error) {
		if name != "setfacl" {
			t.Fatalf("ACL command name = %q, want setfacl", name)
		}
		calls = append(calls, append([]string(nil), args...))
		return nil, nil
	})
	defer restore()

	if err := runRepair([]string{"--path", targetPath, "--strategy", "acl"}); err != nil {
		t.Fatalf("runRepair() error = %v", err)
	}

	if len(calls) != 1 {
		t.Fatalf("setfacl calls = %#v, want one call", calls)
	}
	resolvedTarget, err := filepath.EvalSymlinks(targetPath)
	if err != nil {
		t.Fatalf("EvalSymlinks(target) error = %v", err)
	}
	if !hasExactACL(calls[0], "u:4242:rw-") || !hasExactACL(calls[0], "m::rw-") || calls[0][len(calls[0])-1] != resolvedTarget {
		t.Fatalf("unexpected setfacl args: %#v", calls[0])
	}
}

func TestRunRepairACLGrantsExecuteToExecutableFile(t *testing.T) {
	tempDir := t.TempDir()
	root := filepath.Join(tempDir, "root")
	stackRoot := filepath.Join(root, "stacks", "demo")
	if err := os.MkdirAll(stackRoot, 0o755); err != nil {
		t.Fatalf("MkdirAll(stackRoot) error = %v", err)
	}
	targetPath := filepath.Join(stackRoot, "run.sh")
	if err := os.WriteFile(targetPath, []byte("#!/bin/sh\n"), 0o700); err != nil {
		t.Fatalf("WriteFile(target) error = %v", err)
	}

	withStacklabEnv(t, "STACKLAB_ROOT="+root+"\n")
	withSudoIdentity(t, 4242, 4343)
	var calls [][]string
	restore := replaceACLCommand(func(name string, args ...string) ([]byte, error) {
		calls = append(calls, append([]string(nil), args...))
		return nil, nil
	})
	defer restore()

	if err := runRepair([]string{"--path", targetPath, "--strategy", "acl"}); err != nil {
		t.Fatalf("runRepair() error = %v", err)
	}

	if len(calls) != 1 || !hasExactACL(calls[0], "u:4242:rwx") || !hasExactACL(calls[0], "m::rwx") {
		t.Fatalf("unexpected setfacl args: %#v", calls)
	}
}

func TestRunRepairACLRecursiveAddsDefaultACLForDirectories(t *testing.T) {
	tempDir := t.TempDir()
	root := filepath.Join(tempDir, "root")
	targetDir := filepath.Join(root, "config", "demo")
	childDir := filepath.Join(targetDir, "nested")
	if err := os.MkdirAll(childDir, 0o755); err != nil {
		t.Fatalf("MkdirAll(childDir) error = %v", err)
	}
	if err := os.WriteFile(filepath.Join(childDir, "app.conf"), []byte("PORT=8080\n"), 0o600); err != nil {
		t.Fatalf("WriteFile(app.conf) error = %v", err)
	}

	withStacklabEnv(t, "STACKLAB_ROOT="+root+"\n")
	withSudoIdentity(t, 4242, 4343)
	var calls [][]string
	restore := replaceACLCommand(func(name string, args ...string) ([]byte, error) {
		calls = append(calls, append([]string(nil), args...))
		return nil, nil
	})
	defer restore()

	if err := runRepair([]string{"--path", targetDir, "--strategy", "acl", "--recursive"}); err != nil {
		t.Fatalf("runRepair() error = %v", err)
	}

	if len(calls) != 3 {
		t.Fatalf("setfacl calls = %#v, want target dir, child dir, and file", calls)
	}
	if !hasDefaultACL(calls[0]) || !hasDefaultACL(calls[1]) {
		t.Fatalf("directory calls missing default ACL: %#v", calls)
	}
	if !hasExactACL(calls[0], "u:4242:rwx") || !hasExactACL(calls[1], "u:4242:rwx") {
		t.Fatalf("directory calls missing access ACL: %#v", calls)
	}
	if !hasExactACL(calls[0], "m::rwx") || !hasExactACL(calls[1], "m::rwx") {
		t.Fatalf("directory calls missing access ACL mask: %#v", calls)
	}
	if !hasExactACL(calls[0], "d:m::rwx") || !hasExactACL(calls[1], "d:m::rwx") {
		t.Fatalf("directory calls missing default ACL mask: %#v", calls)
	}
	if hasDefaultACL(calls[2]) {
		t.Fatalf("file call unexpectedly has default ACL: %#v", calls[2])
	}
	if !hasExactACL(calls[2], "u:4242:rw-") || !hasExactACL(calls[2], "m::rw-") {
		t.Fatalf("file call has unexpected access ACL: %#v", calls[2])
	}
}

func replaceACLCommand(replacement func(string, ...string) ([]byte, error)) func() {
	original := runACLCommand
	runACLCommand = replacement
	return func() {
		runACLCommand = original
	}
}

func replaceSystemctlShow(replacement func(string) ([]byte, error)) func() {
	original := runSystemctlShow
	runSystemctlShow = replacement
	return func() {
		runSystemctlShow = original
	}
}

func withStacklabEnv(t *testing.T, content string) {
	t.Helper()

	tempDir := t.TempDir()
	envPath := filepath.Join(tempDir, "stacklab.env")
	if err := os.WriteFile(envPath, []byte(content), 0o600); err != nil {
		t.Fatalf("WriteFile(stacklab.env) error = %v", err)
	}

	original := stacklabEnvFilePath
	stacklabEnvFilePath = envPath
	t.Cleanup(func() {
		stacklabEnvFilePath = original
	})
}

func withSudoIdentity(t *testing.T, uid, gid int) {
	t.Helper()
	t.Setenv("SUDO_UID", fmt.Sprintf("%d", uid))
	t.Setenv("SUDO_GID", fmt.Sprintf("%d", gid))
}

func withoutEnv(t *testing.T, name string) {
	t.Helper()
	value, exists := os.LookupEnv(name)
	if err := os.Unsetenv(name); err != nil {
		t.Fatalf("Unsetenv(%s) error = %v", name, err)
	}
	t.Cleanup(func() {
		if exists {
			_ = os.Setenv(name, value)
			return
		}
		_ = os.Unsetenv(name)
	})
}

func hasDefaultACL(args []string) bool {
	return hasACLEntry(args, "d:u:", ":rwx")
}

func hasACLEntry(args []string, prefix, suffix string) bool {
	for _, arg := range args {
		if strings.HasPrefix(arg, prefix) && strings.HasSuffix(arg, suffix) {
			return true
		}
	}
	return false
}

func hasExactACL(args []string, value string) bool {
	for _, arg := range args {
		if arg == value {
			return true
		}
	}
	return false
}
