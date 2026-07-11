//go:build !linux && !darwin

package atomicfile

import "os"

type platformMetadata struct{}

func snapshotPlatformMetadata(string, os.FileInfo) (platformMetadata, error) {
	return platformMetadata{}, nil
}

func applyPlatformMetadata(*os.File, string, platformMetadata) error {
	return nil
}
