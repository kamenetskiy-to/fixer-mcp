import fs from "node:fs";
import path from "node:path";

const PROFILE_FILE_CANDIDATES = [path.join(".codex", "MCPs.toml"), "MCPs.toml"];
const DEFAULT_PROFILE_NAME = "dev";

const CREDENTIAL_REQUIRED_DEFAULTS = new Set([
  "postgres",
  "clickhouse",
  "sqlite",
  "figma",
  "firebase",
]);

const REQUIRED_ENV_BY_SERVER = {
  postgres: ["POSTGRES_URL"],
  clickhouse: ["CLICKHOUSE_URL"],
  sqlite: ["SQLITE_DB_PATH"],
  figma: ["FIGMA_TOKEN"],
  firebase: ["FIREBASE_PROJECT_ID", "FIREBASE_CLIENT_EMAIL", "FIREBASE_PRIVATE_KEY"],
};

function normalizeProfileName(raw) {
  if (typeof raw !== "string") return null;
  const value = raw.trim();
  return value ? value : null;
}

function stripComments(line) {
  let quote = null;
  let escaped = false;
  for (let i = 0; i < line.length; i++) {
    const ch = line[i];
    if (escaped) {
      escaped = false;
      continue;
    }
    if (ch === "\\") {
      escaped = true;
      continue;
    }
    if (quote) {
      if (ch === quote) quote = null;
      continue;
    }
    if (ch === '"' || ch === "'") {
      quote = ch;
      continue;
    }
    if (ch === "#") return line.slice(0, i);
  }
  return line;
}

