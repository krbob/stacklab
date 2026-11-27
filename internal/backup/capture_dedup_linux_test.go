package backup

import (
	"archive/tar"
	"bytes"
	"encoding/json"
	"fmt"
	"io"
	"os"
	"os/exec"
	"path/filepath"
	"testing"
	"time"
)

func TestNativeArchiveIgnoresAccessAndChangeTimes(t *testing.T) {
	mtime := time.Unix(1700000000, 123456789)
	source := t.TempDir()
	for i := range 8 {
		directory := filepath.Join(source, fmt.Sprintf("directory-%d", i))
		if err := os.Mkdir(directory, 0750); err != nil {
			t.Fatal(err)
		}
		if err := os.WriteFile(filepath.Join(directory, "data"), bytes.Repeat([]byte{byte(i), 0, 255}, 32768), 0640); err != nil {
			t.Fatal(err)
		}
	}
	setAccessTime := func(atime time.Time) {
		t.Helper()
		if err := filepath.WalkDir(source, func(path string, _ os.DirEntry, err error) error {
			if err != nil {
				return err
			}
			// Chtimes also changes ctime. Preserve the meaningful modification
			// time while simulating reads of both directories and regular files.
			return os.Chtimes(path, atime, mtime)
		}); err != nil {
			t.Fatal(err)
		}
	}
	capture := func(path string) []byte {
		t.Helper()
		args, err := ArchiveArguments(path, nil)
		if err != nil {
			t.Fatal(err)
		}
		cmd := exec.Command(tarExecutable(), args...)
		// This GNU tar environment otherwise embeds the process ID in each
		// PAX header name, making equivalent captures different as well.
		cmd.Env = []string{"PATH=" + os.Getenv("PATH"), "LC_ALL=C", "POSIXLY_CORRECT=1"}
		output, err := cmd.Output()
		if err != nil {
			t.Fatalf("archive: %v", err)
		}
		return output
	}

	for _, path := range []string{source, filepath.Join(source, "directory-0", "data")} {
		t.Run(filepath.Base(path), func(t *testing.T) {
			setAccessTime(mtime)
			before := capture(path)
			setAccessTime(mtime.Add(48 * time.Hour))
			after := capture(path)
			if !bytes.Equal(before, after) {
				t.Fatal("unchanged content and recovery metadata produced different archive bytes after access/ctime changes")
			}
			reader := tar.NewReader(bytes.NewReader(after))
			for {
				header, err := reader.Next()
				if err == io.EOF {
					break
				}
				if err != nil {
					t.Fatal(err)
				}
				if !header.AccessTime.IsZero() || !header.ChangeTime.IsZero() {
					t.Fatalf("volatile times retained for %s", header.Name)
				}
				if !header.ModTime.Equal(mtime) || header.Uid != os.Getuid() || header.Gid != os.Getgid() {
					t.Fatalf("recovery time or ownership changed for %s", header.Name)
				}
			}
		})
	}

	t.Run("restic reuses captured data", func(t *testing.T) {
		if _, err := exec.LookPath("restic"); err != nil {
			t.Skip("restic is not installed")
		}
		workspace := t.TempDir()
		archive := filepath.Join(workspace, "source.tar")
		repository := filepath.Join(workspace, "repository")
		run := func(args ...string) []byte {
			t.Helper()
			cmd := exec.Command("restic", append([]string{"--no-cache", "--repo", repository}, args...)...)
			cmd.Env = []string{"PATH=" + os.Getenv("PATH"), "RESTIC_PASSWORD=archive-dedup-test"}
			out, err := cmd.CombinedOutput()
			if err != nil {
				t.Fatalf("restic %s: %v\n%s", args[0], err, out)
			}
			return out
		}
		run("init")
		for round := range 3 {
			if round == 2 {
				// Real data changes must still create new repository blobs.
				if err := os.WriteFile(filepath.Join(source, "directory-0", "data"), []byte("changed application data"), 0640); err != nil {
					t.Fatal(err)
				}
			}
			setAccessTime(mtime.Add(time.Duration(round) * 24 * time.Hour))
			if err := os.WriteFile(archive, capture(source), 0600); err != nil {
				t.Fatal(err)
			}
			// Force a content read: this must exercise blob deduplication,
			// not restic's size/mtime shortcut for an unchanged input file.
			out := run("backup", "--json", "--force", archive)
			found := false
			for _, line := range bytes.Split(out, []byte("\n")) {
				var event struct {
					MessageType string `json:"message_type"`
					DataBlobs   int    `json:"data_blobs"`
				}
				if json.Unmarshal(line, &event) == nil && event.MessageType == "summary" {
					found = true
					if (round == 1) != (event.DataBlobs == 0) {
						t.Fatalf("round %d added %d data blobs", round, event.DataBlobs)
					}
				}
			}
			if !found {
				t.Fatal("missing restic summary")
			}
		}
		run("check", "--read-data")
	})
}
