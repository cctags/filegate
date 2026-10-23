package gate

import (
	"bytes"
	"encoding/json"
	"errors"
	"fmt"
	"io"
	"os"
	"path/filepath"
	"regexp"
	"time"

	"filegate/internal/logging"
)

type Config struct {
	Logging                     logging.Config    `json:"logging"`
	Listen                      string            `json:"listen"`
	Root                        string            `json:"root"`
	StateDir                    string            `json:"state_dir"`
	Blacklist                   string            `json:"blacklist"`
	Directories                 map[string]string `json:"directories"`
	CodeTTL                     string            `json:"code_ttl"`
	IdleTimeout                 string            `json:"idle_timeout"`
	MaxSession                  string            `json:"max_session"`
	FailureWindow               string            `json:"failure_window"`
	BanDuration                 string            `json:"ban_duration"`
	MaxFailures                 int               `json:"max_failures"`
	AttemptsPerMinute           int               `json:"attempts_per_minute"`
	TLSCert                     string            `json:"tls_cert,omitempty"`
	TLSKey                      string            `json:"tls_key,omitempty"`
	ttl, idle, max, window, ban time.Duration
}

func Load(path string) (Config, error) {
	c := Config{Logging: logging.Defaults()}
	f, err := os.Open(path)
	if err != nil {
		return c, err
	}
	defer f.Close()
	const maxConfigSize = 1 << 20
	data, err := io.ReadAll(io.LimitReader(f, maxConfigSize+1))
	if err != nil {
		return c, err
	}
	if len(data) > maxConfigSize {
		return c, errors.New("configuration exceeds 1 MiB")
	}
	data, err = stripJSONComments(data)
	if err != nil {
		return c, err
	}
	d := json.NewDecoder(bytes.NewReader(data))
	d.DisallowUnknownFields()
	if err = d.Decode(&c); err != nil {
		return c, err
	}
	if d.Decode(new(any)) != io.EOF {
		return c, errors.New("configuration must contain one JSON object")
	}
	base, err := filepath.Abs(filepath.Dir(path))
	if err != nil {
		return c, err
	}
	resolve := func(p string) string {
		if filepath.IsAbs(p) {
			return p
		}
		return filepath.Join(base, p)
	}
	if c.Listen == "" || c.Root == "" || c.StateDir == "" || c.Blacklist == "" || len(c.Directories) == 0 {
		return c, errors.New("listen, root, state_dir, blacklist and directories are required")
	}
	c.Root = resolve(c.Root)
	c.StateDir = resolve(c.StateDir)
	c.Blacklist = resolve(c.Blacklist)
	if err := c.Logging.Validate(); err != nil {
		return c, err
	}
	c.Logging.Directory = resolve(c.Logging.Directory)
	if err := logging.ValidateDirectory(c.Logging.Directory, c.Root); err != nil {
		return c, err
	}
	if rel, e := filepath.Rel(c.Root, resolve(filepath.Base(path))); e == nil && filepath.IsLocal(rel) {
		return c, errors.New("configuration must be outside the content root")
	}
	if c.TLSCert != "" {
		c.TLSCert = resolve(c.TLSCert)
	}
	if c.TLSKey != "" {
		c.TLSKey = resolve(c.TLSKey)
	}
	if (c.TLSCert == "") != (c.TLSKey == "") {
		return c, errors.New("tls_cert and tls_key must be set together")
	}
	for _, item := range []struct {
		name, value string
		target      *time.Duration
	}{
		{"code_ttl", c.CodeTTL, &c.ttl}, {"idle_timeout", c.IdleTimeout, &c.idle}, {"max_session", c.MaxSession, &c.max}, {"failure_window", c.FailureWindow, &c.window}, {"ban_duration", c.BanDuration, &c.ban},
	} {
		v, e := time.ParseDuration(item.value)
		if e != nil || v <= 0 {
			return c, fmt.Errorf("%s must be a positive duration", item.name)
		}
		*item.target = v
	}
	if c.MaxFailures < 1 || c.AttemptsPerMinute < 1 {
		return c, errors.New("attempt limits must be positive")
	}
	re := regexp.MustCompile(`^[a-z]{1,16}$`)
	for alias, dir := range c.Directories {
		if !re.MatchString(alias) || dir == "." || !filepath.IsLocal(dir) {
			return c, fmt.Errorf("invalid alias or relative directory: %s", alias)
		}
	}
	return c, nil
}

func atomicWrite(path string, data []byte) error {
	f, err := os.CreateTemp(filepath.Dir(path), ".filegate-*")
	if err != nil {
		return err
	}
	name := f.Name()
	defer os.Remove(name)
	if err = f.Chmod(0600); err == nil {
		_, err = f.Write(data)
	}
	if err == nil {
		err = f.Sync()
	}
	closeErr := f.Close()
	if err == nil {
		err = closeErr
	}
	if err != nil {
		return err
	}
	if err = os.Rename(name, path); err != nil {
		return err
	}
	dir, err := os.Open(filepath.Dir(path))
	if err != nil {
		return err
	}
	defer dir.Close()
	return dir.Sync()
}