function unquote(raw) {
  const value = raw.trim();
  if (value.length < 2) return value;
  const first = value[0];
  const last = value[value.length - 1];
  if ((first === '"' && last === '"') || (first === "'" && last === "'")) {
    const inner = value.slice(1, -1);
    if (first === '"') {
      return inner
        .replace(/\\n/g, "\n")
        .replace(/\\t/g, "\t")
        .replace(/\\"/g, '"')
        .replace(/\\\\/g, "\\");
    }
    return inner.replace(/\\'/g, "'");
  }
  return value;
}

function splitCommaAware(raw) {
  const out = [];
  let buf = "";
  let quote = null;
  let escaped = false;
  let depth = 0;

  for (let i = 0; i < raw.length; i++) {
    const ch = raw[i];
    if (escaped) {
      buf += ch;
      escaped = false;
      continue;
    }
    if (ch === "\\") {
      buf += ch;
      escaped = true;
      continue;
    }
    if (quote) {
      buf += ch;
      if (ch === quote) quote = null;
      continue;
    }
    if (ch === '"' || ch === "'") {
      buf += ch;
      quote = ch;
      continue;
    }
    if (ch === "[") {
      depth += 1;
      buf += ch;
      continue;
    }
    if (ch === "]") {
      depth = Math.max(0, depth - 1);
      buf += ch;
      continue;
    }
    if (ch === "," && depth === 0) {
      out.push(buf.trim());
      buf = "";
      continue;
    }
    buf += ch;
  }
  if (buf.trim()) out.push(buf.trim());
  return out;
}

function parseTomlValue(rawValue) {
  const value = rawValue.trim();
  if (!value) return null;

  if (value[0] === "[" && value[value.length - 1] === "]") {
    const inner = value.slice(1, -1).trim();
    if (!inner) return [];
    return splitCommaAware(inner).map((v) => parseTomlValue(v));
  }

  if (value === "true") return true;
  if (value === "false") return false;

  if (/^[+-]?\d+$/.test(value)) return Number.parseInt(value, 10);

  if (
    (value[0] === '"' && value[value.length - 1] === '"') ||
    (value[0] === "'" && value[value.length - 1] === "'")
  ) {
    return unquote(value);
  }

  // Bare values are treated as strings for this narrow parser.
  return value;
}

function assignNested(root, dottedPath, value) {
  let cursor = root;
  for (let i = 0; i < dottedPath.length; i++) {
    const part = dottedPath[i];
    if (!part) continue;
    if (i === dottedPath.length - 1) {
      cursor[part] = value;
      return;
    }
    if (!cursor[part] || typeof cursor[part] !== "object" || Array.isArray(cursor[part])) {
      cursor[part] = {};
    }
    cursor = cursor[part];
  }
}

export function parseMcpProfilesToml(text) {
  const root = {};
  let tablePath = [];
  const lines = String(text ?? "").split(/\r?\n/);

  for (const rawLine of lines) {
    const withoutComments = stripComments(rawLine);
    const line = withoutComments.trim();
    if (!line) continue;

    if (line.startsWith("[") && line.endsWith("]")) {
      const inner = line.slice(1, -1).trim();
      tablePath = inner
        .split(".")
        .map((s) => s.trim())
        .filter(Boolean)
        .map((s) => unquote(s));
      if (tablePath.length > 0) assignNested(root, tablePath, {});
      continue;
    }

    const eqIndex = line.indexOf("=");
    if (eqIndex <= 0) continue;
    const key = line.slice(0, eqIndex).trim();
    const valueRaw = line.slice(eqIndex + 1).trim();
    if (!key) continue;
    const dottedKey = key
      .split(".")
      .map((s) => s.trim())
      .filter(Boolean)
      .map((s) => unquote(s));
    assignNested(root, [...tablePath, ...dottedKey], parseTomlValue(valueRaw));
  }

  return root;
}

function normalizeStringMap(raw) {
  if (!raw || typeof raw !== "object" || Array.isArray(raw)) return {};
  const out = {};
  for (const [kRaw, vRaw] of Object.entries(raw)) {
    const key = String(kRaw ?? "").trim();
    if (!key) continue;
    if (typeof vRaw !== "string") continue;
    out[key] = vRaw;
  }
  return out;
}

function normalizeStringArray(raw) {
  if (!Array.isArray(raw)) return [];
  return raw
    .filter((v) => typeof v === "string")
    .map((v) => v.trim())
    .filter(Boolean);
}

function normalizeServerProfile(raw, baseDir) {
  if (!raw || typeof raw !== "object" || Array.isArray(raw)) return null;

  const requiredRaw =
    raw.required ??
    raw.requires_credentials ??
    raw.requiresCredentials ??
    raw.credentials_required ??
    null;
  const required =
    typeof requiredRaw === "boolean" ? requiredRaw : null;

  const requiredEnv = normalizeStringArray(
    raw.credential_env ?? raw.required_env ?? raw.requiredEnv ?? raw.credentialEnv ?? [],
  );

  const env = normalizeStringMap(raw.env);

  const configPathRaw =
    raw.config_path ?? raw.configPath ?? raw.path ?? null;
  let configPath = null;
  if (typeof configPathRaw === "string" && configPathRaw.trim()) {
    const rawValue = configPathRaw.trim();
    configPath = path.isAbsolute(rawValue) ? rawValue : path.resolve(baseDir, rawValue);
  }

  return {
    required,
    requiredEnv,
    env,
    configPath,
  };
}

function normalizeProfilesDocument(rawDoc, filePath) {
  const baseDir = filePath ? path.dirname(filePath) : process.cwd();
  const root = rawDoc && typeof rawDoc === "object" && !Array.isArray(rawDoc) ? rawDoc : {};
  const rawProfiles = root.profiles;
  const profiles = {};

  if (rawProfiles && typeof rawProfiles === "object" && !Array.isArray(rawProfiles)) {
    for (const [profileRaw, profileValue] of Object.entries(rawProfiles)) {
      const profile = normalizeProfileName(profileRaw);
      if (!profile) continue;
      if (!profileValue || typeof profileValue !== "object" || Array.isArray(profileValue)) continue;

      const serverRoot =
        profileValue.servers &&
            typeof profileValue.servers === "object" &&
            !Array.isArray(profileValue.servers)
          ? profileValue.servers
          : profileValue;

      const servers = {};
      for (const [serverNameRaw, serverValue] of Object.entries(serverRoot)) {
        const serverName = String(serverNameRaw ?? "").trim();
        if (!serverName) continue;
        const normalized = normalizeServerProfile(serverValue, baseDir);
        if (!normalized) continue;
        servers[serverName] = normalized;
      }

      profiles[profile] = { servers };
    }
  }

  const activeProfile = normalizeProfileName(root.active_profile ?? root.activeProfile);
  return {
    activeProfile,
    profiles,
  };
}

function findFileFromCwd(cwd) {
  const start = typeof cwd === "string" && cwd.trim() ? path.resolve(cwd.trim()) : process.cwd();

  let current = start;
  while (true) {
    for (const candidate of PROFILE_FILE_CANDIDATES) {
      const filePath = path.join(current, candidate);
      if (fs.existsSync(filePath) && fs.statSync(filePath).isFile()) {
        return filePath;
      }
    }
    const parent = path.dirname(current);
    if (parent === current) break;
    current = parent;
  }

  return null;
}

function pickProfileName({ requestedProfile, envProfile, activeProfile, availableProfiles }) {
  const availableSet = new Set(availableProfiles);

  const req = normalizeProfileName(requestedProfile);
  if (req && availableSet.has(req)) return { profile: req, source: "request", profileError: null };
  if (req && !availableSet.has(req)) {
    return {
      profile: null,
      source: "request",
      profileError: `Requested MCP profile "${req}" is not defined.`,
    };
  }

  const env = normalizeProfileName(envProfile);
  if (env && availableSet.has(env)) return { profile: env, source: "env", profileError: null };

  if (activeProfile && availableSet.has(activeProfile)) {
    return { profile: activeProfile, source: "file", profileError: null };
  }

  if (availableSet.has(DEFAULT_PROFILE_NAME)) {
    return { profile: DEFAULT_PROFILE_NAME, source: "default", profileError: null };
  }

  if (availableProfiles.length > 0) {
    return { profile: availableProfiles[0], source: "default", profileError: null };
  }

  return { profile: null, source: "default", profileError: null };
}

function resolveTemplateValue(value) {
  const raw = String(value ?? "").trim();
  if (!raw) return "";

  let match = raw.match(/^\$\{([A-Za-z_][A-Za-z0-9_]*):-([^}]*)\}$/);
  if (match) {
    const envValue = process.env[match[1]];
    return typeof envValue === "string" && envValue.trim() ? envValue : match[2];
  }

  match = raw.match(/^\$\{([A-Za-z_][A-Za-z0-9_]*)\}$/);
  if (match) {
    return process.env[match[1]] ?? "";
  }

  match = raw.match(/^\$([A-Za-z_][A-Za-z0-9_]*)$/);
  if (match) {
    return process.env[match[1]] ?? "";
  }

  return raw;
}

function summarizeBlockedReason({ hasProfileEntry, missingVars, missingConfigPath, requiresCredentials }) {
  if (missingConfigPath) {
    return `Referenced config path is missing: ${missingConfigPath}`;
  }
  if (missingVars.length > 0) {
    return `Missing required credential env vars: ${missingVars.join(", ")}`;
  }
  if (requiresCredentials && !hasProfileEntry) {
    return "Required MCP credentials are not configured for the active profile.";
  }
  return null;
}

function mergeProfileEnvIntoConfigs(mcpServerConfigs, allowlist, serverEnvByName) {
  const out = mcpServerConfigs ? { ...mcpServerConfigs } : {};
  const selected = new Set(Array.isArray(allowlist) ? allowlist : []);
  for (const name of selected) {
    const profileEnv = serverEnvByName[name];
    if (!profileEnv || Object.keys(profileEnv).length === 0) continue;

    const existing = out[name];
    const existingEnv =
      existing && typeof existing === "object" && !Array.isArray(existing) && existing.env
        ? normalizeStringMap(existing.env)
        : {};

    out[name] = {
      ...(existing && typeof existing === "object" && !Array.isArray(existing) ? existing : {}),
      env: { ...profileEnv, ...existingEnv },
    };
  }
  return Object.keys(out).length > 0 ? out : null;
}

export function resolveMcpProfilesForRequest({
  cwd,
  requestedProfile,
  knownServerNames,
  allowlist,
  mcpServerConfigs,
}) {
  const configPath = findFileFromCwd(cwd);
  let parsed = {};
  if (configPath) {
    try {
      const raw = fs.readFileSync(configPath, "utf8");
      parsed = parseMcpProfilesToml(raw);
    } catch {
      parsed = {};
    }
  }

  const normalized = normalizeProfilesDocument(parsed, configPath);
  const availableProfiles = Object.keys(normalized.profiles).sort();
  const picked = pickProfileName({
    requestedProfile,
    envProfile: process.env.CODEX_MCP_PROFILE,
    activeProfile: normalized.activeProfile,
    availableProfiles,
  });

  const profileServers =
    picked.profile && normalized.profiles[picked.profile]
      ? normalized.profiles[picked.profile].servers
      : {};
  const profileServersByLower = {};
  for (const [name, value] of Object.entries(profileServers)) {
    profileServersByLower[name.toLowerCase()] = value;
  }

  const known = [...new Set((Array.isArray(knownServerNames) ? knownServerNames : []).map((n) => String(n).trim()).filter(Boolean))];
  const selected = [...new Set((Array.isArray(allowlist) ? allowlist : []).map((n) => String(n).trim()).filter(Boolean))];
  for (const name of selected) {
    if (!known.includes(name)) known.push(name);
  }
  known.sort();

  const servers = [];
  const serverEnvByName = {};

  for (const name of known) {
    const lower = name.toLowerCase();
    const profileEntry = profileServersByLower[lower] ?? null;

    const requiresCredentials =
      typeof profileEntry?.required === "boolean"
        ? profileEntry.required
        : CREDENTIAL_REQUIRED_DEFAULTS.has(lower);

    const requiredVars =
      Array.isArray(profileEntry?.requiredEnv) && profileEntry.requiredEnv.length > 0
        ? profileEntry.requiredEnv
        : REQUIRED_ENV_BY_SERVER[lower] ?? [];

    const envFromProfileRaw = normalizeStringMap(profileEntry?.env ?? {});
    const envFromProfile = {};
    for (const [k, v] of Object.entries(envFromProfileRaw)) {
      const resolved = resolveTemplateValue(v);
      if (resolved) envFromProfile[k] = resolved;
    }
    if (picked.profile) {
      envFromProfile.CODEX_MCP_ACTIVE_PROFILE = picked.profile;
    }

    const missingVars = [];
    for (const varName of requiredVars) {
      const byProfile = envFromProfile[varName];
      if (typeof byProfile === "string" && byProfile.trim()) continue;
      const byProcess = process.env[varName];
      if (typeof byProcess === "string" && byProcess.trim()) continue;
      missingVars.push(varName);
    }

    const configPathForServer = profileEntry?.configPath ?? null;
    const missingConfigPath =
      typeof configPathForServer === "string" &&
          configPathForServer.trim() &&
          !fs.existsSync(configPathForServer)
        ? configPathForServer
        : null;

    const reason = summarizeBlockedReason({
      hasProfileEntry: Boolean(profileEntry),
      missingVars,
      missingConfigPath,
      requiresCredentials,
    });

    const selectable = reason == null;
    const configured = reason == null;

    serverEnvByName[name] = envFromProfile;
    servers.push({
      name,
      profile: picked.profile,
      selectable,
      configured,
      requiresCredentials,
      hasProfileEntry: Boolean(profileEntry),
      reason,
      missingVars,
      configPath: configPathForServer,
    });
  }

  const allowSet = new Set(selected);
  const blockedSelected = servers
    .filter((s) => allowSet.has(s.name) && !s.selectable)
    .map((s) => ({ name: s.name, reason: s.reason, missingVars: s.missingVars }));

  const mergedMcpServerConfigs = mergeProfileEnvIntoConfigs(
    mcpServerConfigs,
    selected,
    serverEnvByName,
  );

  return {
    profile: picked.profile,
    profileSource: picked.source,
    profileError: picked.profileError,
    configPath,
    availableProfiles,
    servers,
    blockedSelected,
    mergedMcpServerConfigs,
  };
}
