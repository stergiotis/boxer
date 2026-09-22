---
type: explanation
audience: ClickHouse practitioner deciding whether a server-side SQL UDF can stand in for a client-side rewrite without losing primary-key pruning
status: draft
# reviewed-by: "@<handle>"   # fill in and uncomment when flipping to stable
# reviewed-date: YYYY-MM-DD  # fill in and uncomment when flipping to stable
---

> **Status: draft — pre-human-review.** Not verified; do not cite as authoritative.

# Primary-key pruning through a SQL UDF

A predicate written as a ClickHouse SQL user-defined function
(`CREATE FUNCTION … AS (x, tag_value) -> …`) prunes the primary key when, and
only when, three things line up: the analyzer inlines the body, the part of
the body that depends only on literals folds to a constant, and what is left
is a shape `KeyCondition` recognises. This page explains each of the three
with the server mechanism that decides it, using the `LW_ID_HAS_TAG` UDF
emitted by `public/identity/identsql` as the worked example, and records
what each shape costs. The decision that led here is the 2026-09-22 update
of [ADR-0106](../adr/0106-identity-fibonacci-tags-build-tag-retirement.md);
the predicate's meaning is in the
[tagged-id how-to](../howto/fibonacci-tagged-ids.md).

Everything about server internals below names the ClickHouse symbol that
implements it, so a reader can check it against the version they run. The
observations are dated; they were made on ClickHouse 26.8.1 (`clickhouse
local`) on 2026-09-22, and the mechanisms are those of the analyzer that has
been the default since 24.3.

## The predicate

Tagged ids of one tag value form a contiguous `UInt64` range: the tag's
Fibonacci code in the high bits, then the body. Filtering by a known tag is
therefore a range predicate on the key, and any spelling of it that reaches
`KeyCondition` as a range on `id` prunes. The client-side route already does
this: the nanopass `identsql.ExpandPass` folds a literal tag value into
`id BETWEEN lo AND hi` before the statement ships. The question this page
answers is whether the server-side twin — the same name resolved by a
`CREATE FUNCTION` — can do the same when the SQL is not routed through the
pass.

The body has to compute `lo` and the body span from the tag value. That is
the Zeckendorf decomposition of the tag value: greedy over the Fibonacci
numbers F(47)…F(2), digit `d_k = intDiv(r, F(k))`, remainder `r % F(k)`,
each digit being 0 or 1 because the greedy remainder stays below
F(k+1) < 2·F(k). The digits sit MSB-aligned, F(2) at bit 63, so the lowest set
bit of the assembled code is twice the comma bit, and the comma bit is the
body divisor. With `code` and `div` in hand the predicate is

```sql
intDiv(x, div) = intDiv(code, div) + 1
```

which needs no upper bound and no `UInt64 - 1` (that subtraction widens to
`Int64` and has bitten the decoders in this family before, per ADR-0106).

## 1. Inlining: what the analyzer does with a SQL UDF

`QueryAnalyzer::resolveFunction` looks a call's name up in
`UserDefinedSQLFunctionFactory`, obtains the stored lambda
(`tryGetLambdaFromUserDefinedSQLFunctions`), clones it, binds the call's
argument nodes to the lambda's parameter names in a fresh lambda scope
(`expression_argument_name_to_node`), resolves the body in that scope, and
replaces the call node with the resolved body. Two consequences matter here.

**Aliases inside the body are scoped to the call.** `resolveLambda` runs
`QueryExpressionsAliasVisitor` over the lambda scope's own alias map, so an
`AS _lw_r46` inside the body is visible to the rest of that body and to
nothing outside it. Two calls in one query, each defining `_lw_r46`
differently, coexist; an outer query that itself defines `_lw_r46` is not
disturbed. This is what lets the body name each remainder once. The
pre-analyzer path inlined UDFs at AST level and leaked the aliases into the
query scope: under `enable_analyzer = 0` the same two calls fail with
`MULTIPLE_EXPRESSIONS_FOR_ALIAS` (observed 2026-09-22).

**Every reference to a parameter is a copy of the argument.** The bound
argument node is substituted per reference, and the copies are resolved
independently. For a body that reads its parameter *k* times the resolved
tree carries *k* copies of the argument subtree, and nesting multiplies: a
helper UDF threading a six-field tuple state through itself grew the resolved
query tree ×15 per nesting level and hit the 500 000-node query-tree limit
at depth four (2026-09-22). Nested helper UDFs are therefore not a way to
build a loop, and the alias chain is not optional sugar: without it each
remainder is re-spelled from the parameter and the body is quadratic in the
number of digits. `ActionsDAG` deduplicates nodes by name, so the copies are
an analysis-time cost, not an execution-time one.

## 2. Folding: the gate a constant subtree has to pass

