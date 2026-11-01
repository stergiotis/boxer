---
type: adr
status: accepted
date: 2026-07-15
reviewed-by: "@spx"
reviewed-date: 2026-07-15
---

> **Status: accepted (2026-07-15).** Built and committed — see the Status section.

# ADR-0122: `play` kanban result pane — a board from a query result

## Status

Accepted 2026-07-15, same-day as proposed. The design dialogue settled the
column contract (§SD1) and the dot separator (§SD2) before implementation; §SD4
and §SD6 were decided during it, and the app this generalises was removed on the
strength of it (ADR-0092's 2026-07-15 update).

Built and committed:

- **The pane** (§SD1-3): `play_kanban_panel.go` with its tests, the Kanban dock
  tab and its `BOXER_PLAY_FOCUS_KANBAN` knob, `kanban.Model.SetSelected`, and
  three snippet-library entries.
- **The lane inventory** (§SD6): the `lanes` CTE, which closed the one thing the
  Go board could do that a result set could not.
- **The corpus tables** (§SD4): `keelson('adr')` / `('subtask')` / `('coderef')`,
  with the corpus resolution moved out of `adrboard` into `adrcorpus` — where it
  outlives the app — under `BOXER_ADR_DIR` / `BOXER_ADR_ROOT`.

Verified per §Validation. The ADR board renders against the in-process keelson
endpoint with no ClickHouse server and no load step, which is what made removing
`adrboard` a consolidation rather than a regression — before §SD4 it was the only
route to the board that needed no server.

**What acceptance is accepting.** Two of these are worth naming, because neither
is comfortable. §SD4 crosses the line ADR-0094 draws for the `keelson` family:
those tables describe the running process, and these three describe the
repository, reading the filesystem at query time. And the pane is *larger* than
the app it replaced (§Consequences) — it pays for itself from the second board
on, not this one. Both were accepted with the measurements in hand rather than
in spite of them.

## Context

*Written while `adrboard` still existed; it was removed as a consequence of this
decision, so the present tense below is the situation at the time.*

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
that neither route states on its own (§SD2). (In the event the second route
replaced the first rather than sitting beside it — see §Consequences.)

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
introspection tables that put the ADR corpus in SQL reach (§SD4). The `kanban`
widget gains one method (`Model.SetSelected`, §SD3) and nothing else.

The tables began as a way to give the pane a worked example. They ended up
carrying more weight than that: with `adrboard` gone they are the only route to
the ADR board, so §SD4 is load-bearing rather than illustrative, and the
tension it records is a live one.

### SD1 — Panel contract: named columns, not detection ✓

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

### SD2 — Dot contract: `dot_<label>` and `dot_<label>@<token>` ✓

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

### SD3 — Read-only, and the selection carries both ways ✓

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

### SD4 — The corpus in SQL: three `keelson` tables ✓

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

### SD6 — The lane inventory: a `lanes` CTE, not a second editor ✓

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
colour/accent, and move-to-`UPDATE`. None of them changes the contract above.
`Column.IsDone` and the parent axis are one deferral, not two: the widget only
reads `IsDone` for the rollup the parent axis would introduce.

**Not deferred — ruled out.** An earlier draft deferred "caching the corpus scan
behind an mtime check". Measurement killed it rather than postponed it: the scan
is not the expensive part (§SD4), and an mtime key over the scanned tree costs
about as much to compute as the work it would skip. What shipped instead is a
short shared read, which is a consistency device that happens to halve the cost —
a different thing for a different reason. Nobody should re-attempt the mtime key
on the strength of a deferral that was really a wrong guess.

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
- **The cross-check was temporary, and knowing that changes what it was for.**
  While both existed the two folds checked each other, at the price of two
  implementations of one picture. That reads like a standing arrangement and was
  not one: it lasted exactly as long as the migration. Once the SQL board proved
  faithful, keeping the Go one only to disagree with it was a second thing to
  maintain, so it went (ADR-0092's 2026-07-15 update) and the check went with it
  — an oracle cannot outlive the implementation it oracles. What survives is the
  parity test between `keelson` and `boxer adr`'s dump, which pins the *inputs*
  the board reads; nothing now pins the fold but its own tests. Read the
  isomorphism result in §Validation as a gate that passed, not as a property
  under guard.
- **The fold ported exactly; the editorial did not.** Run against the real
  corpus, the SQL board agreed with `buildBoard` on every card — lane, title,
  subtitle and all three tallies (§Validation). What the Go board did that a
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
- **The pane is bigger than the app it replaced, and its win is amortized, not
  immediate.** Measured against `adrboard` while both existed — same board, same
  widget, so the widget cancels out. `adrboard` has since been deleted
  (ADR-0092's 2026-07-15 update), so these are the numbers as of that decision,
  not something a reader can re-measure:

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

  Two things the line counts did not show, both favouring the app for a *single*
  board: `adrboard` was synchronous and self-contained, and could be read top to
  bottom, where the pane has a claim, a fold cache whose invalidation is
  load-bearing (it holds the selection), an async lane and a channel
  negotiation. Its self-containedness is what §SD4 had to answer before the app
  could go — a board that needs a server and a load step is not a replacement
  for one that needs a launch. And one favouring the pane, which no count
  captures: changing what `adrboard` showed meant editing Go and relaunching;
  changing the pane's board means editing SQL and pressing Run.

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
- Isomorphism (**done once, by hand — a gate, and it can never be a test**):
  `buildBoard` dumped as rows and diffed against the snippet-library query over
  the same corpus snapshot, loaded from `boxer adr build`'s Arrow into a server.
  Every card agreed on lane, title, subtitle and all three tallies, and the lane
  inventory matched with its counts. Two traps it surfaced: the comparison must
  not run through TabSeparated, which escapes `'` and `\` in a title and reads as
  a false difference; and the two sides must be dumped at the same moment, since
  a first attempt disagreed on one ADR purely because a concurrent edit added a
  citation between the dump and the fold — the corpus moved under the read,
  which is §SD4's Live-freshness argument stated as evidence rather than as
  reasoning.

  An earlier draft of this section said the check "wants to be a standing test"
  and was blocked only on §SD4. That was wrong twice: §SD4 landed, and the check
  is still impossible — deleting `adrboard` deleted the oracle. A test that pins
  SQL against Go needs the Go, and keeping it only for that is the duplicate this
  decision removed. So the gate is what it is: it licensed the switch, it does
  not guard it. What guards the board now is the pane's own tests and the
  keelson-versus-dump parity above.
- Integration (done): a scripted capture against a `values()` literal board —
  verifiable with no corpus and no ADR table — confirming the lanes and their
  counts, the per-card tallies against the source rows, the zero-tally skip, and
  the legend colours by their hex. Plus a live drive (egui inspection) through
  the whole loop: the snippet's **Replace** into the editor, Run, and a card
  click moving Detail to that row (row 3 of 8, `ADR-0114`) — the viewof duality
  in both directions.

## Updates

### 2026-07-27 — §SD4 gains a fourth table: `adrcontent`

`keelson('adrcontent')` joins the three tables §SD4 describes, carrying each
decision's markdown source (`num`, `path`, `content@text/markdown`) beside the
metadata the others expose. Everything §SD4 decides holds for it unchanged: the
same name and schema `boxer adr` binds over its Arrow dump, Live freshness, the
shared read window, empty rather than erroring off-repo, and the same schema-
and cell-parity tests. The decision to hold it apart from `adr` rather than add
a column — it is ~60× the size, and the engine widens to all columns for any
star, aggregate or join — is recorded in
[ADR-0092](./0092-adr-overview-tool.md)'s 2026-07-27 update, with the
measurements.

The board query in the snippet library then took the table up: it `LEFT JOIN`s
`adrcontent` and carries the column through, so clicking a card shows that
decision's text in **Detail**. The join is `LEFT` because an ADR whose file
cannot be read has no `adrcontent` row, and an inner join would take its card
off the board rather than showing it textless.

Measured on this corpus through the in-process endpoint, that costs the board
~63 ms → ~122 ms and an 11 KB result → ~1.9 MB. The board reads all 141
decisions to show the one that was clicked, because a projection prunes columns
and not rows.

Worth being precise about what the split bought, since the shipped board now
pays: not that this board is cheap, but that the cost is a **choice a query
makes** rather than a tax on every query over `adr`. The dotless boards, the
sub-item worklist and the decision graph name `adr`/`subtask`/`coderef` and are
untouched; had the source been a column, all of them would carry it, because the
engine widens to all columns for any star, aggregate or join. Removing two lines
from the board query gets the 11 KB version back.

One consequence lands outside §SD2's contract and is recorded here because
nothing else would catch it: the Table tab renders whole cells, so on this
result it pushes ADR bodies — up to ~116 KB each — across the FFI boundary per
visible row per frame. The Kanban and Detail tabs do not (one renders short
strings, the other one selected row). That is a property of any result carrying
large cells rather than of this board, but this board is the first shipped
snippet where it is reachable by accident.

One thing this is the first instance of: the column declares its media type in
its *source table's* name, not in a result alias. ADR-0123 §SD2 introduced
`label@mime` for what a query aliases on its way out; here a table already
carries it, and `SELECT *` propagates it to the Detail pane with no alias
written at all. The convention needed no change to work in that position — it
is a column name that requires backticks either way, and clickhouse-local
rejects it unquoted rather than reading `@` as anything plausible.

### 2026-11-01 — a Bow-tie pane on the same contract

A second pane renders the active result through §SD1's contract, as a bow-tie
diagram: threats, prevention barriers in series, one top event, mitigation
barriers in series, consequences (`play_bowtie_panel.go`, over
`widgets/bowtie`). The decision this records is the column contract, which a
reader of this ADR should find beside the board's.

One row per element of a path, columns by name:

| column | type | required | meaning |
| --- | --- | --- | --- |
| `top_event` | any | yes | the loss the diagram is about; one value across the result |
| `side` | any | yes | `prevention` (a threat's path) or `mitigation` (a consequence's) |
| `path` | any | yes | the threat or consequence |
| `step` | integer | with `barrier` | the barrier's place along its path; NULL on a row naming a path with no barrier |
| `barrier` | any | no | the barrier's name; NULL with the step |
| `status` | any | with `barrier` | `effective`, `degraded`, `failed` or `untested`; empty is untested |
| `evidence` | any | no | the test behind the status, shown on hover |

Barriers are ordered by `step`, which need only be distinct on its path, and
paths by first-seen row, so `ORDER BY` decides the vertical order of each side
as it decides the board's lanes. `step` alone is typed, for §SD1's dot reason:
it is an order, and a fractional one has no meaning.

**One row per element, not the Network's `edges`/`vertices` pair.** A
bow-tie's edges are implied by `side` and `step`, and its layers by the same
two columns, so a nodes-and-edges contract would ask the query to spell
twice what it already says once, and give it two places to disagree. The
Network contract also leaves the layering to Graphviz, which ranks the threat
of a short path closer to the top event than the threat of a long one. The
widget pins the layers instead, with the `Node.Rank` that
[ADR-0069](./0069-imzero2-layeredgraph-widget.md)'s 2026-11-01 update adds.

**Refusals, not guesses.** A `barrier` column without `step` or `status` is a
schema rejection. `status` is required because the alternative is to draw
every barrier as untested, which asserts something the query did not say.
Two different `top_event` values, an unknown `side` or `status` word, and two
barriers at one step of one path are data rejections that name the row. The
pane reads at most 400 rows and counts the rest in its status line.

A click on a barrier writes `selection` for that barrier's row, and a click on
a threat or consequence writes it for the first row naming the path. The top
event is not one row and publishes nothing. Like the World pane, the diagram
does not paint a selection, so it does not follow the signal (§SD3's reasoning
applies only to a pane that paints one).

`path` is also the Files pane's one required column (ADR-0200), so a bow-tie
result is offered there too, as a tree of paths. That is a property of
named-column contracts sharing a vocabulary, which §Consequences anticipated.

The worked example is the Snippets library's "Bow-tie" section: three
threats, three consequences and nine barriers from inline `values()` rows.
`apps/play/scenes/38_bowtie.scene.md` runs it and captures the pane.

### 2026-11-01 — Risk matrix and Impact tolerance: two more panes on §SD1

Two further panes read the active result through §SD1's contract, each over a
widget of its own: **Risk matrix** (`play_riskmatrix_panel.go`, over
`widgets/riskmatrix`) and **Impact tolerance** (`play_tolerance_panel.go`,
over `widgets/impacttolerance`). Both write `selection` for the clicked row
and, since both paint a selection, follow it (§SD3). As with the Bow-tie
entry above, the decision recorded here is the column contracts.

Risk matrix, one row per risk:

| column | type | required | meaning |
| --- | --- | --- | --- |
| `id` | any | yes | the mark's label |
| `name` | any | no | the key and the tooltip; `id` stands in |
| `p_lo`, `p_hi` | numeric | yes | likelihood per year, as a range |
| `impact_lo`, `impact_hi` | numeric | yes | impact if it happens, as a range |
| `inherent_p_lo`, `inherent_p_hi`, `inherent_impact_lo`, `inherent_impact_hi` | numeric | all four or none | the same before controls; NULL on a row with no inherent rating |

Impact tolerance, one row per service and scenario:

| column | type | required | meaning |
| --- | --- | --- | --- |
| `service` | any | yes | the row; first-seen order is row order |
| `tolerance_hours` | numeric | yes | the longest disruption the service may suffer, repeated on each of its rows |
| `scenario` | any | yes | first-seen order is legend order; NULL lists a service with no estimate |
| `recovery_hours` | numeric | yes | the estimated time to recover; NULL is no estimate |

**Numbers are typed and must be positive.** The ranges are drawn on log axes,
so §SD2's reasoning for typed tallies applies: a textual likelihood is not a
weaker matrix but a meaningless one. A row whose residual range is not
positive is skipped and counted in the status line rather than drawn at an
invented place. A range written high to low is read as a range.

**A partial inherent rating is a rejection**, for the reason `dot_done@` is: a
query that names two of the four columns has a typo, not a half-rating.

**The tolerance contract is long form**, one row per estimate, because that is
what an exercise records and what a join produces. The wide form — one column
per scenario — was rejected on §SD1's grounds: scenario names would become
column names, and the pane would have to tell estimate columns from the rest
by position or by a prefix grammar. The tolerance repeats on each of a
service's rows; the first row's is drawn and a disagreement is counted.

**Bands are the widget's, not the contract's.** The matrix's banding — the
edges, their labels and the shade boundaries — is a rendering choice over the
stored ranges (`riskmatrix.Bands`), and the pane draws the defaults. A cell's
shade is the expected annual loss at the cell's geometric centre, never a
product of ordinal scores. deferred: declaring band edges from the query, in
the manner of §SD6's `lanes` CTE.

**Every mark the three panes draw is a hit region** (ADR-0149's 2026-11-01
hit regions): a risk dot, a banded cell, an estimate, a tolerance line, a
bow-tie node. The host shows the tooltip at the mark and gives the mark an
accessibility node named for what it stands for — `R8 · Warehouse fire`, a
barrier's name, `Warehouse dispatch · Key supplier failure` — so the scenes
hover by name rather than at a point read off a capture, which went stale
whenever the layout moved. The data marks are Tab stops, and Enter on one is
a click, so the selection these panes write can be made from the keyboard. A
canvas-wide `c.HoverUi` (and, for the bow-tie, `c.HoverText`) was the stopgap
before hit regions existed; it put the tooltip under the canvas's corner and
named no mark.

The worked examples are the Snippets library's "Risk matrix" and "Impact
tolerance" sections, inline `values()` rows of eight risks and of four
services under three scenarios. `apps/play/scenes/38_risk_matrix.scene.md`
and `38_impact_tolerance.scene.md` run them and capture each pane.

### 2026-11-01 — the panes read units from glosses, and the matrix's shades are IDS-native

The entry above assumed a unit on each side: the matrix formatted every amount
in euros, and the tolerance columns baked hours into their names. Neither could
be said from the SQL. [ADR-0186](./0186-play-gloss-catalog.md)'s glosses are how
a column says what its numbers are, and it gains `gloss/money` for this.

**A pane reads the declaration in the column's name**, the alias or the
`gloss(…)` macro that writes one (`play_column_unit.go`). The rule route is not
read, so a `-- play: gloss` line shows in Table and not here. These contracts
already ask for one `AS` per column, which gives the declaration a place to go,
and acceptance runs per frame without the buffer's directives to hand.

**Risk matrix.** `impact_lo`, `impact_hi` and the two inherent impact columns
may declare `gloss/money` or any quantity gloss with a `unit`. The widget knows
no glosses: play passes `riskmatrix.Input.FormatImpact`, built from the gloss's
inline face, and `ImpactUnit` for the axis title. Undeclared, the amounts are
plain numbers; `FormatEuro` is gone and nothing assumes a currency. A
disagreement among the columns is a rejection naming them, and that includes
one column with a unit beside one without, for the reason a partial inherent
rating is: a query saying it for three columns of four has a typo. A declaration
the catalog refuses is rejected with the catalog's reason. `gloss/epoch` is
rejected as a moment, and a gloss with no `unit` (`gloss/raw`, `gloss/bytes`)
says nothing about one. The expected-loss shading multiplies a rate by an amount
and so assumes a ratio scale; it is not meaningful for a temperature in °C.

**Default impact bands follow the register.** Steps of five from 100k assumed
money in the millions, and a register in hours fell wholly into the first band.
A zero `Bands` now anchors the same steps on a power of ten centred on the
geometric mean of the register's impact midpoints, and scales the shade edges
with it, so the shading is the same at every scale. The worked example still
lands on 100k. `DefaultBands` remains that fixed scale.

**Impact tolerance.** The contract is `tolerance` and `recovery` carrying
`gloss/duration`, each in any of its units, converted to the widget's hours for
comparison; the axis labels each tick in the unit it reads best in, as before.
`tolerance_hours` and `recovery_hours` stay as the shorthand for plain columns
in hours. Different units on the two columns are one quantity and are converted,
unlike the matrix's, where no factor exists between currencies. Rejected: a bare
`tolerance`, a gloss that is not a span, a shorthand whose gloss says other than
hours, and both spellings at once.

**The shade classes resolve through the IDS, and their range is computed.** The
per-theme `t` constants on Oslo are replaced by `selectRamp`, which measures the
active LUT against the theme's canvas and the dot's ink. A class must sit between
the canvas and the ink in lightness, keep ΔE 8 from the canvas and 3:1 from the
dot, and rise in lightness and in distance from the canvas; among such sequences
it takes the one whose least step, over normal sight and simulated
deuteranopia, protanopia and tritanopia, is largest, the canvas counting as a
step below the lowest class. ΔE 8 is four times OKLab's just-noticeable
difference of about 2. `IDS_ACCESSIBILITY` and `IDS_PALETTE_SEQUENTIAL` apply,
and Oslo remains the default with neither set, which departs from the IDS default
of Batlow.

Five classes, least adjacent OKLab ΔE; "any" is the smallest across the four
visions:

| arm | theme | normal | deut | prot | trit | any |
| --- | --- | --- | --- | --- | --- | --- |
| Oslo, the earlier constants | dark | 6.8 | 6.6 | 7.1 | 6.8 | 6.6 |
| Oslo, computed | dark | 9.6 | 9.4 | 9.5 | 9.2 | 9.2 |
| Batlow, computed | dark | 10.9 | 10.3 | 9.8 | 9.8 | 9.8 |
| Oslo, the earlier constants | fresh | 8.3 | 8.6 | 7.5 | 8.4 | 7.5 |
| Oslo, computed | fresh | 9.3 | 9.4 | 8.6 | 8.5 | 8.5 |
| Batlow, computed | fresh | 10.4 | 10.1 | 9.0 | 8.7 | 8.7 |

Batlow separates slightly better, and that is not what decided it. Its hue
wanders with loss: on the light theme a dark teal-green tops an olive, a mustard
and a peach, ΔE 9.5 from the theme's success colour, so the highest loss on the
chart reads closest to "good". Oslo stays blue throughout, nearer the info colour
(ΔE 7.3 at the top on the light theme), which the matrix does not use. Under
either, the order is carried by lightness and by distance from the canvas, both
of which rise class by class and survive dichromacy and greyscale; saturation
does not, so `selectRamp` does not constrain it. Batlow stays one setting away.

The dot holds 3:1 or more on every class by construction, which the label ink
does not (2.5:1 at the lowest), so an id's label now has a chip in the canvas
colour, as the dot has its ring. The inherent ring and arrow take the dot's ink
for the same floor: the secondary ink they used has none, and on the light
theme's top class it sat near 1:1.
