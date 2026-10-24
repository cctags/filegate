package gate

import (
	"crypto/hmac"
	"errors"
	"html/template"
	"io"
	"log/slog"
	"mime"
	"net"
	"net/http"
	"net/netip"
	"net/url"
	"os"
	"path"
	"sort"
	"strings"
	"syscall"
	"time"
)

const cookieName = "filegate_session"

var page = template.Must(template.New("page").Parse(`<!doctype html><html lang="{{.Language}}"><meta charset="utf-8"><meta name="viewport" content="width=device-width, initial-scale=1"><title>{{.Text.Title}}</title><style>body{font:16px system-ui,sans-serif;background:#f4f6f8;color:#213040;margin:0}main{max-width:850px;margin:8vh auto;padding:32px;background:white;border:1px solid #dbe2e8;border-radius:14px}input,button{font:inherit;padding:10px;border:1px solid #aebcca;border-radius:6px}button{cursor:pointer;background:#edf4ff}a{color:#175db5}li{padding:9px;overflow-wrap:anywhere}.muted{color:#596a7c}.error{color:#ad2531}header{display:flex;align-items:center;justify-content:space-between;gap:20px}form{margin:16px 0}@media(max-width:600px){main{margin:20px 10px;padding:20px}}</style><main><nav aria-label="{{.Text.Language}}"><a href="{{.LanguageURL}}?lang=zh-CN" lang="zh-CN" hreflang="zh-CN">简体中文</a> · <a href="{{.LanguageURL}}?lang=en" lang="en" hreflang="en">English</a> · <a href="{{.LanguageURL}}?lang=auto">{{.Text.Automatic}}</a></nav>{{if .Confirm}}<h1>{{.Text.ConfirmHeading}}</h1><p>{{.Text.TargetDirectory}}: {{.Alias}}</p><p class="muted">{{.Text.ConfirmInstruction}}</p>{{if .CurrentAlias}}<p>{{.Text.CurrentDirectory}}: {{.CurrentAlias}}</p>{{end}}<p class="muted">{{.Text.ReplaceSession}}</p><form method="post" action="/access"><input type="hidden" name="csrf" value="{{.CSRF}}"><button>{{.Text.ConfirmEnter}}</button></form><p><a href="/">{{.Text.ManualEntry}}</a></p>{{else if .Login}}<h1>{{.Text.Heading}}</h1><p class="muted">{{.Text.Instruction}}</p>{{if .Error}}<p class="error" role="alert">{{.Error}}</p>{{end}}<form method="post" action="/login"><label for="code">{{.Text.Code}}</label><p><input id="code" name="code" required maxlength="24" autocomplete="off" autofocus placeholder="{{.Text.Example}}"></p><button>{{.Text.Enter}}</button></form>{{else}}<header><h1>{{.Text.Directory}} · {{.Alias}}</h1><form method="post" action="/logout"><input type="hidden" name="csrf" value="{{.CSRF}}"><button>{{.Text.Logout}}</button></form></header><p class="muted">{{.Path}}</p><ul>{{if .Parent}}<li><a href="{{.Parent}}">↑ {{.Text.Parent}}</a></li>{{end}}{{range .Entries}}<li><a href="{{.URL}}">{{.Name}}{{if .Dir}} /{{end}}</a>{{if not .Dir}} <span class="muted">{{.Size}} {{$.Text.Bytes}}</span> · <a href="{{.URL}}?download=1">{{$.Text.Download}}</a>{{end}}</li>{{else}}<li class="muted">{{.Text.Empty}}</li>{{end}}</ul>{{end}}</main></html>`))

type entry struct {
	Name, URL string
	Dir       bool
	Size      int64
}
type view struct {
	Language, LanguageURL            string
	Text                             map[string]string
	Login                            bool
	Confirm                          bool
	CurrentAlias                     string
	Error, Alias, Path, CSRF, Parent string
	Entries                          []entry
}

