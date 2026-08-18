---
type: adr
status: proposed
date: 2026-08-18
# reviewed-by: "@<handle>"     # fill in and uncomment when flipping to accepted
# reviewed-date: YYYY-MM-DD    # fill in and uncomment when flipping to accepted
---

> **Status: proposed — pre-human-review.** Decision under consideration; do not
> implement as if accepted.

# ADR-0196: two-stage SLL parsing, and the WITH-clause ambiguity behind it

## Context

Parsing is essentially the entire cost of a `play` Run. On the 9 KB
`runtime_timeline_applet.sql` fixture, one pre-execute stage costs ~3.1 s, of
which **95.4%** is parsing and **4.6%** is everything else — every pass, every
rewrite, every scope build put together. Lexing is 0.2%. A single parse of that
buffer costs 94–97 ms and allocates 103 MB.

The cause is one ambiguity in grammar1, not a general slowness.
`ctes` (line 66) and `withClause` (line 107) have **byte-identical right-hand
sides** and are both reachable at the same input position, so a leading `WITH`
can be consumed by either and the two derivations are indistinguishable at any
lookahead. ANTLR's stock diagnostic listener reports it exactly:

```
AMBIGUITY  decision=4  rule=query  tokens[11..1690]  exact=true  alts={1, 2}
```

Resolving it needs **full-context LL simulation**, which ANTLR never writes into
the DFA — full-context predictions are context-dependent by construction, so
there is nothing stable to memoise. That decision, fired **once** per parse, is
**98.2%** of all full-context work (495,135 of 504,064 ATN configs), and it is
re-simulated on every parse for the life of the process.

This closes out two prior deferrals.
[ADR-0084](./0084-nanopass-antlr-dfa-cache-bounding.md) §Alternatives observed
that SLL "is faster … could be revisited purely for parse latency" and left it;
[ADR-0125](./0125-codeview-prepare-memo.md) §SD5 named "full-context LL
prediction on the `WITH … AS` ambiguity" as an unmeasured suspect. It was the
right suspect. It also explains why ADR-0084's bounded cache, working exactly as
designed (0 resets, 23–1163 states against an 8192 threshold), buys nothing
here: the states it holds are not the ones being recomputed.

Full measurements, the per-decision attribution, the grammar0 comparison showing
this is a repo-local regression, and six costed grammar repairs are in
[nanopass — the full-context prediction tax on `WITH`](../adr-background-work/nanopass-full-context-prediction-tax.md).

## Design space (QOC)

| Question | Options | Criterion | Choice |
|---|---|---|---|
| How is the ambiguity avoided? | repair the grammar · two-stage SLL · decompose per-SELECT · accept it | cost against blast radius | **both** — two-stage SLL, then the repair |
| Which seams change? | `nanopass.Parse` only · every seam | `env.scanBody` is 10 of 28 parses | every seam |
| What guarantees correctness? | reasoning · corpus differential | SLL is provably weaker than LL | differential |
| How does stage one abort? | `BailErrorStrategy` · ordinary listener | the Go port's bail strategy panics | ordinary listener |

**Two-stage SLL** comes first because it is ~80× for tens of lines, touches no
grammar and no consumer, and is reversible by one constant. **The grammar repair
follows** (§SD5): it is not a substitute — SLL already sidesteps the ambiguity —
but it makes the LL *fallback* cheap, and the fallback is not rare. A realistic
`WITH`-led query (`benchMediumSQL`) is SLL-rejected, so before the repair it paid
the full ambiguity on every retry.

Note that neither is an ANTLR-version problem. The tool is at **4.13.2**
(August 2024) and the Go runtime at **v4.13.1** (May 2024), and both are the
newest releases that exist — checked against the module proxy and the GitHub
API. The missing profiling API and the broken `BailErrorStrategy` (§SD2) are not
waiting on an upgrade.

