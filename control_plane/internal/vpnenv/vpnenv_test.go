package vpnenv

import (
	"os"
	"path/filepath"
	"strings"
	"testing"
)

func TestMarkerPath(t *testing.T) {
	cases := []struct {
		name string
		env  map[string]string
		want string
	}{
		{
			name: "xdg root",
			env:  map[string]string{"XDG_CONFIG_HOME": "/xdg", "HOME": "/home/op"},
			want: "/xdg/fleet/vpn.env",
		},
		{
			name: "home fallback",
			env:  map[string]string{"HOME": "/home/op"},
			want: "/home/op/.config/fleet/vpn.env",
		},
		{
			name: "explicit override wins",
			env: map[string]string{
				"FLEET_VPN_ENV_FILE": "/tmp/custom.env",
				"XDG_CONFIG_HOME":    "/xdg",
			},
			want: "/tmp/custom.env",
		},
		{
			name: "override expands home",
			env:  map[string]string{"FLEET_VPN_ENV_FILE": "~/vpn.env", "HOME": "/home/op"},
			want: "/home/op/vpn.env",
		},
	}
	for _, tc := range cases {
		t.Run(tc.name, func(t *testing.T) {
			if got := MarkerPath(tc.env); got != tc.want {
				t.Fatalf("MarkerPath() = %q, want %q", got, tc.want)
			}
		})
	}
}

func TestParseShellAssignments(t *testing.T) {
	text := strings.Join([]string{
		"# operator tunnel",
		"",
		"HTTPS_PROXY=http://127.0.0.1:1055",
		"export HTTP_PROXY=\"http://127.0.0.1:1055\"",
		"NO_PROXY='127.0.0.1,localhost'",
		"  ALL_PROXY = socks5://127.0.0.1:1055  # live egress",
		"not a valid line",
		"1BAD=x",
		"EMPTY=",
	}, "\n")

	got := Parse(text)
	want := map[string]string{
		"HTTPS_PROXY": "http://127.0.0.1:1055",
		"HTTP_PROXY":  "http://127.0.0.1:1055",
		"NO_PROXY":    "127.0.0.1,localhost",
		"ALL_PROXY":   "socks5://127.0.0.1:1055",
		"EMPTY":       "",
	}
	if len(got) != len(want) {
		t.Fatalf("Parse() = %v, want %v", got, want)
	}
	for k, v := range want {
		if got[k] != v {
			t.Fatalf("Parse()[%q] = %q, want %q", k, got[k], v)
		}
	}
	if _, ok := got["1BAD"]; ok {
		t.Fatalf("invalid env name should be skipped: %v", got)
	}
}

func TestEnvMarkerWinsOverBase(t *testing.T) {
	base := []string{"PATH=/usr/bin", "HTTPS_PROXY=http://stale", "HOME=/home/op"}
	values := map[string]string{"HTTPS_PROXY": "http://live", "NO_PROXY": "localhost"}
	got := Env(base, values)
	joined := strings.Join(got, "\n")
	if strings.Count(joined, "HTTPS_PROXY=") != 1 {
		t.Fatalf("expected a single HTTPS_PROXY, got:\n%s", joined)
	}
	if !strings.Contains(joined, "HTTPS_PROXY=http://live") {
		t.Fatalf("marker value did not win:\n%s", joined)
	}
	if !strings.Contains(joined, "PATH=/usr/bin") || !strings.Contains(joined, "HOME=/home/op") {
		t.Fatalf("base env lost entries:\n%s", joined)
	}
	if !strings.Contains(joined, "NO_PROXY=localhost") {
		t.Fatalf("marker value not injected:\n%s", joined)
	}
}

func TestEnvWithNoValuesIsIdentity(t *testing.T) {
	base := []string{"PATH=/usr/bin"}
	got := Env(base, nil)
	if len(got) != 1 || got[0] != "PATH=/usr/bin" {
		t.Fatalf("Env() = %v", got)
	}
}

func TestInspectMissingMarkerIsInactive(t *testing.T) {
	st := Inspect(map[string]string{"FLEET_VPN_ENV_FILE": filepath.Join(t.TempDir(), "missing.env")})
	if st.Active || st.Err != nil {
		t.Fatalf("expected inactive status, got %+v", st)
	}
	if !strings.Contains(st.Summary(), "absent") {
		t.Fatalf("summary = %q", st.Summary())
	}
}

func TestInspectActiveMarkerSourcesValues(t *testing.T) {
	path := filepath.Join(t.TempDir(), "vpn.env")
	if err := os.WriteFile(path, []byte("HTTPS_PROXY=http://127.0.0.1:1055\n"), 0o600); err != nil {
		t.Fatal(err)
	}
	st := Inspect(map[string]string{"FLEET_VPN_ENV_FILE": path})
	if !st.Active || st.Err != nil {
		t.Fatalf("expected active status, got %+v", st)
	}
	if st.Values["HTTPS_PROXY"] != "http://127.0.0.1:1055" {
		t.Fatalf("values = %v", st.Values)
	}
	if !strings.Contains(st.Summary(), "active") {
		t.Fatalf("summary = %q", st.Summary())
	}
}

func TestEnvMapLastWins(t *testing.T) {
	got := EnvMap([]string{"A=1", "B=2", "A=3"})
	if got["A"] != "3" || got["B"] != "2" {
		t.Fatalf("EnvMap() = %v", got)
	}
}
