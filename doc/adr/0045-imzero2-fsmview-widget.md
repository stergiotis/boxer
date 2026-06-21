---
type: adr
status: accepted
date: 2026-05-22
reviewed-by: "p@stergiotis"
reviewed-date: 2026-06-08
---

# ADR-0045: Two-level FSM visualization widget (fsmview)

## Context

Several boxer surfaces already model their lifecycle as a finite
state machine — the spinnaker/play UMAP projector
(`Idle → Running → Cancelling → Cancelled`), the ADR-0026 app runtime's
manifest reconciliation, the ADR-0028 ch.local.exec cap's worker pool —
but each renders state ad-hoc, typically as a plain string in a status
bar or a colourless label. There is no shared widget for "show me the
current state at a glance, and let me drill into the full machine when
I need to."

This ADR proposes a foundational, two-level FSM viewer modelled on the
HCI tooltip idiom:

- **Level 1** — a compact chip (built on `widgets/badge`) showing the
  current state's label, tinted by the IDS accent palette so it reads
  as "live status." Embeds inline anywhere a label fits.
- **Level 2** — a floating popup opened by clicking the chip, with two
  switchable views of the full machine:
  - a **table** (cheap at small N, reads as a state × outgoing-transitions
    grid), and
  - a **graph** rendered via the existing `egui_graphs` binding
    (force-directed-with-centre-gravity layout, scales beyond ~10 states
    without manual positioning).

The visualization needs to be operator-friendly under the immediate-mode
discipline — no callbacks, no event subscriptions, no animations that
require frame-counting; everything reads the FSM state freshly each
frame.

## Design space (QOC)

**Question.** Which Go FSM library should the widget couple to as the
canonical "application-managed FSM" source?

**Options.**

- **O1** — `hishamk/statetrooper` (chosen). Tiny generic FSM
  (`FSM[T comparable]`), flat states, no callbacks. `go 1.20`.
- **O2** — `qmuntal/stateless`. Port of .NET Stateless; hierarchical
  states, OnEntry/OnExit callbacks, guard conditions. `go 1.24`.
- **O3** — `looplab/fsm`. Most-starred Go FSM. Event-driven with
  before/leave/enter/after callbacks. States/events typed as `string`
  (no generics).

**Criteria.**

- **C1 — Go-generics fit.** Does the library type FSM state via a
  generic parameter, or does it fall back to `any` / `string`?
- **C2 — Immediate-mode fit.** Does the API impose a callback /
  event-loop discipline that clashes with the IM render-each-frame
  paradigm?
- **C3 — API surface.** How much of the library does the widget have to
  ignore or wrap?
- **C4 — Hierarchical / advanced features.** Does the library support
  features (substates, guards) the widget might want to surface later?

**Assessment.** `++` strong positive, `+` positive, `−` negative, `−−` strong negative.

|                                 | O1 statetrooper | O2 stateless | O3 looplab/fsm |
|---------------------------------|----------------|--------------|----------------|
| C1 Generics                     | ++ `[T comparable]` | −− `type State = any` (pre-generics, preserved in go 1.24) | −− `string` only |
| C2 IM fit                       | ++ no callbacks | −  callback-heavy, but bypassable | −  event-driven; callbacks load-bearing |
| C3 API surface                  | ++ ~9 methods   | − ~30 methods + Configure DSL | + ~15 methods, but event-DSL is mandatory |
| C4 Hierarchical / guards        | −  flat only    | ++ substates, guards | + nested events; no substates |

statetrooper wins three of four criteria. Hierarchical support is the
only meaningful loss; we accept the risk of forking statetrooper
locally (`~50` LOC to add `ParentState[T]` if needed) over locking in
either an `any`-typed API (qmuntal) or a `string`-typed API
(looplab/fsm) that would force type assertions across every caller of
the widget.

## Decision

We will add `public/thestack/imzero2/egui2/widgets/fsmview/`,
exposing two generic types:

- **`Machine[T comparable]`** — the visualization-aware wrapper around
  `*statetrooper.FSM[T]`. Mirrors rules locally because statetrooper's
  `ruleset` field is unexported, so the widget cannot enumerate
  transitions from the FSM alone. Builders cover labels, edge labels,
  per-state colours, and stable display order.

- **`Widget[T comparable]`** — the composite IM widget. Holds popup-
  open / selected-renderer state on the receiver pointer (per the
  composite-widget pattern, **not** an FFFI2 stateful primitive — does
  not bind via R10/R9 databindings). Renders the level-1 chip via
  `widgets/badge`; opens the level-2 popup as a floating `c.Window`
  with a Table ⇄ Graph `SelectableLabel` toggle.

