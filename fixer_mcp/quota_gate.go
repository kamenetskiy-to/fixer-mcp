package main

import (
	"bytes"
	"fmt"
	"math"
	"os"
	"os/exec"
	"path/filepath"
	"regexp"
	"strconv"
	"strings"
	"time"
)

type ProviderQuota struct {
	PercentLeft int
	ResetDelay  time.Duration
	// Window names the check-my-limits column the verdict was derived from
	// ("5h", "7d", or "1m"). Empty when unknown. It is diagnostic only and
	// does not affect the legacy fields.
	Window string
	// ResetKnown distinguishes a parsed "reset in ..." countdown from a cell
	// with an unknown ("N/A") reset. A genuine 0%% with an unknown reset is
	// still real exhaustion, but it must not invent a long quota wait.
	ResetKnown bool
}

type QuotaGate interface {
	CheckQuota(providerName string) (ProviderQuota, bool, error)
}

// AccountQuotaGate is the optional provider/account-aware refinement of
// QuotaGate. check-my-limits prints one row per configured account (for
// example "CommandCode / Stas (primary)" and "CommandCode / Personal"), and a
// quota verdict must be attributed to the account the worker actually runs
// with — never to "the first row whose label contains the provider name".
type AccountQuotaGate interface {
	QuotaGate
	CheckQuotaForAccount(providerName string, accountLabel string) (ProviderQuota, bool, error)
}

// checkQuotaForAccount prefers account-aware attribution when the gate
// supports it and falls back to the plain provider check otherwise. An empty
// accountLabel means "the account bound to the worker's CLI config", which the
// row selection resolves to the "(primary)" row.
func checkQuotaForAccount(gate QuotaGate, providerName string, accountLabel string) (ProviderQuota, bool, error) {
	if gate == nil {
		return ProviderQuota{}, false, nil
	}
	if accountGate, ok := gate.(AccountQuotaGate); ok {
		return accountGate.CheckQuotaForAccount(providerName, accountLabel)
	}
	return gate.CheckQuota(providerName)
}

type checkMyLimitsGate struct{}

var DefaultQuotaGate QuotaGate = &checkMyLimitsGate{}

const checkMyLimitsPathEnv = "FIXER_CHECK_MY_LIMITS_PATH"

func resolveCheckMyLimitsCommand() (string, error) {
	if configured := strings.TrimSpace(os.Getenv(checkMyLimitsPathEnv)); configured != "" {
		return configured, nil
	}
	if resolved, err := exec.LookPath("check-my-limits"); err == nil {
		return resolved, nil
	}
	home, err := os.UserHomeDir()
	if err != nil {
		return "", fmt.Errorf("resolve check-my-limits: %w", err)
	}
	for _, candidate := range []string{
		filepath.Join(home, "bin", "check-my-limits"),
		filepath.Join(home, ".codex", "fixer_unattached", "bin", "check-my-limits"),
	} {
		if info, statErr := os.Stat(candidate); statErr == nil && !info.IsDir() && info.Mode()&0o111 != 0 {
			return candidate, nil
		}
	}
	return "", fmt.Errorf("check-my-limits executable not found; set %s or add it to PATH", checkMyLimitsPathEnv)
}

func (g *checkMyLimitsGate) CheckQuota(providerName string) (ProviderQuota, bool, error) {
	return g.CheckQuotaForAccount(providerName, "")
}

func (g *checkMyLimitsGate) CheckQuotaForAccount(providerName string, accountLabel string) (ProviderQuota, bool, error) {
	command, err := resolveCheckMyLimitsCommand()
	if err != nil {
		return ProviderQuota{}, false, err
	}
	cmd := execCommand(command)
	var stdout bytes.Buffer
	cmd.Stdout = &stdout
	if err := cmd.Run(); err != nil {
		return ProviderQuota{}, false, err
	}
	return parseCheckMyLimitsOutputForAccount(stdout.String(), providerName, accountLabel)
}

// checkMyLimitsWindow* indexes the check-my-limits table columns.
const (
	checkMyLimitsWindow5h = "5h"
	checkMyLimitsWindow7d = "7d"
	checkMyLimitsWindow1m = "1m"
)

type checkMyLimitsQuotaRow struct {
	// ProviderLabel is the account-qualified provider cell, e.g.
	// "CommandCode / Stas (primary)".
	ProviderLabel string
	// AccountLabel is the account part of the label ("Stas"), or the full
	// label when the row carries no explicit account suffix.
	AccountLabel string
	// Primary marks the row whose account is currently bound to the CLI
	// config (the account a worker actually launches with).
	Primary bool
	// Windows holds the parsed quota cell per window column.
	Windows map[string]ProviderQuota
}

var (
	quotaPercentLeftPattern = regexp.MustCompile(`(\d+(?:\.\d+)?)%\s+left`)
	quotaResetPattern       = regexp.MustCompile(`reset in (?:(\d+)d\s*)?(?:(\d+)h\s*)?(?:(\d+)m\s*)?(?:(\d+)s)?`)
)

