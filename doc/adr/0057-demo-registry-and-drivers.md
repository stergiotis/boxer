---
type: adr
status: accepted
date: 2026-04-23
reviewed-by: "p@stergiotis"
reviewed-date: 2026-05-31
---

# ADR-0057: Demo registry + split drivers (interactive / test / embed)

## Context

The ImZero2 demo app currently renders as 17 top-level `egui::Window`s inside a single eframe viewport, driven by a hardcoded `[]windowEntry` and a 4-phase screenshot state machine at [`egui2_hl_demo.go:47-126`](../../public/thestack/imzero2/egui2/demo/apps/widgets/egui2_hl_demo.go). One loop body plays both roles: it is what a user interacts with via `bash src/rust/hmi.sh`, and it is what CI captures when `IMZERO2_SCREENSHOT_DIR` is set. Demo-level metadata (default size, resizable, default-open state) is scattered across fluent chains at the call sites rather than carried by a declared type.

Forces that have accumulated:

- **Interactive UX and deterministic capture want opposite things.** Humans want animations, scroll, persisted window positions, responsive resize. CI wants animations snapped to their target values, fresh egui memory, scripted navigation, pixel-stable PNGs. Reconciling both in one code path has produced a growing tax of per-widget mitigations: `.DefaultOpen(true)` to survive the 4-frame tour (see [`SKILLS.md §12`](../skills/imzero2/SKILLS.md) CollapsingHeader pitfall), `frame < 6` warmup guards, `RequestRepaint` calls wedged into the tour tick to keep the loop hot, `c.SetWindowCollapsed` / `c.MoveWindowToTop` scaffolding to manage inter-window z-order.
- **Jitter is multi-source.** 17 persisted window positions restored from egui memory; neighbouring collapsed title bars still contributing to layout; z-order flicker during `MoveWindowToTop`; full-viewport PNG captures bleeding neighbour chrome into the target's screenshot; font-warmup and text-shaping shifts on first paint. Each is small; together they desynchronise pixel-compare baselines.
- **The 4-frame budget is a hard ceiling.** CollapsingHeader open animation, force-directed graph settle, walkers tile-load, spring-based layout all exceed it. Already documented in [`SKILLS.md §14.2`](../skills/imzero2/SKILLS.md) and [ADR-0056](0056-walkers-map-h3-binding.md). The walkers demo specifically had to avoid being wrapped in `CollapsingHeader` because of this.
- **Profiler embedding is a first-class use case.** A profiler or debug shell wants to drop a specific demo (e.g. `graphs`, `treemap2`) into its own layout for live inspection. Today a demo *is* a `Window` — it owns its chrome. An embedding host cannot compose it into a `SidePanel` or `Frame` without ripping it out of its window wrapper.
- **New demos pay a tour tax.** Adding a demo today means editing `demoWindows[]` with a matching id, wiring `SetWindowCollapsed` targets, and auditing any animations for 4-frame-tour hazards. Works, but every new widget increases the probability of a CI regression for an unrelated reason.

Invariants the design must respect:

- **`IMZERO2_SCREENSHOT_DIR` contract.** When set, CI expects one PNG per demo under that directory. Filename-per-demo is the stable external surface.
- **FFFI2 register-drain and deferred-block semantics** ([`SKILLS.md §11`](../skills/imzero2/SKILLS.md)). Any new opcode must cooperate with frame-level culling and the drain protocol.
- **CGO-free Go build.** Unaffected here — this ADR is entirely within Go and Rust sources we already compile.
- **Incremental migration.** 17 demos cannot be rewritten atomically. The old and new paths must coexist during the transition.

## Design space (QOC)

**Question.** How should ImZero2 demos be organised so that interactive exploration and deterministic screenshot capture can each be optimised independently, without either mode's constraints bleeding into the other?

**Options.**

