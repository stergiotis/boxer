---
type: adr
status: accepted
date: 2026-07-23
reviewed-by: "@spx"
reviewed-date: 2026-07-23
---

# ADR-0140: Hover-scoped wheel capture — per-widget scroll/zoom ownership for canvases

## Context

Scroll and zoom are **whole-`Context` singletons** in egui's `InputState`:
`smooth_scroll_delta` (plain wheel / two-finger scroll) and the derived
`zoom_delta()` (Ctrl+wheel, trackpad pinch, `±` keys). imzero2 drives a single
`egui::Context` bound to `ViewportId::ROOT` (one native window; the headless and
SVG hosts likewise build one context each), so there is exactly **one** of each
value per frame for the entire UI.

egui's model for exclusivity is **consume-or-share**: the widget that handles the
wheel is expected to *zero* it (`ui.input_mut(|i| i.smooth_scroll_delta =
Vec2::ZERO)`); anything that merely *reads* the value leaves it live for every
other reader in the same frame. imzero2 surfaces both deltas to Go as bare
global reads that neither scope to a rect nor consume:

- `fetchR16ScrollDelta` → `ctx.input(|i| i.smooth_scroll_delta)`
  ([`interpreter.rs:5989`](../../rust/imzero2/src/imzero2/interpreter.rs),
  [`egui2_definition_d_fetchers.go:231`](../../public/thestack/imzero2/egui2/definition/egui2_definition_d_fetchers.go)),
  cached once per frame into `StateManager.r16ScrollDelta` and exposed as
  `GetScrollDelta()`.
- `fetchR19ZoomDelta` → `ctx.input(|i| i.zoom_delta())` — cached as
  `GetZoomDelta()`.

