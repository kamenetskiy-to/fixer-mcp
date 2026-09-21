// Package registry turns declarative operator entries into resolved,
// platform-aware commands.
//
// The default entry list is a fixed set of operator utilities. Each entry is
// resolved at startup through PATH plus optional config overrides, and every
// failure mode becomes an explicit disabled reason instead of a press-time
// crash: that is the exact bug class that made a stale alias break `fixer`,
// `cml` and `codex-switch` on the operator's MacBook Air.
package registry

import (
	"fmt"
	"os"
	"path/filepath"
	"runtime"
	"sort"
	"strings"

	"github.com/fixer-mcp/control-plane/internal/config"
)

// Entry is one declarative operator action.
type Entry struct {
	// ID is the stable identifier used by config path_overrides and order.
	ID string
	// Title is the human label rendered in the menu.
	Title string
	// Help is a short description rendered next to the title.
	Help string
	// Command is the executable name (resolved through PATH) or an explicit
	// path. It may be empty for a repo-script-only entry.
	Command string
	// Args are passed after the resolved repo script path.
	Args []string
	// Platforms limits the entry to GOOS values. Empty means every platform.
	Platforms []string
	// LaneByPlatform maps GOOS to a transport lane name. A non-empty map also
	// gates unsupported platforms.
	LaneByPlatform map[string]string
	// RepoScript is a checkout-relative script that must exist before the
	// entry can run. The resolved absolute path is prepended to Args.
	RepoScript string
	// Override is the per-entry path override from config.
	Override string
}

// Resolved is an Entry plus the outcome of resolving it for the current host.
type Resolved struct {
	Entry
	// Binary is the absolute path of the resolved command.
	Binary string
	// Lane is the transport lane for this platform, when the entry declares
	// one (for example "launchd" on darwin and "ssh-tunnel" on linux).
	Lane string
	// Disabled is the reason the entry cannot run. Empty means enabled.
	Disabled string
}

// Enabled reports whether the entry can be executed.
func (r Resolved) Enabled() bool { return r.Disabled == "" }

// Display returns the label rendered in the menu.
func (r Resolved) Display() string {
	if r.Title != "" {
		return r.Title
	}
	return r.ID
}

// Group is the short right-hand hint shown in the menu (lane or help text).
func (r Resolved) Group() string {
	if r.Lane != "" {
		return r.Lane
	}
	return r.Help
}

// CommandLine renders the resolved invocation for evidence and run summaries.
func (r Resolved) CommandLine() string {
	parts := make([]string, 0, len(r.Args)+1)
	if r.Binary != "" {
		parts = append(parts, r.Binary)
	}
	parts = append(parts, r.Args...)
	return strings.Join(parts, " ")
}

// Options carries the host facts the resolver needs. Every field has a
// production default; tests inject fakes so no test needs a real PATH, a real
// checkout or a real TTY.
type Options struct {
	GOOS     string
	Home     string
	Path     string
	RepoRoot string
	LookPath func(string) (string, error)
	Stat     func(string) (os.FileInfo, error)
}

// BaseEntries returns the built-in operator menu.
func BaseEntries() []Entry {
	return []Entry{
		{
			ID:      "cml",
			Title:   "check-my-limits",
			Help:    "AI provider quota table",
			Command: "cml",
		},
		{
			ID:      "myip",
			Title:   "my IP / VPN exit",
			Help:    "public egress address",
			Command: "myip",
		},
		{
			ID:      "ssh-tui",
			Title:   "ssh-tui",
			Help:    "SSH host picker",
			Command: "ssh-tui",
		},
		{
			ID:      "ai-pro",
			Title:   "ai-pro",
			Help:    "AI provider switcher",
			Command: "ai-pro",
		},
		{
			ID:      "vpn-up",
			Title:   "VPN up",
			Help:    "raise the operator tunnel",
			Command: "vpn-up",
			LaneByPlatform: map[string]string{
				"darwin": "launchd",
				"linux":  "ssh-tunnel",
			},
		},
		{
			ID:      "vpn-down",
			Title:   "VPN down",
			Help:    "drop the operator tunnel",
			Command: "vpn-down",
			LaneByPlatform: map[string]string{
				"darwin": "launchd",
				"linux":  "ssh-tunnel",
			},
		},
		{
			ID:      "codex-switch",
			Title:   "codex-switch",
			Help:    "Codex account switcher",
			Command: "codex-switch",
		},
		{
			ID:      "fixer",
			Title:   "Fixer TUI",
			Help:    "Fixer control channel",
			Command: "fixer",
		},
		{
			ID:         "fleet-check",
			Title:      "fleet check",
			Help:       "operator env doctor",
			Command:    "python3",
			Args:       []string{"check"},
			RepoScript: "scripts/fleet/operator_env.py",
		},
	}
}

