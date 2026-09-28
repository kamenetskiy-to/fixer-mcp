package console

import (
	"context"
	"fmt"
	"math"
	"os"
	"os/exec"
	"path/filepath"
	"regexp"
	"runtime"
	"sort"
	"strconv"
	"strings"
	"time"

	tea "github.com/charmbracelet/bubbletea"
	"github.com/charmbracelet/lipgloss"

	"github.com/fixer-mcp/control-plane/internal/domain"
	"github.com/fixer-mcp/control-plane/internal/launch"
	"github.com/fixer-mcp/control-plane/internal/machines"
	"github.com/fixer-mcp/control-plane/internal/mcpclient"
	"github.com/fixer-mcp/control-plane/internal/network"
	"github.com/fixer-mcp/control-plane/internal/registry"
	"github.com/fixer-mcp/control-plane/internal/resources"
	"github.com/fixer-mcp/control-plane/internal/runner"
	"github.com/fixer-mcp/control-plane/internal/sessions"
	"github.com/fixer-mcp/control-plane/internal/state"
)

const (
	workTab = iota
	resourcesTab
	machinesTab
	tabCount        = 3
	workActionCount = 2
)

type Options struct {
	RuntimeRoot     string
	CanonicalDBPath string
	StatePath       string
	Version         string
	InitialTab      int
	InitialPath     string
	// InitialWorkMode preopens a native work screen (`hands` or `workroom`)
	// instead of the legacy line selectors.
	InitialWorkMode string
}

type Model struct {
	store     *state.Store
	sessions  *sessions.Manager
	opts      Options
	lifecycle context.Context
	cancel    context.CancelFunc

	tab       int
	cursor    int
	width     int
	height    int
	showHelp  bool
	quitting  bool
	form      *launchForm
	workMode  *workModeForm
	multi     *multiSelectState
	search    *searchState
	confirm   *confirmState
	message   string
	messageAt time.Time

	project           domain.Project
	items             []domain.LocalContext
	strayDBs          []string // bare `fixer.db` files the canonical rule refuses to bind
	providers         resources.Snapshot
	machines          []machines.Machine
	network           network.Status
	projectWork       mcpclient.Snapshot
	machinesCheckedAt time.Time
	showRawQuota      bool
	busy              bool
}

type launchForm struct {
	spec     launch.Spec
	focus    int
	edit     bool
	buf      string
	profiles []domain.LaunchDraft
	accounts []resources.Account
	machines []machines.Machine
}

type workModeForm struct {
	kind    string
	project string

	// Project Hands fields.
	workspace    string
	lanes        []mcpclient.Lane
	lane         string
	lanesFromMCP bool
	pinnedLane   string
	handsMCP     string
	handsDocs    string

	// Native selection state: `keep` reproduces what the channel already had,
	// `none`/`all` are quick presets, and `custom` is the multi-select overlay.
	mcpPreset  string
	mcpSel     []string
	mcpPool    []mcpclient.MCPServer
	mcpTouched bool
	docPreset  string
	docSel     []int
	docPool    []mcpclient.ProjectDoc
	docTouched bool

	// Fixer fields.
	action       string
	resumeLabels []string
	resumeIDs    []string
	resumeCWDs   []string
	resumeIdx    int
	pinnedResume bool

	focus int
}

func (f *workModeForm) fieldCount() int {
	if f.kind == launch.KindWorkroom {
		if f.action == "resume" {
			return 2
		}
		return 1
	}
	return 4
}

func (f *workModeForm) mcpValue() string {
	switch f.mcpPreset {
	case "none", "all":
		return f.mcpPreset
	case "custom":
		return strings.Join(f.mcpSel, ",")
	default:
		return "keep"
	}
}

func (f *workModeForm) docValue() string {
	switch f.docPreset {
	case "none", "all":
		return f.docPreset
	case "custom":
		ids := make([]string, 0, len(f.docSel))
		for _, id := range f.docSel {
			ids = append(ids, strconv.Itoa(id))
		}
		return strings.Join(ids, ",")
	default:
		return "keep"
	}
}

func (f *workModeForm) mcpLabel() string {
	switch f.mcpPreset {
	case "none":
		return "ничего не подключать"
	case "all":
		return "все разрешённые проектом"
	case "custom":
		return fmt.Sprintf("выбрано %d: %s", len(f.mcpSel), compactText(strings.Join(f.mcpSel, ", "), 40))
	default:
		return "как в проекте (keep)" + fmt.Sprintf("  ·  %d доступно", len(f.mcpPool))
	}
}

func (f *workModeForm) docLabel() string {
	switch f.docPreset {
	case "none":
		return "без документов"
	case "all":
		return "все документы проекта"
	case "custom":
		return fmt.Sprintf("выбрано %d из %d", len(f.docSel), len(f.docPool))
	default:
		return "как в проекте (keep)" + fmt.Sprintf("  ·  %d документов", len(f.docPool))
	}
}

// persistableHandsLaneFallback mirrors the hands_instruction CHECK list: when
// Fixer MCP is unreachable the console still only offers lanes the launcher
// can persist. It never offers a lane the schema would reject — that is
// exactly how a hardcoded `pi` crashed the launch.
func persistableHandsLaneFallback() []mcpclient.Lane {
	return []mcpclient.Lane{
		{Provider: "codex"},
		{Provider: "commandcode"},
		{Provider: "claude"},
		{Provider: "kimi-code"},
		{Provider: "antigravity"},
		{Provider: "grok"},
	}
}

func laneIndex(lanes []mcpclient.Lane, provider string) int {
	for i, lane := range lanes {
		if lane.Provider == provider {
			return i
		}
	}
	return -1
}

func laneProviders(lanes []mcpclient.Lane) []string {
	values := make([]string, 0, len(lanes))
	for _, lane := range lanes {
		values = append(values, lane.Provider)
	}
	return values
}

// adopt refreshes the async MCP-derived choices without losing what the
// operator already picked in the native screen.
func (f *workModeForm) adopt(snap mcpclient.Snapshot) {
	if snap.Hands.Available && len(snap.Hands.Lanes) > 0 {
		f.lanes = append([]mcpclient.Lane(nil), snap.Hands.Lanes...)
		f.lanesFromMCP = true
	}
	if len(f.lanes) == 0 {
		f.lanes = persistableHandsLaneFallback()
	}
	// Pools feed the native MCP/document pickers; once the operator has chosen,
	// a later refresh must not silently reset that choice.
	if len(snap.MCPPool) > 0 {
		f.mcpPool = append([]mcpclient.MCPServer(nil), snap.MCPPool...)
	}
	if !f.mcpTouched {
		f.mcpSel = append([]string(nil), snap.HandsMCP...)
	}
	if len(snap.DocPool) > 0 {
		f.docPool = append([]mcpclient.ProjectDoc(nil), snap.DocPool...)
	}
	if !f.docTouched {
		f.docSel = append([]int(nil), snap.HandsDocs...)
	}
	// Keep an explicit operator choice; otherwise take the project default,
	// then the lane of the running instruction, then the first registered one.
	if f.pinnedLane != "" {
		if laneIndex(f.lanes, f.pinnedLane) >= 0 {
			f.lane = f.pinnedLane
		}
	} else if f.lane == "" || laneIndex(f.lanes, f.lane) < 0 {
		preferred := ""
		if snap.Hands.Available {
			preferred = snap.Hands.DefaultLane
			if laneIndex(f.lanes, preferred) < 0 {
				preferred = snap.Hands.ActiveLane
			}
		}
		if laneIndex(f.lanes, preferred) < 0 {
			preferred = f.lanes[0].Provider
		}
		f.lane = preferred
	}
	// The resume list is Fixer's own provider sessions, not Netrunner work:
	// passing a Netrunner id to --fixer-session-id resumes the wrong thing.
	if f.kind != launch.KindWorkroom || f.pinnedResume {
		return
	}
	labels := make([]string, 0, len(snap.FixerSessions))
	ids := make([]string, 0, len(snap.FixerSessions))
	cwds := make([]string, 0, len(snap.FixerSessions))
	for _, session := range snap.FixerSessions {
		if strings.TrimSpace(session.SessionID) == "" {
			continue
		}
		labels = append(labels, compactText(strings.Join(strings.Fields(session.Preview), " "), 70))
		ids = append(ids, fixerResumeValue(session))
		cwds = append(cwds, strings.TrimSpace(session.CWD))
	}
	if len(ids) > 0 {
		f.resumeLabels, f.resumeIDs, f.resumeCWDs = labels, ids, cwds
		if f.resumeIdx >= len(ids) {
			f.resumeIdx = 0
		}
	}
}

type searchState struct {
	query  string
	cursor int
}

type confirmState struct {
	title        string
	action       string
	account      resources.Account
	localContext domain.LocalContext
	networkAct   string
}

type tickMsg time.Time
type quotaMsg resources.Snapshot
type machineMsg machines.Snapshot
type networkMsg network.Status
type projectWorkMsg mcpclient.Snapshot
type actionMsg struct {
	text string
	err  error
}
type machineConnectMsg struct {
	machine    machines.Machine
	connection machines.Connection
	err        error
}
type formRouteMsg struct {
	spec        launch.Spec
	logicalHost string
	connection  machines.Connection
	err         error
}
type workModeFinishedMsg struct {
	label string
	err   error
}
type sessionFinishedMsg struct {
	id  string
	err error
}