- **O1 — Status quo, iterate.** Keep the closure-driven 17-window tour; add per-widget tour mitigations as new hazards surface (e.g. per-demo `SettleFrames`, more `.DefaultOpen(true)` uses, animation-skip branches in each widget).
- **O2 — Single Dear-ImGui-style driver.** Collapse everything into one window with a `ScrollArea` of `CollapsingHeader`s, one driver serves both interactive and test modes, with per-widget test-mode branches to suppress animations / reset state.
- **O3 — Shared demo registry + split drivers (chosen).** A `Demo` value type with a `Render` closure, declared per-demo, consumed by three independent hosts: `InteractiveDriver` (human shell), `TestDriver` (headless, deterministic capture), `Embed` (single-demo mount into any host Ui).
- **O4 — Separate binaries.** Two entry points: one interactive demo app, one headless test app, each with its own `main.rs` and its own driver, both linking the same demo packages.

**Criteria.**

- **C1 — Screenshot determinism.** Are CI PNGs pixel-stable across runs without tour mitigations bleeding into widget code?
- **C2 — Interactive UX quality.** Can the human path use animations, persisted positions, scroll, resize without compromise?
- **C3 — Embedability.** Can a host (e.g. profiler) drop a specific demo's body into its own Ui scope with one call?
- **C4 — Migration cost.** Effort to port the existing 17 demos and the screenshot tour without breaking CI mid-migration.
- **C5 — Maintenance surface.** What's the per-demo cost of adding a new one? Does the tour stay hands-off?
- **C6 — Test-mode complexity.** Can the capture pipeline be reasoned about end-to-end without tracing through animation + persisted-state + z-order logic?

**Assessment.** `++` strong positive, `+` positive, `−` negative, `−−` strong negative.

|    | O1 | O2 | O3 | O4 |
|----|----|----|----|----|
| C1 | −  | −  | ++ | ++ |
| C2 | +  | +  | ++ | ++ |
| C3 | −− | +  | ++ | +  |
| C4 | ++ | −  | +  | −− |
| C5 | −  | +  | ++ | +  |
| C6 | −  | −  | ++ | ++ |

O3 dominates O2 on every axis except migration cost, where O2 would rewrite the shell in one pass. O3 also dominates O4 — the binary split gives the same decoupling but doubles build/launch surface and duplicates shared plumbing (font loading, viewport config, panic handlers). O1 is the current trajectory; each new demo adds tour-specific code and each new animation adds a `.DefaultOpen`-style mitigation. The trend is unsustainable.

## Decision

Introduce a shared demo registry and three independent hosts that consume it.

**The registry.** A new Go package `src/go/public/thestack/imzero2/egui2/demo/apps/registry/` exports a `Demo` value type and a `Register(Demo)` function. Each existing demo file adds an `init()` that registers its `Demo` entry. The registry is the single source of truth for the demo list, title, category, and render body.

```go
type Demo struct {
    Name     string           // stable id, used as PNG filename and Embed key
    Category string           // grouping for the interactive shell
    Title    string           // pretty title (with nerdfont icon)
    Stage    [2]float32       // canonical capture size; 0,0 means driver default
    Flags    DemoFlagsE       // NeedsLargeArea | SkipInTour | NeedsNetwork | …
    Render   func(ids *c.WidgetIdStack)  // draws into the current Ui scope; no Window chrome
}
```

**The three hosts.**

1. **InteractiveDriver** — what `hmi.sh` runs when `IMZERO2_SCREENSHOT_DIR` is unset. One window (`"ImZero2 Demo"`), top filter bar, `ScrollArea`, one `CollapsingHeader` per demo grouped by `Category`. Animations on, egui memory persists, resizable.
2. **TestDriver** — what `hmi.sh` runs when `IMZERO2_SCREENSHOT_DIR` *is* set. One demo per frame, rendered into a fixed stage `Frame` at a known rect on a neutral background, no `CollapsingHeader`, no `ScrollArea`, no outer `Window`. Animation-freeze on. egui memory cleared at start. Quiescence detector replaces the fixed 4-phase state machine.
3. **Embed** — public API `demos.Embed(ids *c.WidgetIdStack, name string)`. Renders a single demo's body into the current Ui scope. No chrome. The host owns layout.

