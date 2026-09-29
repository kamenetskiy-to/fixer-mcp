// Package machines owns the small, logical Fixer fleet inventory.
//
// A logical machine is deliberately not an SSH alias.  A host can have several
// transports (Tailscale socket, direct tailnet, SOCKS, LAN), but the console
// exposes one row and chooses a working route before it opens a terminal.
package machines

import (
	"context"
	"encoding/json"
	"errors"
	"fmt"
	"net"
	"os"
	"os/exec"
	"path/filepath"
	"regexp"
	"runtime"
	"sort"
	"strconv"
	"strings"
	"sync"
	"time"
)

const defaultSSHPort = 22

// routeProbeTimeout bounds one transport probe. Every route of every machine
// runs concurrently, so a dead Wi-Fi address must never hold the whole
// inventory hostage for minutes.
const (
	routeProbeTimeout = 6 * time.Second
	// A LAN probe may first have to rediscover the address after a Wi-Fi
	// reconnect, so it gets a wider budget than a fixed-IP transport. The sweep
	// fallback is the slow path and must fit comfortably.
	lanProbeTimeout = 45 * time.Second
	lanCacheTTL     = 3 * time.Minute
	// Tailnet probes run through a proxy command (the SOCKS lane or the
	// tailscale CLI). A cold proxy start inside a 16-wide probe pool has been
	// seen blowing a tight connect budget on older hosts: every machine
	// reported unreachable while plain ssh to the same host worked
	// (macbook-air-lizok, 2026-09-29).
	tailnetProbeTimeout          = 10 * time.Second
	tailnetConnectTimeoutSeconds = 5
	tailnetProbeRetries          = 1
)

// Route is an internal transport candidate for one logical machine. Target is
// intentionally not rendered as a selectable machine: it is only used after
// the operator selects its logical host.
type Route struct {
	Kind           string `json:"kind"`
	Target         string `json:"target"`
	Port           int    `json:"port,omitempty"`
	DisabledReason string `json:"disabled_reason,omitempty"`
}

// RouteStatus is the independent result of probing one transport of one
// logical machine. The console shows them side by side so the operator can see
// at a glance that Tailscale works while Wi-Fi/LAN does not (or vice versa).
type RouteStatus struct {
	Kind      string `json:"kind"`
	Available bool   `json:"available"`
	Detail    string `json:"detail,omitempty"`
}

type Machine struct {
	ID     string `json:"id"`
	Title  string `json:"title"`
	Target string `json:"target,omitempty"` // legacy/config compatibility; routes are authoritative.
	Kind   string `json:"kind"`
	Path   string `json:"path,omitempty"`
	// MDNS is the Bonjour name used to rediscover the machine when DHCP hands it
	// a new address after a Wi-Fi reconnect.
	MDNS          string        `json:"mdns,omitempty"`
	Routes        []Route       `json:"routes,omitempty"`
	ActiveRoute   string        `json:"active_route,omitempty"`
	Available     bool          `json:"available"`
	Detail        string        `json:"detail,omitempty"`
	RouteStatuses []RouteStatus `json:"route_statuses,omitempty"`
	Probed        bool          `json:"probed"`
	// LANDiscovered is the address found at probe time instead of the configured
	// (and possibly stale) one.
	LANDiscovered string `json:"lan_discovered,omitempty"`
}

type Snapshot struct {
	Machines []Machine `json:"machines"`
	Checked  time.Time `json:"checked"`
}

// Connection is the resolved SSH invocation for a selected logical machine.
// SSHOptions is reusable by remote agent launches as well as an interactive
// terminal attach.
type Connection struct {
	Machine    Machine
	Route      Route
	Binary     string
	SSHOptions []string
}

// Discover returns the canonical fleet only. In particular it never scans
// ~/.ssh/config: personal aliases, stale aliases, and transport aliases are
// implementation details and must not turn into fake machines in the UI.
func Discover() []Machine {
	inventory := canonicalInventory()
	overrides := configuredOverrides()
	for i := range inventory {
		if override, ok := overrides[inventory[i].ID]; ok {
			inventory[i] = mergeOverride(inventory[i], override)
		}
	}

	local := Machine{
		ID: "local", Title: "Эта машина", Kind: "local", Available: true,
		Detail: "локальный терминал",
	}
	current := currentLogicalID()
	machines := make([]Machine, 0, len(inventory)+1)
	machines = append(machines, local)
	for _, machine := range inventory {
		// The local physical device has already been represented by "Эта
		// машина". Hiding its remote duplicate keeps the list at one local
		// host plus the seven other logical devices.
		if machine.ID == current {
			continue
		}
		machine.Detail = routeHint(machine)
		machines = append(machines, machine)
	}
	return machines
}

// Probe probes every transport of every logical machine concurrently, so the
// operator sees Tailscale and Wi-Fi/LAN reachability side by side as soon as
// the machines space opens instead of waiting for a sequential sweep.
func Probe(ctx context.Context, input []Machine) Snapshot {
	result := Snapshot{Checked: time.Now(), Machines: cloneMachines(input)}
	// One pool bounds simultaneous `ssh` probes across the whole inventory
	// (machines × routes) so a sweep cannot fork-bomb the host.
	sem := make(chan struct{}, 16)
	var wait sync.WaitGroup
	for i := range result.Machines {
		machine := &result.Machines[i]
		if machine.Kind == "local" {
			machine.Available = true
			machine.Detail = "локальный терминал"
			machine.Probed = true
			continue
		}
		wait.Add(1)
		go func(machine *Machine) {
			defer wait.Done()
			probeMachine(ctx, machine, sem)
		}(machine)
	}
	wait.Wait()
	return result
}

func probeMachine(ctx context.Context, machine *Machine, sem chan struct{}) {
	machine.Probed = true
	routes := probeRoutes(*machine)
	if len(routes) == 0 {
		machine.Available = false
		machine.Detail = "нет настроенного транспорта"
		return
	}

	statuses := make([]RouteStatus, len(routes))
	var routeWait sync.WaitGroup
	for i, route := range routes {
		routeWait.Add(1)
		go func(i int, route Route) {
			defer routeWait.Done()
			available, detail, discovered := probeRouteWithSlot(ctx, *machine, route, sem)
			statuses[i] = RouteStatus{Kind: route.Kind, Available: available, Detail: detail}
			if route.Kind == "lan" && discovered != "" {
				machine.LANDiscovered = discovered
			}
		}(i, route)
	}
	routeWait.Wait()
	machine.RouteStatuses = statuses

	// Statuses keep canonical fallback order, so the first available one is the
	// route Resolve would pick.
	machine.Available = false
	for _, status := range statuses {
		if status.Available {
			machine.Available = true
			machine.ActiveRoute = status.Kind
			break
		}
	}
	machine.Detail = routeStatusSummary(*machine, statuses)
}