The `Machine[T]` ↔ `*statetrooper.FSM[T]` boundary is intentionally
narrow: callers interact with `Machine` for setup and rendering, and
can reach the underlying FSM only by tracking it themselves. This
keeps the door open for a later swap (qmuntal, fork, custom) without
breaking the widget's public API.

## Alternatives

- **Generic interface, no library coupling.** Define `fsmview.FSMI`
  with `Current()` / `States()` / `Edges()` and ship adapters per
  library. Rejected on user feedback: "tight coupling to one library"
  was the explicit choice (a thin interface is the right call when
  cross-library portability matters; for a foundational widget with
  one canonical Go FSM source, the wrapper pattern is enough).
- **`qmuntal/stateless` tight coupling.** Rejected per QOC C1/C3 — the
  `any`-typed API forces type assertions throughout callers and the
  widget seam, which clashes with the boxer/keelson generics-heavy
  idiom.
- **egui-snarl as the level-2 renderer (ADR-0021).** Snarl is a manual
  node editor (drag positions, edit topology); it doesn't auto-layout.
  For *visualisation* the existing `egui_graphs` binding's force-
  directed layout is the right fit. Snarl remains the right tool for
  *editing* an FSM topology — out of scope for this widget.
- **Hover-only tooltip for level-2.** Rejected: a tooltip dismisses on
  pointer-leave, so the operator can't interact with the graph view
  (drag a node, click an edge). Click-to-pin via a `c.Window` is the
  smallest primitive that allows interaction inside the level-2 body.
- **Inline expansion (CollapsingHeader) instead of a floating window.**
  Rejected for the foundational case: an inline expansion shoves
  siblings down the page, which is the wrong affordance for a "drill
  into details" gesture. Floating popup keeps the surrounding layout
  stable. (Inline could be added later as an explicit `.Mode(InlineE)`
  option if a caller needs it.)

## Consequences

### Positive

- A single foundational primitive for "show me current state + the
  whole machine." Surfaces that previously rolled their own status
  labels can drop in `fsmview.New(...)` and get IDS-correct theming +
  drill-down for free.
- Decoupling `Machine` from `*statetrooper.FSM` (one level of
  indirection at the wrapper) lets us swap libraries later without
  breaking callers — the cost is one extra type per widget instance,
  paid once at construction.
- `egui_graphs` integration reuses an already-bound primitive (no FFFI2
  changes, no `./generate.sh` invocation needed for M0).

### Negative

- statetrooper is not hierarchical. Surfaces that need substates
  (nested workflows, hierarchical reconcilers) will either fork
  statetrooper locally or migrate to a richer FSM lib — at which point
  this ADR's decision is up for revisit.
- statetrooper's `ruleset` field is unexported, so `Machine.AddRule`
  has to double-bookkeep (forward to the FSM **and** keep a local
  mirror for graph/table enumeration). Drift is possible if a caller
  bypasses `Machine.AddRule` and calls `*statetrooper.FSM.AddRule`
  directly. M0 ships without a runtime drift guard; we add one in M3
  if it shows up in practice.

### Neutral

- The level-2 popup spawns at egui's default cascade position rather
  than anchored to the chip's bottom-left. egui retains the user's
  drag position across frames, so practically this matters only for
  the first-open frame. A future `.AnchorAtChip()` option could close
  the gap; not load-bearing for M0.
- The widget intentionally **does not** consume statetrooper's
  `GenerateMermaidRulesDiagram` / `GenerateMermaidTransitionHistoryDiagram`
  helpers. They emit static Mermaid source — useful for documentation,
  not for an IM rendering loop. We render directly via `egui_graphs`
  and the existing IDS table primitives.

## Milestones

> **Landed as of 2026-06-08:** M0–M3 incl. M3a (details in Updates below). M4 is an explicit deferral.

