package resources

import (
	"fmt"
	"os"
	"path/filepath"
	"strings"
)

// SwitchAccount changes only the selected client's active auth file. It never
// touches the other client's credentials and refuses an absent profile.
func SwitchAccount(account Account) error {
	if !account.Present {
		return fmt.Errorf("профиль %s отсутствует", account.Name)
	}
	home, err := os.UserHomeDir()
	if err != nil {
		return err
	}
	var destination string
	switch account.Client {
	case "Codex":
		destination = filepath.Join(home, ".codex", "auth.json")
	case "OpenCode Go":
		dataHome := os.Getenv("XDG_DATA_HOME")
		if dataHome == "" {
			dataHome = filepath.Join(home, ".local", "share")
		}
		destination = filepath.Join(dataHome, "opencode", "auth.json")
	case "Kimi Code":
		return fmt.Errorf("переключение Kimi Code пока выполняется нативным клиентом")
	default:
		return fmt.Errorf("неизвестный клиент %q", account.Client)
	}
	if err := copyPrivate(account.Path, destination); err != nil {
		return err
	}
	if account.Client == "OpenCode Go" {
		stateHome := os.Getenv("XDG_STATE_HOME")
		if stateHome == "" {
			stateHome = filepath.Join(home, ".local", "state")
		}
		marker := filepath.Join(stateHome, "opencode", "active-profile")
		if err := os.MkdirAll(filepath.Dir(marker), 0o700); err != nil {
			return err
		}
		if err := os.WriteFile(marker, []byte(strings.ToUpper(account.Name)+"\n"), 0o600); err != nil {
			return err
		}
	}
	return nil
}

// BindAccount snapshots the currently active auth file into a named profile.
func BindAccount(account Account) error {
	home, err := os.UserHomeDir()
	if err != nil {
		return err
	}
	var source string
	switch account.Client {
	case "Codex":
		source = filepath.Join(home, ".codex", "auth.json")
	case "OpenCode Go":
		dataHome := os.Getenv("XDG_DATA_HOME")
		if dataHome == "" {
			dataHome = filepath.Join(home, ".local", "share")
		}
		source = filepath.Join(dataHome, "opencode", "auth.json")
	default:
		return fmt.Errorf("привязка клиента %q пока не поддержана", account.Client)
	}
	if _, err := os.Stat(source); err != nil {
		return fmt.Errorf("активный auth-файл отсутствует: %s", source)
	}
	return copyPrivate(source, account.Path)
}

func copyPrivate(source, destination string) error {
	data, err := os.ReadFile(source)
	if err != nil {
		return fmt.Errorf("прочитать credentials: %w", err)
	}
	if len(data) == 0 {
		return fmt.Errorf("credentials пусты: %s", source)
	}
	if err := os.MkdirAll(filepath.Dir(destination), 0o700); err != nil {
		return err
	}
	tmp, err := os.CreateTemp(filepath.Dir(destination), ".auth.*.tmp")
	if err != nil {
		return err
	}
	name := tmp.Name()
	defer os.Remove(name)
	if err := tmp.Chmod(0o600); err != nil {
		_ = tmp.Close()
		return err
	}
	if _, err := tmp.Write(data); err != nil {
		_ = tmp.Close()
		return err
	}
	if err := tmp.Close(); err != nil {
		return err
	}
	if err := os.Rename(name, destination); err != nil {
		return err
	}
	return nil
}
