// Package config persists how to reach the reviewer's model.
//
// It is the only configuration msr keeps. Everything else about a review is
// derived from git, which is why there is no general settings file: a value that
// can be recomputed should not be stored.
package config

import (
	"encoding/json"
	"errors"
	"fmt"
	"io/fs"
	"os"
	"path/filepath"

	"github.com/mondial7/mondspace-reviewer/internal/domain"
)

// DefaultPath is where the configuration lives when none is named.
func DefaultPath() string {
	dir, err := os.UserConfigDir()
	if err != nil {
		// No config directory is unusual but not fatal: fall back to the working
		// directory rather than refusing to run.
		return filepath.Join(".mondspace-reviewer", "config.json")
	}
	return filepath.Join(dir, "mondspace-reviewer", "config.json")
}

// Load reads the configuration. A file that has never been written is not an
// error — running unconfigured is the normal case and the defaults are good.
//
// A file that exists but cannot be read *is* an error: silently falling back to
// a default endpoint while the file says otherwise leaves nothing to explain why
// the model is not the one that was asked for.
func Load(path string) (domain.AgentConfig, error) {
	body, err := os.ReadFile(path)
	if errors.Is(err, fs.ErrNotExist) {
		return domain.AgentConfig{}, nil
	}
	if err != nil {
		return domain.AgentConfig{}, err
	}

	var c domain.AgentConfig
	if err := json.Unmarshal(body, &c); err != nil {
		return domain.AgentConfig{}, fmt.Errorf("reading %s: %w", path, err)
	}
	return c, nil
}

// Save writes the configuration, creating its directory if needed. It writes to
// a temporary file and renames, so an interrupted write leaves the previous
// configuration intact rather than a truncated one.
//
// The file may name an endpoint on a private network, so it is not readable by
// anyone else.
func Save(path string, c domain.AgentConfig) error {
	dir := filepath.Dir(path)
	if err := os.MkdirAll(dir, 0o700); err != nil {
		return err
	}
	body, err := json.MarshalIndent(c, "", "  ")
	if err != nil {
		return err
	}

	tmp, err := os.CreateTemp(dir, ".config-*.json")
	if err != nil {
		return err
	}
	defer os.Remove(tmp.Name())

	if err := tmp.Chmod(0o600); err != nil {
		tmp.Close()
		return err
	}
	if _, err := tmp.Write(append(body, '\n')); err != nil {
		tmp.Close()
		return err
	}
	if err := tmp.Close(); err != nil {
		return err
	}
	return os.Rename(tmp.Name(), path)
}
