//go:build linux || darwin

package atomicfile

import (
	"bytes"
	"errors"
	"fmt"
	"os"
	"strings"
	"syscall"

	"golang.org/x/sys/unix"
)

const maxExtendedMetadataBytes = 1 << 20

type extendedAttribute struct {
	name  string
	value []byte
}

type platformMetadata struct {
	uid        int
	gid        int
	attributes []extendedAttribute
}

func snapshotPlatformMetadata(path string, info os.FileInfo) (platformMetadata, error) {
	stat, ok := info.Sys().(*syscall.Stat_t)
	if !ok {
		return platformMetadata{}, errors.New("file ownership metadata is unavailable")
	}
	attributes, err := readExtendedAttributes(path)
	if err != nil {
		return platformMetadata{}, err
	}
	return platformMetadata{uid: int(stat.Uid), gid: int(stat.Gid), attributes: attributes}, nil
}

func applyPlatformMetadata(file *os.File, path string, metadata platformMetadata, ownerGroupPolicy ownerGroupPolicy) error {
	return applyPlatformMetadataWithChown(file, path, metadata, ownerGroupPolicy, file.Chown)
}

func applyPlatformMetadataWithChown(file *os.File, path string, metadata platformMetadata, ownerGroupPolicy ownerGroupPolicy, chown func(int, int) error) error {
	info, err := file.Stat()
	if err != nil {
		return fmt.Errorf("stat temporary file ownership: %w", err)
	}
	stat, ok := info.Sys().(*syscall.Stat_t)
	if !ok {
		return errors.New("temporary file ownership metadata is unavailable")
	}
	if int(stat.Uid) != metadata.uid || int(stat.Gid) != metadata.gid {
		if err := chown(metadata.uid, metadata.gid); err != nil {
			if ownerGroupPolicy != allowOwnerGroupAdoption || !errors.Is(err, os.ErrPermission) {
				return fmt.Errorf("preserve owner/group: %w", err)
			}
		}
	}
	if err := replaceExtendedAttributes(path, metadata.attributes); err != nil {
		return fmt.Errorf("preserve extended attributes: %w", err)
	}
	return nil
}

func readExtendedAttributes(path string) ([]extendedAttribute, error) {
	names, err := listExtendedAttributeNames(path)
	if err != nil {
		if extendedAttributesUnsupported(err) {
			return nil, nil
		}
		return nil, fmt.Errorf("list xattrs: %w", err)
	}
	attributes := make([]extendedAttribute, 0, len(names))
	totalBytes := 0
	for _, name := range names {
		value, err := readExtendedAttribute(path, name)
		if err != nil {
			if errors.Is(err, unix.ENODATA) {
				continue
			}
			return nil, fmt.Errorf("read xattr %q: %w", name, err)
		}
		totalBytes += len(name) + len(value)
		if totalBytes > maxExtendedMetadataBytes {
			return nil, fmt.Errorf("extended metadata exceeds %d bytes", maxExtendedMetadataBytes)
		}
		attributes = append(attributes, extendedAttribute{name: name, value: value})
	}
	return attributes, nil
}

func replaceExtendedAttributes(path string, desired []extendedAttribute) error {
	existing, err := listExtendedAttributeNames(path)
	if err != nil && !extendedAttributesUnsupported(err) {
		return fmt.Errorf("list temporary xattrs: %w", err)
	}
	existingValues := make(map[string][]byte, len(existing))
	for _, name := range existing {
		value, readErr := readExtendedAttribute(path, name)
		if readErr != nil {
			if errors.Is(readErr, unix.ENODATA) {
				continue
			}
			return fmt.Errorf("read temporary xattr %q: %w", name, readErr)
		}
		existingValues[name] = value
	}
	desiredNames := make(map[string]struct{}, len(desired))
	for _, attribute := range desired {
		desiredNames[attribute.name] = struct{}{}
	}
	for _, name := range existing {
		if _, keep := desiredNames[name]; keep {
			continue
		}
		if err := unix.Removexattr(path, name); err != nil && !errors.Is(err, unix.ENODATA) {
			return fmt.Errorf("remove inherited xattr %q: %w", name, err)
		}
	}
	for _, attribute := range desired {
		if value, exists := existingValues[attribute.name]; exists && bytes.Equal(value, attribute.value) {
			continue
		}
		if err := unix.Setxattr(path, attribute.name, attribute.value, 0); err != nil {
			return fmt.Errorf("set xattr %q: %w", attribute.name, err)
		}
	}
	return nil
}

func listExtendedAttributeNames(path string) ([]string, error) {
	size, err := unix.Listxattr(path, nil)
	if err != nil || size == 0 {
		return nil, err
	}
	if size > maxExtendedMetadataBytes {
		return nil, fmt.Errorf("xattr name list exceeds %d bytes", maxExtendedMetadataBytes)
	}
	buffer := make([]byte, size)
	size, err = unix.Listxattr(path, buffer)
	if err != nil {
		return nil, err
	}
	parts := strings.Split(string(buffer[:size]), "\x00")
	names := make([]string, 0, len(parts))
	for _, name := range parts {
		if name != "" {
			names = append(names, name)
		}
	}
	return names, nil
}

func readExtendedAttribute(path, name string) ([]byte, error) {
	for attempt := 0; attempt < 3; attempt++ {
		size, err := unix.Getxattr(path, name, nil)
		if err != nil {
			return nil, err
		}
		if size > maxExtendedMetadataBytes {
			return nil, fmt.Errorf("xattr value exceeds %d bytes", maxExtendedMetadataBytes)
		}
		value := make([]byte, size)
		read, err := unix.Getxattr(path, name, value)
		if errors.Is(err, unix.ERANGE) {
			continue
		}
		if err != nil {
			return nil, err
		}
		return value[:read], nil
	}
	return nil, unix.ERANGE
}

func extendedAttributesUnsupported(err error) bool {
	return errors.Is(err, unix.ENOTSUP) || errors.Is(err, unix.EOPNOTSUPP)
}
