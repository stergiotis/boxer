---
type: adr
status: proposed
date: 2026-07-15
# reviewed-by: "@<handle>"     # fill in and uncomment when flipping to accepted
# reviewed-date: YYYY-MM-DD    # fill in and uncomment when flipping to accepted
---

> **Status: proposed — pre-human-review.** Decision under consideration; do not
> implement as if accepted.

# ADR-0122: `play` kanban result pane — a board from a query result

## Status

Proposed, pre-human-review.

The pane (§SD1-3) is built: `play_kanban_panel.go` with its tests, the Kanban
dock tab and its `BOXER_PLAY_FOCUS_KANBAN` knob, `kanban.Model.SetSelected`, and
two snippet-library entries (the contract, and the `countIf` aggregation that
builds one).
Verified per §Validation — unit suites green, and a live run against a
`values()`-literal board confirmed the lanes, the tallies, the zero-tally skip
and the `@token` colours (the exported SVG carries the three legend swatches as
`#8bd28d` / `#e6b55d` / `#616466`, which are `adrboard`'s three legend tokens
exactly).

The corpus tables (§SD4) are built: `keelson('adr')` / `('subtask')` /
`('coderef')`, with the corpus resolution moved out of `adrboard` into
`adrcorpus` (where it outlives the app) under `BOXER_ADR_DIR` / `BOXER_ADR_ROOT`.
The board now renders in `play` against the in-process keelson endpoint with no
ClickHouse server and no load step. The §SD4 tension stands as recorded and
still deserves review.

## Context

The `adrboard` app (ADR-0092) renders the ADR corpus as a read-only kanban
board: lanes are the frontmatter `status`, cards are ADRs, and each card carries
a packed tally of sub-item dots. It computes that board in Go —
`adrcorpus.ParseDir` into `[]Adr`, then `buildBoard` folds the corpus into a
`kanban.Model` (`apps/adrboard/board.go`).

Separately, `boxer adr` (`public/app/commands/adr`) already dumps the same
corpus to three Arrow files and binds them as the tables `adr`, `subtask` and
`coderef`, then runs canned SQL over them through `clickhouse-local`
(`query.go:83-89`). Its `overviewQueries` set already contains board-shaped
aggregations — the two-axis status × evidence board, and a per-`kind` sub-item
tally that is `cardDots` in all but the `GROUP BY` key.

So the corpus is *already* queryable, and the board is *already* expressible as
an aggregation. What is missing is a `play` panel that renders a result set as a
board, which would let the same corpus reach the same picture by a second,
declarative route.

That second route is worth having for its own sake. The two implementations are
a natural cross-check on each other, and the exercise makes an asymmetry visible
that neither route states on its own (§SD2).

Substrate facts that shape the design:

- `play` renders results through the ADR-0097 channel negotiation: a `PanelI`
  declares typed input channels, accepts or rejects the observed node's Arrow
  schema, and renders the claimed result. Panels are generic over schema — the
  World pane (ADR-0114) knows nothing about any dataset, only that it needs a
  column that country-resolves.
- The `kanban` widget is immediate-mode and host-owned: `NewModel(columns,
  cards)` retains the slices, `Render(Input{...})` draws, `RenderLegend` is a
  separate call. It caps a card at **3 dot kinds** and silently truncates beyond
  that (`render.go:366`); a `DotTally` whose ID is absent from `DotLegend` is
  silently skipped (`render.go:377`).
- Card widget IDs are scoped by card ID, so duplicate card IDs make every widget
  inside the second card warn. `adrboard` keys cards positionally for exactly
  this reason (`board.go:88`) — ADR numbers are not unique in the corpus.
- ADR-0116 resolves a **leeway column handle** typed as `section:column`.
  `splitHandle` claims any identifier with exactly one colon
  (`lwsql.go:316`). ADR-0121 adds `cond_N` result columns. Both are conventions
  over result column names, and a third convention has to not collide with them.

