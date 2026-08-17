---
type: explanation
audience: package maintainer
status: draft
# reviewed-by: "@<handle>"     # fill in and uncomment when flipping to stable
# reviewed-date: YYYY-MM-DD    # fill in and uncomment when flipping to stable
---

> **Status: draft — pre-human-review.** Compiled 2026-07-31 as a companion to
> [the app-composition survey](./app-composition-survey.md); nothing here is a
> decision, and an ADR — not this page — is where any of it would become one.
> Provenance is three-tiered: (a) claims about this repository were verified
> against the working tree on the compile date; (b) the runtime behaviour in §2
> was *measured* on the compile date by running the widget gallery on one
> headless box (demo tour + live driving via egui-mcp) — one build, one
> machine, one host context, so read it as an observation, not a proof; (c)
> upstream figures come from the vendored `egui-snarl` 0.11.0 source. Effort
> and line-count estimates are estimates.

# Porting `egui-snarl` — what it would take, and what it would buy

## 1 Question and scope

[ADR-0021](../adr/0021-imzero2-snarl-node-editor-binding.md) bound the
`egui-snarl` crate in 2026-05 as imzero2's node editor, deliberately choosing a
crate binding over a port (its O4, "ground-up on the ImZero2 painter", was
rejected as an order of magnitude more work). Three months and a good deal of
substrate later, [ADR-0149](../adr/0149-implot-core-port-painter-lane.md) made
the opposite call for charting and completed it inside a month: the ImPlot core
now lives in Go on the painter lane and the `egui_plot` crate is gone.

The question here: **should the node editor make the same move, what would it
cost, and what would it unlock** — asked because the composition survey's
ports-and-wires shape (§7) and its S1 "descriptive composition graph" step both
name the snarl binding as the machinery already in stock.

"Port to keelson and/or imzero2" resolves into three different things; keeping
them apart matters, because only two of them are a port:

- **imzero2, Rust side** — replace the crate with a first-party Rust module
  inside `rust/imzero2`, keeping the renderer where it is today.
- **imzero2, Go side** — a widget under
  `public/thestack/imzero2/egui2/widgets/`, drawing on the painter lane. This
  is the ADR-0149 shape and what "port" means for the rest of this page.
- **keelson** — the app layer. Nothing node-editor-shaped belongs in
  `public/keelson/` itself; what keelson would gain is a *consumer* (a
  composition-graph app over the facts plane, survey §10 S1). That consumer
  needs a widget either way, so the keelson question is downstream of the
  imzero2 one, not parallel to it.

## 2 The starting point is worse than the inventory suggests

The composition survey lists the node editor under "rendering machinery already
in stock" (§2.7). That entry is accurate about the *binding surface* and too
generous about what it renders.

### 2.1 Adoption: one consumer, and it is the demo

`SnarlEditor` / `SnarlNode` / `SnarlPin` / `SnarlConnection` /
`FetchSnarlEvents` have exactly one caller in the tree:
`egui2/demo/apps/widgets/egui2_hl_snarl_demo.go`. No app, no widget, no play
panel. For contrast, the `egui_plot` bridge that ADR-0149 replaced had roughly
eight call sites across several apps and a widget at the time it was retired.

### 2.2 What it actually renders (measured 2026-07-31)

Driving the `snarl` gallery demo live (headless compositor, egui-mcp
driving) and capturing the same demo through the TestDriver tour (8 settle
frames per demo) gives the same picture from two different hosts:

- The background grid draws. Its diagonal look is upstream's default
  (`DEFAULT_GRID_ANGLE = 1.0` rad), not a defect.
