package main

import (
	"context"
	"database/sql"
	"strings"
	"testing"
)

func setupEconomyHandlersTestDB(t *testing.T) *sql.DB {
	t.Helper()

	testDB := setupGetProjectsTestDB(t)
	t.Cleanup(func() {
		_ = testDB.Close()
	})

	_, err := testDB.Exec(`
		CREATE TABLE project_balance (
			project_id INTEGER NOT NULL UNIQUE,
			balance_units INTEGER NOT NULL DEFAULT 0,
			currency TEXT NOT NULL DEFAULT 'usd',
			updated_at TEXT NOT NULL DEFAULT CURRENT_TIMESTAMP
		);
		CREATE UNIQUE INDEX project_balance_project_unique_idx ON project_balance(project_id);
		CREATE TABLE fixer_spend_authority (
			project_id INTEGER NOT NULL UNIQUE,
			enabled INTEGER NOT NULL DEFAULT 0,
			allowance_units INTEGER NOT NULL DEFAULT 0,
			updated_at TEXT NOT NULL DEFAULT CURRENT_TIMESTAMP
		);
		CREATE UNIQUE INDEX fixer_spend_authority_project_unique_idx ON fixer_spend_authority(project_id);
		CREATE TABLE balance_ledger (
			id INTEGER PRIMARY KEY AUTOINCREMENT,
			project_id INTEGER NOT NULL,
			delta_units INTEGER NOT NULL,
			kind TEXT NOT NULL CHECK(kind IN ('credit', 'spend', 'authority_grant')),
			reason TEXT NOT NULL,
			actor_role TEXT NOT NULL,
			created_at TEXT NOT NULL DEFAULT CURRENT_TIMESTAMP
		);
		CREATE INDEX balance_ledger_project_id_idx ON balance_ledger(project_id, id);
		CREATE INDEX balance_ledger_kind_idx ON balance_ledger(project_id, kind, id);
	`)
	if err != nil {
		t.Fatalf("create economy schema: %v", err)
	}

	return testDB
}

func installEconomyHandlersTestState(t *testing.T, testDB *sql.DB) {
	t.Helper()

	originalDB := db
	originalRole := authorizedRole
	originalProjectID := authorizedProjectId
	originalSessionID := authorizedSessionId
	t.Cleanup(func() {
		db = originalDB
		authorizedRole = originalRole
		authorizedProjectId = originalProjectID
		authorizedSessionId = originalSessionID
	})

	db = testDB
	authorizedRole = ""
	authorizedProjectId = 0
	authorizedSessionId = 0
}

func seedEconomyProject(t *testing.T, projectID int, balanceUnits int64, enabled bool, allowanceUnits int64) {
	t.Helper()

	authorizedRole = "overseer"
	authorizedProjectId = 0

	if balanceUnits > 0 {
		callResult, _, err := CreditProjectBalance(context.Background(), nil, CreditProjectBalanceInput{
			ProjectId:   projectID,
			AmountUnits: balanceUnits,
			Reason:      "seed balance",
		})
		if err != nil {
			t.Fatalf("seed credit: %v", err)
		}
		if callResult != nil {
			t.Fatalf("expected nil seed credit call result, got %+v", callResult)
		}
	}

	callResult, _, err := SetFixerSpendAuthority(context.Background(), nil, SetFixerSpendAuthorityInput{
		ProjectId:      projectID,
		Enabled:        enabled,
		AllowanceUnits: allowanceUnits,
		Reason:         "seed authority",
	})
	if err != nil {
		t.Fatalf("seed authority: %v", err)
	}
	if callResult != nil {
		t.Fatalf("expected nil seed authority call result, got %+v", callResult)
	}
}

func economyLedgerCount(t *testing.T, projectID int) int {
	t.Helper()

	var count int
	if err := db.QueryRow("SELECT COUNT(*) FROM balance_ledger WHERE project_id = ?", projectID).Scan(&count); err != nil {
		t.Fatalf("count ledger rows: %v", err)
	}
	return count
}

func TestCreditProjectBalanceIncreasesBalanceAndLedger(t *testing.T) {
	testDB := setupEconomyHandlersTestDB(t)
	installEconomyHandlersTestState(t, testDB)

	authorizedRole = "overseer"
	callResult, out, err := CreditProjectBalance(context.Background(), nil, CreditProjectBalanceInput{
		ProjectId:   1,
		AmountUnits: 1250,
		Reason:      "initial operator credit",
	})
	if err != nil {
		t.Fatalf("credit project balance: %v", err)
	}
	if callResult != nil {
		t.Fatalf("expected nil call result, got %+v", callResult)
	}
	if out.Status != "success" || out.Balance.BalanceUnits != 1250 || out.Balance.Currency != "usd" {
		t.Fatalf("unexpected credit output: %+v", out)
	}
	if out.Ledger.Kind != "credit" || out.Ledger.DeltaUnits != 1250 || out.Ledger.ActorRole != "overseer" || out.Ledger.Reason != "initial operator credit" {
		t.Fatalf("unexpected credit ledger: %+v", out.Ledger)
	}

	_, ledgerOut, err := GetBalanceLedger(context.Background(), nil, GetBalanceLedgerInput{ProjectId: 1, Limit: 10})
	if err != nil {
		t.Fatalf("get balance ledger: %v", err)
	}
	if len(ledgerOut.Ledger) != 1 || ledgerOut.Ledger[0].Kind != "credit" || ledgerOut.Ledger[0].DeltaUnits != 1250 {
		t.Fatalf("unexpected ledger rows: %+v", ledgerOut.Ledger)
	}
}

