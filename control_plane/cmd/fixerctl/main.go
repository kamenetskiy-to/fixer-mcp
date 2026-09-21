// Command fixer is the unified Fixer operator console. fixerctl remains a
// compatibility name for the same binary during the migration from the old
// alias palette.
package main

import (
	"context"
	"encoding/json"
	"errors"
	"flag"
	"fmt"
	"os"
	"os/exec"
	"path/filepath"
	"runtime"
	"strings"
	"time"

	"github.com/fixer-mcp/control-plane/internal/config"
	"github.com/fixer-mcp/control-plane/internal/console"
	"github.com/fixer-mcp/control-plane/internal/fleet"
	"github.com/fixer-mcp/control-plane/internal/machines"
	"github.com/fixer-mcp/control-plane/internal/network"
	"github.com/fixer-mcp/control-plane/internal/registry"
	"github.com/fixer-mcp/control-plane/internal/resources"
	"github.com/fixer-mcp/control-plane/internal/runner"
	"github.com/fixer-mcp/control-plane/internal/vpnenv"
)

var version = "0.2.0"

const repoScript = "scripts/fleet/operator_env.py"

func main() {
	code, err := run(os.Args[1:])
	if err != nil {
		fmt.Fprintln(os.Stderr, "fixer:", err)
		if code == 0 {
			code = 1
		}
	}
	os.Exit(code)
}

func run(args []string) (int, error) {
	flags := flag.NewFlagSet("fixer", flag.ContinueOnError)
	flags.SetOutput(os.Stderr)
	var (
		configPath = flags.String("config", "", "config file (compatibility diagnostics)")
		repoRoot   = flags.String("repo", "", "fixer-mcp checkout root")
		printMode  = flags.Bool("print", false, "print compatibility command inventory and exit")
		jsonMode   = flags.Bool("json", false, "emit JSON for -print or quota")
		runID      = flags.String("run", "", "run one compatibility entry by id")
		showVer    = flags.Bool("version", false, "print Fixer version")
	)
	flags.Usage = func() {
		fmt.Fprintln(flags.Output(), "usage: fixer [command] [flags]")
		fmt.Fprintln(flags.Output(), "\nCommands:")
		fmt.Fprintln(flags.Output(), "  console              open the unified operator console (default)")
		fmt.Fprintln(flags.Output(), "  workroom             open the governed Fixer Workroom")
		fmt.Fprintln(flags.Output(), "  quota [--json]       inspect provider clients and cached quotas")
		fmt.Fprintln(flags.Output(), "  machines|network|fleet  open the corresponding console workspace")
		fmt.Fprintln(flags.Output(), "  doctor|update        use the managed installer service command")
		fmt.Fprintln(flags.Output(), "\nKeys in the console: n new work · Enter open · 1..5 workspace · / search · ? help")
		flags.PrintDefaults()
	}
	if err := flags.Parse(args); err != nil {
		if errors.Is(err, flag.ErrHelp) {
			return 0, nil
		}
		return 2, err
	}
	if *showVer {
		fmt.Printf("%s v%s (%s/%s)\n", executableName(), version, runtime.GOOS, runtime.GOARCH)
		return 0, nil
	}
	remaining := flags.Args()
	filtered := remaining[:0]
	for _, item := range remaining {
		if item == "--json" || item == "-json" {
			*jsonMode = true
			continue
		}
		filtered = append(filtered, item)
	}
	remaining = filtered
	if *printMode {
		entries, err := compatibilityEntries(*repoRoot, *configPath)
		if err != nil {
			return 1, err
		}
		return dump(os.Stdout, *jsonMode, *configPath, vpnenv.Inspect(vpnenv.EnvMap(os.Environ())), entries)
	}
	if *runID != "" {
		entries, err := compatibilityEntries(*repoRoot, *configPath)
		if err != nil {
			return 1, err
		}
		return runCompatibilityEntry(*runID, entries)
	}
	command := "console"
	if len(remaining) > 0 {
		command = remaining[0]
	}
	switch command {
	case "console", "fixer", "":
		return runConsole(0)
	case "open":
		if len(remaining) < 2 {
			return 2, errors.New("usage: fixer open PROJECT_PATH")
		}
		return runConsoleAt(0, remaining[1])
	case "resources", "providers", "quota":
		if command == "quota" || *jsonMode {
			snapshot := resources.RefreshQuota(nilContext())
			if *jsonMode {
				payload, err := resources.JSON(snapshot)
				if err != nil {
					return 1, err
				}
				fmt.Println(string(payload))
			} else {
				printQuota(snapshot)
			}
			return 0, nil
		}
		return runConsole(1)
	case "machines":
		if *jsonMode {
			ctx, cancel := context.WithTimeout(context.Background(), 20*time.Second)
			defer cancel()
			snapshot := machines.Probe(ctx, machines.Discover())
			return printJSON(snapshot)
		}
		return runConsole(2)
	case "network", "myip", "vpn-status":
		ctx, cancel := context.WithTimeout(context.Background(), 8*time.Second)
		defer cancel()
		status := network.Inspect(ctx)
		if command == "myip" && !*jsonMode {
			fmt.Println(status.Egress)
			return 0, nil
		}
		if *jsonMode || command != "network" {
			return printJSON(status)
		}
		return runConsole(3)
	case "vpn-up", "vpn-down":
		action := "up"
		if command == "vpn-down" {
			action = "down"
		}
		ctx, cancel := context.WithTimeout(context.Background(), 45*time.Second)
		defer cancel()
		text, err := network.VPNAction(ctx, action, "us")
		if text != "" {
			fmt.Println(text)
		}
		return exitCode(err), err
	case "fleet":
		if len(remaining) > 1 && remaining[1] == "env" {
			fleetArgs := append([]string(nil), remaining[2:]...)
			if *jsonMode {
				fleetArgs = append(fleetArgs, "--json")
			}
			return runFleetEnv(fleetArgs)
		}
		if *jsonMode {
			root := *repoRoot
			if root == "" {
				root = fleet.FindRoot(currentDirectory())
			}
			ctx, cancel := context.WithTimeout(context.Background(), 45*time.Second)
			defer cancel()
			return printJSON(fleet.Check(ctx, root))
		}
		return runConsole(4)
	case "workroom":
		return runWorkroom(remaining[1:])
	case "doctor", "update":
		return runInstallerCommand(command, remaining[1:])
	case "help":
		flags.Usage()
		return 0, nil
	default:
		// Preserve the old launcher's useful behavior: unknown arguments are
		// forwarded to the Workroom rather than to a shell alias.
		return runWorkroom(remaining)
	}
}