func netipAddr(s string) (netip.Addr, error) {
	ip, err := netip.ParseAddr(s)
	if err != nil || !ip.Is4() {
		return netip.Addr{}, errors.New("only IPv4 addresses are supported")
	}
	return ip, nil
}
func clientIP(r *http.Request) (netip.Addr, error) {
	host, _, e := net.SplitHostPort(r.RemoteAddr)
	if e != nil {
		return netip.Addr{}, e
	}
	return netipAddr(host)
}
func render(w http.ResponseWriter, r *http.Request, status int, v view) {
	v.Language = requestLanguage(r)
	v.Text = translations[v.Language]
	v.LanguageURL = (&url.URL{Path: r.URL.Path}).String()
	if v.Login {
		v.LanguageURL = "/"
	}
	if v.Confirm {
		v.LanguageURL = "/access"
	}
	if v.Error != "" {
		v.Error = v.Text[v.Error]
	}
	languageHeaders(w, v.Language)
	w.Header().Set("Content-Type", "text/html; charset=utf-8")
	w.WriteHeader(status)
	if e := page.Execute(w, v); e != nil {
		slog.Error("render", "error", e)
	}
}
func sameOrigin(r *http.Request) bool {
	origin := r.Header.Get("Origin")
	if origin == "" {
		return r.Header.Get("Sec-Fetch-Site") != "cross-site"
	}
	u, e := url.Parse(origin)
	scheme := "http"
	if r.TLS != nil {
		scheme = "https"
	}
	return e == nil && u.Host == r.Host && u.Scheme == scheme
}
func (a *App) Handler() http.Handler {
	mux := http.NewServeMux()
	mux.HandleFunc("GET /{$}", func(w http.ResponseWriter, r *http.Request) {
		if _, ok := a.session(r); ok {
			http.Redirect(w, r, "/browse/", http.StatusSeeOther)
			return
		}
		render(w, r, 200, view{Login: true})
	})
	mux.HandleFunc("POST /login", a.login)
	mux.HandleFunc("GET /access", a.accessPage)
	mux.HandleFunc("POST /access", a.confirmAccess)
	mux.HandleFunc("POST /logout", a.logout)
	mux.HandleFunc("GET /browse/", a.browse)
	mux.HandleFunc("GET /file/", a.file)
	mux.HandleFunc("GET /", func(w http.ResponseWriter, r *http.Request) {
		webError(w, r, "NotFound", http.StatusNotFound)
	})
	return a.accessLog(a.limitHTTP(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		w.Header().Set("Cache-Control", "no-store")
		w.Header().Set("X-Content-Type-Options", "nosniff")
		w.Header().Set("X-Frame-Options", "DENY")
		w.Header().Set("Content-Security-Policy", "default-src 'none'; style-src 'unsafe-inline'; img-src 'self'; media-src 'self'; frame-src 'self'; form-action 'self'; frame-ancestors 'none'; base-uri 'none'")
		ip, e := clientIP(r)
		if e != nil {
			webError(w, r, "InvalidSource", 400)
			return
		}
		a.mu.Lock()
		a.cleanup(time.Now())
		blocked, e := a.blocked(ip)
		if blocked {
			if c, err := r.Cookie(cookieName); err == nil {
				delete(a.sessions, c.Value)
			}
		}
		a.mu.Unlock()
		if e != nil {
			slog.Error("blacklist unavailable", "error", e)
			webError(w, r, "Unavailable", 503)
			return
		}
		if blocked {
			webError(w, r, "Blocked", 403)
			return
		}
		if r.Method == "POST" && !sameOrigin(r) {
			slog.Warn("request origin rejected", "ip", ip.String(), "host", r.Host,
				"origin", logOrigin(r.Header.Get("Origin")), "fetch_site", logFetchSite(r.Header.Get("Sec-Fetch-Site")),
				"https", r.TLS != nil)
			webError(w, r, "InvalidOrigin", 403)
			return
		}
		saveLanguage(w, r)
		mux.ServeHTTP(w, r)
	})))
}
func (a *App) session(r *http.Request) (Session, bool) {
	a.mu.Lock()
	defer a.mu.Unlock()
	return a.sessionLocked(r)
}

