package gate

import (
	"context"
	"log/slog"
	"net/http"
	"strconv"
	"strings"
)

type requestAuditKey struct{}
type requestAudit struct {
	requestID                string
	timezone, timezoneSource string
	utcOffset                any
	attrs                    []any
	outcome, reason, alias   string
}

// Client hints and User-Agent are untrusted descriptions, not identity proof.
// Emit only recognized labels and bounded numeric versions, never raw headers.
func clientFields(r *http.Request) []any {
	ip, err := clientIP(r)
	address := "unknown"
	if err == nil {
		address = ip.String()
	}
	ua := r.UserAgent()
	if len(ua) > 1024 {
		ua = ""
	}
	browser, version := "unknown", "unknown"
	for _, product := range []struct{ marker, name string }{
		{"Googlebot/", "Googlebot"}, {"bingbot/", "bingbot"}, {"EdgiOS/", "Edge"}, {"EdgA/", "Edge"}, {"Edg/", "Edge"}, {"OPR/", "Opera"},
		{"SamsungBrowser/", "SamsungInternet"}, {"FxiOS/", "Firefox"}, {"Firefox/", "Firefox"},
		{"CriOS/", "Chrome"}, {"Chrome/", "Chrome"}, {"curl/", "curl"}, {"Wget/", "wget"},
	} {
		if at := strings.Index(ua, product.marker); at >= 0 {
			browser = product.name
			version = productVersion(ua[at+len(product.marker):])
			break
		}
	}
	if browser == "unknown" && strings.Contains(ua, "Safari/") && strings.Contains(ua, "Version/") {
		browser = "Safari"
		version = productVersion(strings.SplitN(ua, "Version/", 2)[1])
	}
	osName := "unknown"
	switch {
	case strings.Contains(ua, "iPhone") || strings.Contains(ua, "iPad") || strings.Contains(ua, "iPod"):
		osName = "iOS"
	case strings.Contains(ua, "Android"):
		osName = "Android"
	case strings.Contains(ua, "CrOS"):
		osName = "ChromeOS"
	case strings.Contains(ua, "Windows"):
		osName = "Windows"
	case strings.Contains(ua, "Macintosh") || strings.Contains(ua, "Mac OS X"):
		osName = "macOS"
	case strings.Contains(ua, "Linux"):
		osName = "Linux"
	}
	// Only low-entropy, explicitly sent platform hints; do not request high-entropy
	// device models or full OS versions from browsers.
	switch strings.Trim(r.Header.Get("Sec-CH-UA-Platform"), `"`) {
	case "Windows":
		osName = "Windows"
	case "macOS":
		osName = "macOS"
	case "Linux":
		osName = "Linux"
	case "Android":
		osName = "Android"
	case "iOS":
		osName = "iOS"
	case "Chrome OS":
		osName = "ChromeOS"
	}
	kind := "unknown"
	switch browser {
	case "curl", "wget":
		kind = "command_line"
	case "Googlebot", "bingbot":
		kind = "bot"
	case "unknown":
	default:
		kind = "browser"
	}
	device := "unknown"
	switch {
	case strings.Contains(ua, "iPad"):
		device = "tablet"
	case strings.Contains(ua, "Mobile") || strings.Contains(ua, "iPhone") || r.Header.Get("Sec-CH-UA-Mobile") == "?1":
		device = "mobile"
	case osName == "Android" && kind == "browser":
		device = "tablet"
	case osName == "Windows" || osName == "macOS" || osName == "Linux" || osName == "ChromeOS":
		device = "desktop"
	}
	protocol := "unknown"
	if r.ProtoMajor == 1 && r.ProtoMinor == 0 {
		protocol = "HTTP/1.0"
	}
	if r.ProtoMajor == 1 && r.ProtoMinor == 1 {
		protocol = "HTTP/1.1"
	}
	if r.ProtoMajor == 2 {
		protocol = "HTTP/2"
	}
	return []any{"ip", address, "ip_source", "socket", "browser", browser, "browser_version", version,
		"os", osName, "device", device, "client_kind", kind, "client_language", logClientLanguage(r.Header.Get("Accept-Language")),
		"client_info_source", "client_reported", "http_version", protocol, "https", r.TLS != nil}
}