## Decision

A new `play` result pane ("Kanban" dock tab) that renders any result carrying a
lane column and a title column as a `kanban` board, reading its lane vocabulary
from an optional `lanes` CTE of the same query (§SD6), plus three `keelson`
introspection tables that put the ADR corpus in SQL reach so the pane has a
worked example. The `kanban` widget gains one method (`Model.SetSelected`,
§SD3) and nothing else.

### SD1 — Panel contract: named columns, not detection

The pane is a `PanelI` observer of the active node: one required channel
(`main`, the cards) and one optional (`lanes`, §SD6). It claims a `main` schema
carrying, **by name**:

| column | type | required | meaning |
| --- | --- | --- | --- |
| `lane` | any | yes | the lane title this card sits in |
| `title` | any | yes | the card title |
| `subtitle` | any | no | the card's second line |
| `dot_*` | integer | no | a dot tally (§SD2) |

Detection was rejected. The World pane can detect its country column because
country-resolution is a strong signal; nothing distinguishes a lane column from
a title column but intent, and guessing between two string columns is a coin
flip. Named columns cost one `AS` per query — `SELECT status AS lane` — and say
what they mean. This follows the Map pane, which likewise requires
`mercator_x` / `mercator_y` by name rather than guessing.

The three text columns carry **no type requirement**. They are read through
`formatCell`, which is total over Arrow types, so a type check would reject
queries that would have rendered correctly — `SELECT num AS lane` is a board
with numbered lanes. Dot columns are the exception (§SD2): they are counts, and
a fractional or textual tally is not a weaker board but a meaningless one.

Lane **order** is first-seen row order, so the query's `ORDER BY` controls the
board's left-to-right layout with no second mechanism. Lane identity is
positional; so is card identity (row index + 1, non-zero because the widget
reads `ParentID == 0` as "no parent"). A result set has no guaranteed unique
key, and the widget's ID scoping punishes collisions — the same reasoning that
made `adrboard` positional.

The fold is **capped at 2000 cards**. A board is a tens-to-hundreds instrument;
without a cap, naming `lane` and `title` over a large table would lay out every
row. The excess is dropped and counted in the status line — not silently, and
not by rejecting, since a bounded look at a big table is a reasonable thing to
want on the way to a `GROUP BY`.

### SD2 — Dot contract: `dot_<label>` and `dot_<label>@<token>`

Any integer column named `dot_*` is a dot kind. Column order is dot order. The
legend label is the `<label>` part, so `dot_cited` legends as "cited".

Colour comes from the design system, named **in band**:

```sql
countIf(done)                       AS `dot_done@success`,
countIf(NOT done AND code_refs > 0) AS `dot_cited@warning`,
countIf(NOT done AND code_refs = 0) AS `dot_todo@disabled`
```

Without an `@<token>` suffix the colour falls back to the semantic ramp by
column position. An unknown token is rejected in `AcceptForChannel` rather than
defaulted, so a typo surfaces as a message instead of a wrong colour.

**On the separator.** `:` is taken: ADR-0116's `splitHandle` claims any
identifier with exactly one colon as a `section:column` handle, so
`dot_done:success` would be read as section `dot_done`, column `success`, and
sent to the leeway resolver. `_` is taken twice over — it is already this
convention's own prefix separator, and it is leeway's mangled-physical-name
separator (`detectSeparator`, `lwsql.go:331`).