**Decomposition** — parse each SELECT separately, parse the outer structure once
— was measured at 9.3× under LL and **1.11× under SLL**. It attacks the same
cost by shrinking the ambiguous decision's span rather than removing the
ambiguity, so once SLL is in place there is nothing left for it to exploit.

## Decision

Every construction of a grammar1 or grammar2 parser attempts the parse in
`PredictionModeSLL` first, and re-parses in `PredictionModeLL` if the SLL
attempt reports any diagnostic. All parse seams share one bounded DFA cache per
grammar.

And the ambiguity itself is removed: both grammars drop the duplicated
`withClause` rule, so `ctes` is the only rule that accepts a `WITH` and no
single decision has to choose between two rules that could consume one. That
makes the LL fallback cheap as well as rare.

### SD1 — Stage one is SLL; any diagnostic falls back to LL

SLL is strictly weaker than LL: it may reject input LL accepts. The fallback is
therefore load-bearing rather than defensive — with naive SLL and no fallback,
four existing tests fail, all `mismatched input … expecting '.'` on
database-qualified names.

The fallback triggers on *any* lexer or parser diagnostic, not on a
classification of which diagnostics are SLL's fault. Diagnostics from the LL
attempt are the ones reported, so error text and positions are unchanged from
today for every input that fails.

### SD2 — Stage one uses the ordinary error listener, not `BailErrorStrategy`

ANTLR's documented two-stage recipe pairs SLL with `BailErrorStrategy` so stage
one aborts at the first error instead of running recovery. **`antlr4-go`
v4.13.1's `BailErrorStrategy` panics** — `RecoverInline` returns a nil token
which the generated parser then dereferences. It is not usable.

Stage one therefore runs the ordinary error strategy and is judged by its
collected diagnostics. The cost is that a rejected parse runs error recovery
before falling back. That is the rare path by construction, and paying it is
better than a panic in a library seam.

### SD3 — Every seam, and one bounded cache per grammar

ADR-0084 fixed "the two seams" and said so. There are four: `nanopass.Parse`,
`nanopass.ParseCanonical`, `env.scanBody`, and `play.firstSyntaxError`. The
latter two construct a parser directly, so they never had the bounded cache
either — they use the grammar's **unbounded package-level global**, which is the
growth ADR-0084 exists to prevent. This ADR closes that bypass as part of the
same change; it is not a separable cleanup, because `env.scanBody` alone accounts
for 10 of the 28 parses a stage pays, and leaving it on LL caps the win at ~2.3×
instead of ~80×.

`env` cannot import `nanopass` (`nanopass` imports `env`), so the shared holder
cannot live in `nanopass` where it does today. It moves:

- the holder **type** to `public/parsing/antlr4utils`, which is grammar-agnostic
  and already the home for shared ANTLR machinery;
- the holder **instances** to the `grammar1` and `grammar2` packages, as
  hand-written files alongside the generated ones (the `package_props.go`
  precedent; `generate.sh` only sweeps `clickhouse*.go`, so they survive
  regeneration).

Every seam then shares exactly one cache per grammar, so ADR-0084's "one clear
memory number" survives — three private holders would have tripled the bound and
split the warmth.

### SD4 — The fallback rate is observable

A parse that falls back pays SLL *and* LL, so a workload where SLL usually fails
is slower than today. Nothing in the system would say so.

`DFACacheStats` gains a sibling reporting SLL hits and LL fallbacks per grammar.
A steadily rising fallback ratio means this ADR is not paying for that workload
and the grammar repair (§SD5) has become worth its cost. The counters are two
`atomic.Int64` increments on a path that already allocates megabytes.

### SD5 — The grammar repair: one rule, three positions

The duplication is removed. `withClause` is deleted; `ctes` is the only rule that
accepts a `WITH`, and it is reached from the three positions one may open in:

