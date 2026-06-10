package main

import (
	"os"
	"strings"
	"testing"
)

func TestDocsProposalLogHandlersLiveOutsideMain(t *testing.T) {
	mainSource, err := os.ReadFile("main.go")
	if err != nil {
		t.Fatalf("read main.go: %v", err)
	}
	docsSource, err := os.ReadFile("project_docs_handlers.go")
	if err != nil {
		t.Fatalf("read project_docs_handlers.go: %v", err)
	}

	symbols := []string{
		"func CheckCurrentProjectDocs(",
		"func SetSessionAttachedDocs(",
		"func GetSessionAttachedDocs(",
		"func GetAttachedProjectDocs(",
		"func LogNetrunnerProgress(",
		"func ViewNetrunnerLogs(",
		"func ProposeDocUpdate(",
		"func ReviewDocProposals(",
		"func SetDocProposalStatus(",
		"func GetProjectDocs(",
		"func AddProjectDoc(",
		"func UpdateProjectDoc(",
		"func DeleteProjectDoc(",
		"var validNetrunnerLogTypes",
		"var docSlugInvalidChars",
	}

	for _, symbol := range symbols {
		if strings.Contains(string(mainSource), symbol) {
			t.Fatalf("expected %q to be extracted out of main.go", symbol)
		}
		if !strings.Contains(string(docsSource), symbol) {
			t.Fatalf("expected %q in project_docs_handlers.go", symbol)
		}
	}
}

func TestRuntimeHelperClustersLiveOutsideMain(t *testing.T) {
	mainSource, err := os.ReadFile("main.go")
	if err != nil {
		t.Fatalf("read main.go: %v", err)
	}

	cases := []struct {
		file    string
		symbols []string
	}{
		{
			file: "runtime_env_helpers.go",
			symbols: []string{
				"func envSliceToMap(",
				"func clearProxyEnvSlice(",
				"func resolveRuntimeLaunchEnv(",
				"func loadOptionalDotEnv(",
			},
		},
		{
			file: "telegram_helpers.go",
			symbols: []string{
				"func resolveTelegramOperatorConfigFromEnv(",
				"func sendTelegramText(",
				"func renderTelegramOperatorNotification(",
			},
		},
		{
			file: "write_scope_helpers.go",
			symbols: []string{
				"func normalizeWriteScopePath(",
				"func normalizeDeclaredWriteScope(",
				"func writeScopesOverlap(",
			},
		},
		{
			file: "orchestration_control_helpers.go",
			symbols: []string{
				"type orchestrationControl",
				"func fetchOrchestrationControl(",
				"func upsertOrchestrationControl(",
				"func normalizeAutonomousStatusLabel(",
			},
		},
		{
			file: "worker_process_helpers.go",
			symbols: []string{
				"type workerProcessSnapshot",
				"func isProcessAlive(",
				"func latestMatchingFile(",
				"func recordWorkerProcessLaunch(",
			},
		},
	}

	for _, tc := range cases {
		source, err := os.ReadFile(tc.file)
		if err != nil {
			t.Fatalf("read %s: %v", tc.file, err)
		}
		for _, symbol := range tc.symbols {
			if strings.Contains(string(mainSource), symbol) {
				t.Fatalf("expected runtime helper %q to be extracted out of main.go", symbol)
			}
			if !strings.Contains(string(source), symbol) {
				t.Fatalf("expected runtime helper %q in %s", symbol, tc.file)
			}
		}
	}
}

