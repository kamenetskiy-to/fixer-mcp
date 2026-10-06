package main

// System1 restricted transcript reader support.
//
// This file owns three concerns for the one-shot System1 transcript reader:
//
//  1. Full ORIGINAL evidence reading via chunked execution. The original
//     session JSONL continuation history is streamed end to end, sequentially,
//     in line-aligned bounded chunks, with no skipped line or byte ranges and
//     no head/tail truncation. Every chunk becomes one bounded reader
//     execution, so histories larger than any single prompt (including >8MiB)
//     are actually read end to end instead of being assembled into one bounded
//     whole text and infrastructure-blocked. Every read produces a
//     deterministic coverage ledger (per-file line and byte ranges, EOF state,
//     and session identities) that is validated before the judge can run, and
//     each chunk execution is recorded in the ledger (prompt fingerprint,
//     observation fingerprint, status). A compressed or truncated index never
//     substitutes for the primary evidence; the original files are kept
//     untouched.
//
//  2. Restricted launch environment (threat model). The reader is a one-shot
//     review component, never a nested implementation agent. It must not hold
//     any project Fixer MCP, DB, or lifecycle authority and must not inherit
//     project hooks, skills, extensions, or shell tooling. Denied capabilities:
//     FIXER_*/CODEX_*/NETRUNNER_*/OVERSEER_*/HANDS_* control-plane variables
//     (FIXER_DB_PATH included), every MCP configuration/authority variable,
//     git worktree overrides, and shell-injection vectors (BASH_ENV, ENV,
//     PROMPT_COMMAND, SHELLOPTS, PS4, LD_PRELOAD, PYTHONSTARTUP). Allowed: the
//     provider's own auth/config under the agent directory (the reader reuses
//     the actual configured provider/model/auth; no invented ids, no direct
//     API/BYOK), PATH, locale, and temp variables. The reader runs in an
//     isolated empty cwd inside its own run directory with an isolated agent
//     config directory, so a malicious project `.mcp.json`, `.pi/mcp.json`,
//     settings, or hooks file can never be discovered, and user-level
//     extensions/skills/MCP/hooks never load (see restricted_reader_pi.go for
//     the Pi sandbox argv). Exact read capabilities are restricted to the
//     evidenced transcript/artifact paths registered before launch.
//
//  3. Durable, secret-safe run metadata. Each reader run persists metadata,
//     input hash, stdout/stderr byte counts with bounded redacted excerpts, a
//     partial overview on timeout/error, the coverage diagnostics, and the
//     restricted tool-call ledger. Only the exact child process this code
//     started is ever killed.

import (
	"bufio"
	"crypto/sha256"
	"encoding/hex"
	"encoding/json"
	"errors"
	"fmt"
	"io"
	"os"
	"path/filepath"
	"regexp"
	"sort"
	"strings"
)

// system1ReaderChunkBytes is the nominal sequential chunk size (numbered
// reader input bytes) per reader execution. Chunks are line-aligned and
// contiguous: chunk N+1 starts exactly at chunk N's last line + 1, and byte
// coverage per file is contiguous from 0 to the file size. Histories larger
// than one chunk are read through sequential chunked executions; evidence is
// never truncated to fit a prompt.
var system1ReaderChunkBytes int64 = 512 * 1024

// system1ReaderMaxNumberedChunkBytes is the fail-closed bound on one chunk: a
// single numbered line larger than this cannot be split across executions
// without losing line integrity, so it fails as infrastructure (never a
// content verdict). Ordinary large histories are not blocked: they chunk.
var system1ReaderMaxNumberedChunkBytes int64 = 1 << 20

// system1ReaderMaxChunkExecutions bounds the number of sequential chunk
// executions per history (capacity, not a content judgement): 256 executions
// at the nominal chunk size cover 128MiB of original history.
var system1ReaderMaxChunkExecutions = 256

