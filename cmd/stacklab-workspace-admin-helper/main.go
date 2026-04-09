package main

import (
	"bufio"
	"encoding/json"
	"errors"
	"flag"
	"fmt"
	"io/fs"
	"os"
	"path/filepath"
	"strings"
	"syscall"
)

const (
	defaultStacklabRoot = "/opt/stacklab"
	envFilePath         = "/etc/stacklab/stacklab.env"
)

type emittedError struct {
	error
}

type repairResult struct {
	ChangedItems int      `json:"changed_items"`
	Warnings     []string `json:"warnings,omitempty"`
}

func main() {
	if len(os.Args) < 2 {
		failJSON(fmt.Errorf("usage: stacklab-workspace-admin-helper <probe|repair>"))
	}

	switch os.Args[1] {
	case "probe":
		if err := runProbe(os.Args[2:]); err != nil {
			var emitted *emittedError
			if errors.As(err, &emitted) {
				os.Exit(1)
			}
			failJSON(err)
		}
	case "repair":
		if err := runRepair(os.Args[2:]); err != nil {
			var emitted *emittedError
			if errors.As(err, &emitted) {
				os.Exit(1)
			}
			failJSON(err)
		}
	default:
		failJSON(fmt.Errorf("unknown subcommand %q", os.Args[1]))
	}
}

func runProbe(args []string) error {
	flags := flag.NewFlagSet("probe", flag.ContinueOnError)
	flags.SetOutput(os.Stderr)
	if err := flags.Parse(args); err != nil {
		return err
	}

	if _, err := loadStacklabRoot(); err != nil {
		return err
	}

	emitResult(repairResult{ChangedItems: 0})
	return nil
}

func runRepair(args []string) error {
	var targetPath string
	var recursive bool

	flags := flag.NewFlagSet("repair", flag.ContinueOnError)
	flags.SetOutput(os.Stderr)
	flags.StringVar(&targetPath, "path", "", "absolute target path within managed roots")
	flags.BoolVar(&recursive, "recursive", false, "repair recursively when target is a directory")
	if err := flags.Parse(args); err != nil {
		return err
	}
	if !filepath.IsAbs(targetPath) {
		return errors.New("path must be absolute")
	}

	stacklabRoot, err := loadStacklabRoot()
	if err != nil {
		return err
	}
	resolvedTarget, workspaceRoot, err := resolveManagedTarget(stacklabRoot, targetPath)
	if err != nil {
		return err
	}

	uid, gid, err := ownershipOf(workspaceRoot)
	if err != nil {
		return err
	}

	changed, err := repairManagedPath(resolvedTarget, uid, gid, recursive)
	if err != nil {
		emitResult(repairResult{
			ChangedItems: changed,
			Warnings:     []string{err.Error()},
		})
		return &emittedError{err}
	}

	emitResult(repairResult{ChangedItems: changed})
	return nil
}

func loadStacklabRoot() (string, error) {
	root := defaultStacklabRoot

	file, err := os.Open(envFilePath)
	if err == nil {
		defer file.Close()
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
			if strings.TrimSpace(key) == "STACKLAB_ROOT" {
				parsed := strings.TrimSpace(value)
				if parsed != "" {
					root = parsed
				}
				break
			}
		}
		if err := scanner.Err(); err != nil {
			return "", fmt.Errorf("read %s: %w", envFilePath, err)
		}
	} else if !os.IsNotExist(err) {
		return "", fmt.Errorf("open %s: %w", envFilePath, err)
	}

	resolved, err := filepath.Abs(root)
	if err != nil {
		return "", fmt.Errorf("resolve stacklab root: %w", err)
	}
	return resolved, nil
}