func TestMcpRegistryAssignmentHandlersLiveOutsideMain(t *testing.T) {
	mainSource, err := os.ReadFile("main.go")
	if err != nil {
		t.Fatalf("read main.go: %v", err)
	}
	registrySource, err := os.ReadFile("mcp_registry_handlers.go")
	if err != nil {
		t.Fatalf("read mcp_registry_handlers.go: %v", err)
	}

	symbols := []string{
		"type curatedMcpServerSpec",
		"var curatedDefaultMcpServers",
		"type ListMcpServersInput",
		"type McpServerRecord",
		"type ListMcpServersOutput",
		"func ListMcpServers(",
		"type McpServerUpsertInput",
		"type SyncMcpServersInput",
		"type SyncMcpServersOutput",
		"func SyncMcpServers(",
		"type SetProjectMcpServersInput",
		"type SetProjectMcpServersOutput",
		"func SetProjectMcpServers(",
		"type GetProjectMcpServersInput",
		"type GetProjectMcpServersOutput",
		"func GetProjectMcpServers(",
		"type SetSessionMcpServersInput",
		"type SetSessionMcpServersOutput",
		"func SetSessionMcpServers(",
		"type GetSessionMcpServersInput",
		"type GetSessionMcpServersOutput",
		"func GetSessionMcpServers(",
	}

	for _, symbol := range symbols {
		if strings.Contains(string(mainSource), symbol) {
			t.Fatalf("expected %q to be extracted out of main.go", symbol)
		}
		if !strings.Contains(string(registrySource), symbol) {
			t.Fatalf("expected %q in mcp_registry_handlers.go", symbol)
		}
	}
}

func TestProjectVisibilityBridgeHandlersLiveOutsideMain(t *testing.T) {
	mainSource, err := os.ReadFile("main.go")
	if err != nil {
		t.Fatalf("read main.go: %v", err)
	}
	visibilitySource, err := os.ReadFile("project_visibility_handlers.go")
	if err != nil {
		t.Fatalf("read project_visibility_handlers.go: %v", err)
	}
	overseerLaunchSource, err := os.ReadFile("overseer_launch_handlers.go")
	if err != nil {
		t.Fatalf("read overseer_launch_handlers.go: %v", err)
	}

	symbols := []string{
		"type GetProjectsInput",
		"func GetProjects(",
		"type RegisterProjectInput",
		"func RegisterProject(",
		"type AutonomousRunStatusRecord",
		"func resolveAutonomousProjectID(",
		"func resolveAutonomousSessionID(",
		"func fetchAutonomousRunStatusRecord(",
		"func SetAutonomousRunStatus(",
		"func GetAutonomousRunStatus(",
		"type SendOperatorTelegramNotificationInput",
		"func SendOperatorTelegramNotification(",
		"type ProjectHandoffRecord",
		"func resolveProjectHandoffProjectID(",
		"func fetchProjectHandoffRecord(",
		"func SetProjectHandoff(",
		"func GetProjectHandoff(",
		"func ClearProjectHandoff(",
		"type ProjectActivityRecord",
		"func SetProjectActivity(",
		"func SetProjectOverview(",
		"func GetProjectOverview(",
		"func GetActiveProjectOverviews(",
		"type OverseerFixerMessageRecord",
		"func normalizeOverseerFixerSenderRole(",
		"func normalizeOverseerFixerLimit(",
		"func fetchOverseerFixerMessageByID(",
		"func fetchOverseerFixerMessages(",
		"func fetchOverseerFixerRunStateRecord(",
		"func latestOverseerFixerMessageID(",
		"func AppendOverseerFixerMessage(",
		"func GetOverseerFixerMessages(",
		"func SetOverseerFixerRunState(",
		"func GetOverseerFixerRunState(",
		"func activeOverseerFixerRunProjectIDs(",
		"func fetchNewFixerMessages(",
		"func normalizeWaitTimeout(",
		"func normalizeWaitPollInterval(",
		"func cursorFromMessages(",
		"func WaitForOverseerFixerMessages(",
	}

	for _, symbol := range symbols {
		if strings.Contains(string(mainSource), symbol) {
			t.Fatalf("expected %q to be extracted out of main.go", symbol)
		}
		if !strings.Contains(string(visibilitySource), symbol) {
			t.Fatalf("expected %q in project_visibility_handlers.go", symbol)
		}
	}

	launchSymbols := []string{
		"type LaunchAndWaitFixersInput",
		"func LaunchAndWaitFixers(",
		"func targetProjectsForLaunchAndWait(",
		"func launchOverseerFixerForProject(",
	}
	for _, symbol := range launchSymbols {
		if strings.Contains(string(mainSource), symbol) {
			t.Fatalf("expected launch-specific %q to be extracted out of main.go", symbol)
		}
		if strings.Contains(string(visibilitySource), symbol) {
			t.Fatalf("expected launch-specific %q to stay out of project_visibility_handlers.go", symbol)
		}
		if !strings.Contains(string(overseerLaunchSource), symbol) {
			t.Fatalf("expected launch-specific %q in overseer_launch_handlers.go", symbol)
		}
	}
}

