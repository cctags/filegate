package gate

import (
	"context"
	"fmt"
	"io"
	"net"
	"net/http"
	"net/http/httptest"
	"net/netip"
	"os"
	"path/filepath"
	"strings"
	"syscall"
	"testing"
	"time"
)

func mustIP(t *testing.T, value string) netip.Addr {
	t.Helper()
	ip, err := netip.ParseAddr(value)
	if err != nil {
		t.Fatal(err)
	}
	return ip
}

func TestGlobalLoginLimitAndRecovery(t *testing.T) {
	a := setup(t)
	one, two := createCode(t, a, "ab"), createCode(t, a, "ab")
	a.globalSince, a.globalCount = time.Now(), 999
	if request(a, "POST", "/login", "code="+one.Code, nil).Code != 303 {
		t.Fatal("1000th attempt denied")
	}
	r := httptest.NewRequest("POST", "/login", strings.NewReader("code="+two.Code))
	r.Header.Set("Content-Type", "application/x-www-form-urlencoded")
	r.RemoteAddr = "198.51.100.1:1"
	w := httptest.NewRecorder()
	a.Handler().ServeHTTP(w, r)
	if w.Code != 429 || w.Header().Get("Retry-After") != "60" || len(a.state.Codes) != 1 || len(a.sessions) != 1 || len(a.state.Bans) != 0 {
		t.Fatal("global limit consumed code, banned IP or failed to reject")
	}
	a.globalSince = time.Now().Add(-time.Minute)
	if request(a, "POST", "/login", "code="+two.Code, nil).Code != 303 || a.globalCount != 1 {
		t.Fatal("global window failed to reset")
	}
}

func TestGlobalDownloadLimit(t *testing.T) {
	a := setup(t)
	cookie := access(t, a)
	var held []string
	defer func() {
		for _, ip := range held {
			a.downloads.release(ip)
		}
	}()
	for i := 0; i < maxDownloads; i++ {
		ip := fmt.Sprintf("198.51.100.%d", i/4+1)
		if !a.downloads.acquire(ip) {
			t.Fatal("global limit reached before 32 slots")
		}
		held = append(held, ip)
	}
	w := request(a, "GET", "/file/hello.txt", "", cookie)
	if w.Code != 429 || w.Header().Get("Retry-After") != "5" {
		t.Fatal("33rd cross-IP download accepted")
	}
	a.downloads.release(held[len(held)-1])
	held = held[:len(held)-1]
	if request(a, "GET", "/file/hello.txt", "", cookie).Code != 200 {
		t.Fatal("released global slot unavailable")
	}
}

type brokenResponse struct{ header http.Header }

func (w *brokenResponse) Header() http.Header       { return w.header }
func (w *brokenResponse) WriteHeader(int)           {}
func (w *brokenResponse) Write([]byte) (int, error) { return 0, context.Canceled }

func TestDownloadSlotsReleasedOnFailures(t *testing.T) {
	a := setup(t)
	cookie := access(t, a)
	for _, target := range []string{"/file/missing", "/file/escape.txt", "/file/sub", "/file/hello.txt"} {
		r := httptest.NewRequest("GET", target, nil)
		r.RemoteAddr = "192.0.2.1:1"
		r.AddCookie(cookie)
		a.Handler().ServeHTTP(&brokenResponse{header: http.Header{}}, r)
		if a.downloads.total != 0 || len(a.downloads.perIP) != 0 || len(a.requests) != 0 {
			t.Fatal("failed response leaked slots")
		}
	}
}

func TestAttemptWindowsAndUnblockCounters(t *testing.T) {
	a := setup(t)
	code := createCode(t, a, "ab")
	now := time.Now()
	a.attempts["192.0.2.1"] = attempts{Since: now, FailureSince: now, Count: 2, Failures: 2}
	if request(a, "POST", "/login", "code="+code.Code, nil).Code != 303 {
		t.Fatal("valid code rejected")
	}
	v := a.attempts["192.0.2.1"]
	if v.Count != 3 || v.Failures != 0 {
		t.Fatal("successful login reset rate count or kept failures")
	}
	v.Count = a.cfg.AttemptsPerMinute
	v.Failures = 2
	a.attempts["192.0.2.1"] = v
	a.state.Bans["192.0.2.1"] = now.Add(time.Minute)
	if out := a.Admin(Request{Action: "unblock", Value: "192.0.2.1"}); out.Error != "" {
		t.Fatal(out.Error)
	}
	if got := a.attempts["192.0.2.1"]; got.Count != v.Count || got.Failures != v.Failures {
		t.Fatal("unblock reset counters")
	}
	code = createCode(t, a, "ab")
	if request(a, "POST", "/login", "code="+code.Code, nil).Code != 403 || len(a.sessions) != 0 {
		t.Fatal("rate ban not reapplied or sessions survived")
	}
	v.Since = now.Add(-2 * time.Minute)
	v.FailureSince = now.Add(-2 * a.cfg.window)
	a.attempts["192.0.2.1"] = v
	a.state.Bans["192.0.2.1"] = now.Add(-time.Second)
	if request(a, "POST", "/login", "code="+code.Code, nil).Code != 303 {
		t.Fatal("expired ban/windows did not recover")
	}
	if got := a.attempts["192.0.2.1"]; got.Count != 1 || got.Failures != 0 {
		t.Fatal("windows failed to reset")
	}
}

