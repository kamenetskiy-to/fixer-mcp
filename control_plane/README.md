# `fixer` — unified operator console

`fixer` is the managed, work-first Super-TUI for the operator fleet. It owns one
keyboard context for projects, persistent agent sessions, provider resources,
machines, network state and Fleet checks. `fixerctl` and `fx` remain temporary
compatibility names for the same executable; they are not separate products.

## Build and run

From the repository root:

```sh
make fixer                 # builds control_plane/fixerctl and fixer-console
./control_plane/fixerctl   # opens the console
./control_plane/fixerctl quota --json
```

The managed installer places `fixer-console` in the release payload and
installs `~/.local/bin/fixer` as the sole operator entry point. Maintenance
commands still use the installer service:

```sh
fixer update
fixer doctor
```

`fixerctl` is accepted while old scripts are being retired. The release shim
selects the native console for ordinary work, so no shell alias, current cwd,
or legacy Python TUI is required.

## Workspaces

| Workspace | Purpose |
| --- | --- |
| **Работа** | current project, persistent sessions, continue/detach/stop, new launch card |
| **Ресурсы** | installed clients, accounts, quota snapshots and explicit credential actions |
| **Машины** | saved local/WSL/Air targets, availability probes and terminal attach |
| **Сеть** | actual egress, VPN marker/proxy state and guarded up/down actions |
| **Fleet** | managed environment check and its bounded output |

The launch card keeps project, work type, client/provider, account, model,
reasoning, prompt, machine and resume data together. Sessions run in tmux;
`Enter` attaches without stopping the process and `Ctrl-b d` detaches without
losing it. Provider switches are represented as a new launch context rather
than a false universal resume. `fixer` without a path resumes the last project;
`fixer open PATH` deliberately selects another one.

Remote machine targets are read from `~/.config/fixer/machines.json` (or
`$XDG_CONFIG_HOME/fixer/machines.json`). Each entry may provide `id`, `title`,
`target`, `kind: "ssh"` and a remote `path`; SSH transport and reconnect flags
are chosen by the console, not by a shell alias.

## Keys

* `1`–`5` — Работа / Ресурсы / Машины / Сеть / Fleet
* `n` — new work from the saved project profile
* `Enter` — continue a session or open the selected action
* `s` — stop the selected work session; on Ресурсы, request account switch
* `b` — bind current credentials to the selected account profile
* `r` — refresh the active workspace asynchronously
* `/` — action/workspace search
* `?` — help; `q` / `Ctrl-C` — quit

Credential switch and bind operations always show a confirmation screen and
write only the selected client's auth file. Quota errors, missing clients,
network failures and exhausted windows remain separate statuses.

## Non-interactive surfaces

```sh
fixer -version
fixer -print
fixer -print -json
fixer quota --json
fixer -run cml
```

`-print` and `-run` are compatibility diagnostics. They do not define the
interactive product and may be removed after fleet migration.

## State and packaging

UI drafts and session/tmux metadata are stored locally under the Fixer state
directory. Project and orchestration truth remains in Fixer MCP; this console
does not create a second project database. The release assembler cross-builds
`bin/fixer-console`, copies the identical binary to `bin/fixerctl` and
`control_plane/fixerctl`, and injects the release version at link time.

```sh
make test-control-plane
python3 -m pytest packaging/test_assembler_payload.py tests/installer/test_shim.py
```