// probeRoutes returns each transport of a machine exactly once, in canonical
// fallback order, including explicitly disabled ones so the UI can show *why*
// a route is unavailable instead of hiding it.
func probeRoutes(machine Machine) []Route {
	routes := machine.Routes
	if len(routes) == 0 && strings.TrimSpace(machine.Target) != "" {
		routes = []Route{{Kind: "tailscale", Target: machine.Target}}
	}
	seen := make(map[string]bool, len(routes))
	result := make([]Route, 0, len(routes))
	for _, route := range routes {
		if route.Kind == "" || seen[route.Kind] {
			continue
		}
		seen[route.Kind] = true
		result = append(result, route)
	}
	return result
}

func probeRouteWithSlot(ctx context.Context, machine Machine, route Route, sem chan struct{}) (bool, string, string) {
	if strings.TrimSpace(route.DisabledReason) != "" {
		return false, "отключён: " + route.DisabledReason, ""
	}
	select {
	case sem <- struct{}{}:
	case <-ctx.Done():
		return false, "проверка отменена", ""
	}
	defer func() { <-sem }()
	budget := routeProbeTimeout
	switch {
	case route.Kind == "lan":
		// Wi-Fi addresses move after reconnects; rediscovery needs room.
		budget = lanProbeTimeout
	case isTailnetRoute(route):
		budget = tailnetProbeTimeout
	}
	routeCtx, cancel := context.WithTimeout(ctx, budget)
	defer cancel()
	return probeRoute(routeCtx, machine, route)
}

func probeRoute(ctx context.Context, machine Machine, route Route) (bool, string, string) {
	if strings.TrimSpace(route.Target) == "" {
		return false, "нет адреса", ""
	}
	args, available := sshArgs(machine.ID, route, false)
	if !available {
		return false, "транспорт недоступен", ""
	}
	connectTimeout := "3"
	if isTailnetRoute(route) {
		connectTimeout = strconv.Itoa(tailnetConnectTimeoutSeconds)
	}
	probeArgs := append([]string{"-o", "BatchMode=yes", "-o", "ConnectTimeout=" + connectTimeout}, args...)
	probeArgs = append(probeArgs, "true")
	output, err := exec.CommandContext(ctx, "ssh", probeArgs...).CombinedOutput()
	if err != nil && isTailnetRoute(route) {
		// One cold proxy start must not decide the verdict: the recurring
		// failure is a first-attempt timeout that succeeds on retry while the
		// operator's own ssh to the same host works (Air, 2026-09-29).
		for attempt := 0; attempt < tailnetProbeRetries && err != nil && timeoutClassFailure(ctx, output); attempt++ {
			select {
			case <-ctx.Done():
			case <-time.After(time.Second):
			}
			output, err = exec.CommandContext(ctx, "ssh", probeArgs...).CombinedOutput()
		}
	}
	if err == nil {
		return true, "", ""
	}
	if route.Kind != "lan" || !staleLANFailure(route, ctx, output) {
		return false, classifyRouteFailure(ctx, route, output), ""
	}
	// The configured Wi-Fi address no longer answers: rediscover the machine
	// instead of reporting a dead host it may well be using right now.
	discovery, discoverErr := discoverLAN(ctx, machine)
	if discoverErr != nil {
		return false, lanDiscoveryFailure(discoverErr, discovery), ""
	}
	if !discovery.authed {
		// The address is known but not usable with this key: name it without
		// claiming the machine was successfully discovered.
		return false, "нет доступа по ключу (" + discovery.ip + ")", ""
	}
	retry := route
	retry.Target = replaceHost(route.Target, discovery.ip)
	retryArgs, ok := sshArgs(machine.ID, retry, false)
	if !ok {
		return false, "транспорт недоступен", discovery.ip
	}
	retryProbe := append([]string{"-o", "BatchMode=yes", "-o", "ConnectTimeout=4"}, retryArgs...)
	retryProbe = append(retryProbe, "true")
	if retryErr := exec.CommandContext(ctx, "ssh", retryProbe...).Run(); retryErr != nil {
		return false, classifyRouteFailure(ctx, retry, nil), discovery.ip
	}
	return true, "через обнаруженный адрес", discovery.ip
}

// staleLANFailure reports whether a LAN attempt failed in a way that can be
// caused by DHCP handing the machine a new address.
func staleLANFailure(route Route, ctx context.Context, output []byte) bool {
	if strings.TrimSpace(route.DisabledReason) != "" {
		return false
	}
	if ctx.Err() != nil {
		return true
	}
	lower := strings.ToLower(string(output))
	for _, marker := range []string{
		"operation timed out", "connection timed out", "connection refused",
		"host is down", "no route to host", "network is unreachable",
	} {
		if strings.Contains(lower, marker) {
			return true
		}
	}
	return false
}

func lanDiscoveryFailure(err error, discovery lanDiscovery) string {
	if discovery.ip != "" && !discovery.authed {
		return "нет доступа по ключу (" + discovery.ip + ")"
	}
	if errors.Is(err, errLANNoRoute) {
		return "нет LAN-маршрута"
	}
	if errors.Is(err, errLANNotFound) {
		return "адрес не найден в Wi-Fi (подключитесь один раз по Tailscale)"
	}
	return compactDetail(err.Error())
}

func replaceHost(target, ip string) string {
	prefix := ""
	if at := strings.Index(target, "@"); at >= 0 {
		prefix = target[:at+1]
	}
	_, port := splitTarget(target)
	if port != "" {
		return prefix + ip + ":" + port
	}
	return prefix + ip
}

func splitTarget(target string) (host, port string) {
	value := target
	if at := strings.LastIndex(value, "@"); at >= 0 {
		value = value[at+1:]
	}
	if colon := strings.LastIndex(value, ":"); colon >= 0 && !strings.Contains(value[colon+1:], "/") {
		return value[:colon], value[colon+1:]
	}
	return value, ""
}

// probeFailure classifies a failed probe so the operator sees *why* a route is
// down instead of a generic "нет ответа". The most important case is a host
// key conflict: one LAN IP can change owners (WSL used to own .75, Ubuntu owns
// it now), and reporting that as a timeout hides a solvable problem.
func probeFailure(route Route, ctx context.Context, output []byte) string {
	text := string(output)
	lower := strings.ToLower(text)
	switch {
	case ctx.Err() != nil:
		if route.Kind == "lan" {
			return "IP не отвечает"
		}
		return "нет ответа"
	case strings.Contains(lower, "host identification has changed"),
		strings.Contains(lower, "host key verification failed"):
		return "конфликт ключа хоста"
	case strings.Contains(lower, "permission denied"):
		return "нет доступа по ключу"
	case strings.Contains(lower, "connection refused"):
		return "порт закрыт"
	case strings.Contains(lower, "no route to host"),
		strings.Contains(lower, "network is unreachable"),
		strings.Contains(lower, "host is unreachable"):
		if route.Kind == "lan" {
			return "машина не в этой Wi-Fi-сети"
		}
		return "сеть недоступна"
	}
	for _, line := range strings.Split(text, "\n") {
		line = strings.TrimSpace(line)
		if line != "" && !strings.HasPrefix(line, "Warning:") {
			return compactDetail(line)
		}
	}
	return "нет ответа"
}