// parseQuotaPercentLeft parses "N% left" and "N.M% left" cells. The decimal
// form is real: the CommandCode windows render fractional percentages
// ("50.0% left"). The old integer-only regex anchored on the digits right
// before the percent sign and read 50.0 as 0, 56.1 as 1, and 70.6 as 6 —
// turning healthy quotas into false exhaustion (host feedback #114).
//
// Positive fractions below 1 (e.g. 0.1%, 0.5%, 0.99%) represent positive
// remaining capacity and must not floor to 0 (which would trigger false
// exhaustion in quota gates requiring actual 0). We preserve the PercentLeft
// int compatibility by returning at least 1 for finite valid 0 < value < 1,
// while keeping exact zero (0, 0.0) as 0 (genuine exhaustion) and rejecting
// invalid/negative/error cells without inventing capacity.
func parseQuotaPercentLeft(cell string) (int, bool) {
	matchIdx := quotaPercentLeftPattern.FindStringSubmatchIndex(cell)
	if matchIdx == nil {
		return 0, false
	}
	prefix := strings.TrimSpace(cell[:matchIdx[0]])
	if strings.HasSuffix(prefix, "-") {
		return 0, false
	}
	match := quotaPercentLeftPattern.FindStringSubmatch(cell)
	if match == nil {
		return 0, false
	}
	value, err := strconv.ParseFloat(match[1], 64)
	if err != nil || math.IsNaN(value) || math.IsInf(value, 0) {
		return 0, false
	}
	if value < 0 {
		return 0, false
	}
	if value == 0 {
		return 0, true
	}
	percent := int(math.Floor(value + 1e-9))
	if percent < 1 {
		percent = 1
	}
	if percent > 100 {
		percent = 100
	}
	return percent, true
}

// parseQuotaCellWindow parses one check-my-limits quota cell. Malformed cells
// ("N/A", "—", error text) report found=false: an unparseable limit is never
// treated as exhaustion.
func parseQuotaCellWindow(cell string) (ProviderQuota, bool) {
	percentLeft, found := parseQuotaPercentLeft(cell)
	if !found {
		return ProviderQuota{}, false
	}
	quota := ProviderQuota{PercentLeft: percentLeft}
	if resetMatch := quotaResetPattern.FindStringSubmatch(cell); resetMatch != nil {
		component := func(raw string) int {
			value, _ := strconv.Atoi(raw)
			return value
		}
		hasComponent := false
		for _, raw := range resetMatch[1:] {
			if raw != "" {
				hasComponent = true
				break
			}
		}
		if hasComponent {
			delay := time.Duration(component(resetMatch[1]))*24*time.Hour +
				time.Duration(component(resetMatch[2]))*time.Hour +
				time.Duration(component(resetMatch[3]))*time.Minute +
				time.Duration(component(resetMatch[4]))*time.Second
			quota.ResetDelay = delay
			quota.ResetKnown = true
		}
	}
	return quota, true
}

func parseQuotaCell(cell string) (ProviderQuota, bool, error) {
	quota, found := parseQuotaCellWindow(cell)
	return quota, found, nil
}

// parseCheckMyLimitsRows parses the provider table into one row per provider
// account. The table columns are: provider label, status, 5h, 7d, 1m.
func parseCheckMyLimitsRows(output string) []checkMyLimitsQuotaRow {
	rows := []checkMyLimitsQuotaRow{}
	for _, line := range strings.Split(output, "\n") {
		if !strings.Contains(line, "│") {
			continue
		}
		parts := strings.Split(line, "│")
		if len(parts) < 4 {
			continue
		}
		label := strings.TrimSpace(parts[0])
		if label == "" || strings.Contains(label, "Провайдер") || strings.HasPrefix(label, "-") || strings.HasPrefix(label, "─") {
			continue
		}

		primary := false
		accountLabel := label
		if trimmed := strings.TrimSuffix(accountLabel, "(primary)"); trimmed != accountLabel {
			primary = true
			accountLabel = strings.TrimSpace(trimmed)
		}
		if separatorIndex := strings.Index(accountLabel, "/"); separatorIndex >= 0 {
			accountLabel = strings.TrimSpace(accountLabel[separatorIndex+1:])
		}

		row := checkMyLimitsQuotaRow{
			ProviderLabel: label,
			AccountLabel:  accountLabel,
			Primary:       primary,
			Windows:       map[string]ProviderQuota{},
		}
		for window, cellIndex := range map[string]int{
			checkMyLimitsWindow5h: 2,
			checkMyLimitsWindow7d: 3,
			checkMyLimitsWindow1m: 4,
		} {
			if cellIndex >= len(parts) {
				continue
			}
			if quota, found := parseQuotaCellWindow(strings.TrimSpace(parts[cellIndex])); found {
				row.Windows[window] = quota
			}
		}
		rows = append(rows, row)
	}
	return rows
}

