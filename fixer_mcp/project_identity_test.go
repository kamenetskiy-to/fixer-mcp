package main

import (
	"context"
	"database/sql"
	"os"
	"path/filepath"
	"strings"
	"testing"
)

const (
	testIdentityKeyAlpha = "git@github.com:Acme/Alpha.git"
	testIdentityKeyBeta  = "operator:beta-slug"
)

func setupProjectIdentityTestDB(t *testing.T) *sql.DB {
	t.Helper()

	originalDB := db
	originalRole := authorizedRole
	originalProjectID := authorizedProjectId
	originalSessionID := authorizedSessionId
	t.Cleanup(func() {
		db = originalDB
		authorizedRole = originalRole
		authorizedProjectId = originalProjectID
		authorizedSessionId = originalSessionID
	})

	testDB := setupGetProjectsTestDB(t)
	t.Cleanup(func() {
		_ = testDB.Close()
	})

	db = testDB
	authorizedRole = "overseer"
	authorizedProjectId = 0
	authorizedSessionId = 0
	return testDB
}

func mustNormalizeIdentityPath(t *testing.T, path string) string {
	t.Helper()
	normalized, err := normalizeProjectCWD(path)
	if err != nil {
		t.Fatalf("normalize path %q: %v", path, err)
	}
	return normalized
}

func TestRegisterProjectIdentityKeyLinksSameProjectAcrossPaths(t *testing.T) {
	testDB := setupProjectIdentityTestDB(t)

	normalizedA := mustNormalizeIdentityPath(t, t.TempDir())
	normalizedB := mustNormalizeIdentityPath(t, t.TempDir())

	_, created, err := RegisterProject(context.Background(), nil, RegisterProjectInput{
		Cwd:         normalizedA,
		IdentityKey: testIdentityKeyAlpha,
	})
	if err != nil {
		t.Fatalf("register first path with identity failed: %v", err)
	}
	if created.Status != "created" {
		t.Fatalf("expected created status, got %+v", created)
	}
	if created.IdentityKey != "git@github.com:acme/alpha" {
		t.Fatalf("expected normalized identity on the project row, got %q", created.IdentityKey)
	}

	_, aliased, err := RegisterProject(context.Background(), nil, RegisterProjectInput{
		Cwd:         normalizedB,
		IdentityKey: "GIT@GitHub.com:Acme/Alpha",
	})
	if err != nil {
		t.Fatalf("register second path with the same identity failed: %v", err)
	}
	if aliased.Status != "exists" {
		t.Fatalf("expected exists status for the identity-bound second path, got %+v", aliased)
	}
	if aliased.ProjectId != created.ProjectId {
		t.Fatalf("expected the same project id across paths, created=%d aliased=%d", created.ProjectId, aliased.ProjectId)
	}

	var aliasProjectID int
	if err := testDB.QueryRow(
		`SELECT project_id FROM project_path_alias WHERE path = ?`,
		normalizedB,
	).Scan(&aliasProjectID); err != nil {
		t.Fatalf("expected a path alias for the second path: %v", err)
	}
	if aliasProjectID != created.ProjectId {
		t.Fatalf("alias %q points to project %d, want %d", normalizedB, aliasProjectID, created.ProjectId)
	}

	for _, lookup := range []string{
		normalizedA,
		normalizedB,
		filepath.Join(normalizedB, "nested", "child"),
	} {
		projectID, _, knownCWD, lookupErr := findProjectByCWD(lookup)
		if lookupErr != nil {
			t.Fatalf("expected %q to resolve to the identity project: %v", lookup, lookupErr)
		}
		if projectID != created.ProjectId {
			t.Fatalf("expected %q to resolve to project %d, got %d (cwd=%q)", lookup, created.ProjectId, projectID, knownCWD)
		}
	}

	projectID, _, _, lookupErr := findProjectByCWDOrIdentity(filepath.Join(normalizedB, "unregistered"), testIdentityKeyAlpha)
	if lookupErr != nil {
		t.Fatalf("identity lookup failed: %v", lookupErr)
	}
	if projectID != created.ProjectId {
		t.Fatalf("expected identity lookup to resolve project %d, got %d", created.ProjectId, projectID)
	}

	var owners int
	if err := testDB.QueryRow(
		`SELECT COUNT(*) FROM project WHERE identity_key = ?`,
		"git@github.com:acme/alpha",
	).Scan(&owners); err != nil {
		t.Fatalf("count identity owners: %v", err)
	}
	if owners != 1 {
		t.Fatalf("expected exactly one project for the identity, got %d", owners)
	}
}

