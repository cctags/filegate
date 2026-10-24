package gate

import (
	"net/http"
	"net/http/httptest"
	"net/url"
	"os"
	"path/filepath"
	"strings"
	"testing"
)

func TestBrowserLanguage(t *testing.T) {
	for _, tc := range []struct{ header, want string }{
		{"", "zh-CN"}, {"fr, de;q=0.9", "zh-CN"},
		{"en-US,en;q=0.9", "en"}, {"zh-CN,en;q=0.9", "zh-CN"},
		{"zh-TW", "zh-CN"}, {"en;q=0.4,zh;q=0.8", "zh-CN"},
		{"fr,en-GB;q=0.7,zh;q=0.5", "en"}, {"en;q=0,zh;q=0.2", "zh-CN"},
		{"en;q=1,zh;q=1", "en"}, {"en;q=NaN,zh;q=0.5", "zh-CN"},
		{"en;q=2", "zh-CN"}, {"en;q=bad", "zh-CN"}, {"*", "zh-CN"},
	} {
		t.Run(tc.header, func(t *testing.T) {
			if got := browserLanguage(tc.header); got != tc.want {
				t.Fatalf("got %q, want %q", got, tc.want)
			}
		})
	}
}

func languageRequest(a *App, method, target, body, header string, cookies ...*http.Cookie) *httptest.ResponseRecorder {
	r := httptest.NewRequest(method, target, strings.NewReader(body))
	r.RemoteAddr = "192.0.2.1:1234"
	r.Header.Set("Accept-Language", header)
	r.Header.Set("Content-Type", "application/x-www-form-urlencoded")
	for _, cookie := range cookies {
		if cookie != nil {
			r.AddCookie(cookie)
		}
	}
	w := httptest.NewRecorder()
	a.Handler().ServeHTTP(w, r)
	return w
}

func TestLanguagePreference(t *testing.T) {
	a := setup(t)
	english := &http.Cookie{Name: languageCookieName, Value: "en"}
	for _, tc := range []struct {
		target, header, want, text string
		cookie                     *http.Cookie
	}{
		{"/", "en-US", "en", "Access files", nil},
		{"/", "zh-CN", "zh-CN", "访问文件", nil},
		{"/", "zh-CN", "en", "Access files", english},
		{"/?lang=zh-CN", "en", "zh-CN", "访问文件", english},
		{"/?lang=auto", "zh-CN", "zh-CN", "访问文件", english},
		{"/?lang=invalid", "zh-CN", "en", "Access files", english},
		{"/?lang=%3Cscript%3E", "en", "en", "Access files", nil},
		{"/", "en", "en", "Access files", &http.Cookie{Name: languageCookieName, Value: "invalid"}},
	} {
		w := languageRequest(a, "GET", tc.target, "", tc.header, tc.cookie)
		if w.Code != 200 || w.Header().Get("Content-Language") != tc.want || !strings.Contains(w.Body.String(), `<html lang="`+tc.want+`">`) || !strings.Contains(w.Body.String(), tc.text) {
			t.Fatalf("%s: %d %s", tc.target, w.Code, w.Body.String())
		}
		if !strings.Contains(w.Body.String(), "English</a>") || !strings.Contains(w.Body.String(), "简体中文</a>") || !strings.Contains(w.Header().Get("Vary"), "Accept-Language") {
			t.Fatal("missing language controls or cache variation")
		}
	}
	for _, target := range []string{"http://example/?lang=en", "https://example/?lang=zh-CN", "/?lang=auto"} {
		w := languageRequest(a, "GET", target, "", "en", english)
		cookies := w.Result().Cookies()
		if len(cookies) != 1 {
			t.Fatalf("expected one preference cookie, got %v", cookies)
		}
		c := cookies[0]
		if c.Name != languageCookieName || c.Path != "/" || !c.HttpOnly || c.SameSite != http.SameSiteStrictMode || c.Secure != strings.HasPrefix(target, "https:") {
			t.Fatalf("unexpected preference cookie: %+v", c)
		}
		if strings.Contains(target, "auto") {
			if c.MaxAge != -1 {
				t.Fatal("automatic mode must remove the preference")
			}
		} else if c.MaxAge != 180*24*60*60 {
			t.Fatal("preference must last 180 days")
		}
	}
}

