package backup

import (
	"context"
	"errors"
	"regexp"
	"strings"
	"time"
)

const checkBackendFailureTimeout = 10 * time.Minute

// Restic 0.14 does not report successful retries. A quiet interval must not
// turn one recovered retry into a deadline for an otherwise healthy check.
const checkFailureQuietReset = 6 * time.Minute

var errCheckBackendUnavailable = errors.New("repository check stopped after sustained or exhausted backend failures; restore destination access and retry")
var resticBackendRetry = regexp.MustCompile(`^((?:list|load|stat|save|remove)\(.+\)) returned error, retrying after [^:]+: .+$`)
var resticBackendSuccess = regexp.MustCompile(`^((?:list|load|stat|save|remove)\(.+\)) operation successful after [0-9]+ retries$`)
var resticBackendTerminal = regexp.MustCompile(`^(?:list|load|stat)\(.+\) failed: (.+)$`)

// A quiet, healthy check has no deadline here. Track only reported failed backend
// calls; another retry does not extend their budget. Recovery of that same call
// clears its deadline without masking concurrent failures on other objects.
type checkFailureTracker struct {
	pending     map[string]time.Time
	overflow    time.Time
	budget      time.Duration
	lastFailure time.Time
}

func (w *checkFailureTracker) observe(now time.Time, record resticDiagnosticRecord) bool {
	for _, raw := range strings.Split(record.message, "\n") {
		line := strings.TrimSpace(resticQuotedText.ReplaceAllString(raw, `""`))
		if match := resticBackendRetry.FindStringSubmatch(line); match != nil {
			if !w.lastFailure.IsZero() && now.Sub(w.lastFailure) > checkFailureQuietReset {
				w.pending = nil
				w.overflow = time.Time{}
			}
			w.lastFailure = now
			if w.pending == nil {
				w.pending = map[string]time.Time{}
			}
			if _, exists := w.pending[match[1]]; !exists {
				if len(w.pending) < diagnosticCauseLimit && len(match[1]) <= diagnosticCauseBytes {
					w.pending[match[1]] = now.Add(w.budget)
				} else if w.overflow.IsZero() {
					w.overflow = now.Add(w.budget)
				}
			}
			if deadline := w.deadline(); !deadline.IsZero() && !now.Before(deadline) {
				return true
			}
			continue
		}
		if match := resticBackendSuccess.FindStringSubmatch(line); match != nil {
			delete(w.pending, match[1])
			continue
		}
		if resticListFailure.MatchString(line) {
			return true
		}
		if match := resticBackendTerminal.FindStringSubmatch(line); match != nil && resticTransportFailure(match[1]) {
			return true
		}
		if containsDiagnostic(line, "download error:", "failed to load ") && resticTransportFailure(line) {
			return true
		}
	}
	return false
}

func (w *checkFailureTracker) deadline() time.Time {
	deadline := w.overflow
	for _, at := range w.pending {
		if deadline.IsZero() || at.Before(deadline) {
			deadline = at
		}
	}
	return deadline
}

// Consume on the stderr writer goroutine, after folding each record into the
// integrity evidence. Enforce the failure budget on a subsequent failure report,
// never on silence: legacy restic does not emit successful-retry messages.
type checkFailureWatchdog struct {
	tracker checkFailureTracker
	cancel  context.CancelCauseFunc
}

func newCheckFailureWatchdog(budget time.Duration, cancel context.CancelCauseFunc) *checkFailureWatchdog {
	return &checkFailureWatchdog{tracker: checkFailureTracker{budget: budget}, cancel: cancel}
}

func (w *checkFailureWatchdog) observe(record resticDiagnosticRecord) {
	if w.tracker.observe(time.Now(), record) {
		w.cancel(errCheckBackendUnavailable)
	}
}
