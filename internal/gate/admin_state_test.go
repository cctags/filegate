package gate

import (
	"bytes"
	"encoding/json"
	"log/slog"
	"os"
	"path/filepath"
	"strings"
	"sync"
	"testing"
	"time"
)

func assertStatusRedacted(t *testing.T, a *App, issued, status Reply) {
	t.Helper()
	if status.Error != "" || len(status.Codes) != 1 {
		t.Fatalf("missing issued code in status: %+v", status)
	}
	c := status.Codes[0]
	if c.ID != issued.ID || c.Alias != "ab" || !c.Expires.Equal(issued.Expires) || c.Digest != "" {
		t.Fatal("status lost code metadata or exposed its digest")
	}
	data, err := json.Marshal(status)
	if err != nil {
		t.Fatal(err)
	}
	for _, secret := range []string{issued.Code, a.digest(issued.Code), a.state.Secret} {
		if secret == "" || bytes.Contains(data, []byte(secret)) {
			t.Fatal("status exposes a credential")
		}
	}
}

func TestStatusRedactsActualCredentials(t *testing.T) {
	a := setup(t)
	issued := createCode(t, a, "ab")
	assertStatusRedacted(t, a, issued, a.Admin(Request{Action: "status"}))
	if a.state.Codes[issued.ID].Digest != a.digest(issued.Code) {
		t.Fatal("status mutated stored digest")
	}
}

func diskState(t *testing.T, a *App) State {
	t.Helper()
	data, err := os.ReadFile(filepath.Join(a.cfg.StateDir, "state.json"))
	if err != nil {
		t.Fatal(err)
	}
	var state State
	if err := json.Unmarshal(data, &state); err != nil {
		t.Fatal(err)
	}
	return state
}

// Fail the rename stage without relying on permissions or running as root.
func failStateSave(t *testing.T, a *App) func() {
	t.Helper()
	path := filepath.Join(a.cfg.StateDir, "state.json")
	if err := os.Rename(path, path+".saved"); err != nil {
		t.Fatal(err)
	}
	if err := os.Mkdir(path, 0700); err != nil {
		t.Fatal(err)
	}
	var once sync.Once
	restore := func() {
		once.Do(func() {
			if err := os.Remove(path); err != nil {
				t.Fatal(err)
			}
			if err := os.Rename(path+".saved", path); err != nil {
				t.Fatal(err)
			}
		})
	}
	t.Cleanup(restore)
	return restore
}

func TestAdminPersistenceFailures(t *testing.T) {
	for _, action := range []string{"code", "revoke"} {
		t.Run(action, func(t *testing.T) {
			a := setup(t)
			issued := createCode(t, a, "ab")
			restore := failStateSave(t, a)
			value := "ab"
			if action == "revoke" {
				value = issued.ID
			}
			out := a.Admin(Request{Action: action, Value: value})
			if out.Error == "" || out.Code != "" || out.ID != "" || len(a.state.Codes) != 1 || a.state.Codes[issued.ID].Digest != a.digest(issued.Code) {
				t.Fatal("failed mutation changed usable codes or returned credentials")
			}
			restore()
			if len(diskState(t, a).Codes) != 1 {
				t.Fatal("failed save changed disk state")
			}
			a.Close()
			b, err := New(a.cfg)
			if err != nil {
				t.Fatal(err)
			}
			defer b.Close()
			loginCode(t, b, issued.Code)
		})
	}
}

