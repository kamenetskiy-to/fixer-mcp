package machines

import (
	"context"
	"os"
	"path/filepath"
	"strings"
	"testing"
)

func TestDiscoverUsesBoundedCanonicalInventoryAndKeepsLocalFirst(t *testing.T) {
	t.Setenv("XDG_CONFIG_HOME", t.TempDir())
	machines := Discover()
	if len(machines) < 7 || len(machines) > 9 {
		t.Fatalf("logical fleet size = %d, want bounded canonical inventory: %+v", len(machines), machines)
	}
	if machines[0].ID != "local" || machines[0].Kind != "local" {
		t.Fatalf("local machine must be first: %+v", machines[0])
	}
	for _, machine := range machines {
		if strings.Contains(machine.ID, "-tailscale") || strings.Contains(machine.ID, "-local") {
			t.Fatalf("transport alias leaked into logical inventory: %+v", machine)
		}
	}
}

func TestDiscoverAppliesOnlyCanonicalConfigOverrides(t *testing.T) {
	xdg := t.TempDir()
	t.Setenv("XDG_CONFIG_HOME", xdg)
	configDir := filepath.Join(xdg, "fixer")
	if err := os.MkdirAll(configDir, 0o700); err != nil {
		t.Fatal(err)
	}
	config := `[
		{"id":"wsl-tinker","title":"WSL for work","path":"/home/megur/project"},
		{"id":"personal-ssh-alias","title":"Must not become a machine","target":"example.org","kind":"ssh"}
	]`
	if err := os.WriteFile(filepath.Join(configDir, "machines.json"), []byte(config), 0o600); err != nil {
		t.Fatal(err)
	}
	machines := Discover()
	var wsl *Machine
	for i := range machines {
		if machines[i].ID == "personal-ssh-alias" {
			t.Fatalf("unapproved SSH alias became a machine: %+v", machines[i])
		}
		if machines[i].ID == "wsl-tinker" {
			wsl = &machines[i]
		}
	}
	if wsl == nil {
		t.Fatalf("wsl-tinker absent from canonical inventory: %+v", machines)
	}
	if wsl.Title != "WSL for work" || wsl.Path != "/home/megur/project" {
		t.Fatalf("canonical override not applied: %+v", wsl)
	}
}

func TestWSLTinkerNeverFallsBackToConflictedLAN(t *testing.T) {
	var wsl Machine
	for _, machine := range canonicalInventory() {
		if machine.ID == "wsl-tinker" {
			wsl = machine
			break
		}
	}
	if len(wsl.Routes) == 0 {
		t.Fatal("missing WSL routes")
	}
	for _, route := range wsl.Routes {
		if route.Kind == "lan" && route.DisabledReason == "" {
			t.Fatalf("unsafe WSL LAN route is enabled: %+v", route)
		}
	}
	if !strings.Contains(routeHint(wsl), "конфликтует") {
		t.Fatalf("route hint must explain disabled LAN fallback: %q", routeHint(wsl))
	}
}

func TestRouteStatusSummaryShowsTailnetAndWifiSides(t *testing.T) {
	got := routeStatusSummary(Machine{}, []RouteStatus{
		{Kind: "tailscale", Available: true},
		{Kind: "socks", Detail: "транспорт недоступен"},
		{Kind: "lan", Available: false, Detail: "нет ответа"},
	})
	if !strings.Contains(got, "Tailscale ✓") {
		t.Fatalf("reachable tailnet side must be visible: %q", got)
	}
	if !strings.Contains(got, "Wi-Fi/LAN ✗") {
		t.Fatalf("unreachable Wi-Fi side must be visible: %q", got)
	}

	disabled := routeStatusSummary(Machine{}, []RouteStatus{
		{Kind: "tailscale", Available: true},
		{Kind: "lan", Available: false, Detail: "отключён: старый IP конфликтует с Ubuntu"},
	})
	if !strings.Contains(disabled, "Wi-Fi/LAN — отключён") {
		t.Fatalf("an intentionally disabled route must not read as a failure: %q", disabled)
	}
}