func productVersion(value string) string {
	end := strings.IndexAny(value, " ;)")
	if end >= 0 {
		value = value[:end]
	}
	if value == "" || len(value) > 19 {
		return "unknown"
	}
	parts := strings.Split(value, ".")
	if len(parts) > 4 {
		return "unknown"
	}
	for _, part := range parts {
		if len(part) < 1 || len(part) > 4 {
			return "unknown"
		}
		for _, c := range part {
			if c < '0' || c > '9' {
				return "unknown"
			}
		}
	}
	return value
}

func logClientLanguage(header string) string {
	if len(header) > 256 {
		return "unknown"
	}
	selected, best := "unknown", 0.0
	for _, item := range strings.Split(header, ",") {
		parameters := strings.Split(item, ";")
		tag := strings.TrimSpace(parameters[0])
		parts := strings.Split(tag, "-")
		if len(tag) > 35 || len(parts) > 5 {
			continue
		}
		valid := len(parts[0]) >= 2 && len(parts[0]) <= 8
		for i, part := range parts {
			if len(part) < 1 || len(part) > 8 {
				valid = false
				break
			}
			for _, c := range part {
				if !((c >= 'a' && c <= 'z') || (c >= 'A' && c <= 'Z') || (i > 0 && c >= '0' && c <= '9')) {
					valid = false
				}
			}
		}
		quality := 1.0
		for _, parameter := range parameters[1:] {
			key, value, ok := strings.Cut(strings.TrimSpace(parameter), "=")
			if !ok || !strings.EqualFold(key, "q") {
				valid = false
				break
			}
			q, err := strconv.ParseFloat(strings.TrimSpace(value), 64)
			if err != nil || !(q >= 0 && q <= 1) {
				valid = false
				break
			}
			quality = q
		}
		if valid && quality > best {
			selected, best = tag, quality
		}
	}
	return selected
}

func auditFor(r *http.Request) *requestAudit {
	audit, _ := r.Context().Value(requestAuditKey{}).(*requestAudit)
	return audit
}
func clientLogger(r *http.Request) *slog.Logger {
	return slog.Default().With(clientAttributes(r)...)
}
func setRequestResult(r *http.Request, outcome, reason string) {
	if audit := auditFor(r); audit != nil {
		audit.outcome = outcome
		audit.reason = reason
	}
}
func attachRequestAudit(r *http.Request) *http.Request {
	id := token()[:16]
	attrs := append([]any{"request_id", id}, clientFields(r)...)
	return r.WithContext(context.WithValue(r.Context(), requestAuditKey{}, &requestAudit{requestID: id, attrs: attrs, timezone: "unknown", timezoneSource: "unavailable", utcOffset: "unknown"}))
}
func requestOperation(r *http.Request) string {
	switch {
	case r.Method == http.MethodPost && r.URL.Path == "/login":
		return "login_form"
	case r.Method == http.MethodPost && r.URL.Path == "/access":
		return "login_link"
	case r.Method == http.MethodPost && r.URL.Path == "/logout":
		return "logout"
	case r.URL.Path == "/access":
		return "link_page"
	default:
		return "request"
	}
}
func loginResult(status int, message string) (string, string) {
	if status == http.StatusSeeOther {
		return "success", "authenticated"
	}
	switch message {
	case "InvalidCode":
		return "failure", "invalid_used_or_expired_code"
	case "RateBanned":
		return "denied", "ip_rate_limit"
	case "FailureBanned":
		return "denied", "too_many_invalid_codes"
	case "Blocked":
		return "denied", "ip_blocked"
	case "InvalidSource":
		return "denied", "invalid_client_address"
	case "TooManyRequests":
		return "denied", "global_login_rate_limit"
	case "SessionLimit":
		return "error", "session_limit"
	case "SaveFailed":
		return "error", "persistence_failure"
	default:
		return "error", "service_unavailable"
	}
}
