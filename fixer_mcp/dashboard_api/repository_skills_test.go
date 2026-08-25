package dashboardapi

import (
	"context"
	"errors"
	"os"
	"path/filepath"
	"testing"
)

func TestProjectSkillsListsOnlyManagedProjectLocalSkills(t *testing.T) {
	repo := openFixtureRepository(t)
	defer repo.Close()

	writeFixtureSkill(t, repo.currentProjectCWD, ".agents/skills", "init-fixer", `---
name: init-fixer
description: Initialize a project Fixer.
---
Use $run-netrunner-wave for parallel work.
`)
	writeFixtureSkill(t, repo.currentProjectCWD, ".claude/skills", "init-fixer", `---
name: init-fixer
description: Claude materialization.
---
`)
	writeFixtureSkill(t, repo.currentProjectCWD, ".kimi-code/skills", "init-fixer", `---
name: init-fixer
description: Kimi Code materialization.
---
`)
	writeFixtureSkill(t, repo.currentProjectCWD, ".agents/skills", "personal-skill", `---
name: personal-skill
description: Must remain outside Fixer management.
---
`)
	writeFixtureSkill(t, repo.currentProjectCWD, ".agents/skills", "run-netrunner-wave", `---
name: different-name
description: A corrupt managed copy.
---
`)

	response, err := repo.ProjectSkills(context.Background(), 1)
	if err != nil {
		t.Fatalf("list project skills: %v", err)
	}
	if response.Project.ID != 1 || response.Project.CWD != repo.currentProjectCWD {
		t.Fatalf("unexpected project header: %+v", response.Project)
	}
	if len(response.Skills) != 1 {
		t.Fatalf("expected one logical managed skill, got %+v", response.Skills)
	}
	skill := response.Skills[0]
	if skill.Name != "init-fixer" || skill.Description != "Initialize a project Fixer." {
		t.Fatalf("unexpected skill metadata: %+v", skill)
	}
	if len(skill.Locations) != 3 || skill.Locations[0].RootID != "agents" || skill.Locations[1].RootID != "claude" || skill.Locations[2].RootID != "kimi-code" {
		t.Fatalf("expected ordered materializations, got %+v", skill.Locations)
	}
	if len(skill.RelatedSkills) != 1 || skill.RelatedSkills[0] != "run-netrunner-wave" {
		t.Fatalf("expected obvious managed relationship, got %+v", skill.RelatedSkills)
	}
}

func TestProjectSkillsIncludesBackendProvidersAndVisualVerifier(t *testing.T) {
	repo := openFixtureRepository(t)
	defer repo.Close()

	for _, name := range []string{"fixer-backend-providers", "run-visual-verifier"} {
		writeFixtureSkill(t, repo.currentProjectCWD, ".agents/skills", name, "---\nname: "+name+"\ndescription: Managed Fixer skill.\n---\n")
	}
	writeFixtureSkill(t, repo.currentProjectCWD, ".agents/skills", "personal-skill", `---
name: personal-skill
description: Must remain outside Fixer management.
---
`)

	response, err := repo.ProjectSkills(context.Background(), 1)
	if err != nil {
		t.Fatalf("list project skills: %v", err)
	}
	if len(response.Skills) != 2 || response.Skills[0].Name != "fixer-backend-providers" || response.Skills[1].Name != "run-visual-verifier" {
		t.Fatalf("expected both newly managed skills and no personal skills, got %+v", response.Skills)
	}
	for _, name := range []string{"fixer-backend-providers", "run-visual-verifier"} {
		detail, err := repo.ProjectSkill(context.Background(), 1, "agents", name)
		if err != nil {
			t.Fatalf("read managed skill %q: %v", name, err)
		}
		if detail.Name != name || detail.Content == "" {
			t.Fatalf("unexpected detail for %q: %+v", name, detail)
		}
	}
	if _, err := repo.ProjectSkill(context.Background(), 1, "agents", "personal-skill"); !errors.Is(err, ErrInvalidSkillPath) {
		t.Fatalf("expected personal skill to remain excluded, got %v", err)
	}
}

func TestProjectSkillReadsAndAtomicallyUpdatesExactMaterialization(t *testing.T) {
	repo := openFixtureRepository(t)
	defer repo.Close()

	writeFixtureSkill(t, repo.currentProjectCWD, ".agents/skills", "save-fixer-handoff", `---
name: save-fixer-handoff
description: Old description.
---
Old body.
`)

	detail, err := repo.ProjectSkill(context.Background(), 1, "agents", "save-fixer-handoff")
	if err != nil {
		t.Fatalf("read managed skill: %v", err)
	}
	if detail.RelativePath != ".agents/skills/save-fixer-handoff/SKILL.md" {
		t.Fatalf("unexpected relative path: %q", detail.RelativePath)
	}

	updatedContent := `---
name: save-fixer-handoff
description: Persist a concise handoff.
---
Use $refresh-project-overview first.
`
	updated, err := repo.UpdateProjectSkill(context.Background(), 1, "save-fixer-handoff", UpdateManagedSkillInput{
		RootID: "agents", Content: updatedContent,
	})
	if err != nil {
		t.Fatalf("update managed skill: %v", err)
	}
	if updated.Content != updatedContent || updated.Description != "Persist a concise handoff." {
		t.Fatalf("unexpected updated detail: %+v", updated)
	}
	if len(updated.RelatedSkills) != 1 || updated.RelatedSkills[0] != "refresh-project-overview" {
		t.Fatalf("unexpected relationships: %+v", updated.RelatedSkills)
	}
}

