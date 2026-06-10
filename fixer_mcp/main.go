package main

import (
	"bytes"
	"context"
	"database/sql"
	"fmt"
	"io"
	"log"
	"os"
	"os/exec"
	"path/filepath"
	"strconv"

	_ "github.com/glebarez/go-sqlite"
	"github.com/modelcontextprotocol/go-sdk/mcp"
)

// Global state for this specific stdio session
var (
	authorizedRole      string
	authorizedProjectId int
	authorizedSessionId int
	db                  *sql.DB
)

var execCommand = exec.Command

var codexSessionTranscriptRoot = filepath.Join(os.Getenv("HOME"), ".codex", "sessions")
var droidSessionTranscriptRoot = filepath.Join(os.Getenv("HOME"), ".factory", "sessions")

func main() {
	if err := loadOptionalDotEnv(".env.local", ".env", "../.env.local", "../.env"); err != nil {
		fmt.Fprintf(os.Stderr, "error loading .env files: %v", err)
		os.Exit(1)
	}

	// Configure logging to a file since stdio is used for MCP JSON-RPC
	f, err := os.OpenFile("fixer_mcp.log", os.O_RDWR|os.O_CREATE|os.O_APPEND, 0666)
	if err != nil {
		fmt.Fprintf(os.Stderr, "error opening log file: %v", err)
		os.Exit(1)
	}
	defer func() {
		_ = f.Close()
	}()
	log.SetOutput(f)
	log.Println("Starting Fixer MCP server...")

	initDB()
	bootstrapDefaultNetrunnerAuthFromEnv()

	// Create MCP server
	server := mcp.NewServer(&mcp.Implementation{Name: "fixer_mcp", Version: "v1.0.0"}, nil)
	lockedRole, lockedRoleErr := lockedRoleFromEnv()
	if lockedRoleErr != nil {
		log.Fatalf("Invalid locked role config: %v", lockedRoleErr)
	}
	if lockedRole != "" {
		log.Printf("Registering locked Fixer MCP tool surface for role=%s", lockedRole)
	} else {
		log.Println("Registering legacy unlocked Fixer MCP tool surface")
	}
	registerMcpTools(server, lockedRole)

	// Run stdio transport
	if err := server.Run(context.Background(), &mcp.StdioTransport{}); err != nil {
		log.Fatalf("Server error: %v", err)
	}
}

type WakeFixerAutonomousInput struct {
	SessionId int    `json:"session_id,omitempty" jsonschema:"Optional local session ID to wake the Fixer for. Defaults to the currently checked-out netrunner session."`
	Summary   string `json:"summary,omitempty" jsonschema:"Concise handoff summary for the Fixer resume prompt."`
}

type WakeFixerAutonomousOutput struct {
	Status            string `json:"status"`
	SessionId         int    `json:"session_id"`
	ProjectCwd        string `json:"project_cwd"`
	LauncherScript    string `json:"launcher_script"`
	FixerStateFile    string `json:"fixer_state_file"`
	SpawnedBackground bool   `json:"spawned_background"`
	SuppressedReason  string `json:"suppressed_reason,omitempty"`
}

