package gate

import (
	"net/http"
	"net/http/httptest"
	"net/url"
	"strings"
	"testing"
)

func TestLoginReferrerPolicy(t *testing.T) {
	a := setup(t)
	response := request(a, http.MethodGet, "/", "", nil)
	// Fetch's Origin-header algorithm turns a normal form POST's origin into
	// null under no-referrer. same-origin preserves it for our own forms while
	// withholding Referer from other origins.
	if got := response.Header().Get("Referrer-Policy"); got != "same-origin" {
		t.Fatalf("Referrer-Policy = %q; want same-origin to preserve form POST origins", got)
	}
}

func TestOriginValidation(t *testing.T) {
	cases := []struct {
		name, target, origin, fetchSite string
		want                            int
	}{
		{"HTTP same origin", "http://192.0.2.10:8080/login", "http://192.0.2.10:8080", "", http.StatusSeeOther},
		{"HTTPS same origin", "https://files.example/login", "https://files.example", "same-origin", http.StatusSeeOther},
		{"opaque origin from old policy", "http://192.0.2.10:8080/login", "null", "", http.StatusForbidden},
		{"opaque origin with same-origin metadata", "http://192.0.2.10:8080/login", "null", "same-origin", http.StatusForbidden},
		{"different host", "http://192.0.2.10:8080/login", "http://evil.example:8080", "", http.StatusForbidden},
		{"different port", "http://192.0.2.10:8080/login", "http://192.0.2.10:8081", "", http.StatusForbidden},
		{"different scheme", "http://192.0.2.10:8080/login", "https://192.0.2.10:8080", "", http.StatusForbidden},
		{"cross-site without Origin", "http://192.0.2.10:8080/login", "", "cross-site", http.StatusForbidden},
	}
	for _, tc := range cases {
		t.Run(tc.name, func(t *testing.T) {
			a := setup(t)
			code := a.Admin(Request{Action: "code", Value: "ab"})
			if code.Error != "" {
				t.Fatal(code.Error)
			}
			r := httptest.NewRequest(http.MethodPost, tc.target, strings.NewReader(url.Values{"code": {code.Code}}.Encode()))
			r.RemoteAddr = "192.0.2.1:1234"
			r.Header.Set("Content-Type", "application/x-www-form-urlencoded")
			if tc.origin != "" {
				r.Header.Set("Origin", tc.origin)
			}
			if tc.fetchSite != "" {
				r.Header.Set("Sec-Fetch-Site", tc.fetchSite)
			}
			w := httptest.NewRecorder()
			a.Handler().ServeHTTP(w, r)
			if w.Code != tc.want {
				t.Fatalf("status=%d, want=%d: %s", w.Code, tc.want, w.Body.String())
			}
			if tc.want == http.StatusForbidden {
				if !strings.Contains(w.Body.String(), "无效请求来源") {
					t.Fatalf("unexpected error: %s", w.Body.String())
				}
				if _, ok := a.state.Codes[code.ID]; !ok {
					t.Fatal("origin rejection consumed the access code")
				}
				if len(a.attempts) != 0 || len(a.state.Bans) != 0 {
					t.Fatal("origin rejection counted as an invalid access code")
				}
			}
		})
	}
}

func TestSameOriginBrowserFlow(t *testing.T) {
	a := setup(t)
	code := a.Admin(Request{Action: "code", Value: "ab"})
	post := func(target, body string, cookie *http.Cookie) *httptest.ResponseRecorder {
		r := httptest.NewRequest(http.MethodPost, "http://192.0.2.10:8080"+target, strings.NewReader(body))
		r.RemoteAddr = "192.0.2.1:1234"
		r.Header.Set("Content-Type", "application/x-www-form-urlencoded")
		r.Header.Set("Origin", "http://192.0.2.10:8080")
		if cookie != nil {
			r.AddCookie(cookie)
		}
		w := httptest.NewRecorder()
		a.Handler().ServeHTTP(w, r)
		return w
	}
	login := post("/login", url.Values{"code": {code.Code}}.Encode(), nil)
	if login.Code != http.StatusSeeOther {
		t.Fatalf("login failed: %d %s", login.Code, login.Body.String())
	}
	cookie := login.Result().Cookies()[0]
	if browse := request(a, http.MethodGet, "/browse/", "", cookie); browse.Code != http.StatusOK {
		t.Fatal("authorized directory unavailable")
	}
	session := a.sessions[cookie.Value]
	logout := post("/logout", url.Values{"csrf": {session.CSRF}}.Encode(), cookie)
	if logout.Code != http.StatusSeeOther {
		t.Fatalf("logout failed: %d %s", logout.Code, logout.Body.String())
	}
	if _, ok := a.sessions[cookie.Value]; ok {
		t.Fatal("logout failed to revoke session")
	}
}
