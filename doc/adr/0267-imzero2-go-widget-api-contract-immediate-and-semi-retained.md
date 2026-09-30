---
type: adr
status: proposed
date: 2026-09-29
# reviewed-by: "@<handle>"     # fill in and uncomment when flipping to accepted
# reviewed-date: YYYY-MM-DD    # fill in and uncomment when flipping to accepted
---

> **Status: proposed — pre-human-review.** Decision under consideration; do not implement as if accepted.

# ADR-0267: imzero2 Go widgets — one contract for immediate-mode widgets, one for semi-retained widgets

## Context

The Go widgets under `public/thestack/imzero2/egui2/widgets/` — the trees,
maps, graphs, editors and inspectors built *over* the egui2 bindings — have no
API contract of their own. [ADR-0013](0013-imzero2-stateful-widget-contract.md)
fixes the bindings' `Send` / `SendResp` / `SendRespVal` shape and stops at the
binding boundary; each Go widget above it chose a shape when it was written.
Four months in, the census in
[imzero2-go-widget-api-styles-survey](../adr-background-work/imzero2-go-widget-api-styles-survey.md)
finds 49 widgets in eight shapes, disagreeing along every axis a host has to
learn: eleven ways to receive an id space, six places persistent state lives,
seven option mechanisms, twelve ways to report what the user did, seven ways to
be given a size, and a vocabulary of twenty entry verbs.

Two of the inconsistencies are bugs, not taste. A widget that keys its layout
probe on `c.ProbeSeq(scopeKey, role)` alone shares the slot with every other
instance whose host chose the same scope key, because that hash ignores the id
stack; four widgets do this. A widget that builds **absolute** ids from a
caller-supplied `idPrefix` cannot be nested twice under one host without the
caller inventing distinct prefixes, and has nowhere but a process-global map to
keep state; four widgets do that, one with a `sync.Map` that is never pruned.

The survey also finds that the drift has already stopped. Every widget written
since August 2026 that draws in the host's flow took the `Input` / `Result`
function shape the tree introduced (ADR-0176 §SD2, §SD11; ADR-0239 §SD3), and
every painter-lane canvas written since August took the `New` / `Render` /
events shape the graph introduced (ADR-0224). What is missing is the sentence
that says *these two, and only these two*, and a plan for the widgets written
before it.

## Decision

Every Go widget is one of three shapes, chosen by one test, and each shape has
one contract. The rules are numbered so the migration table and a conformance
test can cite them.

### The three shapes and the test that picks one

| Shape | Test | Signature the host writes |
|---|---|---|
| **F — fluid** | The widget *is* one interactive binding widget with one id and no state (a badge, a radio bound to an enum) | `pkg.New(id c.WidgetIdCreatorI, …).Knob(…).Send()` / `.SendResp()` |
| **IM — immediate-mode** | Nothing has to survive a frame except what can be named as a small host-owned value: selection, expansion, cursor, a scroll target, a bound text buffer | `res := pkg.Render(pkg.Input{Ids, ScopeKey, Model, State, …})` |
| **SR — semi-retained** | Something must survive a frame that cannot be re-derived from the model each frame: a cache (layout, tile pyramid, texture, measurer), a camera or view, a simulation, a worker, a subscription | `w := pkg.New(ids, scopeKey, pkg.Options{…})`; per frame `ev := w.Render(model, …)`; `w.Close()` when the widget has background work |

The F shape is the bindings' own contract (ADR-0013) reached through pure Go
composition; this ADR adds nothing to it beyond W1 and W3. "Semi" in SR is
literal: the *declaration* is still immediate — the host says every frame what
exists — and the object retains only what is derived. A widget that retains
the model itself (a parsed document, an edit buffer) is an SR widget whose
model is loaded through a method, not a fourth shape.

An IM widget may embed only IM and F widgets and stateless helpers. The moment
it needs an SR child, it is SR (W16). This is what settles the ambiguous cases
in the tree: `mappingplanview` holds a pager and an FSM view, so it is SR;
`fieldview` holds only a `tree.State`, so it is IM.

### W1–W3 · Entry points and names

- **W1 — One verb per shape.** `Render` for IM and SR widgets that lay out in
  the host's `Ui`; `Paint` for a helper that draws into a canvas the host owns
  and takes no ids; `Send` / `SendResp` for F. `Show`, `Draw`, `RenderInline`,
  `RenderControls`, `RenderReport` are not entry verbs. A widget with several
  separately placeable parts names each `RenderPart` (`RenderChip`,
  `RenderPopup`, `RenderMinimap`); a widget with a size-probing twin names it
  `RenderFill` (W12).
