// stacklab-backup-helper only archives and restores approved data roots. It is
// intentionally separate from restic: cloud credentials never reach root.
package main

import (
	"bufio"
	"context"
	"encoding/json"
	"errors"
	"flag"
	"fmt"
	"io"
	"os"
	"os/exec"
	"os/signal"
	"path/filepath"
	"runtime"
	"strings"
	"syscall"

	"stacklab/internal/backup"
	"stacklab/internal/limitedio"
)

var configurationPath = "/etc/stacklab/stacklab.env"

type repeatedFlag []string

func (f *repeatedFlag) String() string         { return strings.Join(*f, ",") }
func (f *repeatedFlag) Set(value string) error { *f = append(*f, value); return nil }

func main() {
	if err := run(os.Args[1:]); err != nil {
		fmt.Fprintln(os.Stderr, err)
		os.Exit(1)
	}
}

func run(args []string) error {
	ctx, cancel := signal.NotifyContext(context.Background(), syscall.SIGTERM, syscall.SIGINT)
	defer cancel()
	if len(args) == 0 {
		return errors.New("expected archive or restore")
	}
	action := args[0]
	if len(args) == 2 && args[1] == "--probe" {
		return probeRecoveryAction(action)
	}
	if action == "service-config" {
		if len(args) != 1 {
			return errors.New("service-config accepts no arguments")
		}
		if _, _, err := trustedRoots(); err != nil {
			return err
		}
		content, err := limitedio.ReadFile(configurationPath, 64*1024)
		if err != nil {
			return err
		}
		_, err = os.Stdout.Write(content)
		return err
	}
	if action == "check-restores" {
		return checkPreparedRestores(ctx, args[1:])
	}
	if action == "prepare-restore" || action == "check-restore" || action == "apply-restore" || action == "release-restore" {
		return runPreparedRestore(ctx, action, args[1:])
	}
	if action != "archive" && action != "restore" && action != "check-target" {
		return errors.New("unsupported backup helper action")
	}
	flags := flag.NewFlagSet(action, flag.ContinueOnError)
	var target, volume, archive string
	var excludes repeatedFlag
	flags.StringVar(&target, "path", "", "approved absolute data path")
	flags.StringVar(&volume, "volume", "", "Docker local volume name")
	flags.StringVar(&archive, "archive", "", "private archive path")
	flags.Var(&excludes, "exclude", "relative exclusion pattern")
	if err := flags.Parse(args[1:]); err != nil {
		return err
	}
	if flags.NArg() != 0 {
		return errors.New("unexpected positional arguments")
	}
	if (target == "") == (volume == "") {
		return errors.New("exactly one data path or volume is required")
	}
	roots, stateRoot, err := trustedRoots()
	if err != nil {
		return err
	}
	if volume != "" {
		target, err = volumePath(volume)
		if err != nil {
			return err
		}
	} else {
		target, err = authorizePath(target, roots, action != "archive")
		if err != nil {
			// In-place recovery mappings may contain a Docker-inspected local
			// mountpoint; independently re-inspect before granting access.
			if action == "archive" || !isLocalVolumePath(target) {
				return err
			}
		}
	}
	if action != "archive" {
		if err = rejectRecoveryPrivateTarget(target, stateRoot); err != nil {
			return err
		}
	}
	if action == "check-target" {
		if len(excludes) > 0 || archive != "" {
			return errors.New("check-target accepts only a data path or volume")
		}
		return nil
	}
	if action == "archive" {
		if archive != "" {
			return errors.New("archive action writes only to stdout")
		}
		tarArgs, err := backup.ArchiveArguments(target, excludes)
		if err != nil {
			return err
		}
		cmd := exec.CommandContext(ctx, "/usr/bin/tar", tarArgs...)
		if runtime.GOOS == "linux" {
			info, statErr := os.Stat(target)
			if statErr != nil {
				return statErr
			}
			directory := target
			if !info.IsDir() {
				directory = filepath.Dir(target)
			}
			pinned, openErr := os.Open(directory)
			if openErr != nil {
				return openErr
			}
			defer pinned.Close()
			actual, linkErr := os.Readlink(fmt.Sprintf("/proc/self/fd/%d", pinned.Fd()))
			if linkErr != nil {
				return linkErr
			}
			if actual != directory {
				return errors.New("archive source changed during validation")
			}
			for index, arg := range cmd.Args {
				if strings.HasPrefix(arg, "--directory=") {
					cmd.Args[index] = "--directory=/proc/self/fd/3"
				}
			}
			cmd.ExtraFiles = []*os.File{pinned}
		}
		cmd.Stdout = os.Stdout
		cmd.Stderr = os.Stderr
		cmd.Env = []string{"PATH=/usr/bin:/bin", "LC_ALL=C"}
		return cmd.Run()
	}
	if len(excludes) > 0 {
		return errors.New("restore does not accept exclusions")
	}
	if archive == "" {
		return errors.New("restore archive is required")
	}
	// The helper never opens an arbitrary root-owned archive on behalf of the
	// caller. Only Stacklab's private restored/staged recovery sets are allowed.
	archive, err = authorizePath(archive, []string{filepath.Join(stateRoot, "backup", "restores"), filepath.Join(stateRoot, "backup", "staging")}, false)
	if err != nil {
		return err
	}
	info, err := os.Lstat(archive)
	if err != nil {
		return err
	}
	if !info.Mode().IsRegular() {
		return errors.New("archive must be a regular file")
	}
	input, err := os.Open(archive)
	if err != nil {
		return err
	}
	defer input.Close()
	opened, err := input.Stat()
	if err != nil || !os.SameFile(info, opened) {
		return errors.New("recovery archive changed during authorization")
	}
	token, err := prepareRestore(ctx, input, info.Size(), target, "merge", "complete")
	if err != nil {
		return err
	}
	defer func() { _ = runPreparedRestore(context.Background(), "release-restore", []string{"--token", token}) }()
	return runPreparedRestore(ctx, "apply-restore", []string{"--token", token})
}

