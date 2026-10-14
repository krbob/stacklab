package main

import (
	"encoding/json"
	"errors"
	"os"
	"path/filepath"

	"stacklab/internal/backup"
)

func validateRestoreScope(target, mode, sourceScope string, roots []string) error {
	if sourceScope != "complete" && sourceScope != "filtered" {
		return errors.New("recovery input has no supported capture scope; prepare it again with the current service and helper")
	}
	if mode != "replace" && mode != "merge" {
		return errors.New("restore mode must be replace or merge")
	}
	if mode != "replace" {
		return nil
	}
	if sourceScope != "complete" {
		return errors.New("exact replacement of a filtered source would delete excluded live data; use explicit merge or offline recovery")
	}
	for _, root := range roots {
		if beneath(filepath.Clean(target), filepath.Clean(root)) {
			return errors.New("exact replacement cannot target a managed or configured backup root or its ancestor")
		}
	}
	return nil
}

// Each privileged action is probed separately: permission for one sudo command
// does not establish permission for the remaining recovery protocol.
func probeRecoveryAction(action string) error {
	switch action {
	case "prepare-restore", "check-restore", "check-restores", "apply-restore", "release-restore", "check-target":
	default:
		return errors.New("unsupported recovery probe action")
	}
	if os.Geteuid() != 0 {
		return errors.New("recovery helper requires root privileges; check the configured sudoers rule")
	}
	if _, _, err := trustedRoots(); err != nil {
		return err
	}
	// Readiness checks must not create/chmod the spool or expire prepared work.
	if err := validateRestoreSpool(false); err != nil {
		return err
	}
	return json.NewEncoder(os.Stdout).Encode(struct {
		Protocol int    `json:"protocol"`
		Action   string `json:"action"`
	}{backup.RecoveryHelperProtocol, action})
}
