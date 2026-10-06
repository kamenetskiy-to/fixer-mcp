package main

import (
	"context"
	"database/sql"
	"encoding/json"
	"errors"
	"fmt"
	"math"
	"os"
	"path/filepath"
	"strings"
	"testing"
	"unicode/utf8"
)

// System1 first-stage review layer (contract system1-trial-0.1): the tests
// fake the managed launcher/judge so no provider process is ever spawned.
// Reader-output fixtures are sanitized reconstructions of the real cmd
// --output-format json JSONL shape; no operator-local transcripts, paths,
// secrets, or multi-megabyte artifacts are copied into the repo.

const system1TestPassJSON = `{"model":"typesafe/jev","answers":{"c1":{"type":"noul","noul":0.91},"c2":{"type":"noul","noul":0.82},"stronger_but_different":{"type":"noul","noul":0.1}}}`

const system1TestFailJSON = `{"model":"typesafe/jev","answers":{"c1":{"type":"noul","noul":0.3},"c2":{"type":"noul","noul":0.9},"stronger_but_different":{"type":"noul","noul":0.2}}}`

const system1TestStrongerJSON = `{"model":"typesafe/jev","answers":{"c1":{"type":"noul","noul":0.2},"c2":{"type":"noul","noul":0.3},"stronger_but_different":{"type":"noul","noul":0.88}}}`

func setupSystem1ReviewTest(t *testing.T) (string, *sql.DB) {
	t.Helper()
	originalDB, originalRole, originalProjectID := db, authorizedRole, authorizedProjectId
	originalReader, originalJudge := system1ReaderExec, system1JudgeExec
	originalCommandcodeRoot, originalPiRoot := commandcodeSessionTranscriptRoot, piSessionTranscriptRoot
	t.Cleanup(func() {
		db, authorizedRole, authorizedProjectId = originalDB, originalRole, originalProjectID
		system1ReaderExec, system1JudgeExec = originalReader, originalJudge
		commandcodeSessionTranscriptRoot, piSessionTranscriptRoot = originalCommandcodeRoot, originalPiRoot
	})

	repoDir := setupCleanGitRepo(t)
	// Transcript evidence is written to throwaway roots only: no test ever
	// touches the operator's real session stores.
	commandcodeRoot, piRoot := setupTemporaryHomeTranscriptRoots(t)
	commandcodeSessionTranscriptRoot = commandcodeRoot
	piSessionTranscriptRoot = piRoot
	testDB := setupParallelWaveTestDB(t, repoDir)
	t.Cleanup(func() { _ = testDB.Close() })
	db, authorizedRole, authorizedProjectId = testDB, "fixer", 1

	if _, err := testDB.Exec(`
		ALTER TABLE parallel_wave_worker ADD COLUMN system1_state TEXT NOT NULL DEFAULT '';
		ALTER TABLE parallel_wave_worker ADD COLUMN system1_checks_used INTEGER NOT NULL DEFAULT 0;
		CREATE TABLE wave_system1_packet (
			wave_id INTEGER PRIMARY KEY,
			project_id INTEGER NOT NULL,
			criteria_prompt TEXT NOT NULL,
			hard_ids TEXT NOT NULL DEFAULT '[]',
			threshold REAL NOT NULL DEFAULT 0.75,
			max_checks INTEGER NOT NULL DEFAULT 3,
			contract_version TEXT NOT NULL,
			created_at TEXT NOT NULL DEFAULT CURRENT_TIMESTAMP,
			updated_at TEXT NOT NULL DEFAULT CURRENT_TIMESTAMP
		);
		CREATE TABLE wave_system1_check (
			id INTEGER PRIMARY KEY AUTOINCREMENT,
			wave_id INTEGER NOT NULL,
			wave_worker_id INTEGER NOT NULL,
			project_id INTEGER NOT NULL,
			session_id INTEGER NOT NULL,
			local_session_id INTEGER NOT NULL,
			check_number INTEGER NOT NULL,
			check_id TEXT NOT NULL DEFAULT '',
			contract_version TEXT NOT NULL,
			verdict TEXT NOT NULL DEFAULT 'fail',
			overall_probability REAL NOT NULL DEFAULT 0,
			threshold REAL NOT NULL DEFAULT 0.75,
			escalated INTEGER NOT NULL DEFAULT 0,
			summary TEXT NOT NULL DEFAULT '',
			artifact_path TEXT NOT NULL DEFAULT '',
			created_at TEXT NOT NULL DEFAULT CURRENT_TIMESTAMP
		);
	`); err != nil {
		t.Fatalf("seed system1 schema: %v", err)
	}
	return repoDir, testDB
}

func system1TestPacket() *System1CheckInput {
	return &System1CheckInput{
		CriteriaPrompt:  "c1 (weight 0.6, hard): the claimed tests were actually run and pass.\nc2 (weight 0.4, soft): the final report matches the diff.",
		HardIds:         []string{"c1"},
		Threshold:       0.75,
		MaxChecks:       3,
		ContractVersion: system1TrialContractVersion,
	}
}

// stageSystem1WorkerTranscriptEvidence binds one staged worker to real
// transcript evidence on disk: a persisted external session id plus an
// original JSONL transcript under the throwaway commandcode store. Without
// provenance the System1 gate must fail closed as infrastructure, so every
// review-cycle test stages evidence explicitly.
func stageSystem1WorkerTranscriptEvidence(t *testing.T, testDB *sql.DB, wave NetrunnerWaveSnapshot, localSessionID int, externalSessionID string, transcriptBody string) string {
	t.Helper()
	globalSessionID, err := globalSessionIDFromProjectScoped(localSessionID, wave.ProjectId)
	if err != nil {
		t.Fatalf("resolve global session id for evidence: %v", err)
	}
	if _, err := testDB.Exec(`
		CREATE TABLE IF NOT EXISTS session_external_link (
			id INTEGER PRIMARY KEY AUTOINCREMENT,
			session_id INTEGER NOT NULL,
			backend TEXT NOT NULL,
			external_session_id TEXT NOT NULL,
			updated_at TEXT NOT NULL DEFAULT CURRENT_TIMESTAMP
		);
	`); err != nil {
		t.Fatalf("ensure session_external_link: %v", err)
	}
	if _, err := testDB.Exec("UPDATE session SET cli_backend = 'commandcode' WHERE id = ?", globalSessionID); err != nil {
		t.Fatalf("stage evidence backend: %v", err)
	}
	if _, err := testDB.Exec(
		"INSERT OR REPLACE INTO session_external_link (session_id, backend, external_session_id) VALUES (?, 'commandcode', ?)",
		globalSessionID,
		externalSessionID,
	); err != nil {
		t.Fatalf("stage evidence external id: %v", err)
	}
	path := filepath.Join(
		commandcodeSessionTranscriptRoot,
		commandcodeProjectTranscriptDirName(wave.ProjectCwd),
		externalSessionID+".jsonl",
	)
	if err := os.MkdirAll(filepath.Dir(path), 0o755); err != nil {
		t.Fatalf("mkdir evidence dir: %v", err)
	}
	if err := os.WriteFile(path, []byte(transcriptBody), 0o644); err != nil {
		t.Fatalf("write evidence transcript: %v", err)
	}
	return path
}

// system1TestTranscriptBody is a small sanitized transcript fixture: a real
// session header shape plus a couple of messages, with no operator data.
func system1TestTranscriptBody(externalSessionID string, projectCWD string) string {
	return strings.Join([]string{
		fmt.Sprintf(`{"type":"session","version":3,"id":%q,"timestamp":"2026-10-05T12:00:00.000Z","cwd":%q}`, externalSessionID, projectCWD),
		`{"type":"message","id":"m1","timestamp":"2026-10-05T12:00:01.000Z","message":{"role":"user","content":"implement and test"}}`,
		`{"type":"message","id":"m2","timestamp":"2026-10-05T12:00:02.000Z","message":{"role":"assistant","content":"implemented; go test ./... passed"}}`,
		"",
	}, "\n")
}