const (
	restrictedReaderRunDirName     = "system1_reader_runs"
	restrictedReaderExcerptMaxByte = 2000

	// system1ReaderObservationMaxBytes bounds one chunk observation so the
	// bounded final overview/Jev input stays bounded after the full reading.
	system1ReaderObservationMaxBytes = 4000

	// system1ReaderSynthesisMaxBytes bounds the total observation input of
	// the final synthesis execution; overflow is compacted through further
	// bounded reader executions, never by dropping evidence ranges silently.
	system1ReaderSynthesisMaxBytes = 48 * 1024

	// system1ReaderOverviewMaxBytes bounds the final factual overview handed
	// to typesafe/jev after the full original history has been read.
	system1ReaderOverviewMaxBytes = 16 * 1024
)

// errTranscriptCoverage reports that full original evidence could not be read
// end to end. It is always an infrastructure condition, never a content
// verdict.
var errTranscriptCoverage = errors.New("transcript coverage is incomplete")

type transcriptCoverageChunk struct {
	FileIndex int    `json:"file_index"`
	Path      string `json:"path"`
	StartByte int64  `json:"start_byte"`
	EndByte   int64  `json:"end_byte"`
	StartLine int    `json:"start_line"`
	EndLine   int    `json:"end_line"`
}

// transcriptNumberedChunk is one reader-execution unit: its coverage range
// plus the globally numbered text that must be read in that execution.
type transcriptNumberedChunk struct {
	transcriptCoverageChunk
	NumberedText string
}

// transcriptChunkExecution is the durable record of one chunk reader
// execution: the exact covered range, the prompt fingerprint, the bounded
// observation fingerprint, and the terminal status. The judge only runs after
// every chunk has exactly one successful execution.
type transcriptChunkExecution struct {
	Index             int    `json:"index"`
	Label             string `json:"label"`
	FileIndex         int    `json:"file_index"`
	Path              string `json:"path"`
	StartByte         int64  `json:"start_byte"`
	EndByte           int64  `json:"end_byte"`
	StartLine         int    `json:"start_line"`
	EndLine           int    `json:"end_line"`
	PromptBytes       int    `json:"prompt_bytes"`
	PromptSHA256      string `json:"prompt_sha256"`
	ObservationBytes  int    `json:"observation_bytes"`
	ObservationSHA256 string `json:"observation_sha256"`
	Status            string `json:"status"`
	Error             string `json:"error,omitempty"`
}

const (
	transcriptChunkExecutionStatusExecuted = "executed"
	transcriptChunkExecutionStatusFailed   = "failed"
)

type transcriptCoverageFile struct {
	Path             string `json:"path"`
	Bytes            int64  `json:"bytes"`
	Lines            int    `json:"lines"`
	SHA256           string `json:"sha256"`
	SessionID        string `json:"session_id,omitempty"`
	SessionCWD       string `json:"session_cwd,omitempty"`
	SessionTimestamp string `json:"session_timestamp,omitempty"`
	EofReached       bool   `json:"eof_reached"`
}

// transcriptCoverageLedger is the deterministic proof that the reader
// executions cover the original evidence completely: per file, byte ranges are
// contiguous from 0 to the file size, every line is numbered exactly once, and
// EOF was reached. Identities (session id/cwd/timestamp from the transcript
// header, plus the filename-encoded id) are recorded per file so continuation
// history is unambiguously linked, never an arbitrary newest file.
// ChunkExecutions records what the reader actually executed per range.
type transcriptCoverageLedger struct {
	Files           []transcriptCoverageFile   `json:"files"`
	Chunks          []transcriptCoverageChunk  `json:"chunks"`
	ChunkExecutions []transcriptChunkExecution `json:"chunk_executions,omitempty"`
	TotalBytes      int64                      `json:"total_bytes"`
	TotalLines      int                        `json:"total_lines"`
	NumberedBytes   int64                      `json:"numbered_bytes"`
	HeadSessionID   string                     `json:"head_session_id,omitempty"`
	Complete        bool                       `json:"complete"`
}

