package terminal

import (
	"errors"
	"os"
	"os/exec"
	"testing"
	"time"

	"github.com/creack/pty"
)

func TestNormalizeShell(t *testing.T) {
	t.Parallel()

	tests := []struct {
		name    string
		input   string
		want    string
		wantErr bool
	}{
		{name: "default", input: "", want: "/bin/sh"},
		{name: "sh", input: "/bin/sh", want: "/bin/sh"},
		{name: "bash", input: "/bin/bash", want: "/bin/bash"},
		{name: "invalid", input: "/bin/zsh", wantErr: true},
	}

	for _, test := range tests {
		test := test
		t.Run(test.name, func(t *testing.T) {
			t.Parallel()

			got, err := normalizeShell(test.input)
			if test.wantErr {
				if err == nil {
					t.Fatalf("normalizeShell(%q) error = nil, want error", test.input)
				}
				return
			}
			if err != nil {
				t.Fatalf("normalizeShell(%q) error = %v", test.input, err)
			}
			if got != test.want {
				t.Fatalf("normalizeShell(%q) = %q, want %q", test.input, got, test.want)
			}
		})
	}
}

func TestAttachReplacesExistingConnection(t *testing.T) {
	ptmx, tty, err := pty.Open()
	if err != nil {
		t.Fatalf("pty.Open() error = %v", err)
	}
	defer ptmx.Close()
	defer tty.Close()

	lifecycleEvents := make(chan LifecycleEvent, 4)
	service := NewService(nil, Config{IdleTimeout: time.Minute}, func(event LifecycleEvent) {
		lifecycleEvents <- event
	})

	oldEvents := make(chan Event, 1)
	terminalSession := &session{
		info: SessionInfo{
			ID:          "term_1",
			StackID:     "demo",
			ContainerID: "container_1",
			Shell:       "/bin/sh",
		},
		ownerID:       "owner_1",
		ptyFile:       ptmx,
		current:       &attachment{id: "attach_old", connectionID: "conn_old", events: oldEvents},
		idleTimer:     time.NewTimer(time.Minute),
		lifecycleHook: nil,
	}
	defer terminalSession.idleTimer.Stop()

	service.sessions["term_1"] = terminalSession
	service.owners["owner_1"] = map[string]struct{}{"term_1": {}}

	info, attachmentID, events, err := service.Attach("owner_1", "term_1", 80, 24, "conn_new")
	if err != nil {
		t.Fatalf("Attach() error = %v", err)
	}
	if info.ID != "term_1" {
		t.Fatalf("Attach() info.ID = %q, want %q", info.ID, "term_1")
	}
	if attachmentID == "" {
		t.Fatalf("Attach() attachmentID = empty")
	}
	if events == nil {
		t.Fatalf("Attach() events channel = nil")
	}

	select {
	case event, ok := <-oldEvents:
		if !ok {
			t.Fatalf("old attachment channel closed before event")
		}
		if event.Type != "exited" || event.Reason != "connection_replaced" {
			t.Fatalf("unexpected old attachment event: %#v", event)
		}
	default:
		t.Fatalf("expected connection_replaced event for old attachment")
	}

	select {
	case _, ok := <-oldEvents:
		if ok {
			t.Fatalf("old attachment channel still open")
		}
	default:
		t.Fatalf("expected old attachment channel to be closed")
	}

	select {
	case event := <-lifecycleEvents:
		if event.Type != "attached" || event.SessionID != "term_1" {
			t.Fatalf("unexpected lifecycle event: %#v", event)
		}
	default:
		t.Fatalf("expected attached lifecycle event")
	}
}