func compactDetail(value string) string {
	value = strings.TrimSpace(value)
	if len(value) > 60 {
		return value[:57] + "…"
	}
	return value
}

// classifyRouteFailure turns an ssh failure into an operator-facing reason.
// For tailnet transports it also asks Tailscale what the peer is doing, because
// "Connection timed out during banner exchange" tells nothing while "the peer
// has been offline for 11 minutes" answers the actual question.
func classifyRouteFailure(ctx context.Context, route Route, output []byte) string {
	base := probeFailure(route, ctx, output)
	if !isTailnetRoute(route) || !needsPeerExplanation(base) {
		return base
	}
	peers, queryable := tailscalePeers()
	if !queryable {
		return base
	}
	host, _ := splitTarget(route.Target)
	peer, known := peers[host]
	return tailnetFailureDetail(base, peer, known)
}

func isTailnetRoute(route Route) bool {
	switch route.Kind {
	case "tailscale", "tailnet-direct", "socks":
		return true
	default:
		return false
	}
}

func needsPeerExplanation(base string) bool {
	lower := strings.ToLower(base)
	// Identity and authorization failures are about this host, not about
	// reachability, so they must keep their own precise wording.
	for _, marker := range []string{
		"нет ответа", "banner exchange", "timed out", "timeout",
		"no route to host", "network is unreachable", "сеть недоступна",
	} {
		if strings.Contains(lower, marker) {
			return true
		}
	}
	return false
}

// tailnetFailureDetail is pure so the wording can be tested without a network.
func tailnetFailureDetail(base string, peer tailscalePeer, known bool) string {
	if !known {
		return "нет пира в тейлнете (проверьте вход tailscale на этой машине)"
	}
	if !peer.Online {
		detail := "пир офлайн в тейлнете"
		if since := visibleSince(peer.LastSeen); since != "" {
			detail += " · " + since
		}
		return detail
	}
	return "пир online · " + base
}

func visibleSince(stamp time.Time) string {
	if stamp.IsZero() {
		return ""
	}
	delta := time.Since(stamp)
	if delta < 0 {
		delta = 0
	}
	switch {
	case delta < 90*time.Second:
		return fmt.Sprintf("виден %dс назад", int(delta.Seconds()))
	case delta < time.Hour:
		return fmt.Sprintf("виден %dм назад", int(delta.Minutes()))
	case delta < 24*time.Hour:
		return fmt.Sprintf("виден %dч назад", int(delta.Hours()))
	default:
		return "виден " + stamp.Local().Format("02.01 15:04")
	}
}

type tailscalePeer struct {
	Hostname string
	Online   bool
	LastSeen time.Time
}

var (
	peersMu    sync.Mutex
	peersCache map[string]tailscalePeer
	peersAt    time.Time
)

// tailscalePeers maps every tailnet IP to its peer state, cached briefly so a
// sweep does not shell out once per route.
func tailscalePeers() (map[string]tailscalePeer, bool) {
	peersMu.Lock()
	if peersCache != nil && time.Since(peersAt) < 60*time.Second {
		cached := peersCache
		peersMu.Unlock()
		return cached, true
	}
	peersMu.Unlock()

	binary, socket, ok := tailscaleBinaryAndSocket()
	if !ok {
		return nil, false
	}
	args := []string{"status", "--json"}
	if socket != "" {
		args = append([]string{"--socket=" + socket}, args...)
	}
	queryCtx, cancel := context.WithTimeout(context.Background(), 5*time.Second)
	defer cancel()
	output, err := exec.CommandContext(queryCtx, binary, args...).Output()
	if err != nil {
		return nil, false
	}
	var payload struct {
		Peer map[string]struct {
			HostName     string    `json:"HostName"`
			Online       bool      `json:"Online"`
			LastSeen     time.Time `json:"LastSeen"`
			TailscaleIPs []string  `json:"TailscaleIPs"`
		} `json:"Peer"`
	}
	if err := json.Unmarshal(output, &payload); err != nil {
		return nil, false
	}
	peers := make(map[string]tailscalePeer, len(payload.Peer))
	for _, node := range payload.Peer {
		peer := tailscalePeer{Hostname: node.HostName, Online: node.Online, LastSeen: node.LastSeen}
		for _, ip := range node.TailscaleIPs {
			peers[ip] = peer
		}
	}
	peersMu.Lock()
	peersCache = peers
	peersAt = time.Now()
	cached := peersCache
	peersMu.Unlock()
	return cached, true
}

// routeStatusSummary compresses the per-route results into the two reachability
// views the operator actually cares about: the private tailnet side and the
// local Wi-Fi/LAN side.
func routeStatusSummary(machine Machine, statuses []RouteStatus) string {
	if len(statuses) == 0 {
		return "нет маршрутов"
	}
	var tailnet []RouteStatus
	var lan *RouteStatus
	for i := range statuses {
		if statuses[i].Kind == "lan" {
			lan = &statuses[i]
		} else {
			tailnet = append(tailnet, statuses[i])
		}
	}
	parts := make([]string, 0, 2)
	if len(tailnet) > 0 {
		parts = append(parts, "Tailscale "+sideMark(tailnet))
	}
	if lan != nil {
		mark := "✓"
		if !lan.Available {
			if strings.HasPrefix(lan.Detail, "отключён") {
				mark = "— отключён"
			} else {
				mark = "✗"
			}
		} else if machine.LANDiscovered != "" {
			// DHCP moved the machine: show where it was actually found, so the
			// operator can see the inventory address is stale.
			mark = "✓ " + machine.LANDiscovered
		}
		parts = append(parts, "Wi-Fi/LAN "+mark)
	}
	return strings.Join(parts, " · ")
}

func sideMark(routes []RouteStatus) string {
	for _, route := range routes {
		if route.Available {
			return "✓"
		}
	}
	for _, route := range routes {
		switch route.Detail {
		case "", "нет ответа", "таймаут":
			continue
		default:
			return "✗ " + route.Detail
		}
	}
	return "✗"
}

