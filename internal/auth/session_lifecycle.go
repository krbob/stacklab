package auth

import (
	"context"
	"fmt"
	"sync"
	"time"
)

type SessionTerminationReason string

const (
	SessionTerminationLogout          SessionTerminationReason = "logout"
	SessionTerminationPasswordChanged SessionTerminationReason = "password_changed"
	SessionTerminationRevoked         SessionTerminationReason = "revoked"
	SessionTerminationIdleExpired     SessionTerminationReason = "idle_expired"
	SessionTerminationAbsoluteExpired SessionTerminationReason = "absolute_expired"
)

type SessionTermination struct {
	SessionID string
	All       bool
	Reason    SessionTerminationReason
}

type trackedSession struct {
	idleDeadline     time.Time
	absoluteDeadline time.Time
	lastPersistedAt  time.Time
	timer            *time.Timer
	subscribers      map[uint64]chan SessionTermination
}

type sessionActivityTouch struct {
	activityAt         time.Time
	expiresAt          time.Time
	expectedLastSeenAt time.Time
	shouldPersist      bool
	active             bool
}

// sessionLifecycleHub is the in-memory lease and revocation fan-out for active
// transports. SQLite remains authoritative at authentication boundaries; the
// hub lets an established WebSocket react immediately without querying SQLite
// for every frame.
type sessionLifecycleHub struct {
	mu               sync.Mutex
	now              func() time.Time
	idleTimeout      time.Duration
	absoluteLifetime time.Duration
	persistInterval  time.Duration
	onExpired        func(string, SessionTerminationReason)
	nextSubscriberID uint64
	sessions         map[string]*trackedSession
	closing          bool
	expirationWG     sync.WaitGroup
}

func newSessionLifecycleHub(now func() time.Time, idleTimeout, absoluteLifetime time.Duration, onExpired func(string, SessionTerminationReason)) *sessionLifecycleHub {
	return &sessionLifecycleHub{
		now:              now,
		idleTimeout:      idleTimeout,
		absoluteLifetime: absoluteLifetime,
		persistInterval:  sessionTouchInterval(idleTimeout),
		onExpired:        onExpired,
		sessions:         make(map[string]*trackedSession),
	}
}

func (h *sessionLifecycleHub) Subscribe(session Session) (<-chan SessionTermination, func()) {
	h.mu.Lock()
	if h.closing {
		events := make(chan SessionTermination)
		close(events)
		h.mu.Unlock()
		return events, func() {}
	}
	now := h.now().UTC()
	absoluteDeadline := session.CreatedAt.Add(h.absoluteLifetime)
	idleDeadline := minTime(session.ExpiresAt, absoluteDeadline)
	lastPersistedAt := session.LastSeenAt
	if lastPersistedAt.IsZero() {
		lastPersistedAt = now
	}
	if !idleDeadline.After(now) {
		reason := SessionTerminationIdleExpired
		if !absoluteDeadline.After(now) {
			reason = SessionTerminationAbsoluteExpired
		}
		events := make(chan SessionTermination, 1)
		events <- SessionTermination{SessionID: session.ID, Reason: reason}
		close(events)
		h.mu.Unlock()
		return events, func() {}
	}

	tracked := h.sessions[session.ID]
	if tracked == nil {
		tracked = &trackedSession{
			idleDeadline:     idleDeadline,
			absoluteDeadline: absoluteDeadline,
			lastPersistedAt:  lastPersistedAt,
			subscribers:      make(map[uint64]chan SessionTermination),
		}
		h.sessions[session.ID] = tracked
	} else {
		if idleDeadline.After(tracked.idleDeadline) {
			tracked.idleDeadline = idleDeadline
		}
		if lastPersistedAt.After(tracked.lastPersistedAt) {
			tracked.lastPersistedAt = lastPersistedAt
		}
	}

	h.nextSubscriberID++
	subscriberID := h.nextSubscriberID
	events := make(chan SessionTermination, 1)
	tracked.subscribers[subscriberID] = events
	h.scheduleLocked(session.ID, tracked, now)
	h.mu.Unlock()

	var once sync.Once
	return events, func() {
		once.Do(func() {
			h.unsubscribe(session.ID, subscriberID)
		})
	}
}

// Touch extends the in-memory idle lease and reserves no persistence state.
// The caller marks a successful write with TouchPersisted; a failed write is
// therefore immediately retryable by another active transport.
func (h *sessionLifecycleHub) Touch(sessionID string) sessionActivityTouch {
	h.mu.Lock()
	tracked := h.sessions[sessionID]
	if tracked == nil || h.closing {
		h.mu.Unlock()
		return sessionActivityTouch{}
	}
	now := h.now().UTC()
	if reason, expired := tracked.expirationReason(now); expired {
		h.terminateLocked(sessionID, reason)
		h.mu.Unlock()
		h.expirePersistentSession(sessionID, reason)
		return sessionActivityTouch{}
	}

	tracked.idleDeadline = minTime(now.Add(h.idleTimeout), tracked.absoluteDeadline)
	shouldPersist := now.Sub(tracked.lastPersistedAt) >= h.persistInterval
	h.scheduleLocked(sessionID, tracked, now)
	touch := sessionActivityTouch{
		activityAt:         now,
		expiresAt:          tracked.idleDeadline,
		expectedLastSeenAt: tracked.lastPersistedAt,
		shouldPersist:      shouldPersist,
		active:             true,
	}
	h.mu.Unlock()
	return touch
}

