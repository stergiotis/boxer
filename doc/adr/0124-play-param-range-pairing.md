---
type: adr
status: proposed
date: 2026-07-15
# reviewed-by: "@<handle>"     # fill in and uncomment when flipping to accepted
# reviewed-date: YYYY-MM-DD    # fill in and uncomment when flipping to accepted
---

> **Status: proposed — pre-human-review.** Decision under consideration; do not
> implement as if accepted.

# ADR-0124: `play` parameter ranges — pair by name stem, and say when it did not

## Status

Proposed, pre-human-review. Nothing here is built.

The behaviour this ADR changes is live and pinned by tests: the from/to matcher
(`apps/play/play_param_widget.go:143`) and its adjacency assertion
(`apps/play/play_param_widget_test.go:25`). §SD1 removes that assertion, so the
decision should be reviewed before the test is touched.

## Context

`play` renders one widget per `{name : Type}` placeholder in the SQL editor. The
placeholder is a grammar production, not a regex match — `paramSlot` in
`grammar1/ClickHouseParserGrammar1.g4:276` — and `collectParamSlots`
(`apps/play/play_param_slots.go:72`) walks the CST into a slot list, **deduped by
name**, first occurrence's type winning. The renderer offers that list to a
chain of widgets in registration order (`apps/play/play_renderer.go:616`); each
claims what it can, and the tail `scalarTextWidget` claims whatever is left.

Exactly one widget in that chain reacts to a *combination* of placeholders, and
its rule is twelve lines:

```go
// matchAdjacentFromToDateTime — apps/play/play_param_widget.go:143
for i := 0; i+1 < len(slots); i++ {
    a, b := slots[i], slots[i+1]
    if !strings.EqualFold(a.Name, "from") || !strings.EqualFold(b.Name, "to") { continue }
    if !isDateTimeType(a.Type) || !isDateTimeType(b.Type) { continue }
    return []int{i, i + 1}, true
}
```

Two DateTime placeholders fold into one range control — the Grafana-style picker
from [ADR-0016](./0016-imzero2-time-range-picker.md) when the host wired an
evaluator, two calendar buttons otherwise. Everything else in the query, of every
other type, gets an independent text field. There is no type whitelist and no
error path: an unrecognised shape is not reported, it is simply a `TextEdit`.

The rule has five ways to not fire, and all five degrade to text fields with no
explanation:

- a slot between the two breaks adjacency — `{from:DateTime64} … {a:UInt64} …
  {to:DateTime64}` folds nothing;
- the names must be `from` and `to`;
- `to` before `from` does not match;
- `Date` is not `DateTime` (`isDateTimeType`, `play_param_inject.go:160`);
- with no evaluator wired the picker silently becomes two calendar buttons.

Two facts turn this from a rough edge into a decision worth recording.

**Adjacency is vestigial.** The re-dispatch loop that offers a widget repeated
matches justifies itself with "two from/to pairs in one query"
(`play_param_render.go:81-84`). Dedup by name forecloses that: there can never be
two slots named `from`. Adjacency guards a collision the parser cannot produce,
so it only generates false negatives — and one of them is pinned as
`TestDateTimePairWidgetRejectsNonAdjacent`, which uses `{x:UInt64}` as the
spoiler.

**The vocabulary excludes our own range.** The timeline snippet
(`apps/play/help/snippets.md:139-140`) binds `{tl_min:DateTime64(3, 'UTC')}` and
`{tl_max:DateTime64(3, 'UTC')}` — a textbook range that gets two text fields. It
cannot be renamed to `from`/`to`: those names are the contract the Timeline panel
publishes as signals ([ADR-0097](./0097-play-reactive-query-graph.md), and
`apps/play/help/features.md:84`). The matcher's vocabulary structurally excludes
the app's own flagship range, and no rename can reconcile them.

The feature is documented (`apps/play/help/features.md:60-71`), but the Snippets
tab — where a user actually meets the app's vocabulary — carries no range
example at all.

## Design space (QOC)

**Question.** How should `play` decide that two DateTime placeholders are one
range, and how should a user find out that ranges exist?

**Options.**

- **O1** — Widen and explain: pair by name stem over a closed suffix table, drop
  adjacency, and hint in the pane when a fold did not happen.
- **O2** — Explain only: keep the from/to adjacency rule, add the hints and a
  snippet.
- **O3** — Widen only: fix the matcher, add no chrome.
- **O4** — Declarative marker: infer nothing; add `-- play: range <lo> <hi>` in
  the vocabulary of the existing `-- play: ungroup`.

**Criteria.**

- **C1** — Covers the in-repo range vocabulary; assessed by whether
  `tl_min`/`tl_max` folds without violating the Timeline signal contract.
