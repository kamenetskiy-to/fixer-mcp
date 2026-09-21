package console

import (
	"context"
	"fmt"
	"os"
	"os/exec"
	"path/filepath"
	"runtime"
	"strings"
	"time"

	tea "github.com/charmbracelet/bubbletea"
	"github.com/charmbracelet/lipgloss"

	"github.com/fixer-mcp/control-plane/internal/domain"
	"github.com/fixer-mcp/control-plane/internal/fleet"
	"github.com/fixer-mcp/control-plane/internal/launch"
	"github.com/fixer-mcp/control-plane/internal/machines"
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
	networkTab
	fleetTab
)

type Options struct {
	RuntimeRoot string
	RepoRoot    string
	StatePath   string
	Version     string
	InitialTab  int
	InitialPath string
}

type Model struct {
	store    *state.Store
	sessions *sessions.Manager
	opts     Options

	tab       int
	cursor    int
	width     int
	height    int
	showHelp  bool
	quitting  bool
	form      *launchForm
	search    *searchState
	confirm   *confirmState
	message   string
	messageAt time.Time

	project   domain.Project
	items     []domain.Session
	providers resources.Snapshot
	machines  []machines.Machine
	network   network.Status
	fleet     fleet.Result
	busy      bool
}

type launchForm struct {
	spec     launch.Spec
	focus    int
	edit     bool
	buf      string
	profiles []domain.LaunchProfile
	accounts []resources.Account
	machines []machines.Machine
}

type searchState struct {
	query  string
	cursor int
}

type confirmState struct {
	title      string
	action     string
	account    resources.Account
	session    domain.Session
	networkAct string
}

type tickMsg time.Time
type quotaMsg resources.Snapshot
type machineMsg machines.Snapshot
type networkMsg network.Status
type fleetMsg fleet.Result
type actionMsg struct {
	text string
	err  error
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
	if cwd == "" && snapshot.LastProjectID != "" {
		if old := snapshot.ProjectByID(snapshot.LastProjectID); old != nil && old.Path != "" {
			cwd = old.Path
		}
	}
	if cwd == "" {
		cwd, _ = os.Getwd()
	}
	projectPath := discoverProject(cwd)
	project := domain.Project{ID: launch.ProjectID(projectPath), Name: filepath.Base(projectPath), Path: projectPath, Host: "local", LastOpened: time.Now()}
	if old := snapshot.ProjectByID(project.ID); old != nil {
		project = *old
		project.Path = projectPath
		project.LastOpened = time.Now()
	}
	if err := store.TouchProject(project); err != nil {
		return Model{}, err
	}
	model := Model{
		store: store, sessions: sessions.NewManager(store), opts: opts,
		tab: opts.InitialTab, cursor: 0, project: project,
		providers: resources.Inspect(), machines: machines.Discover(),
		network: network.Status{Platform: runtime.GOOS, State: "checking", Checked: time.Now()},
	}
	model.refreshSessions()
	return model, nil
}

func Run(opts Options) error {
	model, err := New(opts)
	if err != nil {
		return err
	}
	program := tea.NewProgram(model, tea.WithAltScreen())
	_, err = program.Run()
	return err
}

func (m Model) Init() tea.Cmd { return tea.Batch(m.tick(), m.refreshNetwork()) }

func (m Model) tick() tea.Cmd {
	return tea.Tick(3*time.Second, func(t time.Time) tea.Msg { return tickMsg(t) })
}

