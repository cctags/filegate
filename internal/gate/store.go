package gate

import (
	"crypto/hmac"
	"crypto/rand"
	"crypto/sha256"
	"encoding/hex"
	"encoding/json"
	"errors"
	"fmt"
	"net/netip"
	"os"
	"path/filepath"
	"strings"
	"sync"
	"time"
)

type Code struct {
	ID      string    `json:"id"`
	Alias   string    `json:"alias"`
	Digest  string    `json:"digest"`
	Expires time.Time `json:"expires"`
}
type State struct {
	Secret string               `json:"secret"`
	Codes  map[string]Code      `json:"codes"`
	Bans   map[string]time.Time `json:"bans"`
}
type Session struct {
	Alias, CSRF, IP string
	Created, Seen   time.Time
}
type attempts struct {
	Since, FailureSince time.Time
	Count, Failures     int
}
type App struct {
	cfg         Config
	mu          sync.Mutex
	state       State
	sessions    map[string]Session
	attempts    map[string]attempts
	roots       map[string]*os.Root
	root        *os.Root
	globalSince time.Time
	globalCount int
}

func token() string { b := make([]byte, 32); rand.Read(b); return hex.EncodeToString(b) }
func New(c Config) (*App, error) {
	if err := os.MkdirAll(c.StateDir, 0700); err != nil {
		return nil, err
	}
	if err := os.Chmod(c.StateDir, 0700); err != nil {
		return nil, err
	}
	a := &App{cfg: c, sessions: map[string]Session{}, attempts: map[string]attempts{}, roots: map[string]*os.Root{}}
	b, err := os.ReadFile(filepath.Join(c.StateDir, "state.json"))
	if os.IsNotExist(err) {
		a.state = State{Secret: token(), Codes: map[string]Code{}, Bans: map[string]time.Time{}}
		err = a.save()
	} else if err == nil {
		err = json.Unmarshal(b, &a.state)
	}
	if err != nil {
		return nil, err
	}
	secret, e := hex.DecodeString(a.state.Secret)
	if e != nil || len(secret) != 32 || a.state.Codes == nil || a.state.Bans == nil {
		return nil, errors.New("invalid state file")
	}
	// Sensitive files must never be reachable through the content root.
	contentPath, err := filepath.EvalSymlinks(c.Root)
	if err != nil {
		return nil, err
	}
	protected := []string{c.StateDir, c.Blacklist}
	if c.TLSKey != "" {
		protected = append(protected, c.TLSKey)
	}
	for _, p := range protected {
		actual, e := filepath.EvalSymlinks(p)
		if e != nil {
			return nil, e
		}
		rel, e := filepath.Rel(contentPath, actual)
		if e != nil {
			return nil, e
		}
		if filepath.IsLocal(rel) {
			return nil, errors.New("state, blacklist and TLS private key must be outside the content root")
		}
	}
	a.root, err = os.OpenRoot(c.Root)
	if err != nil {
		return nil, err
	}
	for alias, dir := range c.Directories {
		r, e := a.root.OpenRoot(dir)
		if e != nil {
			a.Close()
			return nil, fmt.Errorf("directory %s: %w", alias, e)
		}
		a.roots[alias] = r
	}
	if _, err = a.blacklist(); err != nil {
		a.Close()
		return nil, err
	}
	return a, nil
}
func (a *App) Close() {
	for _, r := range a.roots {
		r.Close()
	}
	if a.root != nil {
		a.root.Close()
	}
}
func (a *App) save() error {
	b, e := json.MarshalIndent(a.state, "", "  ")
	if e != nil {
		return e
	}
	return atomicWrite(filepath.Join(a.cfg.StateDir, "state.json"), b)
}
func (a *App) digest(value string) string {
	key, _ := hex.DecodeString(a.state.Secret)
	h := hmac.New(sha256.New, key)
	h.Write([]byte(value))
	return hex.EncodeToString(h.Sum(nil))
}
func (a *App) blacklist() ([]netip.Prefix, error) {
	b, e := os.ReadFile(a.cfg.Blacklist)
	if os.IsNotExist(e) {
		return nil, errors.New("blacklist file missing")
	}
	if e != nil {
		return nil, e
	}
	var out []netip.Prefix
	for n, line := range strings.Split(string(b), "\n") {
		line = strings.TrimSpace(strings.SplitN(line, "#", 2)[0])
		if line == "" {
			continue
		}
		p, e := parsePrefix(line)
		if e != nil {
			return nil, fmt.Errorf("blacklist line %d: %w", n+1, e)
		}
		out = append(out, p)
	}
	return out, nil
}
func parsePrefix(s string) (netip.Prefix, error) {
	if ip, e := netip.ParseAddr(s); e == nil {
		ip = ip.Unmap()
		return netip.PrefixFrom(ip, ip.BitLen()), nil
	}
	p, e := netip.ParsePrefix(s)
	if e != nil {
		return p, e
	}
	if p.Addr().Is4In6() {
		return netip.Prefix{}, errors.New("use IPv4 notation instead of mapped IPv6 CIDR")
	}
	return p.Masked(), nil
}
func (a *App) blocked(ip netip.Addr) (bool, error) {
	list, e := a.blacklist()
	if e != nil {
		return true, e
	}
	for _, p := range list {
		if p.Contains(ip) {
			return true, nil
		}
	}
	return time.Now().Before(a.state.Bans[ip.String()]), nil
}
func (a *App) cleanup(now time.Time) {
	for k, v := range a.sessions {
		if now.Sub(v.Seen) >= a.cfg.idle || now.Sub(v.Created) >= a.cfg.max {
			delete(a.sessions, k)
		}
	}
	for k, v := range a.attempts {
		if now.Sub(v.Since) > time.Minute && now.Sub(v.FailureSince) > a.cfg.window {
			delete(a.attempts, k)
		}
	}
	for k, v := range a.state.Codes {
		if !now.Before(v.Expires) {
			delete(a.state.Codes, k)
		}
	}
	for k, v := range a.state.Bans {
		if !now.Before(v) {
			delete(a.state.Bans, k)
		}
	}
}
