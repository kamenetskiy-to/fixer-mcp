package main

import (
	"context"
	"io"
	"os"
	"os/exec"
	"path/filepath"
	"strings"
	"testing"
	"time"
)

func TestHelperProcessImageGenerationLaunch(t *testing.T) {
	if os.Getenv("GO_WANT_HELPER_PROCESS") != "1" {
		return
	}
	capturePath := os.Getenv("HELPER_CAPTURE_STDIN_PATH")
	if capturePath == "" {
		os.Exit(2)
	}
	payload, err := io.ReadAll(os.Stdin)
	if err != nil {
		os.Exit(3)
	}
	if err := os.MkdirAll(filepath.Dir(capturePath), 0o755); err != nil {
		os.Exit(4)
	}
	if err := os.WriteFile(capturePath, payload, 0o644); err != nil {
		os.Exit(5)
	}
	os.Exit(0)
}

func TestLaunchImageGenerationJob_DetachesStdinAndPersistsJob(t *testing.T) {
	originalDB := db
	originalRole := authorizedRole
	originalProjectID := authorizedProjectId
	originalExecCommand := execCommand
	defer func() {
		db = originalDB
		authorizedRole = originalRole
		authorizedProjectId = originalProjectID
		execCommand = originalExecCommand
	}()

	testDB := setupGetProjectsTestDB(t)
	defer func() {
		_ = testDB.Close()
	}()

	db = testDB
	authorizedRole = "fixer"
	authorizedProjectId = 1

	homeDir := t.TempDir()
	t.Setenv("HOME", homeDir)
	if err := os.MkdirAll(testProjectCWD, 0o755); err != nil {
		t.Fatalf("mkdir project cwd: %v", err)
	}
	capturePath := filepath.Join(t.TempDir(), "stdin.txt")
	t.Setenv("GO_WANT_HELPER_PROCESS", "1")
	t.Setenv("HELPER_CAPTURE_STDIN_PATH", capturePath)
	var launchedName string
	var launchedArgs []string

	execCommand = func(name string, arg ...string) *exec.Cmd {
		launchedName = name
		launchedArgs = append([]string{}, arg...)
		return exec.Command(os.Args[0], "-test.run=TestHelperProcessImageGenerationLaunch", "--")
	}

	callResult, out, err := LaunchImageGenerationJob(context.Background(), nil, LaunchImageGenerationJobInput{
		Prompt: "Generate a red square.",
	})
	if err != nil {
		t.Fatalf("launch_image_generation_job failed: %v", err)
	}
	if callResult != nil {
		t.Fatalf("expected nil call result on success, got %+v", callResult)
	}
	if out.JobId <= 0 || out.JobStatus != imageJobStatusRunning {
		t.Fatalf("unexpected launch output: %+v", out)
	}
	if launchedName != "codex" {
		t.Fatalf("expected codex launch, got %q", launchedName)
	}
	expectedArgs := []string{
		"--enable", "image_generation",
		"exec",
		"--skip-git-repo-check",
		"--json",
		"--output-last-message", out.LastOutput,
		"--model", "gpt-5.4",
		"Generate a red square.",
	}
	if strings.Join(launchedArgs, "\n") != strings.Join(expectedArgs, "\n") {
		t.Fatalf("unexpected launch args:\nwant: %#v\ngot:  %#v", expectedArgs, launchedArgs)
	}

	var payload []byte
	var readErr error
	for attempt := 0; attempt < 20; attempt++ {
		payload, readErr = os.ReadFile(capturePath)
		if readErr == nil {
			break
		}
		if !os.IsNotExist(readErr) {
			t.Fatalf("read captured stdin: %v", readErr)
		}
		time.Sleep(10 * time.Millisecond)
	}
	if readErr != nil {
		t.Fatalf("read captured stdin: %v", readErr)
	}
	if len(payload) != 0 {
		t.Fatalf("expected detached empty stdin, got %q", string(payload))
	}

	job, err := fetchImageGenerationJob(out.JobId, 1)
	if err != nil {
		t.Fatalf("fetchImageGenerationJob failed: %v", err)
	}
	if job.JSONOutputPath == "" || job.OutputLastMessagePath == "" || job.StderrLogPath == "" {
		t.Fatalf("expected persisted artifact paths, got %+v", job)
	}
}

