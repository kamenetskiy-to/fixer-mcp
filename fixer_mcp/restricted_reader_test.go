package main

// Regression tests for the System1 restricted transcript reader: full original
// evidence coverage via sequential chunked executions with a deterministic
// ledger (including histories above 8MiB), fail-closed provenance, exact
// evidence-path read restriction, restricted launch environment (deny cases),
// and secret-safe durable run metadata. All fixtures are synthetic; no
// operator transcripts, paths, or secrets are used.

import (
	"context"
	"fmt"
	"os"
	"path/filepath"
	"regexp"
	"strings"
	"testing"
)

// buildRealStyleTranscriptPayload builds the real-style 218-line payload from
// the acceptance contract: lines 16..193 hold every command plus large UTF-8
// content, spread across multiple continuation attempts (two files), with the
// rest being plausible session traffic.
func buildRealStyleTranscriptPayload(externalSessionID string, projectCWD string) (string, string) {
	fileOne := []string{
		fmt.Sprintf(`{"type":"session","version":3,"id":%q,"timestamp":"2026-10-05T10-00-00.000Z","cwd":%q}`, externalSessionID, projectCWD),
		`{"type":"model_change","id":"mc1","timestamp":"2026-10-05T10-00-01.000Z","provider":"p","modelId":"m"}`,
		`{"type":"message","id":"u0","timestamp":"2026-10-05T10-00-02.000Z","message":{"role":"system","content":"preamble"}}`,
		`{"type":"message","id":"u1","timestamp":"2026-10-05T10-00-03.000Z","message":{"role":"user","content":"task one"}}`,
		`{"type":"message","id":"a1","timestamp":"2026-10-05T10-00-04.000Z","message":{"role":"assistant","content":"starting"}}`,
		`{"type":"message","id":"a2","timestamp":"2026-10-05T10-00-05.000Z","message":{"role":"assistant","content":"reading files"}}`,
		`{"type":"message","id":"a3","timestamp":"2026-10-05T10-00-06.000Z","message":{"role":"assistant","content":"plan"}}`,
		`{"type":"message","id":"a4","timestamp":"2026-10-05T10-00-07.000Z","message":{"role":"assistant","content":"step 1"}}`,
		`{"type":"message","id":"a5","timestamp":"2026-10-05T10-00-08.000Z","message":{"role":"assistant","content":"step 2"}}`,
		`{"type":"message","id":"a6","timestamp":"2026-10-05T10-00-09.000Z","message":{"role":"assistant","content":"step 3"}}`,
		`{"type":"message","id":"a7","timestamp":"2026-10-05T10-00-10.000Z","message":{"role":"assistant","content":"step 4"}}`,
		`{"type":"message","id":"a8","timestamp":"2026-10-05T10-00-11.000Z","message":{"role":"assistant","content":"step 5"}}`,
		`{"type":"message","id":"a9","timestamp":"2026-10-05T10-00-12.000Z","message":{"role":"assistant","content":"step 6"}}`,
		`{"type":"message","id":"a10","timestamp":"2026-10-05T10-00-13.000Z","message":{"role":"assistant","content":"step 7"}}`,
		`{"type":"message","id":"a11","timestamp":"2026-10-05T10-00-14.000Z","message":{"role":"assistant","content":"step 8"}}`,
	}
	// Lines 16..193 (1-indexed over the joined history) carry all commands and
	// the large UTF-8 content.
	largeUTF8 := strings.Repeat("команда-проверка-✅-длинная-строка-码检验-", 2000)
	for index := 0; index < 100; index++ {
		line := fmt.Sprintf(`{"type":"tool_call","id":"t%d","timestamp":"2026-10-05T10-01-%02d.000Z","tool":"bash","command":"go test ./... -run TestFeature%d"}`, index, index%60, index)
		if index == 50 {
			line = fmt.Sprintf(`{"type":"tool_call","id":"t%d","timestamp":"2026-10-05T10-01-50.000Z","tool":"bash","command":"echo %s"}`, index, largeUTF8)
		}
		fileOne = append(fileOne, line)
	}
	// fileOne now has 15 + 100 = 115 lines.
	fileTwo := []string{
		fmt.Sprintf(`{"type":"session","version":3,"id":%q,"timestamp":"2026-10-05T11-00-00.000Z","cwd":%q}`, externalSessionID, projectCWD),
		`{"type":"message","id":"u2","timestamp":"2026-10-05T11-00-01.000Z","message":{"role":"user","content":"continue after resume"}}`,
	}
	// Lines 118..193 of the joined history: the rest of the commands in the
	// resumed continuation attempt.
	for index := 100; index < 176; index++ {
		fileTwo = append(fileTwo, fmt.Sprintf(
			`{"type":"tool_call","id":"t%d","timestamp":"2026-10-05T11-01-00.000Z","tool":"bash","command":"go test ./... -run TestFeature%d"}`, index, index,
		))
	}
	// Lines 194..218 of the joined history: non-command wrap-up traffic.
	for index := 0; index < 25; index++ {
		fileTwo = append(fileTwo, fmt.Sprintf(`{"type":"message","id":"pad%d","timestamp":"2026-10-05T11-02-%02d.000Z","message":{"role":"assistant","content":"wrap-up %d"}}`, index, index%60, index))
	}
	return strings.Join(fileOne, "\n") + "\n", strings.Join(fileTwo, "\n") + "\n"
}

var numberedLineRE = regexp.MustCompile(`^(\d+): `)

// splitNumberedText splits one chunk's numbered reader input back into its
// numbered lines so tests can prove contiguous, unskipped coverage.
func splitNumberedText(text string) []string {
	lines := []string{}
	for _, line := range strings.Split(strings.TrimSuffix(text, "\n"), "\n") {
		if line != "" {
			lines = append(lines, line)
		}
	}
	return lines
}