**Two new interpreter primitives** (see SD1, SD2 below): `RequestScreenshotRect(path, rect)` and an animation-freeze mode flag.

### Subsidiary design decisions

- **SD1 — `RequestScreenshotRect(path, rect)` opcode, Rust-side crop.** New Go-side helper alongside the existing `RequestScreenshot(path)`; new Rust-side opcode that stores the rect in `UserData` alongside the path and crops the `ColorImage` before PNG encode in `handle_screenshot_event` ([`interpreter.rs:1734`](../../rust/imzero2/src/imzero2/interpreter.rs)). Rust-side crop keeps PNG output tight and avoids a readback-crop-reencode round trip. The existing full-viewport `RequestScreenshot` stays; rect is additive.
- **SD2 — Animation-freeze mode is a single Rust-side flag, not a per-widget opt-in.** A bool on the interpreter (`animation_freeze: bool`) that, when set, makes `ctx.animate_*` short-circuit to the target value. TestDriver flips it on at startup via a new `SetAnimationFreeze(bool)` opcode; InteractiveDriver never touches it. Collapses `CollapsingHeader` open-animation to one frame; eliminates spring-settle, fade-in, and other time-dependent transitions at the source. New animated widgets inherit the behaviour without per-widget changes.
- **SD3 — TestDriver activation is purely env-driven.** `IMZERO2_SCREENSHOT_DIR` set → TestDriver runs and the interactive shell is not constructed at all; unset → InteractiveDriver. No CLI flag, no build tag. Matches the existing CI invocation contract and preserves the per-demo filename convention.
- **SD4 — `Demo.Render` draws into the current Ui scope; the driver owns chrome.** Demos do not call `c.Window(...)` or `c.Frame(...)` at the outer level. InteractiveDriver wraps each demo in a `CollapsingHeader`; TestDriver wraps in a fixed `Frame`; Embed wraps in nothing. A demo that genuinely needs its own internal layout (e.g. a fixed-height viewport for the walkers map) creates that layout *inside* its Render, unaware of the outer host. This is the load-bearing invariant that makes Embed trivial.
- **SD5 — TestDriver uses a fixed viewport size and a quiescence detector.** The eframe inner size is locked at a canonical 1280×720 at startup (configurable via `IMZERO2_SCREENSHOT_SIZE=WxH`). Per demo, the driver renders inside a centered stage `Frame` sized by `Demo.Stage` (defaulting to 1024×600). The 4-phase state machine is replaced by: `(a)` render demo N in isolation, `(b)` compare current frame pixel hash with previous, `(c)` when two consecutive frames match (or a `SettleFrames` cap of ~64 is hit), issue `RequestScreenshotRect` for the stage rect, `(d)` advance. No collapse/uncollapse, no move-to-top, no `frame < 6` warmup — each demo starts from a clean egui memory snapshot.
- **SD6 — `Demo` registration is via per-file `init()`.** Each existing demo file gains a file-level `init()` that calls `registry.Register(Demo{...})`. The registry package maintains a sorted slice; enforces name-uniqueness; supports iteration (for drivers) and lookup-by-name (for Embed). Package-level side effects are acceptable here — the same pattern is used for ClickHouse nanopass registrations and for widget-id seed allocation.
- **SD7 — Demos declare their settle budget; InteractiveDriver ignores it.** `Demo.Flags` carries `NeedsLargeArea` (TestDriver picks a larger stage), `SkipInTour` (TestDriver skips, InteractiveDriver includes), `NeedsNetwork` (TestDriver skips unless `IMZERO2_ALLOW_NETWORK=1` — e.g. walkers tile fetches). The quiescence detector obsoletes most per-demo `SettleFrames` values; the field is retained as a cap to prevent infinite waits on pathologically non-converging animations even with animation-freeze on.
- **SD8 — Embed API is minimal.** `demos.Embed(ids, "graphs")` and that's it. No layout hints, no size override, no filter. The host wraps it in whatever chrome it needs. Looking up a non-existent name panics in debug, logs+noops in release — matches the ergonomics of other registry lookups in the codebase.
- **SD9 — Migration is incremental, both paths live in parallel.** The existing `RenderLoopHandlerDemo2()` stays. New entry points `InteractiveDriver.Run()` and `TestDriver.Run()` are added and dispatched from `main.go` based on `IMZERO2_SCREENSHOT_DIR`. Demos port one at a time from their current `c.Window(...) { ... }` body into a `Render(ids)` function + `init()` registration. During migration the old tour captures old-shell PNGs; the new TestDriver captures new-shell PNGs into a subdirectory (`$IMZERO2_SCREENSHOT_DIR/new/`) so both are available for compare. Migration completes when all 17 demos are ported; the old tour and `demoWindows[]` are deleted in a single cleanup commit.
- **SD10 — Interactive shell layout is deliberately underspecified.** First cut is Dear-ImGui-style: one window, one filter text input, one `ScrollArea`, `CollapsingHeader` per demo grouped by `Category`. Alternatives (SidePanel+CentralPanel, DockArea tabs, launcher grid) can replace it later without an ADR revision — they are all consumers of the same registry. The only hard constraint on the interactive shell is that it must call `demos.Embed(ids, name)` internally rather than duplicating the render path, so that interactive and Embed cannot drift.
- **SD11 — TestDriver screenshots capture demos in isolation, not catalog context.** Explicit design choice. The catalog view (interactive shell with all demos collapsed) is not a TestDriver artefact; if catalog documentation screenshots are wanted later, they are produced by a separate `CatalogDriver` that walks the InteractiveDriver shell programmatically. TestDriver PNGs are for visual regression of the widget itself. Naming this avoids the implicit assumption that a tour PNG represents "what the user sees" — it represents "what this widget renders given a clean slate".