func TestLaunchImageGenerationJob_WithInputImages(t *testing.T) {
	originalDB := db
	originalRole := authorizedRole
	originalProjectID := authorizedProjectId
	originalExecCommand := execCommand
	defer func() {
		db = originalDB
		authorizedRole = originalRole
		authorizedProjectId = originalProjectID
		execCommand = originalExecCommand
	}()

	testDB := setupGetProjectsTestDB(t)
	defer func() {
		_ = testDB.Close()
	}()

	db = testDB
	authorizedRole = "fixer"
	authorizedProjectId = 1

	normalizedProjectCWD, err := normalizeProjectCWD(testProjectCWD)
	if err != nil {
		t.Fatalf("normalize project cwd: %v", err)
	}
	homeDir := t.TempDir()
	t.Setenv("HOME", homeDir)
	if err := os.MkdirAll(testProjectCWD, 0o755); err != nil {
		t.Fatalf("mkdir project cwd: %v", err)
	}
	capturePath := filepath.Join(t.TempDir(), "stdin.txt")
	t.Setenv("GO_WANT_HELPER_PROCESS", "1")
	t.Setenv("HELPER_CAPTURE_STDIN_PATH", capturePath)

	projectImageDir := filepath.Join(normalizedProjectCWD, "assets")
	if err := os.MkdirAll(projectImageDir, 0o755); err != nil {
		t.Fatalf("mkdir project image dir: %v", err)
	}
	relativeImagePath := filepath.Join(projectImageDir, "mask.png")
	if err := os.WriteFile(relativeImagePath, []byte("png"), 0o644); err != nil {
		t.Fatalf("write relative image: %v", err)
	}
	absoluteImagePath := filepath.Join(t.TempDir(), "reference.webp")
	if err := os.WriteFile(absoluteImagePath, []byte("webp"), 0o644); err != nil {
		t.Fatalf("write absolute image: %v", err)
	}

	var launchedArgs []string
	execCommand = func(name string, arg ...string) *exec.Cmd {
		launchedArgs = append([]string{}, arg...)
		return exec.Command(os.Args[0], "-test.run=TestHelperProcessImageGenerationLaunch", "--")
	}

	callResult, out, err := LaunchImageGenerationJob(context.Background(), nil, LaunchImageGenerationJobInput{
		Prompt:          "Edit this image into a monochrome poster.",
		InputImagePaths: []string{"assets/mask.png", absoluteImagePath},
	})
	if err != nil {
		t.Fatalf("launch_image_generation_job with input images failed: %v", err)
	}
	if callResult != nil {
		t.Fatalf("expected nil call result on success, got %+v", callResult)
	}
	expectedResolvedPaths := []string{relativeImagePath, absoluteImagePath}
	if strings.Join(out.InputImagePaths, "\n") != strings.Join(expectedResolvedPaths, "\n") {
		t.Fatalf("unexpected resolved input image paths:\nwant: %#v\ngot:  %#v", expectedResolvedPaths, out.InputImagePaths)
	}
	expectedArgs := []string{
		"--enable", "image_generation",
		"exec",
		"--skip-git-repo-check",
		"--json",
		"--output-last-message", out.LastOutput,
		"--model", "gpt-5.4",
		"--image", relativeImagePath,
		"--image", absoluteImagePath,
		"-",
	}
	if strings.Join(launchedArgs, "\n") != strings.Join(expectedArgs, "\n") {
		t.Fatalf("unexpected launch args with input images:\nwant: %#v\ngot:  %#v", expectedArgs, launchedArgs)
	}
	var payload []byte
	var readErr error
	for attempt := 0; attempt < 20; attempt++ {
		payload, readErr = os.ReadFile(capturePath)
		if readErr == nil {
			break
		}
		if !os.IsNotExist(readErr) {
			t.Fatalf("read captured stdin: %v", readErr)
		}
		time.Sleep(10 * time.Millisecond)
	}
	if readErr != nil {
		t.Fatalf("read captured stdin: %v", readErr)
	}
	if string(payload) != "Edit this image into a monochrome poster." {
		t.Fatalf("expected prompt on stdin, got %q", string(payload))
	}
}

func TestResolveLaunchImageInputPaths_Validation(t *testing.T) {
	projectCWD := t.TempDir()
	imageDir := filepath.Join(projectCWD, "images")
	if err := os.MkdirAll(imageDir, 0o755); err != nil {
		t.Fatalf("mkdir image dir: %v", err)
	}
	validImagePath := filepath.Join(imageDir, "source.png")
	if err := os.WriteFile(validImagePath, []byte("png"), 0o644); err != nil {
		t.Fatalf("write valid image: %v", err)
	}
	textPath := filepath.Join(imageDir, "notes.txt")
	if err := os.WriteFile(textPath, []byte("txt"), 0o644); err != nil {
		t.Fatalf("write text file: %v", err)
	}
	outsideImagePath := filepath.Join(t.TempDir(), "outside.jpg")
	if err := os.WriteFile(outsideImagePath, []byte("jpg"), 0o644); err != nil {
		t.Fatalf("write outside image: %v", err)
	}

	tests := []struct {
		name        string
		input       []string
		want        []string
		wantErrPart string
	}{
		{
			name:  "resolves relative and absolute paths",
			input: []string{"images/source.png", outsideImagePath},
			want:  []string{validImagePath, outsideImagePath},
		},
		{
			name:        "rejects escaping relative path",
			input:       []string{"../escape.png"},
			wantErrPart: "must stay within the project root",
		},
		{
			name:        "rejects directories",
			input:       []string{"images"},
			wantErrPart: "must reference a file",
		},
		{
			name:        "rejects unsupported extension",
			input:       []string{"images/notes.txt"},
			wantErrPart: "supported local image file",
		},
		{
			name:        "rejects missing files",
			input:       []string{"images/missing.png"},
			wantErrPart: "does not exist",
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			got, err := resolveLaunchImageInputPaths(projectCWD, tt.input)
			if tt.wantErrPart != "" {
				if err == nil {
					t.Fatalf("expected error containing %q, got nil", tt.wantErrPart)
				}
				if !strings.Contains(err.Error(), tt.wantErrPart) {
					t.Fatalf("expected error containing %q, got %v", tt.wantErrPart, err)
				}
				return
			}
			if err != nil {
				t.Fatalf("resolveLaunchImageInputPaths failed: %v", err)
			}
			if strings.Join(got, "\n") != strings.Join(tt.want, "\n") {
				t.Fatalf("unexpected resolved paths:\nwant: %#v\ngot:  %#v", tt.want, got)
			}
		})
	}
}

