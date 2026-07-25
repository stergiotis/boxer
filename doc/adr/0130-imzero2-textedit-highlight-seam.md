---
type: adr
status: accepted
date: 2026-07-18
reviewed-by: "@spx"
reviewed-date: 2026-07-18
---

# ADR-0130: TextEdit highlight seam — syntax-colored SQL editing via CodeViewJob

## Context

The play app's SQL editor is a plain monospace `TextEdit`
(`c.TextEdit(...).CodeEditor()`); syntax color exists only in read-only views
through the `codeView` opcode, which ships text plus byte-range color sections
(a `CodeViewJob`) over the FFI and renders a cached egui `LayoutJob`
([ADR-0125](./0125-codeview-prepare-memo.md)). The survey
[sql-editor-highlighting-survey](../explanation/sql-editor-highlighting-survey.md)
compared the ways to get a highlighted *editing* experience (ClickHouse
`play.html`, the egui ecosystem, embedding micro, embedding neovim, plus a
second pass) and concluded the gap is one FFI seam: egui's
`TextEdit::layouter` lets a caller lay out the live buffer each frame, but the
egui2 IDL does not expose it, and nothing connects the repo's exact-dialect
lexer (`grammar1`) to an editable widget.

Constraints that shape the seam:

- **The FFI split makes spans stale by one frame.** Go computes sections from
  the buffer it received at the previous frame's `SendRespVal`; the live
  buffer inside egui may already contain this frame's keystrokes. The seam
  must tolerate mismatched sections gracefully — in an editor, a `LayoutJob`
  that does not cover every byte drops glyphs, which is unacceptable.
- **Cost discipline** (measured 2026-07-18, linux/amd64, Ryzen AI MAX+ 395,
  `-benchtime=2s`):

  | input | lex-only phase | full `Highlight` (lex+parse+CST refine) |
  | --- | --- | --- |
  | 180 B CTE | 26 µs, 20 KB, 219 allocs | 5.7 ms, 3.4 MB, 46 k allocs |
  | 2.5 KB query | 279 µs, 187 KB, 1.8 k allocs | 70 ms, 41 MB, 545 k allocs |

  The lex-only phase is comfortably per-keystroke (≤ 2 % of a 60 fps frame
  budget at editor-typical sizes; roughly linear at ~9 MB/s). The full parse
  is not — and at 70 ms / 2.5 KB it is not even per-quiescence on the render
  goroutine. (ADR-0125 recorded the steady-state parse cost as an open
  problem; these numbers confirm it worsens super-linearly with input size.)
- **Precedent.** The `insertAtCursor` method (ADR-0063) already demonstrates
  the required mechanics: a builder method stashes pending state on the
  interpreter, and TextEdit's custom apply block consumes it around
  `apply_widget`. Construction, the method loop, and apply share one
  interpreter match-arm scope, so a stack-local layouter closure can be
  attached between taking the widget and adding it — the `&mut FnMut`
  lifetime works without storing a closure.

The shape below was settled in a design dialogue on 2026-07-18.

## Design space (QOC)

**Question.** How does an editable egui2 TextEdit obtain syntax-colored
rendering for ClickHouse SQL?

**Options.**

- **O1** — `highlightJob` method on the existing `textEdit` opcode, consuming
  the existing `CodeViewJob` evaluated arg; spans produced Go-side by the
  lex-only phase of `nanopass/highlight`.
- **O2** — a dedicated `codeEdit` opcode/widget wrapping its own editor state
  and highlighting.
- **O3** — Rust-side ClickHouse lexer (vendored or ported
  `src/Parsers/Lexer`) running inside the layouter; no per-keystroke span
  traffic.
- *(Embedding an editor — micro, neovim, kakoune — was killed in the survey
  §4–§6 and is not re-argued here.)*

**Criteria.**

- **C1** — dialect fidelity with a single source of truth (`grammar1`).
- **C2** — integration cost / added IDL and Rust surface.
- **C3** — editor-state continuity (caret, undo, focus, `insertAtCursor`,
  `SendRespVal` unchanged).
