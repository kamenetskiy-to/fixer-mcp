package main

// System1 restricted Pi reader launch (execution side of the restricted-reader
// threat model in restricted_reader.go).
//
// The one-shot System1 transcript reader runs on the Pi CLI with the existing
// CommandCode subscription the Architect already configured:
// provider commandcode, model xiaomi/mimo-v2.6-flash (Flash,
// not Pro). Provider, model, and auth configuration are REUSED from the real
// agent config directory (models.json/auth.json are referenced in place, never
// copied and never swapped); no invented provider id, no --api-key, no direct
// commandcode CLI call, no paid API.
//
// Capability restriction (Pi sandbox argv): `--no-extensions` disables every
// discovered, configured, and built-in extension (MCP included) except the one
// explicitly pinned evidence tool, `--tools read_evidence` is the complete tool
// selection (no bash, write, edit, read, codemode, tool_search), and
// `--no-skills`, `--no-prompt-templates`, `--no-themes`, `--no-context-files`
// disable all discovered resources including AGENTS.md/CLAUDE.md and
// `.pi/SYSTEM.md` steering. `--no-approve` refuses project-local files even if
// a project is pre-trusted, `--no-session` keeps the run ephemeral, and
// `--offline` disables startup network activity beyond the model call itself.
// The agent config directory is an isolated per-run directory that references
// only models.json/auth.json, so user-level extensions, skills, MCP servers,
// settings, hooks, and packages cannot load at all.
//
// The reader's only read capability is the pinned `read_evidence` tool
// (system1_reader_evidence_tool.ts), restricted to the exact evidenced
// transcript/artifact paths registered in READER_EVIDENCE_ALLOWLIST before
// launch. Only the exact child process started here is ever killed.

import (
	"bytes"
	"context"
	_ "embed"
	"encoding/json"
	"fmt"
	"log"
	"os"
	"os/exec"
	"path/filepath"
	"strconv"
	"strings"
	"time"
)

const (
	system1ReaderProvider = "commandcode"

	// system1ReaderEvidenceToolName is the single tool the reader may hold.
	system1ReaderEvidenceToolName = "read_evidence"

	system1ReaderEvidenceAllowlistEnv = "READER_EVIDENCE_ALLOWLIST"
	system1ReaderEvidenceReadLogEnv   = "READER_EVIDENCE_READ_LOG"
	system1ReaderAgentDirEnv          = "PI_CODING_AGENT_DIR"

	// system1ReaderAgentModelsFile is the one required provider configuration
	// file reused (referenced in place) for the reader.
	system1ReaderAgentModelsFile = "models.json"
	system1ReaderAgentAuthFile   = "auth.json"
)

//go:embed system1_reader_evidence_tool.ts
var system1ReaderEvidenceToolSource []byte

// system1ReaderExecTimeout bounds one reader execution. Tests shrink it; it is
// never raised ad hoc to paper over a slow or oversized run.
var system1ReaderExecTimeout = system1ManagedExecTimeout

// resolveSystem1PiBinary locates the Pi CLI. The reader is Pi-only: there is no
// commandcode/direct-command fallback path for the reader.
func resolveSystem1PiBinary() string {
	if path, err := exec.LookPath("pi"); err == nil {
		return path
	}
	return "pi"
}

// buildSystem1PiReaderArgs is the complete, deterministic Pi sandbox argv for
// one bounded reader execution. Every capability the reader does not need is
// disabled explicitly; the only loaded extension is the pinned restricted
// evidence tool and the only tool selection is that tool.
func buildSystem1PiReaderArgs(model string, extensionPath string) []string {
	return []string{
		"--provider", system1ReaderProvider,
		"--model", strings.TrimSpace(model),
		"--mode", "json",
		"--thinking", "low",
		"--no-session",
		"--no-approve",
		"--offline",
		"--no-extensions",
		"--no-skills",
		"--no-prompt-templates",
		"--no-themes",
		"--no-context-files",
		"--tools", system1ReaderEvidenceToolName,
		"-e", extensionPath,
	}
}

// restrictedReaderArgvFlagNames extracts only the flag NAMES of a reader argv
// for durable run metadata. Full argv (with values and paths) is never
// persisted.
func restrictedReaderArgvFlagNames(args []string) []string {
	flags := []string{}
	for _, arg := range args {
		switch {
		case strings.HasPrefix(arg, "--"):
			flags = append(flags, strings.SplitN(strings.TrimPrefix(arg, "--"), "=", 2)[0])
		case len(arg) > 1 && arg[0] == '-' && arg[1] != '-' && arg[1] >= 'a' && arg[1] <= 'z':
			flags = append(flags, strings.TrimPrefix(arg, "-"))
		}
	}
	return flags
}

