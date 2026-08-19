---
type: adr
status: accepted
date: 2026-05-30
reviewed-by: "p@stergiotis"
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

## Status

Accepted 2026-05-30 by @spx. Continuous default with reactive opt-in via `IMZERO2_RENDER_CADENCE`, the warmup-then-heartbeat reactive path, and the real-work slow-frame gate are implemented on both sides; the hidden-throttle option is deferred (SD6).

Status lifecycle: `Proposed → Accepted → (Deferred | Deprecated | Superseded by ADR-XXXX)`. See boxer's `DOCUMENTATION_STANDARD.md` §1 ADR for the edit-policy tiers (Tier 1 in-place / Tier 2 `## Updates` H3 / Tier 3 superseding ADR).

## Updates

### 2026-07-22 — Frame-pacing measurements: single-process control, governor and vsync A/B

A follow-up investigation into reports of uneven ("flaky") scrolling measured the pacing directly — the [`metrics`](../../public/thestack/imzero2/metrics/metrics.go) overlay's fps distribution plus a `WAYLAND_DEBUG` `wl_surface.commit`-cadence capture. The results refine the Context's analysis without changing the decision:

- **Compute and GC are not the jitter** (as the Context already found): steady-state `render + interpret` is ~1 ms and stable; GC runs every ~2 s with sub-millisecond stop-the-world pauses. The jitter lives in the `sync`/present-wait.
- **The steady "60→30" doubling is a compositor/toolkit floor, not the Go↔Rust lock-step.** A single-process `eframe`/`wgpu` control app (continuous repaint, `AutoVsync`, same viewport) on the same COSMIC session showed the *same* doubling rate — the second process and the FFFI2 pipe add nothing measurable to it. The lock-step still makes `sync` inherit display-wait (SD3), but it is not the source of the missed-refresh beat.
- **CPU governor / EPP moves the tail, not the beat.** `powersave` (`amd-pstate-epp`) widens the worst-case tail via wake-up latency on the mostly-idle loop; `performance` tightens it (≈−30 % pacing std in one A/B) but leaves the steady doubling untouched.
- **`vsync` induces the beat.** With `-vsync on` a present that lands just past the refresh deadline slips a whole refresh (60→30); `-vsync off` (the compositor composites, so no tearing) markedly reduces the doubling. This is the one app-side lever, and it reinforces the SD6 argument that a hidden-throttle option is largely redundant under `vsync`.

No decision change: the continuous default and the real-work slow-frame gate stand. The practical upshot — under continuous + `vsync` a compositor pacing floor remains, addressable only by `-vsync off` or the compositor — is captured as a how-to: [triage janky rendering](../howto/imzero2-render-troubleshooting.md).

### 2026-08-18 — SD6 reopened: the occluded window spins a core

SD6 deferred a throttle-when-hidden option on the premise that "under `vsync` the compositor already throttles occluded windows, making the option largely redundant today"; the 2026-07-22 entry above reinforced that reading. A measurement of an occluded window shows the premise holds for *presentation* and not for *CPU*, so SD6 is reopened.

Measured over a clean 10 s window with no profiler client attached, demo carousel hosting `play`, COSMIC/Wayland, `-vsync on`, cadence unset (so `continuous`):

| | |
| --- | --- |
| Go host | 6.9 % of a core — render goroutine in `[IO wait]` |
| Rust client | **96.8 % of a core**, of which 96.6 % is the main thread |
| client frame rate | 1.0 fps, each frame ~20 ms (p50 19.7, p99 28.3) |
| GPU busy | 5 % |

Frame rate and per-frame cost were read from the client's own `puffin` frame metadata on the loopback profiler port, so they are the client's numbers, not an inference from the Go side.

One frame per second at ~20 ms is a ~2 % duty cycle, so ~95 % of a core is spin, not work. The Context's original investigation of this same scenario recorded the opposite — "both processes were idle on average", "~81 % of wall-clock blocked reading the FFFI2 pipe" — and that half is still true of the Go side only. Stacks put the client in `polling::epoll::Poller::wait_deadline` ← `calloop` ← winit's Wayland `loop_dispatch` ← `eframe::run_and_return`, while `/proc/<pid>/syscall` reports `running`: the wait is returning on an already-past deadline and looping. The mechanism is decision 1 meeting the compositor — `logic()`'s unconditional `ctx.request_repaint()` keeps asking for an immediate repaint, the compositor delivers frame callbacks at ~1 Hz, and the loop spins full-tilt in between.

