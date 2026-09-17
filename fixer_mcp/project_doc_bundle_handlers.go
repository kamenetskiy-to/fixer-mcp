package main

import (
	"archive/zip"
	"context"
	"database/sql"
	"encoding/json"
	"fmt"
	"io"
	"os"
	pathpkg "path"
	"path/filepath"
	"sort"
	"strings"
	"time"

	"github.com/modelcontextprotocol/go-sdk/mcp"
)

const projectDocBundleSchemaVersion = 1

type ProjectDocBundleManifest struct {
	SchemaVersion         int                           `json:"schema_version"`
	ExportedAt            string                        `json:"exported_at"`
	ProjectId             int                           `json:"project_id"`
	ProjectName           string                        `json:"project_name"`
	ProjectRoot           string                        `json:"project_root"`
	DocumentationLanguage string                        `json:"documentation_language"`
	Docs                  []ProjectDocBundleManifestDoc `json:"docs"`
}

type ProjectDocBundleManifestDoc struct {
	ProjectDocId   int    `json:"project_doc_id"`
	Title          string `json:"title"`
	LocalizedTitle string `json:"localized_title,omitempty"`
	DocType        string `json:"doc_type"`
	Level          int    `json:"level"`
	Slug           string `json:"slug"`
	Path           string `json:"path"`
	Status         string `json:"status"`
	ParentPath     string `json:"parent_path,omitempty"`
	ArchivePath    string `json:"archive_path"`
}

type ExportProjectDocBundleInput struct {
	ProjectDocIds []int  `json:"project_doc_ids" jsonschema:"Required non-empty array of compact project-scoped canonical document IDs"`
	Path          string `json:"path,omitempty" jsonschema:"Optional output ZIP path; relative paths resolve against the bound project root"`
}

type ExportProjectDocBundleOutput struct {
	Status        string `json:"status"`
	Path          string `json:"path"`
	ProjectId     int    `json:"project_id"`
	ProjectDocIds []int  `json:"project_doc_ids"`
	DocsCount     int    `json:"docs_count"`
	ArchiveBytes  int64  `json:"archive_bytes"`
}

type projectDocBundleDoc struct {
	Manifest ProjectDocBundleManifestDoc
	Content  string
}

func normalizeProjectDocBundleIDs(ids []int) ([]int, error) {
	if len(ids) == 0 {
		return nil, fmt.Errorf("project_doc_ids must be a non-empty array")
	}
	for _, id := range ids {
		if id <= 0 {
			return nil, fmt.Errorf("project_doc_ids must contain only positive IDs")
		}
	}
	return normalizeDocIDs(ids), nil
}

func projectDocBundlePathWithin(root string, path string) bool {
	rel, err := filepath.Rel(root, path)
	if err != nil {
		return false
	}
	return rel != ".." && !strings.HasPrefix(rel, ".."+string(filepath.Separator))
}

func resolvedExistingPath(path string) (string, error) {
	current := filepath.Clean(path)
	for {
		if _, err := os.Lstat(current); err == nil {
			return filepath.EvalSymlinks(current)
		} else if !os.IsNotExist(err) {
			return "", err
		}
		parent := filepath.Dir(current)
		if parent == current {
			return "", fmt.Errorf("could not resolve existing parent for %q", path)
		}
		current = parent
	}
}

func resolveProjectDocBundlePath(projectRoot string, projectName string, rawPath string) (string, error) {
	root, err := filepath.Abs(filepath.Clean(strings.TrimSpace(projectRoot)))
	if err != nil {
		return "", fmt.Errorf("invalid project root: %v", err)
	}
	resolvedRoot, err := resolvedExistingPath(root)
	if err != nil {
		return "", fmt.Errorf("failed to resolve project root: %v", err)
	}

	trimmedPath := strings.TrimSpace(rawPath)
	if trimmedPath == "" {
		filename := fmt.Sprintf("%s-docs-%s.zip", normalizeDocSlugValue(projectName), time.Now().UTC().Format("20060102T150405Z"))
		trimmedPath = filepath.Join("artifacts", "project_doc_bundles", filename)
	}
	var outputPath string
	if filepath.IsAbs(trimmedPath) {
		outputPath = filepath.Clean(trimmedPath)
	} else {
		outputPath = filepath.Clean(filepath.Join(root, trimmedPath))
	}
	outputPath, err = filepath.Abs(outputPath)
	if err != nil {
		return "", fmt.Errorf("invalid output path: %v", err)
	}
	if !projectDocBundlePathWithin(root, outputPath) {
		return "", fmt.Errorf("output path must remain under the bound project root")
	}
	if _, err := os.Lstat(outputPath); err == nil {
		return "", fmt.Errorf("output path already exists: %s", outputPath)
	} else if !os.IsNotExist(err) {
		return "", fmt.Errorf("failed to inspect output path: %v", err)
	}

	resolvedParent, err := resolvedExistingPath(filepath.Dir(outputPath))
	if err != nil {
		return "", fmt.Errorf("failed to resolve output parent: %v", err)
	}
	if !projectDocBundlePathWithin(resolvedRoot, resolvedParent) {
		return "", fmt.Errorf("output path resolves outside the bound project root")
	}
	return outputPath, nil
}