- **C2** — Explains a non-fold; assessed by whether a user seeing text fields can
  tell why from the pane alone.
- **C3** — Mis-fire cost; assessed by what an unwanted fold costs and whether an
  escape hatch exists.
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
`tl_min`/`tl_max`, so its hint would permanently advise a rename the Timeline
contract forbids. O4 scores `−−` on C4 for the reason it scores well elsewhere —
a syntax with no inference behind it is only found by people who already know it.

## Decision

We will pair range placeholders by **name stem over a closed suffix table**,
drop the adjacency requirement (§SD1), report in the PARAMETERS pane when
DateTime slots did **not** fold and why (§SD2), and add a range snippet to the
library (§SD3). §SD4 records the signal-shadowing semantic this reaches; §SD5
records what is deferred.

The widget chain, the `paramWidgetI` contract, the `SET param_*` prelude and the
URL wire path are unchanged. Only the matcher and the pane's chrome move.

### SD1 — Pair by stem and suffix, not by position

A slot name decomposes into a stem and a suffix: it either equals a suffix
(stem is empty) or ends with `_` followed by one. Two slots pair when their stems
are equal, their suffixes are the two halves of one table entry, and both types
are DateTime or DateTime64 after `Nullable` unwrap.

The table is closed and ordered:

| lo | hi |
|----|-----|
| `from` | `to` |
| `min` | `max` |
| `start` | `end` |
| `lo` | `hi` |
| `since` | `until` |

`first`/`last` is excluded: it names an ordering, not a bound. `t0`/`t1` is
excluded: a digit suffix needs a different decomposition rule and earns little.

Position is not consulted. Because `collectParamSlots` dedupes by name, at most
one slot can carry a given (stem, suffix), so a stem admits at most one pair per
table entry and no tiebreak is needed. `{to:…}` written before `{from:…}` folds,
and the widget renders lo-then-hi regardless of editor order. A query with two
stems — `a_from`/`a_to` and `b_from`/`b_to` — yields two pairs, which is the
first real use the re-dispatch loop has ever had.

`isDateTimeType` is unchanged, so the fold stays DateTime-only. `-- play:
ungroup` is unchanged and still disables every group widget.

### SD2 — Say when a pair did not fold

The matcher knows why it declined; today it returns a bare `false` and throws
that away. It will instead surface the near-miss, in the register the Signals
chrome already uses for `⚠ type conflict across nodes`
(`play_graph_view.go:199-207`): one weak, small line, never an error, never a
modal.

After dispatch, the pane reports on the slots no widget claimed:

- two or more unclaimed DateTime slots, no stem match — name the slots and the
  vocabulary that would fold them;
- a stem that pairs but whose halves disagree on type — name both types, since
  this is the one case where the user's intent is unambiguous and the fix is a
  one-word edit;
- `-- play: ungroup` in effect — say so, because otherwise the comment's effect
  is indistinguishable from a matcher that failed.

A pair that *did* fold gets a label naming the fold and its escape hatch, so the
inference is legible and reversible rather than magic. The pair widget
additionally says when it is standing in for the picker because no evaluator was
wired — two visibly different controls for one query shape is exactly the
surprise this ADR exists to remove.

The hint is advisory text. It does not gate execution, and a query that ignores
it behaves as it does today.

### SD3 — A range in the snippet library

The Snippets tab is where the vocabulary is met, so the vocabulary goes there: a
`## Time range` entry whose insertion produces a picker. The timeline snippet's
prose gains a line noting that its `tl_min`/`tl_max` now fold, and
`features.md:60-71` is restated in terms of the stem rule rather than "an
adjacent `{from:…}` + `{to:…}` pair".

### SD4 — Filling a folded picker pins a signal, and that is not new

Once §SD1 lands, `tl_min`/`tl_max` get a picker. Filling it authors `SET
param_tl_min = …`, and a SET-bound constant shadows the same-named signal
(`play_client.go:384`) — so the Timeline stops driving the range the moment the
user touches the control.

That is the correct semantic and §SD1 does not invent it. It is the
constant-versus-signal distinction those names already lived under
(`features.md:49-58`): the text field those slots have *today* authors the same
SET and shadows the same signal. §SD1 changes which control does it, not what it
means.

What §SD1 does change is how reachable the gesture is, and a picker invites a
click in a way a blank text field does not. Annotating a folded pair whose name
is a live signal is deferred (§SD5), not dismissed — the knowledge exists
(`play_signals_chrome.go` already distinguishes `pinned by SET` from `unfilled
input`), but plumbing it across panes is a scope this ADR does not need to open
to be useful.