This does not change the continuous default, and the ~1 fps itself remains expected and documented ([triage janky rendering](../howto/imzero2-render-troubleshooting.md)). What changes is SD6's cost basis: the compositor throttles what is *presented*, not what the client *burns*, so a hidden-throttle option is not redundant — it is worth roughly one core on any backgrounded window. SD6 already names the mechanism (`ViewportInfo.occluded`, set by eframe from winit's `WindowEvent::Occluded`) and the ownership question (occlusion is known only Rust-side, so the client would become the authority and the Go decorator defer to it); both stand. `IMZERO2_RENDER_CADENCE=reactive` avoids the spin today, but it is not the answer SD6 wants — it also throttles a *visible*-but-idle window, which decision 1 and the O2 rejection deliberately declined to impose.

Open, not decided here: whether the fix is occlusion-aware repaint scheduling in `app.rs`, or whether the spin is better read as a winit/`calloop` deadline bug worth reproducing against the single-process control app the 2026-07-22 entry already built.

### 2026-08-15 — `interpret_us` is now net of stream-wait; the two gated slots were not disjoint

A re-investigation of recurring slow-frame warnings — prompted by the suspicion that the ~1 Hz occlusion throttle from the Context was still the cause — confirmed the gate holds against that, and found a separate defect in what it sums.

**The throttle is not the cause.** Reproducing the Context's scenario with the compositor pinned to 1 Hz gives `total_us ≈ 999_800`, `render_us ≈ 800`, `interpret_us ≈ 1_600` — and fires on 0 of 21 frames. Slowing an identical workload (the same 184 tour captures) from 60 Hz to 10 Hz moved `sync_us` 16.5 ms → 99.5 ms, a clean 6×, while the warning count went 110 → 108. Display-wait stays in `sync`, as SD3 intended.

**The two gated slots overlapped.** Go streams a frame opcode by opcode — every `SendSingleUseMsg` flushes — and `interpret_commands_outer` stamped its `t0` before a read loop whose `begin_consume_message` blocks on the pipe. Because the lock-step releases Go only once the pass is *already inside* that loop, Go's widget build ran while the Rust span was open, so the span covered it: `interpret_us > render_us` in 1140 of 1141 frames, correlation +0.887, with a near-constant ~565 µs offset that is Rust's actual dispatch. Summing the two slots therefore charged `render` twice, and the 25 ms budget tripped at ~12 ms of Go render. SD3's reasoning was right; its arithmetic assumed a disjointness the wire protocol did not provide. The `{13 ms, 13 ms → fires}` case in the gate's own test is this scenario.

**Change.** The interpreter accumulates the time each pass spends blocked on the message-length word (`read_blocked_ns`, [`interpreter.rs`](../../rust/imzero2/src/imzero2/interpreter.rs)) and subtracts it from the span, so `last_interpret_us` reports dispatch rather than a second copy of `render_us`. Measured on the same scenes: median `interpret_us` 1121 → 620 µs on the demo carousel, and 32.1 → 9.1 ms on the tour's >256 KiB frames, with `render_us` unchanged and whole-run wall clock up 0.4 % from the added clock reads.

**Residual.** A message large enough for Go's writer to split still parks the pass mid-message, and that wait is left in — bulk transfer rather than idling. The subtraction is also a cross-language invariant with no compile-time link to the Go-side sum, the same fragility SD1/SD2 already carry; it is noted at [`shouldWarnSlowFrame`](../../public/thestack/imzero2/metrics/metrics.go).

No decision change: the continuous default, reactive opt-in, and the real-work gate all stand. This restores the budget SD3 intended.

### 2026-08-19 — Measured: the spin costs a core and buys no frames; the root cause is upstream

The 2026-08-18 entry reopened SD6 on a live occluded window. An A/B on a controlled compositor now bounds the cost and locates the defect, which is **not** in this tree.

