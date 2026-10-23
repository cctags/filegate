package main

import (
	"errors"
	"fmt"
	"strings"
	"time"

	"filegate/internal/gate"
)

const usage = `Usage: filegate [--config PATH] ACTION [OPTIONS]

Choose exactly one action:
  --init                 Create configuration and example directories
  --serve                Start the file server
  --status               Show codes, bans and session count
  --code ALIAS           Create a one-time access code
  --revoke ID            Revoke an unused access code
  --logout ALIAS         Invalidate all sessions for a directory
  --block IP_OR_CIDR     Add a permanent blacklist entry
  --unblock IP_OR_CIDR   Remove an entry and matching temporary bans
  --version              Print version and build information
  --help                 Show this help

Options:
  --config PATH          Configuration file (default: config.json)
  --ttl DURATION         Access code lifetime, only with --code (e.g. 30m)

Options may appear in any order. Values accept --option VALUE or
--option=VALUE. Single-dash options and positional commands are not accepted.

Server signals:
  SIGHUP                 Reload directory mappings and runtime limits
  SIGINT, SIGTERM        Shut down gracefully

Examples:
  filegate --init --config config.json
  filegate --serve --config config.json
  filegate --code ab --ttl 30m --config config.json
  filegate --block 203.0.113.25
`

type options struct {
	config string
	action gate.Request
}

func parseOptions(args []string) (options, error) {
	opts := options{config: "config.json"}
	seen := map[string]bool{}
	fail := func(message string) (options, error) { return options{}, fmt.Errorf("%s; see --help", message) }
	for i := 0; i < len(args); i++ {
		arg := args[i]
		if !strings.HasPrefix(arg, "--") || arg == "--" {
			return fail("expected a long option, got " + arg)
		}
		name, value, hasValue := strings.Cut(strings.TrimPrefix(arg, "--"), "=")
		takesValue := false
		switch name {
		case "config", "ttl", "code", "revoke", "logout", "block", "unblock":
			takesValue = true
		case "init", "serve", "status", "version", "help":
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
				return fail("choose exactly one action")
			}
			opts.action.Action = name
			opts.action.Value = value
		}
	}
	if opts.action.Action == "" {
		return fail("an action is required")
	}
	if seen["ttl"] {
		if opts.action.Action != "code" {
			return fail("--ttl requires --code")
		}
		duration, err := time.ParseDuration(opts.action.TTL)
		if err != nil || duration <= 0 {
			return opts, errors.New("--ttl must be a positive duration, e.g. 30m")
		}
	}
	return opts, nil
}
