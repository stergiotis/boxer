---
type: adr
status: accepted
date: 2026-05-03
reviewed-by: "@spx"
reviewed-date: 2026-05-03
---

> **Status: accepted 2026-05-03 by @spx.** Implementation tracked across M1–M5 in this document.

# ADR-0020: `imztop` — btop-style Resource Monitor on ImZero2 + sysmetrics

## Context

[ADR-0019](./0019-observability-sysmetrics-linux-collector.md) just landed a pure-Go Linux metrics collector under [`../../public/observability/sysmetrics/`](../../public/observability/sysmetrics) — the data layer is a finished product. The natural follow-on is a graphical resource monitor that consumes it: replicate btop's user-facing experience as a desktop GUI without dragging in a TUI, ANSI escape sequences, or btop's globals. The repo already has the rendering host: [`../../public/thestack/imzero2/`](../../public/thestack/imzero2) plus the egui2 widget surface ([`../../public/thestack/imzero2/egui2/`](../../public/thestack/imzero2/egui2)). Existing demos under [`../../public/thestack/imzero2/egui2/demo/`](../../public/thestack/imzero2/egui2/demo) — `regex_explorer`, `hn_explorer`, `pijul`, `play` — establish the precedent for non-trivial multi-panel apps in this stack.

Forces the design must respect:

- **ImZero2 continuous rendering.** The Rust side's `logic()` calls `ctx.request_repaint()` every pass; the frame loop ticks at compositor cadence (typically 60 Hz). A frame must never block on an OS read. The collector loop and the render loop are therefore **decoupled** by construction.
- **FFFI databindings reset every Sync.** `r9_*` bindings re-register every frame and carry a one-frame lag (see CLAUDE.md "FFFI databindings reset every Sync"). Slices passed to plot widgets must come from stable memory and be re-sliced, not re-allocated.
- **~100k visible-points-per-pane budget.** Inherited from the Grafana-replacement scope target. At 1 Hz with a 10-minute history window, a 12-panel app stays well under it.
- **Host shell wraps top + bottom panels.** [`../../public/thestack/imzero2/egui2/demo/carousel/imzero2_demo_resolve.go:24-66`](../../public/thestack/imzero2/egui2/demo/carousel/imzero2_demo_resolve.go) (`decorateRenderer`) provides `PanelTop` (menu bar with Quit / Layout / theme toggles) and `PanelBottom` (metrics overlay). Subcommand bodies fit **between** those — they must not double up.
- **Boxer conventions** (CLAUDE.md, [CODINGSTANDARDS.md](../../CODINGSTANDARDS.md)) — `inst` receivers, `*I` interface suffix, `*E` enum suffix, `eh.Errorf` errors, sized integers, zero-value-usable structs.
- **No process write-side.** ADR-0019 explicitly excludes `kill(pid)` / `set_priority(pid, nice)`. imztop is read-only against `sysmetrics`.