- Node frames draw, and per-node deferred bodies (the `Number` node's value
  editor, the `Sink` node's label) draw inside them.
- Interaction and the event lane work: dragging a node header produces
  `NodeMoved` events that reach Go and update the demo's model.
- **No wires are visible for the three declared edges** — not at the initial
  view, not after dragging the nodes apart. They are, however, *generated*:
  the SVG export of the same frame carries exactly three stroked polylines in
  the pin colour (upstream paints wires as sampled `Shape::line`, not
  `Shape::CubicBezier`, and the exporter handles both). So the defect is in
  what reaches the framebuffer, not in wire generation — either the recorded
  sublayer issue (§2.3), or simple occlusion by the oversized node frames
  below, or both.
- **Pins are missing on the nodes that have only pins** (`add`, `result`);
  the only pin circles that appear sit next to the nodes that also have
  bodies.
- **Node geometry does not agree with the Go coordinate space.** Node frames
  are drawn far wider than their content, so nodes declared 190 px apart
  overlap heavily; a drag moving the pointer 340 px reported a model delta of
  197 units on one axis and a differently-scaled delta on the other. Text
  inside the editor renders at roughly 1.7× the surrounding UI, which is
  consistent with content being measured at one scale and painted at another.
- **Hit-testing follows the wrong rects**: with the nodes piled up, drags
  aimed at the visible `n2` and `result` headers were swallowed; only the
  top-most node responded.

The net user-visible result is a grid with a few overlapping boxes and no
wires — i.e. the one feature that defines a node editor never reaches the
screen, even though the shapes for it are produced.

### 2.3 Two structural mismatches that plausibly explain it

Neither is a mystery; both are already written down in the tree:

- **Multipass is pinned off.** `apphost.rs` sets `max_passes = 1` because the
  FFFI opcode stream is consumed by the first pass, so a re-run pass draws
  nothing. Its own comment names `egui_snarl`'s `SnarlState` / `NodeState`
  first-frame fitup as the machinery that wants multipass, and argues the
  state stored via `cx.data_mut` makes later frames converge anyway. The
  measurements above are from frame 8 and later, so if that convergence
  argument holds, something else is also wrong.
- **The Scene + sublayer path is a known-bad interaction.** The apply in
  `interpreter.rs` (`render_snarl_editor`) carries a "KNOWN ISSUE" comment
  saying snarl's `Scene` + `set_sublayer` painting does not reach the
  framebuffer in this pipeline, with diagnosis deferred. `svgexport.rs`
  independently special-cases `set_transform_layer` layers to export them at
  all. Transform layers are the mechanism snarl uses for zoom, and zoom is
  where the geometry disagreement shows up.

ADR-0021's 2026-07-08 Update records the demo as "re-verified rendering" after
the egui 0.35 / snarl 0.11 bump. That is consistent with what is on screen
(it is not blank) but reads more favourably than the current behaviour
warrants. Either outcome of the decision below wants a dated Update on
ADR-0021 recording what the binding does today.

### 2.4 What the binding costs to carry

Roughly 750 lines of hand-written Rust in `interpreter.rs` (per-frame
accumulators, retained `SnarlState` with the two-way id maps, the
`FffiSnarlViewer` delegate, the reconcile-and-render apply, the event fetcher,
four opcode dispatch blocks), 299 lines of IDL, 85 lines of Go bindings, one
Cargo dependency, and a demo. Removing it is a subtraction of about the same
size as one milestone of a port.

## 3 What "port" can mean here

Four options, sketched at QOC depth — an ADR would do this properly.

- **P0 — do nothing more.** Leave the binding as-is; serve the survey's
  read-only S1 with `layeredgraph` (which already has real consumers) and
  revisit if a prescriptive-wiring consumer ever appears.
- **P1 — repair the binding in place.** Diagnose the sublayer/transform
  interaction and the sizing mismatch; keep the crate. Cost is genuinely
  unknown until the diagnosis exists — it lands somewhere between a day and
  "the FFFI protocol cannot host a transform-layer widget", and the second
  outcome is not implausible given that the deferred-block replay re-enters
  the interpreter inside a scaled child `Ui`.
- **P2 — port to Go on the painter lane.** The ADR-0149 shape. Go owns
  topology *and* geometry; Rust draws primitives. Precedent for the doctrine
  is older than ADR-0149: [ADR-0069](../adr/0069-imzero2-layeredgraph-widget.md)
  already moved graph layout host-side after concluding `egui_graphs` capped
  rendering quality, with "only coordinates cross the FFI".
- **P3 — first-party Rust module in imzero2.** Drops the crate but keeps the
  renderer in Rust. It inherits the same egui-model constraints that appear to
  be causing the trouble (this is the layer where transform layers, multipass
  and sublayers live), and it accumulates logic on the side of the boundary the
  architecture keeps thin. Hard to motivate unless the diagnosis in P1 shows
  the problem is snarl-specific and cheaply avoidable in Rust.

| | P0 keep | P1 repair | P2 Go port | P3 Rust rewrite |
|---|---|---|---|---|
| Cost now | none | unknown | high (§7) | high |
| Risk retired | none | some | the whole class | little |
| Fits Go-first doctrine | n/a | no | yes | no |
| Typed pins (survey S7) | no | still deferred | designed in | designed in |
| Bodies as embedded app surfaces (survey §4) | unclear | unclear | explicit (§5) | possible |
| Dependency surface | +1 crate | +1 crate | −1 crate | −1 crate |

## 4 Substrate check for a Go-side port

ADR-0149 needed an M0 milestone of new painter primitives before the port could
start. The notable finding here is that **a node editor appears to need no new
IDL at all** — largely because ADR-0149's M0 and
[ADR-0140](../adr/0140-imzero2-hover-scoped-wheel-capture.md) already landed the
pieces.

| Need | Primitive | Status |
|---|---|---|
| Wires | `PaintCubicBezier` (4 control points, colour, width), `PaintDashedLine`, `PaintPolyline` | in stock |
| Node frames, headers, pins | `PaintRectFilled`/`Stroke`, `PaintRectsFilled` (batched), `PaintCircleFilled`/`Stroke`, `PaintPolygonFilled`, `PaintArrow`, `PaintMarkers` | in stock |
| Per-node / per-area clipping | `PaintClipPush` / `PaintClipPop` (ADR-0149 M0) | in stock |
| Labels | `PaintText` | in stock |
| Node sizing from text | `MeasureText` / `MeasureTextSize` + the `…Bind` databinding wrappers | in stock, one frame late |
| Hover / click / drag per node and per pin | `PaintSenseRegion` + R7 flags | in stock |
| Drag deltas, press origin, modifiers | R24 per-canvas pointer rows (`FetchR24CanvasPointers`) | in stock |
| Wheel zoom with anchor, scoped to the canvas | R23 (`CaptureZoom` / `CaptureScroll`, ADR-0140) | in stock |
| Real widgets at a computed rect | `AllocateUiAtRect` | in stock |
| Overlay above windows (drag ghosts, tethers) | `PaintAbsoluteOverlay` + `CaptureUiRect` (r21) | in stock |
| Dormant heavy bodies | `widgets/lazypane` | in stock |

What is *not* in stock, and what each gap forces:

- **No `egui::Scene` / transform-layer binding.** Painted geometry can be
  scaled trivially (the widget owns the transform in Go), but **real egui
  widgets cannot** — there is no way to render a `TextEdit` at 0.4× today. This
  is the single load-bearing constraint, and §5 is about what to do with it.
  Note the survey's F6 asks the Scene question from the other end.
- **Text measurement is frame-lagged.** Node sizes settle a frame after their
  content changes; the fix is the same caching idiom the existing widgets use
  (measure once per distinct string, keyed by content hash).
- **No context menus on the paint lane.** Right-click menus (snarl's node/pin
  menus) need a real widget popup anchored at a canvas position — the
  `PaintAbsoluteOverlay` + captured-rect idiom from the bezier-connector demo
  is the closest existing pattern, and menus are deferrable chrome regardless.
- **Hit-test priority is emission order.** ADR-0149's M7 lesson (later regions
  win, so the plot area swallowed legend clicks) applies directly: pins must be
  emitted after node bodies, wires after both, or they are dead.