// TouchLease applies authenticated REST activity to an established transport
// without claiming that the throttled SQLite write happened.
func (h *sessionLifecycleHub) TouchLease(sessionID string, expiresAt time.Time) {
	h.mu.Lock()
	defer h.mu.Unlock()

	tracked := h.sessions[sessionID]
	if tracked == nil || h.closing {
		return
	}
	now := h.now().UTC()
	deadline := minTime(expiresAt, tracked.absoluteDeadline)
	if deadline.After(tracked.idleDeadline) {
		tracked.idleDeadline = deadline
	}
	h.scheduleLocked(sessionID, tracked, now)
}

// TouchPersisted commits a successful SQLite lease write to the in-memory
// throttle state and keeps active transports on the same deadline.
func (h *sessionLifecycleHub) TouchPersisted(sessionID string, lastSeenAt, expiresAt time.Time) {
	h.mu.Lock()
	defer h.mu.Unlock()

	tracked := h.sessions[sessionID]
	if tracked == nil || h.closing {
		return
	}
	now := h.now().UTC()
	deadline := minTime(expiresAt, tracked.absoluteDeadline)
	if deadline.After(tracked.idleDeadline) {
		tracked.idleDeadline = deadline
	}
	if lastSeenAt.After(tracked.lastPersistedAt) {
		tracked.lastPersistedAt = lastSeenAt
	}
	h.scheduleLocked(sessionID, tracked, now)
}

func sessionTouchInterval(idleTimeout time.Duration) time.Duration {
	interval := idleTimeout / 2
	if interval <= 0 || interval > time.Minute {
		return time.Minute
	}
	return interval
}

func (h *sessionLifecycleHub) Terminate(sessionID string, reason SessionTerminationReason) {
	h.mu.Lock()
	h.terminateLocked(sessionID, reason)
	h.mu.Unlock()
}

func (h *sessionLifecycleHub) TerminateAll(reason SessionTerminationReason) {
	h.mu.Lock()
	for sessionID := range h.sessions {
		h.terminateLocked(sessionID, reason)
	}
	h.mu.Unlock()
}

func (h *sessionLifecycleHub) unsubscribe(sessionID string, subscriberID uint64) {
	h.mu.Lock()
	defer h.mu.Unlock()

	tracked := h.sessions[sessionID]
	if tracked == nil {
		return
	}
	delete(tracked.subscribers, subscriberID)
	if len(tracked.subscribers) == 0 {
		if tracked.timer != nil {
			tracked.timer.Stop()
		}
		delete(h.sessions, sessionID)
	}
}

func (h *sessionLifecycleHub) scheduleLocked(sessionID string, tracked *trackedSession, now time.Time) {
	if h.closing {
		return
	}
	deadline := minTime(tracked.idleDeadline, tracked.absoluteDeadline)
	delay := deadline.Sub(now)
	if delay < 0 {
		delay = 0
	}
	if tracked.timer == nil {
		tracked.timer = time.AfterFunc(delay, func() { h.runExpiration(sessionID) })
		return
	}
	tracked.timer.Reset(delay)
}

func (h *sessionLifecycleHub) runExpiration(sessionID string) {
	h.mu.Lock()
	if h.closing {
		h.mu.Unlock()
		return
	}
	h.expirationWG.Add(1)
	h.mu.Unlock()
	defer h.expirationWG.Done()
	h.expire(sessionID)
}

func (h *sessionLifecycleHub) expire(sessionID string) {
	h.mu.Lock()
	tracked := h.sessions[sessionID]
	if tracked == nil {
		h.mu.Unlock()
		return
	}
	now := h.now().UTC()
	reason, expired := tracked.expirationReason(now)
	if !expired {
		h.scheduleLocked(sessionID, tracked, now)
		h.mu.Unlock()
		return
	}
	h.terminateLocked(sessionID, reason)
	h.mu.Unlock()

	h.expirePersistentSession(sessionID, reason)
}

func (h *sessionLifecycleHub) expirePersistentSession(sessionID string, reason SessionTerminationReason) {
	if h.onExpired != nil {
		h.onExpired(sessionID, reason)
	}
}

func (h *sessionLifecycleHub) Shutdown(ctx context.Context) error {
	h.mu.Lock()
	if !h.closing {
		h.closing = true
		for sessionID, tracked := range h.sessions {
			if tracked.timer != nil {
				tracked.timer.Stop()
			}
			delete(h.sessions, sessionID)
			for _, events := range tracked.subscribers {
				close(events)
			}
		}
	}
	h.mu.Unlock()

	done := make(chan struct{})
	go func() {
		h.expirationWG.Wait()
		close(done)
	}()
	if ctx == nil {
		ctx = context.Background()
	}
	select {
	case <-done:
		return nil
	case <-ctx.Done():
		return fmt.Errorf("wait for session lifecycle expiration: %w", ctx.Err())
	}
}

func (h *sessionLifecycleHub) terminateLocked(sessionID string, reason SessionTerminationReason) {
	tracked := h.sessions[sessionID]
	if tracked == nil {
		return
	}
	if tracked.timer != nil {
		tracked.timer.Stop()
	}
	delete(h.sessions, sessionID)
	for _, events := range tracked.subscribers {
		events <- SessionTermination{SessionID: sessionID, Reason: reason}
		close(events)
	}
}

func (s *trackedSession) expirationReason(now time.Time) (SessionTerminationReason, bool) {
	if !s.absoluteDeadline.After(now) {
		return SessionTerminationAbsoluteExpired, true
	}
	if !s.idleDeadline.After(now) {
		return SessionTerminationIdleExpired, true
	}
	return "", false
}
