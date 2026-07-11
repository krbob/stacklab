package auth

import (
	"context"
	"encoding/base64"
	"errors"
	"fmt"
	"net/http"
	"net/http/httptest"
	"net/netip"
	"path/filepath"
	"strings"
	"sync/atomic"
	"testing"
	"time"

	"stacklab/internal/config"
	"stacklab/internal/store"
)

func TestServiceBootstrapLoginAuthenticateAndUpdatePassword(t *testing.T) {
	t.Parallel()

	ctx := context.Background()
	authStore := openTestStore(t)
	service := NewService(testConfig("test-password"), authStore)

	if err := service.Bootstrap(ctx); err != nil {
		t.Fatalf("Bootstrap() error = %v", err)
	}

	passwordHash, configured, err := authStore.PasswordHash(ctx)
	if err != nil {
		t.Fatalf("PasswordHash() error = %v", err)
	}
	if !configured {
		t.Fatalf("expected bootstrap to configure password")
	}
	if passwordHash == "" || passwordHash == "test-password" {
		t.Fatalf("expected hashed password to be stored")
	}

	if _, err := service.Login(ctx, "wrong", "ua", "127.0.0.1"); !errors.Is(err, ErrInvalidCredentials) {
		t.Fatalf("Login(wrong) error = %v, want ErrInvalidCredentials", err)
	}

	session, err := service.Login(ctx, "test-password", "ua", "127.0.0.1")
	if err != nil {
		t.Fatalf("Login(secret) error = %v", err)
	}

	request := httptest.NewRequest("GET", "http://stacklab.test/api/session", nil)
	request.AddCookie(service.SessionCookie(session))

	authenticated, err := service.AuthenticateRequest(ctx, request)
	if err != nil {
		t.Fatalf("AuthenticateRequest() error = %v", err)
	}
	if authenticated.ID != session.ID {
		t.Fatalf("AuthenticateRequest() session id = %q, want %q", authenticated.ID, session.ID)
	}

	if err := service.UpdatePassword(ctx, "test-password", "too-short"); !errors.Is(err, ErrInvalidPassword) {
		t.Fatalf("UpdatePassword(short password) error = %v, want ErrInvalidPassword", err)
	}
	if _, err := service.AuthenticateRequest(ctx, request); err != nil {
		t.Fatalf("AuthenticateRequest(session after rejected password update) error = %v", err)
	}

	if err := service.UpdatePassword(ctx, "test-password", "new-test-password"); err != nil {
		t.Fatalf("UpdatePassword() error = %v", err)
	}
	if _, err := service.AuthenticateRequest(ctx, request); !errors.Is(err, ErrUnauthorized) {
		t.Fatalf("AuthenticateRequest(old session after password update) error = %v, want ErrUnauthorized", err)
	}

	if _, err := service.Login(ctx, "test-password", "ua", "127.0.0.1"); !errors.Is(err, ErrInvalidCredentials) {
		t.Fatalf("Login(old password) error = %v, want ErrInvalidCredentials", err)
	}
	if _, err := service.Login(ctx, "new-test-password", "ua", "127.0.0.1"); err != nil {
		t.Fatalf("Login(new password) error = %v", err)
	}
}

func TestBootstrapRejectsPasswordOutsidePolicy(t *testing.T) {
	t.Parallel()

	ctx := context.Background()
	authStore := openTestStore(t)
	service := NewService(testConfig(strings.Repeat("x", PasswordMinimumLength-1)), authStore)

	if err := service.Bootstrap(ctx); !errors.Is(err, ErrInvalidPassword) {
		t.Fatalf("Bootstrap() error = %v, want ErrInvalidPassword", err)
	}
	if _, configured, err := authStore.PasswordHash(ctx); err != nil {
		t.Fatalf("PasswordHash() error = %v", err)
	} else if configured {
		t.Fatal("invalid bootstrap password configured authentication")
	}
}

func TestValidateNewPasswordBoundaries(t *testing.T) {
	t.Parallel()

	tests := []struct {
		name     string
		password string
		valid    bool
	}{
		{name: "empty", password: "", valid: false},
		{name: "below minimum", password: strings.Repeat("x", PasswordMinimumLength-1), valid: false},
		{name: "minimum", password: strings.Repeat("x", PasswordMinimumLength), valid: true},
		{name: "unicode minimum", password: strings.Repeat("🔐", PasswordMinimumLength), valid: true},
		{name: "maximum", password: strings.Repeat("x", PasswordMaximumLength), valid: true},
		{name: "above maximum", password: strings.Repeat("x", PasswordMaximumLength+1), valid: false},
		{name: "invalid utf8", password: string([]byte{0xff, 0xfe}), valid: false},
	}

	for _, test := range tests {
		t.Run(test.name, func(t *testing.T) {
			err := validateNewPassword(test.password)
			if test.valid && err != nil {
				t.Fatalf("validateNewPassword() error = %v", err)
			}
			if !test.valid && !errors.Is(err, ErrInvalidPassword) {
				t.Fatalf("validateNewPassword() error = %v, want ErrInvalidPassword", err)
			}
		})
	}
}