// assertContiguousNumbering proves the numbered lines cover exactly
// expectedFirst..expectedLast with no skipped ranges.
func assertContiguousNumbering(t *testing.T, numberedLines []string, expectedFirst int, expectedLast int) {
	t.Helper()
	expected := expectedFirst
	for _, line := range numberedLines {
		match := numberedLineRE.FindStringSubmatch(line)
		if match == nil {
			t.Fatalf("numbered line without a number: %.60q", line)
		}
		var number int
		if _, err := fmt.Sscanf(match[1], "%d", &number); err != nil || number != expected {
			t.Fatalf("line numbering must be contiguous with no skipped ranges: want %d, got %q", expected, match[1])
		}
		expected++
	}
	if expected != expectedLast+1 {
		t.Fatalf("expected lines %d..%d covered, got %d..%d", expectedFirst, expectedLast, expectedFirst, expected-1)
	}
}

func TestReadTranscriptCoverageChunksCoversRealStyle218LinePayloadEndToEnd(t *testing.T) {
	dir := t.TempDir()
	projectCWD := filepath.Join(dir, "Project")
	externalSessionID := "real-style-session-1"
	fileOneBody, fileTwoBody := buildRealStyleTranscriptPayload(externalSessionID, projectCWD)
	pathOne := filepath.Join(dir, "2026-10-05T10-00-00-000Z_"+externalSessionID+".jsonl")
	pathTwo := filepath.Join(dir, "2026-10-05T11-00-00-000Z_"+externalSessionID+".jsonl")
	if err := os.WriteFile(pathOne, []byte(fileOneBody), 0o644); err != nil {
		t.Fatalf("write continuation one: %v", err)
	}
	if err := os.WriteFile(pathTwo, []byte(fileTwoBody), 0o644); err != nil {
		t.Fatalf("write continuation two: %v", err)
	}

	// A small chunk bound forces multiple sequential chunk executions over the
	// real-style payload; the history must still be covered end to end.
	chunks, ledger, err := readTranscriptCoverageChunks([]string{pathOne, pathTwo}, 32*1024)
	if err != nil {
		t.Fatalf("full coverage read failed: %v", err)
	}
	if !ledger.Complete {
		t.Fatalf("expected a complete coverage ledger, got %+v", ledger)
	}
	if ledger.TotalLines != 218 {
		t.Fatalf("expected the real-style 218-line history to be covered, got %d lines", ledger.TotalLines)
	}
	if ledger.HeadSessionID != externalSessionID {
		t.Fatalf("expected session identity %q in the ledger, got %q", externalSessionID, ledger.HeadSessionID)
	}
	if problems := ledger.transcriptCoverageProblems(); len(problems) != 0 {
		t.Fatalf("expected no coverage gaps, got %v", problems)
	}
	if len(chunks) < 2 {
		t.Fatalf("expected multiple sequential chunks at a 32KiB bound, got %d", len(chunks))
	}

	// Every line is numbered exactly once with no skipped ranges: the numbered
	// sequence must be 1..218 strictly increasing, and every command must live
	// exactly in the 16..193 evidence window from the contract.
	numberedLines := []string{}
	for _, chunk := range chunks {
		numberedLines = append(numberedLines, splitNumberedText(chunk.NumberedText)...)
	}
	assertContiguousNumbering(t, numberedLines, 1, 218)

	seenCommands := 0
	seenToolLines := 0
	seenLargeUTF8 := false
	firstCommandLine := 218
	lastCommandLine := 0
	for _, line := range numberedLines {
		rest := line[strings.Index(line, ": ")+2:]
		if strings.Contains(rest, `"command":`) {
			seenToolLines++
			var number int
			fmt.Sscanf(numberedLineRE.FindStringSubmatch(line)[1], "%d", &number)
			if number < firstCommandLine {
				firstCommandLine = number
			}
			if number > lastCommandLine {
				lastCommandLine = number
			}
		}
		if strings.Contains(rest, `"command":"go test ./... -run TestFeature`) {
			seenCommands++
		}
		if strings.Contains(rest, "команда-проверка-✅") {
			seenLargeUTF8 = true
		}
	}
	if seenToolLines != 176 || firstCommandLine != 16 || lastCommandLine != 193 {
		t.Fatalf(
			"all 176 commands must survive in the 16..193 evidence window, got %d command lines spanning %d..%d",
			seenToolLines, firstCommandLine, lastCommandLine,
		)
	}
	if seenCommands != 175 {
		t.Fatalf("expected 175 go-test command lines plus the UTF-8 echo, got %d", seenCommands)
	}
	if !seenLargeUTF8 {
		t.Fatal("the large UTF-8 command line must reach the reader input intact")
	}
	// The tail must be present: no head/tail truncation.
	lastLine := numberedLines[len(numberedLines)-1]
	if !strings.HasPrefix(lastLine, "218: ") || !strings.Contains(lastLine, "wrap-up 24") {
		t.Fatalf("the end of the original history must be covered; last line: %.120q", lastLine)
	}
	if ledger.TotalBytes != int64(len(fileOneBody)+len(fileTwoBody)) {
		t.Fatalf("ledger byte accounting mismatch: %+v", ledger)
	}
}