func stageSystem1ReviewReadyWorker(t *testing.T, testDB *sql.DB, waveID int, localSessionID int) NetrunnerWaveSnapshot {
	t.Helper()
	globalSessionID, err := globalSessionIDFromProjectScoped(localSessionID, 1)
	if err != nil {
		t.Fatalf("resolve global session id: %v", err)
	}
	if _, err := testDB.Exec(
		"UPDATE session SET status = 'review', report = ? WHERE id = ?",
		`{"files_changed":["fixer_mcp/x.go"],"commands_run":["go test ./..."],"checks_run":["go test ./..."],"blockers":[]}`,
		globalSessionID,
	); err != nil {
		t.Fatalf("stage session review state: %v", err)
	}
	result, err := testDB.Exec(
		"INSERT INTO worker_process (project_id, session_id, pid, launch_epoch, status) VALUES (1, ?, 424242, 0, 'running')",
		globalSessionID,
	)
	if err != nil {
		t.Fatalf("stage worker process row: %v", err)
	}
	processID, err := result.LastInsertId()
	if err != nil {
		t.Fatalf("worker process id: %v", err)
	}
	if _, err := testDB.Exec(
		"UPDATE parallel_wave_worker SET status = ?, worker_process_id = ? WHERE wave_id = ? AND session_id = ?",
		parallelWaveWorkerStatusReviewReady, processID, waveID, globalSessionID,
	); err != nil {
		t.Fatalf("stage review_ready worker: %v", err)
	}
	wave, err := fetchNetrunnerWaveSnapshot(waveID, 1)
	if err != nil {
		t.Fatalf("fetch staged wave: %v", err)
	}
	externalSessionID := fmt.Sprintf("sys1-transcript-%d", localSessionID)
	stageSystem1WorkerTranscriptEvidence(
		t,
		testDB,
		wave,
		localSessionID,
		externalSessionID,
		system1TestTranscriptBody(externalSessionID, wave.ProjectCwd),
	)
	for _, worker := range wave.Workers {
		if worker.SessionId == localSessionID {
			return wave
		}
	}
	t.Fatalf("staged worker %d not found in wave %d", localSessionID, waveID)
	return NetrunnerWaveSnapshot{}
}

func fakeSystem1Executor(t *testing.T, judgeOutput string, judgeErr error) *[]string {
	t.Helper()
	models := &[]string{}
	system1ReaderExec = func(ctx context.Context, projectCWD string, run system1ReaderRun) (string, error) {
		*models = append(*models, run.Model)
		return "FACTUAL OVERVIEW: transcript read; commands and outcomes listed without quality judgment.", nil
	}
	system1JudgeExec = func(ctx context.Context, projectCWD string, payload string) (string, error) {
		*models = append(*models, system1JevModel)
		if judgeErr != nil {
			return "", judgeErr
		}
		return judgeOutput, nil
	}
	return models
}

func fakeSystem1ReaderFailure(t *testing.T, readerErr error) *bool {
	t.Helper()
	judgeCalled := false
	system1ReaderExec = func(ctx context.Context, projectCWD string, run system1ReaderRun) (string, error) {
		return "", readerErr
	}
	system1JudgeExec = func(ctx context.Context, projectCWD string, payload string) (string, error) {
		judgeCalled = true
		return "", errors.New("judge must not run after a reader infrastructure failure")
	}
	return &judgeCalled
}

func TestNormalizeSystem1CheckInput(t *testing.T) {
	t.Run("missing packet fails closed", func(t *testing.T) {
		if _, err := normalizeSystem1CheckInput(nil); err == nil || !strings.Contains(err.Error(), "system1_check is required") {
			t.Fatalf("expected missing-packet rejection, got %v", err)
		}
	})

	t.Run("defaults and clamps", func(t *testing.T) {
		packet, err := normalizeSystem1CheckInput(&System1CheckInput{
			CriteriaPrompt:  "c1 (weight 1, hard)",
			ContractVersion: system1TrialContractVersion,
			MaxChecks:       99,
		})
		if err != nil {
			t.Fatalf("normalize valid packet: %v", err)
		}
		if packet.Threshold != system1DefaultThreshold {
			t.Fatalf("expected default threshold %v, got %v", system1DefaultThreshold, packet.Threshold)
		}
		if packet.MaxChecks != system1MaxChecks {
			t.Fatalf("expected max_checks clamped to %d, got %d", system1MaxChecks, packet.MaxChecks)
		}
		packet, err = normalizeSystem1CheckInput(&System1CheckInput{
			CriteriaPrompt:  "c1 (weight 1, hard)",
			ContractVersion: system1TrialContractVersion,
			MaxChecks:       -4,
		})
		if err != nil {
			t.Fatalf("normalize valid packet: %v", err)
		}
		if packet.MaxChecks != system1MinChecks {
			t.Fatalf("expected max_checks clamped to %d, got %d", system1MinChecks, packet.MaxChecks)
		}
	})

	t.Run("invalid content fails closed", func(t *testing.T) {
		if _, err := normalizeSystem1CheckInput(&System1CheckInput{CriteriaPrompt: "  ", ContractVersion: system1TrialContractVersion}); err == nil ||
			!strings.Contains(err.Error(), "criteria_prompt") {
			t.Fatalf("expected criteria_prompt rejection, got %v", err)
		}
		if _, err := normalizeSystem1CheckInput(&System1CheckInput{CriteriaPrompt: "c1", ContractVersion: "bogus"}); err == nil ||
			!strings.Contains(err.Error(), system1TrialContractVersion) {
			t.Fatalf("expected contract_version rejection, got %v", err)
		}
		if _, err := normalizeSystem1CheckInput(&System1CheckInput{CriteriaPrompt: "c1", ContractVersion: system1TrialContractVersion, Threshold: 1.5}); err == nil ||
			!strings.Contains(err.Error(), "threshold") {
			t.Fatalf("expected threshold rejection, got %v", err)
		}
	})
}

func TestSystem1InvalidCriteriaAreVisibleInputErrors(t *testing.T) {
	t.Run("duplicate criterion ids", func(t *testing.T) {
		_, err := normalizeSystem1CheckInput(&System1CheckInput{
			CriteriaPrompt:  "c1: first.\nc1: second.",
			ContractVersion: system1TrialContractVersion,
		})
		if err == nil || !strings.Contains(err.Error(), "duplicate criterion id") {
			t.Fatalf("duplicate criterion ids must be a visible input error, got %v", err)
		}
	})

	t.Run("reserved stronger_but_different criterion id", func(t *testing.T) {
		_, err := normalizeSystem1CheckInput(&System1CheckInput{
			CriteriaPrompt:  "stronger_but_different (weight 2): the work is stronger.",
			ContractVersion: system1TrialContractVersion,
		})
		if err == nil || !strings.Contains(err.Error(), "reserved") {
			t.Fatalf("the reserved stronger_but_different id must be rejected so it cannot overwrite the real question, got %v", err)
		}
		_, _, payloadErr := buildSystem1JevPayload(system1CheckPacket{
			CriteriaPrompt:  "stronger_but_different: the work is stronger.",
			Threshold:       system1DefaultThreshold,
			MaxChecks:       system1DefaultMaxChecks,
			ContractVersion: system1TrialContractVersion,
		}, "report", "overview")
		if payloadErr == nil || !strings.Contains(payloadErr.Error(), "reserved") {
			t.Fatalf("payload building must surface the reserved id error, got %v", payloadErr)
		}
	})

	t.Run("invalid weight is never silently altered", func(t *testing.T) {
		_, err := normalizeSystem1CheckInput(&System1CheckInput{
			CriteriaPrompt:  "c1 (weight heavy): the work is done.",
			ContractVersion: system1TrialContractVersion,
		})
		if err == nil || !strings.Contains(err.Error(), "invalid weight") {
			t.Fatalf("an invalid stated weight must be a visible input error, got %v", err)
		}
	})

	t.Run("hard_id must reference a parsed criterion", func(t *testing.T) {
		_, err := normalizeSystem1CheckInput(&System1CheckInput{
			CriteriaPrompt:  "c1: the work is done.",
			HardIds:         []string{"c2"},
			ContractVersion: system1TrialContractVersion,
		})
		if err == nil || !strings.Contains(err.Error(), "hard_ids") {
			t.Fatalf("an unparseable hard criterion must be a visible input error, got %v", err)
		}
	})

	t.Run("oversized criteria prompt", func(t *testing.T) {
		oversized := strings.Repeat("c1: the work is done and verified. ", 300)
		_, err := normalizeSystem1CheckInput(&System1CheckInput{
			CriteriaPrompt:  oversized,
			ContractVersion: system1TrialContractVersion,
		})
		if err == nil || !strings.Contains(err.Error(), "oversized") {
			t.Fatalf("an oversized criteria prompt must be a visible input error, got %v", err)
		}
	})

	t.Run("too many criteria for the judge question budget", func(t *testing.T) {
		var prompt strings.Builder
		for i := 0; i < 200; i++ {
			fmt.Fprintf(&prompt, "c%d: requirement number %d with some explanatory detail text.\n", i, i)
		}
		_, err := normalizeSystem1CheckInput(&System1CheckInput{
			CriteriaPrompt:  prompt.String(),
			ContractVersion: system1TrialContractVersion,
		})
		if err == nil || !strings.Contains(err.Error(), "oversized") {
			t.Fatalf("criteria that cannot fit the judge question budget must be a visible input error, got %v", err)
		}
	})
}

