---
type: adr
status: proposed
date: 2026-08-07
# reviewed-by: "@<handle>"     # fill in and uncomment when flipping to accepted
# reviewed-date: YYYY-MM-DD    # fill in and uncomment when flipping to accepted
---

> **Status: proposed — pre-human-review.** Decision under consideration; do not implement as if accepted.

# ADR-0176: A native Go tree widget over etable

## Context

imzero2 has had a tree since the `egui_ltreeview` binding landed. Nothing uses
it. The only two call sites are demos — `egui2_hl_tree_demo.go` and the node
section of `egui2_hl_imzero2_demo.go` — while widgets that display hierarchies
(`fieldview`, `schemaview`, and keelson's `configview`) each hand-roll a
`CollapsingHeader` nest instead. A binding with a demo and no adopter, sitting
beside hand-rolled reimplementations of what it does, is evidence about the
binding rather than about the need.

**M3 correction: three adopters, not four.** This paragraph originally counted
`componentview` as a fourth. It is not a hierarchy — it is a one-level
accordion whose bodies are arbitrary-height widget content supplied by a
registered `RendererI` (its battery renderer draws a 115px radial gauge). A
fixed-height tree row cannot host that, and porting it would mean redefining a
published extension interface for every registered renderer in order to render
a flat list. It stays a `CollapsingHeader` accordion, and M5 is unaffected: it
never used `egui_ltreeview` either.

The binding is a register-drain protocol, not a block
(`definition/egui2_definition_d_blocks.go`). `nodeDir` / `nodeLeaf` /
`nodeDirClose` push into a process-global `r3_node_cmds` vec; `tree` drains it.
Five consequences follow, and together they are the reason for the
non-adoption:

- **Emission is decoupled from placement.** Nodes are queued wherever the Go
  code happens to be; the tree renders wherever `Tree()` lands. The queue is
  global, so two trees in one frame must be emitted and drained in strict
  alternation. The demo carries this as a comment rather than as a guarantee.
- **Expansion state is invisible to Go.** It lives in Rust
  (`TreeViewState::load` / `store`). `NodeDirFluid.SendIter`
  (`bindings/egui2_methods.go:45`) yields unconditionally, so Go walks and
  marshals the entire tree every frame even when every node is collapsed.
  There is no culling, no lazy child fetch, no programmatic expand, and no way
  to persist or restore expansion. The `BLOCK_SKIPPED` flag pushed for closed
  dirs has no reader.
- **A row is a `WidgetText` and nothing else** — no badge, count, secondary
  column, per-row control, or context menu.
- **The action stream is dead.** `Action::Activate` and `SetSelected` are
  commented out in the apply code; only a `NODELIKE_SELECTED` state poll comes
  back, so there is no activate-versus-select distinction.
- **It is one upstream change from breaking.** A `close_dir` → `Rect::clamp`
  panic in 0.7 required a defensive `clip.is_negative()` guard whose failure
  mode is the whole tree silently not rendering.

Meanwhile the `endETable` binding (`definition/egui2_definition_d_table2.go`)
has quietly accumulated most of what a tree needs: visible-range prefetch
(`et.VisibleRange()`), per-row heights via a prefix sum into `row_top_offset`,
`ScrollToRow` with alignment, a selected-row and striping decorator, the
ADR-0151 column-width epoch protocol, and a bounded-height wrap. A tree row is
a table row with an indent and a disclosure glyph, and those two are plain Go
inside a cell.

One gap is real. `egui_table 0.9` added
`TableDelegate::row_ui(&mut Ui, row_nr)` — a `Ui` whose `max_rect` spans the
whole row across all columns, called before that row's cells (`table.rs:207`,
call site `table.rs:711`). The binding's `EtStripedDelegate` does not implement
it, and `logviewer.go:478` pays for the absence: five cells each wrapped in a
`TintedScope`, clicks ORed together, with a per-cell fill chosen so that
"summed across the row, [it] reads as a contiguous outlined row across
egui_table's natural inter-column gutters". That is a workaround for exactly
what `row_ui` provides.

## Design space (QOC)

**Question.** What should render a hierarchy in imzero2?

**Options.**

- **O1** — Keep `egui_ltreeview`, extend the binding to close its gaps.
- **O2** — Native Go, rows composed inside a `ScrollArea`.
- **O3** — Native Go, rows on `endETable`.
- **O4** — Native Go, painted directly onto a canvas the way `icicle` and
  `treemap` are.

**Criteria.**

- **C1 — Per-frame cost at scale.** Whether collapsed or off-screen nodes cost
  anything, measured as deferred blocks built and marshalled per frame.
- **C2 — Row expressiveness.** Whether a row can carry a badge, a count, a
  second column, or a control.