// TestReadTranscriptCoverageChunksCoversHistoryOver8MiBEndToEnd is the >8MiB
// acceptance case: a history larger than the old whole-input bound must be
// actually read end to end through sequential chunk executions with correct
// coverage — never assembled into one bounded whole text and
// infrastructure-blocked, and never truncated.
func TestReadTranscriptCoverageChunksCoversHistoryOver8MiBEndToEnd(t *testing.T) {
	dir := t.TempDir()
	projectCWD := filepath.Join(dir, "Project")
	externalSessionID := "oversize-session-1"
	lines := []string{
		fmt.Sprintf(`{"type":"session","version":3,"id":%q,"timestamp":"2026-10-05T10-00-00.000Z","cwd":%q}`, externalSessionID, projectCWD),
	}
	totalTarget := 9 * 1024 * 1024
	written := len(lines[0]) + 1
	index := 0
	for written < totalTarget {
		line := fmt.Sprintf(`{"type":"tool_call","id":"t%d","timestamp":"2026-10-05T10-01-00.000Z","tool":"bash","command":"go test ./... -run TestFeature%d padding-padding-padding-padding-padding"}`, index, index)
		lines = append(lines, line)
		written += len(line) + 1
		index++
	}
	path := filepath.Join(dir, "2026-10-05T10-00-00-000Z_"+externalSessionID+".jsonl")
	if err := os.WriteFile(path, []byte(strings.Join(lines, "\n")+"\n"), 0o644); err != nil {
		t.Fatalf("write oversized fixture: %v", err)
	}
	if written <= 8*1024*1024 {
		t.Fatalf("fixture must exceed 8MiB, wrote %d bytes", written)
	}

	chunks, ledger, err := readTranscriptCoverageChunks([]string{path}, system1ReaderChunkBytes)
	if err != nil {
		t.Fatalf("a history above 8MiB must be read end to end via chunked execution, not blocked: %v", err)
	}
	if !ledger.Complete {
		t.Fatalf("expected complete coverage for the oversized history, got %+v", ledger)
	}
	if len(chunks) < 8 {
		t.Fatalf("expected the oversized history to be read in many sequential chunks, got %d", len(chunks))
	}
	if ledger.TotalLines != len(lines) {
		t.Fatalf("expected %d lines covered, got %d", len(lines), ledger.TotalLines)
	}
	if problems := ledger.transcriptCoverageProblems(); len(problems) != 0 {
		t.Fatalf("expected no coverage gaps in the oversized read, got %v", problems)
	}
	// Contiguity across the whole numbered sequence, verified per chunk.
	expectedNext := 1
	for chunkIndex, chunk := range chunks {
		numbered := splitNumberedText(chunk.NumberedText)
		assertContiguousNumbering(t, numbered, expectedNext, expectedNext+len(numbered)-1)
		expectedNext += len(numbered)
		if chunkIndex > 0 {
			previous := chunks[chunkIndex-1]
			if chunk.StartByte != previous.EndByte {
				t.Fatalf("byte coverage must be contiguous: chunk %d starts at %d, previous ended at %d", chunkIndex, chunk.StartByte, previous.EndByte)
			}
		}
	}
	if expectedNext != len(lines)+1 {
		t.Fatalf("numbered coverage does not reach the end of history: %d", expectedNext)
	}
	lastLine := splitNumberedText(chunks[len(chunks)-1].NumberedText)
	if !strings.HasPrefix(lastLine[len(lastLine)-1], fmt.Sprintf("%d: ", len(lines))) {
		t.Fatalf("the final line of the oversized history must be covered, got %.80q", lastLine[len(lastLine)-1])
	}
	if ledger.NumberedBytes <= 8*1024*1024 {
		t.Fatalf("the oversized fixture must exceed 8MiB of numbered evidence, got %d", ledger.NumberedBytes)
	}
}

func TestReadTranscriptCoverageChunksRejectsContradictoryHeaderProvenance(t *testing.T) {
	dir := t.TempDir()
	body := "{\"type\":\"session\",\"id\":\"other-session\",\"timestamp\":\"2026-10-05T10:00:00.000Z\",\"cwd\":\"/tmp/x\"}\n{\"type\":\"message\",\"id\":\"m1\",\"message\":{\"role\":\"user\",\"content\":\"hi\"}}\n"
	path := filepath.Join(dir, "2026-10-05T10-00-00-000Z_declared-session.jsonl")
	if err := os.WriteFile(path, []byte(body), 0o644); err != nil {
		t.Fatalf("write fixture: %v", err)
	}
	_, _, err := readTranscriptCoverageChunks([]string{path}, system1ReaderChunkBytes)
	if err == nil || !strings.Contains(err.Error(), "contradictory provenance") {
		t.Fatalf("expected a contradictory-provenance coverage failure, got %v", err)
	}
}

func TestReadTranscriptCoverageChunksRejectsContradictoryContinuationIdentity(t *testing.T) {
	dir := t.TempDir()
	pathOne := filepath.Join(dir, "2026-10-05T10-00-00-000Z_session-a.jsonl")
	pathTwo := filepath.Join(dir, "2026-10-05T11-00-00-000Z_session-b.jsonl")
	for _, path := range []string{pathOne, pathTwo} {
		if err := os.WriteFile(path, []byte("{\"type\":\"message\",\"id\":\"m1\",\"message\":{\"role\":\"user\",\"content\":\"hi\"}}\n"), 0o644); err != nil {
			t.Fatalf("write fixture: %v", err)
		}
	}
	_, _, err := readTranscriptCoverageChunks([]string{pathOne, pathTwo}, system1ReaderChunkBytes)
	if err == nil || !strings.Contains(err.Error(), "multiple session identities") {
		t.Fatalf("expected a cross-file identity contradiction, got %v", err)
	}
}