type helperContextReader struct {
	ctx    context.Context
	reader io.Reader
}

func (r helperContextReader) Read(data []byte) (int, error) {
	if err := r.ctx.Err(); err != nil {
		return 0, err
	}
	return r.reader.Read(data)
}

func trustedRoots() ([]string, string, error) {
	root := "/opt/stacklab"
	state := "/var/lib/stacklab"
	additional := ""
	file, err := os.Open(configurationPath)
	if err != nil && !errors.Is(err, os.ErrNotExist) {
		return nil, "", err
	}
	if err == nil {
		defer file.Close()
		info, err := file.Stat()
		if err != nil {
			return nil, "", err
		}
		stat, ok := info.Sys().(*syscall.Stat_t)
		if !ok || stat.Uid != 0 || info.Mode().Perm()&0022 != 0 {
			return nil, "", errors.New("backup helper configuration must be root-owned and not writable by group or others")
		}
		scanner := bufio.NewScanner(file)
		for scanner.Scan() {
			line := strings.TrimSpace(scanner.Text())
			if line == "" || strings.HasPrefix(line, "#") {
				continue
			}
			key, value, found := strings.Cut(line, "=")
			if !found {
				continue
			}
			value = strings.Trim(strings.TrimSpace(value), "\"'")
			switch strings.TrimSpace(key) {
			case "STACKLAB_ROOT":
				root = value
			case "STACKLAB_DATA_DIR":
				state = value
			case "STACKLAB_BACKUP_ALLOWED_PATHS":
				additional = value
			}
		}
		if err = scanner.Err(); err != nil {
			return nil, "", err
		}
	}
	if !filepath.IsAbs(root) || !filepath.IsAbs(state) {
		return nil, "", errors.New("helper requires absolute trusted root and state paths")
	}
	roots := []string{filepath.Join(root, "stacks"), filepath.Join(root, "config"), filepath.Join(root, "data")}
	for _, path := range strings.Split(additional, ",") {
		if path = strings.TrimSpace(path); path != "" {
			if !filepath.IsAbs(path) || filepath.Clean(path) == "/" {
				return nil, "", errors.New("invalid additional backup root")
			}
			roots = append(roots, path)
		}
	}
	return roots, state, nil
}

func beneath(root, target string) bool {
	rel, err := filepath.Rel(root, target)
	return err == nil && rel != ".." && !strings.HasPrefix(rel, ".."+string(filepath.Separator))
}

func authorizePath(target string, roots []string, allowMissing bool) (string, error) {
	if !filepath.IsAbs(target) || filepath.Clean(target) == "/" {
		return target, errors.New("helper target must be an absolute data path")
	}
	target = filepath.Clean(target)
	for _, root := range roots {
		root = filepath.Clean(root)
		if !beneath(root, target) {
			continue
		}
		// Reject symlinks in all components, including trusted roots. The main
		// service cannot make a configured workspace alias grant root access.
		current := "/"
		for _, component := range strings.Split(strings.TrimPrefix(target, "/"), "/") {
			current = filepath.Join(current, component)
			info, err := os.Lstat(current)
			if errors.Is(err, os.ErrNotExist) && allowMissing {
				continue
			}
			if err != nil {
				return target, err
			}
			if info.Mode()&os.ModeSymlink != 0 {
				return target, errors.New("helper paths must not contain symlinks")
			}
		}
		return target, nil
	}
	return target, errors.New("path is outside trusted backup roots")
}

func volumePath(name string) (string, error) {
	if name == "" || strings.HasPrefix(name, "-") || strings.ContainsAny(name, "/\\\x00\r\n") {
		return "", errors.New("invalid Docker volume name")
	}
	cmd := exec.Command("/usr/bin/docker", "volume", "inspect", name)
	cmd.Env = []string{"PATH=/usr/bin:/bin"}
	output, err := cmd.Output()
	if err != nil {
		return "", errors.New("cannot inspect local Docker volume")
	}
	var volumes []struct {
		Name, Driver, Mountpoint string
		Options                  map[string]string
	}
	if err = json.Unmarshal(output, &volumes); err != nil || len(volumes) != 1 || volumes[0].Name != name || volumes[0].Driver != "local" || len(volumes[0].Options) != 0 {
		return "", errors.New("only plain local Docker volumes are supported")
	}
	path := volumes[0].Mountpoint
	return authorizePath(path, []string{path}, false)
}

func isLocalVolumePath(target string) bool {
	cmd := exec.Command("/usr/bin/docker", "volume", "ls", "--quiet")
	cmd.Env = []string{"PATH=/usr/bin:/bin"}
	output, err := cmd.Output()
	if err != nil {
		return false
	}
	for _, name := range strings.Fields(string(output)) {
		path, err := volumePath(name)
		if err == nil && path == target {
			return true
		}
	}
	return false
}
