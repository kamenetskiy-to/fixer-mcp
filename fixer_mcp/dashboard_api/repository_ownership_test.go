package dashboardapi

import (
	"os"
	"strings"
	"testing"
)

func TestRepositoryDomainsLiveOutsideRepositoryFacade(t *testing.T) {
	facadeSource, err := os.ReadFile("repository.go")
	if err != nil {
		t.Fatalf("read repository.go: %v", err)
	}

	cases := []struct {
		file    string
		symbols []string
	}{
		{
			file: "repository_docs.go",
			symbols: []string{
				"func (r *Repository) ProjectDocs(",
				"func (r *Repository) SetProposalStatus(",
				"func (r *Repository) loadAttachedDocs(",
				"func (r *Repository) loadSessionProposals(",
			},
		},
		{
			file: "repository_sessions.go",
			symbols: []string{
				"func (r *Repository) ProjectNetrunners(",
				"func (r *Repository) NetrunnerDetail(",
				"func (r *Repository) CreateTask(",
				"func (r *Repository) loadSessionSummaries(",
			},
		},
		{
			file: "repository_chat.go",
			symbols: []string{
				"func (r *Repository) FixerChatBinding(",
				"func loadCodexChatSessions(",
				"func rolesFromMarkers(",
			},
		},
		{
			file: "repository_activity.go",
			symbols: []string{
				"func (r *Repository) loadAutonomousStatuses(",
				"func (r *Repository) loadActiveWorkers(",
				"func (r *Repository) loadRunningWorkersBySession(",
			},
		},
		{
			file: "repository_helpers.go",
			symbols: []string{
				"func normalizeProjectCWD(",
				"func resolveDatabasePath(",
				"func decodeStructuredFinalReport(",
			},
		},
	}

	for _, tc := range cases {
		source, err := os.ReadFile(tc.file)
		if err != nil {
			t.Fatalf("read %s: %v", tc.file, err)
		}
		for _, symbol := range tc.symbols {
			if strings.Contains(string(facadeSource), symbol) {
				t.Fatalf("expected %q to be extracted out of repository.go", symbol)
			}
			if !strings.Contains(string(source), symbol) {
				t.Fatalf("expected %q in %s", symbol, tc.file)
			}
		}
	}
}
