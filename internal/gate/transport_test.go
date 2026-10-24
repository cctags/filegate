package gate

import (
	"context"
	"crypto/ecdsa"
	"crypto/elliptic"
	"crypto/rand"
	"crypto/tls"
	"crypto/x509"
	"crypto/x509/pkix"
	"errors"
	"io"
	"math/big"
	"net"
	"net/http"
	"net/http/cookiejar"
	"net/url"
	"regexp"
	"strings"
	"sync"
	"testing"
	"time"
)

// Exercise real HTTP/TLS framing using in-memory connections, without binding
// a local port or depending on permission to create sockets in a sandbox.
type pipeListener struct {
	incoming chan net.Conn
	done     chan struct{}
	once     sync.Once
}

func newPipeListener() *pipeListener {
	return &pipeListener{incoming: make(chan net.Conn), done: make(chan struct{})}
}
func (l *pipeListener) Accept() (net.Conn, error) {
	select {
	case c := <-l.incoming:
		return c, nil
	case <-l.done:
		return nil, net.ErrClosed
	}
}
func (l *pipeListener) Close() error   { l.once.Do(func() { close(l.done) }); return nil }
func (l *pipeListener) Addr() net.Addr { return &net.TCPAddr{IP: net.IPv4(127, 0, 0, 1), Port: 17342} }

type ipv4Pipe struct{ net.Conn }

