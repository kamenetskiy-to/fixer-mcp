package console

import (
	"os"
	"path/filepath"
	"strings"
	"testing"
	"time"

	tea "github.com/charmbracelet/bubbletea"

	"github.com/fixer-mcp/control-plane/internal/domain"
	"github.com/fixer-mcp/control-plane/internal/launch"
	"github.com/fixer-mcp/control-plane/internal/mcpclient"
	"github.com/fixer-mcp/control-plane/internal/resources"
	"github.com/fixer-mcp/control-plane/internal/state"
)

func TestNewConsoleIsWorkFirstAndLaunchCardIsEditable(t *testing.T) {
	t.Setenv("HOME", t.TempDir())
	model, err := New(Options{StatePath: t.TempDir() + "/console.json", InitialPath: t.TempDir(), Version: "test"})
	if err != nil {
		t.Fatal(err)
	}
	view := model.View()
	for _, want := range []string{"Работа", "Ресурсы", "Машины", "Проектная работа", "Руки", "Фиксер", "Новый локальный запуск"} {
		if !strings.Contains(view, want) {
			t.Fatalf("initial view does not contain %q:\n%s", want, view)
		}
	}
	for _, forbidden := range []string{"Fleet", "Сеть"} {
		if strings.Contains(view, forbidden) {
			t.Fatalf("top-level UI must not expose %q:\n%s", forbidden, view)
		}
	}
	updated, _ := model.Update(tea.KeyMsg{Type: tea.KeyRunes, Runes: []rune{'n'}})
	model = updated.(Model)
	if model.form == nil {
		t.Fatal("n should open the launch card")
	}
	if !strings.Contains(model.View(), "одна редактируемая карточка запуска") {
		t.Fatal("launch card view missing")
	}
}

func TestWorkViewShowsResumableRecentSessions(t *testing.T) {
	model, err := New(Options{StatePath: t.TempDir() + "/console.json", InitialPath: t.TempDir()})
	if err != nil {
		t.Fatal(err)
	}
	updated, _ := model.Update(projectWorkMsg(mcpclient.Snapshot{
		Available: true,
		Overview:  "Canonical overview",
		FixerSessions: []mcpclient.FixerSession{
			{SessionID: "sess-1", Provider: "pi", Model: "deepseek-v4.1-flash", Preview: "Resume the console work", Updated: "2026-09-22 02:18"},
			{SessionID: "sess-2", Provider: "codex", Model: "gpt-5.6-luna", Preview: "Another fixer run", Updated: "2026-09-17 21:40"},
		},
		HandsInstructions: []mcpclient.HandsInstruction{
			{ID: "inst-1", Text: "Ship the release", Lane: "codex", State: "running", CreatedAt: "2026-08-05"},
		},
	}))
	model = updated.(Model)
	view := model.View()
	for _, want := range []string{"Canonical overview", "Последние сессии", "Фиксер", "Руки", "Resume the console work", "Ship the release"} {
		if !strings.Contains(view, want) {
			t.Fatalf("home view missing %q:\n%s", want, view)
		}
	}
	for _, forbidden := range []string{"Канонические сессии MCP", "Руки: ", "Continue deployment"} {
		if strings.Contains(view, forbidden) {
			t.Fatalf("home view must not show %q:\n%s", forbidden, view)
		}
	}
	if strings.Index(view, "Resume the console work") > strings.Index(view, "＋ Новый локальный запуск") {
		t.Fatalf("recent sessions must be selectable rows above the new-launch action:\n%s", view)
	}

	// Rows 0/1 are the Руки/Фиксер entries; row 2 is the first recent session.
	updated, _ = model.Update(tea.KeyMsg{Type: tea.KeyDown})
	model = updated.(Model)
	updated, _ = model.Update(tea.KeyMsg{Type: tea.KeyDown})
	model = updated.(Model)
	updated, _ = model.Update(tea.KeyMsg{Type: tea.KeyEnter})
	model = updated.(Model)
	if model.workMode == nil || model.workMode.action != "resume" {
		t.Fatalf("Enter on a Fixer row must open the prefilled resume screen: %#v", model.workMode)
	}
	if len(model.workMode.resumeIDs) != 1 || model.workMode.resumeIDs[0] != "pi:sess-1" {
		t.Fatalf("resume target not pinned: %#v", model.workMode.resumeIDs)
	}

	// The Руки row keeps the lane it was recorded with (rows: 2 fixer, then Руки).
	model.workMode = nil
	updated, _ = model.Update(tea.KeyMsg{Type: tea.KeyDown})
	model = updated.(Model)
	updated, _ = model.Update(tea.KeyMsg{Type: tea.KeyDown})
	model = updated.(Model)
	updated, _ = model.Update(tea.KeyMsg{Type: tea.KeyEnter})
	model = updated.(Model)
	if model.workMode == nil || model.workMode.kind != launch.KindHands || model.workMode.pinnedLane != "codex" {
		t.Fatalf("Enter on a Hands row must open the channel with its lane: %#v", model.workMode)
	}
}

