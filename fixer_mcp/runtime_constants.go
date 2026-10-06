package main

var validSessionStatuses = map[string]struct{}{
	"pending":     {},
	"in_progress": {},
	"review":      {},
	"completed":   {},
}

const (
	forcedMcpServerName         = "fixer_mcp"
	philologistsProjectMarker   = "philologists"
	researchQueryMcpName        = "research_query_mcp"
	telegramNotifyMcpName       = "telegram_notify"
	defaultTelegramAPIBaseURL   = "https://api.telegram.org"
	explicitLaunchDefaultWait   = 7200
	explicitLaunchMaxWait       = 21600
	explicitLaunchDefaultPoll   = 5
	explicitLaunchMaxPoll       = 60
	defaultCliBackend           = "codex"
	defaultCliModel             = "gpt-6.1-sol"
	defaultCliReasoning         = "high"
	defaultCommandCodeCliModel  = "commandcode/zai-org/glm-5.3-flash"
	defaultDroidCliModel        = "kimi-k2.6"
	defaultDroidCliReasoning    = "high"
	defaultAntigravityReasoning = "high"
	defaultKimiCodeCliModel     = "kimi-k3-256k"
	defaultKimiCodeReasoning    = "default"
	defaultJunieCliReasoning    = "default"
	defaultGrokCliModel         = "grok-4.7"
	defaultGrokCliReasoning     = "high"
	reworkRepairThreshold       = 2
	workerStatusRunning         = "running"
	workerStatusStopped         = "stopped"
	workerStatusExited          = "exited"
)

var supportedCliBackends = map[string]struct{}{
	"antigravity": {},
	"claude":      {},
	"codex":       {},
	"commandcode": {},
	"droid":       {},
	"grok":        {},
	"junie":       {},
	"kimi-code":   {},
	"pi":          {},
}

// No alias for "pi": the binary, the catalog provider id, the Python package
// name and the DB-backed backend name are all already the same two letters.
var cliBackendAliases = map[string]string{
	"agy":  "antigravity",
	"cmd":  "commandcode",
	"cmdc": "commandcode",
}

var droidLegacyModelAliases = map[string]string{
	"kimi":                      defaultDroidCliModel,
	"kimi k2.6":                 defaultDroidCliModel,
	"kimi-k2.6":                 defaultDroidCliModel,
	"kimi k2.6 [kimi]":          defaultDroidCliModel,
	"custom:kimi-k2.6-[kimi]-0": defaultDroidCliModel,
	"kimi-k2.7-code":            "kimi-k2.7-code",
	"kimi-k2.7":                 "kimi-k2.7-code",
	"kimi k2.7 code":            "kimi-k2.7-code",
	"glm-5.1":                   "glm-5.1",
	"z.ai glm-5.1":              "glm-5.1",
	"z.ai glm 5.1":              "glm-5.1",
	"custom:glm-5.1-[z.ai]-0":   "glm-5.1",
}

var supportedDroidCliModels = map[string]struct{}{
	defaultDroidCliModel: {},
	"kimi-k2.7-code":     {},
	"glm-5.1":            {},
}

var supportedKimiCodeCliModels = map[string]struct{}{
	"kimi-k2.7-code":           {},
	"kimi-k2.7-code-highspeed": {},
	"kimi-k3":                  {},
	defaultKimiCodeCliModel:    {},
}

var supportedGrokCliModels = map[string]struct{}{
	defaultGrokCliModel: {},
	"grok-4.6":          {},
	"grok-4.5":          {},
}

func handsProviderConfig(provider string) (model string, reasoning string, ok bool) {
	switch provider {
	case "pi":
		return "mimo-v2.6-pro", "high", true
	case "codex":
		return "gpt-6.1-sol", "high", true
	case "grok":
		return "grok-4.7", "high", true
	case "antigravity", "agy":
		return "gemini-3.8-flash", "high", true
	default:
		return "", "", false
	}
}
