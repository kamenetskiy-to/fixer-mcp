package resources

import (
	"os"
	"path/filepath"
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

func TestSwitchAccountRefusesMissingProfile(t *testing.T) {
	t.Setenv("HOME", t.TempDir())
	err := SwitchAccount(Account{Client: "Codex", Name: "Missing", Path: filepath.Join(t.TempDir(), "no.json")})
	if err == nil {
		t.Fatal("expected missing profile error")
	}
}