func TestQuitCancelsBackgroundRefreshes(t *testing.T) {
	model, err := New(Options{StatePath: t.TempDir() + "/console.json", InitialPath: t.TempDir()})
	if err != nil {
		t.Fatal(err)
	}
	updated, cmd := model.Update(tea.KeyMsg{Type: tea.KeyRunes, Runes: []rune{'q'}})
	if cmd == nil {
		t.Fatal("q should request program termination")
	}
	model = updated.(Model)
	select {
	case <-model.lifecycle.Done():
	default:
		t.Fatal("q did not cancel background refreshes")
	}
}

func TestNativeHandsScreenReplacesLegacySelector(t *testing.T) {
	model, err := New(Options{StatePath: t.TempDir() + "/console.json", InitialPath: t.TempDir()})
	if err != nil {
		t.Fatal(err)
	}
	updated, _ := model.Update(tea.KeyMsg{Type: tea.KeyRunes, Runes: []rune{'h'}})
	model = updated.(Model)
	if model.workMode == nil || model.form != nil {
		t.Fatalf("h must open the native work screen, workMode=%v form=%v", model.workMode != nil, model.form != nil)
	}
	view := model.View()
	for _, want := range []string{"Руки — Project Hands", "Режим worktree", "Safe — изолированный worktree", "без legacy-промптов"} {
		if !strings.Contains(view, want) {
			t.Fatalf("native Hands screen missing %q:\n%s", want, view)
		}
	}
	for _, forbidden := range []string{"Project Hands workspace mode", "Register project name", "fixer_wire.py", "^[OB"} {
		if strings.Contains(view, forbidden) {
			t.Fatalf("native screen must not show legacy marker %q:\n%s", forbidden, view)
		}
	}

	// Arrow keys must stay inside the native screen and cycle the value.
	updated, _ = model.Update(tea.KeyMsg{Type: tea.KeyRight})
	model = updated.(Model)
	if model.workMode == nil || model.workMode.workspace != "hotfix" {
		t.Fatalf("right arrow did not cycle the worktree mode: %#v", model.workMode)
	}
	if !strings.Contains(model.View(), "Hotfix — текущий живой worktree") {
		t.Fatalf("hotfix value not rendered:\n%s", model.View())
	}

	// Focus moves down to the lane and cycles without escaping the screen.
	updated, _ = model.Update(tea.KeyMsg{Type: tea.KeyDown})
	model = updated.(Model)
	updated, _ = model.Update(tea.KeyMsg{Type: tea.KeyRight})
	model = updated.(Model)
	if model.workMode == nil || model.workMode.lane == "" || model.workMode.focus != 1 {
		t.Fatalf("lane field did not cycle in place: %#v", model.workMode)
	}

	updated, _ = model.Update(tea.KeyMsg{Type: tea.KeyEsc})
	model = updated.(Model)
	if model.workMode != nil {
		t.Fatal("esc must close the native work screen")
	}
}