func TestParsePasswordHashEnforcesResourceBoundaries(t *testing.T) {
	t.Parallel()

	validSalt := bytesOfLength(argon2MinimumSaltLength)
	validHash := bytesOfLength(argon2MinimumHashLength)
	tests := []struct {
		name       string
		parameters string
		salt       []byte
		hash       []byte
		valid      bool
	}{
		{name: "minimum parameters", parameters: "m=8192,t=1,p=1", salt: validSalt, hash: validHash, valid: true},
		{name: "maximum parameters and components", parameters: "m=131072,t=10,p=8", salt: bytesOfLength(argon2MaximumSaltLength), hash: bytesOfLength(argon2MaximumHashLength), valid: true},
		{name: "memory below minimum", parameters: "m=8191,t=1,p=1", salt: validSalt, hash: validHash},
		{name: "memory above maximum", parameters: "m=131073,t=1,p=1", salt: validSalt, hash: validHash},
		{name: "zero iterations", parameters: "m=8192,t=0,p=1", salt: validSalt, hash: validHash},
		{name: "iterations above maximum", parameters: "m=8192,t=11,p=1", salt: validSalt, hash: validHash},
		{name: "zero parallelism", parameters: "m=8192,t=1,p=0", salt: validSalt, hash: validHash},
		{name: "parallelism above maximum", parameters: "m=8192,t=1,p=9", salt: validSalt, hash: validHash},
		{name: "short salt", parameters: "m=8192,t=1,p=1", salt: bytesOfLength(argon2MinimumSaltLength - 1), hash: validHash},
		{name: "long salt", parameters: "m=8192,t=1,p=1", salt: bytesOfLength(argon2MaximumSaltLength + 1), hash: validHash},
		{name: "short hash", parameters: "m=8192,t=1,p=1", salt: validSalt, hash: bytesOfLength(argon2MinimumHashLength - 1)},
		{name: "long hash", parameters: "m=8192,t=1,p=1", salt: validSalt, hash: bytesOfLength(argon2MaximumHashLength + 1)},
	}

	for _, test := range tests {
		t.Run(test.name, func(t *testing.T) {
			encoded := passwordHashFixture(test.parameters, test.salt, test.hash)
			_, err := parsePasswordHash(encoded)
			if test.valid && err != nil {
				t.Fatalf("parsePasswordHash() error = %v", err)
			}
			if !test.valid && !errors.Is(err, ErrInvalidCredentials) {
				t.Fatalf("parsePasswordHash() error = %v, want ErrInvalidCredentials", err)
			}
		})
	}
}

func TestVerifyPasswordRejectsMaliciousHashesWithoutPanic(t *testing.T) {
	t.Parallel()

	component := base64.RawStdEncoding.EncodeToString(bytesOfLength(argon2MinimumHashLength))
	tests := []string{
		"",
		"$stacklab$argon2id$v=18$m=65536,t=3,p=2$" + component + "$" + component,
		"$stacklab$argon2id$v=19$m=4294967295,t=3,p=2$" + component + "$" + component,
		"$stacklab$argon2id$v=19$m=65536,t=4294967295,p=2$" + component + "$" + component,
		"$stacklab$argon2id$v=19$m=65536,t=3,p=255$" + component + "$" + component,
		"$stacklab$argon2id$v=19$m=65536,t=3,p=0$" + component + "$" + component,
		"$stacklab$argon2id$v=19$m=+65536,t=3,p=2$" + component + "$" + component,
		"$stacklab$argon2id$v=19$m=65536,t=3,p=2,trailing$" + component + "$" + component,
		"$stacklab$argon2id$v=19$m=65536,t=3,p=2$%%%$" + component,
		"$stacklab$argon2id$v=19$m=65536,t=3,p=2$" + component + "\n$" + component,
		"$stacklab$argon2id$v=19$m=65536,t=3,p=2$" + strings.Repeat("A", 10_000) + "$" + component,
		strings.Repeat("$", maximumEncodedPasswordHashLength),
	}

	for index, encoded := range tests {
		t.Run(fmt.Sprintf("case_%d", index), func(t *testing.T) {
			defer func() {
				if recovered := recover(); recovered != nil {
					t.Fatalf("verifyPassword() panicked: %v", recovered)
				}
			}()
			if err := verifyPassword(encoded, "test-password"); !errors.Is(err, ErrInvalidCredentials) {
				t.Fatalf("verifyPassword() error = %v, want ErrInvalidCredentials", err)
			}
		})
	}
}

func TestDerivePasswordHashContainsArgonPanic(t *testing.T) {
	t.Parallel()

	parameters := passwordHashParameters{
		memory:      argon2Memory,
		iterations:  argon2Iterations,
		parallelism: 0,
	}
	if _, err := derivePasswordHash([]byte("test-password"), bytesOfLength(argon2MinimumSaltLength), parameters, argon2KeyLength); !errors.Is(err, ErrInvalidCredentials) {
		t.Fatalf("derivePasswordHash() error = %v, want ErrInvalidCredentials", err)
	}
}

func FuzzParsePasswordHash(f *testing.F) {
	component := bytesOfLength(argon2MinimumHashLength)
	f.Add(passwordHashFixture("m=65536,t=3,p=2", component, component))
	f.Add("$stacklab$argon2id$v=19$m=4294967295,t=4294967295,p=255$invalid$invalid")
	f.Add(strings.Repeat("$", maximumEncodedPasswordHashLength))

	f.Fuzz(func(t *testing.T, encoded string) {
		_, _ = parsePasswordHash(encoded)
	})
}