## Alternatives

- **O1 — Status quo, iterate.** Keeps migration cost at zero but doubles down on the trend that produced the current pain. Every new animated widget adds a tour mitigation; every new demo adds a `demoWindows[]` entry plus an id-collision audit. The `frame < 6` warmup and the 4-phase state machine are load-bearing guesses, not derived from demo properties. Rejected because the marginal cost of each future demo is not bounded.
- **O2 — Single Dear-ImGui-style driver.** Would cleanly remove the 17-window problem but does not remove the two-modes-one-code-path problem. Every animation-carrying widget would still need a tour-mode branch, just routed through `ctx.memory().add_screenshot_mode` or similar. Same tax, new shape. Rejected because it gets the window reduction without the decoupling.
- **O4 — Separate binaries.** Maximally decoupled but pays twice for shared plumbing (font loading, panic handlers, viewport config, dev-loop reload scaffolding) and forces two `hmi.sh` variants. The O3 split is already as strong because the two drivers share nothing at runtime beyond the registry — there is no runtime coupling to break. Rejected as overkill.
- **Region-capture only, keep 17 windows.** A narrow scope that would fix screenshot determinism without touching the demo structure. Loses the embedability requirement (C3) and leaves the per-window jitter as a standing liability. Rejected because the embedability use case is the primary driver for the restructure, not a bonus.

## Consequences

### Positive

- **Zero per-widget tour branches.** Animation freeze is a single interpreter-level flag; no widget needs to know it exists. Any new widget that uses `ctx.animate_*` is automatically tour-safe.
- **New demos are cheap.** One `Demo{}` struct literal + one `Render` closure + one `init()`. No tour bookkeeping, no id collision audits, no `demoWindows[]` edits.
- **Embedding is first-class.** A profiler can call `demos.Embed(ids, "graphs")` inside any `Frame` / `Window` / `Panel` without demo modifications. Same for debug shells, design-review tools, screenshot-generation tooling for docs.
- **TestDriver is simpler end-to-end.** No z-order management, no collapse/uncollapse, no fixed phase count, no cross-window settle interactions. The pipeline is: clear memory → render demo N → wait for pixel quiescence → capture rect → advance. Failures have narrow causes.
- **TestDriver PNGs become a stable pixel-compare baseline.** Combined with fixed viewport, fixed stage rect, cleared egui memory, and animation freeze, run-N and run-N+1 produce byte-identical PNGs (modulo font rasterization changes across OS updates, which are uniform across demos).
- **Interactive UX decoupled from CI constraints.** The human shell can adopt whatever layout works best (Dear-ImGui-style first, possibly tabs or docked later) without CI risk.