// materializeSystem1ReaderEvidenceTool writes the embedded restricted evidence
// tool into the isolated reader run directory so the Pi sandbox can load it
// explicitly with -e. The tool source is part of the binary; nothing is
// discovered from the project or the user's extension directories.
func materializeSystem1ReaderEvidenceTool(runDir string) (string, error) {
	path := filepath.Join(runDir, "system1_reader_evidence_tool.ts")
	if err := os.WriteFile(path, system1ReaderEvidenceToolSource, 0o644); err != nil {
		return "", fmt.Errorf("failed to materialize restricted evidence tool: %v", err)
	}
	return path, nil
}

// system1ReaderConfigSourceDir is the real agent config directory whose
// provider/model/auth configuration is reused for the reader (PI_CODING_AGENT_DIR
// when set, else ~/.pi/agent). The reader never copies or rewrites it.
func system1ReaderConfigSourceDir(baseEnv []string) string {
	env := envSliceToMap(baseEnv)
	if configured := strings.TrimSpace(env[system1ReaderAgentDirEnv]); configured != "" {
		return configured
	}
	if home := strings.TrimSpace(env["HOME"]); home != "" {
		return filepath.Join(home, ".pi", "agent")
	}
	return ""
}

// prepareSystem1ReaderAgentDir builds the isolated per-run agent config
// directory. It references only the real models.json (required) and auth.json
// (optional) in place via symlinks, so the reader reuses the actual
// provider/model/auth configuration while user-level extensions, skills, MCP
// servers, settings, hooks, and packages physically cannot load.
func prepareSystem1ReaderAgentDir(baseEnv []string, runDir string) (string, string, error) {
	sourceDir := system1ReaderConfigSourceDir(baseEnv)
	if sourceDir == "" {
		return "", "", fmt.Errorf("reader provider configuration not found: no HOME or PI_CODING_AGENT_DIR in the launch environment")
	}
	modelsPath := filepath.Join(sourceDir, system1ReaderAgentModelsFile)
	if _, err := os.Stat(modelsPath); err != nil {
		return "", sourceDir, fmt.Errorf("reader provider configuration not found at the agent config directory: %v", err)
	}
	agentDir := filepath.Join(runDir, "agent-config")
	if err := os.MkdirAll(agentDir, 0o755); err != nil {
		return "", sourceDir, fmt.Errorf("failed to prepare isolated reader agent config dir: %v", err)
	}
	for _, name := range []string{system1ReaderAgentModelsFile, system1ReaderAgentAuthFile} {
		source := filepath.Join(sourceDir, name)
		if _, err := os.Stat(source); err != nil {
			if name == system1ReaderAgentAuthFile {
				// auth.json is optional: providers configured with a models.json
				// apiKey (CommandCode) do not need it.
				continue
			}
			return "", sourceDir, fmt.Errorf("reader provider configuration not found at the agent config directory: %v", err)
		}
		if err := os.Symlink(source, filepath.Join(agentDir, name)); err != nil {
			return "", sourceDir, fmt.Errorf("failed to reference reader provider configuration: %v", err)
		}
	}
	return agentDir, sourceDir, nil
}