func validateProjectDocBundleCanonicalSegment(value string, label string) error {
	if value == "" || normalizeDocSlugValue(value) != value || strings.ContainsAny(value, `/\\`) {
		return fmt.Errorf("selected document has unsafe canonical %s %q", label, value)
	}
	return nil
}

func validateProjectDocBundleCanonicalPath(path string, slug string) error {
	if path == "" || filepath.IsAbs(path) || strings.ContainsAny(path, `\\`) || filepath.Clean(path) != path {
		return fmt.Errorf("selected document has unsafe canonical path %q", path)
	}
	parts := strings.Split(path, "/")
	for _, part := range parts {
		if err := validateProjectDocBundleCanonicalSegment(part, "path segment"); err != nil {
			return err
		}
	}
	if len(parts) == 0 || parts[len(parts)-1] != slug {
		return fmt.Errorf("selected document canonical path %q does not end with slug %q", path, slug)
	}
	return nil
}

func fetchProjectDocBundleDocs(projectID int, localIDs []int) ([]projectDocBundleDoc, error) {
	language, err := projectDocLanguage(projectID)
	if err != nil {
		return nil, err
	}
	if err := requireProjectDocLocalizationCoverage(projectID); err != nil {
		return nil, err
	}

	globalByLocal := make(map[int]int, len(localIDs))
	globalIDs := make([]int, 0, len(localIDs))
	for _, localID := range localIDs {
		globalID, err := globalProjectDocIDFromProjectScoped(localID, projectID)
		if err == sql.ErrNoRows {
			return nil, fmt.Errorf("unknown project_doc_id %d in current project", localID)
		}
		if err != nil {
			return nil, fmt.Errorf("failed to resolve project_doc_id %d: %v", localID, err)
		}
		globalByLocal[localID] = globalID
		globalIDs = append(globalIDs, globalID)
	}

	localizedTitleExpr := "''"
	localizationJoin := ""
	args := make([]any, 0, len(globalIDs)+2)
	if projectDocLocalizationRequired(language) {
		localizedTitleExpr = "COALESCE(l.localized_title, '')"
		localizationJoin = "LEFT JOIN project_doc_title_localization l ON l.project_doc_id = d.id AND l.language_code = ?"
		args = append(args, language)
	}
	args = append(args, projectID)
	placeholders := make([]string, len(globalIDs))
	for i, globalID := range globalIDs {
		placeholders[i] = "?"
		args = append(args, globalID)
	}
	rows, err := db.Query(fmt.Sprintf(`
		SELECT d.id, d.title, %s, d.content,
		       COALESCE(d.doc_type, 'documentation'), COALESCE(parent.path, ''),
		       COALESCE(d.level, 0), COALESCE(d.slug, ''), COALESCE(d.path, ''),
		       COALESCE(d.status, 'current')
		FROM project_doc d
		LEFT JOIN project_doc parent ON parent.id = d.parent_doc_id AND parent.project_id = d.project_id
		%s
		WHERE d.project_id = ? AND d.id IN (%s)`, localizedTitleExpr, localizationJoin, strings.Join(placeholders, ",")), args...)
	if err != nil {
		return nil, fmt.Errorf("DB query error: %v", err)
	}
	defer rows.Close()

	type rawDoc struct {
		globalID   int
		manifest   ProjectDocBundleManifestDoc
		content    string
		parentPath string
	}
	found := make(map[int]rawDoc, len(globalIDs))
	for rows.Next() {
		var doc rawDoc
		if err := rows.Scan(&doc.globalID, &doc.manifest.Title, &doc.manifest.LocalizedTitle, &doc.content, &doc.manifest.DocType, &doc.parentPath, &doc.manifest.Level, &doc.manifest.Slug, &doc.manifest.Path, &doc.manifest.Status); err != nil {
			return nil, fmt.Errorf("DB scan error: %v", err)
		}
		doc.manifest.ParentPath = doc.parentPath
		found[doc.globalID] = doc
	}
	if err := rows.Err(); err != nil {
		return nil, fmt.Errorf("DB rows error: %v", err)
	}

	docs := make([]projectDocBundleDoc, 0, len(localIDs))
	seenPaths := make(map[string]struct{}, len(localIDs))
	for _, localID := range localIDs {
		raw, ok := found[globalByLocal[localID]]
		if !ok {
			return nil, fmt.Errorf("project_doc_id %d not found in current project", localID)
		}
		if err := validateProjectDocBundleCanonicalSegment(raw.manifest.Slug, "slug"); err != nil {
			return nil, err
		}
		if err := validateProjectDocBundleCanonicalPath(raw.manifest.Path, raw.manifest.Slug); err != nil {
			return nil, err
		}
		if raw.manifest.ParentPath != "" {
			if err := validateProjectDocBundleCanonicalPath(raw.manifest.ParentPath, filepath.Base(raw.manifest.ParentPath)); err != nil {
				return nil, fmt.Errorf("selected document has unsafe parent path: %v", err)
			}
		}
		raw.manifest.ProjectDocId = localID
		raw.manifest.ArchivePath = filepath.ToSlash(filepath.Join("docs", raw.manifest.Path+".md"))
		if !strings.HasPrefix(raw.manifest.ArchivePath, "docs/") {
			return nil, fmt.Errorf("selected document archive path escapes docs/")
		}
		if _, exists := seenPaths[raw.manifest.ArchivePath]; exists {
			return nil, fmt.Errorf("selected documents have duplicate archive path %q", raw.manifest.ArchivePath)
		}
		seenPaths[raw.manifest.ArchivePath] = struct{}{}
		docs = append(docs, projectDocBundleDoc{Manifest: raw.manifest, Content: raw.content})
	}
	return docs, nil
}

