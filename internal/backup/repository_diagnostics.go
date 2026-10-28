package backup

import (
	"encoding/json"
	"regexp"
	"strconv"
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
			// Preserve operation lifecycle records for both evidence and liveness.
			if strings.TrimSpace(message) != "" {
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

func containsDiagnostic(diagnostic string, patterns ...string) bool {
	for _, pattern := range patterns {
		if strings.Contains(diagnostic, pattern) {
			return true
		}
	}
	return false
}

func resticTransportFailure(diagnostic string) bool {
	return containsDiagnostic(diagnostic, "connection refused", "connection reset", "connection closed", "connection lost", "closed by remote host", "ssh command exited: exit status 255", "circuit breaker open", "no such host", "timeout", "timed out", "network is unreachable", "no route to host", "broken pipe", "server closed idle connection", "service unavailable", "serviceunavailable", "internal server error", "bad gateway", "slowdown", "requesttimeout", "context deadline exceeded", "use of closed network connection", "server misbehaving", "temporary failure in name resolution", "server sent goaway", "too many requests", "ssh command exited: signal: killed") || resticEOFDiagnostic.MatchString(diagnostic) || resticHTTPFailureDiagnostic.MatchString(diagnostic)
}

type resticInventoryFailure struct{ packs, indexes bool }

func resticMissingReference(diagnostic string) bool {
	return resticMissingPackDiagnostic.MatchString(diagnostic) || resticMissingBlobDiagnostic.MatchString(diagnostic) || containsDiagnostic(diagnostic, "not found in index", "pack is empty or not indexed", "is not contained in index or position is incorrect")
}

func resticIntegrityFailure(diagnostic string, inventory resticInventoryFailure) bool {
	if resticPackSizeDiagnostic.MatchString(diagnostic) || resticInvalidNodeDiagnostic.MatchString(diagnostic) || containsDiagnostic(diagnostic, "ciphertext verification failed", "invalid data returned", "unexpected pack id", "pack id does not match", "pack header size does not match", "hash does not match", "checksum does not match", "index for pack contains gaps / overlapping blobs", "has nil blob list", "has null id", "has no subtree", "subtree id is null", "node with empty name", "wrong data returned, hash is", "hash mismatch", "data mismatch", "invalid blob length", "overlapping blobs in pack") || (strings.Contains(diagnostic, "decompressing blob") && strings.Contains(diagnostic, "failed")) {
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

// Operation context is stronger evidence than a provider's choice of error words.
var resticPackErrors = regexp.MustCompile(`^pack [a-f0-9]{8,64} contains ([0-9]+) errors: \[(.*)\]$`)
var resticQuotedText = regexp.MustCompile(`"(?:\\.|[^"\\])*"`)

const diagnosticCauseLimit = 128
const diagnosticCauseBytes = 4096

type diagnosticEvidence struct {
	inventory                                                              resticInventoryFailure
	explicitDamage, missingPack, missingBlob                               bool
	treeErrors, summary, explained, unknown, incomplete                    bool
	transport, locked, credentials, storage, permission, missingRepository bool
	operationCauses                                                        map[string]bool
	pendingOperations                                                      map[string]string
	unmatchedCauses                                                        map[string]bool
	cancelled                                                              bool
}

func diagnosticCause(line string) string {
	for _, prefix := range []string{"error: ", "partial download error: ", "download error: ", "streampack: "} {
		line = strings.TrimPrefix(line, prefix)
	}
	return strings.TrimSpace(line)
}

func (e *diagnosticEvidence) remember(cause string, operation bool) {
	if len(cause) > diagnosticCauseBytes {
		e.incomplete = true
		return
	}
	if operation {
		if e.operationCauses == nil {
			e.operationCauses = map[string]bool{}
		}
		if len(e.operationCauses) >= diagnosticCauseLimit && !e.operationCauses[cause] {
			e.incomplete = true
			return
		}
		e.operationCauses[cause] = true
		delete(e.unmatchedCauses, cause)
	} else if !e.operationCauses[cause] {
		if e.unmatchedCauses == nil {
			e.unmatchedCauses = map[string]bool{}
		}
		if len(e.unmatchedCauses) >= diagnosticCauseLimit && !e.unmatchedCauses[cause] {
			e.incomplete = true
			return
		}
		e.unmatchedCauses[cause] = true
	}
}

func (e *diagnosticEvidence) operational(line string) bool {
	transport := resticTransportFailure(line)
	locked := containsDiagnostic(line, "repository is already locked", "unable to create lock")
	credentials := containsDiagnostic(line, "wrong password", "no key found")
	storage := containsDiagnostic(line, "no space left on device", "quota exceeded")
	permission := containsDiagnostic(line, "permission denied", "access denied")
	missing := containsDiagnostic(line, "unable to open config file", "is there a repository at")
	e.transport = e.transport || transport
	e.locked = e.locked || locked
	e.credentials = e.credentials || credentials
	e.storage = e.storage || storage
	e.permission = e.permission || permission
	e.missingRepository = e.missingRepository || missing
	return transport || locked || credentials || storage || permission || missing
}

func (e *diagnosticEvidence) add(record resticDiagnosticRecord) {
	for _, raw := range strings.Split(record.message, "\n") {
		line := strings.TrimSpace(resticQuotedText.ReplaceAllString(raw, `""`))
		if line == "" || resticCacheHousekeeping(line) {
			continue
		}
		if containsDiagnostic(line, "unable to refresh lock", "failed to refresh lock", "remove(<lock/") {
			continue
		}
		// Cancellation changes the provenance of subsequent output. Drained
		// workers cannot establish missing inventory or unknown errors, but
		// concrete corruption remains evidence even if it was buffered at stop.
		if e.cancelled {
			e.explicitDamage = e.explicitDamage || explicitResticDamage(line)
			continue
		}
		if op, ok := parseBackendDiagnostic(line); ok {
			e.backend(op)
			continue
		}
		if containsDiagnostic(line, "repository contains errors", "repository contains damaged pack files", "repository is damaged and must be repaired", "repository index is damaged and must be repaired") {
			e.summary = true
		}
		if containsDiagnostic(line, "the repository is damaged and must be repaired", "the repository contains damaged pack files", "the repository index is damaged and must be repaired") || strings.HasPrefix(line, "restic repair packs ") || strings.HasPrefix(line, "restic repair snapshots ") || strings.HasPrefix(line, "damaged pack files can be caused by ") {
			e.summary = true
			e.treeErrors = false
			continue
		}
		// Accumulate concrete evidence independently of output volume and order.
		if resticIntegrityFailure(line, resticInventoryFailure{packs: true, indexes: true}) {
			e.explicitDamage = true
		}
		e.missingPack = e.missingPack || resticMissingPackDiagnostic.MatchString(line)
		e.missingBlob = e.missingBlob || resticMissingBlobDiagnostic.MatchString(line) || containsDiagnostic(line, "not found in index", "pack is empty or not indexed", "is not contained in index or position is incorrect")
		if record.summary {
			// exit_error is a terminal record, but can still carry a concrete
			// corruption or access diagnosis. It only ends the tree grouping.
			e.treeErrors = false
			e.explained = e.operational(line) || e.explained
			continue
		}
		if legacy := resticLegacyDownload.FindStringSubmatch(line); legacy != nil {
			e.backend(backendDiagnostic{"load(<data/legacy>)", legacy[1], "failed"})
			continue
		}
		if containsDiagnostic(line, "load indexes", "load index ", "load(<index/") && containsDiagnostic(line, "failed", "error") {
			e.inventory.indexes = true
		}
		if strings.HasPrefix(line, "error for tree ") {
			e.treeErrors = true
			if separator := strings.IndexByte(line, ':'); separator >= 0 {
				line = strings.TrimSpace(line[separator+1:])
			}
			if line == "" {
				continue
			}
		} else if strings.HasPrefix(line, "fatal:") || line == "repository contains errors" {
			e.treeErrors = false
			// Fatal operational failures still identify access/credential failures.
			e.explained = e.operational(line) || e.explained
			continue
		}
		if diagnosticCause(line) == "context canceled" || diagnosticCause(line) == "context cancelled" || (strings.HasPrefix(line, "readfull(<") && strings.HasSuffix(line, "): context canceled")) {
			continue
		}
		if resticMissingReference(line) {
			continue
		}
		cause := diagnosticCause(line)
		if e.operationCauses[cause] || e.legacyFailure(cause) {
			e.explained = true
			continue
		}
		object := e.treeErrors || resticRepositoryObjectDiagnostic.MatchString(line)
		if pack := resticPackErrors.FindStringSubmatch(line); pack != nil {
			count, _ := strconv.Atoi(pack[1])
			// fmt's error slice has no unambiguous separator. Multiple errors must not
			// all be explained by a transport word in just one member.
			if count != 1 {
				e.unknown = true
				continue
			}
			cause = pack[2]
			if !strings.HasPrefix(cause, "download error:") && !strings.HasPrefix(cause, "partial download error:") {
				e.unknown = true
				continue
			}
		}
		// Decoding is about an object, not the connection that delivered it. Names
		// quoted in tree diagnostics must never supply a transport/credential word.
		objectOperation := containsDiagnostic(line, "download error:", "failed to load ", "load(<", "readfull(<") || containsDiagnostic(cause, "download error:", "failed to load ", "load(<", "readfull(<")
		if object && (!objectOperation || containsDiagnostic(cause, "decode failed", "decoding", "unexpected length")) {
			e.unknown = true
			continue
		}
		cause = diagnosticCause(cause)
		if e.operationCauses[cause] || e.legacyFailure(cause) {
			e.explained = true
			continue
		}
		if e.operational(cause) {
			e.explained = true
			continue
		}
		if object && objectOperation {
			e.remember(cause, false)
			continue
		}
		if record.errorRecord || strings.HasPrefix(line, "error: ") {
			e.remember(cause, false)
		}
	}
}

func (e *diagnosticEvidence) integrityFailure() bool {
	inventory := e.uncertainInventory()
	return e.explicitDamage || (e.missingPack && !inventory.packs) || (e.missingBlob && !inventory.indexes)
}
func (e *diagnosticEvidence) unexplainedFailure() bool {
	inventory := e.uncertainInventory()
	ambiguousMissing := (e.missingPack && inventory.packs && !e.inventory.packs) || (e.missingBlob && inventory.indexes && !e.inventory.indexes)
	return e.incomplete || e.unknown || ambiguousMissing || len(e.unmatchedCauses) > 0 || (e.summary && !e.explained)
}
