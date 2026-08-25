package main

import (
	"context"
	"encoding/json"
	"os"
	"path/filepath"
	"strings"
	"testing"
)

func setupWorkroomHandlerTestDB(t *testing.T) {
	t.Helper()
	originalDB := db
	originalRole := authorizedRole
	originalProjectID := authorizedProjectId
	originalSessionID := authorizedSessionId
	t.Cleanup(func() {
		if db != nil && db != originalDB {
			_ = db.Close()
		}
		db = originalDB
		authorizedRole = originalRole
		authorizedProjectId = originalProjectID
		authorizedSessionId = originalSessionID
	})
	t.Setenv(fixerDBPathEnv, filepath.Join(t.TempDir(), "workroom.db"))
	initDB()
	authorizedRole = "fixer"
	authorizedProjectId = 1
	authorizedSessionId = 0
}

func TestRequestGenUISurfaceKnownRequestIsDurableAndIdempotent(t *testing.T) {
	setupWorkroomHandlerTestDB(t)
	ctx := context.Background()
	input := RequestGenUISurfaceInput{
		SurfaceType:    "project.overview",
		SurfaceVersion: 1,
		Arguments:      map[string]any{},
		Provider:       "codex",
		Model:          "gpt-5.6-sol",
		IdempotencyKey: "surface-known-1",
	}

	callResult, first, err := RequestGenUISurface(ctx, nil, input)
	if err != nil || callResult != nil {
		t.Fatalf("request known surface: call=%+v output=%+v err=%v", callResult, first, err)
	}
	if first.Status != "presented" || first.SurfaceID == "" || first.SurfaceRevision != 1 || first.ProjectSeq < 1 {
		t.Fatalf("unexpected first receipt: %+v", first)
	}
	var document map[string]any
	if err := json.Unmarshal(first.Document, &document); err != nil {
		t.Fatalf("decode materialized document: %v", err)
	}
	if document["protocol"] != projectWorkroomProtocol || int(document["protocol_version"].(float64)) != projectWorkroomProtocolVersion {
		t.Fatalf("unexpected document protocol: %+v", document)
	}

	_, replay, err := RequestGenUISurface(ctx, nil, input)
	if err != nil {
		t.Fatalf("replay known surface: %v", err)
	}
	if replay.SurfaceID != first.SurfaceID || replay.ProjectSeq != first.ProjectSeq {
		t.Fatalf("idempotency replay diverged: first=%+v replay=%+v", first, replay)
	}
	var surfaceCount int
	if err := db.QueryRow(`SELECT COUNT(*) FROM genui_surface_instance WHERE project_id = 1`).Scan(&surfaceCount); err != nil {
		t.Fatalf("count surfaces: %v", err)
	}
	if surfaceCount != 1 {
		t.Fatalf("expected one durable surface after replay, got %d", surfaceCount)
	}

	_, fetched, err := GetGenUISurface(ctx, nil, GetGenUISurfaceInput{SurfaceID: first.SurfaceID})
	if err != nil {
		t.Fatalf("get surface: %v", err)
	}
	if fetched.SurfaceID != first.SurfaceID || fetched.SourceSeq != first.ProjectSeq || !json.Valid(fetched.Document) {
		t.Fatalf("unexpected fetched surface: %+v", fetched)
	}
}

func TestRequestGenUISurfaceUnknownAndInvalidRequestsFailClosed(t *testing.T) {
	setupWorkroomHandlerTestDB(t)
	ctx := context.Background()

	_, unknown, err := RequestGenUISurface(ctx, nil, RequestGenUISurfaceInput{
		SurfaceType: "model.arbitrary_widget", SurfaceVersion: 1,
		Arguments: map[string]any{"dart": "runAnything()"}, Provider: "test", Model: "fixture",
		IdempotencyKey: "unknown-surface-1",
	})
	if err != nil {
		t.Fatalf("unknown surface should return governed rejection receipt: %v", err)
	}
	if unknown.Status != "rejected" || unknown.RejectionCode != "surface_type_unsupported" || unknown.DemandExampleID == "" || unknown.SurfaceID != "" {
		t.Fatalf("unexpected unknown rejection: %+v", unknown)
	}

	_, invalid, err := RequestGenUISurface(ctx, nil, RequestGenUISurfaceInput{
		SurfaceType: "wave.detail", SurfaceVersion: 1,
		Arguments: map[string]any{"wave_id": 1, "javascript": "alert(1)"}, Provider: "test", Model: "fixture",
		IdempotencyKey: "invalid-surface-1",
	})
	if err != nil {
		t.Fatalf("invalid surface should return governed rejection receipt: %v", err)
	}
	if invalid.Status != "rejected" || invalid.RejectionCode != "arguments_invalid" || invalid.DemandExampleID == "" {
		t.Fatalf("unexpected invalid rejection: %+v", invalid)
	}
	var demandCount, surfaceCount int
	if err := db.QueryRow(`SELECT COUNT(*) FROM genui_demand_example WHERE project_id = 1`).Scan(&demandCount); err != nil {
		t.Fatalf("count demands: %v", err)
	}
	if err := db.QueryRow(`SELECT COUNT(*) FROM genui_surface_instance WHERE project_id = 1`).Scan(&surfaceCount); err != nil {
		t.Fatalf("count surfaces: %v", err)
	}
	if demandCount != 2 || surfaceCount != 0 {
		t.Fatalf("fail-closed persistence mismatch: demands=%d surfaces=%d", demandCount, surfaceCount)
	}
}