- **W2 — Type names.** IM: `Input`, `Result`, `State`, `Model`. SR: `Options`,
  `Events`, and the object named by what it is (`View`, `Map`, `Player`,
  `Editor`, `Monitor`) — never `Inst`, `Widget` or `Renderer`. F: `Fluid`.
  Pointer receivers are named `inst`, as the house style already does in most
  of the tree. IM helpers may hang off `Input` (`func (in Input) renderRows()`),
  since `Input` is the only value that exists for the duration of the call.
- **W3 — What `Render` produces, `Render` returns.** IM returns `Result`; SR
  returns `Events`. No separate `Result()` or `Events()` accessor, no
  `(clicked, ok)` tuple, no bare `bool` — a `Result` with one field is still a
  struct, because the second field always comes. Sentinels: `-1` for "no
  ordinal", `false` for "did not happen", `0` for "no id" only where ids are
  documented never to be zero. Slices in a `Result` or `Events` are the
  widget's scratch, valid until the next `Render`, and the doc comment says
  so once, on the type.

### W4–W7 · Identity

- **W4 — One id space, one scope.** An IM widget takes `Ids *c.WidgetIdStack`
  and `ScopeKey string` on `Input`; an SR widget takes `ids *c.WidgetIdStack,
  scopeKey string` as the first two arguments of `New` and stores them. Both
  open exactly one `c.IdScope(ids.PrepareStr(scopeKey))` at the root of every
  `Render` and derive every child id **relative**, under it. `ScopeKey` is the
  only thing two instances under one host need to differ in. There is no
  `idPrefix`; a string that is concatenated into child keys is doing what the
  scope already does, once per child per frame.
- **W5 — Child ids come from the vocabulary, not from constants.** A singleton
  child is `ids.PrepareStr("literal")`. A repeated child is
  `ids.PrepareSeq(uint64(ordinal))` inside a per-row `c.IdScope`, or a
  `PrepareStr` of the row's stable key when ordinals shift between frames (the
  ADR-0176 §SD11 projection rule). Hexadecimal seeds, `idBase + n` arithmetic
  and `fmt.Sprintf` keys are not used: the first two are unique by hope, the
  third allocates per frame, and none of them says what the id is *for*.
- **W6 — Absolute ids only for a floating surface the widget owns.** A
  `c.Window`, popup or tether the widget floats above the host is the one
  legitimate absolute id, and it is derived *from the scope*, never from a
  caller-supplied string: `c.MakeAbsoluteIdHighEntropy(ids.PrepareStr("window").Derive())`.
  Cells, toggles, buttons and sense regions are never absolute.
- **W7 — Probe seqs are derived from the stack.** A layout probe
  (`c.CapturePaneSize`, `c.CaptureUiRect`) or measure id is keyed by a seq the
  widget obtains from `WidgetIdStack.ProbeSeq(role)`, called inside its root
  scope: one binding method that hashes the role into the current
  stack-derived id, side-effect free. `c.ProbeSeq(scopeKey, role)` alone is
  stack-independent and is retired for widgets; the five hand-rolled
  `probeSalt` copies become one line.

### W8–W10 · Model and state

- **W8 — The model is the host's, and it is data.** What a widget draws is a
  `Model` the host owns and passes by pointer every frame (IM: on `Input`; SR:
  as the argument of `Render`, or through one idempotent `Set*` when the data
  is expensive to re-declare — a track, a tile source, a parsed document).
  A `Model` carries no UI state: no selection, no filter text, no drag, no
  child widgets. Where the widget iterates rows, the model is **columnar** —
  one slice per attribute over ordinals, ragged attributes as a value slice
  plus an offset slice — the shape `tree`, `chatview` and `cardgrid` already
  take and the leeway read surface produces.
- **W9 — Persistent UI state is either the host's `State` or the object's,
  and it is never global.** IM: a `State` struct whose zero value is usable,
  passed as `*State`, keyed on ordinals, with the projection rule for a
  rebuilt model documented on the type (ADR-0176 §SD11). SR: inside the
  object, but every part a host might persist or drive — selection, view,
  camera, expansion — has a getter and an idempotent setter. Package-level
  maps keyed by a name are not state storage.
- **W10 — Bound values live in `State`.** A widget that wraps a data-bound
  binding (`TextEdit`, `DragValue`, `Slider`) keeps the bound `*T` in its
  `State` (IM) or object (SR), because the binding writes it one frame later
  (SKILL §12 "Stable Pointers"). Programmatic writes to a bound value go
  through `StateManager.OverrideDatabinding*`, inside the widget.

### W11–W12 · Options and size