func TestNativeFixerScreenOffersNewResumeUnattached(t *testing.T) {
	model, err := New(Options{StatePath: t.TempDir() + "/console.json", InitialPath: t.TempDir()})
	if err != nil {
		t.Fatal(err)
	}
	updated, _ := model.Update(projectWorkMsg(mcpclient.Snapshot{
		Available: true,
		// Netrunner work must never leak into Fixer's resume list: handing a
		// Netrunner id to --fixer-session-id resumes the wrong thing.
		Sessions: []mcpclient.Session{
			{ID: 673, Status: "in_progress", TaskSummary: "Netrunner task", Backend: "pi"},
		},
		FixerSessions: []mcpclient.FixerSession{
			{SessionID: "01a0-session", Provider: "pi", Preview: "Ship the term", Model: "deepseek-v4.1-flash"},
		},
	}))
	model = updated.(Model)
	updated, _ = model.Update(tea.KeyMsg{Type: tea.KeyRunes, Runes: []rune{'f'}})
	model = updated.(Model)
	if model.workMode == nil {
		t.Fatal("f must open the native Fixer screen")
	}
	view := model.View()
	for _, want := range []string{"Фиксер — Fixer", "Действие", "Новый Fixer"} {
		if !strings.Contains(view, want) {
			t.Fatalf("native Fixer screen missing %q:\n%s", want, view)
		}
	}
	if model.workMode.action != "new" {
		t.Fatalf("default Fixer action = %q, want new", model.workMode.action)
	}
	if len(model.workMode.resumeIDs) != 1 || model.workMode.resumeIDs[0] != "pi:01a0-session" {
		t.Fatalf("the Fixer session must be the only resume target, got %#v", model.workMode.resumeIDs)
	}
	if strings.Contains(strings.Join(model.workMode.resumeIDs, ","), "673") {
		t.Fatalf("a Netrunner id leaked into Fixer resume: %#v", model.workMode.resumeIDs)
	}

	// Resume becomes a second field with its own selectable sessions.
	updated, _ = model.Update(tea.KeyMsg{Type: tea.KeyRight})
	model = updated.(Model)
	if model.workMode.action != "resume" || model.workMode.fieldCount() != 2 {
		t.Fatalf("resume action was not selected: %#v", model.workMode)
	}
	if !strings.Contains(model.View(), "Ship the term") {
		t.Fatalf("resume target not rendered:\n%s", model.View())
	}
}

func TestLaunchCardHandsKindOpensNativeWorkScreen(t *testing.T) {
	model, err := New(Options{StatePath: t.TempDir() + "/console.json", InitialPath: t.TempDir()})
	if err != nil {
		t.Fatal(err)
	}
	updated, _ := model.Update(tea.KeyMsg{Type: tea.KeyRunes, Runes: []rune{'n'}})
	model = updated.(Model)
	// Cycle the kind field until it is the Fixer workroom.
	for i := 0; i < 8 && model.form.spec.Kind != launch.KindWorkroom; i++ {
		updated, _ = model.Update(tea.KeyMsg{Type: tea.KeyRight})
		model = updated.(Model)
	}
	if model.form == nil || model.form.spec.Kind != launch.KindWorkroom {
		t.Fatalf("launch card did not reach the workroom kind: %#v", model.form)
	}
	updated, _ = model.Update(tea.KeyMsg{Type: tea.KeyEnter})
	model = updated.(Model)
	if model.form != nil || model.workMode == nil || model.workMode.kind != launch.KindWorkroom {
		t.Fatalf("workroom kind must hand over to the native screen: form=%v workMode=%#v", model.form != nil, model.workMode)
	}
}

