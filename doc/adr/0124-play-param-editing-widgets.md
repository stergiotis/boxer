---
type: adr
status: accepted
date: 2026-07-15
reviewed-by: "p@stergiotis"
reviewed-date: 2026-07-15
---

# ADR-0124: `play` parameter editing widgets — detection, registration, mechanics

## Status

Accepted — 2026-07-15 (reviewed-by: p@stergiotis).

The subsystem was built long before this record; no ADR had ever covered it.
§SD1-4 and §SD6 document that as-built design — the review covered whether the
design was right, not whether to adopt it.

§SD5 and §SD7 are the changes the review authorises, including §SD5's retirement
of a deliberately pinned assertion (`TestDateTimePairWidgetRejectsNonAdjacent`,
`apps/play/play_param_widget_test.go:25`). Neither is built at acceptance; a
dated `## Updates` entry records what ships.

## Context

`play`'s SQL editor surfaces an editing widget for every `{name : Type}`
placeholder in the buffer, above the editor, under a `PARAMETERS` heading.
Filling one authors a `SET param_<name> = …` line in the buffer's leading
prelude; on Run the prelude is stripped from the body and the values ride the
request URL, so ClickHouse performs the substitution server-side.

The subsystem has a real architecture — a grammar-level detector, a
chain-of-responsibility widget registry with a catch-all tail, stateful widgets
bound to draft strings, and a single-owner mirror back into the query text — and
none of it is written down. The rationale lives in file-level comments
(`apps/play/play_param_widget.go:12-43`, `apps/play/play_param_render.go:49-59`),
which is enough to maintain a file and not enough to answer whether the design is
right, where a new widget goes, or why an unsupported type is not an error.

That gap now costs something concrete. The registry encodes exactly one policy
about *combinations* of placeholders — two adjacent slots named `from` and `to`,
both DateTime, fold into one range control — and that policy is wrong in two
specific ways, which surfaced when someone asked why the editor reacts to some
parameter sets and not others:

**Adjacency guards a collision the detector cannot produce.** The dispatch loop
that offers a widget repeated matches justifies itself with "two from/to pairs in
one query" (`play_param_render.go:81-84`). §SD1's dedup-by-name forecloses that:
there can never be two slots named `from`. So adjacency generates false negatives
and nothing else — `{from:DateTime64} … {a:UInt64} … {to:DateTime64}` folds
nothing, and that behaviour is pinned as a test using `{x:UInt64}` as the spoiler.

**The vocabulary excludes our own range.** The timeline snippet
(`apps/play/help/snippets.md:139-140`) binds `{tl_min:DateTime64(3, 'UTC')}` and
`{tl_max:DateTime64(3, 'UTC')}` — a range that gets two plain text fields. It
cannot be renamed to `from`/`to`: those names are the signal contract the
Timeline panel publishes ([ADR-0097](./0097-play-reactive-query-graph.md), and
`apps/play/help/features.md:84`).

Both are policy bugs inside a sound structure. Recording the structure is the
precondition for fixing the policy — and the structure is also why the fix is
cheap: the fold rule is one function, and changing it touches no widget, no value
path, and no wire format.

## Design space (QOC)

> Scoped to §SD5 and §SD7 — the fold policy and its legibility. §SD1-4 and §SD6
> record an as-built design and are not a live choice.

**Question.** How should the registry decide that two DateTime placeholders are
one range, and how should a user find out that ranges exist at all?

**Options.**

- **O1** — Widen and explain: pair by name stem over a closed suffix table, drop
  adjacency, and report in the pane when a fold was declined.
- **O2** — Explain only: keep the from/to adjacency rule, add the reporting.
- **O3** — Widen only: fix the rule, add no chrome.
- **O4** — Declarative marker: infer nothing; add `-- play: range <lo> <hi>` in
  the vocabulary of the existing `-- play: ungroup` (§SD6).

**Criteria.**

- **C1** — Covers the in-repo range vocabulary; assessed by whether
  `tl_min`/`tl_max` folds without violating the Timeline signal contract.
- **C2** — Explains a declined fold; assessed by whether a user seeing text
  fields can tell why from the pane alone.
- **C3** — Mis-fire cost; assessed by what an unwanted fold costs and whether an
  escape exists.
- **C4** — Discoverable unaided; assessed by whether a user meets the feature
  without reading `features.md`.

**Assessment.** `++` strong positive, `+` positive, `−` negative, `−−` strong negative.

