---
type: adr
status: accepted
date: 2026-07-28
reviewed-by: "p@stergiotis"
reviewed-date: 2026-07-31
---

# ADR-0147: `sqleditor` widget and context-sensitive SQL completion

## Context

[ADR-0130](./0130-imzero2-textedit-highlight-seam.md) gave play's SQL editor
lexical and semantic color, an error underline, a line-number gutter, statement
tinting and run-under-cursor. Two things follow from where that left the code.

**The editor has no completion.** A reader cannot discover a table's columns, a
function's name, or a leeway handle without leaving the buffer. The vocabulary
worth completing here is partly boxer's own — friendly column handles
([ADR-0116](./0116-play-leeway-column-handle-resolution.md)), keelson macros
([ADR-0108](./0108-keelson-sql-pass-registry.md)), introspection tables
([ADR-0094](./0094-keelson-introspection-tables.md)), ad-hoc datasets
([ADR-0134](./0134-adhoc-datasets.md)) — none of which any external tool knows
about.

**The editor is play-private.** Every affordance lives in `apps/play` as
app-local helpers. A second SQL editing surface,
`apps/sqlappletcreator/sqlappletcreator.go`, is a bare
`TextEdit().CodeEditor()` and has inherited none of them; a third would inherit
none either. Completion is roughly twice the code of everything ADR-0130 added,
which puts the editor past the size where app-local is the right home.

The analysis substrate is largely present: an exact-dialect lexer running per
keystroke, a scope model with alias and CTE resolution, a cached `system.columns`
probe, and a quiescence tier for expensive work. The survey
([sql-completion-survey.md](../explanation/sql-completion-survey.md)) works
through what is missing; the load-bearing findings it establishes, which this
ADR takes as given:

- `nanopass.Parse` discards ANTLR's recovered tree on any syntax error
  (`nanopass_parse.go:109-114`), so parse-derived facts are unavailable while
  typing — which is when completion is wanted.
- imzero2 fetchers run at frame end, so a fetcher-based `consume_key` fires
  *after* the `TextEdit` already handled the keystroke. Enter would both accept
  a completion and insert a newline.
- egui 0.35 offers `TextEdit::return_key(None)` as a public builder method, but
  exposes no builder access to `EventFilter` — so Up/Down cannot be reclaimed
  that way.
- `InsertAtCursor` ([ADR-0063](./0063-imzero2-textedit-insert-at-cursor.md))
  already replaces any selection, so setting a selection is sufficient to get
  replace-range.
- Computing caret pixel geometry Go-side is unsound: the host may leave the
  monospace face unconfigured, and `TextStyle::Monospace` then resolves to a
  proportional font.

## Design space (QOC)

**Question.** Where does the editor learn what may appear at the caret?

**Options.**

- **O1** — **Lexical.** Walk back through the existing `HighlightLex` token
  stream: previous significant token, bracket depth, enclosing clause.
- **O2** — **Scope-aware.** Reach `BuildScopes` for alias, CTE and
  table-set resolution, via a best-effort parse or buffer repair.
- **O3** — **Grammar-derived (ATN).** Compute the expected token/rule set from
  grammar1's ATN — the antlr4-c3 technique, ported to Go.
- **O4** — **External language server.** Speak LSP to a generic SQL server.

**Criteria.**

- **C1 — Works on an incomplete buffer.** Does it answer while the user is
  mid-token, which is every moment completion is wanted?
- **C2 — Context precision.** Clause-level, relation-level, or rule-exact?
- **C3 — Knows boxer vocabulary.** leeway handles, keelson macros, ad-hoc
  datasets, the introspection plane.
- **C4 — Build cost and ongoing carry.**

**Assessment.** `++` strong positive, `+` positive, `−` negative, `−−` strong negative.

|    | O1 | O2 | O3 | O4 |
|----|----|----|----|----|
| C1 | ++ | −  | ++ | +  |
| C2 | +  | ++ | ++ | −  |
| C3 | ++ | ++ | ++ | −− |
| C4 | ++ | +  | −− | −  |

O1 and O2 are complements rather than rivals — O1 answers always and coarsely,
O2 answers precisely when the buffer permits — and the assessment reflects that
neither is sufficient alone. O3 dominates on precision and is rejected on C4
alone. O4 fails the criterion that matters most here.

## Decision

