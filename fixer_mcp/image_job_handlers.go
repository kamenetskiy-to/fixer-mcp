package main

import (
	"bufio"
	"bytes"
	"context"
	"database/sql"
	"encoding/json"
	"fmt"
	"io"
	"log"
	"os"
	"os/exec"
	"path/filepath"
	"regexp"
	"sort"
	"strconv"
	"strings"
	"time"

	"github.com/modelcontextprotocol/go-sdk/mcp"
)

const (
	defaultImageJobTimeoutSeconds = 7200
	defaultImageJobPollSeconds    = 3
	maxImageJobTimeoutSeconds     = 21600
	maxImageJobPollSeconds        = 60
)

const (
	imageJobStatusLaunching = "launching"
	imageJobStatusRunning   = "running"
	imageJobStatusCompleted = "completed"
	imageJobStatusFailed    = "failed"
)

var generatedImagePathRegex = regexp.MustCompile(`/(?:[^/\s"']+/)*[^/\s"']+\.(?:png|jpe?g|webp)`)
var codexThreadIDRegex = regexp.MustCompile(`"thread_id"\s*:\s*"([^"]+)"`)

type imageGenerationJobRow struct {
	ID                      int
	ProjectID               int
	Prompt                  string
	Status                  string
	PID                     int
	Model                   string
	OutputPath              string
	WorkspaceCopyPath       string
	OutputLastMessagePath   string
	JSONOutputPath          string
	StderrLogPath           string
	ThreadID                string
	FailureReason           string
	GeneratedImagesBaseline string
	StartedAt               string
	UpdatedAt               string
	CompletedAt             string
}

func imageGenerationArtifacts(projectCWD string, jobID int) (string, string, string, error) {
	logDir := filepath.Join(projectCWD, ".codex", "image_generation_jobs")
	if err := os.MkdirAll(logDir, 0o755); err != nil {
		return "", "", "", fmt.Errorf("failed to prepare image generation job dir: %v", err)
	}
	suffix := strconv.FormatInt(time.Now().Unix(), 10)
	jsonOutputPath := filepath.Join(logDir, fmt.Sprintf("job-%d-events-%s.jsonl", jobID, suffix))
	lastMessagePath := filepath.Join(logDir, fmt.Sprintf("job-%d-last-message-%s.txt", jobID, suffix))
	stderrLogPath := filepath.Join(logDir, fmt.Sprintf("job-%d-stderr-%s.log", jobID, suffix))
	return jsonOutputPath, lastMessagePath, stderrLogPath, nil
}

func imageGenerationRootDir() (string, error) {
	homeDir, err := os.UserHomeDir()
	if err != nil {
		return "", err
	}
	return filepath.Join(homeDir, ".codex", "generated_images"), nil
}

func snapshotGeneratedImagePaths(root string) ([]string, error) {
	if strings.TrimSpace(root) == "" {
		return []string{}, nil
	}
	info, err := os.Stat(root)
	if err != nil {
		if os.IsNotExist(err) {
			return []string{}, nil
		}
		return nil, err
	}
	if !info.IsDir() {
		return []string{}, nil
	}

	paths := []string{}
	err = filepath.Walk(root, func(path string, fileInfo os.FileInfo, walkErr error) error {
		if walkErr != nil {
			return walkErr
		}
		if fileInfo == nil || fileInfo.IsDir() {
			return nil
		}
		if !isSupportedImageFile(path) {
			return nil
		}
		paths = append(paths, path)
		return nil
	})
	if err != nil {
		return nil, err
	}
	sort.Strings(paths)
	return paths, nil
}

func isSupportedImageFile(path string) bool {
	switch strings.ToLower(filepath.Ext(path)) {
	case ".png", ".jpg", ".jpeg", ".webp":
		return true
	default:
		return false
	}
}

