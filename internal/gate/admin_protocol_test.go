package gate

import (
	"context"
	"encoding/json"
	"io"
	"net"
	"os"
	"path/filepath"
	"strings"
	"testing"
	"time"
)

// Retain the production deadline request, but make a stalled peer expire fast.
type fastAdminConn struct {
	net.Conn
	deadline chan time.Duration
}

func (c fastAdminConn) SetDeadline(at time.Time) error {
	c.deadline <- time.Until(at)
	return c.Conn.SetDeadline(time.Now().Add(100 * time.Millisecond))
}

func serveAdminForTest(t *testing.T, a *App, l net.Listener) {
	t.Helper()
	done := make(chan struct{})
	go func() { defer close(done); a.ServeAdmin(l) }()
	t.Cleanup(func() {
		l.Close()
		select {
		case <-done:
		case <-time.After(2 * time.Second):
			t.Error("admin server did not stop")
		}
	})
}

func TestAdminWireRejectsMalformedRequestsAndRecovers(t *testing.T) {
	a := setup(t)
	listener := newPipeListener()
	serveAdminForTest(t, a, listener)
	for _, raw := range []string{"not JSON", `{"action":"status","unknown":1}`, `{"action":"` + strings.Repeat("x", 4096) + `"}`, `{"action":"unknown"}`, `{"action":"code","value":"missing"}`, `{"action":"unblock","value":"192.0.2.1"}`, `{"action":"status"}`} {
		ctx, cancel := context.WithTimeout(context.Background(), time.Second)
		c, err := listener.dial(ctx)
		cancel()
		if err != nil {
			t.Fatal(err)
		}
		c.SetDeadline(time.Now().Add(time.Second))
		written := make(chan struct{})
		go func() { defer close(written); io.WriteString(c, raw) }()
		var out Reply
		err = json.NewDecoder(c).Decode(&out)
		c.Close()
		<-written
		if err != nil {
			t.Fatal(err)
		}
		if strings.Contains(raw, `"action":"status"}`) {
			if out.Error != "" {
				t.Fatal("valid request failed after malformed peer")
			}
		} else if strings.Contains(raw, `"action":"unblock"`) {
			if out.Error != "" || out.Unblock == nil || out.Unblock.TemporaryRemoved != 0 {
				t.Fatal("nested zero result lost on wire")
			}
		} else if out.Error == "" {
			t.Fatal("invalid admin request accepted")
		}
	}
	if len(a.state.Codes) != 0 {
		t.Fatal("invalid request mutated state")
	}
}

func TestAdminWireStalledAndDisconnectedPeers(t *testing.T) {
	a := setup(t)
	l := newPipeListener()
	serveAdminForTest(t, a, l)
	for _, partial := range []bool{false, true} {
		client, server := net.Pipe()
		deadline := make(chan time.Duration, 1)
		select {
		case l.incoming <- fastAdminConn{server, deadline}:
		case <-time.After(time.Second):
			t.Fatal("accept blocked")
		}
		client.SetDeadline(time.Now().Add(time.Second))
		if partial {
			go io.WriteString(client, `{"action":`)
		} else {
			client.Close()
		}
		var out Reply
		if partial {
			if err := json.NewDecoder(client).Decode(&out); err == nil {
				t.Fatal("stalled connection did not expire")
			}
		}
		client.Close()
		select {
		case d := <-deadline:
			if d < 4*time.Second || d > 5*time.Second {
				t.Fatal("production deadline missing")
			}
		case <-time.After(time.Second):
			t.Fatal("no deadline set")
		}
	}
	ctx, cancel := context.WithTimeout(context.Background(), time.Second)
	defer cancel()
	c, err := l.dial(ctx)
	if err != nil {
		t.Fatal("stalled peer blocked next request")
	}
	defer c.Close()
	c.SetDeadline(time.Now().Add(time.Second))
	go json.NewEncoder(c).Encode(Request{Action: "status"})
	if err := json.NewDecoder(c).Decode(new(Reply)); err != nil {
		t.Fatal(err)
	}
}

func TestAdminUnixPermissionsAndRecovery(t *testing.T) {
	a := setup(t)
	// A stale socket from an exited process is safe to replace after the lock.
	stale, err := net.Listen("unix", socketPath(a.cfg))
	if err != nil {
		t.Fatal(err)
	}
	stale.(*net.UnixListener).SetUnlinkOnClose(false)
	stale.Close()
	l, err := a.AdminListener()
	if err != nil {
		t.Fatal(err)
	}
	serveAdminForTest(t, a, l)
	info, err := os.Stat(socketPath(a.cfg))
	if err != nil || info.Mode().Perm() != 0600 {
		t.Fatal("admin socket is not private")
	}
	if _, err := Call(a.cfg, Request{Action: "code", Value: "missing"}); err == nil {
		t.Fatal("business error not propagated")
	}
	out, err := Call(a.cfg, Request{Action: "unblock", Value: "192.0.2.1"})
	if err != nil || out.Unblock == nil || out.Unblock.BlacklistRemoved != 0 {
		t.Fatal("unblock wire result invalid")
	}
	lock, err := Lock(a.cfg)
	if err != nil {
		t.Fatal(err)
	}
	lock.Close()
	lock, err = Lock(a.cfg)
	if err != nil {
		t.Fatal("released lock unavailable")
	}
	lock.Close()
}

func TestAdminUnixSocketWithLongTestName(t *testing.T) {
	t.Run(strings.Repeat("nested-socket-", 8), func(t *testing.T) {
		a := setup(t)
		listener, err := a.AdminListener()
		if err != nil {
			t.Fatal(err)
		}
		serveAdminForTest(t, a, listener)
		if _, err := Call(a.cfg, Request{Action: "status"}); err != nil {
			t.Fatal(err)
		}
	})
}

func TestAdminCallRejectsBrokenReplies(t *testing.T) {
	for _, reply := range []string{"not JSON", `{"error":"controlled failure"}`, `{"error":"` + strings.Repeat("x", 4<<20) + `"}`} {
		t.Run(reply[:min(18, len(reply))], func(t *testing.T) {
			dir, err := os.MkdirTemp(os.Getenv("TMPDIR"), "ipc-")
			if err != nil {
				t.Fatal(err)
			}
			t.Cleanup(func() { os.RemoveAll(dir) })
			cfg := Config{StateDir: dir}
			l, err := net.Listen("unix", filepath.Join(dir, "admin.sock"))
			if err != nil {
				t.Fatal(err)
			}
			done := make(chan struct{})
			go func() {
				defer close(done)
				c, err := l.Accept()
				if err != nil {
					return
				}
				defer c.Close()
				c.SetDeadline(time.Now().Add(time.Second))
				json.NewDecoder(c).Decode(new(Request))
				io.WriteString(c, reply)
			}()
			t.Cleanup(func() {
				l.Close()
				select {
				case <-done:
				case <-time.After(2 * time.Second):
					t.Error("fake server did not stop")
				}
			})
			if _, err := Call(cfg, Request{Action: "status"}); err == nil {
				t.Fatal("broken reply accepted")
			}
		})
	}
	if _, err := Call(Config{StateDir: t.TempDir()}, Request{Action: "status"}); err == nil {
		t.Fatal("missing service accepted")
	}
}
