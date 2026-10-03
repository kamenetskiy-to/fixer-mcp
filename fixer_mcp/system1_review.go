package main

import (
	"bytes"
	"context"
	"database/sql"
	"encoding/json"
	"fmt"
	"log"
	"math"
	"os"
	"os/exec"
	"path/filepath"
	"regexp"
	"strings"
	"time"
	"unicode/utf8"

	"github.com/modelcontextprotocol/go-sdk/mcp"
)

// System1 first-stage review layer (contract system1-trial-0.1).
//
// A wave carries a system1_check packet; when a worker reaches review_ready the
// wait/review path runs a bounded two-stage check before the Fixer is asked to
// accept: a one-shot transcript reader (MiMo 2.6 Flash via cmd) produces a
// factual overview, then typesafe/jev answers one noul question per criterion.
// Pass is decided here (overall_probability >= threshold AND every hard
// criterion >= 0.5), not by the model. At most max_checks (clamped 1..3)
// checks run per worker; a failed
// check requeues the same worker with an appended continuation, and the final
// failed check escalates the worker to Fixer second-stage review.
//
// Technical failures of the check machinery itself (reader/CLI/API/parsing/
// context failures, invalid judge answers, oversized judge input) are
// infrastructure failures, never content verdicts: they do not consume the
// content-check budget, never append rework feedback or requeue the worker,
// and leave the worker review_ready. Each attempt is recorded as a
// verdict=infra_failed row plus a diagnostic artifact; after
// system1InfraMaxAttempts failed attempts the worker is escalated to Fixer
// second-stage review so retries stay bounded.

const (
	system1TrialContractVersion = "system1-trial-0.1"
	system1DefaultThreshold     = 0.75
	system1DefaultMaxChecks     = 3
	system1MinChecks            = 1
	system1MaxChecks            = 3

	system1JevModel    = "typesafe/jev"
	system1ReaderModel = "xiaomi/mimo-v2.6-flash"

	system1StatePassed    = "passed"
	system1StateEscalated = "escalated"

	system1VerdictPass        = "pass"
	system1VerdictFail        = "fail"
	system1VerdictInfraFailed = "infra_failed"

	system1ReviewToolName = "get_system1_reviews"

	system1ManagedExecTimeout = 10 * time.Minute
	system1TranscriptMaxBytes = 128 * 1024

	// A worker gets at most this many infrastructure attempts before the
	// failure is escalated to the Fixer instead of being retried.
	system1InfraMaxAttempts = 3

	// Judge payload budgets. Each section of the typed typesafe/jev payload is
	// bounded independently so a large report can never erase the factual
	// overview or the criterion questions; the whole JSON payload is bounded
	// on top of that. Criterion text is never clipped: oversized or invalid
	// criteria are visible input errors.
	system1CriteriaPromptMaxBytes = 8 * 1024
	system1JevOverviewMaxBytes    = 8 * 1024
	system1JevReportMaxBytes      = 4 * 1024
	system1JevQuestionsMaxBytes   = 8 * 1024
	system1JevPayloadMaxBytes     = 24 * 1024
)

const system1AnalystPrompt = "You are an independent transcript reader. Read the worker full session transcript and produce a FACTUAL overview of what actually happened. Do not judge quality. Sections: timeline with line references; commands actually executed and real outcomes; files actually modified; tests actually run and genuine results; errors, dead ends, reverts; a claims-vs-observed table against the worker final report (supported / partially / unsupported / not observable). Evidence over narration. If the transcript is missing or unreadable, say exactly what is missing. Stay under 1200 words."

type System1CheckInput struct {
	CriteriaPrompt  string   `json:"criteria_prompt" jsonschema:"Per-task System1 review instructions: criteria c1..cn with weight and hard|soft."`
	HardIds         []string `json:"hard_ids,omitempty" jsonschema:"Criterion ids that are hard: each must reach probability >= 0.5 or the check fails."`
	Threshold       float64  `json:"threshold,omitempty" jsonschema:"Overall pass threshold. Defaults to 0.75."`
	MaxChecks       int      `json:"max_checks,omitempty" jsonschema:"Maximum System1 checks per worker. Defaults to 3; clamped to 1..3."`
	ContractVersion string   `json:"contract_version" jsonschema:"System1 contract version; must be system1-trial-0.1."`
}

type system1CheckPacket struct {
	WaveID          int
	ProjectID       int
	CriteriaPrompt  string
	HardIDs         []string
	Threshold       float64
	MaxChecks       int
	ContractVersion string
}

type System1CriterionVerdict struct {
	Id          string   `json:"id"`
	Probability float64  `json:"probability"`
	Evidence    []string `json:"evidence"`
	Gap         string   `json:"gap"`
}

type System1Verdict struct {
	ContractVersion      string                    `json:"contract_version"`
	CheckId              string                    `json:"check_id"`
	Criteria             []System1CriterionVerdict `json:"criteria"`
	OverallProbability   float64                   `json:"overall_probability"`
	Blocking             []string                  `json:"blocking"`
	Verdict              string                    `json:"verdict"`
	Summary              string                    `json:"summary"`
	StrongerButDifferent float64                   `json:"stronger_but_different"`
	FixerReviewBecause   string                    `json:"fixer_review_because,omitempty"`
}

type System1CheckRecord struct {
	Id                 int     `json:"id"`
	WaveId             int     `json:"wave_id"`
	WaveWorkerId       int     `json:"wave_worker_id"`
	SessionId          int     `json:"session_id"`
	CheckNumber        int     `json:"check_number"`
	CheckId            string  `json:"check_id"`
	ContractVersion    string  `json:"contract_version"`
	Verdict            string  `json:"verdict"`
	OverallProbability float64 `json:"overall_probability"`
	Threshold          float64 `json:"threshold"`
	Escalated          bool    `json:"escalated"`
	Summary            string  `json:"summary"`
	ArtifactPath       string  `json:"artifact_path"`
	CreatedAt          string  `json:"created_at"`
}

type GetSystem1ReviewsInput struct {
	WaveId int `json:"wave_id" jsonschema:"Parallel wave ID to read the System1 packet and check history for."`
}

type System1ReviewWorkerState struct {
	SessionId         int    `json:"session_id"`
	Status            string `json:"status"`
	System1State      string `json:"system1_state"`
	System1ChecksUsed int    `json:"system1_checks_used"`
}

type GetSystem1ReviewsOutput struct {
	Status   string                     `json:"status"`
	WaveId   int                        `json:"wave_id"`
	Contract *System1CheckInput         `json:"contract,omitempty"`
	Checks   []System1CheckRecord       `json:"checks"`
	Workers  []System1ReviewWorkerState `json:"workers"`
}

type system1Outcome string

const (
	system1OutcomeSkipped     system1Outcome = "skipped"
	system1OutcomePassed      system1Outcome = "passed"
	system1OutcomeRequeued    system1Outcome = "requeued"
	system1OutcomeEscalated   system1Outcome = "escalated"
	system1OutcomeInfraFailed system1Outcome = "infra_failed"
)

type system1ExecutorFunc func(ctx context.Context, projectCWD string, model string, prompt string) (string, error)

// system1ReaderExec is the one-shot transcript reader (cmd + MiMo 2.6 Flash).
// system1JudgeExec is typesafe/jev: typed questions in, noul probabilities out.
// Tests swap either seam so no provider process is spawned.
var system1ReaderExec system1ExecutorFunc = launchSystem1ManagedNetrunner
var system1JudgeExec func(ctx context.Context, projectCWD string, payload string) (string, error) = launchSystem1Jev

const system1JudgeExecutorName = "typesafe/jev"