func passwordHashFixture(parameters string, salt, hash []byte) string {
	return fmt.Sprintf(
		"$stacklab$argon2id$v=19$%s$%s$%s",
		parameters,
		base64.RawStdEncoding.EncodeToString(salt),
		base64.RawStdEncoding.EncodeToString(hash),
	)
}

func bytesOfLength(length int) []byte {
	return []byte(strings.Repeat("x", length))
}

func TestUpdatePasswordRevokesEveryExistingSession(t *testing.T) {
	t.Parallel()

	ctx := context.Background()
	authStore := openTestStore(t)
	service := NewService(testConfig("test-password"), authStore)
	if err := service.Bootstrap(ctx); err != nil {
		t.Fatalf("Bootstrap() error = %v", err)
	}

	first, err := service.Login(ctx, "test-password", "first", "192.0.2.1")
	if err != nil {
		t.Fatalf("Login(first) error = %v", err)
	}
	second, err := service.Login(ctx, "test-password", "second", "192.0.2.2")
	if err != nil {
		t.Fatalf("Login(second) error = %v", err)
	}

	if err := service.UpdatePassword(ctx, "test-password", "new-test-password"); err != nil {
		t.Fatalf("UpdatePassword() error = %v", err)
	}

	for _, session := range []Session{first, second} {
		request := httptest.NewRequest(http.MethodGet, "http://stacklab.test/api/session", nil)
		request.AddCookie(service.SessionCookie(session))
		if _, err := service.AuthenticateRequest(ctx, request); !errors.Is(err, ErrUnauthorized) {
			t.Fatalf("AuthenticateRequest(%s) error = %v, want ErrUnauthorized", session.ID, err)
		}
	}
}

func TestLogoutAndPasswordChangePublishSessionTermination(t *testing.T) {
	t.Parallel()

	t.Run("logout", func(t *testing.T) {
		ctx := context.Background()
		service := NewService(testConfig("test-password"), openTestStore(t))
		if err := service.Bootstrap(ctx); err != nil {
			t.Fatalf("Bootstrap() error = %v", err)
		}
		session, err := service.Login(ctx, "test-password", "ua", "192.0.2.1")
		if err != nil {
			t.Fatalf("Login() error = %v", err)
		}
		hookEvents := make(chan SessionTermination, 1)
		service.SetSessionTerminationHook(func(termination SessionTermination) { hookEvents <- termination })
		events, unsubscribe := service.sessionLifecycle.Subscribe(session)
		defer unsubscribe()
		if err := service.Logout(ctx, session.ID); err != nil {
			t.Fatalf("Logout() error = %v", err)
		}
		if termination := <-events; termination.Reason != SessionTerminationLogout {
			t.Fatalf("termination reason = %q, want %q", termination.Reason, SessionTerminationLogout)
		}
		hookEvent := <-hookEvents
		if hookEvent.SessionID != session.ID || hookEvent.All || hookEvent.Reason != SessionTerminationLogout {
			t.Fatalf("logout hook event = %#v", hookEvent)
		}
	})

	t.Run("password change", func(t *testing.T) {
		ctx := context.Background()
		service := NewService(testConfig("test-password"), openTestStore(t))
		if err := service.Bootstrap(ctx); err != nil {
			t.Fatalf("Bootstrap() error = %v", err)
		}
		session, err := service.Login(ctx, "test-password", "ua", "192.0.2.1")
		if err != nil {
			t.Fatalf("Login() error = %v", err)
		}
		hookEvents := make(chan SessionTermination, 1)
		service.SetSessionTerminationHook(func(termination SessionTermination) { hookEvents <- termination })
		events, unsubscribe := service.sessionLifecycle.Subscribe(session)
		defer unsubscribe()
		if err := service.UpdatePassword(ctx, "test-password", "new-test-password"); err != nil {
			t.Fatalf("UpdatePassword() error = %v", err)
		}
		if termination := <-events; termination.Reason != SessionTerminationPasswordChanged {
			t.Fatalf("termination reason = %q, want %q", termination.Reason, SessionTerminationPasswordChanged)
		}
		hookEvent := <-hookEvents
		if !hookEvent.All || hookEvent.SessionID != "" || hookEvent.Reason != SessionTerminationPasswordChanged {
			t.Fatalf("password-change hook event = %#v", hookEvent)
		}
	})
}

func TestSessionTerminationHookDoesNotRequireWebSocketSubscriber(t *testing.T) {
	t.Parallel()

	ctx := context.Background()
	service := NewService(testConfig("test-password"), openTestStore(t))
	if err := service.Bootstrap(ctx); err != nil {
		t.Fatalf("Bootstrap() error = %v", err)
	}
	session, err := service.Login(ctx, "test-password", "ua", "192.0.2.1")
	if err != nil {
		t.Fatalf("Login() error = %v", err)
	}
	events := make(chan SessionTermination, 1)
	service.SetSessionTerminationHook(func(termination SessionTermination) { events <- termination })

	if err := service.Logout(ctx, session.ID); err != nil {
		t.Fatalf("Logout() error = %v", err)
	}
	select {
	case termination := <-events:
		if termination.SessionID != session.ID || termination.Reason != SessionTerminationLogout {
			t.Fatalf("termination = %#v", termination)
		}
	case <-time.After(time.Second):
		t.Fatal("timed out waiting for termination hook without WebSocket subscriber")
	}
}