- **C4** — typing latency / staleness of colors.

**Assessment.** `++` strong positive, `+` positive, `−` negative, `−−` strong negative.

|    | O1 | O2 | O3 |
|----|----|----|----|
| C1 | ++ | ++ | −− |
| C2 | ++ | −  | −  |
| C3 | ++ | −− | ++ |
| C4 | +  | +  | ++ |

O3's only win is C4, and the reconcile step below recovers most of it for O1.
O3 was **rejected outright** in the design dialogue: a second in-tree dialect
definition outside `grammar1` (C++ vendoring or a hand port) is not an option
for this repository.

## Decision

We will add a `highlightJob` builder method to the existing `textEdit` opcode.
It consumes a `CodeViewJob` (the evaluated arg `codeView` already uses) and
installs, at apply time, an egui `TextEdit::layouter` that renders the live
buffer with the job's sections applied *advisorily*. Contract:

1. **Text stays authoritative in the TextEdit.** `SendRespVal`,
   `insertAtCursor`, undo, caret, focus and hint behavior are unchanged.
   Color is presentation only; an absent or empty job renders exactly as
   today. The feature is strictly additive and per-widget opt-in.
2. **Reconcile.** If the live buffer equals `job.text`, sections apply
   directly. Otherwise the helper computes the single edit region by
   common-prefix/common-suffix diff (O(n) on KB-sized buffers) and shifts
   section boundaries past the edit by the length delta, merging sections
   that overlap the edit region. This makes the one-frame staleness invisible
   during continuous typing: existing tokens keep their colors; just-typed
   characters inherit the surrounding span until corrected sections arrive
   next frame.
3. **Normalize (fail-safe).** Clamp sections to the buffer length, drop
   inverted ranges, enforce ascending order, and gap-fill uncovered byte
   ranges with the default monospace format. A malformed job degrades to
   plain text, never to missing text.
