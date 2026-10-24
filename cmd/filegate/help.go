package main

import (
	"fmt"
	"io"
	"os"
	"strings"
	"syscall"
	"unsafe"
)

type commandHelp struct {
	name, argument, summary, details string
}

var helpCommands = []commandHelp{
	{"init", "", "Create a new deployment", `Creates config.json (JSONC with field comments), content/ab, content/cd,
content/文件夹, state/, logs/ and blacklist.txt beside the configuration.
An existing configuration is never overwritten. Run only for a NEW deployment.

Examples:
  filegate --config tmp/demo/config.json init
  filegate --help=config
`},
	{"check", "", "Validate a deployment without starting it", `Read-only checks: JSONC and settings, actual directory mappings, private paths,
blacklist entries, existing state and the configured TLS certificate/key pair.
Missing state/log directories and initial state are allowed; nothing is created,
rewritten or chmodded. Does not acquire locks, bind ports or contact the server.

Run as the service user for meaningful read-permission checks. Success is a
point-in-time check, not proof of write access, port availability, certificate
trust/hostname/validity or successful startup/reload. Works while serve is running.

Examples:
  filegate --config tmp/demo/config.json check
  sudo -u filegate filegate --config /srv/filegate/config.json check
`},
	{"serve", "", "Run the server in the foreground", `Run as a dedicated non-root user, normally filegate. Effective UID 0 is
rejected before configuration is read. Use HTTP only on a trusted network.
The listen address comes from config.json (initial default: 0.0.0.0:17342).
Console/file logging settings and TLS are configured in the file.

Examples:
  filegate --config tmp/demo/config.json serve
  sudo -u filegate filegate --config /srv/filegate/config.json serve
`},
	{"status", "", "Inspect the running service", `Shows unused code IDs, aliases and expiries, temporary bans, the permanent
blacklist and session count as JSON. Does not return plaintext access codes.

Example:
  filegate --config tmp/demo/config.json status
`},
	{"code", "ALIAS", "Issue a one-time access code", `ALIAS is required: a configured key of 1-16 lowercase English letters,
not a directory name or path. Directory names may contain Chinese characters;
the initialized alias ch refers to 文件夹.

Options:
  --ttl DURATION   Default: the server's code_ttl (initially 10m).
                   Positive duration, e.g. 30s, 30m or 2h; this code only.
  --base-url URL   Default: omitted (no url output).
                   Public http(s) origin; no credentials, path, query or fragment.

Send the returned code or url to the user, not the id. The code permits one
successful login; sessions have separate timeouts. GET opens a confirmation
page; only its confirmation button consumes the code. Cookies are required.
At most 10 unused, unexpired codes may be issued per alias.

Examples:
  filegate code ab
  filegate code ch --ttl 30m
  filegate code ab --base-url http://192.0.2.10:17342
`},
	{"revoke", "ID", "Revoke an unused access code", `ID is required: use the id returned when issuing a code or shown by status.
A missing/used code ID returns an error. Existing sessions are unaffected;
use logout ALIAS to end sessions.

Example:
  filegate revoke CODE_ID
`},
	{"logout", "ALIAS", "End sessions for a directory", `ALIAS is required: a configured directory alias. Ends all sessions for that
alias and preserves other directories' sessions. Unused codes remain valid.

Example:
  filegate logout ab
`},
	{"block", "IP_OR_CIDR", "Add a permanent blacklist entry", `IP_OR_CIDR is required: an IPv4 address or CIDR network, with no expiry.
Matching sessions are revoked. IPv6 is not supported.

Examples:
  filegate block 203.0.113.25
  filegate block 198.51.100.0/24
`},
	{"unblock", "IP_OR_CIDR", "Remove bans and report the result", `IP_OR_CIDR is required: an IPv4 address or CIDR network.
Removes exact permanent entries and temporary bans within the target range.
Returns blacklist_removed, temporary_removed and an explanatory message.
No match is not an error. For a single IP, still_blocked and covering_blacklist
report remaining permanent coverage. A CIDR result does not mean every address
is unblocked. Failure counters remain; further attempts may cause another ban.

Example:
  filegate unblock 203.0.113.25
`},
}

func findHelpCommand(name string) (commandHelp, bool) {
	for _, command := range helpCommands {
		if command.name == name {
			return command, true
		}
	}
	return commandHelp{}, false
}

func validHelpTopic(topic string) bool {
	if _, ok := findHelpCommand(topic); ok {
		return true
	}
	switch topic {
	case "", "config", "examples", "limits", "all":
		return true
	}
	return false
}

const helpGlobal = `Global options:
  --config PATH    Default: config.json in the current working directory
  --help           Show this command's help; no configuration or service needed
  --version        Show version and build information (used without an action)
`

func commandHelpText(command commandHelp) string {
	invocation := "filegate [--config PATH] " + command.name
	if command.argument != "" {
		invocation += " " + command.argument
	}
	text := "Usage: " + invocation + "\n\n" + command.summary + "\n\n" + helpGlobal + "\n"
	if command.name != "init" && command.name != "serve" && command.name != "check" {
		text += "Requires the running server and its OS user (or root).\n\n"
	}
	text += command.details
	if command.name == "serve" {
		text += "\n" + helpSignals
	}
	return text
}