// transcriptCoverageProblems re-derives the coverage guarantees from the
// ledger alone (no trusted flags): contiguous byte coverage per file, exact
// line accounting over the globally numbered sequence, EOF on every file, and
// one agreed session identity across the continuation history. Any problem
// means the evidence is partial or contradictory and must fail as
// infrastructure before the judge runs.
func (ledger transcriptCoverageLedger) transcriptCoverageProblems() []string {
	problems := []string{}
	linesByFile := map[int]int{}
	bytesByFile := map[int]int64{}
	identities := map[string]struct{}{}
	// Chunks arrive in read order (file order, then byte order). Line numbers
	// are global across the continuation history, so the expected sequence is
	// strictly increasing with no skipped ranges.
	nextExpectedLine := 0
	for _, chunk := range ledger.Chunks {
		if chunk.StartByte != bytesByFile[chunk.FileIndex] {
			problems = append(problems, fmt.Sprintf(
				"file %d (%s): byte coverage gap at offset %d (chunk starts at %d)",
				chunk.FileIndex, chunk.Path, bytesByFile[chunk.FileIndex], chunk.StartByte,
			))
		}
		if chunk.StartLine != nextExpectedLine+1 {
			problems = append(problems, fmt.Sprintf(
				"line coverage gap: chunk of %s starts at line %d, expected %d",
				chunk.Path, chunk.StartLine, nextExpectedLine+1,
			))
		}
		bytesByFile[chunk.FileIndex] = chunk.EndByte
		linesByFile[chunk.FileIndex] += chunk.EndLine - chunk.StartLine + 1
		nextExpectedLine = chunk.EndLine
	}
	for index, file := range ledger.Files {
		if !file.EofReached {
			problems = append(problems, fmt.Sprintf("file %d (%s): EOF not reached", index, file.Path))
		}
		// Byte coverage must reach the recorded size; the per-chunk adjacency
		// check above already proves it started at byte 0 with no gaps.
		if bytesByFile[index] != file.Bytes {
			problems = append(problems, fmt.Sprintf(
				"file %d (%s): covered %d of %d bytes", index, file.Path, bytesByFile[index], file.Bytes,
			))
		}
		if linesByFile[index] != file.Lines {
			problems = append(problems, fmt.Sprintf(
				"file %d (%s): covered %d of %d lines", index, file.Path, linesByFile[index], file.Lines,
			))
		}
		identity := strings.TrimSpace(file.SessionID)
		if identity != "" {
			identities[identity] = struct{}{}
		}
	}
	if len(identities) > 1 {
		problems = append(problems, fmt.Sprintf(
			"contradictory provenance: continuation history declares multiple session identities: %s",
			strings.Join(sortedKeys(identities), ", "),
		))
	}
	return problems
}

// chunkExecutionProblems verifies, from the execution records alone, that the
// reader actually executed every covered range exactly once, in order, with a
// bounded observation recorded. A chunk without a successful execution means
// the full original history was not read, and the judge must not run.
func (ledger transcriptCoverageLedger) chunkExecutionProblems() []string {
	problems := []string{}
	if len(ledger.Chunks) == 0 {
		return problems
	}
	if len(ledger.ChunkExecutions) != len(ledger.Chunks) {
		problems = append(problems, fmt.Sprintf(
			"%d of %d chunk ranges were executed", len(ledger.ChunkExecutions), len(ledger.Chunks),
		))
	}
	for index := 0; index < len(ledger.Chunks) && index < len(ledger.ChunkExecutions); index++ {
		chunk := ledger.Chunks[index]
		execution := ledger.ChunkExecutions[index]
		if execution.Index != index {
			problems = append(problems, fmt.Sprintf(
				"chunk execution %d is out of order (recorded index %d)", index, execution.Index,
			))
		}
		if execution.StartLine != chunk.StartLine || execution.EndLine != chunk.EndLine ||
			execution.StartByte != chunk.StartByte || execution.EndByte != chunk.EndByte ||
			execution.FileIndex != chunk.FileIndex {
			problems = append(problems, fmt.Sprintf(
				"chunk %d execution range %d..%d does not match covered range %d..%d",
				index, execution.StartLine, execution.EndLine, chunk.StartLine, chunk.EndLine,
			))
		}
		if execution.Status != transcriptChunkExecutionStatusExecuted {
			problems = append(problems, fmt.Sprintf("chunk %d was not executed successfully (status %q)", index, execution.Status))
		}
		if execution.ObservationBytes <= 0 {
			problems = append(problems, fmt.Sprintf("chunk %d has no recorded observation", index))
		}
	}
	return problems
}