We will extract play's SQL editor into a reusable
`public/thestack/imzero2/egui2/widgets/sqleditor` package, adopt it in
`sqlappletcreator`, and give it context-sensitive completion built on the
lexical tier (O1), delivered through a keyboard-driven list. Scope of this ADR
runs to a keyboard-operable completion over an unanchored list; scope-aware
context (O2) and an anchored popup are named here with their triggers and
deliberately deferred.

### Widget extraction

- **SD1 — Behaviour-identical move first.** The extraction commit moves the
  `TextEdit` chain, no-wrap layout, gutter and marks lane, styled overlays,
  statement splitting, caret resolution and the highlight tiers into
  `widgets/sqleditor` with no behaviour change, reviewable as a move. Completion
  lands afterwards, never in the same commit. play's editor is load-bearing and
  its id handling is subtle; a combined commit would make a regression
  indistinguishable from a design flaw.
- **SD2 — The seam runs in both directions.** *Inwards*, the widget takes an
  injected provider interface, shaped as *given a buffer and a caret, what may
  go here* plus *given a kind and a prefix, what are the candidates* — which is
  also what makes the completion engine unit-testable without a UI. *Outwards*,
  the widget returns a result carrying the buffer, the caret, the active
  statement's range, its 1-based number and the statement count, and the
  composed run buffer. Anything derivable from buffer-and-caret alone is the
  widget's to compute and publish; the embedder is told, not asked.

  **Run-under-cursor is the worked example.** Its logic is already pure over
  `(sql, caret)` — the split, the caret-to-statement boundary rule, and the
  prelude-plus-statement composition — so all of it moves. What stays in
  `apps/play` is only what the app does *with* the result: shipping it through
  `BuildStatement`, the wire preview, the staleness witness that gates auto-run,
  and history snapshotting the full buffer rather than the statement. The same
  test applies to the rest of the exclusion list: param slots and the prelude
  hide/mirror state machine, unfilled-input gating, endpoint routing and the
  diagnostics banner stay because each needs play's model, not because the
  feature happens to be consumed there.

  Two contract details the result must state rather than leave implicit. A
  single-statement body's run buffer is the whole trimmed buffer, not a
  statement slice — a deliberate byte-identical-to-before path that an embedder
  would otherwise have to rediscover. And the split skips a leading `SET`
  prelude via `env.BodyOffset`, so a parameterised single-statement buffer does
  not read as multi-statement; that is widget behaviour with a knob if a
  consumer ever wants raw counting, not a policy pushed onto the embedder.

  `sqlappletcreator` inherits run-under-cursor from this, having shipped only
  whole buffers until now.
- **SD3 — `sqlappletcreator` adopts the widget** with a narrower provider (or
  none), which is the acceptance test for SD2: if adoption needs play-shaped
  state, the seam is drawn in the wrong place.
- **SD4 — Widget conventions apply.** Multi-child id scoping via `c.IdScope`,
  a `doc.go`, `PackageProps` ([ADR-0080](./0080-packageprops-per-package-declarations.md)),
  and a registered `Demo` so the screenshot driver
  ([ADR-0057](./0057-demo-registry-and-drivers.md)) covers it. The stateful-
  widget contract ([ADR-0013](./0013-imzero2-stateful-widget-contract.md))
  governs the databinding.

### Completion engine

- **SD5 — Lexical context tier.** Context is determined by walking back through
  the `HighlightLex` token stream already computed per keystroke: previous
  significant token, bracket depth, enclosing clause keyword, with a one-token
  peek-ahead for `(` to separate functions from identifiers. It answers on a
  buffer that does not parse, which is the requirement no other option meets as
  cheaply.
- **SD6 — Catalog probes never run on the frame thread.** The existing
  `system.columns` probe runs inline with a 5 s timeout, which is fine on the
  execution path and is not fine per frame. Every completion probe — columns,
  tables, functions, settings — goes through the background-task primitive
  ([ADR-0038](./0038-keelson-background-task-primitive.md)) and answers from
  cache or answers "not yet". A completion list that is one keystroke stale is
  correct behaviour; a dropped frame is not.
- **SD7 — Catalogs are resolved per buffer, not per app.** Endpoint
  auto-routing ([ADR-0141](./0141-play-endpoint-dispatch-seam.md)) makes the
  target plane a property of the query, and ad-hoc datasets contribute tables no
  `system.tables` enumerates. The provider interface therefore resolves the
  catalog per request rather than holding one.