// Build resolves the built-in entries merged with the operator config.
func Build(cfg config.Config, opts Options) []Resolved {
	return ResolveEntries(mergeEntries(BaseEntries(), cfg.ExtraEntries), cfg, opts)
}

// ResolveEntries resolves an explicit entry list against the host facts.
// Config path overrides are applied by entry ID.
func ResolveEntries(entries []Entry, cfg config.Config, opts Options) []Resolved {
	opts = opts.withDefaults()
	entries = cloneEntries(entries)
	applyPathOverrides(entries, cfg.PathOverrides)
	out := make([]Resolved, 0, len(entries))
	for _, e := range entries {
		out = append(out, resolveEntry(e, cfg, opts))
	}
	return applyOrder(out, cfg.Order)
}

func cloneEntries(entries []Entry) []Entry {
	out := make([]Entry, len(entries))
	copy(out, entries)
	return out
}

func (o Options) withDefaults() Options {
	if o.GOOS == "" {
		o.GOOS = runtime.GOOS
	}
	if o.Stat == nil {
		o.Stat = os.Stat
	}
	if o.Home == "" {
		if home, err := os.UserHomeDir(); err == nil {
			o.Home = home
		}
	}
	if o.Path == "" {
		o.Path = os.Getenv("PATH")
	}
	if o.LookPath == nil {
		path := o.Path
		o.LookPath = func(name string) (string, error) { return lookPathIn(path, name) }
	}
	return o
}

func resolveEntry(e Entry, cfg config.Config, opts Options) Resolved {
	r := Resolved{Entry: e}

	if len(e.Platforms) > 0 && !containsString(e.Platforms, opts.GOOS) {
		r.Disabled = fmt.Sprintf("unsupported platform: %s", opts.GOOS)
		return r
	}

	if len(e.LaneByPlatform) > 0 {
		lane, ok := e.LaneByPlatform[opts.GOOS]
		if !ok {
			r.Disabled = fmt.Sprintf("unsupported platform: %s (lanes: %s)", opts.GOOS, laneSummary(e.LaneByPlatform))
			return r
		}
		r.Lane = lane
	}

	if e.RepoScript != "" {
		script, err := resolveRepoScript(e.RepoScript, cfg.RepoRoot, opts)
		if err != nil {
			r.Disabled = fmt.Sprintf("missing: %s", e.RepoScript)
			return r
		}
		args := make([]string, 0, len(e.Args)+1)
		args = append(args, script)
		args = append(args, e.Args...)
		r.Args = args
	}

	binary, reason := resolveBinary(e, opts)
	if reason != "" {
		r.Disabled = reason
		return r
	}
	r.Binary = binary
	return r
}

func resolveBinary(e Entry, opts Options) (string, string) {
	name := e.Command
	explicit := false
	if e.Override != "" {
		name = e.Override
		explicit = true
	}
	if name == "" {
		if e.RepoScript == "" {
			return "", "missing: (no command)"
		}
		// A repo-script entry may still need a runner; without one it cannot
		// run, but that is a configuration error rather than a PATH miss.
		return "", "missing: (no runner)"
	}
	if explicit || strings.ContainsRune(name, os.PathSeparator) {
		p := expandHome(name, opts.Home)
		info, err := opts.Stat(p)
		if err != nil {
			return "", fmt.Sprintf("missing: %s", name)
		}
		if info.IsDir() || info.Mode().Perm()&0o111 == 0 {
			return "", fmt.Sprintf("not executable: %s", name)
		}
		return p, ""
	}
	p, err := opts.LookPath(name)
	if err != nil {
		return "", fmt.Sprintf("missing: %s", name)
	}
	return p, ""
}

func resolveRepoScript(rel, cfgRepoRoot string, opts Options) (string, error) {
	rel = filepath.FromSlash(rel)
	var roots []string
	if cfgRepoRoot != "" {
		roots = append(roots, expandHome(cfgRepoRoot, opts.Home))
	}
	if opts.RepoRoot != "" {
		roots = append(roots, opts.RepoRoot)
	}
	for _, root := range roots {
		candidate := filepath.Join(root, rel)
		info, err := opts.Stat(candidate)
		if err == nil && !info.IsDir() {
			return candidate, nil
		}
	}
	return "", fmt.Errorf("repo script %s not found", rel)
}