func helpText(topic string) string {
	switch topic {
	case "":
		return usage
	case "config":
		return helpConfig + "\n" + helpReference
	case "examples":
		return helpExamples
	case "limits":
		return helpLimits
	case "all":
		var text strings.Builder
		text.WriteString(usage)
		for _, command := range helpCommands {
			text.WriteString("\n" + commandHelpText(command))
		}
		for _, section := range []string{helpConfig, helpReference, helpLimits, helpExamples} {
			text.WriteString("\n" + section)
		}
		return text.String()
	default:
		command, _ := findHelpCommand(topic) // Validated by parseOptions.
		return commandHelpText(command)
	}
}

func helpUsesColor(output io.Writer) bool {
	if _, disabled := os.LookupEnv("NO_COLOR"); disabled || os.Getenv("TERM") == "dumb" {
		return false
	}
	file, ok := output.(*os.File)
	if !ok {
		return false
	}
	var state syscall.Termios
	_, _, errno := syscall.Syscall(syscall.SYS_IOCTL, file.Fd(), syscall.TCGETS, uintptr(unsafe.Pointer(&state)))
	return errno == 0
}

const (
	helpTitle       = "\x1b[1;36m"
	helpCommand     = "\x1b[1;32m"
	helpOption      = "\x1b[1;34m"
	helpArgument    = "\x1b[33m"
	helpDescription = "\x1b[37m"
	helpComment     = "\x1b[96m"
	helpReset       = "\x1b[0m"
)

func paintHelp(text, style string) string {
	if text == "" {
		return ""
	}
	return style + text + helpReset
}

// Only whitespace-delimited shell comments are colored; preserve quoted hashes
// and URL fragments. This formats static help, it does not execute shell input.
func helpCommentStart(line string) int {
	var quote byte
	escaped := false
	for i := 0; i < len(line); i++ {
		c := line[i]
		if escaped {
			escaped = false
			continue
		}
		if c == '\\' && quote != '\'' {
			escaped = true
			continue
		}
		if quote != 0 {
			if c == quote {
				quote = 0
			}
			continue
		}
		if c == '\'' || c == '"' {
			quote = c
			continue
		}
		if c == '#' && (i == 0 || line[i-1] == ' ' || line[i-1] == '\t') {
			return i
		}
	}
	return -1
}

// Optional-argument brackets use one neutral color independently of their contents.
func paintHelpToken(token, style string) string {
	var out strings.Builder
	start := 0
	for i, c := range token {
		if c == '[' || c == ']' {
			out.WriteString(paintHelp(token[start:i], style))
			out.WriteString(paintHelp(token[i:i+1], helpDescription))
			start = i + 1
		}
	}
	out.WriteString(paintHelp(token[start:], style))
	return out.String()
}

func helpSyntax(text string, label bool) string {
	var out strings.Builder
	for len(text) > 0 {
		space := strings.IndexFunc(text, func(c rune) bool { return c != ' ' && c != '\t' })
		if space < 0 {
			out.WriteString(text)
			break
		}
		out.WriteString(text[:space])
		text = text[space:]
		end := strings.IndexAny(text, " \t")
		if end < 0 {
			end = len(text)
		}
		token := text[:end]
		text = text[end:]
		bare := strings.Trim(token, "[]")
		if strings.HasPrefix(bare, "-") {
			if at := strings.IndexByte(token, '='); at >= 0 {
				out.WriteString(paintHelpToken(token[:at+1], helpOption))
				out.WriteString(paintHelpToken(token[at+1:], helpArgument))
				continue
			}
		}
		style := helpArgument
		if label {
			style = helpOption
		}
		_, command := findHelpCommand(bare)
		switch {
		case command || bare == "filegate" || bare == "mkdir" || bare == "ps" || bare == "kill":
			style = helpCommand
		case strings.HasPrefix(bare, "-"):
			style = helpOption
		case bare == "ALIAS" || bare == "ID" || bare == "IP_OR_CIDR" || bare == "IP/CIDR" || bare == "PATH" || bare == "URL" || bare == "DURATION" || bare == "ACTION" || bare == "ARGUMENT" || bare == "OPTIONS" || bare == "CODE_ID":
			style = helpArgument
		}
		out.WriteString(paintHelpToken(token, style))
	}
	return out.String()
}

func formatHelpLine(line string) string {
	comment := ""
	if at := helpCommentStart(line); at >= 0 {
		comment = paintHelp(line[at:], helpComment)
		line = line[:at]
	}
	if strings.TrimSpace(line) == "" {
		return line + comment
	}
	if strings.HasPrefix(line, "Usage:") {
		return paintHelp("Usage:", helpTitle) + helpSyntax(strings.TrimPrefix(line, "Usage:"), false) + comment
	}
	if !strings.HasPrefix(line, " ") && strings.HasSuffix(line, ":") {
		return paintHelp(line, helpTitle) + comment
	}
	trimmed := strings.TrimLeft(line, " \t")
	indent := line[:len(line)-len(trimmed)]
	if len(indent) > 0 {
		if at := strings.Index(trimmed, "  "); at >= 0 && strings.TrimSpace(trimmed[at:]) != "" {
			tail := trimmed[at:]
			description := strings.TrimLeft(tail, " ")
			gap := tail[:len(tail)-len(description)]
			return indent + helpSyntax(trimmed[:at], true) + gap + paintHelp(description, helpDescription) + comment
		}
		first := strings.Fields(trimmed)[0]
		if first == "filegate" || first == "mkdir" || first == "ps" || first == "kill" {
			return indent + helpSyntax(trimmed, false) + comment
		}
	}
	return indent + paintHelp(trimmed, helpDescription) + comment
}

