package main

import (
	"bytes"
	"errors"
	"os"
	"path/filepath"
	"regexp"
	"strings"
	"testing"
)

func TestHelpTopicsAndCommands(t *testing.T) {
	for _, topic := range []string{"", "config", "examples", "limits", "all", "init", "check", "serve", "status", "code", "revoke", "logout", "block", "unblock"} {
		t.Run(topic, func(t *testing.T) {
			base := t.TempDir()
			arg := "--help"
			if topic != "" {
				arg += "=" + topic
			}
			var output bytes.Buffer
			if err := runArgsWithEUID([]string{arg, "--config", filepath.Join(base, "missing", "config.json")}, &output, 0); err != nil {
				t.Fatal(err)
			}
			if output.Len() == 0 || strings.Contains(output.String(), "\x1b") {
				t.Fatal("empty or colored redirected help")
			}
			entries, err := os.ReadDir(base)
			if err != nil || len(entries) != 0 {
				t.Fatal("help created deployment data")
			}
			if command, ok := findHelpCommand(topic); ok {
				if !strings.Contains(output.String(), "filegate [--config PATH] "+command.name) || !strings.Contains(output.String(), command.summary) {
					t.Fatal("wrong command help")
				}
				var commandOutput bytes.Buffer
				if err := runArgsWithEUID([]string{topic, "--help"}, &commandOutput, 0); err != nil || commandOutput.String() != output.String() {
					t.Fatalf("command help differs: %v", err)
				}
			}
		})
	}
	for _, args := range [][]string{{"--help", "code"}, {"--config", "missing.json", "code", "ab", "--help"}, {"code", "--help", "--ttl", "30m"}, {"--help", "code", "--base-url", "http://192.0.2.1:17342"}} {
		opts, err := parseOptions(args)
		if err != nil || opts.action.Action != "help" || opts.helpTopic != "code" {
			t.Fatalf("help options %q: %+v %v", args, opts, err)
		}
	}
}

func TestHelpInvalidCombinations(t *testing.T) {
	for _, args := range [][]string{{"--help="}, {"--help=unknown"}, {"--help=config", "code"}, {"--help", "unknown"}, {"--help", "serve", "status"}, {"code", "ab", "extra", "--help"}, {"--help=all", "--help"}, {"--help=code", "--version"}, {"--help=limits", "--ttl", "30m"}, {"serve", "--help", "--ttl", "30m"}, {"--help", "code", "--ttl", "0s"}, {"--help", "--unknown"}} {
		t.Run(strings.Join(args, " "), func(t *testing.T) {
			var output bytes.Buffer
			if err := runArgs(args, &output); err == nil || output.Len() != 0 {
				t.Fatal("invalid help accepted or produced partial output")
			}
		})
	}
	for _, args := range [][]string{{"code"}, {"code", "ab", "--ttl", "bad"}, {"code", "ab", "--base-url", "http://example.com/path"}, {"unblock"}} {
		_, err := parseOptions(args)
		if err == nil || !strings.Contains(err.Error(), "filegate "+args[0]+" --help") {
			t.Fatalf("missing contextual hint: %v", err)
		}
	}
}

func TestHelpOverviewAndReference(t *testing.T) {
	if lines := strings.Count(usage, "\n"); lines > 30 {
		t.Fatalf("overview too long: %d lines", lines)
	}
	for _, heading := range []string{"Configuration defaults written by init:", "Fixed safety limits (not configuration fields):", "Server signals:", "# 4. Add a directory"} {
		if strings.Contains(usage, heading) {
			t.Fatal("overview includes detailed reference")
		}
	}
	for topic, terms := range map[string][]string{
		"config":   {"0.0.0.0:17342", "ch -> 文件夹", "logging.retention_days", "initialization defaults", "JSONC", "--config config.jsonc"},
		"code":     {"--ttl DURATION", "server's code_ttl (initially 10m)", "--base-url URL", "omitted", "not a directory name", "confirmation"},
		"serve":    {"SIGHUP", "SIGINT, SIGTERM", "restart", "non-root"},
		"check":    {"Read-only", "nothing is created", "service user", "point-in-time", "TLS"},
		"unblock":  {"No match is not an error", "still_blocked", "Failure counters"},
		"examples": {"mkdir -p tmp/demo/content/documents", "\"docs\": \"documents\"", "kill -HUP", "configuration reloaded", "code docs --ttl 30m"},
		"limits":   {"128", "32 globally, 4 per client IP", "10000", "Link confirmations"},
	} {
		for _, term := range terms {
			if !strings.Contains(helpText(topic), term) {
				t.Errorf("%s missing %q", topic, term)
			}
		}
	}
	all := helpText("all")
	for _, topic := range []string{"init", "serve", "status", "code", "revoke", "logout", "block", "unblock", "config", "examples", "limits"} {
		if !strings.Contains(all, helpText(topic)) {
			t.Errorf("all help missing %s", topic)
		}
	}
}