func runConsole(tab int) (int, error) {
	return runConsoleAt(tab, "")
}

func runConsoleAt(tab int, path string) (int, error) {
	root := runtimeRoot()
	return 0, console.Run(console.Options{
		RuntimeRoot: root,
		RepoRoot:    root,
		Version:     version,
		InitialTab:  tab,
		InitialPath: path,
	})
}

func runWorkroom(args []string) (int, error) {
	root := runtimeRoot()
	wire := filepath.Join(root, "client_wires", "fixer_wire.py")
	if _, err := os.Stat(wire); err != nil {
		return 2, fmt.Errorf("Fixer Workroom is unavailable: %s", wire)
	}
	cmd := exec.Command("python3", append([]string{wire}, args...)...)
	cmd.Env = os.Environ()
	cmd.Dir = currentDirectory()
	cmd.Stdin, cmd.Stdout, cmd.Stderr = os.Stdin, os.Stdout, os.Stderr
	if err := cmd.Run(); err != nil {
		return exitCode(err), err
	}
	return 0, nil
}

func runFleetEnv(args []string) (int, error) {
	root := runtimeRoot()
	script := filepath.Join(root, "scripts", "fleet", "operator_env.py")
	if _, err := os.Stat(script); err != nil {
		return 2, fmt.Errorf("fleet environment service is unavailable: %s", script)
	}
	if len(args) == 0 {
		args = []string{"check"}
	}
	cmd := exec.Command("python3", append([]string{script}, args...)...)
	cmd.Env = os.Environ()
	cmd.Stdin, cmd.Stdout, cmd.Stderr = os.Stdin, os.Stdout, os.Stderr
	if err := cmd.Run(); err != nil {
		return exitCode(err), err
	}
	return 0, nil
}

func runInstallerCommand(command string, args []string) (int, error) {
	root := runtimeRoot()
	entrypoint := filepath.Join(root, "installer", "cli.py")
	if _, err := os.Stat(entrypoint); err != nil {
		return 2, fmt.Errorf("managed installer is unavailable: %s", entrypoint)
	}
	cmd := exec.Command("python3", append([]string{entrypoint, command}, args...)...)
	cmd.Env = os.Environ()
	cmd.Stdin, cmd.Stdout, cmd.Stderr = os.Stdin, os.Stdout, os.Stderr
	if err := cmd.Run(); err != nil {
		return exitCode(err), err
	}
	return 0, nil
}

func runtimeRoot() string {
	if root := strings.TrimSpace(os.Getenv("FIXER_RUNTIME_ROOT")); root != "" {
		return root
	}
	cwd := currentDirectory()
	if root := registry.FindRepoRoot(cwd, "client_wires/fixer_wire.py", os.Stat); root != "" {
		return root
	}
	if executable, err := os.Executable(); err == nil {
		candidate := filepath.Clean(filepath.Join(filepath.Dir(executable), ".."))
		if _, err := os.Stat(filepath.Join(candidate, "client_wires", "fixer_wire.py")); err == nil {
			return candidate
		}
	}
	return cwd
}

