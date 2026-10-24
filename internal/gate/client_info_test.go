package gate

import (
	"bufio"
	"bytes"
	"crypto/sha256"
	"encoding/base64"
	"encoding/json"
	"fmt"
	"log/slog"
	"net/http"
	"net/http/httptest"
	"net/url"
	"regexp"
	"strings"
	"testing"
)

func fieldsMap(fields []any) map[string]any {
	out := map[string]any{}
	for i := 0; i < len(fields); i += 2 {
		out[fields[i].(string)] = fields[i+1]
	}
	return out
}
func TestClientDescriptions(t *testing.T) {
	for _, tc := range []struct{ ua, browser, version, os, device, kind string }{
		{"Mozilla/5.0 (Windows NT 10.0; Win64; x64) Chrome/131.0.6778.86 Safari/537.36 Edg/131.0.2903.70", "Edge", "131.0.2903.70", "Windows", "desktop", "browser"},
		{"Mozilla/5.0 (Linux; Android 14) Chrome/131.0.0.0 Mobile Safari/537.36", "Chrome", "131.0.0.0", "Android", "mobile", "browser"},
		{"Mozilla/5.0 (X11; Linux x86_64; rv:130.0) Gecko/20100101 Firefox/130.0", "Firefox", "130.0", "Linux", "desktop", "browser"},
		{"Mozilla/5.0 (Macintosh; Intel Mac OS X 10_15_7) Version/18.0 Safari/605.1.15", "Safari", "18.0", "macOS", "desktop", "browser"},
		{"Mozilla/5.0 (iPad; CPU OS 17_0 like Mac OS X) Version/17.0 Mobile/15E148 Safari/604.1", "Safari", "17.0", "iOS", "tablet", "browser"},
		{"curl/8.12.1", "curl", "8.12.1", "unknown", "unknown", "command_line"},
		{"Googlebot/2.1 (+http://example/bot)", "Googlebot", "2.1", "unknown", "unknown", "bot"},
		{"opaque-client-secret", "unknown", "unknown", "unknown", "unknown", "unknown"},
	} {
		t.Run(tc.browser+tc.os, func(t *testing.T) {
			r := httptest.NewRequest("GET", "/", nil)
			r.RemoteAddr = "192.0.2.1:1234"
			r.Header.Set("User-Agent", tc.ua)
			fields := fieldsMap(clientFields(r))
			for k, want := range map[string]string{"browser": tc.browser, "browser_version": tc.version, "os": tc.os, "device": tc.device, "client_kind": tc.kind, "ip": "192.0.2.1", "ip_source": "socket"} {
				if fields[k] != want {
					t.Errorf("%s=%v, want %s", k, fields[k], want)
				}
			}
		})
	}
	for _, tc := range []struct{ header, want string }{{"en-US,en;q=0.9", "en-US"}, {"zh-CN;q=0.5,en;q=0.9", "en"}, {"en;q=0,fr-FR;q=0.9", "fr-FR"}, {"en;q=NaN,zh;q=0.5", "zh"}, {"secret_ab01234567", "unknown"}, {strings.Repeat("a", 257), "unknown"}} {
		if got := logClientLanguage(tc.header); got != tc.want {
			t.Fatalf("language %q: %q", tc.header, got)
		}
	}
}
func TestClientMetadataNormalization(t *testing.T) {
	r := httptest.NewRequest("GET", "/?timezone=QUERY_SECRET", nil)
	r.RemoteAddr = "192.0.2.1:1234"
	r.Header.Set("User-Agent", "Chrome/ab01234567 RAW_UA_SECRET\n\x1b[31m")
	r.Header.Set("Sec-CH-UA-Platform", `"PLATFORM_SECRET"`)
	r.Header.Set("Accept-Language", "LANG_SECRET")
	r.Header.Set("X-Forwarded-For", "198.51.100.2")
	text := fmt.Sprint(clientFields(r))
	for _, secret := range []string{"ab01234567", "RAW_UA_SECRET", "PLATFORM_SECRET", "LANG_SECRET", "QUERY_SECRET", "198.51.100.2", "\x1b"} {
		if strings.Contains(text, secret) {
			t.Fatalf("raw metadata leaked: %s", secret)
		}
	}
	r.Header.Set("User-Agent", strings.Repeat("Windows Chrome/120.0 ", 100))
	if fieldsMap(clientFields(r))["browser"] != "unknown" {
		t.Fatal("oversized UA was interpreted")
	}
	r.Header.Set("User-Agent", "")
	r.Header.Set("Sec-CH-UA-Platform", `"Android"`)
	r.Header.Set("Sec-CH-UA-Mobile", "?1")
	fields := fieldsMap(clientFields(r))
	if fields["os"] != "Android" || fields["device"] != "mobile" {
		t.Fatal("platform hints not recognized")
	}
}
func readLogRecords(t *testing.T, output *bytes.Buffer) []map[string]any {
	t.Helper()
	var records []map[string]any
	scanner := bufio.NewScanner(bytes.NewReader(output.Bytes()))
	for scanner.Scan() {
		var record map[string]any
		if err := json.Unmarshal(scanner.Bytes(), &record); err != nil {
			t.Fatal(err)
		}
		records = append(records, record)
	}
	if err := scanner.Err(); err != nil {
		t.Fatal(err)
	}
	return records
}
func clientRequest(a *App, method, target, body string, cookie *http.Cookie) *httptest.ResponseRecorder {
	r := httptest.NewRequest(method, target, strings.NewReader(body))
	r.RemoteAddr = "192.0.2.1:1234"
	r.Header.Set("User-Agent", "Mozilla/5.0 (Windows NT 10.0) Chrome/131.0.6778.86 Safari/537.36 UA_SECRET")
	r.Header.Set("Accept-Language", "zh-CN,en;q=0.9")
	r.Header.Set("X-Request-ID", "FORGED_REQUEST_ID")
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
func TestLoginClientAuditAndSessionTimezone(t *testing.T) {
	var output bytes.Buffer
	old := slog.Default()
	slog.SetDefault(slog.New(slog.NewJSONHandler(&output, nil)))
	defer slog.SetDefault(old)
	a := setup(t)
	code := createCode(t, a, "ab")
	failed := clientRequest(a, "POST", "/login", url.Values{"code": {"wrong"}, "client_timezone": {"UTC"}, "client_utc_offset": {"0"}}.Encode(), nil)
	success := clientRequest(a, "POST", "/login", url.Values{"code": {code.Code}, "client_timezone": {"UTC"}, "client_utc_offset": {"0"}}.Encode(), nil)
	if failed.Code != 401 || success.Code != 303 {
		t.Fatal("metadata changed authentication")
	}
	cookie := responseCookie(t, success, cookieName)
	file := clientRequest(a, "GET", "/file/hello.txt?secret=QUERY_SECRET", "", cookie)
	if file.Code != 200 {
		t.Fatal(file.Code)
	}
	records := readLogRecords(t, &output)
	for _, response := range []*httptest.ResponseRecorder{failed, success, file} {
		id := response.Header().Get("X-Request-ID")
		if !regexp.MustCompile(`^[0-9a-f]{16}$`).MatchString(id) || id == "FORGED_REQUEST_ID" {
			t.Fatal("request ID not generated locally")
		}
		found := false
		for _, record := range records {
			if record["request_id"] != id {
				continue
			}
			if record["msg"] == "http request" {
				found = true
			}
			for key, want := range map[string]any{"ip": "192.0.2.1", "browser": "Chrome", "browser_version": "131.0.6778.86", "os": "Windows", "client_language": "zh-CN", "timezone": "UTC", "utc_offset_minutes": float64(0)} {
				if record[key] != want {
					t.Errorf("%s=%v, want %v", key, record[key], want)
				}
			}
		}
		if !found {
			t.Fatal("request not logged")
		}
	}
	results := map[string]bool{}
	for _, record := range records {
		if record["msg"] == "http request" {
			results[fmt.Sprint(record["reason"])] = true
			if record["request_id"] == file.Header().Get("X-Request-ID") && record["timezone_source"] != "session_reported" {
				t.Fatal("file request did not identify cached timezone")
			}
		}
	}
	if !results["invalid_used_or_expired_code"] || !results["authenticated"] {
		t.Fatal("missing login outcomes")
	}
	for _, secret := range []string{code.Code, cookie.Value, a.state.Secret, "UA_SECRET", "QUERY_SECRET", "FORGED_REQUEST_ID"} {
		if strings.Contains(output.String(), secret) {
			t.Fatalf("credential/header leaked: %s", secret)
		}
	}
}
func TestInvalidClientTimezoneDoesNotAffectLogin(t *testing.T) {
	a := setup(t)
	code := createCode(t, a, "ab")
	w := clientRequest(a, "POST", "/login", url.Values{"code": {code.Code}, "client_timezone": {code.Code}, "client_utc_offset": {"480&OFFSET_SECRET"}}.Encode(), nil)
	if w.Code != 303 {
		t.Fatal("invalid optional metadata prevented login")
	}
	session := a.sessions[responseCookie(t, w, cookieName).Value]
	if session.ClientTimezone != "" || session.HasClientOffset {
		t.Fatal("invalid metadata stored")
	}
	for _, zone := range []string{"", "Local", "../../etc/passwd", "/etc/passwd", "Asia/Shanghai?code=SECRET", strings.Repeat("x", 65)} {
		if reportedTimezone(zone) != "unknown" {
			t.Fatalf("accepted unsafe timezone %q", zone)
		}
	}
	if reportedTimezone("UTC") != "UTC" {
		t.Fatal("UTC must work without system zoneinfo")
	}
}
func TestTimezoneCSPIsRestrictedToGeneratedPages(t *testing.T) {
	a := setup(t)
	w := request(a, "GET", "/", "", nil)
	script := regexp.MustCompile(`(?s)<script>(.*?)</script>`).FindStringSubmatch(w.Body.String())
	if len(script) != 2 || script[1] != clientTimezoneScript {
		t.Fatal("rendered script differs from fixed program")
	}
	hash := sha256.Sum256([]byte(script[1]))
	directive := "script-src 'sha256-" + base64.StdEncoding.EncodeToString(hash[:]) + "'"
	if !strings.Contains(w.Header().Get("Content-Security-Policy"), directive) || strings.Contains(w.Header().Get("Content-Security-Policy"), "script-src 'self'") {
		t.Fatal("CSP does not restrict scripts to the fixed hash")
	}
	cookie := access(t, a)
	w = request(a, "GET", "/file/test.html", "", cookie)
	if w.Header().Get("Content-Security-Policy") != baseCSP || w.Header().Get("Content-Type") != "text/plain; charset=utf-8" {
		t.Fatal("file preview script policy relaxed")
	}
}
func TestRejectedLoginHasClientContext(t *testing.T) {
	var output bytes.Buffer
	old := slog.Default()
	slog.SetDefault(slog.New(slog.NewJSONHandler(&output, nil)))
	defer slog.SetDefault(old)
	a := setup(t)
	r := httptest.NewRequest("POST", "/login", strings.NewReader("code=BODY_SECRET"))
	r.RemoteAddr = "192.0.2.1:1234"
	r.Header.Set("Origin", "http://evil.example")
	r.Header.Set("User-Agent", "curl/8.12.1")
	w := httptest.NewRecorder()
	a.Handler().ServeHTTP(w, r)
	if w.Code != 403 {
		t.Fatal(w.Code)
	}
	records := readLogRecords(t, &output)
	found := false
	for _, record := range records {
		if record["msg"] == "http request" && record["reason"] == "invalid_origin" && record["browser"] == "curl" && record["timezone"] == "unknown" {
			found = true
		}
	}
	if !found || strings.Contains(output.String(), "BODY_SECRET") {
		t.Fatal("rejected login missing context or leaked body")
	}
}

func TestLinkConfirmationReportsClientTimezone(t *testing.T) {
	var output bytes.Buffer
	old := slog.Default()
	slog.SetDefault(slog.New(slog.NewJSONHandler(&output, nil)))
	defer slog.SetDefault(old)
	a := setup(t)
	code := createCode(t, a, "ab")
	cookie, csrf := prepareLink(t, a, code.Code)
	response := clientRequest(a, "POST", "/access", url.Values{"csrf": {csrf}, "client_timezone": {"UTC"}, "client_utc_offset": {"0"}}.Encode(), cookie)
	if response.Code != 303 {
		t.Fatal(response.Code)
	}
	found := false
	for _, record := range readLogRecords(t, &output) {
		if record["msg"] == "login succeeded" && record["operation"] == "login_link" && record["timezone"] == "UTC" {
			found = true
		}
	}
	if !found {
		t.Fatal("linked login missing client timezone")
	}
}