- **C3 — Go-side state ownership.** Whether expansion and selection are
  readable, settable, and persistable from Go.
- **C4 — Driveability.** Whether rows appear in the AccessKit tree, so
  egui-mcp and the ADR-0154 headless driver can address them.
- **C5 — New generated surface.** How much IDL and Rust the option adds.

**Assessment.** `++` strong positive, `+` positive, `−` negative, `−−` strong negative.

|    | O1 | O2 | O3 | O4 |
|----|----|----|----|----|
| C1 | −− | −  | ++ | ++ |
| C2 | −− | ++ | ++ | +  |
| C3 | −− | ++ | ++ | ++ |
| C4 | +  | ++ | ++ | −− |
| C5 | +  | ++ | +  | ++ |

O3 loses to O2 and O4 only on C5, and by one small increment — see SD5.

## Decision

We will add `public/thestack/imzero2/egui2/widgets/tree`, a native Go widget
that flattens a hierarchy to rows and renders them through `endETable`; and we
will first bind `egui_table`'s `row_ui` as a deferred block map so the widget
has full-row interaction from its first frame. Once the widget reaches parity
we will remove the `egui_ltreeview` binding and its crate dependency.

### Subsidiary decisions

- **SD1 — Input is columnar, not a pointer tree.** `Labels []string` and
  `Parents []int32` (`-1` for a root, several roots allowed, no ordering
  required), matching `icicle.Tree`, treemap's `layout.Node`, and
  `apps/play/play_hierarchy.go`. Hierarchies here arrive flat — a recursive
  CTE's rows, a profile's stacks, a schema's fields — and demanding a pointer
  tree would make every producer build one first. A third hierarchy shape in
  this repo would be one too many.

- **SD2 — Expansion, selection and cursor live in a caller-owned `State`.** The
  widget reads and mutates it; it never keeps hidden per-frame state and never
  leaves authority in Rust. This is the kanban / layeredgraph `ViewState` /
  treemap breadcrumb pattern, and it is what makes expansion persistable,
  restorable, and settable from code — the single largest thing
  `egui_ltreeview` cannot do.

  **The cursor is a separate field from the selection**, carried from M1 even
  though nothing moves it until [ADR-0177](./0177-imzero2-focus-scoped-keyboard-capture.md)
  lands. Keyboard navigation needs the distinction every file manager has —
  the row the keyboard is *on* is not the row (or rows) that are *selected*,
  or Shift+↓ cannot extend a range and ↓ cannot pass over a selected row
  without changing what is selected. Adding it later would reshape `State`,
  the flatten's `Row`, and every caller written against the old shape; adding
  it now costs a field and a doc sentence. Until 0177, `Cursor` simply tracks
  the last clicked row.

- **SD3 — Flattening is pure and is where the tests live.** `layout.go` turns
  `(Tree, State)` into `[]Row{NodeIdx, Depth, HasChildren, Expanded, IsLastChild}`
  with no bindings import, mirroring the `model.go` / `layout.go` / render split
  that `icicle`, `sankey`, and `layeredgraph` already use. Every ordering,
  depth, and collapse rule is asserted without a renderer.

- **SD4 — `endETable` is the row engine.** The widget emits one etable row per
  flattened row, gated on `et.VisibleRange()`. Virtualisation, per-row heights,
  `ScrollToRow` for reveal-selection, and the column-width protocol come with
  it rather than being rebuilt.

  **M2 found that a declared column width is not a width.** egui_table runs a
  full sizing pass the first frame a table id exists and, for *resizable*
  columns only, replaces the declared width with the widest cell actually laid
  out, then stores that and reuses it forever (`table.rs:845` skips
  non-resizable columns, `:861` does the shrink). Against a truncating label —
  which lays out to whatever width it is given and reports that back — this is
  not a fit but a collapse: the first live probe rendered a ~90px outline
  column and stayed there. The widget therefore declares each width as the
  column's range *minimum* as well as its current value. Growing past it, by
  content or by a drag, still works.

