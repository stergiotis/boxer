---
type: adr
status: accepted
date: 2026-08-07
reviewed-by: "p@stergiotis"
reviewed-date: 2026-08-09
---

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

- **SD11 — A caller keys its own state.**
  And projects it onto `State` each frame. Found at M3, in all three ports. `State` keys on node indices —
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

- **SD12 — A one-line row costs a wrapped second line.**
  The trade is a column plus a tooltip. Found at M3. `fieldview` drew each leaf as name-and-kind
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

- **SD13 — C4's "rows are addressable" was half true.**
  M4 paid the other half. The design scored O3 `++` on driveability because rows appear in the
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
  the glyph carried beside the label in `navNode` rather than inside it. That
  takes `◆` and `◇` off the fallback chain too — they rendered only because a
  CJK font happened to be loaded and happened to have them.

  **And then the glyph itself had to go.** Rendering it faithfully showed the
  second defect: `◈` is "white diamond containing black small diamond", and the
  interior gap that distinguishes it from `◆` is sub-pixel at the size a row
  draws it. Rasterised from the mono face it is byte-identical to `◆` at 12px
  and at 14px, and only at 18px does any structure appear — which is why it
  read the same as `◆` in the legend, where it had been rendering "correctly"
  all along. A mark that cannot be told from its neighbour is not a mark. The
  co-section glyph is now `❖`, which keeps the diamond family and carries a
  visible interior at 13px; it changes with it in the TopologySpark card the
  vocabulary is shared with, so the two do not drift.

  Two defects, one glyph, found in the order tofu → illegible. The first hid
  the second: a box that renders nothing cannot be observed to render the wrong
  thing.

  **The header block replays twice**, the same way SD6's row block would
  without its guard: `header_cell_ui` runs for the sticky region as well as
  the scrollable one, and unlike `row_ui` it has no seen-set. Found while
  measuring M2's columns — every header label appears twice in the
  accessibility tree. It predates this ADR, affects every etable with deferred
  headers rather than trees specifically, and has no read-back consumer today,
  so it is recorded here rather than fixed as part of a tree milestone.

## Status

Accepted 2026-08-09, with M0–M5 built and verified before the flip rather than
after it. What follows is what shipped under each milestone; the ADR is now
Tier 2, so a later refinement lands as a dated `## Updates` entry rather than an
edit in place.

- **M0 — `row_ui` as a deferred block map,** with SD6's seen-set guard;
  `logviewer` off its per-cell tint workaround.
- **M1/M2** — the columnar model, the host-owned `State`, the pure flatten, and
  the etable renderer.
- **M3 — `schemaview`, `configview` and `fieldview` ported.**
  `componentview` is not a tree and stays a `CollapsingHeader` accordion; the
  Context paragraph that counted it as a fourth adopter is corrected in place.
- **M4 — the `tree` demo and its scene script.** The catch-all demo's node
  section, and `scripts/dev/tree-widget-scene.sh`. Cost two additive rungs on ADR-0154's
  anchor ladder (SD13), and its capture caught the outline defect the
  assertions could not.
- **M5 — `egui_ltreeview` gone.** The IDL nodes, the register, the enum, the
  crate. Verified on both lanes with the Go host and both Rust clients rebuilt
  together — the headless scene passes with no wire desync, and the desktop
  gallery renders the ported outlines with no id collisions.

