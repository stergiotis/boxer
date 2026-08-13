---
type: explanation
audience: package maintainer
status: draft
# reviewed-by: "@<handle>"     # fill in and uncomment when flipping to stable
# reviewed-date: YYYY-MM-DD    # fill in and uncomment when flipping to stable
---

> **Status: draft — pre-human-review.** An analysis, not a decision. Claims
> about boxer's current behaviour were measured against the tree on
> 2026-08-13 with a throwaway probe and carry file references. Claims about
> ClickHouse's pipe operators come from the upstream issue, pull request, and
> the merged stateless test `04613_pipe_operators.sql`, read on 2026-08-13;
> they were **not** verified against a running 26.8 server, because none was
> available. Effort figures are estimates, not measurements.

# ClickHouse pipe operators (`|>`) and boxer's SQL stack (August 2026)

ClickHouse 26.8 adds GoogleSQL-style pipe operators: a query may be followed
by a chain of `|>` steps, each of which wraps everything before it into a
subquery. This note works out what accepting that syntax would mean for
`public/db/clickhouse/dsl` and for the tools built on it — chiefly
[`apps/play`](../../apps/play/) — and costs three separable goals: parsing
pipe queries, rewriting ordinary queries into pipe form, and building
visualisation around the pipe structure.

The short form: the third goal is cheaper than it looks and the second is
more expensive than it looks. Pipe syntax is pure sugar — the server lowers
each stage into a nested subquery and the resulting AST is identical to the
hand-written nested form — so boxer needs no new IR, no new scope model, and
no new AST node kinds. What it needs is one lexer token, a suffix rule in
grammar1, and one lowering pass; after that every existing pass, the
CST→AST converter, and play's panels work on pipe queries unchanged. Going
the other way (nested → pipe) is a partial function, not a total one, and is
best built as a second unparser over the existing AST rather than as a token
rewriter. Visualisation is well-positioned because play's Flow panel already
computes the clause-level dataflow that a pipe chain is a linear notation
for.

## 1. What the feature is