func TestRegisterProjectDifferentIdentityKeysCreateDistinctProjects(t *testing.T) {
	testDB := setupProjectIdentityTestDB(t)

	normalizedA := mustNormalizeIdentityPath(t, t.TempDir())
	normalizedB := mustNormalizeIdentityPath(t, t.TempDir())
	normalizedC := mustNormalizeIdentityPath(t, t.TempDir())

	_, first, err := RegisterProject(context.Background(), nil, RegisterProjectInput{
		Cwd:         normalizedA,
		IdentityKey: testIdentityKeyAlpha,
	})
	if err != nil {
		t.Fatalf("register alpha failed: %v", err)
	}

	_, second, err := RegisterProject(context.Background(), nil, RegisterProjectInput{
		Cwd:         normalizedB,
		IdentityKey: testIdentityKeyBeta,
	})
	if err != nil {
		t.Fatalf("register beta failed: %v", err)
	}
	if second.ProjectId == first.ProjectId {
		t.Fatalf("different identities collapsed into project %d", first.ProjectId)
	}

	_, repeated, err := RegisterProject(context.Background(), nil, RegisterProjectInput{
		Cwd:         normalizedA,
		IdentityKey: testIdentityKeyAlpha,
	})
	if err != nil {
		t.Fatalf("re-register alpha failed: %v", err)
	}
	if repeated.Status != "exists" || repeated.ProjectId != first.ProjectId {
		t.Fatalf("expected idempotent alpha registration, got %+v", repeated)
	}

	// Equal paths without an identity keep resolving to the same project.
	_, legacyRepeat, err := RegisterProject(context.Background(), nil, RegisterProjectInput{Cwd: normalizedA})
	if err != nil {
		t.Fatalf("legacy re-register of an equal path failed: %v", err)
	}
	if legacyRepeat.Status != "exists" || legacyRepeat.ProjectId != first.ProjectId {
		t.Fatalf("equal path without identity must reuse the project, got %+v", legacyRepeat)
	}
	if legacyRepeat.IdentityKey != "git@github.com:acme/alpha" {
		t.Fatalf("expected the bound identity to be read back, got %q", legacyRepeat.IdentityKey)
	}

	_, third, err := RegisterProject(context.Background(), nil, RegisterProjectInput{Cwd: normalizedC})
	if err != nil {
		t.Fatalf("legacy register of a new path failed: %v", err)
	}
	if third.Status != "created" || third.ProjectId == first.ProjectId || third.ProjectId == second.ProjectId {
		t.Fatalf("expected a distinct identity-free project, got %+v", third)
	}

	var identityCount int
	if err := testDB.QueryRow(
		`SELECT COUNT(DISTINCT identity_key) FROM project WHERE identity_key IS NOT NULL AND identity_key != ''`,
	).Scan(&identityCount); err != nil {
		t.Fatalf("count distinct identities: %v", err)
	}
	if identityCount != 2 {
		t.Fatalf("expected two distinct identities, got %d", identityCount)
	}
}

