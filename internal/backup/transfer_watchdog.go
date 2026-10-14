package backup

import (
	"fmt"
	"time"
)

const (
	transferProgressTimeout     = 5 * time.Minute
	transferFinalizationTimeout = 30 * time.Minute
)

type transferWatchdog struct {
	deadline            time.Time
	bytes, files, added int64
	finalizing          bool
}

func newTransferWatchdog(now time.Time) transferWatchdog {
	return transferWatchdog{deadline: now.Add(transferProgressTimeout)}
}

// restic can finish reading before final packs/index/snapshot finish uploading.
// Give that phase a larger bounded budget exactly once. Status heartbeats alone
// never extend a deadline; only increased counters or entering finalization do.
func (w *transferWatchdog) observe(now time.Time, p transferProgress) bool {
	advanced := p.BytesProcessed > w.bytes || p.FilesProcessed > w.files || p.BytesAdded > w.added
	w.bytes, w.files, w.added = max(w.bytes, p.BytesProcessed), max(w.files, p.FilesProcessed), max(w.added, p.BytesAdded)
	enteredFinalization := !w.finalizing && p.BytesTotal > 0 && p.BytesProcessed >= p.BytesTotal
	w.finalizing = w.finalizing || enteredFinalization
	if !advanced && !enteredFinalization {
		return false
	}
	budget := transferProgressTimeout
	if w.finalizing {
		budget = transferFinalizationTimeout
	}
	w.deadline = now.Add(budget)
	return true
}

func transferStallError(finalizing bool) error {
	if finalizing {
		return fmt.Errorf("%w: no measured progress for 30 minutes during final pack/index upload", errTransferStalled)
	}
	return fmt.Errorf("%w: no measured progress for 5 minutes while reading and uploading recovery objects", errTransferStalled)
}