func TestSubmitGenUIFeedbackUpsertsOnePrincipalVoteAndJournals(t *testing.T) {
	setupWorkroomHandlerTestDB(t)
	ctx := context.Background()
	_, surface, err := RequestGenUISurface(ctx, nil, RequestGenUISurfaceInput{
		SurfaceType: "project.overview", SurfaceVersion: 1, IdempotencyKey: "feedback-surface-1",
	})
	if err != nil {
		t.Fatalf("request surface: %v", err)
	}

	input := SubmitGenUIFeedbackInput{
		SurfaceID: surface.SurfaceID, Revision: 1, Vote: 1, ReasonCode: "helpful",
		Comment: "Authoritative and compact.", IdempotencyKey: "feedback-1",
	}
	_, receipt, err := SubmitGenUIFeedback(ctx, nil, input)
	if err != nil {
		t.Fatalf("submit feedback: %v", err)
	}
	_, replay, err := SubmitGenUIFeedback(ctx, nil, input)
	if err != nil {
		t.Fatalf("replay feedback: %v", err)
	}
	if replay.ProjectSeq != receipt.ProjectSeq || replay.Vote != 1 {
		t.Fatalf("feedback replay diverged: first=%+v replay=%+v", receipt, replay)
	}

	input.Vote = -1
	input.ReasonCode = "stale"
	input.IdempotencyKey = "feedback-2"
	_, changed, err := SubmitGenUIFeedback(ctx, nil, input)
	if err != nil {
		t.Fatalf("update feedback: %v", err)
	}
	if changed.ProjectSeq <= receipt.ProjectSeq {
		t.Fatalf("expected monotonic feedback event sequence: first=%d second=%d", receipt.ProjectSeq, changed.ProjectSeq)
	}
	var rowCount, vote int
	if err := db.QueryRow(`SELECT COUNT(*), vote FROM genui_surface_feedback WHERE surface_id = ? AND revision = 1`, surface.SurfaceID).Scan(&rowCount, &vote); err != nil {
		t.Fatalf("inspect feedback: %v", err)
	}
	if rowCount != 1 || vote != -1 {
		t.Fatalf("expected one updated vote, count=%d vote=%d", rowCount, vote)
	}
}

func TestRequestGenUISurfaceRejectsIdempotencyKeyReuseWithDifferentRequest(t *testing.T) {
	setupWorkroomHandlerTestDB(t)
	ctx := context.Background()
	input := RequestGenUISurfaceInput{SurfaceType: "project.overview", SurfaceVersion: 1, IdempotencyKey: "conflict-key"}
	if _, _, err := RequestGenUISurface(ctx, nil, input); err != nil {
		t.Fatalf("first request: %v", err)
	}
	input.SurfaceType = "skills.catalog"
	if _, _, err := RequestGenUISurface(ctx, nil, input); err == nil || err.Error() != "idempotency_conflict" {
		t.Fatalf("expected idempotency_conflict, got %v", err)
	}
}

func TestRequestLegalResearchSurfaceReadsOnlyCanonicalProjectMemo(t *testing.T) {
	setupWorkroomHandlerTestDB(t)
	projectRoot := t.TempDir()
	legalDirectory := filepath.Join(projectRoot, "research", "legal")
	if err := os.MkdirAll(legalDirectory, 0o755); err != nil {
		t.Fatalf("create legal research directory: %v", err)
	}
	const memo = "# Legal canon\n\nYurka contract research from the project repository."
	if err := os.WriteFile(filepath.Join(legalDirectory, "legal_memo.md"), []byte(memo), 0o600); err != nil {
		t.Fatalf("write legal research memo: %v", err)
	}
	if _, err := db.Exec(`UPDATE project SET cwd = ? WHERE id = 1`, projectRoot); err != nil {
		t.Fatalf("bind legal research project: %v", err)
	}

	_, output, err := RequestGenUISurface(context.Background(), nil, RequestGenUISurfaceInput{
		SurfaceType: "research.legal", SurfaceVersion: 1,
		Arguments: map[string]any{}, IdempotencyKey: "legal-research-1",
	})
	if err != nil {
		t.Fatalf("request legal research surface: %v", err)
	}
	if output.Status != "presented" || output.SurfaceID == "" {
		t.Fatalf("unexpected legal surface receipt: %+v", output)
	}
	var document map[string]any
	if err := json.Unmarshal(output.Document, &document); err != nil {
		t.Fatalf("decode legal surface: %v", err)
	}
	encoded, _ := json.Marshal(document)
	if document["surface_type"] != "research.legal" ||
		!strings.Contains(string(encoded), "Yurka contract research") ||
		!strings.Contains(string(encoded), legalResearchRelativePath) ||
		!strings.Contains(string(encoded), `"confirmation":"none"`) {
		t.Fatalf("legal research document is not governed and repository-backed: %s", encoded)
	}
}
