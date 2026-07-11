package lifecycle

import (
	"context"
	"errors"
	"sync"
)

var ErrShutdownTimedOut = errors.New("lifecycle shutdown timed out")

// Manager owns a cancellable context and tracks all goroutines started through
// Go. Stop prevents new asynchronous work from being admitted; Wait guarantees
// that every admitted goroutine has returned before its caller releases shared
// resources such as the application store.
type Manager struct {
	ctx    context.Context
	cancel context.CancelFunc

	mu       sync.Mutex
	stopping bool
	wg       sync.WaitGroup
	done     chan struct{}
	doneOnce sync.Once
}

func New(parent context.Context) *Manager {
	if parent == nil {
		parent = context.Background()
	}
	ctx, cancel := context.WithCancel(parent)
	return &Manager{
		ctx:    ctx,
		cancel: cancel,
		done:   make(chan struct{}),
	}
}

func (m *Manager) Context() context.Context {
	return m.ctx
}

// Go admits fn unless shutdown has started. The boolean result lets request
// handlers fall back to synchronous execution in the narrow stop/start race.
func (m *Manager) Go(fn func(context.Context)) bool {
	if fn == nil {
		return false
	}

	m.mu.Lock()
	if m.stopping {
		m.mu.Unlock()
		return false
	}
	m.wg.Add(1)
	m.mu.Unlock()

	go func() {
		defer m.wg.Done()
		fn(m.ctx)
	}()
	return true
}

func (m *Manager) Stop() {
	m.mu.Lock()
	if m.stopping {
		m.mu.Unlock()
		return
	}
	m.stopping = true
	m.cancel()
	m.mu.Unlock()

	m.doneOnce.Do(func() {
		go func() {
			m.wg.Wait()
			close(m.done)
		}()
	})
}

func (m *Manager) Wait(ctx context.Context) error {
	m.Stop()
	if ctx == nil {
		ctx = context.Background()
	}
	select {
	case <-m.done:
		return nil
	case <-ctx.Done():
		return errors.Join(ErrShutdownTimedOut, ctx.Err())
	}
}

func (m *Manager) Shutdown(ctx context.Context) error {
	m.Stop()
	return m.Wait(ctx)
}
