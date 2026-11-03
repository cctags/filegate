package main

import (
	"bytes"
	"context"
	"crypto/ecdsa"
	"crypto/elliptic"
	"crypto/rand"
	"crypto/tls"
	"crypto/x509"
	"crypto/x509/pkix"
	"encoding/json"
	"encoding/pem"
	"io"
	"math/big"
	"net"
	"net/http"
	"net/url"
	"os"
	"os/exec"
	"path/filepath"
	"strings"
	"syscall"
	"testing"
	"time"

	"filegate/internal/gate"
)

type processFixture struct {
	binary, config, base string
	cfg                  gate.Config
	client               *http.Client
}

// Allow the 10-second server shutdown budget plus process scheduling overhead.
const processStopTimeout = 12 * time.Second

type childProcess struct {
	client *http.Client
	cmd    *exec.Cmd
	done   chan struct{}
	err    error
	log    string
}

func processFixtureForTest(t *testing.T) *processFixture {
	t.Helper()
	if os.Getenv("FILEGATE_INTEGRATION") != "1" {
		t.Skip("run make test-integration for real process tests")
	}
	if os.Geteuid() == 0 {
		t.Skip("process serve tests require a non-root user")
	}
	// Linux Unix socket paths are short; do not include the full test name.
	base, tempErr := os.MkdirTemp(os.Getenv("TMPDIR"), "process-")
	if tempErr != nil {
		t.Fatal(tempErr)
	}
	t.Cleanup(func() { os.RemoveAll(base) })
	binary := filepath.Join(base, "filegate")
	ctx, cancel := context.WithTimeout(context.Background(), 30*time.Second)
	defer cancel()
	build := exec.CommandContext(ctx, "go", "build", "-o", binary, ".")
	build.Env = append(os.Environ(), "CGO_ENABLED=0")
	if out, err := build.CombinedOutput(); err != nil {
		t.Fatalf("build: %v %s", err, out)
	}
	transport := http.DefaultTransport.(*http.Transport).Clone()
	f := &processFixture{binary: binary, config: filepath.Join(base, "config.json"), base: base, client: &http.Client{Transport: transport, Timeout: 2 * time.Second, CheckRedirect: func(*http.Request, []*http.Request) error { return http.ErrUseLastResponse }}}
	t.Cleanup(f.client.CloseIdleConnections)
	f.command(t, "init")
	var err error
	f.cfg, err = gate.Load(f.config)
	if err != nil {
		t.Fatal(err)
	}
	f.cfg.Listen = freeAddress(t)
	f.save(t)
	if err := os.WriteFile(filepath.Join(f.cfg.Root, "文件夹", "资料.txt"), []byte("中文内容"), 0600); err != nil {
		t.Fatal(err)
	}
	return f
}

func freeAddress(t *testing.T) string {
	t.Helper()
	l, err := net.Listen("tcp4", "127.0.0.1:0")
	if err != nil {
		t.Fatal(err)
	}
	address := l.Addr().String()
	l.Close()
	return address
}
func (f *processFixture) save(t *testing.T) {
	t.Helper()
	data, err := json.Marshal(f.cfg)
	if err != nil {
		t.Fatal(err)
	}
	if err := os.WriteFile(f.config, data, 0600); err != nil {
		t.Fatal(err)
	}
}
func (f *processFixture) command(t *testing.T, args ...string) []byte {
	t.Helper()
	ctx, cancel := context.WithTimeout(context.Background(), 5*time.Second)
	defer cancel()
	c := exec.CommandContext(ctx, f.binary, append([]string{"--config", f.config}, args...)...)
	var stderr bytes.Buffer
	c.Stderr = &stderr
	out, err := c.Output()
	if err != nil {
		t.Fatalf("CLI %s: %v %s", args[0], err, stderr.String())
	}
	return out
}
func (f *processFixture) admin(t *testing.T, args ...string) gate.Reply {
	t.Helper()
	var out gate.Reply
	if err := json.Unmarshal(f.command(t, args...), &out); err != nil {
		t.Fatal(err)
	}
	return out
}
func (f *processFixture) url() string {
	scheme := "http"
	if f.cfg.TLSCert != "" {
		scheme = "https"
	}
	return scheme + "://" + f.cfg.Listen
}

