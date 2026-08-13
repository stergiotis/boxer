---
type: explanation
audience: SQL practitioner working with leeway tables; engineer building leeway query tooling
status: draft
# reviewed-by: "@<handle>"   # fill in and uncomment when flipping to stable
# reviewed-date: YYYY-MM-DD  # fill in and uncomment when flipping to stable
---

> **Status: draft — pre-human-review.** This page states the three DQL
> contracts [ADR-0181](../adr/0181-leeway-dql-authoring-surface.md) §SD1 made
> normative. Where this page and the ADR disagree, the ADR is the record.

# The three leeway DQL contracts

Working with leeway data from SQL decomposes into three consumer intents,
and each intent is a *contract* on the output of an expression over leeway
columns:

- **F — filter (guard):** a `WHERE` predicate where a cheap superset answer
  is acceptable and index-prunable. False positives licensed, false
  negatives never.
- **X — extract:** an expression pulling attribute values into ordinary,
  opaque columns that downstream consumers use with no leeway awareness.
- **T — transform:** a `SELECT` list that *produces* leeway shape, suitable
  for `INSERT … SELECT` / `CREATE TABLE … AS SELECT` between leeway tables.

This page states the rules of each contract and the canonical transform
patterns. The executable idiom kernel lives in the
[array-idioms how-to](../howto/leeway-clickhouse-array-idioms.md), the
underlying model in the
[query algebra](./leeway-query-algebra.md), and the naming anatomy in
[leeway-column-names](./leeway-column-names.md). Lanes are quoted in the
`"section:column"` handle style; substitute your table's physical names.

## F — the guard contract

### The (S,N) model

Every predicate `P` over leeway columns abstracts to a pair (S, N) with
S ⇒ P ⇒ N: a *sufficient* and a *necessary* condition. The **guard is the N
side**. `has("string:model", '/my/model')` is a guard for "some attribute
has value `/my/model`": every matching row passes it, and some
non-matching rows may too. A guard in `WHERE` never loses a row the exact
predicate would keep — that is the whole contract, and it is what makes the
answer a *superset* rather than merely approximate.

### Grain

A guard answers at a *grain*, and false positives enter exactly where the
question's grain is finer than the guard's:

- `has(lane, x)` is **exact at row grain** ("some instance in this row
  carries x") and a **guard at attribute grain** ("the instance you are
  about to extract carries x").
- Conjunction across lanes erases correlation:
  `has("a:t", x) AND has("a:u", y)` admits rows where x and y sit at
  *different* attribute instances. The algebra names this loss precisely —
  it occurs at Or-reductions over conjunctions — and the exact form is a
  per-instance lambda (`arrayExists((p, q) -> p = x AND q = y, …)`), kept
  *beside* the guard, not instead of it (see
  [the sargable-guard idiom](../howto/leeway-clickhouse-array-idioms.md#existence-tests-with-a-sargable-guard)).

### Polarity

AND and OR compose guards pointwise. **Negation swaps S and N**: `NOT g` of
a guard `g` is no longer a guard for `NOT P` — it is an exact *rejector*
(false negatives possible, false positives never), and it stops pruning.
Index pruning serves positive polarity only; a skip index can only ever
answer "definitely not / maybe". Rewrite negative questions to positive
guards where possible, and treat any `NOT` around a guard as a full-scan
predicate.

### What prunes — an enumerated list

Prunable shapes are a syntactic enumeration, not a semantic property.
Verified (ClickHouse 26.5/26.7, `EXPLAIN indexes = 1`):

| shape | skip index that serves it |
| --- | --- |
| `has(lane, const)`, `hasAny(lane, [consts])`, `hasAll(lane, [consts])` | `bloom_filter` |
| `indexOf(lane, const) > 0` — but **not** the equivalent `!= 0` | `set(N)` |
| `countEqual(lane, const)` comparisons | `set(N)`, while per-granule distinct values stay ≤ N |
| scalar comparisons on plain lanes | `minmax` |

Lambdas past ClickHouse's single-lane pure-equality rewrite
(`arrayExists(v -> v = c, lane)` → `has`) never use indexes — multi-lane
lambdas, `LIKE`, arithmetic are all index-opaque. The composition rule
follows: **the lambda is the semantics, the constant-needle `has` beside it
is the pruner.** Wrapping both in one installed function body (ADR-0162
§SD3) keeps the pruning, because SQL UDFs are inlined before index
analysis.

The indexes themselves are emitted per table via `TableOptions`
(ADR-0181 §SD4); a guard on an unindexed lane is still correct, it merely
scans.

### Zero-install

The `WHERE` story works against a server with nothing installed: the
generated Presence/Validator/Filter artefacts
([ADR-0066](../adr/0066-leeway-dql-clickhouse-readback-generator.md)) and
every shape in the table above are ClickHouse built-ins. Only extraction
needs the installed `LW_` families. New guard sugar, if it ever ships,
must preserve this property; **no guard-sugar names ship today** — the
idioms plus the skip indexes are the guard surface, and names follow
demand (ADR-0181 §SD1).

### Measuring the slack

The guard-vs-exact false-positive factor is one comparison away:

```sql
SELECT countIf(<guard>) AS admitted,
       countIf(<exact>) AS matching,
       admitted / matching AS slack
FROM t
```

A slack near 1 means the guard is nearly exact; the algebra page
demonstrated 10× on an uncorrelated two-lane conjunction. Measure before
tightening: a slack of 1.05 needs no lambda at all if the consumer
tolerates supersets.

## X — the extract contract

The output of an extraction is an ordinary ClickHouse value; leeway-ness
ends at the expression boundary.

### Absence is a three-way choice

An attribute can be absent from a row. Extraction must pick one of three
representable outcomes, and the choice is part of the column's contract:

| form | absent yields | when |
| --- | --- | --- |
| type default | `''` / `0` — `lane[indexOf(tags, x)]`, index 0 hits the default | harmless when the default is out-of-band |
| `NULL` | `if(indexOf(tags, x) > 0, lane[…], NULL)`; generated artefacts emit `if(has(…), …, NULL)` for optional scalars | scalars where the default is a legal value |
| empty-array sentinel | `[]` | **forced** for non-scalars: ClickHouse forbids `Nullable(Array(…))`, so absent and present-empty collide (ADR-0066) |

The type-default form is indistinguishable from a stored default; say so
in the mart's documentation or use the `NULL` form. Note the leeway
refinement: a *present* attribute's list is never empty (positivity — an
empty list is absent, routed to a value-less section), so on leeway reads
`[]` always means absent.

### Aliasing under multi-membership, and the structural fast path

`indexOf`-based extraction returns the **first** match. Two distinct
consequences:

- A tag carried by several attribute instances extracts the first
  instance's value — in every form. The exact per-instance answer is a
  ragged question, not a scalar one.
- Under a **repeating membership** (one instance carrying the tag more
  than once — membership cardinality > 1), a bare `indexOf` against the
  flattened membership stream lands on a *stream* position, not an
  *instance* position. The correct form routes through the
  position→attribute map: `LW_RAGGED_PARENT_IDS(<cardcol>)` (the `m2v`
  argument of `LW_VALUE_BY_TAG_EQUAL` / `LW_LIST_BY_TAG_EQUAL` in the
  installed read-back family).

Whether the bare-`indexOf` fast path is legal is **structurally decidable
from the names**: the membership's `<role>card` column (`lrcard`,
`hrcard`, …) exists iff memberships may repeat. No `<role>card` column ⇒
membership cardinality ≡ 1 ⇒ stream positions are instance positions ⇒
bare `indexOf` is exact. This licence is what schema-aware tooling
(ADR-0181 §SD3) applies mechanically; apply it by hand only after checking
the column list.

### No new numeric semantics

Extraction emits engine arithmetic. Engine properties ride through
unchanged — the second-substrate trial's U5 (an Int64 overflow two of
three engines got silently wrong) is an engine property, and no extraction
surface pretends to fix it.