func TestLanguageLoginBrowseAndFiles(t *testing.T) {
	a := setup(t)
	pref := &http.Cookie{Name: languageCookieName, Value: "en"}
	code := a.Admin(Request{Action: "code", Value: "ab"})
	// A query on a POST cannot change the language or bypass authentication.
	bad := languageRequest(a, "POST", "/login?lang=zh-CN", "code=wrong", "zh-CN", pref)
	if bad.Code != 401 || !strings.Contains(bad.Body.String(), "already used or expired") || !strings.Contains(bad.Body.String(), `href="/?lang=en"`) || len(bad.Result().Cookies()) != 0 {
		t.Fatalf("unexpected failed login: %d %s", bad.Code, bad.Body.String())
	}
	login := languageRequest(a, "POST", "/login", url.Values{"code": {code.Code}}.Encode(), "zh-CN", pref)
	if login.Code != 303 || len(login.Result().Cookies()) != 1 {
		t.Fatalf("login failed: %d %s", login.Code, login.Body.String())
	}
	session := login.Result().Cookies()[0]
	w := languageRequest(a, "GET", "/browse/", "", "zh-CN", pref, session)
	for _, text := range []string{"File directory", "Log out", "Download", "bytes", "hello.txt"} {
		if !strings.Contains(w.Body.String(), text) {
			t.Errorf("missing %s", text)
		}
	}
	w = languageRequest(a, "GET", "/browse/sub/?lang=zh-CN", "", "en", pref, session)
	if w.Code != 200 || !strings.Contains(w.Body.String(), "目录为空") || !strings.Contains(w.Body.String(), "上级目录") || !strings.Contains(w.Body.String(), `href="/browse/sub/?lang=en"`) {
		t.Fatalf("directory language switch failed: %d %s", w.Code, w.Body.String())
	}
	if _, ok := a.sessions[session.Value]; !ok {
		t.Fatal("language switch revoked the login")
	}
	if _, ok := a.state.Codes[code.ID]; ok {
		t.Fatal("language switch restored the consumed code")
	}
	w = languageRequest(a, "GET", "/file/hello.txt?download=1&lang=zh-CN", "", "en", pref, session)
	if w.Code != 200 || w.Body.String() != "hello world" || !strings.HasPrefix(w.Header().Get("Content-Disposition"), "attachment") || w.Header().Get("Content-Language") != "" || len(w.Result().Cookies()) != 0 {
		t.Fatal("language selection changed file data, download behavior or cookies")
	}
	w = languageRequest(a, "GET", "/file/secret.txt", "", "en", pref, session)
	if w.Code != 404 || !strings.Contains(w.Body.String(), "File or page not found") {
		t.Fatal("language selection bypassed isolation or failed to translate error")
	}
	csrf := a.sessions[session.Value].CSRF
	w = languageRequest(a, "POST", "/logout", "csrf="+csrf, "en", pref, session)
	if w.Code != 303 || len(a.sessions) != 0 || w.Result().Cookies()[0].Name != cookieName {
		t.Fatal("logout failed or removed the language preference")
	}
}

func TestLanguageSwitchPreservesEscapedDirectory(t *testing.T) {
	a := setup(t)
	name := "文件夹?#"
	if err := os.Mkdir(filepath.Join(a.cfg.Root, "ab", name), 0700); err != nil {
		t.Fatal(err)
	}
	cookie := access(t, a)
	path := (&url.URL{Path: "/browse/" + name + "/"}).String()
	w := languageRequest(a, "GET", path+"?lang=en", "", "zh-CN", cookie)
	if w.Code != 200 || !strings.Contains(w.Body.String(), `href="`+path+`?lang=zh-CN"`) {
		t.Fatalf("language links did not preserve the directory: %d %s", w.Code, w.Body.String())
	}
}

func TestLocalizedErrorsAndBan(t *testing.T) {
	a := setup(t)
	for _, tc := range []struct {
		method, target, want string
		status               int
	}{
		{"GET", "/missing", "File or page not found", 404},
		{"POST", "/logout", "Invalid logout request", 403},
	} {
		w := languageRequest(a, tc.method, tc.target, "", "en")
		if w.Code != tc.status || !strings.Contains(w.Body.String(), tc.want) || w.Header().Get("Content-Language") != "en" {
			t.Fatalf("unexpected error: %d %s", w.Code, w.Body.String())
		}
	}
	for range a.cfg.MaxFailures {
		languageRequest(a, "POST", "/login", "code=wrong", "en")
	}
	w := languageRequest(a, "GET", "/?lang=zh-CN", "", "en")
	if w.Code != 403 || !strings.Contains(w.Body.String(), "访问已被禁止") {
		t.Fatal("changing language bypassed the IP ban")
	}
}

func TestTranslationKeysMatch(t *testing.T) {
	for language, dictionary := range translations {
		if len(dictionary) != len(translations["zh-CN"]) {
			t.Fatalf("%s has a different number of translations", language)
		}
		for key := range translations["zh-CN"] {
			if dictionary[key] == "" {
				t.Errorf("%s is missing %s", language, key)
			}
		}
	}
}
