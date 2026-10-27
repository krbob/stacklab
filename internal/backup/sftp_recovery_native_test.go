package backup

import (
	"bytes"
	"context"
	"crypto/rand"
	"errors"
	"fmt"
	"io"
	"net"
	"os"
	"os/exec"
	"path/filepath"
	"reflect"
	"runtime"
	"strconv"
	"strings"
	"sync"
	"testing"
	"time"
)

// This opt-in Linux fixture owns its SSH server, encrypted repository and TCP
// fault proxy. It never uses installed Stacklab state or existing containers.
// A byte gate proves that the second full check has started reading data packs
// before the fixture stops sshd, rather than testing only failed admission.
func TestNativeSFTPInterruptedCheckRecovery(t *testing.T) {
	testNativeSFTPInterruptedCheckRecovery(t, false)
}

func TestNativeSFTPInterruptedInventoryRecovery(t *testing.T) {
	testNativeSFTPInterruptedCheckRecovery(t, true)
}

func testNativeSFTPInterruptedCheckRecovery(t *testing.T, inventory bool) {
	if os.Getenv("STACKLAB_TEST_SFTP_RECOVERY") != "1" {
		t.Skip("set STACKLAB_TEST_SFTP_RECOVERY=1 on disposable Linux with Docker and Debian image")
	}
	if runtime.GOOS != "linux" || os.Getuid() == 0 {
		t.Fatal("run as an unprivileged Linux user with Docker administration")
	}
	for _, name := range []string{"docker", "ssh-keygen", "restic"} {
		if _, err := exec.LookPath(name); err != nil {
			t.Fatal(err)
		}
	}
	ctx, cancel := context.WithTimeout(context.Background(), 8*time.Minute)
	defer cancel()
	base := t.TempDir()
	call := func(name string, args ...string) string {
		t.Helper()
		output, err := exec.CommandContext(ctx, name, args...).CombinedOutput()
		if err != nil {
			t.Fatalf("fixture %s %v failed: %v\n%s", name, args, err, output)
		}
		return strings.TrimSpace(string(output))
	}
	resticVersion := call("restic", "version")
	t.Logf("native SFTP fixture: %s", resticVersion)
	call("ssh-keygen", "-q", "-t", "ed25519", "-N", "", "-f", filepath.Join(base, "client_key"))
	call("ssh-keygen", "-q", "-t", "ed25519", "-N", "", "-f", filepath.Join(base, "host_key"))
	configuration := "Port 22\nListenAddress 0.0.0.0\nHostKey /fixture/host_key\nAuthorizedKeysFile /fixture/client_key.pub\nPasswordAuthentication no\nKbdInteractiveAuthentication no\nUsePAM no\nStrictModes no\nAllowUsers fixture\nPidFile /run/sshd.pid\nSubsystem sftp internal-sftp\n"
	if inventory {
		// Run this same Linux test binary as an SFTP packet gate. No synthetic
		// restic output: sshd is stopped before OPENDIR(data) reaches sftp-server.
		executable, err := os.Executable()
		if err != nil {
			t.Fatal(err)
		}
		binary, err := os.ReadFile(executable)
		if err != nil {
			t.Fatal(err)
		}
		if err := os.WriteFile(filepath.Join(base, "sftp-gate"), binary, 0700); err != nil {
			t.Fatal(err)
		}
		configuration = strings.Replace(configuration, "Subsystem sftp internal-sftp", "Subsystem sftp /fixture/sftp-gate -test.run=^TestSFTPListingProtocolGate$", 1)
	}
	if err := os.WriteFile(filepath.Join(base, "sshd_config"), []byte(configuration), 0600); err != nil {
		t.Fatal(err)
	}
	portProbe, err := net.Listen("tcp", "127.0.0.1:0")
	if err != nil {
		t.Fatal(err)
	}
	serverAddress := portProbe.Addr().String()
	_ = portProbe.Close()
	name := fmt.Sprintf("stacklab-sftp-recovery-%d-%d", os.Getpid(), time.Now().UnixNano())
	image := os.Getenv("STACKLAB_TEST_SFTP_IMAGE")
	if image == "" {
		image = "debian:trixie-slim"
	}
	bootstrap := `
if ! test -x /usr/sbin/sshd; then
  apt-get update -qq
  DEBIAN_FRONTEND=noninteractive apt-get install -y -qq --no-install-recommends openssh-server
fi
if ! id fixture >/dev/null 2>&1; then
  useradd --uid "$1" --user-group --home-dir /fixture --shell /bin/sh fixture
  passwd -d fixture >/dev/null
fi
mkdir -p /run/sshd
exec /usr/sbin/sshd -D -e -f /fixture/sshd_config
`
	call("docker", "run", "-d", "--name", name, "--label", "stacklab.fixture=sftp-recovery", "-p", serverAddress+":22", "-v", base+":/fixture", image, "sh", "-eu", "-c", bootstrap, "--", strconv.Itoa(os.Getuid()))
	defer func() {
		cleanup, cleanupCancel := context.WithTimeout(context.Background(), 30*time.Second)
		defer cleanupCancel()
		if t.Failed() {
			output, _ := exec.CommandContext(cleanup, "docker", "logs", "--tail", "30", name).CombinedOutput()
			t.Logf("fixture sshd diagnostics: %s", output)
		}
		if output, err := exec.CommandContext(cleanup, "docker", "rm", "-f", name).CombinedOutput(); err != nil {
			t.Errorf("fixture container cleanup: %v %s", err, output)
		}
	}()
	waitSFTPBanner(t, ctx, serverAddress, 2*time.Minute)
	proxy := newSFTPCheckFaultProxy(t, serverAddress)
	defer proxy.Close()
	key, err := os.ReadFile(filepath.Join(base, "client_key"))
	if err != nil {
		t.Fatal(err)
	}
	hostKey, err := os.ReadFile(filepath.Join(base, "host_key.pub"))
	if err != nil {
		t.Fatal(err)
	}
	_, proxyPort, err := net.SplitHostPort(proxy.listener.Addr().String())
	if err != nil {
		t.Fatal(err)
	}
	s, _, _ := newTestService(t)
	s.runner = nativeRunner{}
	d, err := s.SaveDestination(ctx, "", DestinationInput{
		Name: "Interrupted SFTP fixture", Kind: "sftp", Repository: "sftp://fixture@127.0.0.1:" + proxyPort + "//fixture/repository",
		Secrets: &Secrets{Password: "disposable-sftp-recovery-fixture", SSHPrivateKey: string(key), SSHKnownHosts: "[127.0.0.1]:" + proxyPort + " " + string(hostKey)},
	})
	if err != nil {
		t.Fatal(err)
	}
	operationContext := func(operationCtx context.Context, action string) error {
		job, err := s.StartRepositoryJob(operationCtx, d.ID, action, "native-sftp-fixture")
		if err != nil {
			return err
		}
		_, err = s.ExecuteRepositoryJob(operationCtx, job, d.ID, action, CheckRequest{ReadData: "all"}, RestoreRequest{})
		return err
	}
	operation := func(action string) error { return operationContext(ctx, action) }
	if err := operation("backup_repository_init"); err != nil {
		t.Fatal(err)
	}
	source := filepath.Join(base, "source")
	if err := os.Mkdir(source, 0700); err != nil {
		t.Fatal(err)
	}
	file, err := os.Create(filepath.Join(source, "random.bin"))
	if err != nil {
		t.Fatal(err)
	}
	_, writeErr := io.CopyN(file, rand.Reader, 64<<20)
	if err := errors.Join(writeErr, file.Close()); err != nil {
		t.Fatal(err)
	}
	if _, err := s.restic(ctx, d, source, "backup", "--pack-size", "4", "."); err != nil {
		t.Fatal(err)
	}
	if err := operation("backup_check"); err != nil {
		t.Fatal(err)
	}
	verified := clone(s.state.Destinations[d.ID].DataCheck)
	if verified.Status != "verified" || verified.Scope != "all" {
		t.Fatalf("healthy full check has no proof: %+v", verified)
	}
	t.Log("healthy SFTP full-data check succeeded against 64 MiB random encrypted data")
	gate := proxy.Arm(8 << 20)
	if inventory {
		gate.Release()
		gate = proxy.Arm(1 << 62) // All transport remains open until OPENDIR.
		if err := os.WriteFile(filepath.Join(base, "arm-listing"), nil, 0600); err != nil {
			t.Fatal(err)
		}
		go func() {
			ticker := time.NewTicker(20 * time.Millisecond)
			defer ticker.Stop()
			for {
				select {
				case <-ctx.Done():
					return
				case <-ticker.C:
					if _, err := os.Stat(filepath.Join(base, "listing-reached")); err == nil {
						close(gate.reached)
						return
					}
				}
			}
		}()
	}
	defer gate.Release()
	interruptedDiagnostic := &sftpDiagnosticRunner{base: s.runner}
	s.runner = interruptedDiagnostic
	result := make(chan error, 1)
	checkFinished := make(chan struct{})
	interruptedCtx, cancelInterrupted := context.WithCancel(ctx)
	defer cancelInterrupted()
	go func() { defer close(checkFinished); result <- operationContext(interruptedCtx, "backup_check") }()
	defer func() {
		cancel()
		gate.Release()
		select {
		case <-checkFinished:
		case <-time.After(20 * time.Second):
			t.Error("interrupted native check did not exit during fixture cleanup")
		}
	}()
	select {
	case <-gate.reached:
		if inventory {
			t.Log("second full check reached OPENDIR(data), before pack reads; stopping dedicated sshd now")
		} else {
			t.Log("second full check read at least 8 MiB; stopping dedicated sshd now")
		}
	case err := <-result:
		t.Fatalf("full check ended before the data-read fault gate: %v", err)
	case <-time.After(90 * time.Second):
		t.Fatal("full check did not reach data-read fault gate")
	}
	call("docker", "stop", "--time", "1", name)
	gate.Release()
	var interruptedErr error
	select {
	case interruptedErr = <-result:
	case <-time.After(45 * time.Second):
		t.Fatal("production failure budget did not stop interrupted check")
	}
	<-checkFinished
	s.runner = interruptedDiagnostic.base
	if inventory {
		t.Logf("native SFTP inventory interruption diagnostics:\n%s", interruptedDiagnostic.diagnostic)
		if !containsDiagnostic(interruptedDiagnostic.diagnostic, "List(data)") {
			t.Fatal("fixture did not interrupt the data inventory")
		}
		if err := os.Remove(filepath.Join(base, "arm-listing")); err != nil {
			t.Fatal(err)
		}
	}
	var interruptedCommandErr *repositoryCommandError
	if !errors.As(interruptedErr, &interruptedCommandErr) || interruptedCommandErr.Code != "unavailable" {
		t.Fatalf("SFTP drop did not produce a bounded transport failure: %v", interruptedErr)
	}
	t.Logf("native interrupted check through production runner: %v", interruptedErr)
	afterFailure := s.state.Destinations[d.ID]
	if !reflect.DeepEqual(verified, afterFailure.DataCheck) || afterFailure.CheckStatus != "verified" || afterFailure.CheckHold != nil || afterFailure.LastCheckAttemptStatus != "failed" {
		t.Fatalf("transport failure changed integrity proof: %+v", afterFailure)
	}
	call("docker", "start", name)
	waitSFTPBanner(t, ctx, serverAddress, 20*time.Second)
	if locks := call("docker", "exec", name, "sh", "-eu", "-c", "find /fixture/repository/locks -type f | wc -l"); locks != "0" {
		if err := operation("backup_check"); err == nil {
			t.Fatal("stale lock unexpectedly allowed a new check")
		} else {
			var commandErr *repositoryCommandError
			if !errors.As(err, &commandErr) || commandErr.Code != "repository_locked" {
				t.Fatalf("expected stale-lock diagnostic after server recovery: %v", err)
			}
			t.Logf("server returned with a stale lock: %v", err)
		}
		if err := operation("backup_unlock"); err == nil || !strings.Contains(err.Error(), "unlock_refused") || !strings.Contains(err.Error(), "30 minutes") {
			t.Fatalf("fresh lock was not refused: %v", err)
		}
		// The interrupted subprocess has exited. Exercise old-dead-owner recovery
		// with a simulated Stacklab clock; restic's process check remains native.
		s.now = func() time.Time { return time.Now().UTC().Add(minimumUnlockAge + time.Minute) }
		if err := operation("backup_unlock"); err != nil {
			t.Fatal(err)
		}
		s.now = func() time.Time { return time.Now().UTC() }
		if afterUnlock := s.state.Destinations[d.ID]; afterUnlock.LastOperation != "backup_unlock" || afterUnlock.LastOperationStatus != "succeeded" || !reflect.DeepEqual(verified, afterUnlock.DataCheck) {
			t.Fatalf("unlock lost operation/evidence separation: %+v", afterUnlock)
		}
		if locks := call("docker", "exec", name, "sh", "-eu", "-c", "find /fixture/repository/locks -type f | wc -l"); locks != "0" {
			t.Fatalf("stale lock remains after Stacklab unlock: %s", locks)
		}
	} else {
		t.Log("production SIGINT cleanup removed the lock before recovery")
	}
	if err := operation("backup_check"); err != nil {
		t.Fatal(err)
	}
	recovered := s.state.Destinations[d.ID]
	if recovered.LastCheckAttemptStatus != "succeeded" || recovered.DataCheck.Status != "verified" || !recovered.DataCheck.CheckedAt.After(*verified.CheckedAt) {
		t.Fatalf("full-data proof was not refreshed after recovery: %+v", recovered)
	}
	t.Log("Stacklab unlock removed stale lock; recovered SFTP full-data check succeeded")
	// Reuse the same remote backend to distinguish genuine short reads from a
	// dropped transport. The local fixture owns the bind-mounted encrypted data.
	var pack string
	var size int64
	if err := filepath.WalkDir(filepath.Join(base, "repository", "data"), func(path string, entry os.DirEntry, err error) error {
		if err != nil {
			return err
		}
		if entry.IsDir() {
			return nil
		}
		info, err := entry.Info()
		if err != nil {
			return err
		}
		if info.Size() > size {
			pack, size = path, info.Size()
		}
		return nil
	}); err != nil {
		t.Fatal(err)
	}
	if size < 1<<20 {
		t.Fatal("fixture did not create a substantial encrypted data pack")
	}
	original, err := os.ReadFile(pack)
	if err != nil {
		t.Fatal(err)
	}
	if err := os.Truncate(pack, 64); err != nil {
		t.Fatal(err)
	}
	captured := &sftpDiagnosticRunner{base: s.runner}
	s.runner = captured
	err = operation("backup_check")
	var commandErr *repositoryCommandError
	if !errors.As(err, &commandErr) || commandErr.Code != "integrity_failed" {
		t.Fatalf("SFTP truncated pack was not corruption: %v\n%s", err, captured.diagnostic)
	}
	t.Logf("native SFTP truncated-pack check diagnostics (fixture paths only):\n%s", captured.diagnostic)
	damaged := s.state.Destinations[d.ID]
	if damaged.DataCheck.Status != "failed" || !repositoryMaintenanceBlocked(damaged) {
		t.Fatalf("truncation did not block maintenance: %+v", damaged)
	}
	if err := os.WriteFile(pack, original, 0600); err != nil {
		t.Fatal(err)
	}
	if err := operation("backup_check"); err != nil {
		t.Fatal(err)
	}
	if repaired := s.state.Destinations[d.ID]; repaired.DataCheck.Status != "verified" || repositoryMaintenanceBlocked(repaired) {
		t.Fatalf("repaired full check did not clear failure: %+v", repaired)
	}
	t.Log("real SFTP truncation blocked deletion; restoring encrypted bytes and full check recovered integrity")

}