|    | O1 | O2 | O3 | O4 |
|----|----|----|----|----|
| C1 | ++ | −− | ++ | +  |
| C2 | ++ | ++ | −− | −  |
| C3 | +  | ++ | +  | ++ |
| C4 | ++ | +  | −  | −− |

O2 scores `−−` on C1 by construction: it keeps the rule that excludes
`tl_min`/`tl_max`, so its report would permanently advise a rename the Timeline
contract forbids. O4 scores `−−` on C4 for the reason it scores well elsewhere —
a syntax with no inference behind it is only found by those who already know it.

## Decision

We record the subsystem as four seams — detection (§SD1), registration and
dispatch (§SD2), capability late-binding (§SD3), and the value path (§SD4) — and
set the fold policy (§SD5), its opt-out (§SD6), and its legibility (§SD7) inside
them.

### SD1 — Detection: a grammar production, deduped by name ✓

A placeholder is a parse result, not a text match. `paramSlot` is a production in
the ClickHouse grammar — `(LBRACE identifier COLON columnTypeExpr RBRACE)`,
`grammar1/ClickHouseParserGrammar1.g4:276`, hooked into the expression rule as
`ColumnExprParamSlot` — and `collectParamSlots`
(`apps/play/play_param_slots.go:72`) walks the CST for those nodes. There is no
regex anywhere in the path.

Two properties follow and are relied on downstream. Whitespace is free:
`{a : UInt64}` and `{a:UInt64}` are the same slot, because the lexer eats the
gaps. And a placeholder-shaped run of characters inside a string literal or a
comment cannot produce a phantom slot, because it never reaches the parse tree as
one.

A slot carries `Name`, `Type` (raw type source text — `UInt64`,
`Nullable(DateTime64(3))` — never interpreted here), and `Src` (a source range,
carried for a future highlighter and unread today).

**The list is deduped by name; the first occurrence's type wins.** This is the
load-bearing property of the whole subsystem: a name identifies at most one slot,
so a slot is addressable by name alone. Every later seam depends on it — drafts
key on name (§SD4), widget state keys on name (§SD2), signals unify by name
(§SD4), and pairing is decidable without positions (§SD5).

Dedup discards information, so the discarded copy is recovered where it matters:
`collectSlotTypes` (`play_param_slots.go:117`) does the same walk *without* dedup
and returns each name's distinct declared types, which is what lets the Signals
chrome warn `⚠ type conflict across nodes` when two nodes declare one name
differently. The editor's hot path uses `extractSlotsAndParams`, which produces
the slot list and the prelude values from a single parse.

### SD2 — Registration: an ordered chain with a catch-all tail ✓

Widgets implement `paramWidgetI` (`play_param_widget.go:23`): `Matches` scans
slots and returns the indices it claims, `Render` draws its claim, `IsGroup`
declares whether the claim is a bundle, and `ClearStateForAbsent` prunes state
for vanished names.

They are registered as an **ordered slice** (`play_renderer.go:616`) and offered
the *unconsumed* slots in turn — most specific first, catch-all last:

```go
paramWidgets: []paramWidgetI{
    newDateTimeRangeWidget(),  // folds a range; declines without an evaluator (§SD3)
    newDateTimePairWidget(),   // folds the same pair; no evaluator needed
    newScalarTextWidget(),     // catch-all: claims index 0 unconditionally
}
```

Order is the whole mechanism, and the chain has two properties worth stating
because they are what make it safe to extend:

- **Totality.** `scalarTextWidget.Matches` claims index 0 whenever any slot
  remains, so every slot renders something. A shape nobody recognises is not an
  error and not a blank — it is a text field, which always works because the value
  is text on the wire anyway (§SD4). This is why the subsystem has no type
  whitelist: ClickHouse is the authority on its own type grammar, and an
  unrecognised type costs the user a picker, never a query.
- **Combination.** `Matches` takes the *slice*, not one slot, so a widget can
  claim several slots as one bundle. A per-slot type switch could not express a
  range at all. The re-dispatch loop (`play_param_render.go:85-115`) then lets one
  widget claim multiple disjoint bundles per frame; indices are relative to the
  remaining list and `absoluteIndex` maps them back.

Adding a widget is therefore a registration, not a modification: insert it ahead
of the tail, and the slots it declines fall through unchanged.

### SD3 — Capability absence is a different widget, not a broken one ✓