func TestAuthenticateRequestRejectsExpiredSession(t *testing.T) {
	t.Parallel()

	ctx := context.Background()
	authStore := openTestStore(t)
	service := NewService(testConfig("test-password"), authStore)

	now := time.Now().UTC()
	expired := now.Add(-time.Minute)
	if err := authStore.CreateSession(ctx, store.Session{
		ID:         "expired",
		UserID:     "local",
		CreatedAt:  now.Add(-2 * time.Hour),
		LastSeenAt: now.Add(-2 * time.Hour),
		ExpiresAt:  expired,
	}); err != nil {
		t.Fatalf("CreateSession() error = %v", err)
	}

	request := httptest.NewRequest("GET", "http://stacklab.test/api/session", nil)
	request.AddCookie(&http.Cookie{
		Name:  "stacklab_session",
		Value: "expired",
		Path:  "/",
	})

	if _, err := service.AuthenticateRequest(ctx, request); !errors.Is(err, ErrUnauthorized) {
		t.Fatalf("AuthenticateRequest() error = %v, want ErrUnauthorized", err)
	}
}

func TestAuthenticateRequestThrottlesSlidingExpiryAndCapsCookieAtAbsoluteLifetime(t *testing.T) {
	ctx := context.Background()
	authStore := openTestStore(t)
	cfg := testConfig("test-password")
	cfg.SessionIdleTimeout = 10 * time.Minute
	cfg.SessionAbsoluteLifetime = 30 * time.Minute
	service := NewService(cfg, authStore)
	now := time.Date(2026, 7, 11, 12, 0, 0, 0, time.UTC)
	service.now = func() time.Time { return now }

	if err := service.Bootstrap(ctx); err != nil {
		t.Fatalf("Bootstrap() error = %v", err)
	}
	session, err := service.Login(ctx, "test-password", "ua", "192.0.2.1")
	if err != nil {
		t.Fatalf("Login() error = %v", err)
	}
	request := httptest.NewRequest(http.MethodGet, "http://stacklab.test/api/session", nil)
	request.AddCookie(&http.Cookie{Name: cfg.SessionCookieName, Value: session.ID})

	absoluteDeadline := now.Add(cfg.SessionAbsoluteLifetime)
	if cookie := service.SessionCookie(session); !cookie.Expires.Equal(absoluteDeadline) {
		t.Fatalf("login cookie expiry = %s, want absolute deadline %s", cookie.Expires, absoluteDeadline)
	}

	now = now.Add(30 * time.Second)
	authenticated, err := service.AuthenticateRequest(ctx, request)
	if err != nil {
		t.Fatalf("AuthenticateRequest(within throttle) error = %v", err)
	}
	if !authenticated.ExpiresAt.Equal(now.Add(cfg.SessionIdleTimeout)) {
		t.Fatalf("logical expiry within throttle = %s, want %s", authenticated.ExpiresAt, now.Add(cfg.SessionIdleTimeout))
	}
	record, err := authStore.SessionByID(ctx, session.ID)
	if err != nil {
		t.Fatalf("SessionByID(within throttle) error = %v", err)
	}
	if !record.LastSeenAt.Equal(session.LastSeenAt) || !record.ExpiresAt.Equal(session.ExpiresAt) {
		t.Fatalf("session persisted inside throttle: %#v", record)
	}

	now = session.CreatedAt.Add(time.Minute)
	authenticated, err = service.AuthenticateRequest(ctx, request)
	if err != nil {
		t.Fatalf("AuthenticateRequest(at throttle) error = %v", err)
	}
	record, err = authStore.SessionByID(ctx, session.ID)
	if err != nil {
		t.Fatalf("SessionByID(after touch) error = %v", err)
	}
	if !record.LastSeenAt.Equal(now) || !record.ExpiresAt.Equal(now.Add(cfg.SessionIdleTimeout)) {
		t.Fatalf("persisted sliding lease = last seen %s, expires %s", record.LastSeenAt, record.ExpiresAt)
	}
	if cookie := service.SessionCookie(authenticated); !cookie.Expires.Equal(absoluteDeadline) {
		t.Fatalf("refreshed cookie expiry = %s, want %s", cookie.Expires, absoluteDeadline)
	}

	for _, offset := range []time.Duration{10*time.Minute + 30*time.Second, 20 * time.Minute, 29 * time.Minute} {
		now = session.CreatedAt.Add(offset)
		authenticated, err = service.AuthenticateRequest(ctx, request)
		if err != nil {
			t.Fatalf("AuthenticateRequest(%s) error = %v", offset, err)
		}
	}
	if !authenticated.ExpiresAt.Equal(absoluteDeadline) {
		t.Fatalf("expiry near absolute lifetime = %s, want %s", authenticated.ExpiresAt, absoluteDeadline)
	}

	now = absoluteDeadline
	if _, err := service.AuthenticateRequest(ctx, request); !errors.Is(err, ErrUnauthorized) {
		t.Fatalf("AuthenticateRequest(at absolute deadline) error = %v, want ErrUnauthorized", err)
	}
}