func waitSFTPBanner(t *testing.T, ctx context.Context, address string, timeout time.Duration) {
	t.Helper()
	deadline := time.Now().Add(timeout)
	for time.Now().Before(deadline) && ctx.Err() == nil {
		connection, err := net.DialTimeout("tcp", address, time.Second)
		if err == nil {
			_ = connection.SetReadDeadline(time.Now().Add(time.Second))
			banner := make([]byte, 256)
			n, err := connection.Read(banner)
			_ = connection.Close()
			if err == nil && strings.HasPrefix(string(banner[:n]), "SSH-2.0-") {
				return
			}
		}
		time.Sleep(100 * time.Millisecond)
	}
	t.Fatalf("fixture sshd did not become ready at %s", address)
}

type sftpCheckFaultGate struct {
	remaining int64
	reached   chan struct{}
	release   chan struct{}
	once      sync.Once
}

func (g *sftpCheckFaultGate) Release() { g.once.Do(func() { close(g.release) }) }

type sftpCheckFaultProxy struct {
	listener net.Listener
	upstream string
	mu       sync.Mutex
	gate     *sftpCheckFaultGate
	closed   bool
	conns    map[net.Conn]bool
	workers  sync.WaitGroup
}

func newSFTPCheckFaultProxy(t *testing.T, upstream string) *sftpCheckFaultProxy {
	t.Helper()
	listener, err := net.Listen("tcp", "127.0.0.1:0")
	if err != nil {
		t.Fatal(err)
	}
	p := &sftpCheckFaultProxy{listener: listener, upstream: upstream, conns: map[net.Conn]bool{}}
	p.workers.Add(1)
	go func() {
		defer p.workers.Done()
		for {
			client, err := listener.Accept()
			if err != nil {
				return
			}
			server, err := net.DialTimeout("tcp", upstream, 5*time.Second)
			if err != nil {
				_ = client.Close()
				continue
			}
			p.mu.Lock()
			if p.closed {
				p.mu.Unlock()
				_ = client.Close()
				_ = server.Close()
				return
			}
			p.conns[client], p.conns[server] = true, true
			p.workers.Add(2)
			p.mu.Unlock()
			finish := func() {
				_ = client.Close()
				_ = server.Close()
				p.mu.Lock()
				delete(p.conns, client)
				delete(p.conns, server)
				p.mu.Unlock()
				p.workers.Done()
			}
			go func() { defer finish(); _, _ = io.Copy(server, client) }()
			go func() {
				defer finish()
				buffer := make([]byte, 32<<10)
				for {
					n, err := server.Read(buffer)
					if n > 0 {
						p.mu.Lock()
						gate := p.gate
						if gate != nil {
							gate.remaining -= int64(n)
							if gate.remaining <= 0 {
								p.gate = nil
								close(gate.reached)
							} else {
								gate = nil
							}
						}
						p.mu.Unlock()
						if gate != nil {
							<-gate.release
						}
						if _, writeErr := client.Write(buffer[:n]); writeErr != nil {
							return
						}
					}
					if err != nil {
						return
					}
				}
			}()
		}
	}()
	return p
}