The range picker needs a Phase-4 evaluator
([ADR-0016](./0016-imzero2-time-range-picker.md)) to resolve expressions like
`now() - INTERVAL 1 HOUR`, which needs a bus, which the host supplies only at
`SetCapabilities` time ([ADR-0026](./0026-app-runtime-and-capability-subjects.md),
`play_renderer.go:394-420`) — after `NewPlayApp` has already built the registry.

The seam is an opt-in sub-interface: `evaluatorAwareI`
(`play_param_widget.go:69`). `SetCapabilities` fans the constructed evaluator to
every registered widget that implements it; widgets that do not are untouched.
Only a successfully constructed evaluator is fanned out, because passing a
typed-nil pointer through an interface parameter would arrive non-nil on the
widget side.

The decision this encodes: **a missing capability is expressed as a declined
match, not a disabled control.** `dateTimeRangeWidget.Matches` returns false while
its evaluator is nil (`play_param_widget_range.go:89-94`), so §SD2's chain moves
on and the next widget claims the pair. The user gets a working control with fewer
features rather than a greyed-out one, and no code path has to handle "picker
without evaluator".

The cost is that two visibly different controls exist for one query shape, with
nothing on screen saying why — which §SD7 addresses.

### SD4 — The value path: a draft string is a widget's only writable surface ✓

A widget never writes SQL, never touches the URL, and never sees the request. It
reads and writes one `*string` per claimed slot, handed to it in `paramCtx`. The
orchestrator owns everything else, one direction per phase:

1. **Parse → drafts.** `refreshParamSlotsFromParse` (`play_param_render.go:20`)
   refreshes the slot list, ensures each name has a stable draft pointer,
   overwrites drafts whose prelude value differs (the parser wins on text edits),
   evicts vanished names, and prunes widget state.
2. **Widget → draft.** Each widget mutates its drafts during `Render`.
3. **Drafts → prelude.** `syncParamDriftToPrelude` (`play_param_render.go:127`)
   compares each draft to its last-synced value and, on drift only, rebuilds the
   leading `SET param_*` prelude via `SyncParamPrelude` and commits the new
   buffer. Idempotent when nothing moved.
4. **Prelude → wire.** `BuildStatement` strips the prelude from the body and
   `ExecuteArrowStream` puts the values on the URL as `param_<name>=…`
   (`play_client.go:375-386`).

Two consequences of this shape are load-bearing.

**Draft pointers must be stable across frames.** The FFFI2 `SendRespVal` binding
applies at end-of-frame, so a widget's write lands one frame after the click and
a before/after comparison within one frame never fires. A widget that needs to
detect its own control's output therefore caches it across frames and compares
against the previous frame's value — which is what
`dateTimePairSlotState.lastRenderPacked` is for, and why widgets are stateful at
all.

**Text is the only value type.** `encodeParamLiteral` (`play_param_inject.go:81`)
picks quoting from three prefix families — compound (`Array(`, `Tuple(`, `Map(`)
passes verbatim, numeric passes verbatim when the value parses as numeric,
everything else is single-quoted. A numeric type carrying a non-numeric value
falls to the quoted bucket deliberately, so ClickHouse answers with a typed error
rather than silently coercing. The predicates classify for *quoting*; only
`isDateTimeType` gates a widget.

A slot whose value the user filled is a **constant**: buffer-owned, part of the
query text, reproducible by copy-paste. A slot left without a `SET` is a live
**signal** (ADR-0097), and a SET-bound name shadows a same-named signal
(`play_client.go:384`). The pane is thus the instrument that turns a signal into a
constant.

### SD5 — Fold policy: pair by stem, not by position ✓

A slot name decomposes into a stem and a suffix: it either equals a suffix (empty
stem) or ends with `_` followed by one. Two slots pair when their stems are equal,
their suffixes are the two halves of one table entry, and both types are DateTime
or DateTime64 after `Nullable` unwrap.

The table is closed:

| lo | hi |
|----|-----|
| `from` | `to` |
| `min` | `max` |
| `start` | `end` |
| `lo` | `hi` |
| `since` | `until` |

`first`/`last` is excluded: it names an ordering, not a bound. `t0`/`t1` is
excluded: a digit suffix needs a different decomposition and earns little.

