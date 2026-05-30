---
type: adr
status: accepted
date: 2026-05-30
reviewed-by: "@spx"
reviewed-date: 2026-05-30
---

> **Status: accepted 2026-05-30 by @spx.** First cut scoped to M1–M4 (spine + Heap + GC + Scheduler); observe-only. Implementation tracked across M1–M4 in this document.

# ADR-0061: `imzrt` — Go Runtime Observability Dashboard on ImZero2

## Context

[ADR-0020](./0020-imzero2-imztop-resource-monitor.md) built `imztop`, a btop-style monitor for *system* resources, on top of the [`sysmetrics` data layer](../../public/observability/sysmetrics) ([ADR-0019](./0019-observability-sysmetrics-linux-collector.md)). `imztop` answers "what is the **machine** doing" — per-core CPU, RAM, disk, NUMA topology.

This ADR proposes its inward twin: **`imzrt`** answers "what is **this Go process** doing" — heap occupancy and the GC sawtooth, GC pause distributions, goroutine population and scheduling latency. The data source is the running program's own Go runtime, read through the stdlib `runtime/metrics` package. It is imztop's structural isomorph pointed at the runtime instead of the OS: the same decoupled-sampler / `atomic.Pointer` snapshot / sliding-window-history / dock-tab spine, against a different collector.

**Prior art, named honestly.** Live Go-runtime dashboards are an established genre — [`statsviz`](https://github.com/arl/statsviz) (browser, charts the full `runtime/metrics` set), `expvarmon` (terminal, `expvar`), `gcvis` (parses `GODEBUG=gctrace=1`). The canonical data source is the stdlib `runtime/metrics` package itself. So the justification for `imzrt` is **not** novelty of concept. It is three things this repo can do that a browser tool cannot:

1. **Spine reuse, not a new mechanism.** It is the imztop sampler isomorphism (proven in ADR-0020) over a new collector — the marginal surface is the collector plus three panels, not a monitoring stack.
2. **Two views the standard tools render poorly.** A *scheduling-latency spectrogram* (the `/sched/latencies` histogram over time, as a scrolling heatmap) and a *CPU-class "GC tax" breakdown* (`/cpu/classes/*`, deferred to a follow-on). Flat gauges and single lines lose the distribution; the spectrogram keeps it.
3. **Closed-loop tie-in.** Runtime snapshots can later land in the facts table / ClickHouse ([ADR-0050](./0050-clickhouse-observability-pipeline.md)) so a GC-pause spike correlates with an app event — the observability angle a stateless browser readout can't reach.

Without (2) and (3) `imzrt` is "statsviz in an egui window," and this ADR says so plainly. The first cut (M1–M4) earns (1) and (2); (3) is named future work.

Forces the design must respect (inherited from ADR-0020 unless noted):

- **ImZero2 continuous rendering.** The frame loop ticks at compositor cadence (~60 Hz) and must never block on a read. Collector loop and render loop are decoupled by construction.
- **FFFI databindings reset every Sync.** Slices handed to plot widgets must come from stable backing memory and be re-sliced, not re-allocated (CLAUDE.md "FFFI databindings reset every Sync").
- **Observer effect (new for imzrt).** The dashboard runs *inside* the process it measures — its sampling allocates, its rendering consumes CPU that shows up in the runtime's own CPU accounting. Unlike imztop reading `/proc` of *other* processes, `imzrt` perturbs the very signal it displays. The design must minimise and disclose this (see SD7).
- **Portability (new for imzrt).** `runtime/metrics` is platform-independent; the collector carries **no build tags**, in deliberate contrast to the Linux-only, tag-gated `sysmetrics`. It must tolerate individual metrics being absent on a given Go version or platform.
- **Boxer conventions** (CLAUDE.md, [ADR-0055](./0055-adopt-boxer-standards.md)) — `inst` receivers, `*I`/`*E` suffixes, `eh.Errorf`, sized integers, zero-value-usable structs.
- **Keelson app contract.** Implements `app.AppI` (`Manifest`/`Mount`/`Frame`/`Unmount`) and registers a factory into `app.DefaultRegistry`, the model `imztop` uses today ([ADR-0026](./0026-app-runtime-and-capability-subjects.md), [ADR-0057](./0057-demo-registry-and-drivers.md)) — not the `imzero2` `case N` switch ADR-0020 originally described, which has since been superseded by the manifest registry.

**Naming / disambiguation.** `imzrt` = "imzero2 + Go runtime." It is distinct from the existing [`runtimestatus` widget](../../public/thestack/imzero2/egui2/widgets/runtimestatus), whose "runtime" means the *keelson service* runtime (facts store, bus, fs, persist) — not the Go runtime. The names sit close; this note is the disambiguation.

**One ADR, not two.** ADR-0019/0020 split the collector and the app across two ADRs because `sysmetrics` was a large standalone data layer. The `goruntime` collector here is thin — a stateless snapshot of `runtime/metrics` — so collector and app share this one ADR. If the collector grows a second consumer (a headless exporter, a facts-table writer), it earns its own ADR retroactively.

## Design space (QOC)

### Q1 — How do we turn cumulative histograms into "what is happening now"?

`runtime/metrics` exposes `/gc/pauses:seconds` and `/sched/latencies:seconds` as `Float64Histogram` — `Buckets []float64` (boundaries) and `Counts []uint64` that are **cumulative since process start**. A raw cumulative histogram is dominated by the first seconds of the program and never reflects the current regime.

**Options.**

- **O1 — Bucket-wise windowed delta.** Keep the previous tick's `Counts`; publish `cur.Counts[i] - prev.Counts[i]` as the per-interval histogram. Quantiles computed from the delta.
- **O2 — Cumulative only.** Show lifetime quantiles, no windowing. Trivial, but answers the wrong question.
- **O3 — Reconstruct synthetic samples + t-digest.** Expand bucket counts into representative samples, feed a `tdigest` (as imztop does for CPU distributions). More machinery, lossy twice (bucketing then digesting).

**Criteria.** C1 — answers "now"; C2 — implementation cost; C3 — allocation on the hot path; C4 — fidelity.

|    | O1 (windowed delta) | O2 (cumulative) | O3 (digest) |
|----|---------------------|-----------------|-------------|
| C1 — answers "now"     | ++ | −− | ++ |
| C2 — implementation cost | +  | ++ | −  |
| C3 — hot-path allocation | ++ | ++ | −  |
| C4 — fidelity            | +  | +  | −  |

**Choice: O1.** Bucket-wise subtraction is one loop over a fixed-length slice, allocation-free with a reused output buffer, and preserves the runtime's own bucket boundaries exactly. Quantiles come from accumulating the windowed counts to the target fraction and interpolating at the bucket edge (the runtime's buckets are exponentially spaced, so within-bucket interpolation is approximate and labelled as such). O3's extra lossiness buys nothing the delta histogram lacks.