4. **Wrap preserved.** The layouter sets `wrap.max_width` from its
   `wrap_width` parameter, keeping TextEdit's existing wrap semantics
   (codeview's no-wrap job builder is not reused). Galley layout cost is
   absorbed by egui's persistent `Fonts` galley cache; no new Rust-side cache.
5. **Span source: the lex-only phase of `nanopass/highlight`,** exported from
   today's unexported `lexHighlight`, plus a one-token peek-ahead for `(` to
   classify function names (play.html's trick; a token-stream pass, no
   parse). `grammar1` remains the single dialect definition.
6. **Cost discipline.** Go relexes only when the bound text changed since the
   last frame; unchanged frames re-use the previous job (`.Keep()`). The
   editor path uses the **uncached `Build` tier** — per-keystroke content is
   new by construction, so the ADR-0125 `Prepare` memo would only churn its
   LRU. The full parse (`nanopass.Parse` + CST refine) never runs on the
   keystroke path.

Implementation mechanics (all expressed in the IDL definition +
`egui2gen`-regenerated dispatch; no hand edits to generated code):

- Method snippet stashes the job:
  `self.text_edit_pending_highlight = Some(std::mem::take(&mut self.r12_code_view_job));`
  — the same register take `codeView` performs, the same pending-on-self
  pattern as `insertAtCursor`.
- TextEdit's custom apply (ADR-0063 `MergeVerbatimCode` idiom) takes the
  pending job, declares the layouter closure as a stack local, attaches it
  via `w.layouter(&mut cl)`, then proceeds with the existing
  `apply_widget` / changed-push / pending-insert logic.
- Reconcile/normalize/job-build live in a new hand-written helper module
  (`text_edit_highlight.rs`, sibling of `code_view.rs`) with unit tests for
  the edit-region diff and the normalization invariants.
- Go-side glue: export the lex phase (`highlight` package), add a small
  builder that maps lex categories to theme colors (shared with codeview's
  SQL front-end), and switch play's `sqlTextEditField` over.

## Alternatives

- **O3 — Rust-side ClickHouse lexer in the layouter.** Rejected in the design
  dialogue (2026-07-18): it duplicates the dialect definition outside
  `grammar1` (vendored C++ or a port to keep in sync), for a latency
  advantage the reconcile step already neutralizes. Not deferred — rejected.
- **O2 — dedicated `codeEdit` opcode.** Duplicates TextEdit's retained editor
  state (caret, undo, focus, snippet insertion) behind a second identity and
  adds IDL surface for no fidelity or UX gain. A future line-number gutter
  (L3) can wrap a TextEdit in a container without moving this seam.
- **Clamp-only guard (no reconcile).** Saves ~40 lines of diff/shift logic
  but leaves every span after the caret misaligned by the insertion length
  for one frame — a visible color shimmer on the buffer tail during fast
  typing.
- **Extending `Section` with style flags now** (underline for error
  squiggles). Touches a wire struct shared with four existing codeview
  producers; deferred to L3 as a parallel `sectionStyled` method so existing
  producers do not move.
- **Overlay imitation of play.html, embedded editors (micro / neovim /
  kakoune), syntect grammars, webview editors, LSP.** Killed in the survey
  ([§2, §4–§6](../explanation/sql-editor-highlighting-survey.md)); the
  kill-reasons are recorded there and not repeated.

## Consequences

### Positive

- Exact-dialect highlighted SQL editing with zero new dependencies, one new
  IDL method, and one hand-written Rust helper; the read path (`codeView`)
  and its producers are untouched.
- The seam is span-source-agnostic and lands where L2 (quiescent semantic
  refine) and L3 (squiggles via `sectionStyled`, statement-under-cursor)
  attach without further IDL changes to the base contract.
- Other SQL-bearing TextEdits (e.g. the from/to ClickHouse-expression fields
  in the widgets IDL) can adopt the method for free.
- The fail-safe normalization means highlighting bugs degrade to plain text —
  the editor never loses content to a bad job.

### Negative

- Sections stream over the FFI on every frame in which the text changed
  (few KB per keystroke frame at editor-typical sizes). Idle frames pay
  nothing (`.Keep()`), but a future very-large-buffer use would need the
  per-line incremental approach noted in the survey.
- One-frame color staleness is inherent to the FFI split; reconcile hides it
  but cannot eliminate it (a keystroke that *changes the lexical class* of
  earlier text — e.g. typing the closing `*/` of a comment — corrects one
  frame later).
- The reconcile/normalize helper is subtle enough to need real unit tests
  (edit-region diff, UTF-8 boundary handling, pathological section lists);
  this package becomes load-bearing for editor rendering.
- L1 color depth is lexical only: keywords, literals, comments, operators,
  quoted identifiers, param slots, and peek-ahead function names — semantic
  colors (table/column/alias/CTE) arrive only with L2.

### Neutral

- The editor path deliberately bypasses the ADR-0125 memo; the memo remains
  correct for read-only and quiescent paths.
- The measured full-`Highlight` cost (70 ms at 2.5 KB) hardens a constraint
  on **L2**: the quiescent semantic pass must run off the render goroutine
  (bgjob) or wait for ADR-0125's open steady-state problem to be fixed —
  synchronous-on-quiescence would drop frames. Recorded here so L2's design
  starts from it.
- The lex-only exported API creates the natural place to later fix the open
  ADR-0125 item (the 46 k-alloc parse) without touching the editor seam.

## Status

Accepted 2026-07-18.

## Updates

### 2026-07-18 — L2 (async semantic refine) implemented and live-verified

The quiescent semantic tier shipped on top of the seam, honoring the
constraint recorded below (the full parse never touches the render thread):
a `bgjob.Runner` worker computes `highlight.Highlight` — pure Go, no `c.*`
calls — and the render thread pays only span→`CodeViewJob` serialization
(`codeview.BuildSqlFromSpans`, the split-out tail of the existing builder).
Launch requires the buffer unchanged for 400 ms and no installed job for the
current content; supersession is by content (the run's `Tag` carries the
parsed text, and a drained result installs only while the buffer still
equals it — an edit falls back to the lex tier the same frame and the stale
result is dropped on arrival, freeing the slot for a relaunch). An
unparseable buffer installs its lex-equivalent spans as a visual no-op,
which also stops relaunch churn for that content. State machine
race-clean under `-race`; three unit tests cover quiescence, supersession,
and typing-never-launches. Live-verified: a CTE buffer upgrades at rest
(CTE/table/column names in the semantic palette, matching the read-only
preview), and text typed after a pause re-upgrades ~450 ms later including
the fresh identifiers.

### 2026-07-18 — implemented and live-verified

Shipped as designed, same day as acceptance: `highlightJob` IDL method +
regenerated dispatch, `text_edit_highlight.rs` (reconcile/normalize, 9 unit
tests), `highlight.HighlightLex` export with the `(`-peek-ahead (applied to
`Highlight`'s parse-failure fallback too, so both lex-tier paths color
identically), `codeview.BuildSqlLex`, and the play `sqlTextEditField` wiring
with a text-change-keyed job cache. Live-verified under a headless compositor
via the inspection tooling: colors at rest match the read-only preview,
typing re-colors within a frame (keyword/function/number/string on freshly
typed text), no glyph loss, `SendRespVal` and downstream staleness signals
unaffected — and an *unparseable* mid-edit buffer keeps its lexical colors
while the full-parse preview correctly reports "no canonical form", i.e. the
lex-tier independence this ADR argued for.

Two findings from the bring-up, both outside the seam:

- The editor's accessibility surface is unchanged (`MultilineTextInput`
  value + text runs still exposed), confirming the survey's
  "native accessibility" claim for this route.