- **SD5 — `row_ui` becomes a deferred block map on `endETable`.**
  `WithDeferredBlockMap("rows", ctabb.U64)` generates `BeginRows(row)` /
  `EndRows()`; the existing locally-scoped `EtStripedDelegate` implements
  `row_ui` by replaying the block. `read_deferred_block_map_u64` and
  `skip_deferred_block_map_u64` already exist (`rust/imzero2/src/fffi/io.rs:658`,
  `:674`), and the decorator already lives in the IDL apply string, so
  `interpreter.rs` is not hand-edited.

  The block maps are spliced by the generated `Send()` in **declaration
  order**, so the apply code reads — and the culled else-arm skips — cells,
  headers, rows. Reordering the IDL lines without reordering both Rust sites
  desynchronises the stream.

  **M0 found that the block map alone is not enough, and cost two more
  primitives** — the estimate of "the whole of C5's cost" was wrong:

  - **`uiSetMinWidthAvailable`.** Every Go widget sizes to its own content, so
    a row background drawn from Go covered only its own text — the first live
    probe rendered a ~60px stub per row instead of a row. `set_min_width` takes
    an explicit float and available width is a client-side fact;
    `ScalarSize().AvailableWidth()` exists but **nothing consumes it**, so
    there was no way to express "as wide as the row" at all. Without this
    `row_ui` is unusable from Go.
  - **`labelAtoms.selectable`.** egui defaults `selectable_labels` to true and
    a selectable label senses `click_and_drag`. Since `row_ui` runs *before*
    the cells, a label is registered later and therefore sits above the row
    sense and swallows its click. `label` already had `selectable`;
    `labelAtoms` did not, so a rich-text cell could not hand the click back.

  Both are small and generally useful, but they are new generated surface that
  the Surfaces table did not anticipate.

- **SD6 — The `row_ui` replay guards against a doubled call.** `row_ui` fires
  twice per visible row: `region_ui` is called from both `left_bottom_ui` and
  `right_bottom_ui` (`split_scroll.rs:150`, unconditionally), and its
  `row_range` is computed from the vertical extent without consulting
  `col_range` (`table.rs:668`). With `num_sticky_cols = 0` the sticky region is
  zero-width but full-height, so the row loop still runs. Replaying a Go block
  in both would emit every row's widget ids twice — which does not break
  rendering or clicking, only read-back, silently and newest-wins. The
  decorator carries a per-frame seen-set of replayed rows; the scrollable
  region is called first and therefore wins.

- **SD7 — Full-row interaction is a sense region behind the cell content.** The
  row block emits a full-width click-sensing frame; the cells emit the indent,
  the disclosure control, and the label. Because `row_ui` runs before the
  cells, the row sense sits behind them in hit-test order: the disclosure
  button wins over its own rect, a non-interactive label does not take the
  pointer, and so clicking the arrow toggles while clicking anywhere else on
  the row selects. This falls out of egui's hit-test order rather than needing
  arbitration.

- **SD8 — Keyboard navigation defers to its own ADR; the model hook is taken now.**
  There is no general key-event channel in the bindings —
  `GetModifiers`, `fetchF1KeyPressed` and `fetchCommandEnterPressed` are the
  whole surface, and the latter two are deliberately hardcoded single
  shortcuts. Arrow-key navigation needs a focus-scoped, consuming capture
  primitive plus a register and fetcher: that is a decision about *input*,
  reusable by any widget, not about trees, and it is specified in
  [ADR-0177](./0177-imzero2-focus-scoped-keyboard-capture.md). Pointer
  interaction ships first. What this ADR does take on now is SD2's cursor
  field — the one part of keyboard support that lives in the tree's own model
  and would force a reshape if it arrived late.

- **SD11 — A caller keys its own state, and projects it onto `State` each
  frame.** Found at M3, in all three ports. `State` keys on node indices —
  SD1's columnar input has no other identity — and its doc says a host that
  reorders should reset or remap. Every real caller reorders: schemaview and
  configview rebuild their hierarchy on each filter keystroke, and fieldview
  rebuilds it whenever the field list changes. Left alone, a section collapsed
  before typing reopens as whichever section inherited its index, and a
  selection lands on a different row.

  The remedy each caller reached independently, so it is written down here as
  the pattern rather than left to be rediscovered: keep expansion and selection
  under a key the caller already has — a section id, a category name, an index
  path — and rewrite `State` from it immediately before `Render`. The widget's
  own mutations are then read back out of `Result` and filed under the key.
  `State` becomes per-frame scratch and the caller stays the authority.

  It also settles a question SD7 left open: what a click on a row that selects
  nothing should do. Because the projection overwrites `State`'s selection
  before it is ever drawn, such a click is free to mean something else, and in
  both navigators it toggles — which is what clicking a `CollapsingHeader`
  title did before. The caller suppresses it when `Result.Toggled` names the
  same row, or a double-click would count three toggles instead of two.

  A helper on `State` keyed by a caller-supplied string would remove the
  boilerplate. Deferred rather than designed here: three call sites is thin
  evidence for the shape of the key column, and the projection is a dozen
  lines.

