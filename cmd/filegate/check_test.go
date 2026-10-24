package main

import (
	"bytes"
	"crypto/sha256"
	"encoding/json"
	"fmt"
	"io/fs"
	"os"
	"path/filepath"
	"reflect"
	"strings"
	"syscall"
	"testing"

	"filegate/internal/gate"
)

// Snapshot names, contents, modes and modification times, including directories.
// Reading files may update access times; those are deliberately not compared.
func checkSnapshot(t *testing.T, base string) map[string]string {
	t.Helper()
	out := map[string]string{}
	err := filepath.WalkDir(base, func(path string, entry fs.DirEntry, err error) error {
		if err != nil {
			return err
		}
		info, err := entry.Info()
		if err != nil {
			return err
		}
		value := fmt.Sprintf("%s %d %d", info.Mode(), info.Size(), info.ModTime().UnixNano())
		if info.Mode().IsRegular() {
			data, err := os.ReadFile(path)
			if err != nil {
				return err
			}
			value += fmt.Sprintf(" %x", sha256.Sum256(data))
		} else if info.Mode()&os.ModeSymlink != 0 {
			target, err := os.Readlink(path)
			if err != nil {
				return err
			}
			value += " " + target
		}
		out[path] = value
		return nil
	})
	if err != nil {
		t.Fatal(err)
	}
	return out
}

func TestCheckReadOnly(t *testing.T) {
	for _, scenario := range []string{
		"fresh", "existing state", "missing runtime directories", "missing config",
		"unknown field", "missing mapping", "overlapping mappings", "duplicate mapping",
		"parent mapping", "config inside content", "state inside content",
		"missing state under content symlink", "state file inside content symlink",
		"missing blacklist", "invalid blacklist", "IPv6 blacklist", "blacklist FIFO",
		"invalid state", "invalid state secret", "invalid state expiry", "state FIFO", "dangling state",
		"state directory is file", "log directory is FIFO", "log file is symlink",
		"valid TLS", "missing TLS", "invalid TLS", "mismatched TLS",
	} {
		t.Run(scenario, func(t *testing.T) {
			base := t.TempDir()
			config := filepath.Join(base, "config.json")
			if err := runArgs([]string{"--config", config, "init"}, &bytes.Buffer{}); err != nil {
				t.Fatal(err)
			}
			cfg, err := gate.Load(config)
			if err != nil {
				t.Fatal(err)
			}
			write := func(path, value string) {
				t.Helper()
				if err := os.WriteFile(path, []byte(value), 0600); err != nil {
					t.Fatal(err)
				}
			}
			remove := func(path string) {
				t.Helper()
				if err := os.Remove(path); err != nil {
					t.Fatal(err)
				}
			}
			symlink := func(target, link string) {
				t.Helper()
				if err := os.Symlink(target, link); err != nil {
					t.Fatal(err)
				}
			}
			fifo := func(path string) {
				t.Helper()
				if err := syscall.Mkfifo(path, 0600); err != nil {
					t.Fatal(err)
				}
			}
			statePath := filepath.Join(cfg.StateDir, "state.json")
			valid := false
			switch scenario {
			case "fresh":
				valid = true
			case "existing state":
				app, err := gate.New(cfg)
				if err != nil {
					t.Fatal(err)
				}
				issued := app.Admin(gate.Request{Action: "code", Value: "ch"})
				app.Close()
				if issued.Error != "" {
					t.Fatal(issued.Error)
				}
				// Startup must not silently tighten permissions during a check.
				if err := os.Chmod(cfg.StateDir, 0750); err != nil {
					t.Fatal(err)
				}
				valid = true
			case "missing runtime directories":
				remove(cfg.StateDir)
				remove(cfg.Logging.Directory)
				cfg.StateDir = filepath.Join(base, "new", "state")
				cfg.Logging.Directory = filepath.Join(base, "new", "logs")
				valid = true
			case "missing config":
				config = filepath.Join(base, "missing", "config.json")
			case "missing mapping":
				cfg.Directories["ch"] = "missing"
			case "overlapping mappings":
				if err := os.Mkdir(filepath.Join(cfg.Root, "ab", "sub"), 0700); err != nil {
					t.Fatal(err)
				}
				cfg.Directories["ch"] = "ab/sub"
			case "duplicate mapping":
				symlink("ab", filepath.Join(cfg.Root, "alias"))
				cfg.Directories["ch"] = "alias"
			case "parent mapping":
				cfg.Directories["ch"] = "ab/../cd"
			case "config inside content":
				write(filepath.Join(cfg.Root, "config.json"), "{}")
				remove(config)
				symlink(filepath.Join(cfg.Root, "config.json"), config)
			case "state inside content":
				cfg.StateDir = filepath.Join(cfg.Root, "new-state")
			case "missing state under content symlink":
				symlink(cfg.Root, filepath.Join(base, "into-content"))
				cfg.StateDir = filepath.Join(base, "into-content", "new", "state")
			case "state file inside content symlink":
				write(filepath.Join(cfg.Root, "state.json"), "{}")
				symlink(filepath.Join(cfg.Root, "state.json"), statePath)
			case "missing blacklist":
				remove(cfg.Blacklist)
			case "invalid blacklist":
				write(cfg.Blacklist, "not-an-IP")
			case "IPv6 blacklist":
				write(cfg.Blacklist, "::1")
			case "blacklist FIFO":
				remove(cfg.Blacklist)
				fifo(cfg.Blacklist)
			case "invalid state":
				write(statePath, "{")
			case "invalid state secret":
				write(statePath, `{"secret":"sensitive-test-value","codes":{},"bans":{}}`)
			case "invalid state expiry":
				write(statePath, `{"secret":"`+strings.Repeat("ab", 32)+`","codes":{},"bans":{"192.0.2.1":"sensitive-test-value"}}`)
			case "state FIFO":
				fifo(statePath)
			case "dangling state":
				symlink(filepath.Join(base, "missing-state"), statePath)
			case "state directory is file":
				remove(cfg.StateDir)
				write(cfg.StateDir, "file")
			case "log directory is FIFO":
				remove(cfg.Logging.Directory)
				fifo(cfg.Logging.Directory)
			case "log file is symlink":
				symlink(cfg.Blacklist, filepath.Join(cfg.Logging.Directory, "filegate.log"))
			case "valid TLS", "missing TLS", "invalid TLS", "mismatched TLS":
				cfg.TLSCert, cfg.TLSKey, _ = testCertificateFiles(t, base)
				switch scenario {
				case "valid TLS":
					valid = true
				case "missing TLS":
					remove(cfg.TLSKey)
				case "invalid TLS":
					write(cfg.TLSKey, "sensitive-test-value")
				case "mismatched TLS":
					other := filepath.Join(base, "other")
					if err := os.Mkdir(other, 0700); err != nil {
						t.Fatal(err)
					}
					_, cfg.TLSKey, _ = testCertificateFiles(t, other)
				}
			}
			if scenario != "missing config" && scenario != "fresh" {
				data, err := json.Marshal(cfg)
				if err != nil {
					t.Fatal(err)
				}
				if scenario == "unknown field" {
					data = append([]byte(`{"unknown":true,`), data[1:]...)
				}
				write(config, string(data))
			}
			before := checkSnapshot(t, base)
			var output bytes.Buffer
			err = runArgsWithEUID([]string{"--config", config, "check"}, &output, 0)
			if (err == nil) != valid {
				t.Fatalf("valid=%v: %v", valid, err)
			}
			if valid && !strings.Contains(output.String(), "Configuration check passed") {
				t.Fatal("missing success output")
			}
			if !valid && output.Len() != 0 {
				t.Fatal("failed check emitted success output")
			}
			if strings.Contains(fmt.Sprint(err)+output.String(), "sensitive-test-value") {
				t.Fatal("credential content exposed")
			}
			if !reflect.DeepEqual(before, checkSnapshot(t, base)) {
				t.Fatal("check modified deployment contents, modes or modification times")
			}
		})
	}
}