func (m Model) refreshNetwork() tea.Cmd {
	return func() tea.Msg {
		ctx, cancel := context.WithTimeout(context.Background(), 8*time.Second)
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
		m.busy = false
		return m, nil
	case networkMsg:
		m.network = network.Status(value)
		m.busy = false
		return m, nil
	case fleetMsg:
		m.fleet = fleet.Result(value)
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
	case sessionFinishedMsg:
		m.busy = false
		snapshot := m.store.Snapshot()
		if session := snapshot.SessionByID(value.id); session != nil {
			session.State = domain.SessionDetached
			if value.err != nil {
				session.State = domain.SessionFailed
				session.ExitCode = 1
			}
			session.UpdatedAt = time.Now()
			_ = m.store.UpsertSession(*session)
		}
		m.refreshSessions()
		if value.err != nil {
			m.setMessage("сессия завершилась: " + value.err.Error())
		} else {
			m.setMessage("сессия отсоединена; процесс продолжает работу")
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
	if m.search != nil {
		return m.searchKey(key)
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
		return m, tea.Quit
	case "?":
		m.showHelp = true
		return m, nil
	case "tab", "right", "l":
		m.tab = (m.tab + 1) % 5
		m.cursor = 0
		return m, nil
	case "shift+tab", "left", "h":
		m.tab = (m.tab + 4) % 5
		m.cursor = 0
		return m, nil
	case "1":
		m.tab, m.cursor = workTab, 0
	case "2":
		m.tab, m.cursor = resourcesTab, 0
	case "3":
		m.tab, m.cursor = machinesTab, 0
	case "4":
		m.tab, m.cursor = networkTab, 0
	case "5":
		m.tab, m.cursor = fleetTab, 0
	case "/":
		m.search = &searchState{}
		return m, nil
	case "p":
		return m.switchProject(1)
	case "P":
		return m.switchProject(-1)
	case "n":
		m.form = newLaunchForm(m.project.Path, m.store.Snapshot().Profiles)
		m.form.accounts = append([]resources.Account(nil), m.providers.Accounts...)
		m.form.machines = append([]machines.Machine(nil), m.machines...)
		return m, nil
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
	case "u":
		if m.tab == networkTab {
			return m, m.vpnAction("up")
		}
	case "d":
		if m.tab == networkTab {
			return m.networkConfirm("down")
		}
	}
	return m, nil
}

func (m Model) maxCursor() int {
	switch m.tab {
	case workTab:
		return len(m.items)
	case resourcesTab:
		return max(0, len(m.providers.Providers)+len(m.providers.Accounts)-1)
	case machinesTab:
		return max(0, len(m.machines)-1)
	case networkTab:
		return 2
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
		if m.cursor >= len(m.items) {
			m.form = newLaunchForm(m.project.Path, m.store.Snapshot().Profiles)
			m.form.accounts = append([]resources.Account(nil), m.providers.Accounts...)
			m.form.machines = append([]machines.Machine(nil), m.machines...)
			return m, nil
		}
		session := m.items[m.cursor]
		if session.State == domain.SessionFinished || session.State == domain.SessionFailed {
			m.setMessage("живого процесса нет; n — запустить новый context")
			return m, nil
		}
		return m.attach(session)
	case resourcesTab:
		return m.accountConfirm("switch")
	case machinesTab:
		if m.cursor >= 0 && m.cursor < len(m.machines) {
			machine := m.machines[m.cursor]
			binary, args, err := machines.AttachCommand(machine)
			if err != nil {
				m.setMessage(err.Error())
				return m, nil
			}
			session := domain.Session{ID: "machine-" + machine.ID, Title: machine.Title, ProjectPath: m.project.Path, Command: append([]string{binary}, args...), State: domain.SessionRunning}
			return m.attachCommand(session, registry.Resolved{Entry: registry.Entry{ID: session.ID, Title: session.Title, Args: args}, Binary: binary})
		}
	case networkTab:
		if m.cursor == 0 {
			return m, m.vpnAction("up")
		}
		if m.cursor == 1 {
			return m.networkConfirm("down")
		}
	case fleetTab:
		return m, m.refreshFleet()
	}
	return m, nil
}

func (m Model) attach(session domain.Session) (tea.Model, tea.Cmd) {
	command := m.sessions.Attach(session)
	m.busy = true
	return m, tea.Exec(command, func(err error) tea.Msg { return sessionFinishedMsg{id: session.ID, err: err} })
}

func (m Model) attachCommand(session domain.Session, entry registry.Resolved) (tea.Model, tea.Cmd) {
	command := runner.NewExecCommand(entry, runner.Options{Env: os.Environ(), Dir: session.ProjectPath})
	m.busy = true
	return m, tea.Exec(command, func(err error) tea.Msg { return sessionFinishedMsg{id: session.ID, err: err} })
}

func (m Model) accountConfirm(action string) (tea.Model, tea.Cmd) {
	index := m.cursor - len(m.providers.Providers)
	if index < 0 || index >= len(m.providers.Accounts) {
		m.setMessage("сначала выберите аккаунт")
		return m, nil
	}
	account := m.providers.Accounts[index]
	verb := "переключить"
	if action == "bind" {
		verb = "привязать текущие credentials к"
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
				err = m.sessions.Stop(confirmation.session)
				text = "сессия остановлена"
			case "vpn-down":
				text, err = network.VPNAction(context.Background(), "down", "")
			}
			return actionMsg{text: text, err: err}
		}
	}
	return m, nil
}

