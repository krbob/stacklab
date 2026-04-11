package workspacerepair

import (
	"context"
	"encoding/json"
	"errors"
	"fmt"
	"os"
	"os/exec"
	"strings"
	"time"

	"stacklab/internal/config"
	"stacklab/internal/fsmeta"
)

const unsupportedMessage = "Workspace permission repair is not configured yet."

var ErrUnsupported = errors.New("workspace permission repair is not supported")

type commandRunner func(ctx context.Context, name string, args ...string) ([]byte, error)

type Service struct {
	helperPath string
	useSudo    bool
	runCommand commandRunner
}

func NewService(cfg config.Config) *Service {
	return &Service{
		helperPath: strings.TrimSpace(cfg.WorkspaceAdminHelperPath),
		useSudo:    cfg.WorkspaceAdminUseSudo,
		runCommand: defaultCommandRunner,
	}
}

func defaultCommandRunner(ctx context.Context, name string, args ...string) ([]byte, error) {
	return exec.CommandContext(ctx, name, args...).CombinedOutput()
}

func (s *Service) Capability(ctx context.Context) Capability {
	response := Capability{
		Supported: false,
		Recursive: true,
	}
	if s.helperPath == "" {
		reason := unsupportedMessage
		response.Reason = &reason
		return response
	}
	if info, err := os.Stat(s.helperPath); err != nil || info.IsDir() {
		reason := fmt.Sprintf("Workspace repair helper is unavailable at %s.", s.helperPath)
		response.Reason = &reason
		return response
	}
	if !s.useSudo && os.Geteuid() != 0 {
		reason := "Workspace repair helper requires sudo or a root-owned Stacklab service."
		response.Reason = &reason
		return response
	}
	if s.useSudo {
		probeCtx, cancel := context.WithTimeout(ctx, 2*time.Second)
		defer cancel()
		output, err := s.runHelperCommand(probeCtx, "probe")
		if err != nil {
			message := strings.TrimSpace(string(output))
			lower := strings.ToLower(message)
			switch {
			case strings.Contains(lower, "no new privileges"):
				reason := "Workspace repair helper requires NoNewPrivileges=false in stacklab.service."
				response.Reason = &reason
				return response
			case strings.Contains(lower, "a password is required"),
				strings.Contains(lower, "not allowed to execute"),
				strings.Contains(lower, "may not run sudo"):
				reason := "Workspace repair helper sudoers is not configured correctly."
				response.Reason = &reason
				return response
			default:
				reason := "Workspace repair helper could not be executed successfully."
				if message != "" {
					reason = message
				}
				response.Reason = &reason
				return response
			}
		}
	}
	response.Supported = true
	return response
}

func (s *Service) Repair(ctx context.Context, targetPath string, recursive bool) (Result, error) {
	capability := s.Capability(ctx)
	if !capability.Supported {
		return Result{}, ErrUnsupported
	}

	infoBefore, err := os.Stat(targetPath)
	if err != nil {
		return Result{}, fmt.Errorf("stat repair target before repair: %w", err)
	}
	before := fsmeta.Inspect(targetPath, infoBefore)

	args := []string{"repair", "--path", targetPath}
	if recursive {
		args = append(args, "--recursive")
	}

	output, runErr := s.runHelperCommand(ctx, args...)
	result, parseErr := parseRepairOutput(output)
	if parseErr != nil {
		if runErr != nil {
			return Result{}, fmt.Errorf("workspace repair helper failed: %w: %s", runErr, strings.TrimSpace(string(output)))
		}
		return Result{}, parseErr
	}

	infoAfter, err := os.Stat(targetPath)
	if err != nil {
		return Result{}, fmt.Errorf("stat repair target after repair: %w", err)
	}
	result.TargetPermissionsBefore = before
	result.TargetPermissionsAfter = fsmeta.Inspect(targetPath, infoAfter)

	if runErr != nil {
		return result, fmt.Errorf("workspace repair helper failed: %w", runErr)
	}
	return result, nil
}

func (s *Service) runHelperCommand(ctx context.Context, args ...string) ([]byte, error) {
	if s.useSudo {
		sudoArgs := append([]string{"-n", "--", s.helperPath}, args...)
		return s.runCommand(ctx, "sudo", sudoArgs...)
	}
	return s.runCommand(ctx, s.helperPath, args...)
}

type helperOutput struct {
	ChangedItems int      `json:"changed_items"`
	Warnings     []string `json:"warnings"`
}

func parseRepairOutput(output []byte) (Result, error) {
	trimmed := strings.TrimSpace(string(output))
	if trimmed == "" {
		return Result{}, errors.New("workspace repair helper produced empty output")
	}

	var decoded helperOutput
	if err := json.Unmarshal([]byte(trimmed), &decoded); err != nil {
		lines := strings.Split(trimmed, "\n")
		for i := len(lines) - 1; i >= 0; i-- {
			line := strings.TrimSpace(lines[i])
			if line == "" {
				continue
			}
			var candidate helperOutput
			if decodeErr := json.Unmarshal([]byte(line), &candidate); decodeErr == nil {
				decoded = candidate
				err = nil
				break
			}
		}
		if err != nil {
			return Result{}, fmt.Errorf("parse workspace repair helper output: %w", err)
		}
	}

	return Result{
		ChangedItems: decoded.ChangedItems,
		Warnings:     append([]string(nil), decoded.Warnings...),
	}, nil
}