```antlr
query               : setStmt* selectUnionStmt ;                       // ctes? removed
selectUnionStmt     : ctes? selectStmtWithParens selectUnionStmtItem* ;
selectUnionStmtItem : (( UNION | EXCEPT | INTERSECT ) ( ALL | DISTINCT )? ctes? selectStmtWithParens) ;
selectStmt          : projectionClause fromClause? … ;                 // withClause? removed
// rule withClause deleted
```

**The property that matters is not "one rule" — it is that no single decision
chooses between two alternatives that derive the same tokens.** The three
`ctes?` decisions are each LL(1), because each is preceded by a distinct token:
nothing (statement head), a set operator (a later arm), or `LPAREN` (a
parenthesised statement or a subquery, which route through their own
`selectUnionStmt`). The retired `withClause` was fatal precisely because it sat
at the *same* position as `query`'s `ctes`.

Framing it that way is what made the repair cheap, and it took two wrong turns
to find:

- **Deleting `withClause` outright** (leaving only the head position) kills the
  ambiguity but drops `… UNION ALL WITH … SELECT …` — a form with a dedicated
  regression test (`TestRegressionUnionBranchWithNotQualified`, from the review
  that also produced ADR-0002's H-series) and corpus entries. Rejected: this
  ADR does not get to narrow the accepted language.
- **Routing the parenthesised arm through a new `parenQuery` rule** — the shape
  the background page ranked first — looks confined to parenthesised arms, but
  the three *subquery* positions (`columnsExprSubquery`, `columnExprSubquery`,
  `tableExprSubquery`) also spell `LPAREN selectUnionStmt RPAREN`. Rerouting
  them changes what ~15 consumers find when they walk children for a
  `SelectUnionStmtContext`, and those break **silently** rather than failing to
  compile. Rejected on blast radius, which was the criterion that picked it.

Keeping `selectUnionStmt` spelled the same everywhere means every one of those
walks still works, and the compiler found every genuinely affected consumer.

**What moves in the CST.** Two relocations, both mechanical:

| WITH at | was | is |
|---|---|---|
| statement head | `query.ctes` | `selectUnionStmt.ctes` |
| a later union arm | `selectStmt.withClause` | `selectUnionStmtItem.ctes` |

**One behaviour is repaired along the way.** ClickHouse scopes an arm's WITH
*forward* — arm 2 sees arm 1's items, never the reverse (live-verified, recorded
in `play_subquery.go`). `buildUnionScopes` did not implement that: a later arm's
`withClause` reached only that arm. It now accumulates left to right, so the
forward case is right and the head-clause case is unchanged.

**Measured.** Warm LL parse, and exact-ambiguity count under
`PredictionModeLLExactAmbigDetection`:

| case | LL before | LL after | | ambiguities before → after |
|---|---|---|---|---|
| `applet_9kb` (10 KB) | 103.15 ms | **4.11 ms** | **25×** | 7 → 6 |
| `cte_12` (322 B) | 3.51 ms | **0.11 ms** | 32× | 1 → **0** |
| `cte_1` (72 B) | 0.98 ms | **0.05 ms** | 20× | 1 → **0** |
| `scalar_with_12` (204 B) | 9.44 ms | 4.85 ms | 1.9× | 13 → 12 |
| `bench_medium` (572 B) | 7.32 ms | 2.50 ms | 2.9× | 16 → 15 |
| `bench_large` (5 KB) | 32.62 ms | 29.66 ms | 1.1× | 265 → 264 |

**Exactly one exact ambiguity disappears in every case** — the WITH one — which
is the cleanest evidence available that the repair did what it claims and nothing
else. It beats the 10–30 ms this ADR first estimated for the applet.

The end-to-end payoff lands where §SD4 said to look: `Apply/medium`, the
SLL-rejected buffer that two-stage alone could not help, goes from 107–125 ms to
**55–77 ms**.

**What it does not fix, and why that is left alone.** Those residual counts are
not noise. On `bench_large` they are **264** exact ambiguities that have nothing
to do with `WITH`, and they were examined (see §SD6).

