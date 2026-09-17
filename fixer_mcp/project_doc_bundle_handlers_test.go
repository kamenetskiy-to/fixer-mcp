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
