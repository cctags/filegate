package gate

import (
	"net/http"
	"net/http/httptest"
	"net/url"
	"os"
	"path/filepath"
	"strings"
	"sync"
	"testing"
	"time"

	"filegate/internal/logging"
)

func setup(t *testing.T) *App {
	t.Helper()
	// t.TempDir includes the test name, which can exceed the Unix socket
	// pathname limit under a longer checkout path such as GitHub Actions.
	base, err := os.MkdirTemp(os.Getenv("TMPDIR"), "gate-")
	if err != nil {
		t.Fatal(err)
	}
	t.Cleanup(func() {
		if err := os.RemoveAll(base); err != nil {
			t.Error(err)
		}
	})
	for _, dir := range []string{"content/ab/sub", "content/cd", "state"} {
		if e := os.MkdirAll(filepath.Join(base, dir), 0700); e != nil {
			t.Fatal(e)
		}
	}
	for name, data := range map[string]string{"content/ab/hello.txt": "hello world", "content/cd/secret.txt": "other directory", "content/ab/test.html": "<script>alert(1)</script>", "outside.txt": "outside"} {
		if e := os.WriteFile(filepath.Join(base, name), []byte(data), 0600); e != nil {
			t.Fatal(e)
		}
	}
	if e := os.Symlink(filepath.Join(base, "outside.txt"), filepath.Join(base, "content/ab/escape.txt")); e != nil {
		t.Fatal(e)
	}
	if e := os.WriteFile(filepath.Join(base, "blacklist.txt"), nil, 0600); e != nil {
		t.Fatal(e)
	}
	c := Config{Root: filepath.Join(base, "content"), StateDir: filepath.Join(base, "state"), Blacklist: filepath.Join(base, "blacklist.txt"), Directories: map[string]string{"ab": "ab", "cd": "cd"}, MaxFailures: 3, AttemptsPerMinute: 10, ttl: time.Minute, idle: time.Minute, max: time.Hour, window: time.Minute, ban: time.Minute}
	c.Logging = logging.Defaults()
	c.Logging.Directory = filepath.Join(base, "logs")
	a, e := New(c)
	if e != nil {
		t.Fatal(e)
	}
	t.Cleanup(a.Close)
	return a
}
func request(a *App, method, target, body string, cookie *http.Cookie) *httptest.ResponseRecorder {
	r := httptest.NewRequest(method, target, strings.NewReader(body))
	r.RemoteAddr = "192.0.2.1:1234"
	if method == "POST" {
		r.Header.Set("Content-Type", "application/x-www-form-urlencoded")
	}
	if cookie != nil {
		r.AddCookie(cookie)
	}
	w := httptest.NewRecorder()
	a.Handler().ServeHTTP(w, r)
	return w
}
func access(t *testing.T, a *App) *http.Cookie {
	t.Helper()
	out := a.Admin(Request{Action: "code", Value: "ab"})
	if out.Error != "" {
		t.Fatal(out.Error)
	}
	w := request(a, "POST", "/login", url.Values{"code": {out.Code}}.Encode(), nil)
	if w.Code != 303 {
		t.Fatalf("login: %d %s", w.Code, w.Body.String())
	}
	return w.Result().Cookies()[0]
}
func TestLoginIsolationAndFiles(t *testing.T) {
	a := setup(t)
	cookie := access(t, a)
	for _, tc := range []struct {
		path     string
		status   int
		contains string
	}{
		{"/browse/", 200, "hello.txt"}, {"/file/hello.txt", 200, "hello world"}, {"/file/secret.txt", 404, ""}, {"/file/escape.txt", 404, ""}, {"/file/%2e%2e/cd/secret.txt", 404, ""},
	} {
		w := request(a, "GET", tc.path, "", cookie)
		if w.Code != tc.status || !strings.Contains(w.Body.String(), tc.contains) {
			t.Errorf("%s: %d %s", tc.path, w.Code, w.Body.String())
		}
	}
	if strings.Contains(request(a, "GET", "/browse/", "", cookie).Body.String(), ">cd /") {
		t.Error("sibling directory disclosed")
	}
	w := request(a, "GET", "/file/test.html", "", cookie)
	if !strings.HasPrefix(w.Header().Get("Content-Disposition"), "inline") || w.Header().Get("Content-Type") != "text/plain; charset=utf-8" {
		t.Error("HTML must be displayed as plain source text")
	}
	r := httptest.NewRequest("GET", "/file/hello.txt", nil)
	r.RemoteAddr = "192.0.2.1:1"
	r.AddCookie(cookie)
	r.Header.Set("Range", "bytes=0-4")
	w = httptest.NewRecorder()
	a.Handler().ServeHTTP(w, r)
	if w.Code != 206 || w.Body.String() != "hello" {
		t.Fatalf("range: %d %s", w.Code, w.Body.String())
	}
	if w = request(a, "GET", "/file/hello.txt", "", nil); w.Code != 303 {
		t.Error("unauthenticated file exposed")
	}
}
func TestCodeSingleUseConcurrentAndRestart(t *testing.T) {
	a := setup(t)
	c := a.Admin(Request{Action: "code", Value: "ab"})
	var wg sync.WaitGroup
	statuses := make(chan int, 2)
	for range 2 {
		wg.Add(1)
		go func() { defer wg.Done(); statuses <- request(a, "POST", "/login", "code="+c.Code, nil).Code }()
	}
	wg.Wait()
	close(statuses)
	success := 0
	for s := range statuses {
		if s == 303 {
			success++
		}
	}
	if success != 1 {
		t.Fatalf("successful uses: %d", success)
	}
	b, e := New(a.cfg)
	if e != nil {
		t.Fatal(e)
	}
	defer b.Close()
	if w := request(b, "POST", "/login", "code="+c.Code, nil); w.Code == 303 {
		t.Fatal("consumed code restored after restart")
	}
	if len(b.sessions) != 0 {
		t.Error("sessions survived restart")
	}
}
func TestExpiryLogoutAndRevoke(t *testing.T) {
	a := setup(t)
	c := a.Admin(Request{Action: "code", Value: "ab", TTL: "1ns"})
	if request(a, "POST", "/login", "code="+c.Code, nil).Code == 303 {
		t.Error("expired code accepted")
	}
	c = a.Admin(Request{Action: "code", Value: "ab"})
	if out := a.Admin(Request{Action: "revoke", Value: c.ID}); out.Error != "" {
		t.Fatal(out.Error)
	}
	if request(a, "POST", "/login", "code="+c.Code, nil).Code == 303 {
		t.Error("revoked code accepted")
	}
	cookie := access(t, a)
	s := a.sessions[cookie.Value]
	if request(a, "POST", "/logout", "csrf=bad", cookie).Code != 403 {
		t.Fatal("CSRF accepted")
	}
	if request(a, "POST", "/logout", "csrf="+s.CSRF, cookie).Code != 303 {
		t.Fatal("logout failed")
	}
	if request(a, "GET", "/browse/", "", cookie).Code != 303 {
		t.Fatal("logout did not invalidate session")
	}
	cookie = access(t, a)
	s = a.sessions[cookie.Value]
	s.Seen = time.Now().Add(-2 * time.Minute)
	a.sessions[cookie.Value] = s
	if request(a, "GET", "/browse/", "", cookie).Code != 303 {
		t.Fatal("idle expiry failed")
	}
	cookie = access(t, a)
	s = a.sessions[cookie.Value]
	s.Created = time.Now().Add(-2 * time.Hour)
	a.sessions[cookie.Value] = s
	if request(a, "GET", "/browse/", "", cookie).Code != 303 {
		t.Fatal("absolute expiry failed")
	}
}
func TestBanAndBlacklist(t *testing.T) {
	a := setup(t)
	for range a.cfg.MaxFailures {
		request(a, "POST", "/login", "code=wrong", nil)
	}
	if request(a, "GET", "/", "", nil).Code != 403 {
		t.Fatal("not banned")
	}
	b, e := New(a.cfg)
	if e != nil {
		t.Fatal(e)
	}
	defer b.Close()
	if request(b, "GET", "/", "", nil).Code != 403 {
		t.Fatal("ban lost on restart")
	}
	if out := a.Admin(Request{Action: "unblock", Value: "192.0.2.1"}); out.Error != "" {
		t.Fatal(out.Error)
	}
	cookie := access(t, a)
	if out := a.Admin(Request{Action: "block", Value: "192.0.2.0/24"}); out.Error != "" {
		t.Fatal(out.Error)
	}
	if request(a, "GET", "/file/hello.txt", "", cookie).Code != 403 {
		t.Fatal("manual ban bypassed")
	}
	a.Admin(Request{Action: "unblock", Value: "192.0.2.0/24"})
	if request(a, "GET", "/browse/", "", cookie).Code != 303 {
		t.Fatal("banned session not revoked")
	}
	os.WriteFile(a.cfg.Blacklist, []byte("not-an-ip\n"), 0600)
	if request(a, "GET", "/", "", nil).Code != 503 {
		t.Fatal("invalid blacklist should fail closed")
	}
}
func TestRateLimitEvenWithValidCodes(t *testing.T) {
	a := setup(t)
	a.cfg.AttemptsPerMinute = 1
	access(t, a)
	c := a.Admin(Request{Action: "code", Value: "ab"})
	if request(a, "POST", "/login", "code="+c.Code, nil).Code != 403 {
		t.Fatal("request frequency bypassed by valid code")
	}
}
func TestPersistenceFailureAndCrossOrigin(t *testing.T) {
	a := setup(t)
	c := a.Admin(Request{Action: "code", Value: "ab"})
	dir := a.cfg.StateDir
	a.cfg.StateDir = filepath.Join(dir, "missing")
	if request(a, "POST", "/login", "code="+c.Code, nil).Code != 503 {
		t.Fatal("login succeeded without durable consumption")
	}
	a.cfg.StateDir = dir
	if request(a, "POST", "/login", "code="+c.Code, nil).Code != 303 {
		t.Fatal("code not restored after failed write")
	}
	r := httptest.NewRequest("POST", "http://example.com/login", strings.NewReader("code=wrong"))
	r.RemoteAddr = "192.0.2.1:1"
	r.Header.Set("Origin", "http://evil.example")
	w := httptest.NewRecorder()
	a.Handler().ServeHTTP(w, r)
	if w.Code != 403 {
		t.Fatal("cross-origin POST accepted")
	}
}
func TestAdminSocketAndLock(t *testing.T) {
	a := setup(t)
	lock, e := Lock(a.cfg)
	if e != nil {
		t.Fatal(e)
	}
	defer lock.Close()
	if second, e := Lock(a.cfg); e == nil {
		second.Close()
		t.Fatal("second server lock accepted")
	}
	l, e := a.AdminListener()
	if e != nil {
		t.Fatal(e)
	}
	serveAdminForTest(t, a, l)
	out, e := Call(a.cfg, Request{Action: "code", Value: "ab"})
	if e != nil || out.Code == "" {
		t.Fatalf("admin IPC: %v %+v", e, out)
	}
	issued := out
	out, e = Call(a.cfg, Request{Action: "status"})
	if e != nil {
		t.Fatal(e)
	}
	assertStatusRedacted(t, a, issued, out)
}
