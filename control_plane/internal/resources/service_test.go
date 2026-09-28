package resources

import (
	"context"
	"os"
	"path/filepath"
	"strings"
	"testing"
)

func TestParseQuotaExtractsStructuredWindows(t *testing.T) {
	raw := strings.Join([]string{
		"Провайдер │ Статус │ Лимиты 5h │ Лимиты 7d │ Лимиты 1m",
		"-----------------------------------------+--------------+----------------------+----------------------+----------------------",
		"Codex / Babushka │ plus │ 27% left, reset in 11m 45s │ 89% left, reset in 4d 13h 13m │ —",
		"OpenCode / Stas │ error │ — │ unknown error │ —",
		"Agy/Images │ ok │ ~13 left, idle │ last spent 10.09 06:08 │ —",
	}, "\n")
	quotas := parseQuota(raw)
	if len(quotas) != 3 {
		t.Fatalf("parsed %d rows, want 3: %#v", len(quotas), quotas)
	}

	codex := quotas[0]
	if codex.Window5h.Percent == nil || *codex.Window5h.Percent != 27 {
		t.Fatalf("5h percent = %#v, want 27", codex.Window5h.Percent)
	}
	if codex.Window5h.Reset != "11m 45s" {
		t.Fatalf("5h reset = %q", codex.Window5h.Reset)
	}
	if codex.Window7d.Percent == nil || *codex.Window7d.Percent != 89 {
		t.Fatalf("7d percent = %#v, want 89", codex.Window7d.Percent)
	}
	if codex.Window7d.Reset != "4d 13h 13m" {
		t.Fatalf("7d reset = %q", codex.Window7d.Reset)
	}
	if codex.Window1m.Percent != nil || codex.Window1m.Text != "—" {
		t.Fatalf("empty window must stay text-only: %#v", codex.Window1m)
	}

	errorRow := quotas[1]
	if errorRow.Window7d.Percent != nil || errorRow.Window7d.Text != "unknown error" {
		t.Fatalf("error cell must not become a number: %#v", errorRow.Window7d)
	}
	idle := quotas[2]
	if idle.Window5h.Percent != nil || idle.Window5h.Text != "~13 left, idle" {
		t.Fatalf("free-form cell must be preserved: %#v", idle.Window5h)
	}
}

func TestCMLBinaryFindsManagedBinWithoutShellPATH(t *testing.T) {
	dir := t.TempDir()
	cml := filepath.Join(dir, "cml")
	if err := os.WriteFile(cml, []byte("#!/bin/sh\nprintf managed-cml\\n"), 0o700); err != nil {
		t.Fatal(err)
	}
	t.Setenv("PATH", t.TempDir())
	t.Setenv("FIXER_USER_BIN", dir)
	resolved, name, err := cmlBinary()
	if err != nil {
		t.Fatal(err)
	}
	if resolved != cml || name != "cml" {
		t.Fatalf("managed cml = (%q, %q), want (%q, cml)", resolved, name, cml)
	}
}

func TestRefreshQuotaPrefersAndPreservesCMLReport(t *testing.T) {
	dir := t.TempDir()
	cml := filepath.Join(dir, "cml")
	check := filepath.Join(dir, "check-my-limits")
	if err := os.WriteFile(cml, []byte("#!/bin/sh\nprintf '%s\\n' 'Провайдер │ Статус' 'Pi │ ok'\n"), 0o700); err != nil {
		t.Fatal(err)
	}
	if err := os.WriteFile(check, []byte("#!/bin/sh\nprintf '%s\\n' 'wrong fallback'\n"), 0o700); err != nil {
		t.Fatal(err)
	}
	t.Setenv("PATH", dir)
	t.Setenv("XDG_CACHE_HOME", filepath.Join(dir, "cache"))
	snapshot := RefreshQuota(context.Background())
	if snapshot.Error != "" {
		t.Fatalf("RefreshQuota error: %s", snapshot.Error)
	}
	if snapshot.CMLCommand != "cml" {
		t.Fatalf("quota command = %q, want cml", snapshot.CMLCommand)
	}
	if snapshot.CMLOutput != "Провайдер │ Статус\nPi │ ok" {
		t.Fatalf("raw cml output was changed: %q", snapshot.CMLOutput)
	}
	if len(snapshot.Quotas) != 1 || snapshot.Quotas[0].Provider != "Pi" {
		t.Fatalf("parsed quotas = %#v", snapshot.Quotas)
	}
	cached := Inspect()
	if cached.CMLOutput != snapshot.CMLOutput || cached.CMLCommand != "cml" {
		t.Fatalf("cml report was not restored from UI cache: %#v", cached)
	}
	info, err := os.Stat(cmlCachePath())
	if err != nil {
		t.Fatal(err)
	}
	if info.Mode().Perm() != 0o600 {
		t.Fatalf("cml cache permissions = %o", info.Mode().Perm())
	}
}