func TestNativeHandsScreenUsesOnlyRegisteredLanes(t *testing.T) {
	model, err := New(Options{StatePath: t.TempDir() + "/console.json", InitialPath: t.TempDir()})
	if err != nil {
		t.Fatal(err)
	}
	updated, _ := model.Update(projectWorkMsg(mcpclient.Snapshot{
		Available: true,
		Hands: mcpclient.Hands{
			Available: true,
			// The active instruction runs on pi, but pi is not a registrable
			// Project Hands lane: offering it crashes the launch.
			ActiveLane:  "pi",
			DefaultLane: "codex",
			Lanes: []mcpclient.Lane{
				{Provider: "codex", Model: "gpt-5.6-luna"},
				{Provider: "commandcode", Model: "commandcode/zai-org/glm-5.3-flash"},
			},
		},
	}))
	model = updated.(Model)
	updated, _ = model.Update(tea.KeyMsg{Type: tea.KeyRunes, Runes: []rune{'h'}})
	model = updated.(Model)
	if model.workMode == nil {
		t.Fatal("h must open the native Hands screen")
	}
	if model.workMode.lane != "codex" {
		t.Fatalf("lane = %q, want the project default from MCP", model.workMode.lane)
	}
	view := model.View()
	if !strings.Contains(view, "codex · gpt-5.6-luna") {
		t.Fatalf("registered lane and its model must be visible:\n%s", view)
	}
	if strings.Contains(view, "pi ·") || strings.Contains(view, "Lane           pi") {
		t.Fatalf("an unregistered lane leaked into the native screen:\n%s", view)
	}

	updated, _ = model.Update(tea.KeyMsg{Type: tea.KeyDown})
	model = updated.(Model)
	updated, _ = model.Update(tea.KeyMsg{Type: tea.KeyRight})
	model = updated.(Model)
	if model.workMode.lane != "commandcode" {
		t.Fatalf("cycling must stay inside the registered set, got %q", model.workMode.lane)
	}
}

func TestHandsScreenOffersNativeMCPAndDocumentSelection(t *testing.T) {
	model, err := New(Options{StatePath: t.TempDir() + "/console.json", InitialPath: t.TempDir()})
	if err != nil {
		t.Fatal(err)
	}
	updated, _ := model.Update(projectWorkMsg(mcpclient.Snapshot{
		Available: true,
		Hands: mcpclient.Hands{
			Available: true, DefaultLane: "codex",
			Lanes: []mcpclient.Lane{{Provider: "codex"}, {Provider: "commandcode"}},
		},
		MCPPool: []mcpclient.MCPServer{
			{Name: "postgres", Category: "DB", ShortDescription: "PostgreSQL ops"},
			{Name: "sqlite", Category: "DB", ShortDescription: "SQLite inspection"},
			{Name: "figma-console-mcp", Archived: true},
		},
		HandsMCP: []string{"postgres"},
		DocPool: []mcpclient.ProjectDoc{
			{DocID: 1, Title: "Canon", Level: 0},
			{DocID: 17, Title: "History Split", Level: 2},
		},
		HandsDocs: []int{1},
	}))
	model = updated.(Model)
	updated, _ = model.Update(tea.KeyMsg{Type: tea.KeyRunes, Runes: []rune{'h'}})
	model = updated.(Model)
	view := model.View()
	for _, want := range []string{"MCP-серверы", "Документы", "как в проекте (keep)"} {
		if !strings.Contains(view, want) {
			t.Fatalf("Hands screen missing %q:\n%s", want, view)
		}
	}

	// Focus the MCP row and open the native checkbox overlay.
	updated, _ = model.Update(tea.KeyMsg{Type: tea.KeyDown})
	model = updated.(Model)
	updated, _ = model.Update(tea.KeyMsg{Type: tea.KeyDown})
	model = updated.(Model)
	updated, _ = model.Update(tea.KeyMsg{Type: tea.KeyEnter})
	model = updated.(Model)
	if model.multi == nil {
		t.Fatal("Enter on the MCP row must open the picker")
	}
	view = model.View()
	if !strings.Contains(view, "MCP-серверы для Рук") || !strings.Contains(view, "postgres") {
		t.Fatalf("MCP picker missing pool:\n%s", view)
	}
	if strings.Contains(view, "figma-console-mcp") {
		t.Fatalf("archived servers must not be offered:\n%s", view)
	}
	if model.multi.selectedCount() != 1 {
		t.Fatalf("stored proposal must start checked, got %d", model.multi.selectedCount())
	}

	// Arrow keys move a real cursor and toggle a second server.
	updated, _ = model.Update(tea.KeyMsg{Type: tea.KeyDown})
	model = updated.(Model)
	updated, _ = model.Update(tea.KeyMsg{Type: tea.KeyRunes, Runes: []rune{' '}})
	model = updated.(Model)
	if model.multi.selectedCount() != 2 {
		t.Fatalf("space must toggle a row, got %d selected", model.multi.selectedCount())
	}
	updated, _ = model.Update(tea.KeyMsg{Type: tea.KeyEnter})
	model = updated.(Model)
	if model.multi != nil || model.workMode.mcpPreset != "custom" {
		t.Fatalf("confirm must return to the work screen: picker=%v preset=%q", model.multi != nil, model.workMode.mcpPreset)
	}
	if got := model.workMode.mcpValue(); got != "postgres,sqlite" {
		t.Fatalf("MCP value = %q", got)
	}

	// Documents picker writes project-local doc ids.
	updated, _ = model.Update(tea.KeyMsg{Type: tea.KeyDown})
	model = updated.(Model)
	updated, _ = model.Update(tea.KeyMsg{Type: tea.KeyEnter})
	model = updated.(Model)
	if model.multi == nil || model.multi.target != "docs" {
		t.Fatalf("documents picker did not open: %#v", model.multi)
	}
	view = model.View()
	if !strings.Contains(view, "History Split") || !strings.Contains(view, "[x] Canon") {
		t.Fatalf("document picker missing pool/selection:\n%s", view)
	}
	updated, _ = model.Update(tea.KeyMsg{Type: tea.KeyDown})
	model = updated.(Model)
	updated, _ = model.Update(tea.KeyMsg{Type: tea.KeyRunes, Runes: []rune{' '}})
	model = updated.(Model)
	updated, _ = model.Update(tea.KeyMsg{Type: tea.KeyEnter})
	model = updated.(Model)
	if got := model.workMode.docValue(); got != "1,17" {
		t.Fatalf("docs value = %q, want stored proposal plus the newly added doc", got)
	}

	// Enter from the top row still launches, and carries both selections.
	for i := 0; i < 3; i++ {
		updated, _ = model.Update(tea.KeyMsg{Type: tea.KeyUp})
		model = updated.(Model)
	}
	if model.workMode.focus != 0 {
		t.Fatalf("focus = %d, want the worktree row", model.workMode.focus)
	}
	if model.workMode.mcpValue() != "postgres,sqlite" || model.workMode.docValue() != "1,17" {
		t.Fatalf("selections were lost: mcp=%q docs=%q", model.workMode.mcpValue(), model.workMode.docValue())
	}
}

