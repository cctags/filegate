package gate

import (
	"bytes"
	"io"
	"log/slog"
	"net/http"
	"net/http/httptest"
	"net/url"
	"strings"
	"testing"
	"time"
)

func TestAuditPanicAndPartialWrite(t *testing.T) {
	var output bytes.Buffer
	old := slog.Default()
	slog.SetDefault(slog.New(slog.NewJSONHandler(&output, nil)))
	defer slog.SetDefault(old)
	a := setup(t)
	func() {
		defer func() {
			if recover() != "PANIC_CREDENTIAL" {
				t.Error("panic not propagated")
			}
		}()
		a.accessLog(http.HandlerFunc(func(http.ResponseWriter, *http.Request) { panic("PANIC_CREDENTIAL") })).ServeHTTP(httptest.NewRecorder(), httptest.NewRequest("GET", "/", nil))
	}()
	if strings.Contains(output.String(), "PANIC_CREDENTIAL") {
		t.Fatal("panic payload logged")
	}
	var found bool
	for _, r := range readLogRecords(t, &output) {
		if r["reason"] == "handler_panic" && r["status"] == float64(500) {
			found = true
		}
	}
	if !found {
		t.Fatal("panic outcome missing")
	}
	w := &loggedResponse{ResponseWriter: &partialResponse{header: http.Header{}}}
	if n, err := w.Write([]byte("abcdef")); n != 2 || err != io.ErrClosedPipe || w.bytes != 2 || w.status != 200 {
		t.Fatal("partial-write accounting incorrect")
	}
}

type partialResponse struct{ header http.Header }

func (w *partialResponse) Header() http.Header       { return w.header }
func (w *partialResponse) WriteHeader(int)           {}
func (w *partialResponse) Write([]byte) (int, error) { return 2, io.ErrClosedPipe }

func TestTimezoneMetadataBoundaries(t *testing.T) {
	for _, tc := range []struct {
		name         string
		form         url.Values
		zone, source string
		offset       any
	}{
		{"missing", url.Values{}, "unknown", "unavailable", "unknown"},
		{"positive", url.Values{"client_timezone": {"UTC"}, "client_utc_offset": {"840"}}, "UTC", "form_reported", 840},
		{"negative", url.Values{"client_timezone": {"UTC"}, "client_utc_offset": {"-840"}}, "UTC", "form_reported", -840},
		{"outside", url.Values{"client_timezone": {"UTC"}, "client_utc_offset": {"841"}}, "UTC", "unverified_form", "unknown"},
		{"repeated", url.Values{"client_timezone": {"UTC", "UTC"}, "client_utc_offset": {"0", "0"}}, "unknown", "unverified_form", "unknown"},
	} {
		t.Run(tc.name, func(t *testing.T) {
			r := attachRequestAudit(httptest.NewRequest("POST", "/login", nil))
			r.PostForm = tc.form
			updateClientTimezone(r)
			a := auditFor(r)
			if a.timezone != tc.zone || a.timezoneSource != tc.source || a.utcOffset != tc.offset {
				t.Fatalf("metadata: %+v", a)
			}
		})
	}
}

func TestAuditSessionExpiryAndAliasSwitch(t *testing.T) {
	var output bytes.Buffer
	old := slog.Default()
	slog.SetDefault(slog.New(slog.NewJSONHandler(&output, nil)))
	defer slog.SetDefault(old)
	a := setup(t)
	cookie := access(t, a)
	s := a.sessions[cookie.Value]
	s.ClientTimezone = "UTC"
	s.HasClientOffset = true
	s.Seen = time.Now().Add(-2 * a.cfg.idle)
	a.sessions[cookie.Value] = s
	w := clientRequest(a, "GET", "/browse/", "", cookie)
	for _, r := range readLogRecords(t, &output) {
		if r["request_id"] == w.Header().Get("X-Request-ID") && r["timezone"] != "unknown" {
			t.Fatal("expired metadata inherited")
		}
	}
	oldCookie := access(t, a)
	target := createCode(t, a, "cd")
	w = clientRequest(a, "POST", "/login", "code="+target.Code, oldCookie)
	var matched bool
	for _, r := range readLogRecords(t, &output) {
		if r["request_id"] == w.Header().Get("X-Request-ID") && r["msg"] == "http request" {
			matched = true
			if r["alias"] != "cd" {
				t.Fatal("old alias logged after switch")
			}
		}
	}
	if !matched {
		t.Fatal("missing switched request")
	}
	logoutCookie := responseCookie(t, w, cookieName)
	session := a.sessions[logoutCookie.Value]
	w = clientRequest(a, "POST", "/logout", "csrf="+session.CSRF, logoutCookie)
	for _, r := range readLogRecords(t, &output) {
		if r["request_id"] == w.Header().Get("X-Request-ID") && r["msg"] == "http request" && r["reason"] != "logged_out" {
			t.Fatal("logout outcome missing")
		}
	}
}

func FuzzClientMetadata(f *testing.F) {
	for _, s := range []string{"Chrome/1.2.3", "\x1b[31m\nSECRET", strings.Repeat("a", 1100), "zh-CN;q=NaN"} {
		f.Add(s)
	}
	f.Fuzz(func(t *testing.T, input string) {
		r := httptest.NewRequest("GET", "/", nil)
		r.Header["User-Agent"] = []string{input}
		r.Header["Accept-Language"] = []string{input}
		m := fieldsMap(clientFields(r))
		version := m["browser_version"].(string)
		if version != "unknown" {
			if len(version) > 19 || strings.Trim(version, "0123456789.") != "" {
				t.Fatal("unbounded version")
			}
		}
		language := m["client_language"].(string)
		if len(language) > 35 || strings.ContainsAny(language, "\r\n\x1b") {
			t.Fatal("unbounded language")
		}
	})
}