- **W11 — Options are a struct, read every frame.** IM: fields on `Input`,
  zero value the default. SR: an `Options` struct passed to `New` and kept as
  a public `Opts` field the widget re-reads on every `Render`, so a toggle is
  an assignment and a layout parameter change needs no rebuild (ADR-0224's
  convention). Functional `...Option` parameters and copy-returning setters
  (`ShowX(b) Renderer`) are not used for widget configuration; a `Set*` method
  exists only for the expensive-model case of W8, never as a twin of a
  constructor option. Per-call functional options (`RenderOpt`) become
  `Input` fields.
- **W12 — A widget declares exactly one sizing shape.** (a) **Canvas:**
  `Render(model, w, h)` takes pixels, and `RenderFill(model, fallbackW,
  fallbackH)` probes the pane with a W7 seq and falls back for the frame the
  probe has not answered. (b) **Flow:** `Input.MaxHeight` is a ceiling and
  `Input.FillHost` says the host already bounds the widget (SKILL §12
  "FillHost"); the widget passes them down to its etable or scroll area and
  probes nothing itself. (c) **Inline:** natural size, no knob. A widget never
  sizes itself against a probe it emitted *after* its own content (the
  ratchet the probe doc warns about), and a host never passes a widget a size
  it measured inside that widget's region.

### W13–W16 · Interaction, threads, composition

- **W13 — Interaction comes back as data, never as a callback.** Clicks,
  activations, toggles, moves, selection changes, view changes are fields of
  `Result` / `Events`. Listener registration (`OnSelection`, `OnHover`,
  `OnBrush`) is retired: it runs host code inside the widget's `Render`, where
  the host cannot see it in the call site and cannot order it against its
  other frame work. Callbacks remain for exactly two things: **host-drawn
  content** in a slot the widget positions (`Cell`, `Block`, `Hero`, an
  overlay `func(Projector)`) and **pure functions** (a label formatter, a
  colour function, a filter predicate).
- **W14 — Everything runs on the render goroutine; workers hand off.**
  Per [ADR-0261](0261-one-render-goroutine-for-every-app.md), no `c.*` call
  leaves the render goroutine. An SR widget with a loader, a build or a bus
  subscription drains its results at the start of `Render` through a channel
  or an atomic, exposes `Close()` that stops the work, and says in its package
  doc that `Close` is required. An IM widget has no goroutines.
- **W15 — Host-skippable regions.** An IM widget is idempotent per frame by
  construction. An SR widget with a send-once protocol (a texture, a delta
  ring, a one-shot setting) re-arms on `StateManager.TextureStarved` or a
  host register, per SKILL §12 "Lost Sends"; it never trusts its own
  "already sent" memory.
- **W16 — Composition.** A widget embeds another by passing its own `Ids`
  and a literal sub-`ScopeKey`. IM embeds IM and F only; SR embeds anything
  and owns its SR children's lifetimes (constructs them in `New`, closes them
  in `Close`). A widget that draws into a canvas it does not own follows the
  hosted-rendering pair of ADR-0228 and is not a fourth shape.

### W17–W19 · Errors, docs, tests

