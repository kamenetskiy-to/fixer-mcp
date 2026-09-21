package launch

import (
	"errors"
	"fmt"
	"os"
	"os/exec"
	"path/filepath"
	"runtime"
	"strings"

	"github.com/fixer-mcp/control-plane/internal/domain"
	"github.com/fixer-mcp/control-plane/internal/vpnenv"
)

const (
	KindAgent    = "agent"
	KindWorkroom = "workroom"
	KindTerminal = "terminal"
	KindSSH      = "ssh"
	KindFleet    = "fleet"
)

type Spec struct {
	Title       string
	Kind        string
	Provider    string
	Account     string
	Model       string
	Thinking    string
	ProjectPath string
	Host        string
	RemotePath  string
	Prompt      string
	Resume      string
	Arguments   []string
}

type Command struct {
	Binary string
	Args   []string
	Dir    string
	Env    []string
}

var Providers = []string{"pi", "agy", "commandcode", "opencode", "droid", "grok", "kimi"}

var Models = map[string][]string{
	"pi":          {"openai-codex/gpt-5.6-luna", "openai-codex/gpt-5.6-terra", "opencode-go/deepseek-v4.1-flash"},
	"agy":         {"Gemini 3.8 Flash", "Gemini 3.7 Flash", "Gemini 3.1 Pro"},
	"commandcode": {"deepseek/deepseek-v4-flash", "google/gemini-3.7-flash", "gpt-5.6-luna"},
	"opencode":    {"opencode-go/deepseek-v4.1-flash"},
	"droid":       {"GLM-5.1", "gpt-5.6-luna"},
	"grok":        {"grok-4.6"},
	"kimi":        {"kimi-k3"},
}

var Thinkings = []string{"high", "medium", "low", "off"}

func DefaultProfile(projectPath string) domain.LaunchProfile {
	return domain.LaunchProfile{
		ID:          "default-agent",
		Title:       "Обычная работа",
		Kind:        KindAgent,
		Provider:    "pi",
		Account:     "Personal",
		Model:       "openai-codex/gpt-5.6-luna",
		Thinking:    "high",
		ProjectID:   projectID(projectPath),
		Permissions: "approve",
		MCPMode:     "project",
	}
}

func PresetProfiles(projectPath string) []domain.LaunchProfile {
	base := DefaultProfile(projectPath)
	return []domain.LaunchProfile{
		base,
		{ID: "fast-agent", Title: "Быстрая работа", Kind: KindAgent, Provider: "agy", Account: "default", Model: "Gemini 3.8 Flash", Thinking: "high", ProjectID: projectID(projectPath), Permissions: "yolo", MCPMode: "project"},
		{ID: "complex-agent", Title: "Сложная задача", Kind: KindAgent, Provider: "pi", Account: "OpenCode Go", Model: "opencode-go/deepseek-v4.1-flash", Thinking: "high", ProjectID: projectID(projectPath), Permissions: "approve", MCPMode: "project"},
		{ID: "fixer-workroom", Title: "Fixer Workroom", Kind: KindWorkroom, ProjectID: projectID(projectPath), Permissions: "governed", MCPMode: "project"},
		{ID: "local-shell", Title: "Локальный терминал", Kind: KindTerminal, ProjectID: projectID(projectPath)},
	}
}

func ProjectID(path string) string { return projectID(path) }

func projectID(path string) string {
	clean := filepath.Clean(path)
	if clean == "." || clean == "" {
		clean = "cwd"
	}
	return strings.NewReplacer("/", "_", "\\", "_", " ", "_").Replace(clean)
}

func Build(spec Spec, runtimeRoot string) (Command, error) {
	if spec.ProjectPath == "" {
		spec.ProjectPath = "."
	}
	env := append([]string(nil), os.Environ()...)
	marker := vpnenv.Inspect(vpnenv.EnvMap(env))
	env = vpnenv.Env(env, marker.Values)
	cmd := Command{Dir: spec.ProjectPath, Env: env}

	switch spec.Kind {
	case KindWorkroom:
		wire := filepath.Join(runtimeRoot, "client_wires", "fixer_wire.py")
		if runtimeRoot == "" {
			wire = filepath.Join("client_wires", "fixer_wire.py")
		}
		if _, err := os.Stat(wire); err != nil {
			return Command{}, fmt.Errorf("Fixer Workroom is unavailable: %s", wire)
		}
		cmd.Binary = pythonBinary()
		cmd.Args = append([]string{wire}, spec.Arguments...)
		return cmd, nil
	case KindTerminal:
		cmd.Binary = shellBinary()
		if runtime.GOOS == "windows" {
			cmd.Args = []string{"/C"}
		} else {
			cmd.Args = []string{"-l"}
		}
		return wrapRemote(spec, cmd)
	case KindSSH:
		if spec.Host == "" {
			return Command{}, errors.New("SSH target is empty")
		}
		cmd.Binary = "ssh"
		cmd.Args = append([]string{"-tt", spec.Host}, spec.Arguments...)
		return cmd, nil
	case KindFleet:
		if runtimeRoot == "" {
			return Command{}, errors.New("fleet check requires a checkout root")
		}
		script := filepath.Join(runtimeRoot, "scripts", "fleet", "operator_env.py")
		if _, err := os.Stat(script); err != nil {
			return Command{}, fmt.Errorf("fleet checker is unavailable: %s", script)
		}
		cmd.Binary = pythonBinary()
		cmd.Args = append([]string{script, "check"}, spec.Arguments...)
		return cmd, nil
	case KindAgent:
		built, err := buildAgent(spec, cmd)
		if err != nil {
			return Command{}, err
		}
		return wrapRemote(spec, built)
	default:
		return Command{}, fmt.Errorf("unknown launch kind %q", spec.Kind)
	}
}