// selectCheckMyLimitsRow attributes a provider/account to exactly one table
// row. Selection rules, in order:
//
//  1. An explicit account hint must match exactly one row (or one primary row);
//     a hint that matches nothing refuses the verdict instead of silently
//     borrowing another account's quota.
//  2. A single unambiguous provider row is used as-is.
//  3. Ambiguous multi-account providers resolve to the "(primary)" row — the
//     account actually bound to the worker's CLI config — never to whichever
//     row happens to come first in the output.
//  4. Anything else reports no row: an unattributable quota reading is not
//     invented exhaustion.
func selectCheckMyLimitsRow(rows []checkMyLimitsQuotaRow, providerName string, accountLabel string) (checkMyLimitsQuotaRow, bool) {
	provider := strings.ToLower(strings.TrimSpace(providerName))
	if provider == "" {
		return checkMyLimitsQuotaRow{}, false
	}
	matchesLabel := func(label string) bool {
		lower := strings.ToLower(label)
		if !strings.HasPrefix(lower, provider) {
			return false
		}
		if len(lower) == len(provider) {
			return true
		}
		next := lower[len(provider)]
		return !(next >= 'a' && next <= 'z') && !(next >= '0' && next <= '9')
	}

	family := []checkMyLimitsQuotaRow{}
	for _, row := range rows {
		if matchesLabel(strings.TrimSpace(strings.TrimSuffix(row.ProviderLabel, "(primary)"))) {
			family = append(family, row)
		}
	}
	if len(family) == 0 {
		return checkMyLimitsQuotaRow{}, false
	}

	hint := strings.ToLower(strings.TrimSpace(accountLabel))
	if hint != "" {
		matched := []checkMyLimitsQuotaRow{}
		for _, row := range family {
			if strings.Contains(strings.ToLower(row.AccountLabel), hint) || strings.Contains(strings.ToLower(row.ProviderLabel), hint) {
				matched = append(matched, row)
			}
		}
		if rows, ok := pickSingleOrPrimaryRow(matched); ok {
			return rows, true
		}
		// A named account that matches no row is a misattribution risk:
		// refuse rather than fall back to a different account's numbers.
		return checkMyLimitsQuotaRow{}, false
	}

	if rows, ok := pickSingleOrPrimaryRow(family); ok {
		return rows, true
	}
	return checkMyLimitsQuotaRow{}, false
}

func pickSingleOrPrimaryRow(candidates []checkMyLimitsQuotaRow) (checkMyLimitsQuotaRow, bool) {
	if len(candidates) == 1 {
		return candidates[0], true
	}
	primaries := []checkMyLimitsQuotaRow{}
	for _, row := range candidates {
		if row.Primary {
			primaries = append(primaries, row)
		}
	}
	if len(primaries) == 1 {
		return primaries[0], true
	}
	return checkMyLimitsQuotaRow{}, false
}

// providerQuotaFromRow consolidates one row's windows into a single verdict.
//
// Exhaustion is genuine only when some window parses to exactly 0% left; the
// binding constraint is the exhausted window with the longest reset. With no
// exhausted window the 5h window governs (falling back to 7d and then 1m when
// a column is hidden), matching the compact report's reading order.
func providerQuotaFromRow(row checkMyLimitsQuotaRow) (ProviderQuota, bool) {
	exhausted := []ProviderQuota{}
	for _, window := range []string{checkMyLimitsWindow5h, checkMyLimitsWindow7d, checkMyLimitsWindow1m} {
		quota, ok := row.Windows[window]
		if !ok {
			continue
		}
		quota.Window = window
		if quota.PercentLeft == 0 {
			exhausted = append(exhausted, quota)
		}
	}
	if len(exhausted) > 0 {
		binding := exhausted[0]
		for _, quota := range exhausted[1:] {
			if quota.ResetDelay > binding.ResetDelay {
				binding = quota
			}
		}
		return binding, true
	}
	for _, window := range []string{checkMyLimitsWindow5h, checkMyLimitsWindow7d, checkMyLimitsWindow1m} {
		if quota, ok := row.Windows[window]; ok {
			quota.Window = window
			return quota, true
		}
	}
	return ProviderQuota{}, false
}

func parseCheckMyLimitsOutput(output, providerName string) (ProviderQuota, bool, error) {
	return parseCheckMyLimitsOutputForAccount(output, providerName, "")
}

func parseCheckMyLimitsOutputForAccount(output, providerName, accountLabel string) (ProviderQuota, bool, error) {
	row, found := selectCheckMyLimitsRow(parseCheckMyLimitsRows(output), providerName, accountLabel)
	if !found {
		return ProviderQuota{}, false, nil
	}
	quota, ok := providerQuotaFromRow(row)
	return quota, ok, nil
}