func TestCheckInvalidArgumentsBeforeIO(t *testing.T) {
	for _, args := range [][]string{{"check", "extra"}, {"--check"}, {"check", "--ttl", "30m"}, {"check", "--base-url", "http://192.0.2.1"}} {
		var output bytes.Buffer
		if err := runArgs(append([]string{"--config", filepath.Join(t.TempDir(), "missing.json")}, args...), &output); err == nil || output.Len() != 0 || strings.Contains(err.Error(), "no such file") {
			t.Fatalf("argument validation failed: %q: %v", args, err)
		}
	}
}

func TestCheckReadPermissions(t *testing.T) {
	if os.Geteuid() == 0 {
		t.Skip("read-permission checks require a non-root user")
	}
	for _, name := range []string{"blacklist.txt", "content/文件夹", "state/state.json"} {
		t.Run(name, func(t *testing.T) {
			config := filepath.Join(t.TempDir(), "config.json")
			if err := runArgs([]string{"--config", config, "init"}, &bytes.Buffer{}); err != nil {
				t.Fatal(err)
			}
			cfg, err := gate.Load(config)
			if err != nil {
				t.Fatal(err)
			}
			app, err := gate.New(cfg)
			if err != nil {
				t.Fatal(err)
			}
			app.Close()
			path := filepath.Join(filepath.Dir(config), name)
			info, err := os.Stat(path)
			if err != nil {
				t.Fatal(err)
			}
			if err := os.Chmod(path, 0); err != nil {
				t.Fatal(err)
			}
			t.Cleanup(func() {
				if err := os.Chmod(path, info.Mode().Perm()); err != nil {
					t.Error(err)
				}
			})
			var output bytes.Buffer
			if err := runArgs([]string{"--config", config, "check"}, &output); err == nil || output.Len() != 0 {
				t.Fatal("unreadable deployment passed check")
			}
			if current, err := os.Stat(path); err != nil || current.Mode().Perm() != 0 {
				t.Fatal("check changed permissions")
			}
		})
	}
}
