package gate

import (
	"encoding/json"
	"os"
	"path/filepath"
	"strings"
	"testing"
	"time"
)

func TestUnblockReportsActualChanges(t *testing.T) {
	for _, tc := range []struct {
		name, target, blacklist string
		bans                    []string
		permanent, temporary    int
		stillBlocked            bool
		covering                string
	}{
		{name: "absent", target: "192.0.2.1"},
		{name: "temporary only", target: "192.0.2.1", bans: []string{"192.0.2.1"}, temporary: 1},
		{name: "explicit host prefix", target: "192.0.2.1/32", bans: []string{"192.0.2.1"}, temporary: 1},
		{name: "permanent only", target: "192.0.2.1", blacklist: "192.0.2.1\n", permanent: 1},
		{name: "both", target: "192.0.2.1", blacklist: "192.0.2.1\n", bans: []string{"192.0.2.1"}, permanent: 1, temporary: 1},
		{name: "broader network remains", target: "192.0.2.1", blacklist: "192.0.2.0/24\n192.0.2.1\n", permanent: 1, stillBlocked: true, covering: "192.0.2.0/24"},
		{name: "only broader network", target: "192.0.2.1", blacklist: "192.0.2.0/24\n", stillBlocked: true, covering: "192.0.2.0/24"},
		{name: "network", target: "192.0.2.99/24", blacklist: "192.0.2.0/24\n192.0.2.5\n", bans: []string{"192.0.2.1", "192.0.2.2"}, permanent: 1, temporary: 2},
	} {
		t.Run(tc.name, func(t *testing.T) {
			a := setup(t)
			if err := os.WriteFile(a.cfg.Blacklist, []byte(tc.blacklist), 0600); err != nil {
				t.Fatal(err)
			}
			for _, ip := range append(tc.bans, "198.51.100.1") {
				a.state.Bans[ip] = time.Now().Add(time.Hour)
			}
			out := a.Admin(Request{Action: "unblock", Value: tc.target})
			if out.Error != "" || out.Unblock == nil {
				t.Fatalf("reply: %+v", out)
			}
			got := out.Unblock
			if got.BlacklistRemoved != tc.permanent || got.TemporaryRemoved != tc.temporary {
				t.Fatalf("counts: %+v", got)
			}
			prefix, _ := parsePrefix(tc.target)
			if prefix.Bits() != 32 {
				if got.StillBlocked != nil {
					t.Fatal("network result must not claim all addresses are unblocked")
				}
			} else if got.StillBlocked == nil || *got.StillBlocked != tc.stillBlocked {
				t.Fatalf("blocked: %+v", got)
			}
			if tc.covering != "" && (len(got.CoveringBlacklist) != 1 || got.CoveringBlacklist[0] != tc.covering) {
				t.Fatalf("covering: %+v", got)
			}
			if tc.permanent == 0 && tc.temporary == 0 && !strings.Contains(got.Message, "No matching ban records") {
				t.Fatal(got.Message)
			}
			if _, ok := a.state.Bans["198.51.100.1"]; !ok {
				t.Fatal("unrelated ban removed")
			}
			for _, ip := range tc.bans {
				if _, ok := a.state.Bans[ip]; ok {
					t.Fatal("matching ban remains")
				}
			}
			if tc.permanent == 0 {
				data, err := os.ReadFile(a.cfg.Blacklist)
				if err != nil || string(data) != tc.blacklist {
					t.Fatal("unchanged blacklist rewritten")
				}
			}
			repeated := a.Admin(Request{Action: "unblock", Value: tc.target})
			if repeated.Error != "" || repeated.Unblock.BlacklistRemoved != 0 || repeated.Unblock.TemporaryRemoved != 0 {
				t.Fatalf("repeat: %+v", repeated)
			}
			data, err := json.Marshal(repeated)
			if err != nil || !strings.Contains(string(data), `"blacklist_removed":0`) || !strings.Contains(string(data), `"temporary_removed":0`) {
				t.Fatalf("zero counts omitted: %s, %v", data, err)
			}
		})
	}
}

func TestUnblockRestoresTemporaryBansOnSaveFailure(t *testing.T) {
	a := setup(t)
	ip := "192.0.2.1"
	expiry := time.Now().Add(time.Hour)
	a.state.Bans[ip] = expiry
	// An existing directory cannot be replaced by the atomic state-file rename.
	statePath := filepath.Join(a.cfg.StateDir, "state.json")
	if err := os.Rename(statePath, statePath+".backup"); err != nil {
		t.Fatal(err)
	}
	if err := os.Mkdir(statePath, 0700); err != nil {
		t.Fatal(err)
	}
	out := a.Admin(Request{Action: "unblock", Value: ip})
	if out.Error == "" || out.Unblock != nil {
		t.Fatalf("reply: %+v", out)
	}
	if a.state.Bans[ip] != expiry {
		t.Fatal("failed persistence removed in-memory ban")
	}
}
