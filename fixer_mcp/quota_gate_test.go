package main

import (
	"fmt"
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

// Host feedback #114: fractional percentages are real ("50.0% left") and the
// old integer-only regex read 50.0 as 0, 56.1 as 1, and 70.6 as 6 — inventing
// exhaustion for healthy quotas.
func TestParseQuotaCellDecimalFractions(t *testing.T) {
	tests := []struct {
		input       string
		percentLeft int
		found       bool
		resetKnown  bool
	}{
		{"50.0% left, reset in 2d 04h 00m", 50, true, true},
		{"56.1% left, reset in 3d 02h 10m", 56, true, true},
		{"70.6% left, reset in 1d 00h 00m", 70, true, true},
		{"100% left, reset in 5h 00m", 100, true, true},
		{"99.99% left, reset in 5h 00m", 99, true, true},
		{"0.1% left, reset in 2h 00m", 1, true, true},
		{"0.5% left, reset in 2h 00m", 1, true, true},
		{"0.99% left, reset in 2h 00m", 1, true, true},
		{"0.0% left, reset in 30d 00h 00m", 0, true, true},
		{"0% left, reset in 30d 00h 00m", 0, true, true},
		{"-5% left, reset in 2h 00m", 0, false, false},
		{"40% left, reset N/A", 40, true, false},
		{"N/A", 0, false, false},
		{"error: unavailable", 0, false, false},
	}
	for _, tc := range tests {
		q, found, err := parseQuotaCell(tc.input)
		if err != nil {
			t.Fatalf("unexpected error for %q: %v", tc.input, err)
		}
		if found != tc.found {
			t.Fatalf("expected found %v for %q, got %v", tc.found, tc.input, found)
		}
		if !found {
			continue
		}
		if q.PercentLeft != tc.percentLeft {
			t.Errorf("expected %d%% for %q, got %d%%", tc.percentLeft, tc.input, q.PercentLeft)
		}
		if q.ResetKnown != tc.resetKnown {
			t.Errorf("expected resetKnown %v for %q, got %v", tc.resetKnown, tc.input, q.ResetKnown)
		}
	}
}

// The 114 repro: a primary CommandCode account at weekly 50.0% must never read
// as exhausted.
func TestParseCheckMyLimitsOutputFractionalQuotaIsNotExhausted(t *testing.T) {
	output := `
CommandCode / Stas (primary) │ ok │ 87.5% left, reset in 2h 10m │ 50.0% left, reset in 3d 04h 00m │ 30.0% left, reset in 12d 00h 00m
`
	q, found, err := parseCheckMyLimitsOutput(output, "CommandCode")
	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}
	if !found {
		t.Fatal("expected a quota verdict for the primary CommandCode row")
	}
	if q.PercentLeft != 87 {
		t.Fatalf("fractional 87.5 must parse as 87, got %d", q.PercentLeft)
	}
	if q.PercentLeft == 0 {
		t.Fatal("weekly 50.0% must never be invented exhaustion")
	}
}

