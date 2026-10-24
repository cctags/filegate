package main

import (
	"bytes"
	"os"
	"path/filepath"
	"strings"
	"testing"
)

func TestRootServeRejectedBeforeConfiguration(t *testing.T) {
	base := t.TempDir()
	config := filepath.Join(base, "missing", "config.json")
	var out bytes.Buffer
	err := runArgsWithEUID([]string{"--config", config, "serve"}, &out, 0)
	if err == nil || !strings.Contains(err.Error(), "refusing to serve as root") || !strings.Contains(err.Error(), "sudo -u filegate") {
		t.Fatalf("expected root rejection and guidance, got %v", err)
	}
	entries, err := os.ReadDir(base)
	if err != nil || len(entries) != 0 || out.Len() != 0 {
		t.Fatalf("root rejection had side effects: entries=%v, output=%q, error=%v", entries, out.String(), err)
	}
}

func TestNonRootServeReachesConfiguration(t *testing.T) {
	config := filepath.Join(t.TempDir(), "missing.json")
	var out bytes.Buffer
	err := runArgsWithEUID([]string{"serve", "--config", config}, &out, 1000)
	if !os.IsNotExist(err) {
		t.Fatalf("expected missing configuration, got %v", err)
	}
}

func TestRootOtherActionsAllowed(t *testing.T) {
	for _, action := range []string{"--help", "--version", "init", "status", "code", "revoke", "logout", "block", "unblock"} {
		t.Run(action, func(t *testing.T) {
			config := filepath.Join(t.TempDir(), "config.json")
			args := []string{"--config", config, action}
			switch action {
			case "code", "logout":
				args = append(args, "ab")
			case "revoke":
				args = append(args, "example-id")
			case "block", "unblock":
				args = append(args, "192.0.2.1")
			}
			var out bytes.Buffer
			err := runArgsWithEUID(args, &out, 0)
			switch action {
			case "--help", "--version", "init":
				if err != nil {
					t.Fatal(err)
				}
			default:
				if !os.IsNotExist(err) {
					t.Fatalf("expected configuration read, got %v", err)
				}
			}
		})
	}
}

func TestRootServeValidatesArgumentsFirst(t *testing.T) {
	var out bytes.Buffer
	err := runArgsWithEUID([]string{"serve", "--unknown"}, &out, 0)
	if err == nil || !strings.Contains(err.Error(), "unknown option") {
		t.Fatalf("expected argument validation, got %v", err)
	}
}