After the body is inlined, `resolveFunction` evaluates a call whose arguments
are all constants and replaces it with a `ConstantNode` — provided the
function reports `isSuitableForConstantFolding()`, which every deterministic
scalar function does by default. The gate is the *all constants* test:
`all_arguments_constants` is true only when each argument is a
`ConstantNode` (or the `__getScalar` of an already evaluated scalar
subquery). A `LambdaNode` argument is typed `DataTypeFunction` and is never
a constant node. Consequently a higher-order call — `arrayFold`, `arrayMap`,
`arraySum(arrayMap(…))` — over entirely literal data is not folded at
analysis time; the analyzer's fallback for that branch,
`getConstantResultForNonConstArguments`, is implemented by a handful of
functions and not by the array family. `ActionsDAG::addFunction` applies the
same test at plan time (every child must carry a `ColumnConst`), with the
same outcome.

The measurement that pins this: `WHERE id = arrayFold((acc, f) -> acc + f,
[1, 2, 3]::Array(UInt64), toUInt64(…))`, every input a literal, reaches
`KeyCondition` as `Condition: true` and reads every granule. A body that
computes the same value with `intDiv`, `modulo`, `bitAnd`, `bitNot`,
`greatest` and `toUInt64` — scalar functions with constant arguments — folds
bottom-up to a single `ConstantNode`. The domain guard
`toUInt64(tag_value) BETWEEN 1 AND 4294967295` folds to the constant `1`,
which `KeyCondition` treats as an always-true conjunct.

So the arithmetic in the body is constrained to scalar functions. The greedy
Zeckendorf loop is unrolled into 46 terms, one per Fibonacci number a
`uint32` tag value can carry, and the loop-carried remainder is threaded by
alias (§1). Nothing else in the body is a lambda.

## 3. Recognition: what `KeyCondition` does with what is left

After folding, the filter is `intDiv(id, C) = K` with `C` and `K` constants.
`KeyCondition` accepts an atom on a key column wrapped in functions that
report monotonicity (`canConstantBeWrappedByMonotonicFunctions`, gated on
`hasInformationAboutMonotonicity()`), and pushes the constant through the
chain. `FunctionBinaryArithmetic::hasInformationAboutMonotonicity` lists
`plus`, `minus`, `multiply`, `divide` and `intDiv` — not `intDivOrZero`,
whose otherwise identical predicate reads every granule (`Condition: true`,
2026-09-22). For an unsigned dividend and a positive unsigned constant
divisor `getMonotonicityForRange` reports the function always monotonic, so
`KeyCondition::matchesExactContinuousRange` holds and
`MergeTreeDataSelectExecutor::markRangesFromPKRange` takes the binary-search
branch over marks rather than the generic exclusion search (the
`IndexBinarySearchAlgorithm` profile event increments for this form exactly
as for the literal `BETWEEN`). A non-empty function chain makes the
constraint a `RANGE` rather than a `POINT` even for `=`, which is the right
reading: one small code covers a range of ids.

The `BETWEEN` spelling of the same body prunes identically. The `intDiv`
spelling was chosen because it needs no upper bound and therefore no
subtraction on a `UInt64`.

## 4. Costs

Observed 2026-09-22 on ClickHouse 26.8.1, `clickhouse local`, one part of
1 500 000 rows (30 tag values × 50 000 bodies), `ORDER BY id`,
`index_granularity = 8192`, tag value 12 (code `101011`, so `C = 2^58`,
`K = 43`):

| Predicate on `id` | `KeyCondition` | Granules | Body size |
| --- | --- | --- | --- |
| Literal `BETWEEN` (nanopass output) | two ranges on `id` | 8 / 184 | — |
| Decode-and-compare UDF (pre-2026-09-22 body) | `true` | 184 / 184 | ~1.5 KB |
| `arrayFold` body, correct but lambda-bearing | `true` | 184 / 184 | ~0.6 KB |
| Unrolled scalar body, remainders re-spelled | `intDiv(id, 2^58) in [43, 43]` | 8 / 184 | ~12.7 KB |
| Unrolled scalar body, alias chain (emitted) | `intDiv(id, 2^58) in [43, 43]` | 8 / 184 | ~3 KB |

Every prunable form selects the same granules as the literal control. The
3 KB and 12.7 KB bodies are the same DAG once analysed (dedup by name); the
difference is analysis-time tree size and, for the 12.7 KB form, the ability
to run under the pre-analyzer path. Per-row cost with a non-literal tag
value — 46 `intDiv` and 46 `modulo` on the tag value against the previous
body's `arrayMap` over the id's tag width — was not measured; the choice was
made for pruning, and a claim about row throughput would need a trial under
`doc/trials`.

## 5. The decoder's cost: why a per-row array loses to lookup tables