func normalizeSystem1CheckInput(input *System1CheckInput) (system1CheckPacket, error) {
	if input == nil {
		return system1CheckPacket{}, fmt.Errorf(
			"system1_check is required for new waves: pass {criteria_prompt, hard_ids, threshold (default 0.75), max_checks (default 3, clamped 1..3), contract_version: %q}",
			system1TrialContractVersion,
		)
	}
	criteriaPrompt := strings.TrimSpace(input.CriteriaPrompt)
	if criteriaPrompt == "" {
		return system1CheckPacket{}, fmt.Errorf("system1_check.criteria_prompt is required and must be non-empty")
	}
	contractVersion := strings.TrimSpace(input.ContractVersion)
	if contractVersion == "" {
		return system1CheckPacket{}, fmt.Errorf("system1_check.contract_version is required; this build implements %q", system1TrialContractVersion)
	}
	if contractVersion != system1TrialContractVersion {
		return system1CheckPacket{}, fmt.Errorf("unsupported system1_check.contract_version %q; this build implements %q", contractVersion, system1TrialContractVersion)
	}
	threshold := input.Threshold
	if threshold == 0 {
		threshold = system1DefaultThreshold
	}
	if threshold <= 0 || threshold > 1 {
		return system1CheckPacket{}, fmt.Errorf("system1_check.threshold must be in (0, 1]; got %v", input.Threshold)
	}
	maxChecks := input.MaxChecks
	if maxChecks == 0 {
		maxChecks = system1DefaultMaxChecks
	}
	if maxChecks < system1MinChecks {
		maxChecks = system1MinChecks
	}
	if maxChecks > system1MaxChecks {
		maxChecks = system1MaxChecks
	}
	hardIDs := make([]string, 0, len(input.HardIds))
	seen := make(map[string]struct{}, len(input.HardIds))
	for _, raw := range input.HardIds {
		id := strings.TrimSpace(raw)
		if id == "" {
			return system1CheckPacket{}, fmt.Errorf("system1_check.hard_ids must contain non-empty criterion ids")
		}
		if _, dup := seen[id]; dup {
			continue
		}
		seen[id] = struct{}{}
		hardIDs = append(hardIDs, id)
	}
	if len(criteriaPrompt) > system1CriteriaPromptMaxBytes {
		return system1CheckPacket{}, fmt.Errorf(
			"system1_check.criteria_prompt is oversized: %d bytes > %d bytes; oversized criteria are a visible input error, never silently clipped requirements",
			len(criteriaPrompt), system1CriteriaPromptMaxBytes,
		)
	}
	criteria, guidance, err := parseSystem1Criteria(criteriaPrompt)
	if err != nil {
		return system1CheckPacket{}, fmt.Errorf("system1_check.criteria_prompt is invalid: %v", err)
	}
	for _, hardID := range hardIDs {
		found := false
		for _, criterion := range criteria {
			if criterion.ID == hardID {
				found = true
				break
			}
		}
		if !found {
			return system1CheckPacket{}, fmt.Errorf(
				"system1_check.hard_ids references %q, which is not a criterion parsed from criteria_prompt; invalid criteria are a visible input error",
				hardID,
			)
		}
	}
	if _, err := buildSystem1JevQuestions(criteria, guidance); err != nil {
		return system1CheckPacket{}, fmt.Errorf("system1_check.criteria_prompt is invalid: %v", err)
	}
	return system1CheckPacket{
		CriteriaPrompt:  criteriaPrompt,
		HardIDs:         hardIDs,
		Threshold:       threshold,
		MaxChecks:       maxChecks,
		ContractVersion: contractVersion,
	}, nil
}

func system1PacketFromRow(packet system1CheckPacket) *System1CheckInput {
	return &System1CheckInput{
		CriteriaPrompt:  packet.CriteriaPrompt,
		HardIds:         packet.HardIDs,
		Threshold:       packet.Threshold,
		MaxChecks:       packet.MaxChecks,
		ContractVersion: packet.ContractVersion,
	}
}

func persistSystem1Packet(waveID int, projectID int, packet system1CheckPacket) error {
	hardPayload, err := json.Marshal(packet.HardIDs)
	if err != nil {
		return err
	}
	_, err = db.Exec(
		`INSERT INTO wave_system1_packet (
			wave_id, project_id, criteria_prompt, hard_ids, threshold, max_checks, contract_version
		) VALUES (?, ?, ?, ?, ?, ?, ?)
		ON CONFLICT(wave_id) DO UPDATE SET
			criteria_prompt = excluded.criteria_prompt,
			hard_ids = excluded.hard_ids,
			threshold = excluded.threshold,
			max_checks = excluded.max_checks,
			contract_version = excluded.contract_version,
			updated_at = CURRENT_TIMESTAMP`,
		waveID,
		projectID,
		packet.CriteriaPrompt,
		string(hardPayload),
		packet.Threshold,
		packet.MaxChecks,
		packet.ContractVersion,
	)
	return err
}

func fetchSystem1Packet(waveID int) (system1CheckPacket, bool, error) {
	if !dbTableExists("wave_system1_packet") {
		return system1CheckPacket{}, false, nil
	}
	var (
		packet      system1CheckPacket
		hardPayload string
	)
	err := db.QueryRow(
		`SELECT wave_id, project_id, criteria_prompt, hard_ids, threshold, max_checks, contract_version
		 FROM wave_system1_packet
		 WHERE wave_id = ?`,
		waveID,
	).Scan(&packet.WaveID, &packet.ProjectID, &packet.CriteriaPrompt, &hardPayload, &packet.Threshold, &packet.MaxChecks, &packet.ContractVersion)
	if err == sql.ErrNoRows {
		return system1CheckPacket{}, false, nil
	}
	if err != nil {
		return system1CheckPacket{}, false, err
	}
	packet.HardIDs = decodeParallelWaveStringList(hardPayload)
	return packet, true, nil
}

func system1CheckID(waveID int, localSessionID int, checkNumber int) string {
	return fmt.Sprintf("system1-w%d-s%d-c%d", waveID, localSessionID, checkNumber)
}

func system1ArtifactPath(projectCWD string, waveID int, localSessionID int, checkNumber int) (string, error) {
	dir := filepath.Join(projectCWD, ".codex", "netrunner_wave_artifacts", fmt.Sprintf("wave-%d", waveID))
	if err := os.MkdirAll(dir, 0o755); err != nil {
		return "", fmt.Errorf("failed to prepare wave artifact dir for system1 review: %v", err)
	}
	return filepath.Join(dir, fmt.Sprintf("session-%d-system1-%d.json", localSessionID, checkNumber)), nil
}

// system1InfraArtifactPath keeps infrastructure-failure artifacts separate from
// content-check artifacts so a later real check number can never overwrite the
// diagnostic record of an earlier failed attempt.
func system1InfraArtifactPath(projectCWD string, waveID int, localSessionID int, checkNumber int, infraAttempt int) (string, error) {
	dir := filepath.Join(projectCWD, ".codex", "netrunner_wave_artifacts", fmt.Sprintf("wave-%d", waveID))
	if err := os.MkdirAll(dir, 0o755); err != nil {
		return "", fmt.Errorf("failed to prepare wave artifact dir for system1 review: %v", err)
	}
	return filepath.Join(dir, fmt.Sprintf("session-%d-system1-%d-infra-%d.json", localSessionID, checkNumber, infraAttempt)), nil
}

type system1Artifact struct {
	CheckId         string             `json:"check_id"`
	ContractVersion string             `json:"contract_version"`
	WaveId          int                `json:"wave_id"`
	SessionId       int                `json:"session_id"`
	CheckNumber     int                `json:"check_number"`
	Packet          *System1CheckInput `json:"packet"`
	WorkerStatus    string             `json:"worker_status"`
	FinalReport     string             `json:"final_report"`
	TranscriptPath  string             `json:"transcript_path"`
	ReaderReport    string             `json:"reader_report"`
	JudgeRawOutput  string             `json:"judge_raw_output"`
	Verdict         *System1Verdict    `json:"verdict,omitempty"`
	Passed          bool               `json:"passed"`
	Outcome         string             `json:"outcome"`
	InfraAttempt    int                `json:"infra_attempt,omitempty"`
	InfraDiagnostic string             `json:"infra_diagnostic,omitempty"`
}

func persistSystem1CheckRecord(record System1CheckRecord, globalSessionID int) (int, error) {
	result, err := db.Exec(
		`INSERT INTO wave_system1_check (
			wave_id, wave_worker_id, project_id, session_id, local_session_id,
			check_number, check_id, contract_version, verdict, overall_probability,
			threshold, escalated, summary, artifact_path
		) VALUES (?, ?, ?, ?, ?, ?, ?, ?, ?, ?, ?, ?, ?, ?)`,
		record.WaveId,
		record.WaveWorkerId,
		authorizedProjectId,
		globalSessionID,
		record.SessionId,
		record.CheckNumber,
		record.CheckId,
		record.ContractVersion,
		record.Verdict,
		record.OverallProbability,
		record.Threshold,
		record.Escalated,
		record.Summary,
		record.ArtifactPath,
	)
	if err != nil {
		return 0, err
	}
	insertID, err := result.LastInsertId()
	if err != nil {
		return 0, err
	}
	return int(insertID), nil
}

const system1StrongerQuestionID = "stronger_but_different"

type system1Criterion struct {
	ID           string
	Instructions string
	Weight       float64
}

type jevQuestion struct {
	Type         string `json:"type"`
	Instructions string `json:"instructions"`
}

type jevRequest struct {
	State     string                 `json:"state"`
	Questions map[string]jevQuestion `json:"questions"`
}

type jevAnswer struct {
	Type string   `json:"type"`
	Noul *float64 `json:"noul"`
}

type jevResponse struct {
	Answers map[string]jevAnswer `json:"answers"`
}