### Q2 — How does the collector expose a heterogeneous, version-varying metric set?

`metrics.All()` returns ~50 metrics whose membership shifts across Go versions. The collector must serve a curated panel set without breaking when a metric is added or removed.

**Options.**

- **O1 — Fully typed struct.** Named field per metric. Ergonomic, but hand-tracks the catalog and silently misses metrics the installed Go adds.
- **O2 — Generic passthrough.** Expose the raw `[]metrics.Sample` + a `name→index` map; the app indexes by string. Robust, less ergonomic, stringly-typed at every call site.
- **O3 — Hybrid.** Typed fields for the curated subset the panels render, **plus** the raw slice + index for completeness and forward-compat. Absent metrics leave their typed field at zero and are simply missing from the raw map.

**Choice: O3.** The panels read typed fields (`snap.Goroutines`, `snap.HeapLiveBytes`, `snap.GCPauses`); anything not yet promoted to a field is reachable via `snap.Lookup("/some/metric:unit")`. The collector builds its `[]metrics.Sample` request template and name index **once**, then `metrics.Read`s into caller-owned memory every tick — zero steady-state allocation. A metric named in the curated set but absent from `metrics.All()` on this Go/platform is skipped at template-build time and its field stays zero (panels treat zero/absent as "not available" and hide the affected element).

## Decision