Status lifecycle: `Proposed → Accepted → (Deferred | Deprecated | Superseded by ADR-XXXX)`.
See [DOCUMENTATION_STANDARD §1 ADR](../DOCUMENTATION_STANDARD.md#architecture-decision-records-why-it-is-this-way) for the edit-policy tiers (Tier 1 in-place / Tier 2 dated `## Updates` entry / Tier 3 new superseding ADR).

## Updates

### 2026-08-09 — Adoption feedback from a fourth caller, and a proposed `Keys` column

`apps/mdedit`'s heading outline was ported off its flat `SelectableLabel` list
onto this widget ([ADR-0178](./0178-mdedit-markdown-editor.md), dated Update of
the same day), making four adopters rather than M3's three. Nothing here blocks
anything — all four callers work — and nothing below is decided. It is what one
more port surfaced, written down while it is fresh, with a proposed shape for
the one item that looks structural rather than cosmetic.

**Every adopter has now written the same projection, and the widget could own
it.** `State` keys on the node's index in the columnar input, which is the only
identity such an input has; SD2 says so and tells a host that renumbers to
remap against its own stable key. All four have done exactly that, each
inventing the key column separately:

| adopter | projection | key |
|---|---|---|
| `schemaview` | `syncNav` | `"plain:" + itemType` / `+ ":" + column` |
| `configview` | `syncTree` | category name, or variable name |
| `fieldview` | `syncState` | a **synthesised** positional path, `"0/2/1"` |
| `mdedit` | `syncOutline` | `slug#ord` |

`fieldview` is the informative case: its own comment records that it had to
invent that path because names are not usable — an array's children are named
by position and an object's keys are not guaranteed unique. Four independent
inventions of the same missing column is evidence about the input model rather
than about the callers.

What makes it worth more than the ~12 lines it costs each time is the failure
mode. Omit the projection and nothing panics and nothing logs: a section the
reader collapsed reappears somewhere else after an edit renumbers the nodes. It
reads as a flaky widget rather than as a host bug. The exposure scales with how
often the host rebuilds — `schemaview` renumbers per filter keystroke, `mdedit`
per keystroke that adds a heading.

**Proposed:** an optional `Tree.Keys []string`, parallel to `Labels` and
`Parents`. When present, `State` files expansion, selection and cursor under
the key and remaps them on each `Flatten`; when absent, behaviour is exactly as
today, so this is additive and no existing caller changes. It does not remove
the need for a host to *have* a stable key — `fieldview` would still synthesise
its path — only the need for each host to write the projection and to have
remembered that it must.

**`State.Reveal` and host-owned expansion quietly cancel.** `Reveal` does two
things: open the target's ancestors, and scroll its row into view. The first is
written into the widget's `State` — which a host that owns expansion overwrites
on the very next frame from its own map, as all four do. Each half is correct
in isolation and neither doc mentions the other, so the naive composition works
for one frame and then stops. `mdedit` opens the ancestors in its own map
before the sync; `ExpandAncestors` is exported, so this is expressible, but it
is currently something each caller has to derive. Worth at minimum a sentence
in both doc comments, and it is a second thing the `Keys` proposal would
simplify: a keyed `State` could survive a rebuild and would not need rewriting
each frame at all.

Four smaller observations, none of which need a decision:

- **`Column.Cell func(node int32)` could carry the `Row`.** A cell that varies
  on expansion — `mdedit` draws a hidden-heading count only on closed rows —
  has to reach back into the `State` it passed in, during the widget's own
  render pass. The renderer already holds the `Row`, with `Expanded`, `Depth`,
  `HasChildren` and `IsLastChild`, at that exact call site. Passing it costs
  nothing and is strictly more informative; SD9's deferred indent guides would
  want the same fields.
- **Truncation versus trailing content is an undocumented trap in the primary
  extension point.** A truncating label takes the whole width it is offered, so
  anything emitted after it in the same row is pushed out of the cell.
  `schemaview` hit this and accepted the loss on long names; `mdedit` moved its
  count into a column of its own instead. `Column.Cell`'s doc says a label must
  be `Selectable(false)`, which is the other footgun in the same place, and
  could say this too.
- **A pending reveal cannot be observed.** `revealP1` and `takeReveal` are
  unexported and `Reveal` returns nothing, so a caller wanting to assert that a
  reveal was issued has to thread a return value through its own code to do it.
  A `State.PendingReveal() int32` would close that.
- **`MaxHeight`'s guidance is rarely the answer in practice.** It says to leave
  it 0 in a bounded host. `schemaview`, `configview` and `mdedit` all pass a
  pane-probed height; `fieldview` re-exports the knob to its own caller rather
  than choosing. No caller relies on the 0 branch, so the doc would serve a
  reader better by leading with the probe.

None of this changes what M3 concluded about the widget being the right shape:
host-owned state is what made `mdedit`'s slug remap possible at all, and is the
thing `egui_ltreeview` could not do. The seam between that host ownership and
index-based identity is simply one step short of finished. If `Keys` is taken
up it should land as a further dated entry here recording what shipped; if the
shape grows past an optional column it wants its own ADR instead.

### 2026-08-09 — `Keys` shipped, with a default the proposal did not have

The `Tree.Keys` column proposed in the entry above is in, along with the four
smaller items beside it. All four adopters now file expansion under their own
key and none keeps a parallel map. What follows is what shipped and what the
building of it found — including one place the shape grew past what that entry
proposed.

**The proposal was one value short of usable.** `State`'s expansion map meant
"absent is collapsed", and filing under a key does not change that: a key the
State has never seen and a key the reader closed are the same absent entry.
Three of the four adopters are default-OPEN — `schemaview` stored `collapsed`,
`mdedit` stored `outlineCollapsed`, `fieldview` has a `DefaultOpen` that is
switchable at runtime — so under `Keys` alone each would have kept its
inversion and only `configview` would have dropped anything. The column would
have paid for one caller in four.

Expansion is therefore three-valued now: open, closed, or no record, with
`State.SetDefaultExpanded` deciding the last. `SetExpanded` drops a record that
agrees with the default, so the store stays bounded by what the reader changed
and a later change of default still moves everything untouched — which is what
`fieldview.DefaultOpen` already promised its callers and now gets from the
widget instead of from its own map. `ExpandAll` and `CollapseAll` set the
default and clear the records rather than enumerating nodes, so "expand all"
covers rows that arrive afterwards; `ExpandAll` lost its `Tree` parameter with
that.

That is one field and one three-valued map past "an optional column", which the
entry above said would want its own ADR. It is recorded here instead, on the
reading that it is the same decision finished rather than a new one — but it is
a judgement call, and an ADR is the honest alternative if this seam moves again.

**What each adopter dropped, and what stayed.**

| adopter | dropped | kept, and why |
|---|---|---|
| `schemaview` | `collapsed`, `setCollapsed`, the expansion loop in `syncNav`, the toggle write-back in `applyNav` | a selection projection: `selection` names a plain column, a section, or a column within one — richer than a node, and set from outside the navigator |
| `configview` | `expanded`, `setExpanded`, the same two | the same, for `selected`; rewriting it each frame is also what keeps a category row from drawing as selected on the way past its own toggle |
| `fieldview` | `open`, `isOpen`, `setOpen`, `syncState`, `applyResult` | nothing — it has no selection, so its projection is gone outright |
| `mdedit` | `outlineCollapsed` and its two helpers, the expansion loop in `syncOutline`, the toggle write-back, and the ancestor walk in `outlineReveal` | the caret-derived selection, which is a projection of the editor rather than of identity |

So the boilerplate the entry above counted did not all have one cause. The
expansion half was about identity and is gone. The selection half is about a
host whose selection is a richer thing than a node, and it stays — three of the
four still write `SelectOnly` each frame, and that is not a gap.

**`State.Reveal`'s quiet cancellation dissolves.** The entry above records that
`Reveal`'s ancestor-opening is undone by a host that rewrites expansion each
frame. With expansion filed under the key there is nothing to rewrite, so the
naive composition is now the correct one: `mdedit`'s `outlineReveal` deleted its
own parent walk and just asks. Both doc comments say so, which was the minimum
that entry asked for.

**A binding is now something a host has to remember, which is the new sharp
edge.** `State` learns its key column from the `Tree` that `Flatten` — and
therefore `Render` — is called with. A host that rebuilds and then writes to
the State by index before the next render is writing under the key the previous
build gave that index. All three navigators do exactly that, to project their
selection, so all three call `State.Bind` immediately after building;
`mdedit`'s is in `buildOutline` itself, because its collapse-all button and its
reveal both run before the sync.

The first-frame case of this is worse and is handled in the widget: a fresh
State seeded before any render has its entries filed by index, and the binding
would then hide them permanently. `Bind` adopts index-filed entries on its
first binding, which is the one frame it can happen on. The between-frames case
cannot be papered over the same way — an index means something either way —
and is left to the doc.

**The four smaller items shipped as proposed.** `Column.Cell` takes the `Row`
rather than the node (`mdedit`'s hidden-heading count now reads `r.Expanded`
instead of reaching back into the `State` mid-render); its doc gained the
truncation-versus-trailing-content trap that cost `schemaview` its long names;
`State.PendingReveal` makes a pending reveal observable, and `mdedit`'s reveal
test now asserts on it rather than on a map; and `MaxHeight`'s doc leads with
the pane probe, which is what all four do.

**Not done: duplicate keys are documented, not detected.** Two nodes sharing a
key read as one node to everything in `State`. Catching it means building a map
of every key on every frame, which is a permanent per-frame cost for a host bug
that shows up the first time the two rows are opened. `Tree.Keys` says so.

**Surfaces.** Two exported Go signatures changed — `Column.Cell func(node
int32)` to `func(r Row)`, and `ExpandAll(Tree)` to `ExpandAll()`. Both are
compile-time breaks with no downstream module compiling against the package;
every in-repo caller moved in the same change. `Tree.Keys`,
`State.SetDefaultExpanded`, `State.Bind` and `State.PendingReveal` are
additive, and a host that supplies no `Keys` behaves exactly as before.

**Verification.** `go test` covers the widget's own keyed rebuild — expansion,
selection, cursor and reveal following their keys across a filter that
renumbers every node — the default's three-valued behaviour, and the first-bind
adoption. Two adopters assert the same property through their real builders:
`schemaview`'s filter keystroke and `mdedit`'s heading insert. The headless
scene passes unchanged, and the demo's tree now carries a `Keys` column so the
scene drives the keyed render path rather than only the unkeyed one; its
capture still shows a selection outline closed on all four sides.

What no test here reaches is a KEYED adopter through a real render — the scene
drives the demo, whose tree never rebuilds, so nothing exercises "collapse a
section, type in the filter, watch it stay collapsed" end to end. That wants a
second scene with an `Nth` anchor to tell the two filter boxes apart, and it is
not built. The unit tests cover each half of it and the render path is
identical either way, which is why this is recorded rather than blocking.

### 2026-08-16 — SD12's fixed row is a height BUDGET, and it was 8 points short

SD12 recorded what a fixed row height costs a wrapping second line. It did not
record what the row height costs the row itself, and two adopters were over it
without any diagnostic saying so.

**A cell taller than its row does not centre — it hangs.** egui_table builds
every cell `Ui` as `left_to_right(Align::Center)` over the cell rect, so cell
content is already centred on the row's midline and this widget adds no
centring of its own. `Layout::next_frame_ignore_wrap` then has a clamp for the
overflowing case: an item taller than its frame is translated back down to the
cursor, "or we will overlap the row above". So the whole overflow lands BELOW
the row rather than straddling it — under the next row, and through the
selection outline, which is drawn to the row pitch and clips it.

**`paddedCell` was spending a third of the row on inset it did not need.** The
cell frame took `PaddingInner` on all four sides: at Standard density that is
8 of a 22-point row, leaving 14 for content. A default `Button` is its text
plus `button_padding.y` twice — about 25 points at `BODY_PT` 13 — so `play`'s
Vocabulary Insert and `sqleditor`'s per-row accept both overflowed by roughly
half a row, on every row, and had done since each was written.

The inset is horizontal only now. It is free for content that fits — the
centre of a 22-point row and the centre of a 14-point box inset 4 from its top
are the same line, so nothing that was correct moved — and it hands the
overflowing case its 8 points back before the clamp fires. That is not enough
for a full-size button on its own, so the budget is now stated on
`Column.Cell`: a per-row control wants `.Small()`, which drops
`button_padding.y` and the `interact_size` floor with it, the way `disclose`
already draws the disclosure control; a host wanting full-size controls raises
`RowHeight` instead. Both adopters took `.Small()`.

Found by reading a screenshot rather than by a test, and it is not clear what
test would have caught it: the widget's own scene asserts SELECTION precisely
because a capture can look correct while read-back is broken, and this is the
mirror case — every assertion passed while the picture was wrong.

## References

- [ADR-0177](./0177-imzero2-focus-scoped-keyboard-capture.md) — the keyboard capture primitive SD8 defers to; SD2's cursor is its hook in this widget.
- [ADR-0151](./0151-table-column-width-overrides.md) — the etable column-width epoch protocol this widget inherits.
- [ADR-0154](./0154-headless-carrier-tree-and-driver.md) — the headless driver the verification plan leans on.
- [ADR-0160](./0160-imzero2-icicle-flamegraph-widget.md) — the columnar hierarchy input SD1 follows, and the model/layout/view split SD3 mirrors.
- [ADR-0166](./0166-play-treemap-panel.md) — the second reader of `play_hierarchy.go`'s column contract.
- [ADR-0178](./0178-mdedit-markdown-editor.md) — the fourth adopter, and the source of the 2026-08-09 update above.
- [ADR-0012](./0012-imzero2-collapsible-retained-bodies.md) — why block bodies emit every frame, and what culling is available instead.