var (
	system1CriterionLine = regexp.MustCompile(`(?m)^([A-Za-z0-9][A-Za-z0-9_.-]*)\s*(?:\(([^)]*)\))?\s*(?::\s*(.*))?$`)
	system1WeightPattern = regexp.MustCompile(`(?i)weight\s+([0-9]*\.?[0-9]+)`)
)

// parseSystem1Criteria splits the criteria prompt into structured criteria and
// free-form judging guidance. A criterion line is "id (meta): instructions";
// every other non-empty line is guidance that applies to all criteria and is
// preserved verbatim so no requirement can go silently missing. Invalid
// weights, duplicate ids, and the reserved stronger_but_different id are
// visible errors instead of silently altered requirements.
func parseSystem1Criteria(prompt string) ([]system1Criterion, []string, error) {
	var criteria []system1Criterion
	var guidance []string
	seen := make(map[string]struct{})
	for _, line := range strings.Split(prompt, "\n") {
		line = strings.TrimSpace(line)
		if line == "" {
			continue
		}
		match := system1CriterionLine.FindStringSubmatch(line)
		if match == nil {
			guidance = append(guidance, line)
			continue
		}
		id := match[1]
		if id == system1StrongerQuestionID {
			return nil, nil, fmt.Errorf(
				"criterion id %q is reserved for the stronger_but_different question and cannot be used as a criterion id",
				id,
			)
		}
		if _, dup := seen[id]; dup {
			return nil, nil, fmt.Errorf("duplicate criterion id %q", id)
		}
		seen[id] = struct{}{}
		weight := 1.0
		meta := match[2]
		if strings.Contains(strings.ToLower(meta), "weight") {
			found := system1WeightPattern.FindStringSubmatch(meta)
			if len(found) != 2 {
				return nil, nil, fmt.Errorf("criterion %q has an invalid weight in (%s): weights must be finite numbers > 0", id, meta)
			}
			parsed, err := parseSystem1Weight(found[1])
			if err != nil || parsed <= 0 || math.IsNaN(parsed) || math.IsInf(parsed, 0) {
				return nil, nil, fmt.Errorf("criterion %q has an invalid weight %q: weights must be finite numbers > 0", id, found[1])
			}
			weight = parsed
		}
		instructions := strings.TrimSpace(match[3])
		if instructions == "" {
			instructions = line
		}
		criteria = append(criteria, system1Criterion{ID: id, Instructions: instructions, Weight: weight})
	}
	if len(criteria) == 0 {
		return []system1Criterion{{ID: "overall", Instructions: strings.TrimSpace(prompt), Weight: 1}}, nil, nil
	}
	return criteria, guidance, nil
}

func parseSystem1Weight(raw string) (float64, error) {
	var value float64
	_, err := fmt.Sscanf(raw, "%f", &value)
	return value, err
}

// buildSystem1JevQuestions assembles the typed noul questions: one per
// criterion plus the stronger_but_different question. Free-form guidance from
// the criteria prompt travels verbatim with every question so it can never go
// silently missing. Criterion text is never clipped; questions that cannot fit
// the budget are a visible input error.
func buildSystem1JevQuestions(criteria []system1Criterion, guidance []string) (map[string]jevQuestion, error) {
	guidanceBlock := ""
	if guidanceText := strings.TrimSpace(strings.Join(guidance, "\n")); guidanceText != "" {
		guidanceBlock = "\n\nJudging guidance (applies to every requirement; it is not a separate requirement):\n" + guidanceText
	}
	questions := make(map[string]jevQuestion, len(criteria)+1)
	for _, criterion := range criteria {
		questions[criterion.ID] = jevQuestion{
			Type:         "noul",
			Instructions: "Is this requirement satisfied by the delivered work? Requirement: " + criterion.Instructions + guidanceBlock,
		}
	}
	questions[system1StrongerQuestionID] = jevQuestion{
		Type:         "noul",
		Instructions: "Is the delivered implementation objectively stronger than what the criteria asked for, even if it is a relatively radical departure from those criteria? Answer high only when the departure is real and the result is clearly better, not merely different." + guidanceBlock,
	}
	encoded, err := json.Marshal(questions)
	if err != nil {
		return nil, fmt.Errorf("failed to encode system1 criterion questions: %v", err)
	}
	if len(encoded) > system1JevQuestionsMaxBytes {
		return nil, fmt.Errorf(
			"system1 criteria are oversized: the criterion questions need %d bytes but the judge payload budget is %d bytes; refusing to silently clip or drop requirements",
			len(encoded), system1JevQuestionsMaxBytes,
		)
	}
	return questions, nil
}

func buildSystem1JevState(overview string, report string, overviewBudget int, reportBudget int) string {
	var state strings.Builder
	state.WriteString("Factual transcript overview (evidence; wins over the report on conflict):\n")
	state.WriteString(clipSystem1Text(overview, overviewBudget, "factual transcript overview"))
	state.WriteString("\n\nFinal report (a claim, not evidence):\n")
	state.WriteString(clipSystem1Text(report, reportBudget, "final report"))
	return state.String()
}

func marshalSystem1JevPayload(state string, questions map[string]jevQuestion) ([]byte, error) {
	return json.Marshal(jevRequest{State: state, Questions: questions})
}

// buildSystem1JevPayload assembles the typed typesafe/jev request that is sent
// to the judge on stdin. Only the final factual overview and the worker final
// report go into state — each clipped UTF-8-safely to its own budget with an
// explicit truncation notice — and the criteria travel as the noul questions.
// A large report can therefore never erase the evidence or the criteria, and
// the whole JSON payload (questions included) is bounded. Oversized or invalid
// criteria are visible input errors, never silently clipped requirements.
func buildSystem1JevPayload(packet system1CheckPacket, finalReport string, transcriptOverview string) (string, []system1Criterion, error) {
	criteria, guidance, err := parseSystem1Criteria(packet.CriteriaPrompt)
	if err != nil {
		return "", nil, err
	}
	questions, err := buildSystem1JevQuestions(criteria, guidance)
	if err != nil {
		return "", nil, err
	}
	overviewBudget := system1JevOverviewMaxBytes
	reportBudget := system1JevReportMaxBytes
	const minSystem1JevSectionBytes = 256
	for {
		state := buildSystem1JevState(transcriptOverview, finalReport, overviewBudget, reportBudget)
		payload, marshalErr := marshalSystem1JevPayload(state, questions)
		if marshalErr != nil {
			return "", nil, fmt.Errorf("failed to encode system1 judge payload: %v", marshalErr)
		}
		if len(payload) <= system1JevPayloadMaxBytes {
			return string(payload), criteria, nil
		}
		shrunk := false
		if overviewBudget > minSystem1JevSectionBytes && len(transcriptOverview) >= len(finalReport) {
			overviewBudget = maxInt(system1JevShrinkBudget(overviewBudget), minSystem1JevSectionBytes)
			shrunk = true
		} else if reportBudget > minSystem1JevSectionBytes {
			reportBudget = maxInt(system1JevShrinkBudget(reportBudget), minSystem1JevSectionBytes)
			shrunk = true
		} else if overviewBudget > minSystem1JevSectionBytes {
			overviewBudget = maxInt(system1JevShrinkBudget(overviewBudget), minSystem1JevSectionBytes)
			shrunk = true
		}
		if !shrunk {
			return "", nil, fmt.Errorf(
				"system1 judge payload is oversized: %d bytes > %d bytes even after section clipping; refusing to silently drop requirements",
				len(payload), system1JevPayloadMaxBytes,
			)
		}
	}
}

func system1JevShrinkBudget(budget int) int {
	return budget / 2
}

func maxInt(a int, b int) int {
	if a > b {
		return a
	}
	return b
}

// clipSystem1Text bounds one payload section to limit bytes including its
// truncation notice, cutting on a UTF-8 rune boundary. Every truncation is
// explicit; nothing is ever silently altered.
func clipSystem1Text(text string, limit int, label string) string {
	if limit <= 0 {
		return ""
	}
	if len(text) <= limit {
		return text
	}
	notice := fmt.Sprintf("\n...[truncated: %s clipped to %d bytes for the typesafe/jev payload budget]...", label, limit)
	if len(notice) >= limit {
		return clipSystem1Runes(text, limit)
	}
	return clipSystem1Runes(text, limit-len(notice)) + notice
}

// clipSystem1Runes cuts text to at most maxBytes without splitting a UTF-8 rune.
func clipSystem1Runes(text string, maxBytes int) string {
	if maxBytes <= 0 {
		return ""
	}
	if len(text) <= maxBytes {
		return text
	}
	end := maxBytes
	for end > 0 && !utf8.RuneStart(text[end]) {
		end--
	}
	return text[:end]
}

