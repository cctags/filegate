package gate

import (
	"crypto/hmac"
	"net/http"
	"net/url"
	"strings"
	"time"
)

const (
	pendingCookieName = "filegate_pending"
	pendingTTL        = 5 * time.Minute
	maxPendingLogins  = 1000
	maxPendingPerIP   = 10
)

type pendingLogin struct {
	Digest, Alias, CSRF, IP string
	Expires                 time.Time
}

func accessCookie(r *http.Request, value string, maxAge int) *http.Cookie {
	// Lax permits the clean redirect after an external top-level link. The POST
	// additionally requires the unpredictable CSRF value and same-origin check.
	return &http.Cookie{Name: pendingCookieName, Value: value, Path: "/access", MaxAge: maxAge, HttpOnly: true, Secure: r.TLS != nil, SameSite: http.SameSiteLaxMode}
}

func accessAlias(code string) string {
	if len(code) < 9 || len(code) > 24 {
		return ""
	}
	alias := code[:len(code)-8]
	for _, c := range alias {
		if c < 'a' || c > 'z' {
			return ""
		}
	}
	for _, c := range code[len(code)-8:] {
		if c < '0' || c > '9' {
			return ""
		}
	}
	return alias
}

func (a *App) accessPage(w http.ResponseWriter, r *http.Request) {
	// HEAD is also matched by the GET route. It must not prepare or consume codes.
	if r.Method == http.MethodHead {
		w.Header().Set("Content-Type", "text/html; charset=utf-8")
		w.WriteHeader(200)
		return
	}
	// Parse explicitly: URL.Query silently discards malformed query parameters.
	query, err := url.ParseQuery(r.URL.RawQuery)
	if err != nil {
		setRequestResult(r, "denied", "invalid_link_format")
		webError(w, r, "InvalidLink", 400)
		return
	}
	if codes, ok := query["code"]; ok {
		if len(codes) != 1 {
			setRequestResult(r, "denied", "invalid_link_format")
			webError(w, r, "InvalidLink", 400)
			return
		}
		code := strings.ToLower(strings.TrimSpace(codes[0]))
		alias := accessAlias(code)
		if alias == "" {
			setRequestResult(r, "denied", "invalid_link_format")
			webError(w, r, "InvalidLink", 400)
			return
		}
		id, status := a.prepareAccess(r, code, alias)
		if status != 200 {
			w.Header().Set("Retry-After", "5")
			webError(w, r, "Busy", status)
			return
		}
		http.SetCookie(w, accessCookie(r, id, int(pendingTTL/time.Second)))
		http.Redirect(w, r, "/access", http.StatusSeeOther)
		return
	}
	pending, current, ok := a.accessConfirmation(r)
	if !ok {
		setRequestResult(r, "denied", "invalid_or_expired_confirmation")
		render(w, r, 403, view{Login: true, Error: "PendingExpired"})
		return
	}
	render(w, r, 200, view{Confirm: true, Alias: pending.Alias, CSRF: pending.CSRF, CurrentAlias: current})
}

func (a *App) prepareAccess(r *http.Request, code, alias string) (string, int) {
	ip, err := clientIP(r)
	if err != nil {
		return "", 400
	}
	a.mu.Lock()
	defer a.mu.Unlock()
	if a.closed {
		return "", 503
	}
	now := time.Now()
	a.cleanup(now)
	if previous, err := r.Cookie(pendingCookieName); err == nil {
		if old, ok := a.pending[previous.Value]; ok && old.IP == ip.String() {
			delete(a.pending, previous.Value)
		}
	}
	count := 0
	for _, p := range a.pending {
		if p.IP == ip.String() {
			count++
		}
	}
	if len(a.pending) >= maxPendingLogins || count >= maxPendingPerIP {
		return "", 429
	}
	id := token()
	a.pending[id] = pendingLogin{Digest: a.digest(code), Alias: alias, CSRF: token(), IP: ip.String(), Expires: now.Add(pendingTTL)}
	return id, 200
}

func (a *App) accessConfirmation(r *http.Request) (pendingLogin, string, bool) {
	ip, err := clientIP(r)
	if err != nil {
		return pendingLogin{}, "", false
	}
	cookie, err := r.Cookie(pendingCookieName)
	if err != nil {
		return pendingLogin{}, "", false
	}
	a.mu.Lock()
	defer a.mu.Unlock()
	p, ok := a.pending[cookie.Value]
	if a.closed || !ok || p.IP != ip.String() || !time.Now().Before(p.Expires) {
		return pendingLogin{}, "", false
	}
	current := ""
	if session, ok := a.sessionLocked(r); ok {
		current = session.Alias
	}
	return p, current, true
}

func (a *App) confirmAccess(w http.ResponseWriter, r *http.Request) {
	r.Body = http.MaxBytesReader(w, r.Body, 1024)
	if r.ParseForm() != nil {
		setRequestResult(r, "denied", "invalid_or_expired_confirmation")
		render(w, r, 403, view{Login: true, Error: "PendingExpired"})
		return
	}
	updateClientTimezone(r)
	digest := a.takeConfirmation(r, r.PostForm.Get("csrf"))
	if digest == "" {
		setRequestResult(r, "denied", "invalid_or_expired_confirmation")
		render(w, r, 403, view{Login: true, Error: "PendingExpired"})
		return
	}
	http.SetCookie(w, accessCookie(r, "", -1))
	status, message, id := a.consumeDigest(r, digest)
	completeLogin(w, r, status, message, id)
}

func (a *App) takeConfirmation(r *http.Request, csrf string) string {
	ip, err := clientIP(r)
	if err != nil {
		return ""
	}
	cookie, err := r.Cookie(pendingCookieName)
	if err != nil {
		return ""
	}
	a.mu.Lock()
	defer a.mu.Unlock()
	p, ok := a.pending[cookie.Value]
	if a.closed || !ok || p.IP != ip.String() || !time.Now().Before(p.Expires) || !hmac.Equal([]byte(p.CSRF), []byte(csrf)) {
		return ""
	}
	delete(a.pending, cookie.Value)
	return p.Digest
}
