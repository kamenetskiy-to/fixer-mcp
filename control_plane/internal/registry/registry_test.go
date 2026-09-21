package registry

import (
	"errors"
	"os"
	"path/filepath"
	"strings"
	"testing"
	"time"

	"github.com/fixer-mcp/control-plane/internal/config"
)

// fakeFileInfo implements os.FileInfo for hermetic resolver tests.
type fakeFileInfo struct {
	name string
	mode os.FileMode
	dir  bool
}

func (f fakeFileInfo) Name() string       { return f.name }
func (f fakeFileInfo) Size() int64        { return 0 }
func (f fakeFileInfo) Mode() os.FileMode  { return f.mode }
func (f fakeFileInfo) ModTime() time.Time { return time.Time{} }
func (f fakeFileInfo) IsDir() bool        { return f.dir }
func (f fakeFileInfo) Sys() any           { return nil }

// fakeHost is a tiny in-memory filesystem plus PATH lookup.
type fakeHost struct {
	files    map[string]os.FileMode
	commands map[string]string
}

func newFakeHost() *fakeHost {
	return &fakeHost{files: map[string]os.FileMode{}, commands: map[string]string{}}
}

func (h *fakeHost) addFile(path string, mode os.FileMode) *fakeHost {
	h.files[path] = mode
	return h
}

func (h *fakeHost) addCommand(name, path string) *fakeHost {
	h.commands[name] = path
	h.files[path] = 0o755
	return h
}

func (h *fakeHost) options(goos string) Options {
	return Options{
		GOOS: goos,
		Home: "/home/op",
		Stat: func(name string) (os.FileInfo, error) {
			mode, ok := h.files[name]
			if !ok {
				return nil, errors.New("not found")
			}
			return fakeFileInfo{name: filepath.Base(name), mode: mode}, nil
		},
		LookPath: func(name string) (string, error) {
			if p, ok := h.commands[name]; ok {
				return p, nil
			}
			return "", errors.New("not found in PATH: " + name)
		},
	}
}

func resolvedByID(t *testing.T, list []Resolved, id string) Resolved {
	t.Helper()
	for _, r := range list {
		if r.ID == id {
			return r
		}
	}
	t.Fatalf("entry %q not found in %v", id, ids(list))
	return Resolved{}
}

func ids(list []Resolved) []string {
	out := make([]string, 0, len(list))
	for _, r := range list {
		out = append(out, r.ID)
	}
	return out
}

func TestBaseEntriesCoverTheOperatorMenu(t *testing.T) {
	want := []string{"cml", "myip", "ssh-tui", "ai-pro", "vpn-up", "vpn-down", "codex-switch", "fixer", "fleet-check"}
	got := make([]string, 0, len(BaseEntries()))
	for _, e := range BaseEntries() {
		got = append(got, e.ID)
	}
	if strings.Join(got, ",") != strings.Join(want, ",") {
		t.Fatalf("BaseEntries() IDs = %v, want %v", got, want)
	}
}

func TestResolvePresentMissingAndRepoScriptEntries(t *testing.T) {
	host := newFakeHost().
		addCommand("cml", "/usr/local/bin/cml").
		addCommand("python3", "/usr/bin/python3").
		addCommand("vpn-up", "/usr/local/bin/vpn-up").
		addFile("/repo/scripts/fleet/operator_env.py", 0o644)

	cfg := config.Config{RepoRoot: "/repo"}
	got := Build(cfg, host.options("darwin"))

	cml := resolvedByID(t, got, "cml")
	if !cml.Enabled() || cml.Binary != "/usr/local/bin/cml" {
		t.Fatalf("cml = %+v, want enabled at /usr/local/bin/cml", cml)
	}

	aiPro := resolvedByID(t, got, "ai-pro")
	if aiPro.Enabled() {
		t.Fatalf("ai-pro should be disabled, got %+v", aiPro)
	}
	if aiPro.Disabled != "missing: ai-pro" {
		t.Fatalf("ai-pro reason = %q, want %q", aiPro.Disabled, "missing: ai-pro")
	}

	fleet := resolvedByID(t, got, "fleet-check")
	if !fleet.Enabled() {
		t.Fatalf("fleet-check should be enabled, got %+v", fleet)
	}
	if fleet.Binary != "/usr/bin/python3" {
		t.Fatalf("fleet-check binary = %q", fleet.Binary)
	}
	wantArgs := []string{"/repo/scripts/fleet/operator_env.py", "check"}
	if strings.Join(fleet.Args, " ") != strings.Join(wantArgs, " ") {
		t.Fatalf("fleet-check args = %v, want %v", fleet.Args, wantArgs)
	}
}

