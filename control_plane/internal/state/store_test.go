package state

import (
	"os"
	"path/filepath"
	"strings"
	"testing"

	"github.com/fixer-mcp/control-plane/internal/domain"
)

func TestStorePersistsOnlyUICacheWithPrivatePermissions(t *testing.T) {
	path := filepath.Join(t.TempDir(), "nested", "console.json")
	store, err := Load(path)
	if err != nil {
		t.Fatal(err)
	}
	if err := store.RememberProject(domain.Project{ID: "project", Name: "demo", Path: "/tmp/demo"}); err != nil {
		t.Fatal(err)
	}
	if err := store.SaveLaunchDraft(domain.LaunchDraft{ID: "draft", ProjectID: "project", Provider: "pi", Model: "model"}); err != nil {
		t.Fatal(err)
	}
	if err := store.UpsertLocalContext(domain.LocalContext{ID: "context", ProjectID: "project", ProjectPath: "/tmp/demo", State: domain.SessionDetached}); err != nil {
		t.Fatal(err)
	}
	loaded, err := Load(path)
	if err != nil {
		t.Fatal(err)
	}
	snapshot := loaded.Snapshot()
	if snapshot.LastProjectID != "project" || len(snapshot.LaunchDrafts) != 1 || len(snapshot.LocalContexts) != 1 {
		t.Fatalf("unexpected snapshot: %+v", snapshot)
	}
	payload, err := os.ReadFile(path)
	if err != nil {
		t.Fatal(err)
	}
	for _, forbidden := range []string{`"projects"`, `"profiles"`, `"sessions"`} {
		if strings.Contains(string(payload), forbidden) {
			t.Fatalf("legacy second-truth field %s persisted: %s", forbidden, payload)
		}
	}
	info, err := os.Stat(path)
	if err != nil {
		t.Fatal(err)
	}
	if info.Mode().Perm() != 0o600 {
		t.Fatalf("state permissions = %o", info.Mode().Perm())
	}
}

func TestLoadMigratesLegacyConsoleStateIntoUICache(t *testing.T) {
	path := filepath.Join(t.TempDir(), "console.json")
	legacy := `{"version":1,"last_project_id":"project","projects":[{"id":"project","name":"demo","path":"/tmp/demo"}],"profiles":[{"id":"draft","project_id":"project"}],"sessions":[{"id":"context","state":"detached"}]}`
	if err := os.WriteFile(path, []byte(legacy), 0o600); err != nil {
		t.Fatal(err)
	}
	store, err := Load(path)
	if err != nil {
		t.Fatal(err)
	}
	snapshot := store.Snapshot()
	if snapshot.Version != 2 || len(snapshot.RecentProjects) != 1 || len(snapshot.LaunchDrafts) != 1 || len(snapshot.LocalContexts) != 1 {
		t.Fatalf("legacy state was not migrated: %+v", snapshot)
	}
}
