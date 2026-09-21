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

func TestBuildRejectsUnknownProvider(t *testing.T) {
	_, err := Build(Spec{Kind: KindAgent, Provider: "unknown", ProjectPath: t.TempDir()}, "")
	if err == nil || !strings.Contains(err.Error(), "unsupported agent provider") {
		t.Fatalf("unexpected error: %v", err)
	}
}
