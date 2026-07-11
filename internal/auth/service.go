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
	"strconv"
	"strings"
	"sync"
	"time"
	"unicode/utf8"

	"stacklab/internal/config"
	"stacklab/internal/store"

	"golang.org/x/crypto/argon2"
)

var (
	ErrUnauthorized       = errors.New("unauthorized")
	ErrInvalidCredentials = errors.New("invalid credentials")
	ErrInvalidPassword    = errors.New("invalid password")
	ErrNotConfigured      = errors.New("auth not configured")
	ErrTooManyAttempts    = errors.New("too many login attempts")
)

const (
	PasswordMinimumLength               = 12
	PasswordMaximumLength               = 256
	localUserID                         = "local"
	defaultLoginVerificationConcurrency = 2
	defaultMaxTrackedLoginClients       = 4096
	trustedProxySecretHeader            = "X-Stacklab-Proxy-Secret"
	argon2Memory                        = uint32(64 * 1024)
	argon2Iterations                    = uint32(3)
	argon2Parallelism                   = uint8(2)
	argon2SaltLength                    = 16
	argon2KeyLength                     = uint32(32)
	argon2MinimumMemory                 = uint32(8 * 1024)
	argon2MaximumMemory                 = uint32(128 * 1024)
	argon2MinimumIterations             = uint32(1)
	argon2MaximumIterations             = uint32(10)
	argon2MinimumParallelism            = uint8(1)
	argon2MaximumParallelism            = uint8(8)
	argon2MinimumSaltLength             = 16
	argon2MaximumSaltLength             = 64
	argon2MinimumHashLength             = 16
	argon2MaximumHashLength             = 64
	maximumEncodedPasswordHashLength    = 256
)

var passwordHashBase64 = base64.RawStdEncoding.Strict()

type passwordHashParameters struct {
	memory      uint32
	iterations  uint32
	parallelism uint8
}

type parsedPasswordHash struct {
	parameters passwordHashParameters
	salt       []byte
	hash       []byte
}

type serviceStore interface {
	PasswordHash(context.Context) (string, bool, error)
	SetPasswordHash(context.Context, string, time.Time) error
	PasswordCredentials(context.Context) (store.PasswordCredentials, bool, error)
	CreateSessionAtPasswordVersion(context.Context, store.Session, int) error
	RevokeSession(context.Context, string, time.Time) error
	UpdatePasswordAndRevokeSessions(context.Context, int, string, time.Time) error
	SessionAtCurrentPasswordVersion(context.Context, string) (store.Session, error)
	TouchSession(context.Context, string, time.Time, time.Time, time.Time) error
}