func waitProcessCondition(t *testing.T, fn func() bool) {
	t.Helper()
	deadline := time.NewTimer(5 * time.Second)
	defer deadline.Stop()
	tick := time.NewTicker(10 * time.Millisecond)
	defer tick.Stop()
	for {
		if fn() {
			return
		}
		select {
		case <-deadline.C:
			t.Fatal("process condition timed out")
		case <-tick.C:
		}
	}
}
func (f *processFixture) start(t *testing.T) *childProcess {
	t.Helper()
	log, err := os.CreateTemp(f.base, "process-*.log")
	if err != nil {
		t.Fatal(err)
	}
	p := &childProcess{client: f.client, cmd: exec.Command(f.binary, "--config", f.config, "serve"), done: make(chan struct{}), log: log.Name()}
	p.cmd.Stdout, p.cmd.Stderr = log, log
	if err := p.cmd.Start(); err != nil {
		log.Close()
		t.Fatal(err)
	}
	go func() { p.err = p.cmd.Wait(); log.Close(); close(p.done) }()
	t.Cleanup(func() {
		select {
		case <-p.done:
		default:
			p.cmd.Process.Kill()
			<-p.done
		}
	})
	waitProcessCondition(t, func() bool {
		select {
		case <-p.done:
			data, _ := os.ReadFile(p.log)
			t.Fatalf("serve exited before ready: %v %s", p.err, data)
		default:
		}
		res, err := f.client.Get(f.url() + "/")
		if err != nil {
			return false
		}
		defer res.Body.Close()
		return res.StatusCode == 200
	})
	return p
}
func (p *childProcess) stop(t *testing.T, sig os.Signal) {
	t.Helper()
	p.client.CloseIdleConnections()
	timer := time.NewTimer(processStopTimeout)
	defer timer.Stop()
	if err := p.cmd.Process.Signal(sig); err != nil {
		t.Fatal(err)
	}
	select {
	case <-p.done:
		if p.err != nil {
			t.Fatalf("process exit: %v", p.err)
		}
	case <-timer.C:
		p.cmd.Process.Kill()
		<-p.done
		t.Fatalf("process did not exit within %s", processStopTimeout)
	}
	data, err := os.ReadFile(p.log)
	if err != nil || !bytes.Contains(data, []byte("filegate stopped")) {
		t.Fatal("shutdown log missing")
	}
}
func (p *childProcess) waitLog(t *testing.T, text string) {
	t.Helper()
	waitProcessCondition(t, func() bool { data, _ := os.ReadFile(p.log); return bytes.Contains(data, []byte(text)) })
}
func (f *processFixture) request(t *testing.T, method, path, body string, cookie *http.Cookie) (*http.Response, string) {
	t.Helper()
	r, err := http.NewRequest(method, f.url()+path, strings.NewReader(body))
	if err != nil {
		t.Fatal(err)
	}
	if method == "POST" {
		r.Header.Set("Content-Type", "application/x-www-form-urlencoded")
		r.Header.Set("Origin", f.url())
	}
	if cookie != nil {
		r.AddCookie(cookie)
	}
	res, err := f.client.Do(r)
	if err != nil {
		t.Fatal(err)
	}
	defer res.Body.Close()
	data, err := io.ReadAll(res.Body)
	if err != nil {
		t.Fatal(err)
	}
	return res, string(data)
}
func (f *processFixture) login(t *testing.T, code string) *http.Cookie {
	t.Helper()
	res, _ := f.request(t, "POST", "/login", url.Values{"code": {code}}.Encode(), nil)
	if res.StatusCode != 303 {
		t.Fatalf("process login: %d", res.StatusCode)
	}
	for _, cookie := range res.Cookies() {
		if cookie.Name == "filegate_session" {
			return cookie
		}
	}
	t.Fatal("session missing")
	return nil
}

