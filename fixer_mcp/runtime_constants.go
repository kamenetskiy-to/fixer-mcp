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
	defaultDeclaredWriteScope   = `["."]`
	defaultWriteScopePath       = "."
	defaultCliBackend           = "codex"
	defaultCliModel             = "gpt-5.6-luna"
	defaultCliReasoning         = "high"
	defaultCommandCodeCliModel  = "commandcode/deepseek/deepseek-v4-flash"
	defaultDroidCliModel        = "kimi-k2.6"
	defaultDroidCliReasoning    = "high"
	defaultAntigravityReasoning = "default"
	defaultKimiCodeCliModel     = "kimi-k3-256k"
	defaultKimiCodeReasoning    = "default"
	defaultJunieCliReasoning    = "default"
	defaultGrokCliModel         = "grok-4.6"
	defaultGrokCliReasoning     = "default"
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
}

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
	"grok-4.5":          {},
}

func handsProviderConfig(provider string) (model string, reasoning string, ok bool) {
	switch provider {
	case "codex":
		return "gpt-5.6-luna", "high", true
	case "commandcode":
		return defaultCommandCodeCliModel, "high", true
	case "claude":
		return "sonnet", "high", true
	case "kimi-code":
		return "kimi-k3-256k", "default", true
	case "antigravity":
		return "Gemini 3.7 Flash", "medium", true
	case "grok":
		return defaultGrokCliModel, defaultGrokCliReasoning, true
	default:
		return "", "", false
	}
}
