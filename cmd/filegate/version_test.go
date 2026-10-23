package main

import (
	"bytes"
	"path/filepath"
	"runtime"
	"strings"
	"testing"
)

func TestVersionWithoutConfiguration(t *testing.T) {
	for _, flag := range []string{"--version"} {
		t.Run(flag, func(t *testing.T) {
			config := filepath.Join(t.TempDir(), "missing.json")
			var out bytes.Buffer
			if err := runArgs([]string{"--config", config, flag}, &out); err != nil {
				t.Fatal(err)
			}
			for _, want := range []string{"filegate " + version, "commit: ", "modified: ", "built: " + buildTime, "go: " + runtime.Version(), "platform: " + runtime.GOOS + "/" + runtime.GOARCH} {
				if !strings.Contains(out.String(), want) {
					t.Errorf("version output missing %q: %s", want, out.String())
				}
			}
		})
	}
}

func TestVersionRejectsCommand(t *testing.T) {
	var out bytes.Buffer
	if err := runArgs([]string{"--version", "serve"}, &out); err == nil {
		t.Fatal("--version must not start a command")
	}
	if out.Len() != 0 {
		t.Fatal("unexpected version output for invalid arguments")
	}
}