Build **`imzrt`** as a new keelson app package `apps/imzrt/`, mirroring `apps/imztop/`'s current file layout and lifecycle. Introduce a shared, build-tag-free collector `public/observability/goruntime/` — the `sysmetrics.Bundle` analog for the Go runtime. A **process-singleton** `Sampler` (one Go runtime per process ⇒ one shared history) owns the collector, samples on a 1 Hz (configurable) ticker into per-series `SlidingWindow` buffers, and publishes an immutable `PublishedSnapshot` via `atomic.Pointer`; the frame loop reads it lock-free. The app is **observe-only** — it declares no capabilities and mutates no runtime state.

First cut = **M1–M4**: collector + sampler spine + top bar + **Heap**, **GC**, **Scheduler** dock tabs (Scheduler carries the latency spectrogram). CPU-classes and allocation-size-class panels, interactive knobs, flight-recorder capture, and facts-table persistence are named follow-ons (see Out of scope).

## Subsidiary design decisions

- **SD1 — App name `imzrt`, package `apps/imzrt/`.** Sibling to `apps/imztop/`. Disambiguated from the `runtimestatus` widget (keelson service runtime, not Go runtime). Manifest `Id` = `github.com/stergiotis/boxer/apps/imzrt` (the import path, per [ADR-0026](./0026-app-runtime-and-capability-subjects.md)). `Display`/`Title` = "imzrt"; icon a Phosphor glyph distinct from imztop's `PhGauge` (e.g. `PhPulse`/`PhHeartbeat`, pinned in M1); `Category: "Tools"`.

- **SD2 — Registered via `app.Manifest` + `DefaultRegistry.RegisterFactory`.** `Surface: app.SurfaceWindowed`. Factory-backed: each window gets its own `*App` (per-window UI state), all sharing the one process Sampler (SD3). This is imztop's current registration model, not the legacy `imzero2` subcommand switch.

- **SD3 — Process-singleton Sampler.** There is exactly one Go runtime per process, so a single shared sampler is the *correct* model, not merely an optimisation as it was for imztop. Constructed lazily via `sync.Once` (`ensureSampler()`), started on first `Frame`, runs for process lifetime regardless of how many windows are open or focused — so history stays continuous even when every window is hidden. Per-`App` state holds only UI selections (active tab, hovered series, paused-view flag).

- **SD4 — Shared `goruntime.Collector`, app-private `Sampler`.** Mirrors the `sysmetrics.Bundle` (shared, stateless snapshot) vs imztop `Sampler` (app-private history + atomic publish) split. `Collector.Read(*Snapshot)` is a pure read into caller-owned memory; the `Sampler` owns ring buffers, rate derivation, histogram windowing, and the `atomic.Pointer`.

- **SD5 — Build-tag-free and portable.** The collector reads only `runtime/metrics` (plus, for deferred panels, `runtime.MemStats` / `debug.ReadGCStats`). No cgo, no `/proc`, no platform SDK — it runs anywhere boxer runs, unlike `sysmetrics`. The curated-metric template is intersected with `metrics.All()` at construction, so a metric absent on the installed Go version is simply not requested and its typed field stays zero.

- **SD6 — Observe-only; `Caps: nil`.** `imzrt` reads GOGC and GOMEMLIMIT (via `/gc/gogc:percent` and `/gc/gomemlimit:bytes`) but never calls `debug.SetGCPercent`, `debug.SetMemoryLimit`, `runtime.GC`, or `runtime.GOMAXPROCS(n)`. The trust surface is `runtime/metrics` + the egui FFFI boundary. Interactive knobs would mutate global process state and belong behind a capability subject + confirm in a separate ADR (Out of scope).

- **SD7 — Prefer `metrics.Read` over `runtime.ReadMemStats` on the hot path (observer effect).** `runtime.ReadMemStats` performs a stop-the-world; sampling it at 1 Hz would inject a periodic STW into the very `/gc/pauses` histogram the GC panel displays. `metrics.Read` does **not** stop the world. M1–M4 therefore read exclusively through `runtime/metrics`. The allocation-size-class panel (which needs `MemStats.BySize`, unavailable in `runtime/metrics`) is deferred precisely for this reason; when added it samples at a coarser cadence or behind an explicit toggle, with the STW cost disclosed in-UI. The observer effect is stated plainly in the app's help corpus and an info line in the top bar.