// Resolve selects a route without asking the operator to distinguish LAN,
// Tailnet, aliases, or proxy plumbing.
func Resolve(ctx context.Context, machine Machine) (Connection, error) {
	return ResolveVia(ctx, machine, "")
}

// ResolveVia resolves a connection, optionally pinned to one transport kind.
// The console exposes this as "open by Wi-Fi": when the operator asks for LAN
// explicitly, falling back to Tailscale silently would defeat the purpose.
func ResolveVia(ctx context.Context, machine Machine, preferKind string) (Connection, error) {
	if machine.Kind == "local" {
		binary, args, err := localShell()
		if err != nil {
			return Connection{}, err
		}
		return Connection{Machine: machine, Binary: binary, SSHOptions: args}, nil
	}
	if len(machine.Routes) == 0 && strings.TrimSpace(machine.Target) != "" {
		machine.Routes = []Route{{Kind: "tailscale", Target: machine.Target}}
	}
	if len(machine.Routes) == 0 {
		return Connection{}, fmt.Errorf("машина %s не имеет маршрута", machine.ID)
	}

	routes := machine.Routes
	if preferKind != "" {
		pinned := make([]Route, 0, 1)
		for _, route := range machine.Routes {
			if route.Kind == preferKind {
				pinned = append(pinned, route)
			}
		}
		if len(pinned) == 0 {
			return Connection{}, fmt.Errorf("у %s нет маршрута %s", machine.Title, routeLabel(preferKind))
		}
		routes = pinned
	}

	var attempted []string
	var lastDetail string
	var discoveredLAN string
	for _, route := range routes {
		if strings.TrimSpace(route.DisabledReason) != "" || strings.TrimSpace(route.Target) == "" {
			lastDetail = strings.TrimSpace(route.DisabledReason)
			continue
		}
		args, available := sshArgs(machine.ID, route, false)
		if !available {
			lastDetail = "транспорт недоступен"
			continue
		}
		attempted = append(attempted, routeLabel(route.Kind))
		probeArgs := append([]string{"-o", "BatchMode=yes", "-o", "ConnectTimeout=3"}, args...)
		probeArgs = append(probeArgs, "true")
		output, err := exec.CommandContext(ctx, "ssh", probeArgs...).CombinedOutput()
		if err != nil {
			lastDetail = classifyRouteFailure(ctx, route, output)
			// A Wi-Fi address that stopped answering is usually not dead: DHCP
			// moved it. Rediscover before giving up on this route.
			if route.Kind == "lan" && staleLANFailure(route, ctx, output) {
				discovery, discoverErr := discoverLAN(ctx, machine)
				switch {
				case discoverErr == nil && discovery.authed:
					retryRoute := route
					retryRoute.Target = replaceHost(route.Target, discovery.ip)
					retryArgs, ok := sshArgs(machine.ID, retryRoute, false)
					if ok {
						retryProbe := append([]string{"-o", "BatchMode=yes", "-o", "ConnectTimeout=4"}, retryArgs...)
						retryProbe = append(retryProbe, "true")
						if retryErr := exec.CommandContext(ctx, "ssh", retryProbe...).Run(); retryErr == nil {
							route = retryRoute
							args = retryArgs
							discoveredLAN = discovery.ip
							lastDetail = ""
						} else {
							lastDetail = "доступен по обнаруженному адресу, но ssh не прошёл"
						}
					}
				case discoverErr != nil:
					lastDetail = lanDiscoveryFailure(discoverErr, lanDiscovery{})
				default:
					lastDetail = lanDiscoveryFailure(errors.New("not authorized"), discovery)
				}
			}
			if lastDetail != "" {
				continue
			}
		}
		machine.ActiveRoute = route.Kind
		machine.Available = true
		machine.LANDiscovered = discoveredLAN
		machine.Detail = "SSH доступен через " + routeLabel(route.Kind)
		return Connection{Machine: machine, Route: route, Binary: "ssh", SSHOptions: append([]string(nil), args[:len(args)-1]...)}, nil
	}
	if len(attempted) == 0 {
		if lastDetail != "" {
			return Connection{}, fmt.Errorf("для %s нет рабочего транспорта: %s", machine.Title, lastDetail)
		}
		return Connection{}, fmt.Errorf("для %s нет настроенного транспорта", machine.Title)
	}
	detail := strings.Join(attempted, " → ")
	if lastDetail != "" {
		detail += ": " + lastDetail
	}
	return Connection{}, fmt.Errorf("%s: ни один маршрут не ответил (%s)", machine.Title, detail)
}

// AttachCommand is kept for CLI callers. The console itself calls Resolve so
// it can complete the fallback before replacing its TUI with an SSH terminal.
func AttachCommand(machine Machine) (string, []string, error) {
	if machine.Kind == "local" {
		return localShell()
	}
	route, ok := activeOrFirstRoute(machine)
	if !ok {
		return "", nil, fmt.Errorf("машина %s не имеет SSH target", machine.ID)
	}
	args, available := sshArgs(machine.ID, route, false)
	if !available {
		return "", nil, fmt.Errorf("транспорт %s для %s недоступен на этой машине", routeLabel(route.Kind), machine.Title)
	}
	return "ssh", append([]string{"-tt"}, args...), nil
}

func canonicalInventory() []Machine {
	inventory := []Machine{
		logical("macbook-pro", "MacBook Pro", "operator@100.125.251.6", "operator@192.168.1.68", defaultSSHPort),
		logical("macbook-air-lizok", "MacBook Air", "timosh@100.100.226.43", "timosh@192.168.1.65", defaultSSHPort),
		logical("ubuntu", "Ubuntu", "timofeyka@100.96.3.88", "timofeyka@192.168.1.75", defaultSSHPort),
		logical("antix", "antiX", "operator@100.78.99.55", "operator@192.168.1.77", defaultSSHPort),
		logical("laptop-windows", "Windows WSL", "user@100.109.27.59", "user@192.168.1.15", defaultSSHPort),
		logical("bky-w09", "BKY-W09", "u0_a191@100.73.247.92", "u0_a191@192.168.1.79", 8022),
		// Never fall back to the old 192.168.1.75 mapping: it collides with
		// Ubuntu and could attach work to the wrong physical machine.
		{
			ID: "wsl-tinker", Title: "WSL Tinker", Kind: "ssh",
			Routes: []Route{
				{Kind: "tailscale", Target: "megur@100.97.178.61", Port: defaultSSHPort},
				{Kind: "tailnet-direct", Target: "megur@100.97.178.61", Port: defaultSSHPort},
				{Kind: "socks", Target: "megur@100.97.178.61", Port: defaultSSHPort},
				{Kind: "lan", Target: "megur@192.168.1.75", Port: defaultSSHPort, DisabledReason: "старый IP конфликтует с Ubuntu"},
			},
		},
		// The canonical inventory used `d0lsi@` here, which answers
		// `Permission denied` on this host: the login that actually works is
		// `operator@` (see `Host asus-laptop-tailscale` in ~/.ssh/config), so the
		// TUI reported the machine as unreachable while it was fine.
		logical("d0lsi-invoker", "d0lsi Invoker", "operator@100.124.14.15", "", defaultSSHPort),
	}
	// Bonjour names let a machine be found again after DHCP hands it a new
	// address. They are discovery hints only and never become selectable
	// identities in the UI.
	for i := range inventory {
		if name := mdnsNameFor(inventory[i].ID); name != "" {
			inventory[i].MDNS = name
		}
	}
	return inventory
}

