package terminal

import (
	"crypto/rand"
	"encoding/base64"
	"errors"
	"io"
	"log/slog"
	"os"
	"os/exec"
	"sync"
	"syscall"
	"time"

	"github.com/creack/pty"
)

var (
	ErrSessionNotFound      = errors.New("terminal session not found")
	ErrSessionLimitExceeded = errors.New("terminal session limit exceeded")
	ErrInvalidShell         = errors.New("invalid shell")
)

const terminalProcessKillDelay = 2 * time.Second

type Config struct {
	MaxSessionsPerOwner int
	IdleTimeout         time.Duration
	DetachGracePeriod   time.Duration
}

type SessionInfo struct {
	ID          string
	StackID     string
	ContainerID string
	Shell       string
}

type Event struct {
	Type     string
	Data     string
	ExitCode *int
	Reason   string
}

type LifecycleEvent struct {
	Type        string
	SessionID   string
	StackID     string
	ContainerID string
	Reason      string
}

type Service struct {
	logger        *slog.Logger
	cfg           Config
	startTerminal func(*exec.Cmd, *pty.Winsize) (*os.File, error)
	mu            sync.Mutex
	sessions      map[string]*session
	owners        map[string]map[string]struct{}
	hook          func(LifecycleEvent)
}

type session struct {
	info          SessionInfo
	ownerID       string
	process       *exec.Cmd
	ptyFile       *os.File
	current       *attachment
	idleTimer     *time.Timer
	detachTimer   *time.Timer
	closeOnce     sync.Once
	lifecycleHook func(LifecycleEvent)
	mu            sync.Mutex
}

type attachment struct {
	id           string
	connectionID string
	events       chan Event
	closed       bool
}

func NewService(logger *slog.Logger, cfg Config, hook func(LifecycleEvent)) *Service {
	if logger == nil {
		logger = slog.Default()
	}
	if cfg.MaxSessionsPerOwner <= 0 {
		cfg.MaxSessionsPerOwner = 5
	}
	if cfg.IdleTimeout <= 0 {
		cfg.IdleTimeout = 30 * time.Minute
	}
	if cfg.DetachGracePeriod <= 0 {
		cfg.DetachGracePeriod = time.Minute
	}

	return &Service{
		logger:        logger,
		cfg:           cfg,
		startTerminal: pty.StartWithSize,
		sessions:      map[string]*session{},
		owners:        map[string]map[string]struct{}{},
		hook:          hook,
	}
}

func (s *Service) Open(ownerID, stackID, containerID, shell string, cols, rows int, connectionID string) (SessionInfo, string, <-chan Event, error) {
	normalizedShell, err := normalizeShell(shell)
	if err != nil {
		return SessionInfo{}, "", nil, err
	}

	cmd := exec.Command("docker", "exec", "-it", containerID, normalizedShell)
	now := time.Now().UTC()
	info := SessionInfo{
		ID:          "term_" + randomID(18),
		StackID:     stackID,
		ContainerID: containerID,
		Shell:       normalizedShell,
	}

	s.mu.Lock()
	if _, ok := s.owners[ownerID]; !ok {
		s.owners[ownerID] = map[string]struct{}{}
	}
	if len(s.owners[ownerID]) >= s.cfg.MaxSessionsPerOwner {
		s.mu.Unlock()
		return SessionInfo{}, "", nil, ErrSessionLimitExceeded
	}
	s.owners[ownerID][info.ID] = struct{}{}
	s.mu.Unlock()

	ptyFile, err := s.startTerminal(cmd, &pty.Winsize{
		Cols: uint16(normalizeCols(cols)),
		Rows: uint16(normalizeRows(rows)),
	})
	if err != nil {
		s.releaseOwnerSession(ownerID, info.ID)
		return SessionInfo{}, "", nil, err
	}

	attachmentID := "attach_" + randomID(18)
	events := make(chan Event, 256)

	terminalSession := &session{
		info:          info,
		ownerID:       ownerID,
		process:       cmd,
		ptyFile:       ptyFile,
		current:       &attachment{id: attachmentID, connectionID: connectionID, events: events},
		lifecycleHook: s.hook,
	}
	terminalSession.idleTimer = time.AfterFunc(s.cfg.IdleTimeout, func() {
		s.endSession(info.ID, nil, "idle_timeout")
	})

	s.mu.Lock()
	s.sessions[info.ID] = terminalSession
	s.mu.Unlock()

	s.emitLifecycle(LifecycleEvent{
		Type:        "opened",
		SessionID:   info.ID,
		StackID:     stackID,
		ContainerID: containerID,
	})

	go s.forwardOutput(info.ID)
	go s.waitForExit(info.ID, now)

	return info, attachmentID, events, nil
}