func New(opts Options) (Model, error) {
	path := opts.StatePath
	if path == "" {
		path = state.DefaultPath(environmentMap())
	}
	store, err := state.Load(path)
	if err != nil {
		return Model{}, err
	}
	cwd := opts.InitialPath
	snapshot := store.Snapshot()
	// The directory the operator is standing in wins over the remembered
	// project. Otherwise one stale entry (on WSL it was `/home/megur`) hijacks
	// every launch: `cd <project> && fixer` kept reopening home and MCP refused
	// to bind it, even though the project was registered.
	if cwd == "" {
		if actual, err := os.Getwd(); err == nil && strings.TrimSpace(actual) != "" {
			cwd = actual
		}
	}
	if isNonProjectDirectory(cwd) && snapshot.LastProjectID != "" {
		if old := snapshot.RecentProjectByID(snapshot.LastProjectID); old != nil && old.Path != "" {
			cwd = old.Path
		}
	}
	if cwd == "" {
		cwd = "."
	}
	projectPath := discoverProject(cwd)
	project := domain.Project{ID: launch.ProjectID(projectPath), Name: filepath.Base(projectPath), Path: projectPath, Host: "local", LastOpened: time.Now()}
	if old := snapshot.RecentProjectByID(project.ID); old != nil {
		project = *old
		project.Path = projectPath
		project.LastOpened = time.Now()
	}
	if err := store.RememberProject(project); err != nil {
		return Model{}, err
	}
	if opts.CanonicalDBPath == "" {
		opts.CanonicalDBPath, _ = mcpclient.DatabasePath(mcpclient.Options{
			RuntimeRoot: opts.RuntimeRoot,
			ProjectPath: project.Path,
			Environment: os.Environ(),
		})
	}
	initialTab := opts.InitialTab
	if initialTab < workTab || initialTab >= tabCount {
		initialTab = workTab
	}
	lifecycle, cancel := context.WithCancel(context.Background())
	model := Model{
		store: store, sessions: sessions.NewManager(store), opts: opts,
		lifecycle: lifecycle, cancel: cancel,
		tab: initialTab, cursor: 0, project: project,
		providers: resources.Inspect(), machines: machines.Discover(),
		network: network.Status{Platform: runtime.GOOS, State: "checking", Checked: time.Now()},
		strayDBs: mcpclient.IgnoredStrayDBs(mcpclient.Options{
			RuntimeRoot: opts.RuntimeRoot,
			ProjectPath: project.Path,
			Environment: os.Environ(),
		}),
	}
	model.refreshSessions()
	if opts.InitialWorkMode == launch.KindHands || opts.InitialWorkMode == launch.KindWorkroom {
		model.workMode = model.newWorkModeForm(opts.InitialWorkMode)
	}
	return model, nil
}

func Run(opts Options) error {
	model, err := New(opts)
	if err != nil {
		return err
	}
	defer model.cancel()
	program := tea.NewProgram(model, tea.WithAltScreen())
	_, err = program.Run()
	return err
}

func (m Model) Init() tea.Cmd {
	// Reachability is probed at once, not on demand: the machines space must
	// already answer "who can I connect to" the first time it is opened.
	return tea.Batch(m.tick(), m.refreshNetwork(), m.refreshQuota(), m.refreshProjectWork(), m.probeMachines())
}

func (m Model) tick() tea.Cmd {
	return tea.Tick(3*time.Second, func(t time.Time) tea.Msg { return tickMsg(t) })
}

func (m Model) refreshQuota() tea.Cmd {
	return func() tea.Msg {
		ctx, cancel := context.WithTimeout(m.lifecycle, 45*time.Second)
		defer cancel()
		return quotaMsg(resources.RefreshQuota(ctx))
	}
}

func (m Model) refreshProjectWork() tea.Cmd {
	return func() tea.Msg {
		ctx, cancel := context.WithTimeout(m.lifecycle, 16*time.Second)
		defer cancel()
		return projectWorkMsg(mcpclient.Inspect(ctx, mcpclient.Options{
			RuntimeRoot: m.opts.RuntimeRoot,
			ProjectPath: m.project.Path,
			Environment: os.Environ(),
		}))
	}
}

func (m Model) refreshNetwork() tea.Cmd {
	return func() tea.Msg {
		ctx, cancel := context.WithTimeout(m.lifecycle, 8*time.Second)
		defer cancel()
		return networkMsg(network.Inspect(ctx))
	}
}

func (m Model) Update(msg tea.Msg) (tea.Model, tea.Cmd) {
	switch value := msg.(type) {
	case tea.WindowSizeMsg:
		m.width, m.height = value.Width, value.Height
		return m, nil
	case tickMsg:
		m.refreshSessions()
		return m, m.tick()
	case quotaMsg:
		m.providers = resources.Snapshot(value)
		m.busy = false
		return m, nil
	case machineMsg:
		m.machines = value.Machines
		m.machinesCheckedAt = value.Checked
		m.busy = false
		return m, nil
	case networkMsg:
		m.network = network.Status(value)
		m.busy = false
		return m, nil
	case projectWorkMsg:
		m.projectWork = mcpclient.Snapshot(value)
		if m.workMode != nil {
			m.workMode.adopt(m.projectWork)
		}
		m.busy = false
		return m, nil
	case actionMsg:
		m.busy = false
		m.providers = resources.Inspect()
		m.refreshSessions()
		if value.err != nil {
			m.setMessage("ошибка: " + value.err.Error())
		} else {
			m.setMessage(value.text)
		}
		return m, nil
	case machineConnectMsg:
		m.busy = false
		if value.err != nil {
			m.setMessage("не удалось открыть " + value.machine.Title + ": " + value.err.Error())
			return m, nil
		}
		for i := range m.machines {
			if m.machines[i].ID == value.connection.Machine.ID {
				m.machines[i] = value.connection.Machine
				break
			}
		}
		return m.attachResolvedMachine(value.connection)
	case formRouteMsg:
		m.busy = false
		if value.err != nil {
			m.setMessage("не удалось выбрать маршрут: " + value.err.Error())
			return m, nil
		}
		value.spec.Host = value.connection.Route.Target
		value.spec.SSHOptions = append([]string(nil), value.connection.SSHOptions...)
		return m.startLaunch(value.spec, value.logicalHost)
	case workModeFinishedMsg:
		m.busy = false
		if value.err != nil {
			m.setMessage(value.label + ": " + value.err.Error())
		} else {
			m.setMessage(value.label + " завершён")
		}
		m.busy = true
		return m, m.refreshProjectWork()
	case sessionFinishedMsg:
		m.busy = false
		snapshot := m.store.Snapshot()
		if localContext := snapshot.LocalContextByID(value.id); localContext != nil {
			localContext.State = domain.SessionDetached
			if value.err != nil {
				localContext.State = domain.SessionFailed
				localContext.ExitCode = 1
			}
			localContext.UpdatedAt = time.Now()
			_ = m.store.UpsertLocalContext(*localContext)
		}
		m.refreshSessions()
		if value.err != nil {
			m.setMessage("локальный терминал завершился: " + value.err.Error())
		} else {
			m.setMessage("локальный терминал отсоединён; процесс продолжает работу")
		}
		return m, nil
	case tea.KeyMsg:
		return m.key(value)
	}
	return m, nil
}

func (m Model) key(key tea.KeyMsg) (tea.Model, tea.Cmd) {
	if m.confirm != nil {
		return m.confirmKey(key)
	}
	if m.multi != nil {
		return m.multiKey(key)
	}
	if m.search != nil {
		return m.searchKey(key)
	}
	if m.workMode != nil {
		return m.workModeKey(key)
	}
	if m.form != nil {
		return m.formKey(key)
	}
	if m.showHelp {
		switch key.String() {
		case "?", "esc", "q":
			m.showHelp = false
		}
		return m, nil
	}
	switch key.String() {
	case "ctrl+c", "q":
		m.quitting = true
		if m.cancel != nil {
			m.cancel()
		}
		return m, tea.Quit
	case "?":
		m.showHelp = true
		return m, nil
	case "tab", "right", "l":
		return m, m.setTab((m.tab + 1) % tabCount)
	case "shift+tab", "left":
		return m, m.setTab((m.tab + tabCount - 1) % tabCount)
	case "1":
		return m, m.setTab(workTab)
	case "2":
		return m, m.setTab(resourcesTab)
	case "3":
		return m, m.setTab(machinesTab)
	case "/":
		m.search = &searchState{}
		return m, nil
	case "p":
		return m.switchProject(1)
	case "P":
		return m.switchProject(-1)
	case "n":
		m.form = newLaunchForm(m.project.Path, m.store.Snapshot().LaunchDrafts)
		m.form.accounts = append([]resources.Account(nil), m.providers.Accounts...)
		m.form.machines = append([]machines.Machine(nil), m.machines...)
		return m, nil
	case "h":
		if m.tab == workTab {
			return m.openWorkMode(launch.KindHands)
		}
	case "f":
		if m.tab == workTab {
			return m.openWorkMode(launch.KindWorkroom)
		}
	case "o":
		if m.tab == resourcesTab {
			m.showRawQuota = !m.showRawQuota
			return m, nil
		}
	case "r":
		return m, m.refreshCurrent()
	case "up", "k":
		m.move(-1)
	case "down", "j":
		m.move(1)
	case "home", "g":
		m.cursor = 0
	case "end", "G":
		m.cursor = m.maxCursor()
	case "enter":
		return m.activate()
	case "s":
		if m.tab == resourcesTab {
			return m.accountConfirm("switch")
		}
		return m.stopConfirm()
	case "b":
		if m.tab == resourcesTab {
			return m.accountConfirm("bind")
		}
	case "w":
		if m.tab == machinesTab {
			return m.openMachineVia("lan")
		}
	case "u":
		if m.tab == machinesTab {
			return m, m.vpnAction("up")
		}
	case "d":
		if m.tab == machinesTab {
			return m.networkConfirm("down")
		}
	}
	return m, nil
}