- **SD8 — Counters → rates in the Sampler.** Most GC/alloc/cgo metrics are cumulative; the Sampler keeps the previous snapshot and publishes Δ/Δt (GC cycles·s⁻¹, alloc bytes·s⁻¹, cgo calls·s⁻¹). `prev` is seeded on the first tick; rate series start empty and publish from the second tick. Same pattern as imztop's disk/net bytes→MiB·s⁻¹.

- **SD9 — Cumulative histograms → windowed deltas (Q1/O1).** A small `imzrt_histogram.go` holds the bucket-wise subtraction and quantile-from-buckets helper. Applied to `/gc/pauses` (GC panel) and `/sched/latencies` (Scheduler panel).

- **SD10 — Scheduling-latency spectrogram via `heatmapscroll`.** The Scheduler panel renders `/sched/latencies` over time as a scrolling heatmap (reusing the [`heatmapscroll` widget](../../public/thestack/imzero2/egui2/widgets/heatmapscroll) / scrolling-texture substrate, [ADR-0058](./0058-imzero2-scrolling-texture-widget.md)) — each frame appends one column = the current windowed latency histogram, log-binned on the Y axis, coloured by bucket density via the Go-side [`colormap` helper](../../public/thestack/imzero2/egui2/widgets/colormap) with a [`colorscale` legend](../../public/thestack/imzero2/egui2/widgets/colorscale). This is imztop's per-core CPU-heatmap pattern reused verbatim. A rolling p99 line overlays it.

- **SD11 — Distribution rendering via `distsummary`.** The GC panel renders the windowed pause distribution with the [`distsummary` widget](../../public/thestack/imzero2/egui2/widgets/distsummary) (the boxenplot/ECDF family imztop already uses), plus rolling p50/p99/max lines.

- **SD12 — Time-series via the egui2 plot widgets.** Memory classes as stacked filled `PlotLine` areas; `/gc/heap/goal` and GOMEMLIMIT as `PlotHLine` reference lines over the live-heap sawtooth; GC rate (auto vs forced) as `PlotBars`; goroutine count as `PlotLine`. ProgressBars for headroom-to-limit gauges.

- **SD13 — `SlidingWindow[T]` reuse.** imztop's [`SlidingWindow`](../../apps/imztop/imztop_slidingwindow.go) (memmove-on-full, stable backing, per-tick copy-out — see ADR-0020's 2026-05-21 update) is exactly what `imzrt` needs. Lifting it into a shared package (e.g. `public/observability/.../slidingwindow`) so both apps depend on one copy is the clean move, but it is **not** gated on M1 — M1 may duplicate the ~60-line type and the extraction lands as a tidy-up (Open question 3).

- **SD14 — Theme: named-colour thresholds over styletokens.** An `imzrt_theme.go` mirrors `imztop_theme.go` — `good`/`warn`/`hot` keyed against the IDS [`styletokens` palette](../../public/keelson/designsystem/styletokens), driving threshold tints: pause p99 over budget → `warn`/`hot`, goroutine count climbing without bound → `warn`, headroom-to-GOMEMLIMIT shrinking → `hot`. No theme-file parser.

- **SD15 — Screenshot tour + headless TestDriver.** An `imzrt_tour.go` registers a deterministic fixture (a synthesised `PublishedSnapshot`, no live runtime) for reproducible PNG capture under the TestDriver ([ADR-0057](./0057-demo-registry-and-drivers.md)), mirroring `imztop_tour.go`. The live sampler is bypassed in tour mode so captures are stable across runs.

## Public API sketch

Illustrative — fields and names pinned during M1.