func mdnsNameFor(id string) string {
	switch id {
	case "macbook-air-lizok":
		return "MacBook-Air-lizok.local"
	case "macbook-pro":
		return "MacBook-Pro.local"
	default:
		return ""
	}
}

func logical(id, title, tailnet, lan string, port int) Machine {
	routes := []Route{
		{Kind: "tailscale", Target: tailnet, Port: port},
		{Kind: "tailnet-direct", Target: tailnet, Port: port},
		{Kind: "socks", Target: tailnet, Port: port},
	}
	if lan != "" {
		routes = append(routes, Route{Kind: "lan", Target: lan, Port: port})
	}
	return Machine{ID: id, Title: title, Kind: "ssh", Routes: routes}
}

func activeOrFirstRoute(machine Machine) (Route, bool) {
	for _, route := range machine.Routes {
		if route.Kind == machine.ActiveRoute && route.Target != "" && route.DisabledReason == "" {
			return route, true
		}
	}
	for _, route := range machine.Routes {
		if route.Target != "" && route.DisabledReason == "" {
			return route, true
		}
	}
	if machine.Target != "" {
		return Route{Kind: "tailscale", Target: machine.Target}, true
	}
	return Route{}, false
}

func sshArgs(hostKeyAlias string, route Route, interactive bool) ([]string, bool) {
	port := route.Port
	if port == 0 {
		port = defaultSSHPort
	}
	args := []string{"-o", "StrictHostKeyChecking=accept-new", "-p", fmt.Sprintf("%d", port)}
	// One identity per logical machine: the same host is reached over its
	// tailnet IP, its LAN IP and SOCKS, and a LAN IP can change owner (WSL used
	// to own 192.168.1.75, Ubuntu owns it now). Keying known_hosts by the
	// logical id keeps one entry per machine instead of one per address.
	if alias := strings.TrimSpace(hostKeyAlias); alias != "" {
		args = append(args, "-o", "HostKeyAlias="+alias)
	}
	switch route.Kind {
	case "tailscale":
		proxy, ok := tailnetProxyCommand()
		if !ok {
			return nil, false
		}
		args = append(args, "-o", "ProxyCommand="+proxy)
	case "socks":
		proxy, ok := socksProxyCommand()
		if !ok {
			return nil, false
		}
		args = append(args, "-o", "ProxyCommand="+proxy)
	case "tailnet-direct", "lan":
		// Direct tailnet is a valid fallback on hosts with a normal Tailscale
		// network interface. LAN is tried only after every private route.
	default:
		return nil, false
	}
	if interactive {
		args = append([]string{"-tt"}, args...)
	}
	return append(args, route.Target), true
}

// tailscaleBinaryAndSocket resolves the tailscale CLI and its socket. The
// socket matters: on macOS the user daemon may not be the one in PATH, and a
// peer-state query against the wrong daemon reports every machine as offline.
func tailscaleBinaryAndSocket() (binary, socket string, ok bool) {
	binary = strings.TrimSpace(firstNonEmpty(os.Getenv("FIXER_TAILSCALE_CLI"), os.Getenv("TAILSCALE_CLI")))
	if binary == "" {
		for _, candidate := range []string{
			"/home/operator/bin/tailscale",
			"/Applications/Tailscale.app/Contents/MacOS/Tailscale",
			"/opt/homebrew/bin/tailscale",
			"/usr/local/bin/tailscale",
		} {
			if info, err := os.Stat(candidate); err == nil && !info.IsDir() && info.Mode()&0o111 != 0 {
				binary = candidate
				break
			}
		}
	}
	if binary == "" {
		if found, err := exec.LookPath("tailscale"); err == nil {
			binary = found
		}
	}
	if binary == "" {
		return "", "", false
	}

	socket = strings.TrimSpace(firstNonEmpty(os.Getenv("TAILSCALE_SOCKET"), os.Getenv("FIXER_TAILSCALE_SOCKET")))
	if socket == "" && runtime.GOOS == "darwin" {
		if home, err := os.UserHomeDir(); err == nil {
			candidate := filepath.Join(home, ".local", "share", "tailscale-cli", "tailscaled.sock")
			if _, err := os.Stat(candidate); err == nil {
				socket = candidate
			}
		}
	}
	return binary, socket, true
}

func tailscaleProxyCommand() (string, bool) {
	binary, socket, ok := tailscaleBinaryAndSocket()
	if !ok {
		return "", false
	}

	parts := []string{shellQuote(binary)}
	if socket != "" {
		parts = append(parts, "--socket="+shellQuote(socket))
	}
	parts = append(parts, "nc", "%h", "%p")
	return strings.Join(parts, " "), true
}

func socksProxyCommand() (string, bool) {
	if _, err := exec.LookPath("nc"); err != nil {
		return "", false
	}
	return "nc -x " + shellQuote(socksProxyAddr()) + " -X 5 %h %p", true
}

// socksProxyAddr is the SOCKS lane endpoint: explicit operator overrides win,
// otherwise the fleet default where the userspace tailscaled listens.
func socksProxyAddr() string {
	proxy := strings.TrimSpace(firstNonEmpty(os.Getenv("FIXER_SOCKS_PROXY"), os.Getenv("SSH_TUI_SOCKS_PROXY")))
	if proxy == "" {
		proxy = proxyFromEnv(firstNonEmpty(os.Getenv("ALL_PROXY"), os.Getenv("all_proxy")))
	}
	if proxy == "" {
		proxy = "127.0.0.1:1055"
	}
	return proxy
}

// socksLaneDial is injectable so the lane decision is testable without a live
// proxy on the machine.
var socksLaneDial = func(addr string) bool {
	conn, err := net.DialTimeout("tcp", addr, 400*time.Millisecond)
	if err != nil {
		return false
	}
	_ = conn.Close()
	return true
}