func (m Model) maxCursor() int {
	switch m.tab {
	case workTab:
		// Two explicit work actions, then the recent resumable sessions, then
		// durable local terminal contexts and one final "new launch" action.
		return workActionCount + len(m.recentSessions()) + len(m.items)
	case resourcesTab:
		return max(0, len(m.providers.Providers)+len(m.providers.Accounts)-1)
	case machinesTab:
		return max(0, len(m.machines)-1)
	default:
		return 0
	}
}

func (m *Model) move(delta int) {
	limit := m.maxCursor()
	m.cursor += delta
	if m.cursor < 0 {
		m.cursor = 0
	}
	if m.cursor > limit {
		m.cursor = limit
	}
}

func (m Model) activate() (tea.Model, tea.Cmd) {
	switch m.tab {
	case workTab:
		recent := m.recentSessions()
		localStart := workActionCount + len(recent)
		switch {
		case m.cursor == 0:
			return m.openWorkMode(launch.KindHands)
		case m.cursor == 1:
			return m.openWorkMode(launch.KindWorkroom)
		case m.cursor < localStart:
			return m.openRecent(recent[m.cursor-workActionCount])
		case m.cursor < localStart+len(m.items):
			session := m.items[m.cursor-localStart]
			if session.State == domain.SessionFinished || session.State == domain.SessionFailed {
				m.setMessage("живого процесса нет; n — запустить новую работу")
				return m, nil
			}
			return m.attach(session)
		default:
			m.form = newLaunchForm(m.project.Path, m.store.Snapshot().LaunchDrafts)
			m.form.accounts = append([]resources.Account(nil), m.providers.Accounts...)
			m.form.machines = append([]machines.Machine(nil), m.machines...)
			return m, nil
		}
	case resourcesTab:
		return m.accountConfirm("switch")
	case machinesTab:
		if m.cursor >= 0 && m.cursor < len(m.machines) {
			machine := m.machines[m.cursor]
			m.busy = true
			return m, m.resolveMachine(machine)
		}
	}
	return m, nil
}

func (m Model) attach(context domain.LocalContext) (tea.Model, tea.Cmd) {
	command := m.sessions.Attach(context)
	m.busy = true
	return m, tea.Exec(command, func(err error) tea.Msg { return sessionFinishedMsg{id: context.ID, err: err} })
}

func (m Model) attachCommand(context domain.LocalContext, entry registry.Resolved) (tea.Model, tea.Cmd) {
	command := runner.NewExecCommand(entry, runner.Options{Env: os.Environ(), Dir: context.ProjectPath})
	m.busy = true
	return m, tea.Exec(command, func(err error) tea.Msg { return sessionFinishedMsg{id: context.ID, err: err} })
}

// openWorkMode opens the native Hands/Fixer screen. No legacy line selector
// is entered: every choice below is collected here and passed to the work
// client as an explicit preset.
func (m Model) openWorkMode(kind string) (tea.Model, tea.Cmd) {
	m.workMode = m.newWorkModeForm(kind)
	return m, nil
}

func (m Model) newWorkModeForm(kind string) *workModeForm {
	form := &workModeForm{
		kind:      kind,
		project:   m.project.Path,
		workspace: "safe",
		handsMCP:  "keep",
		handsDocs: "keep",
		mcpPreset: "keep",
		docPreset: "keep",
		action:    "new",
	}
	// Lane stays empty until Fixer MCP tells us which lanes this project has
	// registered; guessing here is what produced the `pi` crash.
	form.adopt(m.projectWork)
	return form
}

func (m Model) workModeKey(key tea.KeyMsg) (tea.Model, tea.Cmd) {
	form := m.workMode
	fields := form.fieldCount()
	if fields < 1 {
		fields = 1
	}
	switch key.String() {
	case "esc", "q":
		m.workMode = nil
		return m, nil
	case "up", "k":
		form.focus = (form.focus + fields - 1) % fields
	case "down", "j", "tab":
		form.focus = (form.focus + 1) % fields
	case "left", "h", "right", "l", "space":
		delta := 1
		if key.String() == "left" || key.String() == "h" {
			delta = -1
		}
		form.cycle(delta)
	case "enter":
		if target, ok := form.pickerTarget(); ok {
			return m.openHandsPicker(target)
		}
		return m.startWorkMode()
	case "x":
		if target, ok := form.pickerTarget(); ok {
			return m.openHandsPicker(target)
		}
	}
	return m, nil
}

func (f *workModeForm) cycle(delta int) {
	if f.kind == launch.KindHands {
		switch f.focus {
		case 0:
			f.workspace = cycleValue(f.workspace, []string{"safe", "hotfix"}, delta)
		case 1:
			f.lane = cycleValue(f.lane, laneProviders(f.lanes), delta)
		case 2:
			f.mcpPreset = cycleValue(f.mcpPreset, []string{"keep", "none", "all"}, delta)
			f.mcpTouched = true
		case 3:
			f.docPreset = cycleValue(f.docPreset, []string{"keep", "none", "all"}, delta)
			f.docTouched = true
		}
		return
	}
	switch f.focus {
	case 0:
		f.action = cycleValue(f.action, []string{"new", "resume", "unattached"}, delta)
		if f.action != "resume" {
			f.focus = 0
		}
	case 1:
		if len(f.resumeLabels) > 0 {
			f.resumeIdx = (f.resumeIdx + delta + len(f.resumeLabels)) % len(f.resumeLabels)
		}
	}
}

func (m Model) startWorkMode() (tea.Model, tea.Cmd) {
	form := m.workMode
	label := "Руки"
	spec := launch.Spec{
		Kind:        form.kind,
		ProjectPath: form.project,
		Lane:        form.lane,
		Workspace:   form.workspace,
		HandsMCP:    form.mcpValue(),
		HandsDocs:   form.docValue(),
		FixerLaunch: form.action,
	}
	if form.kind == launch.KindWorkroom {
		label = "Фиксер"
		if form.action == "resume" {
			if form.resumeIdx < 0 || form.resumeIdx >= len(form.resumeIDs) {
				m.setMessage("для resume нет доступных сессий — выберите new")
				return m, nil
			}
			spec.FixerSession = form.resumeIDs[form.resumeIdx]
			// Resume where the session ran: otherwise the provider interrupts the
			// launch to ask "session directory or current directory".
			spec.ProjectPath = form.resumeTarget(spec.ProjectPath)
		}
	} else if laneIndex(form.lanes, form.lane) < 0 {
		// Never fall back to a guessed lane: the launcher validates it against
		// the registered set and a wrong guess fails deep inside the wire.
		m.setMessage("линии Project Hands ещё не получены — подождите обновления MCP (r)")
		return m, nil
	}
	command, err := launch.Build(spec, m.opts.RuntimeRoot)
	if err != nil {
		m.setMessage(err.Error())
		return m, nil
	}
	m.workMode = nil
	entry := registry.Resolved{
		Entry:  registry.Entry{ID: "work-" + form.kind, Title: label, Args: command.Args},
		Binary: command.Binary,
	}
	m.busy = true
	process := runner.NewExecCommand(entry, runner.Options{Env: m.workEnvironment(command.Env), Dir: command.Dir})
	return m, tea.Exec(process, func(err error) tea.Msg { return workModeFinishedMsg{label: label, err: err} })
}

func (f *workModeForm) pickerTarget() (string, bool) {
	if f.kind != launch.KindHands {
		return "", false
	}
	switch f.focus {
	case 2:
		return "mcp", true
	case 3:
		return "docs", true
	default:
		return "", false
	}
}

// multiSelectState is the native checkbox overlay used for MCP servers and
// project documents. It replaces the legacy line-input multi selectors, so
// arrow keys move a cursor instead of leaking escape sequences.
type multiSelectState struct {
	target  string
	title   string
	values  []string
	labels  []string
	checked []bool
	cursor  int
}

func (s *multiSelectState) selectedCount() int {
	count := 0
	for _, value := range s.checked {
		if value {
			count++
		}
	}
	return count
}

func (m Model) openHandsPicker(target string) (tea.Model, tea.Cmd) {
	form := m.workMode
	if form == nil {
		return m, nil
	}
	picker := &multiSelectState{target: target}
	switch target {
	case "mcp":
		if len(form.mcpPool) == 0 {
			m.setMessage("пул MCP-серверов ещё не получен из MCP — подождите обновления (r)")
			return m, nil
		}
		picker.title = "MCP-серверы для Рук"
		selected := make(map[string]bool, len(form.mcpSel))
		for _, name := range form.mcpSel {
			selected[name] = true
		}
		for _, server := range form.mcpPool {
			if server.Archived {
				continue
			}
			picker.values = append(picker.values, server.Name)
			label := server.Name
			if server.ShortDescription != "" {
				label += " — " + compactText(server.ShortDescription, 64)
			} else if server.Category != "" {
				label += " — " + server.Category
			}
			picker.labels = append(picker.labels, label)
			picker.checked = append(picker.checked, selected[server.Name])
		}
	case "docs":
		if len(form.docPool) == 0 {
			m.setMessage("пул документов проекта ещё не получен из MCP — подождите обновления (r)")
			return m, nil
		}
		picker.title = "Документы проекта для Рук"
		selected := make(map[int]bool, len(form.docSel))
		for _, id := range form.docSel {
			selected[id] = true
		}
		for _, doc := range form.docPool {
			picker.values = append(picker.values, strconv.Itoa(doc.DocID))
			title := firstNonEmpty(doc.Title, doc.Path, "без названия")
			picker.labels = append(picker.labels, strings.Repeat("  ", max(0, doc.Level))+compactText(title, 74))
			picker.checked = append(picker.checked, selected[doc.DocID])
		}
	default:
		return m, nil
	}
	if len(picker.values) == 0 {
		m.setMessage("нет кандидатов для выбора")
		return m, nil
	}
	m.multi = picker
	return m, nil
}

