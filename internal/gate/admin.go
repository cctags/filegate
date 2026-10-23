package gate

import (
	"crypto/rand"
	"encoding/json"
	"errors"
	"fmt"
	"io"
	"math/big"
	"net"
	"os"
	"path/filepath"
	"strings"
	"syscall"
	"time"
)

type Request struct {
	Action string `json:"action"`
	Value  string `json:"value,omitempty"`
	TTL    string `json:"ttl,omitempty"`
}
type Reply struct {
	Error     string               `json:"error,omitempty"`
	Code      string               `json:"code,omitempty"`
	ID        string               `json:"id,omitempty"`
	Expires   time.Time            `json:"expires,omitempty"`
	Codes     []Code               `json:"codes,omitempty"`
	Bans      map[string]time.Time `json:"bans,omitempty"`
	Blacklist []string             `json:"blacklist,omitempty"`
	Sessions  int                  `json:"sessions"`
}

// All state mutations are owned by the running server, including CLI requests.
func (a *App) Admin(r Request) (out Reply) {
	a.mu.Lock()
	defer a.mu.Unlock()
	now := time.Now()
	a.cleanup(now)
	fail := func(e error) Reply { return Reply{Error: e.Error()} }
	switch r.Action {
	case "code":
		if _, ok := a.roots[r.Value]; !ok {
			return fail(errors.New("unknown directory alias"))
		}
		ttl := a.cfg.ttl
		if r.TTL != "" {
			v, e := time.ParseDuration(r.TTL)
			if e != nil || v <= 0 {
				return fail(errors.New("ttl must be a positive duration, e.g. 30m"))
			}
			ttl = v
		}
		if len(a.state.Codes) >= 10000 {
			return fail(errors.New("too many active codes"))
		}
		var plain, digest string
		for {
			n, e := rand.Int(rand.Reader, big.NewInt(100000000))
			if e != nil {
				return fail(e)
			}
			plain = fmt.Sprintf("%s%08d", r.Value, n.Int64())
			digest = a.digest(plain)
			collision := false
			for _, c := range a.state.Codes {
				if c.Digest == digest {
					collision = true
					break
				}
			}
			if !collision {
				break
			}
		}
		c := Code{ID: token()[:16], Alias: r.Value, Digest: digest, Expires: now.Add(ttl)}
		a.state.Codes[c.ID] = c
		if e := a.save(); e != nil {
			delete(a.state.Codes, c.ID)
			return fail(e)
		}
		out.Code = plain
		out.ID = c.ID
		out.Expires = c.Expires
	case "revoke":
		c, ok := a.state.Codes[r.Value]
		if !ok {
			return fail(errors.New("code ID not found"))
		}
		delete(a.state.Codes, r.Value)
		if e := a.save(); e != nil {
			a.state.Codes[c.ID] = c
			return fail(e)
		}
	case "logout":
		if _, ok := a.roots[r.Value]; !ok {
			return fail(errors.New("unknown directory alias"))
		}
		for k, v := range a.sessions {
			if v.Alias == r.Value {
				delete(a.sessions, k)
			}
		}
	case "block", "unblock":
		p, e := parsePrefix(r.Value)
		if e != nil {
			return fail(e)
		}
		list, e := a.blacklist()
		if e != nil {
			return fail(e)
		}
		lines := []string{"# Managed by filegate; individual IPs and CIDR networks are supported."}
		found := false
		for _, v := range list {
			if v == p {
				found = true
				if r.Action == "unblock" {
					continue
				}
			}
			lines = append(lines, v.String())
		}
		if r.Action == "block" && !found {
			lines = append(lines, p.String())
		}
		if e = atomicWrite(a.cfg.Blacklist, []byte(strings.Join(lines, "\n")+"\n")); e != nil {
			return fail(e)
		}
		if r.Action == "block" {
			for k, v := range a.sessions {
				ip, _ := netipAddr(v.IP)
				if p.Contains(ip) {
					delete(a.sessions, k)
				}
			}
		} else {
			for k := range a.state.Bans {
				ip, _ := netipAddr(k)
				if p.Contains(ip) {
					delete(a.state.Bans, k)
				}
			}
			if e = a.save(); e != nil {
				return fail(e)
			}
		}
	case "status":
		for _, c := range a.state.Codes {
			c.Digest = ""
			out.Codes = append(out.Codes, c)
		}
		out.Bans = map[string]time.Time{}
		for k, v := range a.state.Bans {
			out.Bans[k] = v
		}
		list, e := a.blacklist()
		if e != nil {
			return fail(e)
		}
		for _, p := range list {
			out.Blacklist = append(out.Blacklist, p.String())
		}
		out.Sessions = len(a.sessions)
	default:
		return fail(errors.New("unknown admin action"))
	}
	return out
}

func socketPath(c Config) string { return filepath.Join(c.StateDir, "admin.sock") }
func Call(c Config, r Request) (Reply, error) {
	var out Reply
	conn, e := net.DialTimeout("unix", socketPath(c), 3*time.Second)
	if e != nil {
		return out, fmt.Errorf("connect to running service: %w", e)
	}
	defer conn.Close()
	conn.SetDeadline(time.Now().Add(10 * time.Second))
	if e = json.NewEncoder(conn).Encode(r); e != nil {
		return out, e
	}
	e = json.NewDecoder(io.LimitReader(conn, 4<<20)).Decode(&out)
	if e != nil {
		return out, e
	}
	if out.Error != "" {
		return out, errors.New(out.Error)
	}
	return out, nil
}

// Linux flock prevents two servers from overwriting the same state file.
func Lock(c Config) (*os.File, error) {
	if e := os.MkdirAll(c.StateDir, 0700); e != nil {
		return nil, e
	}
	f, e := os.OpenFile(filepath.Join(c.StateDir, "server.lock"), os.O_CREATE|os.O_RDWR, 0600)
	if e != nil {
		return nil, e
	}
	if e = syscall.Flock(int(f.Fd()), syscall.LOCK_EX|syscall.LOCK_NB); e != nil {
		f.Close()
		return nil, errors.New("another server owns this state directory")
	}
	return f, nil
}
func (a *App) AdminListener() (net.Listener, error) {
	path := socketPath(a.cfg)
	if e := os.Remove(path); e != nil && !os.IsNotExist(e) {
		return nil, e
	}
	l, e := net.Listen("unix", path)
	if e != nil {
		return nil, e
	}
	if e = os.Chmod(path, 0600); e != nil {
		l.Close()
		return nil, e
	}
	return l, nil
}
func (a *App) ServeAdmin(l net.Listener) {
	// Local admin operations are serialized; deadlines bound stalled clients.
	for {
		c, e := l.Accept()
		if e != nil {
			return
		}
		c.SetDeadline(time.Now().Add(5 * time.Second))
		var r Request
		d := json.NewDecoder(io.LimitReader(c, 4096))
		d.DisallowUnknownFields()
		if e = d.Decode(&r); e != nil {
			json.NewEncoder(c).Encode(Reply{Error: "invalid request"})
		} else {
			json.NewEncoder(c).Encode(a.Admin(r))
		}
		c.Close()
	}
}