### SD5 — Deferred

- **`Date` / `Date32` folding.** Both pickers are DateTime-bound; a date-only
  range wants a different control, not a wider predicate.
- **Numeric ranges.** `{a_min:UInt64}` / `{a_max:UInt64}` is the same stem rule
  with no widget behind it. A two-ended slider would need a domain, which the
  placeholder does not carry.
- **The signal-pin annotation** in the PARAMETERS pane (§SD4).
- **`-- play: range <lo> <hi>`** (O4) as a complement for names outside the
  table. Worth having once the table's limits are known from use; premature
  before that.

## Alternatives

- **Explain only, keep from/to adjacency (O2).** Rejected: it preserves the
  blind spot that motivates the ADR. `tl_min`/`tl_max` would still get text
  fields, and the new hint would advise a rename that the Timeline signal
  contract forbids — a hint that cannot be acted on is worse than silence.
- **Widen only, no chrome (O3).** Rejected: a wider guess with no explanation
  relocates the confusion instead of removing it. The near-misses that survive
  §SD1 — type mismatch, `Date`, no evaluator — become rarer and therefore more
  baffling when hit.
- **Declarative marker only (O4).** Rejected as the primary mechanism: a syntax
  nobody can find unaided does not answer a discoverability question. Retained as
  a deferred complement (§SD5).
- **Keep adjacency as a tiebreak under the wider rule.** Rejected: there is
  nothing to break. Dedup by name means at most one slot per name, so two
  same-stem pairs cannot collide, and the tiebreak would only reintroduce the
  false negatives §SD1 removes.
- **Fold any two DateTime slots, ignoring names.** Rejected: `{created:DateTime}`
  and `{deleted:DateTime}` are two independent filters, not a range. Names are
  the only evidence of intent the placeholder carries, and discarding them trades
  a false negative for a false positive that corrupts the query's meaning.

## Consequences

### Positive

- The app's own timeline range folds, and the snippet that documents it
  demonstrates the picker instead of two text boxes.
- Slot order in the editor stops mattering, so an unrelated placeholder between
  `from` and `to` no longer silently costs the user their picker.
- Near-misses explain themselves where they occur, which is also where the
  vocabulary is cheapest to learn.
- The re-dispatch loop in `renderParamSlots` acquires the multi-pair case its
  comment already claims.

### Negative

- A wider guess mis-fires more. Two unrelated DateTime placeholders that happen
  to be `x_min`/`x_max` will fold, and the only escape is `-- play: ungroup`,
  which is all-or-nothing — it disables every group widget in the query, not the
  offending pair. This is a real sharp edge and §SD5's marker is its eventual
  answer.
- The suffix table is a closed vocabulary in a domain with no closed vocabulary.
  It will attract requests to grow, and each one costs a mis-fire surface.
- `TestDateTimePairWidgetRejectsNonAdjacent` asserts the behaviour §SD1 removes.
  It must be inverted rather than deleted, so the record shows the adjacency
  rule was retired deliberately and not lost.

### Neutral

- No change to the wire path: the `SET param_*` prelude, `encodeParamLiteral`'s
  quoting buckets, and the `?param_<name>=` URL channel are untouched.
- No change to `paramWidgetI`, the widget chain, or the registration order. The
  range and pair widgets keep sharing one matcher, so they stay in lockstep on
  what counts as foldable.
- Signals are unaffected: §SD1 changes which control writes a SET, not what a
  SET means.

## Validation

Unit, table-driven over the matcher: `tl_min`/`tl_max` folds; `a_from`/`a_to`
folds; bare `from`/`to` still folds; `to` before `from` folds and renders
lo-then-hi; an interleaved `{x:UInt64}` no longer blocks the fold (the inverted
assertion); mismatched types do not fold; two stems yield two pairs; a stem with
only one half present does not fold.

Chrome: the near-miss line appears for `{created:DateTime}` + `{deleted:DateTime}`
and names both slots; it does not appear once they are renamed to one stem; the
ungroup case reports the comment rather than a failure.

Live, per the `play` screenshot recipe: launch with the timeline snippet and
confirm the picker renders over `tl_min`/`tl_max`, that filling it authors the
prelude, and that the Timeline's own writes stop landing once it does (§SD4).

## References

- [ADR-0016](./0016-imzero2-time-range-picker.md) — the range picker and its
  Phase-4 evaluator; `play` is a consumer.
- [ADR-0097](./0097-play-reactive-query-graph.md) — signals, param slots as
  signal edges, and the `tl_min` / `tl_max` publication.
- `apps/play/help/features.md` §Query parameters — the user-facing statement of
  the widget ladder that §SD3 restates.
