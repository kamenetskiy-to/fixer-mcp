package network

import (
	"context"
	"encoding/json"
	"fmt"
	"net/http"
	"net/url"
	"os"
	"os/exec"
	"path/filepath"
	"runtime"
	"strings"
	"time"
)

type Status struct {
	Platform string            `json:"platform"`
	Marker   string            `json:"marker,omitempty"`
	Values   map[string]string `json:"values,omitempty"`
	Egress   string            `json:"egress,omitempty"`
	State    string            `json:"state"`
	Detail   string            `json:"detail,omitempty"`
	Checked  time.Time         `json:"checked"`
}

func Inspect(ctx context.Context) Status {
	status := Status{Platform: runtime.GOOS, Values: map[string]string{}, Checked: time.Now(), State: "off"}
	path := markerPath()
	status.Marker = path
	if data, err := os.ReadFile(path); err == nil {
		status.State = "configured"
		for _, line := range strings.Split(string(data), "\n") {
			line = strings.TrimSpace(strings.TrimPrefix(line, "export "))
			parts := strings.SplitN(line, "=", 2)
			if len(parts) == 2 && parts[0] != "" {
				status.Values[parts[0]] = strings.Trim(strings.TrimSpace(parts[1]), "\"'")
			}
		}
		status.Detail = "vpn.env найден; проверяется фактический маршрут"
	} else {
		status.Detail = "vpn.env отсутствует"
	}
	status.Egress = egress(ctx, status.Values)
	if status.Egress != "" {
		status.State = "online"
	}
	return status
}

func markerPath() string {
	if value := strings.TrimSpace(os.Getenv("FLEET_VPN_ENV_FILE")); value != "" {
		return value
	}
	home, _ := os.UserHomeDir()
	root := os.Getenv("XDG_CONFIG_HOME")
	if root == "" {
		root = filepath.Join(home, ".config")
	}
	return filepath.Join(root, "fleet", "vpn.env")
}

func egress(ctx context.Context, values map[string]string) string {
	transport := &http.Transport{}
	proxy := values["HTTPS_PROXY"]
	if proxy == "" {
		proxy = values["https_proxy"]
	}
	if proxy == "" {
		proxy = values["HTTP_PROXY"]
	}
	if proxy == "" {
		proxy = values["http_proxy"]
	}
	if parsed, err := url.Parse(proxy); err == nil && (parsed.Scheme == "http" || parsed.Scheme == "https") {
		transport.Proxy = http.ProxyURL(parsed)
	} else if proxy == "" {
		transport.Proxy = http.ProxyFromEnvironment
	}
	client := &http.Client{Timeout: 6 * time.Second, Transport: transport}
	request, err := http.NewRequestWithContext(ctx, http.MethodGet, "https://api.ipify.org", nil)
	if err != nil {
		return ""
	}
	response, err := client.Do(request)
	if err != nil {
		return ""
	}
	defer response.Body.Close()
	if response.StatusCode < 200 || response.StatusCode >= 300 {
		return ""
	}
	buf := make([]byte, 128)
	n, _ := response.Body.Read(buf)
	return strings.TrimSpace(string(buf[:n]))
}

func JSON(status Status) ([]byte, error) { return json.MarshalIndent(status, "", "  ") }

// VPNAction uses the native launchd driver on macOS. Linux/WSL deployments
// keep their transport-specific script as an explicit compatibility driver;
// the console never invokes a shell alias and reports that boundary honestly.
func VPNAction(ctx context.Context, action, region string) (string, error) {
	if runtime.GOOS != "darwin" {
		binary, err := exec.LookPath("vpn-up")
		if action == "down" {
			binary, err = exec.LookPath("vpn-down")
		}
		if err != nil {
			return "", fmt.Errorf("Linux/WSL network driver is not installed")
		}
		args := []string{}
		if action == "up" && region != "" {
			args = append(args, region)
		}
		cmd := exec.CommandContext(ctx, binary, args...)
		output, runErr := cmd.CombinedOutput()
		return strings.TrimSpace(string(output)), runErr
	}
	label := os.Getenv("FLEET_VPN_LABEL")
	if label == "" {
		user := os.Getenv("USER")
		label = "system/com." + user + ".singbox"
	}
	switch action {
	case "up":
		if region == "" {
			region = "ru"
		}
		node := "sos-ru-front-01"
		if region == "us" {
			node = "sos-us-central-01"
		}
		kick := exec.CommandContext(ctx, "sudo", "-n", "/bin/launchctl", "kickstart", label)
		_ = kick.Run()
		if err := waitHTTP(ctx, "http://127.0.0.1:9090/version"); err != nil {
			return "", fmt.Errorf("VPN daemon did not become ready: %w", err)
		}
		request, _ := http.NewRequestWithContext(ctx, http.MethodPut, "http://127.0.0.1:9090/proxies/proxy", strings.NewReader(`{"name":"`+node+`"}`))
		request.Header.Set("Content-Type", "application/json")
		response, err := (&http.Client{Timeout: 6 * time.Second}).Do(request)
		if err != nil {
			return "", err
		}
		_ = response.Body.Close()
		return "VPN включён: " + region + " (" + node + ")", nil
	case "down":
		cmd := exec.CommandContext(ctx, "sudo", "-n", "/bin/launchctl", "kill", "TERM", label)
		if output, err := cmd.CombinedOutput(); err != nil {
			return strings.TrimSpace(string(output)), err
		}
		return "VPN выключен", nil
	default:
		return "", fmt.Errorf("unknown VPN action %q", action)
	}
}

func waitHTTP(ctx context.Context, url string) error {
	client := &http.Client{Timeout: 2 * time.Second}
	for {
		request, _ := http.NewRequestWithContext(ctx, http.MethodGet, url, nil)
		response, err := client.Do(request)
		if err == nil {
			_ = response.Body.Close()
			if response.StatusCode >= 200 && response.StatusCode < 500 {
				return nil
			}
		}
		select {
		case <-ctx.Done():
			return ctx.Err()
		case <-time.After(500 * time.Millisecond):
		}
	}
}