Method: headless weston at a forced frame-callback rate (`weston --backend=headless --renderer=gl --idle-time=0 --refresh-rate=<mHz> --socket=wl-probe`, host run under `WAYLAND_DISPLAY=wl-probe`), the probe the 2026-07-22 entry above established. `--refresh-rate=1000` (1 Hz) reproduces the occluded-window throttle without occluding anything: it produced 99.7 % of a core against the 96.8 % measured on the real occluded window. Frame rate is counted from `wl_surface.commit` in a `WAYLAND_DEBUG=1` capture.

| Compositor | Cadence | fps | Client CPU |
| --- | --- | --- | --- |
| 1 Hz (occluded-equivalent) | continuous | ~1 | **99.7 %** |
| 1 Hz (occluded-equivalent) | reactive | ~1 | **1.8 %** |
| 60 Hz (visible) | continuous | ~60 | 98.5 % |
| 60 Hz (visible) | reactive | ~1 | 0.1 % |

Two readings. **Under throttling both cadences deliver the same ~1 fps, so the spin buys nothing** — continuous burns a whole core for zero additional frames, and reactive removes it 55×. **But reactive still cannot be the default**, because on a *visible* window it drops 60 fps to ~1: exactly the O2 rejection in decision 1. `IMZERO2_RENDER_CADENCE=reactive` is therefore a real workaround for a backgrounded dev window and not a fix.

The mechanism is in eframe, not here. `about_to_wait` sets `ControlFlow::Poll` whenever a repaint is due and `is_invisible_or_minimized(window)` is false; the compositor withholds frame callbacks from a throttled surface, so `RedrawRequested` never arrives, the repaint stays due, and the loop re-Polls. eframe already carries a backstop for this exact busy-loop (`INVISIBLE_WINDOW_REPAINT_INTERVAL`, egui #7776) but keys it on `Window::is_visible()` / `is_minimized()`, which winit's Wayland backend returns `None` for unconditionally. The same gap disposes of SD6's stated mechanism: winit emits `WindowEvent::Occluded` on X11, macOS, iOS and web but **never on Wayland**, and eframe fills `ViewportInfo.occluded` only from that event — so a visibility-aware mode as SD6 describes it is inert on the platform where this was measured.

A client-side fix is also blocked by SD2 as written: [`host/chrome.go`](../../public/thestack/imzero2/host/chrome.go) requests an immediate repaint every frame in continuous mode, and egui's `request_repaint_after` takes `min(existing, d)` — the Rust side cannot lengthen a deadline Go already set to zero. Making the client authoritative would need a Rust→Go signal and a decorator change, and the `RequestRepaint` dispatch arm is generated code.

No decision change. Continuous stays the default and SD6 stays open; the preferred fix is now upstream — re-key eframe's existing backstop from "the window is invisible" to "a redraw has been requested but unserviced for > N ms", which is observable on every platform and costs nothing here, since the frames it suppresses are frames the compositor was never going to deliver.

## References

- [ADR-0009](0009-environment-variable-registry.md) — environment-variable registry; `CategorialStringVar` and the default-on-unrecognised-value convention used here.
- [ADR-0020](0020-imzero2-imztop-resource-monitor.md) — `imztop`, where the warning flood surfaced.
- [ADR-0061](0061-imzero2-imzrt-go-runtime-dashboard.md) — `imzrt`, another visible-idle monitor affected by the cadence default.
- [ADR-0057](0057-demo-registry-and-drivers.md) — registry + Interactive/Test (screenshot tour) drivers; the continuous-capture path SD7 preserves.
- [`metrics.go`](../../public/thestack/imzero2/metrics/metrics.go) — frame metrics and `shouldWarnSlowFrame`.
- [`imzero2env.go`](../../public/thestack/imzero2/imzero2env/imzero2env.go) — `RenderCadence` registration.
- [`app.rs`](../../rust/imzero2/src/imzero2/app.rs) — Rust `logic()` cadence and warmup.
- [`imzero2_demo_resolve.go`](../../public/thestack/imzero2/egui2/demo/carousel/imzero2_demo_resolve.go) — Go decorator cadence.