- **W17 — A broken model is drawn as a message and reported, never a panic
  and never silence.** A structurally invalid model (a dangling parent, a
  ragged co-array of the wrong length) is a host programming error: the widget
  draws the error text where the content would be and sets `Result.Err`
  (`tree`'s rule), so the failure does not look like an empty result.
- **W18 — The package doc says which shape, in its first paragraph,** names
  the ADR that decided the widget, and states the one-frame lag of every
  readback once. A demo is registered per ADR-0057 and draws into the host's
  current scope with no chrome of its own.
- **W19 — Two tests minimum.** (1) A frame renders under the discard channel
  without a host and without panicking, from a zero-value `State` / a fresh
  `New`. (2) For any widget that reacts to input, one scripted-input test in
  the `graphview` `scenetest` pattern: derive the handles from the same id
  stack, `Script*` one frame of input, `Render`, assert on the returned
  `Result` / `Events`. Pure-logic tests remain where they are.

### W20 · Enforcement

A conformance test in `widgets/` walks every widget package with `go/packages`
and asserts the mechanical half of the rules: exported entry verbs (W1), type
names (W2), `Render` returns a struct (W3), `Input` carries `Ids` and
`ScopeKey` or `New` takes them first (W4), no `MakeAbsoluteIdStr` outside a
W6 pattern, no `c.ProbeSeq` call (W7), no exported `On*` registration (W13),
`Close` present where a goroutine is started (W14). It carries an explicit
allowlist of packages not yet migrated; the migration removes entries, and
CI refuses a new package on the list. This is the same device ADR-0013 used
for the bindings (`TestStatefulWidgetsAreGated`).

## Migration

### Principles

- **No big bang.** One widget, one commit, its demo and its importers in the
  same commit. Widgets with more than ten importers keep the old entry point
  as a deprecated wrapper for one phase, then drop it.
- **Order by blast radius and by what the change teaches.** Cheap widgets
  first, so the rules meet real code before the expensive ones move; the two
  bug classes (W7 probes, W6 absolute ids) first of all.
- **Conforming widgets are the reference, not the first to touch.** `tree`,
  `fsbrowser`, `cardgrid`, `chatview` (IM) and `graphview`, `timescrubber`,
  `portolan`, `waveform` (SR) need only the W3 and W7 alignments.
- **Descope, don't gate.** `leewaywidgets` (six views, one app) and `implot`
  (its own lane, ADR-0149) are deferred to a phase of their own; the contract
  does not wait for them.

### Phases

Importer counts are the survey's (2026-09-29), excluding the package's own
tests and the generated `packageprops` table.

**M0 — the two helpers and the guard.** No widget changes. Landed
2026-09-29 under `proposed`.

| Item | What |
|---|---|
| Binding helper for W7 | `WidgetIdStack.ProbeSeq(role)` derives a probe seq from the *current* id stack and a role string, replacing `c.ProbeSeq(scopeKey, role) ^ probeSalt`. `c.ProbeSeq` stays for non-widget callers |
| Recipe for W6 | Documented once in the imzero2 skill: absolute id for an owned window from `ids.PrepareStr("window").Derive()` |
| Conformance test (W20) | `widgets/conformance`: ten syntactic checkers (W1–W7, W11, W13, W14) over every package under `widgets/`, an exemption set for the libraries, and an allowlist frozen with 40 packages on it — green on day one, and every later phase deletes lines. `WIDGET_CONFORMANCE_DUMP=1` regenerates the list after a phase |
| Skill section | `doc/skills/imzero2/SKILL.md` §21 "Widget shapes" summarises the rules; `AGENTS.md`'s subsystem note for egui2/imzero2 points here |

Two things the first run of the guard taught, both folded into the checkers
rather than the rules: a `Show`/`Hide` pair is the house opposite pair and
not a render verb, so `filepicker`'s `Show()` stands (M4's row is amended
below); and fork-join goroutines that their own function waits on need no
`Close`, which is how `graphview` runs its force step.

**M1 — align the references (W3, W7).** Landed 2026-09-29, with the
sibling consumers in the workspace moved in the same change.

| Widget | Shape | Change |
|---|---|---|
| `graphview`, `portolan`, `waveform` | SR | `Render`, `RenderFill` and `HostedPaint` return `Events` (`graphview.Events` is the event slice; `portolan.Events` embeds `ViewEvents` and carries the click); `RenderColumns` and `HostedPaintColumns` return the pair `(Events, error)`; `Events()` kept one phase as a deprecated accessor; pane probes via `WidgetIdStack.ProbeSeq` under the widget's scope |
| `timescrubber`, `timeline`, `spectrumdisplay` | SR | The hand-rolled `probeSalt` and its seed constant replaced by `WidgetIdStack.ProbeSeq`, derived at render time inside the scope |
| `chatview`, `cardgrid`, `schemaview`, `filepicker` | IM / SR | Stack-independent `c.ProbeSeq` replaced by `WidgetIdStack.ProbeSeq` — closes the shared-probe-slot bug for the three that had it (`chatview` had been salted through the stack the day before) |

Three widgets the plan had put here stay on the W7 allowlist for a later
phase, for reasons the code made plain:

- `sqleditor` mints its slots in `Bind`, which runs before `Render` and
  has no id stack; a per-construction counter keeps them unique per editor.
  It moves with the ids in **M4**.
- `gauge` and `distsummary` already fold the caller's derived id into their
  seqs (`readoutMeasureId(idPrefix, callId)`, `c.ProbeSeq(scope, role)` with
  `scope` carrying the call id), so they are per-call today; they take a
  `c.WidgetIdCreatorI` that may be absolute and so have no stack to derive
  from until **M2** gives them `Ids` + `ScopeKey`. The survey's §3.4 row
  calling them stack-independent was wrong and is corrected.

**M2 — small IM widgets (≤ 6 importers each).** Each becomes
`Render(Input) Result`; `Renderer` with-methods become `Input` fields;
`idPrefix` becomes `Ids` + `ScopeKey`. Landed 2026-09-29: nineteen
packages left the allowlist (every row below plus `jobprogress`,
`bgjobrow`, `ecdfdigest`, `selector`), the sibling repo's six callers moved
with them, and every migrated package gained a headless one-frame test.
Four things the phase settled that the rules had not:

- **A painter helper's knobs are a `Style` struct embedded in its `Input`**
  (`boxenplot`, `ecdf`), zero meaning default through a `Resolved()`
  method, so a host paints several distributions with one `Style`. ecdf's
  three render variants collapsed into one `Paint` selected by
  `Input.Band`; its band-job registry is keyed by `ecdf.BandJobKey`, a
  `uint64` the host derives from `Ids.ProbeSeq("band-job")` inside its
  scope, never by a caller string.
- **Boolean options invert to keep the zero value the default:** `ShowX`
  became `HideX`, `DefaultOpen` became `StartCollapsed`; a knob whose zero
  used to mean something (`fieldview.BytesMax`, `errorview.Indent`) takes a
  negative for "none". The former `idPrefix` often doubled as a window
  title, so the summaries and `distsummary` gained `Input.Title`.
- **`regexedit` is an F widget**, not IM: one `TextEdit`, one id, the host's
  own buffer, and a highlight memo re-derivable from the text. It is now
  `Cache.TextEdit(id, text, multiline, mode) c.TextEditFluid`, so the
  binding's own knobs and response stay reachable.
- **The tether infrastructure still takes a string.** `inspector` (not in
  scope) hashes a string into an absolute id; the summaries hand it the
  scope-derived id spelled in hex, which keeps the slots per host scope.
  Changing `inspector` to take an id is housekeeping for a later phase.

Two entries stay on the list from this phase's packages: `ecdf` W14, whose
band-job goroutines live in a process-level registry cancelled through
`CancelBandJob` rather than a `Close` (ADR-0093's design, revisited when
the registry is), and nothing else — `selector` lost `SegmentedAbs`, which
existed only for the I4 hosts that no longer exist. One further sibling
module in the workspace was not migrated (it was already broken by an
unrelated `basemap` change); its callers of `errorview`, `jobprogress` and
`distsummary` are listed in the phase's report.

| Widget | From | Notable change |
|---|---|---|
| `gauge` | S3, `New(idPrefix).Render(idGen, value)` | `Input{Ids, ScopeKey, Value, Min, Max, Zones, …}` |
| `jobprogress`, `bgjobrow` | S1, returns `bool` | Landed 2026-09-29, first in the phase because five other M2 widgets draw the row: `Input` gains `Ids`, `ScopeKey` and `Cancel bool` in place of `CancelId`, the button's id is derived under the row's scope, `Render` returns `Result{CancelClicked}`; `bgjobrow` takes `Job` on `Input` and returns `Result{Running, Cancelled}`. A display-only row may leave `Ids` nil and opens no scope. Thirteen call sites moved, five of them in the sibling repo; the id-stack caveat jackstay carried ("a prepared id left unconsumed breaks the stack") is gone with the prepared id |
| `metricsoverlay`, `runtimestatus` | `RenderInline(…)` | `Render(Input)`; click handler becomes `Result.Clicked` |
| `videooutput` | `ShowGallery/ShowStatus/ShowDialog(ids, st)` | `RenderGallery/RenderStatus/RenderDialog(Input)` |
| `trendsmooth` | `State.RenderControls(ids)` | `RenderControls(Input{Ids, ScopeKey, State})` |
| `errorview`, `fieldview`, `cbordiag` | S3 with `ids` at `New`, `*State` at `Render` | Fold `Renderer` config into `Input`; `State` stays host-owned |
| `regexsummary`, `canonicaltypesummary` | S3 + absolute ids from `idPrefix` (I4) | `Input{Ids, ScopeKey, …, State *State}` with the pinned/open flag in `State`; tether window id per W6. Removes the "distinct idPrefixes" caveat from both package docs |
| `distsummary` | S3 + global `instanceStates` (I4) | As above; the `sync.Map` is deleted, the pinned state moves to `State`. The one behaviour change a host sees: pinned state no longer survives the host forgetting its `State` |
| `boxenplot`, `ecdf` | S3 over `*implot.Plot` | Painter helpers: `Paint(p *implot.Plot, in Input)`; no ids |
| `canonicaltypeedit` | S6, `Model.Render(ids, scopeKey)` | `Render(Input{Ids, ScopeKey, Model, State})`; `barBuf`, `barErr`, `formOpen` move from `Model` to `State`; the `0xC7ED17` seed becomes `PrepareStr("summary")` |
| `componentview` | `Dispatcher.RenderReport(ids, comps)`; `RendererI.Render(ids, value any)` | Both take an `Input`; the plugin interface keeps its name |
| `regexedit` | `Edit.Prepare(id, text, …)` | Verify against the F shape; likely `Fluid` over `TextEdit` |

