package main

import (
	"crypto/sha256"
	"fmt"
	"os"
	"path/filepath"
	"regexp"
	"strconv"
	"strings"
	"time"
)

// Runtime identity and health reporting.
//
// Every connected-runtime health call must report the ACTUAL running process
// identity of the caller (immutable build hash, PID, start time, release,
// provenance/source, schema era and absolute database identity), separately
// from the required/confirmed identities stored in mcp_binary_state. The
// stored row may still describe a long-dead process (a stale Sep-18 build PID
// was once returned as if it were the caller); it is evidence about the past,
// never proof of what is running now.
//
// Epoch 0 with no stored row is not "nothing is running": it means no restart
// bookkeeping has been recorded yet, while the caller reported here is a live
// process by construction.

const (
	schemaEraCurrent  = "current-1.0.11"
	schemaEraStale109 = "stale-1.0.9"
	schemaEraEmpty    = "empty"
	schemaEraUnknown  = "unknown"

	dbAuthorityHostCanonical    = "host_canonical"
	dbAuthorityExplicitOverride = "explicit_override"
)

// RuntimeProcessIdentity is the immutable identity of the process that
// answers the health call right now.
type RuntimeProcessIdentity struct {
	ProcessId         int    `json:"process_id"`
	ProcessStart      string `json:"process_start"`
	BuildId           string `json:"build_id"`
	Release           string `json:"release"`
	Source            string `json:"source"`
	SchemaEra         string `json:"schema_era"`
	SchemaFingerprint string `json:"schema_fingerprint"`
	DBPath            string `json:"db_path"`
	DBFingerprint     string `json:"db_fingerprint"`
	DBAuthority       string `json:"db_authority"`
}

var processIdentityPIDPattern = regexp.MustCompile(`^pid:(\d+):start:(\d+)$`)

// parseProcessIdentityPID extracts the exact recorded PID from a persisted
// process identity string. It only parses; it never probes or signals.
func parseProcessIdentityPID(identity string) (int, bool) {
	match := processIdentityPIDPattern.FindStringSubmatch(strings.TrimSpace(identity))
	if match == nil {
		return 0, false
	}
	pid, err := strconv.Atoi(match[1])
	if err != nil || pid <= 0 {
		return 0, false
	}
	return pid, true
}

// absoluteFixerDBPath reports the absolute, alias-normalized database path the
// process is connected to.
func absoluteFixerDBPath() string {
	raw := resolveFixerDBPath()
	abs, err := filepath.Abs(raw)
	if err != nil {
		return raw
	}
	if resolved, resolveErr := filepath.EvalSymlinks(abs); resolveErr == nil {
		return resolved
	}
	return abs
}

// fixerDBAuthority reports whether the connected database is the host
// canonical state database or an intentional FIXER_DB_PATH override (tests,
// dev and probes). Overrides are respected, never merged away.
func fixerDBAuthority() string {
	if strings.TrimSpace(os.Getenv(fixerDBPathEnv)) != "" {
		return dbAuthorityExplicitOverride
	}
	return dbAuthorityHostCanonical
}

// schemaFingerprint digests the exact schema shape of the connected database.
func schemaFingerprint() string {
	rows, err := db.Query(`SELECT COALESCE(name, ''), COALESCE(sql, '') FROM sqlite_master ORDER BY type, name`)
	if err != nil {
		return "unavailable"
	}
	defer func() { _ = rows.Close() }()
	digest := sha256.New()
	for rows.Next() {
		var name, ddl string
		if err := rows.Scan(&name, &ddl); err != nil {
			return "unavailable"
		}
		_, _ = digest.Write([]byte(name))
		_, _ = digest.Write([]byte{0})
		_, _ = digest.Write([]byte(ddl))
		_, _ = digest.Write([]byte{0})
	}
	if rows.Err() != nil {
		return "unavailable"
	}
	return fmt.Sprintf("sha256:%x", digest.Sum(nil))
}

// dbDataFingerprint digests a stable content summary of the core lifecycle
// tables so two databases can be compared for divergence without copying,
// merging or rewriting either one.
func dbDataFingerprint() string {
	digest := sha256.New()
	for _, table := range []string{"project", "session", "backlog_item", "project_doc", "hands_instruction"} {
		var count int
		countErr := db.QueryRow(`SELECT COUNT(*) FROM ` + table).Scan(&count)
		if countErr != nil {
			_, _ = fmt.Fprintf(digest, "%s:unavailable;", table)
			continue
		}
		_, _ = fmt.Fprintf(digest, "%s:%d;", table, count)
	}
	return fmt.Sprintf("sha256:%x", digest.Sum(nil))
}

// classifySchemaEra distinguishes the stale pre-1.0.11 (1.0.9-era) schema from
// the current 1.0.11+ schema so stale-schema errors never masquerade as
// current-version failures.
func classifySchemaEra() string {
	if !dbTableExists("mcp_binary_state") {
		var tables int
		if err := db.QueryRow(`SELECT COUNT(*) FROM sqlite_master WHERE type = 'table'`).Scan(&tables); err != nil {
			return schemaEraUnknown
		}
		if tables == 0 {
			return schemaEraEmpty
		}
		return schemaEraStale109
	}
	// The immutable build/process identity columns were introduced with the
	// post-1.0.9 restart bookkeeping; their absence pins the DB to the old era.
	if !dbTableHasColumn("mcp_binary_state", "running_build_id") ||
		!dbTableHasColumn("mcp_binary_state", "running_process_identity") {
		return schemaEraStale109
	}
	return schemaEraCurrent
}

// collectRuntimeProcessIdentity reports the caller's actual identity on every
// health call.
func collectRuntimeProcessIdentity() RuntimeProcessIdentity {
	return RuntimeProcessIdentity{
		ProcessId:         os.Getpid(),
		ProcessStart:      mcpProcessStartedAt.UTC().Format(time.RFC3339Nano),
		BuildId:           mcpRunningBuildID,
		Release:           mcpBuildRelease(),
		Source:            mcpBuildSource(),
		SchemaEra:         classifySchemaEra(),
		SchemaFingerprint: schemaFingerprint(),
		DBPath:            absoluteFixerDBPath(),
		DBFingerprint:     dbDataFingerprint(),
		DBAuthority:       fixerDBAuthority(),
	}
}

// schemaEraAwareRestartStateError makes stale 1.0.9-era schema failures
// unmistakably different from current 1.0.11 schema failures, with an
// actionable reconnect instruction instead of a generic SQL error.
func schemaEraAwareRestartStateError(err error) error {
	if err == nil {
		return nil
	}
	message := strings.ToLower(err.Error())
	staleMarker := strings.Contains(message, "no such column") ||
		strings.Contains(message, "no such table") ||
		strings.Contains(message, "malformed")
	era := classifySchemaEra()
	switch {
	case staleMarker && era == schemaEraStale109:
		return fmt.Errorf(
			"stale 1.0.9-era schema at %s cannot serve restart/health state (missing post-1.0.11 identity columns): %v; "+
				"reconnect the MCP transport to a freshly installed current binary and rerun migrations (fail closed until then)",
			absoluteFixerDBPath(), err)
	case staleMarker && era == schemaEraCurrent:
		return fmt.Errorf("current 1.0.11 schema error at %s: %v", absoluteFixerDBPath(), err)
	default:
		return fmt.Errorf("restart/health state error at %s (schema era %s): %v", absoluteFixerDBPath(), era, err)
	}
}
