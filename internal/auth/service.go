package auth

import (
	"context"
	"crypto/rand"
	"crypto/subtle"
	"encoding/base64"
	"errors"
	"fmt"
	"net"
	"net/http"
	"net/url"
	"strings"
	"time"

	"stacklab/internal/config"
	"stacklab/internal/store"

	"golang.org/x/crypto/argon2"
)

var (
	ErrUnauthorized       = errors.New("unauthorized")
	ErrInvalidCredentials = errors.New("invalid credentials")
	ErrNotConfigured      = errors.New("auth not configured")
)

const localUserID = "local"

type Service struct {
	cfg   config.Config
	store *store.Store
}

type Session struct {
	ID        string
	UserID    string
	CreatedAt time.Time
	ExpiresAt time.Time
}

func NewService(cfg config.Config, authStore *store.Store) *Service {
	return &Service{
		cfg:   cfg,
		store: authStore,
	}
}

func (s *Service) Bootstrap(ctx context.Context) error {
	passwordHash, configured, err := s.store.PasswordHash(ctx)
	if err != nil {
		return err
	}
	if configured && passwordHash != "" {
		return nil
	}
	if s.cfg.BootstrapPassword == "" {
		return ErrNotConfigured
	}

	hash, err := hashPassword(s.cfg.BootstrapPassword)
	if err != nil {
		return err
	}
	return s.store.SetPasswordHash(ctx, hash, time.Now().UTC())
}

func (s *Service) Login(ctx context.Context, password, userAgent, ipAddress string) (Session, error) {
	passwordHash, configured, err := s.store.PasswordHash(ctx)
	if err != nil {
		return Session{}, err
	}
	if !configured || passwordHash == "" {
		return Session{}, ErrNotConfigured
	}
	if err := verifyPassword(passwordHash, password); err != nil {
		return Session{}, ErrInvalidCredentials
	}

	now := time.Now().UTC()
	absoluteDeadline := now.Add(s.cfg.SessionAbsoluteLifetime)
	idleDeadline := now.Add(s.cfg.SessionIdleTimeout)
	expiresAt := minTime(idleDeadline, absoluteDeadline)

	session := Session{
		ID:        randomToken(32),
		UserID:    localUserID,
		CreatedAt: now,
		ExpiresAt: expiresAt,
	}

	if err := s.store.CreateSession(ctx, store.Session{
		ID:         session.ID,
		UserID:     session.UserID,
		CreatedAt:  now,
		LastSeenAt: now,
		ExpiresAt:  expiresAt,
		UserAgent:  userAgent,
		IPAddress:  ipAddress,
	}); err != nil {
		return Session{}, err
	}

	return session, nil
}

func (s *Service) Logout(ctx context.Context, sessionID string) error {
	if sessionID == "" {
		return ErrUnauthorized
	}
	if err := s.store.RevokeSession(ctx, sessionID, time.Now().UTC()); err != nil {
		if errors.Is(err, store.ErrNotFound) {
			return ErrUnauthorized
		}
		return err
	}
	return nil
}

func (s *Service) AuthenticateRequest(ctx context.Context, r *http.Request) (Session, error) {
	cookie, err := r.Cookie(s.cfg.SessionCookieName)
	if err != nil || cookie.Value == "" {
		return Session{}, ErrUnauthorized
	}

	record, err := s.store.SessionByID(ctx, cookie.Value)
	if err != nil {
		if errors.Is(err, store.ErrNotFound) {
			return Session{}, ErrUnauthorized
		}
		return Session{}, err
	}

	now := time.Now().UTC()
	if record.RevokedAt != nil || now.After(record.ExpiresAt) || now.After(record.CreatedAt.Add(s.cfg.SessionAbsoluteLifetime)) {
		_ = s.store.RevokeSession(ctx, record.ID, now)
		return Session{}, ErrUnauthorized
	}

	nextExpiresAt := minTime(now.Add(s.cfg.SessionIdleTimeout), record.CreatedAt.Add(s.cfg.SessionAbsoluteLifetime))
	if err := s.store.TouchSession(ctx, record.ID, now, nextExpiresAt); err != nil && !errors.Is(err, store.ErrNotFound) {
		return Session{}, err
	}

	return Session{
		ID:        record.ID,
		UserID:    record.UserID,
		CreatedAt: record.CreatedAt,
		ExpiresAt: nextExpiresAt,
	}, nil
}

