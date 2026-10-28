package backup

import (
	"bytes"
	"context"
	"crypto/ecdsa"
	"crypto/elliptic"
	"crypto/rand"
	"crypto/tls"
	"crypto/x509"
	"crypto/x509/pkix"
	"encoding/pem"
	"fmt"
	"io"
	"math/big"
	"net"
	"net/http"
	"net/http/httptest"
	"net/http/httputil"
	"net/url"
	"os"
	"os/exec"
	"path/filepath"
	"reflect"
	"strings"
	"sync/atomic"
	"testing"
	"time"
)

type nativeFixtureCARunner struct {
	certificate string
	runner      nativeRunner
	legacyRetry bool
}

func (r nativeFixtureCARunner) Run(ctx context.Context, name string, args, env []string, directory string) ([]byte, error) {
	args = append([]string{"--cacert", r.certificate}, args...)
	if r.legacyRetry {
		env = append(env, "RESTIC_FEATURES=backend-error-redesign=false")
	}
	return r.runner.Run(ctx, name, args, env, directory)
}

// This opt-in fixture owns its loopback HTTPS server and every object it
// corrupts. It needs no pre-existing bucket, credentials, container or root.
// Its rclone must expose serve s3 and --no-cleanup (verified with 1.75.2;
// Debian's 1.60.1 lacks serve s3). This is not a production runtime minimum.
func TestNativeS3TruncatedPackIsIntegrityFailure(t *testing.T) {
	if os.Getenv("STACKLAB_TEST_NATIVE_S3_CORRUPTION") != "1" {
		t.Skip("set STACKLAB_TEST_NATIVE_S3_CORRUPTION=1 for an isolated rclone HTTPS S3 fixture")
	}
	for _, command := range []string{"restic", "rclone"} {
		if _, err := exec.LookPath(command); err != nil {
			t.Fatalf("opt-in fixture requires %s", command)
		}
	}
	exhaustRetries := os.Getenv("STACKLAB_TEST_NATIVE_CHECK_RETRY_EXHAUSTION") == "1"
	ctx, cancel := context.WithTimeout(context.Background(), 15*time.Minute)
	defer cancel()
	directory := t.TempDir()
	storage := filepath.Join(directory, "storage")
	if err := os.MkdirAll(filepath.Join(storage, "fixture"), 0700); err != nil {
		t.Fatal(err)
	}
	certificate, key, pool := nativeS3Certificate(t, directory)
	listener, err := net.Listen("tcp", "127.0.0.1:0")
	if err != nil {
		t.Fatal(err)
	}
	address := listener.Addr().String()
	if err := listener.Close(); err != nil {
		t.Fatal(err)
	}
	startServer := func() func() {
		serverCtx, stop := context.WithCancel(ctx)
		log, err := os.CreateTemp(directory, "rclone-")
		if err != nil {
			t.Fatal(err)
		}
		command := exec.CommandContext(serverCtx, "rclone", "serve", "s3", storage, "--addr", address, "--cert", certificate, "--key", key, "--auth-key", "fixture-access,fixture-secret", "--dir-cache-time", "1s", "--vfs-cache-mode", "off", "--no-cleanup", "--config", os.DevNull, "--cache-dir", filepath.Join(directory, "cache"))
		command.Env = cleanProcessEnvironment()
		command.Stdout, command.Stderr = log, log
		if err := command.Start(); err != nil {
			stop()
			_ = log.Close()
			t.Fatal(err)
		}
		done := make(chan error, 1)
		go func() { done <- command.Wait() }()
		stopped := false
		cleanup := func() {
			if !stopped {
				stopped = true
				stop()
				<-done
				_ = log.Close()
			}
		}
		t.Cleanup(cleanup)
		transport := &http.Transport{TLSClientConfig: &tls.Config{RootCAs: pool, MinVersion: tls.VersionTLS12}}
		defer transport.CloseIdleConnections()
		client := &http.Client{Transport: transport, Timeout: 500 * time.Millisecond}
		deadline := time.Now().Add(10 * time.Second)
		for time.Now().Before(deadline) {
			response, err := client.Get("https://" + address + "/")
			if err == nil {
				_ = response.Body.Close()
				return cleanup
			}
			select {
			case err := <-done:
				stopped = true
				stop()
				_ = log.Close()
				output, _ := os.ReadFile(log.Name())
				t.Fatalf("isolated S3 server exited: %v: %s", err, output)
			default:
			}
			time.Sleep(20 * time.Millisecond)
		}
		cleanup()
		t.Fatal("isolated S3 server did not become ready")
		return nil
	}
	s, capture, _ := newTestService(t)
	s.runner = nativeRunner{}
	s.capture = &nativeArchiveCapture{fakeCapture: capture}
	identity, err := FilesystemIdentity(storage)
	if err != nil {
		t.Fatal(err)
	}
	// Construct the encrypted fixture directly on local storage. The assertion
	// concerns S3 reads and check diagnostics, independent of server upload/VFS
	// behaviour; no production destination is changed from local to S3.
	local, err := s.SaveDestination(ctx, "", DestinationInput{Name: "Local S3 fixture preparation", Kind: "local", Repository: filepath.Join(storage, "fixture", "repository"), ExpectedFilesystem: identity, Secrets: &Secrets{Password: "native-s3-test-password"}})
	if err != nil {
		t.Fatal(err)
	}
	if _, err := s.restic(ctx, local, "", "init"); err != nil {
		t.Fatal(err)
	}
	p := addPlan(t, s, local)
	job, run, err := s.StartRun(ctx, p.ID, RunRequest{}, "test")
	if err != nil {
		t.Fatal(err)
	}
	s.ExecuteRun(ctx, job, run.ID)
	if saved := s.state.Runs[run.ID]; saved.State != "succeeded" {
		t.Fatalf("native fixture backup failed: %s", saved.Error)
	}
	stopServer := startServer()
	// The proxy injects actual S3 failures; it never manufactures restic output.
	target, _ := url.Parse("https://" + address)
	transport := &http.Transport{TLSClientConfig: &tls.Config{RootCAs: pool, MinVersion: tls.VersionTLS12}}
	defer transport.CloseIdleConnections()
	proxy := httputil.NewSingleHostReverseProxy(target)
	proxy.Transport = transport
	var fault atomic.Int32 // 1: listing InternalError, 2: partial pack, 3: GET InternalError
	var faultHits atomic.Int32
	proxy.ModifyResponse = func(response *http.Response) error {
		if fault.Load() == 2 && response.Request.Method == "GET" && strings.Contains(response.Request.URL.Path, "/data/") && response.StatusCode == 200 && response.ContentLength > 1 {
			faultHits.Add(1)
			// Preserve the advertised size but close before all bytes arrive.
			response.Body = &nativePartialS3Body{Reader: io.LimitReader(response.Body, response.ContentLength/2), Closer: response.Body}
		}
		return nil
	}
	faultServer := httptest.NewUnstartedServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		if r.Method == "GET" && ((fault.Load() == 1 && strings.Contains(r.URL.Query().Get("prefix"), "/data")) || (fault.Load() == 3 && strings.Contains(r.URL.Path, "/data/"))) {
			faultHits.Add(1)
			w.Header().Set("Content-Type", "application/xml")
			w.WriteHeader(http.StatusInternalServerError)
			_, _ = fmt.Fprint(w, `<Error><Code>InternalError</Code><Message>We encountered an internal error. Please try again.</Message></Error>`)
			return
		}
		proxy.ServeHTTP(w, r)
	}))
	pair, err := tls.LoadX509KeyPair(certificate, key)
	if err != nil {
		t.Fatal(err)
	}
	faultServer.TLS = &tls.Config{Certificates: []tls.Certificate{pair}, MinVersion: tls.VersionTLS12}
	faultServer.StartTLS()
	defer faultServer.Close()
	var diagnostics bytes.Buffer
	budget := 2 * time.Second
	if exhaustRetries {
		budget = checkBackendFailureTimeout
	}
	s.runner = nativeFixtureCARunner{certificate: certificate, legacyRetry: exhaustRetries, runner: nativeRunner{diagnosticObserver: func(data []byte) { _, _ = diagnostics.Write(data) }, checkFailureBudget: budget}}
	d, err := s.SaveDestination(ctx, "", DestinationInput{Name: "Isolated S3 corruption", Kind: "s3", Repository: "s3:" + faultServer.URL + "/fixture/repository", Secrets: &Secrets{Password: "native-s3-test-password", AccessKeyID: "fixture-access", SecretAccessKey: "fixture-secret"}})
	if err != nil {
		t.Fatal(err)
	}
	check := func(wantErr bool) Destination {
		job, err := s.StartRepositoryJob(ctx, d.ID, "backup_check", "test")
		if err != nil {
			t.Fatal(err)
		}
		_, err = s.ExecuteRepositoryJob(ctx, job, d.ID, "backup_check", CheckRequest{ReadData: "all"}, RestoreRequest{})
		if (err != nil) != wantErr {
			t.Fatalf("native S3 full check error=%v, wantErr=%v", err, wantErr)
		}
		return s.state.Destinations[d.ID]
	}
	d = check(false)
	t.Log("native HTTPS S3 baseline full check completed")
	verified := d.DataCheck
	for _, mode := range []int32{1, 2, 3} {
		diagnostics.Reset()
		faultHits.Store(0)
		fault.Store(mode)
		started := time.Now()
		d = check(true)
		if faultHits.Load() == 0 || !strings.Contains(d.LastCheckAttemptError, "unavailable") || d.CheckHold != nil || !reflect.DeepEqual(d.DataCheck, verified) {
			t.Fatalf("S3 fault mode %d changed proof or missed backend: %+v\n%s", mode, d, diagnostics.String())
		}
		t.Logf("native S3 fault mode=%d requests=%d stopped in %s: %s\n%s", mode, faultHits.Load(), time.Since(started), d.LastCheckAttemptError, diagnostics.String())
		if exhaustRetries {
			text := strings.ToLower(diagnostics.String())
			terminal := strings.Contains(text, " failed:") || strings.Contains(text, "failed to download:") || strings.Contains(text, "does not exist")
			for _, line := range strings.Split(text, "\n") {
				terminal = terminal || resticReadFullFailure.MatchString(strings.TrimSpace(line))
			}
			if !terminal {
				t.Fatal("retry-exhaustion fixture did not reach terminal backend/object diagnostics")
			}
		}
		if output := os.Getenv("STACKLAB_TEST_DIAGNOSTIC_DIR"); output != "" {
			if err := os.WriteFile(filepath.Join(output, fmt.Sprintf("s3-mode-%d.stderr", mode)), diagnostics.Bytes(), 0600); err != nil {
				t.Fatal(err)
			}
		}
		fault.Store(0)
		d = check(false)
		verified = d.DataCheck
	}

	stopServer()
	packs, err := filepath.Glob(filepath.Join(storage, "fixture", "repository", "data", "*", "*"))
	if err != nil || len(packs) == 0 {
		t.Fatalf("fixture has no encrypted pack: %v", err)
	}
	pack := packs[0]
	original, err := os.ReadFile(pack)
	if err != nil || len(original) < 64 {
		t.Fatalf("fixture pack could not be read: %v", err)
	}
	if err := os.Chmod(pack, 0600); err != nil {
		t.Fatal(err)
	}
	if err := os.Truncate(pack, int64(len(original)/2)); err != nil {
		t.Fatal(err)
	}
	// Restarting also invalidates rclone's object metadata and read caches.
	stopServer = startServer()
	faultHits.Store(0)
	fault.Store(2)
	d = check(true)
	if faultHits.Load() == 0 {
		t.Fatal("corruption/transport mixture did not inject a partial response")
	}
	fault.Store(0)
	if d.DataCheck.Status != "failed" || !strings.Contains(d.LastCheckAttemptError, "integrity_failed") || !repositoryMaintenanceBlocked(d) {
		t.Fatalf("truncated S3 pack was not a concrete integrity failure: %+v", d)
	}
	stopServer()
	if err := os.WriteFile(pack, original, 0600); err != nil {
		t.Fatal(err)
	}
	startServer()
	d = check(false)
	if d.CheckStatus != "verified" || repositoryMaintenanceBlocked(d) {
		t.Fatalf("repaired S3 fixture did not recover: %+v", d)
	}
}