func TestSystem1CriteriaGuidanceIsNeverSilentlyDropped(t *testing.T) {
	packet, err := normalizeSystem1CheckInput(&System1CheckInput{
		CriteriaPrompt:  "c1 (weight 0.5, hard): wave admission no longer rejects a wave for a missing declared_write_scope, and a test shows that.\nc2 (weight 0.3, soft): the worker prompt no longer tells the worker to stay inside a predeclared file list.\nDo not treat a stronger but different removal as a failure of these criteria if the fence is actually gone.",
		HardIds:         []string{"c1", "c2"},
		ContractVersion: system1TrialContractVersion,
	})
	if err != nil {
		t.Fatalf("normalize packet with free-form guidance: %v", err)
	}
	payload, criteria, err := buildSystem1JevPayload(packet, "report", "overview")
	if err != nil {
		t.Fatalf("build payload with free-form guidance: %v", err)
	}
	if len(criteria) != 2 {
		t.Fatalf("expected two structured criteria, got %+v", criteria)
	}
	if !strings.Contains(payload, "Do not treat a stronger but different removal as a failure of these criteria if the fence is actually gone.") {
		t.Fatalf("free-form criteria guidance must travel verbatim to the judge, never go silently missing: %s", payload)
	}
}

func TestDecideSystem1PassUsesContractRuleNotJudgeClaim(t *testing.T) {
	packet := system1CheckPacket{Threshold: 0.75, HardIDs: []string{"c1"}}
	lyingJudge := System1Verdict{
		ContractVersion:    system1TrialContractVersion,
		OverallProbability: 0.9,
		Verdict:            system1VerdictPass,
		Criteria:           []System1CriterionVerdict{{Id: "c1", Probability: 0.4}},
	}
	if decideSystem1Pass(lyingJudge, packet) {
		t.Fatal("hard criterion below 0.5 must force fail even when the judge claims pass")
	}
	solid := System1Verdict{
		ContractVersion:    system1TrialContractVersion,
		OverallProbability: 0.9,
		Verdict:            system1VerdictPass,
		Criteria:           []System1CriterionVerdict{{Id: "c1", Probability: 0.7}},
	}
	if !decideSystem1Pass(solid, packet) {
		t.Fatal("expected contract pass for overall >= threshold and hard criterion >= 0.5")
	}
	missingHard := System1Verdict{
		ContractVersion:    system1TrialContractVersion,
		OverallProbability: 0.9,
		Verdict:            system1VerdictPass,
		Criteria:           []System1CriterionVerdict{},
	}
	if decideSystem1Pass(missingHard, packet) {
		t.Fatal("a hard criterion missing from the verdict is unproven and must force fail")
	}
}

func TestSystem1CheckRequiredOnCreateAndLaunchTools(t *testing.T) {
	_, testDB := setupSystem1ReviewTest(t)

	if _, _, err := CreateNetrunnerWaveTool(context.Background(), nil, CreateNetrunnerWaveInput{SessionIds: []int{1}}); err == nil ||
		!strings.Contains(err.Error(), "system1_check is required") {
		t.Fatalf("create without system1_check must fail closed, got %v", err)
	}
	if _, _, err := CreateNetrunnerWaveTool(context.Background(), nil, CreateNetrunnerWaveInput{
		SessionIds:   []int{1},
		System1Check: &System1CheckInput{CriteriaPrompt: "c1", ContractVersion: "bogus"},
	}); err == nil || !strings.Contains(err.Error(), system1TrialContractVersion) {
		t.Fatalf("create with invalid system1_check must fail closed, got %v", err)
	}

	callResult, created, err := CreateNetrunnerWaveTool(context.Background(), nil, CreateNetrunnerWaveInput{
		SessionIds:   []int{1},
		System1Check: system1TestPacket(),
	})
	if err != nil || callResult != nil {
		t.Fatalf("create with system1_check must succeed: result=%+v err=%v", callResult, err)
	}
	if _, found, err := fetchSystem1Packet(created.WaveId); err != nil || !found {
		t.Fatalf("system1_check packet must persist on create: found=%t err=%v", found, err)
	}

	_, legacy, err := CreateNetrunnerWave(context.Background(), nil, CreateNetrunnerWaveInput{SessionIds: []int{2}})
	if err != nil {
		t.Fatalf("direct create keeps the legacy manual-review path: %v", err)
	}
	if _, found, err := fetchSystem1Packet(legacy.WaveId); err != nil || found {
		t.Fatalf("direct create without a packet must stay on the manual review path: found=%t err=%v", found, err)
	}
	if _, _, err := LaunchNetrunnerWaveTool(context.Background(), nil, LaunchNetrunnerWaveInput{WaveId: legacy.WaveId}); err == nil ||
		!strings.Contains(err.Error(), "system1_check is required to launch wave") {
		t.Fatalf("launch of a packet-less wave must fail closed, got %v", err)
	}

	var persistedPrompt string
	if err := testDB.QueryRow("SELECT criteria_prompt FROM wave_system1_packet WHERE wave_id = ?", created.WaveId).Scan(&persistedPrompt); err != nil {
		t.Fatalf("read persisted packet: %v", err)
	}
	if !strings.Contains(persistedPrompt, "c1") {
		t.Fatalf("unexpected persisted packet %q", persistedPrompt)
	}
}

