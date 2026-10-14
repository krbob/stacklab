package backup

import (
	"archive/tar"
	"context"
	"io"
	"os"
	"os/exec"
	"path/filepath"
	"strings"
	"testing"
	"time"
)

func requireGNUTar(t *testing.T) {
	t.Helper()
	version, err := exec.Command(tarExecutable(), "--version").Output()
	if err != nil || !strings.Contains(string(version), "GNU tar") {
		t.Skip("GNU tar unavailable")
	}
}
func TestNativeRecoveryMergesExistingDirectoriesWithoutFollowingHardlinks(t *testing.T) {
	requireGNUTar(t)
	base, err := filepath.EvalSymlinks(t.TempDir())
	if err != nil {
		t.Fatal(err)
	}
	target := filepath.Join(base, "target")
	nested := filepath.Join(target, "nested")
	if err = os.MkdirAll(nested, 0700); err != nil {
		t.Fatal(err)
	}
	outside := filepath.Join(base, "outside")
	if err = os.WriteFile(outside, []byte("outside original"), 0600); err != nil {
		t.Fatal(err)
	}
	if err = os.Link(outside, filepath.Join(nested, "data")); err != nil {
		t.Fatal(err)
	}
	if err = os.WriteFile(filepath.Join(nested, "unrelated"), []byte("retain nested contents"), 0600); err != nil {
		t.Fatal(err)
	}
	if err = os.Symlink(outside, filepath.Join(nested, "symlink")); err != nil {
		t.Fatal(err)
	}
	archivePath := filepath.Join(base, "source.tar")
	file, err := os.Create(archivePath)
	if err != nil {
		t.Fatal(err)
	}
	archive := tar.NewWriter(file)
	for _, header := range []*tar.Header{
		{Name: "./", Typeflag: tar.TypeDir, Mode: 0750},
		{Name: "./nested/", Typeflag: tar.TypeDir, Mode: 0750},
		{Name: "./nested/data", Typeflag: tar.TypeReg, Mode: 0640, Size: int64(len("snapshot bytes"))},
		{Name: "./nested/hardlink", Typeflag: tar.TypeLink, Linkname: "./nested/data", Mode: 0640},
		{Name: "./nested/symlink", Typeflag: tar.TypeSymlink, Linkname: "data", Mode: 0777},
	} {
		if err = archive.WriteHeader(header); err != nil {
			t.Fatal(err)
		}
		if header.Typeflag == tar.TypeReg {
			if _, err = io.WriteString(archive, "snapshot bytes"); err != nil {
				t.Fatal(err)
			}
		}
	}
	if err = archive.Close(); err != nil {
		t.Fatal(err)
	}
	if err = file.Close(); err != nil {
		t.Fatal(err)
	}
	for range 2 {
		if err = ExtractArchive(context.Background(), archivePath, target, false); err != nil {
			t.Fatalf("merge existing directories: %v", err)
		}
		for filename, expected := range map[string]string{outside: "outside original", filepath.Join(nested, "data"): "snapshot bytes", filepath.Join(nested, "unrelated"): "retain nested contents"} {
			data, readErr := os.ReadFile(filename)
			if readErr != nil || string(data) != expected {
				t.Fatalf("%s: %q %v", filename, data, readErr)
			}
		}
		actual, _ := os.Stat(filepath.Join(nested, "data"))
		external, _ := os.Stat(outside)
		linked, _ := os.Stat(filepath.Join(nested, "hardlink"))
		directory, _ := os.Stat(nested)
		if os.SameFile(actual, external) {
			t.Fatal("recovery reused externally linked inode")
		}
		if !os.SameFile(actual, linked) {
			t.Fatal("archive hardlink relationship lost")
		}
		if directory.Mode().Perm() != 0750 {
			t.Fatalf("directory mode=%o", directory.Mode().Perm())
		}
		if link, readErr := os.Readlink(filepath.Join(nested, "symlink")); readErr != nil || link != "data" {
			t.Fatalf("symlink=%q %v", link, readErr)
		}
	}
	collision := filepath.Join(base, "collision.tar")
	writeReviewTar(t, collision, "unused")
	// A regular entry cannot remove a populated target directory as a side
	// effect of replacing a file. Unrelated descendants must remain untouched.
	writeCatalogFixture(t, collision, []catalogFixtureEntry{{name: "nested", content: "not a directory"}})
	if err = ExtractArchive(context.Background(), collision, target, false); err == nil {
		t.Fatal("nonempty directory replaced by file")
	}
	if _, err = os.Stat(filepath.Join(nested, "unrelated")); err != nil {
		t.Fatal("unrelated nested file removed")
	}
}
func TestCaptureResumesWritersBeforeIndexingImmutableBytes(t *testing.T) {
	requireGNUTar(t)
	capture, source, calls := captureFixture(t)
	capture.cfg.BackupHelperPath = ""
	if err := os.WriteFile(filepath.Join(source, "data"), []byte("captured bytes"), 0600); err != nil {
		t.Fatal(err)
	}
	stage := t.TempDir()
	baseCommand := capture.command
	capture.command = func(ctx context.Context, name string, args ...string) ([]byte, error) {
		if len(args) > 0 && args[0] == "start" {
			if _, err := os.Stat(filepath.Join(stage, "indexes", "source-a.json")); !os.IsNotExist(err) {
				t.Errorf("source was still stopped during index generation: %v", err)
			}
		}
		return baseCommand(ctx, name, args...)
	}
	plan := Plan{ID: "resume-before-index", Consistency: "stop", StagingMaxBytes: 1 << 20, StopTimeoutSeconds: 60, Sources: []Source{{ID: "source-a", Kind: "service_mount", StackID: "demo", Service: "app", Mount: "/data", Path: source}}}
	manifest, err := capture.Prepare(context.Background(), plan, "resume-before-index", stage)
	if err != nil {
		t.Fatal(err)
	}
	if len(manifest.Sources) != 1 || manifest.Sources[0].IndexPath == "" {
		t.Fatal("capture omitted encrypted index")
	}
	starts := 0
	for _, call := range *calls {
		if strings.HasPrefix(call, "start ") {
			starts++
		}
	}
	if starts != 2 {
		t.Fatalf("writers resumed %d times, want exactly two running writers", starts)
	}
}

