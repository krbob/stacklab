package backup

import (
	"context"
	"errors"
	"strings"
	"time"
)

const checkBackendFailureTimeout = 10 * time.Minute

// Expire each quiet operation separately. Legacy restic has no success record;
// failures of other objects must not keep a recovered operation's deadline alive.
const checkFailureQuietReset = 6 * time.Minute

var errCheckBackendUnavailable = errors.New("repository check stopped after sustained or exhausted backend failures; restore destination access and retry")

type checkFailureWindow struct{ deadline, lastFailure time.Time }

// A quiet, healthy check has no deadline here. Track only reported failed backend
// calls; another retry does not extend their budget. Recovery of that same call
// clears its deadline without masking concurrent failures on other objects.
type checkFailureTracker struct {
	pending map[string]checkFailureWindow
	budget  time.Duration
}

func (w *checkFailureTracker) observe(now time.Time, record resticDiagnosticRecord) bool {
	for _, raw := range strings.Split(record.message, "\n") {
		line := strings.TrimSpace(resticQuotedText.ReplaceAllString(raw, `""`))
		op, ok := parseBackendDiagnostic(line)
		if ok && op.vanishedLock() {
			delete(w.pending, op.key)
			continue // Restic retries acquisition after another client drops its lock.
		}
		if ok && op.phase == "recovered" {
			delete(w.pending, op.key)
			continue
		}
		if explicitResticDamage(line) {
			continue // A consumer can detect corruption inside a retried Load.
		}
		if ok && op.read() && op.phase == "retry" {
			for key, window := range w.pending {
				if now.Sub(window.lastFailure) > checkFailureQuietReset {
					delete(w.pending, key)
				}
			}
			if w.pending == nil {
				w.pending = map[string]checkFailureWindow{}
			}
			window, exists := w.pending[op.key]
			if !exists {
				window.deadline = now.Add(w.budget)
			}
			// Excess keys do not share a synthetic deadline. Evidence has its own
			// conservative overflow hold; terminal failures remain observable.
			if exists || (len(w.pending) < diagnosticCauseLimit && len(op.key) <= diagnosticCauseBytes) {
				window.lastFailure = now
				w.pending[op.key] = window
			}
			if deadline := w.deadline(); !deadline.IsZero() && !now.Before(deadline) {
				return true
			}
			continue
		}
		if ok && op.read() && op.phase == "failed" {
			if diagnosticCause(op.cause) == "context canceled" || diagnosticCause(op.cause) == "context cancelled" {
				continue
			}
			return true
		}
		if resticLegacyDownload.MatchString(line) || (containsDiagnostic(line, "download error:", "failed to load ") && resticTransportFailure(line)) {
			return true
		}
	}
	return false
}

func (w *checkFailureTracker) deadline() time.Time {
	var deadline time.Time
	for _, window := range w.pending {
		if deadline.IsZero() || window.deadline.Before(deadline) {
			deadline = window.deadline
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