func TestWaitForImageGenerationJob_ResolvesFromThreadDirectory(t *testing.T) {
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

	db = testDB
	authorizedRole = "fixer"
	authorizedProjectId = 1

	homeDir := t.TempDir()
	t.Setenv("HOME", homeDir)
	threadID := "thread-test-123"
	threadDir := filepath.Join(homeDir, ".codex", "generated_images", threadID)
	if err := os.MkdirAll(threadDir, 0o755); err != nil {
		t.Fatalf("mkdir thread dir: %v", err)
	}
	imagePath := filepath.Join(threadDir, "ig_test.png")
	if err := os.WriteFile(imagePath, []byte("png"), 0o644); err != nil {
		t.Fatalf("write generated image: %v", err)
	}
	jsonPath := filepath.Join(t.TempDir(), "events.jsonl")
	if err := os.WriteFile(jsonPath, []byte("{\"type\":\"thread.started\",\"thread_id\":\""+threadID+"\"}\n"), 0o644); err != nil {
		t.Fatalf("write jsonl: %v", err)
	}
	if _, err := testDB.Exec(
		`INSERT INTO image_generation_job (project_id, prompt, status, pid, json_output_path, generated_images_baseline)
		 VALUES (1, 'prompt', ?, 0, ?, '[]')`,
		imageJobStatusRunning,
		jsonPath,
	); err != nil {
		t.Fatalf("seed image job: %v", err)
	}

	callResult, out, err := WaitForImageGenerationJob(context.Background(), nil, WaitForImageGenerationJobInput{
		JobId:               1,
		TimeoutSeconds:      1,
		PollIntervalSeconds: 1,
	})
	if err != nil {
		t.Fatalf("wait_for_image_generation_job failed: %v", err)
	}
	if callResult != nil {
		t.Fatalf("expected nil call result on success, got %+v", callResult)
	}
	if !out.Result.Terminal || out.Result.Status != imageJobStatusCompleted {
		t.Fatalf("expected completed terminal result, got %+v", out.Result)
	}
	if out.Result.OutputPath != imagePath {
		t.Fatalf("expected output path %q, got %+v", imagePath, out.Result)
	}
	if out.Result.ThreadId != threadID {
		t.Fatalf("expected thread id %q, got %+v", threadID, out.Result)
	}
}

func TestWaitForImageGenerationJob_UsesFilesystemSnapshotFallback(t *testing.T) {
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

	db = testDB
	authorizedRole = "fixer"
	authorizedProjectId = 1

	homeDir := t.TempDir()
	t.Setenv("HOME", homeDir)
	rootDir := filepath.Join(homeDir, ".codex", "generated_images")
	oldDir := filepath.Join(rootDir, "old")
	newDir := filepath.Join(rootDir, "new")
	if err := os.MkdirAll(oldDir, 0o755); err != nil {
		t.Fatalf("mkdir old dir: %v", err)
	}
	if err := os.MkdirAll(newDir, 0o755); err != nil {
		t.Fatalf("mkdir new dir: %v", err)
	}
	oldPath := filepath.Join(oldDir, "ig_old.png")
	newPath := filepath.Join(newDir, "ig_new.png")
	if err := os.WriteFile(oldPath, []byte("old"), 0o644); err != nil {
		t.Fatalf("write old image: %v", err)
	}
	if err := os.WriteFile(newPath, []byte("new"), 0o644); err != nil {
		t.Fatalf("write new image: %v", err)
	}
	if _, err := testDB.Exec(
		`INSERT INTO image_generation_job (project_id, prompt, status, pid, generated_images_baseline)
		 VALUES (1, 'prompt', ?, 0, ?)`,
		imageJobStatusRunning,
		encodeGeneratedImageBaseline([]string{oldPath}),
	); err != nil {
		t.Fatalf("seed image job: %v", err)
	}

	callResult, out, err := WaitForImageGenerationJob(context.Background(), nil, WaitForImageGenerationJobInput{
		JobId:               1,
		TimeoutSeconds:      1,
		PollIntervalSeconds: 1,
	})
	if err != nil {
		t.Fatalf("wait_for_image_generation_job failed: %v", err)
	}
	if callResult != nil {
		t.Fatalf("expected nil call result on success, got %+v", callResult)
	}
	if out.Result.OutputPath != newPath {
		t.Fatalf("expected snapshot fallback path %q, got %+v", newPath, out.Result)
	}
}