func resolveLaunchImageInputPaths(projectCWD string, rawPaths []string) ([]string, error) {
	if len(rawPaths) == 0 {
		return []string{}, nil
	}
	resolvedPaths := make([]string, 0, len(rawPaths))
	for index, rawPath := range rawPaths {
		trimmed := strings.TrimSpace(rawPath)
		if trimmed == "" {
			return nil, fmt.Errorf("input_image_paths[%d] is required", index)
		}
		var candidate string
		if filepath.IsAbs(trimmed) {
			candidate = filepath.Clean(trimmed)
		} else {
			cleaned := filepath.Clean(trimmed)
			if cleaned == "." || cleaned == ".." || strings.HasPrefix(cleaned, ".."+string(os.PathSeparator)) {
				return nil, fmt.Errorf("input_image_paths[%d] must stay within the project root when using a relative path", index)
			}
			candidate = filepath.Join(projectCWD, cleaned)
		}
		info, err := os.Stat(candidate)
		if err != nil {
			return nil, fmt.Errorf("input_image_paths[%d] does not exist: %v", index, err)
		}
		if info.IsDir() {
			return nil, fmt.Errorf("input_image_paths[%d] must reference a file, not a directory", index)
		}
		if !isSupportedImageFile(candidate) {
			return nil, fmt.Errorf("input_image_paths[%d] must be a supported local image file (.png, .jpg, .jpeg, .webp)", index)
		}
		resolvedPaths = append(resolvedPaths, candidate)
	}
	return resolvedPaths, nil
}

func encodeGeneratedImageBaseline(paths []string) string {
	if len(paths) == 0 {
		return "[]"
	}
	encoded, err := json.Marshal(paths)
	if err != nil {
		return "[]"
	}
	return string(encoded)
}

func decodeGeneratedImageBaseline(raw string) []string {
	trimmed := strings.TrimSpace(raw)
	if trimmed == "" {
		return []string{}
	}
	var paths []string
	if err := json.Unmarshal([]byte(trimmed), &paths); err != nil {
		return []string{}
	}
	return paths
}

func insertImageGenerationJob(projectID int, prompt string, model string, baseline []string) (int, error) {
	result, err := db.Exec(
		`INSERT INTO image_generation_job (project_id, prompt, status, model, generated_images_baseline, updated_at)
		 VALUES (?, ?, ?, ?, ?, CURRENT_TIMESTAMP)`,
		projectID,
		prompt,
		imageJobStatusLaunching,
		model,
		encodeGeneratedImageBaseline(baseline),
	)
	if err != nil {
		return 0, err
	}
	lastInsertID, err := result.LastInsertId()
	if err != nil {
		return 0, err
	}
	return int(lastInsertID), nil
}

func fetchImageGenerationJob(jobID int, projectID int) (imageGenerationJobRow, error) {
	job := imageGenerationJobRow{}
	err := db.QueryRow(
		`SELECT id,
		        project_id,
		        prompt,
		        status,
		        COALESCE(pid, 0),
		        COALESCE(model, ''),
		        COALESCE(output_path, ''),
		        COALESCE(workspace_copy_path, ''),
		        COALESCE(output_last_message_path, ''),
		        COALESCE(json_output_path, ''),
		        COALESCE(stderr_log_path, ''),
		        COALESCE(thread_id, ''),
		        COALESCE(failure_reason, ''),
		        COALESCE(generated_images_baseline, '[]'),
		        started_at,
		        updated_at,
		        COALESCE(completed_at, '')
		 FROM image_generation_job
		 WHERE id = ? AND project_id = ?`,
		jobID,
		projectID,
	).Scan(
		&job.ID,
		&job.ProjectID,
		&job.Prompt,
		&job.Status,
		&job.PID,
		&job.Model,
		&job.OutputPath,
		&job.WorkspaceCopyPath,
		&job.OutputLastMessagePath,
		&job.JSONOutputPath,
		&job.StderrLogPath,
		&job.ThreadID,
		&job.FailureReason,
		&job.GeneratedImagesBaseline,
		&job.StartedAt,
		&job.UpdatedAt,
		&job.CompletedAt,
	)
	return job, err
}