func TestMachinesSpaceProbesReachabilityWhenOpened(t *testing.T) {
	model, err := New(Options{StatePath: t.TempDir() + "/console.json", InitialPath: t.TempDir()})
	if err != nil {
		t.Fatal(err)
	}
	// Until the sweep returns, the row must say it is being checked rather
	// than claiming the machine is unreachable.
	if !strings.Contains(model.View(), "проверяю…") {
		// The machines view is only rendered on its own tab.
	}
	updated, cmd := model.Update(tea.KeyMsg{Type: tea.KeyRunes, Runes: []rune{'3'}})
	model = updated.(Model)
	if model.tab != machinesTab {
		t.Fatalf("tab = %d, want machines", model.tab)
	}
	if cmd == nil {
		t.Fatal("opening the machines space must start the parallel reachability sweep")
	}
	view := model.View()
	if !strings.Contains(view, "проверяю…") {
		t.Fatalf("unprobed machines must not be shown as unreachable:\n%s", view)
	}
	if !strings.Contains(view, "Tailscale") || !strings.Contains(view, "Wi-Fi") {
		t.Fatalf("machines space must advertise both probe sides:\n%s", view)
	}

	// The startup path must also probe, not just the tab switch.
	if model.Init() == nil {
		t.Fatal("Init must schedule the reachability sweep")
	}
}