func TestUnblockDurabilityAndPartialFailure(t *testing.T) {
	for _, fail := range []bool{false, true} {
		t.Run(map[bool]string{false: "durable", true: "partial failure"}[fail], func(t *testing.T) {
			a := setup(t)
			ip := "192.0.2.1"
			a.state.Bans[ip] = time.Now().Add(time.Hour)
			a.state.Bans["192.0.2.2"] = time.Now().Add(-time.Second)
			if err := a.save(); err != nil {
				t.Fatal(err)
			}
			if err := os.WriteFile(a.cfg.Blacklist, []byte(ip+"\n"+ip+"/32\n198.51.100.0/24\n"), 0600); err != nil {
				t.Fatal(err)
			}
			restore := func() {}
			if fail {
				restore = failStateSave(t, a)
			}
			out := a.Admin(Request{Action: "unblock", Value: ip})
			data, err := os.ReadFile(a.cfg.Blacklist)
			if err != nil || strings.Contains(string(data), ip) || !strings.Contains(string(data), "198.51.100.0/24") {
				t.Fatal("permanent changes not applied")
			}
			if fail {
				if out.Error == "" || out.Unblock != nil || a.state.Bans[ip].IsZero() {
					t.Fatal("partial failure falsely succeeded or lost ban")
				}
				restore()
				if diskState(t, a).Bans[ip].IsZero() {
					t.Fatal("failed save changed durable ban")
				}
				out = a.Admin(Request{Action: "unblock", Value: ip})
			}
			if out.Error != "" || out.Unblock == nil || out.Unblock.TemporaryRemoved != 1 || *out.Unblock.StillBlocked {
				t.Fatalf("result: %+v", out)
			}
			if !fail && out.Unblock.BlacklistRemoved != 2 {
				t.Fatal("duplicate permanent records not counted")
			}
			if len(diskState(t, a).Bans) != 0 {
				t.Fatal("deleted/expired bans remain on disk")
			}
			a.Close()
			b, err := New(a.cfg)
			if err != nil {
				t.Fatal(err)
			}
			defer b.Close()
			if blocked, err := b.blocked(mustIP(t, ip)); err != nil || blocked {
				t.Fatal("ban returned after restart")
			}
			out = b.Admin(Request{Action: "unblock", Value: "192.0.2.2"})
			if out.Unblock.TemporaryRemoved != 0 {
				t.Fatal("expired ban counted as active")
			}
		})
	}
}

func TestAutomaticBanSaveFailure(t *testing.T) {
	var output bytes.Buffer
	old := slog.Default()
	slog.SetDefault(slog.New(slog.NewTextHandler(&output, nil)))
	defer slog.SetDefault(old)
	a := setup(t)
	cookie := access(t, a)
	restore := failStateSave(t, a)
	for range a.cfg.MaxFailures {
		request(a, "POST", "/login", "code=wrong", nil)
	}
	if request(a, "GET", "/", "", nil).Code != 403 || len(a.sessions) != 0 {
		t.Fatal("save failure must preserve in-memory ban and revoke sessions")
	}
	if !strings.Contains(output.String(), "persist automatic ban") || strings.Contains(output.String(), cookie.Value) {
		t.Fatal("missing error or leaked credential")
	}
	restore()
	if len(diskState(t, a).Bans) != 0 {
		t.Fatal("failed ban unexpectedly persisted")
	}
	a.Close()
	b, err := New(a.cfg)
	if err != nil {
		t.Fatal(err)
	}
	defer b.Close()
	if request(b, "GET", "/", "", nil).Code != 200 {
		t.Fatal("failed ban should not survive restart")
	}
}

func TestUnblockIgnoresExpiredMatches(t *testing.T) {
	a := setup(t)
	ip := "192.0.2.1"
	a.state.Bans[ip] = time.Now().Add(-time.Second)
	if err := a.save(); err != nil {
		t.Fatal(err)
	}
	out := a.Admin(Request{Action: "unblock", Value: ip})
	if out.Error != "" || out.Unblock == nil || out.Unblock.TemporaryRemoved != 0 || !strings.Contains(out.Unblock.Message, "No matching") {
		t.Fatal("expired matching ban counted as removed")
	}
	if _, ok := a.state.Bans[ip]; ok {
		t.Fatal("expired in-memory ban not reclaimed")
	}
	// No extra state write is needed for an already expired, ineffective record.
	if _, ok := diskState(t, a).Bans[ip]; !ok {
		t.Fatal("no-op unblock unexpectedly saved state")
	}
}

func TestAdminLogoutAndInvalidRequests(t *testing.T) {
	a := setup(t)
	one, two := access(t, a), access(t, a)
	other := loginCode(t, a, createCode(t, a, "cd").Code)
	if out := a.Admin(Request{Action: "logout", Value: "ab"}); out.Error != "" {
		t.Fatal(out.Error)
	}
	for _, cookie := range []string{one.Value, two.Value} {
		if _, ok := a.sessions[cookie]; ok {
			t.Fatal("admin logout kept session")
		}
	}
	if _, ok := a.sessions[other.Value]; !ok {
		t.Fatal("unrelated session revoked")
	}
	for _, req := range []Request{{Action: "logout", Value: "missing"}, {Action: "code", Value: "missing"}, {Action: "code", Value: "ab", TTL: "0s"}, {Action: "code", Value: "ab", TTL: "invalid"}, {Action: "block", Value: "::1"}, {Action: "unblock", Value: "bad"}, {Action: "revoke", Value: "missing"}} {
		if out := a.Admin(req); out.Error == "" {
			t.Fatalf("accepted %+v", req)
		}
	}
}