func writeProjectDocBundleArchive(outputPath string, manifest []byte, docs []projectDocBundleDoc) (retErr error) {
	if err := os.MkdirAll(filepath.Dir(outputPath), 0o755); err != nil {
		return fmt.Errorf("failed to create output directory: %v", err)
	}
	if _, err := os.Lstat(outputPath); err == nil {
		return fmt.Errorf("output path already exists: %s", outputPath)
	} else if !os.IsNotExist(err) {
		return fmt.Errorf("failed to inspect output path: %v", err)
	}

	temp, err := os.CreateTemp(filepath.Dir(outputPath), "."+filepath.Base(outputPath)+".tmp-*")
	if err != nil {
		return fmt.Errorf("failed to create temporary archive: %v", err)
	}
	tempPath := temp.Name()
	keepTemp := true
	defer func() {
		if keepTemp {
			_ = os.Remove(tempPath)
		}
	}()

	archive := zip.NewWriter(temp)
	writeEntry := func(name string, content []byte) error {
		if name == "" || strings.HasPrefix(name, "/") || strings.Contains(name, `\`) || pathpkg.Clean(name) != name || (name != "manifest.json" && !strings.HasPrefix(name, "docs/")) {
			return fmt.Errorf("unsafe archive entry name %q", name)
		}
		entry, err := archive.Create(name)
		if err != nil {
			return err
		}
		_, err = io.Copy(entry, strings.NewReader(string(content)))
		return err
	}
	if err := writeEntry("manifest.json", manifest); err != nil {
		_ = archive.Close()
		_ = temp.Close()
		return fmt.Errorf("failed to write manifest: %v", err)
	}
	archiveDocs := append([]projectDocBundleDoc(nil), docs...)
	sort.Slice(archiveDocs, func(i, j int) bool { return archiveDocs[i].Manifest.ArchivePath < archiveDocs[j].Manifest.ArchivePath })
	for _, doc := range archiveDocs {
		if err := writeEntry(doc.Manifest.ArchivePath, []byte(doc.Content)); err != nil {
			_ = archive.Close()
			_ = temp.Close()
			return fmt.Errorf("failed to write %s: %v", doc.Manifest.ArchivePath, err)
		}
	}
	if err := archive.Close(); err != nil {
		_ = temp.Close()
		return fmt.Errorf("failed to close archive: %v", err)
	}
	if err := temp.Sync(); err != nil {
		_ = temp.Close()
		return fmt.Errorf("failed to sync archive: %v", err)
	}
	if err := temp.Close(); err != nil {
		return fmt.Errorf("failed to close temporary archive: %v", err)
	}
	if _, err := os.Lstat(outputPath); err == nil {
		return fmt.Errorf("output path already exists: %s", outputPath)
	} else if !os.IsNotExist(err) {
		return fmt.Errorf("failed to inspect output path: %v", err)
	}
	if err := os.Rename(tempPath, outputPath); err != nil {
		return fmt.Errorf("failed to atomically publish archive: %v", err)
	}
	keepTemp = false
	return nil
}

func ExportProjectDocBundle(ctx context.Context, req *mcp.CallToolRequest, input ExportProjectDocBundleInput) (*mcp.CallToolResult, ExportProjectDocBundleOutput, error) {
	if authorizedRole != "fixer" {
		return &mcp.CallToolResult{IsError: true}, ExportProjectDocBundleOutput{}, fmt.Errorf("access denied: requires fixer role")
	}
	localIDs, err := normalizeProjectDocBundleIDs(input.ProjectDocIds)
	if err != nil {
		return &mcp.CallToolResult{IsError: true}, ExportProjectDocBundleOutput{}, err
	}
	projectRoot, err := projectCWDFromID(authorizedProjectId)
	if err != nil {
		return &mcp.CallToolResult{IsError: true}, ExportProjectDocBundleOutput{}, fmt.Errorf("failed to resolve project root: %v", err)
	}
	projectName, err := projectNameFromID(authorizedProjectId)
	if err != nil {
		return &mcp.CallToolResult{IsError: true}, ExportProjectDocBundleOutput{}, fmt.Errorf("failed to resolve project name: %v", err)
	}
	outputPath, err := resolveProjectDocBundlePath(projectRoot, projectName, input.Path)
	if err != nil {
		return &mcp.CallToolResult{IsError: true}, ExportProjectDocBundleOutput{}, err
	}
	docs, err := fetchProjectDocBundleDocs(authorizedProjectId, localIDs)
	if err != nil {
		return &mcp.CallToolResult{IsError: true}, ExportProjectDocBundleOutput{}, err
	}
	language, err := projectDocLanguage(authorizedProjectId)
	if err != nil {
		return &mcp.CallToolResult{IsError: true}, ExportProjectDocBundleOutput{}, fmt.Errorf("failed to read documentation language: %v", err)
	}
	manifestDocs := make([]ProjectDocBundleManifestDoc, 0, len(docs))
	for _, doc := range docs {
		manifestDocs = append(manifestDocs, doc.Manifest)
	}
	manifest := ProjectDocBundleManifest{
		SchemaVersion:         projectDocBundleSchemaVersion,
		ExportedAt:            time.Now().UTC().Format(time.RFC3339),
		ProjectId:             authorizedProjectId,
		ProjectName:           projectName,
		ProjectRoot:           projectRoot,
		DocumentationLanguage: language,
		Docs:                  manifestDocs,
	}
	manifestBytes, err := json.MarshalIndent(manifest, "", "  ")
	if err != nil {
		return &mcp.CallToolResult{IsError: true}, ExportProjectDocBundleOutput{}, fmt.Errorf("failed to encode manifest: %v", err)
	}
	manifestBytes = append(manifestBytes, '\n')
	if err := writeProjectDocBundleArchive(outputPath, manifestBytes, docs); err != nil {
		return &mcp.CallToolResult{IsError: true}, ExportProjectDocBundleOutput{}, err
	}
	info, err := os.Stat(outputPath)
	if err != nil {
		return &mcp.CallToolResult{IsError: true}, ExportProjectDocBundleOutput{}, fmt.Errorf("failed to stat archive: %v", err)
	}
	return nil, ExportProjectDocBundleOutput{
		Status:        "success",
		Path:          outputPath,
		ProjectId:     authorizedProjectId,
		ProjectDocIds: localIDs,
		DocsCount:     len(docs),
		ArchiveBytes:  info.Size(),
	}, nil
}
