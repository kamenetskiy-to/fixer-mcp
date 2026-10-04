package main

import (
	"archive/zip"
	"context"
	"encoding/json"
	"io"
	"os"
	"path/filepath"
	"reflect"
	"strings"
	"testing"
)

func readProjectDocBundle(t *testing.T, path string) map[string][]byte {
	t.Helper()
	r, err := zip.OpenReader(path)
	if err != nil {
		t.Fatalf("open bundle: %v", err)
	}
	defer r.Close()
	entries := make(map[string][]byte, len(r.File))
	for _, file := range r.File {
		reader, err := file.Open()
		if err != nil {
			t.Fatalf("open bundle entry %q: %v", file.Name, err)
		}
		data, err := io.ReadAll(reader)
		_ = reader.Close()
		if err != nil {
			t.Fatalf("read bundle entry %q: %v", file.Name, err)
		}
		entries[file.Name] = data
	}
	return entries
}

func TestExportProjectDocBundleExactSelectionAndScopedIDs(t *testing.T) {
	testDB := setupContextPackageTestDB(t)
	defer testDB.Close()
	seedContextPackageSourceProject(t, testDB)
	if _, err := testDB.Exec("INSERT INTO project_doc (id, project_id, title, content, doc_type, level, slug, path, status) VALUES (20, 2, 'Other Root', 'Other content', 'documentation', 0, 'other-root', 'other-root', 'current')"); err != nil {
		t.Fatalf("seed interleaved project doc: %v", err)
	}
	withContextPackageTestAuth(t, testDB, "fixer", 1)

	// The project root is a test temp directory; use a path under it explicitly.
	var projectRoot string
	if err := testDB.QueryRow("SELECT cwd FROM project WHERE id = 1").Scan(&projectRoot); err != nil {
		t.Fatalf("read project root: %v", err)
	}
	outputPath := filepath.Join(projectRoot, "artifacts", "bundle.zip")
	_, out, err := ExportProjectDocBundle(context.Background(), nil, ExportProjectDocBundleInput{
		ProjectDocIds: []int{3, 1, 3},
		Path:          outputPath,
	})
	if err != nil {
		t.Fatalf("export_project_doc_bundle failed: %v", err)
	}
	if out.Status != "success" || out.ProjectId != 1 || out.DocsCount != 2 || out.ArchiveBytes <= 0 {
		t.Fatalf("unexpected export output: %+v", out)
	}
	if !reflect.DeepEqual(out.ProjectDocIds, []int{1, 3}) {
		t.Fatalf("expected sorted deduplicated IDs [1 3], got %+v", out.ProjectDocIds)
	}

	entries := readProjectDocBundle(t, out.Path)
	for _, name := range []string{"manifest.json", "docs/root-doc.md", "docs/root-doc/child-doc/grandchild-doc.md"} {
		if _, ok := entries[name]; !ok {
			t.Fatalf("bundle missing %q; entries=%v", name, entries)
		}
	}
	if _, ok := entries["docs/root-doc/child-doc.md"]; ok {
		t.Fatal("bundle silently added unselected ancestor/descendant")
	}
	if string(entries["docs/root-doc.md"]) != "Root content" || string(entries["docs/root-doc/child-doc/grandchild-doc.md"]) != "Grandchild content" {
		t.Fatalf("bundle content was not preserved verbatim: %+v", entries)
	}
	var manifest ProjectDocBundleManifest
	if err := json.Unmarshal(entries["manifest.json"], &manifest); err != nil {
		t.Fatalf("parse bundle manifest: %v", err)
	}
	if manifest.SchemaVersion != projectDocBundleSchemaVersion || manifest.ProjectName != "Source Project" || manifest.ProjectId != 1 || len(manifest.Docs) != 2 {
		t.Fatalf("unexpected manifest: %+v", manifest)
	}
	if manifest.Docs[0].ProjectDocId != 1 || manifest.Docs[1].ProjectDocId != 3 || manifest.Docs[1].ParentPath != "root-doc/child-doc" {
		t.Fatalf("manifest docs are not sorted/scoped correctly: %+v", manifest.Docs)
	}
}