// TestReadTranscriptCoverageChunksNeverTruncatesAcrossManySmallChunks proves
// the replacement of the old fail-closed whole-input bound: an input larger
// than one chunk is fully covered by sequential chunks, with no skipped ranges
// and no truncation, instead of being infrastructure-blocked.
func TestReadTranscriptCoverageChunksNeverTruncatesAcrossManySmallChunks(t *testing.T) {
	dir := t.TempDir()
	path := filepath.Join(dir, "2026-10-05T10-00-00-000Z_session-a.jsonl")
	body := strings.Repeat("0123456789abcdef0123456789abcdef0123456789abcdef0123456789abcdef\n", 2000)
	if err := os.WriteFile(path, []byte(body), 0o644); err != nil {
		t.Fatalf("write fixture: %v", err)
	}
	chunks, ledger, err := readTranscriptCoverageChunks([]string{path}, 4096)
	if err != nil {
		t.Fatalf("chunked reading must cover oversized-by-chunk inputs instead of failing: %v", err)
	}
	if !ledger.Complete {
		t.Fatalf("expected complete coverage, got %+v", ledger)
	}
	if len(chunks) < 10 {
		t.Fatalf("expected many sequential chunks at a 4096-byte bound, got %d", len(chunks))
	}
	numberedLines := []string{}
	for _, chunk := range chunks {
		numberedLines = append(numberedLines, splitNumberedText(chunk.NumberedText)...)
	}
	assertContiguousNumbering(t, numberedLines, 1, 2000)
	if problems := ledger.chunkExecutionProblems(); len(problems) == 0 {
		t.Fatal("no executions have run yet; execution problems must report that")
	}
}

// TestReadTranscriptCoverageChunksRejectsUnchunkableOversizedLine keeps the
// fail-closed discipline where chunking is genuinely impossible: a single line
// above the unchunkable bound cannot be split across executions without losing
// line integrity, so it fails as infrastructure — never truncated.
func TestReadTranscriptCoverageChunksRejectsUnchunkableOversizedLine(t *testing.T) {
	dir := t.TempDir()
	path := filepath.Join(dir, "2026-10-05T10-00-00-000Z_session-a.jsonl")
	body := "first\n" + strings.Repeat("x", int(system1ReaderMaxNumberedChunkBytes)+128) + "\nthird\n"
	if err := os.WriteFile(path, []byte(body), 0o644); err != nil {
		t.Fatalf("write fixture: %v", err)
	}
	_, _, err := readTranscriptCoverageChunks([]string{path}, system1ReaderChunkBytes)
	if err == nil || !strings.Contains(err.Error(), "never truncated") || !strings.Contains(err.Error(), "single-line bound") {
		t.Fatalf("expected an unchunkable-oversized-line failure, got %v", err)
	}
}

// TestReadTranscriptCoverageChunksBoundedExecutionCapacity: the chunked reader
// has a documented capacity bound; beyond it the run fails as infrastructure
// without truncating or silently dropping ranges.
func TestReadTranscriptCoverageChunksBoundedExecutionCapacity(t *testing.T) {
	original := system1ReaderMaxChunkExecutions
	system1ReaderMaxChunkExecutions = 2
	defer func() { system1ReaderMaxChunkExecutions = original }()

	dir := t.TempDir()
	path := filepath.Join(dir, "2026-10-05T10-00-00-000Z_session-a.jsonl")
	body := strings.Repeat("0123456789abcdef0123456789abcdef0123456789abcdef0123456789abcdef\n", 200)
	if err := os.WriteFile(path, []byte(body), 0o644); err != nil {
		t.Fatalf("write fixture: %v", err)
	}
	_, _, err := readTranscriptCoverageChunks([]string{path}, 1024)
	if err == nil || !strings.Contains(err.Error(), "bounded reader capacity") {
		t.Fatalf("expected a bounded-capacity infrastructure failure, got %v", err)
	}
}

func TestReadTranscriptCoverageMissingFileIsACoverageFailure(t *testing.T) {
	_, _, err := readTranscriptCoverageChunks([]string{filepath.Join(t.TempDir(), "absent.jsonl")}, system1ReaderChunkBytes)
	if err == nil || !strings.Contains(err.Error(), "cannot stat transcript") {
		t.Fatalf("expected a missing-file coverage failure, got %v", err)
	}
}

func TestTranscriptCoverageLedgerDetectsTamperedGaps(t *testing.T) {
	dir := t.TempDir()
	path := filepath.Join(dir, "2026-10-05T10-00-00-000Z_session-a.jsonl")
	if err := os.WriteFile(path, []byte("first\nsecond\nthird\n"), 0o644); err != nil {
		t.Fatalf("write fixture: %v", err)
	}
	_, ledger, err := readTranscriptCoverageChunks([]string{path}, system1ReaderChunkBytes)
	if err != nil {
		t.Fatalf("coverage read failed: %v", err)
	}
	// Tamper: pretend a byte range was never covered.
	ledger.Chunks[0].StartByte = 3
	if problems := ledger.transcriptCoverageProblems(); len(problems) == 0 {
		t.Fatal("expected the ledger validator to detect a byte coverage gap")
	}
}