func (m Model) multiKey(key tea.KeyMsg) (tea.Model, tea.Cmd) {
	picker := m.multi
	switch key.String() {
	case "esc", "q":
		m.multi = nil
		return m, nil
	case "up", "k":
		if picker.cursor > 0 {
			picker.cursor--
		}
	case "down", "j":
		if picker.cursor < len(picker.values)-1 {
			picker.cursor++
		}
	case "home", "g":
		picker.cursor = 0
	case "end", "G":
		picker.cursor = len(picker.values) - 1
	case "space", " ", "x":
		picker.checked[picker.cursor] = !picker.checked[picker.cursor]
	case "a":
		allOn := picker.selectedCount() < len(picker.checked)
		for i := range picker.checked {
			picker.checked[i] = allOn
		}
	case "enter":
		return m.applyPicker()
	}
	return m, nil
}

func (m Model) applyPicker() (tea.Model, tea.Cmd) {
	picker := m.multi
	form := m.workMode
	m.multi = nil
	if picker == nil || form == nil {
		return m, nil
	}
	chosen := make([]string, 0, len(picker.values))
	for i, checked := range picker.checked {
		if checked {
			chosen = append(chosen, picker.values[i])
		}
	}
	switch picker.target {
	case "mcp":
		form.mcpTouched = true
		if len(chosen) == 0 {
			form.mcpPreset, form.mcpSel = "none", nil
		} else {
			form.mcpPreset, form.mcpSel = "custom", chosen
		}
	case "docs":
		form.docTouched = true
		ids := make([]int, 0, len(chosen))
		for _, value := range chosen {
			if id, err := strconv.Atoi(value); err == nil {
				ids = append(ids, id)
			}
		}
		if len(ids) == 0 {
			form.docPreset, form.docSel = "none", nil
		} else {
			form.docPreset, form.docSel = "custom", ids
		}
	}
	m.workMode = form
	return m, nil
}

func (m Model) multiView() string {
	picker := m.multi
	var b strings.Builder
	b.WriteString(accent.Render("  "+picker.title) + muted.Render("  нативный выбор") + "\n\n")

	window := 14
	top := 0
	if picker.cursor >= window {
		top = picker.cursor - window + 1
	}
	end := min(len(picker.values), top+window)
	for i := top; i < end; i++ {
		prefix := "   "
		if i == picker.cursor {
			prefix = selectStyle.Render(" ▸ ")
		}
		mark := "[ ]"
		if picker.checked[i] {
			mark = good.Render("[x]")
		}
		b.WriteString(prefix + mark + " " + picker.labels[i] + "\n")
	}
	if top > 0 || end < len(picker.values) {
		b.WriteString(muted.Render(fmt.Sprintf("   … показаны %d–%d из %d", top+1, end, len(picker.values))) + "\n")
	}
	b.WriteString("\n" + muted.Render(fmt.Sprintf("   выбрано: %d", picker.selectedCount())) + "\n")
	b.WriteString("\n" + panel.Render("  space/x — вкл/выкл · a — все · Enter — готово · Esc — назад") + "\n")
	return b.String()
}

func (m Model) workModeView() string {
	form := m.workMode
	title := "Руки — Project Hands"
	if form.kind == launch.KindWorkroom {
		title = "Фиксер — Fixer"
	}
	var b strings.Builder
	b.WriteString(accent.Render("  "+title) + muted.Render("  нативный выбор · без legacy-промптов") + "\n\n")
	b.WriteString("  " + fmt.Sprintf("%-14s %s\n", "Проект", filepath.Base(form.project)))

	var fields []struct{ label, value string }
	if form.kind == launch.KindHands {
		workspaceLabel := "Safe — изолированный worktree"
		if form.workspace == "hotfix" {
			workspaceLabel = "Hotfix — текущий живой worktree"
		}
		fields = []struct{ label, value string }{
			{"Режим worktree", workspaceLabel},
			{"Lane", form.laneLabel()},
			{"MCP-серверы", form.mcpLabel()},
			{"Документы", form.docLabel()},
		}
	} else {
		actionLabel := map[string]string{
			"new": "Новый Fixer", "resume": "Продолжить Fixer", "unattached": "Unattached Fixer",
		}[form.action]
		fields = append(fields, struct{ label, value string }{"Действие", actionLabel})
		if form.action == "resume" {
			value := "нет доступных сессий — выберите new"
			if form.resumeIdx >= 0 && form.resumeIdx < len(form.resumeLabels) {
				value = form.resumeLabels[form.resumeIdx]
			}
			fields = append(fields, struct{ label, value string }{"Сессия", value})
		}
	}
	for i, field := range fields {
		prefix := "  "
		if i == form.focus {
			prefix = selectStyle.Render("▸ ")
		}
		b.WriteString(prefix + fmt.Sprintf("%-14s %s\n", field.label, field.value))
	}
	if form.kind == launch.KindWorkroom && form.action == "resume" {
		if recorded := form.resumeCWDValue(); recorded != "" {
			suffix := ""
			if filepath.Clean(recorded) != filepath.Clean(form.project) {
				suffix = "  (сессия будет открыта там — иначе клиент спросит, откуда стартовать)"
			}
			b.WriteString(muted.Render("  сессия из: "+recorded+suffix) + "\n")
		}
	}
	if form.kind == launch.KindHands && !form.lanesFromMCP {
		b.WriteString(muted.Render("  MCP недоступен — показаны только линии, сохраняемые в схеме") + "\n")
	}
	b.WriteString("\n" + panel.Render("  Enter — запустить · на MCP/Документах Enter — выбор · ←/→ — keep/none/all · j/k — поле") + "\n")
	return b.String()
}

// laneLabel renders the registered lane together with the model Fixer MCP
// associates with it, so the operator sees what will actually run.
func (f *workModeForm) laneLabel() string {
	if f.lane == "" {
		return "получаю зарегистрированные линии…"
	}
	if index := laneIndex(f.lanes, f.lane); index >= 0 {
		if model := strings.TrimSpace(f.lanes[index].Model); model != "" {
			return f.lane + " · " + model
		}
	}
	return f.lane
}

// resumeTarget picks the directory a Fixer resume must run in: the directory
// the session was recorded in when it still exists, otherwise the current
// project. Resuming elsewhere makes the provider stop and ask which working
// directory to use, which blocks the launch.
func (f *workModeForm) resumeTarget(projectPath string) string {
	recorded := f.resumeCWDValue()
	if recorded == "" || filepath.Clean(recorded) == filepath.Clean(projectPath) {
		return projectPath
	}
	info, err := os.Stat(recorded)
	if err != nil || !info.IsDir() {
		return projectPath
	}
	return recorded
}

// resumeCWDValue returns the directory the selected Fixer session ran in.
func (f *workModeForm) resumeCWDValue() string {
	if f.resumeIdx < 0 || f.resumeIdx >= len(f.resumeCWDs) {
		return ""
	}
	return strings.TrimSpace(f.resumeCWDs[f.resumeIdx])
}

func (m Model) workEnvironment(env []string) []string {
	if m.opts.CanonicalDBPath == "" {
		return env
	}
	result := make([]string, 0, len(env)+1)
	for _, item := range env {
		if !strings.HasPrefix(item, "FIXER_DB_PATH=") {
			result = append(result, item)
		}
	}
	return append(result, "FIXER_DB_PATH="+m.opts.CanonicalDBPath)
}

func (m Model) resolveMachine(machine machines.Machine) tea.Cmd {
	return m.machineResolveCommand(machine, "")
}

// openMachineVia pins the transport: `w` must really go over Wi-Fi/LAN, since
// silently falling back to Tailscale is exactly what the operator is avoiding.
func (m Model) openMachineVia(preferKind string) (tea.Model, tea.Cmd) {
	if m.cursor < 0 || m.cursor >= len(m.machines) {
		m.setMessage("сначала выберите машину")
		return m, nil
	}
	machine := m.machines[m.cursor]
	if machine.Kind == "local" {
		m.setMessage("это локальная машина — маршрут не нужен")
		return m, nil
	}
	m.busy = true
	return m, m.machineResolveCommand(machine, preferKind)
}

func (m Model) machineResolveCommand(machine machines.Machine, preferKind string) tea.Cmd {
	return func() tea.Msg {
		ctx, cancel := context.WithTimeout(m.lifecycle, 20*time.Second)
		defer cancel()
		connection, err := machines.ResolveVia(ctx, machine, preferKind)
		return machineConnectMsg{machine: machine, connection: connection, err: err}
	}
}

func (m Model) attachResolvedMachine(connection machines.Connection) (tea.Model, tea.Cmd) {
	binary := connection.Binary
	args := append([]string(nil), connection.SSHOptions...)
	if connection.Machine.Kind != "local" {
		args = append([]string{"-tt"}, args...)
		args = append(args, connection.Route.Target)
	}
	context := domain.LocalContext{
		ID:          "machine-" + connection.Machine.ID,
		Title:       connection.Machine.Title,
		ProjectPath: m.project.Path,
		Command:     append([]string{binary}, args...),
		State:       domain.SessionRunning,
	}
	return m.attachCommand(context, registry.Resolved{
		Entry:  registry.Entry{ID: context.ID, Title: context.Title, Args: args},
		Binary: binary,
	})
}