func updateImageGenerationJobLaunch(jobID int, projectID int, pid int, jsonOutputPath string, lastMessagePath string, stderrLogPath string) error {
	_, err := db.Exec(
		`UPDATE image_generation_job
		 SET status = ?, pid = ?, json_output_path = ?, output_last_message_path = ?, stderr_log_path = ?, updated_at = CURRENT_TIMESTAMP
		 WHERE id = ? AND project_id = ?`,
		imageJobStatusRunning,
		pid,
		jsonOutputPath,
		lastMessagePath,
		stderrLogPath,
		jobID,
		projectID,
	)
	return err
}

func finalizeImageGenerationJob(jobID int, projectID int, status string, outputPath string, threadID string, failureReason string) error {
	_, err := db.Exec(
		`UPDATE image_generation_job
		 SET status = ?,
		     output_path = ?,
		     thread_id = ?,
		     failure_reason = ?,
		     completed_at = CURRENT_TIMESTAMP,
		     updated_at = CURRENT_TIMESTAMP
		 WHERE id = ? AND project_id = ?`,
		status,
		outputPath,
		threadID,
		failureReason,
		jobID,
		projectID,
	)
	return err
}

func updateImageGenerationJobWorkspaceCopy(jobID int, projectID int, workspaceCopyPath string) error {
	_, err := db.Exec(
		`UPDATE image_generation_job
		 SET workspace_copy_path = ?, updated_at = CURRENT_TIMESTAMP
		 WHERE id = ? AND project_id = ?`,
		workspaceCopyPath,
		jobID,
		projectID,
	)
	return err
}

func extractThreadIDFromJSONL(path string) string {
	file, err := os.Open(path)
	if err != nil {
		return ""
	}
	defer file.Close()

	scanner := bufio.NewScanner(file)
	scanner.Buffer(make([]byte, 0, 64*1024), 1024*1024)
	for scanner.Scan() {
		match := codexThreadIDRegex.FindStringSubmatch(scanner.Text())
		if len(match) == 2 {
			return strings.TrimSpace(match[1])
		}
	}
	return ""
}

func extractPathCandidatesFromText(text string) []string {
	matches := generatedImagePathRegex.FindAllString(text, -1)
	if len(matches) == 0 {
		return []string{}
	}
	seen := make(map[string]struct{}, len(matches))
	candidates := make([]string, 0, len(matches))
	for _, match := range matches {
		cleaned := strings.TrimSpace(match)
		if cleaned == "" || !isSupportedImageFile(cleaned) {
			continue
		}
		if _, exists := seen[cleaned]; exists {
			continue
		}
		seen[cleaned] = struct{}{}
		candidates = append(candidates, cleaned)
	}
	return candidates
}

func extractPathCandidatesFromFile(path string) []string {
	payload, err := os.ReadFile(path)
	if err != nil {
		return []string{}
	}
	return extractPathCandidatesFromText(string(payload))
}

func resolveGeneratedImagePathFromThreadID(threadID string) string {
	trimmed := strings.TrimSpace(threadID)
	if trimmed == "" {
		return ""
	}
	root, err := imageGenerationRootDir()
	if err != nil {
		return ""
	}
	threadDir := filepath.Join(root, trimmed)
	paths, err := snapshotGeneratedImagePaths(threadDir)
	if err != nil {
		return ""
	}
	return latestMatchingFile(paths)
}

func resolveGeneratedImagePathFromSnapshot(job imageGenerationJobRow) string {
	root, err := imageGenerationRootDir()
	if err != nil {
		return ""
	}
	currentPaths, err := snapshotGeneratedImagePaths(root)
	if err != nil {
		return ""
	}
	baseline := decodeGeneratedImageBaseline(job.GeneratedImagesBaseline)
	baselineSet := make(map[string]struct{}, len(baseline))
	for _, path := range baseline {
		baselineSet[path] = struct{}{}
	}
	candidates := make([]string, 0, len(currentPaths))
	for _, path := range currentPaths {
		if _, exists := baselineSet[path]; exists {
			continue
		}
		candidates = append(candidates, path)
	}
	return latestMatchingFile(candidates)
}