func TestProcessLifecycle(t *testing.T) {
	f := processFixtureForTest(t)
	f.command(t, "check")
	p := f.start(t)
	issued := f.admin(t, "code", "ch", "--base-url", f.url())
	f.command(t, "check")
	if issued.URL == "" {
		t.Fatal("CLI did not emit access URL")
	}
	cookie := f.login(t, issued.Code)
	res, body := f.request(t, "GET", "/file/"+url.PathEscape("资料.txt"), "", cookie)
	if res.StatusCode != 200 || body != "中文内容" {
		t.Fatal("Chinese file unavailable")
	}
	f.admin(t, "block", "127.0.0.1")
	res, _ = f.request(t, "GET", "/browse/", "", cookie)
	if res.StatusCode != 403 {
		t.Fatal("CLI block not enforced")
	}
	out := f.admin(t, "unblock", "127.0.0.1")
	if out.Unblock == nil || out.Unblock.BlacklistRemoved != 1 {
		t.Fatal("CLI unblock result lost")
	}
	cookie = f.login(t, f.admin(t, "code", "ch").Code)
	f.admin(t, "logout", "ch")
	res, _ = f.request(t, "GET", "/browse/", "", cookie)
	if res.StatusCode != 303 {
		t.Fatal("CLI logout not enforced")
	}
	revoked := f.admin(t, "code", "ab")
	f.admin(t, "revoke", revoked.ID)
	res, _ = f.request(t, "POST", "/login", "code="+revoked.Code, nil)
	if res.StatusCode != 401 {
		t.Fatal("CLI revoke not enforced")
	}
	oldCookie := f.login(t, f.admin(t, "code", "ab").Code)
	oldCode := f.admin(t, "code", "ab")
	if err := os.Mkdir(filepath.Join(f.cfg.Root, "replacement"), 0700); err != nil {
		t.Fatal(err)
	}
	if err := os.WriteFile(filepath.Join(f.cfg.Root, "replacement", "new.txt"), []byte("new mapping"), 0600); err != nil {
		t.Fatal(err)
	}
	f.cfg.Directories["ab"] = "replacement"
	f.save(t)
	if err := p.cmd.Process.Signal(syscall.SIGHUP); err != nil {
		t.Fatal(err)
	}
	p.waitLog(t, "configuration reloaded")
	res, _ = f.request(t, "GET", "/browse/", "", oldCookie)
	if res.StatusCode != 303 {
		t.Fatal("SIGHUP retained old session")
	}
	status := f.admin(t, "status")
	for _, c := range status.Codes {
		if c.ID == oldCode.ID {
			t.Fatal("SIGHUP retained old code")
		}
	}
	fresh := f.admin(t, "code", "ab")
	cookie = f.login(t, fresh.Code)
	res, body = f.request(t, "GET", "/file/new.txt", "", cookie)
	if res.StatusCode != 200 || body != "new mapping" {
		t.Fatal("SIGHUP did not apply mapping")
	}
	if err := os.WriteFile(f.config, []byte("{broken"), 0600); err != nil {
		t.Fatal(err)
	}
	if err := p.cmd.Process.Signal(syscall.SIGHUP); err != nil {
		t.Fatal(err)
	}
	p.waitLog(t, "configuration reload failed")
	res, _ = f.request(t, "GET", "/file/new.txt", "", cookie)
	if res.StatusCode != 200 {
		t.Fatal("bad reload broke existing service")
	}
	f.save(t)
	ctx, cancel := context.WithTimeout(context.Background(), 3*time.Second)
	second := exec.CommandContext(ctx, f.binary, "--config", f.config, "serve")
	data, err := second.CombinedOutput()
	cancel()
	if err == nil || !bytes.Contains(data, []byte("another server owns")) {
		t.Fatal("second process accepted shared state")
	}
	p.stop(t, syscall.SIGTERM)
	p = f.start(t)
	res, _ = f.request(t, "GET", "/file/new.txt", "", cookie)
	if res.StatusCode != 303 {
		t.Fatal("session survived process restart")
	}
	res, _ = f.request(t, "POST", "/login", "code="+fresh.Code, nil)
	if res.StatusCode != 401 {
		t.Fatal("consumed code returned after process restart")
	}
	if len(f.admin(t, "status").Blacklist) != 0 {
		t.Fatal("unblock did not survive restart")
	}
	p.stop(t, os.Interrupt)
}

// Keep an incomplete request alive until ReadHeaderTimeout expires. The
// shutdown helper must allow the server's grace period and polling overhead.
func TestProcessShutdownWithPendingHeaders(t *testing.T) {
	f := processFixtureForTest(t)
	transport := &http.Transport{DisableKeepAlives: true}
	t.Cleanup(transport.CloseIdleConnections)
	f.client.Transport = transport
	p := f.start(t)
	conn, err := net.DialTimeout("tcp4", f.cfg.Listen, time.Second)
	if err != nil {
		t.Fatal(err)
	}
	defer conn.Close()
	if err := conn.SetWriteDeadline(time.Now().Add(time.Second)); err != nil {
		t.Fatal(err)
	}
	if _, err := io.WriteString(conn, "GET / HTTP/1.1\r\nHost: localhost\r\n"); err != nil {
		t.Fatal(err)
	}
	// A later, independent connection completes a request, ensuring the accept
	// loop has passed the pending connection without relying on a sleep.
	res, _ := f.request(t, "GET", "/", "", nil)
	if res.StatusCode != http.StatusOK {
		t.Fatal("shutdown probe failed")
	}
	p.stop(t, syscall.SIGTERM)
}