Position is not consulted, and §SD1's dedup is what makes that safe: at most one
slot can carry a given name, so a stem admits at most one pair per entry and no
tiebreak is needed. `{to:…}` written before `{from:…}` folds, and the widget
renders lo-then-hi regardless of editor order. A query with two stems —
`a_from`/`a_to` and `b_from`/`b_to` — yields two pairs, which is the first real
use §SD2's re-dispatch loop has ever had.

`isDateTimeType` is unchanged, so the fold stays DateTime-only. The range and pair
widgets keep sharing one matcher, so they cannot drift on what counts as foldable.

### SD6 — The opt-out: `-- play: ungroup` ✓

A fold is an inference from names, and names are weak evidence of intent. When the
inference is wrong the user must be able to refuse it, so the buffer carries an
opt-out: a line comment reading exactly `-- play: ungroup` (`scanUngroupHint`,
`play_param_widget.go:301`). When present, dispatch skips every widget whose
`IsGroup()` is true (`play_param_render.go:73`), so §SD2's tail claims the slots
and each one gets a plain text field.

```sql
-- play: ungroup
SELECT * FROM t WHERE ts BETWEEN {from:DateTime} AND {to:DateTime}
```

Without the comment that query shows one range control; with it, two text fields.

It is a comment rather than a checkbox for the same reason the `SET` prelude is a
statement rather than a side panel: it lives in the query text, so it travels with
the query — copy-paste carries the intent, and no state exists beside the buffer
that a paste would lose.

Its known limit is granularity. The flag is buffer-level and all-or-nothing: it
disables *every* group widget, not one pair. With at most one foldable pair per
query that has never bitten. §SD5 makes several pairs possible and so makes it
reachable, which is what puts a per-pair marker in §SD8 rather than in this
decision.

### SD7 — Legibility: a declined fold says so ✓

The matcher knows why it declined and currently returns a bare `false`, throwing
the reason away. It will instead surface the near-miss, in the register the
Signals chrome already uses for `⚠ type conflict across nodes`
(`play_graph_view.go:199-207`): one weak, small line. Never an error, never a
modal, never a gate on execution.

After dispatch, the pane reports on what no widget claimed:

- two or more unclaimed DateTime slots with no stem match — name the slots and the
  vocabulary that would fold them;
- a stem that pairs but whose halves disagree on type — name both types, since
  intent is unambiguous and the fix is a one-word edit;
- `-- play: ungroup` in effect — say so, because its effect is otherwise
  indistinguishable from a matcher that failed.

A pair that *did* fold gets a label naming the fold and its opt-out, so the
inference is legible and reversible rather than magic. The pair widget says when
it is standing in for the picker because no evaluator was wired — the one gap §SD3
leaves open.

The vocabulary also belongs where it is met: the Snippets tab carries no range
example today, so §SD5 lands with one, and `features.md:60-71` is restated in
terms of the stem rule.

### SD8 — Deferred

- **A per-pair opt-out** — `-- play: range <lo> <hi>` (O4) doubling as a fold
  marker for names outside §SD5's table and as a targeted refusal, retiring §SD6's
  granularity limit. Worth having once the table's limits are known from use;
  premature before that.
- **`Date` / `Date32` folding.** Both pickers are DateTime-bound; a date-only
  range wants a different control, not a wider predicate.
- **Numeric ranges.** `{a_min:UInt64}` / `{a_max:UInt64}` is §SD5's rule with no
  widget behind it. A two-ended slider needs a domain the placeholder does not
  carry.
- **A signal-pin annotation** in the pane: §SD4's shadowing means filling a folded
  `tl_min`/`tl_max` picker stops the Timeline driving it. The knowledge exists
  (`play_signals_chrome.go` distinguishes `pinned by SET` from `unfilled input`);
  plumbing it across panes is scope this decision does not need.
- **`Src` consumers.** §SD1 carries a source range no one reads; a placeholder
  highlighter is the obvious claimant.

## Alternatives

Structural, on §SD1-4:

- **A type switch instead of a widget chain.** Rejected: a per-slot switch cannot
  express a claim over *several* slots, so a range control would be inexpressible
  by construction (§SD2). The chain costs an ordering constraint and buys
  combinations.
- **Validate types against a whitelist.** Rejected: ClickHouse owns its type
  grammar, and a whitelist here would drift behind it and turn an
  unknown-but-valid type into an error. Falling through to a text field (§SD2) is
  wrong about the *widget* and right about the *query*.