- Unmasking collateral: fixing the crate's test-target compile (pre-existing
  `Context::run` → `run_ui` rename from the egui 0.35 bump, in
  `svgexport.rs` tests and two examples) revealed one genuinely failing
  svgexport test (`render_svg_window_content_only_shrinks_viewbox_and_strips_bg`):
  egui 0.35's `run_ui` wraps the pass in an implicit full-screen `Ui` whose
  background the content-only exporter does not strip. Pre-existing 0.35
  migration debt in svgexport, not a seam regression; left failing rather
  than papered over.

### 2026-07-25 — L3 (editor affordances) design settled; implementation pending

L3's scope is the survey §8 list — error-token underlines,
statement-under-cursor execution for multi-statement buffers, and the
line-number gutter — plus the `sectionStyled` channel this ADR's
§Alternatives deferred, and ADR-0124 §SD8's "`Src` consumers" bullet, which
lands here. Settled in a design pass on 2026-07-25; nothing below is built
yet.

- **`sectionStyled` is a parallel channel, not a `Section` change.** A new
  evaluated arg (its own struct and register) plus a parallel textEdit
  method, so the color-only `Section` shared with the existing codeview
  producers does not move — the reason recorded in §Alternatives stands.
  The style vocabulary is exactly what egui's `TextFormat` expresses
  natively: underline, background, strikethrough, italics — flag bits plus
  one style color per section. Styled sections ride the same reconcile
  shift as color sections; their normalization clamps, drops inverted
  ranges, and sorts, but does **not** gap-fill — they are sparse overlays
  over the color tier, and an uncovered byte simply has no styling. A wavy
  "squiggle" is not `TextFormat`-expressible and is deferred as a paint-over
  pass on galley rows; trigger: the straight underline proving insufficient
  in use.
- **Caret report is an opt-in `reportCursor` method.** The apply block
  already loads `TextEditState` for `insertAtCursor`; the method reuses
  that read after apply and pushes the sorted cursor char range packed into
  one u64 (low half start, high half end) — the datePickerButton packed-u64
  value path is the precedent. The push is unconditional on every frame the
  method is present: change detection around end-of-frame value application
  is the known trap, and one u64 per frame is noise. Go converts char to
  byte offsets against its own frame copy of the buffer, clamped; consumers
  gate on the quiescent buffer (text equal to the last parsed text). This
  is deliberately the same seam a later tethered-affordance phase needs, so
  it is designed once here.