func TestWebSocketActivitySharesPersistenceThrottleAndRetriesFailedTouch(t *testing.T) {
	ctx := context.Background()
	authStore := openTestStore(t)
	cfg := testConfig("test-password")
	cfg.SessionIdleTimeout = 10 * time.Minute
	cfg.SessionAbsoluteLifetime = 30 * time.Minute
	service := NewService(cfg, authStore)
	now := time.Date(2026, 7, 11, 12, 0, 0, 0, time.UTC)
	service.now = func() time.Time { return now }

	if err := service.Bootstrap(ctx); err != nil {
		t.Fatalf("Bootstrap() error = %v", err)
	}
	session, err := service.Login(ctx, "test-password", "ua", "192.0.2.1")
	if err != nil {
		t.Fatalf("Login() error = %v", err)
	}
	request := httptest.NewRequest(http.MethodGet, "http://stacklab.test/api/ws", nil)
	request.AddCookie(&http.Cookie{Name: cfg.SessionCookieName, Value: session.ID})

	now = now.Add(30 * time.Second)
	_, _, unsubscribe, err := service.AuthenticateWebSocket(ctx, request)
	if err != nil {
		t.Fatalf("AuthenticateWebSocket() error = %v", err)
	}
	defer unsubscribe()

	now = session.CreatedAt.Add(45 * time.Second)
	if err := service.TouchSessionActivity(ctx, session.ID); err != nil {
		t.Fatalf("TouchSessionActivity(within throttle) error = %v", err)
	}
	record, err := authStore.SessionByID(ctx, session.ID)
	if err != nil {
		t.Fatalf("SessionByID(within throttle) error = %v", err)
	}
	if !record.LastSeenAt.Equal(session.LastSeenAt) {
		t.Fatalf("websocket touch persisted early at %s", record.LastSeenAt)
	}

	databaseErr := errors.New("database unavailable")
	failingStore := &sessionFailureStore{serviceStore: authStore, touchErrors: []error{databaseErr}}
	service.store = failingStore
	now = session.CreatedAt.Add(time.Minute)
	if err := service.TouchSessionActivity(ctx, session.ID); !errors.Is(err, databaseErr) {
		t.Fatalf("TouchSessionActivity(failed persistence) error = %v, want database error", err)
	}
	if err := service.TouchSessionActivity(ctx, session.ID); err != nil {
		t.Fatalf("TouchSessionActivity(retry) error = %v", err)
	}
	if failingStore.touchCalls != 2 {
		t.Fatalf("TouchSession() calls = %d, want immediate retry after failure", failingStore.touchCalls)
	}
	record, err = authStore.SessionByID(ctx, session.ID)
	if err != nil {
		t.Fatalf("SessionByID(after retry) error = %v", err)
	}
	if !record.LastSeenAt.Equal(now) || !record.ExpiresAt.Equal(now.Add(cfg.SessionIdleTimeout)) {
		t.Fatalf("websocket persisted lease = last seen %s, expires %s", record.LastSeenAt, record.ExpiresAt)
	}
}

func TestAuthenticateRequestPreservesDatabaseFailures(t *testing.T) {
	ctx := context.Background()
	authStore := openTestStore(t)
	cfg := testConfig("test-password")
	cfg.SessionIdleTimeout = 10 * time.Minute
	service := NewService(cfg, authStore)
	now := time.Date(2026, 7, 11, 12, 0, 0, 0, time.UTC)
	service.now = func() time.Time { return now }

	if err := service.Bootstrap(ctx); err != nil {
		t.Fatalf("Bootstrap() error = %v", err)
	}
	session, err := service.Login(ctx, "test-password", "ua", "192.0.2.1")
	if err != nil {
		t.Fatalf("Login() error = %v", err)
	}
	request := httptest.NewRequest(http.MethodGet, "http://stacklab.test/api/session", nil)
	request.AddCookie(&http.Cookie{Name: cfg.SessionCookieName, Value: session.ID})

	databaseErr := errors.New("database unavailable")
	failingStore := &sessionFailureStore{serviceStore: authStore, validationErr: databaseErr}
	service.store = failingStore
	if _, err := service.AuthenticateRequest(ctx, request); !errors.Is(err, databaseErr) || errors.Is(err, ErrUnauthorized) {
		t.Fatalf("AuthenticateRequest(validation failure) error = %v, want database error", err)
	}

	failingStore.validationErr = nil
	failingStore.touchErrors = []error{databaseErr}
	now = now.Add(sessionTouchInterval(cfg.SessionIdleTimeout))
	if _, err := service.AuthenticateRequest(ctx, request); !errors.Is(err, databaseErr) || errors.Is(err, ErrUnauthorized) {
		t.Fatalf("AuthenticateRequest(touch failure) error = %v, want database error", err)
	}
	if _, err := service.AuthenticateRequest(ctx, request); err != nil {
		t.Fatalf("AuthenticateRequest(touch retry) error = %v", err)
	}

	now = now.Add(sessionTouchInterval(cfg.SessionIdleTimeout))
	failingStore.touchErrors = []error{store.ErrSessionChanged}
	if _, err := service.AuthenticateRequest(ctx, request); err != nil {
		t.Fatalf("AuthenticateRequest(concurrent touch retry) error = %v", err)
	}
}

type sessionFailureStore struct {
	serviceStore
	validationErr error
	touchErrors   []error
	touchCalls    int
}

func (s *sessionFailureStore) SessionAtCurrentPasswordVersion(ctx context.Context, id string) (store.Session, error) {
	if s.validationErr != nil {
		return store.Session{}, s.validationErr
	}
	return s.serviceStore.SessionAtCurrentPasswordVersion(ctx, id)
}