### Negative

- **Two tours to reason about.** InteractiveDriver and TestDriver are independent; a bug in one does not manifest in the other. The upside is that TestDriver is much simpler than the current tour, so net cognitive load drops; the downside is that "it works in CI, fails for a user" (or vice versa) is a new class of bug.
- **Animation-freeze is new interpreter surface.** `interpreter.rs` gains a flag that participates in the animation-progress path. Must stay synchronized with egui's animate semantics across upstream version bumps. Contained (one place); not zero.
- **TestDriver PNGs no longer show catalog context.** A PNG shows the widget in isolation, not as the user would see it surrounded by nav / filter / neighbouring collapsed headers. For visual-regression this is correct; for documentation artefacts wanting "catalog screenshots", a separate driver is needed. Deferred.
- **Migration transient.** During migration the old tour and the new TestDriver coexist; the CI PNG directory has a `new/` subdirectory until cutover. Short-lived but nonzero.
- **Package-level `init()` side effects.** Registration runs on package import. A test that imports a demo package inherits its registration — unlikely to matter in practice but worth naming. The registry de-duplicates on name so double-import is not fatal.

### Neutral

- **Env-var dispatch (`IMZERO2_SCREENSHOT_DIR`) kept.** Existing CI invocation is unchanged. Adds `IMZERO2_SCREENSHOT_SIZE` and `IMZERO2_ALLOW_NETWORK` as optional overrides.
- **Existing `c.RequestScreenshot(path)` stays.** `RequestScreenshotRect(path, rect)` is additive. Demos or tooling that need full-viewport capture continue to work.
- **Demo render bodies typically shrink.** They lose their outer `c.Window(...) { ... }` wrapper; everything inside the wrapper moves unchanged into the `Render(ids)` function. Sub-demos that were already `CollapsingHeader`-structured (tables, plots, graphs) stay that way — those wrappers are part of the demo's own content, not chrome.
- **Font/warmup stability.** The `frame < 6` warmup becomes an explicit `TestDriver` startup phase that runs once before the first demo render, independent of the quiescence loop. Cleaner than threading it through a per-demo counter.

### Derived practices

- **New animated widgets need no tour-aware code.** They use `ctx.animate_*` as normal. The interpreter-level freeze handles capture-mode behaviour. If a widget *must* have custom tour-mode behaviour, it reads `c.IsScreenshotMode()` (to be added) explicitly — this should be rare.
- **Demos that need large screen area declare `Stage`.** TestDriver uses the stage for the crop rect; InteractiveDriver uses it as a sizing hint for the containing `CollapsingHeader` body. No demo expands the stage dynamically at render time.
- **Interactive shell treats the registry as immutable.** It does not mutate `Demo` fields, does not keep its own copy, does not add synthetic demos. Anything that should be a demo is registered; anything that should not be captured in CI sets `SkipInTour`.
- **Embed is the interactive shell's internal API too.** The interactive shell's `CollapsingHeader` body calls `demos.Embed(ids, demo.Name)` rather than `demo.Render(ids)` directly. Ensures the Embed path is always exercised.
- **CI PNG reference set rebuilds on schema changes only.** Pixel-stable PNGs enable hash-based regression tests; a diff means either the demo changed intentionally or a dependency (egui, walkers, font) shifted. Baseline PNGs are checked in under `doc/screenshots/` and updated in the same commit that causes the diff.

## Status

Accepted — 2026-05-31. Implemented across the registry, TestDriver, and gallery; see the 2026-05-31 Updates entry.

Status lifecycle: `Proposed → Accepted → (Deprecated | Superseded by ADR-XXXX)`.
ADRs are append-only; supersession is recorded, not deleted.

## Updates

