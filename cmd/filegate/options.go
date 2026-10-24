package main

import (
	"errors"
	"fmt"
	"strings"
	"time"

	"filegate/internal/gate"
)

const usage = `Usage: filegate [OPTIONS] ACTION [ARGUMENT]
       filegate --help | --version

Choose exactly one action (no default action):
  init                   Create configuration and example directories;
                         never overwrite an existing configuration
  serve                  Start the server in the foreground (non-root only)
  status                 Show codes, bans and session count
  code ALIAS             Create a one-time code for a configured alias
  revoke ID              Revoke an unused code using its returned id
  logout ALIAS           Invalidate all sessions for a directory alias
  block IP_OR_CIDR       Add a permanent blacklist entry (no expiry)
  unblock IP_OR_CIDR     Remove an entry and matching temporary bans

Options:
  --help                 Show this help
  --version              Print version and build information
  --config PATH          Default: config.json in the current working directory
  --ttl DURATION         Only with code; default: the running server's
                         code_ttl setting (10m in a newly initialized config).
                         Override for this code only, e.g. 30s, 30m or 2h;
                         must be positive. Existing code expiries are unchanged.

ALIAS, ID and IP_OR_CIDR are required for their respective actions and have
no default. ALIAS is a configured key of 1-16 lowercase English letters,
not a directory path. Directory names may contain Chinese characters.
Management actions require a running server and its OS user (or root).
Only IPv4 is supported. listen requires a literal IPv4 address and numeric
port (1-65535); blacklists accept IPv4 addresses and CIDRs only.
Directory mappings must be distinct, non-nested subdirectories, including
resolved symlink targets; the root itself and parent components are rejected.
serve refuses effective UID 0 before reading configuration; use a dedicated
non-root service user (normally filegate). Other actions may run as root.
--help and --version need neither a configuration file nor a running server.
Configuration accepts JSONC // and /* */ comments, or plain JSON, regardless
of extension. The default filename remains config.json; --config config.jsonc
is also supported. Trailing commas and JSON5 syntax are not supported.
Options may appear in any order. Values accept --option VALUE or
--option=VALUE, before or after the action. Actions use bare words (serve,
code, etc.); prefixed actions such as --serve and single-dash options are rejected.

Configuration defaults written by init:
  listen                 0.0.0.0:17342 (all IPv4 interfaces)
  root                   content
  state_dir              state
  blacklist              blacklist.txt
  directories            ab -> ab, cd -> cd, ch -> 文件夹
  code_ttl               10m
  idle_timeout           30m
  max_session            8h
  failure_window         10m
  ban_duration           15m
  max_failures           5
  attempts_per_minute    10
  logging.directory      logs (alongside content/ and state/)
  logging.level          info (debug, info, warn, error)
  logging.color          auto (auto, always, never; honors NO_COLOR in auto)
  logging.max_size_mb    10 (MiB per file; also rotates daily at UTC date change)
  logging.max_files      10 archived files, excluding the active file
  logging.retention_days 7 (older archives removed, even below count limit)
  tls_cert / tls_key     Not configured: HTTP, no certificate required

These are initialization defaults, not overrides of an existing config.
Relative paths inside JSON resolve from the configuration file's directory.
Thus --config tmp/demo/config.json uses tmp/demo/content as its default root.

Fixed safety limits (not configuration fields):
  TCP connections        128; excess connections wait for a free slot
  Active HTTP requests   128 (including HTTP/2 streams); excess returns 429
  File requests          32 globally, 4 per client IP; excess returns 429
  Response write idle    30s, renewed as chunks are written
  Directory entries      10000 raw entries; larger listings return 503
  Active codes per alias 10; revoke unused codes or wait for expiry
  Range requests         One range, at most 128 header bytes; else returns 400

Server signals:
  SIGHUP                 Reload directory mappings and runtime limits;
                         failure keeps the previous configuration
  SIGINT, SIGTERM        Shut down gracefully
Changing the listener, root, state/blacklist paths, logging or TLS requires
a restart. Blacklist file contents take effect without a signal.

Examples:
  # 1. Initialize a NEW deployment (skip if its config already exists).
  filegate --config tmp/demo/config.json init
  # Creates content/ab, content/cd, content/文件夹, state, logs and blacklist.txt
  # under tmp/demo/. config.json contains // explanations above key fields.

  # 2. Start the server. Keep this terminal open; use another for management.
  filegate --config tmp/demo/config.json serve
  # Defaults: browse http://SERVER_IP:17342; use the listen port from your config.
  # Colored levels appear on a terminal; plain logs go to tmp/demo/logs/filegate.log.
  # Redirected console output is uncolored unless logging.color is always.
  # Logs rotate at 10 MiB / UTC date changes; keep <=10 archives for <=7 days.

  # 3. Issue an access code for the existing ab directory.
  filegate --config tmp/demo/config.json code ab
  filegate --config tmp/demo/config.json code ab --ttl 30m
  filegate --config tmp/demo/config.json code ch --ttl 30m
  # ch is the initialized alias for 文件夹.
  # Send the returned code (e.g. ab01234567) to the user, not the id.
  # The code allows one successful login; the session has separate timeouts.

  # 4. Add a directory while the server is running, then copy files into it.
  mkdir -p tmp/demo/content/documents
  # Edit tmp/demo/config.json: preserve other fields and update directories to:
  # "directories": {"ab": "ab", "cd": "cd", "ch": "文件夹",
  #                 "docs": "documents"}
  # Merely creating the folder does not make it accessible.

  # 5. Reload the server after saving the configuration.
  # For a single instance, first verify this is the serve process:
  ps -C filegate -o pid,args
  kill -HUP "$(pidof -s filegate)"
  # With multiple instances, send HUP to the specific server PID instead.
  # Confirm "configuration reloaded" in the server log before issuing a code.
  filegate --config tmp/demo/config.json code docs --ttl 30m
  # Enter the returned docs-prefixed code in the browser to access documents.
  # Remapping/removing aliases revokes their codes and sessions;
  # unchanged aliases keep their authorizations.

  # 6. Inspect, revoke access, or manage the permanent blacklist.
  filegate --config tmp/demo/config.json status
  filegate --config tmp/demo/config.json revoke CODE_ID
  filegate --config tmp/demo/config.json logout docs
  filegate --config tmp/demo/config.json block 203.0.113.25
  filegate --config tmp/demo/config.json block 198.51.100.0/24
  filegate --config tmp/demo/config.json unblock 203.0.113.25
  # Replace CODE_ID with the id returned at creation or shown by status.
`

