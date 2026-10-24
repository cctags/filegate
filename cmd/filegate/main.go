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
	"filegate/internal/logging"
)

const example = `{
  "listen": "0.0.0.0:8080",

  // 相对路径以本配置文件所在目录为基准。
  // 状态目录、黑名单和 TLS 私钥必须放在工作根目录之外。
  "root": "content",
  "state_dir": "state",
  "blacklist": "blacklist.txt",

  // 键是访问码前缀：1–16 个小写英文字母。
  // 值是 root 下的相对目录，支持中文；新增映射前先创建目录。
  "directories": {
    "ab": "ab",
    "cd": "cd",
    "ch": "文件夹"
  },

  // 默认访问码有效期，可由 --ttl 覆盖；成功登录一次即失效。
  // 与登录后的会话期限无关。时长格式支持 30s、10m、8h。
  "code_ttl": "10m",

  // 按最后一次授权请求计算空闲时间。
  // 仅阅读页面或播放已下载的音频，不会自动续期。
  "idle_timeout": "30m",

  // 从登录开始计算的最长会话时间，不因持续访问而延长。
  "max_session": "8h",

  // 同一 IP 在此时间窗口内达到 max_failures 次错误，
  // 或一分钟内超过 attempts_per_minute 次登录提交，会被临时封禁。
  "failure_window": "10m",
  "max_failures": 5,
  "attempts_per_minute": 10,

  // 自动封禁的持续时间；管理员添加的永久黑名单不自动到期。
  "ban_duration": "15m",

  // 日志目录与 content、state 平级，不能放在工作根目录内。
  // 控制台高亮，文件不带颜色；按 UTC 日期或大小轮转。
  // max_files 是归档数量，retention_days 是归档保留天数，两者同时生效。
  // color 支持 auto、always、never；重定向时 auto 不着色。修改 logging 需重启。
  "logging": {
    "directory": "logs",
    "level": "info",
    "color": "auto",
    "max_size_mb": 10,
    "max_files": 10,
    "retention_days": 7
  }

  // 默认使用 HTTP。需要 HTTPS 时，在 logging 对象后加逗号并启用：
  // "tls_cert": "server.crt",
  // "tls_key": "server.key"
}
`

func run() error {
	return runArgs(os.Args[1:], os.Stdout)
}

func runArgs(args []string, output io.Writer) error {
	return runArgsWithEUID(args, output, os.Geteuid())
}

func runArgsWithEUID(args []string, output io.Writer, euid int) (returnErr error) {
	opts, err := parseOptions(args)
	if err != nil {
		return err
	}
	if opts.action.Action == "serve" && euid == 0 {
		return errors.New("refusing to serve as root; run as a dedicated non-root user (for example, sudo -u filegate filegate --config /srv/filegate/config.json serve)")
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
		for _, d := range []string{"content/ab", "content/cd", "content/文件夹", "state", "logs"} {
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
		fmt.Fprintln(output, "Created configuration and directories. Start with: filegate --config", opts.config, "serve")
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
	logs, e := logging.Start(c.Logging, c.Root, os.Stderr)
	if e != nil {
		return e
	}
	previous := slog.Default()
	slog.SetDefault(logs.Logger)
	defer func() {
		if returnErr != nil {
			slog.Error("filegate stopped with error", "error", returnErr)
		} else {
			slog.Info("filegate stopped")
		}
		slog.SetDefault(previous)
		if err := logs.Close(); err != nil {
			logging.ConsoleError(os.Stderr, err)
		}
	}()
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
	reload := make(chan os.Signal, 1)
	signal.Notify(reload, syscall.SIGHUP)
	defer signal.Stop(reload)
	done := make(chan error, 1)
	go func() {
		if c.TLSCert != "" {
			done <- server.ListenAndServeTLS(c.TLSCert, c.TLSKey)
		} else {
			done <- server.ListenAndServe()
		}
	}()
	slog.Info("filegate starting", "listen", c.Listen, "https", c.TLSCert != "", "admin", "local Unix socket only", "log_directory", c.Logging.Directory, "version", version)
	if c.TLSCert == "" {
		slog.Warn("HTTP is unencrypted; use only on a trusted network")
	}
	for {
		select {
		case <-reload:
			result, err := app.Reload(opts.config)
			if err != nil {
				slog.Error("configuration reload failed; keeping previous configuration", "error", err)
			} else {
				slog.Info("configuration reloaded", "directories", result.Directories,
					"revoked_codes", result.RevokedCodes, "revoked_sessions", result.RevokedSessions)
			}
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
}
func main() {
	if e := run(); e != nil {
		slog.Error("filegate", "error", e)
		os.Exit(1)
	}
}