func TestHelpColorAndRedirection(t *testing.T) {
	plain := helpText("all")
	colored := formatHelp(plain, true)
	if !strings.Contains(colored, "\x1b[1;36mUsage:") || !strings.Contains(colored, "\x1b[1;34m--ttl") {
		t.Fatal("missing title/option emphasis")
	}
	strip := regexp.MustCompile(`\x1b\[[0-9;]*m`)
	if strip.ReplaceAllString(colored, "") != plain || formatHelp(plain, false) != plain {
		t.Fatal("color changes help content")
	}
	file, err := os.CreateTemp(t.TempDir(), "help-*")
	if err != nil {
		t.Fatal(err)
	}
	defer file.Close()
	if helpUsesColor(file) || helpUsesColor(&bytes.Buffer{}) {
		t.Fatal("regular file or buffer treated as terminal")
	}
	for name, value := range map[string]string{"NO_COLOR": "", "TERM": "dumb"} {
		t.Run(name, func(t *testing.T) {
			t.Setenv(name, value)
			if helpUsesColor(os.Stdout) {
				t.Fatal("color suppression ignored")
			}
		})
	}
	if err := writeHelp(file, "code"); err != nil {
		t.Fatal(err)
	}
	data, err := os.ReadFile(file.Name())
	if err != nil || string(data) != helpText("code") {
		t.Fatal("redirected output altered")
	}
}

func TestHelpSemanticColors(t *testing.T) {
	for _, tc := range []struct {
		line      string
		fragments []string
	}{
		{"  code ALIAS       Issue a one-time access code", []string{helpCommand + "code" + helpReset, helpArgument + "ALIAS" + helpReset, helpDescription + "Issue a one-time access code" + helpReset}},
		{"  --ttl DURATION   Default: the server setting", []string{helpOption + "--ttl" + helpReset, helpArgument + "DURATION" + helpReset, helpDescription + "Default: the server setting" + helpReset}},
		{"  filegate code ab  # Send the returned code", []string{helpCommand + "filegate" + helpReset, helpCommand + "code" + helpReset, helpArgument + "ab" + helpReset, helpComment + "# Send the returned code" + helpReset}},
		{"  # Edit config.json before reloading.", []string{helpComment + "# Edit config.json before reloading." + helpReset}},
		{"  logging.level    info (debug, info, warn, error)", []string{helpOption + "logging.level" + helpReset, helpDescription + "info (debug, info, warn, error)" + helpReset}},
		{"  filegate --help=examples", []string{helpOption + "--help=" + helpReset, helpArgument + "examples" + helpReset}},
	} {
		t.Run(tc.line, func(t *testing.T) {
			got := formatHelp(tc.line, true)
			for _, fragment := range tc.fragments {
				if !strings.Contains(got, fragment) {
					t.Errorf("missing semantic color %q in %q", fragment, got)
				}
			}
		})
	}
	for _, line := range []string{`  filegate code ab --base-url http://example.com/#fragment`, `  filegate code "#literal"`, `  filegate code '#literal'`} {
		if strings.Contains(formatHelp(line, true), helpComment) {
			t.Fatal("quoted hash or URL fragment treated as comment")
		}
	}
	if got := helpCommentStart(`  filegate code "#literal" # comment`); got != strings.LastIndex(`  filegate code "#literal" # comment`, "#") {
		t.Fatal("trailing comment after quote not recognized")
	}
}

type failedHelpOutput struct{}

func TestHelpOptionalBracketsUseSameColor(t *testing.T) {
	for _, text := range []string{"  --help[=TOPIC]   Show help", "Usage: filegate [OPTIONS] ACTION [ARGUMENT]", "Usage: filegate [--config PATH] code ALIAS"} {
		colored := formatHelp(text, true)
		for _, bracket := range []string{"[", "]"} {
			if !strings.Contains(colored, helpDescription+bracket+helpReset) {
				t.Fatalf("bracket %s lacks neutral color", bracket)
			}
		}
		if plain := regexp.MustCompile(`\x1b\[[0-9;]*m`).ReplaceAllString(colored, ""); plain != text {
			t.Fatal("bracket styling changed text")
		}
	}
	colored := formatHelp("  --help[=TOPIC]   Show help", true)
	if !strings.Contains(colored, helpOption+"--help"+helpReset) || !strings.Contains(colored, helpArgument+"TOPIC"+helpReset) {
		t.Fatal("bracket fix lost option or argument styling")
	}
}

func (failedHelpOutput) Write([]byte) (int, error) { return 0, errors.New("controlled output failure") }
func TestHelpOutputFailure(t *testing.T) {
	if err := runArgs([]string{"--help"}, failedHelpOutput{}); err == nil {
		t.Fatal("help swallowed output error")
	}
}