func (s *sessionFailureStore) TouchSession(ctx context.Context, id string, expectedLastSeenAt, lastSeenAt, expiresAt time.Time) error {
	s.touchCalls++
	if len(s.touchErrors) > 0 {
		err := s.touchErrors[0]
		s.touchErrors = s.touchErrors[1:]
		return err
	}
	return s.serviceStore.TouchSession(ctx, id, expectedLastSeenAt, lastSeenAt, expiresAt)
}

func TestLoginLocksClientAfterRepeatedFailures(t *testing.T) {
	t.Parallel()

	ctx := context.Background()
	authStore := openTestStore(t)
	cfg := testConfig("test-password")
	cfg.LoginMaxFailures = 2
	cfg.LoginFailureWindow = time.Minute
	cfg.LoginLockoutDuration = time.Hour
	service := NewService(cfg, authStore)

	now := time.Date(2026, 7, 3, 12, 0, 0, 0, time.UTC)
	service.now = func() time.Time { return now }

	if err := service.Bootstrap(ctx); err != nil {
		t.Fatalf("Bootstrap() error = %v", err)
	}

	for i := 0; i < 2; i++ {
		if _, err := service.Login(ctx, "wrong", "ua", "127.0.0.1"); !errors.Is(err, ErrInvalidCredentials) {
			t.Fatalf("Login(wrong %d) error = %v, want ErrInvalidCredentials", i+1, err)
		}
	}
	if _, err := service.Login(ctx, "test-password", "ua", "127.0.0.1"); !errors.Is(err, ErrTooManyAttempts) {
		t.Fatalf("Login(locked client) error = %v, want ErrTooManyAttempts", err)
	}
	if _, err := service.Login(ctx, "test-password", "ua", "127.0.0.2"); err != nil {
		t.Fatalf("Login(other client) error = %v", err)
	}

	now = now.Add(cfg.LoginLockoutDuration + time.Nanosecond)
	if _, err := service.Login(ctx, "test-password", "ua", "127.0.0.1"); err != nil {
		t.Fatalf("Login(after lockout) error = %v", err)
	}
}

func TestLoginLimitsConcurrentPasswordVerificationGlobally(t *testing.T) {
	t.Parallel()

	ctx := context.Background()
	authStore := openTestStore(t)
	if err := authStore.SetPasswordHash(ctx, "fixture-hash", time.Now().UTC()); err != nil {
		t.Fatalf("SetPasswordHash() error = %v", err)
	}
	service := NewService(testConfig(""), authStore)
	service.loginVerificationSlots = make(chan struct{}, 1)

	started := make(chan struct{})
	release := make(chan struct{})
	var verificationCalls atomic.Int32
	service.passwordVerifier = func(_, _ string) error {
		verificationCalls.Add(1)
		close(started)
		<-release
		return ErrInvalidCredentials
	}

	firstResult := make(chan error, 1)
	go func() {
		_, err := service.Login(ctx, "wrong", "ua", "192.0.2.1")
		firstResult <- err
	}()
	<-started

	if _, err := service.Login(ctx, "wrong", "ua", "192.0.2.2"); !errors.Is(err, ErrTooManyAttempts) {
		t.Fatalf("Login(while global verifier is busy) error = %v, want ErrTooManyAttempts", err)
	}
	if got := verificationCalls.Load(); got != 1 {
		t.Fatalf("password verification calls = %d, want 1", got)
	}

	close(release)
	if err := <-firstResult; !errors.Is(err, ErrInvalidCredentials) {
		t.Fatalf("Login(first request) error = %v, want ErrInvalidCredentials", err)
	}
}

func TestLoginLimitsConcurrentPasswordVerificationPerClient(t *testing.T) {
	t.Parallel()

	ctx := context.Background()
	authStore := openTestStore(t)
	if err := authStore.SetPasswordHash(ctx, "fixture-hash", time.Now().UTC()); err != nil {
		t.Fatalf("SetPasswordHash() error = %v", err)
	}
	service := NewService(testConfig(""), authStore)

	started := make(chan struct{})
	release := make(chan struct{})
	service.passwordVerifier = func(_, _ string) error {
		close(started)
		<-release
		return ErrInvalidCredentials
	}

	firstResult := make(chan error, 1)
	go func() {
		_, err := service.Login(ctx, "wrong", "ua", "192.0.2.1")
		firstResult <- err
	}()
	<-started

	if _, err := service.Login(ctx, "wrong", "ua", "192.0.2.1"); !errors.Is(err, ErrTooManyAttempts) {
		t.Fatalf("Login(while client verifier is busy) error = %v, want ErrTooManyAttempts", err)
	}

	close(release)
	if err := <-firstResult; !errors.Is(err, ErrInvalidCredentials) {
		t.Fatalf("Login(first request) error = %v, want ErrInvalidCredentials", err)
	}
}