// system1NoulAnswer extracts one required noul answer. A missing answer, a
// missing or null noul, a wrong type, a non-finite value, or a value outside
// [0, 1] is an invalid judge response: it must never be silently scored as a
// 0.0 content verdict and never pass a gate.
func system1NoulAnswer(answers map[string]jevAnswer, id string) (float64, error) {
	answer, ok := answers[id]
	if !ok {
		return 0, fmt.Errorf("invalid judge response: missing noul answer for %q", id)
	}
	if answer.Type != "noul" {
		return 0, fmt.Errorf("invalid judge response: answer for %q has type %q, want \"noul\"", id, answer.Type)
	}
	if answer.Noul == nil {
		return 0, fmt.Errorf("invalid judge response: answer for %q has no noul value", id)
	}
	probability := *answer.Noul
	if math.IsNaN(probability) || math.IsInf(probability, 0) || probability < 0 || probability > 1 {
		return 0, fmt.Errorf("invalid judge response: noul for %q must be a finite probability in [0, 1], got %v", id, probability)
	}
	return probability, nil
}

// verdictFromJev strictly validates and scores typesafe/jev answers. Every
// asked question (each criterion plus stronger_but_different) must be answered
// with a finite noul probability in [0, 1]; anything else is a judge response
// failure — an infrastructure failure, never a meaningful content verdict and
// never a pass. Weights are applied locally and deterministically.
func verdictFromJev(raw string, criteria []system1Criterion, packet system1CheckPacket, checkID string) (System1Verdict, error) {
	body := extractJSONObject(raw)
	if body == "" {
		return System1Verdict{}, fmt.Errorf("malformed judge JSON: typesafe/jev returned no JSON object")
	}
	var parsed jevResponse
	if err := json.Unmarshal([]byte(body), &parsed); err != nil {
		return System1Verdict{}, fmt.Errorf("malformed judge JSON: %v", err)
	}
	if parsed.Answers == nil {
		return System1Verdict{}, fmt.Errorf("malformed judge JSON: typesafe/jev returned no answers")
	}
	probabilities := make(map[string]float64, len(criteria)+1)
	for _, criterion := range criteria {
		probability, err := system1NoulAnswer(parsed.Answers, criterion.ID)
		if err != nil {
			return System1Verdict{}, err
		}
		probabilities[criterion.ID] = probability
	}
	stronger, err := system1NoulAnswer(parsed.Answers, system1StrongerQuestionID)
	if err != nil {
		return System1Verdict{}, err
	}
	verdict := System1Verdict{
		ContractVersion:      packet.ContractVersion,
		CheckId:              checkID,
		Criteria:             make([]System1CriterionVerdict, 0, len(criteria)),
		StrongerButDifferent: stronger,
	}
	var weightSum float64
	var weighted float64
	for _, criterion := range criteria {
		probability := probabilities[criterion.ID]
		gap := ""
		if probability < 0.5 {
			gap = "noul below 0.5"
		}
		verdict.Criteria = append(verdict.Criteria, System1CriterionVerdict{
			Id:          criterion.ID,
			Probability: probability,
			Evidence:    []string{"typesafe/jev noul"},
			Gap:         gap,
		})
		weight := criterion.Weight
		if weight <= 0 {
			weight = 1
		}
		weightSum += weight
		weighted += probability * weight
	}
	if weightSum == 0 {
		verdict.OverallProbability = 0
	} else {
		verdict.OverallProbability = weighted / weightSum
	}
	for _, hardID := range packet.HardIDs {
		probability := 0.0
		found := false
		for _, criterion := range verdict.Criteria {
			if criterion.Id == hardID {
				probability = criterion.Probability
				found = true
				break
			}
		}
		if !found || probability < 0.5 {
			verdict.Blocking = append(verdict.Blocking, hardID)
		}
	}
	if decideSystem1Pass(verdict, packet) {
		verdict.Verdict = system1VerdictPass
	} else {
		verdict.Verdict = system1VerdictFail
	}
	verdict.Summary = fmt.Sprintf("typesafe/jev overall=%.2f stronger_but_different=%.2f", verdict.OverallProbability, verdict.StrongerButDifferent)
	return verdict, nil
}

func extractJSONObject(raw string) string {
	start := strings.Index(raw, "{")
	end := strings.LastIndex(raw, "}")
	if start < 0 || end < start {
		return ""
	}
	return raw[start : end+1]
}

func launchSystem1Jev(ctx context.Context, projectCWD string, payload string) (string, error) {
	// Payload goes on stdin. A transcript overview on argv exceeds ARG_MAX
	// ("argument list too long") and the check never reaches Jev.
	command := execCommand(resolveSystem1CommandcodeBinary(), "-m", system1JevModel, "-p", "--skip-onboarding", "--no-auto-update")
	command.Dir = projectCWD
	command.Stdin = strings.NewReader(payload)
	commandEnv, envErr := resolveRuntimeLaunchEnv(projectCWD, os.Environ())
	if envErr != nil {
		commandEnv = os.Environ()
	}
	command.Env = commandEnv
	var stdout bytes.Buffer
	var stderr bytes.Buffer
	command.Stdout = &stdout
	command.Stderr = &stderr
	if err := command.Start(); err != nil {
		return "", fmt.Errorf("failed to start typesafe/jev: %v", err)
	}
	waitErrCh := make(chan error, 1)
	go func() { waitErrCh <- command.Wait() }()
	select {
	case waitErr := <-waitErrCh:
		if waitErr != nil {
			detail := strings.TrimSpace(stderr.String())
			if detail == "" {
				detail = waitErr.Error()
			}
			return "", fmt.Errorf("typesafe/jev exited with error: %s", boundedParallelWaveSummaryText(detail, 2000))
		}
	case <-time.After(2 * time.Minute):
		_ = command.Process.Kill()
		<-waitErrCh
		return "", fmt.Errorf("typesafe/jev exceeded 2m")
	case <-ctx.Done():
		_ = command.Process.Kill()
		<-waitErrCh
		return "", fmt.Errorf("typesafe/jev canceled: %v", ctx.Err())
	}
	return stdout.String(), nil
}

// parseSystem1JudgeOutput strictly parses a legacy judge JSON blob. The live
// path no longer uses it; typesafe/jev answers are scored in verdictFromJev.
// Kept so a stored artifact can still be re-read. Any malformed output is a
// failed check, never a pass.
func parseSystem1JudgeOutput(raw string, expectedCheckID string, contractVersion string) (System1Verdict, error) {
	trimmed := strings.TrimSpace(raw)
	if trimmed == "" {
		return System1Verdict{}, fmt.Errorf("judge returned empty output")
	}
	payload := trimmed
	if strings.HasPrefix(trimmed, "```") {
		payload = strings.TrimPrefix(trimmed, "```")
		payload = strings.TrimSuffix(payload, "```")
		payload = strings.TrimSpace(payload)
		if newline := strings.IndexByte(payload, '\n'); newline >= 0 && !strings.Contains(payload[:newline], "{") {
			payload = strings.TrimSpace(payload[newline+1:])
		}
	}
	var verdict System1Verdict
	if err := json.Unmarshal([]byte(payload), &verdict); err != nil {
		return System1Verdict{}, fmt.Errorf("malformed judge JSON: %v", err)
	}
	if strings.TrimSpace(verdict.ContractVersion) != contractVersion {
		return System1Verdict{}, fmt.Errorf("malformed judge JSON: contract_version %q does not match requested %q", verdict.ContractVersion, contractVersion)
	}
	if verdict.Verdict != system1VerdictPass && verdict.Verdict != system1VerdictFail {
		return System1Verdict{}, fmt.Errorf("malformed judge JSON: verdict must be %q or %q, got %q", system1VerdictPass, system1VerdictFail, verdict.Verdict)
	}
	if strings.TrimSpace(verdict.CheckId) != "" && strings.TrimSpace(verdict.CheckId) != expectedCheckID {
		return System1Verdict{}, fmt.Errorf("malformed judge JSON: check_id %q does not match expected %q", verdict.CheckId, expectedCheckID)
	}
	verdict.CheckId = expectedCheckID
	verdict.ContractVersion = contractVersion
	return verdict, nil
}

// decideSystem1Pass implements the trial rule exactly: a check passes iff
// overall_probability >= threshold AND every hard criterion has
// probability >= 0.5. A hard criterion missing from the verdict is unproven
// (0.0) and forces fail. The judge's self-reported verdict is never sufficient.
func decideSystem1Pass(verdict System1Verdict, packet system1CheckPacket) bool {
	if verdict.OverallProbability < packet.Threshold {
		return false
	}
	probabilityByID := make(map[string]float64, len(verdict.Criteria))
	for _, criterion := range verdict.Criteria {
		probabilityByID[strings.TrimSpace(criterion.Id)] = criterion.Probability
	}
	for _, hardID := range packet.HardIDs {
		if probability, ok := probabilityByID[hardID]; !ok || probability < 0.5 {
			return false
		}
	}
	return true
}