func TestSetFixerSpendAuthorityWritesAuthorityLedger(t *testing.T) {
	testDB := setupEconomyHandlersTestDB(t)
	installEconomyHandlersTestState(t, testDB)

	authorizedRole = "overseer"
	callResult, out, err := SetFixerSpendAuthority(context.Background(), nil, SetFixerSpendAuthorityInput{
		ProjectId:      1,
		Enabled:        true,
		AllowanceUnits: 700,
		Reason:         "grant fixer operating budget",
	})
	if err != nil {
		t.Fatalf("set fixer spend authority: %v", err)
	}
	if callResult != nil {
		t.Fatalf("expected nil call result, got %+v", callResult)
	}
	if out.Status != "success" || !out.Authority.Enabled || out.Authority.AllowanceUnits != 700 {
		t.Fatalf("unexpected authority output: %+v", out)
	}
	if out.Ledger.Kind != "authority_grant" || out.Ledger.DeltaUnits != 700 || out.Ledger.ActorRole != "overseer" {
		t.Fatalf("unexpected authority ledger: %+v", out.Ledger)
	}

	authorizedRole = "fixer"
	authorizedProjectId = 1
	_, balanceOut, err := GetProjectBalance(context.Background(), nil, GetProjectBalanceInput{})
	if err != nil {
		t.Fatalf("fixer get project balance: %v", err)
	}
	if balanceOut.BalanceUnits != 0 || balanceOut.Currency != "usd" || !balanceOut.Authority.Enabled || balanceOut.Authority.AllowanceUnits != 700 {
		t.Fatalf("unexpected fixer balance view: %+v", balanceOut)
	}
}

func TestRecordFixerSpendWithinAllowanceAndBalanceDecrementsBothAndLedgers(t *testing.T) {
	testDB := setupEconomyHandlersTestDB(t)
	installEconomyHandlersTestState(t, testDB)
	seedEconomyProject(t, 1, 1000, true, 500)

	authorizedRole = "fixer"
	authorizedProjectId = 1
	callResult, out, err := RecordFixerSpend(context.Background(), nil, RecordFixerSpendInput{
		AmountUnits: 200,
		Reason:      "provider API spend",
	})
	if err != nil {
		t.Fatalf("record fixer spend: %v", err)
	}
	if callResult != nil {
		t.Fatalf("expected nil call result, got %+v", callResult)
	}
	if out.Status != "success" || out.Balance.BalanceUnits != 800 || out.Balance.Authority.AllowanceUnits != 300 {
		t.Fatalf("unexpected spend output: %+v", out)
	}
	if out.Ledger.Kind != "spend" || out.Ledger.DeltaUnits != -200 || out.Ledger.ActorRole != "fixer" || out.Ledger.Reason != "provider API spend" {
		t.Fatalf("unexpected spend ledger: %+v", out.Ledger)
	}

	_, ledgerOut, err := GetBalanceLedger(context.Background(), nil, GetBalanceLedgerInput{Limit: 10})
	if err != nil {
		t.Fatalf("get balance ledger: %v", err)
	}
	if len(ledgerOut.Ledger) != 3 || ledgerOut.Ledger[0].Kind != "spend" || ledgerOut.Ledger[1].Kind != "authority_grant" || ledgerOut.Ledger[2].Kind != "credit" {
		t.Fatalf("unexpected recent ledger ordering: %+v", ledgerOut.Ledger)
	}
}

