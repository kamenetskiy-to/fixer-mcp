package main

import (
	"crypto/sha256"
	"fmt"
	"os"
	"runtime/debug"
	"strings"
	"time"
)

const fixerMCPBuildIDEnv = "FIXER_MCP_BUILD_ID"

// Linker-injected provenance. Installed release payloads are built with
// -ldflags "-X main.fixerMCPReleaseVersion=<ver> -X main.fixerMCPSourceRevision=<sha>";
// a plain `go build` in a checkout has neither and falls back to the embedded
// VCS build info. Both paths must work for installed payload and checkout.
var (
	fixerMCPReleaseVersion = ""
	fixerMCPSourceRevision = ""
)

var (
	mcpRunningBuildID   = detectMCPRunningBuildID()
	mcpProcessStartedAt = time.Now().UTC()
	mcpProcessIdentity  = fmt.Sprintf("pid:%d:start:%d", os.Getpid(), mcpProcessStartedAt.UnixNano())
)

// mcpBuildRelease reports the release label of the running binary.
func mcpBuildRelease() string {
	if configured := strings.TrimSpace(fixerMCPReleaseVersion); configured != "" {
		return configured
	}
	if revision := strings.TrimSpace(fixerMCPSourceRevision); revision != "" {
		return "dev"
	}
	if info, ok := debug.ReadBuildInfo(); ok {
		for _, setting := range info.Settings {
			if setting.Key == "vcs.revision" && strings.TrimSpace(setting.Value) != "" {
				return "dev"
			}
		}
	}
	return "unknown"
}

// mcpBuildSource reports where the running binary came from: an installed
// release payload (ldflags provenance) or a source checkout (embedded VCS
// build info). Never trusts caller claims; this is the process's own record.
func mcpBuildSource() string {
	if configured := strings.TrimSpace(fixerMCPSourceRevision); configured != "" {
		return "installed_payload:" + configured
	}
	if info, ok := debug.ReadBuildInfo(); ok {
		revision := ""
		modified := false
		for _, setting := range info.Settings {
			switch setting.Key {
			case "vcs.revision":
				revision = strings.TrimSpace(setting.Value)
			case "vcs.modified":
				modified = strings.TrimSpace(setting.Value) == "true"
			}
		}
		if revision != "" {
			source := "checkout:" + revision
			if modified {
				source += "-modified"
			}
			return source
		}
	}
	return "unknown"
}

func detectMCPRunningBuildID() string {
	if configured := strings.TrimSpace(os.Getenv(fixerMCPBuildIDEnv)); configured != "" {
		return configured
	}
	executable, err := os.Executable()
	if err != nil {
		return "unresolved-executable"
	}
	payload, err := os.ReadFile(executable)
	if err != nil {
		return "unresolved-executable"
	}
	digest := sha256.Sum256(payload)
	return fmt.Sprintf("sha256:%x", digest[:])
}
