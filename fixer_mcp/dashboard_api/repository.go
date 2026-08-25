package dashboardapi

import (
	"context"
	"database/sql"
	"fmt"
	"os"
	"os/exec"
	"path/filepath"
	"strings"
	"time"

	_ "github.com/glebarez/go-sqlite"
)

const defaultFixerDBFilename = "fixer.db"

const (
	fixerSkillMarker     = "Activate skill $init-fixer immediately."
	overseerSkillMarker  = "Activate skill $init-overseer immediately."
	netrunnerSkillMarker = "Activate skill $hands-netrunner immediately."
	handsChannelMarker   = "Use its Project Hands Channel Mode for the current project."
	maxRoleMarkerLines   = 240
	maxCodexChatSessions = 12
	maxCodexSessionScan  = 160
)

type Repository struct {
	db                              *sql.DB
	dbWrite                         *sql.DB
	databasePath                    string
	currentProjectCWD               string
	fixerChatLauncher               func(string, FixerChatLaunchInput) (int, error)
	now                             func() time.Time
	plannedWaveInitializer          func(context.Context, projectRecord, int) (int, error)
	plannedWaveInitializerAvailable func(projectRecord) bool
	workroomEvents                  *projectEventNotifier
}

func OpenRepository(databasePath string, currentProjectCWD string) (*Repository, error) {
	resolvedDBPath, err := resolveDatabasePath(databasePath)
	if err != nil {
		return nil, err
	}
	resolvedDBPath, err = filepath.Abs(resolvedDBPath)
	if err != nil {
		return nil, fmt.Errorf("resolve fixer database path: %w", err)
	}
	readDSN := resolvedDBPath
	if !strings.Contains(readDSN, "?") {
		readDSN += "?_pragma=busy_timeout(5000)&_pragma=journal_mode(WAL)&_pragma=query_only(1)"
	}
	db, err := sql.Open("sqlite", readDSN)
	if err != nil {
		return nil, err
	}
	db.SetMaxOpenConns(4)
	db.SetMaxIdleConns(4)

	writeDSN := resolvedDBPath
	if !strings.Contains(writeDSN, "?") {
		writeDSN += "?_pragma=busy_timeout(5000)&_pragma=journal_mode(WAL)&_pragma=foreign_keys(1)&_txlock=immediate"
	}
	dbWrite, err := sql.Open("sqlite", writeDSN)
	if err != nil {
		_ = db.Close()
		return nil, err
	}
	dbWrite.SetMaxOpenConns(1)
	dbWrite.SetMaxIdleConns(1)
	normalizedCWD := ""
	if strings.TrimSpace(currentProjectCWD) != "" {
		normalizedCWD, err = normalizeProjectCWD(currentProjectCWD)
		if err != nil {
			_ = db.Close()
			_ = dbWrite.Close()
			return nil, err
		}
	}
	repo := &Repository{
		db:                db,
		dbWrite:           dbWrite,
		databasePath:      resolvedDBPath,
		currentProjectCWD: normalizedCWD,
		fixerChatLauncher: launchFixerChatProcess,
		now:               time.Now,
		workroomEvents:    newProjectEventNotifier(),
	}
	repo.plannedWaveInitializer = repo.initializePlannedWaveThroughFixerMCP
	repo.plannedWaveInitializerAvailable = repo.canInitializePlannedWaveThroughFixerMCP
	return repo, nil
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
	activityByProject, err := r.loadProjectActivity(ctx)
	if err != nil {
		return HomeSnapshotResponse{}, err
	}

	cards := make([]ProjectCard, 0, len(projectOrder))
	for _, projectID := range projectOrder {
		project := projectMap[projectID]
		sessions := sessionsByProject[projectID]
		latestLabel, latestID, latestLocalID := latestActivity(sessions)
		activity := activityByProject[projectID]
		card := ProjectCard{
			Project: ProjectBinding{
				ID:   project.ID,
				Name: project.Name,
				CWD:  project.CWD,
			},
			Counts:                countsByProject[projectID],
			LatestActivityLabel:   latestLabel,
			LastActivityAt:        activity.LastActivityAt,
			PrimaryActivitySource: activity.PrimarySource,
			HasFixerActivity:      activity.HasFixer,
			HasHandsActivity:      activity.HasHands,
			HasAutonomousActivity: activity.HasAutonomous,
			ActiveWaveCount:       activity.ActiveWaveCount,
			LatestSessionID:       latestID,
			LatestLocalSessionID:  latestLocalID,
			Autonomous:            autonomousByProject[projectID],
			HasPendingReview:      countsByProject[projectID].Review > 0,
			HasActiveWorkers:      workerByProject[projectID].RunningCount > 0,
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
	waveGroups, _, err := r.groupNetrunnerSessions(ctx, projectID, sessions)
	if err != nil {
		return ProjectSnapshotResponse{}, err
	}
	activeWaveCount := 0
	for _, wave := range waveGroups {
		if isActiveDashboardWave(wave.Status) {
			activeWaveCount++
		}
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
			ActiveWaveCount:      activeWaveCount,
			TotalWaveCount:       len(waveGroups),
			AttachedDocCount:     attachedDocCount,
			PendingProposalCount: pendingProposalCount,
			WorkerState:          workerByProject[projectID],
		},
		Autonomous: autonomousByProject[projectID],
		Docs:       docs.Docs,
		Waves:      waveGroups,
		Netrunners: sessions,
		FixerChat:  chatBinding,
	}, nil
}

func isActiveDashboardWave(status string) bool {
	switch strings.ToLower(strings.TrimSpace(status)) {
	case "completed", "failed", "stopped", "cleaned":
		return false
	default:
		return true
	}
}

func (r *Repository) LaunchFixerChat(ctx context.Context, projectID int, input FixerChatLaunchInput) (FixerChatLaunchResponse, error) {
	project, err := r.requireProject(ctx, projectID)
	if err != nil {
		return FixerChatLaunchResponse{}, err
	}
	projectCWD, err := normalizeProjectCWD(project.CWD)
	if err != nil {
		return FixerChatLaunchResponse{}, err
	}
	requestedCWD := strings.TrimSpace(input.CWD)
	if requestedCWD != "" {
		requestedCWD, err = normalizeProjectCWD(requestedCWD)
		if err != nil {
			return FixerChatLaunchResponse{}, err
		}
		if requestedCWD != projectCWD {
			return FixerChatLaunchResponse{}, fmt.Errorf("fixer chat cwd must match the selected project")
		}
	}
	input.CWD = projectCWD
	input.Backend = strings.ToLower(strings.TrimSpace(input.Backend))
	if input.Backend == "agy" {
		input.Backend = "antigravity"
	}
	switch input.Backend {
	case "codex", "commandcode", "antigravity", "claude", "kimi-code", "droid", "junie":
	default:
		return FixerChatLaunchResponse{}, fmt.Errorf("unsupported Fixer backend %q", input.Backend)
	}
	input.Model = strings.TrimSpace(input.Model)
	input.Reasoning = strings.TrimSpace(input.Reasoning)
	if input.Model == "" || input.Reasoning == "" {
		return FixerChatLaunchResponse{}, fmt.Errorf("fixer chat model and reasoning are required")
	}
	launcher := r.fixerChatLauncher
	if launcher == nil {
		launcher = launchFixerChatProcess
	}
	pid, err := launcher(projectCWD, input)
	if err != nil {
		return FixerChatLaunchResponse{}, err
	}
	return FixerChatLaunchResponse{
		Status:    "started",
		ProjectID: projectID,
		Backend:   input.Backend,
		ProcessID: pid,
	}, nil
}

const fixerChatPythonEntrypoint = `
import sys
from pathlib import Path
from client_wires import fixer_wire, fixer_wire_role_launch

raise SystemExit(fixer_wire_role_launch.launch_new_fixer_chat(
    launch_cwd=Path(sys.argv[1]),
    backend=sys.argv[2],
    model=sys.argv[3],
    reasoning=sys.argv[4],
    dry_run=False,
    Option=None,
    single_select_items=lambda *_args, **_kwargs: None,
    callbacks=fixer_wire._role_launch_callbacks(),
))
`

func launchFixerChatProcess(projectCWD string, input FixerChatLaunchInput) (int, error) {
	repoRoot, err := findFixerWireRepoRoot()
	if err != nil {
		return 0, err
	}
	python := strings.TrimSpace(os.Getenv("PYTHON"))
	if python == "" {
		python = "python3"
	}
	cmd := exec.Command(
		python,
		"-c",
		fixerChatPythonEntrypoint,
		projectCWD,
		input.Backend,
		input.Model,
		input.Reasoning,
	)
	cmd.Dir = repoRoot
	pythonPath := repoRoot
	if existing := strings.TrimSpace(os.Getenv("PYTHONPATH")); existing != "" {
		pythonPath += string(os.PathListSeparator) + existing
	}
	cmd.Env = append(os.Environ(), "PYTHONPATH="+pythonPath)
	if err := cmd.Start(); err != nil {
		return 0, fmt.Errorf("start Fixer chat launcher: %w", err)
	}
	pid := cmd.Process.Pid
	go func() { _ = cmd.Wait() }()
	return pid, nil
}

func findFixerWireRepoRoot() (string, error) {
	candidates := []string{strings.TrimSpace(os.Getenv("FIXER_MCP_REPO_ROOT"))}
	if cwd, err := os.Getwd(); err == nil {
		candidates = append(candidates, cwd, filepath.Dir(cwd), filepath.Dir(filepath.Dir(cwd)))
	}
	for _, candidate := range candidates {
		if candidate == "" {
			continue
		}
		candidate, err := filepath.Abs(candidate)
		if err != nil {
			continue
		}
		if info, err := os.Stat(filepath.Join(candidate, "client_wires", "fixer_wire.py")); err == nil && !info.IsDir() {
			return candidate, nil
		}
	}
	return "", fmt.Errorf("could not locate client_wires/fixer_wire.py; set FIXER_MCP_REPO_ROOT")
}
