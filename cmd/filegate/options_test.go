package main

import (
	"bytes"
	"os"
	"path/filepath"
	"strings"
	"testing"

	"filegate/internal/gate"
)

func TestActionsAndLongOptions(t *testing.T) {
	for _, action := range []string{"init", "serve", "status", "version", "help", "code", "revoke", "logout", "block", "unblock"} {
		t.Run(action, func(t *testing.T) {
			args := []string{action}
			if action == "help" || action == "version" {
				args[0] = "--" + action
			}
			value := ""
			switch action {
			case "code", "revoke", "logout", "block", "unblock":
				value = "example"
				args = append(args, value)
			}
			opts, err := parseOptions(args)
			if err != nil {
				t.Fatal(err)
			}
			if opts.action.Action != action || opts.action.Value != value || opts.config != "config.json" {
				t.Fatalf("unexpected options: %+v", opts)
			}
		})
	}
	for _, args := range [][]string{
		{"--config", "custom.json", "code", "ab", "--ttl", "30m"},
		{"--ttl=30m", "code", "ab", "--config=custom.json"},
		{"code", "--config", "custom.json", "ab", "--ttl", "30m"},
	} {
		opts, err := parseOptions(args)
		if err != nil {
			t.Fatal(err)
		}
		if opts.config != "custom.json" || opts.action.Action != "code" || opts.action.Value != "ab" || opts.action.TTL != "30m" {
			t.Fatalf("unexpected options for %q: %+v", args, opts)
		}
	}
}

func TestInvalidLongOptions(t *testing.T) {
	for _, args := range [][]string{
		{}, {"unknown"}, {"-config", "config.json", "serve"}, {"-version"}, {"--unknown"},
		{"--serve"}, {"--init"}, {"--status"}, {"--code", "ab"}, {"--block", "192.0.2.1"},
		{"serve", "status"}, {"serve", "serve"}, {"serve=false"}, {"code=ab"},
		{"code"}, {"code", ""}, {"revoke"}, {"logout"}, {"block"}, {"unblock"},
		{"code", "ab", "30m"}, {"code", "ab", "--ttl"}, {"--ttl", "30m", "status"},
		{"code", "ab", "--ttl", "0s"}, {"code", "ab", "--ttl", "-1m"}, {"code", "ab", "--ttl", "invalid"},
		{"--config"}, {"--config="}, {"--config", "a", "--config", "b", "serve"},
		{"--version", "serve"}, {"--version", "--help"}, {"--help=true"},
		{"--"}, {"--version", "unexpected"}, {"--ttl", "30m", "--version"},
	} {
		t.Run(strings.Join(args, " "), func(t *testing.T) {
			if _, err := parseOptions(args); err == nil {
				t.Fatalf("accepted invalid options %q", args)
			}
		})
	}
}

func TestHelpWithoutConfiguration(t *testing.T) {
	var out bytes.Buffer
	if err := runArgs([]string{"--help", "--config", filepath.Join(t.TempDir(), "missing.json")}, &out); err != nil {
		t.Fatal(err)
	}
	for _, text := range []string{"init", "serve", "--config PATH", "code ALIAS", "filegate code --help", "block IP/CIDR", "--version"} {
		if !strings.Contains(out.String(), text) {
			t.Errorf("missing help for %s", text)
		}
	}
}

func TestLongOptionInitialization(t *testing.T) {
	config := filepath.Join(t.TempDir(), "config.json")
	var out bytes.Buffer
	if err := runArgs([]string{"init", "--config=" + config}, &out); err != nil {
		t.Fatal(err)
	}
	original, err := os.ReadFile(config)
	if err != nil {
		t.Fatal(err)
	}
	cfg, err := gate.Load(config)
	if err != nil {
		t.Fatalf("generated JSONC configuration cannot be loaded: %v", err)
	}
	if cfg.Directories["ch"] != "文件夹" || !bytes.Contains(original, []byte("//")) {
		t.Fatal("missing Chinese directory or field comments")
	}
	for _, directory := range cfg.Directories {
		info, err := os.Stat(filepath.Join(cfg.Root, directory))
		if err != nil || !info.IsDir() {
			t.Fatalf("configured directory %q was not created", directory)
		}
	}
	app, err := gate.New(cfg)
	if err != nil {
		t.Fatal(err)
	}
	defer app.Close()
	if reply := app.Admin(gate.Request{Action: "code", Value: "ch"}); reply.Error != "" {
		t.Fatalf("cannot issue Chinese directory code: %s", reply.Error)
	}

	if strings.Contains(out.String(), "--serve") || !strings.Contains(out.String(), " serve") {
		t.Fatal("initialization hint uses old syntax")
	}
	if err = runArgs([]string{"--config", config, "init"}, &out); err == nil {
		t.Fatal("existing config overwritten")
	}
	actual, err := os.ReadFile(config)
	if err != nil || !bytes.Equal(original, actual) {
		t.Fatal("existing config changed")
	}
}