func resolveImageGenerationJobResult(job imageGenerationJobRow) (string, string) {
	candidates := []string{}
	if strings.TrimSpace(job.JSONOutputPath) != "" {
		candidates = append(candidates, extractPathCandidatesFromFile(job.JSONOutputPath)...)
	}
	discoveredThreadID := extractThreadIDFromJSONL(job.JSONOutputPath)
	threadID := strings.TrimSpace(job.ThreadID)
	if threadID == "" {
		threadID = discoveredThreadID
	}
	if resolved := resolveGeneratedImagePathFromThreadID(threadID); resolved != "" {
		candidates = append(candidates, resolved)
	}
	if strings.TrimSpace(job.OutputLastMessagePath) != "" {
		candidates = append(candidates, extractPathCandidatesFromFile(job.OutputLastMessagePath)...)
	}
	if resolved := resolveGeneratedImagePathFromSnapshot(job); resolved != "" {
		candidates = append(candidates, resolved)
	}
	return latestMatchingFile(candidates), threadID
}

func refreshImageGenerationJob(job imageGenerationJobRow) (imageGenerationJobRow, error) {
	if job.Status == imageJobStatusCompleted || job.Status == imageJobStatusFailed {
		return job, nil
	}
	if job.PID > 0 && isProcessAlive(job.PID) {
		return job, nil
	}

	outputPath, threadID := resolveImageGenerationJobResult(job)
	if outputPath != "" {
		if err := finalizeImageGenerationJob(job.ID, job.ProjectID, imageJobStatusCompleted, outputPath, threadID, ""); err != nil {
			return imageGenerationJobRow{}, err
		}
		return fetchImageGenerationJob(job.ID, job.ProjectID)
	}

	failureReason := "image generation subprocess exited without a resolved output path"
	if strings.TrimSpace(job.StderrLogPath) != "" {
		failureReason += "; inspect stderr log: " + job.StderrLogPath
	}
	if err := finalizeImageGenerationJob(job.ID, job.ProjectID, imageJobStatusFailed, "", threadID, failureReason); err != nil {
		return imageGenerationJobRow{}, err
	}
	return fetchImageGenerationJob(job.ID, job.ProjectID)
}

func imageJobTimeoutSeconds(timeoutSeconds int) (int, error) {
	if timeoutSeconds == 0 {
		return defaultImageJobTimeoutSeconds, nil
	}
	if timeoutSeconds < 0 {
		return 0, fmt.Errorf("timeout_seconds must be positive")
	}
	if timeoutSeconds > maxImageJobTimeoutSeconds {
		return 0, fmt.Errorf("timeout_seconds must be <= %d", maxImageJobTimeoutSeconds)
	}
	return timeoutSeconds, nil
}

func imageJobPollIntervalSeconds(pollIntervalSeconds int) (int, error) {
	if pollIntervalSeconds == 0 {
		return defaultImageJobPollSeconds, nil
	}
	if pollIntervalSeconds < 0 {
		return 0, fmt.Errorf("poll_interval_seconds must be positive")
	}
	if pollIntervalSeconds > maxImageJobPollSeconds {
		return 0, fmt.Errorf("poll_interval_seconds must be <= %d", maxImageJobPollSeconds)
	}
	return pollIntervalSeconds, nil
}

func copyFile(srcPath string, dstPath string) error {
	source, err := os.Open(srcPath)
	if err != nil {
		return err
	}
	defer source.Close()

	if err := os.MkdirAll(filepath.Dir(dstPath), 0o755); err != nil {
		return err
	}
	destination, err := os.OpenFile(dstPath, os.O_CREATE|os.O_TRUNC|os.O_WRONLY, 0o644)
	if err != nil {
		return err
	}
	defer destination.Close()

	if _, err := io.Copy(destination, source); err != nil {
		return err
	}
	return destination.Close()
}

