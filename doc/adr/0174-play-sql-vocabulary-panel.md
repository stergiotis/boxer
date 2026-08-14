---
type: adr
status: accepted
date: 2026-08-07
reviewed-by: "p@stergiotis"
reviewed-date: 2026-08-14
---

# ADR-0174: a vocabulary panel for play — what this buffer can call, and where it runs

## Context

A statement typed into play's editor may name a function from three
populations, and nothing on screen distinguishes them:

- **Server SQL UDFs.** The `LW_*` family ([ADR-0162](./0162-leeway-co-ragged-function-pack.md),
  [ADR-0066](./0066-leeway-dql-clickhouse-readback-generator.md),
  [ADR-0106](./0106-identity-fibonacci-tags-build-tag-retirement.md)) plus
  whatever else the endpoint happens to carry. These exist only where they
  were installed. A missing one fails server-side as "unknown function",
  which reads as a query mistake rather than a provisioning fact.
- **Client-expanded macros.** `descriptiveStatistics(…)`
  ([ADR-0161](./0161-play-distribution-panel.md)), `docsearch('…')`
  ([ADR-0164](./0164-documentation-regex-search.md)), `keelson('…')`
  ([ADR-0094](./0094-keelson-introspection-tables.md)) and the `LW_ID_*`
  family, expanded by the pre-execute pass registry
  ([ADR-0108](./0108-keelson-sql-pass-registry.md)) before the statement
  ships. They work against any endpoint — including one carrying none of the
  UDFs — because the server never sees the call.
- **play-local `ts*`.** Computed in play and never shipped
  ([ADR-0163](./0163-play-timeseries-workbench.md)), reserved as a whole
  family so a reserved-but-unshipped name refuses loudly instead of
  travelling.

The three fail differently, are provisioned differently, and are documented in
six different places. The
[jsonbench-on-facts trial](../trials/jsonbench-on-facts/README.md) priced what
that costs: its headline number moved four times, every time because it had
hand-written something that already existed, and its verdict was that the
toolbelt is *undiscoverable from where a task-level consumer stands* — not
slow. [ADR-0171](./0171-leeway-sql-read-surface.md) answers that for a reader
of the repository. This ADR answers it for someone sitting in front of play,
which is where the trial's author actually was.

What play has today is adjacent but does not cover it. The Docs pane
([ADR-0120 lineage](./0120-play-natural-language-ask-panel.md), `play_docs_clickhouse.go`)
answers *per name* — including for UDFs, whose `create_query` it renders when
`system.documentation` has no prose — but only once you know the name to ask
about. The `ts*` shadowing probe (`play_ts_collision.go`) already queries
`system.functions` for one purpose. Neither enumerates, and neither says which
population a name belongs to.

The 2026-08-07 namespace rename (ADR-0162 Update) is what makes the server
half cheap: the whole leeway vocabulary is now `WHERE name LIKE 'LW\_%'`,
where it previously meant four prefixes and no way to be sure none was
forgotten.

## Design space (QOC)

**Question.** How does someone writing SQL in play find out what they can
call, and whether the endpoint they are pointed at actually has it?

**Options.**

- **O1 — List the server's UDFs.** A tool tab over
  `system.functions WHERE origin != 'System'`, filtered and insertable.
- **O2 — Three-population panel with a declared-vs-installed diff.** O1 plus
  the client-expanded macros and `ts*`, each labelled by where it runs, and
  the build's declared rosters diffed against what the probe found.
- **O3 — Fold it into the Docs pane.** Give the existing pane a browse mode
  listing everything it can answer about.
- **O4 — Fold it into the Schema panel.** Vocabulary as another thing the
  endpoint has, beside its tables and columns.

**Criteria.**

- **C1 — Answers "what can I call".** Enumerates rather than requiring the
  name up front.
- **C2 — Answers "will it work here".** Distinguishes a function this
  endpoint has from one the client expands from one that is simply absent.
- **C3 — Honest about endpoint dependence.** Retargeting must change the
  answer, visibly.
