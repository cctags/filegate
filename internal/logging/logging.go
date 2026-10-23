package logging

import (
	"context"
	"errors"
	"fmt"
	"io"
	"log/slog"
	"os"
	"strings"
	"sync"
	"syscall"
	"time"
	"unsafe"
)

type Service struct {
	Logger     *slog.Logger
	writer     *rotatingWriter
	stop, done chan struct{}
	once       sync.Once
	closeErr   error
}

func Start(c Config, contentRoot string, console io.Writer) (*Service, error) {
	if err := c.Validate(); err != nil {
		return nil, err
	}
	if err := ValidateDirectory(c.Directory, contentRoot); err != nil {
		return nil, err
	}
	writer, err := newRotatingWriter(c, time.Now)
	if err != nil {
		return nil, err
	}
	var level slog.Level
	level.UnmarshalText([]byte(strings.ToUpper(c.Level)))
	options := &slog.HandlerOptions{Level: level}
	consoleHandler := slog.NewTextHandler(&colorWriter{out: console, color: useColor(c.Color, console)}, options)
	// Failure messages bypass the selected threshold so a broken file sink is
	// visible on the console independently of normal record filtering.
	failureHandler := slog.NewTextHandler(&colorWriter{out: console, color: useColor(c.Color, console)}, nil)
	report := &fileFailure{console: failureHandler}
	handler := &dualHandler{console: consoleHandler, file: slog.NewTextHandler(writer, options), failure: report}
	service := &Service{Logger: slog.New(handler), writer: writer, stop: make(chan struct{}), done: make(chan struct{})}
	go func() {
		defer close(service.done)
		ticker := time.NewTicker(15 * time.Minute)
		defer ticker.Stop()
		for {
			select {
			case <-service.stop:
				return
			case <-ticker.C:
				if err := writer.maintain(); err != nil {
					report.report(context.Background(), err)
				}
			}
		}
	}()
	return service, nil
}
func (s *Service) Close() error {
	s.once.Do(func() { close(s.stop); <-s.done; s.closeErr = s.writer.Close() })
	return s.closeErr
}

type dualHandler struct {
	console, file slog.Handler
	failure       *fileFailure
}

func (h *dualHandler) Enabled(ctx context.Context, level slog.Level) bool {
	return h.console.Enabled(ctx, level) || h.file.Enabled(ctx, level)
}
func (h *dualHandler) Handle(ctx context.Context, r slog.Record) error {
	var errs []error
	if h.console.Enabled(ctx, r.Level) {
		errs = append(errs, h.console.Handle(ctx, r.Clone()))
	}
	if h.file.Enabled(ctx, r.Level) {
		if err := h.file.Handle(ctx, r.Clone()); err != nil {
			h.failure.report(ctx, err)
			errs = append(errs, err)
		}
	}
	return errors.Join(errs...)
}
func (h *dualHandler) WithAttrs(attrs []slog.Attr) slog.Handler {
	return &dualHandler{console: h.console.WithAttrs(attrs), file: h.file.WithAttrs(attrs), failure: h.failure}
}
func (h *dualHandler) WithGroup(name string) slog.Handler {
	return &dualHandler{console: h.console.WithGroup(name), file: h.file.WithGroup(name), failure: h.failure}
}

type fileFailure struct {
	mu      sync.Mutex
	last    time.Time
	console slog.Handler
}

func (f *fileFailure) report(ctx context.Context, err error) {
	f.mu.Lock()
	defer f.mu.Unlock()
	now := time.Now()
	if !f.last.IsZero() && now.Sub(f.last) < time.Minute {
		return
	}
	f.last = now
	r := slog.NewRecord(now, slog.LevelError, "log file operation failed; console logging continues", 0)
	r.Add("error", err.Error())
	f.console.Handle(ctx, r)
}

type colorWriter struct {
	out   io.Writer
	color bool
}

func (w *colorWriter) Write(p []byte) (int, error) {
	original := len(p)
	if w.color {
		text := string(p)
		if index := strings.Index(text, " level="); index >= 0 {
			start := index + len(" level=")
			end := strings.IndexByte(text[start:], ' ')
			if end < 0 {
				end = len(text) - start
			}
			level := text[start : start+end]
			for _, item := range []struct{ level, color string }{{"DEBUG", "36"}, {"INFO", "32"}, {"WARN", "33"}, {"ERROR", "31"}} {
				if strings.HasPrefix(level, item.level) {
					text = text[:start] + "\x1b[1;" + item.color + "m" + level + "\x1b[0m" + text[start+end:]
					break
				}
			}
		}
		n, err := io.WriteString(w.out, text)
		if err != nil {
			return 0, err
		}
		if n != len(text) {
			return 0, io.ErrShortWrite
		}
		return original, nil
	}
	n, err := w.out.Write(p)
	if err == nil && n != len(p) {
		err = io.ErrShortWrite
	}
	return n, err
}
func useColor(mode string, out io.Writer) bool {
	if mode == "always" {
		return true
	}
	if mode == "never" {
		return false
	}
	if _, disabled := os.LookupEnv("NO_COLOR"); disabled || os.Getenv("TERM") == "dumb" {
		return false
	}
	file, ok := out.(*os.File)
	if !ok {
		return false
	}
	var state syscall.Termios
	_, _, errno := syscall.Syscall(syscall.SYS_IOCTL, file.Fd(), syscall.TCGETS, uintptr(unsafe.Pointer(&state)))
	return errno == 0
}

// ConsoleError reports shutdown failures without recursively using a closed sink.
func ConsoleError(console io.Writer, err error) {
	fmt.Fprintf(console, "filegate: log shutdown failed: %v\n", err)
}
