package main

import (
	"context"
	"reflect"
	"sync"
	"testing"
	"time"
)

type blockingShutdownComponent struct {
	name    string
	mu      *sync.Mutex
	order   *[]string
	started chan struct{}
	release chan struct{}
}

func (c *blockingShutdownComponent) Shutdown(context.Context) error {
	c.mu.Lock()
	*c.order = append(*c.order, c.name)
	c.mu.Unlock()
	if c.started != nil {
		close(c.started)
	}
	if c.release != nil {
		<-c.release
	}
	return nil
}

func TestShutdownApplicationWaitsForEachLifecyclePhase(t *testing.T) {
	var mu sync.Mutex
	var order []string
	serverStarted := make(chan struct{})
	releaseServer := make(chan struct{})
	server := &blockingShutdownComponent{name: "server", mu: &mu, order: &order, started: serverStarted, release: releaseServer}
	handler := &blockingShutdownComponent{name: "handler", mu: &mu, order: &order}
	background := &blockingShutdownComponent{name: "background", mu: &mu, order: &order}
	done := make(chan error, 1)
	go func() {
		done <- shutdownApplication(context.Background(), server, func() {
			mu.Lock()
			order = append(order, "cancel")
			mu.Unlock()
		}, handler, background)
	}()

	select {
	case <-serverStarted:
	case <-time.After(time.Second):
		t.Fatal("server shutdown did not start")
	}
	mu.Lock()
	beforeRelease := append([]string(nil), order...)
	mu.Unlock()
	if !reflect.DeepEqual(beforeRelease, []string{"server"}) {
		t.Fatalf("shutdown order before server release = %v, want [server]", beforeRelease)
	}

	close(releaseServer)
	select {
	case err := <-done:
		if err != nil {
			t.Fatalf("shutdownApplication() error = %v", err)
		}
	case <-time.After(time.Second):
		t.Fatal("shutdownApplication() did not finish")
	}
	mu.Lock()
	gotOrder := append([]string(nil), order...)
	mu.Unlock()
	if !reflect.DeepEqual(gotOrder, []string{"server", "cancel", "handler", "background"}) {
		t.Fatalf("shutdown order = %v", gotOrder)
	}
}
