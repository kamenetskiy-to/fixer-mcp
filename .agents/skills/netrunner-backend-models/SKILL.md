---
name: netrunner-backend-models
description: "Authoritative backend/model/reasoning routing table for Netrunner wave workers. Check quota via check-my-limits (cml) before choosing unless the Architect explicitly names the model; unavailable self-selected models are off-limits. Use for every wave launch backend/model decision."
---

# Netrunner Backend/Model Routing

Use this skill before every Netrunner wave launch to choose backend, model, and reasoning per worker.

## Rule 0: quota gate, unless the Architect already chose

If the Architect explicitly names a backend/model for the wave, honor that choice without running `check-my-limits` (`cml`) first unless the Architect asks you to verify availability. Do not override an explicit model request because of your own quota routing preference.

When you choose the backend/model yourself, run the executable `check-my-limits` before choosing and read the current provider windows. (`cml` is the Architect's interactive-shell alias for that executable.) A self-selected provider/model bucket that is exhausted, erroring, or burning paid credits is off-limits.

## Current routing

| Task class | Default route | Notes |
|---|---|---|
| Simple / small / ordinary implementation | `codex + opencode-go/deepseek-v4-flash + high` | Preferred route for the overwhelming majority of work. |
| Complex implementation or research | `codex + gpt-5.6-sol + high` | Use when deeper Codex reasoning is justified; `xhigh` only when explicitly needed. |
| Complex Claude-native task | `claude + opus + high` | Use for tasks where Claude's native workflow is the better fit. |
| Complex Kimi-native or very large-context task | `kimi-code + kimi-k3` | Use `kimi-k3-256k` when the task specifically needs the larger context; native Kimi has no per-invocation effort flag. |
| Browser/UI or Gemini-suitable fallback | `antigravity + Gemini 3.7 Flash + medium` | New Agy default; Antigravity encodes reasoning in the model name. |
| Experiment: Muse Spark | `codex + opencode-go/muse-spark-1.2 + medium` | EXPERIMENT ONLY: allowed solely with the Architect's explicit per-wave permission. Never self-select this route. |
| Experiment: Ox Alpha Free | `codex + opencode-go/ox-alpha-free + high` | EXPERIMENT ONLY: allowed solely with the Architect's explicit per-wave permission. Never self-select this route. |

## Capability inventory

- `codex`: models `gpt-5.6-sol`, `gpt-5.6-terra`, `gpt-5.6-luna`; reasoning `low`, `medium`, `high`, `xhigh`, `max`, `ultra`; `ultra` is rejected for `gpt-5.6-luna`. Experimental route: `opencode-go/muse-spark-1.2` (1M context, reasoning `minimal`…`xhigh`, default `medium`; `max`/`ultra` rejected) — only with the Architect's explicit permission per the routing table. Second experimental route: `opencode-go/ox-alpha-free` (256k context, reasoning `minimal`…`xhigh`, default `high`; codex launches it with `web_search="disabled"` because the upstream rejects the standalone web_search tool) — same Architect-only gate.
- `claude`: models `sonnet` (Sonnet 5) and `opus` (Opus 5); effort `low`, `medium`, `high`, `xhigh`, `max`; default `high`.
- `kimi-code`: models `kimi-k2.7-code`, `kimi-k2.7-code-highspeed`, `kimi-k3`, `kimi-k3-256k`; native CLI selection is via `-m`. Per-invocation thinking effort is not exposed by the current native adapter, so reasoning remains `default`.
- `antigravity`: models `Gemini 3.7 Flash` (default, `medium`), `Gemini 3.6 Flash`, `Gemini 3.1 Pro`, `Claude Sonnet 4.6 (Thinking)`, `Claude Opus 4.6 (Thinking)`; reasoning `low`, `medium`, `high` for Flash models, with `medium` unavailable on Gemini 3.1 Pro. Claude-in-Antigravity thinking is encoded in the model and has no separate effort option.

## Hard bans and cautions

- Claude Fable remains banned.
- Do not claim a reasoning control exists unless the adapter actually passes it.
- Re-check `check-my-limits` between waves when you are choosing the provider/model yourself; provider availability changes faster than project docs.
