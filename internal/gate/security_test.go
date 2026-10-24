package gate

import (
	"bytes"
	"fmt"
	"io"
	"net/http"
	"net/http/httptest"
	"os"
	"path/filepath"
	"strings"
	"sync"
	"testing"
	"time"
)

type pausedBody struct {
	started, release chan struct{}
	once             sync.Once
	reader           io.Reader
}

func (b *pausedBody) Read(p []byte) (int, error) {
	b.once.Do(func() { close(b.started) })
	<-b.release
	return b.reader.Read(p)
}
func (b *pausedBody) Close() error { return nil }
func pausedLogin(t *testing.T, a *App, code string) (*httptest.ResponseRecorder, func()) {
	t.Helper()
	body := &pausedBody{started: make(chan struct{}), release: make(chan struct{}), reader: strings.NewReader("code=" + code)}
	r := httptest.NewRequest("POST", "/login", nil)
	r.Body = body
	r.ContentLength = -1
	r.RemoteAddr = "192.0.2.1:1234"
	r.Header.Set("Content-Type", "application/x-www-form-urlencoded")
	w := httptest.NewRecorder()
	done := make(chan struct{})
	var once sync.Once
	finish := func() { once.Do(func() { close(body.release) }); <-done }
	t.Cleanup(finish)
	go func() { defer close(done); a.Handler().ServeHTTP(w, r) }()
	select {
	case <-body.started:
	case <-time.After(time.Second):
		t.Fatal("request did not reach body read")
	}
	return w, finish
}
func TestSlowLoginDoesNotLockService(t *testing.T) {
	a := setup(t)
	_, finish := pausedLogin(t, a, "wrong")
	done := make(chan struct{})
	go func() { defer close(done); request(a, "GET", "/", "", nil); a.Admin(Request{Action: "status"}) }()
	select {
	case <-done:
	case <-time.After(time.Second):
		t.Fatal("slow body blocked unrelated requests or administration")
	}
	finish()
}
func TestDelayedLoginRechecksAuthorization(t *testing.T) {
	for _, change := range []string{"expiry", "block", "revoke", "reload"} {
		t.Run(change, func(t *testing.T) {
			a := setup(t)
			file, cfg := reloadConfig(t, a)
			code := createCode(t, a, "ab")
			w, finish := pausedLogin(t, a, code.Code)
			switch change {
			case "expiry":
				a.mu.Lock()
				c := a.state.Codes[code.ID]
				c.Expires = time.Now().Add(20 * time.Millisecond)
				a.state.Codes[code.ID] = c
				a.mu.Unlock()
				time.Sleep(time.Until(c.Expires) + 10*time.Millisecond)
			case "block":
				if out := a.Admin(Request{Action: "block", Value: "192.0.2.1"}); out.Error != "" {
					t.Fatal(out.Error)
				}
			case "revoke":
				if out := a.Admin(Request{Action: "revoke", Value: code.ID}); out.Error != "" {
					t.Fatal(out.Error)
				}
			case "reload":
				cfg.Directories = map[string]string{"cd": "cd"}
				writeReloadConfig(t, file, cfg)
				if _, err := a.Reload(file); err != nil {
					t.Fatal(err)
				}
			}
			finish()
			if w.Code == 303 || len(w.Result().Cookies()) != 0 || len(a.sessions) != 0 {
				t.Fatalf("delayed login authorized after %s: %d", change, w.Code)
			}
		})
	}
}

type pausedResponse struct {
	header           http.Header
	started, release chan struct{}
	once             sync.Once
}

