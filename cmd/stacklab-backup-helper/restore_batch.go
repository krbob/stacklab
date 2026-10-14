package main

import (
	"context"
	"encoding/json"
	"errors"
	"flag"
	"fmt"
	"os"
	"path/filepath"
	"syscall"

	"golang.org/x/sys/unix"
)

// Batch capacity checks include all selected targets sharing a filesystem and
// the largest transient extraction member list on the immutable spool disk.
// This is a conservative admission estimate, not a filesystem reservation:
// each helper also rechecks capacity immediately before its own mutation.
func checkPreparedRestores(ctx context.Context, args []string) error {
	flags := flag.NewFlagSet("check-restores", flag.ContinueOnError)
	var tokens repeatedFlag
	flags.Var(&tokens, "token", "prepared recovery token")
	if err := flags.Parse(args); err != nil {
		return err
	}
	if flags.NArg() != 0 || len(tokens) == 0 || len(tokens) > 1000 {
		return errors.New("check-restores requires between 1 and 1000 prepared tokens")
	}
	type capacity struct{ required, available uint64 }
	volumes := map[uint64]capacity{}
	add := func(path string, size uint64) error {
		var info os.FileInfo
		var err error
		for {
			info, err = os.Stat(path)
			if err == nil {
				break
			}
			if !errors.Is(err, os.ErrNotExist) {
				return err
			}
			parent := filepath.Dir(path)
			if parent == path {
				return err
			}
			path = parent
		}
		stat, ok := info.Sys().(*syscall.Stat_t)
		if !ok {
			return errors.New("cannot identify recovery filesystem")
		}
		var space unix.Statfs_t
		if err = unix.Statfs(path, &space); err != nil {
			return err
		}
		device := uint64(stat.Dev)
		volume := volumes[device]
		volume.available = uint64(space.Bavail) * uint64(space.Bsize)
		volume.required += size
		volumes[device] = volume
		return nil
	}
	var largest uint64
	seen := map[string]bool{}
	for _, token := range tokens {
		if seen[token] {
			return errors.New("duplicate prepared recovery token")
		}
		seen[token] = true
		if err := runPreparedRestore(ctx, "check-restore", []string{"--token", token}); err != nil {
			return err
		}
		directory, err := preparedPath(token)
		if err != nil {
			return err
		}
		data, err := os.ReadFile(filepath.Join(directory, "state.json"))
		if err != nil {
			return err
		}
		var state preparedRestore
		if err = json.Unmarshal(data, &state); err != nil {
			return err
		}
		archive, err := os.Stat(filepath.Join(directory, "source.tar"))
		if err != nil {
			return err
		}
		size := uint64(archive.Size())
		if size > largest {
			largest = size
		}
		if err = add(state.Target, size); err != nil {
			return err
		}
	}
	if err := add(restoreSpoolRoot, largest); err != nil {
		return err
	}
	for _, volume := range volumes {
		if volume.available < 128<<20 || volume.required > volume.available-(128<<20) {
			return fmt.Errorf("insufficient shared filesystem capacity for all recovery mappings: require %d bytes plus a 128 MiB reserve", volume.required)
		}
	}
	return nil
}