**grammar2 took the identical edit**, since it carried the identical
duplication. Regeneration needs a hand-provisioned ANTLR 4.13.2 jar and a JRE
(`grammar0/generate.sh`), which is not pinned and is absent from the airgap
bundle — so the generated files are reviewed as part of this change and CI checks
them only as committed source. Regeneration was verified byte-reproducible
against an unmodified tree first, so the committed diff is attributable to the
grammar edit alone.

### SD6 — The residual `t.c` ambiguity is examined and kept

`bench_large`'s 264 exact ambiguities are one decision, in `columnIdentifier`,
and there is **exactly one per qualified name reference**: 6 union arms × 40
qualified projections = 240, plus 6 × 4 dotted references in the FROM/JOIN/WHERE
tail = 24. Strip the projection qualifiers and it measures exactly 24.

Attribution needs a trick, since antlr4-go exposes no profiling API (§SD2): the
`decisionToDFA` slice is ours, so a reported `*DFA` maps back to its index by
pointer identity, and the index to a rule through `ATN.DecisionToState`.

| decision | rule | escalations | ambiguities | context-sensitive | summed span |
|---|---|---|---|---|---|
| 122 | `columnExpr` | 246 | 0 | **246** | 3312 (71%) |
| 128 | `columnIdentifier` | 264 | **264** | 0 | 1320 (28%) |
| 134 | `tableIdentifier` | 6 | 0 | 6 | 30 |

**This one is not a defect.** `t.c` genuinely means either "column `c` of table
`t`" or "field `c` of a Nested/tuple column `t`", and only the schema decides.
The `WITH` ambiguity was an accidental rule duplication; this is a property of
the SQL surface.

It is also **three-way**, which is why the obvious edits do nothing. `t.c` is
derivable by `columnIdentifier`'s optional `tableIdentifier` prefix, by
`nestedIdentifier`'s optional `DOT`, and by `columnExpr DOT identifier`
(ADR-0190 §SD11). Measured: removing `nestedIdentifier`'s `DOT` alone leaves
**264**; removing §SD11's alternative alone leaves **264**; removing both gives
**0**.

Removing both was measured end to end and **rejected**:

| | before | after |
|---|---|---|
| `bench_large` ambiguities | 264 | **0** |
| `bench_large` LL parse | 29.66 ms | **8.02 ms** (~3.5×) |
| `bench_medium` LL parse | 2.20 ms | 0.61 ms |
| **`applet_9kb` LL parse** | **4.11 ms** | **4.14 ms** |
| SLL fast path | — | unchanged |
| SLL fallback rate (corpus) | 42 | **45** |

Four reasons, in order:

1. **The real workload gains nothing.** The 9 KB applet moves 4.11 → 4.14 ms.
   The 264 only materialise on a 240-column qualified projection — a benchmark
   shape, not an authored one.
2. **The hoped-for win does not exist.** The SLL fallback rate does not improve.
   Those rejections are *context-sensitivity*, not ambiguity — a different
   mechanism, untouched by this. 86% of the corpus's SLL-rejects carry a dotted
   name, but so do 82 statements SLL accepts, so the dotted name is necessary
   and not sufficient.
3. **Nothing is broken that this would retire.** `LW_COMPONENT('aaa').MyField`
   parses, canonicalises and ships today — verified through the full pre-execute
   stage, which emits `tupleElement("LW_COMPONENT"('aaa'), 'MyField')`. That is
   §SD11's route working as designed.
4. **It costs four authoring forms**, one of them unexpected: `db.t.c.f`,
   `expr.Field` named tuple access (`LW_COMPONENT('a').MyField`, `f(x).field`),
   and `INSERT INTO t (n.a, n.b) SELECT …` — Nested column lists. Surviving:
   `t.c`, `db.t.c`, `t.1`, `t.*`, `tupleElement(…)`, `cluster('c', db.tbl)`.
   Plus ~6 consumer edits where `AllIdentifier()` becomes `Identifier()`.