// TestChunkExecutionProblemsDetectsMissingAndFailedExecutions proves the
// verified-read side of the ledger: the judge gate must refuse any history
// whose chunk ranges were not all actually read exactly once.
func TestChunkExecutionProblemsDetectsMissingAndFailedExecutions(t *testing.T) {
	dir := t.TempDir()
	path := filepath.Join(dir, "2026-10-05T10-00-00-000Z_session-a.jsonl")
	body := strings.Repeat("0123456789abcdef0123456789abcdef0123456789abcdef0123456789abcdef\n", 100)
	if err := os.WriteFile(path, []byte(body), 0o644); err != nil {
		t.Fatalf("write fixture: %v", err)
	}
	chunks, ledger, err := readTranscriptCoverageChunks([]string{path}, 2048)
	if err != nil {
		t.Fatalf("coverage read failed: %v", err)
	}
	if len(chunks) < 3 {
		t.Fatalf("expected multiple chunks, got %d", len(chunks))
	}

	// Missing executions are detected.
	if problems := ledger.chunkExecutionProblems(); len(problems) == 0 {
		t.Fatal("expected problems when no chunk was executed")
	}

	// One failed execution among executed ones is detected.
	for index, chunk := range chunks {
		var runErr error
		if index == 1 {
			runErr = fmt.Errorf("reader exploded")
		}
		recordSystem1ChunkExecution(&ledger, index, chunk, "prompt-"+chunk.NumberedText, "observation", runErr)
	}
	if problems := ledger.chunkExecutionProblems(); len(problems) == 0 {
		t.Fatal("expected problems when one chunk execution failed")
	}

	// All executions successful: no problems.
	ledger.ChunkExecutions = nil
	for index, chunk := range chunks {
		recordSystem1ChunkExecution(&ledger, index, chunk, "prompt-"+chunk.NumberedText, "observation", nil)
	}
	if problems := ledger.chunkExecutionProblems(); len(problems) != 0 {
		t.Fatalf("expected no execution problems after full execution, got %v", problems)
	}

	// An execution that does not match its covered range is detected.
	ledger.ChunkExecutions[0].StartLine += 1
	if problems := ledger.chunkExecutionProblems(); len(problems) == 0 {
		t.Fatal("expected problems when an execution range diverges from the covered range")
	}
}

type recordedReaderRun struct {
	Label  string
	Prompt string
	Run    system1ReaderRun
}

// fakeChunkedReaderRecordsRuns swaps the reader seam with a recorder that
// returns a bounded observation for chunk/compaction runs and the final
// overview for overview/synthesis runs.
func fakeChunkedReaderRecordsRuns(t *testing.T, failOnLabel string, failErr error) *[]recordedReaderRun {
	t.Helper()
	runs := &[]recordedReaderRun{}
	original := system1ReaderExec
	t.Cleanup(func() { system1ReaderExec = original })
	system1ReaderExec = func(ctx context.Context, projectCWD string, run system1ReaderRun) (string, error) {
		*runs = append(*runs, recordedReaderRun{Label: run.Label, Prompt: run.Prompt, Run: run})
		if run.Label == failOnLabel {
			return "", failErr
		}
		if run.Label == "overview" || run.Label == "synthesis" {
			return "FINAL FACTUAL OVERVIEW: commands, files, tests, and claims-vs-observed recorded.", nil
		}
		return "OBSERVATION " + run.Label + ": commands and outcomes for the covered range.", nil
	}
	return runs
}

// TestExecuteSystem1ChunkedReaderReadsEveryRangeBeforeBoundedSynthesis proves
// the chunked execution contract: every covered chunk range is read by its own
// execution with its numbered lines, in order, and the bounded synthesis runs
// only afterwards with the recorded observations.
func TestExecuteSystem1ChunkedReaderReadsEveryRangeBeforeBoundedSynthesis(t *testing.T) {
	dir := t.TempDir()
	projectCWD := filepath.Join(dir, "Project")
	externalSessionID := "pipeline-session-1"
	fileOneBody, fileTwoBody := buildRealStyleTranscriptPayload(externalSessionID, projectCWD)
	pathOne := filepath.Join(dir, "2026-10-05T10-00-00-000Z_"+externalSessionID+".jsonl")
	pathTwo := filepath.Join(dir, "2026-10-05T11-00-00-000Z_"+externalSessionID+".jsonl")
	if err := os.WriteFile(pathOne, []byte(fileOneBody), 0o644); err != nil {
		t.Fatalf("write continuation one: %v", err)
	}
	if err := os.WriteFile(pathTwo, []byte(fileTwoBody), 0o644); err != nil {
		t.Fatalf("write continuation two: %v", err)
	}

	runs := fakeChunkedReaderRecordsRuns(t, "", nil)
	chunks, ledger, err := readTranscriptCoverageChunks([]string{pathOne, pathTwo}, 32*1024)
	if err != nil {
		t.Fatalf("coverage read failed: %v", err)
	}
	overview, err := executeSystem1ChunkedReader(context.Background(), projectCWD, system1ReaderModel, system1ChunkedReaderInput{
		FinalReport:     "worker claims everything works",
		TranscriptPaths: []string{pathOne, pathTwo},
		Chunks:          chunks,
		Ledger:          &ledger,
	})
	if err != nil {
		t.Fatalf("chunked reader failed: %v", err)
	}
	if !strings.HasPrefix(overview, "FINAL FACTUAL OVERVIEW") {
		t.Fatalf("expected the bounded final overview, got %q", overview)
	}

	// One chunk execution per covered range, in order, then one synthesis.
	if len(*runs) != len(chunks)+1 {
		t.Fatalf("expected %d chunk executions + 1 synthesis, got %d runs", len(chunks), len(*runs))
	}
	numberedLines := []string{}
	for index, chunk := range chunks {
		run := (*runs)[index]
		if run.Label != fmt.Sprintf("chunk-%d", index+1) {
			t.Fatalf("chunk runs must be sequential, got %q", run.Label)
		}
		if !strings.HasSuffix(run.Prompt, chunk.NumberedText) {
			t.Fatalf("chunk %d prompt must carry exactly its numbered range", index)
		}
		if !strings.Contains(run.Prompt, formatChunkRangeLabel(chunk.transcriptCoverageChunk)) {
			t.Fatalf("chunk %d prompt must state its covered range", index)
		}
		numberedLines = append(numberedLines, splitNumberedText(chunk.NumberedText)...)
	}
	// The chunk prompts together cover the whole history with no skipped ranges.
	assertContiguousNumbering(t, numberedLines, 1, 218)

	synthesis := (*runs)[len(*runs)-1]
	if synthesis.Label != "synthesis" {
		t.Fatalf("expected the synthesis run last, got %q", synthesis.Label)
	}
	for index := range chunks {
		if !strings.Contains(synthesis.Prompt, "OBSERVATION chunk-"+fmt.Sprint(index+1)) {
			t.Fatalf("synthesis must carry the observation of chunk %d", index+1)
		}
	}
	if !strings.Contains(synthesis.Prompt, "worker claims everything works") {
		t.Fatal("synthesis must carry the worker report as a claim")
	}
	if problems := ledger.chunkExecutionProblems(); len(problems) != 0 {
		t.Fatalf("expected every chunk execution recorded, got %v", problems)
	}
	if ledger.ChunkExecutions[len(ledger.ChunkExecutions)-1].Status != transcriptChunkExecutionStatusExecuted {
		t.Fatalf("all chunk executions must be recorded as executed: %+v", ledger.ChunkExecutions)
	}
}

