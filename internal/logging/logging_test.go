package logging

import (
	"bytes"
	"fmt"
	"os"
	"path/filepath"
	"strings"
	"sync"
	"testing"
	"time"
)

func testConfig(t *testing.T) Config {
	t.Helper()
	c := Defaults()
	c.Directory = filepath.Join(t.TempDir(), "logs")
	return c
}
func archives(t *testing.T, dir string) []string {
	t.Helper()
	entries, e := os.ReadDir(dir)
	if e != nil {
		t.Fatal(e)
	}
	var names []string
	for _, entry := range entries {
		if archiveName(entry.Name()) {
			names = append(names, entry.Name())
		}
	}
	return names
}

func TestDualOutputAndLevels(t *testing.T) {
	c := testConfig(t)
	c.Color = "always"
	var console bytes.Buffer
	service, e := Start(c, filepath.Join(filepath.Dir(c.Directory), "content"), &console)
	if e != nil {
		t.Fatal(e)
	}
	defer service.Close()
	service.Logger.Debug("hidden")
	service.Logger.Info("message level=DEBUG", "path", "中文.txt")
	service.Logger.With("alias", "ch").WithGroup("request").Warn("blocked", "status", 403)
	data, e := os.ReadFile(filepath.Join(c.Directory, "filegate.log"))
	if e != nil {
		t.Fatal(e)
	}
	if strings.Contains(console.String(), "hidden") || bytes.Contains(data, []byte("hidden")) {
		t.Fatal("level filtering failed")
	}
	if !strings.Contains(console.String(), "level=\x1b[1;32mINFO\x1b[0m") || !strings.Contains(console.String(), "level=\x1b[1;33mWARN\x1b[0m") {
		t.Fatalf("missing colored levels: %q", console.String())
	}
	if bytes.Contains(data, []byte("\x1b")) {
		t.Fatal("file contains terminal controls")
	}
	if !bytes.Contains(data, []byte("request.status=403")) || !bytes.Contains(data, []byte("alias=ch")) {
		t.Fatalf("attributes/groups lost: %s", data)
	}
	info, _ := os.Stat(filepath.Join(c.Directory, "filegate.log"))
	if info.Mode().Perm() != 0600 {
		t.Fatal("log file permissions not private")
	}
	info, _ = os.Stat(c.Directory)
	if info.Mode().Perm() != 0700 {
		t.Fatal("log directory permissions not private")
	}
}

func TestColorAutoAndOverrides(t *testing.T) {
	var buffer bytes.Buffer
	if useColor("auto", &buffer) {
		t.Fatal("redirected output should not be colored")
	}
	t.Setenv("NO_COLOR", "")
	if useColor("auto", os.Stderr) {
		t.Fatal("NO_COLOR ignored")
	}
	if !useColor("always", &buffer) || useColor("never", &buffer) {
		t.Fatal("explicit color mode ignored")
	}
}

func TestRotationAndCount(t *testing.T) {
	c := testConfig(t)
	c.MaxSizeMB = 1
	c.MaxFiles = 2
	now := time.Now().UTC()
	w, e := newRotatingWriter(c, func() time.Time { return now })
	if e != nil {
		t.Fatal(e)
	}
	defer w.Close()
	record := bytes.Repeat([]byte("x"), 600<<10)
	for range 5 {
		now = now.Add(time.Second)
		if _, e = w.Write(record); e != nil {
			t.Fatal(e)
		}
	}
	names := archives(t, c.Directory)
	if len(names) != 2 {
		t.Fatalf("archives=%d, want 2", len(names))
	}
	data, e := os.ReadFile(w.path())
	if e != nil || len(data) != len(record) {
		t.Fatal("active record missing or split")
	}
	// Existing records are appended when a process restarts.
	if e = w.Close(); e != nil {
		t.Fatal(e)
	}
	w2, e := newRotatingWriter(c, func() time.Time { return now })
	if e != nil {
		t.Fatal(e)
	}
	defer w2.Close()
	if _, e = w2.Write([]byte("tail\n")); e != nil {
		t.Fatal(e)
	}
	data, _ = os.ReadFile(w2.path())
	if !bytes.HasSuffix(data, []byte("tail\n")) || len(data) != len(record)+5 {
		t.Fatal("restart truncated active log")
	}
}

func TestDailyRotationAndRetention(t *testing.T) {
	c := testConfig(t)
	c.RetentionDays = 7
	now := time.Now().UTC()
	w, e := newRotatingWriter(c, func() time.Time { return now })
	if e != nil {
		t.Fatal(e)
	}
	defer w.Close()
	if _, e = w.Write([]byte("old day\n")); e != nil {
		t.Fatal(e)
	}
	now = now.Add(24 * time.Hour)
	if e = w.maintain(); e != nil {
		t.Fatal(e)
	}
	if len(archives(t, c.Directory)) != 1 {
		t.Fatal("daily maintenance did not rotate")
	}
	unrelated := filepath.Join(c.Directory, "operator-notes.log")
	os.WriteFile(unrelated, []byte("keep"), 0600)
	// Simulate an archive's expiry; maintenance also runs without any new writes.
	for _, name := range archives(t, c.Directory) {
		old := now.Add(-8 * 24 * time.Hour)
		os.Chtimes(filepath.Join(c.Directory, name), old, old)
	}
	if e = w.maintain(); e != nil {
		t.Fatal(e)
	}
	if len(archives(t, c.Directory)) != 0 {
		t.Fatal("expired archive retained")
	}
	if _, e = os.Stat(unrelated); e != nil {
		t.Fatal("unrelated file removed")
	}
}

