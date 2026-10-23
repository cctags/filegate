package gate

import (
	"bytes"
	"encoding/json"
	"log/slog"
	"net/http/httptest"
	"os"
	"path/filepath"
	"strings"
	"testing"
)

func TestAccessLogsAndCredentialRedaction(t *testing.T) {
	var output bytes.Buffer
	previous := slog.Default()
	slog.SetDefault(slog.New(slog.NewTextHandler(&output, nil)))
	defer slog.SetDefault(previous)
	a := setup(t)
	created := createCode(t, a, "ab")
	cookie := loginCode(t, a, created.Code)
	r := httptest.NewRequest("GET", "/file/hello.txt?download=1&secret=QUERY_SECRET", nil)
	r.RemoteAddr = "192.0.2.1:1234"
	r.AddCookie(cookie)
	r.Header.Set("Authorization", "Bearer HEADER_SECRET")
	r.Header.Set("Range", "bytes=0-4")
	w := httptest.NewRecorder()
	a.Handler().ServeHTTP(w, r)
	if w.Code != 206 {
		t.Fatal(w.Code)
	}
	request(a, "GET", "/file/missing", "", cookie)
	a.Admin(Request{Action: "revoke", Value: "INVALID_REVOKE_SECRET"})
	a.Admin(Request{Action: "unknown", Value: "INVALID_ARGUMENT_SECRET"})
	originRequest := httptest.NewRequest("POST", "/login?secret=ORIGIN_QUERY_SECRET", strings.NewReader("code="+created.Code))
	originRequest.RemoteAddr = "192.0.2.1:1234"
	originRequest.Header.Set("Origin", "https://user:ORIGIN_PASSWORD_SECRET@evil.example/path?secret=ORIGIN_QUERY_SECRET")
	originRequest.Header.Set("Sec-Fetch-Site", "FETCH_HEADER_SECRET")
	a.Handler().ServeHTTP(httptest.NewRecorder(), originRequest)
	data := output.String()
	for _, secret := range []string{created.Code, cookie.Value, a.state.Secret, "QUERY_SECRET", "HEADER_SECRET", "INVALID_REVOKE_SECRET", "INVALID_ARGUMENT_SECRET", "ORIGIN_PASSWORD_SECRET", "ORIGIN_QUERY_SECRET", "FETCH_HEADER_SECRET"} {
		if strings.Contains(data, secret) {
			t.Fatalf("credential leaked to logs: %q", secret)
		}
	}
	for _, field := range []string{"http request", "path=/file/hello.txt", "status=206", "bytes=5", "alias=ab", "download=true", "status=404", "admin operation"} {
		if !strings.Contains(data, field) {
			t.Errorf("missing field %s: %s", field, data)
		}
	}
}

func TestLoggingDefaultsAndReloadRestriction(t *testing.T) {
	a := setup(t)
	file, c := reloadConfig(t, a)
	// Old configurations with no logging object inherit safe defaults.
	data, _ := os.ReadFile(file)
	start := bytes.Index(data, []byte(`"logging":`))
	if start < 0 {
		t.Fatal("missing logging field")
	}
	// Use a decoded map so field order and nested braces do not matter.
	config := map[string]any{}
	if err := json.Unmarshal(data, &config); err != nil {
		t.Fatal(err)
	}
	delete(config, "logging")
	data, _ = json.Marshal(config)
	os.WriteFile(file, data, 0600)
	loaded, err := Load(file)
	if err != nil {
		t.Fatal(err)
	}
	if loaded.Logging.Directory != filepath.Join(filepath.Dir(file), "logs") || loaded.Logging.RetentionDays != 7 || loaded.Logging.MaxFiles != 10 {
		t.Fatalf("invalid defaults: %+v", loaded.Logging)
	}
	c.Logging.MaxFiles = 2
	writeReloadConfig(t, file, c)
	if _, err = a.Reload(file); err == nil || !strings.Contains(err.Error(), "logging") {
		t.Fatalf("logging change should require restart: %v", err)
	}
	c.Logging.Directory = filepath.Join(c.Root, "logs")
	writeReloadConfig(t, file, c)
	if _, err = Load(file); err == nil {
		t.Fatal("log directory inside content accepted")
	}
}

func TestPartialLoggingConfiguration(t *testing.T) {
	a := setup(t)
	file, _ := reloadConfig(t, a)
	data, err := os.ReadFile(file)
	if err != nil {
		t.Fatal(err)
	}
	var config map[string]any
	if err = json.Unmarshal(data, &config); err != nil {
		t.Fatal(err)
	}
	config["logging"] = map[string]any{"level": "warn"}
	data, _ = json.Marshal(config)
	os.WriteFile(file, data, 0600)
	loaded, err := Load(file)
	if err != nil {
		t.Fatal(err)
	}
	if loaded.Logging.Level != "warn" || loaded.Logging.MaxSizeMB != 10 || loaded.Logging.MaxFiles != 10 || loaded.Logging.RetentionDays != 7 || loaded.Logging.Color != "auto" {
		t.Fatalf("partial config lost defaults: %+v", loaded.Logging)
	}
	config["logging"] = map[string]any{"directory": ""}
	data, _ = json.Marshal(config)
	os.WriteFile(file, data, 0600)
	if _, err = Load(file); err == nil {
		t.Fatal("explicit empty logging directory accepted")
	}
}