func buildSystem1ReaderPrompt(finalReport string, transcriptPath string, diagnostics []string, transcriptPayload string) string {
	var prompt strings.Builder
	prompt.WriteString(system1AnalystPrompt)
	prompt.WriteString("\n\nWorker final report (a claim for the claims-vs-observed table, not evidence):\n")
	prompt.WriteString(finalReport)
	prompt.WriteString("\n\nWorker full session transcript resolved via get_netrunner_transcript_path (JSONL; cite line numbers):\n")
	if strings.TrimSpace(transcriptPath) == "" {
		prompt.WriteString("transcript path: (not resolved)\n")
		if len(diagnostics) > 0 {
			prompt.WriteString("lookup diagnostics: " + strings.Join(diagnostics, "; ") + "\n")
		}
		prompt.WriteString("The transcript is missing or unreadable; say exactly what is missing.\n")
		return prompt.String()
	}
	prompt.WriteString("transcript path: ")
	prompt.WriteString(transcriptPath)
	prompt.WriteString("\n")
	if transcriptPayload == "" {
		if len(diagnostics) > 0 {
			prompt.WriteString("lookup diagnostics: " + strings.Join(diagnostics, "; ") + "\n")
		}
		prompt.WriteString("The transcript is missing or unreadable; say exactly what is missing.\n")
		return prompt.String()
	}
	prompt.WriteString(transcriptPayload)
	return prompt.String()
}

// readSystem1TranscriptPayload numbers the transcript lines and bounds the
// embed size (head plus tail with an explicit truncation marker) so the reader
// can still cite real line numbers.
func readSystem1TranscriptPayload(path string) string {
	content, err := os.ReadFile(path)
	if err != nil {
		return ""
	}
	lines := strings.Split(strings.ReplaceAll(string(content), "\r\n", "\n"), "\n")
	numbered := make([]string, 0, len(lines))
	for index, line := range lines {
		numbered = append(numbered, fmt.Sprintf("%d: %s", index+1, line))
	}
	payload := strings.Join(numbered, "\n")
	if len(payload) <= system1TranscriptMaxBytes {
		return payload
	}
	half := system1TranscriptMaxBytes / 2
	head := payload[:half]
	tail := payload[len(payload)-half:]
	return head + "\n...[transcript truncated for prompt budget]...\n" + tail
}

func resolveSystem1WorkerTranscript(globalSessionID int, projectID int, backend string, projectCWD string) (string, []string) {
	diagnostics := []string{}
	externalSessionID, err := fetchSessionExternalID(globalSessionID, backend)
	if err != nil {
		diagnostics = append(diagnostics, fmt.Sprintf("failed to resolve external session id: %v", err))
		return "", diagnostics
	}
	if strings.TrimSpace(externalSessionID) == "" {
		diagnostics = append(diagnostics, "transcript unavailable: no persisted external session id; transcript identity cannot be proven")
		return "", diagnostics
	}
	return resolveBackendTranscriptPath(backend, projectCWD, externalSessionID, &diagnostics), diagnostics
}

func system1CommandcodeModelID(model string) string {
	trimmed := strings.TrimSpace(model)
	return strings.TrimPrefix(trimmed, "commandcode/")
}

// system1CommandcodeEffort mirrors the client-wires adapter: the MiMo 2.6
// family ships fixed reasoning and the CLI refuses any --effort flag for it.
func system1CommandcodeEffort(model string) string {
	modelID := system1CommandcodeModelID(model)
	if strings.HasPrefix(modelID, "xiaomi/mimo-v2.6") {
		return ""
	}
	return "high"
}

func resolveSystem1CommandcodeBinary() string {
	for _, name := range []string{"cmd", "cmdc", "command-code"} {
		if path, err := exec.LookPath(name); err == nil {
			return path
		}
	}
	return "cmd"
}

// launchSystem1ManagedNetrunner runs one bounded headless commandcode query in
// the project cwd with the same runtime environment the wave launcher uses.
// It is the default System1 executor; no HTTP API is ever called directly.
func launchSystem1ManagedNetrunner(ctx context.Context, projectCWD string, model string, prompt string) (string, error) {
	args := []string{
		"--model", system1CommandcodeModelID(model),
		"--yolo",
		"--trust",
		"--skip-onboarding",
		"--no-auto-update",
		"--output-format", "json",
	}
	if effort := system1CommandcodeEffort(model); effort != "" {
		args = append(args, "--effort", effort)
	}
	args = append(args, "--print")

	command := execCommand(resolveSystem1CommandcodeBinary(), args...)
	command.Dir = projectCWD
	// The prompt (reader instructions plus up to 128KiB of numbered transcript)
	// travels on stdin, like the Jev payload. On argv it would exceed ARG_MAX
	// ("argument list too long") and the run would never start.
	command.Stdin = strings.NewReader(prompt)
	commandEnv, envErr := resolveRuntimeLaunchEnv(projectCWD, os.Environ())
	if envErr != nil {
		log.Printf("warning: system1 managed launch: failed to resolve runtime launch env for %s: %v", projectCWD, envErr)
		commandEnv = os.Environ()
	}
	command.Env = commandEnv
	var stdout bytes.Buffer
	var stderr bytes.Buffer
	command.Stdout = &stdout
	command.Stderr = &stderr
	if err := command.Start(); err != nil {
		return "", fmt.Errorf("failed to start managed system1 netrunner (%s): %v", model, err)
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
			return "", fmt.Errorf("managed system1 netrunner (%s) exited with error: %s", model, boundedParallelWaveSummaryText(detail, 2000))
		}
	case <-time.After(system1ManagedExecTimeout):
		_ = command.Process.Kill()
		<-waitErrCh
		return "", fmt.Errorf("managed system1 netrunner (%s) exceeded bounded timeout %s", model, system1ManagedExecTimeout)
	case <-ctx.Done():
		_ = command.Process.Kill()
		<-waitErrCh
		return "", fmt.Errorf("managed system1 netrunner (%s) canceled: %v", model, ctx.Err())
	}
	return extractSystem1ModelText(stdout.String())
}

// extractSystem1ModelText extracts the final overview text from one cmd run's
// stdout. cmd --print emits a JSONL event stream ({"type":"event",...} lines
// followed by a terminal {"type":"result",...} line). For a recognized event
// stream only the terminal successful result.finalText — or the
// run_end.result.finalText fallback — is a legitimate overview: unsuccessful
// results, turn-limit truncation, and streams without a final overview fail
// closed instead of leaking the event stream, intermediate text, thinking, or
// tool output to the judge. Single-document JSON results and plain-text
// results stay supported.
func extractSystem1ModelText(raw string) (string, error) {
	trimmed := strings.TrimSpace(raw)
	if trimmed == "" {
		return "", fmt.Errorf("cmd produced no output: no final overview")
	}
	var single map[string]any
	if err := json.Unmarshal([]byte(trimmed), &single); err == nil {
		if isSystem1CmdStreamMarker(single) {
			return extractSystem1ModelTextFromStream(trimmed)
		}
		for _, key := range []string{"result", "text", "output", "response"} {
			if value, ok := single[key].(string); ok && strings.TrimSpace(value) != "" {
				return strings.TrimSpace(value), nil
			}
		}
		if message, ok := single["message"].(map[string]any); ok {
			for _, key := range []string{"content", "text"} {
				if value, ok := message[key].(string); ok && strings.TrimSpace(value) != "" {
					return strings.TrimSpace(value), nil
				}
			}
		}
		if content, ok := single["content"].(string); ok && strings.TrimSpace(content) != "" {
			return strings.TrimSpace(content), nil
		}
		// A single JSON document without a known text field is still a
		// legitimate single-document result variant.
		return trimmed, nil
	}
	if isSystem1CmdEventStream(trimmed) {
		return extractSystem1ModelTextFromStream(trimmed)
	}
	return trimmed, nil
}

func isSystem1CmdStreamMarker(entry map[string]any) bool {
	kind, _ := entry["type"].(string)
	return kind == "event" || kind == "result"
}

// isSystem1CmdEventStream recognizes the cmd --print JSONL shape: the first
// non-empty line is an event/result wrapper object.
func isSystem1CmdEventStream(raw string) bool {
	for _, line := range strings.Split(raw, "\n") {
		line = strings.TrimSpace(line)
		if line == "" {
			continue
		}
		var entry map[string]any
		if err := json.Unmarshal([]byte(line), &entry); err != nil {
			return false
		}
		return isSystem1CmdStreamMarker(entry)
	}
	return false
}