func (m Model) stopConfirm() (tea.Model, tea.Cmd) {
	if m.tab != workTab || m.cursor >= len(m.items) {
		return m, nil
	}
	session := m.items[m.cursor]
	m.confirm = &confirmState{title: fmt.Sprintf("остановить сессию %q?", session.Title), action: "stop", session: session}
	return m, nil
}

func (m Model) networkConfirm(action string) (tea.Model, tea.Cmd) {
	if action != "down" {
		return m, m.vpnAction(action)
	}
	m.confirm = &confirmState{title: "выключить VPN и изменить маршрут?", action: "vpn-down", networkAct: action}
	return m, nil
}

func (m Model) refreshCurrent() tea.Cmd {
	switch m.tab {
	case resourcesTab:
		m.busy = true
		return func() tea.Msg {
			ctx, cancel := context.WithTimeout(context.Background(), 30*time.Second)
			defer cancel()
			return quotaMsg(resources.RefreshQuota(ctx))
		}
	case machinesTab:
		m.busy = true
		machinesNow := append([]machines.Machine(nil), m.machines...)
		return func() tea.Msg {
			ctx, cancel := context.WithTimeout(context.Background(), 20*time.Second)
			defer cancel()
			return machineMsg(machines.Probe(ctx, machinesNow))
		}
	case networkTab:
		m.busy = true
		return m.refreshNetwork()
	case fleetTab:
		m.busy = true
		return m.refreshFleet()
	default:
		m.refreshSessions()
		return nil
	}
}

func (m Model) refreshFleet() tea.Cmd {
	root := m.opts.RepoRoot
	if root == "" {
		root = fleet.FindRoot(m.project.Path)
	}
	return func() tea.Msg {
		ctx, cancel := context.WithTimeout(context.Background(), 45*time.Second)
		defer cancel()
		return fleetMsg(fleet.Check(ctx, root))
	}
}

func (m Model) vpnAction(action string) tea.Cmd {
	m.busy = true
	return func() tea.Msg {
		ctx, cancel := context.WithTimeout(context.Background(), 45*time.Second)
		defer cancel()
		text, err := network.VPNAction(ctx, action, "us")
		return actionMsg{text: text, err: err}
	}
}

func (m Model) switchProject(delta int) (tea.Model, tea.Cmd) {
	projects := m.store.Snapshot().Projects
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
	if err := m.store.TouchProject(m.project); err != nil {
		m.setMessage(err.Error())
		return m, nil
	}
	m.cursor = 0
	m.refreshSessions()
	m.setMessage("проект: " + m.project.Path)
	return m, nil
}

