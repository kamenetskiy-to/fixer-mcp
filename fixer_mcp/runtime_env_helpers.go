package main

import (
	"bufio"
	"fmt"
	"os"
	"strings"
)

const (
	fixerDBPathEnv         = "FIXER_DB_PATH"
	fixerMcpDefaultRoleEnv = "FIXER_MCP_DEFAULT_ROLE"
	fixerMcpDefaultCwdEnv  = "FIXER_MCP_DEFAULT_CWD"
	fixerMcpLockedRoleEnv  = "FIXER_MCP_LOCKED_ROLE"
	defaultFixerDBFilename = "fixer.db"
)

var proxyEnvNames = map[string]struct{}{
	"ALL_PROXY":   {},
	"all_proxy":   {},
	"HTTP_PROXY":  {},
	"http_proxy":  {},
	"HTTPS_PROXY": {},
	"https_proxy": {},
	"NO_PROXY":    {},
	"no_proxy":    {},
}

func envSliceToMap(env []string) map[string]string {
	payload := make(map[string]string, len(env))
	for _, entry := range env {
		key, value, found := strings.Cut(entry, "=")
		if !found {
			continue
		}
		payload[key] = value
	}
	return payload
}

func clearProxyEnvSlice(baseEnv []string) []string {
	cleaned := make([]string, 0, len(baseEnv))
	for _, entry := range baseEnv {
		key, _, found := strings.Cut(entry, "=")
		if found {
			if _, isProxy := proxyEnvNames[key]; isProxy {
				continue
			}
		}
		cleaned = append(cleaned, entry)
	}
	return cleaned
}

func resolveRuntimeLaunchEnv(projectCWD string, baseEnv []string) ([]string, error) {
	_ = projectCWD
	return clearProxyEnvSlice(baseEnv), nil
}

func loadOptionalDotEnv(paths ...string) error {
	for _, rawPath := range paths {
		candidate := strings.TrimSpace(rawPath)
		if candidate == "" {
			continue
		}
		file, err := os.Open(candidate)
		if err != nil {
			if os.IsNotExist(err) {
				continue
			}
			return fmt.Errorf("open %s: %w", candidate, err)
		}

		scanner := bufio.NewScanner(file)
		lineNumber := 0
		for scanner.Scan() {
			lineNumber++
			line := strings.TrimSpace(scanner.Text())
			if line == "" || strings.HasPrefix(line, "#") {
				continue
			}
			if strings.HasPrefix(line, "export ") {
				line = strings.TrimSpace(strings.TrimPrefix(line, "export "))
			}

			key, value, found := strings.Cut(line, "=")
			if !found {
				continue
			}
			key = strings.TrimSpace(key)
			if key == "" {
				continue
			}
			if _, exists := os.LookupEnv(key); exists {
				continue
			}

			value = strings.TrimSpace(value)
			if len(value) >= 2 {
				if (strings.HasPrefix(value, "\"") && strings.HasSuffix(value, "\"")) ||
					(strings.HasPrefix(value, "'") && strings.HasSuffix(value, "'")) {
					value = value[1 : len(value)-1]
				}
			}
			if err := os.Setenv(key, value); err != nil {
				_ = file.Close()
				return fmt.Errorf("set env %s from %s:%d: %w", key, candidate, lineNumber, err)
			}
		}
		if err := scanner.Err(); err != nil {
			_ = file.Close()
			return fmt.Errorf("scan %s: %w", candidate, err)
		}
		_ = file.Close()
	}
	return nil
}
