package backup

import (
	"encoding/json"
	"regexp"
	"strings"
)

var resticEOFDiagnostic = regexp.MustCompile(`\beof\b`)
var resticHTTPFailureDiagnostic = regexp.MustCompile(`\b(?:http(?:/[^ ]+)?[ :]+|status(?: code)?[ :=]+)(?:500|502|503|504)\b`)
var resticMissingPackDiagnostic = regexp.MustCompile(`pack [a-f0-9]{8,64}: does not exist`)
var resticPackSizeDiagnostic = regexp.MustCompile(`pack [a-f0-9]{8,64}: unexpected file size:`)
var resticMissingBlobDiagnostic = regexp.MustCompile(`id [a-f0-9]{8,64} not found in repository`)
var resticInvalidNodeDiagnostic = regexp.MustCompile(`node .+ with invalid type `)
var resticRepositoryObjectDiagnostic = regexp.MustCompile(`^(?:error: |error for )?(?:blob|tree|pack|index|snapshot)\b`)

type resticDiagnosticRecord struct {
	message     string
	errorRecord bool
	summary     bool
}

type resticDiagnostics []resticDiagnosticRecord

// Restic's JSON printer includes the format string's trailing newline in each
// record. That terminates a message, not an error-for-tree group. Keep records
// and explicit summaries instead of interpreting the extra newline as a boundary.
func parseResticDiagnostics(diagnostic string) resticDiagnostics {
	var records resticDiagnostics
	for _, line := range strings.Split(diagnostic, "\n") {
		var encoded struct {
			MessageType string `json:"message_type"`
			Message     string `json:"message"`
			NumErrors   int    `json:"num_errors"`
		}
		record := resticDiagnosticRecord{message: line}
		if strings.HasPrefix(strings.TrimSpace(line), "{") && json.Unmarshal([]byte(line), &encoded) == nil && encoded.MessageType != "" {
			record.message = encoded.Message
			record.errorRecord = encoded.MessageType == "error"
			record.summary = encoded.MessageType == "summary" || encoded.MessageType == "exit_error"
			if encoded.MessageType == "summary" && encoded.NumErrors > 0 {
				record.message = "repository contains errors"
			}
		}
		var lines []string
		for _, message := range strings.Split(strings.ToLower(record.message), "\n") {
			// A retried listing is not a failed inventory. A terminal error must
			// follow if the retries fail; retain neither its warning nor its cause.
			if strings.TrimSpace(message) != "" && !strings.Contains(message, "returned error, retrying after") {
				lines = append(lines, message)
			}
		}
		record.message = strings.Join(lines, "\n")
		if record.message != "" || record.summary {
			records = append(records, record)
		}
	}
	return records
}

func (records resticDiagnostics) text() string {
	var messages []string
	for _, record := range records {
		messages = append(messages, record.message)
	}
	return strings.Join(messages, "\n")
}

func containsDiagnostic(diagnostic string, patterns ...string) bool {
	for _, pattern := range patterns {
		if strings.Contains(diagnostic, pattern) {
			return true
		}
	}
	return false
}

func resticTransportFailure(diagnostic string) bool {
	return containsDiagnostic(diagnostic, "connection refused", "connection reset", "connection closed", "connection lost", "closed by remote host", "ssh command exited: exit status 255", "circuit breaker open", "no such host", "timeout", "timed out", "network is unreachable", "no route to host", "broken pipe", "server closed idle connection", "service unavailable", "serviceunavailable", "internal server error", "bad gateway", "slowdown", "requesttimeout") || resticEOFDiagnostic.MatchString(diagnostic) || resticHTTPFailureDiagnostic.MatchString(diagnostic)
}

func resticOperationalFailure(diagnostic string) bool {
	return resticTransportFailure(diagnostic) || containsDiagnostic(diagnostic, "permission denied", "access denied", "no space left on device", "quota exceeded", "repository is already locked", "unable to create lock", "wrong password", "no key found", "unable to open config file")
}

type resticInventoryFailure struct{ packs, indexes bool }

func (records resticDiagnostics) inventoryFailure() resticInventoryFailure {
	var failure resticInventoryFailure
	for _, line := range strings.Split(records.text(), "\n") {
		// Only a terminal inventory/index failure compromises absence evidence.
		// Pack downloads and lock-refresh warnings happen independently.
		if !containsDiagnostic(line, "failed", "error") {
			continue
		}
		if strings.Contains(line, "list(data)") {
			failure.packs = true
		}
		if containsDiagnostic(line, "list(index)", "load indexes", "load index ", "load(<index/") {
			failure.indexes = true
		}
	}
	return failure
}

func resticMissingReference(diagnostic string) bool {
	return resticMissingPackDiagnostic.MatchString(diagnostic) || resticMissingBlobDiagnostic.MatchString(diagnostic) || containsDiagnostic(diagnostic, "not found in index", "pack is empty or not indexed", "is not contained in index or position is incorrect")
}

func resticIntegrityFailure(diagnostic string, inventory resticInventoryFailure) bool {
	if resticPackSizeDiagnostic.MatchString(diagnostic) || resticInvalidNodeDiagnostic.MatchString(diagnostic) || containsDiagnostic(diagnostic, "ciphertext verification failed", "invalid data returned", "unexpected pack id", "pack id does not match", "pack header size does not match", "hash does not match", "checksum does not match", "index for pack contains gaps / overlapping blobs", "has nil blob list", "has null id", "has no subtree", "subtree id is null", "node with empty name") {
		return true
	}
	for _, line := range strings.Split(diagnostic, "\n") {
		// Normal EOF before the expected length in local/SFTP pack reads is
		// truncation, unlike a connection interrupted during the download.
		if strings.Contains(line, "file is too short") && containsDiagnostic(line, "load(<data/", "download error:", "pack ") {
			return true
		}
	}
	return (!inventory.packs && resticMissingPackDiagnostic.MatchString(diagnostic)) ||
		(!inventory.indexes && (resticMissingBlobDiagnostic.MatchString(diagnostic) || containsDiagnostic(diagnostic, "not found in index", "pack is empty or not indexed", "is not contained in index or position is incorrect")))
}

func (records resticDiagnostics) unexplainedRepositoryFailure() bool {
	treeErrors := false
	for _, record := range records {
		if record.summary {
			treeErrors = false
			continue
		}
		for _, line := range strings.Split(record.message, "\n") {
			trimmed := strings.TrimSpace(line)
			if strings.HasPrefix(trimmed, "error for tree ") {
				treeErrors = true
				if separator := strings.Index(trimmed, ":"); separator >= 0 {
					trimmed = strings.TrimSpace(trimmed[separator+1:])
				}
				if trimmed == "" {
					continue
				}
			} else if strings.HasPrefix(trimmed, "the repository ") || strings.HasPrefix(trimmed, "fatal:") || trimmed == "repository contains errors" {
				treeErrors = false
				continue
			}
			// Missing references are resolved against the relevant inventory by
			// resticIntegrityFailure. Operational errors explain this record only.
			if resticMissingReference(trimmed) || resticOperationalFailure(trimmed) {
				continue
			}
			if treeErrors || record.errorRecord || resticRepositoryObjectDiagnostic.MatchString(trimmed) || containsDiagnostic(trimmed, "list(data) failed", "list(index) failed", "load indexes failed") {
				return true
			}
		}
	}
	return false
}
