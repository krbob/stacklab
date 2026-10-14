package main

import (
	"fmt"
	"os"
	"path/filepath"
	"runtime"
	"strings"
	"testing"
	"time"
)

// Even future tests that forget to override globals cannot touch an installed
// helper's spool or read its configuration. Each test may narrow these further.
func TestMain(m *testing.M) {
	parent := ""
	if runtime.GOOS == "linux" && os.Geteuid() == 0 {
		parent = "/var/lib"
	}
	root, err := os.MkdirTemp(parent, "stacklab-helper-suite-")
	if err != nil {
		fmt.Fprintln(os.Stderr, err)
		os.Exit(1)
	}
	restoreSpoolRoot = filepath.Join(root, "spool")
	configurationPath = filepath.Join(root, "stacklab.env")
	code := m.Run()
	_ = os.RemoveAll(root)
	os.Exit(code)
}

func TestHelperReadinessDoesNotMutateSpool(t *testing.T) {
	if runtime.GOOS != "linux" || os.Geteuid() != 0 {
		t.Skip("root Linux helper integration")
	}
	root, err := os.MkdirTemp("/var/lib", "stacklab-helper-probe-")
	if err != nil {
		t.Fatal(err)
	}
	defer os.RemoveAll(root)
	oldSpool, oldConfig := restoreSpoolRoot, configurationPath
	defer func() { restoreSpoolRoot, configurationPath = oldSpool, oldConfig }()
	restoreSpoolRoot = filepath.Join(root, "spool")
	configurationPath = filepath.Join(root, "stacklab.env")
	if err := os.WriteFile(configurationPath, []byte("STACKLAB_ROOT="+root+"\nSTACKLAB_DATA_DIR="+filepath.Join(root, "private")+"\n"), 0600); err != nil {
		t.Fatal(err)
	}
	if err := probeRecoveryAction("prepare-restore"); err == nil {
		t.Fatal("missing spool accepted")
	}
	if _, err := os.Lstat(restoreSpoolRoot); !os.IsNotExist(err) {
		t.Fatalf("probe created missing spool: %v", err)
	}
	if err := os.Mkdir(restoreSpoolRoot, 0750); err != nil {
		t.Fatal(err)
	}
	if err := probeRecoveryAction("prepare-restore"); err == nil {
		t.Fatal("non-private spool accepted")
	}
	info, err := os.Stat(restoreSpoolRoot)
	if err != nil || info.Mode().Perm() != 0750 {
		t.Fatalf("probe changed spool permissions: %v", err)
	}
	if err := os.Chmod(restoreSpoolRoot, 0700); err != nil {
		t.Fatal(err)
	}
	abandoned := filepath.Join(restoreSpoolRoot, strings.Repeat("a", 48))
	if err := os.Mkdir(abandoned, 0700); err != nil {
		t.Fatal(err)
	}
	before := time.Now().Add(-25 * time.Hour)
	if err := os.Chtimes(abandoned, before, before); err != nil {
		t.Fatal(err)
	}
	for _, action := range []string{"prepare-restore", "check-restore", "check-restores", "apply-restore", "release-restore", "check-target"} {
		if err := probeRecoveryAction(action); err != nil {
			t.Fatalf("%s: %v", action, err)
		}
	}
	if _, err := os.Stat(abandoned); err != nil {
		t.Fatalf("probe garbage-collected prepared work: %v", err)
	}
}