The separator needs backtick-quoting in ClickHouse. That cost is already paid
elsewhere in `play`, where leeway physical names (`tv:symbol:value:…`) are typed
verbatim and ride through the pipeline quoted — and it is the *unquoted*
behaviour that picked `@` out of the candidates. Measured against
`clickhouse-local`, `@`, `~`, `/`, `!` and `?` all parse quoted and raise a
syntax error unquoted, so a forgotten backtick is caught at once. `#` does not:
it is a ClickHouse line-comment introducer, so `AS dot_done#success` silently
yields a column named `dot_done` followed by a comment — the board would draw
with a positional colour and no diagnostic. A convention whose typo mode is a
wrong-but-plausible board is not worth the mnemonic. Of the loud remainder `@`
reads as *at/as*, where `/` reads as a fraction and `!`/`?` carry the wrong
connotation. All candidates, `#` included, survive the nanopass lexer quoted
(verified against `nanopass.Parse`), so the grammar does not constrain the
choice — only ClickHouse does.

Zero tallies are not rendered — an ADR with no sub-items carries no dots, as on
the Go board. More than three `dot_*` columns is a **rejection**, not a
truncation: the widget would silently drop the fourth (`render.go:366`), and a
board that quietly omits a bucket is worse than one that says why it will not
draw.

**The asymmetry this exercise makes visible.** In Go, `cardDots` is a
first-match switch, and "an author's ✓ outranks code evidence" is implicit in
the case order:

```go
case s.Done:         done++
case s.CodeRefs > 0: cited++
default:             unknown++
```

SQL has no case-order precedence to inherit, so the same rule has to be written
out — `countIf(NOT done AND code_refs > 0)`. The Go version's precedence rule is
invisible in the code that implements it; the SQL cannot express the buckets
without stating it. Neither form is more correct, but the declarative one is the
one that cannot leave the rule unsaid.

### SD3 — Read-only, and the selection carries both ways

`ReadOnly: true`; `DrainMoves` stays unused. A `play` result is a query output —
there is nothing to write a dragged card back to, and inventing a
move-to-`UPDATE` path would make a query playground into a mutation tool on the
strength of a drag gesture. Clicking a card emits the `selection` signal for its
row, the same viewof duality the Table and World panes implement, so Detail and
Table follow the click. `dispatchPanel` stamps `selection_node` / `selection_id`
and provenance; the pane does not.

Unlike the World pane, the board also **follows** the signal, which needs one
new widget method: `Model.SetSelected`, the write side of the existing
`Model.Selected`. The pane reads the shared cursor before `Render` and reads the
user's own click back after. Emit-only was the smaller change and is wrong here:
the World pane has no selected state to contradict, but a board *paints* its
selection, so a cursor moved in Table would leave the board highlighting a card
nothing else agreed was current — showing a selection that is no longer true.
The method is three lines and a host that never calls it keeps the widget's
existing behaviour.

### SD4 — The corpus in SQL: three `keelson` tables

`keelson('adr')`, `keelson('subtask')` and `keelson('coderef')` expose
`adrcorpus` through the ADR-0094 introspection registry, carrying the **same
schemas and the same table names** `boxer adr` already binds
(`arrowemit.go:18-66`). That symmetry is the point: a query written against one
runs verbatim against the other, and `overviewQueries` becomes a set of `play`
snippets for free. A test pins the two schema sets equal so they cannot drift.

Freshness is **Live**: the corpus is files on disk that change under a running
process, which is why `adrboard` has a Reload button at all. A `Static` table
would go stale the first time an ADR is edited, silently.

Live is qualified by a short shared-read window (`adrcorpus.LoadWindow`), for
consistency before cost. A query joining `adr` and `subtask` reads the corpus
twice, and a read is slow enough that an edit landing between them would produce
a **torn join** — two tables describing different repositories, with no error to
show for it. One read per window makes them one snapshot; halving the cost is
the side effect.

The cost is not where it looks. Measured on this corpus, parsing dominates at
~380 ms for ~120 files while the whole-tree citation scan is ~86 ms — the
opposite of what `adrboard`'s own comment implies. That also kills the obvious
alternative: an mtime key over the scanned tree costs ~340 ms to compute, about
as much as the work it would skip. A board query lands at ~640 ms end to end,
which is the same order as `adrboard`'s Reload and is paid per Run, not per
frame.

