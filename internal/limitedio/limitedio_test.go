package limitedio

import (
	"errors"
	"os"
	"path/filepath"
	"testing"
)

func TestReadFileRejectsKnownOversizeBeforeReading(t *testing.T) {
	t.Parallel()

	path := filepath.Join(t.TempDir(), "large.txt")
	if err := os.WriteFile(path, []byte("12345"), 0o600); err != nil {
		t.Fatalf("WriteFile() error = %v", err)
	}

	_, err := ReadFile(path, 4)
	if !errors.Is(err, ErrContentTooLarge) {
		t.Fatalf("ReadFile() error = %v, want %v", err, ErrContentTooLarge)
	}
	if maxBytes, ok := MaxBytes(err); !ok || maxBytes != 4 {
		t.Fatalf("MaxBytes() = %d, %v; want 4, true", maxBytes, ok)
	}
}

func TestBufferCapsRetainedOutputAndContinuesDraining(t *testing.T) {
	t.Parallel()

	buffer := NewBuffer(4)
	if written, err := buffer.Write([]byte("123")); err != nil || written != 3 {
		t.Fatalf("Write(first) = %d, %v; want 3, nil", written, err)
	}
	if written, err := buffer.Write([]byte("456")); err != nil || written != 3 {
		t.Fatalf("Write(second) = %d, %v; want 3, nil", written, err)
	}
	if got := buffer.String(); got != "1234" {
		t.Fatalf("String() = %q, want %q", got, "1234")
	}
	if !errors.Is(buffer.Err(), ErrContentTooLarge) {
		t.Fatalf("Err() = %v, want %v", buffer.Err(), ErrContentTooLarge)
	}
}
