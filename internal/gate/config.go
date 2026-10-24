package gate

import (
	"bytes"
	"encoding/json"
	"errors"
	"fmt"
	"io"
	"net"
	"net/netip"
	"os"
	"path/filepath"
	"regexp"
	"strconv"
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
	host, port, err := net.SplitHostPort(c.Listen)
	if err != nil {
		return c, errors.New("listen must be an IPv4 address and numeric port")
	}
	ip, err := netip.ParseAddr(host)
	number, portErr := strconv.Atoi(port)
	if err != nil || !ip.Is4() || portErr != nil || number < 1 || number > 65535 {
		return c, errors.New("listen must be an IPv4 address and port from 1 to 65535")
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
	configPath, err := filepath.Abs(path)
	if err != nil {
		return c, err
	}
	actual, err := filepath.EvalSymlinks(configPath)
	if err != nil {
		return c, err
	}
	content, err := filepath.EvalSymlinks(c.Root)
	if err != nil {
		return c, err
	}
	opened, err := f.Stat()
	if err != nil {
		return c, err
	}
	current, err := os.Stat(actual)
	if err != nil {
		return c, err
	}
	if !opened.Mode().IsRegular() || !os.SameFile(opened, current) {
		return c, errors.New("configuration changed while loading or is not a regular file")
	}
	if rel, err := filepath.Rel(content, actual); err != nil || filepath.IsLocal(rel) {
		return c, errors.New("configuration must be outside the content root (including symlinks)")
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
		if !re.MatchString(alias) || filepath.Clean(dir) == "." || !filepath.IsLocal(dir) || hasParentComponent(dir) {
			return c, fmt.Errorf("invalid alias or relative directory: %s", alias)
		}
		c.Directories[alias] = filepath.Clean(dir)
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