func resolveWorkspaceDestination(projectCWD string, relativePath string) (string, error) {
	trimmed := strings.TrimSpace(relativePath)
	if trimmed == "" {
		return "", fmt.Errorf("destination_path is required")
	}
	if filepath.IsAbs(trimmed) {
		return "", fmt.Errorf("destination_path must be project-relative")
	}
	cleaned := filepath.Clean(trimmed)
	if cleaned == "." || cleaned == ".." || strings.HasPrefix(cleaned, ".."+string(os.PathSeparator)) {
		return "", fmt.Errorf("destination_path must stay within the project root")
	}
	return filepath.Join(projectCWD, cleaned), nil
}

type LaunchImageGenerationJobInput struct {
	Prompt          string   `json:"prompt" jsonschema:"Prompt to send to the dedicated Codex image-generation subprocess."`
	Model           string   `json:"model,omitempty" jsonschema:"Optional model override for the image-generation subprocess."`
	InputImagePaths []string `json:"input_image_paths,omitempty" jsonschema:"Optional local image file paths to attach for built-in image editing flows. Relative paths are resolved from the project root."`
}

type LaunchImageGenerationJobOutput struct {
	Status          string   `json:"status"`
	JobId           int      `json:"job_id"`
	JobStatus       string   `json:"job_status"`
	PID             int      `json:"pid"`
	Model           string   `json:"model"`
	InputImagePaths []string `json:"input_image_paths,omitempty"`
	JSONOutput      string   `json:"json_output_path"`
	LastOutput      string   `json:"output_last_message_path"`
	StderrLog       string   `json:"stderr_log_path"`
}

type WaitForImageGenerationJobInput struct {
	JobId               int `json:"job_id" jsonschema:"Project-scoped image-generation job id to wait on."`
	TimeoutSeconds      int `json:"timeout_seconds,omitempty" jsonschema:"Optional wait timeout in seconds. Default 7200; max 21600."`
	PollIntervalSeconds int `json:"poll_interval_seconds,omitempty" jsonschema:"Optional poll interval in seconds. Default 3; max 60."`
}

type ImageGenerationJobWaitResult struct {
	JobId                 int    `json:"job_id"`
	Status                string `json:"status"`
	OutputPath            string `json:"output_path,omitempty"`
	WorkspaceCopyPath     string `json:"workspace_copy_path,omitempty"`
	ThreadId              string `json:"thread_id,omitempty"`
	FailureReason         string `json:"failure_reason,omitempty"`
	Terminal              bool   `json:"terminal"`
	TimedOut              bool   `json:"timed_out"`
	TimeoutSeconds        int    `json:"timeout_seconds"`
	PollIntervalSeconds   int    `json:"poll_interval_seconds"`
	ElapsedSeconds        int    `json:"elapsed_seconds"`
	JSONOutputPath        string `json:"json_output_path,omitempty"`
	OutputLastMessagePath string `json:"output_last_message_path,omitempty"`
	StderrLogPath         string `json:"stderr_log_path,omitempty"`
}

type WaitForImageGenerationJobOutput struct {
	Status string                       `json:"status"`
	Result ImageGenerationJobWaitResult `json:"result"`
}

type CopyImageGenerationJobOutputInput struct {
	JobId           int    `json:"job_id" jsonschema:"Project-scoped image-generation job id whose output should be copied."`
	DestinationPath string `json:"destination_path" jsonschema:"Project-relative destination path for the copied image."`
}

type CopyImageGenerationJobOutputOutput struct {
	Status            string `json:"status"`
	JobId             int    `json:"job_id"`
	SourcePath        string `json:"source_path"`
	DestinationPath   string `json:"destination_path"`
	WorkspaceCopyPath string `json:"workspace_copy_path"`
}