- **Id discipline is not optional.** Multi-child widgets must scope the id
  stack, and duplicate ids now fail silently rather than colliding loudly.

## 5 The one design decision that matters: where node bodies live

Everything else in a node-editor port is ordinary widget work. This is the
choice that determines what the widget can be used for.

- **B1 — paint-only bodies.** Node content is painted (text, values, small
  sparklines), never real widgets. Zoom is free and exact at any scale, the
  whole editor is one canvas, and the implementation is the simplest of the
  three. It serves the survey's S1 descriptive graph completely and its
  prescriptive-wiring S7 mostly (a wire editor needs pins and menus, not text
  fields inside nodes). It cannot host a live app surface in a node.
- **B2 — real widget bodies at 1:1, LOD tiers below (recommended).** Bodies
  render through `AllocateUiAtRect` only while the viewport scale is at or near
  1×; below that the node falls back to painted tiers (summary line → title bar
  → icon). This turns the missing transform binding from a limitation into the
  survey's §8 semantic-zoom design, which the literature argues for on its own
  merits — geometric shrinking of a full app body is illegible anyway. It also
  lines up with the survey's S2 embed seam: the tier where a node body is "an
  app's `Frame()`" is exactly the tier where 1:1 is the only sensible scale.
- **B3 — bind `egui::Scene` and keep bodies inside it.** Real widgets scale.
  It also re-enters precisely the transform-layer/sublayer regime that appears
  to be breaking the current binding (§2.3), and it puts a container back on
  the Rust side that the port existed to avoid. Not obviously wrong — but it
  should follow a diagnosis, not precede one.

