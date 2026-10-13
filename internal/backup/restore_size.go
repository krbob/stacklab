package backup

import (
	"bytes"
	"context"
	"encoding/json"
	"fmt"
)

// For source/file selections, estimate the actual selected archive objects from
// authenticated restic tree metadata, not the application's supplied manifest.
// Native stats counts the entire snapshot and has no file-selection filter.
func (s *Service) restoreSize(ctx context.Context, d Destination, snapshot string, selected []string) (int64, error) {
	if len(selected) == 0 {
		output, err := s.restic(ctx, d, "", "stats", "--mode", "restore-size", snapshot)
		if err != nil {
			return 0, err
		}
		var stats struct {
			TotalSize int64 `json:"total_size"`
		}
		if json.Unmarshal(output, &stats) != nil || stats.TotalSize < 0 || stats.TotalSize > 1<<60 {
			return 0, fmt.Errorf("invalid restore size estimate")
		}
		return stats.TotalSize, nil
	}
	output, err := s.restic(ctx, d, "", "ls", "--recursive", snapshot)
	if err != nil {
		return 0, err
	}
	remaining := map[string]bool{}
	for _, path := range selected {
		remaining[path] = true
	}
	var total int64
	for _, line := range bytes.Split(output, []byte("\n")) {
		if len(bytes.TrimSpace(line)) == 0 {
			continue
		}
		var node struct {
			Path string `json:"path"`
			Type string `json:"type"`
			Size int64  `json:"size"`
		}
		if json.Unmarshal(line, &node) != nil {
			return 0, fmt.Errorf("invalid repository file listing")
		}
		if !remaining[node.Path] {
			continue
		}
		if node.Type != "file" || node.Size < 0 || node.Size > 1<<60 || total > (1<<60)-node.Size {
			return 0, fmt.Errorf("invalid selected recovery object size")
		}
		total += node.Size
		delete(remaining, node.Path)
	}
	if len(remaining) > 0 {
		return 0, fmt.Errorf("selected recovery object is missing from snapshot")
	}
	return total, nil
}