- **C4 — Cost, and cost of the second copy.** Against the existing panel,
  lane, filter and docs machinery.

**Assessment.** `++` strong positive, `+` positive, `−` negative, `−−` strong negative.

|    | O1 | O2 | O3 | O4 |
|----|----|----|----|----|
| C1 | +  | ++ | +  | +  |
| C2 | −− | ++ | −  | −  |
| C3 | +  | ++ | +  | ++ |
| C4 | ++ | +  | +  | −  |

O1 is the cheapest and gets C2 exactly backwards: a user whose endpoint lacks
the pack would see an empty list and conclude the vocabulary does not exist,
when in fact `LW_ID_*` and `descriptiveStatistics` would have worked. O3 makes
the Docs pane two things — a reference lookup and a catalog — with different
lifecycles. O4 is the closest miss: the vocabulary genuinely is endpoint
state, but two of the three populations are not, and a panel whose title
promises "this endpoint" cannot honestly hold them.

## Decision

A **Vocabulary** tool tab, sibling of Snippets in `TabZoneTools`, following the
established play tool-panel contract. This is option O2.

### SD1 — Sectioned by where a function runs, not by which package declares it

Three sections — **server**, **client**, **play** — because that is the
distinction that predicts how a call fails and what a user does about it. A
server function that is absent needs provisioning; a client macro that is
absent needs a pass registered in this host; a `ts*` name that is reserved but
unshipped will refuse. Grouping by declaring package (`chpack`,
`readback`, `identsql`, `distsql`, …) would put functions with identical
failure modes in different places and functions with different failure modes
together.

The `LW_ID_*` family is in **both** the server and client sections, because it
genuinely is both — installable as UDFs (`identsql.UdfDdlStatements`) and
expanded client-side by `identsql.ExpandPass`. Listing it once and picking a
side would make one of the two answers wrong.

Where a name runs and what its expansion needs are two questions, and the
second cuts across the first for at least one family — §SD6.

### SD2 — The server section is a probe, and the endpoint is part of the question

`system.functions` through a `nodeLane` over `clientExecutor` — the same
mechanism `play_ts_collision.go` and `ClickHouseDocsSource` use: off the render
thread, memoised on the SQL, routed through whatever endpoint the client
resolves to, inheriting auth and the pre-execute stage. Endpoint dependence is
therefore structural rather than something the panel implements: retargeting
([ADR-0134](./0134-adhoc-datasets.md)) redefines what the lane talks to,
so the answer changes with it.

An unanswered probe reads as *nothing known*, never as *nothing installed* —
the same rule the shadowing probe follows. A panel that says "missing" because
a query has not come back yet would send someone to reprovision a server that
was fine.

### SD3 — The build's declared rosters are exported, and the panel diffs against them

Listing what a server has answers half the question. The half that matters
when something is wrong is what it *should* have and does not, which needs the
build's declared roster on the client side. `chpack.Functions()` already
exports its own; the read-back family and `identsql` publish theirs
(see Surfaces). The panel diffs declared against probed and marks each
function present or missing.

The diff runs both ways: a probed name that no roster claims is listed as an
**extra** — a hand-installed helper, or a spelling left by a rename this build
no longer performs. It is the half of ADR-0171 §SD2's drift question a client
can answer on its own.

Which makes roster completeness load-bearing, and is why a family this build
installs joins an existing roster rather than standing beside its own
generator: undeclared, it reads as *on this endpoint but not in any roster
this build carries* — a sentence about the server, for what would be a gap in
the build. [ADR-0182](./0182-leeway-aspects-v2-codec-and-vocabulary.md) §SD4's
generated `LW_ASPECT_*` family is the worked case. It joined `chpack`, so it
is declared by `Functions()` like the rest of the pack.

The server population is taken from
[ADR-0171](./0171-leeway-sql-read-surface.md) §SD2's declared set — the union
of the three rosters plus the surface marker — rather than by looping the
three rosters here. Same names, but by construction rather than by
maintenance: what the panel diffs against is exactly what the installer
installs, so the marker itself cannot read as an undeclared extra on a
correctly provisioned server.