- **Let widgets write the buffer directly.** Rejected: two writers to one text
  buffer, racing the debounced parse that also owns it. §SD4's single-owner mirror
  is what lets a widget be a pure function of its drafts.
- **Detect placeholders by regex.** Rejected: `play` already holds a parse of the
  buffer for the preview and the graph, so a regex would be a second, weaker
  source of truth — one that would see placeholders inside string literals and
  comments.

Policy, on §SD5 and §SD7:

- **Explain only, keep from/to adjacency (O2).** Rejected: it preserves the blind
  spot that prompted this. `tl_min`/`tl_max` would still get text fields and the
  new report would advise a rename the Timeline contract forbids — a hint that
  cannot be acted on is worse than silence.
- **Widen only, no chrome (O3).** Rejected: a wider guess with no explanation
  relocates the confusion. The near-misses that survive §SD5 — type mismatch,
  `Date`, no evaluator — become rarer and so more baffling when hit.
- **Declarative marker only (O4).** Rejected as the primary mechanism: a syntax
  nobody can find unaided does not answer a discoverability question. Retained as
  a deferred complement (§SD8).
- **Keep adjacency as a tiebreak under the wider rule.** Rejected: there is
  nothing to break. §SD1's dedup means two same-stem pairs cannot collide, so the
  tiebreak would only reintroduce the false negatives §SD5 removes.
- **Fold any two DateTime slots, ignoring names.** Rejected: `{created:DateTime}`
  and `{deleted:DateTime}` are two independent filters, not a range. Names are the
  only evidence of intent a placeholder carries; discarding them trades a false
  negative for a false positive that corrupts the query's meaning.

## Consequences

### Positive

- The subsystem acquires a written contract: where a widget goes, what it may
  write, why an unknown type is not an error, and why the registry is ordered.
- The timeline range folds, and the snippet documenting it demonstrates the picker
  rather than two text boxes.
- Slot order in the editor stops mattering, so an unrelated placeholder between
  `from` and `to` no longer silently costs a picker.
- Declined folds explain themselves where they occur, which is where the
  vocabulary is cheapest to learn.
- §SD2's re-dispatch loop acquires the multi-pair case its comment already claims.

### Negative

- A wider guess mis-fires more. Two unrelated DateTime placeholders that happen to
  be `x_min`/`x_max` will fold, and §SD6's escape is all-or-nothing. §SD8's marker
  is the eventual answer; until then the sharp edge is real.
- §SD5's table is a closed vocabulary in a domain that has none. It will attract
  requests to grow, and each one adds mis-fire surface.
- `TestDateTimePairWidgetRejectsNonAdjacent` asserts the behaviour §SD5 removes. It
  must be inverted rather than deleted, so the record shows adjacency was retired
  deliberately and not lost.
- Recording an as-built design freezes some of it by making it citable. §SD2's
  ordering constraint in particular becomes a contract rather than an accident.

### Neutral

- No change to the wire path: the prelude, `encodeParamLiteral`'s buckets, and the
  `param_<name>=` URL channel are untouched.
- No change to `paramWidgetI`, the chain, or the registration order. §SD5 replaces
  one matcher; no widget's `Render` moves.
- Signals are unaffected: §SD5 changes which control writes a `SET`, not what a
  `SET` means.

## Validation

§SD1-4 and §SD6 are as-built and covered by the existing suites; the citations in
each are the claim, and a reviewer disagreeing with one is disagreeing with the
code rather than with a plan.

For §SD5, unit and table-driven over the matcher: `tl_min`/`tl_max` folds;
`a_from`/`a_to` folds; bare `from`/`to` still folds; `to` before `from` folds and
renders lo-then-hi; an interleaved `{x:UInt64}` no longer blocks the fold (the
inverted assertion); mismatched types do not fold; two stems yield two pairs; a
stem with only one half present does not fold; `-- play: ungroup` still forces
text fields (§SD6 unchanged).

For §SD7: the near-miss line appears for `{created:DateTime}` +
`{deleted:DateTime}` and names both slots; it disappears once they are renamed to
one stem; the ungroup case reports the comment rather than a failure.

Live, per the `play` screenshot recipe: launch with the timeline snippet, confirm
the picker renders over `tl_min`/`tl_max`, that filling it authors the prelude,
and that the Timeline's own writes stop landing once it does (§SD4's shadowing).

## Updates

### 2026-07-15 — §SD5 and §SD7 shipped

