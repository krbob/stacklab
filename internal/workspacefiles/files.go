package workspacefiles

import (
	"fmt"
	"os"
	"time"
	"unicode/utf8"
)

func ValidTextSample(sample []byte, truncated bool) bool {
	if utf8.Valid(sample) {
		return true
	}
	if !truncated {
		return false
	}
	for trim := 1; trim < utf8.UTFMax && trim < len(sample); trim++ {
		if utf8.Valid(sample[:len(sample)-trim]) {
			return true
		}
	}
	return false
}

func EnsureExpectedModifiedAt(targetPath string, expected *time.Time, conflictErr error, statContext string) error {
	if expected == nil {
		return nil
	}

	info, err := os.Stat(targetPath)
	if err != nil {
		if os.IsNotExist(err) {
			return conflictErr
		}
		return fmt.Errorf("stat %s before save: %w", statContext, err)
	}
	if !info.ModTime().UTC().Equal(expected.UTC()) {
		return conflictErr
	}
	return nil
}