- **Statements split at the lex tier, not the CST.** The splitter walks
  `highlight.HighlightLex` tokens for top-level `;` — in spirit a port of
  play.html's `getQueryUnderCursor`. Rationale: it must keep working when a
  *sibling* statement is broken — precisely the situation in which running
  one statement of a multi-statement buffer is most useful — and the lexer
  survives what the parser does not (the same independence argument L1
  made).
- **Run-under-cursor ships conservative semantics.** When the
  prelude-stripped body holds more than one statement, Run ships the
  statement under the caret, with the whole `SET` prelude riding along
  unchanged. Everything else deliberately stays buffer-wide in the first
  cut: param slots and signals keep their existing scope, history snapshots
  the full buffer, and a caret move does not flip the staleness witness —
  what would run is legible from the tint and the wire preview instead.
  Finer per-statement scoping is deferred; trigger: the first real
  multi-statement friction with params or signals.
- **The active statement is tinted, multi-statement only.** A background
  styled section over the caret's statement, rendered only when the body
  holds more than one statement — the common single-statement buffer stays
  visually unchanged.
- **The error underline producer** maps the debounced parse's syntax-error
  line/column to the lex token at that position and underlines its span in
  the error tone; the L1 lexical colors stay up underneath (play.html's
  `q-err`, with a real token extent).
- **The gutter is wanted, and lives Go-side.** A container row — gutter
  column beside the TextEdit — under a shared scroll scope, with a marks
  lane (error line, active statement) beside the numbers. Its alignment
  contract is no-wrap: galley rows must equal logical lines, so the
  layouter this seam owns gains a no-wrap switch and the editor gains
  horizontal scrolling. Monospace plus no-wrap makes uniform row height
  hold by construction.
- **ADR-0124 §SD8's `Src` consumers land as styled sections.** The pane's
  `unfilledInputs` set — the same set the Run gate reads, so the two cannot
  disagree — drives a warning-toned underline on each unfilled
  placeholder's span. §SD8's bullet retires via a dated Update on ADR-0124
  when this ships.

Two hygiene preconditions surfaced by the same design pass, to land first:
the editor's observation pipeline currently parses the whitespace-trimmed
buffer while its consumers slice the untrimmed one, so recorded byte ranges
are skewed by any leading whitespace — the pipeline moves to the untrimmed
buffer before new span consumers land. And per-call-site editor state, when
it appears, must not key on raw byte ranges (they shift with every edit
above the site); name plus same-name ordinal is the stable identity.

Explicitly out of scope, unchanged by this entry: tethered per-literal
editor windows (a later phase that builds on the first two items), the
svgexport 0.35 content-only test debt, and ADR-0125's steady-state parse
cost.

### 2026-07-25 — L3 implemented and live-verified

Everything the design entry above settled is built, in that order. What landed
and where it deviates:

- **`sectionStyled`** shipped as designed: its own evaluated-arg node
  (`styledSections`), its own register (`r24_styled_sections`), and a parallel
  textEdit method, so `Section` did not move. Style flags are underline,
  background, strikethrough, italics; `text_edit_highlight.rs` runs the styled
  list through the same edit-region shift as the colour sections and normalizes
  it by clamping, dropping inverted/empty/flagless ranges and sorting — no
  gap-fill, and no overlap merge either, because overlapping overlays are a
  legitimate composition (an error underline inside a tinted statement). Nine
  new Rust unit tests beside the existing nine. `codeview.BuildStyledSections`
  is the shared Go-side builder.
- **The error underline** producer differs from the design in one respect that
  the design pass did not anticipate: grammar1's `QueryStmt` is
  *single-statement*, so a multi-statement buffer never parses whole and its
  reported error position is the second statement's first token — a position
  that says nothing about either statement. So the producer takes the
  whole-buffer verdict only for single-statement buffers, and for
  multi-statement ones parses the caret's statement alone (memoised on that
  statement's text, so caret travel inside one statement costs no parse). A
  broken sibling then stays the sibling's problem, which is also what makes
  running the healthy statement beside it legible.