// TestExecuteSystem1ChunkedReaderCoversHistoryOver8MiBThroughChunkExecutions:
// the >8MiB acceptance case end to end through the reader pipeline — the full
// history is actually read via chunk executions with correct coverage, and the
// bounded overview is produced only afterwards.
func TestExecuteSystem1ChunkedReaderCoversHistoryOver8MiBThroughChunkExecutions(t *testing.T) {
	dir := t.TempDir()
	projectCWD := filepath.Join(dir, "Project")
	externalSessionID := "oversize-pipeline-1"
	lines := []string{
		fmt.Sprintf(`{"type":"session","version":3,"id":%q,"timestamp":"2026-10-05T10-00-00.000Z","cwd":%q}`, externalSessionID, projectCWD),
	}
	totalTarget := 9 * 1024 * 1024
	written := len(lines[0]) + 1
	index := 0
	for written < totalTarget {
		line := fmt.Sprintf(`{"type":"tool_call","id":"t%d","timestamp":"2026-10-05T10-01-00.000Z","tool":"bash","command":"go test ./... -run TestFeature%d padding-padding-padding-padding-padding"}`, index, index)
		lines = append(lines, line)
		written += len(line) + 1
		index++
	}
	path := filepath.Join(dir, "2026-10-05T10-00-00-000Z_"+externalSessionID+".jsonl")
	if err := os.WriteFile(path, []byte(strings.Join(lines, "\n")+"\n"), 0o644); err != nil {
		t.Fatalf("write oversized fixture: %v", err)
	}

	runs := fakeChunkedReaderRecordsRuns(t, "", nil)
	chunks, ledger, err := readTranscriptCoverageChunks([]string{path}, system1ReaderChunkBytes)
	if err != nil {
		t.Fatalf("coverage read failed: %v", err)
	}
	overview, err := executeSystem1ChunkedReader(context.Background(), projectCWD, system1ReaderModel, system1ChunkedReaderInput{
		FinalReport:     "worker claims the oversized history was handled",
		TranscriptPaths: []string{path},
		Chunks:          chunks,
		Ledger:          &ledger,
	})
	if err != nil {
		t.Fatalf("an oversized history must be read via chunked execution, not blocked: %v", err)
	}
	if !strings.HasPrefix(overview, "FINAL FACTUAL OVERVIEW") {
		t.Fatalf("expected the bounded final overview, got %q", overview)
	}
	if len(chunks) < 8 {
		t.Fatalf("expected many chunks for >8MiB, got %d", len(chunks))
	}
	if len(*runs) < len(chunks)+1 {
		t.Fatalf("expected one run per chunk plus synthesis, got %d runs for %d chunks", len(*runs), len(chunks))
	}
	// Contiguous coverage of every numbered line across the executed chunks.
	expectedNext := 1
	for index, chunk := range chunks {
		numbered := splitNumberedText(chunk.NumberedText)
		assertContiguousNumbering(t, numbered, expectedNext, expectedNext+len(numbered)-1)
		expectedNext += len(numbered)
		if (*runs)[index].Label != fmt.Sprintf("chunk-%d", index+1) {
			t.Fatalf("chunk run %d out of order: %q", index, (*runs)[index].Label)
		}
	}
	if expectedNext != len(lines)+1 {
		t.Fatalf("the full oversized history must be read, stopped at line %d of %d", expectedNext-1, len(lines))
	}
	if problems := ledger.chunkExecutionProblems(); len(problems) != 0 {
		t.Fatalf("expected complete verified chunk execution coverage, got %v", problems)
	}
}

func TestExecuteSystem1ChunkedReaderSingleChunkProducesOverviewDirectly(t *testing.T) {
	dir := t.TempDir()
	path := filepath.Join(dir, "2026-10-05T10-00-00-000Z_session-a.jsonl")
	if err := os.WriteFile(path, []byte("first\nsecond\nthird\n"), 0o644); err != nil {
		t.Fatalf("write fixture: %v", err)
	}
	runs := fakeChunkedReaderRecordsRuns(t, "", nil)
	chunks, ledger, err := readTranscriptCoverageChunks([]string{path}, system1ReaderChunkBytes)
	if err != nil {
		t.Fatalf("coverage read failed: %v", err)
	}
	if len(chunks) != 1 {
		t.Fatalf("expected a single chunk, got %d", len(chunks))
	}
	overview, err := executeSystem1ChunkedReader(context.Background(), dir, system1ReaderModel, system1ChunkedReaderInput{
		FinalReport:     "claim",
		TranscriptPaths: []string{path},
		Chunks:          chunks,
		Ledger:          &ledger,
	})
	if err != nil {
		t.Fatalf("single-chunk read failed: %v", err)
	}
	if len(*runs) != 1 || (*runs)[0].Label != "overview" {
		t.Fatalf("expected exactly one overview execution, got %+v", *runs)
	}
	if !strings.Contains((*runs)[0].Prompt, "3: third") {
		t.Fatal("the single execution must read the whole numbered history")
	}
	if !strings.HasPrefix(overview, "FINAL FACTUAL OVERVIEW") {
		t.Fatalf("expected the final overview, got %q", overview)
	}
}