// Multi-account rows must be attributed to the account the worker runs with
// (the primary/active CLI account or an explicit account hint) — never to
// whichever account row happens to appear first.
func TestParseCheckMyLimitsOutputAccountAttribution(t *testing.T) {
	output := `
CommandCode / Personal       │ ok │ 0% left, reset in 1h 00m    │ 0.0% left, reset in 2d 00h 00m  │ —
CommandCode / Stas (primary) │ ok │ 50.0% left, reset in 2h 00m │ 56.1% left, reset in 3d 00h 00m │ —
`
	t.Run("primary account wins without a hint", func(t *testing.T) {
		q, found, err := parseCheckMyLimitsOutput(output, "CommandCode")
		if err != nil || !found {
			t.Fatalf("expected primary row verdict, found=%v err=%v", found, err)
		}
		if q.PercentLeft != 50 {
			t.Fatalf("the other account's 0%% must not leak into attribution, got %d%%", q.PercentLeft)
		}
	})

	t.Run("explicit account hint selects that account", func(t *testing.T) {
		q, found, err := parseCheckMyLimitsOutputForAccount(output, "CommandCode", "Personal")
		if err != nil || !found {
			t.Fatalf("expected hinted row verdict, found=%v err=%v", found, err)
		}
		if q.PercentLeft != 0 || q.Window != checkMyLimitsWindow7d {
			t.Fatalf("expected the hinted account's real 0%%, got %+v", q)
		}
		if q.ResetDelay != 2*24*time.Hour {
			t.Fatalf("expected binding weekly window reset, got %v", q.ResetDelay)
		}
	})

	t.Run("unmatched hint refuses instead of guessing", func(t *testing.T) {
		if _, found, err := parseCheckMyLimitsOutputForAccount(output, "CommandCode", "UnknownAccount"); err != nil || found {
			t.Fatalf("an unmatched account hint must not fall back to another account, found=%v err=%v", found, err)
		}
	})

	t.Run("ambiguous rows without a primary are not guessed", func(t *testing.T) {
		ambiguous := `
CommandCode / Alpha │ ok │ 0% left, reset in 1h 00m │ — │ —
CommandCode / Beta  │ ok │ 90% left, reset in 2h 00m │ — │ —
`
		if _, found, _ := parseCheckMyLimitsOutput(ambiguous, "CommandCode"); found {
			t.Fatal("multi-account rows without a primary must not be attributed by position")
		}
	})
}

// All windows participate in the exhaustion verdict, including the monthly
// ("1m") column, and the binding constraint is the longest reset.
func TestParseCheckMyLimitsOutputMonthlyExhaustion(t *testing.T) {
	output := `
Kimi Code │ paid │ 40% left, reset in 3h 35m │ 45% left, reset in 5d 06h 35m │ 0% left, reset in 30d 00h 00m
`
	q, found, err := parseCheckMyLimitsOutput(output, "Kimi Code")
	if err != nil || !found {
		t.Fatalf("expected monthly verdict, found=%v err=%v", found, err)
	}
	if q.PercentLeft != 0 || q.Window != checkMyLimitsWindow1m {
		t.Fatalf("monthly exhaustion must be reported, got %+v", q)
	}
	if q.ResetDelay != 30*24*time.Hour {
		t.Fatalf("expected 30d monthly reset, got %v", q.ResetDelay)
	}
}

func TestParseCheckMyLimitsOutputLongestExhaustedWindowBinds(t *testing.T) {
	output := `
Claude Code │ pro │ 0% left, reset in 1h 02m │ 0% left, reset in 4d 07h 24m │ —
`
	q, found, err := parseCheckMyLimitsOutput(output, "Claude Code")
	if err != nil || !found {
		t.Fatalf("expected exhaustion verdict, found=%v err=%v", found, err)
	}
	if q.PercentLeft != 0 || q.ResetDelay != 4*24*time.Hour+7*time.Hour+24*time.Minute {
		t.Fatalf("the longest exhausted window must bind, got %+v", q)
	}
}

// Malformed or error cells are never invented exhaustion.
func TestParseCheckMyLimitsOutputMalformedCellsAreNotFound(t *testing.T) {
	for _, output := range []string{
		"Agy/Gemini │ error │ N/A │ — │ —",
		"Agy/Gemini │ network │ — │ — │ —",
		"Agy/Gemini │ no-auth │ — │ — │ —",
	} {
		if q, found, err := parseCheckMyLimitsOutput(output, "Agy/Gemini"); err != nil || found {
			t.Fatalf("malformed limit %q must not become exhaustion: q=%+v found=%v err=%v", output, q, found, err)
		}
	}
}