Where no corpus resolves — a shipped binary running off-repo — the tables are
**empty rather than erroring**, following `keelson('build')` with no `runinfo`.

**Known tension: this crosses the line the other `keelson` tables hold.**
[ADR-0094](./0094-keelson-introspection-tables.md) founds the family on state
that "exists *only inside a running process*" — the env, the apps, the build,
the registered passes — not on what the repository contains. `keelson('adr')`
answers the second question, and it is the first provider to do filesystem I/O
at query time, which means its rows depend on where the process was started
rather than on what the process is. That was
accepted deliberately: the alternative (§Alternatives) puts a ClickHouse server
and a load step between a user and a board of the repo's own decisions, for a
dataset the binary can already parse in milliseconds. The mitigations are that
it never lies (Live freshness), it degrades quietly off-repo (empty, not an
error), and its root is pinned by an explicit env var rather than discovered
silently. If review finds the tension decisive, §SD4 can split into its own ADR
without touching §SD1-3 — the pane needs *a* table, not *this* table.

### SD6 — The lane inventory: a `lanes` CTE, not a second editor

A board query may carry a top-level CTE named `lanes`. Its rows, in order, are
the board's lane inventory: each becomes a lane whether or not a card sits in
it, and a lane a card carries that the inventory does not name is **appended**
after them. That is `adrboard`'s `buildColumns` exactly — canonical list first,
unknown appended — with the canonical list supplied by a query instead of a Go
slice. Without the CTE, lanes are read off the rows as before.

```sql
WITH lanes AS (SELECT arrayJoin(['proposed', 'accepted', 'withdrawn']) AS lane)
SELECT status AS lane, … FROM adr …
```

This is what an empty lane costs. Lanes-off-the-rows cannot express one — a lane
is read from a row and a row *is* a card — so "nothing is withdrawn" was
unsayable, which §Consequences recorded as the one structural gap against the Go
board.

**Why a CTE rather than a second editor.** The Timeline's `chBands` is the
precedent for an optional second channel, and it pays for a whole apparatus: a
second SQL buffer, its own editor box, a persist key with a Restore/Persist pair
and a manifest entry, and an env knob. That is the right shape for bands, which
are a *different subject* from the events they shade — a different table, often
a different time grain. A board and its lane vocabulary are one thought, and
splitting them across two buffers would make them two things to keep in sync,
with a board that silently loses its lanes when only one is re-run.

The split graph (ADR-0097) already turns every top-level CTE into a node, and
`PlayApp` already holds the last Run's `currentSplit`. So the inventory needs no
new buffer, no persistence and no knob: the panel looks for a node named `lanes`
and demands it on its own lane. The node also shows up in the Graph tab like any
other, for free — an inventory is inspectable and observable without the panel
teaching the Graph view anything.

An unused CTE is legal SQL and rides through the fused sink untouched
(ClickHouse ignores it), so declaring lanes costs the board query nothing.
`lanes` is a plausible name for an unrelated CTE, and a collision degrades
quietly: a `lanes` node without a `lane` column rejects the optional channel and
the board falls back to row-derived lanes. A lanes node that *fails*, though,
does not fall back silently — the error rides the status line, since a board
quietly missing its declared lanes looks like a working board.

### SD5 — Deferred

The parent axis (`ParentID`, `GroupByParent`, `GroupByField`), `Column.IsDone`
(inert without parents — it exists to roll a child up into a done lane), lane
colour/accent, move-to-`UPDATE`, and caching the corpus scan behind an mtime
check. None of them changes the contract above. `Column.IsDone` and the parent
axis are one deferral, not two: the widget only reads `IsDone` for the rollup
the parent axis would introduce.

## Alternatives

- **A `demo/adr/` dataset loaded into a ClickHouse server** (the `demo/adsb`
  pattern) — the on-model answer, and rejected on ceremony: it needs a running
  server, a `setup.sql`, and a load step re-run after every ADR edit, to show a
  board of decisions the binary can parse in milliseconds. Kept in reserve; it
  is what §SD4 becomes if the keelson tension is judged decisive.
