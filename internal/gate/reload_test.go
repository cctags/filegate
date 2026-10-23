package gate

import (
	"encoding/json"
	"net/http"
	"net/http/httptest"
	"os"
	"path/filepath"
	"strings"
	"sync"
	"testing"
	"time"
)

func reloadConfig(t *testing.T, a *App) (string, Config) {
	t.Helper()
	a.mu.Lock()
	c := a.cfg
	a.mu.Unlock()
	c.Listen = "127.0.0.1:8080"
	c.CodeTTL = c.ttl.String()
	c.IdleTimeout = c.idle.String()
	c.MaxSession = c.max.String()
	c.FailureWindow = c.window.String()
	c.BanDuration = c.ban.String()
	c.Directories = map[string]string{}
	for k, v := range a.cfg.Directories {
		c.Directories[k] = v
	}
	a.mu.Lock()
	a.cfg = c
	a.mu.Unlock()
	file := filepath.Join(filepath.Dir(c.Root), "config.json")
	writeReloadConfig(t, file, c)
	return file, c
}
func writeReloadConfig(t *testing.T, file string, c Config) {
	t.Helper()
	b, e := json.Marshal(c)
	if e != nil {
		t.Fatal(e)
	}
	if e = os.WriteFile(file, b, 0600); e != nil {
		t.Fatal(e)
	}
}
func createCode(t *testing.T, a *App, alias string) Reply {
	t.Helper()
	r := a.Admin(Request{Action: "code", Value: alias})
	if r.Error != "" {
		t.Fatal(r.Error)
	}
	return r
}
func loginCode(t *testing.T, a *App, code string) *http.Cookie {
	t.Helper()
	r := request(a, http.MethodPost, "/login", "code="+code, nil)
	if r.Code != 303 {
		t.Fatalf("login failed: %d %s", r.Code, r.Body.String())
	}
	return r.Result().Cookies()[0]
}

func TestReloadAddsChineseDirectory(t *testing.T) {
	a := setup(t)
	file, c := reloadConfig(t, a)
	cookie := access(t, a)
	unused := createCode(t, a, "ab")
	dir := filepath.Join(c.Root, "中文测试")
	if e := os.Mkdir(dir, 0700); e != nil {
		t.Fatal(e)
	}
	if e := os.WriteFile(filepath.Join(dir, "资料.txt"), []byte("中文内容"), 0600); e != nil {
		t.Fatal(e)
	}
	c.Directories = map[string]string{"ab": "ab", "cd": "cd", "cjk": "中文测试"}
	writeReloadConfig(t, file, c)
	result, e := a.Reload(file)
	if e != nil {
		t.Fatal(e)
	}
	if result.Directories != 3 || result.RevokedCodes != 0 || result.RevokedSessions != 0 {
		t.Fatalf("unexpected changes: %+v", result)
	}
	if request(a, "GET", "/file/hello.txt", "", cookie).Code != 200 {
		t.Fatal("unchanged session lost")
	}
	if _, ok := a.state.Codes[unused.ID]; !ok {
		t.Fatal("unchanged code revoked")
	}
	r := createCode(t, a, "cjk")
	chinese := loginCode(t, a, r.Code)
	if response := request(a, "GET", "/file/资料.txt", "", chinese); response.Code != 200 || response.Body.String() != "中文内容" {
		t.Fatalf("Chinese directory unavailable: %d %s", response.Code, response.Body.String())
	}
}

