// Package config loads the operator-owned configuration for fixerctl.
//
// The config file is intentionally optional: a missing
// ~/.config/fixer/control-plane.json is not an error. Every value in it only
// overrides something that already has a sane default, and the defaults come
// from PATH plus the built-in entry registry.
package config

import (
	"encoding/json"
	"errors"
	"fmt"
	"io/fs"
	"os"
	"path/filepath"
)

// DefaultFileName is the config file name inside the fixer config directory.
const DefaultFileName = "control-plane.json"

// DirectoryName is the fixer config directory under the XDG config root.
const DirectoryName = "fixer"

// EntryConfig declares one operator action. It is used both for extra entries
// and for overriding a built-in entry when the ID matches.
type EntryConfig struct {
	// ID is the stable identifier used by path_overrides and order.
	ID string `json:"id"`
	// Title is the human label rendered in the menu.
	Title string `json:"title,omitempty"`
	// Help is a short description rendered next to the title.
	Help string `json:"help,omitempty"`
	// Command is the executable name or path resolved through PATH.
	Command string `json:"command,omitempty"`
	// Args are passed to the command after the resolved script (if any).
	Args []string `json:"args,omitempty"`
	// Platforms limits the entry to GOOS values such as "darwin" or "linux".
	// Empty means "every platform".
	Platforms []string `json:"platforms,omitempty"`
	// LaneByPlatform maps GOOS to the transport lane name shown in the UI and
	// used to gate unsupported platforms (e.g. VPN: darwin=launchd,
	// linux=ssh-tunnel).
	LaneByPlatform map[string]string `json:"lane_by_platform,omitempty"`
}

// Config is the on-disk shape of ~/.config/fixer/control-plane.json.
type Config struct {
	// PathOverrides maps an entry ID to an absolute path or a different
	// command name. "~" is expanded against the operator home directory.
	PathOverrides map[string]string `json:"path_overrides,omitempty"`
	// ExtraEntries appends new entries and overrides built-in ones by ID.
	ExtraEntries []EntryConfig `json:"extra_entries,omitempty"`
	// Order lists entry IDs that should be shown first, in that order. IDs
	// that are not listed keep their built-in relative order afterwards.
	Order []string `json:"order,omitempty"`
	// VPNEnvFile overrides the VPN marker file path.
	VPNEnvFile string `json:"vpn_env_file,omitempty"`
	// RepoRoot points at a fixer-mcp checkout for repo-script entries such as
	// "fleet check". When empty the checkout is discovered by walking up from
	// the working directory.
	RepoRoot string `json:"repo_root,omitempty"`
}

// DefaultPath returns the default config path for the given environment map
// (usually derived from os.Environ). XDG_CONFIG_HOME wins over HOME/.config.
func DefaultPath(env map[string]string) string {
	root := ""
	if v := env["XDG_CONFIG_HOME"]; v != "" {
		root = v
	} else if home := env["HOME"]; home != "" {
		root = filepath.Join(home, ".config")
	} else if home, err := os.UserHomeDir(); err == nil {
		root = filepath.Join(home, ".config")
	}
	if root == "" {
		return ""
	}
	return filepath.Join(root, DirectoryName, DefaultFileName)
}

// Load reads a config file.
//
// A missing file is not an error: it returns an empty Config so callers fall
// back to PATH defaults. A malformed file returns an error so the caller can
// surface it instead of silently ignoring the operator's intent.
func Load(path string) (Config, error) {
	if path == "" {
		return Config{}, nil
	}
	data, err := os.ReadFile(path)
	if err != nil {
		if errors.Is(err, fs.ErrNotExist) {
			return Config{}, nil
		}
		return Config{}, fmt.Errorf("read config %s: %w", path, err)
	}
	var cfg Config
	if err := json.Unmarshal(data, &cfg); err != nil {
		return Config{}, fmt.Errorf("parse config %s: %w", path, err)
	}
	return cfg, nil
}