func (m Model) accountConfirm(action string) (tea.Model, tea.Cmd) {
	index := m.cursor - len(m.providers.Providers)
	if index < 0 || index >= len(m.providers.Accounts) {
		m.setMessage("сначала выберите аккаунт")
		return m, nil
	}
	account := m.providers.Accounts[index]
	verb := "глобально переключить credentials для новых локальных запусков на"
	if action == "bind" {
		verb = "глобально привязать текущие credentials к"
	}
	m.confirm = &confirmState{title: fmt.Sprintf("%s %s / %s?", verb, account.Client, account.Name), action: action, account: account}
	return m, nil
}

func (m Model) confirmKey(key tea.KeyMsg) (tea.Model, tea.Cmd) {
	switch key.String() {
	case "esc", "n", "q":
		m.confirm = nil
	case "y", "enter":
		confirmation := *m.confirm
		m.confirm = nil
		m.busy = true
		return m, func() tea.Msg {
			var err error
			text := "готово"
			switch confirmation.action {
			case "bind":
				err = resources.BindAccount(confirmation.account)
				text = confirmation.account.Client + " / " + confirmation.account.Name + " привязан"
			case "switch":
				err = resources.SwitchAccount(confirmation.account)
				text = confirmation.account.Client + " / " + confirmation.account.Name + " активирован"
			case "stop":
				err = m.sessions.Stop(confirmation.localContext)
				text = "сессия остановлена"
			case "vpn-down":
				text, err = network.VPNAction(m.lifecycle, "down", "")
			}
			return actionMsg{text: text, err: err}
		}
	}
	return m, nil
}

func (m Model) stopConfirm() (tea.Model, tea.Cmd) {
	localStart := workActionCount + len(m.recentSessions())
	if m.tab != workTab || m.cursor < localStart || m.cursor >= localStart+len(m.items) {
		return m, nil
	}
	session := m.items[m.cursor-localStart]
	m.confirm = &confirmState{title: fmt.Sprintf("остановить локальный терминал %q?", session.Title), action: "stop", localContext: session}
	return m, nil
}

func (m Model) networkConfirm(action string) (tea.Model, tea.Cmd) {
	if action != "down" {
		return m, m.vpnAction(action)
	}
	m.confirm = &confirmState{title: "выключить VPN и изменить маршрут?", action: "vpn-down", networkAct: action}
	return m, nil
}

// setTab switches space and starts the reachability sweep as soon as the
// machines space is opened, so the operator never stares at stale rows.
func (m *Model) setTab(tab int) tea.Cmd {
	changed := m.tab != tab
	m.tab = tab
	if changed {
		m.cursor = 0
	}
	if tab == machinesTab && (changed || m.machinesCheckedAt.IsZero()) {
		return m.probeMachines()
	}
	return nil
}

func (m Model) probeMachines() tea.Cmd {
	machinesNow := append([]machines.Machine(nil), m.machines...)
	return func() tea.Msg {
		ctx, cancel := context.WithTimeout(m.lifecycle, 45*time.Second)
		defer cancel()
		return machineMsg(machines.Probe(ctx, machinesNow))
	}
}

func (m Model) refreshCurrent() tea.Cmd {
	switch m.tab {
	case workTab:
		m.busy = true
		m.refreshSessions()
		return m.refreshProjectWork()
	case resourcesTab:
		m.busy = true
		return m.refreshQuota()
	case machinesTab:
		return m.probeMachines()
	default:
		m.refreshSessions()
		return nil
	}
}

func (m Model) vpnAction(action string) tea.Cmd {
	m.busy = true
	return func() tea.Msg {
		ctx, cancel := context.WithTimeout(m.lifecycle, 45*time.Second)
		defer cancel()
		text, err := network.VPNAction(ctx, action, "us")
		return actionMsg{text: text, err: err}
	}
}

func (m Model) switchProject(delta int) (tea.Model, tea.Cmd) {
	projects := m.store.Snapshot().RecentProjects
	if len(projects) < 2 {
		m.setMessage("сохранён только текущий проект; `fixer open PATH` добавит новый")
		return m, nil
	}
	index := 0
	for i, project := range projects {
		if project.ID == m.project.ID {
			index = i
			break
		}
	}
	index = (index + delta + len(projects)) % len(projects)
	m.project = projects[index]
	m.project.LastOpened = time.Now()
	if err := m.store.RememberProject(m.project); err != nil {
		m.setMessage(err.Error())
		return m, nil
	}
	m.cursor = 0
	m.refreshSessions()
	m.setMessage("проект: " + m.project.Path)
	m.busy = true
	return m, m.refreshProjectWork()
}

func (m *Model) refreshSessions() {
	items, err := m.sessions.Refresh()
	if err != nil {
		m.setMessage(err.Error())
	}
	filtered := make([]domain.LocalContext, 0, len(items))
	projectID := launch.ProjectID(m.project.Path)
	for _, item := range items {
		if item.ProjectID == projectID || item.ProjectPath == m.project.Path {
			filtered = append(filtered, item)
		}
	}
	m.items = filtered
	if m.cursor > m.maxCursor() {
		m.cursor = m.maxCursor()
	}
}

func (m *Model) setMessage(text string) {
	m.message = text
	m.messageAt = time.Now()
}

func newLaunchForm(projectPath string, saved []domain.LaunchDraft) *launchForm {
	profile := launch.DefaultDraft(projectPath)
	for _, item := range saved {
		if item.ProjectID == launch.ProjectID(projectPath) {
			profile = item
			break
		}
	}
	return &launchForm{spec: launch.Spec{Title: profile.Title, Kind: profile.Kind, Provider: profile.Provider, Account: profile.Account, Model: profile.Model, Thinking: profile.Thinking, ProjectPath: projectPath, Host: profile.Host}, profiles: saved}
}

func (m Model) formKey(key tea.KeyMsg) (tea.Model, tea.Cmd) {
	form := m.form
	if form.edit {
		switch key.String() {
		case "esc", "enter":
			form.edit = false
			form.spec.Prompt = form.buf
		case "backspace":
			if len(form.buf) > 0 {
				form.buf = form.buf[:len(form.buf)-1]
			}
		default:
			if text := key.String(); len(text) == 1 && text >= " " && text <= "~" {
				form.buf += text
			}
		}
		return m, nil
	}
	switch key.String() {
	case "esc", "q":
		m.form = nil
	case "up", "k":
		form.focus = (form.focus + 7) % 8
	case "down", "j", "tab":
		form.focus = (form.focus + 1) % 8
	case "left", "h":
		form.cycle(-1)
	case "right", "l", "space":
		form.cycle(1)
	case "x":
		form.edit = true
		form.buf = form.spec.Prompt
	case "enter":
		return m.launchForm()
	}
	return m, nil
}

func (f *launchForm) cycle(delta int) {
	switch f.focus {
	case 0:
		values := []string{launch.KindAgent, launch.KindHands, launch.KindWorkroom, launch.KindTerminal}
		f.spec.Kind = cycleValue(f.spec.Kind, values, delta)
	case 1:
		f.spec.Provider = cycleValue(f.spec.Provider, launch.Providers, delta)
		if models := launch.Models[f.spec.Provider]; len(models) > 0 {
			f.spec.Model = models[0]
		}
	case 2:
		values := launch.Models[f.spec.Provider]
		if len(values) > 0 {
			f.spec.Model = cycleValue(f.spec.Model, values, delta)
		}
	case 3:
		f.spec.Thinking = cycleValue(f.spec.Thinking, launch.Thinkings, delta)
	case 6:
		if len(f.machines) > 0 {
			values := make([]string, 0, len(f.machines))
			for _, machine := range f.machines {
				values = append(values, machine.ID)
			}
			selected := f.spec.Host
			if selected == "" {
				selected = "local"
			}
			selected = cycleValue(selected, values, delta)
			for _, machine := range f.machines {
				if machine.ID == selected {
					if machine.Kind == "local" {
						f.spec.Host, f.spec.RemotePath = "", ""
					} else {
						// Persist the logical ID, never a transport alias. The
						// selected route is resolved only at launch time.
						f.spec.Host, f.spec.RemotePath = machine.ID, machine.Path
					}
					break
				}
			}
		}
	case 7:
		if len(f.accounts) > 0 {
			values := make([]string, 0, len(f.accounts))
			for _, account := range f.accounts {
				values = append(values, account.Name)
			}
			f.spec.Account = cycleValue(f.spec.Account, values, delta)
		}
	}
}

func cycleValue(current string, values []string, delta int) string {
	if len(values) == 0 {
		return current
	}
	index := 0
	for i, value := range values {
		if value == current {
			index = i
			break
		}
	}
	index = (index + delta + len(values)) % len(values)
	return values[index]
}

func (m Model) launchForm() (tea.Model, tea.Cmd) {
	form := m.form
	spec := form.spec
	if spec.Kind == launch.KindHands || spec.Kind == launch.KindWorkroom {
		// Руки и Фиксер имеют собственный нативный экран. Их выбор никогда не
		// уходит в legacy-промпты: launch-карточка только выбирает режим.
		m.form = nil
		return m.openWorkMode(spec.Kind)
	}
	if spec.Title == "" {
		spec.Title = "Новая работа"
	}

	logicalHost := spec.Host
	if machine, ok := machineByID(form.machines, logicalHost); ok && machine.Kind != "local" {
		if spec.Kind == launch.KindHands || spec.Kind == launch.KindWorkroom {
			m.setMessage("для Рук и Фиксера сначала откройте выбранную машину в разделе Машины")
			return m, nil
		}
		if strings.TrimSpace(machine.Path) == "" {
			m.setMessage("для удалённого запуска нужен канонический путь проекта; откройте машину и запустите там Руки")
			return m, nil
		}
		m.busy = true
		return m, func() tea.Msg {
			ctx, cancel := context.WithTimeout(m.lifecycle, 16*time.Second)
			defer cancel()
			connection, err := machines.Resolve(ctx, machine)
			return formRouteMsg{spec: spec, logicalHost: logicalHost, connection: connection, err: err}
		}
	}
	return m.startLaunch(spec, logicalHost)
}

