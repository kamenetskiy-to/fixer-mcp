package resources

import (
	"os"
	"path/filepath"
	"strings"
	"testing"
)

func TestSwitchAndBindAccountUsePrivateAtomicFiles(t *testing.T) {
	home := t.TempDir()
	t.Setenv("HOME", home)
	profile := filepath.Join(home, ".codex", "auth_backups", "PLUS_PERSONAL.json")
	active := filepath.Join(home, ".codex", "auth.json")
	if err := os.MkdirAll(filepath.Dir(profile), 0o700); err != nil {
		t.Fatal(err)
	}
	if err := os.WriteFile(profile, []byte(`{"token":"profile"}`), 0o600); err != nil {
		t.Fatal(err)
	}
	account := Account{Client: "Codex", Name: "Personal", Path: profile, Present: true}
	if err := SwitchAccount(account); err != nil {
		t.Fatal(err)
	}
	data, err := os.ReadFile(active)
	if err != nil {
		t.Fatal(err)
	}
	if string(data) != `{"token":"profile"}` {
		t.Fatalf("active credentials = %q", data)
	}
	info, statErr := os.Stat(active)
	if statErr != nil {
		t.Fatal(statErr)
	}
	if info.Mode().Perm() != 0o600 {
		t.Fatalf("active mode = %v", info.Mode().Perm())
	}

	if err := os.WriteFile(active, []byte(`{"token":"new"}`), 0o600); err != nil {
		t.Fatal(err)
	}
	if err := BindAccount(account); err != nil {
		t.Fatal(err)
	}
	bound, err := os.ReadFile(profile)
	if err != nil {
		t.Fatal(err)
	}
	if string(bound) != `{"token":"new"}` {
		t.Fatalf("bound credentials = %q", bound)
	}
}

func TestCommandCodeAccountsAreDataDrivenAndMarkerIsVerbatim(t *testing.T) {
	home := t.TempDir()
	t.Setenv("HOME", home)
	t.Setenv("XDG_STATE_HOME", filepath.Join(home, ".state"))
	profileDir := filepath.Join(home, ".commandcode", "auth_backups")
	if err := os.MkdirAll(profileDir, 0o700); err != nil {
		t.Fatal(err)
	}
	accountKey := "ops@example.com"
	profile := filepath.Join(profileDir, accountKey+".json")
	if err := os.WriteFile(profile, []byte(`{"apiKey":"secret"}`), 0o600); err != nil {
		t.Fatal(err)
	}

	accounts := inspectAccounts()
	var found *Account
	for i := range accounts {
		if accounts[i].Client == "CommandCode" && accounts[i].Name == accountKey {
			found = &accounts[i]
		}
	}
	if found == nil {
		t.Fatalf("CommandCode profile was not discovered: %#v", accounts)
	}
	if !found.Present {
		t.Fatalf("CommandCode profile must be present: %#v", found)
	}
	if found.Active {
		t.Fatalf("CommandCode profile must not be active before a switch: %#v", found)
	}

	if err := SwitchAccount(*found); err != nil {
		t.Fatal(err)
	}
	marker, err := os.ReadFile(filepath.Join(home, ".state", "commandcode", "active-profile"))
	if err != nil {
		t.Fatal(err)
	}
	// ai-switch writes the profile file stem verbatim; upper-casing it would
	// make the console and the switcher disagree about the active account.
	if strings.TrimSpace(string(marker)) != accountKey {
		t.Fatalf("marker = %q, want %q", marker, accountKey)
	}
	var switched *Account
	for i, account := range inspectAccounts() {
		if account.Client == "CommandCode" && account.Name == accountKey {
			switched = &inspectAccounts()[i]
		}
	}
	if switched == nil || !switched.Active {
		t.Fatalf("switched CommandCode account must read back as active: %#v", switched)
	}
}

func TestSwitchAccountRefusesMissingProfile(t *testing.T) {
	t.Setenv("HOME", t.TempDir())
	err := SwitchAccount(Account{Client: "Codex", Name: "Missing", Path: filepath.Join(t.TempDir(), "no.json")})
	if err == nil {
		t.Fatal("expected missing profile error")
	}
}
