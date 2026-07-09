package workspacerepair

import (
	"context"
	"errors"
	"os"
	"path/filepath"
	"strings"
	"testing"

	"stacklab/internal/config"
)

func TestCapabilityUnsupportedWithoutHelper(t *testing.T) {
	t.Parallel()

	service := NewService(config.Config{})
	response := service.Capability(context.Background())
	if response.Supported {
		t.Fatalf("expected unsupported capability, got %#v", response)
	}
	if response.Reason == nil || !strings.Contains(*response.Reason, "not configured") {
		t.Fatalf("unexpected capability reason: %#v", response)
	}
}

func TestCapabilityWithSudoProbeSuccess(t *testing.T) {
	t.Parallel()

	tempDir := t.TempDir()
	helperPath := filepath.Join(tempDir, "helper")
	if err := os.WriteFile(helperPath, []byte("#!/bin/sh\n"), 0o755); err != nil {
		t.Fatalf("WriteFile(helper) error = %v", err)
	}

	service := NewService(config.Config{
		WorkspaceAdminHelperPath: helperPath,
		WorkspaceAdminUseSudo:    true,
	})
	service.runCommand = func(ctx context.Context, name string, args ...string) ([]byte, error) {
		if name != "sudo" {
			t.Fatalf("runCommand name = %q, want sudo", name)
		}
		if len(args) < 4 || args[0] != "-n" || args[1] != "--" || args[2] != helperPath {
			t.Fatalf("unexpected sudo args: %#v", args)
		}
		for _, arg := range args {
			if strings.Contains(arg, "STACKLAB_ROOT") {
				t.Fatalf("sudo args must not preserve STACKLAB_ROOT: %#v", args)
			}
		}
		return []byte(`{"changed_items":0}`), nil
	}

	response := service.Capability(context.Background())
	if !response.Supported {
		t.Fatalf("expected supported capability, got %#v", response)
	}
}

func TestCapabilityCachesSudoProbeAcrossRepairFlow(t *testing.T) {
	t.Parallel()

	tempDir := t.TempDir()
	targetPath := filepath.Join(tempDir, "demo.conf")
	if err := os.WriteFile(targetPath, []byte("hello\n"), 0o600); err != nil {
		t.Fatalf("WriteFile(target) error = %v", err)
	}
	helperPath := filepath.Join(tempDir, "helper")
	if err := os.WriteFile(helperPath, []byte("#!/bin/sh\n"), 0o755); err != nil {
		t.Fatalf("WriteFile(helper) error = %v", err)
	}

	service := NewService(config.Config{
		WorkspaceAdminHelperPath:     helperPath,
		WorkspaceAdminUseSudo:        true,
		WorkspaceAdminRepairStrategy: "acl",
	})
	var probes int
	var repairs int
	service.runCommand = func(ctx context.Context, name string, args ...string) ([]byte, error) {
		if name != "sudo" {
			t.Fatalf("runCommand name = %q, want sudo", name)
		}
		if len(args) < 4 {
			t.Fatalf("unexpected sudo args: %#v", args)
		}
		switch args[3] {
		case "probe":
			probes++
			return []byte(`{"changed_items":0}`), nil
		case "repair":
			repairs++
			return []byte(`{"changed_items":1}`), nil
		default:
			t.Fatalf("unexpected helper command: %#v", args)
			return nil, nil
		}
	}

	if capability := service.Capability(context.Background()); !capability.Supported {
		t.Fatalf("expected supported capability, got %#v", capability)
	}
	if _, err := service.Repair(context.Background(), targetPath, false); err != nil {
		t.Fatalf("Repair() error = %v", err)
	}
	if capability := service.Capability(context.Background()); !capability.Supported {
		t.Fatalf("expected supported capability after repair, got %#v", capability)
	}

	if probes != 1 {
		t.Fatalf("probe calls = %d, want 1", probes)
	}
	if repairs != 1 {
		t.Fatalf("repair calls = %d, want 1", repairs)
	}
}

func TestCapabilityExplainsNoNewPrivileges(t *testing.T) {
	t.Parallel()

	tempDir := t.TempDir()
	helperPath := filepath.Join(tempDir, "helper")
	if err := os.WriteFile(helperPath, []byte("#!/bin/sh\n"), 0o755); err != nil {
		t.Fatalf("WriteFile(helper) error = %v", err)
	}

	service := NewService(config.Config{
		WorkspaceAdminHelperPath: helperPath,
		WorkspaceAdminUseSudo:    true,
	})
	service.runCommand = func(ctx context.Context, name string, args ...string) ([]byte, error) {
		return []byte(`sudo: The "no new privileges" flag is set, which prevents sudo from running as root.`), os.ErrPermission
	}

	response := service.Capability(context.Background())
	if response.Supported {
		t.Fatalf("expected unsupported capability, got %#v", response)
	}
	if response.Reason == nil || !strings.Contains(*response.Reason, "NoNewPrivileges=false") {
		t.Fatalf("unexpected capability reason: %#v", response)
	}
}

