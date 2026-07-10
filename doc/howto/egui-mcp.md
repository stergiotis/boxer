---
type: how-to
audience: engineer with a specific task
status: draft
---

> **Status: draft — pre-human-review.** Not verified end to end; do not cite as
> authoritative.

# How to drive imzero2 from an AI agent with egui_mcp

[`egui_mcp`](https://github.com/rerun-io/kittest_inspector) (from rerun) is an
[MCP](https://modelcontextprotocol.io) server that lets an agent (Claude, Codex,
…) read a live egui app's widget tree and drive it — click, type, scroll, drag,
press keys, screenshot, resize, and wait for the UI to settle. It talks to the
app over the [`egui_inspection`](https://crates.io/crates/egui_inspection)
protocol, which shipped upstream in egui/eframe 0.35 as an `egui::Plugin`. That
version match is why the integration on our side is a single build feature and an
env var — nothing is embedded in imzero2 and no imzero2 code runs on the
inspection path.

The two halves:

- **App side** — imzero2's `inspection` feature (which turns on eframe's) ships
  in the desktop default build, so you only set `EGUI_INSPECTION` at runtime.
  eframe's wgpu integration then auto-attaches the `egui_inspection` plugin at
  startup and binds a TCP port. The plugin reads the AccessKit tree egui already
  emits and injects input/screenshot requests *below* the FFFI2 interpreter, so
  imzero2's Go-driven render loop is untouched. With the env var unset the plugin
  is never attached — the feature is compiled in but inert.
- **Agent side** — the `egui-mcp` binary, installed and registered with your
  agent separately (below). It connects to the app's port and exposes the tools.

This is the **desktop host only**. The headless host has its own input and pixel
path ([ADR-0024](../adr/0024-imzero2-remote-access-browser-viewer.md)); egui_mcp
is for locally driving the eframe window with an agent, not for remote viewing.

## 1. Run imzero2 with inspection enabled

The quickest path is `hmi.sh`: when `EGUI_INSPECTION` is set to a truthy value it
exports the variable so the launched client inherits it and starts the demo. The
`inspection` feature is already in the desktop default build, so no special build
flag or rebuild is involved.

```sh
cd rust/imzero2
EGUI_INSPECTION=1 ./hmi.sh          # binds 127.0.0.1:5719
```

`EGUI_INSPECTION` is read by eframe itself:

| Value | Effect |
|-------|--------|
| unset / empty / `0` / `false` | inspection off (default) |
| `1` / `true` | listen on `127.0.0.1:5719` (loopback) |
| `host:port` | listen on that address (e.g. `0.0.0.0:5719` — see [Security](#security)) |

On startup you should see `egui_inspection: listening on 127.0.0.1:5719` in the
client's stderr. If instead you see *"Inspection env var set but app was compiled
without eframe/inspection feature"*, the client binary was built without the
feature — which for the desktop build only happens if you disabled defaults
(`--no-default-features`) or built a headless target; rebuild with defaults
(a plain `./build_rust.sh` or `cargo build --release` includes it).

The Go launcher passes its environment through to the client process, so any
launch path (`hmi.sh`, `app imzero2 demo --clientBinary …`) that inherits
`EGUI_INSPECTION` works — the variable does not need to be threaded through a flag.

> `EGUI_INSPECTION` is owned and parsed by eframe (Rust), not by boxer's Go
> environment registry (ADR-0009), so it is intentionally absent from the
> generated [env-vars.md](../env-vars.md). This how-to is its reference.

## 2. Install the egui-mcp server

```sh
cargo install --git https://github.com/rerun-io/kittest_inspector egui_mcp
```

This puts the `egui-mcp` binary on your `PATH`. It is a standalone tool; it is not
built as part of boxer.

## 3. Register it with your agent

**Claude Code:**

```sh
claude mcp add egui egui-mcp
```

or add it to `~/.claude.json` / `.mcp.json` by hand:

```json
{
  "mcpServers": {
    "egui": { "command": "egui-mcp" }
  }
}
```

**Codex** — in `~/.codex/config.toml`:

```toml
[mcp_servers.egui]
command = "egui-mcp"
args = []
```

## 4. Use it

With imzero2 running (step 1) and the server registered (step 3), ask the agent to
`attach` (it defaults to `127.0.0.1:5719`), then drive the app: reproduce a bug,
verify a feature, or explore. The exposed tools cover reading the tree
(`query_tree` / `get_node`), input (click / type / scroll / drag / key press),
`screenshot`, `resize`, and `wait_for`.

Because the tree comes from AccessKit, an element is findable to the agent only if
the widget that drew it reports a role and label — the same accessibility metadata
a screen reader would use. Most stock egui widgets do; a bare custom painter draws
pixels with no node behind them.

> **Screenshots need a visible window.** Reading the tree and injecting input work
> while the window is backgrounded, but capturing a frame needs the OS to render
> one, which it won't for a fully-occluded or minimized window. Bring the window
> forward to screenshot it.

## Security

The inspection port is **unauthenticated remote control**: anyone who can reach it
can drive the app and read its screen. The `inspection` feature is compiled into
the desktop default build, so the **runtime env var is the gate**, not the build:

- The port stays closed unless `EGUI_INSPECTION` is set. Unset, eframe never
  attaches the plugin, so there is no listener and no per-frame accessibility
  cost — the compiled-in code is inert.
- When set, the truthy default binds **loopback only** (`127.0.0.1`). Note that
  loopback is **host-scoped, not user-scoped**: any local user on the same
  machine can connect. Enable it only on a trusted, ideally single-user host.
- The remote-access carrier is out of reach regardless: it ships **headless**
  (`--no-default-features`), and `inspection` pulls in the eframe-only `desktop`,
  so a headless build cannot compile it in. Keep it that way — do not add
  `inspection` to a headless or otherwise network-exposed build.

Binding a non-loopback address (`0.0.0.0:5719`, a LAN IP) exposes that control to
the network with no authentication; `egui_inspection` logs a warning when you do.
Only do it on a trusted, isolated network, and prefer an SSH tunnel to reach a
remote instance over binding it wide.
