package atomicfile

import (
	"fmt"
	"os"
	"path/filepath"
)

const defaultFileMode os.FileMode = 0o644

const supportedModeBits = os.ModePerm | os.ModeSetuid | os.ModeSetgid | os.ModeSticky

type ownerGroupPolicy uint8

const (
	requireOwnerGroupPreservation ownerGroupPolicy = iota
	allowOwnerGroupAdoption
)

// WriteString atomically replaces path. Existing owner, group, permission
// bits, and supported extended metadata are preserved. A new file receives
// 0644 permission bits plus directory-inherited metadata.
func WriteString(path, content, pattern string) error {
	stagedPath, err := StageString(path, content, pattern)
	if err != nil {
		return err
	}
	return commit(path, stagedPath)
}

// WriteStringAdoptingOwnership is WriteString with one explicit fallback for
// unprivileged workspace editors: when assigning an existing file's
// owner/group to the replacement is denied, the replacement keeps the process
// owner/group. Permission bits and supported extended metadata are still
// preserved.
func WriteStringAdoptingOwnership(path, content, pattern string) error {
	stagedPath, err := stage(path, []byte(content), pattern, nil, allowOwnerGroupAdoption)
	if err != nil {
		return err
	}
	return commit(path, stagedPath)
}

// WriteStringMode is WriteString with an explicit final permission mode.
// Existing owner, group, and supported extended metadata are still preserved.
func WriteStringMode(path, content, pattern string, mode os.FileMode) error {
	stagedPath, err := StageStringMode(path, content, pattern, mode)
	if err != nil {
		return err
	}
	return commit(path, stagedPath)
}

// WriteBytesMode is the byte-slice counterpart of WriteStringMode.
func WriteBytesMode(path string, content []byte, pattern string, mode os.FileMode) error {
	stagedPath, err := StageBytesMode(path, content, pattern, mode)
	if err != nil {
		return err
	}
	return commit(path, stagedPath)
}

// StageString creates and fsyncs a metadata-preserving replacement in path's
// directory. The caller owns the returned file and must rename or remove it.
func StageString(path, content, pattern string) (string, error) {
	return stage(path, []byte(content), pattern, nil, requireOwnerGroupPreservation)
}

// StageStringMode is StageString with an explicit final permission mode.
func StageStringMode(path, content, pattern string, mode os.FileMode) (string, error) {
	mode &= supportedModeBits
	return stage(path, []byte(content), pattern, &mode, requireOwnerGroupPreservation)
}

// StageStringModeAdoptingOwnership is StageStringMode with one explicit
// fallback for unprivileged workspace editors: when assigning an existing
// file's owner/group to the staged replacement is denied, the replacement
// keeps the process owner/group. All other metadata preservation remains
// strict.
func StageStringModeAdoptingOwnership(path, content, pattern string, mode os.FileMode) (string, error) {
	mode &= supportedModeBits
	return stage(path, []byte(content), pattern, &mode, allowOwnerGroupAdoption)
}

// StageBytesMode is the byte-slice counterpart of StageStringMode.
func StageBytesMode(path string, content []byte, pattern string, mode os.FileMode) (string, error) {
	mode &= supportedModeBits
	return stage(path, content, pattern, &mode, requireOwnerGroupPreservation)
}

func stage(path string, content []byte, pattern string, explicitMode *os.FileMode, ownerGroupPolicy ownerGroupPolicy) (string, error) {
	if pattern == "" {
		pattern = ".stacklab-*"
	}
	directory := filepath.Dir(path)
	if err := os.MkdirAll(directory, 0o755); err != nil {
		return "", fmt.Errorf("create parent directory: %w", err)
	}

	metadata, err := snapshotMetadata(path, explicitMode)
	if err != nil {
		return "", err
	}
	tempFile, err := os.CreateTemp(directory, pattern)
	if err != nil {
		return "", fmt.Errorf("create temporary file: %w", err)
	}
	tempPath := tempFile.Name()
	cleanup := func() {
		_ = tempFile.Close()
		_ = os.Remove(tempPath)
	}

	if _, err := tempFile.Write(content); err != nil {
		cleanup()
		return "", fmt.Errorf("write temporary file: %w", err)
	}
	if metadata.exists {
		if err := applyPlatformMetadata(tempFile, tempPath, metadata.platform, ownerGroupPolicy); err != nil {
			cleanup()
			return "", fmt.Errorf("preserve file metadata: %w", err)
		}
	}
	// Chown and ACL restoration can change permission bits. Chmod last so the
	// requested/preserved mode, including an ACL mask, is authoritative.
	if err := tempFile.Chmod(metadata.mode); err != nil {
		cleanup()
		return "", fmt.Errorf("apply file mode: %w", err)
	}
	if err := tempFile.Sync(); err != nil {
		cleanup()
		return "", fmt.Errorf("sync temporary file: %w", err)
	}
	if err := tempFile.Close(); err != nil {
		_ = os.Remove(tempPath)
		return "", fmt.Errorf("close temporary file: %w", err)
	}
	return tempPath, nil
}

type metadataSnapshot struct {
	exists   bool
	mode     os.FileMode
	platform platformMetadata
}

func snapshotMetadata(path string, explicitMode *os.FileMode) (metadataSnapshot, error) {
	snapshot := metadataSnapshot{mode: defaultFileMode}
	info, err := os.Stat(path)
	if err != nil {
		if !os.IsNotExist(err) {
			return metadataSnapshot{}, fmt.Errorf("stat destination: %w", err)
		}
	} else {
		if !info.Mode().IsRegular() {
			return metadataSnapshot{}, fmt.Errorf("destination %q is not a regular file", path)
		}
		snapshot.exists = true
		snapshot.mode = info.Mode() & supportedModeBits
		platform, platformErr := snapshotPlatformMetadata(path, info)
		if platformErr != nil {
			return metadataSnapshot{}, fmt.Errorf("snapshot file metadata: %w", platformErr)
		}
		snapshot.platform = platform
	}
	if explicitMode != nil {
		snapshot.mode = *explicitMode & supportedModeBits
	}
	return snapshot, nil
}

func commit(path, stagedPath string) error {
	defer os.Remove(stagedPath)
	if err := os.Rename(stagedPath, path); err != nil {
		return fmt.Errorf("replace destination: %w", err)
	}
	if err := SyncDir(filepath.Dir(path)); err != nil {
		return fmt.Errorf("sync parent directory: %w", err)
	}
	return nil
}

// SyncDir fsyncs a directory so preceding entry changes become durable.
func SyncDir(path string) error {
	directory, err := os.Open(path)
	if err != nil {
		return err
	}
	defer directory.Close()
	return directory.Sync()
}
