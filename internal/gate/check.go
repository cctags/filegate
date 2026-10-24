package gate

import (
	"crypto/tls"
	"encoding/json"
	"errors"
	"fmt"
	"io"
	"os"
	"path/filepath"
	"syscall"
)

// Check validates a deployment without initializing state, acquiring locks,
// starting logging or contacting the running service. It is a point-in-time
// read check, not a guarantee that a later start, reload or write will succeed.
func Check(path string) error {
	info, err := os.Stat(path)
	if err != nil {
		return err
	}
	if !info.Mode().IsRegular() {
		return errors.New("configuration must be a regular file")
	}
	c, err := Load(path)
	if err != nil {
		return err
	}
	content, err := filepath.EvalSymlinks(c.Root)
	if err != nil {
		return err
	}
	for _, item := range []struct{ name, path string }{
		{"state_dir", c.StateDir}, {"blacklist", c.Blacklist},
		{"state file", filepath.Join(c.StateDir, "state.json")},
		{"logging.directory", c.Logging.Directory}, {"tls_key", c.TLSKey},
	} {
		if item.path == "" {
			continue
		}
		actual, err := checkPath(item.path)
		if err != nil {
			return fmt.Errorf("%s: %w", item.name, err)
		}
		rel, err := filepath.Rel(content, actual)
		if err != nil || filepath.IsLocal(rel) {
			return fmt.Errorf("%s must be outside the content root (including symlinks)", item.name)
		}
	}
	root, err := os.OpenRoot(c.Root)
	if err != nil {
		return err
	}
	defer root.Close()
	roots, err := openDirectories(root, c.Directories)
	if err != nil {
		return err
	}
	defer closeRoots(roots)
	for _, item := range []struct{ name, path string }{
		{"state_dir", c.StateDir}, {"logging.directory", c.Logging.Directory},
	} {
		if err := checkOptionalDirectory(item.path); err != nil {
			return fmt.Errorf("%s: %w", item.name, err)
		}
	}
	if info, err := os.Lstat(filepath.Join(c.Logging.Directory, "filegate.log")); err == nil {
		if !info.Mode().IsRegular() {
			return errors.New("filegate.log must be a regular file, not a symlink")
		}
	} else if !os.IsNotExist(err) {
		return fmt.Errorf("log file: %w", err)
	}
	statePath := filepath.Join(c.StateDir, "state.json")
	// Missing state is valid before the first serve. A dangling symlink is not.
	if _, err := os.Lstat(statePath); err == nil {
		data, err := readCheckFile(statePath)
		if err != nil {
			return fmt.Errorf("state file: %w", err)
		}
		var state State
		if err := json.Unmarshal(data, &state); err != nil {
			// A field's parse error may quote sensitive input from the state.
			return errors.New("invalid state file JSON or field types")
		}
		if err := validateState(state); err != nil {
			return err
		}
	} else if !os.IsNotExist(err) {
		return fmt.Errorf("state file: %w", err)
	}
	data, err := readCheckFile(c.Blacklist)
	if err != nil {
		return fmt.Errorf("blacklist: %w", err)
	}
	if _, err := parseBlacklist(data); err != nil {
		return err
	}
	if c.TLSCert != "" {
		cert, err := readCheckFile(c.TLSCert)
		if err != nil {
			return fmt.Errorf("tls_cert: %w", err)
		}
		key, err := readCheckFile(c.TLSKey)
		if err != nil {
			return fmt.Errorf("tls_key: %w", err)
		}
		if _, err := tls.X509KeyPair(cert, key); err != nil {
			return fmt.Errorf("TLS certificate/key pair: %w", err)
		}
	}
	return nil
}

// Resolve the nearest existing ancestor for paths serve may create later.
// Unlike simply ignoring ENOENT, reject dangling symlinks and non-directories.
func checkPath(path string) (string, error) {
	if _, err := os.Lstat(path); err == nil {
		return filepath.EvalSymlinks(path)
	} else if !os.IsNotExist(err) {
		return "", err
	}
	parent := filepath.Dir(path)
	if parent == path {
		return "", fmt.Errorf("cannot resolve %s", path)
	}
	actual, err := checkPath(parent)
	if err != nil {
		return "", err
	}
	if info, err := os.Stat(actual); err == nil {
		if !info.IsDir() {
			return "", fmt.Errorf("%s is not a directory", parent)
		}
	} else if !os.IsNotExist(err) {
		return "", err
	}
	return filepath.Join(actual, filepath.Base(path)), nil
}

func checkOptionalDirectory(path string) error {
	if _, err := os.Lstat(path); os.IsNotExist(err) {
		return nil
	} else if err != nil {
		return err
	}
	f, err := os.OpenFile(path, os.O_RDONLY|syscall.O_NONBLOCK, 0)
	if err != nil {
		return err
	}
	defer f.Close()
	info, err := f.Stat()
	if err != nil {
		return err
	}
	if !info.IsDir() {
		return fmt.Errorf("%s is not a directory", path)
	}
	return nil
}

func readCheckFile(path string) ([]byte, error) {
	// O_NONBLOCK avoids waiting forever if a configured file is a FIFO.
	f, err := os.OpenFile(path, os.O_RDONLY|syscall.O_NONBLOCK, 0)
	if err != nil {
		return nil, err
	}
	defer f.Close()
	info, err := f.Stat()
	if err != nil {
		return nil, err
	}
	if !info.Mode().IsRegular() {
		return nil, fmt.Errorf("%s is not a regular file", path)
	}
	return io.ReadAll(f)
}