type options struct {
	config string
	action gate.Request
}

func parseOptions(args []string) (options, error) {
	opts := options{config: "config.json"}
	seen := map[string]bool{}
	var positional []string
	fail := func(message string) (options, error) { return options{}, fmt.Errorf("%s; see --help", message) }
	for i := 0; i < len(args); i++ {
		arg := args[i]
		if !strings.HasPrefix(arg, "-") {
			positional = append(positional, arg)
			continue
		}
		if !strings.HasPrefix(arg, "--") || arg == "--" {
			return fail("expected a double-dash option, got " + arg)
		}
		name, value, hasValue := strings.Cut(strings.TrimPrefix(arg, "--"), "=")
		takesValue := false
		switch name {
		case "config", "ttl":
			takesValue = true
		case "help", "version":
		default:
			return fail("unknown option --" + name)
		}
		if seen[name] {
			return fail("duplicate option --" + name)
		}
		seen[name] = true
		if takesValue {
			if !hasValue {
				if i+1 == len(args) || strings.HasPrefix(args[i+1], "--") {
					return fail("--" + name + " requires a value")
				}
				i++
				value = args[i]
			}
			if value == "" {
				return fail("--" + name + " requires a nonempty value")
			}
		} else if hasValue {
			return fail("--" + name + " does not take a value")
		}
		switch name {
		case "config":
			opts.config = value
		case "ttl":
			opts.action.TTL = value
		default:
			if opts.action.Action != "" {
				return fail("choose either --help or --version")
			}
			opts.action.Action = name
		}
	}
	if opts.action.Action != "" {
		if len(positional) != 0 {
			return fail("--help and --version cannot be combined with an action")
		}
	} else {
		if len(positional) == 0 {
			return fail("an action is required")
		}
		opts.action.Action = positional[0]
		required := 0
		switch opts.action.Action {
		case "init", "serve", "status":
		case "code", "revoke", "logout", "block", "unblock":
			required = 1
		default:
			return fail("unknown action " + positional[0])
		}
		if len(positional) != required+1 {
			return fail(fmt.Sprintf("%s requires %d argument(s)", opts.action.Action, required))
		}
		if required == 1 {
			if positional[1] == "" {
				return fail(opts.action.Action + " requires a nonempty argument")
			}
			opts.action.Value = positional[1]
		}
	}
	if seen["ttl"] {
		if opts.action.Action != "code" {
			return fail("--ttl requires the code action")
		}
		duration, err := time.ParseDuration(opts.action.TTL)
		if err != nil || duration <= 0 {
			return opts, errors.New("--ttl must be a positive duration, e.g. 30m")
		}
	}
	return opts, nil
}