func TestExportProjectDocBundleRejectsInvalidRequestWithoutWriting(t *testing.T) {
	testDB := setupContextPackageTestDB(t)
	defer testDB.Close()
	seedContextPackageSourceProject(t, testDB)
	withContextPackageTestAuth(t, testDB, "fixer", 1)

	var projectRoot string
	if err := testDB.QueryRow("SELECT cwd FROM project WHERE id = 1").Scan(&projectRoot); err != nil {
		t.Fatalf("read project root: %v", err)
	}
	cases := []struct {
		name string
		ids  []int
		path string
		want string
	}{
		{name: "empty", ids: nil, want: "non-empty"},
		{name: "zero", ids: []int{1, 0}, want: "positive"},
		{name: "unknown", ids: []int{99}, want: "unknown project_doc_id"},
		{name: "traversal output", ids: []int{1}, path: "../outside.zip", want: "under the bound project root"},
	}
	for _, tc := range cases {
		t.Run(tc.name, func(t *testing.T) {
			_, _, err := ExportProjectDocBundle(context.Background(), nil, ExportProjectDocBundleInput{ProjectDocIds: tc.ids, Path: tc.path})
			if err == nil || !strings.Contains(err.Error(), tc.want) {
				t.Fatalf("expected error containing %q, got %v", tc.want, err)
			}
		})
	}
	if _, err := os.Stat(filepath.Join(projectRoot, "outside.zip")); !os.IsNotExist(err) {
		t.Fatalf("invalid request created outside archive: %v", err)
	}

	if _, err := testDB.Exec("UPDATE project_doc SET path = '../escape' WHERE id = 10"); err != nil {
		t.Fatalf("seed unsafe canonical path: %v", err)
	}
	unsafeOutput := filepath.Join(projectRoot, "unsafe.zip")
	_, _, err := ExportProjectDocBundle(context.Background(), nil, ExportProjectDocBundleInput{ProjectDocIds: []int{1}, Path: unsafeOutput})
	if err == nil || !strings.Contains(err.Error(), "unsafe canonical path") {
		t.Fatalf("expected unsafe canonical path error, got %v", err)
	}
	if _, statErr := os.Stat(unsafeOutput); !os.IsNotExist(statErr) {
		t.Fatalf("unsafe canonical path created archive: %v", statErr)
	}
}

func TestExportProjectDocBundleRejectsOverwriteAndNonFixer(t *testing.T) {
	testDB := setupContextPackageTestDB(t)
	defer testDB.Close()
	seedContextPackageSourceProject(t, testDB)
	withContextPackageTestAuth(t, testDB, "fixer", 1)
	var projectRoot string
	if err := testDB.QueryRow("SELECT cwd FROM project WHERE id = 1").Scan(&projectRoot); err != nil {
		t.Fatalf("read project root: %v", err)
	}
	outputPath := filepath.Join(projectRoot, "bundle.zip")
	if _, _, err := ExportProjectDocBundle(context.Background(), nil, ExportProjectDocBundleInput{ProjectDocIds: []int{1}, Path: outputPath}); err != nil {
		t.Fatalf("initial export failed: %v", err)
	}
	original, err := os.ReadFile(outputPath)
	if err != nil {
		t.Fatalf("read initial archive: %v", err)
	}
	if _, _, err := ExportProjectDocBundle(context.Background(), nil, ExportProjectDocBundleInput{ProjectDocIds: []int{1}, Path: outputPath}); err == nil || !strings.Contains(err.Error(), "already exists") {
		t.Fatalf("expected overwrite refusal, got %v", err)
	}
	current, err := os.ReadFile(outputPath)
	if err != nil || string(current) != string(original) {
		t.Fatalf("existing archive changed after rejected overwrite: err=%v", err)
	}

	authorizedRole = "netrunner"
	if _, _, err := ExportProjectDocBundle(context.Background(), nil, ExportProjectDocBundleInput{ProjectDocIds: []int{1}, Path: filepath.Join(projectRoot, "netrunner.zip")}); err == nil || !strings.Contains(err.Error(), "requires fixer role") {
		t.Fatalf("expected non-fixer rejection, got %v", err)
	}
}

func TestNormalizeProjectDocBundleIDs(t *testing.T) {
	got, err := normalizeProjectDocBundleIDs([]int{4, 2, 4, 1})
	if err != nil {
		t.Fatalf("normalize IDs failed: %v", err)
	}
	want := []int{1, 2, 4}
	if len(got) != len(want) {
		t.Fatalf("unexpected normalized IDs: %+v", got)
	}
	for i := range want {
		if got[i] != want[i] {
			t.Fatalf("unexpected normalized IDs: got=%+v want=%+v", got, want)
		}
	}
}

func TestWriteProjectDocBundleArchiveCleansTemporaryFileOnFailure(t *testing.T) {
	outputDir := t.TempDir()
	outputPath := filepath.Join(outputDir, "bundle.zip")
	err := writeProjectDocBundleArchive(outputPath, []byte(`{"ok":true}`), []projectDocBundleDoc{
		{Manifest: ProjectDocBundleManifestDoc{ArchivePath: ""}, Content: "invalid entry"},
	})
	if err == nil {
		t.Fatal("expected invalid archive entry to fail")
	}
	if _, statErr := os.Stat(outputPath); !os.IsNotExist(statErr) {
		t.Fatalf("failed archive was published: %v", statErr)
	}
	entries, readErr := os.ReadDir(outputDir)
	if readErr != nil {
		t.Fatalf("read output directory: %v", readErr)
	}
	for _, entry := range entries {
		if strings.Contains(entry.Name(), ".tmp-") {
			t.Fatalf("temporary archive was not cleaned up: %q", entry.Name())
		}
	}
}