func (s *Service) SessionCookie(session Session) *http.Cookie {
	return &http.Cookie{
		Name:     s.cfg.SessionCookieName,
		Value:    session.ID,
		Path:     "/",
		HttpOnly: true,
		SameSite: http.SameSiteStrictMode,
		Secure:   s.cfg.CookieSecure,
		Expires:  session.ExpiresAt,
	}
}

func (s *Service) ClearSessionCookie() *http.Cookie {
	return &http.Cookie{
		Name:     s.cfg.SessionCookieName,
		Value:    "",
		Path:     "/",
		HttpOnly: true,
		SameSite: http.SameSiteStrictMode,
		Secure:   s.cfg.CookieSecure,
		MaxAge:   -1,
		Expires:  time.Unix(0, 0).UTC(),
	}
}

func ClientIP(r *http.Request) string {
	host, _, err := net.SplitHostPort(r.RemoteAddr)
	if err == nil {
		return host
	}
	return r.RemoteAddr
}

func SameOrigin(r *http.Request) bool {
	origin := strings.TrimSpace(r.Header.Get("Origin"))
	if origin == "" {
		return true
	}

	parsedOrigin, err := url.Parse(origin)
	if err != nil {
		return false
	}
	return parsedOrigin.Host == r.Host
}

func hashPassword(password string) (string, error) {
	if password == "" {
		return "", fmt.Errorf("password must not be empty")
	}

	salt := make([]byte, 16)
	if _, err := rand.Read(salt); err != nil {
		return "", fmt.Errorf("generate salt: %w", err)
	}

	const (
		memory      = 64 * 1024
		iterations  = 3
		parallelism = 2
		keyLength   = 32
	)

	hash := argon2.IDKey([]byte(password), salt, iterations, memory, parallelism, keyLength)
	return fmt.Sprintf(
		"$stacklab$argon2id$v=19$m=%d,t=%d,p=%d$%s$%s",
		memory,
		iterations,
		parallelism,
		base64.RawStdEncoding.EncodeToString(salt),
		base64.RawStdEncoding.EncodeToString(hash),
	), nil
}

func verifyPassword(encodedHash, password string) error {
	parts := strings.Split(encodedHash, "$")
	if len(parts) != 7 || parts[1] != "stacklab" || parts[2] != "argon2id" {
		return ErrInvalidCredentials
	}

	var memory uint32
	var iterations uint32
	var parallelism uint8
	if _, err := fmt.Sscanf(parts[4], "m=%d,t=%d,p=%d", &memory, &iterations, &parallelism); err != nil {
		return ErrInvalidCredentials
	}

	salt, err := base64.RawStdEncoding.DecodeString(parts[5])
	if err != nil {
		return ErrInvalidCredentials
	}
	expectedHash, err := base64.RawStdEncoding.DecodeString(parts[6])
	if err != nil {
		return ErrInvalidCredentials
	}

	computedHash := argon2.IDKey([]byte(password), salt, iterations, memory, parallelism, uint32(len(expectedHash)))
	if subtle.ConstantTimeCompare(expectedHash, computedHash) != 1 {
		return ErrInvalidCredentials
	}
	return nil
}

func randomToken(length int) string {
	bytes := make([]byte, length)
	_, _ = rand.Read(bytes)
	return base64.RawURLEncoding.EncodeToString(bytes)
}

func minTime(left, right time.Time) time.Time {
	if left.Before(right) {
		return left
	}
	return right
}