Worth carrying forward if LL latency is ever the constraint again: on that path
the **`AS alias` costs more than the ambiguity does**. It drives 252
context-sensitivity escalations in `columnExpr` — 18 without the alias, 252 with
— which is 71% of the summed span. And the applet's own 6 residual ambiguities
are a third case again, in `columnExpr`/`columnsExpr` at spans of 3–27 tokens.

Note also that **grammar2 has no dot tuple access at all**: canonicalisation
lowers both spellings to `tupleElement(…)` before the validating grammar sees
them, so the canonical form already requires the function.

## Surfaces — Tier 1

| Surface | Change |
|---|---|
| `nanopass.Parse` / `ParseCanonical` | signature and error contract unchanged; prediction strategy internal |
| `nanopass.DFACacheStats` | signature unchanged; now reads the shared holders |
| `nanopass.MaxDFAStates` / `DFACheckInterval` | unchanged names and semantics; now set the shared holders |
| `antlr4utils.DFACache` | **new** — the ADR-0084 holder, generalised and exported |
| `grammar1.SharedDFA` / `grammar2.SharedDFA` | **new** — the one instance per grammar |
| `nanopass.PredictionStats` | **new** — SLL hits / LL fallbacks per grammar (§SD4) |

`env.scanBody` and `play.firstSyntaxError` are unexported; their behaviour
changes but no signature does.

The grammar repair (§SD5) changes generated surface in both grammar packages:

| Surface | Change |
|---|---|
| `grammar{1,2}.WithClauseContext` | **removed** — the rule is deleted |
| `grammar{1,2}.SelectStmtContext.WithClause()` | **removed** |
| `grammar{1,2}.QueryContext.Ctes()` | **removed** — moved down one level |
| `grammar{1,2}.SelectUnionStmtContext.Ctes()` | **new** |
| `grammar{1,2}.SelectUnionStmtItemContext.Ctes()` | **new** |

Every consumer of the removed names is in this repo and is updated here; the
compiler finds them all, because a deleted rule deletes its type and its
accessor. No `SelectUnionStmtContext` reference changes, which is the property
§SD5 chose the repair for.

## Alternatives

- **Repair the grammar instead of two-stage.** Not exclusive, and not
  sufficient: the repair leaves an applet parse at ~4 ms where two-stage reaches
  ~1 ms, and it does not make SLL succeed more often. Both are taken, in that
  order — §SD5 records the repair's own rejected options, including the two
  shapes tried and abandoned.
- **Switch parser technology** (PEG, hand-written recursive descent, goyacc,
  tree-sitter). Each costs a rewrite of 84 hand-written files carrying 943
  typed-context references, and none is a latency argument: DuckDB's PEG parser
  measures ~10× *slower* than the Bison parser it replaces and was adopted for
  runtime extensibility. tree-sitter additionally needs cgo, against this repo's
  `CGO_ENABLED=0` posture. Considered and set aside; see the background page.
- **Parse memo on input text.** Orthogonal and still worth having — 13 of 18
  `nanopass.Parse` calls in a stage are byte-identical to the immediately
  preceding one. It was measured at a further ~35% *after* this change, against
  ~80% before it, which is the argument for doing this first.
- **Set SLL globally with no fallback.** Fails four existing tests. Rejected.
- **Leave `env.scanBody` alone.** Caps the win at ~2.3×. Rejected.

## Consequences

### Positive

- One parse of the 9 KB fixture: 94–97 ms → **~1.0 ms**; 103 MB → 0.74 MB;
  1.36 M allocs → 8.1 K.
- The LL path — every parse SLL rejects — improves on its own: the same fixture
  goes 103 ms → **4.11 ms** under forced LL, and `Apply/medium`, which is
  SLL-rejected end to end, goes 107–125 ms → **55–77 ms** (§SD5).
- The `WITH` ambiguity is gone rather than avoided: exact-ambiguity counts drop
  by exactly one in every measured case, to zero for CTE-only statements.
