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
	"net/netip"
	"net/url"
	"strings"
	"sync"
	"time"

	"stacklab/internal/config"
	"stacklab/internal/store"

	"golang.org/x/crypto/argon2"
)

var (
	ErrUnauthorized       = errors.New("unauthorized")
	ErrInvalidCredentials = errors.New("invalid credentials")
	ErrNotConfigured      = errors.New("auth not configured")
	ErrTooManyAttempts    = errors.New("too many login attempts")
)

const localUserID = "local"

type Service struct {
	cfg           config.Config
	store         *store.Store
	now           func() time.Time
	newSessionID  func() (string, error)
	loginAttempts map[string]loginAttemptState
	loginMu       sync.Mutex
}

type loginAttemptState struct {
	FirstFailedAt time.Time
	FailedCount   int
	LockedUntil   time.Time
}

type Session struct {
	ID        string
	UserID    string
	CreatedAt time.Time
	ExpiresAt time.Time
}

func NewService(cfg config.Config, authStore *store.Store) *Service {
	return &Service{
		cfg:           cfg,
		store:         authStore,
		now:           func() time.Time { return time.Now().UTC() },
		newSessionID:  func() (string, error) { return randomToken(32) },
		loginAttempts: make(map[string]loginAttemptState),
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
	now := s.now().UTC()
	if s.loginLocked(ipAddress, now) {
		return Session{}, ErrTooManyAttempts
	}

	passwordHash, configured, err := s.store.PasswordHash(ctx)
	if err != nil {
		return Session{}, err
	}
	if !configured || passwordHash == "" {
		return Session{}, ErrNotConfigured
	}
	if err := verifyPassword(passwordHash, password); err != nil {
		s.recordLoginFailure(ipAddress, now)
		return Session{}, ErrInvalidCredentials
	}
	s.clearLoginFailures(ipAddress)

	absoluteDeadline := now.Add(s.cfg.SessionAbsoluteLifetime)
	idleDeadline := now.Add(s.cfg.SessionIdleTimeout)
	expiresAt := minTime(idleDeadline, absoluteDeadline)
	sessionID, err := s.newSessionID()
	if err != nil {
		return Session{}, err
	}

	session := Session{
		ID:        sessionID,
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

func (s *Service) loginLocked(ipAddress string, now time.Time) bool {
	key := loginAttemptKey(ipAddress)
	s.loginMu.Lock()
	defer s.loginMu.Unlock()

	attempt := s.loginAttempts[key]
	if attempt.LockedUntil.After(now) {
		return true
	}
	if !attempt.LockedUntil.IsZero() || (attempt.FailedCount > 0 && now.Sub(attempt.FirstFailedAt) > s.loginFailureWindow()) {
		delete(s.loginAttempts, key)
	}
	return false
}

func (s *Service) recordLoginFailure(ipAddress string, now time.Time) {
	key := loginAttemptKey(ipAddress)
	s.loginMu.Lock()
	defer s.loginMu.Unlock()

	attempt := s.loginAttempts[key]
	if attempt.FailedCount == 0 || now.Sub(attempt.FirstFailedAt) > s.loginFailureWindow() {
		attempt = loginAttemptState{FirstFailedAt: now}
	}
	attempt.FailedCount++
	if attempt.FailedCount >= s.loginMaxFailures() {
		attempt.LockedUntil = now.Add(s.loginLockoutDuration())
	}
	s.loginAttempts[key] = attempt
}

func (s *Service) clearLoginFailures(ipAddress string) {
	key := loginAttemptKey(ipAddress)
	s.loginMu.Lock()
	defer s.loginMu.Unlock()
	delete(s.loginAttempts, key)
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

func (s *Service) UpdatePassword(ctx context.Context, currentPassword, newPassword string) error {
	passwordHash, configured, err := s.store.PasswordHash(ctx)
	if err != nil {
		return err
	}
	if !configured || passwordHash == "" {
		return ErrNotConfigured
	}
	if err := verifyPassword(passwordHash, currentPassword); err != nil {
		return ErrInvalidCredentials
	}

	updatedHash, err := hashPassword(newPassword)
	if err != nil {
		return err
	}

	return s.store.SetPasswordHash(ctx, updatedHash, time.Now().UTC())
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

func (s *Service) ClientIP(r *http.Request) string {
	return clientIP(r, s.cfg.TrustedProxies)
}

func (s *Service) SecureRequest(r *http.Request) bool {
	return secureRequest(r, s.cfg.TrustedProxies)
}

func ClientIP(r *http.Request) string {
	return clientIP(r, nil)
}

func clientIP(r *http.Request, trustedProxies []netip.Prefix) string {
	host, _, err := net.SplitHostPort(r.RemoteAddr)
	if err == nil {
		if trustedForwardedIP := trustedForwardedFor(host, r.Header.Get("X-Forwarded-For"), trustedProxies); trustedForwardedIP != "" {
			return trustedForwardedIP
		}
		return host
	}
	return r.RemoteAddr
}

func secureRequest(r *http.Request, trustedProxies []netip.Prefix) bool {
	if r.TLS != nil {
		return true
	}
	host, _, err := net.SplitHostPort(r.RemoteAddr)
	if err != nil {
		host = r.RemoteAddr
	}
	return trustedForwardedProto(host, r.Header.Get("X-Forwarded-Proto"), trustedProxies) == "https"
}

func trustedForwardedFor(remoteHost, headerValue string, trustedProxies []netip.Prefix) string {
	if len(trustedProxies) == 0 || strings.TrimSpace(headerValue) == "" {
		return ""
	}
	remoteAddr, err := netip.ParseAddr(strings.TrimSpace(remoteHost))
	if err != nil || !isTrustedProxy(remoteAddr, trustedProxies) {
		return ""
	}
	parts := strings.Split(headerValue, ",")
	for i := len(parts) - 1; i >= 0; i-- {
		addr, err := netip.ParseAddr(strings.TrimSpace(parts[i]))
		if err != nil {
			continue
		}
		if isTrustedProxy(addr, trustedProxies) {
			continue
		}
		return addr.String()
	}
	return ""
}

func trustedForwardedProto(remoteHost, headerValue string, trustedProxies []netip.Prefix) string {
	if len(trustedProxies) == 0 || strings.TrimSpace(headerValue) == "" {
		return ""
	}
	remoteAddr, err := netip.ParseAddr(strings.TrimSpace(remoteHost))
	if err != nil || !isTrustedProxy(remoteAddr, trustedProxies) {
		return ""
	}
	parts := strings.Split(headerValue, ",")
	if len(parts) == 0 {
		return ""
	}
	return strings.ToLower(strings.TrimSpace(parts[0]))
}

func isTrustedProxy(addr netip.Addr, trustedProxies []netip.Prefix) bool {
	for _, prefix := range trustedProxies {
		if prefix.Contains(addr) {
			return true
		}
	}
	return false
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

func loginAttemptKey(ipAddress string) string {
	trimmed := strings.TrimSpace(ipAddress)
	if trimmed == "" {
		return "unknown"
	}
	return trimmed
}

func (s *Service) loginMaxFailures() int {
	if s.cfg.LoginMaxFailures <= 0 {
		return 5
	}
	return s.cfg.LoginMaxFailures
}

func (s *Service) loginFailureWindow() time.Duration {
	if s.cfg.LoginFailureWindow <= 0 {
		return 5 * time.Minute
	}
	return s.cfg.LoginFailureWindow
}

func (s *Service) loginLockoutDuration() time.Duration {
	if s.cfg.LoginLockoutDuration <= 0 {
		return 5 * time.Minute
	}
	return s.cfg.LoginLockoutDuration
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

func randomToken(length int) (string, error) {
	bytes := make([]byte, length)
	if _, err := rand.Read(bytes); err != nil {
		return "", fmt.Errorf("generate session token: %w", err)
	}
	return base64.RawURLEncoding.EncodeToString(bytes), nil
}

func minTime(left, right time.Time) time.Time {
	if left.Before(right) {
		return left
	}
	return right
}