func (a *App) sessionLocked(r *http.Request) (Session, bool) {
	c, err := r.Cookie(cookieName)
	if err != nil || a.closed {
		return Session{}, false
	}
	s, ok := a.sessions[c.Value]
	now := time.Now()
	if !ok {
		return s, false
	}
	if now.Sub(s.Seen) >= a.cfg.idle || now.Sub(s.Created) >= a.cfg.max {
		delete(a.sessions, c.Value)
		return Session{}, false
	}
	s.Seen = now
	a.sessions[c.Value] = s
	return s, true
}

// Recheck the session and open the file under one lock. Otherwise a reload
// could replace a directory between authorization and opening its contents.
func (a *App) openFile(r *http.Request, name string) (*os.File, error) {
	a.mu.Lock()
	defer a.mu.Unlock()
	s, ok := a.sessionLocked(r)
	if !ok {
		return nil, os.ErrPermission
	}
	root := a.roots[s.Alias]
	if root == nil {
		return nil, os.ErrPermission
	}
	return root.OpenFile(name, os.O_RDONLY|syscall.O_NONBLOCK, 0)
}

func (a *App) login(w http.ResponseWriter, r *http.Request) {
	// Read untrusted network data before taking the shared state lock.
	r.Body = http.MaxBytesReader(w, r.Body, 1024)
	parseErr := r.ParseForm()
	status, message, id := a.consumeLogin(r, parseErr)
	completeLogin(w, r, status, message, id)
}

func completeLogin(w http.ResponseWriter, r *http.Request, status int, message, id string) {
	if status != http.StatusSeeOther {
		if status == http.StatusTooManyRequests {
			w.Header().Set("Retry-After", "60")
		}
		if status == http.StatusUnauthorized {
			render(w, r, status, view{Login: true, Error: message})
		} else {
			webError(w, r, message, status)
		}
		return
	}
	http.SetCookie(w, &http.Cookie{Name: cookieName, Value: id, Path: "/", HttpOnly: true, Secure: r.TLS != nil, SameSite: http.SameSiteStrictMode})
	http.Redirect(w, r, "/browse/", http.StatusSeeOther)
}

// Only state validation and durable consumption are serialized. Never read or
// write a client connection while holding this lock.
func (a *App) consumeLogin(r *http.Request, parseErr error) (int, string, string) {
	plain := strings.ToLower(strings.TrimSpace(r.PostForm.Get("code")))
	digest := ""
	if parseErr == nil && len(plain) >= 9 && len(plain) <= 24 {
		a.mu.Lock()
		digest = a.digest(plain)
		a.mu.Unlock()
	}
	return a.consumeDigest(r, digest)
}