func TestLoginAttemptTrackingIsBoundedAndPrunesExpiredClients(t *testing.T) {
	t.Parallel()

	service := NewService(testConfig(""), openTestStore(t))
	service.maxTrackedLoginClients = 2
	service.cfg.LoginFailureWindow = time.Minute
	now := time.Date(2026, 7, 3, 12, 0, 0, 0, time.UTC)

	service.recordLoginFailure("192.0.2.1", now)
	service.recordLoginFailure("192.0.2.2", now.Add(time.Second))
	service.recordLoginFailure("192.0.2.3", now.Add(2*time.Second))

	service.loginMu.Lock()
	_, oldestStillTracked := service.loginAttempts["192.0.2.1"]
	trackedCount := len(service.loginAttempts)
	service.loginMu.Unlock()
	if trackedCount != 2 {
		t.Fatalf("tracked login clients = %d, want 2", trackedCount)
	}
	if oldestStillTracked {
		t.Fatal("oldest login client was not evicted")
	}

	service.recordLoginFailure("192.0.2.4", now.Add(2*time.Minute))
	service.loginMu.Lock()
	trackedCount = len(service.loginAttempts)
	_, newClientTracked := service.loginAttempts["192.0.2.4"]
	service.loginMu.Unlock()
	if trackedCount != 1 || !newClientTracked {
		t.Fatalf("tracked login clients after expiry = %d, new client tracked = %t; want 1, true", trackedCount, newClientTracked)
	}
}

func TestLoginReturnsTokenGenerationError(t *testing.T) {
	ctx := context.Background()
	authStore := openTestStore(t)
	service := NewService(testConfig("test-password"), authStore)
	tokenErr := errors.New("entropy unavailable")
	service.newSessionID = func() (string, error) {
		return "", tokenErr
	}

	if err := service.Bootstrap(ctx); err != nil {
		t.Fatalf("Bootstrap() error = %v", err)
	}

	if _, err := service.Login(ctx, "test-password", "ua", "127.0.0.1"); !errors.Is(err, tokenErr) {
		t.Fatalf("Login(token error) error = %v, want %v", err, tokenErr)
	}
}

func TestServiceClientIPTrustsForwardedForOnlyFromConfiguredProxy(t *testing.T) {
	t.Parallel()

	cfg := testConfig("test-password")
	cfg.TrustedProxies = []netip.Prefix{netip.MustParsePrefix("10.0.0.0/8")}
	cfg.TrustedProxySecret = "proxy-secret"
	service := NewService(cfg, openTestStore(t))

	trusted := httptest.NewRequest(http.MethodPost, "http://stacklab.test/api/auth/login", nil)
	trusted.RemoteAddr = "10.1.2.3:4567"
	trusted.Header.Set("X-Forwarded-For", "203.0.113.10, 10.1.2.3")
	trusted.Header.Set(trustedProxySecretHeader, cfg.TrustedProxySecret)
	if got := service.ClientIP(trusted); got != "203.0.113.10" {
		t.Fatalf("ClientIP(trusted proxy) = %q, want 203.0.113.10", got)
	}

	spoofed := httptest.NewRequest(http.MethodPost, "http://stacklab.test/api/auth/login", nil)
	spoofed.RemoteAddr = "10.1.2.3:4567"
	spoofed.Header.Set("X-Forwarded-For", "198.51.100.250, 203.0.113.10, 10.2.3.4")
	spoofed.Header.Set(trustedProxySecretHeader, cfg.TrustedProxySecret)
	if got := service.ClientIP(spoofed); got != "203.0.113.10" {
		t.Fatalf("ClientIP(spoofed forwarded chain) = %q, want 203.0.113.10", got)
	}

	allTrusted := httptest.NewRequest(http.MethodPost, "http://stacklab.test/api/auth/login", nil)
	allTrusted.RemoteAddr = "10.1.2.3:4567"
	allTrusted.Header.Set("X-Forwarded-For", "10.9.8.7, 10.2.3.4")
	allTrusted.Header.Set(trustedProxySecretHeader, cfg.TrustedProxySecret)
	if got := service.ClientIP(allTrusted); got != "10.1.2.3" {
		t.Fatalf("ClientIP(all trusted forwarded chain) = %q, want remote proxy 10.1.2.3", got)
	}

	untrusted := httptest.NewRequest(http.MethodPost, "http://stacklab.test/api/auth/login", nil)
	untrusted.RemoteAddr = "198.51.100.20:4567"
	untrusted.Header.Set("X-Forwarded-For", "203.0.113.10")
	untrusted.Header.Set(trustedProxySecretHeader, cfg.TrustedProxySecret)
	if got := service.ClientIP(untrusted); got != "198.51.100.20" {
		t.Fatalf("ClientIP(untrusted proxy) = %q, want 198.51.100.20", got)
	}

	cfg.TrustedProxies = []netip.Prefix{netip.PrefixFrom(netip.MustParseAddr("192.0.2.10"), 32)}
	service = NewService(cfg, openTestStore(t))
	singleIP := httptest.NewRequest(http.MethodPost, "http://stacklab.test/api/auth/login", nil)
	singleIP.RemoteAddr = "192.0.2.10:4567"
	singleIP.Header.Set("X-Forwarded-For", "2001:db8::42")
	singleIP.Header.Set(trustedProxySecretHeader, cfg.TrustedProxySecret)
	if got := service.ClientIP(singleIP); got != "2001:db8::42" {
		t.Fatalf("ClientIP(single trusted proxy) = %q, want 2001:db8::42", got)
	}
}

func TestServiceClientIPRejectsSpoofedForwardedForWithoutProxySecret(t *testing.T) {
	t.Parallel()

	cfg := testConfig("test-password")
	cfg.TrustedProxies = []netip.Prefix{netip.MustParsePrefix("127.0.0.1/32")}
	cfg.TrustedProxySecret = "proxy-secret"
	service := NewService(cfg, openTestStore(t))

	for _, presentedSecret := range []string{"", "wrong-secret"} {
		request := httptest.NewRequest(http.MethodPost, "http://stacklab.test/api/auth/login", nil)
		request.RemoteAddr = "127.0.0.1:4567"
		request.Header.Set("X-Forwarded-For", "203.0.113.200")
		request.Header.Set(trustedProxySecretHeader, presentedSecret)
		if got := service.ClientIP(request); got != "127.0.0.1" {
			t.Fatalf("ClientIP(local spoof with secret %q) = %q, want direct peer", presentedSecret, got)
		}
	}
}