func testCertificateFiles(t *testing.T, base string) (string, string, *x509.CertPool) {
	t.Helper()
	key, err := ecdsa.GenerateKey(elliptic.P256(), rand.Reader)
	if err != nil {
		t.Fatal(err)
	}
	template := &x509.Certificate{SerialNumber: big.NewInt(1), Subject: pkix.Name{CommonName: "localhost"}, IPAddresses: []net.IP{net.ParseIP("127.0.0.1")}, NotBefore: time.Now().Add(-time.Minute), NotAfter: time.Now().Add(time.Hour), KeyUsage: x509.KeyUsageDigitalSignature, ExtKeyUsage: []x509.ExtKeyUsage{x509.ExtKeyUsageServerAuth}}
	der, err := x509.CreateCertificate(rand.Reader, template, template, &key.PublicKey, key)
	if err != nil {
		t.Fatal(err)
	}
	keyDER, err := x509.MarshalPKCS8PrivateKey(key)
	if err != nil {
		t.Fatal(err)
	}
	certFile, keyFile := filepath.Join(base, "test.crt"), filepath.Join(base, "test.key")
	for path, data := range map[string][]byte{certFile: pem.EncodeToMemory(&pem.Block{Type: "CERTIFICATE", Bytes: der}), keyFile: pem.EncodeToMemory(&pem.Block{Type: "PRIVATE KEY", Bytes: keyDER})} {
		if err := os.WriteFile(path, data, 0600); err != nil {
			t.Fatal(err)
		}
	}
	cert, err := x509.ParseCertificate(der)
	if err != nil {
		t.Fatal(err)
	}
	pool := x509.NewCertPool()
	pool.AddCert(cert)
	return certFile, keyFile, pool
}

func TestProcessTLSAndStartupFailures(t *testing.T) {
	f := processFixtureForTest(t)
	cert, key, pool := testCertificateFiles(t, f.base)
	original := f.cfg
	for _, failure := range []string{"occupied port", "missing certificate", "invalid TLS", "logging path"} {
		t.Run(failure, func(t *testing.T) {
			f.cfg = original
			switch failure {
			case "occupied port":
				l, err := net.Listen("tcp4", "127.0.0.1:0")
				if err != nil {
					t.Fatal(err)
				}
				defer l.Close()
				f.cfg.Listen = l.Addr().String()
			case "missing certificate":
				f.cfg.TLSCert, f.cfg.TLSKey = cert+".missing", key
			case "invalid TLS":
				bad := filepath.Join(f.base, "invalid.crt")
				if err := os.WriteFile(bad, []byte("bad"), 0600); err != nil {
					t.Fatal(err)
				}
				f.cfg.TLSCert, f.cfg.TLSKey = bad, key
			case "logging path":
				bad := filepath.Join(f.base, "logs-file")
				if err := os.WriteFile(bad, nil, 0600); err != nil {
					t.Fatal(err)
				}
				f.cfg.Logging.Directory = bad
			}
			f.save(t)
			ctx, cancel := context.WithTimeout(context.Background(), 4*time.Second)
			defer cancel()
			check := exec.CommandContext(ctx, f.binary, "--config", f.config, "check")
			checkErr := check.Run()
			if (checkErr == nil) != (failure == "occupied port") || ctx.Err() != nil {
				t.Fatalf("unexpected check result for %s: %v", failure, checkErr)
			}
			cmd := exec.CommandContext(ctx, f.binary, "--config", f.config, "serve")
			if err := cmd.Run(); err == nil || ctx.Err() != nil {
				t.Fatal("bad startup succeeded or hung")
			}
			f.cfg = original
			f.cfg.Listen = freeAddress(t)
			f.save(t)
			p := f.start(t)
			p.stop(t, syscall.SIGTERM)
		})
	}
	f.cfg = original
	f.cfg.Listen = freeAddress(t)
	f.cfg.TLSCert, f.cfg.TLSKey = cert, key
	f.save(t)
	f.command(t, "check")
	transport := &http.Transport{TLSClientConfig: &tls.Config{RootCAs: pool}, ForceAttemptHTTP2: true}
	defer transport.CloseIdleConnections()
	f.client.Transport = transport
	p := f.start(t)
	cookie := f.login(t, f.admin(t, "code", "ch").Code)
	if !cookie.Secure {
		t.Fatal("real TLS login did not set Secure")
	}
	res, body := f.request(t, "GET", "/file/"+url.PathEscape("资料.txt"), "", cookie)
	if res.StatusCode != 200 || res.ProtoMajor != 2 || body != "中文内容" {
		t.Fatal("configured TLS/HTTP2 file access failed")
	}
	p.stop(t, syscall.SIGTERM)
}