func (a *App) consumeDigest(r *http.Request, digest string) (int, string, string) {
	ip, err := clientIP(r)
	if err != nil {
		return 400, "InvalidSource", ""
	}
	a.mu.Lock()
	defer a.mu.Unlock()
	if a.closed {
		return 503, "Unavailable", ""
	}
	blocked, err := a.blocked(ip)
	if err != nil {
		slog.Error("blacklist unavailable", "error", err)
		return 503, "Unavailable", ""
	}
	if blocked {
		return 403, "Blocked", ""
	}
	now := time.Now()
	a.cleanup(now)
	if now.Sub(a.globalSince) >= time.Minute {
		a.globalSince = now
		a.globalCount = 0
	}
	a.globalCount++
	if a.globalCount > 1000 {
		return 429, "TooManyRequests", ""
	}
	key := ip.String()
	v := a.attempts[key]
	if v.Since.IsZero() || now.Sub(v.Since) >= time.Minute {
		v.Since = now
		v.Count = 0
	}
	if v.FailureSince.IsZero() || now.Sub(v.FailureSince) >= a.cfg.window {
		v.FailureSince = now
		v.Failures = 0
	}
	v.Count++
	ban := func() {
		a.state.Bans[key] = now.Add(a.cfg.ban)
		for k, s := range a.sessions {
			if s.IP == key {
				delete(a.sessions, k)
			}
		}
		if e := a.save(); e != nil {
			slog.Error("persist automatic ban", "error", e)
		}
		slog.Warn("IP automatically banned", "ip", key)
	}
	if v.Count > a.cfg.AttemptsPerMinute {
		ban()
		a.attempts[key] = v
		return 403, "RateBanned", ""
	}
	var matched Code
	valid := false
	if digest != "" {
		for _, c := range a.state.Codes {
			if hmac.Equal([]byte(c.Digest), []byte(digest)) && time.Now().Before(c.Expires) {
				if _, ok := a.roots[c.Alias]; ok {
					matched = c
					valid = true
				}
				break
			}
		}
	}
	if !valid {
		v.Failures++
		a.attempts[key] = v
		slog.Warn("login failed", "ip", key)
		if v.Failures >= a.cfg.MaxFailures {
			ban()
			return 403, "FailureBanned", ""
		}
		return 401, "InvalidCode", ""
	}
	if len(a.sessions) >= 10000 {
		return 503, "SessionLimit", ""
	}
	delete(a.state.Codes, matched.ID)
	if e := a.save(); e != nil {
		a.state.Codes[matched.ID] = matched
		slog.Error("consume code", "error", e)
		return 503, "SaveFailed", ""
	}
	// Preserve IP request counts even after a successful login.
	v.Failures = 0
	a.attempts[key] = v
	if old, e := r.Cookie(cookieName); e == nil {
		delete(a.sessions, old.Value)
	}
	id := token()
	created := time.Now()
	a.sessions[id] = Session{Alias: matched.Alias, CSRF: token(), IP: key, Created: created, Seen: created}
	slog.Info("login succeeded", "ip", key, "alias", matched.Alias, "code_id", matched.ID)
	return 303, "", id
}
func (a *App) logout(w http.ResponseWriter, r *http.Request) {
	s, ok := a.session(r)
	r.Body = http.MaxBytesReader(w, r.Body, 1024)
	if !ok || r.ParseForm() != nil || !hmac.Equal([]byte(r.PostForm.Get("csrf")), []byte(s.CSRF)) {
		webError(w, r, "InvalidLogout", 403)
		return
	}
	c, _ := r.Cookie(cookieName)
	a.mu.Lock()
	delete(a.sessions, c.Value)
	a.mu.Unlock()
	ip, _ := clientIP(r)
	slog.Info("logout succeeded", "ip", ip.String(), "alias", s.Alias)
	http.SetCookie(w, &http.Cookie{Name: cookieName, Value: "", Path: "/", MaxAge: -1, HttpOnly: true, Secure: r.TLS != nil, SameSite: http.SameSiteStrictMode})
	http.Redirect(w, r, "/", 303)
}
func relative(p, prefix string) (string, bool) {
	p = strings.TrimPrefix(p, prefix)
	if strings.Contains(p, "\\") || strings.ContainsRune(p, 0) || strings.HasPrefix(p, "/") {
		return "", false
	}
	for _, part := range strings.Split(p, "/") {
		if part == ".." {
			return "", false
		}
	}
	if p == "" {
		p = "."
	}
	return p, true
}
func (a *App) authorize(w http.ResponseWriter, r *http.Request) (Session, bool) {
	s, ok := a.session(r)
	if !ok {
		http.Redirect(w, r, "/", 303)
	}
	return s, ok
}
func (a *App) browse(w http.ResponseWriter, r *http.Request) {
	s, ok := a.authorize(w, r)
	if !ok {
		return
	}
	name, ok := relative(r.URL.Path, "/browse/")
	if !ok {
		webError(w, r, "NotFound", http.StatusNotFound)
		return
	}
	f, e := a.openFile(r, name)
	if e != nil {
		webError(w, r, "NotFound", http.StatusNotFound)
		return
	}
	defer f.Close()
	list, e := f.ReadDir(maxDirectoryEntries + 1)
	if e != nil && e != io.EOF {
		webError(w, r, "NotFound", http.StatusNotFound)
		return
	}
	if len(list) > maxDirectoryEntries {
		webError(w, r, "DirectoryTooLarge", 503)
		return
	}
	for _, part := range strings.Split(name, "/") {
		if part != "." && strings.HasPrefix(part, ".") {
			webError(w, r, "NotFound", http.StatusNotFound)
			return
		}
	}
	v := view{Alias: s.Alias, CSRF: s.CSRF, Path: name}
	if name != "." {
		v.Parent = (&url.URL{Path: "/browse/" + path.Dir(strings.TrimSuffix(name, "/")) + "/"}).String()
	}
	for _, d := range list {
		if strings.HasPrefix(d.Name(), ".") || d.Type()&os.ModeSymlink != 0 {
			continue
		}
		info, e := d.Info()
		if e != nil || (!info.IsDir() && !info.Mode().IsRegular()) {
			continue
		}
		target := "/file/" + path.Join(name, d.Name())
		if d.IsDir() {
			target = "/browse/" + path.Join(name, d.Name()) + "/"
		}
		v.Entries = append(v.Entries, entry{Name: d.Name(), URL: (&url.URL{Path: target}).String(), Dir: d.IsDir(), Size: info.Size()})
	}
	sort.Slice(v.Entries, func(i, j int) bool {
		if v.Entries[i].Dir != v.Entries[j].Dir {
			return v.Entries[i].Dir
		}
		return v.Entries[i].Name < v.Entries[j].Name
	})
	render(w, r, 200, v)
}
func (a *App) file(w http.ResponseWriter, r *http.Request) {
	_, ok := a.authorize(w, r)
	if !ok {
		return
	}
	ip, _ := clientIP(r)
	if !a.downloads.acquire(ip.String()) {
		w.Header().Set("Retry-After", "5")
		webError(w, r, "Busy", 429)
		return
	}
	defer a.downloads.release(ip.String())
	ranges := r.Header.Values("Range")
	if len(ranges) > 1 || (len(ranges) == 1 && (len(ranges[0]) > 128 || strings.Contains(ranges[0], ","))) {
		webError(w, r, "InvalidRange", 400)
		return
	}
	name, ok := relative(r.URL.Path, "/file/")
	if !ok {
		webError(w, r, "NotFound", http.StatusNotFound)
		return
	}
	for _, part := range strings.Split(name, "/") {
		if strings.HasPrefix(part, ".") {
			webError(w, r, "NotFound", http.StatusNotFound)
			return
		}
	}
	f, e := a.openFile(r, name)
	if e != nil {
		webError(w, r, "NotFound", http.StatusNotFound)
		return
	}
	defer f.Close()
	info, e := f.Stat()
	if e != nil || !info.Mode().IsRegular() {
		webError(w, r, "NotFound", http.StatusNotFound)
		return
	}
	var sample [512]byte
	n, err := f.ReadAt(sample[:], 0)
	if err != nil && err != io.EOF {
		slog.Error("read file preview header", "error", err)
		webError(w, r, "ReadFailed", http.StatusInternalServerError)
		return
	}
	contentType, inline := previewType(name, sample[:n])
	disposition := "inline"
	if !inline || r.URL.Query().Get("download") == "1" {
		disposition = "attachment"
	}
	w.Header().Set("Content-Disposition", mime.FormatMediaType(disposition, map[string]string{"filename": info.Name()}))
	w.Header().Set("Content-Type", contentType)
	http.ServeContent(w, r, info.Name(), info.ModTime(), f)
}
