package backup

import (
	"regexp"
	"strings"
)

// These are protocol shapes, shared by the evidence fold and liveness monitor.
// 0.14 has retries only, 0.16 adds recovery, and 0.17 adds terminal failures.
var resticBackendRetry = regexp.MustCompile(`^((?:list|load|stat|save|remove)\(.+\)) returned error, retrying after [^:]+: (.+)$`)
var resticBackendSuccess = regexp.MustCompile(`^((?:list|load|stat|save|remove)\(.+\)) operation successful after [0-9]+ retries$`)
var resticBackendTerminal = regexp.MustCompile(`^((?:list|load|stat|save|remove)\(.+\)) failed: (.+)$`)
var resticLegacyDownload = regexp.MustCompile(`^pack [a-f0-9]{8,64} failed to download: (.+)$`)
var resticReadFullFailure = regexp.MustCompile(`^readfull\((<[^>]+>)\): (.+)$`)

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
	if match := resticReadFullFailure.FindStringSubmatch(line); match != nil {
		return backendDiagnostic{"load(" + match[1] + ")", match[2], "failed"}, true
	}
	return backendDiagnostic{}, false
}

func (op backendDiagnostic) read() bool {
	return strings.HasPrefix(op.key, "list(") || strings.HasPrefix(op.key, "load(") || strings.HasPrefix(op.key, "stat(")
}

func (op backendDiagnostic) vanishedLock() bool {
	return (strings.HasPrefix(op.key, "load(<lock/") || strings.HasPrefix(op.key, "stat(<lock/")) && missingBackendObject(op.cause)
}

func explicitResticDamage(line string) bool {
	return resticIntegrityFailure(line, resticInventoryFailure{packs: true, indexes: true})
}

type pendingDiagnostic struct {
	cause    string
	sequence uint64
}

func inventoryOperation(key string) bool {
	return key == "list(data)" || key == "list(index)" || strings.HasPrefix(key, "load(<index/")
}

// Retry correlation is expendable; diagnostic evidence is not. Keep inventory
// ambiguity separately even if bounded correlation entries must be evicted.
func (e *diagnosticEvidence) retainRetry(op backendDiagnostic) {
	if len(op.key) > diagnosticCauseBytes || len(op.cause) > diagnosticCauseBytes {
		e.retainUntrackedInventory(op.key)
		return
	}
	if e.pendingOperations == nil {
		e.pendingOperations = make(map[string]pendingDiagnostic)
	}
	if _, exists := e.pendingOperations[op.key]; !exists && len(e.pendingOperations) >= diagnosticCauseLimit {
		oldest := ""
		var sequence uint64
		for key, entry := range e.pendingOperations {
			if oldest == "" || (!inventoryOperation(key) && inventoryOperation(oldest)) ||
				(inventoryOperation(key) == inventoryOperation(oldest) && entry.sequence < sequence) {
				oldest, sequence = key, entry.sequence
			}
		}
		e.retainUntrackedInventory(oldest)
		delete(e.pendingOperations, oldest)
	}
	e.retrySequence++
	e.pendingOperations[op.key] = pendingDiagnostic{op.cause, e.retrySequence}
}

func (e *diagnosticEvidence) retainUntrackedInventory(key string) {
	if key == "list(data)" {
		e.pendingInventory.packs = true
	}
	if key == "list(index)" || strings.HasPrefix(key, "load(<index/") {
		e.pendingInventory.indexes = true
	}
}

// Pack diagnostics follow completion of listing; snapshot/tree/data reads follow
// index loading and pack inventory in restic check. Retire only unresolved retry
// correlations. A previously reported terminal inventory failure remains evidence.
func (e *diagnosticEvidence) advanceCheckPhase(line string) {
	if op, ok := parseBackendDiagnostic(line); ok && op.key == "list(data)" && !e.unknown && !e.incomplete && len(e.unmatchedCauses) == 0 {
		// Restic aborts on LoadIndex failure before starting Packs/List(data).
		// Retire only pending index retries, never established terminal failures.
		e.pendingInventory.indexes = false
		for key := range e.pendingOperations {
			if key == "list(index)" || strings.HasPrefix(key, "load(<index/") {
				delete(e.pendingOperations, key)
			}
		}
	}
	packs := strings.HasPrefix(line, "pack ")
	structure := strings.HasPrefix(line, "error for tree ") || strings.HasPrefix(line, "error: failed to load snapshot ") ||
		strings.HasPrefix(line, "load(<snapshot/") || strings.HasPrefix(line, "list(snapshot)") ||
		strings.HasPrefix(line, "load(<data/") || strings.HasPrefix(line, "readfull(<data/")
	if (!packs && !structure) || e.unknown || e.incomplete || len(e.unmatchedCauses) > 0 {
		return
	}
	e.pendingInventory = resticInventoryFailure{}
	for key := range e.pendingOperations {
		if inventoryOperation(key) {
			delete(e.pendingOperations, key)
		}
	}
}

