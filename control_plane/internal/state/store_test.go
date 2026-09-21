package state

import (
	"os"
	"path/filepath"
	"testing"

	"github.com/fixer-mcp/control-plane/internal/domain"
)

func TestStorePersistsProjectsProfilesAndSessionsWithPrivatePermissions(t *testing.T) {
	path := filepath.Join(t.TempDir(), "nested", "console.json")
	store, err := Load(path)
	if err != nil {
		t.Fatal(err)
	}
	if err := store.TouchProject(domain.Project{ID: "project", Name: "demo", Path: "/tmp/demo"}); err != nil {
		t.Fatal(err)
	}
	if err := store.UpsertProfile(domain.LaunchProfile{ID: "profile", ProjectID: "project", Provider: "pi", Model: "model"}); err != nil {
		t.Fatal(err)
	}
	if err := store.UpsertSession(domain.Session{ID: "session", ProjectID: "project", ProjectPath: "/tmp/demo", State: domain.SessionDetached}); err != nil {
		t.Fatal(err)
	}
	loaded, err := Load(path)
	if err != nil {
		t.Fatal(err)
	}
	snapshot := loaded.Snapshot()
	if snapshot.LastProjectID != "project" || len(snapshot.Profiles) != 1 || len(snapshot.Sessions) != 1 {
		t.Fatalf("unexpected snapshot: %+v", snapshot)
	}
	info, err := os.Stat(path)
	if err != nil {
		t.Fatal(err)
	}
	if info.Mode().Perm() != 0o600 {
		t.Fatalf("state permissions = %o", info.Mode().Perm())
	}
}
