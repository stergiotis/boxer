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
section of `egui2_hl_imzero2_demo.go` — while four widgets that display
hierarchies (`fieldview`, `schemaview`, `componentview`, and keelson's
`configview`) each hand-roll a recursive `CollapsingHeader` instead. A binding
with a demo and no adopter, sitting beside four hand-rolled reimplementations of
what it does, is evidence about the binding rather than about the need.

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

- **SD2 — Expansion and selection live in a caller-owned `State`.** The widget
  reads and mutates it; it never keeps hidden per-frame state and never leaves
  authority in Rust. This is the kanban / layeredgraph `ViewState` / treemap
  breadcrumb pattern, and it is what makes expansion persistable, restorable,
  and settable from code — the single largest thing `egui_ltreeview` cannot do.

- **SD3 — Flattening is pure and is where the tests live.** `layout.go` turns
  `(Tree, State)` into `[]Row{NodeIdx, Depth, HasChildren, Expanded, IsLastChild}`
  with no bindings import, mirroring the `model.go` / `layout.go` / render split
  that `icicle`, `sankey`, and `layeredgraph` already use. Every ordering,
  depth, and collapse rule is asserted without a renderer.

- **SD4 — `endETable` is the row engine.** The widget emits one etable row per
  flattened row, gated on `et.VisibleRange()`. Virtualisation, per-row heights,
  `ScrollToRow` for reveal-selection, and the column-width protocol come with
  it rather than being rebuilt.

- **SD5 — `row_ui` becomes a deferred block map on `endETable`.**
  `WithDeferredBlockMap("rows", ctabb.U64)` generates `BeginRows(row)` /
  `EndRows()`; the existing locally-scoped `EtStripedDelegate` implements
  `row_ui` by replaying the block. `read_deferred_block_map_u64` and
  `skip_deferred_block_map_u64` already exist (`rust/imzero2/src/fffi/io.rs:658`,
  `:674`), and the decorator already lives in the IDL apply string, so
  `interpreter.rs` is not hand-edited. This is the whole of C5's cost.

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

- **SD8 — Keyboard navigation is deferred, not designed.** There is no
  general key-event channel in the bindings — `GetModifiers` and
  `GetF1KeyPressed` are the whole surface — so arrow-key navigation needs a new
  register and fetcher. That is a separate decision about input, not about
  trees, and gating this widget on it would gate the light cut on the hardest
  tenth. Pointer interaction ships first.

- **SD9 — Indent guides are deferred.** The vertical lines connecting a parent
  to its children want row-relative painting, which `row_ui` makes possible but
  which is decoration, not function. `IsLastChild` is carried in `Row` from M1
  so adding them later needs no model change.

- **SD10 — `egui_ltreeview` is removed, not deprecated.** Keeping it means
  keeping a crate, a global register, a `NodeCommand` enum, and a panic guard,
  to serve two demos. Removal is gated on parity and is the last milestone.

### Milestones

- **M0 — `row_ui` deferred block map on `endETable`.** The IDL declaration, the
  `row_ui` implementation with SD6's seen-set guard, and regeneration. Ported
  `logviewer` from five `TintedScope` cells to one row block, which is both the
  payoff and the proof the guard holds. Independently useful with no tree in
  sight.
- **M1 — `model.go` + `layout.go`.** The columnar input, its validation, the
  `State`, and the pure flatten with its test suite. No bindings import.
- **M2 — `render.go`.** The etable renderer: row blocks, indent, disclosure,
  selection, `VisibleRange` gating, reveal-selection via `ScrollToRow`.
- **M3 — Port the in-repo callers.** `fieldview`, `schemaview`,
  `componentview`, `configview`, one at a time, each verified in its own host.
- **M4 — Demo + tour entry.** A registry `Demo` replacing `tree-view`, and the
  node section of the imzero2 demo.
- **M5 — Remove `egui_ltreeview`.** The IDL nodes, the `r3_node_cmds` register,
  the `NodeCommand` enum, the crate dependency, and the imzero2 skill's §7.

## Surfaces — Tier 1