### Three renderings, by portability

The same read has three renderings; pick by target engine, not habit:

1. **Higher-order** — `arrayMap` / `arrayFilter` lambdas, the form the
   [array-idioms how-to](../howto/leeway-clickhouse-array-idioms.md)
   catalogues. ClickHouse and DuckDB carry it (DuckDB: `lambda x:` syntax,
   `list_position`, plus a coalesce for absent).
2. **Positional (lambda-free)** — the canonical mapping's entire read
   vocabulary is one form, `lane[indexOf(tags, x)]`: list indexing plus a
   list-position builtin. Any engine with those two carries it — DataFusion
   (no higher-order array functions at all) reads the packed layout this
   way, `array_element` over `array_position`, with an explicit `CAST`.
3. **Relational (no array functions)** — explode, then ordinary relational
   operators. The only rendering every SQL engine can run, and the one the
   second-substrate trial found undocumented:

   ```sql
   -- per-instance rows from a packed leeway table
   SELECT id, tag, v
   FROM t
   LEFT ARRAY JOIN               -- unnest / UNNEST elsewhere
       "string:model" AS tag,
       "string:short" AS v
   WHERE tag = '/my/model'       -- plain WHERE / GROUP BY / JOIN from here
   ```

   After exploding, absence is a missing row (no sentinel choice needed),
   grain confusion disappears (rows *are* attribute instances), and
   re-assembly is `groupArray` — order across streams is not guaranteed,
   so collect `(i, x)` tuples and sort when order matters. The exploded
   *table* of ADR-0171 §SD5 is this rendering made durable.

## T — the transform contract

### The closure rule

> A `SELECT` whose output column names parse under the leeway naming
> convention and satisfy the vertical-subsetting rule — plain columns
> freely, co-section groups whole — **is** a leeway table.
> `DiscoverTableFromColumnNames` is the mechanical witness.

Because names ride through projection unchanged, bare-identifier
subsetting and row filtering are *already closed*:
`SELECT <cols> FROM t WHERE …` of a leeway table is a leeway table,
provided the vertical subset keeps each section's lanes together (a value
lane without its membership machinery is not a table, it is a fragment).

### What breaks closure, and what re-admits it

Closure breaks exactly when an *expression* appears in the list: the
output column loses its self-describing name. A human cannot re-mint one
(`tv:symbol:lr:lr:u64:1247:::0::data` is not typeable), which is why
computed columns re-enter the closed set through the **constructor
family** (ADR-0181 §SD2): `LW_PLAIN`, `LW_TV`, `LW_TV_MEMB`,
`LW_TV_SUPPORT` are client-side authoring calls that expand to
`<expr> AS "<physical name>"` before the statement ships. Constructors
mint; they never install anything server-side, and a call that reaches a
raw endpoint fails loudly as an unknown function.

The worked example from the naming ground truth: one plain `u64` column
with delta encoding, light compression, and the `nominal` value aspect is
the single physical column `id:mycol:u64:47:D:0:` (v2 aspect segments,
ADR-0182) — one column, no companions, and a valid leeway table by
itself.

### Validation is two-staged

Constructors make columns; nothing yet says the *set* of columns is a
table. The transform contract's validation (ADR-0181 §SD5):

- **Static, from names alone** (`LwShapeCheck`): every output name parses;
  the discovered table passes the validator; section completeness holds
  per channel — a `val` without its membership lanes, a repeating channel
  without its `<role>card`, a dangling half of a co-section group are all
  rejections.
- **Runtime, from data** (the audit-query generator): the invariants
  statics cannot see — co-length equality across a section's lanes
  (`arraySum(card) = length(vals)` per row), cardinality positivity
  (`card ≥ 1`; an empty list is *absent*, not a value), membership-card
  sums consistent with membership lane lengths.

