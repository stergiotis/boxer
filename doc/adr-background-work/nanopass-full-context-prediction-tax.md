---
type: explanation
audience: package maintainer
status: draft
# reviewed-by: "@<handle>"     # fill in and uncomment when flipping to stable
# reviewed-date: YYYY-MM-DD    # fill in and uncomment when flipping to stable
---

> **Status: draft — pre-human-review.** Measurements taken 2026-08-17 and
> 2026-08-18 against the tree at those dates, on one developer machine
> (32 logical cores). Absolute timings move with cache warmth and machine;
> the ratios are what this page asks you to believe. It feeds
> [ADR-0196](../adr/0196-nanopass-two-stage-sll-parsing.md), which took **both**
> the cheap fix and the grammar repair — but *not* the repair this page ranked
> first (see §Options). Once that ADR is accepted it is authoritative and this is
> a snapshot.

# nanopass — the full-context prediction tax on `WITH`

A `play` Run spends seconds rewriting a statement the server answers in 90 ms
([ADR-0192](../adr/0192-nanopass-cost-profiling.md) §Context). Two documents
already recorded a *shape* for that cost without naming its cause:
[ADR-0084](../adr/0084-nanopass-antlr-dfa-cache-bounding.md) §Alternatives
observed that SLL prediction "is faster … could be revisited purely for parse
latency" and left it there; [ADR-0125](../adr/0125-codeview-prepare-memo.md)
§SD5 listed "full-context LL prediction on the `WITH … AS` ambiguity" as one of
two *unmeasured suspects* behind a 3.5 ms tokenize of 180 bytes.

This page closes that out. The suspect was the right one, the mechanism is
specific and nameable, and the cheap fix is worth roughly two orders of
magnitude. It also costs out the grammar repair that the cheap fix makes
optional.

## Where the time goes

CPU profile of one `BenchmarkPlayPipelineApply/applet_9kb` op against the 9 KB
`runtime_timeline_applet.sql` fixture (146.01 s of samples, 64.15 s wall):

| | % total CPU |
|---|---|
| `runtime.gcBgMarkWorker` | **53.3%** |
| `grammar1.Query` — all parsing | 42.1% (90.2% of the mutator) |
| └ `AdaptivePredict` | 42.0% |
| └ **`execATNWithFullContext`** | **40.5% — 96.4% of all prediction** |
| `nanopass.Parse` / `env.Extract` | 25.4% / 16.2% |
| everything that is not a parse | **4.6%** |
| lexing | **0.2%** |

Three things follow, and each contradicts a plausible guess:

- **Lexing is not the cost** — 750 µs of a 198.8 ms parse.
- **CST construction is not the cost** — it does not register.
- **Allocation is not a separate cost.** GC is 53%, but it is *downstream* of
  prediction: `NewATNConfig` is 37.3% of objects and `JMap.Put` 18.2% (55%
  together), and **75.7% of all objects allocate under
  `execATNWithFullContext`**. Rewriter string building is 0.75% of bytes.

**The mechanism.** ANTLR never writes full-context predictions into the DFA —
they are context-dependent by construction, so there is nothing stable to
memoise. The dominant cost is therefore re-simulated on **every** parse and is
**invisible to cache warming**. Warm LL plateaus around 192 ms on this fixture;
warm SLL is about 2 ms. ADR-0084's bounded cache is working exactly as
designed (0 resets across 8 Runs, 23–1163 states against an 8192 threshold) and
cannot help, because the states it holds are not the ones being recomputed.

## The defect

In [`grammar1/ClickHouseParserGrammar1.g4`](../../public/db/clickhouse/dsl/grammar1/ClickHouseParserGrammar1.g4):

```antlr
line  36:  query      : setStmt* ctes? selectUnionStmt ;
line  66:  ctes       : WITH RECURSIVE? withItem (COMMA withItem)* ;
line  84:  selectStmt : withClause? projectionClause ... ;
line 107:  withClause : WITH RECURSIVE? withItem (COMMA withItem)* ;
```

