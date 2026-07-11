package atomicfile

import (
	"os"
	"path/filepath"
)

const defaultFileMode os.FileMode = 0o644

func WriteString(path, content, pattern string) error {
	return writeString(path, content, pattern, 0, true)
}

// WriteStringMode atomically writes content and always applies mode to the
// destination. Use it for managed files whose contents require a fixed,
// restrictive permission regardless of the mode of an existing file.
func WriteStringMode(path, content, pattern string, mode os.FileMode) error {
	return writeString(path, content, pattern, mode.Perm(), false)
}

func writeString(path, content, pattern string, mode os.FileMode, preserveExistingMode bool) error {
	if pattern == "" {
		pattern = ".stacklab-*"
	}
	if err := os.MkdirAll(filepath.Dir(path), 0o755); err != nil {
		return err
	}

	if preserveExistingMode {
		mode = defaultFileMode
		if info, err := os.Stat(path); err == nil {
			mode = info.Mode().Perm()
		} else if !os.IsNotExist(err) {
			return err
		}
	}

	tmpFile, err := os.CreateTemp(filepath.Dir(path), pattern)
	if err != nil {
		return err
	}
	tmpName := tmpFile.Name()

	if _, err := tmpFile.WriteString(content); err != nil {
		_ = tmpFile.Close()
		_ = os.Remove(tmpName)
		return err
	}
	if err := tmpFile.Chmod(mode); err != nil {
		_ = tmpFile.Close()
		_ = os.Remove(tmpName)
		return err
	}
	if err := tmpFile.Sync(); err != nil {
		_ = tmpFile.Close()
		_ = os.Remove(tmpName)
		return err
	}
	if err := tmpFile.Close(); err != nil {
		_ = os.Remove(tmpName)
		return err
	}
	if err := os.Rename(tmpName, path); err != nil {
		_ = os.Remove(tmpName)
		return err
	}
	if err := syncDir(filepath.Dir(path)); err != nil {
		return err
	}

	return nil
}

func syncDir(path string) error {
	dir, err := os.Open(path)
	if err != nil {
		return err
	}
	defer dir.Close()
	return dir.Sync()
}