func (m Model) startLaunch(spec launch.Spec, logicalHost string) (tea.Model, tea.Cmd) {
	command, err := launch.Build(spec, m.opts.RuntimeRoot)
	if err != nil {
		m.form.spec = spec
		m.setMessage(err.Error())
		return m, nil
	}
	profile := domain.LaunchDraft{
		ID: "project-" + launch.ProjectID(spec.ProjectPath), Title: spec.Title,
		Kind: spec.Kind, Provider: spec.Provider, Account: spec.Account,
		Model: spec.Model, Thinking: spec.Thinking, Host: logicalHost,
		ProjectID: launch.ProjectID(spec.ProjectPath), Permissions: "approve", MCPMode: "project",
	}
	if err := m.store.SaveLaunchDraft(profile); err != nil {
		m.setMessage(err.Error())
		return m, nil
	}
	session, err := m.sessions.Start(spec, command)
	if err != nil {
		m.setMessage(err.Error())
		return m, nil
	}
	m.form = nil
	m.refreshSessions()
	return m, tea.Exec(m.sessions.Attach(session), func(runErr error) tea.Msg { return sessionFinishedMsg{id: session.ID, err: runErr} })
}

func machineByID(items []machines.Machine, id string) (machines.Machine, bool) {
	for _, item := range items {
		if item.ID == id {
			return item, true
		}
	}
	return machines.Machine{}, false
}

func (m Model) searchKey(key tea.KeyMsg) (tea.Model, tea.Cmd) {
	search := m.search
	switch key.String() {
	case "esc":
		m.search = nil
	case "backspace":
		if len(search.query) > 0 {
			search.query = search.query[:len(search.query)-1]
		}
	case "enter":
		choice := searchTarget(search.query)
		m.search = nil
		if choice >= 0 {
			return m, m.setTab(choice)
		}
	default:
		text := key.String()
		if len(text) == 1 && text >= " " && text <= "~" {
			search.query += text
		}
	}
	return m, nil
}

func searchTarget(query string) int {
	query = strings.ToLower(strings.TrimSpace(query))
	switch {
	case strings.Contains(query, "work"), strings.Contains(query, "сесс"), strings.Contains(query, "fixer"):
		return workTab
	case strings.Contains(query, "quota"), strings.Contains(query, "лимит"), strings.Contains(query, "account"), strings.Contains(query, "аккаун"):
		return resourcesTab
	case strings.Contains(query, "ssh"), strings.Contains(query, "machine"), strings.Contains(query, "маш"),
		strings.Contains(query, "vpn"), strings.Contains(query, "network"), strings.Contains(query, "сеть"), strings.Contains(query, "ip"):
		return machinesTab
	default:
		return -1
	}
}

// isNonProjectDirectory reports paths that are shells (home, root) rather than
// work directories. There, reopening the remembered project beats binding MCP
// to a folder that was never registered.
func isNonProjectDirectory(path string) bool {
	if strings.TrimSpace(path) == "" {
		return true
	}
	clean := filepath.Clean(path)
	if clean == string(filepath.Separator) || clean == "." {
		return true
	}
	home, err := os.UserHomeDir()
	if err != nil {
		return false
	}
	cleanHome := filepath.Clean(home)
	if clean == cleanHome {
		return true
	}
	// macOS hands out `/var/...` from $HOME and `/private/var/...` from
	// getwd; comparing them raw would treat home as a real project.
	resolvedPath, pathErr := filepath.EvalSymlinks(clean)
	resolvedHome, homeErr := filepath.EvalSymlinks(cleanHome)
	return pathErr == nil && homeErr == nil && resolvedPath == resolvedHome
}

func discoverProject(start string) string {
	if path, err := exec.Command("git", "-C", start, "rev-parse", "--show-toplevel").Output(); err == nil {
		return strings.TrimSpace(string(path))
	}
	if absolute, err := filepath.Abs(start); err == nil {
		return absolute
	}
	return start
}

func environmentMap() map[string]string {
	out := map[string]string{}
	for _, item := range os.Environ() {
		parts := strings.SplitN(item, "=", 2)
		if len(parts) == 2 {
			out[parts[0]] = parts[1]
		}
	}
	return out
}

func firstNonEmpty(values ...string) string {
	for _, value := range values {
		if strings.TrimSpace(value) != "" {
			return value
		}
	}
	return ""
}

func max(a, b int) int {
	if a > b {
		return a
	}
	return b
}

var (
	accent      = lipgloss.NewStyle().Bold(true).Foreground(lipgloss.Color("212"))
	muted       = lipgloss.NewStyle().Foreground(lipgloss.Color("244"))
	good        = lipgloss.NewStyle().Foreground(lipgloss.Color("42"))
	bad         = lipgloss.NewStyle().Foreground(lipgloss.Color("196"))
	warn        = lipgloss.NewStyle().Foreground(lipgloss.Color("220"))
	selectStyle = lipgloss.NewStyle().Bold(true).Foreground(lipgloss.Color("213"))
	panel       = lipgloss.NewStyle().Border(lipgloss.RoundedBorder()).BorderForeground(lipgloss.Color("238")).Padding(0, 1)
)

func (m Model) View() string {
	if m.quitting {
		return ""
	}
	var b strings.Builder
	b.WriteString(m.header())
	if m.confirm != nil {
		b.WriteString(m.confirmView())
	} else if m.showHelp {
		b.WriteString(m.helpView())
	} else if m.multi != nil {
		b.WriteString(m.multiView())
	} else if m.workMode != nil {
		b.WriteString(m.workModeView())
	} else if m.form != nil {
		b.WriteString(m.formView())
	} else if m.search != nil {
		b.WriteString(m.searchView())
	} else {
		switch m.tab {
		case workTab:
			b.WriteString(m.workView())
		case resourcesTab:
			b.WriteString(m.resourcesView())
		case machinesTab:
			b.WriteString(m.machinesView())
		}
	}
	b.WriteString("\n")
	if m.message != "" {
		b.WriteString(muted.Render("  "+m.message) + "\n")
	}
	b.WriteString(muted.Render(m.footer()))
	return m.clip(b.String())
}

func (m Model) header() string {
	name := "fixer"
	version := m.opts.Version
	if version == "" {
		version = "dev"
	}
	project := filepath.Base(m.project.Path)
	if project == "." || project == string(filepath.Separator) {
		project = m.project.Path
	}
	status := "маршрут не проверен"
	if m.network.State == "online" {
		status = "маршрут ✓"
	} else if m.network.State == "configured" {
		status = "маршрут настроен"
	}
	tabs := []string{"Работа", "Ресурсы", "Машины"}
	var rendered []string
	for i, tab := range tabs {
		label := fmt.Sprintf("%d %s", i+1, tab)
		if i == m.tab {
			label = selectStyle.Render("[" + label + "]")
		} else {
			label = muted.Render(" " + label + " ")
		}
		rendered = append(rendered, label)
	}
	return accent.Render(strings.ToUpper(name)) + muted.Render("  "+version+"   "+runtime.GOOS+"/"+runtime.GOARCH) + "\n" +
		muted.Render("  "+project+"  ·  "+status+"  ·  "+m.network.Egress) + "\n" +
		strings.Join(rendered, "") + "\n" + strings.Repeat("─", max(60, m.width-1)) + "\n"
}

func (m Model) workView() string {
	var b strings.Builder
	b.WriteString(accent.Render("  Проектная работа") + muted.Render("  "+m.project.Name) + "\n")
	b.WriteString(muted.Render("  "+m.project.Path+"  ·  p/P — другой сохранённый проект") + "\n")
	if line := m.strayDBLine(); line != "" {
		b.WriteString(warn.Render("  "+line) + "\n")
	}
	b.WriteString("\n")
	// The entry points stay above diagnostics and history: project work must
	// be actionable even when MCP has a long session list.
	b.WriteString(m.workActionLine(0, "Руки", "проектный исполнитель: продолжить или дать новую инструкцию") + "\n")
	b.WriteString(m.workActionLine(1, "Фиксер", "управление работой, волны, review и контекст проекта") + "\n")
	b.WriteString(m.canonicalWorkSummary())

	recent := m.recentSessions()
	localStart := workActionCount + len(recent)
	if len(recent) > 0 {
		b.WriteString("\n" + muted.Render("  Последние сессии · вход с сохранением MCP-серверов") + "\n")
		for i, row := range recent {
			b.WriteString(m.recentSessionLine(workActionCount+i, row) + "\n")
		}
	} else {
		b.WriteString("\n" + muted.Render("  Последних сессий для быстрого входа пока нет") + "\n")
	}
	b.WriteString("\n")

	if len(m.items) > 0 {
		b.WriteString(muted.Render("  Локальные терминалы (не канонические сессии)") + "\n")
		for i, item := range m.items {
			b.WriteString(m.sessionLine(localStart+i, item) + "\n")
		}
		b.WriteString("\n")
	} else {
		b.WriteString(muted.Render("  Локальных терминалов нет. Руки и Фиксер выше доступны для этого проекта сразу.") + "\n\n")
	}

	newIndex := localStart + len(m.items)
	prefix := "  "
	if m.cursor == newIndex {
		prefix = selectStyle.Render("▸ ")
	}
	b.WriteString(prefix + good.Render("＋ Новый локальный запуск") + muted.Render("  (не создаёт MCP-сессию)") + "\n\n")
	switch {
	case len(recent) > 0 && m.cursor >= workActionCount && m.cursor < localStart:
		b.WriteString(panel.Render(m.recentSessionDetail(recent[m.cursor-workActionCount])) + "\n")
	case m.cursor >= localStart && m.cursor < localStart+len(m.items):
		b.WriteString(panel.Render(m.sessionDetail(m.items[m.cursor-localStart])) + "\n")
	}
	return b.String()
}

