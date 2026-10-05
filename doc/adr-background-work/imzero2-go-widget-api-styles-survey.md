---
type: explanation
audience: imzero2 widget authors and reviewers
status: draft
# reviewed-by: "@<handle>"     # fill in and uncomment when flipping to stable
# reviewed-date: YYYY-MM-DD    # fill in and uncomment when flipping to stable
---

> **Status: draft — pre-human-review.** Not verified; do not cite as authoritative.

# imzero2 Go widgets: a survey of their API styles

Background work for [ADR-0267](../adr/0267-imzero2-go-widget-api-contract-immediate-and-semi-retained.md).
A census of every package under `public/thestack/imzero2/egui2/widgets/`, taken
on 2026-09-29 at commit `e1f3667e`, asking one question per package: how does a
host give it ids, data, state, options and a size, and how does it hand back
what the user did. The counts below are of that snapshot and will drift; the
shapes they describe are what the ADR decides about.

The bindings' own widget contract — `Send` / `SendResp` / `SendRespVal`,
`KeepIter`, the r7 read-back — is settled by
[ADR-0013](../adr/0013-imzero2-stateful-widget-contract.md) and is not the
subject here. This survey is about the **Go widgets built over the bindings**,
which ADR-0013 does not reach and which have grown, over four months, into
eight distinguishable API shapes.

## 0. Scope of the census

