package resources

import (
	"bufio"
	"context"
	"crypto/sha256"
	"encoding/json"
	"fmt"
	"os"
	"os/exec"
	"path/filepath"
	"regexp"
	"runtime"
	"strconv"
	"strings"
	"time"
)

type Provider struct {
	Name      string `json:"name"`
	Client    string `json:"client"`
	Available bool   `json:"available"`
	Binary    string `json:"binary,omitempty"`
	Detail    string `json:"detail,omitempty"`
}

type Account struct {
	Client  string `json:"client"`
	Name    string `json:"name"`
	Path    string `json:"path"`
	Present bool   `json:"present"`
	Active  bool   `json:"active"`
}

type Quota struct {
	Provider string `json:"provider"`
	Status   string `json:"status"`
	Windows  string `json:"windows,omitempty"`
	// Structured windows let the console render a real limits board instead of
	// pasting the legacy fixed-width table into the Super-TUI.
	Window5h QuotaWindow `json:"w5h,omitempty"`
	Window7d QuotaWindow `json:"w7d,omitempty"`
	Window1m QuotaWindow `json:"w1m,omitempty"`
	Source   string      `json:"source,omitempty"`
}

// QuotaWindow is one provider usage window (5h / 7d / 1m) with the numeric
// remainder separated out so the UI can draw a bar and sort by urgency.
type QuotaWindow struct {
	Text    string   `json:"text,omitempty"`
	Percent *float64 `json:"percent,omitempty"`
	Reset   string   `json:"reset,omitempty"`
}

type Snapshot struct {
	Providers []Provider `json:"providers"`
	Accounts  []Account  `json:"accounts"`
	Quotas    []Quota    `json:"quotas"`
	// CML is the operator-facing source of truth for quotas. Keep its exact
	// report so the console does not replace a useful report with a lossy card.
	CMLCommand   string    `json:"cml_command,omitempty"`
	CMLOutput    string    `json:"cml_output,omitempty"`
	CMLCheckedAt time.Time `json:"cml_checked_at,omitempty"`
	CheckedAt    time.Time `json:"checked_at"`
	Error        string    `json:"error,omitempty"`
}

var providerBinaries = []struct {
	name   string
	client string
	binary string
}{
	{"Pi", "agent", "pi"},
	{"Antigravity", "agent", "agy"},
	{"CommandCode", "agent", "cmd"},
	{"OpenCode", "agent", "opencode"},
	{"Droid", "agent", "droid"},
	{"Grok", "agent", "grok"},
	{"Kimi Code", "agent", "kimi"},
}

func Inspect() Snapshot {
	s := Snapshot{CheckedAt: time.Now()}
	if cached, ok := loadCMLCache(); ok {
		s.CMLCommand = cached.Command
		s.CMLOutput = cached.Output
		s.CMLCheckedAt = cached.CheckedAt
		s.Quotas = parseQuota(cached.Output)
		for i := range s.Quotas {
			s.Quotas[i].Source = cached.Command
		}
	}
	for _, item := range providerBinaries {
		path, err := exec.LookPath(item.binary)
		provider := Provider{Name: item.name, Client: item.client, Available: err == nil, Binary: path}
		if err != nil {
			provider.Detail = "не установлен"
		} else {
			provider.Detail = "готов"
		}
		s.Providers = append(s.Providers, provider)
	}
	s.Accounts = inspectAccounts()
	return s
}

func RefreshQuota(ctx context.Context) Snapshot {
	s := Inspect()
	if ctx == nil {
		ctx = context.Background()
	}
	path, name, err := cmlBinary()
	if err != nil {
		s.Error = "cml не установлен; лимиты недоступны"
		return s
	}
	cachedOutput, cachedCommand := s.CMLOutput, s.CMLCommand
	s.CMLCommand = name
	cmd := exec.CommandContext(ctx, path)
	output, err := cmd.CombinedOutput()
	freshOutput := strings.TrimSpace(string(output))
	if err != nil {
		if cachedOutput != "" {
			s.CMLOutput, s.CMLCommand = cachedOutput, cachedCommand
		}
		if freshOutput != "" {
			s.Error = fmt.Sprintf("cml: %v · %s", err, freshOutput)
		} else {
			s.Error = fmt.Sprintf("cml: %v", err)
		}
		return s
	}
	if freshOutput == "" {
		s.Error = "cml вернул пустой отчёт"
		return s
	}
	s.CMLOutput = freshOutput
	s.CMLCheckedAt = time.Now()
	s.Quotas = parseQuota(s.CMLOutput)
	for i := range s.Quotas {
		s.Quotas[i].Source = name
	}
	_ = saveCMLCache(cmlCache{Command: s.CMLCommand, Output: s.CMLOutput, CheckedAt: s.CMLCheckedAt})
	return s
}