func WakeFixerAutonomous(ctx context.Context, req *mcp.CallToolRequest, input WakeFixerAutonomousInput) (*mcp.CallToolResult, WakeFixerAutonomousOutput, error) {
	if authorizedRole != "netrunner" {
		return &mcp.CallToolResult{IsError: true}, WakeFixerAutonomousOutput{}, fmt.Errorf("access denied: requires netrunner role")
	}

	projectCWD, err := projectCWDFromID(authorizedProjectId)
	if err != nil {
		return &mcp.CallToolResult{IsError: true}, WakeFixerAutonomousOutput{}, fmt.Errorf("DB query error: %v", err)
	}

	localSessionID := input.SessionId
	globalSessionID := 0
	switch {
	case localSessionID > 0:
		mappedGlobalSessionID, mapErr := globalSessionIDFromProjectScoped(localSessionID, authorizedProjectId)
		if mapErr == sql.ErrNoRows {
			return &mcp.CallToolResult{IsError: true}, WakeFixerAutonomousOutput{}, fmt.Errorf("session %d not found in current project", localSessionID)
		}
		if mapErr != nil {
			return &mcp.CallToolResult{IsError: true}, WakeFixerAutonomousOutput{}, fmt.Errorf("DB query error: %v", mapErr)
		}
		globalSessionID = mappedGlobalSessionID
	case authorizedSessionId > 0:
		mappedGlobalSessionID, mappedSessionID, mapErr := resolveAuthorizedNetrunnerSessionID("wake_fixer_autonomous", nil)
		if mapErr != nil {
			return &mcp.CallToolResult{IsError: true}, WakeFixerAutonomousOutput{}, fmt.Errorf("failed to resolve current session id: %v", mapErr)
		}
		localSessionID = mappedSessionID
		globalSessionID = mappedGlobalSessionID
	default:
		return &mcp.CallToolResult{IsError: true}, WakeFixerAutonomousOutput{}, fmt.Errorf("session_id is required when no current netrunner session is checked out")
	}

	launcherScript, err := resolveExplicitLauncherScript()
	if err != nil {
		return &mcp.CallToolResult{IsError: true}, WakeFixerAutonomousOutput{}, fmt.Errorf("autonomous fixer launcher not found: %v", err)
	}
	fixerStateFile := filepath.Join(projectCWD, ".codex", "autonomous_resolution.json")

	if process, exists, processErr := latestWorkerProcessForSession(authorizedProjectId, globalSessionID); processErr != nil {
		return &mcp.CallToolResult{IsError: true}, WakeFixerAutonomousOutput{}, fmt.Errorf("DB query error: %v", processErr)
	} else if exists && process.LaunchOrigin == "explicit-wait" {
		reason := fmt.Sprintf("suppressed for explicit launch/wait session with worker_process_id=%d status=%s", process.ID, process.Status)
		log.Printf("wake_fixer_autonomous suppressed project_id=%d session_id=%d reason=%q", authorizedProjectId, localSessionID, reason)
		return nil, WakeFixerAutonomousOutput{
			Status:            "suppressed",
			SessionId:         localSessionID,
			ProjectCwd:        projectCWD,
			LauncherScript:    launcherScript,
			FixerStateFile:    fixerStateFile,
			SpawnedBackground: false,
			SuppressedReason:  reason,
		}, nil
	}

	command := execCommand(
		"python3",
		launcherScript,
		"resume-fixer",
		"--cwd",
		projectCWD,
		"--completed-session-id",
		strconv.Itoa(localSessionID),
		"--summary",
		input.Summary,
	)
	command.Dir = projectCWD
	command.Stdin = bytes.NewReader(nil)
	command.Stdout = io.Discard
	command.Stderr = io.Discard
	commandEnv, envErr := resolveRuntimeLaunchEnv(projectCWD, os.Environ())
	if envErr != nil {
		log.Printf("warning: failed to resolve runtime launch env for %s: %v", projectCWD, envErr)
		commandEnv = os.Environ()
	}
	command.Env = commandEnv
	if err := command.Run(); err != nil {
		return &mcp.CallToolResult{IsError: true}, WakeFixerAutonomousOutput{}, fmt.Errorf("failed to wake autonomous fixer: %v", err)
	}

	log.Printf("wake_fixer_autonomous project_id=%d session_id=%d summary=%q", authorizedProjectId, localSessionID, input.Summary)

	return nil, WakeFixerAutonomousOutput{
		Status:            "success",
		SessionId:         localSessionID,
		ProjectCwd:        projectCWD,
		LauncherScript:    launcherScript,
		FixerStateFile:    fixerStateFile,
		SpawnedBackground: true,
	}, nil
}