```go
// public/observability/goruntime/collector.go
package goruntime

// Histogram mirrors metrics.Float64Histogram: Counts has len(Buckets)-1
// entries, cumulative since process start.
type Histogram struct {
    Buckets []float64
    Counts  []uint64
}

type Snapshot struct {
    SampledAtUnixNano int64

    // Curated scalars (zero when the backing metric is absent on this Go/platform).
    Goroutines        uint64
    GomaxProcs        uint64
    HeapLiveBytes     uint64 // /gc/heap/live; falls back to allocs-frees if absent
    HeapGoalBytes     uint64 // /gc/heap/goal
    HeapObjectsBytes  uint64
    HeapFreeBytes     uint64
    HeapReleasedBytes uint64
    MemoryTotalBytes  uint64 // /memory/classes/total
    GCCyclesTotal     uint64
    GCCyclesForced    uint64
    AllocBytesTotal   uint64 // cumulative
    FreeBytesTotal    uint64 // cumulative
    GOGCPercent       uint64 // /gc/gogc:percent
    GOMemLimitBytes   uint64 // /gc/gomemlimit; math.MaxInt64 sentinel ⇒ unset
    CgoCallsTotal     uint64
    MutexWaitTotalSec float64

    GCPauses       Histogram // /gc/pauses:seconds (cumulative)
    SchedLatencies Histogram // /sched/latencies:seconds (cumulative)
}

type Collector struct { /* unexported: []metrics.Sample template + name index */ }

func NewCollector() (inst *Collector)

// Read fills snap in place. Zero allocations in steady state.
func (inst *Collector) Read(snap *Snapshot) (err error)

// Lookup returns a not-yet-promoted metric by runtime/metrics name; ok=false if absent.
func (inst *Collector) Lookup(snap *Snapshot, name string) (v metrics.Value, ok bool)
```

```go
// apps/imzrt/imzrt_sampler.go
package imzrt

type SamplerOptions struct {
    UpdateInterval time.Duration // default 1*time.Second
    HistoryWindow  time.Duration // default 10*time.Minute
}

type Sampler struct { /* unexported; collector, rings, atomic.Pointer[PublishedSnapshot] */ }

func NewSampler(opts SamplerOptions) (inst *Sampler, err error)
func (inst *Sampler) Start(ctx context.Context) (err error)
func (inst *Sampler) Latest() (snap *PublishedSnapshot) // atomic.Load; borrowed
func (inst *Sampler) Close() (err error)
```

```go
// apps/imzrt/imzrt_histogram.go
package imzrt

// WindowDelta writes cur.Counts-prev.Counts into out (per-interval counts).
func WindowDelta(cur, prev goruntime.Histogram, out *WindowedHistogram)

type WindowedHistogram struct { Buckets []float64; Counts []uint64 }

// Quantile returns the value at q in [0,1] via bucket-edge interpolation (approximate;
// runtime buckets are exponentially spaced).
func (inst *WindowedHistogram) Quantile(q float64) (seconds float64)
```

## Implementation plan

Four milestones, each independently shippable. Green `scripts/ci/lint.sh` + `scripts/ci/gotest.sh`, a CHANGELOG entry, and visual verification gate each transition. Build/test/vet run with `-tags="$(cat ./tags)"` (CLAUDE.md / boxer build-tag convention).

### M1 — Collector + Sampler spine + top bar + Heap panel

`public/observability/goruntime/{collector.go,doc.go,collector_test.go}`; `apps/imzrt/{app_register.go,imzrt.go,imzrt_sampler.go,imzrt_panel_topbar.go,imzrt_panel_heap.go}` (+ `SlidingWindow`, duplicated or shared per SD13). Top bar: app name, Go version + GOOS/GOARCH, GOMAXPROCS, live GOGC + GOMEMLIMIT, gauges (goroutines / live-heap / GC count), pause·resume, interval ±, sampler status, observer-effect note. Heap panel: stacked memory-classes area + live-heap-vs-goal-vs-GOMEMLIMIT sawtooth + headroom gauge.

**Done when:** the app opens from the registry, the Heap panel shows live memory classes and the GC sawtooth updating at 1 Hz, GOMEMLIMIT line hides when unset; the collector tolerates a curated metric being absent (unit test stubs `metrics.All()` minus one name); 60 s of running does not grow the sampler's Go heap (`IMZRT_HEAP_TEST=1`, mirroring imztop's guard); `lint.sh` green.

### M2 — GC panel + histogram windowing

`apps/imzrt/{imzrt_histogram.go,imzrt_panel_gc.go}` + `imzrt_histogram_test.go`. Windowed `/gc/pauses` distribution via `distsummary` + rolling p50/p99/max; GC rate (auto vs forced) `PlotBars`; alloc rate (bytes·s⁻¹, objects·s⁻¹); GC-cycle counter. The histogram helper lands here as the first consumer.

