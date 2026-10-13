package main

import (
	"os"
	"path/filepath"
	"testing"
)

func TestAuthorizePathRejectsTraversalAndSymlinks(t *testing.T) {
	root := t.TempDir()
	outside := t.TempDir()
	if err := os.Symlink(outside, filepath.Join(root, "link")); err != nil {
		t.Fatal(err)
	}
	for _, target := range []string{outside, "/", filepath.Join(root, "link", "file"), filepath.Join(root, "..", "escape")} {
		if _, err := authorizePath(target, []string{root}, true); err == nil {
			t.Fatalf("unsafe target accepted: %s", target)
		}
	}
	// macOS /var resolves through /private, so use canonical temporary roots.
	canonical, err := filepath.EvalSymlinks(root)
	if err != nil {
		t.Fatal(err)
	}
	if _, err := authorizePath(filepath.Join(canonical, "new"), []string{canonical}, true); err != nil {
		t.Fatal(err)
	}
}

func TestHelperRejectsArbitraryActionsAndOptions(t *testing.T) {
	for _, args := range [][]string{{}, {"shell"}, {"archive", "--path", "/etc", "--volume", "data"}, {"restore", "--command", "sh"}} {
		if err := run(args); err == nil {
			t.Fatalf("invalid helper request accepted: %v", args)
		}
	}
}