// socksProxyIfUp reports the SOCKS lane proxy only when the lane is actually
// up. An explicit operator override is trusted as-is; the default endpoint is
// dial-checked so hosts without the lane fall back to the tailscale CLI proxy.
func socksProxyIfUp() (string, bool) {
	explicit := strings.TrimSpace(firstNonEmpty(os.Getenv("FIXER_SOCKS_PROXY"), os.Getenv("SSH_TUI_SOCKS_PROXY"))) != ""
	if !explicit && !socksLaneDial(socksProxyAddr()) {
		return "", false
	}
	return socksProxyCommand()
}

// tailnetProxyCommand picks the proxy for tailnet routes. The SOCKS lane —
// the transport ssh-tui uses — is preferred whenever it is up: a userspace
// tailscaled exposes no kernel route to 100.x, and the tailscale CLI proxy has
// been seen hanging past any connect budget on such daemons while the SOCKS
// hop answers immediately (Air, 2026-09-29). Fallback stays the CLI proxy.
func tailnetProxyCommand() (string, bool) {
	if proxy, ok := socksProxyIfUp(); ok {
		return proxy, true
	}
	return tailscaleProxyCommand()
}

// timeoutClassFailure reports whether a failed probe attempt is a pure
// reachability timeout — the only failure class worth retrying.
func timeoutClassFailure(ctx context.Context, output []byte) bool {
	if ctx.Err() != nil {
		return true
	}
	lower := strings.ToLower(string(output))
	for _, marker := range []string{"operation timed out", "connection timed out", "timed out", "timeout"} {
		if strings.Contains(lower, marker) {
			return true
		}
	}
	return false
}

func proxyFromEnv(raw string) string {
	value := strings.TrimSpace(raw)
	for _, prefix := range []string{"socks5h://", "socks5://", "socks://"} {
		if strings.HasPrefix(strings.ToLower(value), prefix) {
			value = value[len(prefix):]
			break
		}
	}
	if slash := strings.IndexByte(value, '/'); slash >= 0 {
		value = value[:slash]
	}
	if at := strings.LastIndexByte(value, '@'); at >= 0 {
		value = value[at+1:]
	}
	return strings.TrimSpace(value)
}

func routeHint(machine Machine) string {
	labels := make([]string, 0, len(machine.Routes))
	for _, route := range machine.Routes {
		if route.DisabledReason != "" {
			continue
		}
		label := routeLabel(route.Kind)
		if len(labels) == 0 || labels[len(labels)-1] != label {
			labels = append(labels, label)
		}
	}
	hint := strings.Join(labels, " → ")
	for _, route := range machine.Routes {
		if route.DisabledReason != "" {
			if hint != "" {
				hint += " · "
			}
			hint += routeLabel(route.Kind) + " отключён: " + route.DisabledReason
		}
	}
	return hint
}

func routeLabel(kind string) string {
	switch kind {
	case "tailscale", "tailnet-direct":
		return "Tailscale"
	case "socks":
		return "SOCKS"
	case "lan":
		return "LAN"
	default:
		return kind
	}
}

func configuredOverrides() map[string]Machine {
	home, err := os.UserHomeDir()
	if err != nil {
		return nil
	}
	configHome := os.Getenv("XDG_CONFIG_HOME")
	if configHome == "" {
		configHome = filepath.Join(home, ".config")
	}
	data, err := os.ReadFile(filepath.Join(configHome, "fixer", "machines.json"))
	if err != nil {
		return nil
	}
	var configured []Machine
	if err := json.Unmarshal(data, &configured); err != nil {
		return nil
	}
	allowed := map[string]bool{}
	for _, machine := range canonicalInventory() {
		allowed[machine.ID] = true
	}
	overrides := map[string]Machine{}
	for _, machine := range configured {
		if allowed[machine.ID] {
			overrides[machine.ID] = machine
		}
	}
	return overrides
}

func mergeOverride(base, override Machine) Machine {
	if value := strings.TrimSpace(override.Title); value != "" {
		base.Title = value
	}
	if value := strings.TrimSpace(override.Path); value != "" {
		base.Path = value
	}
	if value := strings.TrimSpace(override.MDNS); value != "" {
		base.MDNS = value
	}
	if len(override.Routes) > 0 {
		base.Routes = append([]Route(nil), override.Routes...)
	} else if value := strings.TrimSpace(override.Target); value != "" {
		// Migration path for the old one-target config. It changes the
		// canonical primary transport but does not add a new visible machine.
		base.Routes = []Route{{Kind: "tailscale", Target: value}}
	}
	return base
}

func currentLogicalID() string {
	host, _ := os.Hostname()
	value := strings.ToLower(host)
	switch {
	case strings.Contains(value, "macbook-pro"):
		return "macbook-pro"
	case strings.Contains(value, "macbook-air") || strings.Contains(value, "macbook_air"):
		return "macbook-air-lizok"
	case strings.Contains(value, "wsl") || strings.Contains(value, "tinker"):
		return "wsl-tinker"
	case strings.Contains(value, "d0lsi"):
		return "d0lsi-invoker"
	case strings.Contains(value, "bky-w09"):
		return "bky-w09"
	case strings.Contains(value, "antix"):
		return "antix"
	case strings.Contains(value, "ubuntu"):
		return "ubuntu"
	case strings.Contains(value, "windows"):
		return "laptop-windows"
	default:
		return ""
	}
}

func localShell() (string, []string, error) {
	if shell, err := exec.LookPath("zsh"); err == nil {
		return shell, []string{"-l"}, nil
	}
	if shell, err := exec.LookPath("bash"); err == nil {
		return shell, []string{"-l"}, nil
	}
	if shell, err := exec.LookPath("sh"); err == nil {
		return shell, []string{"-l"}, nil
	}
	return "", nil, errors.New("не найден shell для локального терминала")
}

var (
	errLANNoRoute  = errors.New("у машины нет LAN-маршрута")
	errLANNotFound = errors.New("адрес машины не найден в этой Wi-Fi-сети")

	lanCacheMu    sync.Mutex
	lanCacheStore = map[string]lanDiscovery{}
)

// lanDiscovery is the outcome of finding a machine whose DHCP address moved.
type lanDiscovery struct {
	ip     string
	authed bool
}

func cacheLAN(machineID string, result lanDiscovery) {
	lanCacheMu.Lock()
	defer lanCacheMu.Unlock()
	lanCacheStore[machineID+"@"+time.Now().UTC().Format("200601021504")] = result
	cutoff := time.Now().UTC().Add(-lanCacheTTL)
	for key, value := range lanCacheStore {
		stamp, err := time.Parse("200601021504", key[strings.LastIndex(key, "@")+1:])
		if err != nil || stamp.Before(cutoff) {
			delete(lanCacheStore, key)
		}
		_ = value
	}
}