Every consumer of these — plus every egui-native `ScrollArea` (the `etable`'s
internal one, the dock's per-tab body wrapper) — reads the *same* single value.
So one gesture drives all of them at once.

**The demonstrator: an `etable` and a `timeline` scroll from the same gesture.**

- The **etable** is `egui_table`'s internal `ScrollArea`, reading the
  current-frame `InputState`; on plain wheel it scrolls and consumes
  `smooth_scroll_delta`.
- The **timeline** zoom path, `Timeline.applyZoomInput`
  ([`timeline.go:1155`](../../public/thestack/imzero2/egui2/widgets/timeline/timeline.go)),
  reads the global `GetZoomDelta()` (R19) and gates only on the **global,
  single-slot** canvas-pointer being non-NaN. Ctrl+scroll/pinch is routed by
  egui into `zoom_delta()` (leaving `smooth_scroll_delta` at zero —
  [`d_fetchers.go:281`](../../public/thestack/imzero2/egui2/definition/egui2_definition_d_fetchers.go)),
  and no `ScrollArea` ever consumes `zoom_delta` — so the timeline zooms off the
  same gesture the etable reacts to, with no arbitration between the two.

The gate is not scoped to the hovered widget. `cp` is `GetCanvasPointer()`
(R14), and R14 is a **single global slot** on the interpreter —
`r14_canvas_hover_x/y` are plain fields
([`interpreter.rs:2352`](../../rust/imzero2/src/imzero2/interpreter.rs)),
overwritten by whichever canvas ran its capture op last (`:8530`). So
"`cp.HoverX` is non-NaN" means *some* canvas was hovered last frame, not *this*
one — and `applyZoomInput` even anchors the zoom at that other canvas's hover x
(`clamp01(cp.HoverX / effW)`, `timeline.go:1169`). The neighbouring
`applyPanInput` (`timeline.go:1198`) already shows the correct shape: it gates
on **this** canvas's response (`GetResponse(thisCanvas).HasIsPointerButtonDown()`).
Pan got it right; zoom, and the raw R16/R19 reads generally, did not.

**Existing partial mitigations** — three seams patched around the same
un-scoped global, none of which fix it:

- `TabNoScroll` / dock `no_scroll`
  ([`egui2_methods.go:292`](../../public/thestack/imzero2/egui2/bindings/egui2_methods.go),
  [`egui2_definition_d_dock.go:72`](../../public/thestack/imzero2/egui2/definition/egui2_definition_d_dock.go))
  turns off the *wrapping container's* ScrollArea so a canvas tab body does not
  scroll the panel while it pans/zooms. Container-vs-content only; two *content*
  widgets that each read the global (etable + timeline side by side) still both
  move. It also names the upstream half of the disease —
  walkers reads the wheel without consuming
  ([podusowski/walkers#544](https://github.com/podusowski/walkers/issues/544)).
- The graph's inline consume
  ([`egui2_definition_d_graphs.go:305`](../../public/thestack/imzero2/egui2/definition/egui2_definition_d_graphs.go)):
  `if zoom_and_pan && graph_resp.contains_pointer() { input_mut(smooth_scroll_delta = ZERO) }`.
  This is the *correct* pattern — a hover-gated consume — but it is hand-rolled,
  graph-only, and covers `smooth_scroll_delta` only.

**Precedent for the mechanism.** The graph's `contains_pointer() →
input_mut(ZERO)` is the consume half. The delivery half already exists too: the
r21 `captureUiRect` / `GetUiRect(seq)` register is a per-caller keyed map drained
by a fetcher into `StateManager` (the lazypane / inspector mechanism,
[ADR-0012](./0012-imzero2-collapsible-retained-bodies.md) 2026-07-12 update).
This ADR composes the two into a reusable primitive.

The shape below was settled in a design dialogue on 2026-07-23.

## Design space (QOC)

**Question.** How does a Go-side canvas widget take exclusive ownership of the
wheel (scroll and zoom) while the pointer is over it, without a second widget —
an egui-native `ScrollArea` or a sibling canvas — reacting to the same gesture?

**Options.**

- **O1** — Per-widget declarative capture (`.CaptureWheel()`): the canvas's Rust
  apply, gated on the widget's own egui `Response`, reads the deltas and
  consumes scroll; delivery is a per-id keyed register drained like r21.
- **O2** — Standalone rect-keyed scoped-read op: Go emits a `(seq, rect)` op;
  Rust hit-tests the rect against the pointer, reads, and consumes.
- **O3** — Go-side response-gate only: keep the global R16/R19 reads but gate
  each consumer on its own `CONTAINS_POINTER` response flag; no consume.
- **O4** — Interpreter-side automatic arbitration: the interpreter tracks all
  canvas responses in a frame and awards the wheel to the topmost hovered one;
  no per-widget opt-in.

**Criteria.**

- **C1** — Hit-test correctness: resolves the owner via egui's own
  topmost-under-pointer test (honours occlusion / layering), not naive geometry.
- **C2** — Exclusivity: stops *other* readers — egui-native ScrollAreas and
  sibling canvases — from also reacting to the same gesture.
- **C3** — Composability: a canvas can own the wheel inside an ordinary scroll
  container, retiring the need for a bespoke no-scroll host.
- **C4** — Integration cost: new IDL / Rust surface plus per-widget migration.
- **C5** — Opt-in clarity: the widget explicitly controls ownership, rather than
  an implicit global policy it cannot decline.

**Assessment.** `++` strong positive, `+` positive, `−` negative, `−−` strong negative.

|    | O1 | O2 | O3 | O4 |
|----|----|----|----|----|
| C1 | ++ | −  | ++ | ++ |
| C2 | ++ | +  | −− | ++ |
| C3 | ++ | +  | −  | ++ |
| C4 | −  | −  | ++ | −− |
| C5 | ++ | ++ | ++ | −  |

O3 is a real partial fix — it corrects the hover determination (C1) at almost no
cost (C4) — but it fails on the actual disease (C2): with no consume, a canvas
and its wrapping ScrollArea still both react, so `TabNoScroll` stays mandatory
(C3). O2 loses C1: a Go-supplied rect cannot see what egui stacked on top of it.
O4 buys the same guarantees as O1 but as an interpreter-wide, un-declinable
policy at the highest complexity. O1 takes the guarantees with per-widget opt-in
at a moderate, bounded cost.

## Decision

We will add a **hover-scoped, consuming wheel-capture** primitive and migrate
the Go-side canvas widgets onto it. Scroll and zoom are **independent opt-ins** —
`.CaptureScroll()` and `.CaptureZoom()` builder methods on the `paintCanvas`
opcode — because the two axes need different fencing (§2), and a widget that only
zooms (the timeline) must leave plain scroll free to scroll an enclosing pane.
Contract:

1. **Hover-gated ownership via the widget's own egui response.** At apply time
   the interpreter uses the canvas response it already computes for the R14
   canvas-pointer capture (`resp` at
   [`interpreter.rs:8530`](../../rust/imzero2/src/imzero2/interpreter.rs)). If
   `resp.contains_pointer()` — egui's topmost-under-pointer hit-test, which
   respects layering and occlusion — the widget owns the wheel this frame;
   otherwise it captures nothing.

2. **The scroll/zoom consume asymmetry — the exclusivity guarantee.** These two
   deltas need different fencing, and the difference is load-bearing:
   - **Scroll must be zeroed globally on capture.** egui-native `ScrollArea`s
     read `smooth_scroll_delta` from the shared `InputState`, so on ownership
     the interpreter sets it to `Vec2::ZERO` via `input_mut` (generalizing
     `d_graphs.go:314`). Any ScrollArea or later reader in the same frame then
     sees no scroll.
   - **Zoom needs no global mutation.** `zoom_delta()` is read only by our own
     canvas widgets — egui's `ScrollArea` consumes scroll but not zoom, and
     Ctrl+scroll never populates `smooth_scroll_delta` in the first place. So
     hover-gating the *read* per widget is already exclusive: the hovered canvas
     captures the zoom factor; every other canvas captures the identity `1.0`
     because its gate is false. No global zoom accumulator is touched.

3. **Per-id delivery, keyed like r21, one-frame lag.** The captured
   `(scrollX, scrollY, zoom, hoverX, hoverY)` land in a per-widget-id map on the
   interpreter, drained by a new keyed fetcher into `StateManager` and read next
   frame as `GetCanvasWheel(handle)` (mirrors the r21 `captureUiRect` /
   `GetUiRect` keyed-drain mechanism). This replaces the single-global R16/R19
   read for canvas widgets and, by construction, kills the "last canvas wins the
   global pointer" anchor bug: a widget reads *its own* slot, defaulting to the
   identity `(0, 0, 1.0, NaN, NaN)` when it did not own the wheel. The
   canvas-local hover travels *with* the capture (the pointer relative to the
   canvas origin at capture time) so the consumer anchors zoom on its own cursor
   position, never the single-slot global r14 pointer. The one-frame lag is
   unchanged from today's pan/zoom.

4. **Ordering is favourable in the nesting that occurs.** The consume happens
   during the owning widget's apply, so a container that reads later in the
   opcode stream sees the zeroed scroll. A canvas nested inside a `ScrollArea`
   renders (and consumes) during the ScrollArea body, before the ScrollArea's
   own end-of-frame scroll handling — the graph/gallery case, which already
   works. The only losing order is a canvas whose apply runs *after* an
   overlapping consumer's; that requires z-overlap of two wheel-consumers and
   resolves to egui's own last-writer semantics. Documented, not defended
   against.

5. **Migration and the fate of the globals.**
   - **SD1 — Timeline** ✓ — `applyZoomInput` switches from global
     `GetZoomDelta()` + global canvas-pointer gate to `GetCanvasWheel(self)`
     (both factor and anchor hover-scoped by construction); the cross-canvas
     anchor bug disappears with it.
   - **SD2 — layeredgraph/view** ✓ — the other PaintCanvas widget reading zoom
     globally ([`view.go`](../../public/thestack/imzero2/egui2/widgets/layeredgraph/view/view.go))
     migrates the same way, dropping its now-redundant `contains_pointer()` gate.
   - **SD3 — Globals retained, scoped by convention** ✓ — `GetScrollDelta()` /
     `GetZoomDelta()` (R16/R19) stay for genuine whole-viewport uses but are
     documented as "unscoped, non-consuming; prefer `GetCanvasWheel` inside a
     widget." Not deprecated in this ADR.
   - **SD4 — `TabNoScroll` becomes optional** ✓ — a canvas that adopts
     `.CaptureScroll()` can live inside an ordinary scroll container; `TabNoScroll`
     remains for the walkers map until its upstream read-without-consume
     (walkers#544) lands, and for genuinely overflow-clipping viewports.

Implementation milestones (IDL + `egui2gen`-regenerated dispatch; no hand edits
to generated code):

- **M1 — the primitive** ✓ — `.CaptureScroll()` / `.CaptureZoom()` IDL methods on
  `paintCanvas` + a new per-id register (six parallel arrays — id, scrollX/Y,
  zoom, hoverX/Y — R23) written in the canvas apply from the existing `resp`, the
  `fetchR23CanvasWheel` drain, and `StateManager.GetCanvasWheel(handle)`.
- **M2 — migrate timeline + layeredgraph** (SD1, SD2 — code landed and
  unit-tested) then verify one gesture no longer crosses widgets on the
  **interactive host, not the tour** (the tour starts on one widget and never
  exercises the cross-widget edge). The interactive verification is the
  remaining gate.
- **M3 — documentation** ✓ — annotate `GetScrollDelta` / `GetZoomDelta` as
  unscoped (SD3) and relax the `TabNoScroll` guidance (SD4).

## Alternatives

- **O3 — Go-side response-gate only.** Gate each consumer on its own
  `GetResponse(self).ContainsPointer()`; no Rust change. Fixes the most visible
  symptom (the timeline zooming while the pointer is over the etable) cheaply,
  but leaves the disease: with no consume, a canvas and its wrapping ScrollArea
  still both react, so `TabNoScroll` stays mandatory and a canvas cannot own the
  wheel inside a plain `c.ScrollArea()`. Retained as the fallback if M1 is
  descoped — it is strictly a subset of O1's behaviour.
- **O2 — standalone rect-keyed scoped-read op.** A `(seq, rect)` op that Rust
  hit-tests and consumes. Decouples the read from a specific widget's response,
  but a Go-supplied rect cannot account for what egui stacked on top of it, so
  ownership is wrong precisely under the overlap where it matters. Rejected in
  favour of routing through the widget's real response.
- **O4 — interpreter-side automatic arbitration.** The interpreter awards the
  wheel to the topmost hovered canvas each frame. Same guarantees as O1 but as an
  implicit, un-declinable global policy at the highest complexity; a widget that
  wants to *not* own the wheel (pass it through to a parent) has no clean opt-out.
  Rejected as over-magic for the benefit.
- **Do nothing — `TabNoScroll` everywhere.** Ask every canvas host to disable
  the surrounding scroll. Does not scale (each host must remember), does not fix
  sibling canvases that both read the global, and does nothing for zoom.

## Consequences

### Positive

- One wheel gesture is owned by exactly one widget — the one egui says is under
  the pointer — and no other reader (native ScrollArea or sibling canvas) reacts.
  The etable + timeline cross-talk is gone by construction.
- Canvases compose inside ordinary scroll containers; `TabNoScroll` drops from
  mandatory to optional (SD4).
- The per-id keyed delivery removes the "last canvas wins the global
  canvas-pointer" hazard, fixing the timeline's cross-canvas zoom anchor as a
  side effect (SD1).
- Generalizes a pattern already proven in-tree (the graph's inline consume) into
  a reusable, tested primitive, rather than each canvas hand-rolling it.
- The scroll/zoom asymmetry (contract §2) means the primitive mutates global
  input state only for scroll, and only on the owning frame — the minimum
  fencing that achieves exclusivity.

### Negative

- New IDL method + interpreter register + keyed fetcher + `StateManager`
  accessor, and every canvas widget must opt in and migrate. Widgets that never
  migrate keep today's global-read behaviour.
- The one-frame FFI lag on the captured delta is inherent and unchanged; this
  ADR scopes ownership, it does not remove the lag.
- A residual ordering edge case (contract §4) for two wheel-consumers that
  z-overlap and render in the unfavourable order; left to egui's last-writer
  semantics.
- Two delivery mechanisms coexist — the per-id `GetCanvasWheel` and the legacy
  global R16/R19 — until (and if) a later ADR deprecates the globals.

### Neutral

- The walkers map keeps its own internal wheel handling; its read-without-consume
  is upstream (walkers#544) and `TabNoScroll` remains its stopgap regardless of
  this ADR.
- egui's single-`InputState` model is unchanged — this primitive *fences* it
  per widget rather than replacing it. The globals remain the right tool for a
  genuine whole-viewport wheel reader.
- The graph's existing Rust-native inline consume can either fold into
  `CaptureWheel` or stay as-is; it is already correct and not on the critical
  path.

## Status

Accepted 2026-07-23.

Status lifecycle: `Proposed → Accepted → (Deferred | Deprecated | Superseded by ADR-XXXX)`.
See [DOCUMENTATION_STANDARD §1 ADR](../DOCUMENTATION_STANDARD.md#architecture-decision-records-why-it-is-this-way) for the edit-policy tiers (Tier 1 in-place / Tier 2 dated `## Updates` entry / Tier 3 new superseding ADR).

## Updates

### 2026-07-23 — implemented (M1, M3, SD1–SD4); M2 interactive verify pending

Shipped the same day as acceptance:

- **The primitive (M1).** `paintCanvas` gained two independent opt-in methods,
  `.CaptureScroll()` and `.CaptureZoom()` (nullary; presence enables). The
  canvas apply, gated on the `resp.contains_pointer()` it already computes for
  R14, reads the wheel and pushes a row to a new per-id register (R23, six
  parallel `Vec`s: id, scrollX/Y, zoom, hoverX/Y), drained by
  `fetchR23CanvasWheel` into `StateManager.r23CanvasWheel` and read as
  `GetCanvasWheel(handle)` — default identity `{0,0,1,NaN,NaN}` when the canvas
  did not own the wheel. The scroll/zoom asymmetry (Decision §2) is realized:
  `.CaptureScroll()` zeroes `smooth_scroll_delta` via `input_mut`; `.CaptureZoom()`
  touches no global.
- **Refinement — hover travels with the capture.** The delivered tuple gained
  `hoverX/hoverY` (pointer relative to the canvas origin at capture time) beyond
  the proposed `(scrollX, scrollY, zoom)`. Without it the zoom *anchor* still
  read the single-slot global r14 pointer, which a later non-hovered canvas
  overwrites with `NaN` — the same class of bug the ADR set out to remove. The
  anchor is now self-contained in the per-id capture.
- **Migrations (SD1, SD2).** `timeline.applyZoomInput` consumes
  `GetCanvasWheel(canvas)` (factor + anchor), replacing the global
  `GetZoomDelta()` + global-canvas-pointer gate; the timeline canvas opts in with
  `.CaptureZoom()` only, so plain scroll still scrolls an enclosing pane.
  `layeredgraph/view` migrated the same way and dropped its now-redundant
  `resp.HasContainsPointer()` gate. Five table-driven unit tests cover
  `applyZoomInput` (identity/NaN no-ops, left/centre zoom-in anchors, zoom-out).
- **Docs (SD3, SD4).** `GetScrollDelta` / `GetZoomDelta` are annotated as
  unscoped/non-consuming with a pointer to `GetCanvasWheel`; the `TabNoScroll`
  doc notes a `.CaptureScroll()` canvas no longer needs it.
- **Verification.** `cargo check` (desktop + headless — the interpreter is
  shared), `go build` / `go vet` on the affected trees, and the timeline test
  suite all pass. **M2's interactive cross-widget check** (scrolling an etable
  beside a timeline no longer zooms the timeline; a graph in another pane does
  not steal a timeline's wheel) is the remaining gate — it needs the live host,
  since the screenshot tour starts on a single widget and never exercises the
  cross-widget edge.

## References

- [ADR-0043](./0043-imzero2-timeline-widget.md) — the timeline widget; SD1
  migrates its `applyZoomInput`.
- [ADR-0069](./0069-imzero2-layeredgraph-widget.md) — the layeredgraph widget;
  SD2 migrates its global zoom read.
- [ADR-0056](./0056-walkers-map-h3-binding.md) — the walkers map binding; its
  read-without-consume (walkers#544) is the upstream half `TabNoScroll` covers.
- [ADR-0012](./0012-imzero2-collapsible-retained-bodies.md) — the r21
  `captureUiRect` / `GetUiRect(seq)` keyed-fetcher mechanism this primitive's
  delivery mirrors (2026-07-12 update).
- [ADR-0024](./0024-imzero2-remote-access-browser-viewer.md) — the input
  translation edge (`inputmap.rs`) that produces the `egui::Event::MouseWheel` /
  `Zoom` this ADR routes.
- `egui2_definition_d_graphs.go:305` — the graph's inline `contains_pointer() →
  input_mut(ZERO)` consume, generalized here.
- `egui2_methods.go:292`, `egui2_definition_d_dock.go:72` — the `TabNoScroll` /
  dock `no_scroll` mitigation, relaxed by SD4.
- [podusowski/walkers#544](https://github.com/podusowski/walkers/issues/544) —
  upstream read-without-consume.