func extractSystem1ModelTextFromStream(raw string) (string, error) {
	var terminal map[string]any
	var runEnd map[string]any
	for _, line := range strings.Split(raw, "\n") {
		line = strings.TrimSpace(line)
		if line == "" {
			continue
		}
		var entry map[string]any
		if err := json.Unmarshal([]byte(line), &entry); err != nil {
			continue
		}
		switch entry["type"] {
		case "result":
			terminal = entry
		case "event":
			if event, ok := entry["event"].(map[string]any); ok && event["type"] == "run_end" {
				runEnd = event
			}
		}
	}
	if terminal != nil {
		subtype, _ := terminal["subtype"].(string)
		stopReason, _ := terminal["stopReason"].(string)
		switch {
		case subtype == "error":
			errorText, _ := terminal["error"].(string)
			return "", fmt.Errorf("cmd result is unsuccessful: %s", boundedParallelWaveSummaryText(strings.TrimSpace(errorText), 500))
		case subtype == "max_turns" || stopReason == "max_turns":
			return "", fmt.Errorf("cmd result is truncated by the turn limit (max_turns): there is no complete final overview")
		case subtype != "success":
			return "", fmt.Errorf("cmd result has unrecognized subtype %q: refusing to treat it as a final overview", subtype)
		}
		finalText, _ := terminal["finalText"].(string)
		if strings.TrimSpace(finalText) != "" {
			return strings.TrimSpace(finalText), nil
		}
	}
	if runEnd != nil {
		result, ok := runEnd["result"].(map[string]any)
		if ok {
			stopReason, _ := result["stopReason"].(string)
			if stopReason == "max_turns" {
				return "", fmt.Errorf("cmd run ended truncated by the turn limit (max_turns): there is no complete final overview")
			}
			if stopReason == "run_error" {
				return "", fmt.Errorf("cmd run ended unsuccessfully (run_error): there is no final overview")
			}
			finalText, _ := result["finalText"].(string)
			if strings.TrimSpace(finalText) != "" {
				return strings.TrimSpace(finalText), nil
			}
		}
	}
	return "", fmt.Errorf("cmd event stream has no final overview (no successful result.finalText): refusing to send the event stream to the judge")
}

func buildSystem1Continuation(checkID string, verdict System1Verdict, packet system1CheckPacket) string {
	var text strings.Builder
	text.WriteString("You have not passed the System1 check. Here is the check that was performed, its criteria, and the returned probabilities. Address the unresolved parts.\n\n")
	text.WriteString(fmt.Sprintf(
		"Check %s (contract %s): verdict %s, overall_probability %.2f (threshold %.2f).\n\n",
		checkID,
		packet.ContractVersion,
		verdict.Verdict,
		verdict.OverallProbability,
		packet.Threshold,
	))
	text.WriteString("Criteria:\n")
	if len(verdict.Criteria) == 0 {
		text.WriteString("- (no criterion probabilities were returned; treat every criterion as unresolved)\n")
	}
	for _, criterion := range verdict.Criteria {
		state := "gap remains"
		if criterion.Probability >= 0.5 {
			state = "looks satisfied"
		}
		text.WriteString(fmt.Sprintf("- %s: probability %.2f — %s", criterion.Id, criterion.Probability, state))
		if gap := strings.TrimSpace(criterion.Gap); gap != "" {
			text.WriteString(fmt.Sprintf("; gap: %s", gap))
		}
		text.WriteString("\n")
	}
	if len(verdict.Blocking) > 0 {
		text.WriteString("\nBlocking:\n")
		for _, blocking := range verdict.Blocking {
			text.WriteString("- " + blocking + "\n")
		}
	}
	if summary := strings.TrimSpace(verdict.Summary); summary != "" {
		text.WriteString("\nCheck summary: " + summary + "\n")
	}
	return text.String()
}