func TestRecordFixerSpendRejectsViolationsWithoutMutation(t *testing.T) {
	cases := []struct {
		name           string
		balanceUnits   int64
		authorityOn    bool
		allowanceUnits int64
		spendUnits     int64
		wantError      string
	}{
		{
			name:           "over allowance",
			balanceUnits:   1000,
			authorityOn:    true,
			allowanceUnits: 100,
			spendUnits:     150,
			wantError:      "allowance",
		},
		{
			name:           "over balance",
			balanceUnits:   100,
			authorityOn:    true,
			allowanceUnits: 500,
			spendUnits:     150,
			wantError:      "balance",
		},
		{
			name:           "authority disabled",
			balanceUnits:   1000,
			authorityOn:    false,
			allowanceUnits: 500,
			spendUnits:     10,
			wantError:      "disabled",
		},
	}

	for _, tc := range cases {
		t.Run(tc.name, func(t *testing.T) {
			testDB := setupEconomyHandlersTestDB(t)
			installEconomyHandlersTestState(t, testDB)
			seedEconomyProject(t, 1, tc.balanceUnits, tc.authorityOn, tc.allowanceUnits)

			before, err := fetchProjectBalanceRecord(1)
			if err != nil {
				t.Fatalf("fetch before state: %v", err)
			}
			beforeLedgerCount := economyLedgerCount(t, 1)

			authorizedRole = "fixer"
			authorizedProjectId = 1
			callResult, _, err := RecordFixerSpend(context.Background(), nil, RecordFixerSpendInput{
				AmountUnits: tc.spendUnits,
				Reason:      "should reject",
			})
			if err == nil {
				t.Fatalf("expected spend rejection")
			}
			if callResult == nil || !callResult.IsError {
				t.Fatalf("expected MCP error result, got %+v", callResult)
			}
			if !strings.Contains(err.Error(), tc.wantError) {
				t.Fatalf("expected error containing %q, got %v", tc.wantError, err)
			}

			after, err := fetchProjectBalanceRecord(1)
			if err != nil {
				t.Fatalf("fetch after state: %v", err)
			}
			if after.BalanceUnits != before.BalanceUnits ||
				after.Authority.Enabled != before.Authority.Enabled ||
				after.Authority.AllowanceUnits != before.Authority.AllowanceUnits {
				t.Fatalf("state mutated after rejection: before=%+v after=%+v", before, after)
			}
			if got := economyLedgerCount(t, 1); got != beforeLedgerCount {
				t.Fatalf("ledger mutated after rejection: before=%d after=%d", beforeLedgerCount, got)
			}
		})
	}
}

func TestEconomyRoleGuards(t *testing.T) {
	testDB := setupEconomyHandlersTestDB(t)
	installEconomyHandlersTestState(t, testDB)

	authorizedRole = "fixer"
	authorizedProjectId = 1
	callResult, _, err := CreditProjectBalance(context.Background(), nil, CreditProjectBalanceInput{
		ProjectId:   1,
		AmountUnits: 100,
		Reason:      "forbidden credit",
	})
	if err == nil || !strings.Contains(err.Error(), "overseer") || callResult == nil || !callResult.IsError {
		t.Fatalf("expected fixer credit to be rejected by overseer guard, result=%+v err=%v", callResult, err)
	}

	callResult, _, err = SetFixerSpendAuthority(context.Background(), nil, SetFixerSpendAuthorityInput{
		ProjectId:      1,
		Enabled:        true,
		AllowanceUnits: 100,
		Reason:         "forbidden grant",
	})
	if err == nil || !strings.Contains(err.Error(), "overseer") || callResult == nil || !callResult.IsError {
		t.Fatalf("expected fixer authority grant to be rejected by overseer guard, result=%+v err=%v", callResult, err)
	}

	authorizedRole = "overseer"
	authorizedProjectId = 0
	callResult, _, err = RecordFixerSpend(context.Background(), nil, RecordFixerSpendInput{
		AmountUnits: 10,
		Reason:      "forbidden spend",
	})
	if err == nil || !strings.Contains(err.Error(), "fixer") || callResult == nil || !callResult.IsError {
		t.Fatalf("expected overseer spend to be rejected by fixer guard, result=%+v err=%v", callResult, err)
	}
}

// The economy/balance surface is retired from every runtime tool list (the R1
// track that owned it is frozen). FIXER_MCP_ENABLE_ECONOMY=1 restores it for
// fixer/overseer/legacy modes; netrunner never had it and never gets it.
func TestEconomyToolSurfaceIsRetiredUnlessEnabled(t *testing.T) {
	t.Setenv("FIXER_MCP_ENABLE_ECONOMY", "")
	for _, role := range []string{"", "fixer", "overseer", "netrunner"} {
		for _, registered := range registeredToolNamesForMode(role) {
			for _, name := range economyToolNames {
				if registered == name {
					t.Fatalf("economy tool %q must not appear in %q tool surface by default", name, role)
				}
			}
		}
	}

	t.Setenv("FIXER_MCP_ENABLE_ECONOMY", "1")
	for _, role := range []string{"", "fixer", "overseer"} {
		toolSet := make(map[string]bool)
		for _, registered := range registeredToolNamesForMode(role) {
			toolSet[registered] = true
		}
		for _, name := range economyToolNames {
			if !toolSet[name] {
				t.Fatalf("economy tool %q must be restorable in %q tool surface", name, role)
			}
		}
	}
	for _, registered := range registeredToolNamesForMode("netrunner") {
		for _, name := range economyToolNames {
			if registered == name {
				t.Fatalf("economy tool %q must stay out of the netrunner tool surface even when enabled", name)
			}
		}
	}
}