func resolveManagedTarget(stacklabRoot, targetPath string) (string, string, error) {
	configRoot := filepath.Join(stacklabRoot, "config")
	stacksRoot := filepath.Join(stacklabRoot, "stacks")

	resolvedTarget, err := filepath.EvalSymlinks(targetPath)
	if err != nil {
		if os.IsNotExist(err) {
			return "", "", fmt.Errorf("target path not found: %w", err)
		}
		return "", "", fmt.Errorf("resolve target path: %w", err)
	}
	resolvedTarget, err = filepath.Abs(resolvedTarget)
	if err != nil {
		return "", "", fmt.Errorf("resolve absolute target path: %w", err)
	}

	if withinRoot(configRoot, resolvedTarget) {
		return resolvedTarget, configRoot, nil
	}
	if withinRoot(stacksRoot, resolvedTarget) {
		relative, err := filepath.Rel(stacksRoot, resolvedTarget)
		if err != nil {
			return "", "", fmt.Errorf("compare stack workspace path: %w", err)
		}
		parts := strings.Split(filepath.ToSlash(relative), "/")
		if len(parts) == 0 || parts[0] == "." || parts[0] == "" {
			return "", "", errors.New("target path must be inside one stack directory")
		}
		stackRoot := filepath.Join(stacksRoot, parts[0])
		return resolvedTarget, stackRoot, nil
	}

	return "", "", errors.New("target path is outside managed Stacklab roots")
}

func withinRoot(root, target string) bool {
	resolvedRoot, err := filepath.Abs(root)
	if err != nil {
		return false
	}
	if evalRoot, err := filepath.EvalSymlinks(resolvedRoot); err == nil {
		resolvedRoot = evalRoot
	}
	relative, err := filepath.Rel(resolvedRoot, target)
	if err != nil {
		return false
	}
	return relative != ".." && !strings.HasPrefix(relative, ".."+string(filepath.Separator))
}

func ownershipOf(path string) (int, int, error) {
	info, err := os.Stat(path)
	if err != nil {
		return 0, 0, fmt.Errorf("stat workspace root: %w", err)
	}
	stat, ok := info.Sys().(*syscall.Stat_t)
	if !ok {
		return 0, 0, errors.New("workspace root stat missing ownership data")
	}
	return int(stat.Uid), int(stat.Gid), nil
}

func repairManagedPath(targetPath string, uid, gid int, recursive bool) (int, error) {
	info, err := os.Lstat(targetPath)
	if err != nil {
		return 0, fmt.Errorf("lstat target path: %w", err)
	}
	if info.Mode()&os.ModeSymlink != 0 {
		return 0, errors.New("symlinks are not supported in permission repair")
	}

	if !info.IsDir() || !recursive {
		changed, err := repairOne(targetPath, info, uid, gid)
		if err != nil {
			return changed, err
		}
		if info.IsDir() && !recursive {
			return changed, nil
		}
		return changed, nil
	}

	changedItems := 0
	err = filepath.WalkDir(targetPath, func(path string, entry fs.DirEntry, walkErr error) error {
		if walkErr != nil {
			return walkErr
		}
		if entry.Type()&os.ModeSymlink != 0 {
			return errors.New("symlinks are not supported in permission repair")
		}
		info, err := entry.Info()
		if err != nil {
			return err
		}
		changed, err := repairOne(path, info, uid, gid)
		changedItems += changed
		return err
	})
	if err != nil {
		return changedItems, err
	}
	return changedItems, nil
}

func repairOne(path string, info os.FileInfo, uid, gid int) (int, error) {
	if !info.Mode().IsRegular() && !info.IsDir() {
		return 0, fmt.Errorf("unsupported file type at %s", path)
	}

	stat, ok := info.Sys().(*syscall.Stat_t)
	if !ok {
		return 0, fmt.Errorf("missing ownership data for %s", path)
	}

	changed := 0
	if int(stat.Uid) != uid || int(stat.Gid) != gid {
		if err := os.Chown(path, uid, gid); err != nil {
			return changed, fmt.Errorf("chown %s: %w", path, err)
		}
		changed++
	}

	targetMode := normalizedMode(info)
	if info.Mode().Perm() != targetMode {
		if err := os.Chmod(path, targetMode); err != nil {
			return changed, fmt.Errorf("chmod %s: %w", path, err)
		}
		changed++
	}

	return changed, nil
}

func normalizedMode(info os.FileInfo) os.FileMode {
	perm := info.Mode().Perm()
	if info.IsDir() {
		return perm | 0o700
	}
	perm |= 0o600
	if perm&0o111 != 0 {
		perm |= 0o100
	}
	return perm
}

func emitResult(result repairResult) {
	encoded, err := json.Marshal(result)
	if err != nil {
		fmt.Fprintf(os.Stdout, "{\"changed_items\":%d}\n", result.ChangedItems)
		return
	}
	fmt.Fprintln(os.Stdout, string(encoded))
}

func failJSON(err error) {
	result := repairResult{
		Warnings: []string{err.Error()},
	}
	emitResult(result)
	os.Exit(1)
}