func TestRegisterProjectWithoutIdentityKeepsLegacyPathBehavior(t *testing.T) {
	testDB := setupProjectIdentityTestDB(t)

	normalizedA := mustNormalizeIdentityPath(t, t.TempDir())
	nestedA := filepath.Join(normalizedA, "nested", "child")
	if err := os.MkdirAll(nestedA, 0o755); err != nil {
		t.Fatalf("create nested dir: %v", err)
	}
	normalizedNested := mustNormalizeIdentityPath(t, nestedA)
	normalizedB := mustNormalizeIdentityPath(t, t.TempDir())

	_, created, err := RegisterProject(context.Background(), nil, RegisterProjectInput{Cwd: normalizedA})
	if err != nil {
		t.Fatalf("legacy register failed: %v", err)
	}
	if created.Status != "created" {
		t.Fatalf("expected created status, got %+v", created)
	}
	if created.IdentityKey != "" {
		t.Fatalf("expected no identity on an identity-free registration, got %q", created.IdentityKey)
	}

	_, repeated, err := RegisterProject(context.Background(), nil, RegisterProjectInput{Cwd: normalizedA})
	if err != nil {
		t.Fatalf("legacy idempotent register failed: %v", err)
	}
	if repeated.Status != "exists" || repeated.ProjectId != created.ProjectId {
		t.Fatalf("expected path-keyed idempotency, got %+v", repeated)
	}

	_, nested, err := RegisterProject(context.Background(), nil, RegisterProjectInput{Cwd: normalizedNested})
	if err != nil {
		t.Fatalf("register nested cwd failed: %v", err)
	}
	if nested.Status != "exists" || nested.ProjectId != created.ProjectId {
		t.Fatalf("nested cwd must reuse the parent project, got %+v", nested)
	}

	_, other, err := RegisterProject(context.Background(), nil, RegisterProjectInput{Cwd: normalizedB})
	if err != nil {
		t.Fatalf("register second identity-free path failed: %v", err)
	}
	if other.Status != "created" || other.ProjectId == created.ProjectId {
		t.Fatalf("expected a distinct identity-free project, got %+v", other)
	}

	var withIdentity int
	if err := testDB.QueryRow(
		`SELECT COUNT(*) FROM project WHERE identity_key IS NOT NULL`,
	).Scan(&withIdentity); err != nil {
		t.Fatalf("count identified projects: %v", err)
	}
	if withIdentity != 0 {
		t.Fatalf("identity-free registrations must not invent identities, got %d", withIdentity)
	}
}

func TestRegisterProjectIdentityConflictsFailClosed(t *testing.T) {
	testDB := setupProjectIdentityTestDB(t)

	normalizedA := mustNormalizeIdentityPath(t, t.TempDir())
	normalizedB := mustNormalizeIdentityPath(t, t.TempDir())

	_, pathProject, err := RegisterProject(context.Background(), nil, RegisterProjectInput{Cwd: normalizedA})
	if err != nil {
		t.Fatalf("register identity-free path failed: %v", err)
	}

	_, identityProject, err := RegisterProject(context.Background(), nil, RegisterProjectInput{
		Cwd:         normalizedB,
		IdentityKey: testIdentityKeyBeta,
	})
	if err != nil {
		t.Fatalf("register identity project failed: %v", err)
	}

	// The identity points at one project while the path belongs to another.
	conflictResult, _, conflictErr := RegisterProject(context.Background(), nil, RegisterProjectInput{
		Cwd:         normalizedA,
		IdentityKey: testIdentityKeyBeta,
	})
	if conflictErr == nil {
		t.Fatal("expected an identity/path conflict to fail closed")
	}
	if conflictResult == nil || !conflictResult.IsError {
		t.Fatalf("expected an MCP error result, got %+v", conflictResult)
	}
	if !strings.Contains(conflictErr.Error(), "identity/path conflict") {
		t.Fatalf("expected an actionable identity/path conflict message, got: %v", conflictErr)
	}
	if !strings.Contains(conflictErr.Error(), normalizedA) {
		t.Fatalf("expected the conflicting path in the message, got: %v", conflictErr)
	}

	var pathIdentity sql.NullString
	if err := testDB.QueryRow(`SELECT identity_key FROM project WHERE id = ?`, pathProject.ProjectId).Scan(&pathIdentity); err != nil {
		t.Fatalf("read path project identity: %v", err)
	}
	if pathIdentity.Valid && strings.TrimSpace(pathIdentity.String) != "" {
		t.Fatalf("failed registration must not bind an identity, got %q", pathIdentity.String)
	}
	var aliasCount int
	if err := testDB.QueryRow(`SELECT COUNT(*) FROM project_path_alias WHERE path = ?`, normalizedA).Scan(&aliasCount); err != nil {
		t.Fatalf("count alias rows: %v", err)
	}
	if aliasCount != 0 {
		t.Fatalf("failed registration must not create an alias, got %d", aliasCount)
	}
	if identityProject.ProjectId == pathProject.ProjectId {
		t.Fatal("the two seeded projects must stay distinct")
	}

	// A project cannot be rebound to a second identity through an equal path.
	if _, _, err := RegisterProject(context.Background(), nil, RegisterProjectInput{
		Cwd:         normalizedA,
		IdentityKey: testIdentityKeyAlpha,
	}); err != nil {
		t.Fatalf("first identity binding on an equal path should succeed: %v", err)
	}
	rebindResult, _, rebindErr := RegisterProject(context.Background(), nil, RegisterProjectInput{
		Cwd:         normalizedA,
		IdentityKey: "operator:other-slug",
	})
	if rebindErr == nil {
		t.Fatal("expected rebinding a project to another identity to fail closed")
	}
	if rebindResult == nil || !rebindResult.IsError {
		t.Fatalf("expected an MCP error result, got %+v", rebindResult)
	}
	if !strings.Contains(rebindErr.Error(), "identity_key conflict") {
		t.Fatalf("expected an actionable identity_key conflict message, got: %v", rebindErr)
	}
}