// openRecent pre-fills a native work screen so resuming is two keystrokes and
// never re-enters a legacy selector. The pinned values survive later MCP
// refreshes, which is what keeps the originally attached MCP servers.
func (m Model) openRecent(row recentSession) (tea.Model, tea.Cmd) {
	if row.actor == "Руки" {
		form := m.newWorkModeForm(launch.KindHands)
		form.pinnedLane = row.lane
		form.adopt(m.projectWork)
		m.workMode = form
		return m, nil
	}
	form := m.newWorkModeForm(launch.KindWorkroom)
	form.action = "resume"
	form.resumeLabels = []string{row.title}
	form.resumeIDs = []string{row.resume}
	form.resumeCWDs = []string{row.cwd}
	form.resumeIdx = 0
	form.pinnedResume = true
	m.workMode = form
	return m, nil
}

// strayDBLine surfaces databases the canonical resolver deliberately ignored.
// A stray must never be bound silently: the operator sees the path and the one
// explicit override that adopts it.
func (m Model) strayDBLine() string {
	if len(m.strayDBs) == 0 {
		return ""
	}
	return "посторонний fixer.db игнорируется: " + strings.Join(m.strayDBs, ", ") +
		" · чтобы взять его явно, задайте FIXER_DB_PATH=" + m.strayDBs[0]
}

func (m Model) workActionLine(index int, title, detail string) string {
	prefix := "  "
	if m.cursor == index {
		prefix = selectStyle.Render("▸ ")
	}
	return prefix + good.Render("● "+title) + muted.Render("  "+detail)
}

func (m Model) canonicalWorkSummary() string {
	work := m.projectWork
	if !work.Available {
		if strings.TrimSpace(work.Error) == "" {
			return "\n" + muted.Render("  Загружаю канонический контекст проекта через MCP…") + "\n"
		}
		return "\n" + muted.Render("  Контекст MCP пока недоступен: "+compactText(work.Error, 150)) + "\n"
	}

	var b strings.Builder
	b.WriteString("\n")
	if overview := compactText(work.Overview, 180); overview != "" {
		b.WriteString(muted.Render("  "+overview) + "\n")
	}
	// Hands status is deliberately not shown here: the operator enters Руки
	// explicitly, and the home screen trades diagnostics for resumable work.
	// Canonical MCP session history moved out of the way too: the home screen
	// lists the sessions that can actually be resumed instead.
	return b.String()
}

// recentSession is one resumable row on the home screen. Фиксер rows resume a
// provider transcript; Руки rows continue the permanent channel with the MCP
// servers and documents it was configured with.
type recentSession struct {
	actor  string
	title  string
	meta   string
	stamp  string
	resume string
	lane   string
	// cwd is the directory the session was recorded in; resuming elsewhere
	// makes Codex stop and ask which working directory to use.
	cwd string
}

func fixerResumeValue(session mcpclient.FixerSession) string {
	id := strings.TrimSpace(session.SessionID)
	provider := strings.ToLower(strings.TrimSpace(session.Provider))
	if provider == "" || provider == "codex" {
		return id
	}
	return provider + ":" + id
}

func (m Model) recentFixerRows() []recentSession {
	limit := 5
	if m.height > 0 && m.height < 32 {
		limit = 3
	}
	rows := make([]recentSession, 0, limit)
	for _, session := range m.projectWork.FixerSessions {
		if len(rows) >= limit {
			break
		}
		if strings.TrimSpace(session.SessionID) == "" {
			continue
		}
		rows = append(rows, recentSession{
			actor:  "Фиксер",
			title:  compactText(strings.Join(strings.Fields(session.Preview), " "), 74),
			meta:   strings.Trim(strings.Join([]string{session.Provider, session.Model}, " · "), " ·"),
			stamp:  session.Updated,
			resume: fixerResumeValue(session),
			cwd:    strings.TrimSpace(session.CWD),
		})
	}
	return rows
}

func (m Model) recentHandsRows() []recentSession {
	limit := 5
	if m.height > 0 && m.height < 32 {
		limit = 3
	}
	rows := make([]recentSession, 0, limit)
	for _, instruction := range m.projectWork.HandsInstructions {
		if len(rows) >= limit {
			break
		}
		rows = append(rows, recentSession{
			actor: "Руки",
			title: compactText(strings.Join(strings.Fields(instruction.Text), " "), 74),
			meta:  strings.Trim(strings.Join([]string{instruction.Lane, instruction.State}, " · "), " ·"),
			stamp: instruction.CreatedAt,
			lane:  instruction.Lane,
		})
	}
	return rows
}

func (m Model) recentSessions() []recentSession {
	rows := append([]recentSession(nil), m.recentFixerRows()...)
	return append(rows, m.recentHandsRows()...)
}

func (m Model) recentSessionLine(index int, row recentSession) string {
	prefix := "  "
	if index == m.cursor {
		prefix = selectStyle.Render("▸ ")
	}
	actor := good.Render(fmt.Sprintf("%-7s", row.actor))
	meta := muted.Render(fmt.Sprintf("%-26s", compactText(row.meta, 26)))
	stamp := muted.Render(compactText(row.stamp, 19))
	return prefix + actor + " " + meta + " " + stamp + "  " + row.title
}

func (m Model) recentSessionDetail(row recentSession) string {
	if row.actor == "Фиксер" {
		return strings.Join([]string{
			"  " + accent.Render("Фиксер · продолжить сессию"),
			"  сессия: " + row.resume,
			"  клиент: " + row.meta,
			"  Enter — открыть нативный экран с этой сессией; её MCP-серверы сохраняются.",
		}, "\n")
	}
	return strings.Join([]string{
		"  " + accent.Render("Руки · продолжить канал"),
		"  линия: " + firstNonEmpty(row.lane, "по умолчанию проекта"),
		"  Enter — открыть нативный экран; MCP-серверы и документы остаются как были.",
	}, "\n")
}

func compactText(value string, limit int) string {
	value = strings.Join(strings.Fields(strings.TrimSpace(value)), " ")
	if limit <= 0 {
		return ""
	}
	runes := []rune(value)
	if len(runes) <= limit {
		return value
	}
	return string(runes[:limit-1]) + "…"
}

func (m Model) sessionLine(index int, item domain.LocalContext) string {
	prefix := "  "
	if index == m.cursor {
		prefix = selectStyle.Render("▸ ")
	}
	state := string(item.State)
	style := good
	if item.State == domain.SessionFailed {
		style = bad
	}
	if item.State == domain.SessionFinished {
		style = muted
	}
	meta := strings.TrimSpace(strings.Join([]string{item.Provider, item.Model, item.Host}, " · "))
	return prefix + style.Render("● "+item.Title) + muted.Render("  "+state+"  "+meta)
}

func (m Model) sessionDetail(item domain.LocalContext) string {
	lines := []string{"  " + accent.Render(item.Title), "  состояние: " + string(item.State), "  проект: " + item.ProjectPath}
	if item.Provider != "" {
		lines = append(lines, "  маршрут: "+item.Provider+" / "+item.Model+" / "+item.Thinking)
	}
	if item.TmuxTarget != "" {
		lines = append(lines, "  терминал: "+item.TmuxTarget+"  (Enter — подключиться, Ctrl-b d — отсоединиться)")
	}
	return strings.Join(lines, "\n")
}

func (m Model) resourcesView() string {
	var b strings.Builder
	b.WriteString(accent.Render("  Ресурсы") + muted.Render("  лимиты cml · исполнители · аккаунты") + "\n\n")
	if m.providers.Error != "" {
		b.WriteString(bad.Render("  "+m.providers.Error) + "\n\n")
	}

	b.WriteString(m.renderLimits())

	b.WriteString(muted.Render("  Исполнители") + "\n")
	for i, provider := range m.providers.Providers {
		prefix := "  "
		if i == m.cursor {
			prefix = selectStyle.Render("▸ ")
		}
		status := bad.Render("нет")
		if provider.Available {
			status = good.Render("готов")
		}
		b.WriteString(prefix + fmt.Sprintf("%-18s %-8s %s", provider.Name, status, muted.Render(provider.Detail)) + "\n")
	}
	b.WriteString("\n" + muted.Render("  Аккаунты — глобальное действие для новых локальных запусков; всегда требует подтверждения") + "\n")
	for i, account := range m.providers.Accounts {
		index := len(m.providers.Providers) + i
		prefix := "  "
		if index == m.cursor {
			prefix = selectStyle.Render("▸ ")
		}
		status := bad.Render("нет")
		if account.Present {
			status = good.Render("есть")
		}
		active := ""
		if account.Active {
			active = "  ← активно"
		}
		b.WriteString(prefix + fmt.Sprintf("%-26s %-8s %s%s\n", account.Client+" / "+account.Name, status, muted.Render(filepath.Base(account.Path)), active))
	}
	b.WriteString("\n" + muted.Render("  r — обновить лимиты · o — сырой отчёт cml · Enter/s — глобальный аккаунт") + "\n")
	return b.String()
}