func (m *Model) refreshSessions() {
	items, err := m.sessions.Refresh()
	if err != nil {
		m.setMessage(err.Error())
	}
	filtered := make([]domain.Session, 0, len(items))
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

func newLaunchForm(projectPath string, saved []domain.LaunchProfile) *launchForm {
	profile := launch.DefaultProfile(projectPath)
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
		values := []string{launch.KindAgent, launch.KindWorkroom, launch.KindTerminal}
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
						f.spec.Host, f.spec.RemotePath = machine.Target, machine.Path
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
	if spec.Kind == launch.KindWorkroom {
		spec.Title = "Fixer Workroom"
	} else if spec.Title == "" {
		spec.Title = "Новая работа"
	}
	command, err := launch.Build(spec, m.opts.RuntimeRoot)
	if err != nil {
		m.form.spec = spec
		m.setMessage(err.Error())
		return m, nil
	}
	profile := domain.LaunchProfile{
		ID: "project-" + launch.ProjectID(spec.ProjectPath), Title: spec.Title,
		Kind: spec.Kind, Provider: spec.Provider, Account: spec.Account,
		Model: spec.Model, Thinking: spec.Thinking, Host: spec.Host,
		ProjectID: launch.ProjectID(spec.ProjectPath), Permissions: "approve", MCPMode: "project",
	}
	if err := m.store.UpsertProfile(profile); err != nil {
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
			m.tab, m.cursor = choice, 0
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
	case strings.Contains(query, "ssh"), strings.Contains(query, "machine"), strings.Contains(query, "маш"):
		return machinesTab
	case strings.Contains(query, "vpn"), strings.Contains(query, "network"), strings.Contains(query, "сеть"), strings.Contains(query, "ip"):
		return networkTab
	case strings.Contains(query, "fleet"), strings.Contains(query, "doctor"), strings.Contains(query, "check"):
		return fleetTab
	default:
		return -1
	}
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
		case networkTab:
			b.WriteString(m.networkView())
		case fleetTab:
			b.WriteString(m.fleetView())
		}
	}
	b.WriteString("\n")
	if m.message != "" {
		b.WriteString(muted.Render("  "+m.message) + "\n")
	}
	b.WriteString(muted.Render("  ↑/↓ move · enter open · n new work · r refresh · / search · ? help · q quit"))
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
	status := "offline"
	if m.network.State == "online" {
		status = "network ✓"
	} else if m.network.State == "configured" {
		status = "network ?"
	}
	tabs := []string{"Работа", "Ресурсы", "Машины", "Сеть", "Fleet"}
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
	b.WriteString(accent.Render("  Активная работа") + "\n")
	projectHint := "p/P — переключить сохранённый проект"
	b.WriteString(muted.Render("  "+m.project.Path+"  ·  "+m.project.Host+"  ·  "+projectHint) + "\n\n")
	if len(m.items) == 0 {
		b.WriteString(muted.Render("  Нет сохранённых сессий. Enter или n — новая работа.") + "\n")
	} else {
		for i, item := range m.items {
			b.WriteString(m.sessionLine(i, item) + "\n")
		}
	}
	newIndex := len(m.items)
	prefix := "  "
	if m.cursor == newIndex {
		prefix = selectStyle.Render("▸ ")
	}
	b.WriteString(prefix + good.Render("＋ Новая работа") + "\n\n")
	if m.cursor < len(m.items) {
		item := m.items[m.cursor]
		b.WriteString(panel.Render(m.sessionDetail(item)) + "\n")
	}
	return b.String()
}

func (m Model) sessionLine(index int, item domain.Session) string {
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

func (m Model) sessionDetail(item domain.Session) string {
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
	b.WriteString(accent.Render("  Ресурсы") + muted.Render("  квоты, клиенты и аккаунты") + "\n\n")
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
	b.WriteString("\n" + muted.Render("  Аккаунты") + "\n")
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
		b.WriteString(prefix + fmt.Sprintf("%-18s %-8s %s%s\n", account.Client+" / "+account.Name, status, muted.Render(account.Path), active))
	}
	b.WriteString("\n")
	if len(m.providers.Quotas) > 0 {
		b.WriteString(muted.Render("  Последняя проверка квот") + "\n")
		for _, quota := range m.providers.Quotas {
			b.WriteString("  " + quota.Provider + "  " + good.Render(quota.Status) + "  " + muted.Render(quota.Windows) + "\n")
		}
	} else {
		b.WriteString(muted.Render("  r — обновить квоты; запуск не блокируется, если один провайдер недоступен") + "\n")
	}
	if m.providers.Error != "" {
		b.WriteString(bad.Render("  "+m.providers.Error) + "\n")
	}
	return b.String()
}

func (m Model) machinesView() string {
	var b strings.Builder
	b.WriteString(accent.Render("  Машины") + muted.Render("  сохранённый target и фактическая доступность") + "\n\n")
	for i, machine := range m.machines {
		prefix := "  "
		if i == m.cursor {
			prefix = selectStyle.Render("▸ ")
		}
		status := bad.Render("не проверено")
		if machine.Available {
			status = good.Render("доступна")
		}
		b.WriteString(prefix + fmt.Sprintf("%-24s %-12s %s\n", machine.Title, status, muted.Render(machine.Detail)))
	}
	b.WriteString("\n" + muted.Render("  Enter — открыть terminal. r — проверить SSH. Транспорт выбирается сохранённой конфигурацией.") + "\n")
	return b.String()
}