// extractPiReaderOverview extracts the final factual overview and the
// restricted tool-call ledger from one Pi `--mode json` event stream. Only the
// final completed assistant message text is an overview; thinking, tool calls,
// tool results, and intermediate turns never reach the judge. Runs that ended
// truncated (length), aborted, errored, or mid-tool-execution fail closed
// because they have no complete overview.
func extractPiReaderOverview(raw string) (string, []restrictedReaderToolCall, error) {
	trimmed := strings.TrimSpace(raw)
	if trimmed == "" {
		return "", nil, fmt.Errorf("reader produced no output: no final overview")
	}
	type finalAssistant struct {
		text       string
		stopReason string
	}
	var last finalAssistant
	sawAssistant := false
	sawPiStream := false
	toolCalls := []restrictedReaderToolCall{}
	var streamError string
	for _, line := range strings.Split(raw, "\n") {
		line = strings.TrimSpace(line)
		if line == "" {
			continue
		}
		var entry map[string]any
		if err := json.Unmarshal([]byte(line), &entry); err != nil {
			continue
		}
		recordType, _ := entry["type"].(string)
		switch recordType {
		case "session", "agent_start", "turn_start", "message_start", "message_update",
			"tool_execution_start", "tool_execution_update", "queue_update", "entry_appended",
			"session_info_changed", "thinking_level_changed", "compaction_start", "compaction_end",
			"auto_retry_start", "auto_retry_end", "agent_end", "agent_settled":
			sawPiStream = true
		case "message_end":
			sawPiStream = true
			message, ok := entry["message"].(map[string]any)
			if !ok {
				continue
			}
			role, _ := message["role"].(string)
			if role != "assistant" {
				continue
			}
			stopReason, _ := message["stopReason"].(string)
			var texts []string
			if content, ok := message["content"].([]any); ok {
				for _, block := range content {
					if textBlock, ok := block.(map[string]any); ok {
						if blockType, _ := textBlock["type"].(string); blockType == "text" {
							if text, ok := textBlock["text"].(string); ok && strings.TrimSpace(text) != "" {
								texts = append(texts, text)
							}
						}
					}
				}
			}
			last = finalAssistant{text: strings.Join(texts, "\n"), stopReason: stopReason}
			sawAssistant = true
		case "tool_execution_end":
			sawPiStream = true
			call := restrictedReaderToolCall{}
			call.ToolName, _ = entry["toolName"].(string)
			if args, ok := entry["args"].(map[string]any); ok {
				if path, ok := args["path"].(string); ok {
					call.Path = redactReaderSecrets(path)
				}
				if startLine, ok := args["start_line"].(float64); ok {
					call.StartLine = int(startLine)
				}
				if limitLines, ok := args["limit_lines"].(float64); ok {
					call.LimitLines = int(limitLines)
				}
			}
			isError, _ := entry["isError"].(bool)
			call.IsError = isError
			if result, ok := entry["result"].(map[string]any); ok {
				if content, ok := result["content"].([]any); ok {
					for _, block := range content {
						if textBlock, ok := block.(map[string]any); ok {
							if blockType, _ := textBlock["type"].(string); blockType == "text" {
								if text, ok := textBlock["text"].(string); ok {
									call.ResultExcerpt = boundedRedactedExcerpt(text, restrictedReaderExcerptMaxByte)
									break
								}
							}
						}
					}
				}
			}
			toolCalls = append(toolCalls, call)
		case "error":
			sawPiStream = true
			if message, ok := entry["error"].(string); ok {
				streamError = message
			} else if message, ok := entry["message"].(string); ok {
				streamError = message
			}
		}
	}
	if !sawPiStream {
		return "", nil, fmt.Errorf("unrecognized reader output: no Pi JSON event stream (no final overview)")
	}
	if !sawAssistant {
		if streamError != "" {
			return "", nil, fmt.Errorf("reader run failed: %s", boundedParallelWaveSummaryText(redactReaderSecrets(streamError), 500))
		}
		return "", nil, fmt.Errorf("reader produced no assistant message: no final overview")
	}
	switch last.stopReason {
	case "stop":
		if strings.TrimSpace(last.text) == "" {
			return "", nil, fmt.Errorf("reader produced no final overview text")
		}
		return strings.TrimSpace(last.text), toolCalls, nil
	case "length":
		return "", nil, fmt.Errorf("reader output was truncated by the model output limit (stopReason=length): no complete final overview")
	case "aborted":
		return "", nil, fmt.Errorf("reader run was aborted: no complete final overview")
	case "error":
		if streamError != "" {
			return "", nil, fmt.Errorf("reader run ended in error: %s", boundedParallelWaveSummaryText(redactReaderSecrets(streamError), 500))
		}
		return "", nil, fmt.Errorf("reader run ended in error: no complete final overview")
	case "toolUse":
		return "", nil, fmt.Errorf("reader run ended mid tool execution (stopReason=toolUse): no complete final overview")
	default:
		return "", nil, fmt.Errorf(
			"reader run has no completed final overview (stopReason=%q): refusing to send an incomplete overview to the judge",
			boundedParallelWaveSummaryText(last.stopReason, 100),
		)
	}
}

// system1ReaderExecFunc is the reader seam: one bounded reader execution.
// Tests swap the seam so no provider process is ever spawned.
type system1ReaderExecFunc func(ctx context.Context, projectCWD string, run system1ReaderRun) (string, error)

// system1ReaderRun is one bounded reader execution request: a chunk execution
// (labelled with its exact covered range), the bounded synthesis, or the
// single-pass overview. Evidence paths are the exact readable set.
type system1ReaderRun struct {
	Label         string
	Model         string
	Prompt        string
	EvidencePaths []string
	ChunkIndex    int
	Chunk         *transcriptCoverageChunk
}