The surface revision comes out of the same probe, so *skew* — the server has
the families but at a revision this build did not write — is distinguishable
from absence. That is the drift ADR-0171 §SD2 is about, surfaced where someone
is already looking at a wrong answer. A server carrying the retired
`LW_PACK_VERSION` and no surface marker gets its own sentence: it was
provisioned before the three families shared a marker, which is neither
absence nor skew.

It is read out of `LW_PACK_VERSION`'s stored definition rather than by calling
it. Calling would fail with unknown-function on exactly the servers whose
version matters most, and a failed query poisons the lane for the whole
listing; the definition text is already in the probe's result and parsing an
integer out of it cannot fail that way. An unreadable or absent body reports
*unknown* rather than a number, so a hand-edited marker never misdescribes a
server.

Deliberately **not** a provisioning affordance. The panel reports; it does not
offer to install. Provisioning is a process-level reconcile at startup
(`installChPack`), and a per-user "fix it" button in a query tool is a
different decision about who may change server state, which this ADR does not
make.

### SD4 — The panel inserts; the Docs pane explains

A row carries an Insert action (`InsertSqlAtCaret`, the seam Snippets uses)
and a link that routes the name into the Docs pane. Prose is not duplicated:
the Docs pane already renders a UDF's `create_query` and a builtin's
`system.documentation` entry, and a second rendering would drift from it.
What the panel shows inline is the signature and the one-line doc the declared
roster already carries (`chpack.Function.Doc`, `tsSpec.Doc`).

### SD5 — The filter is the shared search battery

The regex battery of [ADR-0164](./0164-documentation-regex-search.md) —
`regexedit` in `ModeTokens`, space-separated patterns ANDed — as in
`renderSnippetsFilterRow`. A user filtering the vocabulary and filtering the
snippet library should not be typing in two different query languages.

### SD6 — A client macro may still need the server, and the row says which

Being expanded client-side is not the same as being endpoint-independent. The
client section's promise — rewritten before it ships, so it works anywhere —
is a property of the *expansion output*, not of where the expansion happens.
For every macro listed when this ADR was written the two coincide: `LW_ID_*`
expands into builtin arithmetic, `descriptiveStatistics` into builtin
aggregates, `docsearch` and `keelson` into table references whose transport
the downstream pass owns. None of them needs anything *installed*.

[ADR-0181](./0181-leeway-dql-authoring-surface.md) §SD3's `LW_GET` family is
the first that does. Its v0 renderer emits pack-form calls
(`LW_VALUE_BY_TAG_EQUAL` and family) because the inline builtins-only renderer
is deliberately not shipped (ADR-0162 §SD8-1) — so the name never reaches the
server, and the expansion still fails on an endpoint without the pack. Listed
unqualified under the client heading it would be the one row this panel
actively misleads about, in the section whose whole point is that its members
cannot fail that way.

A roster entry may therefore declare the server functions its expansion emits.
The panel resolves them against the probe the server section already ran and
marks the row by the weakest of them: unmarked before the probe answers,
present when every dependency is installed, otherwise naming the first missing
one. No new query and no new state — §SD3's declared-vs-probed diff holds both
halves already; this reads it for a second population.

The dependency is on **installation, not on data**. A macro expanding into a
table reference depends on that table being reachable, which is a different
question, has a different answer per retarget ([ADR-0134](./0134-adhoc-datasets.md)),
and is not what this marks. Confining it to function names keeps the mark to
what the probe can decide.

Today's three rosters declare no dependencies, so no exported roster shape
moves until ADR-0181's names arrive.

### Milestones

- **M0 — Declared rosters exported.** ✓ The read-back family and `identsql`
  publish their names and signatures; a test pins each roster against the SQL
  its package actually emits, so a roster cannot drift from its DDL.