func TestReloadRevokesChangedDirectory(t *testing.T) {
	a := setup(t)
	file, c := reloadConfig(t, a)
	oldSession := access(t, a)
	oldCode := createCode(t, a, "ab")
	unaffectedCode := createCode(t, a, "cd")
	unaffectedSession := loginCode(t, a, createCode(t, a, "cd").Code)
	// A request may already have passed its first authorization check.
	r := httptest.NewRequest("GET", "/file/secret.txt", nil)
	r.AddCookie(oldSession)
	if _, ok := a.session(r); !ok {
		t.Fatal("initial authorization failed")
	}
	// Open descriptors belong to the original directory and may finish safely.
	inFlight, e := a.roots["ab"].Open("hello.txt")
	if e != nil {
		t.Fatal(e)
	}
	defer inFlight.Close()
	c.Directories = map[string]string{"ab": "cd", "cd": "cd"}
	writeReloadConfig(t, file, c)
	result, e := a.Reload(file)
	if e != nil {
		t.Fatal(e)
	}
	if result.RevokedCodes != 1 || result.RevokedSessions != 1 {
		t.Fatalf("unexpected revocations: %+v", result)
	}
	if _, ok := a.sessions[oldSession.Value]; ok {
		t.Fatal("old session retained")
	}
	if _, ok := a.state.Codes[oldCode.ID]; ok {
		t.Fatal("old code retained")
	}
	if _, e := a.openFile(r, "secret.txt"); e == nil {
		t.Fatal("old request accessed newly mapped directory")
	}
	if request(a, "GET", "/file/secret.txt", "", unaffectedSession).Code != 200 {
		t.Fatal("unaffected session revoked")
	}
	if _, ok := a.state.Codes[unaffectedCode.ID]; !ok {
		t.Fatal("unaffected code revoked")
	}
	buf := make([]byte, 11)
	if _, e := inFlight.Read(buf); e != nil || string(buf) != "hello world" {
		t.Fatalf("in-flight descriptor lost: %q %v", buf, e)
	}
	// Revocations must survive restarting the server.
	restarted, e := New(a.cfg)
	if e != nil {
		t.Fatal(e)
	}
	defer restarted.Close()
	if _, ok := restarted.state.Codes[oldCode.ID]; ok {
		t.Fatal("revoked code reappeared after restart")
	}
	fresh := loginCode(t, a, createCode(t, a, "ab").Code)
	if request(a, "GET", "/file/secret.txt", "", fresh).Code != 200 {
		t.Fatal("new mapping unavailable")
	}
}

func TestReloadRemovesDirectory(t *testing.T) {
	a := setup(t)
	file, c := reloadConfig(t, a)
	session := access(t, a)
	code := createCode(t, a, "ab")
	c.Directories = map[string]string{"cd": "cd"}
	writeReloadConfig(t, file, c)
	result, e := a.Reload(file)
	if e != nil {
		t.Fatal(e)
	}
	if result.RevokedCodes != 1 || result.RevokedSessions != 1 {
		t.Fatalf("unexpected result: %+v", result)
	}
	if request(a, "GET", "/browse/", "", session).Code != 303 {
		t.Fatal("removed directory still authorized")
	}
	if a.Admin(Request{Action: "code", Value: "ab"}).Error == "" {
		t.Fatal("code generated for removed directory")
	}
	if _, ok := a.state.Codes[code.ID]; ok {
		t.Fatal("removed directory code retained")
	}
}

func TestReloadRejectsInvalidConfiguration(t *testing.T) {
	cases := []struct {
		name   string
		change func(*Config)
	}{
		{"port", func(c *Config) { c.Listen = "127.0.0.1:9999" }},
		{"root", func(c *Config) { c.Root = filepath.Dir(c.Root) }},
		{"state", func(c *Config) { c.StateDir = filepath.Join(filepath.Dir(c.StateDir), "other-state") }},
		{"blacklist", func(c *Config) { c.Blacklist += ".new" }},
		{"tls", func(c *Config) { c.TLSCert = "new.crt"; c.TLSKey = "new.key" }},
		{"missing directory", func(c *Config) { c.Directories = map[string]string{"ab": "missing"} }},
		{"directory escape", func(c *Config) { c.Directories = map[string]string{"ab": "../state"} }},
		{"invalid duration", func(c *Config) { c.CodeTTL = "invalid" }},
	}
	for _, tc := range cases {
		t.Run(tc.name, func(t *testing.T) {
			a := setup(t)
			file, c := reloadConfig(t, a)
			session := access(t, a)
			code := createCode(t, a, "ab")
			tc.change(&c)
			writeReloadConfig(t, file, c)
			if _, e := a.Reload(file); e == nil {
				t.Fatal("invalid reload accepted")
			}
			if request(a, "GET", "/file/hello.txt", "", session).Code != 200 {
				t.Fatal("failed reload affected session")
			}
			if _, ok := a.state.Codes[code.ID]; !ok {
				t.Fatal("failed reload revoked code")
			}
		})
	}
	a := setup(t)
	file, _ := reloadConfig(t, a)
	session := access(t, a)
	os.WriteFile(file, []byte("{broken"), 0600)
	if _, e := a.Reload(file); e == nil {
		t.Fatal("malformed JSON accepted")
	}
	if request(a, "GET", "/file/hello.txt", "", session).Code != 200 {
		t.Fatal("malformed JSON affected service")
	}
}