func (c ipv4Pipe) RemoteAddr() net.Addr { return &net.TCPAddr{IP: net.IPv4(192, 0, 2, 1), Port: 1234} }
func (l *pipeListener) dial(ctx context.Context) (net.Conn, error) {
	client, server := net.Pipe()
	select {
	case l.incoming <- ipv4Pipe{server}:
		return client, nil
	case <-ctx.Done():
		client.Close()
		server.Close()
		return nil, ctx.Err()
	case <-l.done:
		client.Close()
		server.Close()
		return nil, net.ErrClosed
	}
}
func TestConnectionLimitAndClose(t *testing.T) {
	raw := newPipeListener()
	l := newLimitedListener(raw, 1)
	defer l.Close()
	// A queued second Accept must wait until the first connection closes.
	peers := make(chan net.Conn, 2)
	go func() { c, _ := raw.dial(context.Background()); peers <- c }()
	first, err := l.Accept()
	if err != nil {
		t.Fatal(err)
	}
	defer first.Close()
	peer := <-peers
	defer peer.Close()
	accepted := make(chan net.Conn, 1)
	go func() { c, _ := l.Accept(); accepted <- c }()
	go func() { c, _ := raw.dial(context.Background()); peers <- c }()
	select {
	case <-accepted:
		t.Fatal("connection limit bypassed")
	case <-time.After(30 * time.Millisecond):
	}
	first.Close()
	first.Close() // Repeated close must not release another slot.
	var second net.Conn
	select {
	case second = <-accepted:
	case <-time.After(time.Second):
		t.Fatal("connection slot not released")
	}
	if second == nil {
		t.Fatal("second accept failed")
	}
	defer second.Close()
	peer2 := <-peers
	defer peer2.Close()
	closed := make(chan error, 1)
	go func() { _, err := l.Accept(); closed <- err }()
	l.Close()
	select {
	case err := <-closed:
		if !errors.Is(err, net.ErrClosed) {
			t.Fatalf("close: %v", err)
		}
	case <-time.After(time.Second):
		t.Fatal("close did not wake blocked accept")
	}
}
func TestHTTP2WithTLS(t *testing.T) {
	a := setup(t)
	key, err := ecdsa.GenerateKey(elliptic.P256(), rand.Reader)
	if err != nil {
		t.Fatal(err)
	}
	template := &x509.Certificate{SerialNumber: big.NewInt(1), Subject: pkix.Name{CommonName: "filegate.test"}, DNSNames: []string{"filegate.test"}, NotBefore: time.Now().Add(-time.Minute), NotAfter: time.Now().Add(time.Hour), KeyUsage: x509.KeyUsageDigitalSignature, ExtKeyUsage: []x509.ExtKeyUsage{x509.ExtKeyUsageServerAuth}}
	der, err := x509.CreateCertificate(rand.Reader, template, template, &key.PublicKey, key)
	if err != nil {
		t.Fatal(err)
	}
	certificate, err := x509.ParseCertificate(der)
	if err != nil {
		t.Fatal(err)
	}
	pool := x509.NewCertPool()
	pool.AddCert(certificate)
	listener := newLimitedListener(newPipeListener(), maxConnections)
	server := &http.Server{Handler: a.Handler(), TLSConfig: &tls.Config{Certificates: []tls.Certificate{{Certificate: [][]byte{der}, PrivateKey: key}}}, ReadHeaderTimeout: time.Second}
	done := make(chan error, 1)
	go func() { done <- server.ServeTLS(listener, "", "") }()
	defer func() { server.Close(); <-done }()
	transport := &http.Transport{TLSClientConfig: &tls.Config{RootCAs: pool}, ForceAttemptHTTP2: true, DialContext: func(ctx context.Context, network, address string) (net.Conn, error) {
		return listener.Listener.(*pipeListener).dial(ctx)
	}}
	defer transport.CloseIdleConnections()
	jar, err := cookiejar.New(nil)
	if err != nil {
		t.Fatal(err)
	}
	client := &http.Client{Transport: transport, Timeout: 3 * time.Second, Jar: jar}
	response, err := client.Get("https://filegate.test/")
	if err != nil {
		t.Fatal(err)
	}
	defer response.Body.Close()
	_, err = io.Copy(io.Discard, response.Body)
	if err != nil || response.StatusCode != 200 || response.ProtoMajor != 2 {
		t.Fatalf("HTTPS/HTTP2 failed: %s %d %v", response.Proto, response.StatusCode, err)
	}
	response.Body.Close()
	code := createCode(t, a, "ab")
	response, err = client.Get("https://filegate.test/access?code=" + code.Code)
	if err != nil {
		t.Fatal(err)
	}
	body, err := io.ReadAll(response.Body)
	response.Body.Close()
	if err != nil || response.StatusCode != 200 || response.Request.URL.Path != "/access" || response.Request.URL.RawQuery != "" || len(a.state.Codes) != 1 || len(a.sessions) != 0 {
		t.Fatalf("link redirect prepared authentication incorrectly: %v", err)
	}
	csrf := regexp.MustCompile(`name="csrf" value="([^"]+)"`).FindStringSubmatch(string(body))
	if len(csrf) != 2 {
		t.Fatal("missing confirmation token over HTTP2")
	}
	response, err = client.PostForm("https://filegate.test/access", url.Values{"csrf": {csrf[1]}})
	if err != nil {
		t.Fatal(err)
	}
	body, err = io.ReadAll(response.Body)
	response.Body.Close()
	if err != nil || response.StatusCode != 200 || response.ProtoMajor != 2 || response.Request.URL.Path != "/browse/" || !strings.Contains(string(body), "hello.txt") {
		t.Fatalf("link login over TLS/HTTP2 failed: %v", err)
	}
	target, _ := url.Parse("https://filegate.test/access")
	for _, cookie := range jar.Cookies(target) {
		if cookie.Name == pendingCookieName {
			t.Fatal("confirmation cookie remained after login")
		}
	}
}

type pipeResponse struct {
	net.Conn
	header http.Header
}

func (w *pipeResponse) Header() http.Header { return w.header }
func (w *pipeResponse) WriteHeader(int)     {}
func TestBlockedWriteExpires(t *testing.T) {
	server, client := net.Pipe()
	defer server.Close()
	defer client.Close()
	response := &pipeResponse{Conn: server, header: http.Header{}}
	writer := &idleWriter{ResponseWriter: response, controller: http.NewResponseController(response), timeout: 30 * time.Millisecond}
	done := make(chan error, 1)
	go func() { _, err := writer.Write([]byte("unread")); done <- err }()
	select {
	case err := <-done:
		var timeout net.Error
		if !errors.As(err, &timeout) || !timeout.Timeout() {
			t.Fatalf("expected write timeout: %v", err)
		}
	case <-time.After(time.Second):
		t.Fatal("blocked write had no deadline")
	}
}