type cmlCache struct {
	Command   string    `json:"command"`
	Output    string    `json:"output"`
	CheckedAt time.Time `json:"checked_at"`
}

func loadCMLCache() (cmlCache, bool) {
	path := cmlCachePath()
	if path == "" {
		return cmlCache{}, false
	}
	data, err := os.ReadFile(path)
	if err != nil || len(data) == 0 || len(data) > 128*1024 {
		return cmlCache{}, false
	}
	var cached cmlCache
	if err := json.Unmarshal(data, &cached); err != nil || strings.TrimSpace(cached.Output) == "" {
		return cmlCache{}, false
	}
	if strings.TrimSpace(cached.Command) == "" {
		cached.Command = "cml"
	}
	return cached, true
}

func saveCMLCache(cached cmlCache) error {
	path := cmlCachePath()
	if path == "" {
		return nil
	}
	if err := os.MkdirAll(filepath.Dir(path), 0o700); err != nil {
		return err
	}
	payload, err := json.Marshal(cached)
	if err != nil {
		return err
	}
	tmp, err := os.CreateTemp(filepath.Dir(path), ".cml.*.tmp")
	if err != nil {
		return err
	}
	tmpName := tmp.Name()
	defer os.Remove(tmpName)
	if err := tmp.Chmod(0o600); err != nil {
		_ = tmp.Close()
		return err
	}
	if _, err := tmp.Write(append(payload, '\n')); err != nil {
		_ = tmp.Close()
		return err
	}
	if err := tmp.Close(); err != nil {
		return err
	}
	return os.Rename(tmpName, path)
}

func cmlCachePath() string {
	root := strings.TrimSpace(os.Getenv("XDG_CACHE_HOME"))
	if root == "" {
		if home, err := os.UserHomeDir(); err == nil {
			root = filepath.Join(home, ".cache")
		}
	}
	if root == "" {
		return ""
	}
	return filepath.Join(root, "fixer", "cml.json")
}

func cmlBinary() (path, name string, err error) {
	for _, candidate := range []string{"cml", "check-my-limits"} {
		if resolved, lookupErr := exec.LookPath(candidate); lookupErr == nil {
			return resolved, candidate, nil
		}
		// `fixer` must work from non-interactive SSH/tmux as well as a login
		// shell. Consult the managed user-bin location directly instead of
		// relying on a shell rc file to add it to PATH.
		for _, dir := range managedBinDirs() {
			resolved := filepath.Join(dir, candidate)
			if info, statErr := os.Stat(resolved); statErr == nil && !info.IsDir() && info.Mode()&0o111 != 0 {
				return resolved, candidate, nil
			}
		}
	}
	return "", "", fmt.Errorf("cml not found")
}

func managedBinDirs() []string {
	candidates := []string{strings.TrimSpace(os.Getenv("FIXER_USER_BIN"))}
	if home, err := os.UserHomeDir(); err == nil && strings.TrimSpace(home) != "" {
		candidates = append(candidates, filepath.Join(home, ".local", "bin"))
	}
	result := make([]string, 0, len(candidates))
	seen := make(map[string]bool, len(candidates))
	for _, candidate := range candidates {
		candidate = strings.TrimSpace(candidate)
		if candidate == "" {
			continue
		}
		candidate = filepath.Clean(candidate)
		if !seen[candidate] {
			seen[candidate] = true
			result = append(result, candidate)
		}
	}
	return result
}

func JSON(s Snapshot) ([]byte, error) {
	return json.MarshalIndent(s, "", "  ")
}