| M  | Scope                                                                                          | Exit criterion                                                              |
|----|------------------------------------------------------------------------------------------------|-----------------------------------------------------------------------------|
| M0 | `Machine[T]` + `Widget[T]` skeleton, chip + popup, Table view, Graph view, demo registered.    | `go build -tags "$(cat tags)"` green; demo appears in widgets carousel.     |
| M1 | History view inside popup (statetrooper provides timestamped `Transitions()`); chip subscript "last transition Xs ago." | Tour PNG captured under `IMZERO2_SCREENSHOT_DIR`; history reads correctly across a few state changes. |
| M2 | Native title-bar X via `.OpenBound(*bool)` + R10 databinding (per `feedback_egui_native_affordances`); remove the in-body Close button. | Widget passes lint; close-X works in the demo. **Landed 2026-05-23 — see Updates.** |
| M3 | IDS theme polish (active-edge tint via AccentSubtle, density-aware popup padding), REFERENCE.md emitted, ADR flips `proposed → accepted` once reviewed. | All `scripts/ci/lint.sh` gates green; carousel screenshot reviewed. **Landed 2026-05-23 modulo the ADR flip — see Updates.** |
| M3a-i | Add `.DefaultPos(x, y)` to the `Window` definition (`egui2_definition_d_blocks.go`) + regenerate; expose `Widget.PopupAnchor(x, y)` / `ClearPopupAnchor()` so callers can position the popup manually. | Binding regen produces a clean `WindowFluid.DefaultPos`; demo opens popup at the supplied anchor. **Landed 2026-05-23 — see Updates.** |
| M3a-ii | Auto-anchor popup near the click — new `fetchR20Pointer` fetcher backed by `egui::InputState::pointer.latest_pos()`; `Widget.AutoAnchor(true)` captures the pointer on chip-click and writes it into the popup anchor. Cursor position, not chip bottom-left (see Updates for the trade-off). | `Widget.AutoAnchor(true)` makes the popup pop near the click; demo opts in. **Landed 2026-05-23 — see Updates.** |
| M4 *(deferred)* | Hierarchical states via local statetrooper fork *or* migration to a richer FSM lib if hierarchical use cases materialise. | Driven by a concrete use case — not by this ADR.                            |

## Status

Accepted — 2026-06-08 (reviewed by p@stergiotis). This meets the ADR's own flip criterion — the design is reviewed and M3 has landed (see Updates). M0–M3 incl. M3a are shipped; M4 is an explicit, use-case-driven deferral.

Status lifecycle: `Proposed → Accepted → (Deferred | Deprecated | Superseded by ADR-XXXX)`. ADRs are append-only; supersession is recorded, not deleted.

## Updates

### 2026-06-07 — Graph tab migrated to the layeredgraph engine (ADR-0069)

The Graph view no longer uses the `egui_graphs` force-directed (FR+CG)
binding. It now renders a **static layered (Sugiyama) layout** computed by
Graphviz `dot` in-process (WebAssembly, cgo-free) via the `layeredgraph`
package + `goccyengine`, painted through the existing painter binding
([ADR-0069](0069-imzero2-layeredgraph-widget.md)). This realises the *layout*
half of M4 (a richer graph layout) without forking statetrooper; the
two-level chip + popup design — the core of this ADR — is unchanged. The
references to `egui_graphs`, the FR/CG tunables and the first-frame pre-warm
in the body above are superseded for the Graph tab.

`renderGraph` builds a `layeredgraph.GraphModel` from the Machine
(states→nodes by `NodeId`, transitions→edges), lays it out once and caches it
— invalidated when the state/edge count changes, since `Mirror`/`AddRule` can
grow the machine at runtime — and paints via `view.Render`, which adds
interactive pan/zoom, hover/click hit-testing and click-to-transition.
Same-label states (colliding `NodeId`) merge to one node rather than failing
the view. The `graphPrewarmed` flag and the FR constants are removed.

### 2026-05-23 — M3a-ii landed (auto-anchor via R20 pointer)

`fetchR20Pointer` added in `egui2_definition_d_fetchers.go`. Returns
`(x, y, valid)` from `egui::InputState::pointer.latest_pos()`. The Go
side caches it per-frame in `StateManager.r20Pointer` (drained in
`Sync()` like every other R*-cached fetcher) and exposes
`GetPointer() PointerValue`. `PointerValue` and the field/accessor are
hand-written in `egui2_statemanagement.go` to mirror the existing
CanvasPointer/ScrollDelta/etc. cache layout.

The widget grows `AutoAnchor(on bool) *Widget[T]`. When true, the
chip-click branch reads `StateManager.GetPointer()` and writes the
result into `popupAnchor`, overriding any previously-set manual
anchor. The R20 fetcher's one-frame lag is already absorbed by the
response cache that gates the click branch — the pointer reads the
position the click landed on.

**Trade-off recorded.** R20 gives the *cursor* position, not the
chip's bottom-left. For a small badge-sized chip the two are within a
few pixels; for taller chips the popup will pop near the click point
rather than directly under the chip. True bottom-left anchoring would
need a per-widget rect cache on the Rust side (every `response.rect`
recorded keyed by widget id) — substantial new state, not yet scoped.

The demo opts in: `AutoAnchor(true)` for interactive clicks, plus a
fallback `PopupAnchor(60, 220)` so the pre-opened popup in the
screenshot tour still lands at a predictable spot.

### 2026-05-23 — M3a-i landed; M3a-ii deferred

