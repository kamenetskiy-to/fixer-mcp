// Package vpnenv reads the operator's VPN marker file.
//
// The canonical marker is ${XDG_CONFIG_HOME:-$HOME/.config}/fleet/vpn.env (see
// client_wires/launch_env.py and the provider-launch-env-wsl-vpn-lane canon).
// When it exists the operator tunnel is considered up, and its shell
// assignments describe the live egress: proxy URLs, NO_PROXY and similar. The
// control plane sources those values into every child process so a CLI that
// only has a proxied route keeps working instead of failing with a geo error.
package vpnenv

import (
	"errors"
	"fmt"
	"io/fs"
	"os"
	"path/filepath"
	"sort"
	"strings"
)

// MarkerFileEnvName overrides the marker file path (test/automation hook).
const MarkerFileEnvName = "FLEET_VPN_ENV_FILE"

// MarkerRelativePath is the marker path below the XDG config root.
var MarkerRelativePath = []string{"fleet", "vpn.env"}

// Status is the resolved marker state for the current host.
type Status struct {
	// Path is the marker file path that was inspected.
	Path string
	// Active is true when the marker file exists.
	Active bool
	// Values are the shell assignments parsed from the marker file.
	Values map[string]string
	// Err carries a read/parse failure other than "file does not exist".
	Err error
}

// Summary is a one-line description safe to render in the UI.
func (s Status) Summary() string {
	if s.Err != nil {
		return fmt.Sprintf("vpn.env error: %v", s.Err)
	}
	if !s.Active {
		return "vpn.env: absent"
	}
	if len(s.Values) == 0 {
		return fmt.Sprintf("vpn.env: active (%s, no values)", s.Path)
	}
	return fmt.Sprintf("vpn.env: active (%s, %d value(s))", s.Path, len(s.Values))
}

// MarkerPath mirrors client_wires/launch_env.py:vpn_env_path.
func MarkerPath(env map[string]string) string {
	if override := strings.TrimSpace(env[MarkerFileEnvName]); override != "" {
		return expandHome(override, env)
	}
	if xdg := strings.TrimSpace(env["XDG_CONFIG_HOME"]); xdg != "" {
		return filepath.Join(append([]string{expandHome(xdg, env)}, MarkerRelativePath...)...)
	}
	if home := strings.TrimSpace(env["HOME"]); home != "" {
		return filepath.Join(append([]string{home, ".config"}, MarkerRelativePath...)...)
	}
	if home, err := os.UserHomeDir(); err == nil {
		return filepath.Join(append([]string{home, ".config"}, MarkerRelativePath...)...)
	}
	return ""
}

// Inspect reads the marker file state. A missing file is not an error: the
// tunnel is simply reported as down.
func Inspect(env map[string]string) Status {
	path := MarkerPath(env)
	st := Status{Path: path, Values: map[string]string{}}
	if path == "" {
		return st
	}
	values, err := Load(path)
	if err != nil {
		if errors.Is(err, fs.ErrNotExist) {
			return st
		}
		st.Err = err
		return st
	}
	st.Active = true
	st.Values = values
	return st
}

// Load parses the marker file into shell assignments. A missing file returns
// the wrapped fs.ErrNotExist so callers can distinguish "down" from "broken".
func Load(path string) (map[string]string, error) {
	if path == "" {
		return nil, fs.ErrNotExist
	}
	data, err := os.ReadFile(path)
	if err != nil {
		return nil, err
	}
	return Parse(string(data)), nil
}

// Parse reads shell-style KEY=VALUE lines. Comments, blank lines and an
// optional `export ` prefix are accepted; quoted values are unquoted.
func Parse(text string) map[string]string {
	values := map[string]string{}
	for _, raw := range strings.Split(text, "\n") {
		line := strings.TrimSpace(strings.TrimSuffix(raw, "\r"))
		if line == "" || strings.HasPrefix(line, "#") {
			continue
		}
		if strings.HasPrefix(line, "export ") {
			line = strings.TrimSpace(line[len("export "):])
		}
		eq := strings.IndexByte(line, '=')
		if eq <= 0 {
			continue
		}
		key := strings.TrimSpace(line[:eq])
		if !validEnvName(key) {
			continue
		}
		values[key] = unquote(stripTrailingComment(strings.TrimSpace(line[eq+1:])))
	}
	return values
}

// Env merges the marker values into base, letting the marker win. It returns
// base unchanged when there is nothing to source.
func Env(base []string, values map[string]string) []string {
	if len(values) == 0 {
		return base
	}
	out := make([]string, 0, len(base)+len(values))
	for _, kv := range base {
		key := kv
		if i := strings.IndexByte(kv, '='); i >= 0 {
			key = kv[:i]
		}
		if _, overridden := values[key]; overridden {
			continue
		}
		out = append(out, kv)
	}
	keys := make([]string, 0, len(values))
	for k := range values {
		keys = append(keys, k)
	}
	sort.Strings(keys)
	for _, key := range keys {
		out = append(out, key+"="+values[key])
	}
	return out
}

// EnvMap converts a KEY=VALUE slice into a map, last value winning.
func EnvMap(env []string) map[string]string {
	out := make(map[string]string, len(env))
	for _, kv := range env {
		if i := strings.IndexByte(kv, '='); i > 0 {
			out[kv[:i]] = kv[i+1:]
		}
	}
	return out
}

func validEnvName(name string) bool {
	if name == "" {
		return false
	}
	for i, r := range name {
		switch {
		case r == '_':
		case r >= 'A' && r <= 'Z':
		case r >= 'a' && r <= 'z':
		case r >= '0' && r <= '9' && i > 0:
		default:
			return false
		}
	}
	return true
}

func unquote(value string) string {
	if len(value) < 2 {
		return value
	}
	first := value[0]
	last := value[len(value)-1]
	if (first == '"' && last == '"') || (first == '\'' && last == '\'') {
		return value[1 : len(value)-1]
	}
	return value
}

func stripTrailingComment(value string) string {
	if i := strings.Index(value, " #"); i >= 0 {
		return strings.TrimSpace(value[:i])
	}
	return value
}

func expandHome(p string, env map[string]string) string {
	home := env["HOME"]
	if home == "" {
		home, _ = os.UserHomeDir()
	}
	if p == "~" {
		return home
	}
	if strings.HasPrefix(p, "~/") && home != "" {
		return filepath.Join(home, p[2:])
	}
	return p
}
