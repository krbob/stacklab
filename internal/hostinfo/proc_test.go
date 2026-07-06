package hostinfo

import (
	"os"
	"path/filepath"
	"testing"
)

func TestReadProcCPUSampleExcludesGuestFields(t *testing.T) {
	t.Parallel()

	procDir := t.TempDir()
	if err := os.WriteFile(filepath.Join(procDir, "stat"), []byte("cpu  100 20 30 400 50 6 7 8 900 1000\n"), 0o644); err != nil {
		t.Fatalf("WriteFile(stat) error = %v", err)
	}

	sample, ok := readProcCPUSample(procDir)
	if !ok {
		t.Fatalf("readProcCPUSample() ok = false")
	}
	if sample.total != 621 {
		t.Fatalf("sample.total = %d, want 621", sample.total)
	}
	if sample.idle != 450 {
		t.Fatalf("sample.idle = %d, want 450", sample.idle)
	}
}
