package main

import (
	"context"
	"database/sql"
	"fmt"
	"log"
	"os"
	"strings"

	"github.com/modelcontextprotocol/go-sdk/mcp"
)

var validAssumableRoles = map[string]struct{}{
	"overseer":  {},
	"fixer":     {},
	"netrunner": {},
}

func isValidAssumableRole(role string) bool {
	_, ok := validAssumableRoles[role]
	return ok
}

func lockedRoleFromEnv() (string, error) {
	role := strings.ToLower(strings.TrimSpace(os.Getenv(fixerMcpLockedRoleEnv)))
	if role == "" {
		return "", nil
	}
	if !isValidAssumableRole(role) {
		return "", fmt.Errorf("%s must be one of overseer, fixer, netrunner; got %q", fixerMcpLockedRoleEnv, role)
	}
	return role, nil
}

var defaultRolePreprompts = map[string]string{
	"fixer":     defaultFixerRolePreprompt,
	"netrunner": defaultRolePreprompt,
	"overseer":  defaultRolePreprompt,
}

func bootstrapDefaultRoleAuthFromEnv() {
	if strings.TrimSpace(authorizedRole) != "" {
		return
	}
	defaultRole := strings.ToLower(strings.TrimSpace(os.Getenv(fixerMcpDefaultRoleEnv)))
	if defaultRole != "netrunner" && defaultRole != "fixer" {
		return
	}
	lockedRole, lockedRoleErr := lockedRoleFromEnv()
	if lockedRoleErr != nil {
		log.Printf("fixer_mcp env auth bootstrap skipped: invalid locked role: %v", lockedRoleErr)
		return
	}
	if defaultRole == "netrunner" {
		if lockedRole != "" && lockedRole != "netrunner" {
			log.Printf("fixer_mcp env auth bootstrap skipped: %s=%s is not netrunner", fixerMcpLockedRoleEnv, lockedRole)
			return
		}
	} else if strings.TrimSpace(os.Getenv(fixerMcpAutoAuthEnv)) != "1" ||
		lockedRole != "fixer" ||
		strings.TrimSpace(os.Getenv(fixerMcpToolProfileEnv)) != netrunnerGateProfile {
		log.Printf("fixer_mcp env auth bootstrap skipped: fixer auto-auth is restricted to the locked netrunner gate")
		return
	}
	normalizedCWD, err := normalizeProjectCWD(os.Getenv(fixerMcpDefaultCwdEnv))
	if err != nil {
		log.Printf("fixer_mcp env auth bootstrap skipped: invalid cwd: %v", err)
		return
	}

	var projId int
	err = db.QueryRow("SELECT id FROM project WHERE cwd = ?", normalizedCWD).Scan(&projId)
	if err == sql.ErrNoRows {
		log.Printf("fixer_mcp env auth bootstrap skipped: cwd not registered: %s", normalizedCWD)
		return
	}
	if err != nil {
		log.Printf("fixer_mcp env auth bootstrap skipped: project lookup error: %v", err)
		return
	}

	authorizedRole = defaultRole
	authorizedProjectId = projId
	authorizedSessionId = 0
	log.Printf("fixer_mcp env auth bootstrap applied for %s project_id=%d cwd=%s", defaultRole, projId, normalizedCWD)
}

// ensureNetrunnerGateProjectBinding lazily (re)binds the wait-only netrunner
// gate to the project named by env when the one-time startup bootstrap did not
// apply. A long-lived gate process can outlive a project registration or env
// refresh, leaving the wait handlers without an auth context even though the
// primary fixer_mcp process has already been authenticated via assume_role.
// Binding only ever happens inside the locked netrunner_gate profile; every
// other context keeps the original access-denied behavior.
func ensureNetrunnerGateProjectBinding() error {
	if authorizedRole == "fixer" && authorizedProjectId > 0 {
		return nil
	}
	if strings.TrimSpace(os.Getenv(fixerMcpToolProfileEnv)) != netrunnerGateProfile {
		return fmt.Errorf("access denied: requires project-bound fixer role")
	}
	bootstrapDefaultRoleAuthFromEnv()
	if authorizedRole != "fixer" || authorizedProjectId <= 0 {
		return fmt.Errorf("access denied: requires project-bound fixer role")
	}
	return nil
}

type AssumeRoleInput struct {
	Role  string `json:"role" jsonschema:"the role to assume: 'fixer', 'netrunner', or 'overseer'"`
	Cwd   string `json:"cwd,omitempty" jsonschema:"The absolute path to the project root directory. Not required for overseer."`
	Token string `json:"token,omitempty" jsonschema:"secret token for fixer or overseer"`
}