func TestSystem1PassRecordsPassedAndKeepsWaveManualClose(t *testing.T) {
	repoDir, testDB := setupSystem1ReviewTest(t)
	models := fakeSystem1Executor(t, system1TestPassJSON, nil)

	_, created, err := CreateNetrunnerWaveTool(context.Background(), nil, CreateNetrunnerWaveInput{
		SessionIds:   []int{1},
		System1Check: system1TestPacket(),
	})
	if err != nil {
		t.Fatalf("create wave: %v", err)
	}
	wave := stageSystem1ReviewReadyWorker(t, testDB, created.WaveId, 1)
	worker := wave.Workers[0]

	updated, outcome, err := processSystem1ReviewForWorker(context.Background(), wave.ProjectCwd, wave, worker)
	if err != nil {
		t.Fatalf("system1 pass check: %v", err)
	}
	if outcome != system1OutcomePassed {
		t.Fatalf("expected pass outcome, got %q", outcome)
	}
	if updated.System1State != system1StatePassed || updated.System1ChecksUsed != 1 {
		t.Fatalf("expected system1_passed with one used check, got %+v", updated)
	}
	if updated.Status != parallelWaveWorkerStatusReviewReady {
		t.Fatalf("pass must leave the worker review_ready for the manual acceptance close, got %q", updated.Status)
	}
	refetched, err := fetchNetrunnerWaveSnapshot(created.WaveId, 1)
	if err != nil {
		t.Fatalf("refetch wave: %v", err)
	}
	if refetched.Status != wave.Status || refetched.Phase != wave.Phase {
		t.Fatalf("system1 must never complete the wave: before status=%q phase=%q after status=%q phase=%q", wave.Status, wave.Phase, refetched.Status, refetched.Phase)
	}

	var sessionStatus string
	if err := testDB.QueryRow("SELECT status FROM session WHERE id = 1").Scan(&sessionStatus); err != nil {
		t.Fatalf("read session: %v", err)
	}
	if sessionStatus != "review" {
		t.Fatalf("pass must not move the session, got %q", sessionStatus)
	}
	if _, err := os.Stat(mustSystem1ArtifactPath(t, repoDir, created.WaveId, 1, 1)); err != nil {
		t.Fatalf("expected system1 artifact: %v", err)
	}

	if len(*models) < 2 || (*models)[0] != system1ReaderModel || (*models)[1] != system1JevModel {
		t.Fatalf("expected reader then typesafe/jev, got %v", *models)
	}

	callResult, reviews, err := GetSystem1Reviews(context.Background(), nil, GetSystem1ReviewsInput{WaveId: created.WaveId})
	if err != nil || callResult != nil {
		t.Fatalf("get_system1_reviews: result=%+v err=%v", callResult, err)
	}
	if reviews.Contract == nil || reviews.Contract.ContractVersion != system1TrialContractVersion {
		t.Fatalf("expected the persisted packet in the read surface, got %+v", reviews.Contract)
	}
	if len(reviews.Checks) != 1 || reviews.Checks[0].Verdict != system1VerdictPass || reviews.Checks[0].CheckNumber != 1 {
		t.Fatalf("expected one recorded pass check, got %+v", reviews.Checks)
	}
	if len(reviews.Workers) != 1 || reviews.Workers[0].System1State != system1StatePassed {
		t.Fatalf("expected worker system1 state in the read surface, got %+v", reviews.Workers)
	}
}

func TestSystem1FailAppendsContinuationAndRequeuesWorker(t *testing.T) {
	_, testDB := setupSystem1ReviewTest(t)
	fakeSystem1Executor(t, system1TestFailJSON, nil)

	_, created, err := CreateNetrunnerWaveTool(context.Background(), nil, CreateNetrunnerWaveInput{
		SessionIds:   []int{1},
		System1Check: system1TestPacket(),
	})
	if err != nil {
		t.Fatalf("create wave: %v", err)
	}
	wave := stageSystem1ReviewReadyWorker(t, testDB, created.WaveId, 1)
	worker := wave.Workers[0]

	updated, outcome, err := processSystem1ReviewForWorker(context.Background(), wave.ProjectCwd, wave, worker)
	if err != nil {
		t.Fatalf("system1 fail check: %v", err)
	}
	if outcome != system1OutcomeRequeued {
		t.Fatalf("expected requeue outcome, got %q", outcome)
	}
	if updated.Status != parallelWaveWorkerStatusRetryWait {
		t.Fatalf("failed check must requeue the same worker to retry_wait, got %q", updated.Status)
	}
	if updated.WorkerProcessId != 0 {
		t.Fatalf("requeue must clear the stale worker_process_id, got %d", updated.WorkerProcessId)
	}
	if updated.System1ChecksUsed != 1 || updated.System1State != "" {
		t.Fatalf("expected one used check and undecided state, got %+v", updated)
	}

	var (
		sessionStatus   string
		taskDescription string
		reworkCount     int
	)
	if err := testDB.QueryRow("SELECT status, task_description, rework_count FROM session WHERE id = 1").Scan(&sessionStatus, &taskDescription, &reworkCount); err != nil {
		t.Fatalf("read session: %v", err)
	}
	if sessionStatus != "pending" {
		t.Fatalf("expected session review -> pending continuation, got %q", sessionStatus)
	}
	if reworkCount != 1 {
		t.Fatalf("expected the canonical rework transition to count one rework, got %d", reworkCount)
	}
	if !strings.Contains(taskDescription, "You have not passed the System1 check") {
		t.Fatalf("continuation text missing from task description: %q", taskDescription)
	}
	if !strings.Contains(taskDescription, "c1") || !strings.Contains(taskDescription, "looks satisfied") || !strings.Contains(taskDescription, "gap remains") {
		t.Fatalf("continuation must state satisfied criteria and remaining gaps: %q", taskDescription)
	}

	var storedProcessID sql.NullInt64
	if err := testDB.QueryRow("SELECT worker_process_id FROM parallel_wave_worker WHERE wave_id = ?", created.WaveId).Scan(&storedProcessID); err != nil {
		t.Fatalf("read worker row: %v", err)
	}
	if storedProcessID.Valid {
		t.Fatalf("expected NULL worker_process_id after requeue, got %v", storedProcessID.Int64)
	}

	_, reviews, err := GetSystem1Reviews(context.Background(), nil, GetSystem1ReviewsInput{WaveId: created.WaveId})
	if err != nil {
		t.Fatalf("get_system1_reviews: %v", err)
	}
	if len(reviews.Checks) != 1 || reviews.Checks[0].Verdict != system1VerdictFail {
		t.Fatalf("expected one recorded fail check, got %+v", reviews.Checks)
	}
}

func TestSystem1ThirdFailEscalatesWithoutRequeue(t *testing.T) {
	_, testDB := setupSystem1ReviewTest(t)
	fakeSystem1Executor(t, system1TestFailJSON, nil)

	_, created, err := CreateNetrunnerWaveTool(context.Background(), nil, CreateNetrunnerWaveInput{
		SessionIds:   []int{1},
		System1Check: system1TestPacket(),
	})
	if err != nil {
		t.Fatalf("create wave: %v", err)
	}
	wave := stageSystem1ReviewReadyWorker(t, testDB, created.WaveId, 1)
	if _, err := testDB.Exec(
		"UPDATE parallel_wave_worker SET system1_checks_used = 2 WHERE wave_id = ?",
		created.WaveId,
	); err != nil {
		t.Fatalf("stage two used checks: %v", err)
	}
	wave, err = fetchNetrunnerWaveSnapshot(created.WaveId, 1)
	if err != nil {
		t.Fatalf("refetch wave: %v", err)
	}
	worker := wave.Workers[0]

	updated, outcome, err := processSystem1ReviewForWorker(context.Background(), wave.ProjectCwd, wave, worker)
	if err != nil {
		t.Fatalf("system1 third check: %v", err)
	}
	if outcome != system1OutcomeEscalated {
		t.Fatalf("expected escalation on the third failed check, got %q", outcome)
	}
	if updated.System1State != system1StateEscalated || updated.System1ChecksUsed != 3 {
		t.Fatalf("expected escalated state with three used checks, got %+v", updated)
	}
	if updated.Status != parallelWaveWorkerStatusReviewReady {
		t.Fatalf("escalation must not requeue: worker must stay review_ready for Fixer second-stage review, got %q", updated.Status)
	}
	if updated.WorkerProcessId == 0 {
		t.Fatal("escalation must not clear the worker process linkage like a requeue does")
	}

	var sessionStatus string
	if err := testDB.QueryRow("SELECT status FROM session WHERE id = 1").Scan(&sessionStatus); err != nil {
		t.Fatalf("read session: %v", err)
	}
	if sessionStatus != "review" {
		t.Fatalf("escalation must not move the session, got %q", sessionStatus)
	}

	refetched, err := fetchNetrunnerWaveSnapshot(created.WaveId, 1)
	if err != nil {
		t.Fatalf("refetch wave: %v", err)
	}
	summary := buildNetrunnerWaveOperatorSummary(refetched)
	if summary.OperatorState != "system1_escalated" {
		t.Fatalf("operator summary must say system1_escalated, got %q", summary.OperatorState)
	}
	if summary.NextAction != "fixer_second_stage_review" {
		t.Fatalf("next action must be Fixer second-stage review, got %q", summary.NextAction)
	}

	_, reviews, err := GetSystem1Reviews(context.Background(), nil, GetSystem1ReviewsInput{WaveId: created.WaveId})
	if err != nil {
		t.Fatalf("get_system1_reviews: %v", err)
	}
	if len(reviews.Checks) != 1 || !reviews.Checks[0].Escalated {
		t.Fatalf("expected the final check row to be marked escalated, got %+v", reviews.Checks)
	}
}

