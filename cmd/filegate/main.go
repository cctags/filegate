// filegate is a Linux-only, standard-library-only private file server.
package main

import (
	"context"
	"encoding/json"
	"errors"
	"fmt"
	"io"
	"log/slog"
	"net/http"
	"os"
	"os/signal"
	"path/filepath"
	"syscall"
	"time"

	"filegate/internal/gate"
)

const example = `{
  "listen": "0.0.0.0:8080",
  "root": "content",
  "state_dir": "state",
  "blacklist": "blacklist.txt",
  "directories": {"ab": "ab", "cd": "cd"},
  "code_ttl": "10m",
  "idle_timeout": "30m",
  "max_session": "8h",
  "failure_window": "10m",
  "ban_duration": "15m",
  "max_failures": 5,
  "attempts_per_minute": 10
}
`

func run() error {
	return runArgs(os.Args[1:], os.Stdout)
}

func runArgs(args []string, output io.Writer) error {
	opts, err := parseOptions(args)
	if err != nil {
		return err
	}
	if opts.action.Action == "help" {
		_, err := fmt.Fprint(output, usage)
		return err
	}
	if opts.action.Action == "version" {
		_, err := fmt.Fprintln(output, versionInfo())
		return err
	}
	if opts.action.Action == "init" {
		base := filepath.Dir(opts.config)
		if e := os.MkdirAll(base, 0700); e != nil {
			return e
		}
		f, e := os.OpenFile(opts.config, os.O_CREATE|os.O_EXCL|os.O_WRONLY, 0600)
		if e != nil {
			return e
		}
		_, e = f.WriteString(example)
		ce := f.Close()
		if e != nil {
			return e
		}
		if ce != nil {
			return ce
		}
		for _, d := range []string{"content/ab", "content/cd", "state"} {
			if e = os.MkdirAll(filepath.Join(base, d), 0700); e != nil {
				return e
			}
		}
		f, e = os.OpenFile(filepath.Join(base, "blacklist.txt"), os.O_CREATE|os.O_EXCL|os.O_WRONLY, 0600)
		if e != nil && !os.IsExist(e) {
			return e
		}
		if e == nil {
			_, e = f.WriteString("# One IP address or CIDR network per line.\n")
			ce = f.Close()
			if e != nil {
				return e
			}
			if ce != nil {
				return ce
			}
		}
		fmt.Fprintln(output, "Created configuration and directories. Start with: filegate --config", opts.config, "--serve")
		return nil
	}
	c, e := gate.Load(opts.config)
	if e != nil {
		return e
	}
	if opts.action.Action != "serve" {
		out, e := gate.Call(c, opts.action)
		if e != nil {
			return e
		}
		enc := json.NewEncoder(output)
		enc.SetIndent("", "  ")
		return enc.Encode(out)
	}

	lock, e := gate.Lock(c)
	if e != nil {
		return e
	}
	defer lock.Close()
	app, e := gate.New(c)
	if e != nil {
		return e
	}
	defer app.Close()
	admin, e := app.AdminListener()
	if e != nil {
		return e
	}
	defer admin.Close()
	go app.ServeAdmin(admin)
	server := &http.Server{Addr: c.Listen, Handler: app.Handler(), ReadHeaderTimeout: 5 * time.Second, ReadTimeout: 15 * time.Second, IdleTimeout: 60 * time.Second, MaxHeaderBytes: 16 << 10}
	ctx, stop := signal.NotifyContext(context.Background(), os.Interrupt, syscall.SIGTERM)
	defer stop()
	done := make(chan error, 1)
	go func() {
		if c.TLSCert != "" {
			done <- server.ListenAndServeTLS(c.TLSCert, c.TLSKey)
		} else {
			done <- server.ListenAndServe()
		}
	}()
	slog.Info("filegate starting", "listen", c.Listen, "https", c.TLSCert != "", "admin", "local Unix socket only")
	if c.TLSCert == "" {
		slog.Warn("HTTP is unencrypted; use only on a trusted network")
	}
	select {
	case e = <-done:
		if errors.Is(e, http.ErrServerClosed) {
			return nil
		}
		return e
	case <-ctx.Done():
		admin.Close()
		shutdown, cancel := context.WithTimeout(context.Background(), 10*time.Second)
		defer cancel()
		e = server.Shutdown(shutdown)
		if e != nil {
			server.Close()
		}
		return e
	}
}
func main() {
	if e := run(); e != nil {
		slog.Error("filegate", "error", e)
		os.Exit(1)
	}
}