func TestOverseerLaunchHandlersLiveOutsideMain(t *testing.T) {
	mainSource, err := os.ReadFile("main.go")
	if err != nil {
		t.Fatalf("read main.go: %v", err)
	}
	overseerLaunchSource, err := os.ReadFile("overseer_launch_handlers.go")
	if err != nil {
		t.Fatalf("read overseer_launch_handlers.go: %v", err)
	}
	visibilitySource, err := os.ReadFile("project_visibility_handlers.go")
	if err != nil {
		t.Fatalf("read project_visibility_handlers.go: %v", err)
	}
	launchWaitSource, err := os.ReadFile("launch_wait_handlers.go")
	if err != nil {
		t.Fatalf("read launch_wait_handlers.go: %v", err)
	}
	waveSource, err := os.ReadFile("parallel_wave_handlers.go")
	if err != nil {
		t.Fatalf("read parallel_wave_handlers.go: %v", err)
	}
	imageJobSource, err := os.ReadFile("image_job_handlers.go")
	if err != nil {
		t.Fatalf("read image_job_handlers.go: %v", err)
	}

	symbols := []string{
		"type LaunchAndWaitFixersInput",
		"type LaunchAndWaitFixerProjectResult",
		"type LaunchAndWaitFixersOutput",
		"type launchAndWaitFixerTarget",
		"func targetProjectsForLaunchAndWait(",
		"func upsertOverseerFixerRunStateForLaunch(",
		"func appendOverseerFixerMessageForProject(",
		"func fetchNewFixerMessagesAfterProjectCursors(",
		"func maxCursorFromProjectCursors(",
		"func truncateLauncherOutput(",
		"func launcherFailureDiagnostic(",
		"func launchOverseerFixerForProject(",
		"func LaunchAndWaitFixers(",
	}
	for _, symbol := range symbols {
		if strings.Contains(string(mainSource), symbol) {
			t.Fatalf("expected overseer launch symbol %q to be extracted out of main.go", symbol)
		}
		if strings.Contains(string(visibilitySource), symbol) {
			t.Fatalf("expected overseer launch symbol %q to stay out of project_visibility_handlers.go", symbol)
		}
		if strings.Contains(string(launchWaitSource), symbol) {
			t.Fatalf("expected overseer launch symbol %q to stay out of launch_wait_handlers.go", symbol)
		}
		if strings.Contains(string(waveSource), symbol) {
			t.Fatalf("expected overseer launch symbol %q to stay out of parallel_wave_handlers.go", symbol)
		}
		if strings.Contains(string(imageJobSource), symbol) {
			t.Fatalf("expected overseer launch symbol %q to stay out of image_job_handlers.go", symbol)
		}
		if !strings.Contains(string(overseerLaunchSource), symbol) {
			t.Fatalf("expected overseer launch symbol %q in overseer_launch_handlers.go", symbol)
		}
	}

	sharedRuntimeSymbols := []string{
		"var execCommand",
		"func main(",
		"func WakeFixerAutonomous(",
	}
	for _, symbol := range sharedRuntimeSymbols {
		if !strings.Contains(string(mainSource), symbol) {
			t.Fatalf("expected shared/runtime symbol %q to remain in main.go", symbol)
		}
		if strings.Contains(string(overseerLaunchSource), symbol) {
			t.Fatalf("expected shared/runtime symbol %q to stay out of overseer_launch_handlers.go", symbol)
		}
	}

	runtimeEnvSource, err := os.ReadFile("runtime_env_helpers.go")
	if err != nil {
		t.Fatalf("read runtime_env_helpers.go: %v", err)
	}
	if strings.Contains(string(mainSource), "func resolveRuntimeLaunchEnv(") {
		t.Fatalf("expected runtime env helper to be extracted out of main.go")
	}
	if !strings.Contains(string(runtimeEnvSource), "func resolveRuntimeLaunchEnv(") {
		t.Fatalf("expected runtime env helper in runtime_env_helpers.go")
	}
}