// Genuine real 0% and 0.0% are still exhaustion (with the reset countdown kept).
func TestParseCheckMyLimitsOutputRealZeroIsExhaustion(t *testing.T) {
	for _, rawZero := range []string{"0% left", "0.0% left"} {
		output := fmt.Sprintf(`
Codex/OpenAI │ pro │ — (7d лимит израсходован) │ %s, reset in 4d 07h 24m │ —
`, rawZero)
		q, found, err := parseCheckMyLimitsOutput(output, "Codex")
		if err != nil || !found {
			t.Fatalf("expected exhaustion verdict for %q, found=%v err=%v", rawZero, found, err)
		}
		if q.PercentLeft != 0 || !q.ResetKnown || q.ResetDelay != 4*24*time.Hour+7*time.Hour+24*time.Minute {
			t.Fatalf("real zero %q must stay genuine exhaustion, got %+v", rawZero, q)
		}
	}
}

// Sub-one fractions (0.1%, 0.5%, 0.99%) represent positive remaining capacity
// and must not collapse to genuine exhaustion.
func TestParseCheckMyLimitsOutputSubOneFractionsAreNotExhausted(t *testing.T) {
	for _, fraction := range []struct {
		raw  string
		want int
	}{
		{"0.1%", 1},
		{"0.5%", 1},
		{"0.99%", 1},
	} {
		output := fmt.Sprintf(`
Claude Code │ pro │ %s left, reset in 1h 00m │ 50%% left, reset in 2d 00h 00m │ —
`, fraction.raw)
		q, found, err := parseCheckMyLimitsOutput(output, "Claude Code")
		if err != nil || !found {
			t.Fatalf("expected verdict for %s, found=%v err=%v", fraction.raw, found, err)
		}
		if q.PercentLeft != fraction.want {
			t.Fatalf("sub-one fraction %s must parse as %d%%, got %d%%", fraction.raw, fraction.want, q.PercentLeft)
		}
		if q.PercentLeft == 0 {
			t.Fatalf("sub-one fraction %s must not be genuine exhaustion", fraction.raw)
		}
	}
}

// Monthly fractional quota (0.1%) preserves positive capacity and avoids the
// 30d zero-gate exhaustion, while actual 0.0% / 0% triggers genuine exhaustion.
func TestParseCheckMyLimitsOutputMonthlyFractionalZeroGate(t *testing.T) {
	t.Run("monthly actual 0.0% triggers genuine exhaustion zero-gate", func(t *testing.T) {
		output := `
Kimi Code │ paid │ 40% left, reset in 3h 35m │ 45% left, reset in 5d 06h 35m │ 0.0% left, reset in 30d 00h 00m
`
		q, found, err := parseCheckMyLimitsOutput(output, "Kimi Code")
		if err != nil || !found {
			t.Fatalf("expected monthly verdict, found=%v err=%v", found, err)
		}
		if q.PercentLeft != 0 || q.Window != checkMyLimitsWindow1m {
			t.Fatalf("monthly 0.0%% must trigger genuine exhaustion zero-gate, got %+v", q)
		}
		if q.ResetDelay != 30*24*time.Hour {
			t.Fatalf("expected 30d reset delay, got %v", q.ResetDelay)
		}
	})

	t.Run("monthly fractional 0.1% preserves positive capacity and avoids zero-gate", func(t *testing.T) {
		output := `
Kimi Code │ paid │ 40% left, reset in 3h 35m │ 45% left, reset in 5d 06h 35m │ 0.1% left, reset in 30d 00h 00m
`
		q, found, err := parseCheckMyLimitsOutput(output, "Kimi Code")
		if err != nil || !found {
			t.Fatalf("expected verdict, found=%v err=%v", found, err)
		}
		if q.PercentLeft == 0 {
			t.Fatalf("monthly 0.1%% must not trigger zero-gate exhaustion, got %+v", q)
		}
		// Since no window is exhausted (5h=40%, 7d=45%, 1m=1%), the 5h window governs
		if q.Window != checkMyLimitsWindow5h || q.PercentLeft != 40 {
			t.Fatalf("expected governing 5h window (40%%), got window=%s percent=%d", q.Window, q.PercentLeft)
		}
	})
}