func TestReloadRollsBackOnPersistenceFailure(t *testing.T) {
	a := setup(t)
	file, c := reloadConfig(t, a)
	session := access(t, a)
	code := createCode(t, a, "ab")
	c.Directories = map[string]string{"ab": "cd", "cd": "cd"}
	writeReloadConfig(t, file, c)
	// Make the final rename fail without depending on permissions or root status.
	state := filepath.Join(c.StateDir, "state.json")
	if e := os.Rename(state, state+".backup"); e != nil {
		t.Fatal(e)
	}
	if e := os.Mkdir(state, 0700); e != nil {
		t.Fatal(e)
	}
	if _, e := a.Reload(file); e == nil {
		t.Fatal("reload succeeded without durable revocation")
	}
	if a.cfg.Directories["ab"] != "ab" {
		t.Fatal("configuration changed after failed persistence")
	}
	if _, ok := a.state.Codes[code.ID]; !ok {
		t.Fatal("code not restored")
	}
	if request(a, "GET", "/file/hello.txt", "", session).Code != 200 {
		t.Fatal("old root or session lost")
	}
}

func TestReloadDetectsReplacedDirectory(t *testing.T) {
	a := setup(t)
	file, c := reloadConfig(t, a)
	session := access(t, a)
	createCode(t, a, "ab")
	if e := os.Rename(filepath.Join(c.Root, "ab"), filepath.Join(c.Root, "old-ab")); e != nil {
		t.Fatal(e)
	}
	if e := os.Mkdir(filepath.Join(c.Root, "ab"), 0700); e != nil {
		t.Fatal(e)
	}
	result, e := a.Reload(file)
	if e != nil {
		t.Fatal(e)
	}
	if result.RevokedCodes != 1 || result.RevokedSessions != 1 {
		t.Fatalf("replaced directory retained authorization: %+v", result)
	}
	if request(a, "GET", "/browse/", "", session).Code != 303 {
		t.Fatal("old cookie still works")
	}
}

func TestReloadAppliesRuntimeLimits(t *testing.T) {
	a := setup(t)
	file, c := reloadConfig(t, a)
	session := access(t, a)
	unused := createCode(t, a, "ab")
	a.mu.Lock()
	s := a.sessions[session.Value]
	s.Seen = time.Now().Add(-5 * time.Second)
	a.sessions[session.Value] = s
	a.mu.Unlock()
	c.CodeTTL = "45m"
	c.IdleTimeout = "1s"
	c.MaxSession = "2h"
	c.FailureWindow = "5m"
	c.BanDuration = "2h"
	c.MaxFailures = 9
	c.AttemptsPerMinute = 15
	writeReloadConfig(t, file, c)
	if _, e := a.Reload(file); e != nil {
		t.Fatal(e)
	}
	if request(a, "GET", "/browse/", "", session).Code != 303 {
		t.Fatal("new idle timeout not applied")
	}
	fresh := createCode(t, a, "ab")
	if ttl := time.Until(fresh.Expires); ttl < 44*time.Minute || ttl > 46*time.Minute {
		t.Fatalf("new code TTL not applied: %v", ttl)
	}
	if !a.state.Codes[unused.ID].Expires.Equal(unused.Expires) {
		t.Fatal("existing code expiry changed")
	}
	if a.cfg.MaxFailures != 9 || a.cfg.AttemptsPerMinute != 15 || a.cfg.ban != 2*time.Hour || a.cfg.window != 5*time.Minute || a.cfg.max != 2*time.Hour {
		t.Fatal("runtime limits not applied")
	}
}

func TestReloadConcurrentRequests(t *testing.T) {
	a := setup(t)
	file, c := reloadConfig(t, a)
	cookie := access(t, a)
	// Change only an unrelated alias while requests continue through ab.
	c.Directories = map[string]string{"ab": "ab", "cd": "cd", "extra": "cd"}
	writeReloadConfig(t, file, c)
	var wg sync.WaitGroup
	for range 3 {
		wg.Add(1)
		go func() {
			defer wg.Done()
			for range 25 {
				w := request(a, "GET", "/file/hello.txt", "", cookie)
				if w.Code != 200 || !strings.Contains(w.Body.String(), "hello world") {
					t.Errorf("request failed during reload: %d", w.Code)
					return
				}
			}
		}()
	}
	for range 15 {
		if _, e := a.Reload(file); e != nil {
			t.Fatal(e)
		}
	}
	wg.Wait()
}
