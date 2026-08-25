package dashboardapi

import (
	"context"
	"errors"
	"fmt"
	"os"
	"path/filepath"
	"regexp"
	"sort"
	"strings"
)

const skillFilename = "SKILL.md"

var (
	ErrInvalidSkillPath = errors.New("invalid skill path")
	ErrSkillNotFound    = errors.New("managed skill not found")

	managedSkillNames = map[string]struct{}{
		"init-fixer": {}, "init-unattached-fixer": {}, "init-overseer": {},
		"maintain-project-docs": {}, "hands-netrunner": {}, "run-netrunner-wave": {},
		"review-netrunner-session": {}, "complete-netrunner-session": {},
		"inspect-netrunner-transcript": {}, "bridge-overseer-fixer": {},
		"save-fixer-handoff": {}, "refresh-project-overview": {},
		"run-fixer-image-job": {}, "figma-frontend-works": {},
		"shadcn-ui-flutter": {}, "design-system-works": {},
		"fixer-backend-providers": {}, "run-visual-verifier": {},
		"share-project": {},
	}
	managedSkillRoots = []skillRoot{
		{ID: "agents", RelativePath: ".agents/skills", Label: "Agents"},
		{ID: "factory", RelativePath: ".factory/skills", Label: "Droid"},
		{ID: "claude", RelativePath: ".claude/skills", Label: "Claude"},
		{ID: "junie", RelativePath: ".junie/fixer-runtime/skills", Label: "Junie"},
		{ID: "kimi-code", RelativePath: ".kimi-code/skills", Label: "Kimi Code"},
	}
	skillNamePattern      = regexp.MustCompile(`^[a-z0-9][a-z0-9-]*$`)
	skillReferencePattern = regexp.MustCompile("`?\\$([a-z0-9][a-z0-9-]*)`?")
)

type skillRoot struct {
	ID           string
	RelativePath string
	Label        string
}

type SkillLocation struct {
	RootID       string `json:"root_id"`
	RootLabel    string `json:"root_label"`
	RelativePath string `json:"relative_path"`
}

type ManagedSkillSummary struct {
	Name          string          `json:"name"`
	Description   string          `json:"description,omitempty"`
	Locations     []SkillLocation `json:"locations"`
	RelatedSkills []string        `json:"related_skills"`
}

type ManagedSkillDetail struct {
	ManagedSkillSummary
	RootID       string `json:"root_id"`
	RelativePath string `json:"relative_path"`
	Content      string `json:"content"`
}

type ProjectSkillsResponse struct {
	Project ProjectHeader         `json:"project"`
	Skills  []ManagedSkillSummary `json:"skills"`
}

type UpdateManagedSkillInput struct {
	RootID  string `json:"root_id"`
	Content string `json:"content"`
}

// ProjectSkills returns only Fixer-owned skills materialized inside the
// selected project. Duplicate backend materializations are represented as
// locations on one logical skill.
func (r *Repository) ProjectSkills(ctx context.Context, projectID int) (ProjectSkillsResponse, error) {
	project, err := r.requireProject(ctx, projectID)
	if err != nil {
		return ProjectSkillsResponse{}, err
	}
	root, err := normalizeProjectCWD(project.CWD)
	if err != nil {
		return ProjectSkillsResponse{}, err
	}

	byName := map[string]*ManagedSkillSummary{}
	for _, configuredRoot := range managedSkillRoots {
		entries, err := safeSkillRootEntries(root, configuredRoot)
		if err != nil {
			return ProjectSkillsResponse{}, err
		}
		for _, entry := range entries {
			if !entry.IsDir() || !isManagedSkillName(entry.Name()) {
				continue
			}
			path, relativePath, err := resolveManagedSkillPath(root, configuredRoot.ID, entry.Name())
			if err != nil {
				// An unsafe materialization must never hide safe copies in another
				// backend root, but it is not exposed or followed.
				continue
			}
			content, err := os.ReadFile(path)
			if err != nil {
				if errors.Is(err, os.ErrNotExist) {
					continue
				}
				return ProjectSkillsResponse{}, err
			}
			name, description, valid := skillFrontMatter(string(content))
			if !valid || name != entry.Name() {
				// Managed directory identity is authoritative. A mismatched
				// front-matter name is treated as an unmanaged/corrupt copy.
				continue
			}
			summary := byName[name]
			if summary == nil {
				summary = &ManagedSkillSummary{
					Name: name, Description: description,
					Locations:     []SkillLocation{},
					RelatedSkills: relatedManagedSkills(string(content), name),
				}
				byName[name] = summary
			}
			if summary.Description == "" {
				summary.Description = description
			}
			summary.RelatedSkills = mergeSortedStrings(summary.RelatedSkills, relatedManagedSkills(string(content), name))
			summary.Locations = append(summary.Locations, SkillLocation{
				RootID: configuredRoot.ID, RootLabel: configuredRoot.Label, RelativePath: relativePath,
			})
		}
	}

	skills := make([]ManagedSkillSummary, 0, len(byName))
	for _, summary := range byName {
		skills = append(skills, *summary)
	}
	sort.Slice(skills, func(i, j int) bool { return skills[i].Name < skills[j].Name })
	return ProjectSkillsResponse{
		Project: ProjectHeader{ID: project.ID, Name: project.Name, CWD: project.CWD},
		Skills:  skills,
	}, nil
}