func (p *sftpCheckFaultProxy) Arm(bytes int64) *sftpCheckFaultGate {
	p.mu.Lock()
	defer p.mu.Unlock()
	p.gate = &sftpCheckFaultGate{remaining: bytes, reached: make(chan struct{}), release: make(chan struct{})}
	return p.gate
}

func (p *sftpCheckFaultProxy) Close() {
	_ = p.listener.Close()
	p.mu.Lock()
	p.closed = true
	if p.gate != nil {
		p.gate.Release()
	}
	for connection := range p.conns {
		_ = connection.Close()
	}
	p.mu.Unlock()
	p.workers.Wait()
}

// Capture diagnostics only in this disposable fixture, never in production logs.
type sftpDiagnosticRunner struct {
	base       CommandRunner
	diagnostic string
}

func (r *sftpDiagnosticRunner) Run(ctx context.Context, name string, args, env []string, dir string) ([]byte, error) {
	if !commandIn(args, "check") {
		return r.base.Run(ctx, name, args, env, dir)
	}
	var stderr bytes.Buffer
	runner := nativeRunner{diagnosticObserver: func(data []byte) { _, _ = stderr.Write(data) }, checkFailureBudget: 2 * time.Second}
	output, err := runner.Run(ctx, name, args, env, dir)
	r.diagnostic = stderr.String()
	return output, err
}