`ctes` and `withClause` have **byte-identical right-hand sides**, and both are
reachable at the same input position (`query` → `selectUnionStmt` →
`selectStmtWithParens` → `selectStmt`). A leading `WITH` can be consumed by
either. The two derivations are indistinguishable at *any* lookahead, so the
decision is resolvable only by simulating to the end of the WITH clause.

ANTLR says so itself. Stock `DiagnosticErrorListener` under
`PredictionModeLLExactAmbigDetection`, no patch required:

```
AMBIGUITY  decision=4  rule=query  tokens[11..1690]  exact=true  alts={1, 2}
```

`exact=true` is a genuine ambiguity rather than mere context sensitivity, and it
spans **1,680 of the fixture's 1,709 tokens**.

### Evidence it is this and nothing else

Instrumenting `execATNWithFullContext` per decision, one warm parse of the
fixture:

| decision | rule | full-ctx calls | ATN configs | share |
|---|---|---|---|---|
| **4** | **`query`** | **1** | **495,135** | **98.2%** |
| 115 | `columnExpr` | 3 | 7,491 | 1.5% |
| 102 / 136 / 124 / 100 | others | 40 | 1,438 | 0.3% |

One decision, fired **once**, is 98.2% of all full-context work.

Scaling isolates the trigger. Leading `WITH` present or absent, size held
against it:

| leading WITH | bytes | full-ctx calls | ATN configs | LL | SLL |
|---|---|---|---|---|---|
| none | 16 | 0 | 0 | 14 µs | 15 µs |
| 1 CTE | 64 | 1 | 4,034 | **2.79 ms** | 65 µs |
| 12 CTEs | 560 | 1 | 43,722 | 24.8 ms | 393 µs |
| none | 927 | **0** | **0** | 2.46 ms | — |

A 64-byte query costs 2.8 ms because it has one CTE; a 927-byte query with no
`WITH` costs nothing extra. **Size is not the trigger; the `WITH` is.**

An independent probe on 2026-08-18 reproduced the shape from the other side,
comparing three families at matched nesting (private warm DFA cache, second
parse timed):

| shape | n | bytes | LL | SLL | ratio |
|---|---|---|---|---|---|
| `WITH cN AS (SELECT …), …` | 12 | 667 | 15.18 ms | 0.20 ms | **75×** |
| `WITH (a + b*N) AS cN, …` | 12 | **252** | 8.75 ms | 0.09 ms | **103×** |
| nested subqueries, no `WITH` | 12 | 893 | 0.50 ms | 1.12 ms | ~1× |

The scalar-`WITH` row is the sharpest statement of the defect: 252 bytes for
8.75 ms, against 893 bytes of comparably nested subquery for 0.5 ms. Note also
that the no-`WITH` family shows **no SLL advantage at all** — where there is no
ambiguity there is nothing for SLL to win.

## It is a repo-local regression

`grammar0`, the upstream lineage, had:

```antlr
ctes       : WITH namedQuery (COMMA namedQuery)* ;   // name AS ( query )
withClause : WITH columnExprList ;
```

Distinguishable after `WITH ident` by whether `AS (` follows — bounded
lookahead, which SLL resolves. `grammar1` unified both forms into one
`withItem` rule; the grammar comment at lines 52–55 explains why, and the reason
is sound (ClickHouse permits CTE and scalar forms mixed in one `WITH`, in any
order). That change was correct. It incidentally made the two rules identical.

**`grammar2` carries the same duplication** (lines 75 and 116), so
`ParseCanonical` pays the same tax.

## A second parse seam, off the books

`nanopass.Parse` and `ParseCanonical` are not the only places a parser is built.
Four call sites construct one directly:

| site | goes through `nanopass.Parse`? | bounded DFA cache? |
|---|---|---|
| `nanopass.Parse` | — | yes |
| `nanopass.ParseCanonical` | — | yes |
| `env.scanBody` (`env_extract.go`) | **no** | **no** |
| `play.firstSyntaxError` (`play_parse.go`) | **no** | **no** |