func TestResourcesViewRendersNativeLimitsBoard(t *testing.T) {
	model, err := New(Options{StatePath: t.TempDir() + "/console.json", InitialPath: t.TempDir()})
	if err != nil {
		t.Fatal(err)
	}
	pct27, pct89, pct3 := 27.0, 89.0, 3.0
	raw := "Провайдер │ Статус │ Лимиты 5h\nCodex / Babushka │ plus │ 27% left, reset in 11m 45s"
	updated, _ := model.Update(quotaMsg(resources.Snapshot{
		CMLOutput:    raw,
		CMLCheckedAt: time.Now(),
		Quotas: []resources.Quota{
			{Provider: "Codex / Babushka", Status: "plus",
				Window5h: resources.QuotaWindow{Text: "27% left, reset in 11m 45s", Percent: &pct27, Reset: "11m 45s"},
				Window7d: resources.QuotaWindow{Text: "89% left, reset in 4d 13h", Percent: &pct89, Reset: "4d 13h"}},
			{Provider: "Agy/Claude+GPT", Status: "ok",
				Window5h: resources.QuotaWindow{Text: "3% left, reset in 12m", Percent: &pct3, Reset: "12m"}},
		},
	}))
	model = updated.(Model)
	updated, _ = model.Update(tea.KeyMsg{Type: tea.KeyRunes, Runes: []rune{'2'}})
	model = updated.(Model)
	view := model.View()

	for _, want := range []string{"Лимиты", "5 часов", "7 дней", "1 месяц", "27%", "↻11м45с"} {
		if !strings.Contains(view, want) {
			t.Fatalf("native limits board missing %q:\n%s", want, view)
		}
	}
	if strings.Contains(view, "Провайдер │ Статус") {
		t.Fatalf("legacy cml table must not be the default view:\n%s", view)
	}
	if strings.Index(view, "Agy/Claude+GPT") > strings.Index(view, "Codex / Babushka") {
		t.Fatalf("limits are not sorted by urgency:\n%s", view)
	}

	updated, _ = model.Update(tea.KeyMsg{Type: tea.KeyRunes, Runes: []rune{'o'}})
	model = updated.(Model)
	if !strings.Contains(model.View(), "Провайдер │ Статус") {
		t.Fatalf("o must reveal the raw cml report:\n%s", model.View())
	}
}

// A remembered project must never hijack `fixer` in another checkout. On WSL
// the remembered entry was `/home/megur`, so every launch reopened home and MCP
// refused to bind it, even though the project they stood in was registered.
func TestLaunchBindsToTheDirectoryTheOperatorStandsIn(t *testing.T) {
	statePath := filepath.Join(t.TempDir(), "console.json")
	store, err := state.Load(statePath)
	if err != nil {
		t.Fatal(err)
	}
	if err := store.RememberProject(domain.Project{ID: "_stale_home", Name: "home", Path: "/nonexistent/home"}); err != nil {
		t.Fatal(err)
	}

	dir := t.TempDir()
	previous, err := os.Getwd()
	if err != nil {
		t.Fatal(err)
	}
	defer func() { _ = os.Chdir(previous) }()
	if err := os.Chdir(dir); err != nil {
		t.Fatal(err)
	}

	model, err := New(Options{StatePath: statePath})
	if err != nil {
		t.Fatal(err)
	}
	got, _ := filepath.EvalSymlinks(model.project.Path)
	want, _ := filepath.EvalSymlinks(dir)
	if got != want {
		t.Fatalf("project = %q, want the operator's directory %q", model.project.Path, dir)
	}
}

// Standing in home (or root) has no project to show, so there the remembered
// project is the better answer than an unregistered directory.
func TestLaunchFromHomeFallsBackToTheRememberedProject(t *testing.T) {
	statePath := filepath.Join(t.TempDir(), "console.json")
	store, err := state.Load(statePath)
	if err != nil {
		t.Fatal(err)
	}
	home := t.TempDir()
	t.Setenv("HOME", home)
	remembered := t.TempDir()
	if err := store.RememberProject(domain.Project{ID: "_remembered", Name: "remembered", Path: remembered}); err != nil {
		t.Fatal(err)
	}
	previous, err := os.Getwd()
	if err != nil {
		t.Fatal(err)
	}
	defer func() { _ = os.Chdir(previous) }()
	if err := os.Chdir(home); err != nil {
		t.Fatal(err)
	}

	model, err := New(Options{StatePath: statePath})
	if err != nil {
		t.Fatal(err)
	}
	if model.project.Path != remembered {
		t.Fatalf("project = %q, want the remembered %q", model.project.Path, remembered)
	}
}