func TestSystem1StrongerButDifferentEscalatesWithoutRequeue(t *testing.T) {
	_, testDB := setupSystem1ReviewTest(t)
	fakeSystem1Executor(t, system1TestStrongerJSON, nil)

	_, created, err := CreateNetrunnerWaveTool(context.Background(), nil, CreateNetrunnerWaveInput{
		SessionIds:   []int{1},
		System1Check: system1TestPacket(),
	})
	if err != nil {
		t.Fatalf("create wave: %v", err)
	}
	wave := stageSystem1ReviewReadyWorker(t, testDB, created.WaveId, 1)
	updated, outcome, err := processSystem1ReviewForWorker(context.Background(), wave.ProjectCwd, wave, wave.Workers[0])
	if err != nil {
		t.Fatalf("stronger-but-different check: %v", err)
	}
	if outcome != system1OutcomeEscalated || updated.System1State != system1StateEscalated {
		t.Fatalf("a stronger divergent delivery must go to the Fixer, got outcome=%q state=%q", outcome, updated.System1State)
	}
	if updated.Status == parallelWaveWorkerStatusRetryWait {
		t.Fatal("stronger-but-different must not requeue the worker onto the strict spec")
	}
	_, reviews, err := GetSystem1Reviews(context.Background(), nil, GetSystem1ReviewsInput{WaveId: created.WaveId})
	if err != nil {
		t.Fatalf("get_system1_reviews: %v", err)
	}
	if len(reviews.Checks) != 1 || !reviews.Checks[0].Escalated || !strings.Contains(reviews.Checks[0].Summary, "stronger") {
		t.Fatalf("expected an escalated stronger-but-different record, got %+v", reviews.Checks)
	}
}

func assertSystem1InfraFailureState(t *testing.T, testDB *sql.DB, updated NetrunnerWaveWorkerSnapshot) {
	t.Helper()
	if updated.Status != parallelWaveWorkerStatusReviewReady {
		t.Fatalf("an infrastructure failure must leave the worker review_ready, got %q", updated.Status)
	}
	if updated.System1ChecksUsed != 0 {
		t.Fatalf("an infrastructure failure must not consume the content-check budget, got %d used", updated.System1ChecksUsed)
	}
	if updated.System1State != "" {
		t.Fatalf("an infrastructure failure must not record a content verdict state, got %q", updated.System1State)
	}
	var (
		sessionStatus   string
		taskDescription string
		reworkCount     int
	)
	if err := testDB.QueryRow("SELECT status, task_description, rework_count FROM session WHERE id = 1").Scan(&sessionStatus, &taskDescription, &reworkCount); err != nil {
		t.Fatalf("read session: %v", err)
	}
	if sessionStatus != "review" {
		t.Fatalf("an infrastructure failure must not move the session, got %q", sessionStatus)
	}
	if reworkCount != 0 {
		t.Fatalf("an infrastructure failure must not count as rework, got %d", reworkCount)
	}
	if strings.Contains(taskDescription, "You have not passed the System1 check") {
		t.Fatalf("an infrastructure failure must never append 'fix your implementation' feedback, got %q", taskDescription)
	}
}

func TestSystem1ReaderFailureIsAnInfrastructureFailure(t *testing.T) {
	repoDir, testDB := setupSystem1ReviewTest(t)
	judgeCalled := fakeSystem1ReaderFailure(t, errors.New("managed system1 netrunner (xiaomi/mimo-v2.6-flash) exited with error: cmd not found"))

	_, created, err := CreateNetrunnerWaveTool(context.Background(), nil, CreateNetrunnerWaveInput{
		SessionIds:   []int{1},
		System1Check: system1TestPacket(),
	})
	if err != nil {
		t.Fatalf("create wave: %v", err)
	}
	wave := stageSystem1ReviewReadyWorker(t, testDB, created.WaveId, 1)

	updated, outcome, err := processSystem1ReviewForWorker(context.Background(), wave.ProjectCwd, wave, wave.Workers[0])
	if err != nil {
		t.Fatalf("reader infrastructure failure must not fail the wait loop: %v", err)
	}
	if outcome != system1OutcomeInfraFailed {
		t.Fatalf("expected infra_failed outcome, got %q", outcome)
	}
	if *judgeCalled {
		t.Fatal("the judge must never run on a reader infrastructure failure")
	}
	assertSystem1InfraFailureState(t, testDB, updated)

	_, reviews, err := GetSystem1Reviews(context.Background(), nil, GetSystem1ReviewsInput{WaveId: created.WaveId})
	if err != nil {
		t.Fatalf("get_system1_reviews: %v", err)
	}
	if len(reviews.Checks) != 1 || reviews.Checks[0].Verdict != system1VerdictInfraFailed {
		t.Fatalf("expected one visible infra_failed row, got %+v", reviews.Checks)
	}
	if !strings.Contains(reviews.Checks[0].Summary, "reader") {
		t.Fatalf("the infra row must carry the useful diagnostic, got %q", reviews.Checks[0].Summary)
	}
	if _, err := os.Stat(mustSystem1InfraArtifactPath(t, repoDir, created.WaveId, 1, 1, 1)); err != nil {
		t.Fatalf("expected a diagnostic infra artifact: %v", err)
	}
}

func TestSystem1JudgeExecutorErrorIsAnInfrastructureFailure(t *testing.T) {
	_, testDB := setupSystem1ReviewTest(t)
	// The wave 842 root-cause shape: typesafe/jev rejected an unbounded
	// payload. That is a judge infrastructure failure, not a content verdict.
	fakeSystem1Executor(t, "", errors.New(`typesafe/jev exited with error: Error: 400 {"error":{"message":"{\"error_type\":\"max_tokens_exceeded\"}"}}`))

	_, created, err := CreateNetrunnerWaveTool(context.Background(), nil, CreateNetrunnerWaveInput{
		SessionIds:   []int{1},
		System1Check: system1TestPacket(),
	})
	if err != nil {
		t.Fatalf("create wave: %v", err)
	}
	wave := stageSystem1ReviewReadyWorker(t, testDB, created.WaveId, 1)

	updated, outcome, err := processSystem1ReviewForWorker(context.Background(), wave.ProjectCwd, wave, wave.Workers[0])
	if err != nil {
		t.Fatalf("judge infrastructure failure must not fail the wait loop: %v", err)
	}
	if outcome != system1OutcomeInfraFailed {
		t.Fatalf("expected infra_failed outcome, got %q", outcome)
	}
	assertSystem1InfraFailureState(t, testDB, updated)

	_, reviews, err := GetSystem1Reviews(context.Background(), nil, GetSystem1ReviewsInput{WaveId: created.WaveId})
	if err != nil {
		t.Fatalf("get_system1_reviews: %v", err)
	}
	if len(reviews.Checks) != 1 || reviews.Checks[0].Verdict != system1VerdictInfraFailed {
		t.Fatalf("expected one visible infra_failed row, got %+v", reviews.Checks)
	}
	if !strings.Contains(reviews.Checks[0].Summary, "max_tokens_exceeded") {
		t.Fatalf("the infra row must carry the real diagnostic, got %q", reviews.Checks[0].Summary)
	}
}

