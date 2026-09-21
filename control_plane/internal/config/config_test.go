package config

import (
	"os"
	"path/filepath"
	"testing"
)

func TestDefaultPath(t *testing.T) {
	cases := []struct {
		name string
		env  map[string]string
		want string
	}{
		{
			name: "xdg wins",
			env:  map[string]string{"XDG_CONFIG_HOME": "/xdg", "HOME": "/home/op"},
			want: "/xdg/fixer/control-plane.json",
		},
		{
			name: "home fallback",
			env:  map[string]string{"HOME": "/home/op"},
			want: "/home/op/.config/fixer/control-plane.json",
		},
		{
			name: "xdg empty string falls back to home",
			env:  map[string]string{"XDG_CONFIG_HOME": "", "HOME": "/home/op"},
			want: "/home/op/.config/fixer/control-plane.json",
		},
	}
	for _, tc := range cases {
		t.Run(tc.name, func(t *testing.T) {
			if got := DefaultPath(tc.env); got != tc.want {
				t.Fatalf("DefaultPath() = %q, want %q", got, tc.want)
			}
		})
	}
}

func TestLoadMissingFileIsNotAnError(t *testing.T) {
	dir := t.TempDir()
	cfg, err := Load(filepath.Join(dir, "does-not-exist.json"))
	if err != nil {
		t.Fatalf("Load() returned error for a missing file: %v", err)
	}
	if cfg.PathOverrides != nil || len(cfg.ExtraEntries) != 0 || len(cfg.Order) != 0 {
		t.Fatalf("expected empty config, got %+v", cfg)
	}
}

func TestLoadEmptyPathIsEmptyConfig(t *testing.T) {
	cfg, err := Load("")
	if err != nil {
		t.Fatalf("Load(\"\") returned error: %v", err)
	}
	if len(cfg.ExtraEntries) != 0 {
		t.Fatalf("expected empty config, got %+v", cfg)
	}
}

func TestLoadValidConfig(t *testing.T) {
	dir := t.TempDir()
	path := filepath.Join(dir, "control-plane.json")
	body := `{
	  "path_overrides": {"cml": "/opt/bin/cml"},
	  "order": ["fixer", "cml"],
	  "vpn_env_file": "/tmp/vpn.env",
	  "repo_root": "/repo",
	  "extra_entries": [
	    {"id": "notes", "title": "Notes", "command": "notes", "args": ["--fast"]}
	  ]
	}`
	if err := os.WriteFile(path, []byte(body), 0o644); err != nil {
		t.Fatal(err)
	}
	cfg, err := Load(path)
	if err != nil {
		t.Fatalf("Load() error: %v", err)
	}
	if got := cfg.PathOverrides["cml"]; got != "/opt/bin/cml" {
		t.Fatalf("PathOverrides[cml] = %q", got)
	}
	if len(cfg.Order) != 2 || cfg.Order[0] != "fixer" {
		t.Fatalf("Order = %v", cfg.Order)
	}
	if cfg.VPNEnvFile != "/tmp/vpn.env" || cfg.RepoRoot != "/repo" {
		t.Fatalf("unexpected VPN/repo fields: %+v", cfg)
	}
	if len(cfg.ExtraEntries) != 1 || cfg.ExtraEntries[0].ID != "notes" {
		t.Fatalf("ExtraEntries = %+v", cfg.ExtraEntries)
	}
	if len(cfg.ExtraEntries[0].Args) != 1 || cfg.ExtraEntries[0].Args[0] != "--fast" {
		t.Fatalf("ExtraEntries args = %+v", cfg.ExtraEntries[0].Args)
	}
}

func TestLoadMalformedConfigIsAnError(t *testing.T) {
	dir := t.TempDir()
	path := filepath.Join(dir, "control-plane.json")
	if err := os.WriteFile(path, []byte("{not json"), 0o644); err != nil {
		t.Fatal(err)
	}
	if _, err := Load(path); err == nil {
		t.Fatal("expected an error for malformed JSON")
	}
}