| Surface | Change | Moves with it |
| --- | --- | --- |
| egui2 IDL — `endETable` (`egui2_definition_d_table2.go`) | added: `rows` deferred block map, keyed `U64` | regenerated `bindings/methods.out.go` + `factories.out.go`, regenerated region of `interpreter.rs`; both the Go binary and the Rust renderer rebuilt in the same commit |
| egui2 IDL — `tree`, `nodeDir`, `nodeLeaf`, `nodeDirClose` | removed at M5 | `r3_node_cmds` register and `NodeCommand` enum in `interpreter.rs`, `prepare_next_frame` clear arm, the two demos |
| `rust/imzero2/Cargo.toml` | `egui_ltreeview` dependency removed at M5 | `Cargo.lock`, the licence and vendor gates |
| Exported Go API under `public/` | added: `widgets/tree` | nothing yet — no downstream module compiles against it on day one |
| imzero2 skill §7 "The Node & Tree System" | rewritten at M5 | `.claude/skills/imzero2/SKILL.md`; §13.1's register table loses its `r3_node_cmds` row |

## Alternatives

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
- Rows can carry badges, counts, columns, and controls, so the four hand-rolled
  `CollapsingHeader` trees have something to converge on.
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
- No keyboard navigation until SD8's input work happens, which is a real
  regression against `egui_ltreeview` for keyboard-first users.

### Neutral

- "Native Go over existing primitives" still rests on `egui_table`, a
  third-party crate. It is one already load-bearing in `play`, `imztop`,
  `logviewer`, and `fsmview`, so the trade is one lightly-used dependency for
  deeper use of a heavily-used one — not the removal of a dependency class.
- The widget will look different from `egui_ltreeview`, since it is themed from
  `SelectableLabel` and etable striping rather than the crate's own visuals.

## Migration — Tier 1

- **Breaks.** Nothing at M0 through M4 — the `rows` block map is additive and
  the existing tree binding is untouched. At M5, `c.Tree`, `c.NodeDir`,
  `c.NodeLeaf`, and `c.NodeDirClose` stop existing, along with the
  `NodeCommandS` retained type and `ResponseFlagsE.HasNodelikeSelected`.
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

## Verification plan — Tier 1

- **Lane.** Default `go test` for `layout_test.go` (SD3's flatten: ordering,
  depth, collapse, forest roots, cycle and dangling-parent rejection). The
  screenshot tour for the M4 demo. A headless scene (ADR-0154) for
  click-to-select and click-to-expand.
- **What would fail.** A flatten regression fails `go test` directly. An SD6
  guard regression is the interesting one: doubled row ids do not break
  rendering or clicking, so the observable is the headless scene's
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

## Status

Proposed — awaiting review.

Status lifecycle: `Proposed → Accepted → (Deferred | Deprecated | Superseded by ADR-XXXX)`.
See [DOCUMENTATION_STANDARD §1 ADR](../DOCUMENTATION_STANDARD.md#architecture-decision-records-why-it-is-this-way) for the edit-policy tiers (Tier 1 in-place / Tier 2 dated `## Updates` entry / Tier 3 new superseding ADR).

<!--
## Updates

Tier-2 dated entries land here when implementation reveals a refinement, an aspirational
claim turns out false, or a milestone records what shipped. Single H2; add H3s dated
YYYY-MM-DD. Remove this HTML comment when the section first gains a real entry.
-->

## References

- [ADR-0151](./0151-table-column-width-overrides.md) — the etable column-width epoch protocol this widget inherits.
- [ADR-0154](./0154-headless-carrier-tree-and-driver.md) — the headless driver the verification plan leans on.
- [ADR-0160](./0160-imzero2-icicle-flamegraph-widget.md) — the columnar hierarchy input SD1 follows, and the model/layout/view split SD3 mirrors.
- [ADR-0166](./0166-play-treemap-panel.md) — the second reader of `play_hierarchy.go`'s column contract.
- [ADR-0012](./0012-imzero2-collapsible-retained-bodies.md) — why block bodies emit every frame, and what culling is available instead.