The stem matcher (`matchRangePair`, `splitRangeSuffix` and the `rangeSuffixes`
table), the fold label, and the near-miss line are in, with the snippet library's
`Time range` entry and the `features.md` restatement §SD7 called for. Live-verified
per §Validation: `tl_min` / `tl_max` folds into the range picker seeded from its
`SET` prelude, and two DateTime slots that share no stem draw the near-miss line.

Three things the decision did not anticipate:

- **The adjacency assertion was pinned twice, not once.** §Status named only
  `TestDateTimePairWidgetRejectsNonAdjacent`; the range widget carried its own
  mirror, `TestDateTimeRangeWidgetRejectsNonAdjacent`. Both were inverted. The
  duplication is the shared matcher working as intended — the two widgets cannot
  drift on what folds — but it means "one pinned assertion" understated the
  retirement by half.
- **The fold label is decided by the orchestrator, not the widget.** §SD7 reads
  as though the pair widget announces its own stand-in status. It cannot without
  learning that an evaluator exists, which would couple it to §SD3 for a label
  and cost the seam its point. `renderFoldLabel` therefore draws the label
  immediately above whichever group widget claimed the pair, which is the same
  thing on screen.
- **The label separates bounds with an en dash, not an arrow.** The host's main
  font carries no `U+2192`, so an arrow renders only via the CJK mono fallback —
  wrong metrics in a proportional label, and tofu if that fallback moves. Noted
  because `play` renders `→` in three older strings (`play_bindings.go:228`,
  `play_projection.go:550`, `play_affordance.go:329`) that lean on the same
  fallback; whether they should is not this ADR's question.

### 2026-07-22 — §SD4 amended (design): the pane writes both tiers; pin/unpin is the migration gesture

A design amendment settled in dialogue (2026-07-22); the slice lands as its
own shipped Update. §SD4 closed with "the pane is thus the instrument that
turns a signal into a constant" — that one-way wiring is what this amendment
retires. The pane keeps its detection, chain, folding, and draft mechanics
unchanged; what changes is where drift goes.

**The gap, by the frictions it manufactures.** Four costs, all one asymmetry —
the store has no typed writer, and the typed writer has no store path:

1. **Filling a picker pins.** Fill the folded `tl_min`/`tl_max` picker and the
   Timeline's own writes are shadowed (ADR-0097 slice-5 D1) — the panel
   co-writer is silently disconnected. §SD8 deferred an *annotation* for this;
   an annotation explains the trap without removing it.
2. **Pinning kills Live.** A fully SET-bound buffer has no unbound slots, so
   the Live toggle vanishes (`hasUnboundSlots`). The `selection_country` fix
   (fab0e1b4) documents the resulting dead end: the only affordance for "give
   this name a value" was the one that breaks the reactive path.
3. **The unfilled-input gate points away from its own fix.** D3 blocks Run on
   an unfilled name while the typed widget for exactly that name sits in the
   pane, unable to fill the store; the user detours to the Graph tab's raw
   text field and loses the typed control (and the fold) on the way.
4. **Two human surfaces, disjoint capabilities.** Params pane: typed, paired,
   buffer-tier only. Signals editor: raw text, store-tier only. Same names,
   same declared types, no shared machinery.

**Decision.** The pane becomes tier-aware; the tier is *derived*, never
stored:

- **SET-presence is the mode bit.** A name with a `SET` in the buffer is
  pinned; a name without one is live. No new persisted state — slice-5 D1
  already defines this bit, the pane just stops forcing one direction.
- **Drift forks by tier.** Phase 3 of the value path
  (`syncParamDriftToPrelude`) routes a pinned name's drift to the prelude
  rebuild exactly as today, and a live name's drift to
  `setSignalRawFrom(name, draft, "param-widget")` — a new writer identity
  beside `signals-editor`, so provenance distinguishes the two human
  surfaces. Widgets are untouched: they keep mutating draft strings
  (`paramWidgetI` is unchanged; the fork is orchestrator-only, the
  `renderFoldLabel` precedent for orchestrator-drawn chrome beside a claim).