- **M1 — The probe.** ✓ `system.functions` lane, endpoint-scoped, with the
  unanswered-reads-as-unknown rule.
- **M2 — The panel.** ✓ Three sections, filter, Insert, present/missing marks.
- **M3 — Skew and docs routing.** `LW_SURFACE_VERSION` comparison and the
  row → Docs pane link. **Half done:** the skew line landed (and now reads
  the surface marker, with the pre-surface fallback); the row → Docs pane
  link did not, and is the one piece of this ADR still unbuilt.
- **M4 — Expansion dependencies.** ✓ §SD6's mark — landed with ADR-0181's
  `LW_GET` family, which is what it was sequenced behind. A roster entry may
  declare the server functions its client-side expansion emits; the probe
  answers for those names too, and a client row whose dependencies are absent
  says so. Marked for every population, not just the server one: the case the
  mark exists for is precisely a client entry that cannot work here, which no
  per-name "installed?" column can express. The line renders only when
  something is missing — naming an expansion's needs on every endpoint that
  has them would be noise on every row.

## Surfaces — Tier 1

| Surface | Change | Moves with it |
| --- | --- | --- |
| `readback` (exported Go API under `public/`) | gains an exported roster — names and signatures of the family `HelperUDFsSQL()` emits | a test pinning the roster against the embedded SQL |
| `identsql` (exported Go API under `public/`) | gains an exported roster over the existing `Name*` constants | a test pinning it against `UdfDdlStatements()` |
| `distsql` (exported Go API under `public/`) | gains `FuncName`, the macro's canonical spelling; the existing registry key becomes that name normalised | nothing — the key it derives is byte-identical to the literal it replaces |
| play dock tabs (named registry, `play_tabs.go`) | one new tab id and zone entry | the tab roster test, the dock id constants |

`chpack` is unchanged: `Functions()` is already the shape the other two are
gaining. `docsearchsql.FuncName` and `keelsonsql.FuncName` already existed and
are what `distsql.FuncName` is named after — the panel reads all three rather
than carrying macro spellings of its own.

## Migration — Tier 1

- **Breaks.** Nothing. Every change is additive; no existing signature moves.
- **Path.** None required. A host that does not want the tab does not
  register it.
- **Regeneration.** None; no generated-code input and no FFI boundary is
  involved.
- **Old shape.** `HelperUDFsSQL()` and `UdfDdlStatements()` keep their
  signatures and remain the provisioning path. The rosters are a second view
  of the same declaration, pinned to it by test rather than derived from it —
  parsing our own emitted DDL to recover names would be a worse coupling than
  a test that fails when the two disagree.

## Verification plan — Tier 1

- **Lane.** Default `go test`: roster-vs-DDL agreement for both new rosters,
  and the tab-roster test that already guards dock ids.
- **Live.** The `//go:build integration` lane for the probe against a real
  `system.functions` — the same lane `chpack`'s install-and-verify test uses.
- **Goes red if.** A function is added to `HelperUDFsSQL()`'s SQL or to
  `UdfDdlStatements()` without being added to its roster, or a dock tab id
  collides. Also: `sqlapplet`'s tab-policy test, which fails when play
  registers a tab the applet surface classifies as neither chrome nor a
  result panel — this one is chrome (an applet has no editor to insert into).
- **Goes red if (§SD6).** A roster entry declares a dependency on a name no
  server roster carries. An unresolvable dependency can never mark anything,
  so a typo there is silent in the panel and has to be caught here.
- **Not covered by an automated lane.** That the panel *renders* correctly.
  It was driven live instead, against ClickHouse 26.7 on a headless
  compositor (`egui-mcp`), which confirmed: the probe reporting 408
  user-defined functions and reading pack v3 out of `create_query` without
  calling the marker; `✓` on the installed pack; `MISSING` on all six
  `LW_ID_*` in the server section while the *same* six list unmarked under
  client — the §SD1 case, and the one a flat listing cannot express;
  `reserved · tsMotifs()`; the filter matching name and doc jointly
  (`gather` returns `LW_CO_GATHER` plus two functions whose doc lines
  reference it); and Insert splicing a call template at the editor caret. A
  scripted capture is worth adding once the layout settles.

