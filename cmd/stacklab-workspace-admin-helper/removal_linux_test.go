package main

import (
	"fmt"
	"os"
	"os/exec"
	"path/filepath"
	"strings"
	"testing"
)

func TestRemovalRepairsDirectoriesOnly(t *testing.T) {
	for _, strategy := range []string{repairStrategyACL, repairStrategyOwnership} {
		t.Run(strategy, func(t *testing.T) {
			if strategy == repairStrategyACL {
				if _, err := exec.LookPath("setfacl"); err != nil {
					t.Skip(err)
				}
			}
			root := t.TempDir()
			target := filepath.Join(root, "data", "backrest")
			nested := filepath.Join(target, "cache", "restic")
			if err := os.MkdirAll(nested, 0700); err != nil {
				t.Fatal(err)
			}
			file := filepath.Join(nested, "kuma.db")
			if err := os.WriteFile(file, []byte("fixture"), 0400); err != nil {
				t.Fatal(err)
			}
			outside := t.TempDir()
			if err := os.Symlink(outside, filepath.Join(nested, "link")); err != nil {
				t.Fatal(err)
			}
			if err := os.Chmod(nested, 0500); err != nil {
				t.Fatal(err)
			}
			t.Cleanup(func() { _ = os.Chmod(nested, 0700) })
			if err := repairRemovalDirectories(root, target, strategy, os.Getuid(), os.Getgid(), &repairResult{}); err != nil {
				t.Fatal(err)
			}
			for path, want := range map[string]os.FileMode{nested: 0700, file: 0400} {
				info, err := os.Stat(path)
				if err != nil || info.Mode().Perm() != want {
					t.Fatalf("%s expected %o: %v %v", path, want, info, err)
				}
			}
			if err := os.Remove(file); err != nil {
				t.Fatal(err)
			}
			for _, bad := range []string{filepath.Join(root, "data"), nested, outside, filepath.Join(root, "stacks", "backrest")} {
				if err := repairRemovalDirectories(root, bad, strategy, os.Getuid(), os.Getgid(), &repairResult{}); err == nil {
					t.Fatalf("accepted %s", bad)
				}
			}
			link := filepath.Join(root, "data", "other")
			if err := os.Symlink(outside, link); err != nil {
				t.Fatal(err)
			}
			if err := repairRemovalDirectories(root, link, strategy, os.Getuid(), os.Getgid(), &repairResult{}); err == nil {
				t.Fatal("accepted stack symlink")
			}
		})
	}
}

func TestRemovalGrantsNamedACLToServiceUser(t *testing.T) {
	for _, bin := range []string{"setfacl", "getfacl"} {
		if _, err := exec.LookPath(bin); err != nil {
			t.Skip(err)
		}
	}
	root := t.TempDir()
	dir := filepath.Join(root, "data", "demo")
	if err := os.MkdirAll(dir, 0700); err != nil {
		t.Fatal(err)
	}
	serviceUID := os.Getuid() + 1
	withStacklabEnv(t, "STACKLAB_ROOT="+root+"\n")
	withSudoIdentity(t, serviceUID, os.Getgid())
	if err := runRepair([]string{"--path", dir, "--removal-directories", "--strategy", "acl"}); err != nil {
		t.Fatal(err)
	}
	acl, err := exec.Command("getfacl", "-cn", dir).Output()
	if err != nil {
		t.Fatal(err)
	}
	if !strings.Contains(string(acl), fmt.Sprintf("user:%d:rwx", serviceUID)) {
		t.Fatalf("service access missing: %s", acl)
	}
}