func executableName() string {
	name := filepath.Base(os.Args[0])
	if strings.Contains(name, "fixerctl") {
		return "fixerctl"
	}
	return "fixer"
}

func currentDirectory() string {
	cwd, err := os.Getwd()
	if err != nil {
		return "."
	}
	return cwd
}

func nilContext() context.Context { return context.Background() }

func printJSON(value any) (int, error) {
	payload, err := json.MarshalIndent(value, "", "  ")
	if err != nil {
		return 1, err
	}
	fmt.Println(string(payload))
	return 0, nil
}

func printQuota(snapshot resources.Snapshot) {
	fmt.Printf("fixer quota (%s)\n", resources.PlatformLabel())
	for _, provider := range snapshot.Providers {
		status := "missing"
		if provider.Available {
			status = "ready"
		}
		fmt.Printf("%-18s %-8s %s\n", provider.Name, status, provider.Detail)
	}
	for _, quota := range snapshot.Quotas {
		fmt.Printf("%-18s %-12s %s\n", quota.Provider, quota.Status, quota.Windows)
	}
	if snapshot.Error != "" {
		fmt.Fprintln(os.Stderr, snapshot.Error)
	}
}

func compatibilityEntries(root, configPath string) ([]registry.Resolved, error) {
	env := vpnenv.EnvMap(os.Environ())
	if configPath == "" {
		configPath = config.DefaultPath(env)
	}
	cfg, err := config.Load(configPath)
	if err != nil {
		return nil, err
	}
	if root == "" {
		root = cfg.RepoRoot
	}
	if root == "" {
		root = registry.FindRepoRoot(currentDirectory(), repoScript, os.Stat)
	}
	entries := registry.Build(cfg, registry.Options{
		GOOS: runtime.GOOS, Home: env["HOME"], Path: env["PATH"], RepoRoot: root,
	})
	return entries, nil
}

func runCompatibilityEntry(id string, entries []registry.Resolved) (int, error) {
	for _, entry := range entries {
		if entry.ID != id {
			continue
		}
		if !entry.Enabled() {
			return 2, fmt.Errorf("entry %q is disabled: %s", id, entry.Disabled)
		}
		command := runner.NewExecCommand(entry, runner.Options{Env: os.Environ(), Dir: currentDirectory()})
		command.SetStdin(os.Stdin)
		command.SetStdout(os.Stdout)
		command.SetStderr(os.Stderr)
		if err := command.Run(); err != nil {
			return exitCode(err), err
		}
		return 0, nil
	}
	return 2, fmt.Errorf("unknown compatibility entry %q", id)
}

type dumpEntry struct {
	ID          string   `json:"id"`
	Title       string   `json:"title"`
	Help        string   `json:"help,omitempty"`
	Binary      string   `json:"binary,omitempty"`
	Args        []string `json:"args,omitempty"`
	Lane        string   `json:"lane,omitempty"`
	Disabled    string   `json:"disabled,omitempty"`
	Enabled     bool     `json:"enabled"`
	CommandLine string   `json:"command_line,omitempty"`
}

type dumpDoc struct {
	Platform   string      `json:"platform"`
	ConfigPath string      `json:"config_path"`
	VPNStatus  string      `json:"vpn_status"`
	VPNPath    string      `json:"vpn_path"`
	Entries    []dumpEntry `json:"entries"`
}

func dump(out *os.File, asJSON bool, configPath string, marker vpnenv.Status, entries []registry.Resolved) (int, error) {
	doc := dumpDoc{Platform: runtime.GOOS + "/" + runtime.GOARCH, ConfigPath: configPath, VPNStatus: marker.Summary(), VPNPath: marker.Path}
	for _, entry := range entries {
		doc.Entries = append(doc.Entries, dumpEntry{ID: entry.ID, Title: entry.Display(), Help: entry.Help, Binary: entry.Binary, Args: entry.Args, Lane: entry.Lane, Disabled: entry.Disabled, Enabled: entry.Enabled(), CommandLine: entry.CommandLine()})
	}
	if asJSON {
		encoder := json.NewEncoder(out)
		encoder.SetIndent("", "  ")
		return 0, encoder.Encode(doc)
	}
	fmt.Fprintf(out, "%s v%s (%s)\n%s\n\n", executableName(), version, doc.Platform, marker.Summary())
	for _, entry := range doc.Entries {
		status, detail := "enabled", entry.CommandLine
		if !entry.Enabled {
			status, detail = "disabled", entry.Disabled
		}
		fmt.Fprintf(out, "%-24s %-8s %s\n", entry.ID, status, detail)
	}
	return 0, nil
}

func exitCode(err error) int {
	var exitErr *exec.ExitError
	if errors.As(err, &exitErr) && exitErr.ExitCode() > 0 {
		return exitErr.ExitCode()
	}
	return 1
}