- **Server-side `file()` over the Arrow dumps** — reuses the existing dump
  verbatim with no ingest, but needs server filesystem access and a path policy
  that `RunQuery` sidesteps today by setting the working directory and using
  basenames. Rejected: a policy question in exchange for a saved `INSERT`.
- **Positional dot colours only** (no `@<token>`) — simplest contract, no
  separator question at all, and rejected because colour would be a property of
  where a column sits in the `SELECT`. Reordering a projection for readability
  would silently recolour the board.
- **`dot_<label>__<token>`** — a plain identifier needing no backticks, and safe
  from the leeway resolver (no colon, so `splitHandle` declines it). Rejected
  for reading ambiguously: in `dot_all_done__success` the label boundary is
  guesswork for a reader, where `@` is unmistakable.
- **`dot_<label>#<token>`** — the first choice, for the CSS-hex mnemonic.
  Rejected on measurement: `#` opens a ClickHouse line comment, so the unquoted
  typo is silent rather than loud (§SD2).
- **An ADR-specific board pane** — hard-wire the corpus and skip the contract.
  Rejected: every other `play` pane is generic over schema, and a pane that
  renders one dataset is an app, which is what `adrboard` already is.
- **A second `legend` channel** carrying `(kind, label, colour)` rows (the
  `chBands` pattern) — fully explicit and order-independent, but makes the
  simplest possible board a two-query affair. Reconsider if the `#<token>`
  vocabulary proves too narrow.

## Consequences

- `play` gains a fourteenth dock tab and a third result-column convention, after
  leeway handles (ADR-0116) and `cond_N` (ADR-0121). The three do not overlap by
  construction (§SD2), but the space of unclaimed column-name syntax is now
  visibly finite, and a fourth convention should probably prompt a shared
  registry rather than a fourth ad-hoc grammar.
- `adrboard` and the SQL board can disagree. That is the point — they are a
  cross-check — but it does mean two implementations of one picture, and a
  change to `cardDots` semantics now has a second site to update. The shared
  schemas (§SD4) keep the *inputs* from drifting; nothing keeps the *folds* in
  step but the corpus test.
- **The fold ports exactly; the editorial does not.** Run against the real
  corpus, the SQL board agrees with `buildBoard` on every card — lane, title,
  subtitle and all three tallies (§Validation). What the Go board does that a
  result set cannot:
  - **An empty lane** — the one structural gap, and the reason §SD6 exists. A
    lane read off the rows cannot have no cards. The `lanes` CTE closes it: the
    inventory is a query, so a lane may be declared and stay empty.
  - **Legend prose.** `dotLegend` gives each kind a sentence about what the
    colour claims ("a ✓ is the only claim of completion"). A column name carries
    a label, not a paragraph, so the legend names the column instead.
  - **A corpus headline.** `corpusSummary`'s one-liner has no slot in the
    contract; the status line counts cards and lanes.
  The two that remain are editorial — prose about what the board *means* — and
  a result set is not a place to put prose. That is the honest answer to "can
  the query do everything the app does": the arithmetic yes, the vocabulary yes
  (§SD6), the commentary no.
- The `keelson` table family stops being uniformly about the running process
  (§SD4), and that boundary is now a judgement call rather than a rule.