The pruning story covers a *known* tag. Decoding an id's tag value — for
display, or to group by tag — is the other direction, and it is where the
scheme's SQL cost actually sits. The tag value is the Fibonacci-weighted
popcount of the digit bits, Σ bit_j · F(j+2) over the positions above the
comma. The first decoder wrote that as
`arraySum(arrayMap(j -> bitTest(x, 63 - j) * W[j + 1], range(width - 1)))`.
Per row that materialises an array of `width - 1` elements, replicates the
captured `x` once per element, and runs `bitTest`, `arrayElement` and the
multiply per element before the reduction: for a 20-bit tag, roughly twenty
times the data volume of the input column.

The lambda-free alternative splits the digit word into fixed-width chunks
and looks each chunk up in a constant table whose entry `b` holds the sum of
the weights of the bits set in `b` at that chunk's positions. Six 8-bit
chunks need six `arrayElement` calls into 256-entry tables; twelve 4-bit
chunks need twelve into 16-entry tables. Two guards make the tables
sufficient: a comma below position 47 cannot belong to a `uint32` tag value
(the greedy code's last digit is always set, so width 48 already means at
least F(48) > 2^32 − 1), and a width-47 code can still exceed the maximum,
so the value guard stays.

Measured 2026-09-22 on ClickHouse 26.8.1, `clickhouse local`, one part of
17.75 M ids across 269 tags of width 13–27, `max_threads = 1`, best of three
on a shared machine (batches drifted up to 30 %; the ranking did not):

| Expression over `id` | Wall time | Per row above the read floor |
| --- | --- | --- |
| `id` (read floor) | 0.31 s | — |
| `LW_ID_TAG_WIDTH` (comma search) | 0.73 s | ~25 ns |
| 8-bit chunks, six lookups (emitted UDF) | 1.8 – 2.9 s | ~85 – 145 ns |
| 4-bit chunks, twelve lookups (macro expansion) | 3.1 – 6.1 s | ~155 – 330 ns |
| unrolled 46-term `bitTest` sum | 7.9 s | ~430 ns |
| `arraySum(arrayMap(range))` (previous decoder) | 12.1 – 13.2 s | ~670 – 730 ns |
| `bitShiftRight(id, 48)` (fixed-width stand-in) | 0.29 s | < 5 ns |

The macro takes the 4-bit form because an expansion is statement text and
cannot alias its digit word or its sum — a macro lands in the query's own
scope — so the sum is spliced twice (once into the guard) and the digit word
once per chunk: ~8 KB per call at 4 bits, and it would be ~45 KB at 8 bits,
against a 256 KB default `max_query_size`. The UDF aliases both and carries the 8-bit
tables in ~12 KB of DDL. The DAG deduplicates the spliced copies by name, so
the two forms differ at run time only in chunk count.

One consequence for `LW_ID_HAS_TAG` with a *column* tag value: the UDF's
range derivation (§ The predicate) costs ~46 `intDiv` and `%` per row on the
tag value and measured 4.8 s on the same table, against ~1.8 s for decoding
the id and comparing. The range form is kept because the literal case, which
prunes, is the one that matters; a per-row filter by a column tag over a
large table is better written as `LW_ID_TAG_VALUE(id) = tag`, and a decode
that recurs belongs in a `MATERIALIZED` column or a projection, paid once.

## 6. What this does not change

- **The macro's non-literal fallback stays decode-and-compare.** A macro
  expands into the query's own scope, where the alias chain of two calls
  would collide (§1); only the UDF gets a private scope. The two forms agree
  on every id and tag value — the server-truth lane in `identsql` checks
  both against the Go encoder's goldens.
- **Validation differs.** The pass rejects a literal tag value of 0 or beyond
  `uint32` at expansion time; the UDF's guard folds to false and the
  predicate is quietly false. A hand-written statement against a UDF-only
  server gets no such error.
- **The analyzer is assumed.** Under `enable_analyzer = 0` one call works and
  prunes, two calls in a query fail on the leaked aliases. The unrolled
  alias-free body would run there; it was not emitted.
- **Mechanisms are named, not frozen.** Whether a function reports
  monotonicity, and whether a lambda argument can ever count as constant,
  are the two facts a future version could change. Both are checked by the
  `TestServerTruth_UdfHasTagPrunes` lane on every run with a `clickhouse`
  binary on the path, which is the guard for this page's claims.

## Related

- [ADR-0106](../adr/0106-identity-fibonacci-tags-build-tag-retirement.md) —
  the scheme, the split contract, and the dated update that adopted this body.
- [fibonacci-tagged-ids.md](../howto/fibonacci-tagged-ids.md) — minting,
  splitting and querying the ids; the two routes (pass and UDF) side by side.
- [leeway-sql-read-surface.md](./leeway-sql-read-surface.md) — where the
  `LW_ID_*` family sits among the other SQL vocabulary.
