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
	KindHands    = "hands"
	KindWorkroom = "workroom"
	KindTerminal = "terminal"
	KindSSH      = "ssh"
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
	// SSHOptions are route-specific options selected by machines.Resolve.
	// They keep Tailscale/SOCKS/LAN fallback out of the visible launch form.
	SSHOptions []string
	Prompt     string
	Resume     string
	Arguments  []string

	// Work modes (KindHands / KindWorkroom) are chosen in the native console,
	// never by the legacy line selectors. Build() only emits a fully preset,
	// non-interactive command; a work mode missing its presets is rejected
	// instead of silently falling back into fixer_wire.py prompts.
	// Lane is the Project Hands execution lane (defaults to the Pi lane).
	Lane string
	// Workspace is the Hands worktree mode: "safe" or "hotfix".
	Workspace string
	// HandsMCP / HandsDocs are Hands selections in keep|none|all|<list> form.
	HandsMCP  string
	HandsDocs string
	// FixerLaunch is the Fixer launch action: "new", "resume" or "unattached".
	FixerLaunch string
	// FixerSession names the session FixerLaunch=resume resumes.
	FixerSession string
}

type Command struct {
	Binary string
	Args   []string
	Dir    string
	Env    []string
}

var Providers = []string{"pi", "agy", "commandcode", "opencode", "droid", "grok", "kimi"}

var Models = map[string][]string{
	"pi":          {"openai-codex/gpt-5.6-luna", "openai-codex/gpt-5.6-terra", "opencode-go/deepseek-v4.1-flash", "opencode-personal/mimo-v2.6-flash", "opencode-personal/mimo-v2.6-pro", "commandcode/xiaomi/mimo-v2.6-flash", "commandcode/xiaomi/mimo-v2.6-pro", "opencode-stas/claude-opus-5-5"},
	"agy":         {"Gemini 3.8 Flash", "Gemini 3.7 Flash", "Gemini 3.1 Pro"},
	"commandcode": {"commandcode/xiaomi/mimo-v2.5-pro", "commandcode/xiaomi/mimo-v2.6-flash", "commandcode/xiaomi/mimo-v2.6-pro", "deepseek/deepseek-v4-flash", "google/gemini-3.7-flash", "gpt-5.6-luna"},
	"opencode":    {"opencode-go/deepseek-v4.1-flash"},
	"droid":       {"GLM-5.1", "gpt-5.6-luna"},
	"grok":        {"grok-4.6"},
	"kimi":        {"kimi-k3"},
}

var Thinkings = []string{"high", "medium", "low", "off"}

func DefaultDraft(projectPath string) domain.LaunchDraft {
	return domain.LaunchDraft{
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

func PresetDrafts(projectPath string) []domain.LaunchDraft {
	base := DefaultDraft(projectPath)
	return []domain.LaunchDraft{
		base,
		{ID: "fast-agent", Title: "Быстрая работа", Kind: KindAgent, Provider: "agy", Account: "default", Model: "Gemini 3.8 Flash", Thinking: "high", ProjectID: projectID(projectPath), Permissions: "yolo", MCPMode: "project"},
		{ID: "complex-agent", Title: "Сложная задача", Kind: KindAgent, Provider: "pi", Account: "OpenCode Go", Model: "opencode-go/deepseek-v4.1-flash", Thinking: "high", ProjectID: projectID(projectPath), Permissions: "approve", MCPMode: "project"},
		{ID: "project-hands", Title: "Руки", Kind: KindHands, ProjectID: projectID(projectPath), Permissions: "governed", MCPMode: "project"},
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
	case KindHands, KindWorkroom:
		wire := filepath.Join(runtimeRoot, "client_wires", "fixer_wire.py")
		if runtimeRoot == "" {
			wire = filepath.Join("client_wires", "fixer_wire.py")
		}
		if _, err := os.Stat(wire); err != nil {
			return Command{}, fmt.Errorf("Fixer work client is unavailable: %s", wire)
		}
		baseArgs, err := workModeArgs(spec, wire)
		if err != nil {
			return Command{}, err
		}
		cmd.Binary = pythonBinary()
		cmd.Args = append(baseArgs, spec.Arguments...)
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

// workModeArgs renders the fully preset, non-interactive invocation for a
// work mode. It deliberately refuses to emit a bare `--role` command: that is
// exactly the shape that dropped the operator into the legacy line selectors
// (`Project Hands workspace mode`, arrow keys leaking as `^[OB`).
func workModeArgs(spec Spec, wire string) ([]string, error) {
	switch spec.Kind {
	case KindHands:
		workspace := strings.ToLower(strings.TrimSpace(spec.Workspace))
		if workspace != "safe" && workspace != "hotfix" {
			return nil, errors.New("Руки: не выбран режим worktree (safe/hotfix)")
		}
		lane := strings.TrimSpace(spec.Lane)
		if lane == "" {
			// No guessed default: the launcher validates the lane against the
			// registered set, and a wrong guess fails deep inside the wire.
			return nil, errors.New("Руки: не выбрана зарегистрированная линия исполнителя")
		}
		mcp := strings.TrimSpace(spec.HandsMCP)
		if mcp == "" {
			mcp = "keep"
		}
		docs := strings.TrimSpace(spec.HandsDocs)
		if docs == "" {
			docs = "keep"
		}
		return []string{
			wire, "--role", "netrunner",
			"--netrunner-backend", lane,
			"--hands-workspace", workspace,
			"--hands-mcp", mcp,
			"--hands-docs", docs,
		}, nil
	case KindWorkroom:
		switch strings.ToLower(strings.TrimSpace(spec.FixerLaunch)) {
		case "new", "unattached":
			action := strings.ToLower(strings.TrimSpace(spec.FixerLaunch))
			return []string{wire, "--role", "fixer", "--fixer-launch", action}, nil
		case "resume":
			session := strings.TrimSpace(spec.FixerSession)
			if session == "" {
				return nil, errors.New("Фиксер: для resume нужен id сессии")
			}
			return []string{wire, "--role", "fixer", "--fixer-launch", "resume", "--fixer-session-id", session}, nil
		default:
			return nil, errors.New("Фиксер: не выбрано действие (new/resume/unattached)")
		}
	default:
		return nil, fmt.Errorf("unknown work mode %q", spec.Kind)
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
	sshArgs := append([]string{"-tt"}, spec.SSHOptions...)
	sshArgs = append(sshArgs, spec.Host, strings.Join(parts, " "))
	return Command{
		Binary: "ssh",
		Args:   sshArgs,
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
