package resources

import (
	"fmt"
	"os"
	"path/filepath"
	"strings"
)

// clientCredentialPaths resolves the active auth file and the active-profile
// marker for a client that ai-switch manages. Marker values must match the
// profile file stems exactly: `ai-switch` writes them verbatim, and an
// upper-cased guess would make the console and the switcher disagree about
// which account is active.
func clientCredentialPaths(client, account string) (authFile, markerPath, markerValue string, err error) {
	home, err := os.UserHomeDir()
	if err != nil {
		return "", "", "", err
	}
	dataHome := os.Getenv("XDG_DATA_HOME")
	if dataHome == "" {
		dataHome = filepath.Join(home, ".local", "share")
	}
	stateHome := os.Getenv("XDG_STATE_HOME")
	if stateHome == "" {
		stateHome = filepath.Join(home, ".local", "state")
	}
	switch client {
	case "Codex":
		return filepath.Join(home, ".codex", "auth.json"), "", "", nil
	case "OpenCode Go":
		return filepath.Join(dataHome, "opencode", "auth.json"),
			filepath.Join(stateHome, "opencode", "active-profile"), strings.ToUpper(account), nil
	case "CommandCode":
		return filepath.Join(home, ".commandcode", "auth.json"),
			filepath.Join(stateHome, "commandcode", "active-profile"), account, nil
	case "Kimi Code":
		return "", "", "", fmt.Errorf("переключение Kimi Code пока выполняется нативным клиентом")
	default:
		return "", "", "", fmt.Errorf("неизвестный клиент %q", client)
	}
}

// SwitchAccount changes only the selected client's active auth file. It never
// touches the other client's credentials and refuses an absent profile.
func SwitchAccount(account Account) error {
	if !account.Present {
		return fmt.Errorf("профиль %s отсутствует", account.Name)
	}
	destination, markerPath, markerValue, err := clientCredentialPaths(account.Client, account.Name)
	if err != nil {
		return err
	}
	if err := copyPrivate(account.Path, destination); err != nil {
		return err
	}
	if markerPath == "" {
		return nil
	}
	if err := os.MkdirAll(filepath.Dir(markerPath), 0o700); err != nil {
		return err
	}
	return os.WriteFile(markerPath, []byte(markerValue+"\n"), 0o600)
}

// BindAccount snapshots the currently active auth file into a named profile.
func BindAccount(account Account) error {
	source, _, _, err := clientCredentialPaths(account.Client, account.Name)
	if err != nil {
		return err
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