// renderLimits draws the limits board. The legacy fixed-width cml table stays
// available behind `o` for copy/diagnostics, but it is never the default view
// inside a native TUI.
func (m Model) renderLimits() string {
	var b strings.Builder
	when := "ещё не получены"
	if !m.providers.CMLCheckedAt.IsZero() {
		when = m.providers.CMLCheckedAt.Format("15:04:05")
	}
	b.WriteString(accent.Render("  Лимиты") + muted.Render("  источник cml · обновлено "+when) + "\n")

	raw := strings.TrimSpace(m.providers.CMLOutput)
	switch {
	case m.showRawQuota:
		if raw == "" {
			b.WriteString(muted.Render("  сырой отчёт пока пуст · r — обновить") + "\n\n")
		} else {
			b.WriteString(panel.Render("  "+strings.ReplaceAll(raw, "\n", "\n  ")) + "\n\n")
		}
	case len(m.providers.Quotas) > 0:
		b.WriteString(muted.Render(fmt.Sprintf("  %-24s %-7s  %-21s %-21s %-21s", "Провайдер", "Статус", "5 часов", "7 дней", "1 месяц")) + "\n")
		b.WriteString(muted.Render("  "+strings.Repeat("─", 100)) + "\n")
		for _, quota := range sortedQuotas(m.providers.Quotas) {
			b.WriteString("  " + fmt.Sprintf("%-24s %-7s %s %s %s\n",
				visiblePad(compactText(quota.Provider, 24), 24),
				statusCell(quota.Status),
				windowCell(quota.Window5h, 21),
				windowCell(quota.Window7d, 21),
				windowCell(quota.Window1m, 21),
			))
		}
		b.WriteString("\n")
	case raw != "":
		// The parser found nothing structured: show the report rather than lose it.
		b.WriteString(panel.Render("  "+strings.ReplaceAll(raw, "\n", "\n  ")) + "\n\n")
	default:
		b.WriteString(muted.Render("  получаю лимиты через cml…") + "\n\n")
	}
	return b.String()
}

func statusCell(status string) string {
	value := visiblePad(compactText(status, 7), 7)
	switch strings.ToLower(status) {
	case "error", "fail", "failed", "unauthorized":
		return bad.Render(value)
	case "ok", "plus", "active", "ready":
		return good.Render(value)
	default:
		return muted.Render(value)
	}
}

// windowCell renders one usage window: a compact bar, the remaining percent
// and the reset delay. Non-numeric cells keep their original wording.
func windowCell(window resources.QuotaWindow, width int) string {
	if strings.TrimSpace(window.Text) == "" {
		return visiblePad("—", width)
	}
	if window.Percent == nil {
		return muted.Render(visiblePad(compactText(window.Text, width), width))
	}
	percent := *window.Percent
	style := good
	switch {
	case percent < 15:
		style = bad
	case percent < 40:
		style = warn
	}
	cell := style.Render(fmt.Sprintf("%s %4.0f%%", usageBar(percent, 5), percent))
	if window.Reset != "" {
		cell += muted.Render(" ↻" + shortReset(window.Reset))
	}
	if missing := width - lipgloss.Width(cell); missing > 0 {
		cell += strings.Repeat(" ", missing)
	}
	return cell
}

func usageBar(percent float64, cells int) string {
	if percent < 0 {
		percent = 0
	}
	if percent > 100 {
		percent = 100
	}
	filled := int(percent/100*float64(cells) + 0.5)
	if filled > cells {
		filled = cells
	}
	return strings.Repeat("█", filled) + strings.Repeat("░", cells-filled)
}

var resetUnitPattern = regexp.MustCompile(`(\d+)\s*(days?|hours?|minutes?|seconds?|[dhms])\b`)

func shortReset(value string) string {
	converted := resetUnitPattern.ReplaceAllStringFunc(value, func(match string) string {
		parts := resetUnitPattern.FindStringSubmatch(match)
		switch strings.ToLower(parts[2]) {
		case "day", "days", "d":
			return parts[1] + "д"
		case "hour", "hours", "h":
			return parts[1] + "ч"
		case "minute", "minutes", "m":
			return parts[1] + "м"
		default:
			return parts[1] + "с"
		}
	})
	converted = strings.ReplaceAll(converted, " ", "")
	if strings.TrimSpace(converted) == "" {
		return value
	}
	return compactText(converted, 14)
}

func sortedQuotas(quotas []resources.Quota) []resources.Quota {
	sorted := append([]resources.Quota(nil), quotas...)
	sort.SliceStable(sorted, func(i, j int) bool {
		left, right := sortPercent(sorted[i]), sortPercent(sorted[j])
		if left == right {
			return sorted[i].Provider < sorted[j].Provider
		}
		return left < right
	})
	return sorted
}

func sortPercent(quota resources.Quota) float64 {
	if quota.Window5h.Percent != nil {
		return *quota.Window5h.Percent
	}
	return math.MaxFloat64
}

func visiblePad(value string, width int) string {
	value = compactText(value, width)
	if missing := width - lipgloss.Width(value); missing > 0 {
		return value + strings.Repeat(" ", missing)
	}
	return value
}

func (m Model) machinesView() string {
	var b strings.Builder
	b.WriteString(accent.Render("  Машины") + muted.Render("  параллельный пинг Tailscale и Wi-Fi/LAN") + "\n\n")
	for i, machine := range m.machines {
		prefix := "  "
		if i == m.cursor {
			prefix = selectStyle.Render("▸ ")
		}
		var status string
		detail := machine.Detail
		switch {
		case machine.Kind == "local":
			status = good.Render("локально")
		case !machine.Probed:
			status = muted.Render("проверяю…")
			if detail == "" {
				detail = "Tailscale и Wi-Fi/LAN пингуются параллельно"
			}
		case machine.Available:
			status = good.Render("доступна")
		default:
			status = bad.Render("недоступна")
		}
		b.WriteString(prefix + fmt.Sprintf("%-20s %-12s %s\n", machine.Title, status, muted.Render(detail)))
	}
	when := "ещё не проверялись"
	if !m.machinesCheckedAt.IsZero() {
		when = m.machinesCheckedAt.Format("15:04:05")
	}
	b.WriteString("\n" + muted.Render("  Маршрут: "+m.network.State+" · egress: "+firstNonEmpty(m.network.Egress, "неизвестен")+" · проверено "+when) + "\n")
	b.WriteString(muted.Render("  Enter — открыть (лучший маршрут) · w — открыть по Wi-Fi/LAN · r — проверить · u/d — VPN.") + "\n")
	return b.String()
}

func (m Model) formView() string {
	f := m.form
	var b strings.Builder
	b.WriteString(accent.Render("  Новая работа") + muted.Render("  одна редактируемая карточка запуска") + "\n\n")
	fields := []struct{ label, value string }{
		{"Тип", f.spec.Kind},
		{"Клиент", f.spec.Provider},
		{"Модель", f.spec.Model},
		{"Thinking", f.spec.Thinking},
		{"Prompt", firstNonEmpty(f.spec.Prompt, "(не задан; агент спросит после запуска)")},
		{"Проект", f.spec.ProjectPath},
		{"Машина", f.hostLabel()},
		{"Аккаунт", firstNonEmpty(f.spec.Account, "по конфигурации клиента")},
	}
	for i, field := range fields {
		prefix := "  "
		if i == f.focus {
			prefix = selectStyle.Render("▸ ")
		}
		value := field.value
		if i == 4 && f.edit {
			value = f.buf + "▌"
		}
		b.WriteString(prefix + fmt.Sprintf("%-10s %s\n", field.label, value))
	}
	b.WriteString("\n" + panel.Render("  Enter — запустить · ←/→ — изменить поле · j/k — поле · x — редактировать prompt · Esc — назад") + "\n")
	return b.String()
}

func (f *launchForm) hostLabel() string {
	if strings.TrimSpace(f.spec.Host) == "" {
		return "Эта машина"
	}
	for _, machine := range f.machines {
		if machine.Target == f.spec.Host || machine.ID == f.spec.Host {
			if machine.Path != "" {
				return machine.Title + " → " + machine.Path
			}
			return machine.Title
		}
	}
	return f.spec.Host
}

func (m Model) searchView() string {
	return accent.Render("  Поиск действий") + "\n\n  " + m.search.query + "▌\n\n" +
		muted.Render("  Руки · Фиксер · лимиты cml · аккаунты · машины · VPN\n  Enter — перейти, Esc — закрыть") + "\n"
}

func (m Model) confirmView() string {
	return accent.Render("  Подтверждение") + "\n\n" + panel.Render("  "+m.confirm.title+"\n\n  y/Enter — подтвердить · n/Esc — отменить") + "\n"
}

func (m Model) helpView() string {
	return accent.Render("  Помощь") + "\n\n" +
		"  1/2/3      Работа / Ресурсы / Машины\n" +
		"  h / f      открыть Руки / Фиксер для текущего проекта\n" +
		"  n          новый локальный запуск (не MCP-сессия)\n" +
		"  p/P        переключить сохранённый проект\n" +
		"  Enter      открыть выбранное действие, context или машину\n" +
		"  s          остановить выбранный локальный терминал\n" +
		"  r          обновить cml либо доступность машин\n" +
		"  u / d      включить / выключить VPN в разделе Машины\n" +
		"  /          поиск действий\n" +
		"  ? / Esc    закрыть помощь\n\n" +
		muted.Render("  Ввод дочернего агента принадлежит агенту. Fixer не перехватывает его Ctrl+C.") + "\n"
}

func (m Model) footer() string {
	switch m.tab {
	case workTab:
		return "  ↑/↓ move · Enter открыть · h Руки · f Фиксер · n локальный запуск · ? help · q quit"
	case resourcesTab:
		return "  ↑/↓ move · Enter/s глобальный аккаунт · r обновить cml · ? help · q quit"
	case machinesTab:
		return "  ↑/↓ move · Enter открыть · r проверить · u/d VPN · ? help · q quit"
	default:
		return "  ? help · q quit"
	}
}

func (m Model) clip(value string) string {
	if m.width <= 0 {
		return value
	}
	return value
}