func LaunchImageGenerationJob(ctx context.Context, req *mcp.CallToolRequest, input LaunchImageGenerationJobInput) (*mcp.CallToolResult, LaunchImageGenerationJobOutput, error) {
	if authorizedRole != "fixer" {
		return &mcp.CallToolResult{IsError: true}, LaunchImageGenerationJobOutput{}, fmt.Errorf("access denied: requires fixer role")
	}
	prompt := strings.TrimSpace(input.Prompt)
	if prompt == "" {
		return &mcp.CallToolResult{IsError: true}, LaunchImageGenerationJobOutput{}, fmt.Errorf("prompt is required")
	}

	projectCWD, err := projectCWDFromID(authorizedProjectId)
	if err != nil {
		return &mcp.CallToolResult{IsError: true}, LaunchImageGenerationJobOutput{}, fmt.Errorf("DB query error: %v", err)
	}
	inputImagePaths, err := resolveLaunchImageInputPaths(projectCWD, input.InputImagePaths)
	if err != nil {
		return &mcp.CallToolResult{IsError: true}, LaunchImageGenerationJobOutput{}, err
	}
	generatedImagesRoot, err := imageGenerationRootDir()
	if err != nil {
		return &mcp.CallToolResult{IsError: true}, LaunchImageGenerationJobOutput{}, fmt.Errorf("failed to resolve generated images root: %v", err)
	}
	baseline, err := snapshotGeneratedImagePaths(generatedImagesRoot)
	if err != nil {
		return &mcp.CallToolResult{IsError: true}, LaunchImageGenerationJobOutput{}, fmt.Errorf("failed to snapshot generated images root: %v", err)
	}

	model := strings.TrimSpace(input.Model)
	if model == "" {
		model = "gpt-5.4"
	}
	jobID, err := insertImageGenerationJob(authorizedProjectId, prompt, model, baseline)
	if err != nil {
		return &mcp.CallToolResult{IsError: true}, LaunchImageGenerationJobOutput{}, fmt.Errorf("failed to persist image generation job: %v", err)
	}
	jsonOutputPath, lastMessagePath, stderrLogPath, err := imageGenerationArtifacts(projectCWD, jobID)
	if err != nil {
		return &mcp.CallToolResult{IsError: true}, LaunchImageGenerationJobOutput{}, err
	}

	commandArgs := []string{
		"--enable", "image_generation",
		"exec",
		"--skip-git-repo-check",
		"--json",
		"--output-last-message", lastMessagePath,
		"--model", model,
	}
	for _, imagePath := range inputImagePaths {
		commandArgs = append(commandArgs, "--image", imagePath)
	}
	if len(inputImagePaths) > 0 {
		commandArgs = append(commandArgs, "-")
	} else {
		commandArgs = append(commandArgs, prompt)
	}
	command := execCommand("codex", commandArgs...)
	command.Dir = projectCWD
	if len(inputImagePaths) > 0 {
		command.Stdin = strings.NewReader(prompt)
	} else {
		command.Stdin = bytes.NewReader(nil)
	}
	commandEnv, envErr := resolveRuntimeLaunchEnv(projectCWD, os.Environ())
	if envErr != nil {
		log.Printf("warning: failed to resolve runtime launch env for %s: %v", projectCWD, envErr)
		commandEnv = os.Environ()
	}
	command.Env = commandEnv

	jsonHandle, err := os.OpenFile(jsonOutputPath, os.O_CREATE|os.O_WRONLY|os.O_TRUNC, 0o644)
	if err != nil {
		return &mcp.CallToolResult{IsError: true}, LaunchImageGenerationJobOutput{}, fmt.Errorf("failed to open image job JSON output: %v", err)
	}
	defer jsonHandle.Close()
	stderrHandle, err := os.OpenFile(stderrLogPath, os.O_CREATE|os.O_WRONLY|os.O_TRUNC, 0o644)
	if err != nil {
		return &mcp.CallToolResult{IsError: true}, LaunchImageGenerationJobOutput{}, fmt.Errorf("failed to open image job stderr log: %v", err)
	}
	defer stderrHandle.Close()
	command.Stdout = jsonHandle
	command.Stderr = stderrHandle

	if err := command.Start(); err != nil {
		if finalizeErr := finalizeImageGenerationJob(jobID, authorizedProjectId, imageJobStatusFailed, "", "", fmt.Sprintf("failed to launch image generation subprocess: %v", err)); finalizeErr != nil {
			log.Printf("warning: failed to persist image generation launch failure: %v", finalizeErr)
		}
		return &mcp.CallToolResult{IsError: true}, LaunchImageGenerationJobOutput{}, fmt.Errorf("failed to launch image generation subprocess: %v", err)
	}
	if err := updateImageGenerationJobLaunch(jobID, authorizedProjectId, command.Process.Pid, jsonOutputPath, lastMessagePath, stderrLogPath); err != nil {
		return &mcp.CallToolResult{IsError: true}, LaunchImageGenerationJobOutput{}, fmt.Errorf("failed to persist image generation launch metadata: %v", err)
	}

	go func(cmd *exec.Cmd, jobID int) {
		if waitErr := cmd.Wait(); waitErr != nil {
			log.Printf("image generation job %d process wait returned: %v", jobID, waitErr)
		}
	}(command, jobID)

	return nil, LaunchImageGenerationJobOutput{
		Status:          "success",
		JobId:           jobID,
		JobStatus:       imageJobStatusRunning,
		PID:             command.Process.Pid,
		Model:           model,
		InputImagePaths: inputImagePaths,
		JSONOutput:      jsonOutputPath,
		LastOutput:      lastMessagePath,
		StderrLog:       stderrLogPath,
	}, nil
}

