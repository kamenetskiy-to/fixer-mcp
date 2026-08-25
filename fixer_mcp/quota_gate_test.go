package main

import (
	"os"
	"path/filepath"
	"testing"
	"time"
)

func TestResolveCheckMyLimitsCommandHonorsExplicitPath(t *testing.T) {
	t.Setenv(checkMyLimitsPathEnv, "/custom/bin/check-my-limits")
	resolved, err := resolveCheckMyLimitsCommand()
	if err != nil {
		t.Fatalf("resolveCheckMyLimitsCommand returned error: %v", err)
	}
	if resolved != "/custom/bin/check-my-limits" {
		t.Fatalf("expected explicit path, got %q", resolved)
	}
}

func TestResolveCheckMyLimitsCommandFindsArchitectHomeBin(t *testing.T) {
	t.Setenv(checkMyLimitsPathEnv, "")
	t.Setenv("PATH", "")
	home := t.TempDir()
	t.Setenv("HOME", home)
	binDir := filepath.Join(home, "bin")
	if err := os.MkdirAll(binDir, 0o755); err != nil {
		t.Fatal(err)
	}
	path := filepath.Join(binDir, "check-my-limits")
	if err := os.WriteFile(path, []byte("#!/bin/sh\n"), 0o755); err != nil {
		t.Fatal(err)
	}
	resolved, err := resolveCheckMyLimitsCommand()
	if err != nil {
		t.Fatalf("resolveCheckMyLimitsCommand returned error: %v", err)
	}
	if resolved != path {
		t.Fatalf("expected %q, got %q", path, resolved)
	}
}

func TestParseQuotaCell(t *testing.T) {
	tests := []struct {
		input       string
		percentLeft int
		delay       time.Duration
		found       bool
	}{
		{"0% left, reset in 4d 07h 24m", 0, 4*24*time.Hour + 7*time.Hour + 24*time.Minute, true},
		{"40% left, reset in 3h 35m", 40, 3*time.Hour + 35*time.Minute, true},
		{"— (7d лимит израсходован)", 0, 0, false},
		{"N/A", 0, 0, false},
		{"0% left, reset in 1h 02m", 0, 1*time.Hour + 2*time.Minute, true},
	}
	for _, tc := range tests {
		q, found, err := parseQuotaCell(tc.input)
		if err != nil {
			t.Fatalf("unexpected error for %q: %v", tc.input, err)
		}
		if found != tc.found {
			t.Errorf("expected found %v for %q, got %v", tc.found, tc.input, found)
		}
		if found {
			if q.PercentLeft != tc.percentLeft {
				t.Errorf("expected %d%% for %q, got %d%%", tc.percentLeft, tc.input, q.PercentLeft)
			}
			if q.ResetDelay != tc.delay {
				t.Errorf("expected %v delay for %q, got %v", tc.delay, tc.input, q.ResetDelay)
			}
		}
	}
}

func TestParseCheckMyLimitsOutput(t *testing.T) {
	output := `
Провайдер      │ Статус   │ Лимиты 5h                      │ Лимиты 7d                      │ Usage credits
───────────────┼──────────┼────────────────────────────────┼────────────────────────────────┼────────────────
Codex/OpenAI   │ pro      │ — (7d лимит израсходован)      │ 0% left, reset in 4d 07h 24m   │ —
Kimi Code      │ paid     │ 40% left, reset in 3h 35m      │ 45% left, reset in 5d 06h 35m  │ —
Claude Code    │ pro      │ 0% left, reset in 1h 02m       │ 72% left, reset in 5d 09h 22m  │ €62.25 credits
Agy/Gemini     │ ok       │ 75% left, reset in 4h 30m      │ 26% left, reset in 4d 08h 06m  │ —
Agy/Claude+GPT │ ok       │ — (7d лимит израсходован)      │ 0% left, reset in 0d 16h 35m   │ —
`
	tests := []struct {
		provider string
		found    bool
		pctLeft  int
		delay    time.Duration
	}{
		{"Codex", true, 0, 4*24*time.Hour + 7*time.Hour + 24*time.Minute},
		{"Kimi Code", true, 40, 3*time.Hour + 35*time.Minute},
		{"Claude Code", true, 0, 1*time.Hour + 2*time.Minute},
		{"Agy/Gemini", true, 75, 4*time.Hour + 30*time.Minute},
		{"Agy/Claude+GPT", true, 0, 16*time.Hour + 35*time.Minute},
		{"Unknown", false, 0, 0},
	}
	for _, tc := range tests {
		q, found, err := parseCheckMyLimitsOutput(output, tc.provider)
		if err != nil {
			t.Fatalf("unexpected err for %q: %v", tc.provider, err)
		}
		if found != tc.found {
			t.Errorf("expected found %v for %q, got %v", tc.found, tc.provider, found)
		}
		if found {
			if q.PercentLeft != tc.pctLeft {
				t.Errorf("expected %d%% for %q, got %d%%", tc.pctLeft, tc.provider, q.PercentLeft)
			}
			if q.ResetDelay != tc.delay {
				t.Errorf("expected %v delay for %q, got %v", tc.delay, tc.provider, q.ResetDelay)
			}
		}
	}
}