// Resuming in the wrong directory makes Codex stop and ask "session directory
// or current directory", which blocks the launch the operator asked for.
func TestFixerResumeUsesTheRecordedSessionDirectory(t *testing.T) {
	recorded := t.TempDir()
	model, err := New(Options{StatePath: filepath.Join(t.TempDir(), "console.json"), InitialPath: t.TempDir()})
	if err != nil {
		t.Fatal(err)
	}
	updated, _ := model.Update(projectWorkMsg(mcpclient.Snapshot{
		Available: true,
		FixerSessions: []mcpclient.FixerSession{
			{SessionID: "01a0-fixed", Provider: "codex", Preview: "Fixer run in another checkout", CWD: recorded},
		},
	}))
	model = updated.(Model)
	updated, _ = model.Update(tea.KeyMsg{Type: tea.KeyRunes, Runes: []rune{'f'}})
	model = updated.(Model)
	if model.workMode == nil {
		t.Fatal("f must open the native Fixer screen")
	}
	updated, _ = model.Update(tea.KeyMsg{Type: tea.KeyRight})
	model = updated.(Model)
	form := model.workMode
	if form.action != "resume" {
		t.Fatalf("action = %q, want resume", form.action)
	}
	if len(form.resumeIDs) != 1 || form.resumeIDs[0] != "01a0-fixed" {
		t.Fatalf("resume ids = %#v, want the Fixer session", form.resumeIDs)
	}
	if form.resumeCWDValue() != recorded {
		t.Fatalf("recorded cwd = %q, want %q", form.resumeCWDValue(), recorded)
	}
	if got := form.resumeTarget(form.project); got != recorded {
		t.Fatalf("resume target = %q, want the recorded directory %q", got, recorded)
	}
	view := model.View()
	if !strings.Contains(view, "сессия из: "+recorded) {
		t.Fatalf("the resume directory must be visible before launching:\n%s", view)
	}
}

func TestSearchMovesToResources(t *testing.T) {
	model, err := New(Options{StatePath: t.TempDir() + "/console.json", InitialPath: t.TempDir()})
	if err != nil {
		t.Fatal(err)
	}
	updated, _ := model.Update(tea.KeyMsg{Type: tea.KeyRunes, Runes: []rune{'/'}})
	model = updated.(Model)
	for _, r := range "quota" {
		updated, _ = model.Update(tea.KeyMsg{Type: tea.KeyRunes, Runes: []rune{r}})
		model = updated.(Model)
	}
	updated, _ = model.Update(tea.KeyMsg{Type: tea.KeyEnter})
	model = updated.(Model)
	if model.tab != resourcesTab || model.search != nil {
		t.Fatalf("search did not select resources: tab=%d search=%v", model.tab, model.search)
	}
}

// A stray bare fixer.db must never be bound silently. The home screen names it
// and shows the one explicit override that adopts it.
func TestWorkViewSurfacesIgnoredStrayDatabase(t *testing.T) {
	home := t.TempDir()
	t.Setenv("HOME", home)
	t.Setenv("FIXER_DB_PATH", "")
	state := filepath.Join(home, "state")
	if err := os.MkdirAll(state, 0o700); err != nil {
		t.Fatal(err)
	}
	if err := os.WriteFile(filepath.Join(state, "fixer.db"), nil, 0o600); err != nil {
		t.Fatal(err)
	}
	t.Setenv("FIXER_STATE_DIR", state)

	project := t.TempDir()
	stray := filepath.Join(project, "fixer.db")
	if err := os.WriteFile(stray, nil, 0o600); err != nil {
		t.Fatal(err)
	}
	model, err := New(Options{StatePath: filepath.Join(t.TempDir(), "console.json"), InitialPath: project})
	if err != nil {
		t.Fatal(err)
	}
	view := model.View()
	if !strings.Contains(view, "посторонний fixer.db") || !strings.Contains(view, stray) {
		t.Fatalf("home view must surface the ignored stray %q:\n%s", stray, view)
	}
	if !strings.Contains(view, "FIXER_DB_PATH="+stray) {
		t.Fatalf("stray line must show the explicit adoption override:\n%s", view)
	}
}