- **SD12 — A one-line row costs a wrapped second line; the trade is a column
  plus a tooltip.** Found at M3. `fieldview` drew each leaf as name-and-kind
  over a wrapping value, and `configview` drew each variable as signals over a
  wrapping description. Neither survives a fixed row height. Both took the same
  treatment: the second line becomes its own column, truncated, with the full
  text on hover.

  What it costs is real — a long JSON value or a paragraph-length description
  is no longer readable in place. What it buys is that values and descriptions
  line up down the pane instead of each row being its own ragged block, which
  is the shape these panes wanted; and the hover recovers the text. Recorded
  because it is the one place the port is not behaviour-preserving, and because
  a future variable-height row would let both revert.

- **SD13 — C4's "rows are addressable" was half true, and M4 paid the other
  half.** The design scored O3 `++` on driveability because rows appear in the
  AccessKit tree. They do. But a driver has to *find* a row and then *act* on
  it, and the tree as built supports the first and not the second:

  - **Finding** needs the accessible *value*. egui leaves a `Label`'s name
    empty and puts its text in the value slot, and ADR-0154's `Locator`
    matched the name only — so no tree row, and no static readout anywhere in
    the repo, was reachable by any spelling of a name anchor. `value` /
    `valueContains` are now additive fields beside `name` / `contains`, a
    separate pair rather than a widening because a name anchor that silently
    began matching values could turn a resolving anchor in an existing trace
    into an ambiguous one. They want `role: "label"` beside them: egui emits a
    `text_run` child under some of its labels carrying the same text, so a
    bare value anchor resolves on one label and reports an ambiguity on the
    next.
  - **Acting** cannot go through an AccessKit action. SD7 puts the row's sense
    region *behind* its cells so the disclosure control wins its own rect,
    which leaves the row's label an ordinary non-interactive node — findable,
    deaf to a click action. A `pointer: true` on an anchored `click` presses
    the resolved node's bounds centre instead, which is what a human click
    does. It is the rung between "resolve by anchor" and "click a literal
    coordinate", and it was missing.

  Both are ADR-0154 surfaces changed from here, and both are additive. What
  they say about this widget is that SD7's arbitration, which is free and right
  for a pointer, costs a driver the coordinate-free path for the row itself —
  the disclosure control keeps it, being a real button. A row that a driver
  must be able to actuate by node would need its own sense to be a widget with
  a name, which is a larger change than M4 warranted.

- **SD9 — Indent guides are deferred.** The vertical lines connecting a parent
  to its children want row-relative painting, which `row_ui` makes possible but
  which is decoration, not function. `IsLastChild` is carried in `Row` from M1
  so adding them later needs no model change.

- **SD10 — `egui_ltreeview` is removed, not deprecated.** Keeping it means
  keeping a crate, a global register, a `NodeCommand` enum, and a panic guard,
  to serve two demos. Removal is gated on parity and is the last milestone.

### Milestones

- **M0 — `row_ui` deferred block map on `endETable`.** ✓ The IDL declaration, the
  `row_ui` implementation with SD6's seen-set guard, and regeneration. Ported
  `logviewer` from five `TintedScope` cells to one row block, which is both the
  payoff and the proof the guard holds. Independently useful with no tree in
  sight.
- **M1 — `model.go` + `layout.go`.** ✓ The columnar input, its validation, the
  `State`, and the pure flatten with its test suite. No bindings import.
- **M2 — `render.go`.** ✓ The etable renderer: row blocks, indent, disclosure,
  selection, `VisibleRange` gating, reveal-selection via `ScrollToRow`. `State`
  gained a one-shot reveal request and the flatten scratch; `Column` /
  `Input` / `Result` are the render surface, with host columns to the right of
  the outline so M3's callers have somewhere to put a type or a count.
- **M3 — Port the in-repo callers.** ✓ `schemaview`, `configview`, `fieldview`,
  one at a time, each verified live in its own host. `componentview` is not a
  tree and is not ported — see the Context correction. Two things came out of
  it that were not in the design: every caller needs the same
  host-key-to-node-index projection (SD11), and two of the three had to give a
  wrapped second line up for a column (SD12).
- **M4 — Demo + tour entry.** ✓ A registry `Demo` (`tree`, "tree outline")
  replacing `tree-view`, the node section of the imzero2 catch-all demo, and
  the headless scene the verification plan wanted. The demo carries the
  expand-all / collapse-all / reveal controls, because "the host owns the
  state" is the claim that most needs showing. Driving the scene cost two
  additive rungs on the ADR-0154 ladder — see SD13.
- **M5 — Remove `egui_ltreeview`.** ✓ The IDL nodes, the `r3_node_cmds`
  register, the `NodeCommand` enum, the crate dependency, and the imzero2
  skill's §7. Two things came off with them that the plan did not name: the
  `registered` IDL category, now empty and kept as the place a future
  drain-protocol node would land, with a note about what that shape implies;
  and `ResponseFlagsE`'s bit 30, left as a hole rather than reused (see
  Migration).