func TestSessionLifecycleHandlersLiveOutsideMain(t *testing.T) {
	mainSource, err := os.ReadFile("main.go")
	if err != nil {
		t.Fatalf("read main.go: %v", err)
	}
	lifecycleSource, err := os.ReadFile("session_lifecycle_handlers.go")
	if err != nil {
		t.Fatalf("read session_lifecycle_handlers.go: %v", err)
	}
	launchWaitSource, err := os.ReadFile("launch_wait_handlers.go")
	if err != nil {
		t.Fatalf("read launch_wait_handlers.go: %v", err)
	}

	symbols := []string{
		"type GetPendingTasksInput",
		"type PendingTask",
		"func GetPendingTasks(",
		"type CheckoutTaskInput",
		"func CheckoutTask(",
		"type CreateTaskInput",
		"func CreateTask(",
		"type CompleteTaskInput",
		"type SessionCleanupClaims",
		"type SessionFinalReport",
		"const completeTaskReportTemplate",
		"func normalizeStringList(",
		"func decodeStructuredFinalReport(",
		"func CompleteTask(",
		"type UpdateTaskInput",
		"func UpdateTask(",
		"type GetAllSessionsInput",
		"type SessionRecord",
		"func GetAllSessions(",
		"type SetSessionStatusInput",
		"func SetSessionStatus(",
		"type ForkRepairSessionFromInput",
		"func ForkRepairSessionFrom(",
		"type CleanupClaimCheck",
		"func VerifySessionCleanupClaims(",
		"type GetSessionInput",
		"type SessionDetails",
		"func GetSession(",
	}
	for _, symbol := range symbols {
		if strings.Contains(string(mainSource), symbol) {
			t.Fatalf("expected %q to be extracted out of main.go", symbol)
		}
		if !strings.Contains(string(lifecycleSource), symbol) {
			t.Fatalf("expected %q in session_lifecycle_handlers.go", symbol)
		}
	}

	launchSymbols := []string{
		"type ExplicitNetrunnerLaunchMetadata",
		"func LaunchExplicitNetrunner(",
		"func WaitForNetrunnerSession(",
		"func WaitForNetrunnerSessions(",
	}
	for _, symbol := range launchSymbols {
		if !strings.Contains(string(launchWaitSource), symbol) {
			t.Fatalf("expected explicit-launch symbol %q in launch_wait_handlers.go", symbol)
		}
		if strings.Contains(string(lifecycleSource), symbol) {
			t.Fatalf("expected explicit-launch symbol %q to stay out of session_lifecycle_handlers.go", symbol)
		}
	}
}

