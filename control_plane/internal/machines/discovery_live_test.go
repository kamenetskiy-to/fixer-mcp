package machines

import (
	"context"
	"os"
	"strings"
	"testing"
	"time"
)

// TestLiveLANDiscovery is opt-in: it needs a real Wi-Fi network where the
// MacBook Air is reachable under a moved DHCP address.
//
//	FIXER_TEST_LIVE_LAN=1 go test ./internal/machines/ -run TestLiveLANDiscovery -v
//
// It exists because discovery mixes Bonjour, a subnet sweep, the system nc(1)
// and an ssh login, and only a live run exercises all four together.
func TestLiveLANDiscovery(t *testing.T) {
	if os.Getenv("FIXER_TEST_LIVE_LAN") == "" {
		t.Skip("set FIXER_TEST_LIVE_LAN=1 to run against the real Wi-Fi network")
	}
	machine := Machine{
		ID:    "macbook-air-lizok",
		Title: "MacBook Air",
		MDNS:  mdnsNameFor("macbook-air-lizok"),
		Routes: []Route{
			{Kind: "lan", Target: "timosh@192.168.1.65", Port: defaultSSHPort},
		},
	}
	ctx, cancel := context.WithTimeout(context.Background(), 45*time.Second)
	defer cancel()

	ip, err := resolveMDNS(machine.MDNS)
	if err != nil || ip == "" {
		t.Fatalf("mDNS hint %q did not resolve: ip=%q err=%v", machine.MDNS, ip, err)
	}
	t.Logf("mDNS resolved %s -> %s", machine.MDNS, ip)

	open := openLANAddresses(ctx, subnetCandidates("timosh@192.168.1.65"), defaultSSHPort)
	t.Logf("sweep found %d open hosts: %v", len(open), open)

	// The Bonjour candidate must be reachable through the same probe the sweep
	// uses, otherwise the whole discovery silently reports "not found".
	if err := authProbeLAN(ctx, Route{Kind: "lan", Target: "timosh@192.168.1.65", Port: defaultSSHPort}, ip); err != nil {
		t.Fatalf("auth probe against the Bonjour address %s failed: %v", ip, err)
	}

	result, err := discoverLAN(ctx, machine)
	t.Logf("discoverLAN -> ip=%q authed=%v err=%v", result.ip, result.authed, err)
	if err != nil {
		t.Fatalf("discovery failed: %v", err)
	}
	if !result.authed || result.ip == "" {
		t.Fatalf("discovery returned an unusable result: %#v", result)
	}

	// This is the exact path behind "не удалось открыть MacBook Air … Host is
	// down": the configured address is stale, so opening the machine must
	// rediscover it instead of failing.
	connection, err := ResolveVia(ctx, machine, "lan")
	if err != nil {
		t.Fatalf("ResolveVia failed to use the discovered address: %v", err)
	}
	if connection.Route.Target == "timosh@192.168.1.65" {
		t.Fatalf("ResolveVia still used the stale address: %q", connection.Route.Target)
	}
	if !strings.Contains(connection.Route.Target, result.ip) {
		t.Fatalf("ResolveVia target = %q, want the discovered %s", connection.Route.Target, result.ip)
	}
	t.Logf("ResolveVia -> %s", connection.Route.Target)
}