## Surfaces — Tier 1

| Surface | Change | Moves with it |
| --- | --- | --- |
| egui2 IDL — `endETable` (`egui2_definition_d_table2.go`) | added: `rows` deferred block map, keyed `U64` | regenerated `bindings/methods.out.go` + `factories.out.go`, regenerated region of `interpreter.rs`; both the Go binary and the Rust renderer rebuilt in the same commit |
| egui2 IDL — `uiSetMinWidthAvailable` proc, `labelAtoms.selectable` method | added at M0 (see SD5) | same regeneration; adding IDL nodes **renumbers opcodes** (`enums.out.go`, `enums_out.rs`), so a stale binary on either side desyncs the wire |
| egui2 IDL — `tree`, `nodeDir`, `nodeLeaf`, `nodeDirClose` | removed at M5 | `r3_node_cmds` register and `NodeCommand` enum in `interpreter.rs`, `prepare_next_frame` clear arm, the two demos |
| `rust/imzero2/Cargo.toml` | `egui_ltreeview` dependency removed at M5 | `Cargo.lock`, the licence and vendor gates |
| Exported Go API under `public/` | added: `widgets/tree` | nothing yet — no downstream module compiles against it on day one. `render.go` shares the package with the model rather than sitting in a `view/` subpackage the way icicle and sankey split theirs, so the package's ADR-0080 properties flip to `WASMBlocked` at M2 — the trade is a WASM-compilable flatten, which nothing wants, for one import at each of M3's call sites |
| Exported Go API under `public/` — `fieldview.Renderer.Render` | changed at M3: takes a new `*fieldview.State` first | the widget's expansion left egui's memory for the caller's hands (SD2), and a `Renderer` is a value whose setters copy, so view state could not ride along in it. Both call sites move with it: `logviewer` retains one `State` per instance, the demo one per sample list. The demo also needed a distinct `idPrefix` per list, which it should have had already — three lists through one prefix were sharing widget ids before the port |
| `carrierclient` — `Locator.Value` / `.ValueContains`, `Step.Pointer` | added at M4 (see SD13) | the ADR-0154 trace vocabulary gains three fields; all three are additive, so every existing trace resolves exactly as before |
| `scripts/dev/tree-widget-scene.sh` | added at M4 | the headless assertion the verification plan asks for; it needs a current `rust/imzero2/target/headless/release/imzero2`, and fails loudly rather than desyncing when the client predates the codegen |
| imzero2 skill §7 "The Node & Tree System" | rewritten at M5 | `.claude/skills/imzero2/SKILL.md`; §13.1's register table loses its `r3_node_cmds` row |

## Alternatives

- **Port `componentview` too (M3 as originally scoped).** Rejected once the
  code was read: it is an accordion of arbitrary-height bodies, not a
  hierarchy, and a fixed-height row cannot hold a 115px gauge. Making it fit
  would mean redefining the public `RendererI` so every registered renderer
  emits a row, and moving the gauge and chip rows into a detail pane beside the
  tree — a large change to a published extension point, to render a flat list
  of three components. Descoped and recorded rather than gating M3 on it.
- **Keep `egui_ltreeview` and extend the binding (O1).** Every gap in Context
  is upstream of the binding — expansion state, row content, and the action
  stream are all owned by the crate. Closing them means either patching the
  crate or reimplementing around it, and the second is this ADR.
- **Compose rows in a `ScrollArea` (O2).** Simplest to write and the natural
  light cut, but it has no culling: every row builds and marshals a deferred
  block every frame, which is the allocation pathology already documented for
  ungated etables. Since `endETable` supplies virtualisation for free and its
  cells accept the same composed widgets, O2's only advantage over O3 is not
  needing M0 — and M0 pays for itself in `logviewer` regardless.
- **Paint onto a canvas (O4).** Cheapest per row and the way `icicle` and
  `treemap` work, but rows would not exist in the AccessKit tree, so egui-mcp
  and the ADR-0154 headless driver could not address them, and hit-testing,
  hover, eliding, and tooltips would all be rebuilt. Right for a density plot,
  wrong for a control.
- **Ship single-column first and defer `row_ui`.** A one-column tree needs no
  extension, since the cell rect is the row rect. Rejected because it front-
  loads a shape the widget then grows out of, and because M0 stands on its own
  merits.

## Consequences

### Positive

- Expansion, selection, and scroll position become Go state — persistable,
  restorable, and settable from code.
- Collapsed subtrees and off-screen rows cost nothing, where today a fully
  collapsed `egui_ltreeview` still marshals every node every frame.
