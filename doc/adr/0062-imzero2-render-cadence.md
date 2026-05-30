---
type: adr
status: accepted
date: 2026-05-30
reviewed-by: "@spx"
reviewed-date: 2026-05-30
---

> **Status: accepted 2026-05-30 by @spx.** Continuous default with reactive opt-in via `IMZERO2_RENDER_CADENCE`, the warmup-then-heartbeat reactive path, and the real-work slow-frame gate are implemented on both sides. The hidden-throttle option is deferred (SD6).

# ADR-0062: ImZero2 — Idle Render Cadence and the Slow-Frame Signal

## Context

ImZero2 is a Go-driven, Rust-rendered UI over [egui](https://www.egui.rs/). Each frame the Go side emits an FFFI2 opcode stream; the Rust client's `logic()` interprets it, paints, and presents; the Go frame loop then blocks in `Sync` reading the reply. The two processes run lock-step: Go produces one frame per Rust pass, and with `vsync` on the pass rate is gated by the compositor's frame-callback delivery.

Historically both sides requested a repaint every pass ("continuous-rendering mode"), so the client painted at the vsync rate whenever it had the display. The per-frame [`metrics`](../../public/thestack/imzero2/metrics/metrics.go) package splits each frame's wall-clock into `render` (Go widget build), `sync` (Go wait on the reply), and `interpret` (Rust compute), and emitted a structured "slow frame" warning when the **total** wall-clock crossed `SlowFrameThresholdNs` (25 ms, 1.5× the 16.6 ms 60 Hz budget).

The decision was triggered by a warning flood observed under [`imztop`](0020-imzero2-imztop-resource-monitor.md) on a Wayland session (COSMIC compositor): a steady ~1 line/second, every frame reporting `total_us ≈ 1_000_000` while `render_us ≈ 4_000` and `interpret_us ≈ 6_000`. A pprof investigation found the Go process ~3 % CPU, GC negligible (`GCCPUFraction ≈ 5e-4`, sub-millisecond pauses), and ~81 % of wall-clock blocked reading the FFFI2 pipe; both processes were idle on average. The cause was external: when a window is occluded, the compositor throttles its frame-callback delivery to ~1 Hz, and with `vsync` on the lock-stepped Go loop inherits that ~1 s wait as `sync_us`. No real work overran — the warning was reporting display-pacing latency, not compute.

This surfaced two separable questions:

1. **Is the slow-frame warning measuring the right thing?** It gated on total wall-clock, which includes the vsync/compositor wait the application cannot act on.
2. **What should the idle render cadence be, and how configurable?** Continuous rendering paints at vsync rate even when a visible window is idle, spending CPU/GPU re-emitting unchanged frames.

The two are coupled: the cadence default is only comfortable once the warning stops firing on display-wait.

## Design space (QOC)

**Question.** What is the default idle render cadence, how is any throttle exposed, and how does the slow-frame warning decide a frame is slow — given the Go↔Rust lock-step and the vsync/compositor coupling?

**Options.**

- **O1 — Continuous always (status quo).** Request a repaint every pass on both sides; never throttle. Keep the total-wall-clock warning.
- **O2 — Reactive always.** Both sides drop to an idle heartbeat (plus egui's input/animation repaints) with no configuration; reactive becomes the contract.
- **O3 — Continuous default, reactive opt-in, warning gated on real work _(chosen)_.** Keep continuous as the default; expose reactive through one variable read by both sides; change the warning to gate on `render + interpret` rather than total.
- **O4 — Continuous default, visibility-aware throttle.** Full rate when visible; throttle only when the window reports occluded (egui 0.34 surfaces `ViewportInfo.occluded`).

**Criteria.**

- **C1 — Responsiveness when visible**, including a visible-but-idle window an operator is watching.
- **C2 — Idle CPU/GPU cost.**
- **C3 — Correctness of the slow-frame signal** — fires on real overruns, stays quiet on display-wait.
- **C4 — Implementation and configuration cost**, including Go↔Rust coordination across the lock-step.
- **C5 — Reversibility.**

**Assessment.** `++` strong positive, `+` positive, `−` negative, `−−` strong negative.

|    | O1 | O2 | O3 | O4 |
|----|----|----|----|----|
| C1 | ++ | −  | ++ | ++ |
| C2 | −− | ++ | +  | +  |
| C3 | −  | −  | ++ | −  |
| C4 | ++ | +  | +  | −  |
| C5 | ++ | −  | ++ | +  |

O1 leaves C3 unaddressed: the flood remains regardless of cadence, because it is a property of the warning, not the render rate. O2 fixes idle cost but regresses C1 (a visible idle monitor throttles) and changes the default contract. O4's hidden-throttle is largely redundant under `vsync` — the compositor already throttles occluded windows to ~1 Hz, which is the phenomenon that started this — so it adds occlusion-signal plumbing (C4) for little gain over O3 and still needs the C3 fix. O3 addresses C3 directly (the part that actually removed the flood), keeps the responsive default (C1), and offers the idle-cost win (C2) as opt-in.

## Decision

1. **Default render cadence is `continuous`.** Reactive is opt-in via the environment variable `IMZERO2_RENDER_CADENCE` (`continuous` | `reactive`), registered in [`imzero2env`](../../public/thestack/imzero2/imzero2env/imzero2env.go) and read by both the Go decorator and the Rust client.
2. **The slow-frame warning gates on real work, not wall-clock.** It fires when `render_us + interpret_us` crosses `SlowFrameThresholdNs`; the full breakdown (`total_us` included) is still logged so the excluded sync wait stays visible during triage. See [`metrics.shouldWarnSlowFrame`](../../public/thestack/imzero2/metrics/metrics.go).
3. **Reactive mode warms up before throttling.** The Rust client renders the first `WARMUP_PASSES` passes immediately so the Wayland/`vsync` `swap_buffers` startup handshake settles, then requests only an idle heartbeat; egui's own input/animation repaints override the heartbeat, so interaction stays at vsync rate.
4. **No dedicated "throttle when hidden" option.** Under `vsync` the compositor already throttles occluded windows; an explicit option is deferred (SD6).

### Subsidiary design decisions

- **SD1 — One variable, read independently on both sides.** `IMZERO2_RENDER_CADENCE` is the single source of truth. The Go decorator reads it through the [`env`](../../public/config/env) registry ([ADR-0009](0009-environment-variable-registry.md)); the Rust client reads the same variable with `std::env::var` in `App::new` ([`app.rs`](../../rust/imzero2/src/imzero2/app.rs)), inheriting it as a child process of the Go host. No CLI-flag plumbing is added — the Rust client already reads `IMZERO2_*` variables directly (e.g. `IMZERO2_SCREENSHOT_DIR`, `IMZERO2_IDS_FONTS`), so this follows an existing pattern on each side.

- **SD2 — Both sides must agree on cadence.** egui repaints at the soonest requested deadline, so an immediate request on either side overrides the other's heartbeat. The shared variable keeps them consistent; a mismatch silently reverts to continuous. This is why cadence is not decided independently per side.

- **SD3 — The warning keys on `render + interpret` because those are the two slots the application can regress.** `render` is Go widget build; `interpret` is Rust compute. `sync` is dominated by the wait for the next vblank and the compositor, which the app does not control. A frame where `render` and `interpret` are small but `sync` is large is, by construction, waiting on the display. The `Snapshot.SlackNs` field already isolated the vsync residual for the overlay; the warning now matches that intent.

- **SD4 — egui owns activity-driven repaints; the heartbeat is only a floor.** In reactive mode the idle interval (`IDLE_REPAINT_INTERVAL`, 1 s) bounds how often a fully idle window refreshes; egui requests sooner repaints for input, animation, and the metrics/status overlay. The observed idle rate is therefore a few fps, not 1 fps — the overlay's own repaint requests sit above the floor. Lowering the floor does not reduce idle below what the overlay asks for.

- **SD5 — Warmup is reactive-only and exists for the startup handshake.** `WARMUP_PASSES` (16, ≈0.25 s at 60 Hz) covers a first-frame stall previously observed on Wayland with `vsync` on, where the Go-side repaint request arrived too late for the initial `swap_buffers` handshake. Continuous mode requests immediately every pass and needs no warmup.

- **SD6 — A "throttle when hidden" option is deferred, not designed out.** egui 0.34 exposes `ViewportInfo.occluded` (eframe sets it from winit's `WindowEvent::Occluded`), so a visibility-aware mode is implementable. It is deferred because under `vsync` the compositor already throttles occluded windows, making the option largely redundant today. Revisit if `vsync`-off / non-blocking present modes become common, or if a deeper-than-compositor throttle (near-pause when hidden) is wanted. Occlusion is known only Rust-side; a visibility-aware mode would make the Rust client the authority and have the Go decorator defer to it.

- **SD7 — Screenshot/tour mode stays continuous.** When `IMZERO2_SCREENSHOT_DIR` is set the decorator requests an immediate repaint every pass regardless of cadence, so capture drivers render every frame ([ADR-0057](0057-demo-registry-and-drivers.md) registry/tour path).

- **SD8 — Interval and warmup are constants, not configured.** `IDLE_REPAINT_INTERVAL` / `idleRepaintIntervalSecs` (1 s) and `WARMUP_PASSES` are compile-time constants on each side, not exposed as configuration until a consumer needs to tune them; the cadence switch is the only user-facing surface.

## Alternatives

- **Make reactive the default (O2).** Rejected: a visible idle window — the common case for a monitor like `imztop` or [`imzrt`](0061-imzero2-imzrt-go-runtime-dashboard.md) — would throttle while an operator is watching it, and it changes the established continuous contract. Reactive is the right behaviour to *offer*, not to impose.

- **A CLI flag instead of an environment variable.** The Rust client takes most launch config as flags (`-vsync`, fonts, window size) plumbed through [`application.Config`](../../public/thestack/imzero2/application/config.go). A flag would be consistent there but would also require threading the value into the Go decorator. The variable is read directly by both sides with no plumbing and matches the Go decorator's existing `imzero2env` reads; the small inconsistency with the Rust client's flag-based config is accepted for that.

- **Per-app cadence.** Cadence could be a per-app property in the registry rather than a process-wide variable. Deferred: no current app needs a different cadence from its host, and a process-wide default is the smaller surface. An app that genuinely needs a different idle rate can request its own repaints (`RequestRepaintAfter`) without a registry change.

- **Visibility-aware throttle (O4).** Covered by SD6 — deferred on the vsync-redundancy argument rather than designed out.

## Consequences

### Positive

- The slow-frame warning stops firing on occluded/idle frames in **both** cadences, because the gate change is independent of render rate; it now names real `render`/`interpret` overruns.
- The default is unchanged from prior behaviour (continuous), so upgrading does not change anyone's cadence implicitly.
- Reactive mode is available for power-sensitive or background use, cutting a visible-but-idle window from vsync rate to a few fps.
- One variable, read the same way each side already reads its environment; no new flag or wire-format surface.

### Negative

- Two independent reads of the same string (`"reactive"` in Go and Rust) are a cross-language contract; a rename in one place silently desyncs the other. Mitigated by a test pinning the Go-side constant values, with a comment pointing at the Rust reader — but not enforced by the compiler.
- Reactive mode's responsiveness depends on egui continuing to request repaints for all interaction and animation; a widget that mutates state without requesting a repaint would not refresh until the heartbeat. This is the standard reactive-egui contract, but a sharper edge than continuous mode.
- The idle frame rate in reactive mode is not the configured interval (SD4); an operator expecting "1 s ⇒ 1 fps" sees a few fps because of overlay repaints.

### Neutral

- No FFFI2 wire-format change; the `RequestRepaint` / `RequestRepaintAfter` opcodes already existed.
- The warmup and interval values are constants; SD8 leaves them un-exposed until a need appears.
- The hidden-throttle option remains a documented, deferred possibility (SD6), not a closed door.

## References

- [ADR-0009](0009-environment-variable-registry.md) — environment-variable registry; `CategorialStringVar` and the default-on-unrecognised-value convention used here.
- [ADR-0020](0020-imzero2-imztop-resource-monitor.md) — `imztop`, where the warning flood surfaced.
- [ADR-0061](0061-imzero2-imzrt-go-runtime-dashboard.md) — `imzrt`, another visible-idle monitor affected by the cadence default.
- [ADR-0057](0057-demo-registry-and-drivers.md) — registry + Interactive/Test (screenshot tour) drivers; the continuous-capture path SD7 preserves.
- [`metrics.go`](../../public/thestack/imzero2/metrics/metrics.go) — frame metrics and `shouldWarnSlowFrame`.
- [`imzero2env.go`](../../public/thestack/imzero2/imzero2env/imzero2env.go) — `RenderCadence` registration.
- [`app.rs`](../../rust/imzero2/src/imzero2/app.rs) — Rust `logic()` cadence and warmup.
- [`imzero2_demo_resolve.go`](../../public/thestack/imzero2/egui2/demo/carousel/imzero2_demo_resolve.go) — Go decorator cadence.