// ProjectSkill reads one exact backend materialization. Supplying rootID avoids
// silently editing a generated copy when several clients are materialized.
func (r *Repository) ProjectSkill(ctx context.Context, projectID int, rootID, name string) (ManagedSkillDetail, error) {
	project, err := r.requireProject(ctx, projectID)
	if err != nil {
		return ManagedSkillDetail{}, err
	}
	projectRoot, err := normalizeProjectCWD(project.CWD)
	if err != nil {
		return ManagedSkillDetail{}, err
	}
	path, relativePath, err := resolveManagedSkillPath(projectRoot, rootID, name)
	if err != nil {
		return ManagedSkillDetail{}, err
	}
	content, err := os.ReadFile(path)
	if errors.Is(err, os.ErrNotExist) {
		return ManagedSkillDetail{}, ErrSkillNotFound
	}
	if err != nil {
		return ManagedSkillDetail{}, err
	}
	frontMatterName, description, valid := skillFrontMatter(string(content))
	if !valid || frontMatterName != name {
		return ManagedSkillDetail{}, ErrSkillNotFound
	}
	location := skillRootByID(rootID)
	return ManagedSkillDetail{
		ManagedSkillSummary: ManagedSkillSummary{
			Name: name, Description: description,
			Locations:     []SkillLocation{{RootID: rootID, RootLabel: location.Label, RelativePath: relativePath}},
			RelatedSkills: relatedManagedSkills(string(content), name),
		},
		RootID: rootID, RelativePath: relativePath, Content: string(content),
	}, nil
}

// UpdateProjectSkill validates the existing file both before and immediately
// before an atomic rename. It never creates directories or follows symlinks.
func (r *Repository) UpdateProjectSkill(ctx context.Context, projectID int, name string, input UpdateManagedSkillInput) (ManagedSkillDetail, error) {
	if strings.TrimSpace(input.Content) == "" {
		return ManagedSkillDetail{}, fmt.Errorf("%w: skill content is required", ErrInvalidSkillPath)
	}
	project, err := r.requireProject(ctx, projectID)
	if err != nil {
		return ManagedSkillDetail{}, err
	}
	projectRoot, err := normalizeProjectCWD(project.CWD)
	if err != nil {
		return ManagedSkillDetail{}, err
	}
	path, _, err := resolveManagedSkillPath(projectRoot, input.RootID, name)
	if err != nil {
		return ManagedSkillDetail{}, err
	}
	if _, err := os.Stat(path); errors.Is(err, os.ErrNotExist) {
		return ManagedSkillDetail{}, ErrSkillNotFound
	} else if err != nil {
		return ManagedSkillDetail{}, err
	}
	frontMatterName, _, valid := skillFrontMatter(input.Content)
	if !valid || frontMatterName != name {
		return ManagedSkillDetail{}, fmt.Errorf("%w: front-matter name must match %q", ErrInvalidSkillPath, name)
	}

	dir := filepath.Dir(path)
	temp, err := os.CreateTemp(dir, ".skill-edit-*")
	if err != nil {
		return ManagedSkillDetail{}, err
	}
	tempPath := temp.Name()
	defer os.Remove(tempPath)
	if err := temp.Chmod(0o644); err != nil {
		_ = temp.Close()
		return ManagedSkillDetail{}, err
	}
	if _, err := temp.WriteString(input.Content); err != nil {
		_ = temp.Close()
		return ManagedSkillDetail{}, err
	}
	if err := temp.Sync(); err != nil {
		_ = temp.Close()
		return ManagedSkillDetail{}, err
	}
	if err := temp.Close(); err != nil {
		return ManagedSkillDetail{}, err
	}
	// Re-resolve after writing the temporary file to narrow symlink-swap risk.
	resolvedAgain, _, err := resolveManagedSkillPath(projectRoot, input.RootID, name)
	if err != nil || resolvedAgain != path {
		return ManagedSkillDetail{}, fmt.Errorf("%w: skill path changed during edit", ErrInvalidSkillPath)
	}
	if err := os.Rename(tempPath, path); err != nil {
		return ManagedSkillDetail{}, err
	}
	return r.ProjectSkill(ctx, projectID, input.RootID, name)
}