- Rows can carry badges, counts, columns, and controls, so the hand-rolled
  `CollapsingHeader` trees have something to converge on. At M3 all three took
  it: `schemaview` moved its type chip out of the label into its own weight,
  `configview` became a three-column table of name, value and description, and
  `fieldview` put its value in a column beside the name.
- `logviewer` sheds its per-cell tint workaround at M0, and the inter-column
  gutter seam goes with it.
- One fewer Rust dependency, one fewer global register, and one fewer
  defensive panic guard after M5.

### Negative

- M0 touches generated code on both sides of the FFI, so it carries the usual
  drift hazard: the Go binary and the Rust renderer must be rebuilt together.
- SD6's double-call guard is a correctness detail with a silent failure mode,
  and it is pinned to `egui_table` internals that a version bump can move.
- The widget inherits `endETable`'s constraints — including the
  `ETABLE_AUTOFIT_CAP_PX` heuristic, which a tree in a tall panel must override
  with `MaxHeight`.
- No keyboard navigation until [ADR-0177](./0177-imzero2-focus-scoped-keyboard-capture.md)
  lands, which is a real regression against `egui_ltreeview` for keyboard-first
  users. SD2's cursor field keeps the model ready but moves nothing on its own.

### Neutral

- "Native Go over existing primitives" still rests on `egui_table`, a
  third-party crate. It is one already load-bearing in `play`, `imztop`,
  `logviewer`, and `fsmview`, so the trade is one lightly-used dependency for
  deeper use of a heavily-used one — not the removal of a dependency class.
- The widget will look different from `egui_ltreeview`, since it is themed from
  `SelectableLabel` and etable striping rather than the crate's own visuals.

## Migration — Tier 1

- **Breaks.** Nothing at M0 through M2 — the `rows` block map is additive and
  the existing tree binding is untouched. M3 breaks one exported signature,
  `fieldview.Renderer.Render`, which gains a leading `*State`; both in-repo
  callers move with it in the same commit and no downstream module compiles
  against the package. At M5, `c.Tree`, `c.NodeDir`, `c.NodeLeaf`, and
  `c.NodeDirClose` stop existing, along with the `NodeCommandS` retained type
  and `ResponseFlagsE.HasNodelikeSelected`.
- **Path.** Replace a `NodeDir` / `NodeLeaf` recursion plus its `Tree()` drain
  with one `tree.Render` call carrying `Labels` / `Parents` and a caller-owned
  `State`. Only the two demos are affected; no app and no `public/` package
  calls the node API today, so no migration recipe under `doc/migration/` is
  warranted.
- **Regeneration.** `app egui2gen generate` at M0 and again at M5. Both are
  FFI-boundary changes: rebuild the Go binary **and** the Rust renderer, or the
  stream desynchronises at the first `endETable`.
- **Old shape.** Removed outright at M5, gated on M3 landing. Not deprecated
  first — a deprecation period serves downstream consumers, and there are none.

- **The response-flag bit is retired, not recycled.** `NODELIKE_SELECTED` was
  bit 30 of `ResponseFlags` on both sides of the wire. M5 leaves it unused
  rather than handing it to the next flag that wants a bit: the flags are a
  contract between `fenums.rs` and `egui2_enums.go`, and a bit that changes
  meaning is the kind of change that compiles on both sides and lies at
  runtime. Both files carry the note; bit 31 (`BLOCK_SKIPPED`) is unaffected.

## Verification plan — Tier 1

- **Lane.** Default `go test` for `layout_test.go` (SD3's flatten: ordering,
  depth, collapse, forest roots, cycle and dangling-parent rejection) and
  `render_test.go` (the click-to-selection rules: replace, toggle, extend, and
  the two ways an extend loses its anchor). The screenshot tour for the M4
  demo. A headless scene (ADR-0154) for click-to-select and click-to-expand,
  which needs M4's registered demo to drive and lands with it.
- **What would fail.** A flatten or selection regression fails `go test`
  directly. An SD6 guard regression is the interesting one: doubled row ids do
  not break rendering or clicking, so the observable is the headless scene's
  click-to-select assertion going red while the tree still looks correct, plus
  `checkId`'s `id has already been used` WARN on the emitting frame. That
  asymmetry is why the scene asserts selection rather than appearance.
