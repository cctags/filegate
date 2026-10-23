package gate

import (
	"log/slog"
	"net/http"
	"net/url"
	"strings"
	"time"
)

// Origin is supposed to contain only scheme and authority. Do not echo extra
// userinfo, paths or queries from a malformed header into diagnostic logs.
func logOrigin(raw string) string {
	if raw == "" || raw == "null" {
		return raw
	}
	u, err := url.Parse(raw)
	if err != nil || u.Host == "" || (u.Scheme != "http" && u.Scheme != "https") {
		return "invalid"
	}
	return u.Scheme + "://" + u.Host
}

func logFetchSite(raw string) string {
	switch raw {
	case "", "same-origin", "same-site", "cross-site", "none":
		return raw
	default:
		return "invalid"
	}
}

type loggedResponse struct {
	http.ResponseWriter
	status int
	bytes  int64
}

func (w *loggedResponse) Unwrap() http.ResponseWriter { return w.ResponseWriter }
func (w *loggedResponse) WriteHeader(status int) {
	if w.status != 0 {
		return
	}
	if status >= 200 {
		w.status = status
	}
	w.ResponseWriter.WriteHeader(status)
}
func (w *loggedResponse) Write(data []byte) (int, error) {
	if w.status == 0 {
		w.WriteHeader(http.StatusOK)
	}
	n, err := w.ResponseWriter.Write(data)
	w.bytes += int64(n)
	return n, err
}

func (a *App) accessLog(next http.Handler) http.Handler {
	return http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		started := time.Now()
		response := &loggedResponse{ResponseWriter: w}
		ip, _ := clientIP(r)
		alias := ""
		if cookie, err := r.Cookie(cookieName); err == nil {
			a.mu.Lock()
			alias = a.sessions[cookie.Value].Alias
			a.mu.Unlock()
		}
		defer func() {
			panicValue := recover()
			status := response.status
			if status == 0 {
				status = http.StatusOK
			}
			if panicValue != nil {
				status = http.StatusInternalServerError
			}
			fields := []any{"ip", ip.String(), "method", r.Method, "path", r.URL.Path, "status", status, "bytes", response.bytes, "duration_ms", time.Since(started).Milliseconds()}
			if alias != "" {
				fields = append(fields, "alias", alias)
			}
			if strings.HasPrefix(r.URL.Path, "/file/") {
				fields = append(fields, "download", strings.HasPrefix(response.Header().Get("Content-Disposition"), "attachment"), "download_requested", r.URL.Query().Get("download") == "1")
			}
			// Deliberately omit query strings, headers and bodies: they may contain
			// access codes, Cookie values or other credentials.
			if status >= 500 {
				slog.Error("http request", fields...)
			} else if status >= 400 {
				slog.Warn("http request", fields...)
			} else {
				slog.Info("http request", fields...)
			}
			if panicValue != nil {
				panic(panicValue)
			}
		}()
		next.ServeHTTP(response, r)
	})
}