func (s *Service) Attach(ownerID, sessionID string, cols, rows int, connectionID string) (SessionInfo, string, <-chan Event, error) {
	s.mu.Lock()
	terminalSession, ok := s.sessions[sessionID]
	s.mu.Unlock()
	if !ok || terminalSession.ownerID != ownerID {
		return SessionInfo{}, "", nil, ErrSessionNotFound
	}

	events := make(chan Event, 256)
	attachmentID := "attach_" + randomID(18)

	terminalSession.mu.Lock()
	if terminalSession.detachTimer != nil {
		terminalSession.detachTimer.Stop()
		terminalSession.detachTimer = nil
	}
	if terminalSession.current != nil {
		replaced := terminalSession.current
		sendAndCloseAttachmentLocked(replaced, Event{Type: "exited", Reason: "connection_replaced"})
	}
	terminalSession.current = &attachment{id: attachmentID, connectionID: connectionID, events: events}
	terminalSession.touchLocked(s.cfg.IdleTimeout)
	if err := pty.Setsize(terminalSession.ptyFile, &pty.Winsize{
		Cols: uint16(normalizeCols(cols)),
		Rows: uint16(normalizeRows(rows)),
	}); err != nil {
		terminalSession.mu.Unlock()
		close(events)
		return SessionInfo{}, "", nil, err
	}
	info := terminalSession.info
	terminalSession.mu.Unlock()

	s.emitLifecycle(LifecycleEvent{
		Type:        "attached",
		SessionID:   info.ID,
		StackID:     info.StackID,
		ContainerID: info.ContainerID,
	})

	return info, attachmentID, events, nil
}

func (s *Service) Detach(ownerID, sessionID, attachmentID string) {
	s.mu.Lock()
	terminalSession, ok := s.sessions[sessionID]
	s.mu.Unlock()
	if !ok || terminalSession.ownerID != ownerID {
		return
	}

	terminalSession.mu.Lock()
	defer terminalSession.mu.Unlock()
	if terminalSession.current == nil || terminalSession.current.id != attachmentID {
		return
	}
	closeAttachmentLocked(terminalSession.current)
	terminalSession.current = nil
	if terminalSession.detachTimer != nil {
		terminalSession.detachTimer.Stop()
	}
	terminalSession.detachTimer = time.AfterFunc(s.cfg.DetachGracePeriod, func() {
		s.endSession(sessionID, nil, "server_cleanup")
	})
}

func (s *Service) Input(ownerID, sessionID, data string) error {
	terminalSession, err := s.lookupOwnedSession(ownerID, sessionID)
	if err != nil {
		return err
	}

	terminalSession.mu.Lock()
	defer terminalSession.mu.Unlock()
	terminalSession.touchLocked(s.cfg.IdleTimeout)
	_, err = io.WriteString(terminalSession.ptyFile, data)
	return err
}

func (s *Service) Resize(ownerID, sessionID string, cols, rows int) error {
	terminalSession, err := s.lookupOwnedSession(ownerID, sessionID)
	if err != nil {
		return err
	}

	terminalSession.mu.Lock()
	defer terminalSession.mu.Unlock()
	terminalSession.touchLocked(s.cfg.IdleTimeout)
	return pty.Setsize(terminalSession.ptyFile, &pty.Winsize{
		Cols: uint16(normalizeCols(cols)),
		Rows: uint16(normalizeRows(rows)),
	})
}

func (s *Service) Close(ownerID, sessionID, reason string) error {
	terminalSession, err := s.lookupOwnedSession(ownerID, sessionID)
	if err != nil {
		return err
	}
	s.endSession(sessionID, terminalSession.process.ProcessState, reason)
	return nil
}

func (s *Service) lookupOwnedSession(ownerID, sessionID string) (*session, error) {
	s.mu.Lock()
	defer s.mu.Unlock()

	terminalSession, ok := s.sessions[sessionID]
	if !ok || terminalSession.ownerID != ownerID {
		return nil, ErrSessionNotFound
	}
	return terminalSession, nil
}

func (s *Service) releaseOwnerSession(ownerID, sessionID string) {
	s.mu.Lock()
	defer s.mu.Unlock()

	if owned := s.owners[ownerID]; owned != nil {
		delete(owned, sessionID)
		if len(owned) == 0 {
			delete(s.owners, ownerID)
		}
	}
}