`WindowFluid.DefaultPos(posX, posY float32)` added to the
`egui2_definition_d_blocks.go` Window block + regenerated via
`./generate.sh` (four `.out.go` artefacts staged together per
`feedback_generate_sh_full_staging`). One-letter arg names tripped the
IDL validator's reserved-pattern check; renamed `x → posX` / `y → posY`
to match the existing convention.

The widget grows `PopupAnchor(x, y) *Widget[T]` + `ClearPopupAnchor()`
fluent setters. When set, `renderPopup` threads the anchor through as
egui's `default_pos`. egui retains the user's dragged position across
subsequent opens, so the anchor only affects the first open of a fresh
widget instance (which is also the visible behaviour during the
4-frame screenshot tour).

The demo calls `PopupAnchor(60, 220)` so the captured PNG no longer
shows the popup landing on top of the level-1 chip via egui's cascade
default.

**M3a-ii (auto-anchor) is genuinely separate scope.** It needs a new
widget-rect or cursor-pos FFFI2 fetcher (R20-class) so the widget can
look up the chip's previous-frame absolute position without the caller
passing it. That's new IDL + new Rust state + new sync plumbing — best
sized as its own focused milestone rather than tacked onto M3a-i.

### 2026-05-23 — M3 landed; M3a deferred

IDS theme polish + REFERENCE.md shipped. Two visible refinements in the
widget:

- **Edges leaving the current state** tint `AccentSubtle`; **other
  edges** tint `NeutralBorderFaint`. The graph view now reads as
  "here is where the FSM can go next" without an extra legend.
- **Popup body** picks up a top/bottom `PaddingInner` derived from the
  active density preset (ADR-0032 §SD2), so the renderer toggle and the
  tabs no longer butt against the title bar / window resize affordance.

REFERENCE.md (next to the package) enumerates the public API surface,
affordances, IDS-token usage, and lists current limitations. It points
back to this ADR for design rationale.

**M3a deferred — chip-anchored popup positioning.** The original M3
scope included "anchor popup to chip's bottom-left on first open."
Implementing this requires adding a `.DefaultPos(x, y)` method to the
`Window` widget definition (`egui2_definition_d_window.go`) and
regenerating four `.out.go` artefacts via `./generate.sh` — a focused
single-PR change that's better isolated from the IDS/doc polish.
Promoted to its own milestone above.

**ADR status stays `proposed` pending human review.** The M3 exit
criterion called for the flip; the reviewer-handle slot is intentionally
left empty until a code owner signs off.

### 2026-05-23 — M2 landed (native title-bar X)

The in-body Close button is gone; popup dismissal now flows through
`c.Window(...).OpenBound(bindingId)` plus a per-frame
`StateManager.AddR10Databinding(bindingId, &w.popupOpen)`. The binding
id is the Window's own widget id, exposed by a new
`WindowFluid.Id() uint64` accessor in `egui2_methods.go` (parallels the
existing `FrameFluid.Id()`).

One subtlety surfaced during implementation: the M0 design had a Go-side
gate `if w.popupOpen { renderPopup() }` that skipped Window emission
entirely when closed. With OpenBound the gate stays in place — when the
X is clicked, R10 reads back `popupOpen = false` at end-of-frame Sync,
and the next frame's gate elides the Window opcode. The two-way
semantics of egui's `.open(&mut bool)` still work because we re-register
the binding each frame the popup is open.

### 2026-05-22 — graph layout pre-warm

Force-directed layout looked frozen until the operator clicked or
dragged a node. Two compounding causes: (1) the seven FR step parameters
(`dt` / `damping` / `epsilon` / `maxStep` / `kScale` / `cAttract` /
`cRepulse`) defaulted to 0 when unset; (2) egui_graphs initialises node
positions coincident at (0, 0), so even with non-zero params the forces
cancel until user interaction breaks the symmetry.

Fix: set the seven tunables to the graphs-demo defaults, and emit
`.ResetLayout().FastForwardSteps(200)` on the first Graph-view frame
(tracked via a per-`Widget` `graphPrewarmed` flag). The simulation
converges deterministically before the operator sees the graph.

## References

- ADR-0013 — Stateful widget contract (composite widgets opt out of
  R9/R10 databindings).
- ADR-0021 — `egui-snarl` (manual node editor; this widget complements
  rather than replaces it).
- ADR-0026 — App runtime + capability subjects (motivating consumer of
  FSM visualization for app lifecycle).
- ADR-0031 §SD2 — IDS color (active state lights via `AccentDefault`,
  resting states via `NeutralSubtle`).
- `public/thestack/imzero2/egui2/widgets/fsmview/` — M0
  implementation.
- `public/thestack/imzero2/egui2/demo/apps/widgets/egui2_hl_fsmview_demo.go`
  — traffic-light demo registered in the widgets carousel.
- `github.com/hishamk/statetrooper` — chosen FSM library.
