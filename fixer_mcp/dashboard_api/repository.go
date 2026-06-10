package dashboardapi

import (
	"context"
	"database/sql"
	"strings"

	_ "github.com/glebarez/go-sqlite"
)

const defaultFixerDBFilename = "fixer.db"

const (
	fixerSkillMarker     = "Activate skill `$init-fixer` immediately."
	overseerSkillMarker  = "Activate skill `$init-overseer` immediately."
	netrunnerSkillMarker = "Activate skill `$run-manual-netrunner` immediately."
	maxRoleMarkerLines   = 240
	maxCodexChatSessions = 12
	maxCodexSessionScan  = 160
)

type Repository struct {
	db                *sql.DB
	dbWrite           *sql.DB
	databasePath      string
	currentProjectCWD string
}

func OpenRepository(databasePath string, currentProjectCWD string) (*Repository, error) {
	resolvedDBPath, err := resolveDatabasePath(databasePath)
	if err != nil {
		return nil, err
	}
	db, err := sql.Open("sqlite", resolvedDBPath)
	if err != nil {
		return nil, err
	}
	db.SetMaxOpenConns(4)
	db.SetMaxIdleConns(4)
	if _, err := db.Exec("PRAGMA busy_timeout = 5000; PRAGMA query_only = ON;"); err != nil {
		_ = db.Close()
		return nil, err
	}
	dbWrite, err := sql.Open("sqlite", resolvedDBPath)
	if err != nil {
		_ = db.Close()
		return nil, err
	}
	dbWrite.SetMaxOpenConns(1)
	dbWrite.SetMaxIdleConns(1)
	if _, err := dbWrite.Exec("PRAGMA busy_timeout = 5000; PRAGMA foreign_keys = ON;"); err != nil {
		_ = db.Close()
		_ = dbWrite.Close()
		return nil, err
	}
	normalizedCWD := ""
	if strings.TrimSpace(currentProjectCWD) != "" {
		normalizedCWD, err = normalizeProjectCWD(currentProjectCWD)
		if err != nil {
			_ = db.Close()
			_ = dbWrite.Close()
			return nil, err
		}
	}
	return &Repository{
		db:                db,
		dbWrite:           dbWrite,
		databasePath:      resolvedDBPath,
		currentProjectCWD: normalizedCWD,
	}, nil
}

func (r *Repository) Close() error {
	if r == nil {
		return nil
	}
	var closeErr error
	if r.db != nil {
		closeErr = r.db.Close()
	}
	if r.dbWrite != nil {
		if err := r.dbWrite.Close(); closeErr == nil {
			closeErr = err
		}
	}
	return closeErr
}

func (r *Repository) Health(ctx context.Context) (HealthResponse, error) {
	if err := r.db.PingContext(ctx); err != nil {
		return HealthResponse{}, err
	}
	return HealthResponse{
		Status:            "ok",
		DatabasePath:      r.databasePath,
		CurrentProjectCWD: r.currentProjectCWD,
	}, nil
}