func TestSystem1MalformedJudgeResponseIsAnInfrastructureFailure(t *testing.T) {
	_, testDB := setupSystem1ReviewTest(t)
	fakeSystem1Executor(t, "```\nnot valid json at all\n```", nil)

	_, created, err := CreateNetrunnerWaveTool(context.Background(), nil, CreateNetrunnerWaveInput{
		SessionIds:   []int{1},
		System1Check: system1TestPacket(),
	})
	if err != nil {
		t.Fatalf("create wave: %v", err)
	}
	wave := stageSystem1ReviewReadyWorker(t, testDB, created.WaveId, 1)

	updated, outcome, err := processSystem1ReviewForWorker(context.Background(), wave.ProjectCwd, wave, wave.Workers[0])
	if err != nil {
		t.Fatalf("malformed judge response must not fail the wait loop: %v", err)
	}
	if outcome != system1OutcomeInfraFailed {
		t.Fatalf("a parsing failure is an infrastructure failure, never a content check; got %q", outcome)
	}
	assertSystem1InfraFailureState(t, testDB, updated)

	_, reviews, err := GetSystem1Reviews(context.Background(), nil, GetSystem1ReviewsInput{WaveId: created.WaveId})
	if err != nil {
		t.Fatalf("get_system1_reviews: %v", err)
	}
	if len(reviews.Checks) != 1 || reviews.Checks[0].Verdict != system1VerdictInfraFailed {
		t.Fatalf("expected one visible infra_failed row, got %+v", reviews.Checks)
	}
	if !strings.Contains(reviews.Checks[0].Summary, "malformed judge JSON") {
		t.Fatalf("expected the malformed-JSON reason in the recorded summary, got %q", reviews.Checks[0].Summary)
	}
}

func TestSystem1MissingJevAnswerIsAnInfrastructureFailure(t *testing.T) {
	_, testDB := setupSystem1ReviewTest(t)
	// A well-formed response that answers only part of what was asked is
	// invalid: a missing noul must never be scored as a 0.0 content verdict.
	fakeSystem1Executor(t, `{"model":"typesafe/jev","answers":{"c1":{"type":"noul","noul":0.9},"stronger_but_different":{"type":"noul","noul":0.1}}}`, nil)

	_, created, err := CreateNetrunnerWaveTool(context.Background(), nil, CreateNetrunnerWaveInput{
		SessionIds:   []int{1},
		System1Check: system1TestPacket(),
	})
	if err != nil {
		t.Fatalf("create wave: %v", err)
	}
	wave := stageSystem1ReviewReadyWorker(t, testDB, created.WaveId, 1)

	updated, outcome, err := processSystem1ReviewForWorker(context.Background(), wave.ProjectCwd, wave, wave.Workers[0])
	if err != nil {
		t.Fatalf("missing judge answer must not fail the wait loop: %v", err)
	}
	if outcome != system1OutcomeInfraFailed {
		t.Fatalf("a missing answer is not a meaningful content verdict, got %q", outcome)
	}
	assertSystem1InfraFailureState(t, testDB, updated)

	_, reviews, err := GetSystem1Reviews(context.Background(), nil, GetSystem1ReviewsInput{WaveId: created.WaveId})
	if err != nil {
		t.Fatalf("get_system1_reviews: %v", err)
	}
	if len(reviews.Checks) != 1 || !strings.Contains(reviews.Checks[0].Summary, "missing noul answer") {
		t.Fatalf("expected the missing-answer diagnostic in the recorded row, got %+v", reviews.Checks)
	}
}

func TestSystem1InfrastructureRetriesAreBoundedThenEscalate(t *testing.T) {
	_, testDB := setupSystem1ReviewTest(t)
	fakeSystem1Executor(t, "", os.ErrDeadlineExceeded)

	_, created, err := CreateNetrunnerWaveTool(context.Background(), nil, CreateNetrunnerWaveInput{
		SessionIds:   []int{1},
		System1Check: system1TestPacket(),
	})
	if err != nil {
		t.Fatalf("create wave: %v", err)
	}
	wave := stageSystem1ReviewReadyWorker(t, testDB, created.WaveId, 1)

	for attempt := 1; attempt <= system1InfraMaxAttempts; attempt++ {
		wave, err = fetchNetrunnerWaveSnapshot(created.WaveId, 1)
		if err != nil {
			t.Fatalf("refetch wave: %v", err)
		}
		updated, outcome, err := processSystem1ReviewForWorker(context.Background(), wave.ProjectCwd, wave, wave.Workers[0])
		if err != nil {
			t.Fatalf("infra attempt %d: %v", attempt, err)
		}
		if outcome != system1OutcomeInfraFailed {
			t.Fatalf("attempt %d: expected infra_failed outcome, got %q", attempt, outcome)
		}
		if updated.System1ChecksUsed != 0 || updated.Status != parallelWaveWorkerStatusReviewReady {
			t.Fatalf("attempt %d: budget and worker must stay unchanged, got %+v", attempt, updated)
		}
		if attempt < system1InfraMaxAttempts && updated.System1State != "" {
			t.Fatalf("attempt %d: must not escalate before the bounded attempts are exhausted, got %q", attempt, updated.System1State)
		}
		if attempt == system1InfraMaxAttempts && updated.System1State != system1StateEscalated {
			t.Fatalf("attempt %d: expected escalation to Fixer second-stage review, got %q", attempt, updated.System1State)
		}
	}

	wave, err = fetchNetrunnerWaveSnapshot(created.WaveId, 1)
	if err != nil {
		t.Fatalf("refetch wave: %v", err)
	}
	_, outcome, err := processSystem1ReviewForWorker(context.Background(), wave.ProjectCwd, wave, wave.Workers[0])
	if err != nil {
		t.Fatalf("post-escalation call: %v", err)
	}
	if outcome != system1OutcomeSkipped {
		t.Fatalf("retries must stop after escalation (no tight infinite wait loop), got %q", outcome)
	}

	_, reviews, err := GetSystem1Reviews(context.Background(), nil, GetSystem1ReviewsInput{WaveId: created.WaveId})
	if err != nil {
		t.Fatalf("get_system1_reviews: %v", err)
	}
	if len(reviews.Checks) != system1InfraMaxAttempts {
		t.Fatalf("expected %d recorded infra attempts, got %+v", system1InfraMaxAttempts, reviews.Checks)
	}
	for _, check := range reviews.Checks {
		if check.Verdict != system1VerdictInfraFailed {
			t.Fatalf("every recorded row must be an infra row, got %+v", check)
		}
	}
	if !reviews.Checks[len(reviews.Checks)-1].Escalated {
		t.Fatalf("the final bounded attempt must be marked escalated, got %+v", reviews.Checks)
	}
}

func TestSystem1JevPayloadAsksNoulPerCriterion(t *testing.T) {
	packet, err := normalizeSystem1CheckInput(system1TestPacket())
	if err != nil {
		t.Fatalf("packet: %v", err)
	}
	payload, criteria, err := buildSystem1JevPayload(packet, "report", "overview")
	if err != nil {
		t.Fatalf("payload: %v", err)
	}
	if len(criteria) != 2 || criteria[0].ID != "c1" || criteria[0].Weight != 0.6 {
		t.Fatalf("criteria parse: %+v", criteria)
	}
	if !strings.Contains(payload, `"type":"noul"`) || !strings.Contains(payload, system1StrongerQuestionID) || !strings.Contains(payload, "c1") {
		t.Fatalf("jev payload missing noul questions: %s", payload)
	}
}