- **SD8 — Candidate sources in scope.** Keywords from grammar1's vocabulary;
  param names and types from `collectParamSlots`; snippets from the existing
  help-corpus blocks; tables, columns, functions and settings from SD6 probes;
  leeway handles per SD9. Semantic-layer entries
  ([ADR-0139](./0139-semantic-layer-text2dsl.md) T1) are a later tier and not a
  dependency.
- **SD9 — Handle enumeration is a separate reader.** `lwsql.Resolver` stays
  lookup-only. The Resolver is a pass-time object: its per-table indexes are
  built lazily during a rewrite and cached for the session, invalidated by
  `Reset` on an endpoint switch. Completion reads on a different cadence, for a
  table nobody has queried yet, and a miss must be cheap rather than
  authoritative. Giving one object both lifetimes is what the split avoids.

  **The derivation is shared, not duplicated.** Turning a physical column list
  into sections and columns — the part that knows
  `tv:geoPoint:pointLat:val:f32:…` means `geoPoint:pointLat` — is factored out
  so the Resolver and the reader build the same index from the same
  `passes.SchemaProviderI` and cache it independently. A second reading of the
  physical-name grammar is the failure mode here: it would compile, pass its own
  tests, and drift from the Resolver silently, so that completion offers a
  handle the rewrite then refuses. The shape the reader exposes is already
  computed internally — `Resolve` returns exactly this list as `Candidates` on
  an unknown-column result.

### The FFI seam

- **SD10 — Key capture is a `TextEdit` builder method, not a fetcher.** It runs
  `consume_key` before `apply_widget` — the same point in `interpreter.rs` where
  the highlight layouter is installed — and pushes the pressed set back through
  a per-widget value channel, the way the caret channel already works. Go emits
  it only on frames where the list is open, so arrow keys and Tab behave
  normally otherwise. Enter additionally uses `return_key(None)` while the list
  is open, being the switch egui provides for exactly that key. The fetcher
  route is rejected on mechanism, not taste: fetchers drain at frame end, after
  the widget has consumed the event.
- **SD11 — Replace-range is a selection method, not a second insertion path.**
  A method setting the selection to a char range composes with `InsertAtCursor`,
  which already replaces any selection. No new mutation path, and no reliance on
  a `setCursor` that does not exist.
- **SD12 — The list is unanchored, in this ADR.** It renders as a strip beside
  the editor. No caret rect crosses the FFI and no window follows the caret.

### Milestones

- **M0 — Extraction.** SD1–SD4, behaviour-identical, `sqlappletcreator` adopted.
- **M1 — Context and candidates, headless.** SD5, SD7–SD9 as pure Go behind the
  provider interface, table-tested without a UI.
- **M2 — Probes.** SD6: tables, functions and settings probes; columns moved
  off the frame thread.
- **M3 — The list, click-only.** Candidates rendered in the strip,
  click-to-insert through the existing insert-at-caret op. No IDL change to
  here.
- **M4 — The seam.** SD10 and SD11: IDL, regen, Rust, generator. Type, arrow,
  Enter, Esc.
- **M5 — Record and close.** Dated Updates here and on ADR-0130; a help-corpus
  paragraph; full verification.

M3 is the first user-visible payoff and the last rung before any IDL churn.
Taking M1–M3 before M4 validates the two things most likely to be wrong — does
the classifier pick the right context, and are the probes fast enough to feel
live — where they can be seen, and with no regen risk.

## Alternatives

- **Scope-aware context now (O2).** Deferred, not rejected: it is where alias
  and CTE resolution come from, and it needs either a `ParseBestEffort` sibling
  that keeps ANTLR's recovered tree or a buffer-repair layer, neither of which
  should be designed under completion's schedule. **Trigger:** M1–M4 shipped and
  users hitting unqualified-column completion across a JOIN.
- **Grammar-derived expected tokens (O3, antlr4-c3).** The principled answer,
  rejected on cost: no Go port exists, so it is a from-scratch subsystem with
  its own follow-set traversal and caching against a grammar whose DFA cache
  already needed [ADR-0084](./0084-nanopass-antlr-dfa-cache-bounding.md) to
  bound it. Its advantage over SD5 is exhaustive keyword correctness, the part
  of completion users notice least. **Trigger:** grammar1 growing past the
  SELECT surface far enough that SD5's hand-maintained clause tables disagree
  with it.