// Diagnostics returns human-readable coverage diagnostics for infra artifacts
// and run metadata. It never contains transcript content.
func (ledger transcriptCoverageLedger) Diagnostics() []string {
	diagnostics := []string{
		fmt.Sprintf(
			"coverage: %d file(s), %d byte(s), %d line(s), %d numbered byte(s), %d chunk execution(s), complete=%t, head_session_id=%q",
			len(ledger.Files), ledger.TotalBytes, ledger.TotalLines, ledger.NumberedBytes, len(ledger.ChunkExecutions), ledger.Complete, ledger.HeadSessionID,
		),
	}
	for _, problem := range ledger.transcriptCoverageProblems() {
		diagnostics = append(diagnostics, "coverage gap: "+problem)
	}
	for _, problem := range ledger.chunkExecutionProblems() {
		diagnostics = append(diagnostics, "chunk execution gap: "+problem)
	}
	return diagnostics
}

func sortedKeys(set map[string]struct{}) []string {
	keys := make([]string, 0, len(set))
	for key := range set {
		keys = append(keys, key)
	}
	sort.Strings(keys)
	return keys
}

// transcriptHeaderIdentity extracts the session identity from the leading
// session record of a Pi/Codex-style JSONL transcript
// ({"type":"session"|"session_meta", "id", "cwd", "timestamp"}).
func transcriptHeaderIdentity(line string) (id string, cwd string, timestamp string) {
	var payload map[string]any
	if err := json.Unmarshal([]byte(line), &payload); err != nil {
		return "", "", ""
	}
	recordType := transcriptPayloadRecordType(payload)
	if recordType != "session" && recordType != "session_meta" {
		return "", "", ""
	}
	id = payloadString(payload, "external_session_id", "externalSessionId", "session_id", "sessionId", "id")
	cwd = payloadString(payload, "cwd", "current_working_directory", "workingDirectory", "working_directory")
	timestamp = payloadString(payload, "timestamp", "created_at", "createdAt")
	return id, cwd, timestamp
}

// transcriptFileNameSessionID returns the session identity a transcript
// filename encodes: the pi convention <timestamp>_<id>.jsonl yields the id
// after the last underscore, and a bare <id>.jsonl stem is the identity. The
// whole stem may still embed the id (e.g. Codex rollout names), so callers
// only treat it as contradictory when neither identity contains the other.
func transcriptFileNameSessionID(name string) string {
	stem := strings.TrimSuffix(filepath.Base(name), ".jsonl")
	if stem == "" || stem == name {
		return ""
	}
	if separator := strings.LastIndex(stem, "_"); separator >= 0 && separator+1 < len(stem) {
		return stem[separator+1:]
	}
	return stem
}