The last two use the grammar's **unbounded package-level global** — the growth
ADR-0084 exists to prevent. ADR-0084 assumed two seams and said so ("the fix
lives at the two seams … without touching call sites"); there are four.

This is not a footnote. `Pass.Run` calls `env.Extract` per registry unit, and
`env.Extract` calls `scanBody`, which stands up its own parser. Counting both
seams gives **28 parses per pre-execute stage** — 18 `nanopass.Parse` plus 10
through `env.Extract` — at 198.8 ms each, which is 5.57 s of a 6.37 s stage.
(ADR-0192 and the benchmark comment say ~34; 28 is the measured figure with the
seams separated. The discrepancy does not change any conclusion.)

Leaving `env.scanBody` on LL caps the achievable win at about 2.3× instead of
about 80×, because it is then the only thing left paying the tax.

## What the cheap fix buys

Two-stage parsing — attempt SLL, fall back to LL on error — is the standard
ANTLR remedy for exactly this. Ranked, each measured on top of the prior:

| # | change | stage cost | note |
|---|---|---|---|
| 1 | two-stage in `nanopass.Parse` | 6524 → 2583 ms | floor 195.9 → **2.4 ms (82×)**, ~20 lines |
| 2 | same in `env.scanBody`, + the ADR-0084 bypass | 2583 → **157 ms** | combined **41.5×**; allocs 43.2 M → 0.78 M |
| 3 | parse memo on input text | further **−35%** | 13 of 18 calls are byte-identical to the immediately preceding one |

An independent A/B on 2026-08-18, both seams patched, `-benchtime 10x -count 3`:

| benchmark | LL (HEAD) | two-stage | |
|---|---|---|---|
| `ReparseFloor` — one parse, 10 KB | 93.6–97.1 ms · 103 MB · 1.36 M allocs | **1.00–1.40 ms · 0.74 MB · 8.1 K allocs** | **~78×** |
| `Apply/applet_9kb` — full stage | 3.05–3.10 s · 3.28 GB · 42.7 M allocs | **41–44 ms · 53 MB · 271 K allocs** | **~73×** |
| `Apply/medium` (2 CTEs, SLL-rejected) | 109–131 ms | 107–125 ms | ~1× |
| `Apply/small` | 0.39–1.35 ms | 0.40–0.97 ms | ~1× |

The `medium` row is worth dwelling on, because an earlier throwaway patch
reported 74.8–85.4 ms for it and the shipped change does not. The difference is
the fallback: the throwaway version set SLL on `env.scanBody` with **no** LL
retry, so on this SLL-rejected buffer it silently scanned a partially recovered
tree. Falling back is correct and costs the difference. A buffer SLL rejects
gets no benefit from this change — it is the case the grammar repair below is
for.

The spread between "157 ms" and "38 ms" for the same fixture is DFA cache
warmth across repeated runs, not a disagreement: LL cannot warm (the states are
never written), SLL can, so the SLL figure improves with repetition and the LL
figure does not. Both runs agree the ratio is one to two orders of magnitude.

Gains concentrate where the buffer leans on `WITH`. `small` does not move.

## What the cheap fix risks, and what was checked

SLL is strictly weaker than LL: it may report a syntax error on input LL
accepts, and in principle it may resolve an ambiguous decision to a different
alternative. The first is handled by the fallback; the second would not be, so
it was measured.

- **Differential over the repo's SQL corpus** — 146 files, 270 statements that
  parse under LL. Result: **228 identical trees, 42 SLL-rejected (→ LL),
  0 different trees.** The rejects are largely DataFusion/DuckDB-dialect trial
  files rather than ClickHouse input, so the real-world fallback rate is below
  the 15.6% this corpus shows.
- **Naive SLL without the fallback fails 4 tests**, all `mismatched input …
  expecting '.'` on database-qualified names. The fallback is load-bearing.
- **The full `dsl` suite passes** under two-stage, including round-trip
  fidelity, semantic equivalence, grammar compatibility and goldens.

Two runtime notes, both found the hard way:

- **`antlr4-go` v4.13.1 has no profiling API** — no `DecisionInfo`, no
  `SetProfile`, no profiling simulator. The per-decision tables above came from
  hand instrumentation. Anyone targeting a grammar decision in Go is otherwise
  blind.
- **`BailErrorStrategy` panics** in the Go port (a nil token out of
  `RecoverInline`), so stage one must use the ordinary error listener and
  forgo the early abort. The cost is that a rejected parse runs error recovery
  before falling back.

## The grammar repair, costed

The cheap fix makes this optional rather than unnecessary: it removes the tax
from the SLL path, but a query that SLL rejects still pays the full ambiguity
on the LL retry. `benchMediumSQL` is exactly that case — `WITH`-led *and*
SLL-rejected.

Crucially, **the grammar fix and SLL are orthogonal**. Fixing the grammar does
not make SLL succeed more often:

```
benchMediumSQL        SLL errors: 4
  skeleton (460 B)    SLL errors: 4
  fragment active     SLL errors: 0
  fragment recent     SLL errors: 0
  fragment <outer>    SLL errors: 4   <- the JOIN/BETWEEN/CASE body, not the WITH
applet_9kb            SLL errors: 0
```

What the grammar fix does is make the **LL fallback cheap when it fires**.

### Constraints any repair must hold

| # | constraint | source |
|---|---|---|
| C1 | mixed items in one clause: `WITH x AS (SELECT…), 1 AS y SELECT…` | why `withItem` was unified (grammar comment 52–55) |
| C2 | a leading `WITH` scopes over a **whole UNION**, not just the first arm | grammar comment 61–65 |
| C3 | `WITH RECURSIVE` keeps working everywhere, including parenthesised arms | 79 uses in the repo |
| C4 | grammar2 needs the same edit — and it is a *validator*, so over-acceptance is worse there | measured duplication at lines 75/116 |
| C5 | `withClause` is only ever exercised by parenthesised UNION arms | measured |

C5 came out of tracing which rule actually consumes each `WITH`. Because ANTLR
resolves the ambiguity to alternative 1, a top-level `WITH` *always* lands in
`ctes`; `withClause` is reached only in the parenthesised arm, where
`selectStmtWithParens: selectStmt | (LPAREN selectUnionStmt RPAREN)` bypasses
`query`.

### Options

**Option 1 — hoist `ctes?` into `selectUnionStmt`, delete `withClause`.**

```antlr
query           : setStmt* selectUnionStmt ;
selectUnionStmt : ctes? selectStmtWithParens selectUnionStmtItem* ;
selectStmt      : projectionClause fromClause? ... ;   // withClause? deleted
// rule withClause deleted
```

Kills the ambiguity by construction: one rule accepts `WITH`, and at
`selectUnionStmt` the choice is `ctes` (`WITH`) against `selectStmtWithParens`
(`SELECT` / `LPAREN`) — LL(1). Both call sites are served, since the paren arm
already routes through `selectUnionStmt`. C2 is arguably better satisfied than
today, the `WITH` now sitting literally on the union node. No language
widening, so C4 is clean. **Cost:** `ctes` moves from `query` to
`selectUnionStmt` for *every* query, so all five consumers are touched on the
common path, and it interacts with a known subtlety: query-level CTEs are
*siblings* of the scope node rather than children, so a walk anchored at
`scope.Node` misses the WITH-expressions.

**Option 2 — route the paren arm through `query`, delete `withClause`.**

```antlr
selectStmtWithParens : selectStmt | (LPAREN query RPAREN) ;
selectStmt           : projectionClause ... ;   // withClause? deleted
// rule withClause deleted
```

Same kill, LL(1) for the same reason, and the CST changes **only inside
parenthesised arms** — the smallest consumer impact of any option.
**Rejected on C4:** `query` carries `setStmt*`, so `(SET x=1 SELECT …)` becomes
legal. Benign in grammar1, which recognises; wrong in grammar2, whose job is to
reject non-canonical SQL.

**Option 2′ — as Option 2, with a narrow rule instead of `query`.**

```antlr
selectStmtWithParens : selectStmt | (LPAREN parenQuery RPAREN) ;
parenQuery           : ctes? selectUnionStmt ;
selectStmt           : projectionClause ... ;   // withClause? deleted
// rule withClause deleted
```

Everything Option 2 gives without the widening, so C4 holds. **Cost:** one new
rule and context type; `ctes` gains a second parent (`query` and `parenQuery`),
so consumers must match on type rather than on parent.

**Option 3 — delete `ctes?` from `query`, keep `withClause`.** Smallest diff,
and it does kill the ambiguity. **Rejected on C2:** the leading `WITH` would
land inside the first `selectStmt`, so `WITH x AS (…) SELECT … UNION ALL SELECT
…` scopes `x` to the first arm only. That is a misparse, and precisely what
`ctes` sits at query level to prevent. It also has the largest CST churn.

**Option 4 — narrow `withClause`'s language (restore a grammar0-style split).**

```antlr
ctes       : WITH RECURSIVE? withItem (COMMA withItem)* ;
withClause : WITH columnsExpr (COMMA columnsExpr)* ;
```

**Rejected — does not kill it.** `withItem : namedQuery | columnsExpr`, so
`WITH 1 AS x SELECT …` remains derivable by both rules and stays exactly
ambiguous. Killing it fully needs `columnsExpr` removed from `withItem`, which
regresses C1 — the mixed-form bug the unification fixed.

**Option 5 — semantic predicate gating `withClause`.**

```antlr
selectStmt : {!p.ctesAlreadySeen()}? withClause? projectionClause ... ;
```

**Rejected — likely counterproductive.** Context-dependent predicates evaluated
during prediction inhibit DFA reuse for exactly those decisions, which is the
mechanism that makes warm parses cheap. It also introduces mutable parser state,
against the grammar-as-source-of-truth discipline, and would have to be mirrored
in grammar2.

**Option 6 — no grammar change, rely on SLL.** The measured baseline rather
than a fix. Kept as the comparison, and taken by ADR-0196.

### Where a repair would land

Estimated ceiling, **unverified**: the fixture's non-decision-4 full-context
work is 8,929 configs of 504,064, and decomposed fragments summed to 6.5 ms /
9,003 configs — so a fixed grammar should put an LL parse around **10–30 ms**,
down from 198–259 ms. SLL alone already reaches 2.46 ms, so the repair is
insurance for the fallback path, not a substitute for the cheap fix.

### What was actually built, and why not the option ranked here

This page's ranking — Options 1 and 2′ as finalists, leaning 2′ — was **wrong on
both counts**, and the reasons are worth keeping because both are easy to repeat.

**2′ was picked for a blast radius it does not have.** Confining the change to
`selectStmtWithParens` sounds local, but three *other* rules spell
`LPAREN selectUnionStmt RPAREN` — `columnsExprSubquery`, `columnExprSubquery`
and `tableExprSubquery`. Leaving them alone breaks `FROM (WITH … SELECT …)`;
rerouting them changes what ~15 consumers find when they walk children for a
`SelectUnionStmtContext`, and those consumers **fail silently rather than failing
to compile**. The criterion that selected 2′ actually disqualifies it.

**Option 1, as written here, narrows the language.** Deleting `withClause`
outright removes `… UNION ALL WITH … SELECT …`, which has a dedicated regression
test (`TestRegressionUnionBranchWithNotQualified`) and corpus entries — measured,
not guessed: HEAD accepts the form, and eleven tests fail when it goes.

**The property that mattered was misidentified.** It is not "exactly one rule
accepts WITH"; it is "**no single decision chooses between two alternatives that
derive the same tokens**". Those differ, and the difference is the whole repair:
`ctes` can appear at three positions and stay LL(1) at each, because each is
preceded by a distinct token.

So ADR-0196 §SD5 took Option 1 **plus a second `ctes?` on
`selectUnionStmtItem`**:

```antlr
query               : setStmt* selectUnionStmt ;
selectUnionStmt     : ctes? selectStmtWithParens selectUnionStmtItem* ;
selectUnionStmtItem : ((UNION|EXCEPT|INTERSECT) (ALL|DISTINCT)? ctes? selectStmtWithParens) ;
selectStmt          : projectionClause … ;
// rule withClause deleted
```

Nothing is given up, `selectUnionStmt` is spelled the same everywhere, and the
compiler names every affected consumer. Measured after: the applet's LL parse
goes **103.15 ms → 4.11 ms**, CTE-only statements reach **zero** exact
ambiguities, and each measured case loses **exactly one** — the WITH one.
`Apply/medium`, SLL-rejected end to end, goes **107–125 ms → 55–77 ms**.

The estimate below was 10–30 ms for the applet; the repair beat it.

**What it did not fix.** `bench_large` still reports **264** exact ambiguities,
none of them the WITH one and none examined, which is why its LL parse is still
~30 ms. The residual counts in the tables above are that, not measurement noise.

Both grammars took the same edit. Regeneration needs a hand-provisioned ANTLR
4.13.2 jar and a JRE, per
[`grammar0/generate.sh`](../../public/db/clickhouse/dsl/grammar0/generate.sh) —
present on a developer machine, not pinned, and absent from the airgap bundle, so
CI checks the generated files only as committed source. Reproducibility was
verified against an unmodified tree first, so the committed diff is attributable
to the `.g4` edit alone.

There is no newer ANTLR to move to: the tool is at **4.13.2** (2024-08-03) and
the Go runtime at **v4.13.1** (2024-05-15), both the latest releases — so the
missing profiling API and the panicking `BailErrorStrategy` are not upgrade
candidates.

## Adjacent ideas that were measured and rejected

**Trim `RECURSIVE` support to one rule.** Would resolve `WITH RECURSIVE …` in
two tokens, but plain `WITH …` stays exactly ambiguous, and the two forms cost
the same today (5,916 configs / 5.16 ms without, 6,680 / 3.57 ms with). The
9 KB fixture contains **zero** occurrences of `RECURSIVE`, so the expensive case
gains nothing. Dropping `RECURSIVE?` from `withClause` would additionally break
`(WITH RECURSIVE … ) UNION ALL (…)`, and `WITH RECURSIVE` appears 79 times in
the repo. It narrows the trigger set, not the ambiguity.

**Decompose: parse each SELECT separately, parse the outer structure once.**

| | bytes | LL | full-ctx configs | SLL |
|---|---|---|---|---|
| whole statement (today) | 10,031 | **259.3 ms** | 504,064 | **2.46 ms** |
| outer skeleton only | 3,408 | 21.5 ms | 37,489 | 639 µs |
| 9 fragments, summed | ~7,200 | 6.5 ms | 9,003 | 1.59 ms |
| **split total** | | **28.0 ms** | | **2.23 ms** |
| | | **9.3× faster** | | **1.11× — nothing** |

Splitting does not reduce bytes parsed (10,605 against 10,031); it wins by
shrinking the *span* of the ambiguous decision, whose cost is superlinear in
span. So decomposition and SLL attack the identical cost by different means, and
once SLL removes the ambiguity there is nothing left to exploit. One detail
worth keeping: fragment `ev` is 237 bytes yet costs 2.136 ms with 7,768 configs
— a second, smaller local ambiguity that decomposition does *not* fix and SLL
takes to 60 µs. Splitting shrinks spans; it does not remove ambiguities.

The second half of the idea — parse the outer structure once — is separable,
survives SLL, and is the same lever as the parse memo, which delivers it for
~30 lines with output proven byte-identical. Building decomposition instead
would also have to contend with three measured obstacles: `InjectParamsAsCTE`
prepends CTE definitions mid-run so any precomputed split is invalidated; six
passes need cross-fragment scope (`resolve_names`, `expand_columns`,
`qualify_tables`, `dynamize_columns`, `selection_conditions`,
`inject_params_cte`), and `ResolveColumnNames` is one of only three passes that
actually changed the fixture; and rewritten fragments need splice bookkeeping
back to the right offsets.

## Reproducing

```sh
go test -tags="$(cat ./tags)" -run xxx -benchtime 10x -count 3 -benchmem \
  -bench 'BenchmarkPlayPipelineApply|BenchmarkPlayPipelineReparseFloor' \
  ./public/db/clickhouse/dsl/nanopass_test/
```

`BenchmarkPlayPipelineReparseFloor` is one bare parse of the fixture;
`Apply/applet_9kb` is one full pre-execute stage. Their ratio is the parse
count. Prediction mode is compared by pointing a parser's `Interpreter` at a
private `ParserATNSimulator` and calling `SetPredictionMode`; the ambiguity
report needs only ANTLR's stock `DiagnosticErrorListener` under
`PredictionModeLLExactAmbigDetection`.