type AssumeRoleOutput struct {
	Status        string `json:"status" jsonschema:"status of authentication"`
	Message       string `json:"message" jsonschema:"response message"`
	RolePreprompt string `json:"role_preprompt,omitempty" jsonschema:"Optional system preprompt for this role"`
}

func AssumeRole(ctx context.Context, req *mcp.CallToolRequest, input AssumeRoleInput) (*mcp.CallToolResult, AssumeRoleOutput, error) {
	log.Printf("assume_role called with role: %s, cwd: %s", input.Role, input.Cwd)
	requestedRole := strings.ToLower(strings.TrimSpace(input.Role))
	lockedRole, lockedRoleErr := lockedRoleFromEnv()
	if lockedRoleErr != nil {
		return &mcp.CallToolResult{IsError: true}, AssumeRoleOutput{
			Status:  "error",
			Message: fmt.Sprintf("Auth Error: invalid %s config: %v", fixerMcpLockedRoleEnv, lockedRoleErr),
		}, nil
	}
	if lockedRole != "" && requestedRole != lockedRole {
		return &mcp.CallToolResult{IsError: true}, AssumeRoleOutput{
			Status: "error",
			Message: fmt.Sprintf(
				"Auth Error: %s=%s locks this MCP server to role %q; assume_role(%q) is not allowed. Launch the correct role-specific fixer_mcp server or unset %s for legacy/dev mode.",
				fixerMcpLockedRoleEnv,
				lockedRole,
				lockedRole,
				requestedRole,
				fixerMcpLockedRoleEnv,
			),
		}, nil
	}
	authorizedSessionId = 0

	if requestedRole == "overseer" {
		if lockedRole != "overseer" && input.Token != "supersecret" {
			return &mcp.CallToolResult{IsError: true}, AssumeRoleOutput{Status: "error", Message: "invalid token"}, nil
		}
		authorizedRole = "overseer"
		authorizedProjectId = 0
		return nil, AssumeRoleOutput{Status: "success", Message: "Authenticated as Overseer. Global view granted.", RolePreprompt: getRolePreprompt("overseer")}, nil
	}

	if input.Cwd == "" {
		return &mcp.CallToolResult{IsError: true}, AssumeRoleOutput{Status: "error", Message: "CWD is required in the input arguments"}, nil
	}

	normalizedCWD, normalizeErr := normalizeProjectCWD(input.Cwd)
	if normalizeErr != nil {
		return &mcp.CallToolResult{IsError: true}, AssumeRoleOutput{Status: "error", Message: fmt.Sprintf("Auth Error: %v", normalizeErr)}, nil
	}

	projId, _, _, err := findProjectByCWD(normalizedCWD)
	if err != nil {
		if err == sql.ErrNoRows {
			return &mcp.CallToolResult{IsError: true}, AssumeRoleOutput{
				Status: "error",
				Message: fmt.Sprintf(
					"Auth Error: Unknown CWD (%s). Project onboarding is Overseer-only. Authenticate as overseer and call register_project(cwd=%q, name=%q). Do not retry assume_role as fixer/netrunner for onboarding.",
					normalizedCWD,
					normalizedCWD,
					defaultProjectName(normalizedCWD),
				),
			}, nil
		}
		return &mcp.CallToolResult{IsError: true}, AssumeRoleOutput{Status: "error", Message: "Database error during auth"}, nil
	}

	switch requestedRole {
	case "fixer":
		if lockedRole != "fixer" && input.Token != "supersecret" { // hardcoded for demo, normally check env
			return &mcp.CallToolResult{IsError: true}, AssumeRoleOutput{Status: "error", Message: "invalid token"}, nil
		}
		authorizedRole = "fixer"
		authorizedProjectId = projId
		return nil, AssumeRoleOutput{Status: "success", Message: "Authenticated as Fixer. Full access granted.", RolePreprompt: getRolePreprompt("fixer")}, nil
	case "netrunner":
		authorizedRole = "netrunner"
		authorizedProjectId = projId
		return nil, AssumeRoleOutput{Status: "success", Message: fmt.Sprintf("Authenticated as Netrunner for Project %d", projId), RolePreprompt: getRolePreprompt("netrunner")}, nil
	default:
		return &mcp.CallToolResult{IsError: true}, AssumeRoleOutput{Status: "error", Message: "unknown role"}, nil
	}
}