func TestFailureThresholdBoundary(t *testing.T) {
	a := setup(t)
	for i := 1; i < a.cfg.MaxFailures; i++ {
		if w := request(a, "POST", "/login", "code=wrong", nil); w.Code != 401 {
			t.Fatal("failure ban applied early")
		}
	}
	if w := request(a, "POST", "/login", "code=wrong", nil); w.Code != 403 {
		t.Fatal("failure threshold did not ban")
	}
}

func TestGlobalStateCaps(t *testing.T) {
	for _, kind := range []string{"codes", "sessions"} {
		t.Run(kind, func(t *testing.T) {
			a := setup(t)
			issued := createCode(t, a, "ab")
			now := time.Now()
			limit := 10000
			if kind == "codes" {
				limit--
			} // The issued code already occupies one slot.
			for i := 0; i < limit; i++ {
				id := fmt.Sprint(i)
				if kind == "codes" {
					a.state.Codes[id] = Code{Alias: "cd", Expires: now.Add(time.Hour)}
				} else {
					a.sessions[id] = Session{Alias: "ab", Created: now, Seen: now}
				}
			}
			if kind == "codes" {
				if out := a.Admin(Request{Action: "code", Value: "ab"}); out.Error == "" {
					t.Fatal("global code cap bypassed")
				}
			} else if w := request(a, "POST", "/login", "code="+issued.Code, nil); w.Code != 503 || len(a.state.Codes) != 1 || len(w.Result().Cookies()) != 0 {
				t.Fatal("session cap consumed code or authorized")
			}
		})
	}
}

func TestSessionCookieAndLifetime(t *testing.T) {
	for _, scheme := range []string{"http", "https"} {
		t.Run(scheme, func(t *testing.T) {
			a := setup(t)
			issue := createCode(t, a, "ab")
			post := func(path, body string, cookie *http.Cookie) *httptest.ResponseRecorder {
				r := httptest.NewRequest("POST", scheme+"://example.com"+path, strings.NewReader(body))
				r.RemoteAddr = "192.0.2.1:1"
				r.Header.Set("Content-Type", "application/x-www-form-urlencoded")
				r.Header.Set("Origin", scheme+"://example.com")
				if cookie != nil {
					r.AddCookie(cookie)
				}
				w := httptest.NewRecorder()
				a.Handler().ServeHTTP(w, r)
				return w
			}
			w := post("/login", "code="+issue.Code, nil)
			cookie := responseCookie(t, w, cookieName)
			if w.Code != 303 || !cookie.HttpOnly || cookie.SameSite != http.SameSiteStrictMode || cookie.Path != "/" || cookie.Secure != (scheme == "https") || cookie.MaxAge != 0 {
				t.Fatal("unsafe session cookie")
			}
			s := a.sessions[cookie.Value]
			oldSeen := time.Now().Add(-time.Second)
			s.Seen = oldSeen
			a.sessions[cookie.Value] = s
			// Expiry of the already consumed code must not shorten the session.
			a.state.Codes[issue.ID] = Code{Alias: "ab", Expires: time.Now().Add(-time.Second)}
			if request(a, "GET", "/browse/", "", cookie).Code != 200 || !a.sessions[cookie.Value].Seen.After(oldSeen) {
				t.Fatal("session access did not refresh idle time")
			}
			if failed := post("/login", "code=wrong", cookie); failed.Code != 401 || len(a.sessions) != 1 {
				t.Fatal("failed relogin discarded existing session")
			}
			w = post("/logout", "csrf="+s.CSRF, cookie)
			deleted := responseCookie(t, w, cookieName)
			if w.Code != 303 || deleted.MaxAge != -1 || deleted.Value != "" || deleted.Secure != cookie.Secure || !deleted.HttpOnly || deleted.SameSite != cookie.SameSite || deleted.Path != "/" {
				t.Fatal("logout failed to clear secure cookie")
			}
		})
	}
}