// TestExecuteSystem1ChunkedReaderCompactsObservationsWithinBound: the bounded
// synthesis input is kept within budget through further bounded reader
// executions over consecutive observation batches — never by silently dropping
// observations.
func TestExecuteSystem1ChunkedReaderCompactsObservationsWithinBound(t *testing.T) {
	dir := t.TempDir()
	path := filepath.Join(dir, "2026-10-05T10-00-00-000Z_session-a.jsonl")
	lines := []string{`{"type":"session","version":3,"id":"session-a","timestamp":"2026-10-05T10-00-00.000Z","cwd":"/tmp/p"}`}
	for index := 0; index < 4000; index++ {
		lines = append(lines, fmt.Sprintf(`{"type":"tool_call","id":"t%d","command":"go test ./... -run TestFeature%d"}`, index, index))
	}
	if err := os.WriteFile(path, []byte(strings.Join(lines, "\n")+"\n"), 0o644); err != nil {
		t.Fatalf("write fixture: %v", err)
	}

	runs := fakeChunkedReaderRecordsRuns(t, "", nil)
	original := system1ReaderExec
	system1ReaderExec = func(ctx context.Context, projectCWD string, run system1ReaderRun) (string, error) {
		*runs = append(*runs, recordedReaderRun{Label: run.Label, Prompt: run.Prompt, Run: run})
		if run.Label == "overview" || run.Label == "synthesis" {
			return "FINAL FACTUAL OVERVIEW: bounded.", nil
		}
		// Oversized observations force the compaction path.
		return strings.Repeat("observation detail ", 200), nil
	}
	t.Cleanup(func() { system1ReaderExec = original })

	chunks, ledger, err := readTranscriptCoverageChunks([]string{path}, 16*1024)
	if err != nil {
		t.Fatalf("coverage read failed: %v", err)
	}
	if len(chunks) < 20 {
		t.Fatalf("expected many chunks, got %d", len(chunks))
	}
	overview, err := executeSystem1ChunkedReader(context.Background(), dir, system1ReaderModel, system1ChunkedReaderInput{
		FinalReport:     "claim",
		TranscriptPaths: []string{path},
		Chunks:          chunks,
		Ledger:          &ledger,
	})
	if err != nil {
		t.Fatalf("chunked reader with compaction failed: %v", err)
	}
	if !strings.HasPrefix(overview, "FINAL FACTUAL OVERVIEW") {
		t.Fatalf("expected the bounded final overview, got %q", overview)
	}
	sawCompaction := false
	for _, run := range *runs {
		if strings.HasPrefix(run.Label, "compact-") {
			sawCompaction = true
		}
	}
	if !sawCompaction {
		t.Fatal("expected bounded compaction executions for oversized observations")
	}
	synthesis := (*runs)[len(*runs)-1]
	if synthesis.Label != "synthesis" {
		t.Fatalf("expected the synthesis run last, got %q", synthesis.Label)
	}
	if len(synthesis.Prompt) > system1ReaderSynthesisMaxBytes+system1ReaderObservationMaxBytes+8192 {
		t.Fatalf("the synthesis input must stay bounded, got %d bytes", len(synthesis.Prompt))
	}
}

// TestExecuteSystem1ChunkedReaderChunkFailureStopsWithFailedExecutionRecord:
// a failed chunk execution is an infrastructure condition with a durable
// failed record; later chunks and the synthesis never run.
func TestExecuteSystem1ChunkedReaderChunkFailureStopsWithFailedExecutionRecord(t *testing.T) {
	dir := t.TempDir()
	path := filepath.Join(dir, "2026-10-05T10-00-00-000Z_session-a.jsonl")
	lines := []string{`{"type":"session","version":3,"id":"session-a","timestamp":"2026-10-05T10-00-00.000Z","cwd":"/tmp/p"}`}
	for index := 0; index < 400; index++ {
		lines = append(lines, fmt.Sprintf(`{"type":"tool_call","id":"t%d","command":"go test ./... -run TestFeature%d"}`, index, index))
	}
	if err := os.WriteFile(path, []byte(strings.Join(lines, "\n")+"\n"), 0o644); err != nil {
		t.Fatalf("write fixture: %v", err)
	}

	runs := fakeChunkedReaderRecordsRuns(t, "chunk-2", fmt.Errorf("reader chunk exploded"))
	chunks, ledger, err := readTranscriptCoverageChunks([]string{path}, 16*1024)
	if err != nil {
		t.Fatalf("coverage read failed: %v", err)
	}
	if len(chunks) < 3 {
		t.Fatalf("expected at least three chunks, got %d", len(chunks))
	}
	_, runErr := executeSystem1ChunkedReader(context.Background(), dir, system1ReaderModel, system1ChunkedReaderInput{
		FinalReport:     "claim",
		TranscriptPaths: []string{path},
		Chunks:          chunks,
		Ledger:          &ledger,
	})
	if runErr == nil || !strings.Contains(runErr.Error(), "chunk 2") {
		t.Fatalf("expected the failing chunk to surface, got %v", runErr)
	}
	if len(*runs) != 2 {
		t.Fatalf("execution must stop at the failing chunk, got %d runs", len(*runs))
	}
	if len(ledger.ChunkExecutions) != 2 {
		t.Fatalf("expected exactly two recorded executions, got %+v", ledger.ChunkExecutions)
	}
	if ledger.ChunkExecutions[1].Status != transcriptChunkExecutionStatusFailed {
		t.Fatalf("the failed chunk must be recorded as failed, got %+v", ledger.ChunkExecutions[1])
	}
	if ledger.ChunkExecutions[1].Error == "" {
		t.Fatal("the failed chunk record must carry its bounded error")
	}
	if problems := ledger.chunkExecutionProblems(); len(problems) == 0 {
		t.Fatal("the ledger must report incomplete reading after a failed chunk")
	}
}