// readTranscriptCoverageChunks streams every original transcript file end to
// end, sequentially, in line-aligned bounded chunks, and returns the numbered
// reader-execution chunks plus a deterministic coverage ledger. Errors are
// coverage/provenance errors (missing file, contradictory identities,
// unreadable content, an unchunkable oversized single line, or a history above
// the bounded chunked-execution capacity): the caller must treat them as
// infrastructure failures. Evidence is never truncated and original files are
// never modified. There is no whole-input size bound: histories larger than
// one prompt (including >8MiB) are read through sequential chunk executions.
func readTranscriptCoverageChunks(paths []string, chunkBytes int64) ([]transcriptNumberedChunk, transcriptCoverageLedger, error) {
	ledger := transcriptCoverageLedger{Files: []transcriptCoverageFile{}, Chunks: []transcriptCoverageChunk{}}
	chunks := []transcriptNumberedChunk{}
	if len(paths) == 0 {
		return chunks, ledger, fmt.Errorf("%w: no transcript files were provided", errTranscriptCoverage)
	}
	if chunkBytes <= 0 {
		chunkBytes = system1ReaderChunkBytes
	}
	identities := map[string]struct{}{}
	lineNumber := 0
	for fileIndex, path := range paths {
		if strings.TrimSpace(path) == "" {
			return chunks, ledger, fmt.Errorf("%w: empty transcript path at index %d", errTranscriptCoverage, fileIndex)
		}
		info, statErr := os.Stat(path)
		if statErr != nil {
			return chunks, ledger, fmt.Errorf("%w: cannot stat transcript %q: %v", errTranscriptCoverage, path, statErr)
		}
		if info.IsDir() {
			return chunks, ledger, fmt.Errorf("%w: transcript %q is a directory", errTranscriptCoverage, path)
		}
		file, openErr := os.Open(path)
		if openErr != nil {
			return chunks, ledger, fmt.Errorf("%w: cannot open transcript %q: %v", errTranscriptCoverage, path, openErr)
		}
		hasher := sha256.New()
		reader := bufio.NewReader(io.TeeReader(file, hasher))
		fileRecord := transcriptCoverageFile{Path: path, Bytes: info.Size()}
		var readBytes int64
		fileLines := 0
		chunkStartByte := int64(0)
		chunkStartLine := lineNumber + 1
		var chunkText strings.Builder
		chunkNumberedBytes := int64(0)
		flushChunk := func(endByte int64, endLine int) {
			if endByte == chunkStartByte && endLine == chunkStartLine-1 {
				return
			}
			record := transcriptCoverageChunk{
				FileIndex: fileIndex,
				Path:      path,
				StartByte: chunkStartByte,
				EndByte:   endByte,
				StartLine: chunkStartLine,
				EndLine:   endLine,
			}
			ledger.Chunks = append(ledger.Chunks, record)
			chunks = append(chunks, transcriptNumberedChunk{
				transcriptCoverageChunk: record,
				NumberedText:            chunkText.String(),
			})
			chunkText.Reset()
			chunkNumberedBytes = 0
			chunkStartByte = endByte
			chunkStartLine = endLine + 1
		}
		for {
			rawLine, readErr := reader.ReadString('\n')
			if len(rawLine) > 0 {
				readBytes += int64(len(rawLine))
				lineContent := strings.TrimSuffix(rawLine, "\n")
				lineContent = strings.TrimSuffix(lineContent, "\r")
				lineNumber++
				fileLines++
				if fileLines <= 8 {
					if id, cwd, timestamp := transcriptHeaderIdentity(lineContent); id != "" || cwd != "" {
						fileRecord.SessionID = id
						fileRecord.SessionCWD = cwd
						fileRecord.SessionTimestamp = timestamp
					}
				}
				numberedLine := fmt.Sprintf("%d: %s\n", lineNumber, lineContent)
				if int64(len(numberedLine)) > system1ReaderMaxNumberedChunkBytes {
					_ = file.Close()
					return chunks, ledger, fmt.Errorf(
						"%w: transcript %q line %d is %d numbered bytes, above the unchunkable single-line bound %d; evidence is never truncated",
						errTranscriptCoverage, path, lineNumber, len(numberedLine), system1ReaderMaxNumberedChunkBytes,
					)
				}
				if chunkNumberedBytes > 0 && chunkNumberedBytes+int64(len(numberedLine)) > chunkBytes {
					flushChunk(readBytes-int64(len(rawLine)), lineNumber-1)
				}
				chunkText.WriteString(numberedLine)
				chunkNumberedBytes += int64(len(numberedLine))
			}
			if readErr != nil {
				if errors.Is(readErr, io.EOF) {
					break
				}
				_ = file.Close()
				return chunks, ledger, fmt.Errorf("%w: failed reading transcript %q: %v", errTranscriptCoverage, path, readErr)
			}
		}
		_ = file.Close()
		flushChunk(readBytes, lineNumber)
		fileRecord.EofReached = readBytes == info.Size()
		fileRecord.Lines = fileLines
		fileRecord.SHA256 = hex.EncodeToString(hasher.Sum(nil))

		filenameID := transcriptFileNameSessionID(path)
		if fileRecord.SessionID != "" && filenameID != "" &&
			fileRecord.SessionID != filenameID &&
			!strings.Contains(filenameID, fileRecord.SessionID) &&
			!strings.Contains(fileRecord.SessionID, filenameID) {
			return chunks, ledger, fmt.Errorf(
				"%w: contradictory provenance in %q: filename encodes session id %q but the session header declares %q",
				errTranscriptCoverage, path, filenameID, fileRecord.SessionID,
			)
		}
		identity := fileRecord.SessionID
		if identity == "" {
			identity = filenameID
			fileRecord.SessionID = identity
		}
		if identity != "" {
			identities[identity] = struct{}{}
		}
		ledger.Files = append(ledger.Files, fileRecord)
		ledger.TotalBytes += fileRecord.Bytes
		ledger.TotalLines += fileLines
	}
	if len(identities) > 1 {
		return chunks, ledger, fmt.Errorf(
			"%w: contradictory provenance: continuation history declares multiple session identities: %s",
			errTranscriptCoverage, strings.Join(sortedKeys(identities), ", "),
		)
	}
	for identity := range identities {
		ledger.HeadSessionID = identity
	}
	for _, chunk := range chunks {
		ledger.NumberedBytes += int64(len(chunk.NumberedText))
	}
	if len(chunks) > system1ReaderMaxChunkExecutions {
		return chunks, ledger, fmt.Errorf(
			"%w: full history needs %d chunk executions, above the bounded reader capacity %d; evidence is never truncated and never silently dropped",
			errTranscriptCoverage, len(chunks), system1ReaderMaxChunkExecutions,
		)
	}
	ledger.Complete = len(ledger.transcriptCoverageProblems()) == 0
	if !ledger.Complete {
		return chunks, ledger, fmt.Errorf("%w: coverage ledger validation failed", errTranscriptCoverage)
	}
	return chunks, ledger, nil
}