func missingBackendObject(cause string) bool {
	return containsDiagnostic(cause, "no such file or directory", "specified key does not exist", "nosuchkey") ||
		cause == "not found" || cause == "does not exist" || cause == "file does not exist" ||
		strings.HasSuffix(cause, ": file does not exist") || resticMissingBackendObject.MatchString(cause)
}

var resticMissingBackendObject = regexp.MustCompile(`^<(?:lock|data|index|snapshot|key)/[a-f0-9]{8,64}> does not exist$`)

func (e *diagnosticEvidence) backend(op backendDiagnostic) {
	if op.phase == "recovered" {
		delete(e.pendingOperations, op.key)
		if op.key == "list(data)" {
			e.pendingInventory.packs = false
		}
		return
	}
	if !op.read() {
		if !strings.Contains(op.key, "(<lock/") && op.phase == "failed" {
			e.explained = e.operational(op.cause) || e.explained
		}
		return // Writes do not establish check evidence.
	}
	if explicitResticDamage(op.cause) {
		e.explicitDamage = true
	}
	if op.phase == "retry" {
		if op.vanishedLock() && !e.invocation.lockInspection() {
			return // A foreign lock may disappear during normal lock acquisition.
		}
		e.retainRetry(op)
		return
	}
	if diagnosticCause(op.cause) == "context canceled" || diagnosticCause(op.cause) == "context cancelled" {
		return // An individual restic worker cancellation does not diagnose storage.
	}
	if missingBackendObject(op.cause) && (strings.HasPrefix(op.key, "load(<") || strings.HasPrefix(op.key, "stat(<")) {
		if op.vanishedLock() {
			e.missingLock = true
			delete(e.pendingOperations, op.key)
		} else {
			e.unknown = true
		}
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
		if diagnosticCause(pending.cause) == cause {
			e.backend(backendDiagnostic{key, pending.cause, "failed"})
			matched = true
		}
	}
	return matched
}

func (e *diagnosticEvidence) uncertainInventory() resticInventoryFailure {
	result := resticInventoryFailure{
		packs:   e.inventory.packs || e.pendingInventory.packs,
		indexes: e.inventory.indexes || e.pendingInventory.indexes,
	}
	for key := range e.pendingOperations {
		result.packs = result.packs || key == "list(data)"
		result.indexes = result.indexes || key == "list(index)" || strings.HasPrefix(key, "load(<index/")
	}
	return result
}

// Legacy listing errors are emitted before pack diagnostics. Associate only an
// unwrapped, operational failure with that still-pending listing. Never match a
// later snapshot error to an inventory operation by coincidental message equality.
func (e *diagnosticEvidence) legacyListingFailure(line string) bool {
	if strings.HasPrefix(line, "error:") || strings.HasPrefix(line, "fatal:") ||
		resticRepositoryObjectDiagnostic.MatchString(line) || strings.Contains(line, " failed:") {
		return false
	}
	if _, ok := e.pendingOperations["list(data)"]; ok && resticTransportFailure(line) {
		e.backend(backendDiagnostic{"list(data)", line, "failed"})
		return true
	}
	return false
}

func resticCacheHousekeeping(line string) bool {
	line = strings.TrimPrefix(line, "error: ")
	return strings.HasPrefix(line, "error removing temporary cache directory: ") ||
		strings.HasPrefix(line, "unable to create temporary directory for cache during check, disabling cache: ") ||
		strings.HasPrefix(line, "unable to open cache: ") ||
		strings.HasPrefix(line, "unable to find old cache directories: ") ||
		(strings.HasPrefix(line, "unable to create cache directory ") && strings.Contains(line, ", disabling cache: ")) ||
		strings.HasPrefix(line, "using temporary cache in ")
}