func TestAssumeRoleResolvesIdentityAliasPath(t *testing.T) {
	setupProjectIdentityTestDB(t)

	normalizedA := mustNormalizeIdentityPath(t, t.TempDir())
	normalizedB := mustNormalizeIdentityPath(t, t.TempDir())

	_, created, err := RegisterProject(context.Background(), nil, RegisterProjectInput{
		Cwd:         normalizedA,
		IdentityKey: testIdentityKeyAlpha,
	})
	if err != nil {
		t.Fatalf("register primary path failed: %v", err)
	}
	if _, _, err := RegisterProject(context.Background(), nil, RegisterProjectInput{
		Cwd:         normalizedB,
		IdentityKey: testIdentityKeyAlpha,
	}); err != nil {
		t.Fatalf("register alias path failed: %v", err)
	}

	_, assumeOut, assumeErr := AssumeRole(context.Background(), nil, AssumeRoleInput{
		Role: "fixer",
		Cwd:  normalizedB,
	})
	if assumeErr != nil {
		t.Fatalf("assume_role on an alias path failed: %v", assumeErr)
	}
	if assumeOut.Status != "success" {
		t.Fatalf("expected success on an alias path, got %+v", assumeOut)
	}
	if authorizedProjectId != created.ProjectId {
		t.Fatalf("assume_role bound project %d, want %d", authorizedProjectId, created.ProjectId)
	}
}

func TestProjectIdentityExposedInReadSurfaces(t *testing.T) {
	setupProjectIdentityTestDB(t)

	normalizedA := mustNormalizeIdentityPath(t, t.TempDir())
	_, created, err := RegisterProject(context.Background(), nil, RegisterProjectInput{
		Cwd:         normalizedA,
		IdentityKey: testIdentityKeyAlpha,
	})
	if err != nil {
		t.Fatalf("register identified project failed: %v", err)
	}

	activityResult, activityOut, activityErr := SetProjectActivity(context.Background(), nil, SetProjectActivityInput{
		ProjectId: created.ProjectId,
		Activity:  "active",
	})
	if activityErr != nil {
		t.Fatalf("set_project_activity failed: %v", activityErr)
	}
	if activityResult != nil && activityResult.IsError {
		t.Fatalf("set_project_activity returned an MCP error result: %+v", activityResult)
	}
	if activityOut.Record.IdentityKey != "git@github.com:acme/alpha" {
		t.Fatalf("expected identity on the activity record, got %q", activityOut.Record.IdentityKey)
	}

	_, overviews, overviewErr := GetActiveProjectOverviews(context.Background(), nil, GetActiveProjectOverviewsInput{})
	if overviewErr != nil {
		t.Fatalf("get_active_project_overviews failed: %v", overviewErr)
	}
	var found *ActiveProjectOverview
	for index := range overviews.Projects {
		if overviews.Projects[index].ProjectId == created.ProjectId {
			found = &overviews.Projects[index]
			break
		}
	}
	if found == nil {
		t.Fatalf("active project %d missing from overviews: %+v", created.ProjectId, overviews.Projects)
	}
	if found.IdentityKey != "git@github.com:acme/alpha" {
		t.Fatalf("expected identity on the project overview, got %q", found.IdentityKey)
	}
}