func TestSystem1JevPayloadBoundsSectionsAndWholeJSON(t *testing.T) {
	packet, err := normalizeSystem1CheckInput(system1TestPacket())
	if err != nil {
		t.Fatalf("packet: %v", err)
	}
	report := strings.Repeat("Доклад о проделанной работе содержит много текста и юникод. ", 500)
	overview := "OVERVIEW SENTINEL: commands executed; tests ran; outcomes observed."

	payload, _, err := buildSystem1JevPayload(packet, report, overview)
	if err != nil {
		t.Fatalf("payload: %v", err)
	}
	if len(payload) > system1JevPayloadMaxBytes {
		t.Fatalf("whole payload must be bounded, got %d bytes > %d", len(payload), system1JevPayloadMaxBytes)
	}

	var req jevRequest
	if err := json.Unmarshal([]byte(payload), &req); err != nil {
		t.Fatalf("payload must be a typed jev request: %v", err)
	}
	if !strings.Contains(req.State, overview) {
		t.Fatalf("a large report must not erase the factual overview: %q", req.State)
	}
	if !strings.Contains(req.State, "truncated") {
		t.Fatalf("every truncation must carry an explicit notice: %q", req.State)
	}
	if !utf8.ValidString(req.State) {
		t.Fatal("clipping must be UTF-8 safe")
	}
	overviewSection := req.State
	if _, after, ok := strings.Cut(req.State, "Factual transcript overview (evidence; wins over the report on conflict):\n"); ok {
		overviewSection, _, _ = strings.Cut(after, "\n\nFinal report (a claim, not evidence):\n")
	} else {
		t.Fatalf("overview section label missing: %q", req.State)
	}
	if len(overviewSection) > system1JevOverviewMaxBytes {
		t.Fatalf("overview section must respect its own budget, got %d bytes", len(overviewSection))
	}
	_, reportSection, ok := strings.Cut(req.State, "Final report (a claim, not evidence):\n")
	if !ok {
		t.Fatalf("report section label missing: %q", req.State)
	}
	if len(reportSection) > system1JevReportMaxBytes {
		t.Fatalf("report section must respect its own budget, got %d bytes", len(reportSection))
	}

	questionsJSON, err := json.Marshal(req.Questions)
	if err != nil {
		t.Fatalf("marshal questions: %v", err)
	}
	if len(questionsJSON) > system1JevQuestionsMaxBytes {
		t.Fatalf("questions must respect their own budget, got %d bytes", len(questionsJSON))
	}
	for _, id := range []string{"c1", "c2", system1StrongerQuestionID} {
		if _, ok := req.Questions[id]; !ok {
			t.Fatalf("criterion question %q must survive a large report, got %s", id, questionsJSON)
		}
	}
	if !strings.Contains(string(questionsJSON), "the claimed tests were actually run and pass") {
		t.Fatalf("criterion text must survive a large report: %s", questionsJSON)
	}
}

func TestSystem1JevPayloadShrinksEscapingBlowupWithinBound(t *testing.T) {
	packet, err := normalizeSystem1CheckInput(system1TestPacket())
	if err != nil {
		t.Fatalf("packet: %v", err)
	}
	// Worst-case escaping: quotes and newlines inflate ~3x when marshaled.
	overview := strings.Repeat("\"\n\\", 4000)
	report := strings.Repeat("\"\n\\", 2000)
	payload, _, err := buildSystem1JevPayload(packet, report, overview)
	if err != nil {
		t.Fatalf("payload: %v", err)
	}
	if len(payload) > system1JevPayloadMaxBytes {
		t.Fatalf("whole payload must stay bounded under escaping blowup, got %d bytes", len(payload))
	}
}

func TestClipSystem1TextIsUTF8SafeWithExplicitNotice(t *testing.T) {
	text := strings.Repeat("ю", 200)
	clipped := clipSystem1Text(text, 120, "test section")
	if !utf8.ValidString(clipped) {
		t.Fatal("clipping must not split a UTF-8 rune")
	}
	if len(clipped) > 120 {
		t.Fatalf("section limit must include the notice, got %d bytes", len(clipped))
	}
	if !strings.Contains(clipped, "truncated") {
		t.Fatalf("truncation must be explicit, got %q", clipped)
	}
	if !strings.HasPrefix(clipped, strings.Repeat("ю", 15)) {
		t.Fatalf("expected 15 whole runes before the notice, got %q", clipped)
	}

	if got := clipSystem1Runes("aaaююю", 4); got != "aaa" {
		t.Fatalf("rune-boundary clip: got %q", got)
	}
	if got := clipSystem1Text("short", 100, "x"); got != "short" {
		t.Fatalf("unclipped text must pass through untouched, got %q", got)
	}
}

func TestSystem1JevAnswerValidation(t *testing.T) {
	packet := system1CheckPacket{Threshold: 0.75, HardIDs: []string{"c1"}, ContractVersion: system1TrialContractVersion}
	criteria := []system1Criterion{
		{ID: "c1", Instructions: "one", Weight: 0.6},
		{ID: "c2", Instructions: "two", Weight: 0.4},
	}

	t.Run("valid answers score with locally deterministic weights", func(t *testing.T) {
		raw := `{"answers":{"c1":{"type":"noul","noul":0.9},"c2":{"type":"noul","noul":0.5},"stronger_but_different":{"type":"noul","noul":0.1}}}`
		verdict, err := verdictFromJev(raw, criteria, packet, "system1-w1-s1-c1")
		if err != nil {
			t.Fatalf("valid answers: %v", err)
		}
		if math.Abs(verdict.OverallProbability-0.74) > 1e-9 {
			t.Fatalf("deterministic weighted overall: got %v want 0.74", verdict.OverallProbability)
		}
		if verdict.StrongerButDifferent != 0.1 {
			t.Fatalf("stronger_but_different: got %v", verdict.StrongerButDifferent)
		}
	})

	t.Run("missing answer is not a default zero", func(t *testing.T) {
		raw := `{"answers":{"c1":{"type":"noul","noul":0.9},"stronger_but_different":{"type":"noul","noul":0.1}}}`
		if _, err := verdictFromJev(raw, criteria, packet, "c"); err == nil || !strings.Contains(err.Error(), "missing noul answer") {
			t.Fatalf("a missing answer must be an invalid judge response, got %v", err)
		}
	})

	t.Run("missing noul value is invalid", func(t *testing.T) {
		for _, raw := range []string{
			`{"answers":{"c1":{"type":"noul"},"c2":{"type":"noul","noul":0.5},"stronger_but_different":{"type":"noul","noul":0.1}}}`,
			`{"answers":{"c1":{"type":"noul","noul":null},"c2":{"type":"noul","noul":0.5},"stronger_but_different":{"type":"noul","noul":0.1}}}`,
		} {
			if _, err := verdictFromJev(raw, criteria, packet, "c"); err == nil || !strings.Contains(err.Error(), "no noul value") {
				t.Fatalf("a missing noul must never become 0.0, got %v for %s", err, raw)
			}
		}
	})

	t.Run("out of range or wrong type is invalid", func(t *testing.T) {
		raw := `{"answers":{"c1":{"type":"noul","noul":1.5},"c2":{"type":"noul","noul":0.5},"stronger_but_different":{"type":"noul","noul":0.1}}}`
		if _, err := verdictFromJev(raw, criteria, packet, "c"); err == nil || !strings.Contains(err.Error(), "[0, 1]") {
			t.Fatalf("noul out of [0,1] must be invalid, got %v", err)
		}
		raw = `{"answers":{"c1":{"type":"probability","noul":0.9},"c2":{"type":"noul","noul":0.5},"stronger_but_different":{"type":"noul","noul":0.1}}}`
		if _, err := verdictFromJev(raw, criteria, packet, "c"); err == nil || !strings.Contains(err.Error(), "want \"noul\"") {
			t.Fatalf("a wrong answer type must be invalid, got %v", err)
		}
	})

	t.Run("missing stronger answer is invalid", func(t *testing.T) {
		raw := `{"answers":{"c1":{"type":"noul","noul":0.9},"c2":{"type":"noul","noul":0.5}}}`
		if _, err := verdictFromJev(raw, criteria, packet, "c"); err == nil || !strings.Contains(err.Error(), system1StrongerQuestionID) {
			t.Fatalf("the stronger_but_different answer is required, got %v", err)
		}
	})

	t.Run("malformed JSON is invalid", func(t *testing.T) {
		if _, err := verdictFromJev("chatty prose without json", criteria, packet, "c"); err == nil || !strings.Contains(err.Error(), "malformed judge JSON") {
			t.Fatalf("non-JSON output must be invalid, got %v", err)
		}
	})
}