func TestLoginRateLimitSeparatesAuthenticatedProxyClientsButBoundsLocalSpoofing(t *testing.T) {
	t.Parallel()

	ctx := context.Background()
	cfg := testConfig("test-password")
	cfg.LoginMaxFailures = 2
	cfg.LoginFailureWindow = time.Minute
	cfg.LoginLockoutDuration = time.Hour
	cfg.TrustedProxies = []netip.Prefix{netip.MustParsePrefix("127.0.0.1/32")}
	cfg.TrustedProxySecret = "proxy-secret"
	service := NewService(cfg, openTestStore(t))
	if err := service.Bootstrap(ctx); err != nil {
		t.Fatalf("Bootstrap() error = %v", err)
	}

	clientIP := func(forwardedFor, secret string) string {
		request := httptest.NewRequest(http.MethodPost, "http://stacklab.test/api/auth/login", nil)
		request.RemoteAddr = "127.0.0.1:4567"
		request.Header.Set("X-Forwarded-For", forwardedFor)
		request.Header.Set(trustedProxySecretHeader, secret)
		return service.ClientIP(request)
	}

	firstClient := clientIP("203.0.113.10", cfg.TrustedProxySecret)
	secondClient := clientIP("203.0.113.11", cfg.TrustedProxySecret)
	for attempt := 0; attempt < cfg.LoginMaxFailures; attempt++ {
		if _, err := service.Login(ctx, "wrong", "ua", firstClient); !errors.Is(err, ErrInvalidCredentials) {
			t.Fatalf("Login(first proxied client, attempt %d) error = %v", attempt+1, err)
		}
	}
	if _, err := service.Login(ctx, "test-password", "ua", secondClient); err != nil {
		t.Fatalf("Login(second proxied client) error = %v", err)
	}

	// Without the shared header, changing X-Forwarded-For does not create new
	// limiter identities: every attempt remains bound to the direct loopback peer.
	for attempt, spoofedIP := range []string{"198.51.100.1", "198.51.100.2"} {
		if _, err := service.Login(ctx, "wrong", "ua", clientIP(spoofedIP, "")); !errors.Is(err, ErrInvalidCredentials) {
			t.Fatalf("Login(local spoof, attempt %d) error = %v", attempt+1, err)
		}
	}
	if _, err := service.Login(ctx, "test-password", "ua", clientIP("198.51.100.3", "")); !errors.Is(err, ErrTooManyAttempts) {
		t.Fatalf("Login(local spoof after lockout) error = %v, want ErrTooManyAttempts", err)
	}
}

func TestServiceSecureRequestTrustsForwardedProtoOnlyFromConfiguredProxy(t *testing.T) {
	t.Parallel()

	cfg := testConfig("test-password")
	cfg.TrustedProxies = []netip.Prefix{netip.MustParsePrefix("10.0.0.0/8")}
	cfg.TrustedProxySecret = "proxy-secret"
	service := NewService(cfg, openTestStore(t))

	trusted := httptest.NewRequest(http.MethodPost, "http://stacklab.test/api/auth/login", nil)
	trusted.RemoteAddr = "10.1.2.3:4567"
	trusted.Header.Set("X-Forwarded-Proto", "https")
	trusted.Header.Set(trustedProxySecretHeader, cfg.TrustedProxySecret)
	if !service.SecureRequest(trusted) {
		t.Fatal("SecureRequest(trusted proxy with https proto) = false, want true")
	}

	untrusted := httptest.NewRequest(http.MethodPost, "http://stacklab.test/api/auth/login", nil)
	untrusted.RemoteAddr = "198.51.100.20:4567"
	untrusted.Header.Set("X-Forwarded-Proto", "https")
	untrusted.Header.Set(trustedProxySecretHeader, cfg.TrustedProxySecret)
	if service.SecureRequest(untrusted) {
		t.Fatal("SecureRequest(untrusted proxy with https proto) = true, want false")
	}

	directTLS := httptest.NewRequest(http.MethodPost, "https://stacklab.test/api/auth/login", nil)
	directTLS.RemoteAddr = "198.51.100.20:4567"
	if !service.SecureRequest(directTLS) {
		t.Fatal("SecureRequest(direct TLS) = false, want true")
	}
}

func openTestStore(t *testing.T) *store.Store {
	t.Helper()

	databasePath := filepath.Join(t.TempDir(), "stacklab.db")
	testStore, err := store.Open(databasePath)
	if err != nil {
		t.Fatalf("store.Open() error = %v", err)
	}
	t.Cleanup(func() {
		if err := testStore.Close(); err != nil {
			t.Fatalf("Store.Close() error = %v", err)
		}
	})

	return testStore
}

func testConfig(bootstrapPassword string) config.Config {
	return config.Config{
		BootstrapPassword:       bootstrapPassword,
		SessionCookieName:       "stacklab_session",
		SessionIdleTimeout:      30 * time.Minute,
		SessionAbsoluteLifetime: 24 * time.Hour,
	}
}
