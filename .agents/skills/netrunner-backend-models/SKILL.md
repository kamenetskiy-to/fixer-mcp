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
| Base harness (Pi) | `pi + opencode-go + deepseek-v4.1-flash + high` | The Architect's base agent harness: one CLI over the OpenAI, OpenCode Go and CommandCode subscriptions. On this model the only sendable levels are `high` and `max`; `low`, `medium` and `xhigh` are declared unsupported by the model's own `thinkingLevelMap`, and `pi --thinking low` clamps silently to `high` (the adapter's guard rejects an undeclared level loudly instead). Never advertise `ultra` for any Pi model. |
| Simple / small / ordinary implementation | `codex + gpt-5.6-luna + high` | OpenAI subscription baseline. Use another route only when explicitly selected. |
| Complex implementation or research | `codex + gpt-5.6-luna + high` | OpenAI subscription baseline unless the Architect explicitly selects another model. |
| Complex Claude-native task | `claude + opus + high` | Use for tasks where Claude's native workflow is the better fit. |
| Complex Kimi-native or very large-context task | `kimi-code + kimi-k3` | Use `kimi-k3-256k` when the task specifically needs the larger context; native Kimi has no per-invocation effort flag. |
| Browser/UI or Gemini-suitable fallback | `antigravity + Gemini 3.7 Flash + medium` | New Agy default; Antigravity encodes reasoning in the model name. |
| Speculative/free breadth | `commandcode + commandcode/minimaxai/minimax-m3 + high` | Optional parallel hypothesis/research worker only; latency and MiniMax endpoint load remain unproven. |

## Capability inventory

- `codex`: models `gpt-5.6-sol`, `gpt-5.6-terra`, `gpt-5.6-luna`, `opencode-go/glm-5.3-flash`, and the remaining explicit OpenCode Go/CommandCode catalog entries.
- `commandcode`: verified live inventory includes `zai-org/glm-5.3-flash`, `meta/muse-spark-1.2-contributor`, `deepseek/deepseek-v4-flash`, `deepseek/deepseek-v4-flash-vision-exp`, `deepseek/deepseek-v4-pro`, `moonshotai/kimi-k3`, `qwen/qwen3.8-max`, `qwen/qwen3.8-27b`, `minimaxai/minimax-m3`, and `gpt-5.6-sol`.
- `claude`: models `sonnet` (Sonnet 5) and `opus` (Opus 5); effort `low`, `medium`, `high`, `xhigh`, `max`; default `high`.
- `kimi-code`: models `kimi-k2.7-code`, `kimi-k2.7-code-highspeed`, `kimi-k3`, `kimi-k3-256k`; native CLI selection is via `-m`. Per-invocation thinking effort is not exposed by the current native adapter, so reasoning remains `default`.
- `antigravity`: models `Gemini 3.7 Flash` (default, `medium`), `Gemini 3.6 Flash`, `Gemini 3.1 Pro`, `Claude Sonnet 4.6 (Thinking)`, `Claude Opus 4.6 (Thinking)`; reasoning `low`, `medium`, `high` for Flash models, with `medium` unavailable on Gemini 3.1 Pro. Claude-in-Antigravity thinking is encoded in the model and has no separate effort option.
- `pi`: providers `commandcode`, `google`, `kimi-coding`, `openai-codex`, `opencode-go`, `openrouter` (452 models on this machine at the last probe). Selection is `--provider <name>` plus `--model <pattern>`, where the pattern accepts `provider/id` and an optional `:<thinking>` suffix. The CLI accepts thinking levels `off`, `minimal`, `low`, `medium`, `high`, `xhigh`, `max`, but each model clamps to the levels its own `thinkingLevelMap` declares, so check the model before promising a level. Headless modes are `--print`, `--mode json`, and `--mode rpc`; sessions are JSONL files handled with `--session`, `--session-id`, `--continue`, `--fork`, `--session-dir`, `--no-session`. There is no MCP in Pi core: MCP arrives through the installed `pi-mcp-adapter` extension, which exposes one `mcp` proxy tool whose calls address tools as `<server>_<tool>`; scope it with `--mcp-config <file>`. Project-local skills require `--approve`. Run `python3 scripts/pi_probe.py` for the live capability matrix before a Pi wave.

## Hard bans and cautions

- Claude Fable remains banned.
- Keep `codex + gpt-5.6-luna + high` as the ordinary-worker default until the Architect explicitly changes the route.
- Muse Spark Contributor via OpenCode Go remains an explicit non-default option; do not silently treat allowance availability as execution proof.
- MiniMax M3 remains optional speculative breadth until endpoint latency and load are measured.
- Do not claim a reasoning control exists unless the adapter actually passes it.
- Pi reads `~/.pi/agent/models.json` as a full override of the fetched model
  catalog: a locally declared model that omits `thinkingLevelMap` silently
  clamps `xhigh`/`max` to `high`. Every locally declared Pi model must carry the
  map (for `opencode-go/deepseek-v4.1-flash`: `high` and `max` sendable,
  everything else clamped). Verify with `python3 scripts/pi_probe.py` before a
  Pi wave; the probe prints the override and the fetched catalog side by side.
- Re-check `check-my-limits` between waves when you are choosing the provider/model yourself; provider availability changes faster than project docs.