// processSystem1ReviewForWorker runs one System1 gate cycle for a worker that
// reached review_ready. It is idempotent per decision: passed/escalated workers
// are skipped, and the checks-used counter is claimed atomically before the
// judge runs so concurrent wait loops cannot double-check a worker.
func processSystem1ReviewForWorker(ctx context.Context, projectCWD string, wave NetrunnerWaveSnapshot, worker NetrunnerWaveWorkerSnapshot) (NetrunnerWaveWorkerSnapshot, system1Outcome, error) {
	packet, found, err := fetchSystem1Packet(wave.Id)
	if err != nil {
		return worker, system1OutcomeSkipped, err
	}
	if !found {
		return worker, system1OutcomeSkipped, nil
	}
	if !dbTableExists("wave_system1_check") || !dbTableHasColumn("parallel_wave_worker", "system1_state") {
		return worker, system1OutcomeSkipped, nil
	}
	if worker.System1State == system1StatePassed || worker.System1State == system1StateEscalated {
		return worker, system1OutcomeSkipped, nil
	}

	checkNumber := worker.System1ChecksUsed + 1
	if checkNumber > packet.MaxChecks {
		if err := markSystem1WorkerDecision(worker.Id, wave.ProjectId, system1StateEscalated, worker.System1ChecksUsed); err != nil {
			return worker, system1OutcomeSkipped, err
		}
		refreshed, refreshErr := fetchSystem1WorkerSnapshot(wave.Id, wave.ProjectId, worker.Id)
		if refreshErr != nil {
			return worker, system1OutcomeEscalated, refreshErr
		}
		return refreshed, system1OutcomeEscalated, nil
	}

	claim, err := db.Exec(
		`UPDATE parallel_wave_worker
		 SET system1_checks_used = ?, updated_at = CURRENT_TIMESTAMP
		 WHERE id = ? AND project_id = ? AND system1_checks_used = ?`,
		checkNumber,
		worker.Id,
		wave.ProjectId,
		checkNumber-1,
	)
	if err != nil {
		return worker, system1OutcomeSkipped, err
	}
	if claimed, _ := claim.RowsAffected(); claimed == 0 {
		refreshed, refreshErr := fetchSystem1WorkerSnapshot(wave.Id, wave.ProjectId, worker.Id)
		if refreshErr != nil {
			return worker, system1OutcomeSkipped, refreshErr
		}
		return refreshed, system1OutcomeSkipped, nil
	}

	checkID := system1CheckID(wave.Id, worker.SessionId, checkNumber)
	artifact := system1Artifact{
		CheckId:         checkID,
		ContractVersion: packet.ContractVersion,
		WaveId:          wave.Id,
		SessionId:       worker.SessionId,
		CheckNumber:     checkNumber,
		Packet:          system1PacketFromRow(packet),
	}
	globalSessionID, err := globalSessionIDFromProjectScoped(worker.SessionId, wave.ProjectId)
	if err != nil {
		return system1InfraFailure(projectCWD, wave, worker, packet, checkNumber, checkID, 0, artifact,
			fmt.Sprintf("system1 infrastructure failure (session context): session %d not found in current project: %v", worker.SessionId, err))
	}
	status, report, _, backend, _, _, _, err := fetchSessionWaitSnapshot(globalSessionID, wave.ProjectId)
	if err != nil {
		return system1InfraFailure(projectCWD, wave, worker, packet, checkNumber, checkID, globalSessionID, artifact,
			fmt.Sprintf("system1 infrastructure failure (session context): failed to read session snapshot: %v", err))
	}
	artifact.WorkerStatus = status
	artifact.FinalReport = report

	transcriptPath, diagnostics := resolveSystem1WorkerTranscript(globalSessionID, wave.ProjectId, backend, projectCWD)
	transcriptPayload := ""
	if strings.TrimSpace(transcriptPath) != "" {
		transcriptPayload = readSystem1TranscriptPayload(transcriptPath)
	}
	artifact.TranscriptPath = transcriptPath

	readerPrompt := buildSystem1ReaderPrompt(report, transcriptPath, diagnostics, transcriptPayload)
	readerReport, readerErr := system1ReaderExec(ctx, projectCWD, system1ReaderModel, readerPrompt)
	if readerErr != nil {
		return system1InfraFailure(projectCWD, wave, worker, packet, checkNumber, checkID, globalSessionID, artifact,
			fmt.Sprintf("system1 infrastructure failure (reader): %v", readerErr))
	}
	if strings.TrimSpace(readerReport) == "" {
		return system1InfraFailure(projectCWD, wave, worker, packet, checkNumber, checkID, globalSessionID, artifact,
			"system1 infrastructure failure (reader): the transcript reader produced no factual overview")
	}
	artifact.ReaderReport = readerReport

	jevPayload, criteria, payloadErr := buildSystem1JevPayload(packet, report, readerReport)
	if payloadErr != nil {
		return system1InfraFailure(projectCWD, wave, worker, packet, checkNumber, checkID, globalSessionID, artifact,
			fmt.Sprintf("system1 input error: %v", payloadErr))
	}
	judgeRaw, judgeErr := system1JudgeExec(ctx, projectCWD, jevPayload)
	if judgeErr != nil {
		return system1InfraFailure(projectCWD, wave, worker, packet, checkNumber, checkID, globalSessionID, artifact,
			fmt.Sprintf("system1 infrastructure failure (judge): %v", judgeErr))
	}
	artifact.JudgeRawOutput = judgeRaw

	verdict, verdictErr := verdictFromJev(judgeRaw, criteria, packet, checkID)
	if verdictErr != nil {
		return system1InfraFailure(projectCWD, wave, worker, packet, checkNumber, checkID, globalSessionID, artifact,
			fmt.Sprintf("system1 infrastructure failure (judge response): %v", verdictErr))
	}
	passed := decideSystem1Pass(verdict, packet)
	stronger := verdict.StrongerButDifferent >= packet.Threshold && !passed

	artifactPath, artifactErr := system1ArtifactPath(projectCWD, wave.Id, worker.SessionId, checkNumber)
	if artifactErr != nil {
		return worker, system1OutcomeSkipped, artifactErr
	}
	artifact.Verdict = &verdict
	artifact.Passed = passed
	artifact.Outcome = string(system1OutcomeRequeued)
	if passed {
		artifact.Outcome = string(system1OutcomePassed)
	}

	outcome := system1OutcomeRequeued
	finalCheck := checkNumber >= packet.MaxChecks
	if passed {
		outcome = system1OutcomePassed
	} else if stronger {
		// Strict criteria failed, but Jev says the delivery is objectively
		// stronger despite a radical departure. Do not grind the worker back
		// onto the spec. The Fixer reviews this one.
		outcome = system1OutcomeEscalated
		verdict.FixerReviewBecause = "stronger_but_different"
		verdict.Summary = verdict.Summary + "; strict criteria failed, but typesafe/jev rates the delivery as objectively stronger and different — Fixer reviews, worker is not requeued"
	} else if finalCheck {
		outcome = system1OutcomeEscalated
	}
	artifact.Outcome = string(outcome)
	artifact.Verdict = &verdict
	artifactPayload, marshalErr := json.MarshalIndent(artifact, "", "  ")
	if marshalErr != nil {
		return worker, system1OutcomeSkipped, marshalErr
	}
	if err := os.WriteFile(artifactPath, artifactPayload, 0o644); err != nil {
		return worker, system1OutcomeSkipped, fmt.Errorf("failed to write system1 artifact: %v", err)
	}

	record := System1CheckRecord{
		WaveId:             wave.Id,
		WaveWorkerId:       worker.Id,
		SessionId:          worker.SessionId,
		CheckNumber:        checkNumber,
		CheckId:            checkID,
		ContractVersion:    packet.ContractVersion,
		Verdict:            verdict.Verdict,
		OverallProbability: verdict.OverallProbability,
		Threshold:          packet.Threshold,
		Escalated:          outcome == system1OutcomeEscalated,
		Summary:            boundedParallelWaveSummaryText(verdict.Summary, 2000),
		ArtifactPath:       artifactPath,
	}
	if _, err := persistSystem1CheckRecord(record, globalSessionID); err != nil {
		return worker, system1OutcomeSkipped, fmt.Errorf("failed to persist system1 check row: %v", err)
	}
	log.Printf(
		"system1_check wave_id=%d session_id=%d check=%d/%d check_id=%s executor=%s verdict=%s overall=%.2f threshold=%.2f outcome=%s",
		wave.Id, worker.SessionId, checkNumber, packet.MaxChecks, checkID, system1JudgeExecutorName, verdict.Verdict, verdict.OverallProbability, packet.Threshold, outcome,
	)

	switch outcome {
	case system1OutcomePassed:
		if err := markSystem1WorkerDecision(worker.Id, wave.ProjectId, system1StatePassed, checkNumber); err != nil {
			return worker, system1OutcomeSkipped, err
		}
		refreshed, refreshErr := fetchSystem1WorkerSnapshot(wave.Id, wave.ProjectId, worker.Id)
		if refreshErr != nil {
			return worker, outcome, refreshErr
		}
		return refreshed, outcome, nil
	case system1OutcomeEscalated:
		if err := markSystem1WorkerDecision(worker.Id, wave.ProjectId, system1StateEscalated, checkNumber); err != nil {
			return worker, system1OutcomeSkipped, err
		}
		refreshed, refreshErr := fetchSystem1WorkerSnapshot(wave.Id, wave.ProjectId, worker.Id)
		if refreshErr != nil {
			return worker, outcome, refreshErr
		}
		return refreshed, outcome, nil
	default:
		continuation := buildSystem1Continuation(checkID, verdict, packet)
		if _, _, err := UpdateTask(ctx, nil, UpdateTaskInput{
			SessionId:           worker.SessionId,
			AppendedDescription: continuation,
		}); err != nil {
			return worker, system1OutcomeSkipped, fmt.Errorf("failed to append system1 continuation: %v", err)
		}
		if _, _, err := SetSessionStatus(ctx, nil, SetSessionStatusInput{
			SessionId: worker.SessionId,
			Status:    "pending",
			Reason:    fmt.Sprintf("system1_check_failed: %s (%d/%d)", checkID, checkNumber, packet.MaxChecks),
		}); err != nil {
			return worker, system1OutcomeSkipped, fmt.Errorf("failed to requeue system1 continuation: %v", err)
		}
		refreshed, refreshErr := fetchSystem1WorkerSnapshot(wave.Id, wave.ProjectId, worker.Id)
		if refreshErr != nil {
			return worker, outcome, refreshErr
		}
		return refreshed, outcome, nil
	}
}

// system1InfraFailure records one infrastructure failure attempt and leaves
// the worker review_ready with its content-check budget intact. The atomic
// claim on system1_checks_used is released with the same compare-and-set
// discipline it was taken with — never a blind decrement — and historical check
// rows are never mutated. Every attempt appends a verdict=infra_failed row plus
// a diagnostic artifact so the failure is visible to the Fixer, without any
// rework feedback or requeue of the implementation worker. After
// system1InfraMaxAttempts attempts the worker is escalated to Fixer
// second-stage review so the wait loop can never retry in a tight loop.
func system1InfraFailure(projectCWD string, wave NetrunnerWaveSnapshot, worker NetrunnerWaveWorkerSnapshot, packet system1CheckPacket, checkNumber int, checkID string, globalSessionID int, artifact system1Artifact, diagnostic string) (NetrunnerWaveWorkerSnapshot, system1Outcome, error) {
	if err := releaseSystem1CheckClaim(worker.Id, wave.ProjectId, checkNumber); err != nil {
		log.Printf("system1_infra wave_id=%d session_id=%d failed to release claim on check %d: %v", wave.Id, worker.SessionId, checkNumber, err)
	}
	attempt, err := countSystem1InfraAttempts(wave.Id, worker.Id)
	if err != nil {
		return worker, system1OutcomeSkipped, err
	}
	attempt++
	escalated := attempt >= system1InfraMaxAttempts

	artifact.Outcome = string(system1OutcomeInfraFailed)
	artifact.InfraAttempt = attempt
	artifact.InfraDiagnostic = diagnostic
	artifactPath, artifactErr := system1InfraArtifactPath(projectCWD, wave.Id, worker.SessionId, checkNumber, attempt)
	if artifactErr != nil {
		return worker, system1OutcomeSkipped, artifactErr
	}
	artifactPayload, marshalErr := json.MarshalIndent(artifact, "", "  ")
	if marshalErr != nil {
		return worker, system1OutcomeSkipped, marshalErr
	}
	if err := os.WriteFile(artifactPath, artifactPayload, 0o644); err != nil {
		return worker, system1OutcomeSkipped, fmt.Errorf("failed to write system1 infra artifact: %v", err)
	}

	record := System1CheckRecord{
		WaveId:             wave.Id,
		WaveWorkerId:       worker.Id,
		SessionId:          worker.SessionId,
		CheckNumber:        checkNumber,
		CheckId:            fmt.Sprintf("%s-infra%d", checkID, attempt),
		ContractVersion:    packet.ContractVersion,
		Verdict:            system1VerdictInfraFailed,
		OverallProbability: 0,
		Threshold:          packet.Threshold,
		Escalated:          escalated,
		Summary:            boundedParallelWaveSummaryText(diagnostic, 2000),
		ArtifactPath:       artifactPath,
	}
	if _, err := persistSystem1CheckRecord(record, globalSessionID); err != nil {
		return worker, system1OutcomeSkipped, fmt.Errorf("failed to persist system1 infra row: %v", err)
	}
	log.Printf(
		"system1_infra wave_id=%d session_id=%d check=%d attempt=%d/%d check_id=%s escalated=%t diagnostic=%s",
		wave.Id, worker.SessionId, checkNumber, attempt, system1InfraMaxAttempts, record.CheckId, escalated,
		boundedParallelWaveSummaryText(diagnostic, 500),
	)

	if escalated {
		if err := markSystem1WorkerDecision(worker.Id, wave.ProjectId, system1StateEscalated, checkNumber-1); err != nil {
			return worker, system1OutcomeSkipped, err
		}
	}
	refreshed, refreshErr := fetchSystem1WorkerSnapshot(wave.Id, wave.ProjectId, worker.Id)
	if refreshErr != nil {
		return worker, system1OutcomeInfraFailed, refreshErr
	}
	return refreshed, system1OutcomeInfraFailed, nil
}