- **An external SQL language server (O4).** Rejected: no ClickHouse-dialect-exact
  server exists, and an external one would know nothing about the vocabulary
  most worth completing here — the same external-binary objection that ruled out
  nvim for highlighting.
- **`egui_code_editor`'s completion.** Rejected as a dependency, kept as a parts
  reference: keyword-set-grade classification, with its own theming and id
  handling.
- **Handle enumeration as a method on `lwsql.Resolver`.** Rejected (SD9): it is
  the smaller diff, and it gives one object two cache lifetimes — a pass-time
  index built during a rewrite and invalidated on endpoint switch, serving
  frame-time reads for tables no query has touched. The reader is kept separate
  and the index derivation shared.
- **A fetcher-based key channel.** Rejected on mechanism (SD10): frame-end drain
  is too late.
- **Computing the caret's pixel position Go-side.** Rejected: unsound whenever
  the monospace face is unconfigured.
- **Leaving the editor in `apps/play` and adding completion there.** Rejected:
  it strands `sqlappletcreator` for a second time and doubles the surface a
  third editor would have to re-implement.

## Consequences

### Positive

- One editor implementation, so gutter, overlays, statements and completion
  reach every SQL surface at once rather than per app.
- Completion offers vocabulary no external tool can — leeway handles, keelson
  macros, introspection tables, ad-hoc datasets — because the analysis is
  in-process.
- The provider seam makes context and candidate selection unit-testable without
  a UI or a live server.
- M0–M3 deliver a usable feature with no IDL, regen or Rust change, so the
  risky part is isolated to M4.
- The key-capture method is reusable: any future widget needing to steal a
  keystroke from a focused `TextEdit` has a precedent.

### Negative

- The extraction touches load-bearing play code with subtle id handling; a
  behaviour-identical move is still a real regression risk.
- Two new `TextEdit` methods widen the IDL surface, and `interpreter.rs`'s
  hybrid region must be reconciled before regen.
- SD5 hand-maintains clause knowledge that grammar1 also encodes — a duplication
  that only O3 removes, and the drift is silent.
- Candidate quality is bounded by the lexical tier until O2 lands: unqualified
  column completion across a JOIN offers the union of plausible relations rather
  than the resolved set.
- More per-frame work in the editor, on a surface with an open frame-pacing
  jitter question.

### Neutral

- The completion list is one frame behind the buffer, as the highlight sections
  already are. Text stays authoritative in the `TextEdit`; candidates are
  advisory.
- SD12 leaves the list further from the caret than an IDE popup. Whether that
  reads as wrong is the trigger for C4 in the survey's ladder, not a defect to
  pre-empt.

## Updates

### 2026-07-31 — M0 landed, plus a caret-entity seam ahead of schedule

M0 (SD1–SD4) is implemented and `sqlappletcreator` has adopted the widget
(SD3). Recorded here rather than folded into the text above because the work
diverged from the plan in four places worth stating.

**The seam is two calls, not one.** `Editor.Bind(Frame) Result` then
`Editor.Render(ids, Decoration)`. The split is forced by ordering: the caret
arrives one frame late through the FFI, and the embedder's overlays are derived
*from* the caret, so there is no single call that can both resolve it and
consume decorations computed from it. Bind publishes; the embedder decorates;
Render draws.

**The statement tint moved to the widget**, per SD2's own test — it follows
from buffer and caret alone. Two consequences. The gutter's active mark is
derived by recognising a section's *tone*, so the tones are exported
(`ToneError`, `ToneStatementTint`, …) and play composes in the same vocabulary
rather than defining a second one that would drift. And the tint is no longer
behind play's pipeline-quiescence gate, which it never needed: it now stays put
while typing instead of flickering off. That is a deliberate behaviour change
in a commit that was meant to be behaviour-identical, and it is the only one.

**The caret is resolved once, against the buffer that rendered.** The
pre-extraction code resolved twice in hide-prelude mode — first against the
canonical buffer (short by the elided prelude), then against the mirror, after
the overlays had already been derived from the first answer. Deciding the
binding before `Bind` retires that; play's `renderSqlEditor` now picks the
buffer, then binds, then decorates.

**Subquery narrowing did not move.** SD1 lists "statement splitting, caret
resolution"; narrowing is neither — it needs a nanopass parse of the statement,
which is a subsystem rather than a buffer-and-caret derivation. It stays in
`apps/play` and reaches the widget as `Decoration.SubqueryMark`.