func WaitForImageGenerationJob(ctx context.Context, req *mcp.CallToolRequest, input WaitForImageGenerationJobInput) (*mcp.CallToolResult, WaitForImageGenerationJobOutput, error) {
	if authorizedRole != "fixer" {
		return &mcp.CallToolResult{IsError: true}, WaitForImageGenerationJobOutput{}, fmt.Errorf("access denied: requires fixer role")
	}
	timeoutSeconds, err := imageJobTimeoutSeconds(input.TimeoutSeconds)
	if err != nil {
		return &mcp.CallToolResult{IsError: true}, WaitForImageGenerationJobOutput{}, err
	}
	pollIntervalSeconds, err := imageJobPollIntervalSeconds(input.PollIntervalSeconds)
	if err != nil {
		return &mcp.CallToolResult{IsError: true}, WaitForImageGenerationJobOutput{}, err
	}

	job, err := fetchImageGenerationJob(input.JobId, authorizedProjectId)
	if err == sql.ErrNoRows {
		return &mcp.CallToolResult{IsError: true}, WaitForImageGenerationJobOutput{}, fmt.Errorf("image generation job not found in current project")
	}
	if err != nil {
		return &mcp.CallToolResult{IsError: true}, WaitForImageGenerationJobOutput{}, fmt.Errorf("DB query error: %v", err)
	}

	startedAt := time.Now()
	deadline := startedAt.Add(time.Duration(timeoutSeconds) * time.Second)
	buildResult := func(job imageGenerationJobRow, terminal bool, timedOut bool) ImageGenerationJobWaitResult {
		return ImageGenerationJobWaitResult{
			JobId:                 job.ID,
			Status:                job.Status,
			OutputPath:            job.OutputPath,
			WorkspaceCopyPath:     job.WorkspaceCopyPath,
			ThreadId:              job.ThreadID,
			FailureReason:         job.FailureReason,
			Terminal:              terminal,
			TimedOut:              timedOut,
			TimeoutSeconds:        timeoutSeconds,
			PollIntervalSeconds:   pollIntervalSeconds,
			ElapsedSeconds:        int(time.Since(startedAt).Seconds()),
			JSONOutputPath:        job.JSONOutputPath,
			OutputLastMessagePath: job.OutputLastMessagePath,
			StderrLogPath:         job.StderrLogPath,
		}
	}

	for {
		if err := ctx.Err(); err != nil {
			return &mcp.CallToolResult{IsError: true}, WaitForImageGenerationJobOutput{}, err
		}

		job, err = refreshImageGenerationJob(job)
		if err != nil {
			return &mcp.CallToolResult{IsError: true}, WaitForImageGenerationJobOutput{}, fmt.Errorf("failed to refresh image generation job: %v", err)
		}
		if job.Status == imageJobStatusCompleted || job.Status == imageJobStatusFailed {
			return nil, WaitForImageGenerationJobOutput{
				Status: "success",
				Result: buildResult(job, true, false),
			}, nil
		}

		if time.Now().After(deadline) {
			return nil, WaitForImageGenerationJobOutput{
				Status: "success",
				Result: buildResult(job, false, true),
			}, nil
		}

		time.Sleep(time.Duration(pollIntervalSeconds) * time.Second)
	}
}