**Done when:** `WindowDelta` + `Quantile` pass a table test (known cumulative pairs → expected per-interval quantiles, including the empty-first-tick and no-new-pauses cases); the GC panel shows a non-degenerate pause distribution under a synthetic alloc-churn workload; rates read zero (not NaN) on the first tick.

### M3 — Scheduler panel + latency spectrogram

`apps/imzrt/imzrt_panel_sched.go`. Goroutine count `PlotLine`; `/sched/latencies` spectrogram via `heatmapscroll` (SD10) with a `colorscale` legend; rolling p99 latency overlay; GOMAXPROCS vs NumCPU; STW totals (`/sched/pauses/total/{gc,other}` when present); cgo call rate.

**Done when:** the spectrogram scrolls one column per sample and its colour density visibly tracks induced scheduling pressure (a goroutine storm fixture); p99 overlay aligns with the spectrogram's hot band; the panel degrades cleanly (hides the STW sub-row) on a Go version lacking `/sched/pauses/total/*`.

### M4 — Polish: theme, pause/interval, screenshot tour

`apps/imzrt/{imzrt_theme.go,imzrt_tour.go}` + keybindings (`space` pause-view, `+`/`-` interval). Threshold tints (SD14); deterministic fixture tour + TestDriver registration (SD15).

**Done when:** threshold colours fire at their cutoffs; pause freezes the displayed snapshot while the sampler keeps running; the TestDriver produces stable PNGs across two runs; CHANGELOG updated.

### Out of scope for this ADR (named follow-ons)

- **CPU-classes "GC tax" panel.** Stacked `/cpu/classes/*` (user / gc-mark-{dedicated,assist,idle} / gc-pause / scavenge / idle) as a fraction of GOMAXPROCS·time. High-value, but a fourth panel; M5 or a follow-on.
- **Allocation-size-class panel.** `runtime.MemStats.BySize` (61 classes) + tiny-allocator. Needs `ReadMemStats` (STW) — deferred per SD7; coarser cadence / toggle when it lands.
- **Interactive knobs.** Buttons to set GOGC/GOMEMLIMIT or force a GC. Mutates global process state ⇒ separate ADR, gated behind a capability subject + confirm. Changes the observe-only posture (SD6).
- **Flight-recorder capture.** `trace.FlightRecorder` (recent Go) "dump last N seconds on a pause-over-threshold" button. Writing a trace file ⇒ an `fs.dialog.write` capability ⇒ separate ADR. The genuine differentiator over a metrics readout.
- **Facts-table persistence.** Periodic snapshots to ClickHouse ([ADR-0050](./0050-clickhouse-observability-pipeline.md)) for the closed-loop correlation story.
- **Shared `SlidingWindow` extraction** (SD13 / Open question 3).

## Alternatives

- **Reuse `imztop` with a Go-runtime collector domain instead of a new app.** Rejected — `sysmetrics.Bundle` is Linux-/proc-shaped and tag-gated; the Go runtime is portable and STW-sensitive (SD5/SD7). Bolting it on as a `sysmetrics` domain would drag imztop's Linux-only build constraints onto a portable concern and bury the runtime view inside a system monitor. A sibling app keeps both honest.
- **`expvar` / `net/http/pprof` HTTP endpoint instead of in-process.** Rejected for the dashboard — the repo already has a [`profiling` package](../../public/observability/profiling) for pprof. An in-process collector reads `runtime/metrics` directly with no HTTP hop, no serialization, and no open port; it is also what lets the closed-loop facts-table path (future) see the same struct the UI sees.
- **`runtime.ReadMemStats` as the primary source.** Rejected — it stops the world and would corrupt the pause histogram (SD7). `runtime/metrics` is the modern, non-STW, self-describing source the stdlib itself steers toward; `MemStats` is used only for `BySize`, which is deferred.
- **Cumulative histograms shown as-is.** Rejected (Q1/O2) — lifetime quantiles are dominated by startup and never show the current regime.
- **Per-window samplers.** Rejected — there is one Go runtime; N samplers would read the same global stats N times and N× the (already small) observer effect, for N copies of identical history (SD3).

## Consequences

### Positive

