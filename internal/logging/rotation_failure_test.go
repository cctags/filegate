package logging

import (
	"bytes"
	"errors"
	"os"
	"path/filepath"
	"strings"
	"sync"
	"testing"
	"time"
)

func TestFileRenameFailureKeepsConsoleAndRecovers(t *testing.T) {
	c := testConfig(t)
	c.MaxSizeMB = 1
	var console bytes.Buffer
	s, err := Start(c, filepath.Join(filepath.Dir(c.Directory), "content"), &console)
	if err != nil {
		t.Fatal(err)
	}
	defer s.Close()
	s.Logger.Info("before", "payload", strings.Repeat("x", 1<<20))
	hold := c.Directory + ".held"
	if err := os.Rename(c.Directory, hold); err != nil {
		t.Fatal(err)
	}
	s.Logger.Error("event during rename failure")
	if err := os.Rename(hold, c.Directory); err != nil {
		t.Fatal(err)
	}
	if !strings.Contains(console.String(), "event during rename failure") || !strings.Contains(console.String(), "log file operation failed") {
		t.Fatal("filesystem failure swallowed console event")
	}
	s.Logger.Info("recovered event")
	data, err := os.ReadFile(filepath.Join(c.Directory, "filegate.log"))
	if err != nil || !bytes.Contains(data, []byte("recovered event")) {
		t.Fatal("file logging did not recover")
	}
}

func TestRotationFailureAndRecovery(t *testing.T) {
	c := testConfig(t)
	c.MaxSizeMB = 1
	w, err := newRotatingWriter(c, time.Now)
	if err != nil {
		t.Fatal(err)
	}
	defer w.Close()
	first := bytes.Repeat([]byte("a"), 1<<20)
	if _, err = w.Write(first); err != nil {
		t.Fatal(err)
	}
	if len(archives(t, c.Directory)) != 0 {
		t.Fatal("exact threshold rotated too early")
	}
	hold := c.Directory + ".held"
	if err = os.Rename(c.Directory, hold); err != nil {
		t.Fatal(err)
	}
	_, writeErr := w.Write([]byte("tail"))
	if err = os.Rename(hold, c.Directory); err != nil {
		t.Fatal(err)
	}
	if writeErr == nil {
		t.Fatal("missing rotation path did not fail")
	}
	if _, err = w.Write([]byte("tail")); err != nil {
		t.Fatal(err)
	}
	if len(archives(t, c.Directory)) != 1 {
		t.Fatal("rotation did not recover")
	}
	data, err := os.ReadFile(w.path())
	if err != nil || string(data) != "tail" {
		t.Fatal("tail lost after recovery")
	}
	archived, err := os.ReadFile(filepath.Join(c.Directory, archives(t, c.Directory)[0]))
	if err != nil || !bytes.Equal(archived, first) {
		t.Fatal("original record lost")
	}
}

func TestOversizedRecordAndRetentionBoundary(t *testing.T) {
	c := testConfig(t)
	c.MaxSizeMB = 1
	now := time.Now().UTC()
	w, err := newRotatingWriter(c, func() time.Time { return now })
	if err != nil {
		t.Fatal(err)
	}
	defer w.Close()
	large := bytes.Repeat([]byte("x"), (1<<20)+1)
	if n, err := w.Write(large); err != nil || n != len(large) {
		t.Fatal("oversized record split or dropped")
	}
	if err := w.maintain(); err != nil {
		t.Fatal(err)
	}
	names := archives(t, c.Directory)
	if len(names) != 1 {
		t.Fatal("oversized record not archived")
	}
	path := filepath.Join(c.Directory, names[0])
	cutoff := now.Add(-time.Duration(c.RetentionDays) * 24 * time.Hour)
	if err := os.Chtimes(path, cutoff, cutoff); err != nil {
		t.Fatal(err)
	}
	if err := w.maintain(); err != nil {
		t.Fatal(err)
	}
	if len(archives(t, c.Directory)) != 1 {
		t.Fatal("exact retention cutoff removed prematurely")
	}
	now = now.Add(time.Second)
	if err := w.maintain(); err != nil {
		t.Fatal(err)
	}
	if len(archives(t, c.Directory)) != 0 {
		t.Fatal("expired archive kept")
	}
}

func TestConcurrentCloseAndWrites(t *testing.T) {
	c := testConfig(t)
	w, err := newRotatingWriter(c, time.Now)
	if err != nil {
		t.Fatal(err)
	}
	var wg sync.WaitGroup
	for range 4 {
		wg.Add(1)
		go func() {
			defer wg.Done()
			for range 20 {
				if _, err := w.Write([]byte("record\n")); err != nil && !errors.Is(err, os.ErrClosed) {
					t.Errorf("write: %v", err)
				}
			}
		}()
	}
	for range 2 {
		wg.Add(1)
		go func() {
			defer wg.Done()
			if err := w.Close(); err != nil {
				t.Errorf("close: %v", err)
			}
		}()
	}
	wg.Wait()
	if err := w.maintain(); !errors.Is(err, os.ErrClosed) {
		t.Fatal("maintenance accepted closed writer")
	}
}