func TestProbeRoutesKeepsEveryTransportExactlyOnce(t *testing.T) {
	var wsl Machine
	for _, machine := range canonicalInventory() {
		if machine.ID == "wsl-tinker" {
			wsl = machine
		}
	}
	if wsl.ID == "" {
		t.Fatal("wsl-tinker missing from inventory")
	}
	routes := probeRoutes(wsl)
	seen := map[string]bool{}
	for _, route := range routes {
		if seen[route.Kind] {
			t.Fatalf("route kind %q probed twice: %+v", route.Kind, routes)
		}
		seen[route.Kind] = true
	}
	lan, ok := func() (Route, bool) {
		for _, route := range routes {
			if route.Kind == "lan" {
				return route, true
			}
		}
		return Route{}, false
	}()
	if !ok || lan.DisabledReason == "" {
		t.Fatalf("disabled LAN route must stay visible during probing: %+v", routes)
	}
}

func TestLANDiscoveryHelpers(t *testing.T) {
	candidates := subnetCandidates("timosh@192.168.1.65")
	if len(candidates) != 253 {
		t.Fatalf("candidates = %d, want the /24 minus the stale address", len(candidates))
	}
	for _, candidate := range candidates {
		if candidate == "192.168.1.65" {
			t.Fatal("the already-tried stale address must not be swept again")
		}
		if !strings.HasPrefix(candidate, "192.168.1.") {
			t.Fatalf("candidate %q escaped the configured subnet", candidate)
		}
	}
	if subnetCandidates("MacBook-Air-lizok.local") != nil {
		t.Fatal("a non-IP target must not produce a sweep")
	}

	if got := replaceHost("timosh@192.168.1.65", "192.168.1.69"); got != "timosh@192.168.1.69" {
		t.Fatalf("replaceHost = %q", got)
	}
	if got := replaceHost("timosh@192.168.1.65:2222", "192.168.1.69"); got != "timosh@192.168.1.69:2222" {
		t.Fatalf("replaceHost with port = %q", got)
	}
	host, port := splitTarget("u0_a191@192.168.1.79:8022")
	if host != "192.168.1.79" || port != "8022" {
		t.Fatalf("splitTarget = (%q, %q)", host, port)
	}
}

// After a Wi-Fi reconnect DHCP hands machines new addresses, so a failing LAN
// probe must be recognised as "stale address" (rediscoverable) rather than as
// a dead host or an identity failure.
func TestStaleLANFailureIsRecognised(t *testing.T) {
	route := Route{Kind: "lan", Target: "timosh@192.168.1.65", Port: 22}
	for _, output := range []string{
		"ssh: connect to host 192.168.1.65 port 22: Operation timed out",
		"ssh: connect to host 192.168.1.65 port 22: Host is down",
		"ssh: connect to host 192.168.1.65 port 22: Connection refused",
	} {
		if !staleLANFailure(route, context.Background(), []byte(output)) {
			t.Fatalf("%q must trigger rediscovery", output)
		}
	}
	// An identity or authorization failure is a different problem: the address
	// is current, so rediscovering it would only waste time.
	if staleLANFailure(route, context.Background(), []byte("Permission denied (publickey,password).")) {
		t.Fatal("an authorization failure must not trigger rediscovery")
	}
	if staleLANFailure(Route{Kind: "lan", DisabledReason: "конфликт"}, context.Background(), []byte("Operation timed out")) {
		t.Fatal("a deliberately disabled route must never be rediscovered")
	}
}

func TestRouteSummaryShowsTheDiscoveredAddress(t *testing.T) {
	got := routeStatusSummary(
		Machine{ID: "macbook-air-lizok", LANDiscovered: "192.168.1.69"},
		[]RouteStatus{{Kind: "tailscale", Available: true}, {Kind: "lan", Available: true}},
	)
	if !strings.Contains(got, "Tailscale ✓") || !strings.Contains(got, "Wi-Fi/LAN ✓ 192.168.1.69") {
		t.Fatalf("summary must show where the machine was found: %q", got)
	}
}

