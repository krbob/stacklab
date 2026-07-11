package auth

import (
	"context"
	"errors"
	"net/http"
	"net/http/httptest"
	"net/netip"
	"path/filepath"
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
	service := NewService(testConfig("secret"), authStore)

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
	if passwordHash == "" || passwordHash == "secret" {
		t.Fatalf("expected hashed password to be stored")
	}

	if _, err := service.Login(ctx, "wrong", "ua", "127.0.0.1"); !errors.Is(err, ErrInvalidCredentials) {
		t.Fatalf("Login(wrong) error = %v, want ErrInvalidCredentials", err)
	}

	session, err := service.Login(ctx, "secret", "ua", "127.0.0.1")
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

	if err := service.UpdatePassword(ctx, "secret", "newsecret"); err != nil {
		t.Fatalf("UpdatePassword() error = %v", err)
	}

	if _, err := service.Login(ctx, "secret", "ua", "127.0.0.1"); !errors.Is(err, ErrInvalidCredentials) {
		t.Fatalf("Login(old password) error = %v, want ErrInvalidCredentials", err)
	}
	if _, err := service.Login(ctx, "newsecret", "ua", "127.0.0.1"); err != nil {
		t.Fatalf("Login(new password) error = %v", err)
	}
}

func TestAuthenticateRequestRejectsExpiredSession(t *testing.T) {
	t.Parallel()

	ctx := context.Background()
	authStore := openTestStore(t)
	service := NewService(testConfig("secret"), authStore)

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

func TestLoginLocksClientAfterRepeatedFailures(t *testing.T) {
	t.Parallel()

	ctx := context.Background()
	authStore := openTestStore(t)
	cfg := testConfig("secret")
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
	if _, err := service.Login(ctx, "secret", "ua", "127.0.0.1"); !errors.Is(err, ErrTooManyAttempts) {
		t.Fatalf("Login(locked client) error = %v, want ErrTooManyAttempts", err)
	}
	if _, err := service.Login(ctx, "secret", "ua", "127.0.0.2"); err != nil {
		t.Fatalf("Login(other client) error = %v", err)
	}

	now = now.Add(cfg.LoginLockoutDuration + time.Nanosecond)
	if _, err := service.Login(ctx, "secret", "ua", "127.0.0.1"); err != nil {
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
	service := NewService(testConfig("secret"), authStore)
	tokenErr := errors.New("entropy unavailable")
	service.newSessionID = func() (string, error) {
		return "", tokenErr
	}

	if err := service.Bootstrap(ctx); err != nil {
		t.Fatalf("Bootstrap() error = %v", err)
	}

	if _, err := service.Login(ctx, "secret", "ua", "127.0.0.1"); !errors.Is(err, tokenErr) {
		t.Fatalf("Login(token error) error = %v, want %v", err, tokenErr)
	}
}

func TestServiceClientIPTrustsForwardedForOnlyFromConfiguredProxy(t *testing.T) {
	t.Parallel()

	cfg := testConfig("secret")
	cfg.TrustedProxies = []netip.Prefix{netip.MustParsePrefix("10.0.0.0/8")}
	service := NewService(cfg, openTestStore(t))

	trusted := httptest.NewRequest(http.MethodPost, "http://stacklab.test/api/auth/login", nil)
	trusted.RemoteAddr = "10.1.2.3:4567"
	trusted.Header.Set("X-Forwarded-For", "203.0.113.10, 10.1.2.3")
	if got := service.ClientIP(trusted); got != "203.0.113.10" {
		t.Fatalf("ClientIP(trusted proxy) = %q, want 203.0.113.10", got)
	}

	spoofed := httptest.NewRequest(http.MethodPost, "http://stacklab.test/api/auth/login", nil)
	spoofed.RemoteAddr = "10.1.2.3:4567"
	spoofed.Header.Set("X-Forwarded-For", "198.51.100.250, 203.0.113.10, 10.2.3.4")
	if got := service.ClientIP(spoofed); got != "203.0.113.10" {
		t.Fatalf("ClientIP(spoofed forwarded chain) = %q, want 203.0.113.10", got)
	}

	allTrusted := httptest.NewRequest(http.MethodPost, "http://stacklab.test/api/auth/login", nil)
	allTrusted.RemoteAddr = "10.1.2.3:4567"
	allTrusted.Header.Set("X-Forwarded-For", "10.9.8.7, 10.2.3.4")
	if got := service.ClientIP(allTrusted); got != "10.1.2.3" {
		t.Fatalf("ClientIP(all trusted forwarded chain) = %q, want remote proxy 10.1.2.3", got)
	}

	untrusted := httptest.NewRequest(http.MethodPost, "http://stacklab.test/api/auth/login", nil)
	untrusted.RemoteAddr = "198.51.100.20:4567"
	untrusted.Header.Set("X-Forwarded-For", "203.0.113.10")
	if got := service.ClientIP(untrusted); got != "198.51.100.20" {
		t.Fatalf("ClientIP(untrusted proxy) = %q, want 198.51.100.20", got)
	}

	cfg.TrustedProxies = []netip.Prefix{netip.PrefixFrom(netip.MustParseAddr("192.0.2.10"), 32)}
	service = NewService(cfg, openTestStore(t))
	singleIP := httptest.NewRequest(http.MethodPost, "http://stacklab.test/api/auth/login", nil)
	singleIP.RemoteAddr = "192.0.2.10:4567"
	singleIP.Header.Set("X-Forwarded-For", "2001:db8::42")
	if got := service.ClientIP(singleIP); got != "2001:db8::42" {
		t.Fatalf("ClientIP(single trusted proxy) = %q, want 2001:db8::42", got)
	}
}

func TestServiceSecureRequestTrustsForwardedProtoOnlyFromConfiguredProxy(t *testing.T) {
	t.Parallel()

	cfg := testConfig("secret")
	cfg.TrustedProxies = []netip.Prefix{netip.MustParsePrefix("10.0.0.0/8")}
	service := NewService(cfg, openTestStore(t))

	trusted := httptest.NewRequest(http.MethodPost, "http://stacklab.test/api/auth/login", nil)
	trusted.RemoteAddr = "10.1.2.3:4567"
	trusted.Header.Set("X-Forwarded-Proto", "https")
	if !service.SecureRequest(trusted) {
		t.Fatal("SecureRequest(trusted proxy with https proto) = false, want true")
	}

	untrusted := httptest.NewRequest(http.MethodPost, "http://stacklab.test/api/auth/login", nil)
	untrusted.RemoteAddr = "198.51.100.20:4567"
	untrusted.Header.Set("X-Forwarded-Proto", "https")
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
