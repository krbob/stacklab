package backup

import (
	"archive/tar"
	"errors"
	"fmt"
	"io"
	"path"
	"strings"
)

// ValidateCapturedArchive checks the complete stream with bounded memory. An
// archive is an offline recovery artifact even when privileged online extraction
// cannot safely reproduce its links, special files or privilege metadata.
func ValidateCapturedArchive(reader io.Reader) ([]string, error) {
	archive := tar.NewReader(reader)
	restrictions := []string{}
	entries := 0
	seen := map[string]bool{}
	restrict := func(category, name string) {
		if !seen[category] {
			seen[category] = true
			restrictions = append(restrictions, fmt.Sprintf("%s (first entry: %q); recover this archive offline", category, name))
		}
	}
	for {
		header, err := archive.Next()
		if errors.Is(err, io.EOF) {
			return restrictions, nil
		}
		if err != nil {
			return nil, err
		}
		entries++
		if entries > 1000000 {
			restrict("archive exceeds the automatic extraction entry limit", header.Name)
		}
		name := path.Clean(header.Name)
		if len(header.Name) > 4096 || len(header.Linkname) > 4096 || strings.ContainsRune(name, 0) || path.IsAbs(name) || name == ".." || strings.HasPrefix(name, "../") {
			return nil, fmt.Errorf("archive contains an invalid member path: %q", header.Name)
		}
		if header.Uid < 0 || header.Gid < 0 || header.Mode < 0 || header.Mode&^07777 != 0 {
			return nil, fmt.Errorf("invalid ownership or permissions for %q", name)
		}
		if strings.Contains(name, "\\") {
			restrict("member name contains a backslash", name)
		}
		if header.Mode&04000 != 0 || header.Mode&02000 != 0 && header.Typeflag != tar.TypeDir {
			restrict("setuid or setgid file permissions", name)
		}
		if _, exists := header.Xattrs["security.capability"]; exists {
			restrict("file capabilities", name)
		}
		for key := range header.PAXRecords {
			if strings.HasSuffix(key, ".security.capability") {
				restrict("file capabilities", name)
			}
		}
		switch header.Typeflag {
		case tar.TypeReg, tar.TypeRegA, tar.TypeDir:
		case tar.TypeSymlink:
			resolved := path.Clean(path.Join(path.Dir(name), header.Linkname))
			if path.IsAbs(header.Linkname) || resolved == ".." || strings.HasPrefix(resolved, "../") {
				restrict("symlink points outside the source", name)
			}
		case tar.TypeLink:
			link := path.Clean(header.Linkname)
			if path.IsAbs(link) || link == ".." || strings.HasPrefix(link, "../") {
				restrict("hard link points outside the source", name)
			}
		default:
			restrict("special file metadata", name)
		}
	}
}