func formatHelp(text string, color bool) string {
	if !color {
		return text
	}
	lines := strings.Split(text, "\n")
	for i, line := range lines {
		lines[i] = formatHelpLine(line)
	}
	return strings.Join(lines, "\n")
}

func writeHelp(output io.Writer, topic string) error {
	_, err := fmt.Fprint(output, formatHelp(helpText(topic), helpUsesColor(output)))
	return err
}

const usage = `Usage: filegate [OPTIONS] ACTION [ARGUMENT]
       filegate ACTION --help

Actions:
  init             Create configuration and example directories
  check            Validate configuration and files without starting
  serve            Run the server in the foreground (non-root)
  status           Show active code IDs, bans and session count
  code ALIAS       Issue a one-time access code
  revoke ID        Revoke an unused code
  logout ALIAS     End all sessions for a directory alias
  block IP/CIDR    Add a permanent blacklist entry
  unblock IP/CIDR  Remove matching bans and report changes

Global options:
  --config PATH    Configuration file (default: config.json)
  --help[=TOPIC]   Overview, command help or a topic below
  --version        Show version and build information

Quick start:
  filegate init
  filegate check
  filegate serve
  filegate code ab  # Run in another terminal; send the returned code
More help:
  filegate code --help      Command options, defaults and examples
  filegate --help=config    Configuration defaults and path rules
  filegate --help=examples  Full setup and new-directory walkthrough
  filegate --help=limits    Fixed safety limits
  filegate --help=all       Complete reference (can pipe to less)
`

const helpConfig = `Configuration defaults written by init:
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

`

const helpLimits = `Fixed safety limits (not configuration fields):
  TCP connections        128; excess connections wait for a free slot
  Active HTTP requests   128 (including HTTP/2 streams); excess returns 429
  File requests          32 globally, 4 per client IP; excess returns 429
  Response write idle    30s, renewed as chunks are written
  Directory entries      10000 raw entries; larger listings return 503
  Active codes per alias 10; revoke unused codes or wait for expiry
  Range requests         One range, at most 128 header bytes; else returns 400
  Link confirmations     5m, 1000 globally / 10 per IPv4; memory only.
                         Each browser retains only its newest prepared link.
                         Access-code expiry is unchanged; cookies required.

`

const helpSignals = `Server signals:
  SIGHUP                 Reload directory mappings and runtime limits;
                         failure keeps the previous configuration
  SIGINT, SIGTERM        Shut down gracefully
Changing the listener, root, state/blacklist paths, logging or TLS requires
a restart. Blacklist file contents take effect without a signal.

`

const helpExamples = `Examples:
  # 1. Initialize a NEW deployment (skip if its config already exists).
  filegate --config tmp/demo/config.json init
  # Creates content/ab, content/cd, content/文件夹, state, logs and blacklist.txt
  # under tmp/demo/. config.json contains // explanations above key fields.

  # 2. Start the server. Keep this terminal open; use another for management.
  filegate --config tmp/demo/config.json check
  filegate --config tmp/demo/config.json serve
  # Defaults: browse http://SERVER_IP:17342; use the listen port from your config.
  # Colored levels appear on a terminal; plain logs go to tmp/demo/logs/filegate.log.
  # Redirected console output is uncolored unless logging.color is always.
  # Logs rotate at 10 MiB / UTC date changes; keep <=10 archives for <=7 days.

  # 3. Issue an access code for the existing ab directory.
  filegate --config tmp/demo/config.json code ab
  filegate --config tmp/demo/config.json code ab --ttl 30m
  filegate --config tmp/demo/config.json code ch --ttl 30m
  filegate --config tmp/demo/config.json code ab --ttl 30m --base-url http://192.0.2.10:17342
  # Send the returned url to the user. Replace 192.0.2.10 with your public address.
  # Opening it prepares a clean confirmation page; only the button consumes the code.
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
  filegate --config tmp/demo/config.json check
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
  # Replace CODE_ID with the id returned at creation or shown by status.`

const helpReference = `Argument and configuration rules:
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
All help pages and --version work without configuration or a running server.
Configuration accepts JSONC // and /* */ comments, or plain JSON, regardless
of extension. The default filename remains config.json; --config config.jsonc
is also supported. Trailing commas and JSON5 syntax are not supported.
Options may appear in any order. Values accept --option VALUE or
--option=VALUE, before or after the action. Actions use bare words (serve,
code, etc.); prefixed actions such as --serve and single-dash options are rejected.

`