func safeSkillRootEntries(projectRoot string, root skillRoot) ([]os.DirEntry, error) {
	path := filepath.Join(projectRoot, filepath.FromSlash(root.RelativePath))
	if err := rejectSymlinkComponents(projectRoot, path); err != nil {
		if errors.Is(err, os.ErrNotExist) {
			return []os.DirEntry{}, nil
		}
		// A symlinked managed root is ignored instead of followed.
		if errors.Is(err, ErrInvalidSkillPath) {
			return []os.DirEntry{}, nil
		}
		return nil, err
	}
	entries, err := os.ReadDir(path)
	if errors.Is(err, os.ErrNotExist) {
		return []os.DirEntry{}, nil
	}
	return entries, err
}

func resolveManagedSkillPath(projectRoot, rootID, name string) (string, string, error) {
	if !isManagedSkillName(name) {
		return "", "", fmt.Errorf("%w: unknown or malformed skill name", ErrInvalidSkillPath)
	}
	root := skillRootByID(rootID)
	if root.ID == "" {
		return "", "", fmt.Errorf("%w: unknown skill root", ErrInvalidSkillPath)
	}
	relativePath := filepath.Join(filepath.FromSlash(root.RelativePath), name, skillFilename)
	path := filepath.Join(projectRoot, relativePath)
	if err := rejectSymlinkComponents(projectRoot, path); err != nil {
		if errors.Is(err, os.ErrNotExist) {
			return "", "", ErrSkillNotFound
		}
		return "", "", err
	}
	return path, filepath.ToSlash(relativePath), nil
}

func rejectSymlinkComponents(projectRoot, target string) error {
	relative, err := filepath.Rel(projectRoot, target)
	if err != nil || relative == ".." || strings.HasPrefix(relative, ".."+string(filepath.Separator)) || filepath.IsAbs(relative) {
		return fmt.Errorf("%w: path escapes project", ErrInvalidSkillPath)
	}
	current := projectRoot
	for _, part := range strings.Split(relative, string(filepath.Separator)) {
		current = filepath.Join(current, part)
		info, err := os.Lstat(current)
		if err != nil {
			return err
		}
		if info.Mode()&os.ModeSymlink != 0 {
			return fmt.Errorf("%w: symlinked skill path", ErrInvalidSkillPath)
		}
	}
	return nil
}

func isManagedSkillName(name string) bool {
	if !skillNamePattern.MatchString(name) {
		return false
	}
	_, ok := managedSkillNames[name]
	return ok
}

func skillRootByID(id string) skillRoot {
	for _, root := range managedSkillRoots {
		if root.ID == id {
			return root
		}
	}
	return skillRoot{}
}

func skillFrontMatter(content string) (string, string, bool) {
	lines := strings.Split(content, "\n")
	if len(lines) == 0 || strings.TrimSpace(lines[0]) != "---" {
		return "", "", false
	}
	name := ""
	description := ""
	closed := false
	for _, line := range lines[1:] {
		if strings.TrimSpace(line) == "---" {
			closed = true
			break
		}
		key, value, found := strings.Cut(line, ":")
		if !found {
			continue
		}
		value = strings.Trim(strings.TrimSpace(value), `"'`)
		switch strings.TrimSpace(key) {
		case "name":
			if value != "" {
				name = value
			}
		case "description":
			description = value
		}
	}
	return name, description, closed && isManagedSkillName(name)
}

func relatedManagedSkills(content, self string) []string {
	seen := map[string]struct{}{}
	for _, match := range skillReferencePattern.FindAllStringSubmatch(content, -1) {
		name := match[1]
		if name != self && isManagedSkillName(name) {
			seen[name] = struct{}{}
		}
	}
	result := make([]string, 0, len(seen))
	for name := range seen {
		result = append(result, name)
	}
	sort.Strings(result)
	return result
}

func mergeSortedStrings(left, right []string) []string {
	seen := map[string]struct{}{}
	for _, value := range append(append([]string{}, left...), right...) {
		seen[value] = struct{}{}
	}
	result := make([]string, 0, len(seen))
	for value := range seen {
		result = append(result, value)
	}
	sort.Strings(result)
	return result
}