func (w *pausedResponse) Header() http.Header { return w.header }
func (w *pausedResponse) WriteHeader(int)     {}
func (w *pausedResponse) Write(p []byte) (int, error) {
	w.once.Do(func() { close(w.started) })
	<-w.release
	return len(p), nil
}
func TestSlowLoginResponseDoesNotLockService(t *testing.T) {
	a := setup(t)
	w := &pausedResponse{header: http.Header{}, started: make(chan struct{}), release: make(chan struct{})}
	r := httptest.NewRequest("POST", "/login", strings.NewReader("code=wrong"))
	r.RemoteAddr = "192.0.2.1:1"
	r.Header.Set("Content-Type", "application/x-www-form-urlencoded")
	done := make(chan struct{})
	go func() { defer close(done); a.Handler().ServeHTTP(w, r) }()
	t.Cleanup(func() { close(w.release); <-done })
	select {
	case <-w.started:
	case <-time.After(time.Second):
		t.Fatal("response did not start")
	}
	adminDone := make(chan struct{})
	go func() { a.Admin(Request{Action: "status"}); close(adminDone) }()
	select {
	case <-adminDone:
	case <-time.After(time.Second):
		t.Fatal("slow response blocked administration")
	}
}
func TestIPv4Only(t *testing.T) {
	for _, value := range []string{"::1", "fe80::1%eth0", "::ffff:192.0.2.1"} {
		if _, err := netipAddr(value); err == nil {
			t.Fatalf("accepted non-IPv4 address %s", value)
		}
	}
	for _, value := range []string{"::1", "::/0", "fe80::1%eth0", "::ffff:192.0.2.1", "::ffff:192.0.2.0/120"} {
		if _, err := parsePrefix(value); err == nil {
			t.Fatalf("accepted non-IPv4 blacklist %s", value)
		}
	}
	if p, err := parsePrefix("192.0.2.129/24"); err != nil || p.String() != "192.0.2.0/24" {
		t.Fatalf("IPv4 prefix: %v %v", p, err)
	}
	a := setup(t)
	for _, remote := range []string{"[::1]:1", "[fe80::1%eth0]:1", "[::ffff:192.0.2.1]:1"} {
		r := httptest.NewRequest("GET", "/", nil)
		r.RemoteAddr = remote
		w := httptest.NewRecorder()
		a.Handler().ServeHTTP(w, r)
		if w.Code != 400 {
			t.Fatalf("accepted %s: %d", remote, w.Code)
		}
	}
	file, c := reloadConfig(t, a)
	for _, listen := range []string{"[::]:17342", "localhost:17342", ":17342", "127.0.0.1:0", "127.0.0.1:65536", "127.0.0.1:http"} {
		c.Listen = listen
		writeReloadConfig(t, file, c)
		if _, err := Load(file); err == nil {
			t.Fatalf("accepted listener %s", listen)
		}
	}
}
func TestConfigurationSymlinkProtection(t *testing.T) {
	a := setup(t)
	file, _ := reloadConfig(t, a)
	cwd, err := os.Getwd()
	if err != nil {
		t.Fatal(err)
	}
	relative, err := filepath.Rel(cwd, file)
	if err != nil {
		t.Fatal(err)
	}
	if _, err := Load(relative); err != nil {
		t.Fatalf("relative configuration path: %v", err)
	}
	data, err := os.ReadFile(file)
	if err != nil {
		t.Fatal(err)
	}
	actual := filepath.Join(a.cfg.Root, "ab", "published-config.json")
	if err = os.WriteFile(actual, data, 0600); err != nil {
		t.Fatal(err)
	}
	link := filepath.Join(filepath.Dir(file), "config-link.json")
	if err = os.Symlink(actual, link); err != nil {
		t.Fatal(err)
	}
	if _, err = Load(link); err == nil {
		t.Fatal("configuration in content accepted through symlink")
	}
	cookie := access(t, a)
	if _, err = a.Reload(link); err == nil {
		t.Fatal("reload accepted exposed config")
	}
	if w := request(a, "GET", "/file/hello.txt", "", cookie); w.Code != 200 {
		t.Fatal("failed reload changed existing session")
	}
	external := filepath.Join(filepath.Dir(file), "external-link.json")
	if err = os.Symlink(file, external); err != nil {
		t.Fatal(err)
	}
	if _, err = Load(external); err != nil {
		t.Fatalf("safe external symlink rejected: %v", err)
	}
}
func TestDirectoryMappingProtection(t *testing.T) {
	for _, name := range []string{"normalized root", "nested", "duplicate", "symlink nested", "symlink root", "outside slash"} {
		t.Run(name, func(t *testing.T) {
			a := setup(t)
			file, c := reloadConfig(t, a)
			cookie := access(t, a)
			unused := createCode(t, a, "ab")
			var linkTarget string
			switch name {
			case "normalized root":
				c.Directories = map[string]string{"ab": "ab/.."}
			case "nested":
				c.Directories = map[string]string{"ab": "ab", "private": "ab/sub"}
			case "duplicate":
				c.Directories = map[string]string{"ab": "ab", "other": "ab"}
			case "symlink nested":
				linkTarget = "ab/sub"
				c.Directories = map[string]string{"ab": "ab", "other": "linked"}
			case "symlink root":
				linkTarget = "."
				c.Directories = map[string]string{"ab": "linked"}
			case "outside slash":
				linkTarget = ".."
				c.Directories = map[string]string{"ab": "linked/"}
			}
			if linkTarget != "" {
				if err := os.Symlink(linkTarget, filepath.Join(c.Root, "linked")); err != nil {
					t.Fatal(err)
				}
			}
			writeReloadConfig(t, file, c)
			if next, err := Load(file); err == nil {
				if b, err := New(next); err == nil {
					b.Close()
					t.Fatal("unsafe startup mapping accepted")
				}
			}
			if _, err := a.Reload(file); err == nil {
				t.Fatal("unsafe reload mapping accepted")
			}
			if w := request(a, "GET", "/file/hello.txt", "", cookie); w.Code != 200 {
				t.Fatal("unsafe reload revoked old session")
			}
			if _, ok := a.state.Codes[unused.ID]; !ok {
				t.Fatal("unsafe reload revoked old code")
			}
		})
	}
	a := setup(t)
	cookie := access(t, a)
	if err := os.Symlink(filepath.Join(a.cfg.Root, "cd"), filepath.Join(a.cfg.Root, "ab", "outside")); err != nil {
		t.Fatal(err)
	}
	if w := request(a, "GET", "/browse/outside/", "", cookie); w.Code != 404 {
		t.Fatalf("root escaped via trailing slash: %d", w.Code)
	}
}
func TestFileRangeLimits(t *testing.T) {
	a := setup(t)
	cookie := access(t, a)
	for _, value := range []string{"bytes=0-0,2-2", "bytes=" + strings.Repeat("0-0,", 2000), strings.Repeat("x", 129)} {
		r := httptest.NewRequest("GET", "/file/hello.txt", nil)
		r.RemoteAddr = "192.0.2.1:1"
		r.AddCookie(cookie)
		r.Header.Set("Range", value)
		w := httptest.NewRecorder()
		a.Handler().ServeHTTP(w, r)
		if w.Code != 400 {
			t.Fatalf("accepted range: %d", w.Code)
		}
	}
	r := httptest.NewRequest("GET", "/file/hello.txt", nil)
	r.RemoteAddr = "192.0.2.1:1"
	r.AddCookie(cookie)
	r.Header.Add("Range", "bytes=0-0")
	r.Header.Add("Range", "bytes=2-2")
	w := httptest.NewRecorder()
	a.Handler().ServeHTTP(w, r)
	if w.Code != 400 {
		t.Fatal("multiple Range headers accepted")
	}
	r.Header.Del("Range")
	r.Header.Set("Range", "bytes=-5")
	w = httptest.NewRecorder()
	a.Handler().ServeHTTP(w, r)
	if w.Code != 206 || w.Body.String() != "world" {
		t.Fatalf("single suffix range broken: %d %s", w.Code, w.Body.String())
	}
}
func TestPerAliasCodeLimit(t *testing.T) {
	a := setup(t)
	var first string
	for range maxCodesPerAlias {
		code := createCode(t, a, "ab")
		first = code.ID
	}
	if out := a.Admin(Request{Action: "code", Value: "ab"}); out.Error == "" {
		t.Fatal("per-alias cap exceeded")
	}
	if out := a.Admin(Request{Action: "code", Value: "cd"}); out.Error != "" {
		t.Fatal("unrelated alias capped")
	}
	a.Admin(Request{Action: "revoke", Value: first})
	createCode(t, a, "ab")
}
func TestDirectoryEntryLimit(t *testing.T) {
	a := setup(t)
	// Use hidden files too: limiting raw entries must bound memory independently
	// of how many of them survive filtering for the listing.
	for i := range maxDirectoryEntries + 1 {
		if err := os.WriteFile(filepath.Join(a.cfg.Root, "ab", "sub", fmt.Sprintf(".entry-%05d", i)), nil, 0600); err != nil {
			t.Fatal(err)
		}
	}
	if w := request(a, "GET", "/browse/sub/", "", access(t, a)); w.Code != 503 {
		t.Fatalf("large directory accepted: %d", w.Code)
	}
}