// recordChunkExecution appends the durable execution record of one chunk range.
// The judge gate verifies these records so a skipped or failed chunk can never
// be presented as full history coverage.
func (ledger *transcriptCoverageLedger) recordChunkExecution(execution transcriptChunkExecution) {
	ledger.ChunkExecutions = append(ledger.ChunkExecutions, execution)
}

// restrictedReaderDeniedEnvNames lists exact environment variables that carry
// shell-injection or worktree-override authority the reader must never hold.
var restrictedReaderDeniedEnvNames = map[string]struct{}{
	"TMUX":                 {},
	"TMUX_PANE":            {},
	"BASH_ENV":             {},
	"ENV":                  {},
	"PROMPT_COMMAND":       {},
	"SHELLOPTS":            {},
	"PS4":                  {},
	"LD_PRELOAD":           {},
	"PYTHONSTARTUP":        {},
	"GIT_DIR":              {},
	"GIT_WORK_TREE":        {},
	"GIT_INDEX_FILE":       {},
	"GIT_OBJECT_DIRECTORY": {},
}

// restrictedReaderDeniedEnvPrefixes strips every control-plane authority the
// Fixer/Netrunner lifecycle injects. FIXER_DB_PATH (database authority) and
// FIXER_MCP_* (project Fixer MCP authority) live under FIXER_.
var restrictedReaderDeniedEnvPrefixes = []string{
	"FIXER_",
	"CODEX_",
	"NETRUNNER_",
	"OVERSEER_",
	"HANDS_",
}

// restrictedReaderEnvDeny reports whether one environment variable name is
// denied for the isolated reader: control-plane authority, any MCP
// configuration/authority, or a shell-injection/worktree-override vector.
func restrictedReaderEnvDeny(name string) bool {
	upper := strings.ToUpper(name)
	if _, denied := restrictedReaderDeniedEnvNames[upper]; denied {
		return true
	}
	for _, prefix := range restrictedReaderDeniedEnvPrefixes {
		if strings.HasPrefix(upper, prefix) {
			return true
		}
	}
	return strings.Contains(upper, "MCP")
}

// restrictedReaderEnv builds the isolated reader environment and returns the
// denied variable NAMES (never values) for durable run metadata.
func restrictedReaderEnv(baseEnv []string) ([]string, []string) {
	cleaned := make([]string, 0, len(baseEnv))
	denied := []string{}
	seenDenied := map[string]struct{}{}
	for _, entry := range baseEnv {
		name, _, found := strings.Cut(entry, "=")
		if !found {
			continue
		}
		if restrictedReaderEnvDeny(name) {
			if _, seen := seenDenied[name]; !seen {
				seenDenied[name] = struct{}{}
				denied = append(denied, name)
			}
			continue
		}
		cleaned = append(cleaned, entry)
	}
	sort.Strings(denied)
	return cleaned, denied
}