- **`reportCursor`** shipped as designed — unconditional packed-u64 push per
  frame. The Go side needed no generator change: `r9_s` and `r9_u64`
  databindings live in separate id-keyed maps, so one widget carries text and
  caret independently; `TextEditFluid.SendRespValCursor` registers both after a
  single `Send`, beside the hand-written `SendRespVal` it mirrors.
- **The statement splitter** walks `HighlightLex` for top-level `;` as
  designed, over the *prelude-stripped* body — counting the `SET param_*` lines
  the parameter widgets author would make every parameterised buffer look
  multi-statement. The caret's statement resolves by play.html's own rule, read
  as served from ClickHouse 26.6.1 and mirrored rather than approximated: the
  winner is the first statement whose terminating `;` ends at or after the
  caret, with a fallback to the last statement when only trivia follows. That
  is slightly different from "the caret between statements resolves to the
  previous one" — a caret exactly one byte past a `;` belongs to the statement
  it closes, but anywhere further into the gap already belongs to the next.
- **Run-under-cursor** ships prelude + active statement, and everything else
  stayed buffer-wide as recorded. The history entry gained a `Buffer` field so
  restoring a run of a multi-statement buffer brings its siblings back instead
  of the fragment that ran; it is empty whenever the run WAS the buffer, so
  single-statement behaviour is byte-identical end to end.
- **The gutter** is one monospace `CodeView`, not a column of labels: N labels
  accumulate item spacing and drift out of step with the editor's rows within a
  screenful. It carries the mark lane and the right-aligned numbers in one
  text, with per-line colour sections — and every byte has to be claimed by
  one, because a `CodeViewJob` does not gap-fill and egui drops the glyphs of
  unclaimed bytes (the numbers were invisible until this was fixed).
- **Horizontal scrolling** deviates from "under a shared scroll scope": the two
  columns share the tab's *vertical* scope, but the editor owns the
  *horizontal* one, because a gutter that slides out of view on the first long
  line is not a gutter. Live-verified pinned while scrolled to the end of a
  150-character line. The editor's desired width must be finite and content-
  sized: egui caps a TextEdit's allocation at its desired width, so `+Inf`
  under no-wrap clips the tail of a long line out of reach.

Two findings worth recording:

