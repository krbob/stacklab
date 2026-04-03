package auth

import (
	"context"
	"errors"
	"net/http"
	"net/http/httptest"
	"path/filepath"
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