- **Gap.** SD6's guard is Rust-side and has no Go-level unit test; it is
  covered only by the headless scene, which exercises `num_sticky_cols = 0`
  and not the two-region sticky-column case. A tree does not use sticky
  columns, so the uncovered path is `logviewer`'s and any future multi-column
  adopter's — worth a note in the binding rather than a test, until something
  needs it. SD8's absent keyboard navigation is untested because it is
  unimplemented.

  **Modified clicks cannot be driven.** M2 was verified live through egui-mcp,
  which carries a modifier on the click event but does not set the frame's
  modifier state, so a synthesized ctrl- or shift-click arrives unmodified and
  reads as a plain one. `render_test.go` covers what each mode means and
  `clickMode` — the register read it cannot exercise — is three lines. Held
  modifiers are not a gap in the widget, but they are a gap in what any driver
  here can assert about it.

  **M4 built the scene, and it asserts state rather than pixels.**
  `scripts/dev/tree-widget-scene.sh` launches the widget gallery through the
  headless host with no compositor, narrows it to the tree demo, and then:
  collapse-all leaves one row; clicking the root's disclosure control leaves
  four; a pointer click on the *Chordata* row makes the readout say
  `selected: [Chordata]` **and leaves the row count at four**, so selecting
  did not fold anything; and the reveal button opens a species four ranks and
  a phylum away and selects it. Each assertion is a `wait` that fails the run
  when it never resolves, which is what makes an SD6 regression red — the
  picture would still look right.

  **The scene's capture caught a defect the assertions could not.** Reviewing
  M4 by eye found the selection outline missing its BOTTOM edge on any row with
  another row after it — top, left and right painted, the bottom not. The
  assertions were all green, correctly: selection and expansion were working,
  and only the drawing was wrong. Three explanations were ruled out by
  measuring a headless capture at one pixel per point — the row below paints
  nothing (its fill is `RGBA(0,0,0,0)` and its stroke width 0), the row Ui is
  never clipped to its own rect (egui_table calls `shrink_clip_rect` for cells
  and not for rows), and at stroke width 2 the edge was absent rather than
  thinned. What is left is epaint's inside-stroke tessellation of a rect whose
  height is exactly the row pitch; asking for a row height one point short
  moves the stroke off that boundary and all four sides paint, with the fill
  still covering the row so no seam appears between stripes. The workaround
  and its ruled-out alternatives are written at `rowChrome`.

  The lesson for the scene is not that it should assert pixels — it should not,
  for the SD6 reason it exists — but that **a capture nobody looks at is not a
  capture**. Its first version photographed the post-reveal state, in which the
  selected row is scrolled out of the gallery window: green run, empty picture.
  It now captures while the selected row is on screen, and the run prints what
  to look at.

  Two things it needed that are worth knowing before writing another one. The
  demo body lays out **~3200 px down the gallery's scroll** when the gallery is
  unfiltered, and an etable emits only its visible range, so every row but the
  first was culled and a pointer press landed outside the window: the trace
  filters the gallery and then `scroll_into_view`s the body. And the readout
  line is deliberately sorted, because the selection is a set and map order is
  arbitrary — an unsorted readout flickers between frames with nothing having
  changed, which is precisely what a polling `wait` cannot tolerate.

  **M3's ports are covered by unit tests over their hierarchy builders and by
  live driving, not by scenes.** `schemaview`'s `navtree_test.go` asserts the
  row order and parentage the CollapsingHeader nest produced, the stable keys,
  the per-node selection, and — the part SD11 exists for — that a collapse and
  a selection survive a filter renumbering every node. What no unit test
  reaches is the rendering, so each port was driven through egui-mcp in its own
  host: full-row click-to-select past the label text, arrow-to-toggle, click on
  a grouping row toggling, expansion surviving a live filter keystroke, and the
  truncate-plus-hover the SD12 columns depend on. `configview` and `fieldview`
  have no equivalent builder test yet — their hierarchy is a flatter shape and
  the assertions would restate the code — which is a thinner net than
  `schemaview`'s and is worth closing if either grows a second grouping rule.

  **A single-column tree wants its column measured, not declared.** M2's SD4
  fix stops egui_table shrinking a column below its declared width; it does not
  make the column follow its pane. In `schemaview`'s navigator the outline is
  the only column, so a declared width left the rest of the pane empty and the
  row's selection outline — which spans the table's columns, not the pane —
  stopped short of the edge. The fix is to feed the column the pane width from
  `CapturePaneSize` and mark it **non-resizable**, since egui_table only leaves
  a non-resizable column's declared width alone; it then tracks the dock
  splitter as it is dragged. A tree that is one column wide should expect to do
  this.

  **The disclosure glyph escaped to the CJK fallback.** Reported after M5 as
  "too big and not centred vertically", and it is one cause: `▶` / `▼`
  (U+25B6 / U+25BC) are not in Noto Sans, so they fall through to the CJK face,
  which draws them at ideographic full-em and centres them on the ideographic
  box rather than the Latin baseline. Measured on one scene with and without
  `--fallbackFontTTF`, everything else equal: the ink grows from 8px to 12px
  and drops 2px below the label beside it. It therefore appears on every
  desktop launch and on no run of the headless lane, which loads no CJK
  fallback — which is why four rounds of headless verification never showed it.
  The fix is the Phosphor caret pair the client loads explicitly, as
  `canonicaltypeedit` already uses for its own disclosure: ink 5px, offset
  0.5px. The scene's `click` anchor was the glyph, so it broke on the change
  and had to be repointed — a trace rotting loudly, which is the property
  ADR-0154 claims for them.

  The two glyph defects in these notes share a root and suggest a rule the
  repo does not have: **an affordance's glyph should come from a font the
  client loads, not from the fallback chain.** Text can fall back; a control
  cannot, because its size and baseline are then whatever face answered.

  **The `◈` co-section glyph rendered as tofu, and is now drawn from the mono
  face.** Found while checking `schemaview`'s ported rows and left recorded
  rather than fixed at M3; fixed after M5, once the disclosure-glyph defect
  showed it was the same root and not a curiosity. Reading the cmaps of every
  face the client loads settles it exactly: Noto Sans has none of `◆ ◇ ◈`, the
  CJK fallback has `◆` and `◇` but **not** `◈` — so that one had no face in the
  chain at all — and the mono font has all three. That is also why the legend
  window always showed it correctly: its chips are `Monospace()`.

  So the navigator now draws the category glyph as its own monospace run, with
  the glyph carried beside the label in `navNode` rather than inside it. The
  documented vocabulary is unchanged; only the face is. It fixes the tofu and
  takes `◆` and `◇` off the fallback chain too — they rendered only because a
  CJK font happened to be loaded and happened to have them.

  **The header block replays twice**, the same way SD6's row block would
  without its guard: `header_cell_ui` runs for the sticky region as well as
  the scrollable one, and unlike `row_ui` it has no seen-set. Found while
  measuring M2's columns — every header label appears twice in the
  accessibility tree. It predates this ADR, affects every etable with deferred
  headers rather than trees specifically, and has no read-back consumer today,
  so it is recorded here rather than fixed as part of a tree milestone.