func TestRepairReturnsBeforeAndAfterPermissions(t *testing.T) {
	t.Parallel()

	tempDir := t.TempDir()
	targetPath := filepath.Join(tempDir, "demo.conf")
	if err := os.WriteFile(targetPath, []byte("hello\n"), 0o600); err != nil {
		t.Fatalf("WriteFile(target) error = %v", err)
	}
	helperPath := filepath.Join(tempDir, "helper")
	if err := os.WriteFile(helperPath, []byte("#!/bin/sh\n"), 0o755); err != nil {
		t.Fatalf("WriteFile(helper) error = %v", err)
	}

	service := NewService(config.Config{
		WorkspaceAdminHelperPath: helperPath,
		WorkspaceAdminUseSudo:    true,
	})
	service.runCommand = func(ctx context.Context, name string, args ...string) ([]byte, error) {
		if len(args) >= 4 && args[3] == "probe" {
			return []byte(`{"changed_items":0}`), nil
		}
		if err := os.Chmod(targetPath, 0o644); err != nil {
			t.Fatalf("Chmod(target) error = %v", err)
		}
		return []byte(`{"changed_items":1,"warnings":["updated mode"]}`), nil
	}

	result, err := service.Repair(context.Background(), targetPath, false)
	if err != nil {
		t.Fatalf("Repair() error = %v", err)
	}
	if result.ChangedItems != 1 {
		t.Fatalf("ChangedItems = %d, want 1", result.ChangedItems)
	}
	if result.TargetPermissionsBefore.Mode != "0600" || result.TargetPermissionsAfter.Mode != "0644" {
		t.Fatalf("unexpected permissions transition: before=%#v after=%#v", result.TargetPermissionsBefore, result.TargetPermissionsAfter)
	}
}

func TestRepairPassesACLStrategyToHelper(t *testing.T) {
	t.Parallel()

	tempDir := t.TempDir()
	targetPath := filepath.Join(tempDir, "demo.conf")
	if err := os.WriteFile(targetPath, []byte("hello\n"), 0o600); err != nil {
		t.Fatalf("WriteFile(target) error = %v", err)
	}
	helperPath := filepath.Join(tempDir, "helper")
	if err := os.WriteFile(helperPath, []byte("#!/bin/sh\n"), 0o755); err != nil {
		t.Fatalf("WriteFile(helper) error = %v", err)
	}

	service := NewService(config.Config{
		WorkspaceAdminHelperPath:     helperPath,
		WorkspaceAdminUseSudo:        true,
		WorkspaceAdminRepairStrategy: "acl",
	})
	service.runCommand = func(ctx context.Context, name string, args ...string) ([]byte, error) {
		if len(args) >= 4 && args[3] == "probe" {
			return []byte(`{"changed_items":0}`), nil
		}
		joined := strings.Join(args, " ")
		if !strings.Contains(joined, " --strategy acl") {
			t.Fatalf("repair command did not include ACL strategy: %#v", args)
		}
		return []byte(`{"changed_items":1}`), nil
	}

	if _, err := service.Repair(context.Background(), targetPath, false); err != nil {
		t.Fatalf("Repair() error = %v", err)
	}
}

func TestParseRepairOutputFromLastJSONLine(t *testing.T) {
	t.Parallel()

	result, err := parseRepairOutput([]byte("warning line\n{\"changed_items\":2,\"warnings\":[\"one\"]}\n"))
	if err != nil {
		t.Fatalf("parseRepairOutput() error = %v", err)
	}
	if result.ChangedItems != 2 || len(result.Warnings) != 1 {
		t.Fatalf("unexpected parsed result: %#v", result)
	}
}

func TestRepairUnsupportedWhenCapabilityIsDisabled(t *testing.T) {
	t.Parallel()

	service := NewService(config.Config{})
	_, err := service.Repair(context.Background(), "/tmp/missing", false)
	if !errors.Is(err, ErrUnsupported) {
		t.Fatalf("Repair() error = %v, want ErrUnsupported", err)
	}
}