func TestHTTPAndDownloadLimits(t *testing.T) {
	a := setup(t)
	cookie := access(t, a)
	for range maxDownloadsPerIP {
		if !a.downloads.acquire("192.0.2.1") {
			t.Fatal("unexpected limit")
		}
	}
	w := request(a, "GET", "/file/hello.txt", "", cookie)
	if w.Code != 429 {
		t.Fatalf("download limit bypassed: %d", w.Code)
	}
	if w = request(a, "GET", "/browse/", "", cookie); w.Code != 200 {
		t.Fatal("downloads blocked directory navigation")
	}
	for range maxDownloadsPerIP {
		a.downloads.release("192.0.2.1")
	}
	if w = request(a, "GET", "/file/hello.txt", "", cookie); w.Code != 200 {
		t.Fatal("download slot not reusable")
	}
	for range maxRequests {
		a.requests <- struct{}{}
	}
	w = request(a, "GET", "/", "", nil)
	if w.Code != 429 {
		t.Fatal("request cap bypassed")
	}
	for range maxRequests {
		<-a.requests
	}
	if w = request(a, "GET", "/", "", nil); w.Code != 200 {
		t.Fatal("request slots not reusable")
	}
}

// A writer that supports deadlines lets the test exercise chunking without a
// network socket; the transport test separately verifies real blocked writes.
type deadlineWriter struct {
	header    http.Header
	deadlines int
	largest   int
	data      bytes.Buffer
}

func (w *deadlineWriter) Header() http.Header              { return w.header }
func (w *deadlineWriter) WriteHeader(int)                  {}
func (w *deadlineWriter) SetWriteDeadline(time.Time) error { w.deadlines++; return nil }
func (w *deadlineWriter) Write(p []byte) (int, error) {
	w.largest = max(w.largest, len(p))
	return w.data.Write(p)
}
func TestWriteIdleDeadlineProgress(t *testing.T) {
	target := &deadlineWriter{header: http.Header{}}
	w := &idleWriter{ResponseWriter: target, controller: http.NewResponseController(target), timeout: time.Second}
	data := bytes.Repeat([]byte("x"), 100<<10)
	n, err := w.Write(data)
	if err != nil || n != len(data) || !bytes.Equal(target.data.Bytes(), data) || target.largest > 32<<10 || target.deadlines < 4 {
		t.Fatalf("write progress/deadlines: %d %v %d %d", n, err, target.largest, target.deadlines)
	}
}