func TestExportProjectDocBundleLegacyPathValidation(t *testing.T) {
	testDB := setupContextPackageTestDB(t)
	defer testDB.Close()
	seedContextPackageSourceProject(t, testDB)
	withContextPackageTestAuth(t, testDB, "fixer", 1)

	var projectRoot string
	if err := testDB.QueryRow("SELECT cwd FROM project WHERE id = 1").Scan(&projectRoot); err != nil {
		t.Fatalf("read project root: %v", err)
	}

	// Fixtures mirroring docs 19 and 22:
	// Doc 19: path "tordoki/backend-data-integrations/serverpod-domain-protocol/track-recording" vs slug "track-recording-contract"
	// Doc 22: path "operations/release-checklist" vs slug "release-checklist-contract"
	// Genuinely inconsistent doc 30: path "operations/completely-unrelated-path" vs slug "mismatched-contract"
	fixtures := []struct {
		id      int
		title   string
		content string
		slug    string
		path    string
	}{
		{
			id:      19,
			title:   "Track Recording Contract",
			content: "Track recording contract body",
			slug:    "track-recording-contract",
			path:    "tordoki/backend-data-integrations/serverpod-domain-protocol/track-recording",
		},
		{
			id:      22,
			title:   "Release Checklist Contract",
			content: "Release checklist contract body",
			slug:    "release-checklist-contract",
			path:    "operations/release-checklist",
		},
		{
			id:      30,
			title:   "Genuinely Inconsistent Doc",
			content: "Mismatched path body",
			slug:    "mismatched-contract",
			path:    "operations/completely-unrelated-path",
		},
	}
	for _, f := range fixtures {
		if _, err := testDB.Exec(
			"INSERT INTO project_doc (id, project_id, title, content, doc_type, level, slug, path, status) VALUES (?, 1, ?, ?, 'contract', 1, ?, ?, 'current')",
			f.id, f.title, f.content, f.slug, f.path,
		); err != nil {
			t.Fatalf("seed fixture doc %d: %v", f.id, err)
		}
	}

	localID19, err := projectScopedDocIDFromGlobal(19, 1)
	if err != nil {
		t.Fatalf("resolve local id for doc 19: %v", err)
	}
	localID22, err := projectScopedDocIDFromGlobal(22, 1)
	if err != nil {
		t.Fatalf("resolve local id for doc 22: %v", err)
	}
	localID30, err := projectScopedDocIDFromGlobal(30, 1)
	if err != nil {
		t.Fatalf("resolve local id for doc 30: %v", err)
	}

	// 1. Proving export succeeds for legacy fixtures mirroring docs 19 and 22.
	outputPath := filepath.Join(projectRoot, "artifacts", "legacy-bundle.zip")
	_, out, err := ExportProjectDocBundle(context.Background(), nil, ExportProjectDocBundleInput{
		ProjectDocIds: []int{localID19, localID22},
		Path:          outputPath,
	})
	if err != nil {
		t.Fatalf("export_project_doc_bundle failed for legacy docs: %v", err)
	}
	if out.Status != "success" || out.ProjectId != 1 || out.DocsCount != 2 || out.ArchiveBytes <= 0 {
		t.Fatalf("unexpected export output: %+v", out)
	}

	entries := readProjectDocBundle(t, out.Path)
	doc19ArchivePath := "docs/tordoki/backend-data-integrations/serverpod-domain-protocol/track-recording.md"
	doc22ArchivePath := "docs/operations/release-checklist.md"
	for _, name := range []string{"manifest.json", doc19ArchivePath, doc22ArchivePath} {
		if _, ok := entries[name]; !ok {
			t.Fatalf("bundle missing %q; entries=%v", name, entries)
		}
	}
	if string(entries[doc19ArchivePath]) != "Track recording contract body" {
		t.Fatalf("doc 19 content mismatch: %q", string(entries[doc19ArchivePath]))
	}
	if string(entries[doc22ArchivePath]) != "Release checklist contract body" {
		t.Fatalf("doc 22 content mismatch: %q", string(entries[doc22ArchivePath]))
	}

	var manifest ProjectDocBundleManifest
	if err := json.Unmarshal(entries["manifest.json"], &manifest); err != nil {
		t.Fatalf("parse manifest: %v", err)
	}
	if len(manifest.Docs) != 2 {
		t.Fatalf("expected 2 docs in manifest, got %d", len(manifest.Docs))
	}
	docBySlug := make(map[string]ProjectDocBundleManifestDoc, len(manifest.Docs))
	for _, d := range manifest.Docs {
		docBySlug[d.Slug] = d
	}
	m19, ok := docBySlug["track-recording-contract"]
	if !ok || m19.Path != "tordoki/backend-data-integrations/serverpod-domain-protocol/track-recording" || m19.ArchivePath != doc19ArchivePath {
		t.Fatalf("unexpected doc 19 manifest: %+v", m19)
	}
	m22, ok := docBySlug["release-checklist-contract"]
	if !ok || m22.Path != "operations/release-checklist" || m22.ArchivePath != doc22ArchivePath {
		t.Fatalf("unexpected doc 22 manifest: %+v", m22)
	}

	// Export must stay strictly READ-ONLY: verify DB rows were not modified.
	var storedSlug19, storedPath19 string
	if err := testDB.QueryRow("SELECT slug, path FROM project_doc WHERE id = 19").Scan(&storedSlug19, &storedPath19); err != nil {
		t.Fatalf("query doc 19 from db: %v", err)
	}
	if storedSlug19 != "track-recording-contract" || storedPath19 != "tordoki/backend-data-integrations/serverpod-domain-protocol/track-recording" {
		t.Fatalf("doc 19 mutated in db: slug=%q path=%q", storedSlug19, storedPath19)
	}
	var storedSlug22, storedPath22 string
	if err := testDB.QueryRow("SELECT slug, path FROM project_doc WHERE id = 22").Scan(&storedSlug22, &storedPath22); err != nil {
		t.Fatalf("query doc 22 from db: %v", err)
	}
	if storedSlug22 != "release-checklist-contract" || storedPath22 != "operations/release-checklist" {
		t.Fatalf("doc 22 mutated in db: slug=%q path=%q", storedSlug22, storedPath22)
	}

	// 2. Genuinely inconsistent fixture must still fail with a clear actionable error.
	inconsistentPath := filepath.Join(projectRoot, "artifacts", "inconsistent.zip")
	_, _, err = ExportProjectDocBundle(context.Background(), nil, ExportProjectDocBundleInput{
		ProjectDocIds: []int{localID30},
		Path:          inconsistentPath,
	})
	if err == nil {
		t.Fatal("expected export of genuinely inconsistent doc to fail, but it succeeded")
	}
	if !strings.Contains(err.Error(), "does not end with slug") || !strings.Contains(err.Error(), "legacy form") {
		t.Fatalf("expected clear actionable error mentioning slug and legacy form, got: %v", err)
	}
	if _, statErr := os.Stat(inconsistentPath); !os.IsNotExist(statErr) {
		t.Fatalf("inconsistent export created archive on disk: %v", statErr)
	}
}

