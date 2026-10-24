package main

import (
	"fmt"
	"net/url"
	"strings"
	"time"

	"filegate/internal/gate"
)

type options struct {
	config    string
	baseURL   *url.URL
	helpTopic string
	action    gate.Request
}

func parseOptions(args []string) (options, error) {
	opts := options{config: "config.json"}
	seen := map[string]bool{}
	var positional []string
	fail := func(message string) (options, error) {
		hint := "filegate --help"
		if len(positional) > 0 {
			if _, ok := findHelpCommand(positional[0]); ok {
				hint = "filegate " + positional[0] + " --help"
			}
		}
		return options{}, fmt.Errorf("%s; see: %s", message, hint)
	}
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
		case "config", "ttl", "base-url":
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
		} else if hasValue && name != "help" {
			return fail("--" + name + " does not take a value")
		}
		switch name {
		case "config":
			opts.config = value
		case "ttl":
			opts.action.TTL = value
		case "base-url":
			parsed, err := parseBaseURL(value)
			if err != nil {
				return fail(err.Error())
			}
			opts.baseURL = parsed
		case "help":
			if opts.action.Action != "" {
				return fail("choose either --help or --version")
			}
			if hasValue && (value == "" || !validHelpTopic(value)) {
				return fail("unknown help topic; use config, examples, limits, all or an action name")
			}
			opts.helpTopic = value
			opts.action.Action = "help"
		default:
			if opts.action.Action != "" {
				return fail("choose either --help or --version")
			}
			opts.action.Action = name
		}
	}
	if opts.action.Action == "help" {
		if len(positional) != 0 {
			command, ok := findHelpCommand(positional[0])
			if !ok {
				return fail("unknown action " + positional[0])
			}
			max := 1
			if command.argument != "" {
				max++
			}
			if opts.helpTopic != "" || len(positional) > max {
				return fail("choose a single command or help topic")
			}
			opts.helpTopic = command.name
		}
	} else if opts.action.Action != "" {
		if len(positional) != 0 {
			return fail("--version cannot be combined with an action")
		}
	} else {
		if len(positional) == 0 {
			return fail("an action is required")
		}
		opts.action.Action = positional[0]
		required := 0
		switch opts.action.Action {
		case "init", "check", "serve", "status":
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
	selected := opts.action.Action
	if selected == "help" {
		selected = opts.helpTopic
	}
	if seen["ttl"] {
		if selected != "code" {
			return fail("--ttl requires the code action")
		}
		duration, err := time.ParseDuration(opts.action.TTL)
		if err != nil || duration <= 0 {
			return fail("--ttl must be a positive duration, e.g. 30m")
		}
	}
	if seen["base-url"] && selected != "code" {
		return fail("--base-url requires the code action")
	}
	return opts, nil
}