### 2026-05-31 — Implemented; the `Demo` type grew a stateful path, and six keelson tour apps joined the registry

The registry, the `TestDriver`, and the interactive gallery (which consumes the registry through `Embed`) are built and in use. Three places where the implementation departed from or extended the decision above:

- **`Demo` gained a stateful path beyond SD4's `Render`-only shape.** Alongside `Render(ids)`, the type now carries `Init(ids) (state any)` + `RenderStateful(ids, state)` (plus a bus-aware `BusInit` variant). Stateless demos keep `Render`; demos that hold widget singletons (filepicker, treemap, and the migrated apps below) set `Init`/`RenderStateful` so those singletons bind to the host's per-window `WidgetIdStack` rather than a process-shared stack — otherwise two open gallery windows collide on widget ids. The struct also grew `Kind` (UX/DX/Mixed), `Description`, and `SourceFunc`/`SourceFile` (the per-demo "view source" link). `Register` enforces exactly one of `Render` or `(Init|BusInit)+RenderStateful`.
- **A `DemoFlagNonDeterministic` flag was added** beyond SD7's `NeedsLargeArea`/`SkipInTour`/`NeedsNetwork`. It marks demos whose pixels drift across runs (live runtime/system metrics, synthetic-corpus scans); the TestDriver skips them under `IMZERO2_SCREENSHOT_DETERMINISTIC=1`, so the default run still shows them while a CI lane can stay byte-stable.
- **Scope extended past the original 17 widgets demos.** Six apps built on the keelson app runtime — `configview`, `leewaywidgets`, `regex_explorer`, `imzrt`, `imztop`, `splashscreen` — each previously carried its own per-app `RenderLoopHandlerTour` (a settle→capture→advance state machine) wired through a screenshot-mode `SeededFuncApp` factory. Those per-app tours were deleted and replaced with `registry.Register(Demo{})`; the central TestDriver now captures all of them in one `--launch widgets` run, and they surface in the gallery via `Embed`. The interactive shell left underspecified in SD10 landed as a "Widget gallery" `AppI` hosted by the keelson window host (ADR-0026) rather than a standalone shell; demos are reached only through it (the earlier dual-registration into `app.DefaultRegistry` was removed — ADR-0026 M3 C3, 2026-05-12).

## References

- [`src/go/public/thestack/imzero2/egui2/demo/apps/widgets/egui2_hl_demo.go`](../../public/thestack/imzero2/egui2/demo/apps/widgets/egui2_hl_demo.go) — current closure-driven 17-window tour, the source of the problem.
- [`src/rust/src/imzero2/interpreter.rs`](../../rust/imzero2/src/imzero2/interpreter.rs) — `handle_screenshot_event` at line 1734; site of the `RequestScreenshotRect` crop logic (SD1) and animation-freeze flag (SD2).
- [`doc/skills/imzero2/SKILLS.md`](../skills/imzero2/SKILLS.md) §14 (screenshot infrastructure) and §12 (CollapsingHeader 4-frame tour pitfall) — existing documented hazards this ADR supersedes with structural fixes.
- [ADR-0056](0056-walkers-map-h3-binding.md) — establishes the "4-frame tour is a constraint on widget design" precedent that SD2 (animation freeze) removes.
- [ADR-0052](0052-imzero2-unified-color-type.md) — prior ImZero2 binding ADR; template shape followed here.
- [Dear ImGui `imgui_demo.cpp`](https://github.com/ocornut/imgui/blob/master/imgui_demo.cpp) — UX reference for the interactive shell (SD10); one-window catalog with CollapsingHeader-per-topic + top-of-window filter.
- [`src/go/public/thestack/imzero2/egui2/bindings/egui2_methods.go`](../../public/thestack/imzero2/egui2/bindings/egui2_methods.go) — `DockArea` component (line 127), candidate interactive-shell layout for a future iteration (SD10 alternative).
- [`src/rust/hmi.sh`](../../rust/imzero2/hmi.sh) — launch script; unchanged by this ADR, dispatches to whichever driver based on `IMZERO2_SCREENSHOT_DIR`.
