package backup

import (
	"context"
	"encoding/json"
	"os"
	"os/exec"
	"path/filepath"
	"strings"
	"testing"
)

// Corrupt a data-only encrypted pack without changing its length or metadata.
// Real restic metadata checks pass while full reads detect the corruption; this
// exercises the native diagnostic classifier as well as sticky evidence.
func TestNativeDataCorruptionSurvivesMetadataAndSubsetSuccess(t *testing.T) {
	if _, err := exec.LookPath("restic"); err != nil {
		t.Skip("restic is not installed")
	}
	s, capture, _ := newTestService(t)
	s.runner = nativeRunner{}
	s.capture = &nativeArchiveCapture{fakeCapture: capture}
	d := addDestination(t, s, "Native integrity")
	p := addPlan(t, s, d)
	if _, err := s.restic(context.Background(), d, "", "init"); err != nil {
		t.Fatal(err)
	}
	job, run, err := s.StartRun(context.Background(), p.ID, RunRequest{}, "test")
	if err != nil {
		t.Fatal(err)
	}
	s.ExecuteRun(context.Background(), job, run.ID)
	if saved := s.state.Runs[run.ID]; saved.State != "succeeded" {
		t.Fatalf("backup: %+v", saved)
	}
	indexes, err := os.ReadDir(filepath.Join(d.Repository, "index"))
	if err != nil {
		t.Fatal(err)
	}
	packID := ""
	for _, index := range indexes {
		data, readErr := s.restic(context.Background(), d, "", "cat", "index", index.Name())
		if readErr != nil {
			t.Fatal(readErr)
		}
		var decoded struct {
			Packs []struct {
				ID    string `json:"id"`
				Blobs []struct {
					Type string `json:"type"`
				} `json:"blobs"`
			} `json:"packs"`
		}
		if err := json.Unmarshal(data, &decoded); err != nil {
			t.Fatal(err)
		}
		for _, pack := range decoded.Packs {
			dataOnly := len(pack.Blobs) > 0
			for _, blob := range pack.Blobs {
				dataOnly = dataOnly && blob.Type == "data"
			}
			if dataOnly {
				packID = pack.ID
				break
			}
		}
		if packID != "" {
			break
		}
	}
	if len(packID) != 64 {
		t.Fatal("fixture has no data-only pack")
	}
	packPath := filepath.Join(d.Repository, "data", packID[:2], packID)
	original, err := os.ReadFile(packPath)
	if err != nil || len(original) < 64 {
		t.Fatalf("read pack: %v", err)
	}
	corrupt := append([]byte(nil), original...)
	corrupt[32] ^= 0xff
	if err := os.Chmod(packPath, 0600); err != nil {
		t.Fatal(err)
	}
	if err := os.WriteFile(packPath, corrupt, 0600); err != nil {
		t.Fatal(err)
	}
	d = runEvidenceCheck(t, s, d, "all", true)
	if d.DataCheck.Status != "failed" || !strings.Contains(d.DataCheck.Error, "integrity_failed") {
		t.Fatalf("native corruption not classified: %+v", d)
	}
	d = runEvidenceCheck(t, s, d, "metadata", false)
	if d.CheckStatus != "failed" || d.MetadataCheck.Status != "verified" {
		t.Fatalf("native metadata check erased corruption: %+v", d)
	}
	// Repairing the bytes does not itself establish new evidence. A successful
	// random sample also cannot certify the full repository has been repaired.
	if err := os.WriteFile(packPath, original, 0600); err != nil {
		t.Fatal(err)
	}
	d = runEvidenceCheck(t, s, d, "subset", false)
	if d.CheckStatus != "failed" {
		t.Fatal("native subset check erased data failure")
	}
	d = runEvidenceCheck(t, s, d, "all", false)
	if d.CheckStatus != "verified" || d.DataCheck.Status != "verified" {
		t.Fatalf("native full check did not resolve repaired corruption: %+v", d)
	}
}