- **Small, provable, observe-only contract.** No process writes, no capabilities, no platform SDKs. The trust surface is `runtime/metrics` + the egui FFFI boundary — strictly smaller than imztop's.
- **Portable by construction.** No build tags; runs on every platform boxer targets, unlike imztop.
- **Spine reuse.** Sampler / `atomic.Pointer` / `SlidingWindow` / dock-tab patterns are lifted from a shipped app (ADR-0020); risk concentrates in the new collector and the histogram helper, both unit-testable without the Rust runtime.
- **The distribution survives.** The spectrogram and windowed pause distribution keep information that gauges and single lines discard — the part of the value that isn't "statsviz in a window."

### Negative

- **Observer effect is real and only mitigated, not eliminated.** The dashboard's own allocation and render CPU appear in the runtime it measures. Disclosed in-UI (SD7) but a user reading absolute numbers must account for the watcher. Worst at high frame rates and tiny heaps.
- **Histogram quantiles are approximate.** Exponentially-spaced runtime buckets mean within-bucket interpolation is an estimate; p99 is "p99 to the nearest bucket edge," labelled as such.
- **Version-dependent panels degrade silently-ish.** A metric absent on an older Go leaves its element hidden; a user on an old toolchain sees a thinner dashboard. Mitigated by the absence being visible (hidden row) rather than a zero that reads as real data.
- **No allocation-size view in the first cut.** `BySize` is genuinely useful and its deferral (for the STW reason) is a real gap until the follow-on.

### Neutral

- **Single ADR for collector + app** is reversible: a second collector consumer retroactively justifies splitting out a `goruntime` ADR.
- **History window = 600 samples** is a knob, as in imztop; memory cost is negligible at any reasonable cap.
- **Observe-only is a starting posture, not a ceiling.** Interactive knobs remain open behind a future capability-gated ADR.

### Derived practices

