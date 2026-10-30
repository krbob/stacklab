package backup

import (
	"regexp"
	"strings"
)

var rcloneNotice = regexp.MustCompile(`^rclone: [0-9]{4}/[0-9]{2}/[0-9]{2} [0-9]{2}:[0-9]{2}:[0-9]{2}(?:\.[0-9]+)? notice: (.+)$`)
var rcloneDefaultConfig = regexp.MustCompile(`^config file "[^"]+" not found - using defaults$`)
var fullRepositoryObjectID = regexp.MustCompile(`^[a-f0-9]{64}$`)

func (e *diagnosticEvidence) housekeeping(line string) bool {
	// A malformed/concatenated structured suffix must not disappear inside a
	// warning. Valid JSON suffixes are split by parseResticDiagnostics first.
	if strings.Contains(line, `{"`) {
		return false
	}
	if resticCacheHousekeeping(line) {
		e.cacheWarning = e.cacheWarning || !strings.HasPrefix(line, "using temporary cache in ")
		return true
	}
	if strings.HasPrefix(line, "error while unlocking: ") {
		e.cleanupWarning = true
		return true
	}
	match := rcloneNotice.FindStringSubmatch(line)
	if match == nil {
		return false
	}
	if rcloneDefaultConfig.MatchString(match[1]) || ignoredRcloneSymlink(match[1]) {
		e.backendNotice = true
		return true
	}
	return false
}

func ignoredRcloneSymlink(message string) bool {
	const suffix = ": can't follow symlink without -l/--copy-links"
	if !strings.HasSuffix(message, suffix) {
		return false
	}
	parts := strings.Split(strings.TrimSuffix(message, suffix), "/")
	// Restic ignores non-ID filenames in these object directories. A skipped
	// valid object, directory, config or unknown layout remains inconclusive.
	if len(parts) != 2 {
		return false
	}
	switch parts[0] {
	case "snapshots", "index", "keys", "locks":
		return parts[1] != "" && !strings.ContainsAny(parts[1], `"\`) && !fullRepositoryObjectID.MatchString(parts[1])
	default:
		return false
	}
}