[btop](https://github.com/aristocratos/btop) — Apache 2.0 ([LICENSE](https://github.com/aristocratos/btop/blob/main/LICENSE)) — is the feature-parity reference. We mirror btop's panel set (CPU / MEM / DISK / NET / PROC / GPU / SENSORS / BATTERY) but not its implementation: btop's UI lives in `src/btop_draw.cpp` (TUI character cells), `src/btop_input.cpp` (terminal input), `src/btop_menu.cpp` (modal menus). None of that translates to egui. The mapping is **feature → sysmetrics field → egui2 widget**, not source-to-source.

## Design space (QOC)

**Question.** How do we lay out CPU / MEM / DISK / NET / PROC / GPU / SENSORS / BATTERY panels inside the host's central area?

**Options.**

- **O1 — Nested egui panels.** `PanelLeftInside` + `PanelCentralInside` recursion, mirroring [`regex_explorer.go:127-219`](../../public/thestack/imzero2/egui2/demo/apps/regex_explorer/regex_explorer.go).
- **O2 — `DockArea` (egui_dock 0.19).** User-arrangeable tiles. Already in production use in `regex_explorer.go:219`. The bounded-allocation pattern (DockAreaRaw apply allocates a child ui via `allocate_ui_with_layout`) is established in the existing wrapper.
- **O3 — Custom tile manager via `AllocateUiAtRect`.** Compute absolute rectangles in Go, position each panel directly.

**Criteria.**

- **C1 — M1 implementation cost.** Lines of layout code, debug surface.
- **C2 — User mobility.** Can the user resize / rearrange / hide panels at runtime?
- **C3 — Persistence cost.** Cross-frame state to keep coherent.
- **C4 — Plot interaction risk.** Multi-plot Ctrl+Wheel zoom interaction (warned about in [`egui2_hl_graphs_demo.go:58-61`](../../public/thestack/imzero2/egui2/demo/apps/widgets/egui2_hl_graphs_demo.go)) compounds when each plot lives in its own scope.

**Assessment.** `++` strong positive, `+` positive, `−` negative, `−−` strong negative.

|    | O1 (nested panels) | O2 (DockArea) | O3 (AllocateUiAtRect) |
|----|--------------------|---------------|------------------------|
| C1 — M1 implementation cost     | ++ | −  | −− |
| C2 — User mobility              | −  | ++ | +  |
| C3 — Persistence cost           | ++ | −  | +  |
| C4 — Plot interaction risk      | +  | −  | +  |

Notes per cell:

- **O3 / C1 (−−).** `AllocateUiAtRect` positions child Ui at parent's absolute coordinates and silently breaks enclosing `Vertical` / `Horizontal` flow (see CLAUDE.md "imzero2 `AllocateUiAtRect` is absolute"). Building a tile manager around it means re-implementing what egui's panels already provide.
- **O2 / C3 (−).** DockArea state lives in egui memory keyed off `WidgetIdStack`. Tab close + reopen + reorder all need persistence wiring; for a long-running monitor app this is non-trivial.
- **O2 / C4 (−).** Each dock leaf gets its own clip rect; the global Ctrl+Wheel zoom handler that already trips when *two* plots share a screen multiplies with leaf count.

## Decision

We adopt **O1 — nested egui panels** for M1–M5 under a new application package `apps/imztop/` (created in M1), wired into the existing `imzero2` subcommand registry as the next free `appCode` (currently 7) in [`imzero2_demo_resolve.go`](../../public/thestack/imzero2/egui2/demo/carousel/imzero2_demo_resolve.go). The application name is **`imztop`** (not `btop` — disambiguates from upstream binaries on `$PATH` and signals "imzero2 + top-style monitor"). Sampling runs on a dedicated goroutine writing into per-collector ring buffers behind an `atomic.Pointer[BundleSnapshot]`; the frame loop reads via pointer-load-acquire. Plotting uses **`egui_hl_plot`** (`PlotLine` / `PlotBars` / `ProgressBar`). The process panel uses the virtualized **`EndETable`** ([`egui2_hl_etable_demo.go:39-77`](../../public/thestack/imzero2/egui2/demo/apps/widgets/egui2_hl_etable_demo.go)).

DockArea is **deferred** to a post-M5 follow-on; once M1–M5 stabilises and persistence semantics are clearer, dock support can be a `--layout=dock` opt-in.

## Subsidiary design decisions

- **SD1 — Application name `imztop`, package `apps/imztop/`.** Disambiguates from upstream `btop` on `$PATH`. Top-level under `thestack/` rather than nested under `thestack/imzero2/egui2/demo/` because imztop is a real application, not a widget demo. Sibling to `imzero2/`, `pijul`-equivalent in scope.

- **SD2 — Wired as `imzero2` subcommand, not a top-level binary.** Reuses the existing `decorateRenderer` host (PanelTop menu + PanelBottom metrics overlay), the existing build pipeline, and the existing `appCode` switch in [`imzero2_demo_resolve.go`](../../public/thestack/imzero2/egui2/demo/carousel/imzero2_demo_resolve.go). Next free `appCode` is **7**. Tour variant follows the same pattern as `regex_explorer` (`RenderLoopHandlerDemo` + `RenderLoopHandlerTour`).

- **SD3 — Sampler goroutine + ring buffer + `atomic.Pointer[BundleSnapshot]`.** One long-lived goroutine owns `*sysmetrics.Bundle` (constructed once, [`bundle.go:100`](../../public/observability/sysmetrics/bundle.go)). On each tick it calls `Bundle.Sample(ctx)` ([`bundle.go:119`](../../public/observability/sysmetrics/bundle.go)), appends the result to per-collector `Ring[float64]` instances, then `atomic.Store`s a pointer to a struct holding the new snapshot + ring tail indices. The frame loop calls `atomic.Load` once per frame and re-slices the ring's stable backing array — no allocation per frame, no mutex on the hot read path. Rationale: sysmetrics' `proc.Sample` is the slow domain (walks `/proc/[pid]/`); blocking the frame loop on it would tear vsync.

- **SD4 — Update interval default 1000 ms (configurable).** btop ships 2000 ms; sysmetrics' end-to-end Bundle latency is "well under 5 ms" per `doc/observability/sysmetrics/REFERENCE.md`, so 1000 ms is comfortable. Exposed via keybinding (`+` / `-`) at M5.

- **SD5 — Ring buffer fixed at 600 samples (10-minute window at 1 Hz).** Per-series, two `[]float64` (xs unix-seconds, ys value), pre-allocated, head + length tracked atomically. Re-sliced via `xs[:n]` / `ys[:n]` per frame. The ring's backing array does not move for the lifetime of the goroutine, satisfying the FFFI stable-memory requirement.

- **SD6 — Plot library: `egui_hl_plot`.** `PlotLine` for time-series, `PlotBars` for per-core current-value columns, `ProgressBar` for MEM / SWAP snapshot bars. `egui_hl_graphs` is a force-directed node-edge graph (verified at [`egui2_hl_graphs_demo.go`](../../public/thestack/imzero2/egui2/demo/apps/widgets/egui2_hl_graphs_demo.go)) — wrong primitive for time-series.

- **SD7 — Process panel: virtualized `EndETable`.** Sort + filter computed in the **sampler** (once per tick), not the renderer (every frame). Variant patterns at [`egui2_hl_etable_demo.go:53,95,146,190,246,309,368`](../../public/thestack/imzero2/egui2/demo/apps/widgets/egui2_hl_etable_demo.go) — large/sparse/varheight all sized for >1k rows. Sort-by header click is the next-tick re-sort, not an in-frame re-sort.

- **SD8 — Theming: egui-native dark palette only.** No port of `contrib/btop/themes/*.theme` files. A theme parser plus a colour-grading layer is an entire side project; egui's default dark palette plus a small named-colour table for "good / warn / hot / dead" thresholds covers M1–M5. Revisit post-M5 if users ask.

- **SD9 — GPU build tag: `gpu_rocm` only.** Added to [`../../tags`](../../tags). `gpu_intel` and `gpu_nvml` are explicitly **not** baked into the default `imztop` build. Rationale: target hardware for the first user is AMD; Intel and NVIDIA support exist in sysmetrics ([ADR-0019 M5/M6](./0019-observability-sysmetrics-linux-collector.md)) and can be enabled by editing `./tags` and rebuilding. The GPU panel hides itself when `gpu.Sampler` returns no devices (zero-value graceful behaviour from sysmetrics SD-equivalent).

- **SD10 — Container panel: host-classification badge only.** `container.Detect(ctx)` returns the engine type (docker / podman / lxc / kubernetes / unknown / none) per [ADR-0019 SD9](./0019-observability-sysmetrics-linux-collector.md). imztop renders this as a small badge in the top bar ("running in: docker"). Cgroup-aware **process trees** (showing nested cgroup hierarchies in the process list) are out of scope for M1–M5.

- **SD11 — No process write-side (kill / nice).** ADR-0019 SD-equivalent already excluded write-side from sysmetrics. imztop does not ship a kill button, signal-sender, or priority editor. Read-only against `sysmetrics`. Revisit only if a user explicitly asks; would need a separate `os.Process.Signal` shim plus a confirmation modal, neither of which belongs in M1–M5.

- **SD12 — Time axis labelling deferred to M5.** The egui2 `Plot` fluent ([`components/methods.out.go`](../../public/thestack/imzero2/egui2/bindings/methods.out.go), `Plot*` block) does not expose a tick-formatter callback. M1–M4 use Unix-second numeric X labels (acceptable for short windows). M5 adds a thin Go-side helper that pre-computes labels via [`public/math/numerical/timeticks`](../../public/math/numerical/timeticks/) and renders them with `PlotText` at the appropriate X positions.

- **SD13 — Snapshot ownership and lifecycle.** Sampler owns `*sysmetrics.Bundle`; sampler goroutine constructs once, defers `Close()` ([`bundle.go:272`](../../public/observability/sysmetrics/bundle.go)) on shutdown. The application's `RenderLoopHandlerDemo` does not touch the Bundle directly. Shutdown signal flows through a `context.Context` cancelled on egui close.

- **SD14 — Pause / freeze.** A keybinding (`space`, M5) toggles the sampler goroutine into "freeze the most recent snapshot" mode — sampler keeps running but does not publish. No timeline scrubbing; that is post-M5 if at all. Rationale: btop has no pause; in a GUI the gesture is natural and cheap.

## Public API sketch

Illustrative only — fields and method names will be pinned during M1.

```go
// apps/imztop/imztop.go
package imztop

// RenderLoopHandlerDemo is the per-frame entry registered with imzero2_demo_resolve.go (case 7).
func RenderLoopHandlerDemo() (err error) { /* read snapshot via atomic, render panels */ }

// RenderLoopHandlerTour is the deterministic-frame-count entry for IMZERO2_SCREENSHOT_DIR captures.
func RenderLoopHandlerTour() (err error) { /* fixed N frames, prepared fixture data */ }
```

```go
// apps/imztop/imztop_sampler.go
package imztop

type SamplerOptions struct {
    UpdateInterval time.Duration   // default 1*time.Second
    HistoryWindow  time.Duration   // default 10*time.Minute
    Bundle         *sysmetrics.Bundle
}

type Sampler struct { /* unexported; constructed via NewSampler */ }

func NewSampler(opts SamplerOptions) (inst *Sampler, err error) { /* ... */ }

// Start spawns the goroutine and returns immediately.
func (inst *Sampler) Start(ctx context.Context) (err error) { /* ... */ }

// Latest returns a borrowed pointer to the most recent published snapshot.
// Slices in the returned struct are re-slices of stable ring backing memory.
func (inst *Sampler) Latest() (snap *PublishedSnapshot) { /* atomic.Load */ }

// Close stops the sampler goroutine and calls Bundle.Close.
func (inst *Sampler) Close() (err error) { /* ... */ }

var _ SamplerI = (*Sampler)(nil)
```

```go
// apps/imztop/imztop_ringbuf.go
package imztop

// Ring is a fixed-capacity ring buffer with stable backing memory.
// Push is O(1); Snapshot returns re-slices of the underlying array.
type Ring[T any] struct { /* head, length, cap, data []T */ }

func NewRing[T any](cap int32) (inst *Ring[T]) { /* ... */ }
func (inst *Ring[T]) Push(v T)                  { /* ... */ }
func (inst *Ring[T]) Snapshot() (xs, ys []T)    { /* re-slice; no copy */ }
```

## Implementation plan

Five milestones, each independently shippable. A green `scripts/ci/lint.sh` and `scripts/ci/gotest.sh`, a CHANGELOG entry, and visual verification gate each transition.

### M1 — Skeleton + CPU + memory

`imztop.go` (`RenderLoopHandlerDemo`), `imztop_sampler.go` (goroutine + lifecycle), `imztop_ringbuf.go` (`Ring[float64]`), `imztop_panel_cpu.go` (`PlotLine` history + `PlotBars` per-core), `imztop_panel_mem.go` (`ProgressBar` snapshot + `PlotLine` history). Wire into `imzero2_demo_resolve.go` as `case 7`. Append `gpu_rocm` to `./tags`.

**Done when:** `case 7` opens a window with a CPU panel showing live total + per-core history and a MEM panel showing live used / available / swap; both update at 1 Hz; `lint.sh` green; running 30 s does not grow Go heap (verified via `runtime.ReadMemStats` in a test).

### M2 — Disk + network + GPU + battery + sensors

`imztop_panel_disk.go` (one row per mount; dual `PlotLine` for read/write rates), `imztop_panel_net.go` (per-interface dropdown + dual `PlotLine`), `imztop_panel_gpu.go` (`gpu.Sampler` adapter, hidden when no devices), `imztop_panel_battery.go` (`ProgressBar` + state label, hidden when no batteries), `imztop_panel_sensors.go` (label grid for temperatures). `BundleOptions` wires all collectors per REFERENCE.md "Combined GPU wiring".

**Done when:** every panel renders live data on a laptop fixture (battery + WiFi + 1 SSD + AMD iGPU); on a server fixture (no battery, multiple NVMe) the BATTERY panel cleanly shows "no batteries"; GPU panel hides on hardware with no AMD device.

### M3 — Process panel

`imztop_panel_proc.go`. `EndETable` with columns PID / User / CPU% / RSS / VSZ / State / Cmd. Sort key + direction + filter string in package-level state; sampler re-sorts and re-filters once per tick before publishing. Filter input via `TextEdit` above the table.

**Done when:** rendering 1000+ synthetic PIDs (stress workload or test fixture) holds 60 fps; sorting by CPU% promotes the busy process within 2 ticks; filtering "ssh" leaves only matching rows and the row count badge updates.

### M4 — Polish: top bar, container badge, theme palette

`imztop_panel_topbar.go` adds a content row inside the host's existing menu bar (or a dedicated row below it) showing: app name, container engine badge from `container.Detect`, sampler status (running / paused), update interval. `imztop_theme.go` defines a small named-colour table (`good`, `warn`, `hot`, `dead`, `accent`) keyed against egui's default dark palette — no theme file parser, no theme switcher.

**Done when:** the top row renders the container badge when invoked under docker / podman / lxc; threshold-coloured cells (CPU% > 80 = `warn`, > 95 = `hot`) work; on a bare metal host the badge cleanly shows "host".

### M5 — Keybindings, time-axis ticks, screenshot tour

`imztop_keybindings.go` (`q` quit, `space` pause, `+`/`-` interval, `1-7` panel toggle). `imztop_timeticks.go` — Go-side helper computing labels via [`public/math/numerical/timeticks`](../../public/math/numerical/timeticks/) and rendering them with `PlotText` at the appropriate X positions. `imztop_tour.go` — deterministic 6-frame tour for `IMZERO2_SCREENSHOT_DIR` capture, mirroring [`regex_explorer_tour.go:74-96`](../../public/thestack/imzero2/egui2/demo/apps/regex_explorer/regex_explorer_tour.go).

**Done when:** keybindings respond on the next frame; X axis tick labels read as `15:42:00` / `15:42:30` (not `1746635320`); `IMZERO2_SCREENSHOT_DIR=/tmp/imztop-tour ./rust/imzero2/hmi.sh imzero2 7 --tour` produces 6 deterministic PNGs that survive a re-run.

### Out of scope for this ADR (named follow-ons)

- **`DockArea` user-arrangeable layout.** Deferred; revisit post-M5 as `--layout=dock` opt-in.
- **Theme file port.** `contrib/btop/themes/*.theme` parsing — separate ADR or skip.
- **NVIDIA / Intel GPU support.** Build-tag-gated in sysmetrics ([ADR-0019 M5/M6](./0019-observability-sysmetrics-linux-collector.md)); enabled by editing `./tags`.
- **Process write-side (kill / nice / signal).** Excluded by ADR-0019; see SD11.
- **Cgroup-aware process tree** in the process panel. Container panel shows host classification only (SD10).
- **Remote sysmetrics over network.** Pure-local for M1–M5; remote needs a transport package and is a separate ADR.
- **Sample timeline scrubbing.** Pause is M5; scrubbing back through the 10-minute history is a follow-on if users ask.
- **Configuration persistence.** Update interval, panel visibility, sort columns — runtime-only for M1–M5; persist later if M5 reveals friction.

## Alternatives

- **Top-level binary `cmd/imztop` instead of an `imzero2` subcommand.** Rejected — the host shell (`decorateRenderer`) already provides menu bar, theme toggles, metrics overlay, screenshot capture, and the entire egui2 wiring. Standing up a parallel `cmd/imztop` would duplicate all of that without benefit. Discoverability via `./rust/imzero2/hmi.sh imzero2 7` is already the established pattern for non-trivial demos (regex_explorer = case 6).

- **Synchronous per-frame `Bundle.Sample` call (no goroutine).** Rejected — sysmetrics' `proc.Sample` walks `/proc/[pid]/` for every visible PID; on a 500-process box this is comfortably tens of milliseconds. At 60 fps the frame budget is 16.6 ms. Blocking the frame loop tears vsync. The cost of the goroutine is one channel-less `atomic.Pointer` swap per tick, which is free.

- **`egui_hl_graphs` for time-series.** Rejected — it is a force-directed node-edge graph layout. Confirmed by inspecting [`egui2_hl_graphs_demo.go`](../../public/thestack/imzero2/egui2/demo/apps/widgets/egui2_hl_graphs_demo.go). Wrong primitive.

- **Plain `Vertical` of process rows in `ScrollArea` instead of `EndETable`.** Rejected — every row paints every frame. With 300+ rows at 60 fps this is wasteful; `EndETable` issues `BeginCells` / `EndCells` per cell and the Rust side replays only the visible window. Demo evidence at [`egui2_hl_etable_demo.go:190` (10k dense)](../../public/thestack/imzero2/egui2/demo/apps/widgets/egui2_hl_etable_demo.go) shows this is what the table widget was built for.

- **Port btop's theme file format.** Rejected for M1–M5 — `contrib/btop/themes/*.theme` is a parser plus a colour-mapping layer; egui's dark palette plus a 5-named-colour threshold table covers the visual taxonomy without the parsing surface. Revisit post-M5 if users ask.

- **In-render sort + filter for the process table** (instead of in-sampler). Rejected — sort + filter at 60 fps recomputes the same answer 60× per second. In the sampler at 1 Hz, it runs once per tick. The renderer iterates the prepared slice; cell rendering stays branch-free.

## Consequences

### Positive

- **Read-only contract is small and provable.** No process writes, no GPU vendor SDK calls beyond what sysmetrics already does. The trust surface is `*sysmetrics.Bundle` and the egui FFFI boundary — both already exercised by ADR-0019 and the existing demos.
- **Decoupled sampler + renderer** means render-loop latency is a function of egui only; sampler latency is a function of `/proc` + `/sys` only. Diagnosing one does not require disentangling the other. The existing `metrics.Current` frame-timing overlay (Go-render / Rust-interpret / vsync-slack slots) covers the render side; sampler latency lands in its own per-tick log line.
- **Five independently shippable milestones.** M1 demonstrates the architecture against just CPU + MEM; M2 fans out to all collectors; M3 lands the only non-trivial widget choice (process table); M4 + M5 are polish. Risk concentrates in M1 + M3.
- **Sub-second freshness, 60 fps render.** 1 Hz sampler with 60 Hz render gives a smooth display of slowly-changing data without the renderer ever waiting on `/proc`.
- **GPU support free-after-`gpu_rocm`-tag.** Adding `gpu_intel` or `gpu_nvml` is a `./tags` edit + recompile, no imztop code change.

### Negative

- **No user-arrangeable layout in M1–M5.** Power users used to btop's modal panel toggling get a fixed nested-panel arrangement until DockArea lands as a follow-on. Mitigated by per-panel collapse (`CollapsingHeader`) and by the M5 keybind toggles.
- **No theme switching.** Single dark palette; users locked to it. Acceptable for M1–M5; will become a feature request if multiple users adopt imztop.
- **Time-axis labels are numeric until M5.** `1746635320` is not pleasant; mitigated by a short history window (10 minutes) and a sticky right-edge anchor that always shows "now".
- **GPU support gated on `gpu_rocm` only by default.** A user with NVIDIA hardware sees an empty GPU panel until they edit `./tags`. Documented in M2 done-criteria.
- **Pause is keybind-only.** No on-screen pause button in M1–M5. Cheap to add later; left out of M4 to keep the top bar lean.

### Neutral

- **Subcommand vs. top-level binary** is reversible. If imztop becomes the primary tool, splitting it out into `cmd/imztop` is a one-day refactor — same RenderLoopHandler, different shell.
- **Ring buffer cap = 600 samples** is a knob. Increasing to 3600 (1-hour window) is one constant; the memory cost (~100 KB for 12 series) is irrelevant either way.

### Derived practices

- **Sampler goroutines own their `*sysmetrics.Bundle`.** No two goroutines share a Bundle. Rationale: `Bundle.Sample` is internally concurrent and not designed for cross-goroutine reuse; a second goroutine must construct a second Bundle.
- **Slices passed across the FFFI boundary** (subject to the per-Sync databinding reset documented in CLAUDE.md "FFFI databindings reset every Sync") come from the Sampler's stable ring backing arrays. Re-slice; do not allocate per frame. This is enforced by code review in M1 — there is no automated lint for it.
- **Panel render functions are pure of the Sampler.** Each `renderXPanel(snap *PublishedSnapshot)` takes the snapshot pointer as an argument; no package-level reach into `Sampler.Latest()` from inside a panel. Keeps test fixtures trivial.

## Open questions

Tracked as named follow-ons; resolved at the milestone where they bind.

1. **Top bar placement** — does imztop add its own row inside the host's `PanelTop`, or claim a new `PanelTopInside` below the host's menu? Decided in M4 once the visual density is clear.
2. **Per-panel collapse policy** — collapse-by-default for less-common panels (BATTERY, SENSORS) on first run? Decided in M4 with first-user feedback.
3. **Process panel column set** — minimum columns for the read-only view; do we show TIME+ (cumulative CPU time), nice value, threads count? btop shows all of these; sysmetrics provides `proc.Info` per [ADR-0019 §Public-API-sketch](./0019-observability-sysmetrics-linux-collector.md). Decided in M3.
4. **Sampler granularity for the proc panel** — 1 Hz is fine for CPU/MEM/NET/DISK; the proc panel may want sampling at 500 ms when the user is actively interacting (sort/filter). Decoupled from CPU/MEM cadence, or unified? Decided in M3.
5. **GPU panel layout when multiple AMD GPUs are present.** Single panel with a device-picker dropdown, vs. one panel per device. Decided in M2.

## Status

Accepted 2026-05-03 by @spx. Implementation tracked across M1–M5 above.

Status lifecycle: `Proposed → Accepted → (Deferred | Deprecated | Superseded by ADR-XXXX)`. See boxer's `DOCUMENTATION_STANDARD.md` §1 ADR for the edit-policy tiers (Tier 1 in-place / Tier 2 `## Updates` H3 / Tier 3 superseding ADR).

## Updates

### 2026-07-12 — `Proc Map` panel (process-tree treemap) + freeze relabel

Adds a `Proc Map` dock tab (`dockTabProcMap`) that draws the live process tree as a treemap: processes nested by PPID, each box sized by resident memory (RSS, the default) or CPU%, tinted by CPU load. It complements the text process-tree (the proc panel's `tree` toggle, 2026-05-30 Update): that view answers *who spawned whom* structurally; this one answers *where is memory / CPU going, by subtree* spatially. Built entirely over existing bus data (`ProcInfo.PID` / `PPID` / `RSSBytes` / `CPUPercent`, delivered on the [ADR-0090](./0090-sysmetrics-pubsub-data-plane.md) metric plane) and the existing `treemap` widget — no new dependency, IDL, or scraper change, so no separate ADR.

- **Reuses the `treemap` widget and the `Topology` panel's idiom.** Same monochrome-depth + continuous-CPU-load `CompositeColoring`, same `WithMaxNestingDepth(0)` whole-forest render, same `SetContainerSize`-to-pane and hover-detail line. The area metric (RSS ⇄ CPU%) is a `ComboBox`, matching the Topology dim-switch. RSS is the default because CPU% is 0 for most processes, which the min-cell cull would otherwise hide.
- **Self-leaves for a parent's own weight.** `layout.Node.TotalSize()` ignores an interior node's own `Size` (a parent's area is the sum of its children), so a process that *has* children is given a synthetic self-leaf carrying its own RSS/CPU; otherwise a heavyweight parent with light children reads as tiny.
- **Rebuilt every sample, with stable node identity.** Unlike the static Topology tree, the process set and sizes change each snapshot, so the tree is rebuilt in `reconcileProcTree`. Process nodes are pooled by `(PID, StartedAt)` — the same key the per-process CPU EWMA uses — so their pointer identity (which the widget keys drill state off) survives rebuilds; the breadcrumb is healed (`DrillTo` / `Reset`) when the focused process is reparented or vanishes. Orphans (a child whose parent fell outside the sampler's top-N cap) reparent under a synthetic root; the forest builder is cycle-safe against PID reuse.
- **Rebuild gated on the sample clock, not the frame.** The visible tab repaints at ~60 fps, but the process data changes only at the ~1 Hz sample cadence, so the O(n) rebuild is gated on `SampledAtUnixMs` and runs at most once per sample. A hidden tab is skipped entirely by the `widgets/lazypane` gate that co-landed for the heavy tabs (CPU / Topology / Proc Map): it is the general early-out for the `DockArea`'s otherwise-*late* cull, where an inactive tab would still run its whole Go body every frame ([ADR-0012](./0012-imzero2-collapsible-retained-bodies.md)). On re-show the stale sample clock triggers one catch-up rebuild.

Also relabels the top-bar global freeze `Pause` / `Resume` → `Freeze` / `Go live` and adds a "FROZEN — live updates paused" indicator, so a frozen (stale-but-plausible) view is not mistaken for live; the underlying `Sampler.Pause` (a process-singleton frame-drop that freezes every panel at once) is unchanged.

`status` and `reviewed-date` are deliberately not re-stamped: this adds one panel over existing bus data plus a label change, not a new decision.

### 2026-05-30 — Pressure tab + process-tree mode

Two panel additions beyond the M1–M5 btop set:

- **Pressure tab** (`dockTabPressure`) renders the new [`psi`](./0019-observability-sysmetrics-linux-collector.md) collector — CPU / memory / IO stall shares (`some` / `full`, over 10/60/300 s). PSI has no btop analogue; it answers "which resource is the bottleneck", which utilisation alone cannot. Values-only for now; avg-history sparklines are a deferred follow-up.
- **Process-tree mode** in the proc panel: a `tree` toggle reorders the (filtered + sorted) rows into a PPID forest, depth-first, indenting the Name column. Render-only over the existing `proc.Info.PPID`; siblings keep the active sort; the table's top-N-by-CPU cap means the tree is of the *visible* processes (orphans become roots). A `visited` guard makes PID-reuse cycles safe.

`status` / `reviewed-date` unchanged.

### 2026-05-29 — `Topology` panel (lstopo-style CPU hierarchy)

Adds a `Topology` dock tab (`dockTabTopo`) that draws the CPU package → NUMA → L3/L2/L1 → core → SMT-thread containment tree, the view `lstopo`(1) is known for. Data comes from the static [`cpu.ReadTopology`](./0019-observability-sysmetrics-linux-collector.md) reader added in ADR-0019's Update of the same date; it is read **once** at `App` construction and never re-sampled.

- **Rendering reuses the `treemap` widget** ([`../../public/thestack/imzero2/egui2/widgets/treemap/`](../../public/thestack/imzero2/egui2/widgets/treemap/)) rather than a bespoke lstopo grid-pack layout. The topology maps to a `layout.Node` tree (PU leaves `Size:1`); cache/package nesting becomes parent/child boxes, which is the structurally meaningful part lstopo conveys. **Trade-off recorded:** squarify weights box area by value, so equal-weight leaves yield an *irregular* grid, not lstopo's uniform cells. Accepted — it reuses a production widget and inherits hover, drill-in, and zoom for free; faithful grid-pack remains a later option if the irregular packing reads poorly on high-core-count machines. The whole hierarchy is shown at once (not just the drilled level): this drove one small, backward-compatible addition to the widget — `WithMaxNestingDepth(0)` makes its non-interactive preview recurse the entire subtree (bounded by a min-cell cull), so every core/thread is visible without drilling while drill-in/zoom still work on the top-level boxes. The canvas tracks `ui.available_size` each frame (`SetContainerSize`) so it fills the dock pane.
- **Static structure, live tint.** The `layout.Node` tree is built once (its node pointers are the identity the widget keys drill-in state on, so it must be stable across frames). Two colour channels encode orthogonal things: structural **depth** uses the IDS curated *monochrome* ramp (Crameri grayC — `styletokens.SequentialGrayC`, the same grayscale `AccessibilityMonochrome` selects), reserving the one *coloured* sequential gradient for **magnitude** — a per-PU live [`treemap.ContinuousColoring`](../../public/thestack/imzero2/egui2/widgets/treemap/coloring.go) layered last so it wins on PU leaves (non-PU `fn`→`NaN` falls through to the depth base; order matters because `CompositeColoring` is last-ok-wins and depth always opines). The tinted dimension is **switchable** between per-core utilisation and frequency, the value normalised to `[0,1]` against a shared `topoScaleMax` so the tint and a [`colorscale`](../../public/thestack/imzero2/egui2/widgets/colorscale) legend (a real-unit value axis below the tree) always agree. Utilisation's max is the known 100; frequency's is *not* known a priori, so it uses a smoothed fast-attack/slow-release peak estimate — the legend rebuilds only when that rounded max (or the dimension) changes. A hover line below the tree reads out per-object detail (a PU's live %/MHz + cpufreq policy [governor, min–max], a cache's sharing fan-out, the package's core/thread counts + watts + temperature, a NUMA node's local RAM, and the machine's system-memory total/used); NUMA boxes also carry their RAM size in the label, lstopo-style, and PUs outside the cgroup-effective cpuset (`ActiveCPUs`) render inactive (grey, via the tint fall-through). The renderer refreshes the live slices each frame from the published snapshot before `Render()`; the tree never changes. Net effect is a live "lstopo + heatmap" — the closed-loop-observability angle a static `lstopo` invocation can't give.

`status` and `reviewed-date` are deliberately not re-stamped: the M1–M5 panel-set decision stands; this adds one panel built from a new static data source.

### 2026-05-21 — SD5 sliding-window honesty rename

SD5 framed the per-series history buffer as "head + length tracked atomically" — a true ring with O(1) push and stable backing memory. The shipped implementation at [`../../apps/imztop/imztop_slidingwindow.go`](../../apps/imztop/imztop_slidingwindow.go) is a memmove-on-full sliding window: `Push` is O(1) until full, then O(cap) per push (`copy(data, data[1:])`); `Values()` returns the same backing slice on every call. At cap≈600 and 1Hz that's ~5 KB/s of memcpy on 8-byte floats — negligible — and the published-snapshot path copies the window into a fresh slice per tick (`copyFloats` in [`../../apps/imztop/imztop_sampler.go`](../../apps/imztop/imztop_sampler.go)) so the renderer never aliases live window memory either. The type was renamed `Ring` → `SlidingWindow` (and the file from `imztop_ringbuf.go`) to stop overpromising; the head+length upgrade remains a defensible option if the new opt-in M1 heap-drift guard ([`../../apps/imztop/imztop_sampler_heap_test.go`](../../apps/imztop/imztop_sampler_heap_test.go), `IMZTOP_HEAP_TEST=1`) or a future profiling pass flags the per-tick copies as material. `status` and `reviewed-date` are deliberately not re-stamped.

### 2026-05-15 — keelson namespace path migration (ADR-0035)

Runtime-tree path references in this ADR were swept from `public/thestack/runtime/...` to `public/keelson/runtime/...` as part of the keelson namespace introduction ([ADR-0035](./0035-keelson-namespace-introduction.md)). The imztop app itself was moved from `public/thestack/imztop/` to `apps/imztop/` (Step 5 of the migration); per ADR-0026's `Manifest.Id`-equals-import-path rule, the AppId moved with it (historical fact rows under the old AppId are orphaned, accepted because the runtime is pre-stable). The decision recorded here is unchanged; only path strings reflect the new locations. `status` and `reviewed-date` are deliberately not re-stamped.

## References

- [ADR-0019](./0019-observability-sysmetrics-linux-collector.md) — sysmetrics data layer (parent ADR; this ADR is the GUI consumer).
- [`btop`](https://github.com/aristocratos/btop) — feature-parity reference; Apache 2.0.
- [`btop/LICENSE`](https://github.com/aristocratos/btop/blob/main/LICENSE) — license verification.
- [`../../public/observability/sysmetrics/`](../../public/observability/sysmetrics) — data source.
- `../../doc/observability/sysmetrics/REFERENCE.md` — public API of sysmetrics.
- [`../../public/thestack/imzero2/egui2/demo/carousel/imzero2_demo_resolve.go`](../../public/thestack/imzero2/egui2/demo/carousel/imzero2_demo_resolve.go) — host shell + subcommand registry.
- [`../../public/thestack/imzero2/egui2/demo/apps/regex_explorer/regex_explorer.go`](../../public/thestack/imzero2/egui2/demo/apps/regex_explorer/regex_explorer.go) — multi-panel demo precedent.
- [`../../public/thestack/imzero2/egui2/demo/apps/widgets/egui2_hl_plot_demo.go`](../../public/thestack/imzero2/egui2/demo/apps/widgets/egui2_hl_plot_demo.go) — `PlotLine` / `PlotBars` usage.
- [`../../public/thestack/imzero2/egui2/demo/apps/widgets/egui2_hl_etable_demo.go`](../../public/thestack/imzero2/egui2/demo/apps/widgets/egui2_hl_etable_demo.go) — virtualized-table usage at 10k rows.
- [`public/math/numerical/timeticks/`](../../public/math/numerical/timeticks/) — calendar-aware time-axis tick generator (M5).
- [`../../tags`](../../tags) — build-tag listing; `gpu_rocm` appended in M1.
- [ADR-0013](./0013-imzero2-stateful-widget-contract.md) — stateful-widget contract; relevant for any future settings dialog.
- `../../CLAUDE.md` — repo conventions, build tags, ImZero2-local invariants.