func TestRotationConcurrentWrites(t *testing.T) {
	c := testConfig(t)
	c.MaxFiles = 10
	var console bytes.Buffer
	service, e := Start(c, filepath.Join(filepath.Dir(c.Directory), "content"), &console)
	if e != nil {
		t.Fatal(e)
	}
	var wg sync.WaitGroup
	for worker := range 8 {
		wg.Add(1)
		go func() {
			defer wg.Done()
			for n := range 30 {
				service.Logger.Info("concurrent", "worker", worker, "n", n)
			}
		}()
	}
	wg.Wait()
	if e = service.Close(); e != nil {
		t.Fatal(e)
	}
	data, _ := os.ReadFile(filepath.Join(c.Directory, "filegate.log"))
	if lines := bytes.Count(data, []byte("\n")); lines != 240 {
		t.Fatalf("missing or interleaved records: %d", lines)
	}
	if bytes.Count(console.Bytes(), []byte("\n")) != 240 {
		t.Fatal("console records missing")
	}
}

func TestFileFailureStillLogsToConsole(t *testing.T) {
	c := testConfig(t)
	var console bytes.Buffer
	service, e := Start(c, filepath.Join(filepath.Dir(c.Directory), "content"), &console)
	if e != nil {
		t.Fatal(e)
	}
	defer service.Close()
	if e = service.writer.Close(); e != nil {
		t.Fatal(e)
	}
	service.Logger.Error("important event")
	if !strings.Contains(console.String(), "important event") || !strings.Contains(console.String(), "log file operation failed") {
		t.Fatal("file sink failure swallowed the console log")
	}
}

func TestPrivateDirectoryAndLock(t *testing.T) {
	c := testConfig(t)
	root := filepath.Join(filepath.Dir(c.Directory), "content")
	os.MkdirAll(root, 0700)
	if e := ValidateDirectory(filepath.Join(root, "logs"), root); e == nil {
		t.Fatal("log directory allowed inside content")
	}
	link := filepath.Join(filepath.Dir(c.Directory), "link")
	os.Symlink(root, link)
	if e := ValidateDirectory(filepath.Join(link, "new-logs"), root); e == nil {
		t.Fatal("symlink allowed logs under content")
	}
	w, e := newRotatingWriter(c, time.Now)
	if e != nil {
		t.Fatal(e)
	}
	defer w.Close()
	if second, e := newRotatingWriter(c, time.Now); e == nil {
		second.Close()
		t.Fatal("multiple writers allowed")
	}
	w.Close()
	os.Remove(filepath.Join(c.Directory, "filegate.log"))
	target := filepath.Join(root, "secret.txt")
	os.WriteFile(target, []byte("unchanged"), 0600)
	os.Symlink(target, filepath.Join(c.Directory, "filegate.log"))
	if second, e := newRotatingWriter(c, time.Now); e == nil {
		second.Close()
		t.Fatal("symlink log allowed")
	}
	data, _ := os.ReadFile(target)
	if string(data) != "unchanged" {
		t.Fatal("symlink target modified")
	}
	// A failed initialization must release its lock.
	os.Remove(filepath.Join(c.Directory, "filegate.log"))
	second, e := newRotatingWriter(c, time.Now)
	if e != nil {
		t.Fatal(e)
	}
	second.Close()
}

func TestConfigValidation(t *testing.T) {
	for _, change := range []func(*Config){func(c *Config) { c.Directory = "" }, func(c *Config) { c.Level = "invalid" }, func(c *Config) { c.Color = "invalid" }, func(c *Config) { c.MaxSizeMB = 0 }, func(c *Config) { c.MaxFiles = 0 }, func(c *Config) { c.RetentionDays = 0 }} {
		c := Defaults()
		change(&c)
		if e := c.Validate(); e == nil {
			t.Fatal(fmt.Sprintf("accepted invalid config: %+v", c))
		}
	}
}

func TestEmptyLogStartsOnFirstWriteDay(t *testing.T) {
	c := testConfig(t)
	now := time.Now().UTC()
	w, err := newRotatingWriter(c, func() time.Time { return now })
	if err != nil {
		t.Fatal(err)
	}
	defer w.Close()
	now = now.Add(48 * time.Hour)
	for range 2 {
		if _, err = w.Write([]byte("same day\n")); err != nil {
			t.Fatal(err)
		}
	}
	if len(archives(t, c.Directory)) != 0 {
		t.Fatal("empty prior-day file caused a spurious rotation")
	}
}