// restrictedReaderEvidenceAllowlist canonicalizes the exact evidence paths the
// reader may read (transcript and artifact paths derived before launch). It is
// the single source of the reader's read capability: anything outside this set
// is denied by the restricted evidence tool.
func restrictedReaderEvidenceAllowlist(paths []string) []string {
	allowed := []string{}
	seen := map[string]struct{}{}
	for _, rawPath := range paths {
		trimmed := strings.TrimSpace(rawPath)
		if trimmed == "" {
			continue
		}
		canonical := canonicalRestrictedReaderPath(trimmed)
		if _, exists := seen[canonical]; exists {
			continue
		}
		seen[canonical] = struct{}{}
		allowed = append(allowed, canonical)
	}
	sort.Strings(allowed)
	return allowed
}

func canonicalRestrictedReaderPath(candidate string) string {
	if resolved, err := filepath.EvalSymlinks(candidate); err == nil {
		absolute, absErr := filepath.Abs(resolved)
		if absErr == nil {
			return absolute
		}
		return resolved
	}
	absolute, absErr := filepath.Abs(candidate)
	if absErr == nil {
		return absolute
	}
	return candidate
}

// restrictedReaderPathAllowed reports whether one path is inside the exact
// registered evidence set. Comparison is canonical (symlinks resolved) and
// exact: no directory-prefix widening is allowed.
func restrictedReaderPathAllowed(candidate string, allowed []string) bool {
	if strings.TrimSpace(candidate) == "" {
		return false
	}
	target := canonicalRestrictedReaderPath(candidate)
	for _, entry := range allowed {
		if canonicalRestrictedReaderPath(entry) == target {
			return true
		}
	}
	return false
}

var (
	readerSecretBearerRE = regexp.MustCompile(`(?i)\b(bearer\s+)[A-Za-z0-9._\-/+=]{8,}`)
	readerSecretKeyRE    = regexp.MustCompile(`(?i)\b(sk-[A-Za-z0-9_\-]{8,}|[A-Fa-f0-9]{32,})\b`)
	readerSecretAssignRE = regexp.MustCompile(`(?i)\b(api[_-]?key|access[_-]?token|refresh[_-]?token|secret|password|authorization)(["']?\s*[:=]\s*["']?)[^\s"',;]+`)
)

// redactReaderSecrets masks credential-shaped substrings before any excerpt,
// partial overview, or error text is persisted. It is applied to every durable
// reader artifact field that can contain model or environment output.
func redactReaderSecrets(text string) string {
	redacted := readerSecretBearerRE.ReplaceAllString(text, "$1[redacted]")
	redacted = readerSecretKeyRE.ReplaceAllString(redacted, "[redacted]")
	return readerSecretAssignRE.ReplaceAllString(redacted, "$1$2[redacted]")
}

func boundedRedactedExcerpt(text string, limit int) string {
	if limit <= 0 {
		limit = restrictedReaderExcerptMaxByte
	}
	redacted := redactReaderSecrets(text)
	if len(redacted) > limit {
		redacted = redacted[:limit] + "...[excerpt clipped]"
	}
	return redacted
}

// restrictedReaderToolCall is one entry of the restricted tool-call ledger:
// what the reader tried to read, and whether the restricted evidence tool
// allowed it. It never carries file content beyond a bounded redacted excerpt.
type restrictedReaderToolCall struct {
	ToolName      string `json:"tool_name"`
	Path          string `json:"path,omitempty"`
	StartLine     int    `json:"start_line,omitempty"`
	LimitLines    int    `json:"limit_lines,omitempty"`
	IsError       bool   `json:"is_error,omitempty"`
	ResultExcerpt string `json:"result_excerpt,omitempty"`
}