### Handles mint nothing; constructors resolve nothing

The duality rule, stated once: resolving an *existing* column is a
**handle** (`` `section:column` ``, ADR-0116); minting a *new* column is a
**constructor**. `INSERT INTO <existing leeway table>` therefore wants
handles in its column list — resolution against the target — while
constructors serve `CREATE TABLE … AS SELECT` and new marts. One column,
one spelling per direction.

### Wrapping is manual, for now

The SQL pipeline parses `SELECT` only. The `INSERT … SELECT` /
`CREATE TABLE … AS SELECT` wrapper is composed by hand *around* the
expanded `SELECT` (ADR-0181 §SD8 records the deferral and the fixed
direction). Two consequences worth knowing:

- CTAS applies **no codecs**: the encoding aspects in a minted name ride
  as recorded intent without taking effect. For a durable table with real
  `CODEC` clauses, compose the DDL through the generator
  (`leeway ddl compose`) and `INSERT … SELECT` into it.
- The transform pipeline's product is portable SQL text; where it runs is
  wherever SQL is authored (play, the CLI), not where it executes.

## Canonical transform patterns

Four shapes cover routine leeway→leeway work. All are `SELECT` bodies
under the closure rule; the DML/DDL wrapper is composed around them.

**Datamart projection — X made durable.** Extract into opaque names,
optionally re-admitting them as leeway-plain:

```sql
SELECT
    "id:id"                                   AS device_id,     -- opaque mart column
    LW_PLAIN("string:short"[indexOf("string:model", '/my/model')],
             'model-short', 's', 'item:oq')                     -- stays a leeway table
FROM devices
```

**Re-tagging / re-sectioning.** Value lanes ride through; names are
re-minted into the new section, membership lanes constructed by channel:

```sql
SELECT
    "sym:value"                          -- untouched lanes keep their names
    , LW_TV("legacy:value", 'symbol', 'value', 's')
    , LW_TV_MEMB("legacy:lr", 'symbol', 'low-card-ref')
FROM old_table
```

**Annotation overlay.** A co-section adds lanes on an *existing* section's
instance axis — secondary memberships, quality flags — without touching
the section it annotates. The overlay's lanes must be co-length with the
annotated section's (the audit queries check exactly this), and a
co-section group moves whole under vertical subsetting.

**Packed ↔ exploded.** The relational rendering made durable
(ADR-0171 §SD5): one `ARRAY JOIN` pass converts packed to exploded
(measured: 100M documents in ~51 s); `groupArray` with an order witness
converts back. The two are one schema in two representations — carrying
both costs ~1.7× the packed footprint, and nothing routes queries
automatically; the consumer names the table it reads.

## Reading list

- [array-idioms how-to](../howto/leeway-clickhouse-array-idioms.md) — the
  executable kernel behind F and X.
- [query algebra](./leeway-query-algebra.md) — axes, planes, the (S,N)
  abstraction, cost model.
- [leeway-column-names](./leeway-column-names.md) — name anatomy and
  handles.
- [ADR-0181](../adr/0181-leeway-dql-authoring-surface.md) — the authoring
  surface: constructors, extraction sugar, shape checking, skip-index
  options.
- [ADR-0171](../adr/0171-leeway-sql-read-surface.md) — the read surface;
  the exploded companion (§SD5).
- [ADR-0066](../adr/0066-leeway-dql-clickhouse-readback-generator.md) —
  generated per-kind artefacts; the index matrix behind the prunable-shape
  list.
- [ADR-0162](../adr/0162-leeway-co-ragged-function-pack.md) — the
  installed lane algebra; guard bundling (§SD3).
- [leeway-second-substrate trial](../trials/leeway-second-substrate/README.md)
  — the portability evidence behind the three renderings.