func (m Model) networkView() string {
	var b strings.Builder
	b.WriteString(accent.Render("  Сеть") + muted.Render("  состояние фактического маршрута") + "\n\n")
	b.WriteString(fmt.Sprintf("  платформа: %s\n  состояние: %s\n  egress:    %s\n  marker:    %s\n", m.network.Platform, m.network.State, m.network.Egress, m.network.Marker))
	if len(m.network.Values) > 0 {
		b.WriteString("  proxy env: ")
		var names []string
		for name := range m.network.Values {
			names = append(names, name)
		}
		b.WriteString(strings.Join(names, ", ") + "\n")
	}
	b.WriteString("\n")
	items := []string{"Включить VPN (US)", "Выключить VPN", "Обновить фактический маршрут"}
	for i, item := range items {
		prefix := "  "
		if i == m.cursor {
			prefix = selectStyle.Render("▸ ")
		}
		b.WriteString(prefix + item + "\n")
	}
	b.WriteString("\n" + muted.Render("  u/d — быстрые действия. Перед выключением активных маршрутов нужен явный контроль.") + "\n")
	return b.String()
}

func (m Model) fleetView() string {
	var b strings.Builder
	b.WriteString(accent.Render("  Fleet") + muted.Render("  doctor и управляемое окружение") + "\n\n")
	root := m.opts.RepoRoot
	if root == "" {
		root = fleet.FindRoot(m.project.Path)
	}
	b.WriteString("  root: " + root + "\n")
	if m.fleet.CheckedAt.IsZero() {
		b.WriteString("\n" + muted.Render("  Enter или r — запустить проверку") + "\n")
	} else if m.fleet.OK {
		b.WriteString("  " + good.Render("✓ окружение готово") + "  " + m.fleet.Summary + "\n")
	} else {
		b.WriteString("  " + bad.Render("✗ есть проблемы") + "  " + m.fleet.Summary + "\n")
		if m.fleet.Error != "" {
			b.WriteString("  " + bad.Render(m.fleet.Error) + "\n")
		}
	}
	if m.fleet.Output != "" && m.height > 18 {
		output := m.fleet.Output
		lines := strings.Split(output, "\n")
		if len(lines) > 8 {
			lines = lines[len(lines)-8:]
		}
		b.WriteString("\n" + muted.Render(strings.Join(lines, "\n")) + "\n")
	}
	b.WriteString("\n" + muted.Render("  Enter/r — проверить. Обновление Fixer запускается командой `fixer update`, не скрыто внутри doctor.") + "\n")
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
		muted.Render("  Работа · лимиты · аккаунты · машины · сеть · fleet\n  Enter — перейти, Esc — закрыть") + "\n"
}

func (m Model) confirmView() string {
	return accent.Render("  Подтверждение") + "\n\n" + panel.Render("  "+m.confirm.title+"\n\n  y/Enter — подтвердить · n/Esc — отменить") + "\n"
}

func (m Model) helpView() string {
	return accent.Render("  Помощь") + "\n\n" +
		"  1/2/3/4/5  Работа / Ресурсы / Машины / Сеть / Fleet\n" +
		"  n           новая работа с сохранённой карточкой\n" +
		"  p/P         переключить сохранённый проект\n" +
		"  fixer open PATH  открыть и запомнить проект на другой машине/пути\n" +
		"  Enter       открыть сессию, терминал или действие\n" +
		"  Ctrl-b d    отсоединиться от tmux-сессии, не останавливая агента\n" +
		"  s           остановить выбранную detachable-сессию\n" +
		"  r           обновить текущий экран\n" +
		"  /           поиск действий\n" +
		"  ? / Esc     закрыть помощь\n\n" +
		muted.Render("  Ввод дочернего агента принадлежит агенту. Fixer не перехватывает его Ctrl+C.") + "\n"
}

func (m Model) clip(value string) string {
	if m.width <= 0 {
		return value
	}
	return value
}
