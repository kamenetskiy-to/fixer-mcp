package machines

import (
	"bufio"
	"context"
	"encoding/json"
	"fmt"
	"os"
	"os/exec"
	"path/filepath"
	"strings"
	"sync"
	"time"
)

type Machine struct {
	ID        string `json:"id"`
	Title     string `json:"title"`
	Target    string `json:"target,omitempty"`
	Kind      string `json:"kind"`
	Path      string `json:"path,omitempty"`
	Available bool   `json:"available"`
	Detail    string `json:"detail,omitempty"`
}

type Snapshot struct {
	Machines []Machine `json:"machines"`
	Checked  time.Time `json:"checked"`
}

func Discover() []Machine {
	machines := []Machine{{ID: "local", Title: "Эта машина", Kind: "local", Available: true, Detail: "локальный терминал"}}
	seen := map[string]bool{"local": true}
	for _, machine := range configured() {
		if !seen[machine.ID] {
			machines = append(machines, machine)
			seen[machine.ID] = true
		}
	}
	for _, alias := range sshAliases() {
		if seen[alias] || strings.ContainsAny(alias, "*?!") {
			continue
		}
		machines = append(machines, Machine{ID: alias, Title: alias, Target: alias, Kind: "ssh", Detail: "проверка не запускалась"})
		seen[alias] = true
	}
	return machines
}

func Probe(ctx context.Context, machines []Machine) Snapshot {
	result := Snapshot{Checked: time.Now(), Machines: append([]Machine(nil), machines...)}
	sem := make(chan struct{}, 6)
	var wait sync.WaitGroup
	for i := range result.Machines {
		machine := &result.Machines[i]
		if machine.Kind == "local" {
			machine.Available = true
			machine.Detail = "локальный терминал"
			continue
		}
		if machine.Target == "" {
			machine.Detail = "не настроен"
			continue
		}
		wait.Add(1)
		go func(machine *Machine) {
			defer wait.Done()
			select {
			case sem <- struct{}{}:
			case <-ctx.Done():
				machine.Detail = "проверка отменена"
				return
			}
			defer func() { <-sem }()
			probe := exec.CommandContext(ctx, "ssh", "-o", "BatchMode=yes", "-o", "ConnectTimeout=3", machine.Target, "true")
			if err := probe.Run(); err != nil {
				machine.Available = false
				if ctx.Err() != nil {
					machine.Detail = "проверка отменена"
				} else {
					machine.Detail = "недоступен"
				}
			} else {
				machine.Available = true
				machine.Detail = "SSH доступен"
			}
		}(machine)
	}
	wait.Wait()
	return result
}

func AttachCommand(machine Machine) (string, []string, error) {
	if machine.Kind == "local" {
		if shell, err := exec.LookPath("zsh"); err == nil {
			return shell, []string{"-l"}, nil
		}
		return "bash", []string{"-l"}, nil
	}
	if machine.Target == "" {
		return "", nil, fmt.Errorf("машина %s не имеет SSH target", machine.ID)
	}
	return "ssh", []string{"-tt", machine.Target}, nil
}

func configured() []Machine {
	home, err := os.UserHomeDir()
	if err != nil {
		return nil
	}
	configHome := os.Getenv("XDG_CONFIG_HOME")
	if configHome == "" {
		configHome = filepath.Join(home, ".config")
	}
	path := filepath.Join(configHome, "fixer", "machines.json")
	data, err := os.ReadFile(path)
	if err != nil {
		return nil
	}
	var machines []Machine
	if err := json.Unmarshal(data, &machines); err != nil {
		return nil
	}
	return machines
}

func sshAliases() []string {
	home, err := os.UserHomeDir()
	if err != nil {
		return nil
	}
	files := []string{filepath.Join(home, ".ssh", "config"), filepath.Join(home, ".ssh", "fixer-fleet-ssh-mesh.conf")}
	seen := map[string]bool{}
	var result []string
	for _, path := range files {
		file, err := os.Open(path)
		if err != nil {
			continue
		}
		scanner := bufio.NewScanner(file)
		for scanner.Scan() {
			fields := strings.Fields(scanner.Text())
			if len(fields) < 2 || strings.ToLower(fields[0]) != "host" {
				continue
			}
			for _, alias := range fields[1:] {
				if !seen[alias] {
					seen[alias] = true
					result = append(result, alias)
				}
			}
		}
		_ = file.Close()
	}
	return result
}