func TestTranscriptHandlersLiveOutsideMain(t *testing.T) {
	mainSource, err := os.ReadFile("main.go")
	if err != nil {
		t.Fatalf("read main.go: %v", err)
	}
	transcriptSource, err := os.ReadFile("transcript_handlers.go")
	if err != nil {
		t.Fatalf("read transcript_handlers.go: %v", err)
	}
	launchWaitSource, err := os.ReadFile("launch_wait_handlers.go")
	if err != nil {
		t.Fatalf("read launch_wait_handlers.go: %v", err)
	}
	overseerLaunchSource, err := os.ReadFile("overseer_launch_handlers.go")
	if err != nil {
		t.Fatalf("read overseer_launch_handlers.go: %v", err)
	}

	transcriptSymbols := []string{
		"type GetNetrunnerTranscriptPathInput",
		"type GetNetrunnerTranscriptPathOutput",
		"func droidProjectTranscriptDirName(",
		"func transcriptFileMetadata(",
		"func findCodexTranscriptPath(",
		"func payloadString(",
		"func nestedPayloadMap(",
		"func transcriptPayloadRecordType(",
		"func transcriptPayloadCWD(",
		"func transcriptPayloadSessionID(",
		"func sameTranscriptCWD(",
		"func candidateTranscriptFiles(",
		"func transcriptMatchByProjectCWD(",
		"func findCodexTranscriptPathByProjectCWD(",
		"func findDroidTranscriptPath(",
		"func findDroidTranscriptPathByProjectCWD(",
		"func persistDiscoveredSessionExternalID(",
		"func resolveTranscriptLookupProjectAndSession(",
		"func GetNetrunnerTranscriptPath(",
	}
	for _, symbol := range transcriptSymbols {
		if strings.Contains(string(mainSource), symbol) {
			t.Fatalf("expected transcript symbol %q to be extracted out of main.go", symbol)
		}
		if !strings.Contains(string(transcriptSource), symbol) {
			t.Fatalf("expected transcript symbol %q in transcript_handlers.go", symbol)
		}
	}

	launchSymbols := []string{
		"type LaunchAndWaitFixersInput",
		"func LaunchAndWaitFixers(",
		"type ExplicitNetrunnerLaunchMetadata",
		"func LaunchExplicitNetrunner(",
		"func WaitForNetrunnerSession(",
		"func WaitForNetrunnerSessions(",
		"type LaunchAndWaitNetrunnerInput",
		"func LaunchAndWaitNetrunner(",
	}
	for _, symbol := range launchSymbols {
		if strings.Contains(string(transcriptSource), symbol) {
			t.Fatalf("expected launch/wait symbol %q to stay out of transcript_handlers.go", symbol)
		}
		if strings.Contains(symbol, "Netrunner") {
			if !strings.Contains(string(launchWaitSource), symbol) {
				t.Fatalf("expected explicit launch/wait symbol %q in launch_wait_handlers.go", symbol)
			}
			continue
		}
		if strings.Contains(string(mainSource), symbol) {
			t.Fatalf("expected overseer launch symbol %q to be extracted out of main.go", symbol)
		}
		if !strings.Contains(string(overseerLaunchSource), symbol) {
			t.Fatalf("expected overseer launch symbol %q in overseer_launch_handlers.go", symbol)
		}
	}
}

