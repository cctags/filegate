package gate

import (
	"errors"
	"fmt"
	"os"
	"strings"
	"time"
)

// ReloadResult describes changes without exposing credentials or session IDs.
type ReloadResult struct {
	Directories     int
	RevokedCodes    int
	RevokedSessions int
}

// Reload prepares every directory before changing live configuration. Existing
// file descriptors remain valid after their os.Root is closed, so transfers
// already authorized may finish without delaying the switch.
func (a *App) Reload(path string) (ReloadResult, error) {
	var result ReloadResult
	next, err := Load(path)
	if err != nil {
		return result, err
	}
	a.mu.Lock()
	defer a.mu.Unlock()
	if a.closed {
		return result, errors.New("service is closed")
	}
	var restart []string
	for _, item := range []struct{ name, old, next string }{
		{"listen", a.cfg.Listen, next.Listen}, {"root", a.cfg.Root, next.Root},
		{"state_dir", a.cfg.StateDir, next.StateDir}, {"blacklist", a.cfg.Blacklist, next.Blacklist},
		{"tls_cert", a.cfg.TLSCert, next.TLSCert}, {"tls_key", a.cfg.TLSKey, next.TLSKey},
	} {
		if item.old != item.next {
			restart = append(restart, item.name)
		}
	}
	if a.cfg.Logging != next.Logging {
		restart = append(restart, "logging")
	}
	if len(restart) > 0 {
		return result, fmt.Errorf("restart required for configuration changes: %s", strings.Join(restart, ", "))
	}
	if _, err = a.blacklist(); err != nil {
		return result, err
	}
	roots, err := openDirectories(a.root, next.Directories)
	if err != nil {
		return result, err
	}
	applied := false
	defer func() {
		if !applied {
			closeRoots(roots)
		}
	}()
	changed := map[string]bool{}
	for alias, dir := range next.Directories {
		root := roots[alias]
		if old, ok := a.roots[alias]; ok {
			oldInfo, e := old.Stat(".")
			if e != nil {
				return result, e
			}
			newInfo, e := root.Stat(".")
			if e != nil {
				return result, e
			}
			// A replaced directory or retargeted symlink also changes authorization,
			// even if the configured path string has not changed.
			if a.cfg.Directories[alias] != dir || !os.SameFile(oldInfo, newInfo) {
				changed[alias] = true
			}
		}
	}
	for alias := range a.roots {
		if _, ok := roots[alias]; !ok {
			changed[alias] = true
		}
	}
	oldCodes := a.state.Codes
	codes := make(map[string]Code, len(oldCodes))
	for id, code := range oldCodes {
		if changed[code.Alias] || roots[code.Alias] == nil {
			result.RevokedCodes++
			continue
		}
		codes[id] = code
	}
	if result.RevokedCodes > 0 {
		a.state.Codes = codes
		if err = a.save(); err != nil {
			a.state.Codes = oldCodes
			return ReloadResult{}, fmt.Errorf("persist revoked codes: %w", err)
		}
	}
	// Nothing after persistence can fail: publish configuration and revoke
	// affected sessions while file opening and administrative changes are locked.
	oldRoots := a.roots
	a.cfg = next
	a.roots = roots
	for id, session := range a.sessions {
		if changed[session.Alias] || roots[session.Alias] == nil {
			delete(a.sessions, id)
			result.RevokedSessions++
		}
	}
	a.cleanup(time.Now())
	applied = true
	closeRoots(oldRoots)
	result.Directories = len(roots)
	return result, nil
}

func closeRoots(roots map[string]*os.Root) {
	for _, root := range roots {
		root.Close()
	}
}
