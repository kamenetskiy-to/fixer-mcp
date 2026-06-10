package main

import (
	"context"
	"os/exec"
	"testing"
)

func TestResolveRuntimeLaunchEnvClearsProxyVariables(t *testing.T) {
	projectCWD := t.TempDir()

	resolvedEnv, err := resolveRuntimeLaunchEnv(projectCWD, []string{
		"PATH=/usr/bin",
		"ALL_PROXY=http://example.invalid:8080",
		"HTTP_PROXY=http://example.invalid:8080",
		"HTTPS_PROXY=http://example.invalid:8080",
	})
	if err != nil {
		t.Fatalf("resolveRuntimeLaunchEnv failed: %v", err)
	}

	envMap := envSliceToMap(resolvedEnv)
	for _, key := range []string{"ALL_PROXY", "all_proxy", "HTTP_PROXY", "http_proxy", "HTTPS_PROXY", "https_proxy", "NO_PROXY", "no_proxy"} {
		if _, ok := envMap[key]; ok {
			t.Fatalf("expected %s to be removed from launch env", key)
		}
	}
	if envMap["PATH"] != "/usr/bin" {
		t.Fatalf("expected PATH to remain, got %q", envMap["PATH"])
	}
}

func TestListAndStopActiveWorkerProcesses_FreezesOrchestration(t *testing.T) {
	originalDB := db
	originalRole := authorizedRole
	originalProjectID := authorizedProjectId
	defer func() {
		db = originalDB
		authorizedRole = originalRole
		authorizedProjectId = originalProjectID
	}()

	testDB := setupGetProjectsTestDB(t)
	defer func() {
		_ = testDB.Close()
	}()

	worker := exec.Command("sleep", "30")
	if err := worker.Start(); err != nil {
		t.Fatalf("start worker process: %v", err)
	}
	defer func() {
		_ = worker.Process.Kill()
		_, _ = worker.Process.Wait()
	}()

	if _, err := testDB.Exec("INSERT INTO worker_process (project_id, session_id, pid, launch_epoch, status) VALUES (1, 1, ?, 0, 'running')", worker.Process.Pid); err != nil {
		t.Fatalf("seed worker_process: %v", err)
	}

	db = testDB
	authorizedRole = "fixer"
	authorizedProjectId = 1

	_, listed, err := ListActiveWorkerProcesses(context.Background(), nil, ListActiveWorkerProcessesInput{})
	if err != nil {
		t.Fatalf("list_active_worker_processes failed: %v", err)
	}
	if len(listed.Processes) != 1 || listed.Processes[0].SessionID != 1 || !listed.Processes[0].Alive {
		t.Fatalf("unexpected listed processes: %+v", listed.Processes)
	}

	_, stopped, err := StopActiveWorkerProcesses(context.Background(), nil, StopActiveWorkerProcessesInput{
		SessionIds:          []int{1},
		FreezeOrchestration: true,
		Reason:              "operator stop",
	})
	if err != nil {
		t.Fatalf("stop_active_worker_processes failed: %v", err)
	}
	if stopped.StoppedProcessCount != 1 || !stopped.FreezeApplied || stopped.OrchestrationEpoch != 1 {
		t.Fatalf("unexpected stop output: %+v", stopped)
	}

	var epoch, frozen, notificationsEnabled, forcedStopCount int
	var workerStatus string
	if err := db.QueryRow("SELECT orchestration_epoch, orchestration_frozen, notifications_enabled_for_active_run FROM autonomous_run_status WHERE project_id = 1").Scan(&epoch, &frozen, &notificationsEnabled); err != nil {
		t.Fatalf("query autonomous status: %v", err)
	}
	if epoch != 1 || frozen != 1 || notificationsEnabled != 0 {
		t.Fatalf("unexpected autonomous freeze state: epoch=%d frozen=%d notifications=%d", epoch, frozen, notificationsEnabled)
	}
	if err := db.QueryRow("SELECT forced_stop_count FROM session WHERE id = 1").Scan(&forcedStopCount); err != nil {
		t.Fatalf("query forced_stop_count: %v", err)
	}
	if forcedStopCount != 1 {
		t.Fatalf("expected forced_stop_count 1, got %d", forcedStopCount)
	}
	if err := db.QueryRow("SELECT status FROM worker_process WHERE session_id = 1").Scan(&workerStatus); err != nil {
		t.Fatalf("query worker_process status: %v", err)
	}
	if workerStatus == workerStatusRunning {
		t.Fatalf("expected worker process to be stopped or exited, got %q", workerStatus)
	}
}
