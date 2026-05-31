---
type: adr
status: proposed
date: 2026-05-24
# reviewed-by: "@<handle>"     # fill in and uncomment when flipping to accepted
# reviewed-date: YYYY-MM-DD    # fill in and uncomment when flipping to accepted
---

> **Status: proposed — pre-human-review.** Decision under consideration; do not implement as if accepted.

# ADR-0046: Value-inspector infrastructure — Provenance, Source[T], chip, anchor chevron, bezier tether

## Context

Several pebble2impl widgets have emerged organically as "value inspectors" —
small surfaces that bind to a domain value and expose it for human
exploration:

- **fsmview** ([ADR-0045](0045-imzero2-fsmview-widget.md)) — finite-
  state-machine viewer; level-1 chip + level-2 click-popup window with
  table / graph / history tabs.
- **distsummary** — distribution summariser; level-1 monospace
  5-number summary + level-2 hover popup showing a Hofmann/Wickham/
  Kafadar letter-value plot.
- **fieldview** — hierarchical typed-field inspector; level-1
  always-rendered list, optional level-2 nesting via collapsible.
- **errorview** — structured wrapped-error chain renderer for
  `eh.MarshalError`-shaped data; level-1 always-rendered, level-2 via
  collapsible.

Each invented its own header + disclosure affordance + per-widget
identity story. None carried a visible "where did this value come from"
trail back to the source; screenshots of a running app could show four
distinct inspectors and no reader could tell which subject any of them
was watching. None shared a disclosure cue — fsmview's chip looked
clickable because of its IDS-accent background, distsummary's level-1
read as plain text with no visual indication that hovering opened
anything.

The earlier branch design discussion concluded that inspectors should
become first-class citizens with shared infrastructure spanning four
concerns:

1. **Identity** — every inspector needs a visible "this is what I'm
   reading from" affordance keyed off a stable canonical name
   (typically an [ADR-0026](0026-app-runtime-and-capability-subjects.md) §SD3
   app-event subject).
2. **Source binding** — a uniform shape that bus-subscribed inspectors
   (future) and method-arg / receiver-owned inspectors (today) both
   satisfy, so widgets accept one parameter and the migration to live
   binding is one-call-site swap.
