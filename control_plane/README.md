# `fixer` — work-first operator console

`fixer` is the managed entry point for project work. `fixerctl` and `fx` are
compatibility names for the same binary, not separate products.

## Three spaces

| Space | What it opens |
| --- | --- |
| **Работа** | the current project, **Руки** (project execution) and **Фиксер** (governance/review), then detachable local terminal contexts and new work |
| **Ресурсы** | the exact live `cml` report first, followed by available clients and explicit account actions |
| **Машины** | one bounded canonical logical-machine inventory; choosing a host automatically resolves Tailscale, SOCKS, then a safe LAN fallback |

Network state and VPN controls live inside **Машины** because they are transport
controls, not a fourth workspace. Provisioning diagnostics are available via
`fixer doctor`; they are not an interactive product space.

## Direct work entry

The default selected action in **Работа** is **Руки**. It opens the permanent
project execution client on the Pi lane by default; a different registered lane
must be chosen deliberately. **Фиксер** opens the governed project workroom.
They can also be opened directly:

```sh
fixer hands
fixer workroom
```

`fixer open PATH` selects a project deliberately. `fixer` otherwise reopens the
last project context.

## Resources and limits

`cml` is the operator-facing quota source. The console runs `cml` first (with
`check-my-limits` only as a compatibility fallback) and renders its report
unchanged in **Ресурсы**. `r` refreshes that report.

```sh
fixer quota       # prints the same cml report
fixer quota --json
```

Account bind/switch actions are explicitly marked **global for new local
launches** and always require confirmation. They are shown separately from
provider availability so an account mutation cannot look like a provider or
model selection.

## Logical machines

The console never enumerates `~/.ssh/config`. Its inventory is the canonical
physical machine list plus `Эта машина`; `*-tailscale`, `*-local`, personal aliases,
and stale SSH entries remain internal transport details. For a chosen machine
Fixer tries the route candidates automatically. The known unsafe old WSL LAN
address is explicitly disabled rather than risking a connection to Ubuntu.

`~/.config/fixer/machines.json` may override the title, remote project path, or
transport values of an existing canonical ID. It cannot add arbitrary SSH
aliases to the visible inventory.

## Keys

* `1`–`3` — Работа / Ресурсы / Машины
* `h` / `f` — Руки / Фиксер for the current project
* `n` — new local launch (it is explicitly not an MCP project session)
* `Enter` — open the selected action, context, or logical machine
* `r` — refresh `cml` or machine availability
* `u` / `d` — VPN up/down in **Машины**
* `s` — stop a selected detachable local context; in **Ресурсы**, request account switch
* `b` — bind current credentials to the selected account profile
* `/` — search; `?` — help; `q` / `Ctrl-C` — quit

## Build

```sh
make fixer
./control_plane/fixerctl
make test-control-plane
```

The release assembler packages one native console binary as `fixer-console`,
with compatibility copies named `fixerctl`.
