package auth

import (
	"context"
	"testing"
	"time"
)

func TestSessionLifecycleHubFansOutTermination(t *testing.T) {
	t.Parallel()

	now := time.Date(2026, 7, 11, 12, 0, 0, 0, time.UTC)
	hub := newSessionLifecycleHub(func() time.Time { return now }, time.Hour, 24*time.Hour, nil)
	session := Session{ID: "session-1", CreatedAt: now, ExpiresAt: now.Add(time.Hour)}
	first, unsubscribeFirst := hub.Subscribe(session)
	defer unsubscribeFirst()
	second, unsubscribeSecond := hub.Subscribe(session)
	defer unsubscribeSecond()

	hub.Terminate(session.ID, SessionTerminationLogout)
	for index, events := range []<-chan SessionTermination{first, second} {
		termination, ok := <-events
		if !ok {
			t.Fatalf("subscriber %d closed without a termination", index)
		}
		if termination.Reason != SessionTerminationLogout {
			t.Fatalf("subscriber %d reason = %q, want %q", index, termination.Reason, SessionTerminationLogout)
		}
		if termination.SessionID != session.ID {
			t.Fatalf("subscriber %d session id = %q, want %q", index, termination.SessionID, session.ID)
		}
		if _, ok := <-events; ok {
			t.Fatalf("subscriber %d remains open after termination", index)
		}
	}
}

func TestSessionLifecycleHubThrottlesPersistenceAndEnforcesAbsoluteExpiry(t *testing.T) {
	t.Parallel()

	now := time.Date(2026, 7, 11, 12, 0, 0, 0, time.UTC)
	hub := newSessionLifecycleHub(func() time.Time { return now }, 40*time.Minute, 30*time.Minute, nil)
	events, unsubscribe := hub.Subscribe(Session{
		ID:        "session-1",
		CreatedAt: now,
		ExpiresAt: now.Add(30 * time.Minute),
	})
	defer unsubscribe()

	now = now.Add(30 * time.Second)
	expiresAt, persist, active := hub.Touch("session-1")
	if !active || persist {
		t.Fatalf("first touch = expires %s, persist %t, active %t; want active without persistence", expiresAt, persist, active)
	}
	now = now.Add(30 * time.Second)
	_, persist, active = hub.Touch("session-1")
	if !active || !persist {
		t.Fatalf("throttled touch persist = %t, active = %t; want true, true", persist, active)
	}

	// Activity cannot move the lease beyond its original absolute lifetime.
	now = time.Date(2026, 7, 11, 12, 29, 0, 0, time.UTC)
	expiresAt, _, active = hub.Touch("session-1")
	if !active || !expiresAt.Equal(time.Date(2026, 7, 11, 12, 30, 0, 0, time.UTC)) {
		t.Fatalf("touch near absolute deadline = %s, active %t", expiresAt, active)
	}
	now = now.Add(time.Minute)
	hub.expire("session-1")
	termination := <-events
	if termination.Reason != SessionTerminationAbsoluteExpired {
		t.Fatalf("termination reason = %q, want %q", termination.Reason, SessionTerminationAbsoluteExpired)
	}
}

func TestSessionLifecycleHubExpiresIdleSessionAndRunsPersistentCallback(t *testing.T) {
	t.Parallel()

	now := time.Date(2026, 7, 11, 12, 0, 0, 0, time.UTC)
	expired := make(chan SessionTerminationReason, 1)
	hub := newSessionLifecycleHub(func() time.Time { return now }, time.Minute, time.Hour, func(_ string, reason SessionTerminationReason) {
		expired <- reason
	})
	events, unsubscribe := hub.Subscribe(Session{ID: "session-1", CreatedAt: now, ExpiresAt: now.Add(time.Minute)})
	defer unsubscribe()

	now = now.Add(time.Minute)
	hub.expire("session-1")
	if termination := <-events; termination.Reason != SessionTerminationIdleExpired {
		t.Fatalf("termination reason = %q, want %q", termination.Reason, SessionTerminationIdleExpired)
	}
	select {
	case reason := <-expired:
		if reason != SessionTerminationIdleExpired {
			t.Fatalf("persistent callback reason = %q, want %q", reason, SessionTerminationIdleExpired)
		}
	case <-time.After(time.Second):
		t.Fatal("timed out waiting for persistent expiry callback")
	}
}

func TestSessionLifecycleHubShutdownWaitsForActiveExpiration(t *testing.T) {
	t.Parallel()

	now := time.Date(2026, 7, 11, 12, 0, 0, 0, time.UTC)
	callbackStarted := make(chan struct{})
	releaseCallback := make(chan struct{})
	hub := newSessionLifecycleHub(func() time.Time { return now }, time.Minute, time.Hour, func(string, SessionTerminationReason) {
		close(callbackStarted)
		<-releaseCallback
	})
	_, unsubscribe := hub.Subscribe(Session{ID: "session-1", CreatedAt: now, ExpiresAt: now.Add(time.Minute)})
	defer unsubscribe()
	now = now.Add(time.Minute)
	go hub.runExpiration("session-1")

	select {
	case <-callbackStarted:
	case <-time.After(time.Second):
		t.Fatal("expiration callback did not start")
	}

	shutdownDone := make(chan error, 1)
	go func() { shutdownDone <- hub.Shutdown(context.Background()) }()
	select {
	case err := <-shutdownDone:
		t.Fatalf("Shutdown() returned before expiration callback: %v", err)
	case <-time.After(20 * time.Millisecond):
	}

	close(releaseCallback)
	select {
	case err := <-shutdownDone:
		if err != nil {
			t.Fatalf("Shutdown() error = %v", err)
		}
	case <-time.After(time.Second):
		t.Fatal("Shutdown() did not finish after expiration callback")
	}
	if _, _, active := hub.Touch("session-1"); active {
		t.Fatal("Touch() kept a session active after shutdown")
	}
}