- The host may leave `mono_font_ttf` unset, in which case
  `TextStyle::Monospace` resolves to the proportional main font — per-glyph
  advances then range ~2.4–8.3 px at BodyPt. Row alignment is unaffected (row
  height is uniform per font size, which is all the gutter's contract needs);
  only the editor's width estimate is approximate, and it is deliberately an
  over-estimate.
- The statement tint reaches the galley as one background rect per glyph rather
  than one per run, which the SVG export makes visible as faint seams under
  magnification. Cosmetic, below this seam, and not pursued.

Live-verified under the inspection tooling on a four-statement buffer: the
error underline appears on the offending token with the lexical colours intact
and clears on the fix; clicking into a statement moves the tint and the gutter
marks to exactly its lines; the wire preview reads "statement 2 of 4" and shows
that statement's body; Run executes it and returns its rows, with a broken
sibling above it and a healthy one below.

### 2026-07-25 — the tethered per-literal editor phase is deferred, not started

Assessed immediately after L3 shipped, and declined in that form. Two
findings, the second the deciding one.

**The 2026-07-25 entry's claim that L3's first two items are this phase's
foundations is half right.** `sectionStyled` and `reportCursor` give
*identity* — which literal the caret is in, and a way to mark it. They do not
give *position*. Tethering a window to a literal needs the screen rect of a
byte range inside the editor's galley, and nothing exposes one:
`inspector.AnchorTether` — the mechanism `fsmview.Tethered()` uses — anchors
to a **Ui rect** captured via `c.CaptureUiRect`, i.e. to a widget, and no
byte-range-to-rect channel exists in the IDL. Nor can it be derived Go-side:
the galley lives in Rust, and this host resolves `TextStyle::Monospace` to
the proportional main font when `mono_font_ttf` is unset, so column
arithmetic does not survive contact with the glyphs. The phase therefore
needs a third seam of its own, not a composition of the two that shipped.

**The UX case is weaker than the design entry assumed.** A bare literal is
already editable — it is text in a text editor — so a widget earns its place
only where it beats typing, which is a narrow set (dates, colors, regexes).
More decisively, this repository already has a home for typed value editing:
the parameter slot. `{d:DateTime}` gets a widget from
[ADR-0124](./0124-play-param-editing-widgets.md)'s ladder *and* signal
binding, URL shipping, history snapshotting, and the unfilled-Run gate. A
tethered editor over a bare literal would be a second editing surface with
none of those, and a second widget ladder to keep in step with the pane's.
Tethering to text is also fragile where tethering to a widget is not: the
anchor moves on every keystroke, scroll and wrap change, and both the caret
report and any future position report carry this seam's inherent one-frame
lag, so the window visibly trails its anchor while the user types — over the
code being edited.

Two cheaper shapes were identified and are recorded here as available rather
than chosen, since neither has a demonstrated need behind it yet:

- **Promote-to-parameter.** An action that rewrites the literal under the
  caret into `{name:Type}` and authors the matching `SET`, handing the
  editing to the pane that already exists. It composes the shipped pieces —
  the lex-tier token walk, the styled channel to mark promotable literals,
  the existing prelude author — and needs no new FFI seam. It also makes the
  value reusable and signal-bindable, which the tethered form does not.
- **Caret-scoped affordances.** Rendering the affordance for the call site
  under the caret instead of listing every one. Uses only what L3 shipped.

**Trigger to revisit:** a value whose editing genuinely cannot be served by
promoting it to a parameter — the case would have to show why the pane is
the wrong place, not merely that a floating window would be closer to the
text. Until then the out-of-scope line in the entry above stands, with this
reasoning attached so it is not re-derived.

### 2026-07-25 — correction: the svgexport test debt was discharged, and its recorded cause was wrong

The 2026-07-18 entry above reports
`render_svg_window_content_only_shrinks_viewbox_and_strips_bg` as "left
failing rather than papered over", and both later entries carry that forward
in their out-of-scope lists. It has passed since `15bd9116`, landed the same
day, so the claim went stale within hours of being written and was then
repeated twice. Noticed 2026-07-25 while running the suite for L3.

The diagnosis recorded with it was also wrong, which is the part worth
correcting rather than merely dating. This ADR blamed egui 0.35's `run_ui`
wrapping the pass in an implicit full-screen `Ui` whose background the
content-only exporter fails to strip — i.e. a defect in the exporter. The
actual cause was in the test: its `baseline_rect_present` heuristic treated
any bare `<rect x=… fill=…>` without rx / fill-opacity / stroke as a
`finish()` baseline background, and egui 0.35 paints transparent rects behind
text that `emit_rect` writes as `fill="none"`. A real baseline always carries
an opaque colour fill, so excluding `fill="none"` was the fix. The exporter
was not stripping the wrong thing; the assertion was recognising the wrong
thing.

Nothing in this ADR's decision depends on either version of that story — the
test debt was always collateral from the egui bump, noted here only because
fixing the crate's test target to run the seam's own tests is what surfaced
it. The out-of-scope lists' remaining items stand.

## References

- [sql-editor-highlighting-survey](../explanation/sql-editor-highlighting-survey.md) —
  the option space and kill-reasons this ADR builds on.
- [ADR-0125](./0125-codeview-prepare-memo.md) — codeview `Prepare*` memo; the
  open steady-state parse cost this ADR routes around.
- [ADR-0063](./0063-imzero2-textedit-insert-at-cursor.md) — TextEdit
  `insertAtCursor`; the pending-state + custom-apply idiom this seam reuses.
- [ADR-0084](./0084-nanopass-antlr-dfa-cache-bounding.md) — the ANTLR DFA
  cache the lex phase shares.
- egui 0.35 `TextEdit::layouter` (`widgets/text_edit/builder.rs`) — the
  upstream hook.
- ClickHouse `play.html` (read as served by ClickHouse 26.6.1) — prior art
  for lex-only per-edit highlighting and the function-name peek-ahead.