func nativeS3Certificate(t *testing.T, directory string) (string, string, *x509.CertPool) {
	t.Helper()
	key, err := ecdsa.GenerateKey(elliptic.P256(), rand.Reader)
	if err != nil {
		t.Fatal(err)
	}
	template := &x509.Certificate{SerialNumber: big.NewInt(1), Subject: pkix.Name{CommonName: "Stacklab isolated S3 fixture"}, NotBefore: time.Now().Add(-time.Hour), NotAfter: time.Now().Add(time.Hour), IPAddresses: []net.IP{net.ParseIP("127.0.0.1")}, KeyUsage: x509.KeyUsageDigitalSignature | x509.KeyUsageCertSign, ExtKeyUsage: []x509.ExtKeyUsage{x509.ExtKeyUsageServerAuth}, BasicConstraintsValid: true, IsCA: true}
	der, err := x509.CreateCertificate(rand.Reader, template, template, &key.PublicKey, key)
	if err != nil {
		t.Fatal(err)
	}
	keyDER, err := x509.MarshalECPrivateKey(key)
	if err != nil {
		t.Fatal(err)
	}
	certificate, keyPath := filepath.Join(directory, "fixture.crt"), filepath.Join(directory, "fixture.key")
	certificatePEM := pem.EncodeToMemory(&pem.Block{Type: "CERTIFICATE", Bytes: der})
	if err := os.WriteFile(certificate, certificatePEM, 0600); err != nil {
		t.Fatal(err)
	}
	if err := os.WriteFile(keyPath, pem.EncodeToMemory(&pem.Block{Type: "EC PRIVATE KEY", Bytes: keyDER}), 0600); err != nil {
		t.Fatal(err)
	}
	pool := x509.NewCertPool()
	if !pool.AppendCertsFromPEM(certificatePEM) {
		t.Fatal("fixture certificate could not be loaded")
	}
	return certificate, keyPath, pool
}

// A short body with the original Content-Length makes the proxy close the native
// HTTP response early. Its underlying network reader still closes normally.
type nativePartialS3Body struct {
	io.Reader
	io.Closer
}