- The board is capped at three dot kinds by the widget, with no headroom.
- **The pane is bigger than the app it generalises, and its win is amortized,
  not immediate.** Measured against `adrboard` — which renders the same board
  from the same widget, so the widget cancels out:

  | | `adrboard` | Kanban pane |
  | --- | --- | --- |
  | non-test Go | 486 | 611 |
  | tests | 230 | 428 |
  | renders | one corpus | any result |
  | the next board costs | ~486 more Go | ~25 lines of SQL |

  The pane is **~26% more code**, not less. But the fold — the part that
  actually computes the board — is a wash: 114 lines against `board.go`'s 96,
  and the pane's does more (declared lanes, the card cap). The whole difference
  is contract (168) and host plumbing (150): the price of *not knowing your
  data*, and of living somewhere async and negotiated rather than calling
  `ParseDir` inline. `adrboard` hardcodes what the pane must be told — its dot
  vocabulary is 27 lines of Go where the pane needs a token grammar, a parser
  and six reject paths.

  Two things the line counts do not show, both favouring the app for a *single*
  board: `adrboard` is synchronous and self-contained (launch it and there is a
  board — no server, no load step), and it can be read top to bottom, where the
  pane has a claim, a fold cache whose invalidation is load-bearing (it holds
  the selection), an async lane and a channel negotiation. And one favouring the
  pane, which no count captures: changing what `adrboard` shows means editing Go
  and relaunching; changing the pane's board means editing SQL and pressing Run.

## Validation

- Unit (done): `AcceptForChannel` over synthetic schemas — missing `lane`,
  missing `title`, no `dot_*` (accepted, dotless board), four `dot_*` columns
  (rejected, §SD2), float tally, unknown `@` token, empty `@`, unlabelled
  `dot_`, and `@`-suffixed / bare `dot_*` mixed. Card and lane positional
  identity, first-seen lane order, the zero-tally skip, the 2000-card cap and
  its status line, the selection claim, and the fold cache — which is asserted
  to *preserve* a selection, since rebuilding the Model would clear it.
- Unit (done, §SD6): the optional `lanes` channel and its `lane` claim; the
  inventory read in row order and de-duplicated; **a declared lane with no cards
  renders**; an undeclared lane is appended, not dropped; the inventory keys the
  fold cache (it is an input that changes without the result changing); and a
  failed lanes query reaches the status line rather than silently reverting the
  board to row-derived lanes.
- Unit (done, §SD4): the `keelson('adr')` / `('subtask')` / `('coderef')` schemas
  equal `arrowemit.go`'s field for field; the **cells** equal it too, on
  identical input with distinct same-typed values — a schema check alone would
  pass a `code_files`/`code_pkgs` swap and misreport the repository forever. Plus:
  the tables are Live; they are empty rather than erroring off-repo and on an
  unresolvable `BOXER_ADR_DIR`; and all three register under the names
  `boxer adr` binds.
- Live (done, §SD4): the board renders in `play` against the **in-process
  keelson endpoint** — no ClickHouse server, no `boxer adr build`, no load step.
- Isomorphism (**done once by hand; not yet a test**): `buildBoard` dumped as
  rows and diffed against the snippet-library query over the same corpus
  snapshot, loaded from `boxer adr build`'s Arrow into a server. Every card
  agrees on lane, title, subtitle and all three tallies, and the lane inventory
  matches with its counts. Two traps it surfaced: the comparison must not run
  through TabSeparated, which escapes `'` and `\` in a title and reads as a
  false difference; and the two sides must be dumped at the same moment, since
  a first attempt disagreed on one ADR purely because a concurrent edit added a
  citation between the dump and the fold — the corpus moved under the read,
  which is the case for §SD4's Live freshness stated as evidence.

  This wants to be a standing test rather than a one-off. It cannot be, until a
  corpus table exists that a test can reach without a server (§SD4) — that is
  what §SD4 buys, beyond convenience.
- Integration (done): a scripted capture against a `values()` literal board —
  verifiable with no corpus and no ADR table — confirming the lanes and their
  counts, the per-card tallies against the source rows, the zero-tally skip, and
  the legend colours by their hex. Plus a live drive (egui inspection) through
  the whole loop: the snippet's **Replace** into the editor, Run, and a card
  click moving Detail to that row (row 3 of 8, `ADR-0114`) — the viewof duality
  in both directions.