func TestProjectSkillRejectsTraversalUnknownRootsAndNameMismatch(t *testing.T) {
	repo := openFixtureRepository(t)
	defer repo.Close()
	writeFixtureSkill(t, repo.currentProjectCWD, ".agents/skills", "init-fixer", `---
name: init-fixer
---
`)

	for _, tc := range []struct {
		root string
		name string
	}{
		{root: "../outside", name: "init-fixer"},
		{root: "agents", name: "../init-fixer"},
		{root: "agents", name: "personal-skill"},
	} {
		_, err := repo.ProjectSkill(context.Background(), 1, tc.root, tc.name)
		if !errors.Is(err, ErrInvalidSkillPath) {
			t.Fatalf("expected invalid path for root=%q name=%q, got %v", tc.root, tc.name, err)
		}
	}

	_, err := repo.UpdateProjectSkill(context.Background(), 1, "init-fixer", UpdateManagedSkillInput{
		RootID:  "agents",
		Content: "---\nname: init-overseer\n---\n",
	})
	if !errors.Is(err, ErrInvalidSkillPath) {
		t.Fatalf("expected mismatched front matter to fail, got %v", err)
	}
	_, err = repo.UpdateProjectSkill(context.Background(), 1, "init-fixer", UpdateManagedSkillInput{
		RootID:  "agents",
		Content: "# Missing front matter\n",
	})
	if !errors.Is(err, ErrInvalidSkillPath) {
		t.Fatalf("expected missing front matter to fail, got %v", err)
	}
}

func TestProjectSkillsNeverFollowsSymlinkedRootsOrFiles(t *testing.T) {
	repo := openFixtureRepository(t)
	defer repo.Close()

	outside := t.TempDir()
	writeFixtureSkill(t, outside, "skills", "init-fixer", `---
name: init-fixer
description: Outside project.
---
`)
	if err := os.MkdirAll(filepath.Join(repo.currentProjectCWD, ".agents"), 0o755); err != nil {
		t.Fatalf("mkdir agents parent: %v", err)
	}
	if err := os.Symlink(filepath.Join(outside, "skills"), filepath.Join(repo.currentProjectCWD, ".agents", "skills")); err != nil {
		t.Fatalf("symlink managed root: %v", err)
	}

	response, err := repo.ProjectSkills(context.Background(), 1)
	if err != nil {
		t.Fatalf("list with symlinked root: %v", err)
	}
	if len(response.Skills) != 0 {
		t.Fatalf("symlinked root leaked skills: %+v", response.Skills)
	}
	_, err = repo.ProjectSkill(context.Background(), 1, "agents", "init-fixer")
	if !errors.Is(err, ErrInvalidSkillPath) {
		t.Fatalf("expected direct symlink read rejection, got %v", err)
	}

	if err := os.Remove(filepath.Join(repo.currentProjectCWD, ".agents", "skills")); err != nil {
		t.Fatalf("remove root symlink: %v", err)
	}
	if err := os.MkdirAll(filepath.Join(repo.currentProjectCWD, ".agents", "skills", "init-fixer"), 0o755); err != nil {
		t.Fatalf("mkdir skill: %v", err)
	}
	outsideFile := filepath.Join(outside, skillFilename)
	if err := os.WriteFile(outsideFile, []byte("---\nname: init-fixer\n---\n"), 0o644); err != nil {
		t.Fatalf("write outside skill: %v", err)
	}
	if err := os.Symlink(outsideFile, filepath.Join(repo.currentProjectCWD, ".agents", "skills", "init-fixer", skillFilename)); err != nil {
		t.Fatalf("symlink skill file: %v", err)
	}
	response, err = repo.ProjectSkills(context.Background(), 1)
	if err != nil || len(response.Skills) != 0 {
		t.Fatalf("symlinked file must be ignored, response=%+v err=%v", response, err)
	}
}

func writeFixtureSkill(t *testing.T, projectRoot, relativeRoot, name, content string) {
	t.Helper()
	dir := filepath.Join(projectRoot, filepath.FromSlash(relativeRoot), name)
	if err := os.MkdirAll(dir, 0o755); err != nil {
		t.Fatalf("mkdir fixture skill: %v", err)
	}
	if err := os.WriteFile(filepath.Join(dir, skillFilename), []byte(content), 0o644); err != nil {
		t.Fatalf("write fixture skill: %v", err)
	}
}
