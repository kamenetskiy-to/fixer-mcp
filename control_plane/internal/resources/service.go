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
	"runtime"
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
	Source   string `json:"source,omitempty"`
}

type Snapshot struct {
	Providers []Provider `json:"providers"`
	Accounts  []Account  `json:"accounts"`
	Quotas    []Quota    `json:"quotas"`
	CheckedAt time.Time  `json:"checked_at"`
	Error     string     `json:"error,omitempty"`
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
	path, err := exec.LookPath("check-my-limits")
	if err != nil {
		s.Error = "check-my-limits не установлен; доступность клиентов показана без квот"
		return s
	}
	cmd := exec.CommandContext(ctx, path)
	output, err := cmd.Output()
	if err != nil {
		s.Error = fmt.Sprintf("quota refresh: %v", err)
		return s
	}
	s.Quotas = parseQuota(string(output))
	if len(s.Quotas) == 0 {
		s.Error = "quota refresh вернул пустой отчёт"
	}
	for i := range s.Quotas {
		s.Quotas[i].Source = "check-my-limits"
	}
	return s
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
		client string
		name   string
		path   string
		marker string
	}{
		{"Codex", "Personal", filepath.Join(home, ".codex", "auth_backups", "PLUS_PERSONAL.json"), ""},
		{"Codex", "Babushka", filepath.Join(home, ".codex", "auth_backups", "PLUS_BABUSHKA.json"), ""},
		{"OpenCode Go", "Personal", filepath.Join(dataHome, "opencode", "auth_backups", "Personal.json"), "PERSONAL"},
		{"OpenCode Go", "Stas", filepath.Join(dataHome, "opencode", "auth_backups", "Stas.json"), "STAS"},
	}
	activeMarker := ""
	if data, err := os.ReadFile(filepath.Join(stateHome, "opencode", "active-profile")); err == nil {
		activeMarker = strings.TrimSpace(string(data))
	}
	activeCodex, _ := os.ReadFile(filepath.Join(home, ".codex", "auth.json"))
	activeCodexHash := sha256.Sum256(activeCodex)
	accounts := make([]Account, 0, len(entries))
	for _, item := range entries {
		data, readErr := os.ReadFile(item.path)
		present := readErr == nil && len(data) > 0
		active := item.marker != "" && item.marker == strings.ToUpper(activeMarker)
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
		result = append(result, Quota{Provider: provider, Status: status, Windows: strings.Join(windows, " · ")})
	}
	return result
}

func PlatformLabel() string { return runtime.GOOS + "/" + runtime.GOARCH }