func (r *Repository) HomeSnapshot(ctx context.Context) (HomeSnapshotResponse, error) {
	projectMap, projectOrder, err := r.loadProjects(ctx)
	if err != nil {
		return HomeSnapshotResponse{}, err
	}
	sessions, countsByProject, globalCounts, err := r.loadSessionSummaries(ctx, 0, nil)
	if err != nil {
		return HomeSnapshotResponse{}, err
	}
	sessionsByProject := map[int][]NetrunnerSummary{}
	for _, session := range sessions {
		sessionsByProject[session.ProjectID] = append(sessionsByProject[session.ProjectID], session)
	}
	autonomousByProject, autonomousSummary, err := r.loadAutonomousStatuses(ctx)
	if err != nil {
		return HomeSnapshotResponse{}, err
	}
	workerByProject, activeWorkers, err := r.loadActiveWorkers(ctx)
	if err != nil {
		return HomeSnapshotResponse{}, err
	}

	cards := make([]ProjectCard, 0, len(projectOrder))
	for _, projectID := range projectOrder {
		project := projectMap[projectID]
		sessions := sessionsByProject[projectID]
		latestLabel, latestID, latestLocalID := latestActivity(sessions)
		card := ProjectCard{
			Project: ProjectBinding{
				ID:   project.ID,
				Name: project.Name,
				CWD:  project.CWD,
			},
			Counts:               countsByProject[projectID],
			LatestActivityLabel:  latestLabel,
			LatestSessionID:      latestID,
			LatestLocalSessionID: latestLocalID,
			Autonomous:           autonomousByProject[projectID],
			HasPendingReview:     countsByProject[projectID].Review > 0,
			HasActiveWorkers:     workerByProject[projectID].RunningCount > 0,
		}
		cards = append(cards, card)
	}

	defaultChatBinding := placeholderFixerChatBinding(0, "Chat binding is loaded separately from the home snapshot.")
	if currentProject := r.currentProjectBinding(projectMap); currentProject != nil {
		defaultChatBinding.ProjectID = currentProject.ID
	}

	return HomeSnapshotResponse{
		CurrentProject:     r.currentProjectBinding(projectMap),
		DefaultChatBinding: defaultChatBinding,
		GlobalCounts:       globalCounts,
		Projects:           cards,
		ActiveWorkers:      activeWorkers,
		AutonomousSummary:  autonomousSummary,
	}, nil
}

func (r *Repository) ProjectSnapshot(ctx context.Context, projectID int) (ProjectSnapshotResponse, error) {
	return r.projectSnapshot(ctx, projectID, true)
}

func (r *Repository) ProjectOverview(ctx context.Context, projectID int) (ProjectSnapshotResponse, error) {
	return r.projectSnapshot(ctx, projectID, false)
}

func (r *Repository) projectSnapshot(ctx context.Context, projectID int, includeHeavySections bool) (ProjectSnapshotResponse, error) {
	project, err := r.requireProject(ctx, projectID)
	if err != nil {
		return ProjectSnapshotResponse{}, err
	}
	sessions, counts, _, err := r.loadSessionSummaries(ctx, projectID, nil)
	if err != nil {
		return ProjectSnapshotResponse{}, err
	}
	autonomousByProject, _, err := r.loadAutonomousStatuses(ctx)
	if err != nil {
		return ProjectSnapshotResponse{}, err
	}
	workerByProject, _, err := r.loadActiveWorkers(ctx)
	if err != nil {
		return ProjectSnapshotResponse{}, err
	}

	attachedDocCount := 0
	pendingProposalCount := 0
	for _, session := range sessions {
		attachedDocCount += session.AttachedDocCount
		pendingProposalCount += session.PendingProposalCount
	}

	docs := ProjectDocsResponse{
		Project: ProjectHeader{ID: project.ID, Name: project.Name, CWD: project.CWD},
		Docs: DocsSummary{
			TotalDocs:            0,
			Groups:               []DocGroup{},
			PendingProposalCount: pendingProposalCount,
		},
	}
	chatBinding := placeholderFixerChatBinding(projectID, "Chat binding is loaded separately from the project overview.")
	if includeHeavySections {
		docs, err = r.ProjectDocs(ctx, projectID)
		if err != nil {
			return ProjectSnapshotResponse{}, err
		}
		chatBinding, err = r.loadChatBinding(ctx, projectID, "fixer")
		if err != nil {
			return ProjectSnapshotResponse{}, err
		}
	}

	return ProjectSnapshotResponse{
		Project: ProjectHeader{
			ID:   project.ID,
			Name: project.Name,
			CWD:  project.CWD,
		},
		Metrics: OverviewMetrics{
			Counts:               counts[projectID],
			AttachedDocCount:     attachedDocCount,
			PendingProposalCount: pendingProposalCount,
			WorkerState:          workerByProject[projectID],
		},
		Autonomous: autonomousByProject[projectID],
		Docs:       docs.Docs,
		Netrunners: sessions,
		FixerChat:  chatBinding,
	}, nil
}