func CopyImageGenerationJobOutput(ctx context.Context, req *mcp.CallToolRequest, input CopyImageGenerationJobOutputInput) (*mcp.CallToolResult, CopyImageGenerationJobOutputOutput, error) {
	if authorizedRole != "fixer" {
		return &mcp.CallToolResult{IsError: true}, CopyImageGenerationJobOutputOutput{}, fmt.Errorf("access denied: requires fixer role")
	}
	job, err := fetchImageGenerationJob(input.JobId, authorizedProjectId)
	if err == sql.ErrNoRows {
		return &mcp.CallToolResult{IsError: true}, CopyImageGenerationJobOutputOutput{}, fmt.Errorf("image generation job not found in current project")
	}
	if err != nil {
		return &mcp.CallToolResult{IsError: true}, CopyImageGenerationJobOutputOutput{}, fmt.Errorf("DB query error: %v", err)
	}
	job, err = refreshImageGenerationJob(job)
	if err != nil {
		return &mcp.CallToolResult{IsError: true}, CopyImageGenerationJobOutputOutput{}, fmt.Errorf("failed to refresh image generation job: %v", err)
	}
	if job.Status != imageJobStatusCompleted || strings.TrimSpace(job.OutputPath) == "" {
		return &mcp.CallToolResult{IsError: true}, CopyImageGenerationJobOutputOutput{}, fmt.Errorf("image generation job %d does not have a completed output to copy", input.JobId)
	}

	projectCWD, err := projectCWDFromID(authorizedProjectId)
	if err != nil {
		return &mcp.CallToolResult{IsError: true}, CopyImageGenerationJobOutputOutput{}, fmt.Errorf("DB query error: %v", err)
	}
	destinationPath, err := resolveWorkspaceDestination(projectCWD, input.DestinationPath)
	if err != nil {
		return &mcp.CallToolResult{IsError: true}, CopyImageGenerationJobOutputOutput{}, err
	}
	if err := copyFile(job.OutputPath, destinationPath); err != nil {
		return &mcp.CallToolResult{IsError: true}, CopyImageGenerationJobOutputOutput{}, fmt.Errorf("failed to copy generated image into workspace: %v", err)
	}
	if err := updateImageGenerationJobWorkspaceCopy(job.ID, job.ProjectID, destinationPath); err != nil {
		return &mcp.CallToolResult{IsError: true}, CopyImageGenerationJobOutputOutput{}, fmt.Errorf("failed to persist workspace copy path: %v", err)
	}

	return nil, CopyImageGenerationJobOutputOutput{
		Status:            "success",
		JobId:             job.ID,
		SourcePath:        job.OutputPath,
		DestinationPath:   destinationPath,
		WorkspaceCopyPath: destinationPath,
	}, nil
}