B2 is the recommendation, with B1 as its own first milestone: B1 *is* M1 of
B2, so choosing B2 costs nothing up front and can be re-decided at M3.

## 6 What already exists to build on

A Go-side port does not start from zero; the read-only half is largely written,
twice:

- **`widgets/layeredgraph/view`** (343 lines) already paints nodes, cubic-Bézier
  edges with synthesized arrowheads and labels into a `PaintCanvas`, fits them
  to the viewport, and handles wheel zoom (R23) plus drag pan (R24) with one
  sense region per node for hover/click. Its consumers are play, godepview and
  capinspector.
- **`widgets/pipelineview`** (model + layout + painter view, ~1.5k lines) adds
  a deterministic layout engine, typed port classes (its "shelf rule"), and
  orthogonal edge routing with track assignment.
- **`widgets/implot`** (~3.4k lines) is the reference for the interaction
  discipline a canvas widget needs: pixel-space gestures, per-canvas pointer
  registers, sense-region ordering, clip stacks.
- **`widgets/kanban`** is the reference for drag-and-drop over real widgets
  (`CaptureUiRect` + pointer registers + an absolute overlay for the ghost).

That is also an argument the port has to answer honestly: the tree would then
hold four node-and-edge renderers (`egui_graphs` binding, `layeredgraph`,
`pipelineview`, plus the new one). Either the port shares a geometry/routing
core with them, or it earns its separateness on interaction alone.

## 7 Cost and milestones

Upstream `egui-snarl` 0.11.0 is 6.4k lines of Rust excluding its example:
`ui.rs` 2610 (layout, draw, interaction), `ui/wire.rs` 1330 (wire geometry,
hit-testing, pin shapes), `lib.rs` 836 (the graph container), `ui/state.rs` 624
(viewport, selection, in-flight wires), `ui/viewer.rs` 355 (the trait),
`ui/pin.rs` 296, plus effects, background and scale helpers.