- The accepted language is unchanged. Nine `WITH` spellings, including the
  regression-tested `… UNION ALL WITH … SELECT …` and `EXCEPT WITH …`, parse
  before and after.
- One pre-execute stage on that fixture: 3.05–3.10 s → **41–44 ms**; 3.28 GB →
  53 MB; 42.7 M allocs → 271 K.
- The ADR-0084 bypass at two seams is closed; the unbounded package global is no
  longer reachable from any parse this repo performs.
- Reversible: one constant returns every seam to LL-only.

### Negative

- **An SLL-rejected buffer gains only from the grammar repair**, not from
  two-stage: `benchMediumSQL` reports 4 SLL diagnostics, and two-stage alone left
  its stage cost unchanged (§SD5 has the figures). Two-stage is a win or a wash
  there, never (measurably) a loss — but "never a loss" rests on a failing SLL
  attempt staying cheap, which §SD2's inability to abort early makes less certain
  than the textbook recipe would.
- **264 exact ambiguities remain** in `bench_large`, none of them the WITH one.
  Its LL parse is still ~30 ms. They are examined in §SD6 and deliberately kept:
  removing them needs three grammar edits, costs four authoring forms, and moves
  the real applet by 0.03 ms. This ADR fixed the one that dominated; it did not
  audit the grammar.
- **SLL can in principle resolve an ambiguous decision to a different
  alternative than LL**, which the fallback does not catch. Measured at 0
  occurrences over 270 corpus statements (§Verification), but it is a property of
  the technique rather than a bug that was fixed, and it is the reason §SD4's
  counters exist.
- An SLL-rejected parse now costs SLL + LL. Worst case is a workload that is
  almost entirely SLL-rejected, which would be slower than today.
- Stage one cannot abort early (§SD2), so that worst case is worse than the
  textbook recipe would make it.
- Two grammar packages gain a hand-written file, so `generate.sh`'s sweep rule
  now matters to correctness rather than tidiness.
- The generated parsers are re-generated, so the diff is large (~2,400 lines per
  grammar) and cannot be produced in CI: it needs a hand-provisioned ANTLR jar
  and a JRE. Reviewing it means reviewing the `.g4` edit and trusting
  reproducibility, which was checked against an unmodified tree first.

### Neutral

- Gains concentrate on `WITH`-heavy buffers that SLL accepts. `Apply/small`
  does not move (no ambiguity to skip). `Apply/medium` does not move either
  (109–131 ms → 107–125 ms), and it is the more interesting case: it is
  `WITH`-led *and* SLL-rejected, so every parse in that stage now pays SLL and
  then LL. It comes out neutral rather than worse because a failing SLL attempt
  is cheap beside the LL one — but neutral is the honest reading, and this is
  the workload §SD5's deferred grammar repair exists for.
- Absolute figures move with DFA cache warmth: LL cannot warm on the ambiguous
  decision, SLL can, so repeated-run numbers improve for SLL and not for LL.

## Migration — Tier 1

No data migration and no call-site changes. Three mechanical steps:

- **M0 — the holder moves.** `nanopass`'s private `dfaCache` becomes
  `antlr4utils.DFACache`; `grammar1`/`grammar2` each gain a `SharedDFA`
  instance; `nanopass` points at those. `MaxDFAStates`, `DFACheckInterval` and
  `DFACacheStats` keep their names, positions and semantics.
- **M1 — the seams take two-stage.** `nanopass.Parse`, `ParseCanonical`,
  `env.scanBody` and `play.firstSyntaxError`.
