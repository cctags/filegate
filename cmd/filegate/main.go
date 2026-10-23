// filegate is a Linux-only, standard-library-only private file server.
package main

import (
	"context"
	"encoding/json"
	"errors"
	"flag"
	"fmt"
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
	fs := flag.NewFlagSet("filegate", flag.ContinueOnError)
	cfgPath := fs.String("config", "config.json", "configuration path (before command)")
	fs.Usage = func() {
		fmt.Fprintln(os.Stderr, "Usage: filegate [-config path] init|serve|status|code ALIAS [TTL]|revoke ID|logout ALIAS|block IP_OR_CIDR|unblock IP_OR_CIDR")
	}
	if e := fs.Parse(os.Args[1:]); e != nil {
		return e
	}
	args := fs.Args()
	if len(args) == 0 {
		fs.Usage()
		return errors.New("command required")
	}
	if args[0] == "init" {
		if len(args) != 1 {
			return errors.New("init takes no arguments")
		}
		base := filepath.Dir(*cfgPath)
		if e := os.MkdirAll(base, 0700); e != nil {
			return e
		}
		f, e := os.OpenFile(*cfgPath, os.O_CREATE|os.O_EXCL|os.O_WRONLY, 0600)
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
		fmt.Println("Created configuration and directories. Start with: filegate -config", *cfgPath, "serve")
		return nil
	}
	c, e := gate.Load(*cfgPath)
	if e != nil {
		return e
	}
	if args[0] != "serve" {
		r := gate.Request{Action: args[0]}
		switch r.Action {
		case "status":
			if len(args) != 1 {
				return errors.New("status takes no arguments")
			}
		case "code":
			if len(args) < 2 || len(args) > 3 {
				return errors.New("usage: code ALIAS [TTL]")
			}
			r.Value = args[1]
			if len(args) == 3 {
				r.TTL = args[2]
			}
		case "revoke", "logout", "block", "unblock":
			if len(args) != 2 {
				return errors.New("command requires one argument")
			}
			r.Value = args[1]
		default:
			return errors.New("unknown command")
		}
		out, e := gate.Call(c, r)
		if e != nil {
			return e
		}
		enc := json.NewEncoder(os.Stdout)
		enc.SetIndent("", "  ")
		return enc.Encode(out)
	}
	if len(args) != 1 {
		return errors.New("serve takes no arguments")
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