## Status

Proposed — awaiting review. M0–M5 are built; what the review is for is the
decision, not the delivery.

- **M0** — `row_ui` as a deferred block map, with SD6's seen-set guard;
  `logviewer` off its per-cell tint workaround.
- **M1/M2** — the columnar model, the host-owned `State`, the pure flatten, and
  the etable renderer.
- **M3** — `schemaview`, `configview` and `fieldview` ported.
  `componentview` is not a tree and stays a `CollapsingHeader` accordion; the
  Context paragraph that counted it as a fourth adopter is corrected in place.
- **M4** — the `tree` demo, the catch-all demo's node section, and
  `scripts/dev/tree-widget-scene.sh`. Cost two additive rungs on ADR-0154's
  anchor ladder (SD13), and its capture caught the outline defect the
  assertions could not.
- **M5** — `egui_ltreeview` gone: the IDL nodes, the register, the enum, the
  crate. Verified on both lanes with the Go host and both Rust clients rebuilt
  together — the headless scene passes with no wire desync, and the desktop
  gallery renders the ported outlines with no id collisions.

Status lifecycle: `Proposed → Accepted → (Deferred | Deprecated | Superseded by ADR-XXXX)`.
See [DOCUMENTATION_STANDARD §1 ADR](../DOCUMENTATION_STANDARD.md#architecture-decision-records-why-it-is-this-way) for the edit-policy tiers (Tier 1 in-place / Tier 2 dated `## Updates` entry / Tier 3 new superseding ADR).

<!--
## Updates

Tier-2 dated entries land here when implementation reveals a refinement, an aspirational
claim turns out false, or a milestone records what shipped. Single H2; add H3s dated
YYYY-MM-DD. Remove this HTML comment when the section first gains a real entry.
-->

## References

- [ADR-0177](./0177-imzero2-focus-scoped-keyboard-capture.md) — the keyboard capture primitive SD8 defers to; SD2's cursor is its hook in this widget.
- [ADR-0151](./0151-table-column-width-overrides.md) — the etable column-width epoch protocol this widget inherits.
- [ADR-0154](./0154-headless-carrier-tree-and-driver.md) — the headless driver the verification plan leans on.
- [ADR-0160](./0160-imzero2-icicle-flamegraph-widget.md) — the columnar hierarchy input SD1 follows, and the model/layout/view split SD3 mirrors.
- [ADR-0166](./0166-play-treemap-panel.md) — the second reader of `play_hierarchy.go`'s column contract.
- [ADR-0012](./0012-imzero2-collapsible-retained-bodies.md) — why block bodies emit every frame, and what culling is available instead.