// releaseSystem1CheckClaim gives the claimed content-check budget back after an
// infrastructure failure with the same compare-and-set guard as the claim
// itself: the counter is only restored while it still holds exactly the
// claimed value, so a concurrent decision is never overwritten and no blind
// decrement can corrupt the budget.
func releaseSystem1CheckClaim(waveWorkerID int, projectID int, claimedCheckNumber int) error {
	_, err := db.Exec(
		`UPDATE parallel_wave_worker
		 SET system1_checks_used = ?, updated_at = CURRENT_TIMESTAMP
		 WHERE id = ? AND project_id = ? AND system1_checks_used = ?`,
		claimedCheckNumber-1,
		waveWorkerID,
		projectID,
		claimedCheckNumber,
	)
	return err
}

// countSystem1InfraAttempts derives the attempt count from the appended
// infra rows; historical rows are only ever appended to, never mutated.
func countSystem1InfraAttempts(waveID int, waveWorkerID int) (int, error) {
	if !dbTableExists("wave_system1_check") {
		return 0, nil
	}
	var count int
	err := db.QueryRow(
		`SELECT COUNT(*) FROM wave_system1_check WHERE wave_id = ? AND wave_worker_id = ? AND verdict = ?`,
		waveID,
		waveWorkerID,
		system1VerdictInfraFailed,
	).Scan(&count)
	if err != nil {
		return 0, err
	}
	return count, nil
}

func markSystem1WorkerDecision(waveWorkerID int, projectID int, state string, checksUsed int) error {
	_, err := db.Exec(
		`UPDATE parallel_wave_worker
		 SET system1_state = ?, system1_checks_used = ?, updated_at = CURRENT_TIMESTAMP
		 WHERE id = ? AND project_id = ?`,
		state,
		checksUsed,
		waveWorkerID,
		projectID,
	)
	return err
}

func fetchSystem1WorkerSnapshot(waveID int, projectID int, waveWorkerID int) (NetrunnerWaveWorkerSnapshot, error) {
	wave, err := fetchNetrunnerWaveSnapshot(waveID, projectID)
	if err != nil {
		return NetrunnerWaveWorkerSnapshot{}, err
	}
	for _, worker := range wave.Workers {
		if worker.Id == waveWorkerID {
			return worker, nil
		}
	}
	return NetrunnerWaveWorkerSnapshot{}, fmt.Errorf("wave worker %d not found after system1 update", waveWorkerID)
}

// CreateNetrunnerWaveTool is the create_netrunner_wave tool surface. New waves
// must carry a system1_check packet; internal callers (the planned-wave
// initializer and engine tests) call CreateNetrunnerWave directly and keep the
// legacy manual-review behavior for packet-less waves.
func CreateNetrunnerWaveTool(ctx context.Context, req *mcp.CallToolRequest, input CreateNetrunnerWaveInput) (*mcp.CallToolResult, CreateNetrunnerWaveOutput, error) {
	if _, err := normalizeSystem1CheckInput(input.System1Check); err != nil {
		return &mcp.CallToolResult{IsError: true}, CreateNetrunnerWaveOutput{}, err
	}
	return CreateNetrunnerWave(ctx, req, input)
}

// LaunchNetrunnerWaveTool is the launch_netrunner_wave tool surface. A wave
// without a persisted packet must receive system1_check at launch (legacy or
// planned waves); waves that already carry a packet launch unchanged.
func LaunchNetrunnerWaveTool(ctx context.Context, req *mcp.CallToolRequest, input LaunchNetrunnerWaveInput) (*mcp.CallToolResult, LaunchNetrunnerWaveOutput, error) {
	if input.System1Check == nil && authorizedRole == "fixer" && authorizedProjectId > 0 {
		wave, err := fetchNetrunnerWaveSnapshot(input.WaveId, authorizedProjectId)
		if err == nil {
			if _, found, packetErr := fetchSystem1Packet(wave.Id); packetErr == nil && !found {
				return &mcp.CallToolResult{IsError: true}, LaunchNetrunnerWaveOutput{}, fmt.Errorf(
					"system1_check is required to launch wave %d: it has no persisted System1 packet; pass {criteria_prompt, hard_ids, threshold (default 0.75), max_checks (default 3, clamped 1..3), contract_version: %q}",
					input.WaveId,
					system1TrialContractVersion,
				)
			}
		}
	}
	return LaunchNetrunnerWave(ctx, req, input)
}

// GetSystem1Reviews exposes the wave's System1 packet and per-check rows so the
// Fixer can read the short recorded verdicts without touching SQLite directly.
func GetSystem1Reviews(ctx context.Context, req *mcp.CallToolRequest, input GetSystem1ReviewsInput) (*mcp.CallToolResult, GetSystem1ReviewsOutput, error) {
	if authorizedRole != "fixer" {
		return &mcp.CallToolResult{IsError: true}, GetSystem1ReviewsOutput{}, fmt.Errorf("access denied: requires fixer role")
	}
	if authorizedProjectId <= 0 {
		return &mcp.CallToolResult{IsError: true}, GetSystem1ReviewsOutput{}, fmt.Errorf("access denied: fixer role is not bound to a project")
	}
	wave, err := fetchNetrunnerWaveSnapshot(input.WaveId, authorizedProjectId)
	if err == sql.ErrNoRows {
		return &mcp.CallToolResult{IsError: true}, GetSystem1ReviewsOutput{}, fmt.Errorf("wave %d not found in current project", input.WaveId)
	}
	if err != nil {
		return &mcp.CallToolResult{IsError: true}, GetSystem1ReviewsOutput{}, fmt.Errorf("DB query error: %v", err)
	}

	output := GetSystem1ReviewsOutput{
		Status:  "success",
		WaveId:  wave.Id,
		Checks:  []System1CheckRecord{},
		Workers: []System1ReviewWorkerState{},
	}
	if packet, found, packetErr := fetchSystem1Packet(wave.Id); packetErr != nil {
		return &mcp.CallToolResult{IsError: true}, GetSystem1ReviewsOutput{}, fmt.Errorf("DB query error: %v", packetErr)
	} else if found {
		output.Contract = system1PacketFromRow(packet)
	}
	for _, worker := range wave.Workers {
		output.Workers = append(output.Workers, System1ReviewWorkerState{
			SessionId:         worker.SessionId,
			Status:            worker.Status,
			System1State:      worker.System1State,
			System1ChecksUsed: worker.System1ChecksUsed,
		})
	}
	if dbTableExists("wave_system1_check") {
		rows, queryErr := db.Query(
			`SELECT id, wave_id, wave_worker_id, local_session_id, check_number, check_id,
			        contract_version, verdict, overall_probability, threshold, escalated,
			        COALESCE(summary, ''), COALESCE(artifact_path, ''), COALESCE(created_at, '')
			 FROM wave_system1_check
			 WHERE wave_id = ? AND project_id = ?
			 ORDER BY check_number, id`,
			wave.Id,
			authorizedProjectId,
		)
		if queryErr != nil {
			return &mcp.CallToolResult{IsError: true}, GetSystem1ReviewsOutput{}, fmt.Errorf("DB query error: %v", queryErr)
		}
		defer rows.Close()
		for rows.Next() {
			var record System1CheckRecord
			var escalated int
			if scanErr := rows.Scan(
				&record.Id,
				&record.WaveId,
				&record.WaveWorkerId,
				&record.SessionId,
				&record.CheckNumber,
				&record.CheckId,
				&record.ContractVersion,
				&record.Verdict,
				&record.OverallProbability,
				&record.Threshold,
				&escalated,
				&record.Summary,
				&record.ArtifactPath,
				&record.CreatedAt,
			); scanErr != nil {
				return &mcp.CallToolResult{IsError: true}, GetSystem1ReviewsOutput{}, fmt.Errorf("DB scan error: %v", scanErr)
			}
			record.Escalated = escalated != 0
			output.Checks = append(output.Checks, record)
		}
		if rowsErr := rows.Err(); rowsErr != nil {
			return &mcp.CallToolResult{IsError: true}, GetSystem1ReviewsOutput{}, fmt.Errorf("DB query error: %v", rowsErr)
		}
	}
	return nil, output, nil
}