- **Pin/unpin is an explicit per-claim gesture.** Pin authors a `SET` from
  the current store value (today's author path); unpin removes the `SET` via
  the prelude rebuild and seeds the store with the same value. It operates on
  the *claim*, so a folded pair migrates as a unit and the pane cannot
  produce a mixed-tier range. A tier migration moves the draft's last-synced
  baseline with it: the frame after an unpin neither re-authors the `SET` nor
  tears the draft.
- **Mixed-tier pairs decline the fold.** A hand-authored half-pinned pair
  (`SET` for one half only) is a §SD5 decline; §SD7's near-miss line grows
  the case and names the halves and their tiers.
- **Live drafts are reseed-guarded.** The slice-5e Signals-editor rule
  applies to the pane: an idle draft follows external writes (a
  Timeline-published extent shows up in the picker), typing wins while the
  store holds still. Co-writing resolves as last-writer-wins with visible
  provenance — the store already dedups identical re-sets, so a settled
  co-writer is quiet.
- **The fill default flips, deliberately.** Typing into a widget whose name
  has no `SET` writes the live tier; it no longer authors a `SET` as a side
  effect. This changes behaviour only for previously-unbound names — exactly
  the cases where author-on-fill manufactures frictions 1–3 — and a buffer
  whose prelude already binds every slot behaves identically. Pinned-tier
  editing (a name with a `SET`) is unchanged.

**What this buys inside the existing model, unpriced:** a live widget write
flips the staleness witness and auto-runs under Live (an ordinary
provenance'd store write); Run resolution and the history snapshot already
carry store values, so reproducibility via D4 is untouched; a folded pair in
live mode emits both halves in one frame, and the frame-snapshot rule makes
the range move atomic for every consumer; and D3's Run-block hint gains its
fill affordance — the empty, highlighted live widget in the pane *is* the
"set parameter {x}" empty-state, typed.

**Rejected:**

- *A tuple-typed single slot for ranges* (`{r:Tuple(DateTime64, DateTime64)}`):
  costs `tupleElement` noise at every SQL reference and kills per-name reuse —
  a bands query reading only `{tl_min}` cannot reference half a tuple. Stem
  pairing already gives the ergonomics at the widget layer; the store stays
  flat name→raw.
- *A defaults/domain side-syntax* (a comment DSL beside the buffer): the
  pinned tier already is the defaults mechanism — author a `SET` to share a
  default, the recipient unpins to go live. A second syntax would be state
  beside the buffer that a paste half-loses.
- *Keeping author-on-fill as the default with an opt-in live mode*: preserves
  the trap as the default and makes the pane's write target ambiguous per
  interaction. The SET-presence rule keeps the mode legible from the buffer
  alone.

**Deferred (with triggers):**

- **Live relative time.** The range picker's evaluator already resolves
  `now() - INTERVAL 1 HOUR` — at the pinned tier that is evaluate-once-at-
  edit. A live-tier expression draft re-evaluated at emit time would give
  rolling windows, but a sliding `now()` is a wall-clock writer feeding the
  auto-run loop — the feedback class ADR-0097's acyclicity guard does not
  cover. Wants deliberate quantization and a Live circuit-breaker witness
  first. Trigger: the first real need for rolling windows under Live.
- **Query-backed value domains** (the dropdown-from-a-node widget): under
  §SD2 this is a registration ahead of the catch-all tail, not a redesign.
  Its own decision when it comes.
- §SD8's **signal-pin annotation** retires here — superseded: the pin state
  becomes the control itself, not a label about a side effect.

**Delivery:** one slice — the drift fork + writer identity, the pin/unpin
gesture with baseline migration, the reseed guard, the mixed-tier decline +
§SD7 line, and the pane's unfilled highlight. Tests: live drift lands in the
store with `param-widget` provenance and no buffer change; pin authors the
`SET` from the store value; unpin removes it, seeds the store, and does not
re-author next frame; an idle live draft follows a Timeline extent write
while a mid-edit draft does not; a folded live pair emits both halves at one
snapshot; a half-pinned pair declines with the named reason; Live reappears
after unpin; an empty live widget blocks Run with the pane hint; a fully
SET-bound buffer is behaviour-identical end to end.

## References

- [ADR-0016](./0016-imzero2-time-range-picker.md) — the range picker and its
  Phase-4 evaluator; `play` is a consumer (§SD3).
- [ADR-0026](./0026-app-runtime-and-capability-subjects.md) — the
  `SetCapabilities` host seam §SD3 hangs off.
- [ADR-0097](./0097-play-reactive-query-graph.md) — signals, param slots as signal
  edges, and the `tl_min` / `tl_max` publication (§SD4).
- `apps/play/help/features.md` §Query parameters — the user-facing statement of
  the widget ladder that §SD7 restates.
