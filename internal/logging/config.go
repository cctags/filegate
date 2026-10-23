// Package logging provides standard-library-only console and rotating file logs.
package logging

import (
	"errors"
	"fmt"
	"os"
	"path/filepath"
	"strings"
)

type Config struct {
	Directory     string `json:"directory"`
	Level         string `json:"level"`
	Color         string `json:"color"`
	MaxSizeMB     int    `json:"max_size_mb"`
	MaxFiles      int    `json:"max_files"`
	RetentionDays int    `json:"retention_days"`
}

func Defaults() Config {
	return Config{Directory: "logs", Level: "info", Color: "auto", MaxSizeMB: 10, MaxFiles: 10, RetentionDays: 7}
}
func (c Config) Validate() error {
	if c.Directory == "" {
		return errors.New("logging.directory must not be empty")
	}
	switch strings.ToLower(c.Level) {
	case "debug", "info", "warn", "error":
	default:
		return fmt.Errorf("logging.level must be debug, info, warn or error: %q", c.Level)
	}
	switch c.Color {
	case "auto", "always", "never":
	default:
		return errors.New("logging.color must be auto, always or never")
	}
	if c.MaxSizeMB < 1 || c.MaxSizeMB > 1024 {
		return errors.New("logging.max_size_mb must be between 1 and 1024")
	}
	if c.MaxFiles < 1 || c.MaxFiles > 10000 {
		return errors.New("logging.max_files must be between 1 and 10000")
	}
	if c.RetentionDays < 1 || c.RetentionDays > 36500 {
		return errors.New("logging.retention_days must be between 1 and 36500")
	}
	return nil
}

// ValidateDirectory checks existing ancestors as well, so a not-yet-created log
// directory reached through a symlink cannot silently be placed under content.
func ValidateDirectory(directory, contentRoot string) error {
	resolve := func(p string) (string, error) {
		p, err := filepath.Abs(p)
		if err != nil {
			return "", err
		}
		var tail []string
		for {
			actual, e := filepath.EvalSymlinks(p)
			if e == nil {
				for i := len(tail) - 1; i >= 0; i-- {
					actual = filepath.Join(actual, tail[i])
				}
				return actual, nil
			}
			if !os.IsNotExist(e) {
				return "", e
			}
			parent := filepath.Dir(p)
			if parent == p {
				return "", e
			}
			tail = append(tail, filepath.Base(p))
			p = parent
		}
	}
	dir, err := resolve(directory)
	if err != nil {
		return err
	}
	root, err := resolve(contentRoot)
	if err != nil {
		return err
	}
	rel, err := filepath.Rel(root, dir)
	if err != nil {
		return err
	}
	if filepath.IsLocal(rel) {
		return errors.New("logging.directory must be outside the content root")
	}
	return nil
}