Sources: [issue 69364](https://github.com/ClickHouse/ClickHouse/issues/69364),
[PR 111151](https://github.com/ClickHouse/ClickHouse/pull/111151), and the
merged test `tests/queries/0_stateless/04613_pipe_operators.sql`. The test is
the most precise specification of the three and is quoted from throughout.

```sql
FROM orders
|> WHERE cancelled = 0
|> AGGREGATE sum(amount) AS total GROUP BY customer
|> ORDER BY total DESC
|> LIMIT 3
```

Operators implemented upstream:

| Operator | Meaning | Nested equivalent |
| --- | --- | --- |
| `WHERE e` | filter; after `AGGREGATE` it acts as `HAVING` | `SELECT * FROM (…) WHERE e` |
| `SELECT [DISTINCT] list` | replace the projection | `SELECT list FROM (…)` |
| `EXTEND list` | add columns | `SELECT *, list FROM (…)` |
| `SET c = e` | replace a column | `SELECT * REPLACE (e AS c) FROM (…)` |
| `DROP c` | remove a column | `SELECT * EXCEPT (c) FROM (…)` |
| `AS a` | name the current stage | alias on the subquery |
| `AGGREGATE list [GROUP BY keys]` | aggregate; grouping columns come first in the output | `SELECT keys, list FROM (…) GROUP BY keys` |
| `DISTINCT` | de-duplicate | `SELECT DISTINCT * FROM (…)` |
| `ORDER BY …` | full clause, incl. `ALL`, `WITH FILL`, `INTERPOLATE` | as written |
| `LIMIT n [OFFSET m]`, `OFFSET m` | as written | as written |
| `UNION`/`INTERSECT`/`EXCEPT` | binds the **whole** preceding query | set operation |
| all `JOIN` forms, `ARRAY JOIN`, comma-join | left side is the stage input | as written |

Four properties matter for the design:

- **Lowering is exact.** The test asserts via `EXPLAIN SYNTAX oneline = 1`
  that a pipe chain and its nested spelling produce the same AST. There is no
  pipe-specific semantics to model.
- **A `SETTINGS` clause may end any stage**, and stays attached to that
  stage's generated wrapper.
- **Set operations do not chain freely.** A `|>` may continue after a set
  operation only when the last operand is parenthesised;
  `… |> UNION ALL SELECT 'zed' |> WHERE …` is a syntax error.
- **`SELECT` becomes optional after `FROM`.** `FROM orders WHERE amount > 200
  ORDER BY customer` is a complete query in 26.8. This is shipped in the same
  PR but is a *separate* language change from `|>`, and it carries almost all
  of the parsing difficulty — see §4.2.

Not covered by any pipe operator: `PREWHERE`, `LIMIT BY`, `QUALIFY`,
`WITH TOTALS`, `TOP`, `SAMPLE`, `FINAL`. Those remain expressible only in the
head query. This asymmetry is what makes the nested→pipe direction partial.

## 2. What boxer does with pipe SQL today

Measured 2026-08-13 with a throwaway probe against
`public/db/clickhouse/dsl/nanopass` at HEAD.

**`|>` is a lexer error, not a parse error.** The grammar1 lexer defines
`CONCAT: '||'` but no single `|`
([`grammar1/ClickHouseLexer.g4:273`](../../public/db/clickhouse/dsl/grammar1/ClickHouseLexer.g4)),
and there is no catch-all rule. ANTLR reports `token recognition error at:
'|>'` and **drops the characters from the token stream**:

```
=== FROM orders |> WHERE amount >= 250 |> ORDER BY amount
      lexer error 1:12 token recognition error at: '|>'
      lexer error 1:35 token recognition error at: '|>'
  tokens: FROM IDENTIFIER("orders") WS WS WHERE IDENTIFIER("amount") GE …
  nanopass.Parse: ERR syntax error: 1:0: mismatched input 'FROM' expecting
                  {SELECT, SET, WITH, '('}; 1:12: token recognition error …
  ClassifyStatementKind: unknown
```

Three consequences, each of which is a distinct piece of work:

1. **`nanopass.Parse` rejects the query**, because it collects lexer
   diagnostics as well as parser ones
   ([`nanopass_parse.go`](../../public/db/clickhouse/dsl/nanopass/nanopass_parse.go)).
   Everything downstream — the canonicalize pipeline, `ConvertCSTToAST`,
   scope resolution, param-slot extraction — is unreachable.

2. **`analysis.ClassifyStatementKind` returns `KindUnknown`**, which
   consumers are contractually required to treat as *mutating*. So a pipe
   query is not merely unparsed; it is treated as potentially destructive by
   play's dispatch policy
   ([`play_dispatch_policy.go:65`](../../apps/play/play_dispatch_policy.go))
   and refused by the Flow and lineage panels
   ([`play_flow_model.go:111`](../../apps/play/play_flow_model.go),
   [`play_flow_lineage.go:41`](../../apps/play/play_flow_lineage.go)). That
   is fail-safe and therefore fine, but it means the leading-keyword scan
   tier needs `FROM` adding when the FROM-first form lands, or every
   `FROM`-headed query stays classified as mutating.

3. **The highlight contiguity invariant breaks.** `HighlightLex` documents
   that "spans cover the input contiguously in source order"
   ([`highlight/dsl_highlight_lex.go`](../../public/db/clickhouse/dsl/nanopass/highlight/dsl_highlight_lex.go)).
   Measured on `FROM orders |> WHERE amount >= 250 |> ORDER BY amount`:
   19 spans, 49 of 53 bytes covered, two 2-byte gaps at the `|>` positions.
   The editor path gap-fills deliberately (ADR-0130), so in the SQL editor
   the operator renders unstyled. The CodeView path does **not** gap-fill
   ([`widgets/codeview/sql.go:70`](../../public/thestack/imzero2/egui2/widgets/codeview/sql.go)),
   so in any read-only SQL CodeView — doc panes, previews, applet bodies —
   the `|>` glyphs are simply absent from the render, with no error.

A related current-state fact: **the FROM-first form fails independently of
pipes.** `FROM orders SELECT customer, amount` lexes cleanly and still fails
with `mismatched input 'FROM' expecting {SELECT, SET, WITH, '('}`, because
`query: setStmt* ctes? selectUnionStmt` and `selectStmt` requires
`projectionClause`
([`ClickHouseParserGrammar1.g4:33,67`](../../public/db/clickhouse/dsl/grammar1/ClickHouseParserGrammar1.g4)).

## 3. The surface that has to move

```
ClickHouseLexer.g4  ×3  (grammar0/1/2 — byte-identical copies, verified by md5)
        │  add PIPE: '|>';
        ▼
grammar1 parser      accept `|>` chains          ← goal (a)
        │
        ▼
LowerPipeOperators   pipe → nested, new pass     ← goal (a), enables everything below
        │
        ├─► CanonicalizeFull ─► grammar2 ─► ast.Query ─► existing passes, play panels
        │
        └─► ast.Query.ToPipeSQL()  nested → pipe ← goal (b)
                    │
                    ▼
            play: editor gutter, Flow duality,   ← goal (c)
                  per-stage cardinality, diff pane
```

Notes on the mechanics:

- The three grammar directories each hold their own copy of
  `ClickHouseLexer.g4`, and the three copies are byte-identical (md5
  `0b1cee3b…`). `grammar0/generate.sh` regenerates all three from their local
  copies. A token addition must be applied to all three or the `.tokens`
  files drift.
- Regeneration needs a hand-provisioned ANTLR 4.13.2 jar and is a manual
  step; the airgap bundle pins deserve a check before this lands.
- **grammar2 should stay pipe-free.** It is the canonical-only grammar whose
  parse errors are the structural proof that normalisation ran. Canonical
  means nested, so pipes are lowered before grammar2 ever sees them. This is
  the same discipline the canonicalize passes already follow.

## 4. Goal (a) — parse and use pipe queries

### 4.1 The `|>` chain itself

Cheap, and unambiguous by construction: a new token in prefix position
cannot collide with anything. Sketch:

```antlr
PIPE: '|>';                                   // lexer, all three copies

query: setStmt* ctes? selectUnionStmt pipeOp*;
pipeOp
    : PIPE WHERE columnExpr                                    # PipeWhere
    | PIPE SELECT DISTINCT? columnExprList COMMA?              # PipeSelect
    | PIPE EXTEND columnExprList COMMA?                        # PipeExtend
    | PIPE SET settingLikeAssignList                           # PipeSet
    | PIPE DROP identifier (COMMA identifier)*                 # PipeDrop
    | PIPE AS identifier                                       # PipeAs
    | PIPE AGGREGATE columnExprList COMMA? groupByClause?      # PipeAggregate
    | PIPE DISTINCT                                            # PipeDistinct
    | PIPE orderByClause                                       # PipeOrderBy
    | PIPE limitClause (OFFSET columnExpr)?                    # PipeLimit
    | PIPE OFFSET columnExpr                                   # PipeOffset
    | PIPE (UNION|EXCEPT|INTERSECT) (ALL|DISTINCT)? selectStmtWithParens  # PipeSetOp
    | PIPE (GLOBAL|LOCAL)? joinOp? JOIN joinExpr joinConstraintClause     # PipeJoin
    | PIPE arrayJoinClause                                     # PipeArrayJoin
    | PIPE COMMA joinExpr (COMMA joinExpr)*                    # PipeCommaJoin
    ;
```

with `settingsClause?` admitted at the tail of each alternative. Risks are
small and local: the suffix loop adds no left recursion, and adaptive
prediction cost should be flat in the number of stages. Two details worth
pinning in tests rather than reasoning about: the `PipeSetOp` restriction
(a following `|>` is legal only when the operand is parenthesised — upstream
rejects the unparenthesised continuation), and whether `AGGREGATE` needs to
become a keyword token. Today it lexes as `IDENTIFIER`; a contextual match on
the identifier text avoids reserving a word that existing queries may use as
a column name, and grammar1's `alias: IDENTIFIER` rule means adding it to
`keyword` would silently forbid `SELECT x AGGREGATE` as a bare alias.

The nesting guard needs a thought: `MaxNestingDepth = 128`
([`nanopass_guard.go`](../../public/db/clickhouse/dsl/nanopass/nanopass_guard.go))
counts quote-aware bracket depth on the *input*. A 130-stage pipe chain has
zero parentheses on input and 130 levels after lowering. Either cap stage
count at lowering time or re-run the guard on the lowered text.

### 4.2 The FROM-first form — a separate, harder milestone

This is where the upstream test spends most of its length, and it is worth
treating as its own milestone rather than bundling it with `|>`. The
ambiguities upstream had to resolve:

- `FROM t select` — alias, or the start of a `SELECT` clause? Upstream rule:
  only the alias that *ends* the FROM clause can be the keyword; a quoted
  `` `select` `` never is; a table *named* `select` keeps its own alias.
- `FROM sampled SAMPLE 1 OFFSET 0` — sample offset or query-level `OFFSET`?
  Upstream requires an explicit `SELECT` in that position and rejects the
  bare form.
- `FROM (…) AS o (name, total)` — a column alias list after the tables,
  before the (optional) `SELECT`.

grammar1 is unusually well placed here, and for a reason its own header
comment already states: `keywordForAlias` was removed, so
`alias: IDENTIFIER` and `SELECT` is a keyword token that can never be a bare
alias. The hardest upstream ambiguity therefore cannot arise in boxer. The
price is a deliberate divergence: `FROM select s WHERE s.id = 1` (a table
named `select`) parses in ClickHouse and would not in boxer — consistent with
the divergence grammar1 already accepts for `FROM t system`, and worth
recording rather than fixing.

Estimate: `|>` alone is a day or two of grammar work plus regeneration;
FROM-first is a week-shaped problem dominated by the `SAMPLE … OFFSET` case
and by writing the negative tests.

### 4.3 The lowering pass

`LowerPipeOperators` walks the `pipeOp*` suffix and emits the nested form.
It has to run **before** `CanonicalizeFull` — which is registered at
`StagePreExecute` Order 50, currently first — so Order 25 or similar in
`passreg.Default`. Once it is in place, nothing else in the stack needs to
know pipes exist: grammar2, `ConvertCSTToAST`, `SelectScope`, param slots,
macro expansion, and every play panel operate on the nested output.

Properties: idempotent (a lowered query has no `|>` left), `Reads`/`Writes`
= `Body`, `Produces` a no-pipes form tag. Verifiable by `AssertProperties`
against the upstream corpus.

One design consequence is worth stating plainly because it is what makes the
whole feature usable before 26.8 is deployed anywhere: **if boxer lowers by
default and only sends raw pipe SQL when explicitly opted in, pipe queries
run against today's servers.** The parse-and-lower path needs no server
support at all. A version gate is then needed only for the opt-in
pass-through — and nothing in `public/db/clickhouse` or `apps/play` currently
reads `version()`, so that gate does not exist yet and would have to be
built.

## 5. Goal (b) — rewriting ordinary queries into pipe form

The two directions are not symmetric and should not share an implementation.

**Lowering (pipe → nested)** is total, mechanical, and — unusually —
verifiable against the vendor. `EXPLAIN SYNTAX oneline = 1` on the pipe form
and on boxer's lowered output must agree, and the repo already has that
harness: `TestExplainSyntaxEquivalence` / `TestExplainDifferentialPerPass` in
[`passes_test/nanopass_passes_canonicalization2_explain_test.go`](../../public/db/clickhouse/dsl/nanopass/passes_test/nanopass_passes_canonicalization2_explain_test.go),
`CLICKHOUSE_ENDPOINT`-gated. (That suite is known to be stale against the
local server — diff against a clean worktree before reading failures as
regressions.)

**Raising (nested → pipe)** is a partial function. The easy part is a single
`Select`: FROM → WHERE → AGGREGATE/GROUP BY → HAVING (becomes a second
`WHERE`) → SELECT → ORDER BY → LIMIT, in that order. Where it stops being
mechanical:

- **Clauses with no pipe spelling** — `PREWHERE`, `LIMIT BY`, `QUALIFY`,
  `WITH TOTALS`, `TOP`, `SAMPLE`, `FINAL`. These can stay in the head query
  when they belong to the source, but a raiser must be able to *decline*
  rather than emit something wrong. The `Conditional` combinator and the
  `PassDiscardOutput` marker already give the framework for a pass that
  declines.
- **Subquery flattening is the actual value.** `SELECT … FROM (SELECT … FROM
  t WHERE …) WHERE …` is precisely a pipe chain, and unnesting it is the
  transformation a reader benefits from. It is safe only when the inner query
  is a plain single `Select` — not a set operation.
- **Joins need a name for the left side**: `|> AS a |> JOIN b ON …`. Alias
  synthesis must not collide with existing names in scope.
- **Prefer `EXTEND` / `SET` / `DROP` over a full `SELECT` list.** Per the PR,
  `SET` *is* `* REPLACE` and `DROP` *is* `* EXCEPT`, so
  `SELECT * REPLACE (x AS y) FROM t` → `FROM t |> SET y = x` is a purely
  syntactic mapping needing no catalog. Choosing `EXTEND` over `SELECT`
  otherwise needs to know the input columns, which the raiser generally does
  not — so the honest default is: map the `* REPLACE` / `* EXCEPT` shapes,
  and emit `SELECT` for everything else.

**Where raising should live.** Not in the token rewriter. Raising *reorders*
clauses wholesale, and `TrackedRewriter`'s documented semantics make that
risky — partially overlapping replaces panic at `GetText`, and no existing
pass moves a clause rather than replacing it in place. The natural seam is
the AST: `ConvertCSTToAST` already produces a structural `ast.Query` from
grammar2, and `Query.ToSQL()` already unparses it
([`dsl/ast/dsl_ast_unparse_sql.go`](../../public/db/clickhouse/dsl/ast/dsl_ast_unparse_sql.go)).
Raising is a **second unparser** — `Query.ToPipeSQL()` — plus a thin `Pass`
wrapper that runs canonicalize → parse grammar2 → AST → `ToPipeSQL`.

The cost of that choice, and it is a real one: **the AST round trip discards
comments and formatting.** So "rewrite to pipe form" is a transform, not an
idempotent formatter, and should be surfaced as a preview or diff rather than
an in-place edit of the user's buffer. A comment-preserving raiser built on
token moves is possible but much fiddlier; the repo's own guidance is to ship
the light cut and record the deferral rather than gate on the hardest part.

A pure round-trip property gives the raiser trustworthiness without a server:
`raise` then `lower` must equal `canonicalize` of the original.

## 6. Goal (c) — visualisation

The observation that makes this cheap: play's Flow panel (ADR-0153,
[`play_flow_model.go`](../../apps/play/play_flow_model.go)) already builds a
clause-level dataflow graph of a query from the grammar1 CST and renders it
with layeredgraph, left-to-right. **A pipe chain is that graph, linearised.**
Pipe syntax is not a new visualisation problem; it is a second notation for a
model play already computes. Four candidates, cheapest first:

1. **Stage gutter in the editor.** One `|>` starts one stage; show the stage
   index and its role (filter / project / aggregate / order / limit) in the
   gutter. The ADR-0130 highlight seam already carries per-line sections and
   the gutter is already a CodeView. Needs the new highlight category from
   §2 anyway.

2. **Flow ↔ pipe duality.** For a pipe query the Flow graph is a straight
   line, one node per stage — the clearest reading the panel can produce.
   Conversely, clicking a Flow node should scroll the editor to the stage
   that produced it. `flowBuilder.snipRange` already records a source snippet
   and byte range per node, so the mapping mostly exists; what is missing is
   stage identity. This is the highest-value item: it turns the Flow panel
   from a picture of the query into a navigable index of it.

3. **Per-stage cardinality.** This is the thing pipe syntax enables that
   nested SQL does not: **every prefix of a chain is itself a runnable
   query.** Truncate at stage *k*, execute, show the row count next to that
   `|>`. Nothing else in the stack offers a per-clause row count without an
   `EXPLAIN` plan. Costed: *N* stages means *N* queries, so it belongs behind
   an explicit action with a stage cap, reusing the existing endpoint and
   dataset plumbing. Caveats: prefix truncation is invalid across a set
   operation, and a mid-chain `SETTINGS` binds to its own stage.

4. **Rewrite preview pane.** Nested and pipe side by side with stage-aligned
   highlighting — hovering a pipe stage highlights the corresponding subquery
   level. This makes the raiser legible and doubles as its manual test
   surface.

One thing to keep separate: play's reactive query graph (ADR-0097) is
node-level, and pipe stages are intra-node. Merging them would muddle the
graph's shape-reject semantics. Stages should stay a within-node concern.

## 7. Verification

- **Upstream's own test file is a ready-made conformance corpus.**
  `04613_pipe_operators.sql` holds roughly 110 statements, including negative
  cases annotated `-- { clientError SYNTAX_ERROR }`. Importing it as a
  parser-acceptance fixture — every positive statement parses under
  grammar1, every annotated one fails — is the cheapest broad coverage
  available and needs no server. The FROM-first section will need
  adjustment for the deliberate `alias: IDENTIFIER` divergence (§4.2).
- **Differential lowering** against `EXPLAIN SYNTAX oneline = 1` on a 26.8
  server, in the integration lane (`//go:build integration`, per AGENTS.md
  §Build & test).
- **Raiser round trip** — `raise ∘ lower == canonicalize`, pure, no server.
- **`AssertProperties`** for both new passes against the corpus.

## 8. Risks and open questions

- **26.8 is not released and the repo has no server-version detection.**
  Measured: no `version()` read anywhere under `public/db/clickhouse` or
  `apps/play`. Lowering-by-default makes this a non-blocker for goals (a) and
  (b); an opt-in pass-through needs a gate built from scratch.
- **`AGGREGATE` as a keyword.** Reserving it would break bare aliases named
  `aggregate` under grammar1's `alias: IDENTIFIER` rule. A contextual match
  avoids that but costs grammar clarity. Needs a decision.
- **Nesting guard interaction** (§4.1) — lowering multiplies depth.
- **Lexer triplication** — one token, three files, one manual ANTLR run.
- **Comment loss in the raiser** (§5) — acceptable if surfaced as a preview,
  not if wired to an in-place edit.
- **Not investigated:** how `text2sql` / text2dsl (ADR-0139) should treat
  pipe form as a generation target, and whether sqlapplet books
  ([`apps/sqlapplet/`](../../apps/sqlapplet/)) should be allowed to author
  pipe SQL. Both are downstream of the parser landing and can wait.

## 9. A possible cut

Ordered so that each milestone is independently useful and none gates on the
hardest part of the next.

- **M0 — lex and parse `|>`.** Token in all three lexer copies, `pipeOp*`
  suffix in grammar1, highlight category, `FROM` added to the statement-kind
  keyword scan. Parse-only; execution unchanged. Fixes the vanished-glyph
  bug in CodeView as a side effect.
- **M1 — `LowerPipeOperators`**, registered ahead of `CanonicalizeFull`.
  Everything downstream starts working on pipe queries with no further
  change. Upstream corpus as the acceptance fixture.
- **M2 — FROM-first optional `SELECT`.** The ambiguity-heavy half, separable
  and deferrable.
- **M3 — `Query.ToPipeSQL()` + `RaiseToPipeForm` + preview pane** in play.
- **M4 — Flow-panel stage duality; per-stage cardinality behind an action.**

M0+M1 is the load-bearing pair: it is what makes pipe SQL a first-class input
to the whole stack, and it is the part with the strongest verification story.

This touches a public surface (the accepted SQL dialect), carries a
migration (grammar regeneration across three packages), and needs a stated
verification approach — so it warrants an ADR rather than landing as a
sequence of commits. The next free number at the time of writing is 0184.