func TestLaunchWaitHandlersLiveOutsideMain(t *testing.T) {
	mainSource, err := os.ReadFile("main.go")
	if err != nil {
		t.Fatalf("read main.go: %v", err)
	}
	launchWaitSource, err := os.ReadFile("launch_wait_handlers.go")
	if err != nil {
		t.Fatalf("read launch_wait_handlers.go: %v", err)
	}
	overseerLaunchSource, err := os.ReadFile("overseer_launch_handlers.go")
	if err != nil {
		t.Fatalf("read overseer_launch_handlers.go: %v", err)
	}
	sharedCliSource, err := os.ReadFile("cli_backend_helpers.go")
	if err != nil {
		t.Fatalf("read cli_backend_helpers.go: %v", err)
	}

	sharedCliSymbols := []string{
		"func normalizeCliBackend(",
		"func normalizeCliModel(",
		"func defaultCliModelForBackend(",
		"func normalizeCliReasoning(",
		"func validateCliModelForBackend(",
		"func fetchSessionExternalID(",
	}
	for _, symbol := range sharedCliSymbols {
		if strings.Contains(string(mainSource), symbol) {
			t.Fatalf("expected shared CLI/session-link helper %q to stay out of main.go", symbol)
		}
		if strings.Contains(string(launchWaitSource), symbol) {
			t.Fatalf("expected shared CLI/session-link helper %q to stay out of launch_wait_handlers.go", symbol)
		}
		if !strings.Contains(string(sharedCliSource), symbol) {
			t.Fatalf("expected shared CLI/session-link helper %q in cli_backend_helpers.go", symbol)
		}
	}

	launchWaitSymbols := []string{
		"const (\n\texplicitLauncherExitGracePeriod",
		"type explicitLaunchWorkerMetadata",
		"func explicitWaitTimeoutSeconds(",
		"func explicitWaitPollIntervalSeconds(",
		"func resolveExplicitLauncherScript(",
		"type sessionLaunchConfig",
		"func readSessionLaunchConfig(",
		"func resolveSessionLaunchConfig(",
		"func waitForSessionExternalID(",
		"func explicitLaunchArtifacts(",
		"func readExplicitLaunchWorkerMetadata(",
		"type ListActiveWorkerProcessesInput",
		"type ListActiveWorkerProcessesOutput",
		"func ListActiveWorkerProcesses(",
		"type StopActiveWorkerProcessesInput",
		"type StopActiveWorkerProcessesOutput",
		"func StopActiveWorkerProcesses(",
		"type ExplicitNetrunnerLaunchMetadata",
		"type LaunchExplicitNetrunnerInput",
		"type LaunchExplicitNetrunnerOutput",
		"type WaitForNetrunnerSessionInput",
		"type ExplicitNetrunnerWaitResult",
		"type WaitForNetrunnerSessionOutput",
		"type WaitForNetrunnerSessionsInput",
		"type ExplicitNetrunnerWaitAnyResult",
		"type WaitForNetrunnerSessionsOutput",
		"type LaunchAndWaitNetrunnerInput",
		"type LaunchAndWaitNetrunnerOutput",
		"type activeLaunchSession",
		"func loadActiveLaunchSessions(",
		"func waitFollowUpDecision(",
		"func launchExplicitNetrunnerWithMetadata(",
		"func fetchSessionWaitSnapshot(",
		"type explicitWaitCandidate",
		"type explicitWaitSnapshot",
		"func resolveExplicitWaitCandidatesFromList(",
		"func discoverProjectWaitCandidates(",
		"func fetchExplicitWaitSnapshot(",
		"func classifyWaitTerminalCondition(",
		"func malformedReviewSnapshotReason(",
		"func waitForNetrunnerSessionsResult(",
		"func waitForNetrunnerSessionResult(",
		"func LaunchExplicitNetrunner(",
		"func WaitForNetrunnerSession(",
		"func WaitForNetrunnerSessions(",
		"func LaunchAndWaitNetrunner(",
	}
	for _, symbol := range launchWaitSymbols {
		if strings.Contains(string(mainSource), symbol) {
			t.Fatalf("expected launch/wait symbol %q to be extracted out of main.go", symbol)
		}
		if !strings.Contains(string(launchWaitSource), symbol) {
			t.Fatalf("expected launch/wait symbol %q in launch_wait_handlers.go", symbol)
		}
	}

	overseerLaunchSymbols := []string{
		"type LaunchAndWaitFixersInput",
		"func LaunchAndWaitFixers(",
	}
	for _, symbol := range overseerLaunchSymbols {
		if strings.Contains(string(mainSource), symbol) {
			t.Fatalf("expected overseer launch symbol %q to be extracted out of main.go", symbol)
		}
		if strings.Contains(string(launchWaitSource), symbol) {
			t.Fatalf("expected overseer launch symbol %q to stay out of launch_wait_handlers.go", symbol)
		}
		if !strings.Contains(string(overseerLaunchSource), symbol) {
			t.Fatalf("expected overseer launch symbol %q in overseer_launch_handlers.go", symbol)
		}
	}
}