A Go port carries less than that: the graph container is the consumer's
already (topology is Go-authoritative today and would stay so), the viewer
trait becomes an interface or closures, and `SnarlStyle`'s serde/probe
plumbing collapses. Applying ImPlot's observed ratio (12.6k C++ → ~3.4k Go
including tests) gives **≈1.8k–2.8k lines of Go for a core** plus demo — one
`pipelineview`, not one `implot`.

Milestones, each independently useful (descope-over-gate):

- **M0 — substrate: expected to be empty.** Re-verify against §4; the only
  plausible additions are a batched line/bezier opcode if wire counts demand
  it, and a canvas-anchored menu affordance, both deferrable.
- **M1 — read-only node/pin/wire renderer** with fit, pan, anchored zoom,
  hover and click, starting from `layeredgraph/view`. ~600–900 lines.
  *Unlocks survey S1.*
- **M2 — editing**: node drag (with the Go-authoritative / widget-persisted
  choice ADR-0021 SD6 already framed), selection including box-select, wire
  hit-testing and deletion, and connect/disconnect drags **with typed-pin
  validation** — the thing ADR-0021 deferred at SD8 and never revisited.
  ~700–1000 lines. *Unlocks survey S7's prescriptive half.*
- **M3 — bodies and LOD tiers** per §5 B2. ~300–500 lines. *Feeds survey S5
  and the S2 embed seam.*
- **M4 — chrome**: context menus, collapse/expand, minimap, undo (cheap when
  Go owns topology: replay the event log backwards). ~400–600 lines.
- **M5 — consumers, then retire ADR-0021's binding** (IDL, Rust apply,
  fetcher, crate), with a dated ADR Update recording the supersession.

Two sequencing notes. First, unlike the ImPlot migration there is no adoption
to migrate — the binding's only consumer is its own demo — so the coexistence
period ADR-0149 SD7 had to manage does not exist here; the binding could even
be removed *before* the port lands, on the strength of §2 alone. Second, M1
alone may not need a new widget at all: extending `layeredgraph/view` with pin
geometry and a wire layer is a smaller, more honest first step, and it defers
the "is this one widget or two" question until the editing half proves it.

## 8 What the composition survey actually needs

Mapping the survey's sequencing (§10) onto the milestones above:

- **S1 — descriptive composition graph** needs M1 only, and could be served by
  extending `layeredgraph` instead. It does not need an editor.
- **S3 — canvas of applets** does not need a node editor at all.
- **S4 — workflow conductor** does not either; a stepper is not a graph view.
- **S7-style prescriptive wiring** (survey §7's expensive half) is the only
  shape that genuinely needs M2 — and specifically needs typed pins, since a
  wire that executes a grant or a launch must refuse to connect incompatible
  ports. The current binding cannot express that (SD8).
- **S5 — semantic zoom tiers** needs M3, and is where B2's LOD contract and
  the survey's per-participant LOD capability interface are the same design.

So the port's critical path for composition work is M1 → M2, with M3 arriving
alongside the survey's own S5. That also means the port is not on the critical
path for the survey's *first* two steps, which is an argument for sequencing it
behind S1/S2 rather than in front of them.

## 9 Risks and named non-goals

- **Wires as a programming language.** The survey's §7 trap applies to the
  widget too: SQL stays the fine-grained composition language; wires connect
  participants. A node editor that grows expression nodes has drifted.
- **Readability at scale.** The genre's recurring finding is that patch
  editors degrade into spaghetti past a few dozen edges, and every mature
  system grew subpatches. A port should assume grouping/collapsing is a
  requirement, not polish — M4, not "someday".
- **Per-frame paint volume.** Every node, pin and wire is opcodes on the wire
  each frame. Viewport culling and LOD are the mitigations, and both are
  cheaper here than in the crate because Go knows the geometry.
- **Layout authority.** ADR-0021's SD6 (Go-authoritative by default, opt-in
  widget persistence) is a sound framing to keep; a port makes the default
  cheaper because there is no cross-language round trip.
