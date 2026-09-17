package main

import (
	"bytes"
	"context"
	"database/sql"
	"encoding/json"
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

const (
	schemaBootstrapArg  = "--bootstrap-schema"
	schemaBootstrapName = "project-workroom-v1"
)

type schemaBootstrapResult struct {
	Status            string `json:"status"`
	Schema            string `json:"schema"`
	ProjectCount      int    `json:"project_count"`
	ProjectHandsCount int    `json:"project_hands_count"`
}

func schemaBootstrapRequested(args []string) bool {
	return len(args) == 1 && args[0] == schemaBootstrapArg
}

func helpRequested(args []string) bool {
	return len(args) == 1 && (args[0] == "--help" || args[0] == "-h" || args[0] == "-help")
}

func runSchemaBootstrap(output io.Writer) (returnErr error) {
	initDB()
	bootstrapDB := db
	defer func() {
		if bootstrapDB != nil {
			if closeErr := bootstrapDB.Close(); returnErr == nil && closeErr != nil {
				returnErr = fmt.Errorf("close bootstrapped database: %w", closeErr)
			}
		}
		if db == bootstrapDB {
			db = nil
		}
	}()

	var tableCount int
	if err := bootstrapDB.QueryRow(`
		SELECT COUNT(*)
		FROM sqlite_master
		WHERE type = 'table'
		  AND name = 'project_hands'
	`).Scan(&tableCount); err != nil {
		return fmt.Errorf("verify Project Hands table: %w", err)
	}
	if tableCount != 1 {
		return fmt.Errorf("verify Project Hands table: expected 1, found %d", tableCount)
	}

	result := schemaBootstrapResult{Status: "ready", Schema: schemaBootstrapName}
	if err := bootstrapDB.QueryRow(`SELECT COUNT(*) FROM project`).Scan(&result.ProjectCount); err != nil {
		return fmt.Errorf("count projects after schema bootstrap: %w", err)
	}
	if err := bootstrapDB.QueryRow(`SELECT COUNT(*) FROM project_hands`).Scan(&result.ProjectHandsCount); err != nil {
		return fmt.Errorf("count Project Hands identities after schema bootstrap: %w", err)
	}
	if result.ProjectCount != result.ProjectHandsCount {
		return fmt.Errorf(
			"incomplete Project Hands bootstrap: projects=%d identities=%d",
			result.ProjectCount,
			result.ProjectHandsCount,
		)
	}

	if err := json.NewEncoder(output).Encode(result); err != nil {
		return fmt.Errorf("encode schema bootstrap result: %w", err)
	}
	return nil
}

func main() {
	if err := loadOptionalDotEnv(".env.local", ".env", "../.env.local", "../.env"); err != nil {
		fmt.Fprintf(os.Stderr, "error loading .env files: %v", err)
		os.Exit(1)
	}
	if helpRequested(os.Args[1:]) {
		fmt.Println("Usage: fixer_mcp [--bootstrap-schema]")
		return
	}
	if schemaBootstrapRequested(os.Args[1:]) {
		if err := runSchemaBootstrap(os.Stdout); err != nil {
			fmt.Fprintf(os.Stderr, "error bootstrapping Fixer MCP schema: %v\n", err)
			os.Exit(1)
		}
		return
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
	bootstrapDefaultRoleAuthFromEnv()

	// Create MCP server
	server := mcp.NewServer(&mcp.Implementation{Name: "fixer_mcp", Version: "v1.0.0"}, nil)
	lockedRole, lockedRoleErr := lockedRoleFromEnv()
	if lockedRoleErr != nil {
		log.Fatalf("Invalid locked role config: %v", lockedRoleErr)
	}
	toolProfile := os.Getenv(fixerMcpToolProfileEnv)
	if toolProfile == netrunnerGateProfile {
		if lockedRole != "fixer" {
			log.Fatalf("%s=%s requires %s=fixer", fixerMcpToolProfileEnv, toolProfile, fixerMcpLockedRoleEnv)
		}
		if authorizedRole != "fixer" || authorizedProjectId <= 0 {
			log.Fatalf("%s=%s requires successful project-bound Fixer env authentication", fixerMcpToolProfileEnv, toolProfile)
		}
		log.Printf("Registering direct Netrunner gate tool surface for role=%s", lockedRole)
		registerNetrunnerGateTools(server)
	} else if toolProfile != "" {
		log.Fatalf("Invalid %s config: %q", fixerMcpToolProfileEnv, toolProfile)
	} else if lockedRole != "" {
		log.Printf("Registering locked Fixer MCP tool surface for role=%s", lockedRole)
		registerMcpTools(server, lockedRole)
	} else {
		log.Println("Registering legacy unlocked Fixer MCP tool surface")
		registerMcpTools(server, lockedRole)
	}

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
		resolveFixerPythonExecutable(os.Environ()),
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