func cachedLAN(machineID string) (lanDiscovery, bool) {
	lanCacheMu.Lock()
	defer lanCacheMu.Unlock()
	for key, value := range lanCacheStore {
		if strings.HasPrefix(key, machineID+"@") {
			return value, true
		}
	}
	return lanDiscovery{}, false
}

func lanRoute(machine Machine) (Route, bool) {
	for _, route := range machine.Routes {
		if route.Kind == "lan" && strings.TrimSpace(route.DisabledReason) == "" && strings.TrimSpace(route.Target) != "" {
			return route, true
		}
	}
	return Route{}, false
}

// discoverLAN finds the machine's current Wi-Fi address without requiring the
// configured one to be correct. Candidates come from Bonjour first (cheapest
// and self-healing) and then from the /24 that owns the configured address —
// the very subnet whose DHCP range caused the problem. A candidate only counts
// once SSH authenticates with this machine's user, so a sweep can never attach
// the operator to somebody else's host.
func discoverLAN(ctx context.Context, machine Machine) (lanDiscovery, error) {
	if cached, ok := cachedLAN(machine.ID); ok {
		return cached, nil
	}
	route, ok := lanRoute(machine)
	if !ok {
		return lanDiscovery{}, errLANNoRoute
	}

	host, _ := splitTarget(route.Target)
	mdnsIP := ""
	// Fast path first: identity-bearing candidates (Bonjour, ARP neighbours,
	// the configured address) are auth-checked before any subnet sweep. A /24
	// sweep is pathological on some Wi-Fi networks — nc to absent hosts can
	// hang far past its timeout, and one sweep did not finish in five minutes
	// on a 2020 Air — so it must never be the reason a healthy machine reads as
	// not found (macbook-air-lizok -> macbook-pro, 2026-09-29).
	priority := make([]string, 0, 8)
	if mdns := strings.TrimSpace(machine.MDNS); mdns != "" {
		if ip, err := resolveMDNS(mdns); err == nil && ip != "" {
			mdnsIP = ip
			priority = append(priority, ip)
		}
	}
	priority = appendUniqueIPs(priority, parseARPCandidates(arpTableText(), host))
	if host != "" {
		priority = appendUniqueIPs(priority, []string{host})
	}
	priority = withoutOwnAddresses(priority)
	if discovery, found := firstAuthedLAN(ctx, route, priority, mdnsIP); found {
		cacheLAN(machine.ID, discovery)
		return discovery, nil
	}

	candidates := withoutOwnAddresses(subnetCandidates(route.Target))
	if len(candidates) == 0 {
		return lanDiscovery{}, errLANNotFound
	}
	open := openLANAddresses(ctx, candidates, lanRoutePort(route))
	if discovery, found := firstAuthedLAN(ctx, route, open, mdnsIP); found {
		cacheLAN(machine.ID, discovery)
		return discovery, nil
	}
	return lanDiscovery{}, errLANNotFound
}

func lanRoutePort(route Route) int {
	if route.Port == 0 {
		return defaultSSHPort
	}
	return route.Port
}

// firstAuthedLAN auth-checks candidates concurrently and returns as soon as one
// of them accepts this machine's key. Only the Bonjour candidate may be
// reported found without a key: a permission-denied answer from an ARP or
// sweep candidate means somebody else lives there and must never be attributed
// to this machine.
func firstAuthedLAN(ctx context.Context, route Route, candidates []string, mdnsIP string) (lanDiscovery, bool) {
	if len(candidates) == 0 {
		return lanDiscovery{}, false
	}
	type probeResult struct {
		ip  string
		err error
	}
	results := make(chan probeResult, len(candidates))
	var (
		authWait sync.WaitGroup
		authSem  = make(chan struct{}, 6)
	)
	for _, ip := range candidates {
		authWait.Add(1)
		go func(ip string) {
			defer authWait.Done()
			select {
			case authSem <- struct{}{}:
			case <-ctx.Done():
				results <- probeResult{ip: ip, err: ctx.Err()}
				return
			}
			defer func() { <-authSem }()
			results <- probeResult{ip: ip, err: authProbeLAN(ctx, route, ip)}
		}(ip)
	}
	authWait.Wait()
	close(results)
	fallback := lanDiscovery{}
	for result := range results {
		if result.err == nil {
			return lanDiscovery{ip: result.ip, authed: true}, true
		}
		if mdnsIP != "" && result.ip == mdnsIP && strings.Contains(strings.ToLower(result.err.Error()), "permission denied") {
			fallback = lanDiscovery{ip: result.ip, authed: false}
		}
	}
	if fallback.ip != "" {
		return fallback, true
	}
	return lanDiscovery{}, false
}

var arpIPPattern = regexp.MustCompile(`\b(?:\d{1,3}\.){3}\d{1,3}\b`)

// arpTableText reads the host's ARP/neighbour table; failure is not an error,
// it just leaves the fast path smaller.
func arpTableText() string {
	var cmd *exec.Cmd
	if runtime.GOOS == "darwin" {
		cmd = exec.Command("arp", "-a")
	} else {
		cmd = exec.Command("ip", "neigh", "show")
	}
	out, err := cmd.Output()
	if err != nil {
		return ""
	}
	return string(out)
}

// parseARPCandidates extracts neighbour addresses on the same /24 as the
// configured host — instant candidates that skip the sweep entirely.
func parseARPCandidates(table, host string) []string {
	ip := net.ParseIP(host)
	if ip == nil || ip.To4() == nil {
		return nil
	}
	prefix := strings.Join(strings.Split(ip.To4().String(), ".")[:3], ".") + "."
	result := make([]string, 0, 8)
	seen := make(map[string]bool)
	for _, match := range arpIPPattern.FindAllString(table, -1) {
		if !strings.HasPrefix(match, prefix) || seen[match] || strings.HasSuffix(match, ".255") {
			continue
		}
		seen[match] = true
		result = append(result, match)
	}
	return result
}

// withoutOwnAddresses drops this machine's own addresses so a sweep never
// tries to authenticate into the console it is running on.
func withoutOwnAddresses(candidates []string) []string {
	own := map[string]bool{}
	if interfaces, err := net.Interfaces(); err == nil {
		for _, network := range interfaces {
			addresses, _ := network.Addrs()
			for _, address := range addresses {
				if ipnet, ok := address.(*net.IPNet); ok {
					own[ipnet.IP.String()] = true
				} else {
					own[address.String()] = true
				}
			}
		}
	}
	result := candidates[:0]
	for _, candidate := range candidates {
		if own[candidate] {
			continue
		}
		result = append(result, candidate)
	}
	return result
}

