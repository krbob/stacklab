package backup

import (
	"bytes"
	"context"
	"crypto/rand"
	"crypto/sha256"
	"errors"
	"io"
	"net"
	"net/http"
	"net/http/httptest"
	"net/http/httputil"
	"net/url"
	"os"
	"os/exec"
	"path/filepath"
	"strings"
	"sync"
	"testing"
	"time"
)

// This opt-in experiment lasts slightly over five minutes. It runs the actual
// nativeRunner/restic process against rclone's encrypted REST repository, while
// a localhost proxy delays final index completion or stalls pack upload.
func TestNativeTransferWatchdogControlledTransport(t *testing.T) {
	if os.Getenv("STACKLAB_TEST_SLOW_BACKUP") != "1" {
		t.Skip("set STACKLAB_TEST_SLOW_BACKUP=1 for the real five-minute watchdog experiment")
	}

	for _, phase := range []string{"finalization", "reading"} {
		t.Run(phase, func(t *testing.T) { t.Parallel(); exerciseControlledWatchdog(t, phase) })
	}
}

func exerciseControlledWatchdog(t *testing.T, phase string) {
	t.Helper()
	if _, err := exec.LookPath("rclone"); err != nil {
		t.Fatal(err)
	}
	if _, err := exec.LookPath("restic"); err != nil {
		t.Fatal(err)
	}
	base, err := os.MkdirTemp("/tmp", "stacklab-watchdog-")
	if err != nil {
		t.Fatal(err)
	}
	defer os.RemoveAll(base)
	ctx, cancel := context.WithTimeout(context.Background(), 8*time.Minute)
	defer cancel()
	repo, source, socket := filepath.Join(base, "repository"), filepath.Join(base, "source"), filepath.Join(base, "rclone.sock")
	for _, path := range []string{repo, source} {
		if err = os.Mkdir(path, 0700); err != nil {
			t.Fatal(err)
		}
	}
	command := exec.CommandContext(ctx, "rclone", "serve", "restic", repo, "--addr", "unix://"+socket, "--log-level", "ERROR", "--server-read-timeout", "10m", "--server-write-timeout", "10m")
	diagnosticPath := filepath.Join(base, "rclone.log")
	diagnostic, err := os.Create(diagnosticPath)
	if err != nil {
		t.Fatal(err)
	}
	defer diagnostic.Close()
	command.Stderr = diagnostic
	if err = command.Start(); err != nil {
		t.Fatal(err)
	}
	defer func() { _ = command.Process.Kill(); _ = command.Wait() }()
	deadline := time.Now().Add(10 * time.Second)
	for {
		if _, err = os.Stat(socket); err == nil {
			break
		}
		if time.Now().After(deadline) {
			output, _ := os.ReadFile(diagnosticPath)
			t.Fatalf("rclone listener did not start: %s", output)
		}
		time.Sleep(20 * time.Millisecond)
	}
	upstream, _ := url.Parse("http://rclone")
	proxy := httputil.NewSingleHostReverseProxy(upstream)
	proxy.Transport = &http.Transport{DialContext: func(ctx context.Context, _, _ string) (net.Conn, error) {
		return (&net.Dialer{}).DialContext(ctx, "unix", socket)
	}}
	proxy.ErrorHandler = func(w http.ResponseWriter, r *http.Request, err error) {
		http.Error(w, "controlled backend unavailable", 502)
	}
	held := make(chan struct{})
	release := make(chan struct{})
	var holdOnce, releaseOnce sync.Once
	defer releaseOnce.Do(func() { close(release) })
	gatedPath := "/index/"
	if phase == "reading" {
		gatedPath = "/data/"
	}
	server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		if r.Method == http.MethodPost && strings.Contains(r.URL.Path, gatedPath) {
			holdOnce.Do(func() { t.Logf("%s request held at %s", gatedPath, time.Now().UTC().Format(time.RFC3339)); close(held) })
			select {
			case <-release:
			case <-r.Context().Done():
				return
			case <-ctx.Done():
				return
			}
		}
		proxy.ServeHTTP(w, r)
	}))
	defer func() { releaseOnce.Do(func() { close(release) }); server.Close() }()
	env := append(cleanProcessEnvironment(), "RESTIC_PASSWORD=fixture-only-watchdog-password")
	args := []string{"--repo", "rest:" + server.URL + "/", "--json", "--no-cache", "--stuck-request-timeout", "10m"}
	call := func(extra ...string) ([]byte, error) {
		return (nativeRunner{}).Run(ctx, "restic", append(append([]string{}, args...), extra...), env, source)
	}
	if _, err = call("init"); err != nil {
		t.Fatal(err)
	}
	size := int64(8 << 20)
	if phase == "reading" {
		size = 256 << 20
	}
	payload := filepath.Join(source, "payload.bin")
	file, err := os.Create(payload)
	if err != nil {
		t.Fatal(err)
	}
	digest := sha256.New()
	_, writeErr := io.CopyN(io.MultiWriter(file, digest), rand.Reader, size)
	closeErr := file.Close()
	if err = errors.Join(writeErr, closeErr); err != nil {
		t.Fatal(err)
	}
	var mu sync.Mutex
	var latest transferProgress
	var finalAt time.Time
	firstFinal := make(chan struct{})
	var finalOnce sync.Once
	started := time.Now()
	result := make(chan error, 1)
	done := make(chan struct{})
	defer func() {
		cancel()
		releaseOnce.Do(func() { close(release) })
		select {
		case <-done:
		case <-time.After(20 * time.Second):
			t.Error("restic did not stop during fixture cleanup")
		}
	}()
	go func() {
		defer close(done)
		_, err := (nativeRunner{}).RunProgress(ctx, "restic", append(append([]string{}, args...), "backup", "--pack-size", "4", "--host", "stacklab-watchdog", "."), env, source, func(p transferProgress) {
			mu.Lock()
			latest = p
			if p.BytesTotal > 0 && p.BytesProcessed >= p.BytesTotal && finalAt.IsZero() {
				finalAt = time.Now()
				t.Logf("finalization observed elapsed=%s bytes=%d/%d files=%d/%d", time.Since(started).Round(time.Millisecond), p.BytesProcessed, p.BytesTotal, p.FilesProcessed, p.FilesTotal)
				finalOnce.Do(func() { close(firstFinal) })
			}
			mu.Unlock()
		})
		result <- err
	}()
	select {
	case <-held:
	case err := <-result:
		t.Fatalf("backup exited before controlled stall: %v", err)
	case <-time.After(30 * time.Second):
		t.Fatal("backup did not reach controlled request")
	}
	if phase == "reading" {
		select {
		case err := <-result:
			elapsed := time.Since(started)
			mu.Lock()
			p := latest
			observedFinal := !finalAt.IsZero()
			mu.Unlock()
			t.Logf("reading watchdog result elapsed=%s bytes=%d/%d files=%d/%d finalizing=%t err=%v", elapsed.Round(time.Millisecond), p.BytesProcessed, p.BytesTotal, p.FilesProcessed, p.FilesTotal, observedFinal, err)
			if !errors.Is(err, errTransferStalled) || !strings.Contains(err.Error(), "5 minutes") || observedFinal || elapsed < 5*time.Minute || elapsed > 5*time.Minute+30*time.Second {
				t.Fatal("reading stall did not enforce actual five-minute deadline")
			}
		case <-time.After(6 * time.Minute):
			t.Fatal("reading watchdog did not cancel stalled upload")
		}
		releaseOnce.Do(func() { close(release) })
		if _, err = call("check", "--read-data"); err != nil {
			t.Fatalf("repository invalid after graceful cancellation: %v", err)
		}
		return
	}
	select {
	case <-firstFinal:
	case err := <-result:
		t.Fatalf("backup exited before finalization counters: %v", err)
	case <-time.After(20 * time.Second):
		t.Fatal("finalization counters not observed")
	}
	mu.Lock()
	observed := finalAt
	initial := latest
	mu.Unlock()
	timer := time.NewTimer(time.Until(observed.Add(5*time.Minute + 20*time.Second)))
	defer timer.Stop()
	select {
	case err := <-result:
		t.Fatalf("backup terminated before passing old five-minute deadline: %v", err)
	case <-timer.C:
	}
	mu.Lock()
	p := latest
	mu.Unlock()
	t.Logf("beyond original deadline elapsed=%s bytes=%d/%d files=%d/%d unchanged=%t", time.Since(observed).Round(time.Millisecond), p.BytesProcessed, p.BytesTotal, p.FilesProcessed, p.FilesTotal, p == initial)
	if p.BytesProcessed != initial.BytesProcessed || p.FilesProcessed != initial.FilesProcessed || p.BytesAdded != initial.BytesAdded {
		t.Fatal("counters changed, so experiment does not prove finalization budget")
	}
	releaseOnce.Do(func() { close(release) })
	select {
	case err := <-result:
		if err != nil {
			t.Fatalf("released finalization failed: %v", err)
		}
	case <-time.After(40 * time.Second):
		t.Fatal("backup did not complete after release")
	}
	if _, err = call("check", "--read-data"); err != nil {
		t.Fatalf("repository check: %v", err)
	}
	restored := filepath.Join(base, "restored")
	if _, err = call("restore", "latest", "--target", restored, "--verify"); err != nil {
		t.Fatalf("restore: %v", err)
	}
	restoredFile, err := os.Open(filepath.Join(restored, "payload.bin"))
	if err != nil {
		t.Fatal(err)
	}
	restoredHash := sha256.New()
	_, err = io.Copy(restoredHash, restoredFile)
	_ = restoredFile.Close()
	if err != nil {
		t.Fatal(err)
	}
	if !bytes.Equal(digest.Sum(nil), restoredHash.Sum(nil)) {
		t.Fatal("restored payload differs")
	}
	t.Logf("finalization succeeded elapsed=%s encrypted check --read-data and restore --verify SHA256 passed (%d bytes)", time.Since(started).Round(time.Millisecond), size)
}