func TestResolveMissingRepoScriptDisablesFleetCheck(t *testing.T) {
	host := newFakeHost().addCommand("python3", "/usr/bin/python3")
	got := Build(config.Config{RepoRoot: "/repo"}, host.options("darwin"))
	fleet := resolvedByID(t, got, "fleet-check")
	if fleet.Enabled() {
		t.Fatalf("fleet-check should be disabled without a checkout, got %+v", fleet)
	}
	if fleet.Disabled != "missing: scripts/fleet/operator_env.py" {
		t.Fatalf("fleet-check reason = %q", fleet.Disabled)
	}
}

func TestResolveMissingRepoScriptWithoutRunnerReportsRunner(t *testing.T) {
	host := newFakeHost().addFile("/repo/scripts/fleet/operator_env.py", 0o644)
	got := Build(config.Config{RepoRoot: "/repo"}, host.options("darwin"))
	fleet := resolvedByID(t, got, "fleet-check")
	if fleet.Enabled() {
		t.Fatalf("fleet-check should be disabled without python3, got %+v", fleet)
	}
	if fleet.Disabled != "missing: python3" {
		t.Fatalf("fleet-check reason = %q, want missing: python3", fleet.Disabled)
	}
}

func TestVPNLaneIsPlatformAware(t *testing.T) {
	cases := []struct {
		goos      string
		wantLane  string
		wantDisab string
	}{
		{goos: "darwin", wantLane: "launchd"},
		{goos: "linux", wantLane: "ssh-tunnel"},
		{goos: "windows", wantDisab: "unsupported platform: windows (lanes: darwin=launchd, linux=ssh-tunnel)"},
	}
	for _, tc := range cases {
		t.Run(tc.goos, func(t *testing.T) {
			host := newFakeHost().
				addCommand("vpn-up", "/usr/local/bin/vpn-up").
				addCommand("vpn-down", "/usr/local/bin/vpn-down")
			got := Build(config.Config{}, host.options(tc.goos))
			for _, id := range []string{"vpn-up", "vpn-down"} {
				r := resolvedByID(t, got, id)
				if tc.wantDisab != "" {
					if r.Enabled() {
						t.Fatalf("%s should be disabled on %s", id, tc.goos)
					}
					if r.Disabled != tc.wantDisab {
						t.Fatalf("%s reason = %q, want %q", id, r.Disabled, tc.wantDisab)
					}
					continue
				}
				if !r.Enabled() {
					t.Fatalf("%s should be enabled on %s, got %+v", id, tc.goos, r)
				}
				if r.Lane != tc.wantLane {
					t.Fatalf("%s lane = %q, want %q", id, r.Lane, tc.wantLane)
				}
			}
		})
	}
}

func TestVPNUnsupportedPlatformStillReportsMissingBinary(t *testing.T) {
	// On a supported lane with no binary the reason must stay actionable.
	host := newFakeHost()
	got := Build(config.Config{}, host.options("linux"))
	up := resolvedByID(t, got, "vpn-up")
	if up.Disabled != "missing: vpn-up" {
		t.Fatalf("vpn-up reason = %q, want missing: vpn-up", up.Disabled)
	}
	if up.Lane != "ssh-tunnel" {
		t.Fatalf("vpn-up lane = %q, want ssh-tunnel", up.Lane)
	}
}

func TestPlatformListGating(t *testing.T) {
	entries := []Entry{{
		ID:        "apt-only",
		Title:     "apt only",
		Command:   "apt-only",
		Platforms: []string{"linux"},
	}}
	host := newFakeHost().addCommand("apt-only", "/usr/bin/apt-only")

	onDarwin := ResolveEntries(entries, config.Config{}, host.options("darwin"))
	if r := resolvedByID(t, onDarwin, "apt-only"); r.Disabled != "unsupported platform: darwin" {
		t.Fatalf("darwin reason = %q", r.Disabled)
	}
	onLinux := ResolveEntries(entries, config.Config{}, host.options("linux"))
	if r := resolvedByID(t, onLinux, "apt-only"); !r.Enabled() {
		t.Fatalf("linux should enable apt-only, got %+v", r)
	}
}

func TestPathOverrideWinsOverPath(t *testing.T) {
	host := newFakeHost().
		addCommand("cml", "/usr/local/bin/cml").
		addFile("/opt/bin/cml", 0o755)
	cfg := config.Config{PathOverrides: map[string]string{"cml": "/opt/bin/cml"}}
	got := Build(cfg, host.options("darwin"))
	cml := resolvedByID(t, got, "cml")
	if cml.Binary != "/opt/bin/cml" {
		t.Fatalf("cml binary = %q, want override /opt/bin/cml", cml.Binary)
	}
}

func TestPathOverrideExpandsHome(t *testing.T) {
	host := newFakeHost().addFile("/home/op/bin/cml", 0o755)
	cfg := config.Config{PathOverrides: map[string]string{"cml": "~/bin/cml"}}
	got := Build(cfg, host.options("darwin"))
	cml := resolvedByID(t, got, "cml")
	if cml.Binary != "/home/op/bin/cml" {
		t.Fatalf("cml binary = %q", cml.Binary)
	}
}