- **`runtime/metrics` is the default runtime data source; `ReadMemStats` is last-resort and STW-disclosed.** Any future runtime panel justifies a `ReadMemStats`/`ReadGCStats` dependency against the observer effect explicitly.
- **Collectors fill caller-owned snapshots.** `Collector.Read(snap)` allocates nothing in steady state; the `[]metrics.Sample` template and name index are built once. New curated fields extend the template, not the per-tick path.
- **Panels are pure of the Sampler.** Each `renderXPanel(snap *PublishedSnapshot)` takes the snapshot as an argument; no panel reaches into `Sampler.Latest()`. Keeps fixtures trivial (mirrors imztop's derived practice).

## Open questions

Resolved at the milestone where they bind.

1. **Spectrogram Y-binning.** Reuse `/sched/latencies`' native exponential buckets as heatmap rows, or re-bin to a fixed log grid for a stable axis across Go versions? Decided in M3.
2. **Heap "live" source.** Prefer `/gc/heap/live` where present, else derive `allocs-frees`; do the two agree closely enough to switch transparently, or must the panel label which is in use? Decided in M1.
3. **`SlidingWindow` extraction.** Lift imztop's type into a shared package now (touches imztop) or after imzrt stabilises? Decided at M4 tidy-up (SD13).
4. **Top-bar density.** The runtime has many single-number knobs (GOGC, GOMEMLIMIT, GOMAXPROCS, NumCPU, version, build revision). Which belong in the always-visible bar vs a collapsible "build info" popover? Decided in M1/M4.
5. **Interval coupling.** 1 Hz suits heap/GC; the scheduling spectrogram may read better at 2 Hz. One cadence or a faster sched sampler? Decided in M3.

## Status

Accepted 2026-05-30 by @spx. Implementation tracked across M1–M4 above.

Status lifecycle: `Proposed → Accepted → (Deferred | Deprecated | Superseded by ADR-XXXX)`. See boxer's `DOCUMENTATION_STANDARD.md` §1 ADR for the edit-policy tiers (Tier 1 in-place / Tier 2 `## Updates` H3 / Tier 3 superseding ADR).

## Updates

### 2026-05-30 — M1–M4 landed; deviations and one spawned refactor

The first cut shipped across four commits — M1 (collector + sampler spine + top
bar + Heap), M2 (GC tab + cumulative-histogram→windowed-delta helper), M3
(Scheduler tab + `/sched/latencies` spectrogram), M4 (screenshot tour + threshold
tints). The accepted decision stands; this records where the implementation
diverged from the text above.

- **No cgo series.** boxer is cgo-free, so `/cgo/go-to-c-calls:calls` is a
  permanently-zero metric. It was dropped from the `goruntime` collector and the
  sampler rather than shown as a flat-zero line — contrary to the `CgoCalls`
  field in the Public API sketch.

- **GC pause distribution as rolling percentile lines, not `distsummary`.** M2
  specified "via `distsummary`." It renders as windowed-delta p50/p99/max lines
  over time instead: feeding a bucketed histogram into `distsummary` (a
  t-digest / sample widget) needs exactly the sample-synthesis step Q1/O1
  rejected as lossy, so the percentile lines are the faithful view. The
  instantaneous per-bucket shape is left to the M3 spectrogram.

- **Pause stops sampling.** The text frames pause as "freeze the displayed
  snapshot while the sampler keeps running"; the implementation stops sampling
  while paused (matching imztop's actual behaviour, ADR-0020 SD14). For a
  self-measuring app this also halts the observer effect, which is the honest
  choice. The display still freezes (`Latest()` holds the last snapshot).

- **Spectrogram clipped + labelled.** SD10 specified the `heatmapscroll`
  spectrogram; the implementation adds a useful-latency clip window
  (`[10ns, 100ms]`, dropping the always-empty extreme bins and the ±Inf ends),
  calendar-aware x-axis time ticks, a y-range caption, and a colour→count
  `colorscale` legend. Separately, `/gc/heap/live` reads zero until the first GC
  completes, so `HeapLive` falls back to allocs-frees (the continuous live-byte
  count that drives the sawtooth).

- **Spawned follow-on: `colorscale`/`colormap` decoupled from `treemap`.** Giving
  the spectrogram a legend required a widget-framework change outside imzrt:
  `colorscale.New` took a `*treemap.Colormap`, so it could not legend the
  heatmap's `colormap.Config`. `colormap.Config` gained the value-axis API
  (`Range`/`IsLog`/`Normalize`/`At`/`IndexAt`), `treemap.Colormap` became a thin
  wrapper over it (deduping the range/log math), and `colorscale.New` now takes
  `*colormap.Config`. Consumers (imztop topology, sccmap, the colorscale demo)
  were updated. No separate ADR, per @spx.

Still deferred exactly as named above: the CPU-classes and allocation-size-class
panels, interactive knobs, flight-recorder capture, and facts-table persistence.
Two smaller open follow-ons surfaced: per-row y-axis latency labels on the
spectrogram (vs the y-range caption), and fully eliminating `treemap.Colormap`
(vs the thin wrapper).

Visual check: the three tabs were captured via the screenshot tour and reviewed.
The spectrogram is correctly sparse on an idle runtime — it fills under
scheduling pressure, not before. The colorscale legend renders below the tour's
display-height limit, so it is confirmed by build + unit test rather than capture.

`status` and `reviewed-date` are deliberately not re-stamped: the M1–M4 panel-set
and observe-only decisions stand; this records implementation detail and one
spawned refactor.

## References

- [ADR-0020](./0020-imzero2-imztop-resource-monitor.md) — `imztop`; the structural template this ADR mirrors.
- [ADR-0019](./0019-observability-sysmetrics-linux-collector.md) — `sysmetrics`; the collector/app split precedent and the Bundle-vs-Sampler pattern.
- [ADR-0026](./0026-app-runtime-and-capability-subjects.md) — keelson app runtime, `Manifest.Id`-equals-import-path, capability subjects (the observe-only posture's reference point).
- [ADR-0057](./0057-demo-registry-and-drivers.md) — registry + Interactive/Test drivers (registration + screenshot tour).
- [ADR-0058](./0058-imzero2-scrolling-texture-widget.md) — scrolling-texture / `heatmapscroll`, the spectrogram substrate.
- [ADR-0050](./0050-clickhouse-observability-pipeline.md) — facts-table / ClickHouse path for the future closed-loop persistence.
- [`runtime/metrics`](https://pkg.go.dev/runtime/metrics) — stdlib data source; `metrics.All` / `metrics.Read` / `Float64Histogram`.
- [`statsviz`](https://github.com/arl/statsviz) — prior-art browser dashboard over the same data source; the genre reference.
