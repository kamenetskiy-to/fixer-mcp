package main

import (
	"strings"
	"testing"
)

func TestDefaultRolePrepromptUsesProviderNeutralOrchestrationRule(t *testing.T) {
	if !strings.Contains(defaultRolePreprompt, "current provider's built-in subagent") {
		t.Fatal("default role preprompt must use provider-neutral orchestration wording")
	}
	if strings.Contains(strings.ToLower(defaultRolePreprompt), "antigravity's built-in") {
		t.Fatal("default role preprompt must not hard-code Antigravity")
	}
	if !strings.Contains(defaultRolePreprompt, "Fixer MCP Netrunner waves") {
		t.Fatal("default role preprompt must route orchestration through Netrunner waves")
	}
}

func TestDefaultFixerRolePrepromptDropsWaveStatusRitual(t *testing.T) {
	text := defaultFixerRolePreprompt
	if strings.Contains(text, "рапортовать") || strings.Contains(text, "начале каждого") {
		t.Fatal("fixer preprompt must not require a wave-status ritual on every reply")
	}
	if !strings.Contains(text, "волна Fixer MCP Netrunner") {
		t.Fatal("fixer preprompt must still route delegation through Fixer MCP Netrunner waves")
	}
	if strings.Count(text, "—") > 0 {
		t.Fatal("fixer preprompt should model ordinary punctuation rather than em-dash cadence")
	}
}