func TestDetachSchedulesCleanup(t *testing.T) {
	readEnd, writeEnd, err := os.Pipe()
	if err != nil {
		t.Fatalf("os.Pipe() error = %v", err)
	}
	defer readEnd.Close()

	lifecycleEvents := make(chan LifecycleEvent, 4)
	service := NewService(nil, Config{
		IdleTimeout:       time.Minute,
		DetachGracePeriod: 10 * time.Millisecond,
	}, func(event LifecycleEvent) {
		lifecycleEvents <- event
	})

	events := make(chan Event, 1)
	terminalSession := &session{
		info: SessionInfo{
			ID:          "term_2",
			StackID:     "demo",
			ContainerID: "container_2",
			Shell:       "/bin/sh",
		},
		ownerID:   "owner_2",
		ptyFile:   writeEnd,
		current:   &attachment{id: "attach_2", connectionID: "conn_2", events: events},
		idleTimer: time.NewTimer(time.Minute),
	}
	defer terminalSession.idleTimer.Stop()

	service.sessions["term_2"] = terminalSession
	service.owners["owner_2"] = map[string]struct{}{"term_2": {}}

	service.Detach("owner_2", "term_2", "attach_2")

	select {
	case _, ok := <-events:
		if ok {
			t.Fatalf("detached attachment channel should be closed")
		}
	case <-time.After(100 * time.Millisecond):
		t.Fatalf("timed out waiting for detach to close attachment channel")
	}

	deadline := time.Now().Add(300 * time.Millisecond)
	for time.Now().Before(deadline) {
		service.mu.Lock()
		_, exists := service.sessions["term_2"]
		service.mu.Unlock()
		if !exists {
			break
		}
		time.Sleep(10 * time.Millisecond)
	}

	service.mu.Lock()
	_, exists := service.sessions["term_2"]
	service.mu.Unlock()
	if exists {
		t.Fatalf("session still exists after detach grace period")
	}

	select {
	case event := <-lifecycleEvents:
		if event.Type != "closed" || event.Reason != "server_cleanup" {
			t.Fatalf("unexpected lifecycle event: %#v", event)
		}
	case <-time.After(100 * time.Millisecond):
		t.Fatalf("timed out waiting for closed lifecycle event")
	}
}

func TestCloseSendsExitEventAndCleansUp(t *testing.T) {
	readEnd, writeEnd, err := os.Pipe()
	if err != nil {
		t.Fatalf("os.Pipe() error = %v", err)
	}
	defer readEnd.Close()

	lifecycleEvents := make(chan LifecycleEvent, 4)
	service := NewService(nil, Config{IdleTimeout: time.Minute}, func(event LifecycleEvent) {
		lifecycleEvents <- event
	})

	events := make(chan Event, 1)
	terminalSession := &session{
		info: SessionInfo{
			ID:          "term_3",
			StackID:     "demo",
			ContainerID: "container_3",
			Shell:       "/bin/sh",
		},
		ownerID:   "owner_3",
		process:   &exec.Cmd{},
		ptyFile:   writeEnd,
		current:   &attachment{id: "attach_3", connectionID: "conn_3", events: events},
		idleTimer: time.NewTimer(time.Minute),
	}
	defer terminalSession.idleTimer.Stop()

	service.sessions["term_3"] = terminalSession
	service.owners["owner_3"] = map[string]struct{}{"term_3": {}}

	if err := service.Close("owner_3", "term_3", "client_close"); err != nil {
		t.Fatalf("Close() error = %v", err)
	}

	select {
	case event, ok := <-events:
		if !ok {
			t.Fatalf("attachment channel closed before exit event")
		}
		if event.Type != "exited" || event.Reason != "client_close" {
			t.Fatalf("unexpected exit event: %#v", event)
		}
	case <-time.After(100 * time.Millisecond):
		t.Fatalf("timed out waiting for exit event")
	}

	service.mu.Lock()
	_, exists := service.sessions["term_3"]
	service.mu.Unlock()
	if exists {
		t.Fatalf("session still exists after Close()")
	}

	select {
	case event := <-lifecycleEvents:
		if event.Type != "closed" || event.Reason != "client_close" {
			t.Fatalf("unexpected lifecycle event: %#v", event)
		}
	case <-time.After(100 * time.Millisecond):
		t.Fatalf("timed out waiting for lifecycle closed event")
	}
}

func TestExitCodeFromError(t *testing.T) {
	t.Parallel()

	if code := exitCodeFromError(errors.New("boom")); code != nil {
		t.Fatalf("exitCodeFromError(generic) = %#v, want nil", code)
	}
}
