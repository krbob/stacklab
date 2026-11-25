//go:build !linux

package main

import "errors"

func repairRemovalDirectories(root, target, strategy string, uid, gid int, result *repairResult) error {
	return errors.New("stack removal permission repair requires Linux")
}