- **M2 — the counters.** `nanopass.PredictionStats`.
- **M3 — the grammar repair.** Both `.g4` files, regenerated; then the six
  consumers the compiler names: `nanopass_scope` (the ctes harvest moves into
  `buildUnionScopes` and accumulates left to right), `resolve_names`
  (`scopeOfFirstSelect` takes the clause's parent, now a union or a union item),
  `inject_params_cte` (one walk replaces the query-level lookup and the
  selectStmt scan), `sqlcomplete` (the WITH-naming ancestor), `ast`
  (`convertQuery` reads the union's clause; `convertSelectUnionItem` and
  `convertParenthesisedUnion` park a nested clause on the head Select, keeping
  the AST shape), and `play_subquery` (the chain's clause is hoisted and the
  unit's range pulled in to start at the first arm, so subquery mode still ships
  the query without its WITH).

Consumers of `nanopass.DFACacheStats` see no change. A caller that had come to
rely on grammar1's package-global DFA cache being populated as a side effect of
`env.scanBody` would break; there is no such caller.

## Verification plan — Tier 1

The risk is silent: a parse that succeeds under SLL with a *different* tree. Two
checks address it directly, both run before proposing this.

- **Corpus differential.** Every `.sql` file in the repo, parsed under LL and
  under SLL, trees compared as s-expressions. Result over 146 files / 270
  statements that parse under LL: **228 identical, 42 SLL-rejected (→ LL),
  0 different trees.** The rejects are largely DataFusion/DuckDB-dialect trial
  files, so the ClickHouse-input fallback rate is below the 15.6% shown. This
  becomes a checked-in test, so a future grammar edit that introduces a genuine
  SLL divergence fails rather than ships.
- **The `dsl` suite passes** under two-stage — round-trip fidelity, semantic
  equivalence, grammar compatibility, goldens, fuzz corpus.
- **Benchmarks** `BenchmarkPlayPipelineApply` and `…ReparseFloor` pin the
  ratio; they already exist and need no change.
- **Naive-SLL control**: without the fallback, four tests fail. That the
  fallback is load-bearing is itself asserted, so a refactor that drops it is
  caught.

For the grammar repair specifically:

- **Language preservation.** Nine `WITH` spellings across all three positions —
  head, later arm (bare and parenthesised), subquery in FROM and in projection,
  `RECURSIVE`, mixed scalar-and-CTE, `EXCEPT WITH` — parse before and after.
  `TestRegressionUnionBranchWithNotQualified` is the checked-in guard for the one
  form an earlier attempt dropped.
- **Ambiguity count.** Under `PredictionModeLLExactAmbigDetection`, exactly one
  exact ambiguity disappears per case, and CTE-only statements reach zero. A
  repair that removed more or fewer would not be this repair.
- **Regeneration reproducibility.** Running `generate.sh` against an unmodified
  tree produced a byte-identical result, so the committed generated diff is
  attributable to the `.g4` edit alone.
- **The whole `public/…` and `apps/…` suite passes**, which is the real check on
  the CST relocation: scope building, name resolution, AST round-trip and play's
  subquery model all read the moved nodes.

## Status

Proposed 2026-08-18. Implemented in the working tree as one change: two-stage
prediction, the shared bounded cache, and the grammar repair. Flip to accepted
only after human review of §SD1's correctness argument, the differential result,
and the regenerated grammar diff.

## References

- [nanopass — the full-context prediction tax on `WITH`](../adr-background-work/nanopass-full-context-prediction-tax.md)
  — profile, per-decision attribution, grammar repair options.
- [ADR-0084](./0084-nanopass-antlr-dfa-cache-bounding.md) — the bounded DFA
  cache this generalises, and the SLL deferral this takes up.
- [ADR-0125](./0125-codeview-prepare-memo.md) §SD5 — named the suspect.
- [ADR-0192](./0192-nanopass-cost-profiling.md) — per-pass cost attribution; the
  pane that made the cost visible.
- [ADR-0002](./0002-nanopass-discipline.md) — why passes exchange text, which is
  what makes parse cost multiply.
- [Adaptive LL(\*) Parsing: The Power of Dynamic Analysis](https://www.antlr.org/papers/allstar-techreport.pdf)
  — Parr, Harwell, Fisher; the two-stage strategy and SLL's relationship to LL.
