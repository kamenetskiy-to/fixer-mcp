export const ALWAYS_ON_MCP_SERVER = "grand_entity_mcp";

export function normalizeAllowlist(allowlist) {
  if (allowlist == null) return null;
  const uniq = [...new Set(allowlist.map((s) => String(s).trim()).filter(Boolean))];
  uniq.sort();
  return uniq;
}

export function stableStringify(obj) {
  if (obj == null) return JSON.stringify(obj);
  if (Array.isArray(obj)) return `[${obj.map(stableStringify).join(",")}]`;
  if (typeof obj === "object") {
    const keys = Object.keys(obj).sort();
    const entries = keys.map((k) => `${JSON.stringify(k)}:${stableStringify(obj[k])}`);
    return `{${entries.join(",")}}`;
  }
  return JSON.stringify(obj);
}

export function normalizeMcpServerConfigs(raw) {
  if (!raw || typeof raw !== "object" || Array.isArray(raw)) return null;
  const out = {};
  for (const [nameRaw, cfgRaw] of Object.entries(raw)) {
    const name = String(nameRaw ?? "").trim();
    if (!name) continue;
    if (!cfgRaw || typeof cfgRaw !== "object" || Array.isArray(cfgRaw)) continue;

    const cfg = {};

    const enabledTools = cfgRaw.enabled_tools ?? cfgRaw.enabledTools ?? null;
    if (Array.isArray(enabledTools)) {
      const tools = enabledTools
        .filter((t) => typeof t === "string")
        .map((t) => t.trim())
        .filter(Boolean);
      cfg.enabled_tools = tools;
    }

    const disabledTools = cfgRaw.disabled_tools ?? cfgRaw.disabledTools ?? null;
    if (Array.isArray(disabledTools)) {
      const tools = disabledTools
        .filter((t) => typeof t === "string")
        .map((t) => t.trim())
        .filter(Boolean);
      cfg.disabled_tools = tools;
    }

    const env = cfgRaw.env ?? null;
    if (env && typeof env === "object" && !Array.isArray(env)) {
      const envOut = {};
      for (const [kRaw, vRaw] of Object.entries(env)) {
        const k = String(kRaw ?? "").trim();
        if (!k) continue;
        if (typeof vRaw !== "string") continue;
        const v = vRaw.trim();
        if (!v) continue;
        envOut[k] = v;
      }
      if (Object.keys(envOut).length > 0) cfg.env = envOut;
    }

    if (Object.keys(cfg).length > 0) out[name] = cfg;
  }
  return Object.keys(out).length > 0 ? out : null;
}

export function ensureAlwaysOnAllowlist(allowlist) {
  const normalized = normalizeAllowlist(allowlist);
  if (normalized == null) return [ALWAYS_ON_MCP_SERVER];
  if (!normalized.includes(ALWAYS_ON_MCP_SERVER)) normalized.push(ALWAYS_ON_MCP_SERVER);
  normalized.sort();
  return normalized;
}

export function allowlistKey(allowlist, mcpServerConfigs) {
  const normalized = normalizeAllowlist(allowlist);
  if (normalized == null) return null;
  const cfg = normalizeMcpServerConfigs(mcpServerConfigs);
  return stableStringify({ allowlist: normalized, mcpServerConfigs: cfg ?? {} });
}

export function mcpServerConfigsFromBody(body) {
  const raw = body?.mcpServerConfigs ?? body?.mcp_server_configs ?? null;
  return normalizeMcpServerConfigs(raw);
}