- **Provenance and licence.** `egui-snarl` is MIT OR Apache-2.0, so a port is
  a licence-clean derivative work exactly as ImPlot's was: carry the upstream
  licence text and attribution in-package (ADR-0149 SD8), with authorship
  provenance in git trailers per
  [ADR-0083](../adr/0083-retire-llm-generated-build-tags.md), never in-file
  markers. This is the permissive-source path, not `pipelineview`'s
  clean-room protocol (ADR-0119 SD6), which exists for copyleft sources.
- **Not a node-editor framework.** Go owns topology; the widget renders and
  reports. Nothing in the port should acquire its own graph model.

## 10 Open forks for a design dialogue

- **F1 — the binding's fate, decided independently of the port.** Delete it now
  (zero consumers, ~1.1k lines of IDL+Rust+bindings, one crate), or keep it
  pending a P1 diagnosis? Keeping a widget that does not draw wires is a
  standing misrepresentation in the demo gallery and in ADR-0092's
  status-versus-evidence view.
- **F2 — P1 first?** A time-boxed diagnosis of the sublayer/transform
  interaction has value beyond snarl: it answers whether *any* transform-layer
  widget can live under the FFFI protocol, which is the same question the
  survey's F6 (bind `egui::Scene`) has to answer.
- **F3 — body hosting (§5).** B1 / B2 / B3, and whether M3 is in the first
  cut at all.
- **F4 — one widget or an extension.** New `nodeeditor` widget, or pins and
  wires grown inside `layeredgraph` with the editor as a mode?
- **F5 — shared core.** Is there one geometry/edge-routing core under
  `layeredgraph`, `pipelineview` and the editor, or do they stay three
  renderers with three layout models?
- **F6 — pin typing vocabulary.** The survey's §3 "port" notion (LaunchKind,
  cap subjects, dataset aliases, workingset kinds) is a natural pin-kind
  vocabulary. Designing the widget's pin types against it — rather than an
  opaque `u32` — is what would make M2 usable for composition rather than
  generically node-editor-shaped.
- **F7 — when.** The port is not on the critical path for the survey's S1–S4.
  Doing it early buys a better S7 and retires a broken widget; doing it late
  risks S1 hardening around `layeredgraph` in ways the editor then has to
  fight.

## References

Internal: [ADR-0021](../adr/0021-imzero2-snarl-node-editor-binding.md),
[ADR-0069](../adr/0069-imzero2-layeredgraph-widget.md),
[ADR-0119](../adr/0119-imzero2-pipelineview-widget.md),
[ADR-0140](../adr/0140-imzero2-hover-scoped-wheel-capture.md),
[ADR-0149](../adr/0149-implot-core-port-painter-lane.md),
[app-composition-survey](./app-composition-survey.md).

Code touched by this analysis (the three snarl files were removed 2026-08-17 by
[ADR-0194](../adr/0194-retire-egui-snarl-binding.md); read them at a commit
before that date):
`egui2_definition_d_snarl.go`,
`egui2_snarl.go`,
`egui2_hl_snarl_demo.go`,
[`layeredgraph/view`](../../public/thestack/imzero2/egui2/widgets/layeredgraph/view/view.go),
[`pipelineview/view`](../../public/thestack/imzero2/egui2/widgets/pipelineview/view/view.go),
[`widgets/implot`](../../public/thestack/imzero2/egui2/widgets/implot/),
`rust/imzero2/src/imzero2/interpreter.rs` (`SnarlState`, `FffiSnarlViewer`,
`render_snarl_editor`), `rust/imzero2/src/imzero2/apphost.rs` (the
`max_passes = 1` pin).

External: [`egui-snarl`](https://github.com/zakarumych/egui-snarl) 0.11.0,
MIT OR Apache-2.0 (figures read from the vendored source).