func (s *Service) forwardOutput(sessionID string) {
	s.mu.Lock()
	terminalSession, ok := s.sessions[sessionID]
	s.mu.Unlock()
	if !ok {
		return
	}

	buffer := make([]byte, 4096)
	for {
		readBytes, err := terminalSession.ptyFile.Read(buffer)
		if readBytes > 0 {
			chunk := string(buffer[:readBytes])
			terminalSession.mu.Lock()
			attachment := terminalSession.current
			dropped := false
			if attachment != nil && !attachment.closed {
				select {
				case attachment.events <- Event{Type: "output", Data: chunk}:
				default:
					dropped = true
				}
			}
			terminalSession.mu.Unlock()
			if dropped {
				s.logger.Warn("terminal output dropped due to backpressure", "session_id", sessionID)
			}
		}
		if err != nil {
			if errors.Is(err, io.EOF) {
				return
			}
			return
		}
	}
}

func (s *Service) waitForExit(sessionID string, startedAt time.Time) {
	s.mu.Lock()
	terminalSession, ok := s.sessions[sessionID]
	s.mu.Unlock()
	if !ok {
		return
	}

	err := terminalSession.process.Wait()
	if err == nil {
		exitCode := 0
		s.endSessionWithCode(sessionID, &exitCode, "process_exit")
		return
	}

	exitCode := exitCodeFromError(err)
	s.endSessionWithCode(sessionID, exitCode, "process_exit")
}

func (s *Service) endSession(sessionID string, _ *os.ProcessState, reason string) {
	var exitCode *int
	s.endSessionWithCode(sessionID, exitCode, reason)
}

func (s *Service) endSessionWithCode(sessionID string, exitCode *int, reason string) {
	s.mu.Lock()
	terminalSession, ok := s.sessions[sessionID]
	s.mu.Unlock()
	if !ok {
		return
	}

	terminalSession.closeOnce.Do(func() {
		terminalSession.mu.Lock()
		current := terminalSession.current
		if terminalSession.idleTimer != nil {
			terminalSession.idleTimer.Stop()
		}
		if terminalSession.detachTimer != nil {
			terminalSession.detachTimer.Stop()
		}
		terminalSession.current = nil
		if current != nil {
			sendAndCloseAttachmentLocked(current, Event{
				Type:     "exited",
				ExitCode: exitCode,
				Reason:   reason,
			})
		}
		terminalSession.mu.Unlock()

		_ = terminalSession.ptyFile.Close()
		terminateTerminalProcess(terminalSession.process)

		s.mu.Lock()
		delete(s.sessions, sessionID)
		if owned := s.owners[terminalSession.ownerID]; owned != nil {
			delete(owned, sessionID)
			if len(owned) == 0 {
				delete(s.owners, terminalSession.ownerID)
			}
		}
		s.mu.Unlock()

		s.emitLifecycle(LifecycleEvent{
			Type:        "closed",
			SessionID:   terminalSession.info.ID,
			StackID:     terminalSession.info.StackID,
			ContainerID: terminalSession.info.ContainerID,
			Reason:      reason,
		})
	})
}

func terminateTerminalProcess(cmd *exec.Cmd) {
	if cmd == nil || cmd.Process == nil {
		return
	}
	_ = cmd.Process.Signal(syscall.SIGTERM)
	go func() {
		time.Sleep(terminalProcessKillDelay)
		_ = cmd.Process.Kill()
	}()
}

func (s *Service) emitLifecycle(event LifecycleEvent) {
	if s.hook != nil {
		s.hook(event)
	}
}

func (s *session) touchLocked(timeout time.Duration) {
	if s.idleTimer != nil {
		s.idleTimer.Reset(timeout)
	}
}

func sendAndCloseAttachmentLocked(attachment *attachment, event Event) {
	if attachment == nil || attachment.closed {
		return
	}
	select {
	case attachment.events <- event:
	default:
	}
	closeAttachmentLocked(attachment)
}

func closeAttachmentLocked(attachment *attachment) {
	if attachment == nil || attachment.closed {
		return
	}
	close(attachment.events)
	attachment.closed = true
}

func normalizeShell(shell string) (string, error) {
	switch shell {
	case "", "/bin/sh":
		return "/bin/sh", nil
	case "/bin/bash":
		return "/bin/bash", nil
	default:
		return "", ErrInvalidShell
	}
}

func normalizeCols(cols int) int {
	if cols <= 0 {
		return 120
	}
	return cols
}

func normalizeRows(rows int) int {
	if rows <= 0 {
		return 36
	}
	return rows
}

func exitCodeFromError(err error) *int {
	var exitErr *exec.ExitError
	if errors.As(err, &exitErr) {
		code := exitErr.ExitCode()
		return &code
	}
	return nil
}

func randomID(length int) string {
	bytes := make([]byte, length)
	if _, err := rand.Read(bytes); err != nil {
		return "fallback"
	}
	return base64.RawURLEncoding.EncodeToString(bytes)
}
