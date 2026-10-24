package gate

import (
	"bytes"
	"log/slog"
	"net/http"
	"net/http/httptest"
	"net/url"
	"path/filepath"
	"regexp"
	"strings"
	"sync"
	"testing"
	"time"
)

func linkRequest(a *App, method, target, body string, cookies ...*http.Cookie) *httptest.ResponseRecorder {
	r := httptest.NewRequest(method, target, strings.NewReader(body))
	r.RemoteAddr = "192.0.2.1:1234"
	if method == "POST" {
		r.Header.Set("Content-Type", "application/x-www-form-urlencoded")
		r.Header.Set("Origin", "http://example.com")
	}
	for _, cookie := range cookies {
		if cookie != nil {
			r.AddCookie(cookie)
		}
	}
	w := httptest.NewRecorder()
	a.Handler().ServeHTTP(w, r)
	return w
}
func responseCookie(t *testing.T, w *httptest.ResponseRecorder, name string) *http.Cookie {
	t.Helper()
	for _, cookie := range w.Result().Cookies() {
		if cookie.Name == name {
			return cookie
		}
	}
	t.Fatalf("missing cookie %s (status %d)", name, w.Code)
	return nil
}
func prepareLink(t *testing.T, a *App, code string, cookies ...*http.Cookie) (*http.Cookie, string) {
	t.Helper()
	w := linkRequest(a, "GET", "/access?"+url.Values{"code": {code}}.Encode(), "", cookies...)
	if w.Code != 303 || w.Header().Get("Location") != "/access" {
		t.Fatalf("prepare: %d %s", w.Code, w.Body.String())
	}
	cookie := responseCookie(t, w, pendingCookieName)
	w = linkRequest(a, "GET", "/access", "", cookie)
	if w.Code != 200 {
		t.Fatalf("confirmation: %d %s", w.Code, w.Body.String())
	}
	match := regexp.MustCompile(`name="csrf" value="([^"]+)"`).FindStringSubmatch(w.Body.String())
	if len(match) != 2 {
		t.Fatal("missing confirmation CSRF")
	}
	if strings.Contains(w.Body.String(), code) || strings.Contains(w.Body.String(), a.digest(code)) {
		t.Fatal("confirmation page contains code or digest")
	}
	return cookie, match[1]
}
func TestAccessLinkRequiresConfirmation(t *testing.T) {
	a := setup(t)
	created := createCode(t, a, "ab")
	head := linkRequest(a, "HEAD", "/access?code="+created.Code, "")
	if head.Code != 200 || head.Body.Len() != 0 || len(head.Result().Cookies()) != 0 || len(a.pending) != 0 {
		t.Fatal("HEAD prepared a login")
	}
	cookie, csrf := prepareLink(t, a, created.Code)
	if cookie.Path != "/access" || cookie.MaxAge != 300 || !cookie.HttpOnly || cookie.SameSite != http.SameSiteLaxMode || cookie.Secure {
		t.Fatalf("pending cookie: %+v", cookie)
	}
	if len(a.sessions) != 0 || len(a.attempts) != 0 || len(a.state.Codes) != 1 {
		t.Fatal("GET consumed code or counted a login")
	}
	if w := linkRequest(a, "GET", "/browse/", "", cookie); w.Code != 303 {
		t.Fatal("pending cookie granted access")
	}
	w := linkRequest(a, "GET", "/access?lang=en", "", cookie)
	if w.Code != 200 || !strings.Contains(w.Body.String(), "Confirm and enter") || w.Header().Get("Referrer-Policy") != "origin" || !strings.Contains(w.Body.String(), `href="/access?lang=zh-CN"`) {
		t.Fatal("confirmation translation or safe links missing")
	}
	w = linkRequest(a, "POST", "/access", url.Values{"csrf": {csrf}}.Encode(), cookie)
	if w.Code != 303 || w.Header().Get("Location") != "/browse/" || len(a.state.Codes) != 0 || len(a.pending) != 0 {
		t.Fatalf("confirm: %d %s", w.Code, w.Body.String())
	}
	session := responseCookie(t, w, cookieName)
	if responseCookie(t, w, pendingCookieName).MaxAge != -1 {
		t.Fatal("pending cookie not deleted")
	}
	if request(a, "GET", "/file/hello.txt", "", session).Code != 200 || request(a, "GET", "/file/secret.txt", "", session).Code != 404 {
		t.Fatal("linked login lost directory isolation")
	}
	if w := linkRequest(a, "POST", "/access", "csrf="+csrf, cookie); w.Code != 403 {
		t.Fatal("confirmation replay accepted")
	}
}
func TestAccessLinkCookieAndOrigin(t *testing.T) {
	a := setup(t)
	code := createCode(t, a, "ab")
	cookie, csrf := prepareLink(t, a, code.Code)
	for _, kind := range []string{"missing cookie", "wrong CSRF", "cross origin", "opaque origin", "other IP"} {
		t.Run(kind, func(t *testing.T) {
			r := httptest.NewRequest("POST", "/access", strings.NewReader("csrf="+csrf))
			r.RemoteAddr = "192.0.2.1:1234"
			r.Header.Set("Content-Type", "application/x-www-form-urlencoded")
			r.Header.Set("Origin", "http://example.com")
			if kind != "missing cookie" {
				r.AddCookie(cookie)
			}
			switch kind {
			case "wrong CSRF":
				r.Body = http.NoBody
			case "cross origin":
				r.Header.Set("Origin", "http://evil.example")
			case "opaque origin":
				r.Header.Set("Origin", "null")
			case "other IP":
				r.RemoteAddr = "192.0.2.2:1234"
			}
			w := httptest.NewRecorder()
			a.Handler().ServeHTTP(w, r)
			if w.Code != 403 || len(a.sessions) != 0 || len(a.state.Codes) != 1 || len(a.pending) != 1 || len(a.attempts) != 0 {
				t.Fatalf("invalid confirmation changed authentication: %d", w.Code)
			}
		})
	}
	r := httptest.NewRequest("GET", "https://example.com/access?code="+code.Code, nil)
	r.RemoteAddr = "192.0.2.1:1234"
	w := httptest.NewRecorder()
	a.Handler().ServeHTTP(w, r)
	if !responseCookie(t, w, pendingCookieName).Secure || w.Header().Get("Referrer-Policy") != "origin" {
		t.Fatal("TLS pending cookie or referrer policy missing")
	}
}
func TestAccessLinkRechecksCurrentState(t *testing.T) {
	for _, change := range []string{"expired code", "revoked code", "blocked IP", "changed mapping", "expired preparation", "persistence failure"} {
		t.Run(change, func(t *testing.T) {
			a := setup(t)
			file, cfg := reloadConfig(t, a)
			code := createCode(t, a, "ab")
			cookie, csrf := prepareLink(t, a, code.Code)
			expected := 401
			switch change {
			case "expired code":
				c := a.state.Codes[code.ID]
				c.Expires = time.Now().Add(-time.Second)
				a.state.Codes[code.ID] = c
			case "revoked code":
				a.Admin(Request{Action: "revoke", Value: code.ID})
			case "blocked IP":
				a.Admin(Request{Action: "block", Value: "192.0.2.1"})
				expected = 403
			case "changed mapping":
				cfg.Directories = map[string]string{"cd": "cd"}
				writeReloadConfig(t, file, cfg)
				if _, err := a.Reload(file); err != nil {
					t.Fatal(err)
				}
			case "expired preparation":
				p := a.pending[cookie.Value]
				p.Expires = time.Now().Add(-time.Second)
				a.pending[cookie.Value] = p
				expected = 403
			case "persistence failure":
				a.cfg.StateDir = filepath.Join(a.cfg.StateDir, "missing")
				expected = 503
			}
			w := linkRequest(a, "POST", "/access", "csrf="+csrf, cookie)
			if w.Code != expected || len(a.sessions) != 0 {
				t.Fatalf("%s: %d %s", change, w.Code, w.Body.String())
			}
			for _, c := range w.Result().Cookies() {
				if c.Name == cookieName {
					t.Fatal("failed login issued a session")
				}
			}
			if change == "expired preparation" || change == "blocked IP" || change == "persistence failure" {
				if _, ok := a.state.Codes[code.ID]; !ok {
					t.Fatal("code lost without successful consumption")
				}
			}
		})
	}
}
func TestAccessLinkDoesNotSilentlyReplaceSession(t *testing.T) {
	a := setup(t)
	oldSession := access(t, a)
	target := createCode(t, a, "cd")
	cookie, csrf := prepareLink(t, a, target.Code, oldSession)
	if _, ok := a.sessions[oldSession.Value]; !ok || len(a.sessions) != 1 {
		t.Fatal("opening link replaced login")
	}
	w := linkRequest(a, "GET", "/access?lang=en", "", cookie, oldSession)
	for _, text := range []string{"Current directory alias: ab", "Target directory alias: cd", "replaces any existing login"} {
		if !strings.Contains(w.Body.String(), text) {
			t.Fatalf("missing warning %q: %s", text, w.Body.String())
		}
	}
	w = linkRequest(a, "POST", "/access", "csrf="+csrf, cookie, oldSession)
	if w.Code != 303 {
		t.Fatal("confirmed switch failed")
	}
	if _, ok := a.sessions[oldSession.Value]; ok {
		t.Fatal("previous session not revoked")
	}
	session := responseCookie(t, w, cookieName)
	if request(a, "GET", "/file/secret.txt", "", session).Code != 200 || request(a, "GET", "/file/hello.txt", "", session).Code != 404 {
		t.Fatal("wrong directory after confirmation")
	}
}
func TestAccessLinkNewTabCannotConsumeOtherCode(t *testing.T) {
	a := setup(t)
	first := createCode(t, a, "ab")
	second := createCode(t, a, "cd")
	old, oldCSRF := prepareLink(t, a, first.Code)
	latest, csrf := prepareLink(t, a, second.Code, old)
	if w := linkRequest(a, "POST", "/access", "csrf="+oldCSRF, latest); w.Code != 403 {
		t.Fatal("stale tab consumed newest link")
	}
	if w := linkRequest(a, "POST", "/access", "csrf="+oldCSRF, old); w.Code != 403 {
		t.Fatal("replaced preparation accepted")
	}
	w := linkRequest(a, "POST", "/access", url.Values{"csrf": {csrf}, "code": {first.Code}}.Encode(), latest)
	if w.Code != 303 {
		t.Fatal("newest link failed")
	}
	if _, ok := a.state.Codes[first.ID]; !ok {
		t.Fatal("POST code parameter overrode prepared digest")
	}
	if _, ok := a.state.Codes[second.ID]; ok {
		t.Fatal("prepared code not consumed")
	}
}
func TestAccessLinkConcurrentConsumption(t *testing.T) {
	for _, sameBrowser := range []bool{true, false} {
		t.Run(map[bool]string{true: "same preparation", false: "two browsers"}[sameBrowser], func(t *testing.T) {
			a := setup(t)
			code := createCode(t, a, "ab")
			one, csrfOne := prepareLink(t, a, code.Code)
			two, csrfTwo := one, csrfOne
			if !sameBrowser {
				two, csrfTwo = prepareLink(t, a, code.Code)
			}
			responses := make(chan int, 2)
			var wg sync.WaitGroup
			for _, p := range []struct {
				cookie *http.Cookie
				csrf   string
			}{{one, csrfOne}, {two, csrfTwo}} {
				wg.Add(1)
				go func() { defer wg.Done(); responses <- linkRequest(a, "POST", "/access", "csrf="+p.csrf, p.cookie).Code }()
			}
			wg.Wait()
			close(responses)
			success := 0
			for status := range responses {
				if status == 303 {
					success++
				}
			}
			if success != 1 || len(a.sessions) != 1 || len(a.state.Codes) != 0 {
				t.Fatalf("single-use violated: successes=%d", success)
			}
		})
	}
}
func TestAccessLinkInvalidAndBoundedPreparation(t *testing.T) {
	a := setup(t)
	for _, query := range []string{"code=bad", "code=", "code=ab12345678&code=cd12345678", "code=%zz", "code=ab12345678;extra=x"} {
		if w := linkRequest(a, "GET", "/access?"+query, ""); w.Code != 400 {
			t.Fatalf("accepted malformed link %s: %d", query, w.Code)
		}
	}
	if len(a.pending) != 0 || len(a.attempts) != 0 {
		t.Fatal("malformed links created state")
	}
	// Syntactically valid, nonexistent codes must not become a GET validity oracle.
	cookie, csrf := prepareLink(t, a, "unknown12345678")
	if len(a.attempts) != 0 {
		t.Fatal("GET counted an invalid login")
	}
	if w := linkRequest(a, "POST", "/access", "csrf="+csrf, cookie); w.Code != 401 || a.attempts["192.0.2.1"].Failures != 1 {
		t.Fatal("confirmation did not use normal failure accounting")
	}
	for range maxPendingPerIP {
		w := linkRequest(a, "GET", "/access?code=ab12345678", "")
		if w.Code != 303 {
			t.Fatal(w.Code)
		}
	}
	if w := linkRequest(a, "GET", "/access?code=ab12345678", ""); w.Code != 429 {
		t.Fatal("pending IP cap bypassed")
	}
	// Preparation saturation must not disable the original input-based login.
	access(t, a)
	clear(a.pending)
	for i := range maxPendingLogins {
		a.pending[time.Unix(int64(i), 0).String()] = pendingLogin{IP: "198.51.100.1", Expires: time.Now().Add(time.Minute)}
	}
	if w := linkRequest(a, "GET", "/access?code=ab12345678", ""); w.Code != 429 {
		t.Fatal("global pending cap bypassed")
	}
	for id, p := range a.pending {
		p.Expires = time.Now().Add(-time.Second)
		a.pending[id] = p
	}
	if w := linkRequest(a, "GET", "/access?code=ab12345678", ""); w.Code != 303 {
		t.Fatal("expired preparations were not reclaimed")
	}
}
func TestAccessLinkRestartAndSlowPost(t *testing.T) {
	a := setup(t)
	created := createCode(t, a, "ab")
	cookie, csrf := prepareLink(t, a, created.Code)
	b, err := New(a.cfg)
	if err != nil {
		t.Fatal(err)
	}
	defer b.Close()
	if w := linkRequest(b, "POST", "/access", "csrf="+csrf, cookie); w.Code != 403 || len(b.state.Codes) != 1 {
		t.Fatal("preparation survived restart or code lost")
	}
	body := &pausedBody{started: make(chan struct{}), release: make(chan struct{}), reader: strings.NewReader("csrf=" + csrf)}
	r := httptest.NewRequest("POST", "/access", nil)
	r.Body = body
	r.RemoteAddr = "192.0.2.1:1234"
	r.Header.Set("Content-Type", "application/x-www-form-urlencoded")
	r.AddCookie(cookie)
	w := httptest.NewRecorder()
	done := make(chan struct{})
	go func() { defer close(done); a.Handler().ServeHTTP(w, r) }()
	t.Cleanup(func() { close(body.release); <-done })
	select {
	case <-body.started:
	case <-time.After(time.Second):
		t.Fatal("confirmation body not reached")
	}
	adminDone := make(chan struct{})
	go func() { a.Admin(Request{Action: "revoke", Value: created.ID}); close(adminDone) }()
	select {
	case <-adminDone:
	case <-time.After(time.Second):
		t.Fatal("slow confirmation blocked administration")
	}
}
func TestAccessLinkCredentialRedaction(t *testing.T) {
	var log bytes.Buffer
	old := slog.Default()
	slog.SetDefault(slog.New(slog.NewTextHandler(&log, nil)))
	defer slog.SetDefault(old)
	a := setup(t)
	code := createCode(t, a, "ab")
	cookie, csrf := prepareLink(t, a, code.Code)
	w := linkRequest(a, "POST", "/access", "csrf="+csrf, cookie)
	session := responseCookie(t, w, cookieName)
	for _, secret := range []string{code.Code, a.digest(code.Code), cookie.Value, csrf, session.Value, a.state.Secret} {
		if strings.Contains(log.String(), secret) {
			t.Fatal("link credential logged")
		}
	}
	if !strings.Contains(log.String(), "path=/access") {
		t.Fatal("link request not audited")
	}
}