func inspectAccounts() []Account {
	home, err := os.UserHomeDir()
	if err != nil {
		return nil
	}
	dataHome := os.Getenv("XDG_DATA_HOME")
	if dataHome == "" {
		dataHome = filepath.Join(home, ".local", "share")
	}
	stateHome := os.Getenv("XDG_STATE_HOME")
	if stateHome == "" {
		stateHome = filepath.Join(home, ".local", "state")
	}
	entries := []struct {
		client     string
		name       string
		path       string
		markerPath string
		marker     string
	}{
		{"Codex", "Personal", filepath.Join(home, ".codex", "auth_backups", "PLUS_PERSONAL.json"), "", ""},
		{"Codex", "Babushka", filepath.Join(home, ".codex", "auth_backups", "PLUS_BABUSHKA.json"), "", ""},
		{"OpenCode Go", "Personal", filepath.Join(dataHome, "opencode", "auth_backups", "Personal.json"), filepath.Join(stateHome, "opencode", "active-profile"), "PERSONAL"},
		{"OpenCode Go", "Stas", filepath.Join(dataHome, "opencode", "auth_backups", "Stas.json"), filepath.Join(stateHome, "opencode", "active-profile"), "STAS"},
	}
	// CommandCode accounts are data-driven: every profile `ai-switch` keeps in
	// the backup directory is one account, and its file stem is the account key
	// (this keeps operator account identities out of hard-coded sources).
	commandcodeDir := filepath.Join(home, ".commandcode", "auth_backups")
	if profiles, globErr := filepath.Glob(filepath.Join(commandcodeDir, "*.json")); globErr == nil {
		for _, profile := range profiles {
			name := strings.TrimSuffix(filepath.Base(profile), ".json")
			if name == "" {
				continue
			}
			entries = append(entries, struct {
				client     string
				name       string
				path       string
				markerPath string
				marker     string
			}{"CommandCode", name, profile, filepath.Join(stateHome, "commandcode", "active-profile"), name})
		}
	}
	activeCodex, _ := os.ReadFile(filepath.Join(home, ".codex", "auth.json"))
	activeCodexHash := sha256.Sum256(activeCodex)
	accounts := make([]Account, 0, len(entries))
	for _, item := range entries {
		data, readErr := os.ReadFile(item.path)
		present := readErr == nil && len(data) > 0
		active := false
		if item.markerPath != "" {
			if markerData, markerErr := os.ReadFile(item.markerPath); markerErr == nil {
				active = strings.TrimSpace(string(markerData)) == item.marker
			}
		}
		if item.client == "Codex" && present && len(activeCodex) > 0 {
			active = sha256.Sum256(data) == activeCodexHash
		}
		accounts = append(accounts, Account{
			Client: item.client, Name: item.name, Path: item.path,
			Present: present, Active: active,
		})
	}
	return accounts
}

var (
	percentPattern = regexp.MustCompile(`^(\d+(?:\.\d+)?)% left`)
	resetPattern   = regexp.MustCompile(`reset in\s+(.+)$`)
)

// parseWindow turns one cml cell into a structured window. Cells that are not
// percentages (`—`, `unknown error`, `~13 left, idle`) keep their text so the
// UI can show them verbatim instead of inventing a number.
func parseWindow(text string) QuotaWindow {
	window := QuotaWindow{Text: text}
	if match := percentPattern.FindStringSubmatch(text); match != nil {
		if value, err := strconv.ParseFloat(match[1], 64); err == nil {
			window.Percent = &value
		}
	}
	if match := resetPattern.FindStringSubmatch(text); match != nil {
		window.Reset = strings.TrimSpace(match[1])
	}
	return window
}

func parseQuota(output string) []Quota {
	var result []Quota
	scanner := bufio.NewScanner(strings.NewReader(output))
	for scanner.Scan() {
		line := strings.TrimSpace(scanner.Text())
		if !strings.Contains(line, "│") || strings.Contains(line, "Провайдер") || strings.HasPrefix(line, "-") {
			continue
		}
		parts := strings.Split(line, "│")
		if len(parts) < 2 {
			continue
		}
		provider := strings.TrimSpace(parts[0])
		status := strings.TrimSpace(parts[1])
		if provider == "" || status == "" {
			continue
		}
		windows := make([]string, 0, len(parts)-2)
		for _, part := range parts[2:] {
			value := strings.TrimSpace(part)
			if value != "" {
				windows = append(windows, value)
			}
		}
		quota := Quota{Provider: provider, Status: status, Windows: strings.Join(windows, " · ")}
		cells := make([]string, 3)
		for i := 0; i < 3 && i+2 < len(parts); i++ {
			cells[i] = strings.TrimSpace(parts[i+2])
		}
		quota.Window5h = parseWindow(cells[0])
		quota.Window7d = parseWindow(cells[1])
		quota.Window1m = parseWindow(cells[2])
		result = append(result, quota)
	}
	return result
}

func PlatformLabel() string { return runtime.GOOS + "/" + runtime.GOARCH }