func buildAgent(spec Spec, cmd Command) (Command, error) {
	if spec.Provider == "" {
		return Command{}, errors.New("agent provider is empty")
	}
	switch spec.Provider {
	case "pi":
		cmd.Binary = "pi"
		if spec.Model != "" {
			provider, model := splitModel(spec.Model)
			cmd.Args = append(cmd.Args, "--provider", provider, "--model", model)
		}
		if spec.Thinking != "" && spec.Thinking != "off" {
			cmd.Args = append(cmd.Args, "--thinking", spec.Thinking)
		}
		cmd.Args = append(cmd.Args, "--approve")
		if spec.Resume != "" {
			cmd.Args = append(cmd.Args, "--session", spec.Resume)
		}
	case "agy":
		cmd.Binary = "agy"
		cmd.Args = append(cmd.Args, "--dangerously-skip-permissions")
		if spec.Model != "" {
			cmd.Args = append(cmd.Args, "--model", spec.Model)
		}
		if spec.Thinking != "" && spec.Thinking != "off" {
			cmd.Args = append(cmd.Args, "--effort", spec.Thinking)
		}
		if spec.Resume != "" {
			cmd.Args = append(cmd.Args, "--conversation", spec.Resume)
		}
	case "commandcode":
		cmd.Binary = firstAvailable("cmd", "cmdc", "commandcode")
		cmd.Args = append(cmd.Args, "--model", spec.Model, "--effort", spec.Thinking, "--yolo", "--trust", "--skip-onboarding", "--no-auto-update")
	case "opencode":
		cmd.Binary = "opencode"
		if spec.Model != "" {
			cmd.Args = append(cmd.Args, "--model", spec.Model)
		}
		cmd.Args = append(cmd.Args, "--auto")
	case "droid":
		cmd.Binary = "droid"
		if spec.Resume != "" {
			cmd.Args = append(cmd.Args, "--resume", spec.Resume)
		}
	case "grok":
		cmd.Binary = "grok"
		if spec.Model != "" {
			cmd.Args = append(cmd.Args, "--model", spec.Model)
		}
		cmd.Args = append(cmd.Args, "--always-approve")
	case "kimi":
		cmd.Binary = "kimi"
		if spec.Model != "" {
			cmd.Args = append(cmd.Args, "--model", spec.Model)
		}
		cmd.Args = append(cmd.Args, "--yolo")
	default:
		return Command{}, fmt.Errorf("unsupported agent provider %q", spec.Provider)
	}
	if spec.Prompt != "" {
		cmd.Args = append(cmd.Args, spec.Prompt)
	}
	cmd.Args = append(cmd.Args, spec.Arguments...)
	if _, err := exec.LookPath(cmd.Binary); err != nil {
		return Command{}, fmt.Errorf("provider %s is not installed", spec.Provider)
	}
	return cmd, nil
}

func wrapRemote(spec Spec, cmd Command) (Command, error) {
	if strings.TrimSpace(spec.Host) == "" || strings.EqualFold(strings.TrimSpace(spec.Host), "local") {
		return cmd, nil
	}
	if strings.TrimSpace(spec.RemotePath) == "" && strings.TrimSpace(spec.ProjectPath) == "" {
		return Command{}, errors.New("remote launch requires a project path")
	}
	remotePath := spec.RemotePath
	if remotePath == "" {
		remotePath = spec.ProjectPath
	}
	parts := []string{"cd --", shellQuote(remotePath), "&& exec", shellQuote(filepath.Base(cmd.Binary))}
	for _, arg := range cmd.Args {
		parts = append(parts, shellQuote(arg))
	}
	return Command{
		Binary: "ssh",
		Args:   []string{"-tt", spec.Host, strings.Join(parts, " ")},
		Env:    cmd.Env,
	}, nil
}

func shellQuote(value string) string {
	return "'" + strings.ReplaceAll(value, "'", "'\"'\"'") + "'"
}

func splitModel(model string) (string, string) {
	parts := strings.SplitN(model, "/", 2)
	if len(parts) == 2 {
		return parts[0], parts[1]
	}
	return "openai-codex", model
}

func firstAvailable(names ...string) string {
	for _, name := range names {
		if path, err := exec.LookPath(name); err == nil {
			return path
		}
	}
	return names[0]
}

func pythonBinary() string {
	if path, err := exec.LookPath("python3"); err == nil {
		return path
	}
	return "python3"
}

func shellBinary() string {
	if path, err := exec.LookPath("zsh"); err == nil {
		return path
	}
	if path, err := exec.LookPath("bash"); err == nil {
		return path
	}
	return "sh"
}