// launchSystem1PiReader runs one bounded headless Pi query in the restricted
// reader sandbox (see the file header and restricted_reader.go). The prompt
// travels on stdin. Every run persists secret-safe durable metadata (input
// hash, covered chunk range, tool-call ledger, stdout/stderr byte counts and
// bounded redacted excerpts, partial overview on failure, timing), and only
// the exact child process started here is ever killed.
func launchSystem1PiReader(ctx context.Context, projectCWD string, run system1ReaderRun) (string, error) {
	model := strings.TrimSpace(run.Model)
	if model == "" {
		model = system1ReaderModel
	}
	runID := fmt.Sprintf("reader-%d-%s", os.Getpid(), time.Now().UTC().Format("20060102T150405.000000000Z"))
	readerCwd, cwdErr := restrictedReaderRunDir(projectCWD, runID)
	if cwdErr != nil {
		return "", fmt.Errorf("managed system1 reader (%s/%s): %v", system1ReaderProvider, model, cwdErr)
	}
	extensionPath, toolErr := materializeSystem1ReaderEvidenceTool(readerCwd)
	if toolErr != nil {
		return "", fmt.Errorf("managed system1 reader (%s/%s): %v", system1ReaderProvider, model, toolErr)
	}
	agentDir, configSource, configErr := prepareSystem1ReaderAgentDir(os.Environ(), readerCwd)
	if configErr != nil {
		// Missing provider configuration is a distinct infrastructure
		// condition: it is not a missing transcript path and the reader is
		// never launched for it.
		return "", fmt.Errorf("managed system1 reader (%s/%s): %v", system1ReaderProvider, model, configErr)
	}

	args := buildSystem1PiReaderArgs(model, extensionPath)
	allowedPaths := restrictedReaderEvidenceAllowlist(run.EvidencePaths)
	allowlistPayload, _ := json.Marshal(allowedPaths)

	promptBytes, promptSHA := readerInputFingerprint(run.Prompt)
	startedAt := time.Now()
	metadata := restrictedReaderRunMetadata{
		RunId:             runID,
		Label:             run.Label,
		Model:             model,
		Provider:          system1ReaderProvider,
		ReaderCwd:         readerCwd,
		AgentConfigSource: filepath.Base(configSource),
		StartedAt:         startedAt.UTC().Format(time.RFC3339Nano),
		TimeoutMs:         system1ReaderExecTimeout.Milliseconds(),
		PromptBytes:       promptBytes,
		PromptSHA256:      promptSHA,
		EvidencePaths:     len(allowedPaths),
		ArgvFlags:         restrictedReaderArgvFlagNames(args),
	}
	if run.Chunk != nil {
		metadata.ChunkIndex = run.ChunkIndex
		metadata.ChunkStartLine = run.Chunk.StartLine
		metadata.ChunkEndLine = run.Chunk.EndLine
		metadata.ChunkStartByte = run.Chunk.StartByte
		metadata.ChunkEndByte = run.Chunk.EndByte
	}

	command := execCommand(resolveSystem1PiBinary(), args...)
	// The reader never runs in the project cwd: project hooks, skills,
	// extensions, and malicious project MCP configs are unreachable from an
	// empty isolated directory.
	command.Dir = readerCwd
	// The prompt (reader instructions plus the numbered chunk/overview input)
	// travels on stdin. On argv it would exceed ARG_MAX and the run would
	// never start.
	command.Stdin = strings.NewReader(run.Prompt)
	commandEnv, envErr := resolveRuntimeLaunchEnv(projectCWD, os.Environ())
	if envErr != nil {
		log.Printf("warning: system1 reader launch: failed to resolve runtime launch env: %v", envErr)
		commandEnv = os.Environ()
	}
	cleanedEnv, denied := restrictedReaderEnv(commandEnv)
	cleanedEnv = replaceEnvSliceValue(cleanedEnv, system1ReaderAgentDirEnv, agentDir)
	cleanedEnv = replaceEnvSliceValue(cleanedEnv, "PI_SKIP_VERSION_CHECK", "1")
	// The reader never reports telemetry or install attribution.
	cleanedEnv = replaceEnvSliceValue(cleanedEnv, "PI_TELEMETRY", "0")
	cleanedEnv = replaceEnvSliceValue(cleanedEnv, system1ReaderEvidenceAllowlistEnv, string(allowlistPayload))
	cleanedEnv = replaceEnvSliceValue(cleanedEnv, system1ReaderEvidenceReadLogEnv, filepath.Join(readerCwd, "evidence_reads.jsonl"))
	command.Env = cleanedEnv
	metadata.DeniedEnvNames = denied
	var stdout bytes.Buffer
	var stderr bytes.Buffer
	command.Stdout = &stdout
	command.Stderr = &stderr

	persistMetadata := func(runErr error, timedOut bool, canceled bool, killedOwnChild bool) {
		metadata.FinishedAt = time.Now().UTC().Format(time.RFC3339Nano)
		metadata.DurationMs = time.Since(startedAt).Milliseconds()
		metadata.TimedOut = timedOut
		metadata.Canceled = canceled
		metadata.KilledOwnChild = killedOwnChild
		metadata.StdoutBytes = int64(stdout.Len())
		metadata.StderrBytes = int64(stderr.Len())
		metadata.StdoutExcerpt = boundedRedactedExcerpt(stdout.String(), restrictedReaderExcerptMaxByte)
		metadata.StderrExcerpt = boundedRedactedExcerpt(stderr.String(), restrictedReaderExcerptMaxByte)
		// The restricted tool-call ledger is recovered from whatever complete
		// events stdout carries, so timeout/error runs still show what the
		// reader tried to read and what was denied.
		if _, toolCalls, _ := extractPiReaderOverview(stdout.String()); len(toolCalls) > 0 {
			metadata.ToolCalls = toolCalls
		}
		if runErr != nil {
			metadata.ExitError = boundedRedactedExcerpt(redactReaderSecrets(runErr.Error()), restrictedReaderExcerptMaxByte)
			if partial, _, extractErr := extractPiReaderOverview(stdout.String()); extractErr == nil {
				metadata.PartialOverview = boundedRedactedExcerpt(partial, restrictedReaderExcerptMaxByte)
			}
		}
		if metadataPath, metaErr := persistRestrictedReaderRunMetadata(projectCWD, metadata); metaErr != nil {
			log.Printf("warning: system1 reader launch: run metadata not persisted for %s: %v", runID, metaErr)
		} else {
			log.Printf("system1 reader run metadata: %s", metadataPath)
		}
	}

	if err := command.Start(); err != nil {
		startErr := fmt.Errorf("failed to start managed system1 reader (%s/%s): %v", system1ReaderProvider, model, err)
		persistMetadata(startErr, false, false, false)
		return "", startErr
	}
	waitErrCh := make(chan error, 1)
	go func() {
		waitErrCh <- command.Wait()
	}()
	select {
	case waitErr := <-waitErrCh:
		if waitErr != nil {
			detail := strings.TrimSpace(stderr.String())
			if detail == "" {
				detail = waitErr.Error()
			}
			runErr := fmt.Errorf("managed system1 reader (%s/%s) exited with error: %s", system1ReaderProvider, model, boundedParallelWaveSummaryText(detail, 2000))
			persistMetadata(runErr, false, false, false)
			return "", runErr
		}
	case <-time.After(system1ReaderExecTimeout):
		// Kill only the exact child process started above; nothing else is
		// ever signaled. The timeout diagnostic carries the covered range and
		// input size so a huge-transcript chunk read is distinguishable from a
		// missing-path failure (which never launches the reader at all).
		_ = command.Process.Kill()
		<-waitErrCh
		runErr := fmt.Errorf(
			"managed system1 reader (%s/%s) exceeded bounded timeout %s (label=%s, prompt_bytes=%d)",
			system1ReaderProvider, model, system1ReaderExecTimeout, run.Label, promptBytes,
		)
		persistMetadata(runErr, true, false, true)
		return "", runErr
	case <-ctx.Done():
		_ = command.Process.Kill()
		<-waitErrCh
		runErr := fmt.Errorf("managed system1 reader (%s/%s) canceled: %v", system1ReaderProvider, model, ctx.Err())
		persistMetadata(runErr, false, true, true)
		return "", runErr
	}
	result, _, extractErr := extractPiReaderOverview(stdout.String())
	persistMetadata(extractErr, false, false, false)
	if extractErr != nil {
		return "", extractErr
	}
	return result, nil
}

// formatChunkRangeLabel renders the exact covered range of one chunk execution
// for prompts, labels, and timeout diagnostics.
func formatChunkRangeLabel(chunk transcriptCoverageChunk) string {
	return fmt.Sprintf(
		"lines %d..%d (bytes %s..%s of file %d)",
		chunk.StartLine, chunk.EndLine,
		strconv.FormatInt(chunk.StartByte, 10), strconv.FormatInt(chunk.EndByte, 10),
		chunk.FileIndex,
	)
}
