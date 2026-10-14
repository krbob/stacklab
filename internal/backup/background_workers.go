package backup

import (
	"context"
	"sync"
)

// Admission never waits for a worker: a saturated pool leaves the occurrence
// due. Workers are counted before launch and joined during shutdown, including
// capture's service-recovery compensation after cancellation.
type backgroundWorkers struct {
	slots   chan struct{}
	waiting sync.WaitGroup
}

func newBackgroundWorkers(limit int) *backgroundWorkers {
	return &backgroundWorkers{slots: make(chan struct{}, limit)}
}
func (w *backgroundWorkers) reserve(ctx context.Context) bool {
	if ctx.Err() != nil {
		return false
	}
	select {
	case w.slots <- struct{}{}:
		w.waiting.Add(1)
		return true
	default:
		return false
	}
}
func (w *backgroundWorkers) release() { <-w.slots; w.waiting.Done() }
func (w *backgroundWorkers) wait()    { w.waiting.Wait() }