func TestValidateProjectDocBundleCanonicalPath(t *testing.T) {
	cases := []struct {
		name    string
		path    string
		slug    string
		wantErr bool
		errMsg  string
	}{
		{
			name:    "exact match single segment",
			path:    "root-doc",
			slug:    "root-doc",
			wantErr: false,
		},
		{
			name:    "exact match nested path",
			path:    "a/b/c",
			slug:    "c",
			wantErr: false,
		},
		{
			name:    "legacy contract doc 19 mirror",
			path:    "tordoki/backend-data-integrations/serverpod-domain-protocol/track-recording",
			slug:    "track-recording-contract",
			wantErr: false,
		},
		{
			name:    "legacy contract doc 22 mirror",
			path:    "operations/release-checklist",
			slug:    "release-checklist-contract",
			wantErr: false,
		},
		{
			name:    "genuinely inconsistent with contract slug",
			path:    "operations/other-topic",
			slug:    "release-checklist-contract",
			wantErr: true,
			errMsg:  "does not end with slug \"release-checklist-contract\" or legacy form \"release-checklist\"",
		},
		{
			name:    "genuinely inconsistent with normal slug",
			path:    "operations/other-topic",
			slug:    "release-checklist",
			wantErr: true,
			errMsg:  "does not end with slug \"release-checklist\"",
		},
		{
			name:    "unsafe path traversal",
			path:    "../outside",
			slug:    "outside",
			wantErr: true,
			errMsg:  "unsafe canonical path",
		},
	}
	for _, tc := range cases {
		t.Run(tc.name, func(t *testing.T) {
			err := validateProjectDocBundleCanonicalPath(tc.path, tc.slug)
			if (err != nil) != tc.wantErr {
				t.Fatalf("validateProjectDocBundleCanonicalPath(%q, %q) err=%v, wantErr=%v", tc.path, tc.slug, err, tc.wantErr)
			}
			if tc.wantErr && !strings.Contains(err.Error(), tc.errMsg) {
				t.Fatalf("expected error containing %q, got %v", tc.errMsg, err)
			}
		})
	}
}