func authProbeLAN(ctx context.Context, route Route, ip string) error {
	// splitTarget deliberately drops `user@`, so the identity is read from the
	// raw target here: without it every candidate reports "no user" and
	// discovery reports a healthy machine as not found.
	user := ""
	if at := strings.Index(route.Target, "@"); at > 0 {
		user = route.Target[:at]
	}
	if user == "" {
		return errors.New("в LAN-маршруте нет пользователя")
	}
	port := route.Port
	if port == 0 {
		port = defaultSSHPort
	}
	args := []string{
		"-o", "BatchMode=yes", "-o", "ConnectTimeout=3",
		// Discovery must not rewrite known_hosts: identity is proven by the
		// successful login, and a wrong candidate must leave no trace.
		"-o", "StrictHostKeyChecking=no", "-o", "UserKnownHostsFile=/dev/null", "-o", "LogLevel=ERROR",
		"-p", strconv.Itoa(port), user + "@" + ip, "true",
	}
	output, err := exec.CommandContext(ctx, "ssh", args...).CombinedOutput()
	if err == nil {
		return nil
	}
	return errors.New(strings.TrimSpace(string(output)))
}

// openLANAddresses sweeps the candidate list concurrently and returns the
// addresses that accept a TCP connection on the SSH port.
//
// Results are cached per subnet for a minute: every machine on that subnet
// pays for one sweep instead of re-walking the whole address range.
//
// The system nc(1) is used on purpose: on the operator's Wi-Fi a raw
// net.Dial() to a live LAN host can fail with EHOSTUNREACH while nc(1) and
// ssh(1) reach it fine (observed while rediscovering MacBook Air after a
// reconnect). A Go-only probe would therefore report healthy machines as dead.
func openLANAddresses(ctx context.Context, candidates []string, port int) []string {
	cacheKey := fmt.Sprintf("%s:%d", subnetKey(candidates), port)
	if cached, ok := cachedSweep(cacheKey); ok {
		return filterCandidates(cached, candidates)
	}
	ncPath, ncErr := exec.LookPath("nc")
	var (
		mu    sync.Mutex
		wait  sync.WaitGroup
		open  []string
		limit = make(chan struct{}, 32)
	)
	for _, ip := range candidates {
		wait.Add(1)
		go func(ip string) {
			defer wait.Done()
			select {
			case limit <- struct{}{}:
			case <-ctx.Done():
				return
			}
			defer func() { <-limit }()

			reachable := false
			if ncErr == nil {
				probeCtx, cancel := context.WithTimeout(ctx, 1200*time.Millisecond)
				defer cancel()
				reachable = exec.CommandContext(probeCtx, ncPath, "-z", "-w", "1", ip, strconv.Itoa(port)).Run() == nil
			} else {
				dialer := net.Dialer{Timeout: 900 * time.Millisecond}
				conn, err := dialer.DialContext(ctx, "tcp", net.JoinHostPort(ip, strconv.Itoa(port)))
				if err == nil {
					reachable = true
					_ = conn.Close()
				}
			}
			if !reachable {
				return
			}
			mu.Lock()
			open = append(open, ip)
			mu.Unlock()
		}(ip)
	}
	wait.Wait()
	sort.Strings(open)
	cacheSweep(cacheKey, open)
	return open
}

const sweepCacheTTL = 60 * time.Second

var (
	sweepCacheMu    sync.Mutex
	sweepCacheStore = map[string]sweepCache{}
)

type sweepCache struct {
	ips      []string
	captured time.Time
}

func subnetKey(candidates []string) string {
	if len(candidates) == 0 {
		return ""
	}
	ip := net.ParseIP(candidates[len(candidates)-1])
	if ip == nil {
		return ""
	}
	if v4 := ip.To4(); v4 != nil {
		return fmt.Sprintf("%d.%d.%d.0", v4[0], v4[1], v4[2])
	}
	return ""
}

func cachedSweep(key string) ([]string, bool) {
	if key == "" {
		return nil, false
	}
	sweepCacheMu.Lock()
	defer sweepCacheMu.Unlock()
	entry, ok := sweepCacheStore[key]
	if !ok || time.Since(entry.captured) > sweepCacheTTL {
		return nil, false
	}
	return append([]string(nil), entry.ips...), true
}

func cacheSweep(key string, ips []string) {
	if key == "" {
		return
	}
	// An empty sweep must never be cached: it is usually a budget casualty, and
	// caching it would pin every retry to a known-wrong answer.
	if len(ips) == 0 {
		return
	}
	sweepCacheMu.Lock()
	defer sweepCacheMu.Unlock()
	sweepCacheStore[key] = sweepCache{ips: append([]string(nil), ips...), captured: time.Now()}
}

func filterCandidates(open, candidates []string) []string {
	allowed := make(map[string]bool, len(candidates))
	for _, candidate := range candidates {
		allowed[candidate] = true
	}
	result := open[:0]
	for _, ip := range open {
		if allowed[ip] {
			result = append(result, ip)
		}
	}
	return result
}

// subnetCandidates lists the /24 that owns the configured LAN address. The
// stale address itself is skipped: it has already been tried and failed.
func subnetCandidates(target string) []string {
	host, _ := splitTarget(target)
	ip := net.ParseIP(host)
	if ip == nil || ip.To4() == nil {
		return nil
	}
	bytes := ip.To4()
	result := make([]string, 0, 253)
	for last := 1; last <= 254; last++ {
		candidate := fmt.Sprintf("%d.%d.%d.%d", bytes[0], bytes[1], bytes[2], last)
		if candidate == host {
			continue
		}
		result = append(result, candidate)
	}
	return result
}

func resolveMDNS(name string) (string, error) {
	addresses, err := net.LookupHost(name)
	if err != nil {
		return "", err
	}
	for _, address := range addresses {
		parsed := net.ParseIP(address)
		if parsed != nil && parsed.To4() != nil && !parsed.IsLoopback() {
			return parsed.String(), nil
		}
	}
	return "", errors.New("mDNS не вернул IPv4")
}

func appendUniqueIPs(base, extra []string) []string {
	seen := make(map[string]bool, len(base)+len(extra))
	for _, value := range base {
		seen[value] = true
	}
	for _, value := range extra {
		if value == "" || seen[value] {
			continue
		}
		seen[value] = true
		base = append(base, value)
	}
	return base
}

func cloneMachines(input []Machine) []Machine {
	out := make([]Machine, len(input))
	for i, machine := range input {
		out[i] = machine
		out[i].Routes = append([]Route(nil), machine.Routes...)
	}
	return out
}

func shellQuote(value string) string {
	return "'" + strings.ReplaceAll(value, "'", "'\\\"'\\\"'") + "'"
}

func firstNonEmpty(values ...string) string {
	for _, value := range values {
		if strings.TrimSpace(value) != "" {
			return strings.TrimSpace(value)
		}
	}
	return ""
}