func TestNativeRootMetadataRecoveryPreservesDirectoryAndUnselectedFiles(t *testing.T) {
	requireGNUTar(t)
	base, err := filepath.EvalSymlinks(t.TempDir())
	if err != nil {
		t.Fatal(err)
	}
	target := filepath.Join(base, "target")
	if err = os.Mkdir(target, 0700); err != nil {
		t.Fatal(err)
	}
	outside := filepath.Join(base, "outside")
	if err = os.WriteFile(outside, []byte("retain existing inode and content"), 0600); err != nil {
		t.Fatal(err)
	}
	existing := filepath.Join(target, "unselected")
	if err = os.Link(outside, existing); err != nil {
		t.Fatal(err)
	}
	before, err := os.Stat(target)
	if err != nil {
		t.Fatal(err)
	}
	original, err := os.Stat(existing)
	if err != nil {
		t.Fatal(err)
	}
	archivePath := filepath.Join(base, "root-only.tar")
	file, err := os.Create(archivePath)
	if err != nil {
		t.Fatal(err)
	}
	archive := tar.NewWriter(file)
	stamp := time.Unix(1700000000, 0)
	if err = archive.WriteHeader(&tar.Header{Name: ".", Typeflag: tar.TypeDir, Mode: 0750, ModTime: stamp}); err != nil {
		t.Fatal(err)
	}
	if err = archive.Close(); err != nil {
		t.Fatal(err)
	}
	if err = file.Close(); err != nil {
		t.Fatal(err)
	}
	if err = ExtractArchive(context.Background(), archivePath, target, false); err != nil {
		t.Fatalf("directory-only recovery attempted to unlink its root: %v", err)
	}
	after, err := os.Stat(target)
	if err != nil {
		t.Fatal(err)
	}
	if !os.SameFile(before, after) || after.Mode().Perm() != 0750 || !after.ModTime().Equal(stamp) {
		t.Fatalf("directory identity or metadata changed unexpectedly: %v", after)
	}
	current, err := os.Stat(existing)
	if err != nil {
		t.Fatal(err)
	}
	external, err := os.Stat(outside)
	if err != nil {
		t.Fatal(err)
	}
	if !os.SameFile(original, current) || !os.SameFile(current, external) {
		t.Fatal("metadata-only recovery unlinked an unselected file")
	}
	if content, readErr := os.ReadFile(existing); readErr != nil || string(content) != "retain existing inode and content" {
		t.Fatalf("unselected file changed: %q %v", content, readErr)
	}
}