**M3 — IM widgets whose model carries state (W8, W9).** Landed 2026-09-29;
both packages left the allowlist, and the sibling repo's two callers moved.

| Widget | Change |
|---|---|
| `kanban` | `sel`, `drag`, `dragStop` moved to a host-owned `State` (with `Selected` / `SetSelected`); the move queue and `DrainMoves` became `Result.Moves`, the frame's moves, with the mutators returning the `Move` they made; `Result.Clicked` reports the card clicked; `DotLegend` stays on `Model`. The drag hit-test's rect probes were two process-wide seq bases (`0xCA0B…` plus an index, "a single active drag is assumed"); they are now derived per card and per lane under each one's own scope and kept in `State`, so two boards can drag at once. Column, card, header and swimlane ids went from `"col:" + id` strings to per-item scopes |
| `schemaview` | `Model`, `NewModel` and `SetTable` are gone: the schema arrives as `Input.Table` every frame, and `sel`, `filter`, `legendOpen`, the type-summary state and the tree state live in a host-owned `State`, which resets its selection when the `Table` pointer changes (what `SetTable` did) and keeps the filter. Internally a per-frame `view{Table, *State}` keeps the render and navigation code unchanged. The legend toggle is relative, the legend window is the one W6 absolute id, the tether string is the scope-derived id in hex, and the badge rows use per-key scopes with ordinal ids |
| `markdown` | See M5 — same rule, widest footprint |

One host simplification fell out: the sibling's board app no longer saves
and restores the selection around a rescan, because the selection now lives
on its app state rather than on the model the rescan rebuilds.

**M4 — SR normalisation.** Nine of the ten packages landed 2026-09-29 in
two parallel groups plus the file picker; `sqleditor` followed on its own,
because it shares a play file with pager and fsmview.

| Widget | Change |
|---|---|
| `pager` | `New(ids, scopeKey, Options{PageSize, PageSizeOptions, Unit, HideSizeCombo})`, `Render() Events{Changed}`; the `0x1000+i` seeds became scopes with ordinal ids, and `Render` no longer resets the caller's stack |
| `worldmap` | `Widget` → `Map`; `Options{Projection, RasterWidth, Style}` with a nested `Style` whose zero value is the old look; `Render(w, h) Events{Clicked, Hovered, …}` + `RenderFill`; the legend draws under the host's ids instead of a fresh stack. In a bounded pane the map now fits the leaf where it used to overflow to a fixed height |
| `heatmapscroll`, `spectrumdisplay` | `Options` at `New`, public `Opts`, readback in `Events`; the self-probe and the `dispW/dispH` knobs became the W12(a) `Render(w, h)` + `RenderFill` pair; spectrumdisplay's axes, markers and regions are public data fields (the timescrubber precedent), its eleven `Set*` gone; `PushColumn` stays as the W8 expensive-data exception |
| `colorscale` | Six `With*` → `Options` (with `TickerAuto` as the zero ticker); `OnHover(fn)` → `Render().Hover`, fed to the hover band at the call site |
| `taskmonitor` | `Inst` → `Monitor`; `New(ids, scopeKey, api, Options)`; `Render() Events{CancelRequested}`; `Close()` replaces `Stop()` and is documented as required; rows scoped by task id so they survive reordering |
| `filepicker` | `Inst` → `Dialog`; `New(ids, scopeKey, Options)` with the fourteen `With*` as fields; `Render() Events{Action, Paths}`; `Show()` / `Hide()` stay (a house opposite pair); the window is the one W6 absolute id. Filter precedence (`Filter` over `Globs` over `Extensions`) replaces last-option-wins. The window host had no id stack at construction and now builds the dialog on its first frame |
| `fsmview` | `Widget[T]` → `View[T]` with `Options[T]` in place of the chained setters; `Machine`'s `Option[T]` → `MachineOptions[T]`; all three render entries return `Events{Toggled, Driven}`; the `idBase` arithmetic and `Sprintf` keys became per-row scopes; popup and tether ids scope-derived |
| `mappingplanview` | Became SR: `New(ids, scopeKey, Options{Recompute, FillHost}) *View`, `Render(model) Events{Edited, Recomputed}`; the pager, the FSM chips and the error buffer moved from `Model` and `FieldRow` into the `View`, pruned per frame for vanished rows |
| `sqleditor` | `Editor` and `Field` take ids at `New` and return `Result` from `Render(Decoration)`; the `Result()` accessor and the caller-named `IDSlot` are gone, and `Frame.View` names an optional child scope for an editor that alternates between two buffers. `Bind` stays as the pre-render step: several play consumers read the caret it resolves before the editor's turn in the frame. The completion pane is IM (`RenderPane(PaneInput) PaneResult` with a host-owned `PaneState`; the accept callback became result fields). The per-construction `probeSalt` counter is gone; slots are `ids.ProbeSeq` under the editor's scope. The zero-value `Editor` still binds but draws nothing without ids |