func TestMalformedLoginRequests(t *testing.T) {
	for _, tc := range []struct{ name, query, body, contentType string }{
		{"oversized", "", "code=CODE&extra=" + strings.Repeat("x", 1024), "application/x-www-form-urlencoded"},
		{"bad escape", "", "code=CODE&extra=%zz", "application/x-www-form-urlencoded"},
		{"query only", "?code=CODE", "", "application/x-www-form-urlencoded"},
		{"JSON", "", `{"code":"CODE"}`, "application/json"},
	} {
		t.Run(tc.name, func(t *testing.T) {
			a := setup(t)
			c := createCode(t, a, "ab")
			r := httptest.NewRequest("POST", "/login"+strings.ReplaceAll(tc.query, "CODE", c.Code), strings.NewReader(strings.ReplaceAll(tc.body, "CODE", c.Code)))
			r.RemoteAddr = "192.0.2.1:1"
			r.Header.Set("Content-Type", tc.contentType)
			w := httptest.NewRecorder()
			a.Handler().ServeHTTP(w, r)
			if w.Code != 401 || len(a.state.Codes) != 1 || len(a.sessions) != 0 || a.attempts["192.0.2.1"].Failures != 1 {
				t.Fatal("malformed request authorized or changed accounting")
			}
		})
	}
	a := setup(t)
	for _, method := range []string{"GET", "PUT", "DELETE"} {
		if w := request(a, method, "/login", "code=bad", nil); w.Code != 404 && w.Code != 405 {
			t.Fatal("wrong method accepted")
		}
	}
}

type interruptedForm struct{ data []byte }

func (r *interruptedForm) Read(p []byte) (int, error) {
	n := copy(p, r.data)
	r.data = r.data[n:]
	return n, io.ErrUnexpectedEOF
}

func TestInterruptedAndRepeatedLoginFields(t *testing.T) {
	a := setup(t)
	issued := createCode(t, a, "ab")
	r := httptest.NewRequest("POST", "/login", nil)
	r.RemoteAddr = "192.0.2.1:1"
	r.Header.Set("Content-Type", "application/x-www-form-urlencoded")
	r.Body = io.NopCloser(&interruptedForm{data: []byte("code=" + issued.Code)})
	w := httptest.NewRecorder()
	a.Handler().ServeHTTP(w, r)
	if w.Code != 401 || len(a.state.Codes) != 1 || len(a.sessions) != 0 {
		t.Fatal("incomplete form authorized")
	}
	// Preserve net/url's established first-value semantics, not query values.
	if w := request(a, "POST", "/login", "code=wrong&code="+issued.Code, nil); w.Code != 401 {
		t.Fatal("later field overrode first value")
	}
	if w := request(a, "POST", "/login", "code="+issued.Code+"&code=wrong", nil); w.Code != 303 {
		t.Fatal("first field no longer used")
	}
}

func TestHiddenAndSpecialFiles(t *testing.T) {
	a := setup(t)
	cookie := access(t, a)
	dir := filepath.Join(a.cfg.Root, "ab")
	// Bind at a short path, then move the node into content (sockaddr_un is short).
	socketDir, err := os.MkdirTemp(os.Getenv("TMPDIR"), "node-")
	if err != nil {
		t.Fatal(err)
	}
	t.Cleanup(func() { os.RemoveAll(socketDir) })
	l, err := net.Listen("unix", filepath.Join(socketDir, "s"))
	if err != nil {
		t.Fatal(err)
	}
	t.Cleanup(func() { l.Close() })
	if err := os.Rename(filepath.Join(socketDir, "s"), filepath.Join(dir, "socket")); err != nil {
		t.Fatal(err)
	}
	if err := os.Mkdir(filepath.Join(dir, ".hidden"), 0700); err != nil {
		t.Fatal(err)
	}
	if err := os.WriteFile(filepath.Join(dir, ".hidden", "secret"), []byte("secret"), 0600); err != nil {
		t.Fatal(err)
	}
	if err := syscall.Mkfifo(filepath.Join(dir, "fifo"), 0600); err != nil {
		t.Fatal(err)
	}
	for _, target := range []string{"/file/.hidden/secret", "/browse/.hidden/", "/file/fifo", "/file/socket", "/file/sub", "/file/%252e%252e/cd/secret.txt", "/file/%5c..%5ccd%5csecret.txt"} {
		done := make(chan *httptest.ResponseRecorder, 1)
		go func() { done <- request(a, "GET", target, "", cookie) }()
		select {
		case w := <-done:
			if w.Code != 404 {
				t.Fatalf("%s: %d", target, w.Code)
			}
		case <-time.After(time.Second):
			t.Fatal("special file request blocked")
		}
	}
	listing := request(a, "GET", "/browse/", "", cookie).Body.String()
	for _, name := range []string{">.hidden", ">fifo", ">socket", ">escape.txt"} {
		if strings.Contains(listing, name) {
			t.Fatal("special file exposed in listing")
		}
	}
}