// d0lsi Invoker answers `Permission denied` for the login the inventory used to
// carry, which made a perfectly healthy machine read as unreachable. The
// working login is the one in `Host asus-laptop-tailscale`.
func TestInventoryUsesTheLoginThatActuallyWorksForD0LSI(t *testing.T) {
	for _, machine := range canonicalInventory() {
		if machine.ID != "d0lsi-invoker" {
			continue
		}
		if len(machine.Routes) == 0 {
			t.Fatalf("d0lsi-invoker has no routes: %+v", machine)
		}
		for _, route := range machine.Routes {
			if !strings.HasPrefix(route.Target, "operator@") {
				t.Fatalf("d0lsi-invoker route %q uses a login that is not authorized from this host: %q", route.Kind, route.Target)
			}
		}
		return
	}
	t.Fatal("d0lsi-invoker missing from the canonical inventory")
}

func TestInventoryCarriesBonjourHints(t *testing.T) {
	var air, wsl Machine
	for _, machine := range canonicalInventory() {
		switch machine.ID {
		case "macbook-air-lizok":
			air = machine
		case "wsl-tinker":
			wsl = machine
		}
	}
	if air.MDNS != "MacBook-Air-lizok.local" {
		t.Fatalf("MacBook Air needs a Bonjour hint, got %q", air.MDNS)
	}
	if wsl.MDNS != "" {
		t.Fatalf("a machine without a known Bonjour name must stay empty: %q", wsl.MDNS)
	}
}

func TestSSHArgsKeepTransportInsideOneLogicalMachine(t *testing.T) {
	route := Route{Kind: "lan", Target: "operator@192.0.2.10", Port: 2222}
	args, available := sshArgs("ubuntu", route, false)
	if !available {
		t.Fatal("LAN route unexpectedly unavailable")
	}
	got := strings.Join(args, " ")
	for _, want := range []string{"StrictHostKeyChecking=accept-new", "-p 2222", "operator@192.0.2.10", "HostKeyAlias=ubuntu"} {
		if !strings.Contains(got, want) {
			t.Fatalf("ssh args %q missing %q", got, want)
		}
	}
}

// One LAN IP can change owner (WSL used to own 192.168.1.75, Ubuntu owns it
// now). Every transport of one logical machine must therefore share a single
// host-key identity, or the second transport fails with a false MITM warning.
func TestAllRoutesOfOneMachineShareOneHostKeyAlias(t *testing.T) {
	for _, machine := range canonicalInventory() {
		for _, route := range probeRoutes(machine) {
			args, ok := sshArgs(machine.ID, route, false)
			if !ok {
				continue
			}
			joined := strings.Join(args, " ")
			if !strings.Contains(joined, "HostKeyAlias="+machine.ID) {
				t.Fatalf("%s/%s has no machine-scoped host key: %s", machine.ID, route.Kind, joined)
			}
		}
	}
}

func TestProbeFailureExplainsHostKeyConflict(t *testing.T) {
	lan := Route{Kind: "lan", Target: "timofeyka@192.168.1.75", Port: 22}
	msg := probeFailure(lan, context.Background(), []byte(
		"@@@@ WARNING: REMOTE HOST IDENTIFICATION HAS CHANGED! @@@@\nHost key verification failed.\n",
	))
	if msg != "конфликт ключа хоста" {
		t.Fatalf("host key conflict classified as %q", msg)
	}

	msg = probeFailure(lan, context.Background(), []byte("Connection refused\n"))
	if msg != "порт закрыт" {
		t.Fatalf("connection refused classified as %q", msg)
	}

	cancelled, cancel := context.WithCancel(context.Background())
	cancel()
	msg = probeFailure(lan, cancelled, nil)
	if msg != "IP не отвечает" {
		t.Fatalf("unreachable LAN address classified as %q", msg)
	}
}