Two more behaviours changed for hosts, both in the direction the rules
point: options that used to be read once at construction are re-read every
frame (a toggle is an assignment), and every option whose zero value used to
mean something got the inverted or `Auto` spelling so a zero `Options`
reproduces the old default. Two hosts had no id stack at construction (the
window host, the demo registry's `Init`) and now build their widget on the
first frame; `Bind`-style pre-render steps that need a slot open the widget's
scope for the derivation alone.

**M5 — the large surfaces.** Landed 2026-09-30, together with the
single-rule leftovers of M1, and the allowlist is empty. The deprecated
wrappers the plan called for were not needed: every importer is in-tree or in
a workspace sibling, so each surface moved with its callers in one change.

| Widget | Change |
|---|---|
| `treemap` | Thirteen `With*`, their `Set*` twins and `OnNavigate` → `Options` + `Opts` and `Events{Nav, ClickedLeaf, Hovered}`; `Render(w, h)` + `RenderFill` replace the container-size pair; `SetRoot` and the navigation methods stay (W8). Navigation, including the host's own `NavigateTo`, is reported in the next frame's `Events` rather than synchronously. Cell ids are derived under the treemap's scope and then wrapped as absolute: a cell `Frame` pushes its creator, so a relative one would renumber nested cells under their parent. The label measure ids moved from a stack-free hash to `ids.ProbeSeq` |
| `timeline` | Seventeen `With*`, `WithBrush` and both listener types → `Options` + `Opts` and `Events{SelectionChanged, Selection, BrushChanged, Brush}`; points, annotations and a range are data set after `New`. It keeps its self-probing `Render()`: its height is content-driven and its width fits the pane, so the canvas pair does not apply |
| `markdown` | The parsed `Doc` stays the model; `ParseWith(md, ParseOptions)` replaces the parse-time `With*`; rendering is IM, `Render(Input) Result{Actions, Links}`, with the render-time options as `Input` fields and the link router's click returned rather than called back. Code blocks are keyed at parse time on a hash of language and text plus their occurrence among identical blocks, so an insertion above no longer moves them (I8). Nothing else survives a frame, so there is no `State` |
| `leewaywidgets` | The five constructors take a scope key and draw under it; the card emitter opens its scope around the one flush every drawing path goes through, and its seeded section-header ids became composite ordinals |
| M1 leftovers | `portolan.New` and `timescrubber.New` take the scope key as their second argument, so two maps under one host need only distinct keys; portolan's concatenated tile ids are scoped; the land and flow overlays' `Draw` became `Paint`; cardgrid's toolbar is a child scope |

Two checker refinements came out of the phase, both narrowing a rule to
what it was written for: a render entry may return `(Events, error)` when it
validates a declaration first (graphview's columnar form), and W14 binds to
goroutines started in a rendering type's methods, since work a free function
starts under a caller's context or task handle has no widget to own it (the
ecdf band job, the waveform peaks task).

**M6 — close.** Delete the deprecated `Events()` accessors M1 kept, move
this ADR's rules into the skill as the single authoring reference (ADR-0059
§SD5), and add the `## Updates` entry.

### Deferred

- `implot` keeps its ADR-0149 contract; whether `Plot` is an SR widget in
  this sense is a question for that ADR's successor.
- Whether the 17 non-widget packages in `widgets/` (§6 of the survey) move
  to a sibling directory is housekeeping this ADR notes and does not decide.
- A `RadioGroup[T]` helper (ADR-0013 deferred it) is what `selector` already
  is; no further F widget is planned here.

## Alternatives

- **One shape for everything — `Render(Input) Result` with `State` holding
  whatever survives.** Rejected: a tile pyramid, a peaks build or a
  force-layout worker in a host-owned `State` is an object the host must
  construct and close without a constructor or a `Close` to do it in. The
  survey's two attractors exist because the two kinds of widget have
  different lifetimes, and the contract should say so rather than hide it.
- **One shape for everything — `New` / `Render` objects.** Rejected: every
  tree, list and inspector becomes something to allocate, key and free, and
  host-owned state — the property ADR-0176 and ADR-0239 chose deliberately
  so a host can persist it and drive it from another pane — is lost.
