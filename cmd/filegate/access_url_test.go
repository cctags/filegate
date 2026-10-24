package main

import (
	"bytes"
	"path/filepath"
	"strings"
	"testing"
)

func TestCodeBaseURL(t *testing.T) {
	for _, base := range []string{"http://192.0.2.10:17342", "https://files.example/"} {
		opts, err := parseOptions([]string{"code", "ab", "--base-url", base, "--ttl", "30m"})
		if err != nil {
			t.Fatal(err)
		}
		result := accessURL(opts.baseURL, "ab01234567")
		if !strings.HasSuffix(result, "/access?code=ab01234567") {
			t.Fatalf("unexpected URL: %s", result)
		}
	}
	for _, base := range []string{"", "ftp://files.example", "http://user:pass@files.example", "http://files.example/path", "http://files.example?secret=x", "http://files.example?", "http://files.example#fragment", "http://files.example#", "http://[::1]:17342", "http://0.0.0.0:17342", "http://files.example:0", "http://files.example:65536", "http://files.example:", "//files.example"} {
		args := []string{"code", "ab", "--base-url=" + base}
		if _, err := parseOptions(args); err == nil {
			t.Fatalf("accepted base URL %q", base)
		}
	}
	for _, args := range [][]string{{"serve", "--base-url", "http://files.example"}, {"init", "--base-url", "http://files.example"}, {"--help", "--base-url", "http://files.example"}, {"code", "ab", "--base-url", "http://files.example", "--base-url", "http://files.example"}} {
		if _, err := parseOptions(args); err == nil {
			t.Fatalf("accepted flags: %q", args)
		}
	}
	var out bytes.Buffer
	err := runArgs([]string{"--config", filepath.Join(t.TempDir(), "missing.json"), "code", "ab", "--base-url", "http://files.example/path"}, &out)
	if err == nil || !strings.Contains(err.Error(), "--base-url") {
		t.Fatal("invalid URL did not fail before configuration or code generation")
	}
}
