package logging

import (
	"crypto/rand"
	"encoding/hex"
	"errors"
	"io"
	"os"
	"path/filepath"
	"sort"
	"strings"
	"sync"
	"syscall"
	"time"
)

const archiveTimeFormat = "20060102T150405.000000000Z"

type rotatingWriter struct {
	mu         sync.Mutex
	cfg        Config
	file, lock *os.File
	size       int64
	day        string
	closed     bool
	now        func() time.Time
}

func newRotatingWriter(c Config, now func() time.Time) (w *rotatingWriter, err error) {
	if err = os.MkdirAll(c.Directory, 0700); err != nil {
		return nil, err
	}
	if err = os.Chmod(c.Directory, 0700); err != nil {
		return nil, err
	}
	lock, err := os.OpenFile(filepath.Join(c.Directory, ".filegate.lock"), os.O_CREATE|os.O_RDWR, 0600)
	if err != nil {
		return nil, err
	}
	if err = syscall.Flock(int(lock.Fd()), syscall.LOCK_EX|syscall.LOCK_NB); err != nil {
		lock.Close()
		return nil, errors.New("another server owns the logging directory")
	}
	w = &rotatingWriter{cfg: c, lock: lock, now: now}
	created := w
	defer func() {
		if err != nil {
			created.Close()
		}
	}()
	if err = w.open(); err != nil {
		return nil, err
	}
	if err = w.maintain(); err != nil {
		return nil, err
	}
	return w, nil
}
func (w *rotatingWriter) path() string { return filepath.Join(w.cfg.Directory, "filegate.log") }
func (w *rotatingWriter) open() error {
	if info, err := os.Lstat(w.path()); err == nil {
		if !info.Mode().IsRegular() {
			return errors.New("filegate.log must be a regular file, not a symlink")
		}
	} else if !os.IsNotExist(err) {
		return err
	}
	f, err := os.OpenFile(w.path(), os.O_CREATE|os.O_APPEND|os.O_WRONLY, 0600)
	if err != nil {
		return err
	}
	if err = f.Chmod(0600); err != nil {
		f.Close()
		return err
	}
	info, err := f.Stat()
	if err != nil {
		f.Close()
		return err
	}
	w.file = f
	w.size = info.Size()
	w.day = info.ModTime().UTC().Format(time.DateOnly)
	if w.size == 0 {
		w.day = w.now().UTC().Format(time.DateOnly)
	}
	return nil
}
func (w *rotatingWriter) rotate() error {
	if w.file == nil {
		if err := w.open(); err != nil {
			return err
		}
	}
	if err := w.file.Sync(); err != nil {
		return err
	}
	if err := w.file.Close(); err != nil {
		w.file = nil
		return err
	}
	w.file = nil
	suffix := make([]byte, 4)
	if _, err := rand.Read(suffix); err != nil {
		return err
	}
	archive := filepath.Join(w.cfg.Directory, "filegate-"+w.now().UTC().Format(archiveTimeFormat)+"-"+hex.EncodeToString(suffix)+".log")
	if err := os.Rename(w.path(), archive); err != nil {
		return err
	}
	return w.open()
}
func archiveName(name string) bool {
	if !strings.HasPrefix(name, "filegate-") || !strings.HasSuffix(name, ".log") {
		return false
	}
	middle := strings.TrimSuffix(strings.TrimPrefix(name, "filegate-"), ".log")
	n := len(archiveTimeFormat)
	if len(middle) != n+1+8 || middle[n] != '-' {
		return false
	}
	if _, err := time.Parse(archiveTimeFormat, middle[:n]); err != nil {
		return false
	}
	_, err := hex.DecodeString(middle[n+1:])
	return err == nil
}
func (w *rotatingWriter) prune() error {
	entries, err := os.ReadDir(w.cfg.Directory)
	if err != nil {
		return err
	}
	type archive struct {
		name     string
		modified time.Time
	}
	var kept []archive
	cutoff := w.now().Add(-time.Duration(w.cfg.RetentionDays) * 24 * time.Hour)
	for _, entry := range entries {
		if !archiveName(entry.Name()) || !entry.Type().IsRegular() {
			continue
		}
		info, e := entry.Info()
		if e != nil {
			return e
		}
		name := filepath.Join(w.cfg.Directory, entry.Name())
		if info.ModTime().Before(cutoff) {
			if e = os.Remove(name); e != nil {
				return e
			}
		} else {
			kept = append(kept, archive{name, info.ModTime()})
		}
	}
	sort.Slice(kept, func(i, j int) bool {
		if kept[i].modified.Equal(kept[j].modified) {
			return kept[i].name > kept[j].name
		}
		return kept[i].modified.After(kept[j].modified)
	})
	for _, old := range kept[min(len(kept), w.cfg.MaxFiles):] {
		if err = os.Remove(old.name); err != nil {
			return err
		}
	}
	return nil
}
func (w *rotatingWriter) maintain() error {
	w.mu.Lock()
	defer w.mu.Unlock()
	if w.closed {
		return os.ErrClosed
	}
	if w.file == nil {
		if err := w.open(); err != nil {
			return err
		}
	}
	if w.size > 0 && (w.day != w.now().UTC().Format(time.DateOnly) || w.size >= int64(w.cfg.MaxSizeMB)<<20) {
		if err := w.rotate(); err != nil {
			return err
		}
	}
	return w.prune()
}
func (w *rotatingWriter) Write(p []byte) (int, error) {
	w.mu.Lock()
	defer w.mu.Unlock()
	if w.closed {
		return 0, os.ErrClosed
	}
	if w.file == nil {
		if err := w.open(); err != nil {
			return 0, err
		}
	}
	rotated := false
	if w.size == 0 {
		w.day = w.now().UTC().Format(time.DateOnly)
	}
	if w.size > 0 && (w.day != w.now().UTC().Format(time.DateOnly) || w.size+int64(len(p)) > int64(w.cfg.MaxSizeMB)<<20) {
		if err := w.rotate(); err != nil {
			return 0, err
		}
		rotated = true
	}
	n, err := w.file.Write(p)
	w.size += int64(n)
	if err == nil && n != len(p) {
		err = io.ErrShortWrite
	}
	if err == nil && rotated {
		err = w.prune()
	}
	return n, err
}
func (w *rotatingWriter) Close() error {
	if w == nil {
		return nil
	}
	w.mu.Lock()
	defer w.mu.Unlock()
	if w.closed {
		return nil
	}
	w.closed = true
	var errs []error
	if w.file != nil {
		errs = append(errs, w.file.Sync(), w.file.Close())
	}
	if w.lock != nil {
		errs = append(errs, w.lock.Close())
	}
	return errors.Join(errs...)
}