func TestPathOverrideDisabledReasons(t *testing.T) {
	t.Run("missing override path", func(t *testing.T) {
		host := newFakeHost().addCommand("cml", "/usr/local/bin/cml")
		cfg := config.Config{PathOverrides: map[string]string{"cml": "/opt/gone/cml"}}
		cml := resolvedByID(t, Build(cfg, host.options("darwin")), "cml")
		if cml.Disabled != "missing: /opt/gone/cml" {
			t.Fatalf("reason = %q", cml.Disabled)
		}
	})
	t.Run("not executable override path", func(t *testing.T) {
		host := newFakeHost().addFile("/opt/bin/cml", 0o644)
		cfg := config.Config{PathOverrides: map[string]string{"cml": "/opt/bin/cml"}}
		cml := resolvedByID(t, Build(cfg, host.options("darwin")), "cml")
		if cml.Disabled != "not executable: /opt/bin/cml" {
			t.Fatalf("reason = %q", cml.Disabled)
		}
	})
}

func TestExtraEntriesAndOverrideByID(t *testing.T) {
	host := newFakeHost().
		addCommand("cml", "/usr/local/bin/cml").
		addCommand("notes", "/usr/local/bin/notes")

	cfg := config.Config{ExtraEntries: []config.EntryConfig{
		{ID: "notes", Title: "Notes", Command: "notes", Args: []string{"--fast"}},
		{ID: "cml", Title: "limits", Command: "notes"},
	}}
	got := Build(cfg, host.options("darwin"))

	notes := resolvedByID(t, got, "notes")
	if !notes.Enabled() || notes.Binary != "/usr/local/bin/notes" {
		t.Fatalf("extra entry = %+v", notes)
	}
	if notes.Display() != "Notes" {
		t.Fatalf("extra title = %q", notes.Display())
	}

	cml := resolvedByID(t, got, "cml")
	if cml.Display() != "limits" || cml.Binary != "/usr/local/bin/notes" {
		t.Fatalf("override-by-id entry = %+v", cml)
	}
	// The override replaced the command, so args from the extension are kept.
	if len(cml.Args) != 0 {
		t.Fatalf("expected no args on overridden cml, got %v", cml.Args)
	}
}

func TestOrderMovesListedEntriesFirst(t *testing.T) {
	host := newFakeHost().
		addCommand("cml", "/usr/local/bin/cml").
		addCommand("fixer", "/usr/local/bin/fixer")
	cfg := config.Config{Order: []string{"fixer", "cml", "unknown-id"}}
	got := Build(cfg, host.options("darwin"))
	if got[0].ID != "fixer" || got[1].ID != "cml" {
		t.Fatalf("leading order = %v", ids(got)[:2])
	}
	if len(got) != len(BaseEntries()) {
		t.Fatalf("ordering changed entry count: %v", ids(got))
	}
}

func TestEmptyOrderPreservesDefaults(t *testing.T) {
	host := newFakeHost()
	got := Build(config.Config{}, host.options("darwin"))
	if strings.Join(ids(got), ",") != strings.Join(ids(Build(config.Config{}, host.options("darwin"))), ",") {
		t.Fatal("ordering is not stable")
	}
	if got[0].ID != "cml" {
		t.Fatalf("first entry = %q, want cml", got[0].ID)
	}
}

func TestFindRepoRoot(t *testing.T) {
	files := map[string]os.FileMode{
		"/a/b/scripts/fleet/operator_env.py": 0o644,
	}
	stat := func(name string) (os.FileInfo, error) {
		if name == "/a/b" || name == "/a/b/control_plane" {
			return fakeFileInfo{name: filepath.Base(name), mode: os.ModeDir | 0o755, dir: true}, nil
		}
		if mode, ok := files[name]; ok {
			return fakeFileInfo{name: filepath.Base(name), mode: mode}, nil
		}
		return nil, errors.New("not found")
	}
	if got := FindRepoRoot("/a/b/control_plane", "scripts/fleet/operator_env.py", stat); got != "/a/b" {
		t.Fatalf("FindRepoRoot() = %q, want /a/b", got)
	}
	if got := FindRepoRoot("/x/y", "scripts/fleet/operator_env.py", stat); got != "" {
		t.Fatalf("FindRepoRoot() = %q, want empty", got)
	}
	if got := FindRepoRoot("", "scripts/fleet/operator_env.py", stat); got != "" {
		t.Fatalf("FindRepoRoot() with empty start = %q, want empty", got)
	}
}

func TestCommandLineRendersResolvedInvocation(t *testing.T) {
	r := Resolved{Entry: Entry{ID: "x", Args: []string{"/repo/tool.py", "check"}}, Binary: "/usr/bin/python3"}
	if got := r.CommandLine(); got != "/usr/bin/python3 /repo/tool.py check" {
		t.Fatalf("CommandLine() = %q", got)
	}
}