func TestFileConditionalRequestsAndAuthorization(t *testing.T) {
	a := setup(t)
	cookie := access(t, a)
	initial := request(a, "GET", "/file/hello.txt", "", cookie)
	for _, tc := range []struct {
		header, value string
		status        int
	}{{"Range", "bytes=99-100", 416}, {"If-Modified-Since", initial.Header().Get("Last-Modified"), 304}, {"If-Range", "Thu, 01 Jan 1970 00:00:00 GMT", 200}} {
		r := httptest.NewRequest("GET", "/file/hello.txt", nil)
		r.RemoteAddr = "192.0.2.1:1"
		r.AddCookie(cookie)
		r.Header.Set(tc.header, tc.value)
		if tc.header == "If-Range" {
			r.Header.Set("Range", "bytes=0-1")
		}
		w := httptest.NewRecorder()
		a.Handler().ServeHTTP(w, r)
		if w.Code != tc.status || w.Header().Get("X-Content-Type-Options") != "nosniff" {
			t.Fatalf("conditional %s: %d", tc.header, w.Code)
		}
	}
	if request(a, "HEAD", "/file/hello.txt", "", nil).Code != 303 {
		t.Fatal("HEAD bypassed authentication")
	}
	a.Admin(Request{Action: "block", Value: "192.0.2.1"})
	if request(a, "HEAD", "/file/hello.txt", "", cookie).Code != 403 {
		t.Fatal("HEAD bypassed blacklist")
	}
}

func TestInvalidStateAndProtectedPaths(t *testing.T) {
	for _, data := range []string{"{", `null`, `{"secret":"bad","codes":{},"bans":{}}`, `{"secret":"` + strings.Repeat("01", 32) + `","codes":null,"bans":{}}`} {
		t.Run(data[:min(12, len(data))], func(t *testing.T) {
			a := setup(t)
			a.Close()
			path := filepath.Join(a.cfg.StateDir, "state.json")
			if err := os.WriteFile(path, []byte(data), 0600); err != nil {
				t.Fatal(err)
			}
			if b, err := New(a.cfg); err == nil {
				b.Close()
				t.Fatal("invalid state accepted")
			}
			actual, err := os.ReadFile(path)
			if err != nil || string(actual) != data {
				t.Fatal("invalid state silently reset")
			}
		})
	}
	for _, kind := range []string{"state", "blacklist", "key"} {
		for _, symlink := range []bool{false, true} {
			t.Run(fmt.Sprintf("%s/link=%v", kind, symlink), func(t *testing.T) {
				a := setup(t)
				c := a.cfg
				target := filepath.Join(c.Root, "ab", "private")
				if kind == "state" {
					if err := os.Mkdir(target, 0700); err != nil {
						t.Fatal(err)
					}
				} else {
					if err := os.WriteFile(target, nil, 0600); err != nil {
						t.Fatal(err)
					}
				}
				value := target
				if symlink {
					value = filepath.Join(filepath.Dir(c.Root), "private-link")
					if err := os.Symlink(target, value); err != nil {
						t.Fatal(err)
					}
				}
				switch kind {
				case "state":
					c.StateDir = value
				case "blacklist":
					c.Blacklist = value
				case "key":
					c.TLSKey = value
				}
				if b, err := New(c); err == nil {
					b.Close()
					t.Fatal("protected path inside content accepted")
				}
			})
		}
	}
}

func FuzzAccessAlias(f *testing.F) {
	for _, seed := range []string{"ab01234567", "abcdefghijklmnop12345678", "AB12345678", "文件夹12345678", "ab1234567"} {
		f.Add(seed)
	}
	f.Fuzz(func(t *testing.T, code string) {
		alias := accessAlias(code)
		if alias == "" {
			return
		}
		if len(alias) > 16 || len(alias) < 1 || len(code) != len(alias)+8 || alias != code[:len(alias)] {
			t.Fatal("invalid alias shape")
		}
		for _, c := range alias {
			if c < 'a' || c > 'z' {
				t.Fatal("non-lowercase alias")
			}
		}
		for _, c := range code[len(alias):] {
			if c < '0' || c > '9' {
				t.Fatal("non-digit suffix")
			}
		}
	})
}

func FuzzRelativePath(f *testing.F) {
	for _, seed := range []string{"/file/a", "/file/../secret", "/file/a%2fb", "/file/文件夹"} {
		f.Add(seed)
	}
	f.Fuzz(func(t *testing.T, value string) {
		name, ok := relative(value, "/file/")
		if !ok {
			return
		}
		if strings.ContainsAny(name, "\\\x00") || strings.HasPrefix(name, "/") {
			t.Fatal("unsafe relative path")
		}
		for _, part := range strings.Split(name, "/") {
			if part == ".." {
				t.Fatal("parent accepted")
			}
		}
	})
}