func TestRestrictedReaderEnvStripsAuthorityAndInjectionVectors(t *testing.T) {
	baseEnv := []string{
		"PATH=/usr/bin",
		"HOME=/home/op",
		"FIXER_DB_PATH=/home/op/live/fixer.db",
		"FIXER_MCP_LOCKED_ROLE=fixer",
		"FIXER_MCP_AUTO_AUTH=1",
		"CODEX_THREAD_ID=abc",
		"NETRUNNER_GATE=on",
		"OVERSEER_BRIDGE=x",
		"HANDS_INBOX=y",
		"SQLITE_MCP_CONFIG=/proj/sqliteMCP.toml",
		"WEB_MCP_CONFIG=/proj/webMCP.toml",
		"MCP_PROJECT_APPROVALS=1",
		"BASH_ENV=/proj/evil.sh",
		"ENV=/proj/evil.sh",
		"PROMPT_COMMAND=rm -rf /",
		"LD_PRELOAD=/proj/evil.so",
		"GIT_DIR=/proj/.git",
		"GIT_WORK_TREE=/proj",
		"TMUX=/tmp/tmux-1/default,1,0",
	}
	cleaned, denied := restrictedReaderEnv(baseEnv)
	cleanedMap := envSliceToMap(cleaned)
	for _, name := range []string{
		"FIXER_DB_PATH",
		"FIXER_MCP_LOCKED_ROLE",
		"FIXER_MCP_AUTO_AUTH",
		"CODEX_THREAD_ID",
		"NETRUNNER_GATE",
		"OVERSEER_BRIDGE",
		"HANDS_INBOX",
		"SQLITE_MCP_CONFIG",
		"WEB_MCP_CONFIG",
		"MCP_PROJECT_APPROVALS",
		"BASH_ENV",
		"ENV",
		"PROMPT_COMMAND",
		"LD_PRELOAD",
		"GIT_DIR",
		"GIT_WORK_TREE",
		"TMUX",
	} {
		if _, present := cleanedMap[name]; present {
			t.Fatalf("reader env must not carry %s", name)
		}
		found := false
		for _, deniedName := range denied {
			if deniedName == name {
				found = true
			}
		}
		if !found {
			t.Fatalf("denied variable %s must be recorded by name in run metadata", name)
		}
	}
	if cleanedMap["PATH"] != "/usr/bin" || cleanedMap["HOME"] != "/home/op" {
		t.Fatalf("reader keeps its provider runtime configuration: %v", cleanedMap)
	}
}

func TestRedactReaderSecretsMasksCredentialShapes(t *testing.T) {
	secretToken := "fake-secret-token-val-123"
	secretKey := "abcd1234efgh5678"
	secretHex := strings.Repeat("deadbeef", 5)
	raw := fmt.Sprintf("%s: %s sk-%s api_key=%s and hex %s", "Authorization", "Bearer", secretToken, secretKey, secretHex)
	redacted := redactReaderSecrets(raw)
	for _, secret := range []string{secretToken, secretKey, secretHex} {
		if strings.Contains(redacted, secret) {
			t.Fatalf("secret %q must be redacted, got %q", secret, redacted)
		}
	}
}

// TestRestrictedReaderEvidenceAllowlistIsExactAndCanonical: the reader's read
// capability is exactly the registered evidence set — canonical (symlinks
// resolved) and exact, with no directory-prefix widening and no path tricks.
func TestRestrictedReaderEvidenceAllowlistIsExactAndCanonical(t *testing.T) {
	dir := t.TempDir()
	transcript := filepath.Join(dir, "2026-10-05T10-00-00-000Z_session-a.jsonl")
	if err := os.WriteFile(transcript, []byte("x\n"), 0o644); err != nil {
		t.Fatalf("write evidence: %v", err)
	}
	artifact := filepath.Join(dir, "artifact.txt")
	if err := os.WriteFile(artifact, []byte("y\n"), 0o644); err != nil {
		t.Fatalf("write artifact: %v", err)
	}
	secret := filepath.Join(dir, "fixer.db")
	if err := os.WriteFile(secret, []byte("db\n"), 0o644); err != nil {
		t.Fatalf("write db: %v", err)
	}
	link := filepath.Join(dir, "link.jsonl")
	if err := os.Symlink(transcript, link); err != nil {
		t.Fatalf("symlink: %v", err)
	}

	allowed := restrictedReaderEvidenceAllowlist([]string{transcript, artifact, transcript, ""})
	if len(allowed) != 2 {
		t.Fatalf("expected two deduplicated evidence paths, got %v", allowed)
	}
	for _, good := range []string{transcript, link, artifact} {
		if !restrictedReaderPathAllowed(good, allowed) {
			t.Fatalf("evidence path %q must be readable", good)
		}
	}
	for _, bad := range []string{secret, dir, filepath.Join(dir, "other.jsonl"), "", "/etc/passwd", transcript + ".bak"} {
		if restrictedReaderPathAllowed(bad, allowed) {
			t.Fatalf("path %q must be denied: the allowlist is exact", bad)
		}
	}
}