// restrictedReaderRunMetadata is the durable, secret-safe record of one reader
// run: input hash and size, exact model, timing, exit state, stdout/stderr byte
// counts with bounded redacted excerpts, the partial overview recovered on
// timeout or error, the covered chunk range, and the restricted tool-call
// ledger. It never stores the prompt body, environment values, full argv, or
// credentials.
type restrictedReaderRunMetadata struct {
	RunId             string                     `json:"run_id"`
	Label             string                     `json:"label,omitempty"`
	Model             string                     `json:"model"`
	Provider          string                     `json:"provider,omitempty"`
	ReaderCwd         string                     `json:"reader_cwd"`
	AgentConfigSource string                     `json:"agent_config_source,omitempty"`
	StartedAt         string                     `json:"started_at"`
	FinishedAt        string                     `json:"finished_at"`
	DurationMs        int64                      `json:"duration_ms"`
	TimeoutMs         int64                      `json:"timeout_ms"`
	TimedOut          bool                       `json:"timed_out"`
	Canceled          bool                       `json:"canceled"`
	KilledOwnChild    bool                       `json:"killed_own_child"`
	ExitError         string                     `json:"exit_error,omitempty"`
	PromptBytes       int                        `json:"prompt_bytes"`
	PromptSHA256      string                     `json:"prompt_sha256"`
	ChunkIndex        int                        `json:"chunk_index,omitempty"`
	ChunkStartLine    int                        `json:"chunk_start_line,omitempty"`
	ChunkEndLine      int                        `json:"chunk_end_line,omitempty"`
	ChunkStartByte    int64                      `json:"chunk_start_byte,omitempty"`
	ChunkEndByte      int64                      `json:"chunk_end_byte,omitempty"`
	EvidencePaths     int                        `json:"evidence_paths,omitempty"`
	ArgvFlags         []string                   `json:"argv_flags,omitempty"`
	ToolCalls         []restrictedReaderToolCall `json:"tool_calls,omitempty"`
	StdoutBytes       int64                      `json:"stdout_bytes"`
	StderrBytes       int64                      `json:"stderr_bytes"`
	StdoutExcerpt     string                     `json:"stdout_excerpt,omitempty"`
	StderrExcerpt     string                     `json:"stderr_excerpt,omitempty"`
	PartialOverview   string                     `json:"partial_overview,omitempty"`
	DeniedEnvNames    []string                   `json:"denied_env_names"`
	Coverage          []string                   `json:"coverage_diagnostics,omitempty"`
}

// persistRestrictedReaderRunMetadata writes one run-metadata JSON file under
// <projectCWD>/.codex/system1_reader_runs/ and returns its path. The write is
// append-per-run (one immutable file per run), so run history stays durable
// without any database schema.
func persistRestrictedReaderRunMetadata(projectCWD string, metadata restrictedReaderRunMetadata) (string, error) {
	dir := filepath.Join(projectCWD, ".codex", restrictedReaderRunDirName)
	if err := os.MkdirAll(dir, 0o755); err != nil {
		return "", fmt.Errorf("failed to prepare reader run dir: %v", err)
	}
	path := filepath.Join(dir, metadata.RunId+".json")
	payload, err := json.MarshalIndent(metadata, "", "  ")
	if err != nil {
		return "", err
	}
	if err := os.WriteFile(path, payload, 0o644); err != nil {
		return "", fmt.Errorf("failed to persist reader run metadata: %v", err)
	}
	return path, nil
}

// readerInputFingerprint returns the byte size and SHA-256 of the reader
// input (prompt) for durable run metadata. The prompt body itself is never
// persisted.
func readerInputFingerprint(prompt string) (int, string) {
	sum := sha256.Sum256([]byte(prompt))
	return len(prompt), hex.EncodeToString(sum[:])
}

// restrictedReaderRunDir returns the isolated empty working directory for one
// reader run. The reader never runs in the project cwd, so project hooks,
// skills, extensions, and malicious project MCP configs are unreachable.
func restrictedReaderRunDir(projectCWD string, runID string) (string, error) {
	dir := filepath.Join(projectCWD, ".codex", restrictedReaderRunDirName, runID)
	if err := os.MkdirAll(dir, 0o755); err != nil {
		return "", fmt.Errorf("failed to prepare isolated reader cwd: %v", err)
	}
	return dir, nil
}
