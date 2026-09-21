package fleet

import (
	"context"
	"encoding/json"
	"fmt"
	"os"
	"os/exec"
	"path/filepath"
	"strings"
	"time"
)

type Result struct {
	Root      string    `json:"root,omitempty"`
	OK        bool      `json:"ok"`
	Summary   string    `json:"summary,omitempty"`
	Output    string    `json:"output,omitempty"`
	CheckedAt time.Time `json:"checked_at"`
	Error     string    `json:"error,omitempty"`
}

func FindRoot(start string) string {
	if start == "" {
		start, _ = os.Getwd()
	}
	path, err := filepath.Abs(start)
	if err != nil {
		return ""
	}
	if info, err := os.Stat(path); err != nil || !info.IsDir() {
		path = filepath.Dir(path)
	}
	for {
		if info, err := os.Stat(filepath.Join(path, "scripts", "fleet", "operator_env.py")); err == nil && !info.IsDir() {
			return path
		}
		parent := filepath.Dir(path)
		if parent == path {
			return ""
		}
		path = parent
	}
}

func Check(ctx context.Context, root string) Result {
	result := Result{Root: root, CheckedAt: time.Now()}
	if root == "" {
		result.Error = "чекаут fixer-mcp не найден"
		return result
	}
	script := filepath.Join(root, "scripts", "fleet", "operator_env.py")
	if _, err := os.Stat(script); err != nil {
		result.Error = fmt.Sprintf("нет %s", script)
		return result
	}
	cmd := exec.CommandContext(ctx, "python3", script, "check", "--json")
	cmd.Dir = root
	output, err := cmd.CombinedOutput()
	result.Output = strings.TrimSpace(string(output))
	if err != nil {
		result.Error = err.Error()
		return result
	}
	var payload struct {
		OK      bool `json:"ok"`
		Summary struct {
			Blocking int `json:"blocking"`
		} `json:"summary"`
	}
	if err := json.Unmarshal(output, &payload); err == nil {
		result.OK = payload.OK
		result.Summary = fmt.Sprintf("blocking: %d", payload.Summary.Blocking)
	} else {
		result.OK = true
		result.Summary = "check завершён"
	}
	return result
}

func JSON(result Result) ([]byte, error) { return json.MarshalIndent(result, "", "  ") }