**M1's provider interface is not built.** M0 shipped without it because nothing
needs it yet; the completion engine (M1–M4) is untouched and its milestones
stand.

#### The caret-entity seam

`Result.Entity` publishes what the caret is pointing at: the name it sits on
(`highlight.CaretEntity.Name`, with `Call` set when the lexer's `(`
peek-ahead promoted it) and `Enclosing`, the callees of the argument lists
around it, innermost first. The resolver is `highlight.EntityAt`, pure over the
lex-span stream and the caret offset — the same tier, and for the same reason,
as SD5's context classifier: it answers on a buffer that does not parse, which
is every moment the question is asked.

This is ahead of the milestone order — the first consumer is documentation
lookup, not completion — and it is placed in `highlight` rather than in the
widget because it is pure over the token stream that already lives there, and
so is table-testable without a UI. The widget publishes it because SD2 says
anything derivable from buffer-and-caret is the widget's to publish, and
because two consumers deriving it separately would eventually disagree about
which frame's caret they used. Completion is the next reader of the same walk.

#### First consumer: play's Docs pane

A tool pane (nil `PanelI`, dock id 17) rendering ClickHouse's own
`system.documentation` row for the caret's entity as markdown. It reads
`Result.Entity` and nothing else of the editor's, which is the acceptance test
for the seam pointing outwards. Two facts it established that a later reader
will want:

- **`type` is an `Enum8`, and ClickHouse ships an Enum8 over Arrow as the raw
  int8 ordinal** — the enum's names do not cross the wire at all. A client
  reading the column directly gets numbers and no way back to "Table Engine";
  `toString(type)` server-side is the only place the dictionary exists. Caught
  by a live test, not a unit test.
- **The corpus's links are site-relative** — a CommonMark link whose target
  starts `/sql-reference/…` — written for ClickHouse's documentation site,
  which has a base this app does not. The markdown widget's resolver
  seam covers wikilinks and embeds, not plain CommonMark links, so they are
  absolutised in a textual pre-pass before `markdown.Parse`.

The lookup runs on an ordinary `newNodeLane` over `clientExecutor`, so it
inherits endpoint routing, auth, the pre-execute pass registry and the Arrow
decode, and stays off the frame thread — the same discipline SD6 requires of
the completion probes, arrived at independently because the constraint is the
same.

## Status

Accepted 2026-07-31. M0 is implemented and adopted by both surfaces (see the
2026-07-31 Update); M1–M5, the completion engine, are open and their milestones
stand as written.

Status lifecycle: `Proposed → Accepted → (Deferred | Deprecated | Superseded by ADR-XXXX)`.
See [DOCUMENTATION_STANDARD §1 ADR](../DOCUMENTATION_STANDARD.md#architecture-decision-records-why-it-is-this-way) for the edit-policy tiers (Tier 1 in-place / Tier 2 dated `## Updates` entry / Tier 3 new superseding ADR).

## References

- [sql-completion-survey.md](../explanation/sql-completion-survey.md) — the
  survey this ADR decides on.
- [sql-editor-highlighting-survey.md](../explanation/sql-editor-highlighting-survey.md)
  — the prior survey, whose §6 deferred completion to this one.
- [ADR-0130](./0130-imzero2-textedit-highlight-seam.md) — the highlight seam and
  the L3 affordances this builds on.
- [ADR-0063](./0063-imzero2-textedit-insert-at-cursor.md) — insert-at-cursor,
  which SD11 composes with.
- [ADR-0116](./0116-play-leeway-column-handle-resolution.md),
  [ADR-0108](./0108-keelson-sql-pass-registry.md),
  [ADR-0094](./0094-keelson-introspection-tables.md),
  [ADR-0134](./0134-adhoc-datasets.md),
  [ADR-0141](./0141-play-endpoint-dispatch-seam.md) — the vocabulary and catalog
  sources SD7 and SD8 draw on.
- [ADR-0139](./0139-semantic-layer-text2dsl.md) — the semantic layer, a later
  candidate tier.
- [ADR-0038](./0038-keelson-background-task-primitive.md) — the async primitive
  SD6 requires.
- [ADR-0013](./0013-imzero2-stateful-widget-contract.md),
  [ADR-0080](./0080-packageprops-per-package-declarations.md),
  [ADR-0057](./0057-demo-registry-and-drivers.md) — the widget conventions SD4
  invokes.
- [antlr4-c3](https://github.com/mike-lischke/antlr4-c3) — the O3 technique.