func mergeEntries(base []Entry, extras []config.EntryConfig) []Entry {
	out := make([]Entry, len(base))
	copy(out, base)
	for _, ex := range extras {
		id := strings.TrimSpace(ex.ID)
		if id == "" {
			continue
		}
		converted := entryFromConfig(ex)
		converted.ID = id
		idx := -1
		for i := range out {
			if out[i].ID == id {
				idx = i
				break
			}
		}
		if idx < 0 {
			out = append(out, converted)
			continue
		}
		out[idx] = mergeEntry(out[idx], converted)
	}
	return out
}

func mergeEntry(base, override Entry) Entry {
	merged := base
	if override.Title != "" {
		merged.Title = override.Title
	}
	if override.Help != "" {
		merged.Help = override.Help
	}
	if override.Command != "" {
		merged.Command = override.Command
	}
	if override.Args != nil {
		merged.Args = override.Args
	}
	if override.Platforms != nil {
		merged.Platforms = override.Platforms
	}
	if override.LaneByPlatform != nil {
		merged.LaneByPlatform = override.LaneByPlatform
	}
	if override.RepoScript != "" {
		merged.RepoScript = override.RepoScript
	}
	return merged
}

func entryFromConfig(ex config.EntryConfig) Entry {
	return Entry{
		ID:             ex.ID,
		Title:          ex.Title,
		Help:           ex.Help,
		Command:        ex.Command,
		Args:           ex.Args,
		Platforms:      ex.Platforms,
		LaneByPlatform: ex.LaneByPlatform,
	}
}

func applyOrder(resolved []Resolved, order []string) []Resolved {
	if len(order) == 0 {
		return resolved
	}
	index := make(map[string]int, len(resolved))
	for i, r := range resolved {
		index[r.ID] = i
	}
	out := make([]Resolved, 0, len(resolved))
	used := make([]bool, len(resolved))
	for _, id := range order {
		if i, ok := index[id]; ok && !used[i] {
			out = append(out, resolved[i])
			used[i] = true
		}
	}
	for i, r := range resolved {
		if !used[i] {
			out = append(out, r)
		}
	}
	return out
}

// applyPathOverrides attaches cfg.PathOverrides to the matching entries by ID.
func applyPathOverrides(entries []Entry, overrides map[string]string) {
	for i := range entries {
		if v, ok := overrides[entries[i].ID]; ok {
			entries[i].Override = v
		}
	}
}

// FindRepoRoot walks up from start until it sees rel, then returns the root.
// An empty start or a missing rel returns "".
func FindRepoRoot(start, rel string, stat func(string) (os.FileInfo, error)) string {
	if start == "" || rel == "" {
		return ""
	}
	if stat == nil {
		stat = os.Stat
	}
	dir := start
	if info, err := stat(dir); err != nil || !info.IsDir() {
		dir = filepath.Dir(dir)
	}
	for {
		candidate := filepath.Join(dir, filepath.FromSlash(rel))
		if info, err := stat(candidate); err == nil && !info.IsDir() {
			return dir
		}
		parent := filepath.Dir(dir)
		if parent == dir {
			return ""
		}
		dir = parent
	}
}

func lookPathIn(path, name string) (string, error) {
	if name == "" {
		return "", fmt.Errorf("empty command name")
	}
	for _, dir := range filepath.SplitList(path) {
		if dir == "" {
			continue
		}
		candidate := filepath.Join(dir, name)
		info, err := os.Stat(candidate)
		if err != nil || info.IsDir() {
			continue
		}
		if info.Mode().Perm()&0o111 == 0 {
			continue
		}
		return candidate, nil
	}
	return "", fmt.Errorf("executable file not found in $PATH: %s", name)
}

func expandHome(p, home string) string {
	if p == "~" && home != "" {
		return home
	}
	if strings.HasPrefix(p, "~/") && home != "" {
		return filepath.Join(home, p[2:])
	}
	return p
}

func laneSummary(lanes map[string]string) string {
	keys := make([]string, 0, len(lanes))
	for k := range lanes {
		keys = append(keys, k)
	}
	sort.Strings(keys)
	parts := make([]string, 0, len(keys))
	for _, k := range keys {
		parts = append(parts, fmt.Sprintf("%s=%s", k, lanes[k]))
	}
	return strings.Join(parts, ", ")
}

func containsString(values []string, want string) bool {
	for _, v := range values {
		if v == want {
			return true
		}
	}
	return false
}
