package main

import (
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

func TestRunRepairACLGrantsAccessWithoutChangingMode(t *testing.T) {
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
	before, err := os.Stat(targetPath)
	if err != nil {
		t.Fatalf("Stat(target before) error = %v", err)
	}

	withStacklabEnv(t, "STACKLAB_ROOT="+root+"\n")
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

	after, err := os.Stat(targetPath)
	if err != nil {
		t.Fatalf("Stat(target after) error = %v", err)
	}
	if before.Mode().Perm() != after.Mode().Perm() {
		t.Fatalf("mode changed from %v to %v", before.Mode().Perm(), after.Mode().Perm())
	}
	if len(calls) != 1 {
		t.Fatalf("setfacl calls = %#v, want one call", calls)
	}
	resolvedTarget, err := filepath.EvalSymlinks(targetPath)
	if err != nil {
		t.Fatalf("EvalSymlinks(target) error = %v", err)
	}
	if !hasACLEntry(calls[0], "u:", ":rwX") || !hasExactACL(calls[0], "m::rwX") || calls[0][len(calls[0])-1] != resolvedTarget {
		t.Fatalf("unexpected setfacl args: %#v", calls[0])
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
	if !hasExactACL(calls[0], "d:m::rwx") || !hasExactACL(calls[1], "d:m::rwx") {
		t.Fatalf("directory calls missing default ACL mask: %#v", calls)
	}
	if hasDefaultACL(calls[2]) {
		t.Fatalf("file call unexpectedly has default ACL: %#v", calls[2])
	}
	if !hasExactACL(calls[2], "m::rwX") {
		t.Fatalf("file call missing ACL mask: %#v", calls[2])
	}
}

func replaceACLCommand(replacement func(string, ...string) ([]byte, error)) func() {
	original := runACLCommand
	runACLCommand = replacement
	return func() {
		runACLCommand = original
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

func hasDefaultACL(args []string) bool {
	return hasACLEntry(args, "d:u:", ":rwX")
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