## Alternatives

- **Query `system.functions` and show everything, builtins included.**
  ~1500 rows the Docs pane already answers about with real prose. The
  panel's value is the part `system.functions` cannot know — what is
  *missing*, and what never reaches the server at all.
- **Derive the rosters by parsing the emitted DDL.** Removes the
  duplicate declaration, at the cost of a SQL parser in the read path and a
  failure mode where a parse bug silently shrinks the roster. A test that
  compares the two is cheaper and fails louder.
- **Put it in the Passes tab.** That tab already shows client-side rewrites,
  so the macro population would fit — but it is a pipeline schematic keyed on
  the current buffer, and a vocabulary list is neither.
- **Wait for ADR-0171 §SD2's reconciler and read its output.** The
  reconciler is the right long-term source for "what is on this server that
  should not be". It is still proposed, and the panel's question — "what
  should be here and is not" — is answerable now from the declared rosters.

Considered for §SD6 and rejected:

- **Say it in the doc line only.** One sentence on `LW_GET`'s entry, the way
  `keelsonsql`'s entry already carries its table-position rule in prose. It is
  the cheapest honest thing and it is what the panel should say *anyway*, but
  on its own it leaves the section blurb promising endpoint-independence while
  a row underneath it says otherwise, and leaves the mark column — the thing a
  user scans — silent on the one case in that section where it has something
  to report. C2 is the criterion this ADR exists for.
- **List the `LW_GET` family in the server section instead.** Then the diff
  applies unchanged. But the name never reaches the server, so `MISSING` would
  be permanently and uninformatively true, and §SD1's rule would be inverted:
  the row would sit under a heading that mispredicts how it fails.
- **A fourth section for client-with-server-support.** Sections answer where a
  name runs, and that has not changed. Splitting the population would separate
  rows that are identical in every other respect — provisioned the same way,
  failing the same way when the pass is absent — and would put `LW_GET` away
  from `LW_PLAIN`, which is exactly the comparison an author of a leeway→leeway
  statement is making.

## Consequences

### Positive

- The three populations become nameable, and the failure modes attributable:
  "your endpoint does not have this" is a different sentence from "this never
  goes to your endpoint".
- The provisioning drift ADR-0171 found becomes visible to the person
  affected by it, at the moment they are affected.

### Negative

- A second declaration of two rosters, kept honest by test rather than by
  construction. If the test is ever weakened, the panel can report a function
  as missing that is installed under a name the roster forgot.
- One more `system.functions` query per session per endpoint. It is memoised
  by the lane and lazy with the tab, so a session that never opens the tab
  never pays it.
- §SD6 costs the three-population story some of its crispness: "client" no
  longer means "works anywhere" without qualification, and a reader now has to
  look at the mark as well as the heading. The alternative was a heading that
  is simply wrong for one family.

### Neutral

- Nothing about execution changes. The panel reads; no statement is rewritten
  because it exists.

## Status

Accepted 2026-08-14. M0–M2 are the useful cut: rosters, probe, panel. M3 (version skew
and Docs routing) is separable and can be dropped without making the rest
incoherent — the panel is still worth having when it can only say present or
missing.

Implemented and driven 2026-08-07 ahead of review, per the verification items
above.

M3's skew half landed with M0–M2 rather than after: reading the revision out
of the marker's stored definition instead of calling it turned out to be
simpler than the conditional second probe the milestone assumed, so it cost
nothing to include. It now reads `LW_SURFACE_VERSION`, falling back to the
retired `LW_PACK_VERSION` for a server no current build has reconciled
(ADR-0171 §SD2). Routing a row into the Docs pane is the part of M3 still
open — the Docs tab already answers for any of these names typed by hand.

M4 landed 2026-08-14 with ADR-0181 §SD3's `LW_GET` family, which is what it
was sequenced behind.