func TestSystem1ArtifactsAndVerdictJSONRoundTrip(t *testing.T) {
	verdictJSON := `{"contract_version":"system1-trial-0.1","check_id":"system1-w3-s9-c2","criteria":[{"id":"c1","probability":0.8,"evidence":["e"],"gap":""}],"overall_probability":0.8,"blocking":[],"verdict":"pass","summary":"ok"}`
	verdict, err := parseSystem1JudgeOutput(verdictJSON, "system1-w3-s9-c2", system1TrialContractVersion)
	if err != nil {
		t.Fatalf("parse strict judge JSON: %v", err)
	}
	if verdict.Verdict != system1VerdictPass || len(verdict.Criteria) != 1 {
		t.Fatalf("unexpected verdict round trip: %+v", verdict)
	}

	if _, err := parseSystem1JudgeOutput(`{"contract_version":"bogus","check_id":"","criteria":[],"overall_probability":0.9,"blocking":[],"verdict":"pass","summary":""}`, "x", system1TrialContractVersion); err == nil ||
		!strings.Contains(err.Error(), "contract_version") {
		t.Fatalf("contract_version mismatch must be malformed, got %v", err)
	}
	if _, err := parseSystem1JudgeOutput("chatty prose without json", "x", system1TrialContractVersion); err == nil ||
		!strings.Contains(err.Error(), "malformed judge JSON") {
		t.Fatalf("non-JSON output must be malformed, got %v", err)
	}

	payload, err := json.Marshal(system1Artifact{CheckId: "system1-w1-s1-c1", Passed: true, Outcome: string(system1OutcomePassed)})
	if err != nil {
		t.Fatalf("marshal artifact: %v", err)
	}
	if !strings.Contains(string(payload), "system1-w1-s1-c1") {
		t.Fatalf("unexpected artifact payload: %s", payload)
	}
}

func mustSystem1ArtifactPath(t *testing.T, projectCWD string, waveID int, localSessionID int, checkNumber int) string {
	t.Helper()
	path, err := system1ArtifactPath(projectCWD, waveID, localSessionID, checkNumber)
	if err != nil {
		t.Fatalf("resolve system1 artifact path: %v", err)
	}
	return path
}

func mustSystem1InfraArtifactPath(t *testing.T, projectCWD string, waveID int, localSessionID int, checkNumber int, infraAttempt int) string {
	t.Helper()
	path, err := system1InfraArtifactPath(projectCWD, waveID, localSessionID, checkNumber, infraAttempt)
	if err != nil {
		t.Fatalf("resolve system1 infra artifact path: %v", err)
	}
	return path
}

// TestSystem1MissingTranscriptProvenanceIsInfraFailed proves the fail-closed
// gate: without provable transcript provenance the judge never runs and no
// content verdict or budget is consumed.
func TestSystem1MissingTranscriptProvenanceIsInfraFailed(t *testing.T) {
	_, testDB := setupSystem1ReviewTest(t)
	judgeCalled := fakeSystem1ReaderFailure(t, fmt.Errorf("judge must not run"))

	_, created, err := CreateNetrunnerWaveTool(context.Background(), nil, CreateNetrunnerWaveInput{
		SessionIds:   []int{1},
		System1Check: system1TestPacket(),
	})
	if err != nil {
		t.Fatalf("create wave: %v", err)
	}
	wave := stageSystem1ReviewReadyWorker(t, testDB, created.WaveId, 1)
	// Remove the staged provenance to simulate the undetectable-transcript
	// failure mode.
	if _, err := testDB.Exec("DELETE FROM session_external_link"); err != nil {
		t.Fatalf("drop provenance: %v", err)
	}

	updated, outcome, err := processSystem1ReviewForWorker(context.Background(), wave.ProjectCwd, wave, wave.Workers[0])
	if err != nil {
		t.Fatalf("missing provenance must not fail the wait loop: %v", err)
	}
	if outcome != system1OutcomeInfraFailed {
		t.Fatalf("missing provenance must be an infrastructure failure, got %q", outcome)
	}
	if *judgeCalled {
		t.Fatal("the judge must never run without proven transcript evidence")
	}
	if updated.System1ChecksUsed != 0 {
		t.Fatalf("infrastructure failure must not consume the content-check budget, got %+v", updated)
	}
	assertSystem1InfraFailureState(t, testDB, updated)

	_, reviews, err := GetSystem1Reviews(context.Background(), nil, GetSystem1ReviewsInput{WaveId: created.WaveId})
	if err != nil {
		t.Fatalf("get_system1_reviews: %v", err)
	}
	if len(reviews.Checks) != 1 || !strings.Contains(reviews.Checks[0].Summary, "transcript provenance") {
		t.Fatalf("expected a provenance diagnostic in the recorded row, got %+v", reviews.Checks)
	}
}

// TestSystem1ContradictoryTranscriptProvenanceIsInfraFailed: a transcript whose
// session header contradicts its filename identity must never reach the judge.
func TestSystem1ContradictoryTranscriptProvenanceIsInfraFailed(t *testing.T) {
	_, testDB := setupSystem1ReviewTest(t)
	judgeCalled := fakeSystem1ReaderFailure(t, fmt.Errorf("judge must not run"))

	_, created, err := CreateNetrunnerWaveTool(context.Background(), nil, CreateNetrunnerWaveInput{
		SessionIds:   []int{1},
		System1Check: system1TestPacket(),
	})
	if err != nil {
		t.Fatalf("create wave: %v", err)
	}
	wave := stageSystem1ReviewReadyWorker(t, testDB, created.WaveId, 1)
	externalSessionID := "sys1-transcript-1"
	contradictory := "{\"type\":\"session\",\"id\":\"someone-else\",\"timestamp\":\"2026-10-05T12:00:00.000Z\",\"cwd\":\"/elsewhere\"}\n"
	transcriptPath := filepath.Join(
		commandcodeSessionTranscriptRoot,
		commandcodeProjectTranscriptDirName(wave.ProjectCwd),
		externalSessionID+".jsonl",
	)
	if err := os.WriteFile(transcriptPath, []byte(contradictory), 0o644); err != nil {
		t.Fatalf("overwrite with contradictory evidence: %v", err)
	}

	updated, outcome, err := processSystem1ReviewForWorker(context.Background(), wave.ProjectCwd, wave, wave.Workers[0])
	if err != nil {
		t.Fatalf("contradictory provenance must not fail the wait loop: %v", err)
	}
	if outcome != system1OutcomeInfraFailed {
		t.Fatalf("contradictory provenance must be an infrastructure failure, got %q", outcome)
	}
	if *judgeCalled {
		t.Fatal("the judge must never run on contradictory provenance")
	}
	if updated.System1ChecksUsed != 0 {
		t.Fatalf("infrastructure failure must not consume the content-check budget, got %+v", updated)
	}

	_, reviews, err := GetSystem1Reviews(context.Background(), nil, GetSystem1ReviewsInput{WaveId: created.WaveId})
	if err != nil {
		t.Fatalf("get_system1_reviews: %v", err)
	}
	if len(reviews.Checks) != 1 || !strings.Contains(reviews.Checks[0].Summary, "transcript coverage") {
		t.Fatalf("expected a coverage diagnostic in the recorded row, got %+v", reviews.Checks)
	}
}