- **Functional options as the option mechanism (the Go idiom).** Rejected
  for widgets: a per-frame toggle then needs a `Set*` twin (the `treemap`
  duplication), the zero value cannot be the default, and an LLM or a
  reviewer cannot read the option set off the call site. Kept for
  non-widget constructors (parsers, loaders).
- **Keep `Events()` as an accessor rather than returning it.** Rejected in
  favour of symmetry with IM's `Result` and one rule to remember; a host
  that renders inside a tab body and reacts outside it stores the returned
  value, which is what it stores today.
- **Document the shapes without a conformance test.** Rejected: ADR-0013's
  experience is that an implicit contract drifts within weeks; the
  allowlist device makes the migration's progress visible and the end state
  enforceable.
- **Leave the older shapes in place and only bind new widgets.** Rejected
  because two of the older idioms are latent bugs (W6, W7), and because a
  host author meets the old widgets first — they are the small, general
  ones.

## Consequences

### Positive

- One question — *does anything have to survive the frame that cannot be
  re-derived?* — picks the shape, and each shape has one signature a host
  can write from memory.
- The shared-probe-slot collision and the absolute-id-from-prefix collision
  become impossible by construction; the "must use distinct idPrefixes"
  caveats disappear from four package docs.
- A widget is testable in one call (IM) or one `New` (SR), which is the
  precondition for the scripted-input tests the survey finds in four
  packages becoming the norm.
- Host code loses a category of bug: interaction can no longer arrive
  through a callback the host did not see at the call site.

### Negative

- Every widget older than August 2026 changes signature; twenty-odd
  commits, each touching its demo and importers. The phases bound the blast
  radius but not the total.
- Two behaviours change for hosts: `distsummary`'s pinned state no longer
  outlives a host that drops its `State`, and `markdown` code-block ids
  become key-based, so egui memory for a block is keyed differently once.
- The `Opts`-as-public-field convention means a host can put an SR widget
  into an invalid configuration between frames; widgets validate on `Render`
  and draw an error (W17) rather than panic.

### Neutral

- The bindings' F contract (ADR-0013) is unchanged; `badge` and `selector`
  are already conforming.
- `Set*` survives only in the narrow W8 sense, which matches the house
  `Set` rule (idempotent setters) in CODINGSTANDARDS.

## Verification plan — Tier 1

- The W20 conformance test is green with the full allowlist at M0 and with
  an empty allowlist at M6; each phase's commits shorten it.
- Every migrated widget's demo re-registers under the ADR-0057 registry and
  the screenshot tour is compared before and after the phase; a layout
  difference is a defect unless the phase names it.
- The two bug classes are demonstrated closed: a test renders two instances
  of the same widget under one host with the same `ScopeKey` in different
  parent scopes and asserts distinct probe slots (W7), and two inspectors
  for two values under one host and asserts distinct toggle ids (W6).
- `apps/play`, the app importing most widgets, runs its existing scenes
  after each phase.

## Status

Proposed — 2026-09-29. Becomes accepted when reviewed; M0 may land under
`proposed` since it adds helpers and a green test and changes no widget.

Status lifecycle: `Proposed → Accepted → (Deprecated | Superseded by ADR-XXXX)`.
ADRs are append-only; supersession is recorded, not deleted.

## Updates

_None yet._

## References

- Survey: [imzero2-go-widget-api-styles-survey](../adr-background-work/imzero2-go-widget-api-styles-survey.md)
- Bindings contract: [ADR-0013](0013-imzero2-stateful-widget-contract.md)
- IM precedents: [ADR-0176 — native tree widget](0176-native-tree-widget.md) §SD2, §SD11, §SD12; [ADR-0239 — chatview](0239-play-chat-panel-and-chatview-widget.md) §SD3
- SR precedents: [ADR-0224 — graphview](0224-graphview-go-graph-widget-painter-lane.md); [ADR-0228 — hosted canvas rendering](0228-hosted-canvas-rendering-and-graph-widget-sharing.md)
- Demo registry: [ADR-0057](0057-demo-registry-and-drivers.md)
- Threading: [ADR-0261 — one render goroutine](0261-one-render-goroutine-for-every-app.md)
- Skipping and lost sends: [ADR-0012](0012-imzero2-collapsible-retained-bodies.md); the imzero2 skill §12, §18
- Not force-porting imperative call sites: [ADR-0059](0059-imzero2-declarative-layouting-over-visual-builder.md) §SD12 — this ADR ports API shapes, not layout code
