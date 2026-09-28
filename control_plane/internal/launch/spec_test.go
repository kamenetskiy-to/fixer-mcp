package launch

import (
	"os"
	"strings"
	"testing"
)

func TestBuildPiUsesUnifiedProviderSpecAndVPNMarker(t *testing.T) {
	marker := t.TempDir() + "/vpn.env"
	if err := os.WriteFile(marker, []byte("HTTPS_PROXY=http://127.0.0.1:8888\n"), 0o600); err != nil {
		t.Fatal(err)
	}
	t.Setenv("FLEET_VPN_ENV_FILE", marker)
	command, err := Build(Spec{
		Kind:        KindAgent,
		Provider:    "pi",
		Model:       "openai-codex/gpt-5.6-luna",
		Thinking:    "high",
		ProjectPath: t.TempDir(),
		Prompt:      "continue the work",
	}, "")
	if err != nil {
		t.Fatal(err)
	}
	if command.Binary != "pi" {
		t.Fatalf("binary = %q", command.Binary)
	}
	joined := strings.Join(command.Args, " ")
	for _, want := range []string{"--provider openai-codex", "--model gpt-5.6-luna", "--thinking high", "--approve", "continue the work"} {
		if !strings.Contains(joined, want) {
			t.Fatalf("args %q missing %q", joined, want)
		}
	}
	foundProxy := false
	for _, value := range command.Env {
		if value == "HTTPS_PROXY=http://127.0.0.1:8888" {
			foundProxy = true
		}
	}
	if !foundProxy {
		t.Fatalf("VPN marker was not merged into launch env: %v", command.Env)
	}
}

func TestBuildRemoteAgentUsesSavedHostAndPath(t *testing.T) {
	command, err := Build(Spec{
		Kind:        KindAgent,
		Provider:    "pi",
		Model:       "openai-codex/gpt-5.6-luna",
		ProjectPath: "/Users/local/project",
		RemotePath:  "/home/operator/project",
		Host:        "wsl-tinker-tailscale",
	}, "")
	if err != nil {
		t.Fatal(err)
	}
	if command.Binary != "ssh" || len(command.Args) != 3 || command.Args[1] != "wsl-tinker-tailscale" {
		t.Fatalf("remote command = %+v", command)
	}
	if !strings.Contains(command.Args[2], "cd -- '/home/operator/project' && exec 'pi'") {
		t.Fatalf("remote command did not use saved remote path: %q", command.Args[2])
	}
}

func workModeRuntimeRoot(t *testing.T) string {
	t.Helper()
	runtimeRoot := t.TempDir()
	wireDir := runtimeRoot + "/client_wires"
	if err := os.MkdirAll(wireDir, 0o755); err != nil {
		t.Fatal(err)
	}
	if err := os.WriteFile(wireDir+"/fixer_wire.py", []byte("# test"), 0o600); err != nil {
		t.Fatal(err)
	}
	return runtimeRoot
}

// A work mode without its native presets must be rejected, never converted
// into a bare `--role` command: that command shape is what used to drop the
// operator into the legacy line selectors.
func TestBuildWorkModesRejectMissingPresets(t *testing.T) {
	runtimeRoot := workModeRuntimeRoot(t)
	for _, test := range []struct {
		name string
		spec Spec
	}{
		{"hands without workspace", Spec{Kind: KindHands, ProjectPath: t.TempDir()}},
		{"hands without a registered lane", Spec{Kind: KindHands, ProjectPath: t.TempDir(), Workspace: "safe"}},
		{"workroom without action", Spec{Kind: KindWorkroom, ProjectPath: t.TempDir()}},
		{"workroom resume without session", Spec{Kind: KindWorkroom, ProjectPath: t.TempDir(), FixerLaunch: "resume"}},
	} {
		if _, err := Build(test.spec, runtimeRoot); err == nil {
			t.Fatalf("%s: Build must not fall back to a legacy selector", test.name)
		}
	}
}

func TestBuildWorkModesAreExplicitRoles(t *testing.T) {
	runtimeRoot := workModeRuntimeRoot(t)
	for _, test := range []struct {
		name string
		spec Spec
		want []string
	}{
		{
			name: "hands hotfix on the project's registered lane",
			spec: Spec{Kind: KindHands, ProjectPath: t.TempDir(), Workspace: "hotfix", Lane: "codex"},
			want: []string{
				"--role netrunner", "--netrunner-backend codex",
				"--hands-workspace hotfix", "--hands-mcp keep", "--hands-docs keep",
			},
		},
		{
			name: "hands safe with an explicit lane",
			spec: Spec{Kind: KindHands, ProjectPath: t.TempDir(), Workspace: "safe", Lane: "antigravity"},
			want: []string{"--role netrunner", "--netrunner-backend antigravity", "--hands-workspace safe"},
		},
		{
			name: "fixer new",
			spec: Spec{Kind: KindWorkroom, ProjectPath: t.TempDir(), FixerLaunch: "new"},
			want: []string{"--role fixer", "--fixer-launch new"},
		},
		{
			name: "fixer unattached",
			spec: Spec{Kind: KindWorkroom, ProjectPath: t.TempDir(), FixerLaunch: "unattached"},
			want: []string{"--role fixer", "--fixer-launch unattached"},
		},
		{
			name: "fixer resume names its session",
			spec: Spec{Kind: KindWorkroom, ProjectPath: t.TempDir(), FixerLaunch: "resume", FixerSession: "codex:673"},
			want: []string{"--role fixer", "--fixer-launch resume", "--fixer-session-id codex:673"},
		},
	} {
		command, err := Build(test.spec, runtimeRoot)
		if err != nil {
			t.Fatalf("%s: %v", test.name, err)
		}
		joined := strings.Join(command.Args, " ")
		for _, want := range test.want {
			if !strings.Contains(joined, want) {
				t.Fatalf("%s command %q missing %q", test.name, joined, want)
			}
		}
	}
}

func TestCommandCodeOffersMiMoModels(t *testing.T) {
	models := Models["commandcode"]
	found := false
	for _, model := range models {
		if strings.Contains(model, "mimo") {
			found = true
		}
	}
	if !found {
		t.Fatalf("mimo models must stay selectable for commandcode: %v", models)
	}
}

func TestBuildRemoteAgentIncludesResolvedTransportOptions(t *testing.T) {
	command, err := Build(Spec{
		Kind:        KindAgent,
		Provider:    "pi",
		Model:       "openai-codex/gpt-5.6-luna",
		ProjectPath: "/Users/local/project",
		RemotePath:  "/home/operator/project",
		Host:        "operator@100.97.178.61",
		SSHOptions:  []string{"-o", "ProxyCommand=nc -x 127.0.0.1:1055 -X 5 %h %p", "-p", "22"},
	}, "")
	if err != nil {
		t.Fatal(err)
	}
	joined := strings.Join(command.Args, " ")
	if !strings.Contains(joined, "ProxyCommand=nc -x 127.0.0.1:1055") || !strings.Contains(joined, "operator@100.97.178.61") {
		t.Fatalf("resolved route was not preserved: %q", joined)
	}
}

func TestBuildRejectsUnknownProvider(t *testing.T) {
	_, err := Build(Spec{Kind: KindAgent, Provider: "unknown", ProjectPath: t.TempDir()}, "")
	if err == nil || !strings.Contains(err.Error(), "unsupported agent provider") {
		t.Fatalf("unexpected error: %v", err)
	}
}