func TestImageGenerationJobHandlersLiveOutsideMain(t *testing.T) {
	mainSource, err := os.ReadFile("main.go")
	if err != nil {
		t.Fatalf("read main.go: %v", err)
	}
	imageJobSource, err := os.ReadFile("image_job_handlers.go")
	if err != nil {
		t.Fatalf("read image_job_handlers.go: %v", err)
	}
	launchWaitSource, err := os.ReadFile("launch_wait_handlers.go")
	if err != nil {
		t.Fatalf("read launch_wait_handlers.go: %v", err)
	}
	waveSource, err := os.ReadFile("parallel_wave_handlers.go")
	if err != nil {
		t.Fatalf("read parallel_wave_handlers.go: %v", err)
	}

	imageJobSymbols := []string{
		"const (\n\tdefaultImageJobTimeoutSeconds",
		"const (\n\timageJobStatusLaunching",
		"var generatedImagePathRegex",
		"var codexThreadIDRegex",
		"type imageGenerationJobRow",
		"func imageGenerationArtifacts(",
		"func imageGenerationRootDir(",
		"func snapshotGeneratedImagePaths(",
		"func isSupportedImageFile(",
		"func resolveLaunchImageInputPaths(",
		"func encodeGeneratedImageBaseline(",
		"func decodeGeneratedImageBaseline(",
		"func insertImageGenerationJob(",
		"func fetchImageGenerationJob(",
		"func updateImageGenerationJobLaunch(",
		"func finalizeImageGenerationJob(",
		"func updateImageGenerationJobWorkspaceCopy(",
		"func extractThreadIDFromJSONL(",
		"func extractPathCandidatesFromText(",
		"func extractPathCandidatesFromFile(",
		"func resolveGeneratedImagePathFromThreadID(",
		"func resolveGeneratedImagePathFromSnapshot(",
		"func resolveImageGenerationJobResult(",
		"func refreshImageGenerationJob(",
		"func imageJobTimeoutSeconds(",
		"func imageJobPollIntervalSeconds(",
		"func copyFile(",
		"func resolveWorkspaceDestination(",
		"type LaunchImageGenerationJobInput",
		"type LaunchImageGenerationJobOutput",
		"type WaitForImageGenerationJobInput",
		"type ImageGenerationJobWaitResult",
		"type WaitForImageGenerationJobOutput",
		"type CopyImageGenerationJobOutputInput",
		"type CopyImageGenerationJobOutputOutput",
		"func LaunchImageGenerationJob(",
		"func WaitForImageGenerationJob(",
		"func CopyImageGenerationJobOutput(",
	}
	for _, symbol := range imageJobSymbols {
		if strings.Contains(string(mainSource), symbol) {
			t.Fatalf("expected image-job symbol %q to be extracted out of main.go", symbol)
		}
		if strings.Contains(string(launchWaitSource), symbol) {
			t.Fatalf("expected image-job symbol %q to stay out of launch_wait_handlers.go", symbol)
		}
		if strings.Contains(string(waveSource), symbol) {
			t.Fatalf("expected image-job symbol %q to stay out of parallel_wave_handlers.go", symbol)
		}
		if !strings.Contains(string(imageJobSource), symbol) {
			t.Fatalf("expected image-job symbol %q in image_job_handlers.go", symbol)
		}
	}

	workerHelperSource, err := os.ReadFile("worker_process_helpers.go")
	if err != nil {
		t.Fatalf("read worker_process_helpers.go: %v", err)
	}
	sharedRuntimeSymbols := []string{
		"func isProcessAlive(",
		"func latestMatchingFile(",
	}
	for _, symbol := range sharedRuntimeSymbols {
		if strings.Contains(string(mainSource), symbol) {
			t.Fatalf("expected shared runtime symbol %q to be extracted out of main.go", symbol)
		}
		if strings.Contains(string(imageJobSource), symbol) {
			t.Fatalf("expected shared runtime symbol %q to stay out of image_job_handlers.go", symbol)
		}
		if !strings.Contains(string(workerHelperSource), symbol) {
			t.Fatalf("expected shared runtime symbol %q in worker_process_helpers.go", symbol)
		}
	}

	nonImageJobSymbols := []string{
		"type LaunchAndWaitNetrunnerInput",
		"func LaunchAndWaitNetrunner(",
		"type CreateNetrunnerWaveInput",
		"func CreateNetrunnerWave(",
		"func LaunchNetrunnerWave(",
		"func WaitForNetrunnerWave(",
	}
	for _, symbol := range nonImageJobSymbols {
		if strings.Contains(string(imageJobSource), symbol) {
			t.Fatalf("expected non-image-job symbol %q to stay out of image_job_handlers.go", symbol)
		}
	}
}
