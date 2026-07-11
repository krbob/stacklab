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

const (
	localUserID                         = "local"
	defaultLoginVerificationConcurrency = 2
	defaultMaxTrackedLoginClients       = 4096
)

type Service struct {
	cfg                    config.Config
	store                  *store.Store
	now                    func() time.Time
	newSessionID           func() (string, error)
	passwordVerifier       func(string, string) error
	loginVerificationSlots chan struct{}
	loginAttempts          map[string]loginAttemptState
	loginInFlight          map[string]struct{}
	maxTrackedLoginClients int
	loginMu                sync.Mutex
	sessionLifecycle       *sessionLifecycleHub
	sessionTransitionMu    sync.Mutex
	sessionTerminationMu   sync.RWMutex
	sessionTerminationHook func(SessionTermination)
}

type loginAttemptState struct {
	FirstFailedAt time.Time
	FailedCount   int
	LockedUntil   time.Time
	UpdatedAt     time.Time
}

type Session struct {
	ID        string
	UserID    string
	CreatedAt time.Time
	ExpiresAt time.Time
}

func NewService(cfg config.Config, authStore *store.Store) *Service {
	service := &Service{
		cfg:                    cfg,
		store:                  authStore,
		now:                    func() time.Time { return time.Now().UTC() },
		newSessionID:           func() (string, error) { return randomToken(32) },
		passwordVerifier:       verifyPassword,
		loginVerificationSlots: make(chan struct{}, defaultLoginVerificationConcurrency),
		loginAttempts:          make(map[string]loginAttemptState),
		loginInFlight:          make(map[string]struct{}),
		maxTrackedLoginClients: defaultMaxTrackedLoginClients,
	}
	service.sessionLifecycle = newSessionLifecycleHub(
		func() time.Time { return service.now().UTC() },
		cfg.SessionIdleTimeout,
		cfg.SessionAbsoluteLifetime,
		func(sessionID string, reason SessionTerminationReason) {
			service.expireSession(sessionID, reason)
		},
	)
	return service
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
	release, err := s.acquireLoginVerification(ctx, ipAddress)
	if err != nil {
		return Session{}, err
	}
	defer release()

	credentials, configured, err := s.store.PasswordCredentials(ctx)
	if err != nil {
		return Session{}, err
	}
	if !configured || credentials.Hash == "" {
		return Session{}, ErrNotConfigured
	}
	if err := s.passwordVerifier(credentials.Hash, password); err != nil {
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

	if err := s.store.CreateSessionAtPasswordVersion(ctx, store.Session{
		ID:              session.ID,
		UserID:          session.UserID,
		CreatedAt:       now,
		LastSeenAt:      now,
		ExpiresAt:       expiresAt,
		UserAgent:       userAgent,
		IPAddress:       ipAddress,
		PasswordVersion: credentials.Version,
	}, credentials.Version); err != nil {
		if errors.Is(err, store.ErrPasswordVersionChanged) {
			return Session{}, ErrInvalidCredentials
		}
		return Session{}, err
	}

	return session, nil
}

func (s *Service) acquireLoginVerification(ctx context.Context, ipAddress string) (func(), error) {
	if err := ctx.Err(); err != nil {
		return nil, err
	}
	select {
	case s.loginVerificationSlots <- struct{}{}:
	default:
		return nil, ErrTooManyAttempts
	}

	key := loginAttemptKey(ipAddress)
	s.loginMu.Lock()
	if _, exists := s.loginInFlight[key]; exists {
		s.loginMu.Unlock()
		<-s.loginVerificationSlots
		return nil, ErrTooManyAttempts
	}
	s.loginInFlight[key] = struct{}{}
	s.loginMu.Unlock()

	var once sync.Once
	return func() {
		once.Do(func() {
			s.loginMu.Lock()
			delete(s.loginInFlight, key)
			s.loginMu.Unlock()
			<-s.loginVerificationSlots
		})
	}, nil
}

func (s *Service) loginLocked(ipAddress string, now time.Time) bool {
	key := loginAttemptKey(ipAddress)
	s.loginMu.Lock()
	defer s.loginMu.Unlock()

	attempt, exists := s.loginAttempts[key]
	if !exists {
		return false
	}
	if loginAttemptExpired(attempt, now, s.loginFailureWindow()) {
		delete(s.loginAttempts, key)
		return false
	}
	return attempt.LockedUntil.After(now)
}

func (s *Service) recordLoginFailure(ipAddress string, now time.Time) {
	key := loginAttemptKey(ipAddress)
	s.loginMu.Lock()
	defer s.loginMu.Unlock()
	s.pruneLoginAttemptsLocked(now)

	attempt, exists := s.loginAttempts[key]
	if attempt.FailedCount == 0 || now.Sub(attempt.FirstFailedAt) > s.loginFailureWindow() {
		attempt = loginAttemptState{FirstFailedAt: now}
	}
	attempt.FailedCount++
	attempt.UpdatedAt = now
	if attempt.FailedCount >= s.loginMaxFailures() {
		attempt.LockedUntil = now.Add(s.loginLockoutDuration())
	}
	if !exists && len(s.loginAttempts) >= s.maxTrackedClients() {
		s.evictOldestLoginAttemptLocked()
	}
	s.loginAttempts[key] = attempt
}

func (s *Service) pruneLoginAttemptsLocked(now time.Time) {
	for key, attempt := range s.loginAttempts {
		if loginAttemptExpired(attempt, now, s.loginFailureWindow()) {
			delete(s.loginAttempts, key)
		}
	}
}

func loginAttemptExpired(attempt loginAttemptState, now time.Time, failureWindow time.Duration) bool {
	if !attempt.LockedUntil.IsZero() {
		return !attempt.LockedUntil.After(now)
	}
	return attempt.FailedCount > 0 && now.Sub(attempt.FirstFailedAt) > failureWindow
}

func (s *Service) evictOldestLoginAttemptLocked() {
	oldestKey := ""
	var oldestUpdatedAt time.Time
	for key, attempt := range s.loginAttempts {
		if oldestKey == "" || attempt.UpdatedAt.Before(oldestUpdatedAt) || (attempt.UpdatedAt.Equal(oldestUpdatedAt) && key < oldestKey) {
			oldestKey = key
			oldestUpdatedAt = attempt.UpdatedAt
		}
	}
	if oldestKey != "" {
		delete(s.loginAttempts, oldestKey)
	}
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
	s.sessionTransitionMu.Lock()
	defer s.sessionTransitionMu.Unlock()
	if err := s.store.RevokeSession(ctx, sessionID, s.now().UTC()); err != nil {
		if errors.Is(err, store.ErrNotFound) {
			return ErrUnauthorized
		}
		return err
	}
	s.terminateSession(sessionID, SessionTerminationLogout)
	return nil
}

func (s *Service) UpdatePassword(ctx context.Context, currentPassword, newPassword string) error {
	credentials, configured, err := s.store.PasswordCredentials(ctx)
	if err != nil {
		return err
	}
	if !configured || credentials.Hash == "" {
		return ErrNotConfigured
	}
	if err := verifyPassword(credentials.Hash, currentPassword); err != nil {
		return ErrInvalidCredentials
	}

	updatedHash, err := hashPassword(newPassword)
	if err != nil {
		return err
	}

	s.sessionTransitionMu.Lock()
	defer s.sessionTransitionMu.Unlock()
	if err := s.store.UpdatePasswordAndRevokeSessions(ctx, credentials.Version, updatedHash, s.now().UTC()); err != nil {
		if errors.Is(err, store.ErrPasswordVersionChanged) {
			return ErrInvalidCredentials
		}
		return err
	}
	s.terminateAllSessions(SessionTerminationPasswordChanged)
	return nil
}

func (s *Service) AuthenticateWebSocket(ctx context.Context, r *http.Request) (Session, <-chan SessionTermination, func(), error) {
	s.sessionTransitionMu.Lock()
	defer s.sessionTransitionMu.Unlock()

	session, err := s.authenticateRequest(ctx, r)
	if err != nil {
		return Session{}, nil, nil, err
	}
	terminations, unsubscribe := s.sessionLifecycle.Subscribe(session)
	return session, terminations, unsubscribe, nil
}

// TouchSessionActivity extends an active WebSocket's idle lease in memory and
// only persists it at a bounded interval. It performs no SQLite read.
func (s *Service) TouchSessionActivity(ctx context.Context, sessionID string) error {
	expiresAt, shouldPersist, active := s.sessionLifecycle.Touch(sessionID)
	if !active {
		return ErrUnauthorized
	}
	if !shouldPersist {
		return nil
	}
	if err := s.store.TouchSession(ctx, sessionID, s.now().UTC(), expiresAt); err != nil {
		if errors.Is(err, store.ErrNotFound) {
			s.terminateSession(sessionID, SessionTerminationRevoked)
			return ErrUnauthorized
		}
		return err
	}
	return nil
}

func (s *Service) AuthenticateRequest(ctx context.Context, r *http.Request) (Session, error) {
	s.sessionTransitionMu.Lock()
	defer s.sessionTransitionMu.Unlock()
	return s.authenticateRequest(ctx, r)
}

func (s *Service) authenticateRequest(ctx context.Context, r *http.Request) (Session, error) {
	cookie, err := r.Cookie(s.cfg.SessionCookieName)
	if err != nil || cookie.Value == "" {
		return Session{}, ErrUnauthorized
	}

	record, err := s.store.SessionAtCurrentPasswordVersion(ctx, cookie.Value)
	if err != nil {
		if errors.Is(err, store.ErrNotFound) {
			s.terminateSession(cookie.Value, SessionTerminationRevoked)
			return Session{}, ErrUnauthorized
		}
		return Session{}, err
	}

	now := s.now().UTC()
	if record.RevokedAt != nil || !now.Before(record.ExpiresAt) || !now.Before(record.CreatedAt.Add(s.cfg.SessionAbsoluteLifetime)) {
		_ = s.store.RevokeSession(ctx, record.ID, now)
		reason := SessionTerminationRevoked
		switch {
		case !now.Before(record.CreatedAt.Add(s.cfg.SessionAbsoluteLifetime)):
			reason = SessionTerminationAbsoluteExpired
		case !now.Before(record.ExpiresAt):
			reason = SessionTerminationIdleExpired
		}
		s.terminateSession(record.ID, reason)
		return Session{}, ErrUnauthorized
	}

	nextExpiresAt := minTime(now.Add(s.cfg.SessionIdleTimeout), record.CreatedAt.Add(s.cfg.SessionAbsoluteLifetime))
	if err := s.store.TouchSession(ctx, record.ID, now, nextExpiresAt); err != nil {
		if errors.Is(err, store.ErrNotFound) {
			s.terminateSession(record.ID, SessionTerminationRevoked)
			return Session{}, ErrUnauthorized
		}
		return Session{}, err
	}
	s.sessionLifecycle.TouchPersisted(record.ID, nextExpiresAt)

	return Session{
		ID:        record.ID,
		UserID:    record.UserID,
		CreatedAt: record.CreatedAt,
		ExpiresAt: nextExpiresAt,
	}, nil
}

func (s *Service) expireSession(sessionID string, reason SessionTerminationReason) {
	s.sessionTransitionMu.Lock()
	defer s.sessionTransitionMu.Unlock()

	revokeCtx, cancel := context.WithTimeout(context.Background(), 5*time.Second)
	defer cancel()
	_ = s.store.RevokeSession(revokeCtx, sessionID, s.now().UTC())
	// A WebSocket may have authenticated between the timer firing and the
	// persistent revocation. Terminating again closes that race.
	s.terminateSession(sessionID, reason)
}

func (s *Service) Shutdown(ctx context.Context) error {
	if s.sessionLifecycle == nil {
		return nil
	}
	return s.sessionLifecycle.Shutdown(ctx)
}

// SetSessionTerminationHook connects privileged resources, such as PTYs, to
// authentication lifecycle events independently of WebSocket attachment state.
func (s *Service) SetSessionTerminationHook(hook func(SessionTermination)) {
	s.sessionTerminationMu.Lock()
	s.sessionTerminationHook = hook
	s.sessionTerminationMu.Unlock()
}

func (s *Service) terminateSession(sessionID string, reason SessionTerminationReason) {
	s.sessionLifecycle.Terminate(sessionID, reason)
	s.notifySessionTermination(SessionTermination{SessionID: sessionID, Reason: reason})
}

func (s *Service) terminateAllSessions(reason SessionTerminationReason) {
	s.sessionLifecycle.TerminateAll(reason)
	s.notifySessionTermination(SessionTermination{All: true, Reason: reason})
}

func (s *Service) notifySessionTermination(termination SessionTermination) {
	s.sessionTerminationMu.RLock()
	hook := s.sessionTerminationHook
	s.sessionTerminationMu.RUnlock()
	if hook != nil {
		hook(termination)
	}
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

func (s *Service) maxTrackedClients() int {
	if s.maxTrackedLoginClients <= 0 {
		return defaultMaxTrackedLoginClients
	}
	return s.maxTrackedLoginClients
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