3. **Disclosure cue** — a consistent visual marker on inspectors that
   open level-2 popups, so the "more inside" affordance is readable at
   rest (not hover-only — screenshots can't capture hover).
4. **Visual tethering** — bezier-style connectors from a source-
   selector chip to a floating inspector window, so multi-pane
   dashboards make the "this inspector watches that source" relation
   geometrically obvious.

Doing all four under one umbrella avoids the fan-out where each
inspector grows its own ad-hoc subject-display + interactivity-cue +
window-anchoring code.

## Design space (QOC)

**Question.** What shape should the shared infrastructure take, and
how do existing inspectors migrate to it?

**Options.**

- **O1 — `widgets/inspector` package, inspectors as widgets bound
  to `Source[T]`** (chosen). Provenance struct + Source[T] interface
  + ProvenanceChip widget + AnchorChevron constant live in one new
  package; every existing inspector adds a `.Provenance(p)` builder
  and renders the chip in its level-2 body. Two FFFI2 primitives
  (`captureUiRect`, `paintAbsoluteOverlay`) added in parallel to
  unblock the bezier-connector tether across egui::Window
  boundaries.
- **O2 — Inspector as full `AppI`** (per ADR-0026). Each inspector
  becomes a runtime-registered app, claims a NATS-style cap subject
  filter, gets a windowed surface managed by the carousel host.
- **O3 — Provenance as a method-arg, no shared abstraction.** Each
  inspector grows its own `.Provenance(p)` field; no shared widget,
  no shared `Source[T]`, no shared chevron — every inspector chooses
  its own header format.

**Criteria.**

- **C1 — Composition.** Can inspectors be embedded inside other
  inspectors (a `fieldview` cell containing a `distsummary` for a
  numeric field, an `errorview` chain quoting an `fsmview` snapshot
  inside a fact's structured-data section)?
- **C2 — Bus-binding bridge.** Does the design naturally accommodate a
  future `LiveSource[T]` that subscribes to a bus subject + decodes
  via [ADR-0036](0036-runtime-buscodec.md), or does live binding
  require a per-inspector API rewrite?
- **C3 — Migration cost.** How much code does an existing inspector
  change to opt in (lines, files, breaking-API)?
- **C4 — Screenshot legibility.** Does the affordance survive a static
  screenshot — can a reader see *what* an inspector is watching
  without running the app and reading hover text?

**Assessment.** `++` strong positive, `+` positive, `−` negative, `−−` strong negative.

|                              | O1 widget + Source[T] | O2 inspector-as-App | O3 ad-hoc method-arg |
|------------------------------|-----------------------|---------------------|----------------------|
| C1 Composition               | ++ widgets nest freely | −− apps are top-level surfaces, can't be nested | + still widget-shaped, but no shared embedding contract |
| C2 Bus-binding bridge        | ++ Source[T] anchors LiveSource at the interface, one-line swap at call sites | + every app already has BusI access via MountContext | − each inspector reinvents subscription / decode plumbing |
| C3 Migration cost            | ++ one builder + one chip call per inspector (≈10 LoC) | −− each inspector rewrites as AppI (Manifest, Mount/Frame/Unmount lifecycle) | + zero new abstraction but every inspector still has to add Provenance plumbing |
| C4 Screenshot legibility     | ++ ProvenanceChip + AnchorChevron always rendered | + apps can render any header they want | − no uniform "what am I watching" affordance |

O1 wins three of four criteria; the AppI route trades composition for
infrastructure parity that inspectors don't actually need (they don't
own their own window most of the time — they're embedded). Method-arg
without a shared abstraction (O3) gives up the bridge to bus binding
and the screenshot affordance.

## Decision

We will introduce `widgets/inspector` as the shared infrastructure
package for value inspectors, exposing four pieces of surface:

1. **`Provenance` struct** — `{ Subject, Schema, SourceApp, SampledAt }`
   with `IsZero` suppression. Subject is the load-bearing field
   (canonical name); other fields are optional metadata rendered as
   muted trailers on the chip.

2. **`Source[T any]` interface** — `Provenance() Provenance` plus
   `Snapshot() T`. Pull-based to match immediate-mode rendering; no
   callbacks, no async update semantics. `NewStaticSource[T](p, v)` is
   the only impl wired today; `LiveSource[T]` is deferred until the
   first bus-bound inspector arrives.

3. **`ProvenanceChip(p)`** — canonical one-line header: `↳` + Subject
   in monospace + optional muted trailers (`· Xs ago` / `· schema` /
   `· from app`) via the `NeutralTextSecondary` IDS token. Renders
   nothing on zero Provenance so callers invoke unconditionally.

4. **`AnchorChevron = " ▸"`** — standard trailing disclosure cue
   appended to every inspector's level-1 anchor that opens a popup,
   so the "more inside" affordance reads at rest. Pairs visually with
   the chip's leading `↳`: `▸` says "drill into me", `↳` says "this
   came from".

Inspectors adopt the infrastructure by adding a single
`.Provenance(p)` builder and one `inspector.ProvenanceChip(...)` call
at the top of their level-2 body. fsmview and distsummary are
migrated as proof; fieldview / errorview follow the same shape on
demand.

Two FFFI2 primitives land in parallel to support the bezier-connector
tether (PoC under `demo/apps/widgets/egui2_hl_bezier_connector_demo.go`):

- **`captureUiRect(seq)`** — stamps `ui.min_rect()` into r21 parallel
  vectors keyed by seq, drained by `fetchR21UiRects`. Lets Go learn a
  Ui scope's viewport-absolute rect with the standard one-frame lag.
- **`paintAbsoluteOverlay()`** — drains `paint_cmds` into an
  `Order::Foreground` painter with full-screen clip, so connector
  curves can cross egui::Window boundaries instead of being clipped
  at their host's content area.

Both primitives are general — the bezier-connector tether is the first
consumer, but any cross-window annotation / debug overlay can use
them.

## Alternatives

- **LiveSource[T] today.** Defining the full bus-bound Source impl
  alongside the static one. Rejected as YAGNI: no caller yet wants a
  bus-subscribed inspector, and shipping unused infrastructure means
  the API may bend wrong under the first real consumer. Interface is
  anchored so the bridge is one constructor swap.
- **Phosphor icon as disclosure cue.** A `MagnifyingGlass` /
  `ArrowsOutSimple` glyph instead of the unicode chevron. Rejected
  because (a) the chevron is design-system-restraint-aligned (no
  chroma), (b) Unicode survives every rendering path including the
  SVG screenshot exporter, and (c) one glyph belongs to one
  vocabulary — pairing `▸` with the chip's `↳` reads as one
  disclosure-and-source idiom rather than two unrelated icons.
- **Inspector-as-App** (option O2 above). Already covered in QOC —
  rejected because most inspectors are embedded, not top-level
  surfaces, and the AppI lifecycle is overkill for a value renderer.
- **Per-widget rect fetcher** instead of `captureUiRect(seq)`. Would
  require every widget to opt in to having its rect captured.
  Rejected: the capture op is generic and one site per Ui scope is
  enough; we don't need a rect for every widget, only for ones we
  want to tether to.
- **Top-layer painter parameterised by layer-id.** Today
  `paintAbsoluteOverlay` uses a hardcoded `Id::new("imzero-absolute-
  overlay")`. Rejected (for now) because all paint_cmds drain into
  one queue regardless of layer id — parameterising the layer id
  alone wouldn't isolate independent overlays, and the storage
  rework is large. Revisit when a second consumer arrives.

## Consequences

### Positive

- **One disclosure vocabulary.** `↳` for source-binding, `▸` for
  level-2 disclosure, applied consistently across inspectors. Reading
  a screenshot of a multi-pane app now answers "what is this
  inspector watching" and "what hides behind this anchor" without
  any text explanation.
- **Source[T] anchors future bus binding.** When the first
  bus-subscribed inspector arrives, `LiveSource[T]` slots in as the
  second impl; every existing inspector's signature stays unchanged.
  Caller code switches from `inspector.NewStaticSource(p, v)` to
  `inspector.NewLiveSource[T](bus, subject, decoder)` — one line.
- **`captureUiRect` + `paintAbsoluteOverlay` unlock generic
  cross-scope drawing.** Future affordances (debug overlays, popover
  arrows, drag-trail breadcrumbs) reuse the same primitives without
  another FFFI2 addition.
- **Low migration cost.** fsmview (24 LoC) and distsummary (per-row
  Provenance in the demo, ~10 LoC in widget) both landed without
  changing existing callers — `IsZero()`-suppression keeps callers
  who don't opt in visually unchanged.

### Negative

- **Source[T] interface is anchored but not exercised.** Until
  `LiveSource[T]` arrives, the interface might be the wrong shape
  (does it need lifecycle methods, Close, error propagation,
  back-pressure signals?). The first bus-bound inspector will
  pressure-test and possibly bend the contract.
- **`captureUiRect` semantics: inner content, not outer window.**
  Inside a `c.Window(...)` body, `ui.min_rect()` returns the content
  area (excluding title bar + frame). For window-tethered bezier
  endpoints this means the endpoint lands at content top/middle, not
  the window's true geometric centre. Acceptable visually; a clean
  fix would expose `egui::Window` response.rect via a different
  capture mechanism (`captureLastWindowRect` op or similar).
- **`paintAbsoluteOverlay` uses a single global layer id.** Multiple
  consumers can paint to it, but their cmds arrive interleaved via a
  shared paint_cmds queue with no separation. Independent
  overlays-in-the-same-frame are not isolable; document as a
  single-consumer-per-frame contract.
- **One-frame lag on every capture.** Bezier endpoints visibly lag
  during fast window drags; acceptable for the value-inspector use
  case (drag-and-watch is not the dominant interaction) but a hard
  limit on what the primitive can support.
- **AnchorChevron changes level-1 label widths.** Tight layouts that
  hardcode column widths or padding can break by ~6 px. Demo
  callers must accept the affordance trades a few pixels of width
  for affordance legibility. fsmview opts out of the chevron because
  its IDS-accent state chip is already a strong affordance.
- **`SenseRegion` cmds drained through `paintAbsoluteOverlay` are
  silently skipped** (no Ui scope for `ui.interact()`); logs a
  `tracing::warn!` instead. Interactive overlays can't use the
  foreground-layer painter — they need a Ui-scoped PaintCanvas.

### Neutral

- **fsmview opts out of AnchorChevron, opts in to ProvenanceChip.**
  The chevron is convention, not contract; widgets with a strong
  existing affordance (coloured state chip, hover-highlighted
  selectable) can keep their visual unchanged. The infrastructure
  accommodates either choice.
- **`widgets/inspector` is positioned next to other widget packages
  rather than under `keelson/runtime/`.** It's a widget package
  (composes egui primitives, not runtime services); the keelson
  pillar is for runtime / data / security infrastructure, not
  widgets.
- **Source[T] uses Go generics**, not `any`. Consistent with the
  fsmview decision (ADR-0045 §QOC C1) to prefer typed FSM state over
  string / any.

## Open points and known limitations

These are tracked here so the next reader doesn't have to rediscover
them from code archaeology.

1. **`LiveSource[T]` not implemented.** Needs (a) runtime BusI
   plumbing wire — the `widgets/inspector` package can't import the
   keelson runtime cleanly without introducing a layering question,
   (b) [ADR-0036](0036-runtime-buscodec.md) buscodec integration for
   decoding payloads, (c) a lifecycle story (unsubscribe on widget
   unmount), (d) a capslock policy for which apps are allowed which
   subjects. Probably warrants its own ADR when the first consumer
   surfaces.

2. **Provenance schema is freeform string.** No registry,
   no validation that `Subject` matches any cap's actual filter.
   Operators can write arbitrary text in the Subject field. We could
   tighten this with a type, but at the cost of making
   StaticSource (the today-only impl) harder to construct in demos
   and tests.

3. **One-frame lag on bezier endpoints during drag.** Window-rect
   captures inside the window body land in r21; bezier endpoints
   computed next frame visibly lag during fast drags. Acceptable for
   value-inspector use; not acceptable for high-frequency interactive
   overlays (drag-trails, live cursor breadcrumbs).

4. **`captureUiRect(seq)` collisions in nested scopes.** If two
   widgets both capture using the same `seq` constant, the second
   write wins in the StateManager map. Recommendation in the doc
   comment is to use widely-spaced numeric constants
   (`0xBC0001`, `0xDD0001`, ...), but the runtime can't enforce
   uniqueness. A `MustBeValidStylableName(...).Seq()` helper could
   tighten this if collisions become a real problem.

5. **`paintAbsoluteOverlay` is single-consumer-per-frame.** Two
   widgets that both queue paint cmds and both call
   `paintAbsoluteOverlay` get their cmds mixed in submission order.
   Document as a contract; revisit when a second consumer needs
   isolation.

6. **`AnchorChevron` is content-text-mutation.** It modifies the
   label string before egui sees it; widgets that rely on label
   width for sizing (e.g., the distsummary fixed-width row column)
   may need adjustment.

7. **No way to fetch the outer `egui::Window` rect** (title bar +
   frame), only the inner content rect via `captureUiRect`. For the
   bezier connector this means endpoints land at the content edge,
   not the window edge. A `captureWindowRect(windowId)` op would
   close the gap by reading `egui::Memory::area_rect(layer_id)` for
   the named window.

8. **Inspector lifecycle vs. captured rect lifetime.** When an
   inspector is closed (popup dismissed), its captured rect stays in
   the StateManager map until the next Sync drains and refills it.
   Stale rects for one frame after dismissal. Currently invisible
   because the consumer (the bezier connector) only renders when both
   endpoints are present; pathological cases may surface.

9. **The Source[T] type parameter is unused in today's call sites.**
   Migrated inspectors (fsmview, distsummary) don't actually consume
   `Source[T]` — they hold their value internally and only adopt
   `Provenance`. The Source interface exists for the next inspector
   that genuinely wants the bus-binding bridge; until then it's
   anchor-only.

## Status

Proposed — awaiting review.

Status lifecycle: `Proposed → Accepted → (Deferred | Deprecated | Superseded by ADR-XXXX)`. See boxer's `DOCUMENTATION_STANDARD.md` §1 ADR for the edit-policy tiers (Tier 1 in-place / Tier 2 dated `## Updates` entry / Tier 3 new superseding ADR).

## Updates

### 2026-05-25 — `AnchorChevron` retires in favour of an accent-coloured `arrow-square-out` toggle

The M1 `AnchorChevron = " ▸"` shipped and read as ornament, not as a system affordance. Operators looking at a multi-pane screenshot couldn't tell at rest that the chip's `↳`, the chevron's `▸`, and the bezier tether were three projections of one composable infrastructure — keelson's orthogonal-`Source[T]`-inspectors-over-bus-subjects thesis was true under the hood but had no witness at the glass layer. The chevron's unicode dingbat carried no colour, no shape vocabulary, and no visible relationship to the tether — a reader of a static screenshot saw four inspectors with one anonymous mark each and could not infer the system behind them.

This amendment redirects the disclosure affordance:

- **Glyph.** `arrow-square-out` (Phosphor ``, `keelson/runtime/icons.PhArrowSquareOut`) — the standard "open in another surface" convention from IDEs and web UIs; the diagonal vector reinforces what the bezier visibly shows, that the level-2 surface lives elsewhere.
- **Colour.** `styletokens.AccentDefault` — the same token the bezier-connector overlay uses for its curve and endpoints (`demo/apps/widgets/egui2_hl_bezier_connector_demo.go:183`). Toggle and tether share one hue so a screenshot reads "this control connects to that window" without any caption.
- **Interaction.** Click toggles the level-2 popup pinned-open state; hover preserves the existing transient-popup flow. State is widget-scoped via standard `SendRespVal` per ADR-0013; no new databinding contract.
- **API shape.** `AnchorChevron` constant retires; `inspector.AnchorToggle(ids, *bool)` becomes the helper that emits the toggle and returns the click response. distsummary/internal.go is the only external call site requiring migration; fsmview's existing chevron opt-out carries over (its IDS-accent state chip remains a sufficient affordance, per the original Consequences §).

**Reversal of the Alternatives § Phosphor rejection.** The original ADR rejected Phosphor on three grounds, each of which still holds locally but each of which optimised the wrong global:

- *"No chroma per design-system restraint."* The restraint goal is to keep most surfaces calm so accent affordances *mean something*. A single accented toggle per inspector — one of the smallest controls on screen — falls well inside that envelope, and the colour-tethering payoff justifies the chroma.
- *"Unicode survives the SVG screenshot exporter."* Phosphor is already wired through that exporter via the fsmview state chips and the ADR-0044 iconography landing. No new export path is introduced.
- *"One glyph per vocabulary — `▸` pairs with chip `↳` as one idiom."* The chip's `↳` continues to pair, now with `arrow-square-out` instead of `▸`. The "source-binding glyph + drill-in glyph" idiom stays; the drill-in glyph just stops being a unicode dingbat indistinguishable from a list bullet.

**Vision alignment.** Keelson's compositional and orthogonal story (one `Source[T]` interface, one `Provenance` card, one disclosure affordance, all reusable across inspectors) had no witness at the glass layer — operators saw four widgets with one dingbat each. The accented toggle is the witness: same colour as the tether, same Phosphor surface as fsmview's chips, same shape across every inspector family. A reader who sees one inspector immediately reads "there is a system here, and that line goes somewhere"; the infrastructure ADR's claim and the screen now agree.

**Implementation status.** This Updates entry is the design pivot; the code change (constant retirement, helper introduction, distsummary migration) lands as a follow-up commit. `status` stays `proposed` until the M2 anchor toggle ships and the combined design earns a single human-review pass.

### 2026-05-25 — Shared source-of-truth sketch: inspectors read, never re-accumulate

distsummary's level-2 body grew a second renderer (ECDF + simultaneous confidence band, default tab) alongside the existing boxenplot tab. Both bodies plus the level-1 5-number-summary label now consume the same distribution. The implementation revealed a structural rule the original ADR left implicit, and that future multi-view inspectors will trip on without an explicit statement:

**Rule.** A value inspector binds to **one** source-of-truth sketch / value / aggregate. Every view derives by reading that single state; widgets never accumulate a private copy, never push observations into an internal sketch, never maintain a parallel statistic per tab. The caller owns the sketch; the inspector is a read surface over it.

**Why.** A widget that secretly accumulates its own t-digest (or running mean, or histogram) per tab introduces three failure modes none of which surface in code review:

- **Drift.** Two parallel sketches diverge under concurrent pushes — the level-1 label and the level-2 plot disagree about the same distribution. The reader trusts both because they live in the same widget; the disagreement is silent.
- **Cost.** Per-frame allocation balloons by N (one sketch per view) for a quantity that has no semantic reason to multiply. A 16-core sccmap with one distsummary per core × 3 internal sketches per inspector is 48 sketches for what is conceptually one distribution.
- **Lifecycle confusion.** "When does the widget's sketch reset?" becomes a question with no good answer. The caller's sketch has an obvious owner; a widget-private sketch has none.

**How to apply.** Inspectors take the caller's `*Sketch` (or `Source[T]`, or analogous handle) and thread it through every view function as a pointer / handle. Per-frame extraction is fine and expected:

- **distsummary level 1** (`computeFiveNumberSummary`) — `Count` + `Min` + `Max` are O(1) field reads on `*tdigest.TDigest` (boxer `tdigest.go:92,99,107`); `Quantiles([0.25,0.5,0.75])` calls `compress()` once and loops, per the in-source comment at `query.go:85` ("flushes the buffer once").
- **distsummary boxenplot tab** (`renderBoxenplotBody`) — `letterval.RecommendedLevels(digest)` reads quantiles for the depth implied by `Count()`; bounded by `RecommendedDepth(n)`, typically ~7 levels.
- **distsummary ECDF tab** (`renderEcdfBody`) — `ecdfdigest.BuildDigestGrid(digest, 128)` evaluates `digest.CDF(x)` at 128 grid points. Each `CDF` calls `compress()` (boxer `query.go:99`); after the first call within a frame the rest are no-ops since no observations are pushed mid-render.

All three views read the same `*tdigest.TDigest` pointer. The caller (imztop, sccmap, demo) owns the push side; the widget owns nothing accumulator-shaped.

**Cache across frames where the library supports it.** `ecdfbands.BandsForGrid` is keyed on `(n, α, method)` (boxer `invert.go:19`); the Moscovich-Nadler inversion runs once and the cached band is reused on every subsequent frame until the digest grows, alpha changes, or the band family changes. The widget does nothing to enable this — it just calls the library — but inspectors with their own expensive per-frame computation (custom kernel densities, layout solves) should follow the same pattern: cache by the smallest hash of inputs that pin the output, never recompute on every Sync.

**Active-tab-only rendering.** When a level-2 body has multiple views (tabs, accordions, switch), only the active one runs per frame. distsummary's `renderLevel2Body` dispatches on `state.tab` and never calls both `renderEcdfBody` and `renderBoxenplotBody` in one frame — the fallback path inside the ECDF branch only fires when the ECDF cannot render (Count==0 or Min==Max), and the boxenplot then takes its place for that single frame. Inactive tabs cost nothing.

**Redundancy is acceptable when it's O(1) and obvious.** `renderEcdfBody` re-reads `Count` / `Min` / `Max` even though level 1 already touched them; these are O(1) field reads so the cost is invisible, and threading the precomputed `fiveNumberSummary` into every helper would couple the bodies to a level-1 byproduct they don't otherwise need. Optimise only when the redundancy is in a flushing path (`Quantile`, `CDF` on a digest with pending pushes) or in a bounded-but-non-trivial step (band inversion, layout solve).

**Anti-pattern (forbidden).** Any of:

- A widget-private sketch / accumulator field on a `Renderer` struct, fed by a `Push(...)` method on the widget.
- A `New(digest *Sketch)` constructor that **copies** the digest into widget-local storage.
- A second sketch per tab / view, with cross-tab "sync" code reconciling them.
- A per-frame `digest.Clone()` to "stabilise" the view — clones defeat both the cache and the single-owner story.

The screening question at design time is: *"If the caller pushes an observation between two views' reads in the same frame, do both views see it?"* If the answer is "no", the widget has forked the state and the design is wrong.

**Scope.** The rule applies to value inspectors in the ADR-0046 sense — widgets that bind to a domain value (sketch, FSM, error chain, typed record) and surface it for human exploration. Layout-state widgets (DockArea, ScrollArea) and timing-state widgets (animation drivers) are out of scope; they own their own state by definition.

**Status.** This Updates entry codifies a rule the ADR's original Decision § implied but did not name. It does not change any shipped widget — fsmview already reads `Machine[T]` without cloning, distsummary already reads `*tdigest.TDigest` without cloning, fieldview / errorview hold the caller's value by pointer. The entry exists so the next multi-view inspector author cannot reinvent a private accumulator and call it "encapsulation".

### 2026-05-26 — Bezier tether animates: spring-driven interior control points, fixed endpoints

The M2 bezier connector landed as a static geometric S-curve recomputed each frame — accurate, but it read as a hardcoded overlay rather than a system that knows the window and toggle are *connected*. Drag the window and the line snapped rigidly with it. The amendment routes the curve through a per-axis mass-spring-damper so the rope's belly trails and overshoots when an endpoint moves, then settles. The visual contract ("this line goes from here to there") is preserved frame-by-frame: only the curve's interior wobbles.

**What springs.** The two interior cubic-bezier control points p1 and p2 only. Their targets are the geometric S-curve positions `(fromX + tangent, fromY)` and `(toX - tangent, toY)` recomputed from the current toggle/window rects every frame. The bezier's p0/p3 endpoints and both `PaintCircleFilled` endpoint dots ride the raw targets — they need to read as anchored to their widgets. The rope-tied-between-two-pegs feel is the design intent; springing the endpoints too made them visibly leave the toggle/window edges during motion and broke that feel.

**Tuning.** `tetherSpringK = 200` (stiffness, 1/sec²), `tetherSpringC = 11` (damping, 1/sec). Damping ratio ζ ≈ 0.39, ω ≈ 14.1 rad/s, first overshoot ≈ 25 %, two to three visible oscillations, settles in ≈ 1.0 sec. Underdamped on purpose; the rubbery feel was the point. Stability bound `ω·dt < 2` has an order of magnitude of headroom at 60 Hz. Constants live in `anchor.go` for downstream taste-tuning; no new API.

**Snap path.** Three conditions bypass integration and write target → position with zeroed velocity: (a) first valid paint (state uninitialized), (b) wall-clock gap > 100 ms since the previous paint (inspector was closed for a while and reopened), (c) target jumped > 200 px from the current spring position (panel reflow, large window reposition, fast mouse fling). Without these, the simulation would sweep visibly from `(0, 0)` on first open, integrate one huge step on resume, or animate across the screen on reflow.

**Reduced-motion compliance.** A final-pass snap when `styletokens.MotionEnabled()` is false overrides the integrated pose so tour conformance captures and OS reduced-motion mode see the instantaneous geometric curve. Simulation state still updates each frame so motion can be re-enabled mid-session and the spring resumes from the displayed pose rather than a stale lagged one.

**State location.** Per-tether sim state lives in a package `sync.Map` keyed by `toggleSeq` (already derived from scope by [`NewAnchorTether`]). Mirrors the R21 rect-capture storage pattern — `AnchorTether` stays value-typed and the only caller (distsummary) is untouched. 48 bytes per active tether (eight `float32`s + an `int64` + a `bool`); zero per-frame allocations.

**Cost.** ~50 ns + ~50 FLOPs per tether per frame. Bezier paint command dominates by ~1000×. Concrete profiling not warranted.

**Status.** Shipped as commit `0b81717a` (single-file change in `widgets/inspector/anchor.go`). ADR `status` remains `proposed` — this is a behaviour amendment to a primitive the ADR already names, not a new decision.

### 2026-05-31 — fsmview becomes first-class **tethered**

The 2026-05-25 toggle update kept fsmview opted out of the disclosure affordance — its accent state chip was "a sufficient affordance", so an fsmview popup opened on a chip click and floated free, with no `AnchorToggle` and no bezier back to its origin. That held while an FSM chip was a bare status indicator. play's query-result FSM (an [ADR-0045](0045-imzero2-fsmview-widget.md) consumer) changed the calculus: promoted to a status-bar **summary** (state badge + result stats — rows / elapsed / age, and the empty / stale / error message), it wants the same "this summary ↔ that window" legibility the distsummary / regexsummary inspectors already get from the tether. So fsmview adopts the pattern — as an **opt-in** mode named **tethered**.

**API.** `fsmview.Widget` gains three chainable builders, all no-ops unless opted in:

- `Tethered()` — switch the level-1 chip to a tethered inspector summary: the state badge gains an `inspector.AnchorToggle` and the level-2 window is linked back to it by the spring `inspector.AnchorTether` — `CaptureToggle` in the chip row, `CaptureWindow` at the top of the window body, `Paint` after the window. The pinned window is `AlwaysOnTop` (the `PaintAbsoluteOverlay` bezier is foreground regardless, but the *window* it points at must stay foreground too, or it falls behind the panes it's anchored from — the same `AlwaysOnTop` distsummary / regexsummary set). This is the exact distsummary / regexsummary recipe; the tether's scope is the widget's existing `scopeKey`.
- `Summary(func())` — a caller-owned addendum rendered just right of the badge (the stats line).
- `BadgeTone(func(T) badge.ToneE)` — colour the badge by state severity. Orthogonal to tethering; applies to plain chips too.

**Default unchanged.** Non-tethered widgets — including play's own projector FSM — keep the plain badge-click popup; every tethered branch is gated on the opt-in flag. So this *extends* the shipped behaviour rather than breaking it: the 2026-05-25 opt-out remains the default, and `Tethered()` is the door out of it for inspectors that want the connector.

**First consumer.** play's query-result FSM (`app.play.query.result-state`): the status bar now reads `[state badge] N rows · 12ms · 8s ago [↗]`, and the `↗` toggle pops the bezier-tethered graph / history / provenance window. "Tethered" is the word for this composition across the ADR, the code comments, and the `Tethered()` API.

**Status.** `status` stays `proposed` — a widget gaining an opt-in mode over primitives the ADR already names, not a new decision.

## References

- [ADR-0026 — app runtime + cap subjects](0026-app-runtime-and-capability-subjects.md) — source of the `app.<id>.event.<name>` subject convention used in `Provenance.Subject`.
- [ADR-0029 — design system + policy-as-code](0029-imzero2-design-system-and-policy-as-code.md) — restrained Swiss aesthetic the chip + chevron follow.
- [ADR-0031 — IDS colour foundations](0031-imzero2-design-system-color.md) — `NeutralTextSecondary`, `AccentDefault` tokens used by the chip and bezier overlay.
- [ADR-0036 — runtime buscodec](0036-runtime-buscodec.md) — codec the future `LiveSource[T]` will use to decode payloads.
- [ADR-0044 — iconography](0044-imzero2-design-system-iconography.md) — Phosphor catalogue (alternative considered for the disclosure cue).
- [ADR-0045 — fsmview widget](0045-imzero2-fsmview-widget.md) — the first widget to migrate to this infrastructure.
- `src/go/public/thestack/imzero2/egui2/widgets/inspector/` — implementation.
- `src/go/public/thestack/imzero2/egui2/demo/apps/widgets/egui2_hl_bezier_connector_demo.go` — bezier-connector PoC that exercises both new FFFI2 primitives and the full chip + chevron vocabulary.
- Commits: `2443eb0f` (FFFI2 primitives + PoC), `f5012da9` (inspector package + fsmview/distsummary migration), `9c68fbda` (AnchorChevron + distsummary application).