| Measured | Value |
|---|---|
| Packages under `widgets/` | 66 |
| Of which widgets (draw into a host's `Ui` or canvas) | 49 |
| Of which libraries the widgets share (colour, colormap, camera, layout engines, highlighters, decoders, tile sources) | 17 |
| Demo files registered through the ADR-0057 registry | 85 |
| Packages with a test that renders a frame headlessly | 6 |
| Packages with a scripted-input (`Script*`) test | 4 |

The 17 non-widget packages are listed in §6 so they are not mistaken for
outliers of the contract: `axisruler` and `legend` are painter helpers,
`camera`, `color`, `colormap`, `imagedecode`, `basemap`, `ecdfdigest`,
`timerangepicker` are value libraries, `gohighlight` / `jsonhighlight` /
`regexhighlight` / `markdownhighlight` are tokenizers, `layeredgraph`,
`sankey`, `icicle`, `pipelineview` are layout engines whose painters live
elsewhere, `scctree` is a data adapter, and `codeview` builds retained
`CodeViewJob` holders.

## 1. The eight shapes

Each row names the shape by the signature a host has to write, gives the
packages that use it, and notes what the shape makes easy and what it makes
impossible.

| # | Shape | Host writes | Packages | Buys | Costs |
|---|---|---|---|---|---|
| S1 | **Function over `Input`/`Result`** | `res := pkg.Render(pkg.Input{Ids, ScopeKey, Model, State, …})` | `tree`, `fsbrowser`, `cardgrid`, `chatview`, `kanban`, `schemaview`, `mappingplanview`, `jobprogress`, `bgjobrow`, `waveform.RenderThumbnail` | No object to construct or free; state is a value the host can persist; two instances differ by `ScopeKey` alone; testable with one call | Nothing may survive a frame except what `State` holds, so no worker, cache or subscription can live inside |
| S2 | **Fluid over one binding** | `pkg.New(id, label).Tone(…).SendResp()` | `badge`, `selector` | Reads exactly like the bindings; zero state; one id | Only for a composite that *is* one interactive widget |
| S3 | **Value-receiver `Renderer` with copy-returning setters** | `pkg.New(idPrefix).ShowX(true).Render(idGen, data)` | `gauge`, `distsummary`, `regexsummary`, `canonicaltypesummary`, `boxenplot`, `ecdf`, `fieldview`, `errorview`, `cbordiag` | Configuration reads as a chain; the value can be built once and reused | Where the widget *needs* state it has nowhere to put it: `distsummary` keeps a package-level `sync.Map` keyed by `idPrefix` that is never pruned; `fieldview` and `cbordiag` bolt a `*State` onto `Render`; ids come from a string prefix the host cannot scope |
| S4 | **Retained object, data set by methods, `Render()` takes nothing** | `t := pkg.New(ids, key, data, opts...)`; per frame `t.SetX(…)`; `t.Render()`; readback via getters | `treemap`, `timeline`, `colorscale`, `heatmapscroll`, `spectrumdisplay`, `worldmap`, `pager`, `taskmonitor`, `leewaywidgets.Table2CardEmitter` | Caches and derived layout live where they belong | Two ways to set every knob (`treemap` has `WithContainerSize` *and* `SetContainerSize`, `WithColoring` *and* `SetColoring`); readback is scattered over getters (`ClickedLeaf`, `HoveredNode`, `Selection`, `CursorTime`) and, in `timeline` and `colorscale`, over listener callbacks that run inside the frame |
| S5 | **Retained object, data declared per frame, `Options` struct, events** | `v := pkg.New(ids, key, pkg.Options{…})`; per frame `v.Render(data, w, h)`; `v.Events()` | `graphview`, `portolan`, `waveform`, `timescrubber`, `leewaywidgets.{Chart,Graph,Hierarchy,Lens}View` | Go owns topology, the widget owns geometry; `Opts` is a public field re-read every frame so a toggle is an assignment; a `RenderFill` twin takes the pane's size | Events come back two ways (`Events()` method in three packages, returned from `Render` in `timescrubber`); data enters three ways (per-frame args, `New`-time track, `SetLayers`) |
| S6 | **Model or editor object whose `Render` takes the ids** | `m.Render(ids, …)` | `canonicaltypeedit`, `markdown.Doc`, `sqleditor.{Editor,Field,Pane}`, `fsmview.Widget[T]`, `filepicker`, `videooutput.Show*`, `trendsmooth.State.RenderControls`, `componentview` | The object is the parsed document or the editing state, and rendering it is a method | Ids arrive at render time, so probe seqs and window ids cannot be derived before the first frame; `sqleditor` returns its result through a second call (`Result()`); `filepicker` names "open the dialog" `Show()` |
| S7 | **Painter helper, no ids** | `pkg.Paint(side, base, lo, hi, ticks, style)` | `axisruler`, `legend`, `inspector.AnchorTether`, `layeredgraph/view` (takes a raw `idBase uint64`) | Draws inside a canvas the host owns; composes by call order | Not widgets; listed so they are not force-fitted |
| S8 | **Gate / inline readout** | `if pane.Skip() { continue }`; `RenderInline(id)` | `lazypane`, `metricsoverlay`, `runtimestatus` | Infrastructure | `RenderInline` is a fourth verb for "render" |

Two of the shapes are the attractors the rest have been drifting toward.
**S1** is what every widget written since 2026-08 that draws in the host's
flow took (`tree` 2026-08-07, `fsbrowser` 2026-08-21, `cardgrid` 2026-09-17,
`chatview` 2026-09-15, `bgjobrow` 2026-09-19), and ADR-0176 §SD2/§SD11 and
ADR-0239 §SD3 already state its rules for the tree and the chat view. **S5**
is what every painter-lane canvas written since 2026-08 took (`portolan`
2026-08-22, `waveform` 2026-08-28, `graphview` 2026-09-10, `timescrubber`
2026-09-20), and ADR-0224 states its rules for the graph. Shapes S3, S4 and
S6 are earlier (2026-05/06) and no widget has adopted them since July.

## 2. Axes along which the shapes disagree

The shape table hides that a package can agree with its neighbours on one axis
and differ on the next. These are the axes, with the spread found on each.

### 2.1 Where the id space enters

Eleven distinct spellings. The user's observation that "ID handling is very
inhomogeneous" is the most measurable of the findings, so it has its own
section (§3).

### 2.2 Where persistent UI state lives

| Location | Packages | Consequence |
|---|---|---|
| Host-owned `*State`, data-only `Model` beside it | `tree`, `fsbrowser`, `cardgrid`, `chatview` | Persist, project stable keys, "select from another pane" are assignments (ADR-0176 §SD11) |
| Inside a `*Model` that also carries the data | `kanban` (`sel`, `moves`, `drag` next to `Columns`/`Cards`), `schemaview` (`sel`, `filter`, `navState` next to `Table`), `mappingplanview` (a `*pager.Pager` and an `*fsmview.Widget` next to `Fields`) | Rebuilding the model from a new query result drops the UI state, or the host must copy it across by hand |
| Inside the retained object | every S4/S5/S6 package | Correct for caches; for selection and view it is fine only when there are getters and setters, which `treemap` and `timeline` have and `worldmap` does not |
| Package-level map keyed by a string | `distsummary` (`instanceStates`, a `sync.Map` keyed by `idPrefix`) | Grows for the life of the process; two hosts that pick the same prefix share pinned state |
| `*State` bolted onto a stateless `Renderer` | `fieldview`, `cbordiag` | Works, but the same widget then has configuration in one place and state in another |
| None | `badge`, `selector`, `gauge`, `jobprogress`, `bgjobrow` | Correct for what they are |

### 2.3 How data enters

| Route | Packages |
|---|---|
| Per-frame argument, columnar `Model` by pointer | `tree`, `fsbrowser`, `cardgrid`, `chatview`, `kanban`, `schemaview` |
| Per-frame argument, row structs | `graphview.Render(nodes []NodeSpec, edges []EdgeSpec, …)` — with a `RenderColumns` twin |
| Per-frame scalar | `gauge.Render(idGen, value)`, `timescrubber.Render(w, steps)` |
| Fixed at `New`, replaced by a setter | `treemap` (`root`, `SetRoot`), `timeline` (`intervals`, `SetIntervals`), `waveform` (`*track.Track`) |
| Pushed incrementally | `heatmapscroll.PushColumn`, `spectrumdisplay` |
| Pulled from a bus by the widget itself | `taskmonitor` (subscribes to `task.>`, holds a mutex) |
| Parsed into the object | `markdown.Doc`, `canonicaltypeedit.Model` |

### 2.4 How options enter

| Mechanism | Packages | Note |
|---|---|---|
| Fields on `Input` | all S1 | Zero value is the default; per frame; nothing to construct |
| `Options` struct at `New`, exposed as public `Opts` re-read every frame | `graphview`, `timescrubber` | The convention ADR-0224 fixed |
| `Options` struct at `New`, private afterwards | `portolan`, `taskmonitor` (`Opts`) | Changing one means a new widget |
| Functional `...Option` at `New` | `treemap` (15 `With*`), `timeline` (17 `With*`), `colorscale` (6), `filepicker`, `fsmview` (generic `Option[T]`) | Each option is a function plus, in `treemap`, a `Set*` twin for the same field |
| Functional options per render call | `markdown.RenderOpt` (`WithScrollToSection`, `WithLinkRouter`, …) | Allocates a closure slice per frame |
| Copy-returning setters on a value | S3 (`ShowIcon(b) Renderer`) | Reads well; cannot hold state |
| `Set*` methods on the object | `treemap`, `timeline`, `heatmapscroll` | Idempotent per the house `Set` rule, but duplicates the constructor options |

### 2.5 How interaction comes back

| Mechanism | Packages |
|---|---|
| `Result` struct returned from `Render`, sentinels `-1` / `false`, scratch slices valid until the next `Render` | `tree`, `fsbrowser`, `chatview`, `cardgrid` |
| Bare `bool` returned | `pager.Render() bool`, `jobprogress`, `bgjobrow` |
| Tuple returned | `worldmap.Render() (clicked, ok)`, `filepicker.Render(ids) (action, paths)` |
| `Events` struct returned from `Render` | `timescrubber` |
| `Events()` method after `Render`, valid until the next `Render` | `graphview`, `portolan`, `waveform` |
| Getters after `Render` | `treemap.ClickedLeaf/HoveredNode`, `timeline.Selection/CursorTime/Brush`, `portolan.Hover/Clicked`, `heatmapscroll` |
| Separate `Result()` method | `sqleditor.Editor` |
| Listener callbacks registered on the object | `timeline` (`onSelection`, `onBrush`), `colorscale` (`onHover`) |
| Drained from the model | `kanban.Model.DrainMoves()` |
| `iter.Seq` of actions | `markdown.Doc.RenderActions` |
| `c.ResponseFlagsE` | `badge.SendResp()` |
| `changed bool` | `selector.SendResp()` |

Twelve mechanisms for one concept. The two that a reviewer cannot audit from
the call site are the listener callbacks (control leaves the host's frame
function and comes back inside the widget's `Render`) and the drain-from-model
(`kanban` mutates the host's `Model` to report a move).

### 2.6 Who decides the size

| Mechanism | Packages |
|---|---|
| Host passes `w, h`; `RenderFill(fallbackW, fallbackH)` twin probes the pane | `graphview`, `portolan`, `waveform` |
| Host passes `w` only; `RenderFillWidth(fallbackW)` twin | `timescrubber`, `waveform` |
| `Input.MaxHeight` ceiling, `Input.FillHost` fill | `tree`, `fsbrowser`, `chatview`, `kanban`, `schemaview`, `mappingplanview`, `sqleditor.PaneInput` |
| Widget probes its own pane every frame | `spectrumdisplay` (`availW/availH`), `timeline` (`containerW`), `cardgrid` (`paneW`), `chatview` (`paneW`), `timescrubber` |
| `WithContainerSize` / `SetContainerSize` | `treemap` |
| Explicit `dispW/dispH`, zero meaning "derive" | `spectrumdisplay` |
| Natural size, no knob | `badge`, `selector`, `gauge` (via `SizeE`), `jobprogress` |

The probe seq those widgets use differs too, and one variant is a latent
collision: see §3.4.

### 2.7 Verbs, receivers, type names

- **Entry verbs in use:** `Render`, `RenderFill`, `RenderFillWidth`,
  `RenderInline`, `RenderControls`, `RenderReport`, `RenderChip`,
  `RenderPopup`, `RenderMinimap`, `RenderFrontmatter`, `RenderActions`,
  `Show`, `ShowGallery`, `ShowStatus`, `ShowDialog`, `Paint`, `Draw`, `Send`,
  `SendResp`, `Skip`. `Show` means "render" in `videooutput` and "open the
  dialog" in `filepicker`.
- **Type names that say nothing:** `Renderer` (7 packages), `Inst`
  (`filepicker`, `taskmonitor`), `Widget` (`fsmview`, `worldmap`). The
  packages written since August name the object (`View`, `Map`, `Player`,
  `Scrubber`, `Pane`).
- **Receivers:** `inst` in most retained objects, `t` in `treemap`, `v` in
  `graphview`, `m` in `portolan` and the models, `p` in `implot`.
- **Generics:** `selector` and `fsmview` are generic over the enum they bind.

### 2.8 Threads and lifetimes

`taskmonitor` subscribes to the bus and guards its rows with a mutex;
`portolan` runs a tile loader; `waveform` runs a peaks build; `graphview`
splits its force step across cores and joins inside `Render`. All four hand
results to the render goroutine correctly, which is
[ADR-0261](../adr/0261-one-render-goroutine-for-every-app.md)'s rule, but
only `waveform`'s `Close()` and `taskmonitor`'s `unsubscribe` release
anything, and neither the S4 nor the S5 shape says whether a widget must be
closed.

### 2.9 Tests

Forty-nine widgets, six with a test that renders a frame under the discard
channel, four with a scripted-input test (`graphview`, `portolan`,
`timescrubber`, `waveform`), four with no test at all (`badge`, `pager`,
`metricsoverlay`, `trendsmooth`). The recipe exists — `graphview`'s
`scenetest` harness and the `StateManager.Script*` setters — and the S1/S5
shapes are the ones that make it a one-call test; the S3/S6 shapes need an
`idGen` or a parsed object first.

## 3. Id handling, in detail

Eleven idioms across 49 widgets. The table gives each, an example, and the
failure it either causes or invites. The two rows marked ✓ are the ones the
recent widgets converged on and the ADR keeps.

| # | Idiom | Example | Packages | Failure |
|---|---|---|---|---|
| I1 ✓ | `Ids *c.WidgetIdStack` + `ScopeKey string` on `Input`; the widget opens one `c.IdScope(ids.PrepareStr(scopeKey))` and every child is relative | `tree`, `chatview` | `tree`, `fsbrowser`, `cardgrid`, `chatview`, `kanban`, `schemaview`, `mappingplanview` | Two instances under one host differ by `ScopeKey`; the host may nest the widget anywhere |
| I2 ✓ | `ids *c.WidgetIdStack, key string` at `New`, stored, scope opened at every `Render` | `graphview.New(ids, "deps", …)` | `graphview`, `portolan`, `waveform`, `timescrubber`, `treemap`, `timeline`, `colorscale`, `heatmapscroll`, `spectrumdisplay`, `pager`, `worldmap` | Same as I1; ids are available before the first frame for probe seqs |
| I3 | `idPrefix string` at `New`, concatenated into every child key: `ids.PrepareStr(idPrefix + ":hdr-inflight")`, `fmt.Sprintf("%s-d-%d-%d", idPrefix, si, fi)` | `taskmonitor`, `errorview`, `fieldview`, `cbordiag` | The prefix does what a scope does, at a string concatenation per child per frame; two hosts that pick the same prefix under different parents do *not* collide (the stack still differs), so the prefix is redundant with I1/I2 |
| I4 | `idPrefix string` turned into **absolute** ids: `c.MakeAbsoluteIdStr(scope + "-anchor-toggle")` | `distsummary`, `regexsummary`, `canonicaltypesummary`, `inspector` | Absolute ids ignore the host's stack. Two hosts that pick the same prefix — or one host rendering the same inspector for two values — share a toggle, a window and a probe slot; the package docs say "must use distinct idPrefixes", which is the rule I1 makes unnecessary. Also the reason `distsummary`'s state map has to be global: there is no scope to hang state on |
| I5 | `id c.WidgetIdCreatorI` for a single control | `badge.New(id, label)`, `selector.RadioValue(id, …)`, `jobprogress.Input.CancelId` | ✓ for the S2 shape (mirrors the bindings). Misused when a multi-widget renderer takes one creator and has to fan it out: `gauge.Render(idGen, value)` and `distsummary.Render(idGen, …)` derive a `callId` and open `IdScope(PrepareHighEntropy(callId))` under it |
| I6 | Ids at `Render`, not at `New` | `markdown.Doc.Render(ids, …)`, `canonicaltypeedit.Model.Render(ids, scopeKey)`, `sqleditor.Editor.Render(ids, …)`, `componentview` | The object cannot derive its probe seq or window id before the first frame, so `sqleditor` and others compute a `probeSalt` lazily |
| I7 | Magic sequence numbers | `ids.PrepareSeq(0xC7ED17)`, `ids.PrepareSeq(0x60000 + accent*16 + col)` | `canonicaltypeedit`, `leewaywidgets`, `mappingplanview`, `markdown`, `fsbrowser` (10 sites) | Unique by hope; a collision is the silent read-back loss of SKILL §11 |
| I8 | Ids that are stable within a frame only | `markdown` numbers actionable code blocks `PrepareSeq(0), PrepareSeq(1), …` in document order | `markdown` | A block inserted above shifts every id below it; egui memory for those blocks (scroll, collapse) resets — the "Jumping UI" pitfall |
| I9 | Raw `uint64` id base | `layeredgraph/view.Render(idBase uint64, …)`, `fsmview` `stateBadge(idBase, seq, …)` | `layeredgraph/view`, `fsmview` | The caller must know how many ids the widget will consume above the base |
| I10 | Absolute id for a cell, with a hand-built handle | `treemap.cellIds(seq)` returns `c.AbsoluteWidgetId(id)` and `widgethandle.Make(...)` | `treemap` | Two treemaps under one host share cell ids unless their scope keys differ, which the absolute id does not guarantee |
| I11 | `scope string` for an absolute-id form beside the scoped form | `selector.SegmentedAbs(scope, current)` next to `selector.Segmented(ids, scopeKey, current)` | `selector` | Exists only because I4 hosts have no stack to pass |

### 3.4 Probe seqs are a fourth id space

Layout probes (`c.CapturePaneSize`, `c.CaptureUiRect`) are keyed by a `seq`
that must be unique per instance in the process. Five derivations were found:

| Derivation | Packages | Property |
|---|---|---|
| `c.ProbeSeq(scopeKey, role)` — FNV over the two strings, **stack-independent** | `chatview`, `cardgrid`, `schemaview`, `filepicker` | Two hosts that both say `ScopeKey: "chat"` under different parent scopes share one probe slot and read each other's pane size. Nothing at either call site shows it |
| `c.ProbeSeq(scopeKey, role) ^ probeSalt`, with `probeSalt = ids.PrepareHighEntropy(pkgSeed).Derive()` computed lazily on the first `Render` | `timescrubber`, `timeline`, `waveform`, `spectrumdisplay`, `sqleditor` | Stack-dependent, so unique per host scope — the correct property, reached by five copies of the same eight lines |
| A hash of `idPrefix` plus a call id, or `c.ProbeSeq` over a scope string that carries the call id | `gauge.readoutMeasureId`, `distsummary` | Stack-dependent after all — the call id is the caller's derived id — but the widget takes a `c.WidgetIdCreatorI` that may be absolute, so it has no stack of its own to derive from (corrected 2026-09-29 while landing ADR-0267 M1; the first draft called these stack-independent) |
| A hash of a string key the caller must namespace | `lazypane.New(key, …)` | Documented as "must be unique among all r21 probe users in the process" |
| A per-instance `PrepareHighEntropy` under the widget's scope | `treemap`, `implot` | Stack-dependent |

The fix is one binding helper that derives the seq from the *current stack*
plus a role, so that every widget writes the same line and none can be
stack-independent by accident. That is the first item of ADR-0267's migration.

## 4. What already conforms

Nothing needs to be invented. The attractors are already in the tree, with
their rules already written down:

- **Immediate:** `tree` (ADR-0176 §SD2, §SD11, §SD12), `chatview`
  (ADR-0239 §SD3), `cardgrid`, `fsbrowser`. `Input{Ids, ScopeKey, Model,
  State, …}` → `Result`, one `IdScope`, host-owned `State` keyed on ordinals
  with a documented projection rule, `MaxHeight` / `FillHost` for size,
  scratch slices in `Result` valid until the next call.
- **Semi-retained:** `graphview` (ADR-0224 §SD4, §SD12), `timescrubber`,
  `portolan`, `waveform`. `New(ids, key, Options) *Noun`, public `Opts`
  re-read every frame, per-frame data declaration, `Render(w, h)` with a
  `RenderFill` twin, events one frame behind, scripted headless tests.

The disagreements that remain *between* the attractors — `Events()` method
versus returned `Events`, per-frame data versus `Set*` — are the only
decisions the ADR has to make rather than record.

## 5. Consumer footprint

Importers per widget package, excluding the package itself, its tests and the
generated `packageprops` table (which imports everything). This is the cost
basis for the migration order: a widget with two importers can change its
signature in one commit; `markdown` cannot.

| Importers | Packages |
|---|---|
| ≥ 20 | `color` (154, a library), `badge` (31), `codeview` (30, a library), `implot` (28, a lane), `markdown` (25), `colormap` (21, a library) |
| 10–19 | `selector` (19), `tree` (19), `portolan` (15), `treemap` (15), `colorscale` (14), `inspector` (13), `layeredgraph` (13), `leewaywidgets` (13), `sqleditor` (13), `basemap` (13), `graphview` (12), `jobprogress` (12), `regexedit` (11), `fsbrowser` (10) |
| 5–9 | `fsmview` (9), `schemaview` (9), `timeline` (9), `worldmap` (9), `timerangepicker` (8), `axisruler` (7), `bgjobrow` (7), `ecdf` (7), `chatview` (6), `componentview` (6), `distsummary` (6), `filepicker` (6), `heatmapscroll` (6), `icicle` (6), `lazypane` (6), `legend` (6), `pager` (6), `sankey` (6), `boxenplot` (5), `fieldview` (5), `imagedecode` (5), `pipelineview` (5), `regexsummary` (5), `taskmonitor` (5), `timescrubber` (5), `trendsmooth` (5), `waveform` (5) |
| ≤ 4 | `canonicaltypeedit`, `canonicaltypesummary`, `cardgrid`, `cbordiag`, `errorview`, `gauge`, `kanban`, `mappingplanview`, `spectrumdisplay`, `videooutput`, `metricsoverlay`, `runtimestatus`, `scctree`, and the tokenizers |

Apps importing the most widget packages: `play` (nearly all), `imztop`,
`imzrt`, `tally`, `mdedit`, `chat`, `watchbill`, `terrainscope`.

## 6. Non-widgets in the directory

Kept out of the contract on purpose: `axisruler`, `legend` (painter helpers
called from inside another widget's canvas); `camera`, `color`, `colormap`,
`imagedecode`, `basemap`, `ecdfdigest`, `timerangepicker` (value types and
loaders); `gohighlight`, `jsonhighlight`, `regexhighlight`,
`markdownhighlight` (tokenizers); `layeredgraph`, `sankey`, `icicle`,
`pipelineview` (layout engines — their painters are in `leewaywidgets`,
`fsmview` and `layeredgraph/view`); `scctree` (adapter); `codeview` (retained
job builders); `implot` (a plotting lane with its own ADR-0149 contract);
`inspector` (tether infrastructure); `lazypane` (a gate). Whether some of
these should move out of `widgets/` is a housekeeping question the ADR notes
but does not decide.

## 7. Observations that shaped the ADR

1. **Two modes, not one.** The S1 and S5 attractors are both right for what
   they hold. Forcing canvases into `Input`/`Result` would put pyramids and
   workers into a host-owned `State`; forcing outlines into `New`/`Render`
   would make every tree an object to construct and free. The line between
   them is testable: *does anything have to survive a frame that cannot be
   re-derived from the model?*
2. **Everything else is one axis at a time.** Ids, state, options, readback,
   size and naming each have one clear winner among the shapes in the tree,
   and the winner is in every case the one the most recent widgets chose.
3. **Two idioms cause bugs today, not just inconsistency:** stack-independent
   probe seqs (§3.4) and absolute ids from a caller-chosen prefix (I4). Both
   are fixed by giving the widget a scope and deriving everything from it.
4. **Callbacks for interaction are the one mechanism to retire outright.**
   Callbacks for *host-drawn content* (`Cell`, `Block`, `Hero`, the portolan
   overlay) are the opposite case and stay: they let the host draw inside a
   slot the widget positions.
5. **The migration is mostly renames and field moves.** Only `distsummary`
   (global state), `kanban` / `schemaview` / `mappingplanview` (state inside
   the model), `timeline` (listeners) and `markdown` (per-frame ids, 25
   importers) change behaviour a host can observe.
