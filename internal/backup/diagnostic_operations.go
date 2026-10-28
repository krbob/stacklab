package backup

import (
	"regexp"
	"strings"
)

// These are protocol shapes, shared by the evidence fold and liveness monitor.
// 0.14 has retries only, 0.16 adds recovery, and 0.17 adds terminal failures.
var resticBackendRetry = regexp.MustCompile(`^((?:list|load|stat|save|remove)\(.+\)) returned error, retrying after [^:]+: (.+)$`)
var resticBackendSuccess = regexp.MustCompile(`^((?:list|load|stat|save|remove)\(.+\)) operation successful after [0-9]+ retries$`)
var resticBackendTerminal = regexp.MustCompile(`^((?:list|load|stat)\(.+\)) failed: (.+)$`)
var resticLegacyDownload = regexp.MustCompile(`^pack [a-f0-9]{8,64} failed to download: (.+)$`)

type backendDiagnostic struct {
	key, cause, phase string
}

func parseBackendDiagnostic(line string) (backendDiagnostic, bool) {
	if match := resticBackendRetry.FindStringSubmatch(line); match != nil {
		return backendDiagnostic{match[1], match[2], "retry"}, true
	}
	if match := resticBackendSuccess.FindStringSubmatch(line); match != nil {
		return backendDiagnostic{key: match[1], phase: "recovered"}, true
	}
	if match := resticBackendTerminal.FindStringSubmatch(line); match != nil {
		return backendDiagnostic{match[1], match[2], "failed"}, true
	}
	return backendDiagnostic{}, false
}

func (op backendDiagnostic) read() bool {
	return strings.HasPrefix(op.key, "list(") || strings.HasPrefix(op.key, "load(") || strings.HasPrefix(op.key, "stat(")
}

func explicitResticDamage(line string) bool {
	return resticIntegrityFailure(line, resticInventoryFailure{packs: true, indexes: true})
}

func (e *diagnosticEvidence) backend(op backendDiagnostic) {
	if op.phase == "recovered" {
		delete(e.pendingOperations, op.key)
		return
	}
	if !op.read() {
		return // Lock refresh/removal is not repository evidence.
	}
	if explicitResticDamage(op.cause) {
		e.explicitDamage = true
	}
	if op.phase == "retry" {
		if len(op.key) > diagnosticCauseBytes || len(op.cause) > diagnosticCauseBytes || (len(e.pendingOperations) >= diagnosticCauseLimit && e.pendingOperations[op.key] == "") {
			e.incomplete = true
			return
		}
		if e.pendingOperations == nil {
			e.pendingOperations = make(map[string]string)
		}
		e.pendingOperations[op.key] = op.cause
		return
	}
	delete(e.pendingOperations, op.key)
	switch {
	case op.key == "list(data)":
		e.inventory.packs = true
	case op.key == "list(index)" || strings.HasPrefix(op.key, "load(<index/"):
		e.inventory.indexes = true
	}
	e.remember(diagnosticCause(op.cause), true)
	if !e.operational(op.cause) {
		e.transport = true
	}
	e.explained = true
}

// Legacy restic emits the exhausted cause without a terminal operation wrapper.
// Require equality with a pending cause, not a transport word elsewhere in output.
func (e *diagnosticEvidence) legacyFailure(cause string) bool {
	matched := false
	for key, pending := range e.pendingOperations {
		if diagnosticCause(pending) == cause {
			e.backend(backendDiagnostic{key, pending, "failed"})
			matched = true
		}
	}
	return matched
}

func (e *diagnosticEvidence) uncertainInventory() resticInventoryFailure {
	result := e.inventory
	for key := range e.pendingOperations {
		result.packs = result.packs || key == "list(data)"
		result.indexes = result.indexes || key == "list(index)" || strings.HasPrefix(key, "load(<index/")
	}
	return result
}

func resticCacheHousekeeping(line string) bool {
	line = strings.TrimPrefix(line, "error: ")
	return strings.HasPrefix(line, "error removing temporary cache directory: ") ||
		strings.HasPrefix(line, "unable to create temporary directory for cache during check, disabling cache: ")
}
