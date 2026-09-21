package machines

import (
	"os"
	"path/filepath"
	"testing"
)

func TestDiscoverReadsXDGMachineTargetsAndKeepsLocalFirst(t *testing.T) {
	xdg := t.TempDir()
	t.Setenv("XDG_CONFIG_HOME", xdg)
	configDir := filepath.Join(xdg, "fixer")
	if err := os.MkdirAll(configDir, 0o700); err != nil {
		t.Fatal(err)
	}
	config := `[{"id":"wsl","title":"WSL","target":"wsl.example","kind":"ssh","path":"/home/operator/project"}]`
	if err := os.WriteFile(filepath.Join(configDir, "machines.json"), []byte(config), 0o600); err != nil {
		t.Fatal(err)
	}
	machines := Discover()
	if len(machines) < 2 || machines[0].ID != "local" || machines[1].ID != "wsl" {
		t.Fatalf("machines = %+v", machines)
	}
	if machines[1].Path != "/home/operator/project" {
		t.Fatalf("remote path = %q", machines[1].Path)
	}
}