type Service struct {
	cfg                    config.Config
	store                  serviceStore
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
	ID         string
	UserID     string
	CreatedAt  time.Time
	LastSeenAt time.Time
	ExpiresAt  time.Time
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
		ID:         sessionID,
		UserID:     localUserID,
		CreatedAt:  now,
		LastSeenAt: now,
		ExpiresAt:  expiresAt,
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
// only persists it at a bounded interval. The normal path performs no SQLite
// read; a compare-and-swap conflict is re-read so concurrent activity is not
// mistaken for revocation.
func (s *Service) TouchSessionActivity(ctx context.Context, sessionID string) error {
	touch := s.sessionLifecycle.Touch(sessionID)
	if !touch.active {
		return ErrUnauthorized
	}
	if !touch.shouldPersist {
		return nil
	}

	s.sessionTransitionMu.Lock()
	defer s.sessionTransitionMu.Unlock()

	if err := s.store.TouchSession(ctx, sessionID, touch.expectedLastSeenAt, touch.activityAt, touch.expiresAt); err != nil {
		if errors.Is(err, store.ErrSessionChanged) {
			record, loadErr := s.store.SessionAtCurrentPasswordVersion(ctx, sessionID)
			if loadErr != nil {
				if errors.Is(loadErr, store.ErrNotFound) {
					s.terminateSession(sessionID, SessionTerminationRevoked)
					return ErrUnauthorized
				}
				return loadErr
			}
			if reason, invalid := s.invalidSessionReason(record, s.now().UTC()); invalid {
				s.terminateSession(sessionID, reason)
				return ErrUnauthorized
			}
			s.sessionLifecycle.TouchPersisted(sessionID, record.LastSeenAt, record.ExpiresAt)
			return nil
		}
		if errors.Is(err, store.ErrNotFound) {
			s.terminateSession(sessionID, SessionTerminationRevoked)
			return ErrUnauthorized
		}
		return err
	}
	s.sessionLifecycle.TouchPersisted(sessionID, touch.activityAt, touch.expiresAt)
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

	for attempt := 0; attempt < 2; attempt++ {
		record, err := s.store.SessionAtCurrentPasswordVersion(ctx, cookie.Value)
		if err != nil {
			if errors.Is(err, store.ErrNotFound) {
				s.terminateSession(cookie.Value, SessionTerminationRevoked)
				return Session{}, ErrUnauthorized
			}
			return Session{}, err
		}

		now := s.now().UTC()
		if reason, invalid := s.invalidSessionReason(record, now); invalid {
			_ = s.store.RevokeSession(ctx, record.ID, now)
			s.terminateSession(record.ID, reason)
			return Session{}, ErrUnauthorized
		}

		nextExpiresAt := minTime(now.Add(s.cfg.SessionIdleTimeout), record.CreatedAt.Add(s.cfg.SessionAbsoluteLifetime))
		lastSeenAt := record.LastSeenAt
		persisted := false
		if now.Sub(record.LastSeenAt) >= sessionTouchInterval(s.cfg.SessionIdleTimeout) {
			if err := s.store.TouchSession(ctx, record.ID, record.LastSeenAt, now, nextExpiresAt); err != nil {
				if errors.Is(err, store.ErrSessionChanged) {
					continue
				}
				return Session{}, err
			}
			lastSeenAt = now
			persisted = true
		}

		if persisted {
			s.sessionLifecycle.TouchPersisted(record.ID, lastSeenAt, nextExpiresAt)
		} else {
			s.sessionLifecycle.TouchLease(record.ID, nextExpiresAt)
		}

		return Session{
			ID:         record.ID,
			UserID:     record.UserID,
			CreatedAt:  record.CreatedAt,
			LastSeenAt: lastSeenAt,
			ExpiresAt:  nextExpiresAt,
		}, nil
	}

	return Session{}, fmt.Errorf("authenticate session after concurrent touches: %w", store.ErrSessionChanged)
}

func (s *Service) invalidSessionReason(record store.Session, now time.Time) (SessionTerminationReason, bool) {
	if record.RevokedAt != nil {
		return SessionTerminationRevoked, true
	}
	if !now.Before(record.CreatedAt.Add(s.cfg.SessionAbsoluteLifetime)) {
		return SessionTerminationAbsoluteExpired, true
	}
	if !now.Before(record.ExpiresAt) {
		return SessionTerminationIdleExpired, true
	}
	return "", false
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

// SessionCookie remains available until the immutable absolute deadline. The
// shorter sliding idle deadline is authoritative in SQLite and the lifecycle
// hub; using it as the browser expiry would strand an otherwise active session.
func (s *Service) SessionCookie(session Session) *http.Cookie {
	expiresAt := session.ExpiresAt
	if !session.CreatedAt.IsZero() && s.cfg.SessionAbsoluteLifetime > 0 {
		expiresAt = session.CreatedAt.Add(s.cfg.SessionAbsoluteLifetime)
	}
	return &http.Cookie{
		Name:     s.cfg.SessionCookieName,
		Value:    session.ID,
		Path:     "/",
		HttpOnly: true,
		SameSite: http.SameSiteStrictMode,
		Secure:   s.cfg.CookieSecure,
		Expires:  expiresAt,
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
	return clientIP(r, s.cfg.TrustedProxies, s.cfg.TrustedProxySecret)
}

func (s *Service) SecureRequest(r *http.Request) bool {
	return secureRequest(r, s.cfg.TrustedProxies, s.cfg.TrustedProxySecret)
}

func ClientIP(r *http.Request) string {
	return clientIP(r, nil, "")
}

func clientIP(r *http.Request, trustedProxies []netip.Prefix, trustedProxySecret string) string {
	host, _, err := net.SplitHostPort(r.RemoteAddr)
	if err == nil {
		if trustedProxyRequest(host, r.Header.Get(trustedProxySecretHeader), trustedProxies, trustedProxySecret) {
			if trustedForwardedIP := trustedForwardedFor(r.Header.Get("X-Forwarded-For"), trustedProxies); trustedForwardedIP != "" {
				return trustedForwardedIP
			}
		}
		return host
	}
	return r.RemoteAddr
}

func secureRequest(r *http.Request, trustedProxies []netip.Prefix, trustedProxySecret string) bool {
	if r.TLS != nil {
		return true
	}
	host, _, err := net.SplitHostPort(r.RemoteAddr)
	if err != nil {
		host = r.RemoteAddr
	}
	if !trustedProxyRequest(host, r.Header.Get(trustedProxySecretHeader), trustedProxies, trustedProxySecret) {
		return false
	}
	return trustedForwardedProto(r.Header.Get("X-Forwarded-Proto")) == "https"
}

func trustedProxyRequest(remoteHost, presentedSecret string, trustedProxies []netip.Prefix, expectedSecret string) bool {
	if len(trustedProxies) == 0 || expectedSecret == "" || len(presentedSecret) != len(expectedSecret) {
		return false
	}
	remoteAddr, err := netip.ParseAddr(strings.TrimSpace(remoteHost))
	if err != nil || !isTrustedProxy(remoteAddr, trustedProxies) {
		return false
	}
	return subtle.ConstantTimeCompare([]byte(presentedSecret), []byte(expectedSecret)) == 1
}

func trustedForwardedFor(headerValue string, trustedProxies []netip.Prefix) string {
	if strings.TrimSpace(headerValue) == "" {
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

func trustedForwardedProto(headerValue string) string {
	if strings.TrimSpace(headerValue) == "" {
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
	if err := validateNewPassword(password); err != nil {
		return "", err
	}

	salt := make([]byte, argon2SaltLength)
	if _, err := rand.Read(salt); err != nil {
		return "", fmt.Errorf("generate salt: %w", err)
	}

	hash := argon2.IDKey([]byte(password), salt, argon2Iterations, argon2Memory, argon2Parallelism, argon2KeyLength)
	return fmt.Sprintf(
		"$stacklab$argon2id$v=19$m=%d,t=%d,p=%d$%s$%s",
		argon2Memory,
		argon2Iterations,
		argon2Parallelism,
		base64.RawStdEncoding.EncodeToString(salt),
		base64.RawStdEncoding.EncodeToString(hash),
	), nil
}

func verifyPassword(encodedHash, password string) error {
	parsed, err := parsePasswordHash(encodedHash)
	if err != nil {
		return ErrInvalidCredentials
	}

	computedHash, err := derivePasswordHash([]byte(password), parsed.salt, parsed.parameters, uint32(len(parsed.hash)))
	if err != nil || subtle.ConstantTimeCompare(parsed.hash, computedHash) != 1 {
		return ErrInvalidCredentials
	}
	return nil
}

func validateNewPassword(password string) error {
	if !utf8.ValidString(password) {
		return fmt.Errorf("%w: must be valid UTF-8", ErrInvalidPassword)
	}
	length := utf8.RuneCountInString(password)
	if length < PasswordMinimumLength || length > PasswordMaximumLength {
		return fmt.Errorf(
			"%w: must contain between %d and %d Unicode characters",
			ErrInvalidPassword,
			PasswordMinimumLength,
			PasswordMaximumLength,
		)
	}
	return nil
}

func parsePasswordHash(encodedHash string) (parsedPasswordHash, error) {
	if len(encodedHash) == 0 || len(encodedHash) > maximumEncodedPasswordHashLength {
		return parsedPasswordHash{}, ErrInvalidCredentials
	}

	parts := strings.Split(encodedHash, "$")
	if len(parts) != 7 || parts[0] != "" || parts[1] != "stacklab" || parts[2] != "argon2id" || parts[3] != "v=19" {
		return parsedPasswordHash{}, ErrInvalidCredentials
	}

	parameterParts := strings.Split(parts[4], ",")
	if len(parameterParts) != 3 {
		return parsedPasswordHash{}, ErrInvalidCredentials
	}
	memory, err := parsePasswordHashParameter(parameterParts[0], "m=", 32)
	if err != nil {
		return parsedPasswordHash{}, ErrInvalidCredentials
	}
	iterations, err := parsePasswordHashParameter(parameterParts[1], "t=", 32)
	if err != nil {
		return parsedPasswordHash{}, ErrInvalidCredentials
	}
	parallelism, err := parsePasswordHashParameter(parameterParts[2], "p=", 8)
	if err != nil {
		return parsedPasswordHash{}, ErrInvalidCredentials
	}
	parameters := passwordHashParameters{
		memory:      uint32(memory),
		iterations:  uint32(iterations),
		parallelism: uint8(parallelism),
	}
	if parameters.memory < argon2MinimumMemory || parameters.memory > argon2MaximumMemory ||
		parameters.iterations < argon2MinimumIterations || parameters.iterations > argon2MaximumIterations ||
		parameters.parallelism < argon2MinimumParallelism || parameters.parallelism > argon2MaximumParallelism {
		return parsedPasswordHash{}, ErrInvalidCredentials
	}

	salt, err := decodePasswordHashComponent(parts[5], argon2MinimumSaltLength, argon2MaximumSaltLength)
	if err != nil {
		return parsedPasswordHash{}, ErrInvalidCredentials
	}
	expectedHash, err := decodePasswordHashComponent(parts[6], argon2MinimumHashLength, argon2MaximumHashLength)
	if err != nil {
		return parsedPasswordHash{}, ErrInvalidCredentials
	}

	return parsedPasswordHash{parameters: parameters, salt: salt, hash: expectedHash}, nil
}

func parsePasswordHashParameter(value, prefix string, bitSize int) (uint64, error) {
	if !strings.HasPrefix(value, prefix) || len(value) == len(prefix) {
		return 0, ErrInvalidCredentials
	}
	digits := value[len(prefix):]
	for _, digit := range digits {
		if digit < '0' || digit > '9' {
			return 0, ErrInvalidCredentials
		}
	}
	parsed, err := strconv.ParseUint(digits, 10, bitSize)
	if err != nil {
		return 0, ErrInvalidCredentials
	}
	return parsed, nil
}

func decodePasswordHashComponent(encoded string, minimumLength, maximumLength int) ([]byte, error) {
	if len(encoded) == 0 || len(encoded) > base64.RawStdEncoding.EncodedLen(maximumLength) {
		return nil, ErrInvalidCredentials
	}
	decoded, err := passwordHashBase64.DecodeString(encoded)
	if err != nil || len(decoded) < minimumLength || len(decoded) > maximumLength ||
		base64.RawStdEncoding.EncodeToString(decoded) != encoded {
		return nil, ErrInvalidCredentials
	}
	return decoded, nil
}

func derivePasswordHash(password, salt []byte, parameters passwordHashParameters, keyLength uint32) (hash []byte, err error) {
	defer func() {
		if recover() != nil {
			hash = nil
			err = ErrInvalidCredentials
		}
	}()
	return argon2.IDKey(password, salt, parameters.iterations, parameters.memory, parameters.parallelism, keyLength), nil
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
