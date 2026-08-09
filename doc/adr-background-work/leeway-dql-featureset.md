---
type: explanation
audience: package maintainer
status: draft
# reviewed-by: "@<handle>"     # fill in and uncomment when flipping to stable
# reviewed-date: YYYY-MM-DD    # fill in and uncomment when flipping to stable
---

> **Status: draft — pre-human-review.** The analysis behind
> [ADR-0181](../adr/0181-leeway-dql-authoring-surface.md): ground truth,
> design space, and rejected options for a leeway data-query-language (DQL)
> surface over ClickHouse. Decisions live in the ADR, not here. Code claims
> were verified by reading the tree at the time of writing; emitted names and
> DDL are quoted from committed goldens, not re-generated.

# Working with leeway data from SQL — a DQL featureset analysis

## 1. The question

Leeway data lands in ClickHouse as self-describing physical columns. Three
distinct things a SQL-fluent consumer wants to do with it:

- **(a) Filter** — predicates in `WHERE`, where a cheap superset answer
  ("maybe") is acceptable and index-prunable: predicates with false positives,
  never false negatives.
- **(b) Extract** — pull attribute values out of the shredded shape into
  ordinary, opaque columns in a `SELECT` list, so that built-in functions,
  BI tools, and data marts can consume them with no leeway awareness.
- **(c) Transform** — map one leeway table to another in SQL: the `SELECT`
  list *produces* leeway shape (tagged sections or leeway-plain columns),
  suitable for `INSERT … SELECT` / `CREATE TABLE … AS SELECT`.

The acceptance criterion driving all three: a senior SQL practitioner must be
able to do routine leeway work **without Go in the loop and without ever
typing a physical column name**. Today (c) is impossible without Go — a
physical name like `tv:symbol:lr:lr:u64:2q:0:0:0::data` cannot feasibly be
composed by a human — and (a)/(b) exist but are split across three packages
with different discoverability and installation stories
([ADR-0171](../adr/0171-leeway-sql-read-surface.md) is about exactly that gap
on the read side).

Scope guard: this is about SQL-embedded vocabulary and authoring-time
rewriting, deliberately **not** a query language.
[ADR-0022](../adr/0022-leeway-lwq-flwor-query-language.md) (lwq, FLWOR)
remains proposed-never-built, and
[ADR-0066](../adr/0066-leeway-dql-clickhouse-readback-generator.md) already
positioned the read-back generator as "not lwq". Nothing below changes that.

## 2. Ground truth: the physical name is the schema

Everything in aspect (c) — and the ergonomics gap in (a)/(b) — reduces to the
naming convention, so its actual mechanics are the load-bearing facts.
[leeway-column-names](../explanation/leeway-column-names.md) explains the
anatomy at reader level (and the handle short form); what follows is the
composer's view. The grammar lives in one file:
`public/semistructured/leeway/ddl/lw_ddl_gen_naming_human.go`
(`HumanReadableNamingConvention`, implementing `common.NamingConventionI`).

Two layouts exist:

```
plain  (7 tokens):  <prefix>:<name>:<ctype>:<encHints>:<valueSem>:<rowCfg>:<streamGroup>
tagged (11 tokens): tv:<section>:<column>:<role>:<ctype>:<encHints>:<useAspects>:<valueSem>:<rowCfg>:<coSectionGroup>:<streamGroup>
```

- **Prefixes** are the plain item types: `id`, `ts`, `ro`, `lc`, `tx`, `oq`
  (opaque), plus `tv` for tagged. There is no generic "pv" prefix — the item
  type *is* the prefix.
- **Aspects are three closed, independent vocabularies**, each serialized as a
  base62-encoded bitmask segment: `encodingaspects` (24 entries — the only
  ones that affect DDL: `LowCardinality(...)` wrapping and the `CODEC(...)`
  clause), `valueaspects` (62 entries — pure metadata, e.g.
  `scale-of-measurement-nominal`), `useaspects` (58 entries — **section-level,
  tagged-only**; plain names have no useAspects segment at all).
- The trailing `::data` in the example above is not a subcolumn suffix — it is
  an empty `coSectionGroup` followed by the streaming group `data`. Both are
  free-form table/section-level keys.
- Support and membership columns (`lr`, `lrcard`, `len`, `card`, …) are
  **machine-chosen** from the section's `MembershipSpecE` and canonical type;
  their roles, types and encoding hints are never authored per column.
- A worked minimal example: one plain `u64` column named `mycol` with
  delta-encoding + light compression and the `nominal` value aspect is one
  physical column, no companions:
  `id:mycol:u64:2k:2:0:` → `"id:mycol:u64:2k:2:0:" UInt64 CODEC(Delta,ZSTD(3))`.
  A single plain column is a valid leeway table (`common.TableValidator`
  passes vacuously with zero sections).

Feasibility facts for a constructor surface:

1. **No TableDesc/IR round-trip is needed to mint a name.** The exported seam
   `MapIntermediateToPhysicalColumns` takes two hand-buildable structs
   (`IntermediateColumnContext`, `IntermediateColumnProps`), and there is a
   production precedent doing exactly this:
   `lwsql.Resolver.NameConditions` (ADR-0121) synthesizes brand-new physical
   names (`tv:conditions:c1:val:b:0:0:0:0::`) for SQL-computed boolean
   columns, reusing the target table's separator and row config so the minted
   names re-parse into the same table. The constructor family proposed below
   is a generalization of this precedent, not new machinery.
2. **A column spec alone does not determine a name.** Non-derivable inputs:
   `tableRowConfig` (table-wide; only `0` exists today), streaming and
   co-section groups (free-form), `useAspects` (section-level), the separator
   (a convention parameter, `":"` everywhere in-tree), and — for membership
   and support columns — everything (machine-chosen). A constructor surface
   therefore needs defaults for the table-level segments and must *reject*
   attempts to hand-author support columns' properties.
3. **Aspect names are ambiguous across vocabularies.** `json-scalar`,
   `json-array`, `json-object`, `json` and the `cbor*` family exist in both
   `encodingaspects` and `valueaspects`; `none`/`indefinite` occupy bit 0
   differently. A flat aspect argument list ("`'delta-encoding'`,
   `'privacy'`, …") cannot be routed unambiguously — the arguments need a
   vocabulary marker.
4. **A natural first sketch already trips over point 3's sibling.**
   `LW_PLAIN('mycol','u64','scale-of-measurement-nominal','privacy','delta-encoding')`
   mixes all three vocabularies — and `privacy` (a use aspect) *cannot be
   attached to a plain column at all*; it forces a tagged section. Exactly
   the error class the constructor must catch loudly at authoring time,
   where today nothing surfaces at all.
5. The hint segment of machine-chosen membership columns is
   technology-filtered (`EncodingAspectFilterFuncFromTechnology`), so the same
   section renders different hint segments under the ClickHouse and Arrow
   backends. All in-tree goldens are ClickHouse-rendered; a constructor
   surface targeting ClickHouse SQL should pin the ClickHouse filter and note
   the divergence.

Incidental finding: `public/semistructured/leeway/EXPLANATION.md` shows a DDL
naming scheme (`user_p_present`, `cart_vlen`) that does not exist in the code.
It predates the current convention and should be reconciled separately.

## 3. Three contracts, precisely

The three aspects are three *output contracts* for expressions over leeway
columns. Naming them precisely is most of the design.

### F — filter (guard) contract, for `WHERE`

The theory already exists in
[the query algebra](../explanation/leeway-query-algebra.md): every predicate
abstracts to a pair (S, N) with S ⇒ P ⇒ N. The **N (necessary) side is the
DQL guard**: false positives allowed, false negatives never. Rules that the
surface must state normatively, because they are where practitioners get cut:

- **Grain.** `has(lmv, '/a')` is *exact* at row grain ("some attribute in this
  row carries the tag") and a *guard* at attribute grain. False positives
  enter through grain mismatch and through conjunction across lanes
  (`has(a,x) AND has(b,y)` admits rows where x and y sit at different
  attributes — the erased-axis-correlation class). A guard vocabulary should
  say which grain it answers.
- **Polarity.** AND/OR compose guards pointwise; **negation swaps S and N** —
  `NOT <guard>` is no longer a guard for `NOT P` (it is an exact rejector,
  and it stops pruning). Index pruning serves positive polarity only.
- **Prunable shapes are an enumerated list**, not a semantic property:
  `has`/`hasAny`/`hasAll` with constant needles prune via bloom-filter skip
  indexes; `indexOf(a,x) > 0` prunes while the equivalent
  `indexOf(a,x) != 0` scans. This is why ADR-0162 §SD3 bundles guards inside
  UDF bodies, and why the guard contract belongs to the *surface*, not to
  user discipline.
- **Zero-install property (worth preserving).** The generated Presence /
  Validator / Filter artefacts (ADR-0066) use ClickHouse built-ins only —
  `has`, `hasAll`, `countEqual`. The `WHERE`-side story works on a server
  with nothing installed; only extraction needs the UDF families. Any new
  guard sugar should either stay client-expanded or remain optional.
- **Measurable slack.** The guard-vs-exact false-positive factor is one
  `countIf` comparison away; the algebra doc demonstrated 10× on an
  uncorrelated two-lane conjunction. A slack-measurement recipe belongs in
  the featureset docs; a facts home for calibration is deferred (ADR-0162
  §SD8 already parks it).

### X — extract contract, for `SELECT` into opaque columns

The output is an ordinary ClickHouse value; leeway-ness ends at the
expression boundary. The semantics that must be uniform and documented:

- **Absent handling** is a three-way choice: type default (ClickHouse-native,
  what `lane[indexOf(...)]` gives), `NULL` (the `LW_CO_LOOKUP_NULL` /
  scalar-Option `if(has(...), …, NULL)` form), or empty-array sentinel
  (forced for non-scalars — ClickHouse forbids `Nullable(Array)`, so absent
  collides with present-empty; already decided in ADR-0066).
- **Aliasing.** Under multi-membership, `indexOf`-based extraction silently
  returns the *first* match. The exact form routes through the
  position→attribute map (`LW_RAGGED_PARENT_IDS(<cardcol>)` as `m2v`).
  Which form is legal is *structurally decidable from the names*: a
  `<role>card` column exists iff memberships may repeat — its absence proves
  MC ≡ 1 and licenses the bare-`indexOf` fast path. (This closes ADR-0066's
  open "fast-path detection" item as a by-product of any schema-aware
  expansion pass.)
- **No new numeric semantics.** Extraction emits engine arithmetic; the
  second-substrate trial's U5 (Int64 overflow, two of three engines silently
  wrong) is an engine property the surface must not pretend to fix.
- Portability tiering, from the trial: the packed layout reads with
  list-indexing + list-position on every engine probed (H1 held — no pack
  needed for the canonical mapping); the higher-order rendering is
  ClickHouse/DuckDB-only; the exploded rendering needs no array functions at
  all. The repo documents only the higher-order form — a standing gap the
  featureset docs should close.

### T — transform contract, for `SELECT` producing leeway shape

The anchor is a **closure property** the repo has never written down:

> A `SELECT` whose output column names parse under the naming convention and
> satisfy the vertical-subsetting rule (all plain columns; co-section groups
> whole) *is* a leeway table. `DiscoverTableFromColumnNames` is the
> mechanical witness.

Bare-identifier projection (subsetting) and row filtering are therefore
already closed — `SELECT <cols> FROM t WHERE …` preserves leeway-validity
because names ride through unchanged. Closure breaks exactly when an
*expression* appears: the output column loses its self-describing name. The
transform featureset is precisely: **let computed columns re-enter the closed
set** (constructors mint names — the LW_PLAIN idea), plus validation that the
assembled `SELECT` list forms a coherent table (section completeness,
co-length invariants, positivity — `card ≥ 1`; an empty list is *absent*, not
a value).

What transformation covers, concretely: re-tagging and annotation overlays
(secondary memberships, membership-only co-sections), re-sectioning
(type/cardinality moves), datamart projection (X made durable), and the
packed↔exploded second representation (ADR-0171 §SD5 — the one-pass
`ARRAY JOIN` conversion measured at 100M docs / 50.8 s exists only as a trial
shell script today). Channel changes verbatim↔ref are blocked on a SQL-side
name→id lookup (ADR-0171 §SD4) and stay out of scope until that lands.

## 4. Inventory against the three contracts

| Capability | Exists today | Substrate | Status |
| --- | --- | --- | --- |
| F: row-grain tag guard | `has` idioms; generated Presence | built-ins only | shipped (0066) |
| F: exact row conformance | Validator, Filter = Presence AND Validator | built-ins only | shipped (0066) |
| F: correlated two-lane guard | `LW_CO_EXISTS_EQ2` (guards bundled) | server UDF | shipped (0162) |
| F: skip-index DDL emission | — (`ddl/clickhouse` emits no `INDEX` clause) | generator | **gap** (0066 open) |
| F: slack measurement recipe | one-off in algebra doc | docs | **gap** |
| X: lane algebra | chpack, 16 fns `LW_CO_*`/`LW_RAGGED_*` | server UDF | shipped, versioned (0162) |
| X: extract by tag | `LW_VALUE_BY_TAG_EQUAL`, `LW_LIST_BY_TAG_EQUAL`, `LW_LU_*` | server UDF | shipped, **unversioned** (0171 §SD2 pending) |
| X: per-kind typed projection | generated `CAST(tuple(…))` artefact | codegen | shipped (0066) |
| X: schema-blind extraction sugar | — | — | **gap** |
| X: id decode | `LW_ID_*` | both client + server | shipped (0106) |
| read-name ergonomics | handles `section:column` (`lwsql`, ADR-0116) | client pass | shipped |
| write-name ergonomics | only `NameConditions` (fixed synthesized section) | client pass | **gap** — the LW_PLAIN hole |
| T: table construction from SQL | — (CTAS pattern undocumented; conversion is shell-only) | — | **gap** |
| T: transform validation | `TableValidator` exists Go-side only, on `TableDesc` | — | **gap** |
| T: materialized projections | — | generator | proposed (0171 §SD3) |
| T: exploded companion | — | generator + SQL | proposed (0171 §SD5) |
| membership name→id from SQL | — (ids ride SQL as literals) | — | proposed (0171 §SD4) |

Two structural observations fall out:

- **(a) needs no new server functions.** Its gaps are a generator feature
  (skip indexes), documentation (polarity/grain/prunable-shape rules), and a
  recipe. The zero-install property can be kept intact.
- **(c) is not a missing function but a missing *direction*.** Read-side
  ergonomics resolve existing names (handles); write-side ergonomics must
  mint new ones. The machinery (naming seam, precedent, validator) all
  exists in Go; nothing exposes it to SQL authoring.

## 5. The constructor family (the `LW_PLAIN` idea, worked out)

**Where it can run.** A ClickHouse SQL UDF cannot serve: UDFs are macros over
*expressions*, and a minted name must land in **identifier position** (an
alias, a column list, a DDL column). Server-side is structurally out. The
constructor is therefore an **authoring-time client rewrite** — a nanopass
pass in the existing `StagePreExecute` family (precedent: `LW_ID_*`
expansion, handles at order 200). The expanded output is plain SQL text any
client can carry to any endpoint; nothing needs installing server-side, and a
constructor call that accidentally reaches a server fails loudly as an
unknown function rather than silently mis-writing.

One hard constraint discovered en route: **grammar1 parses SELECT only**
(`queryStmt → … selectUnionStmt`). `INSERT INTO … SELECT` and
`CREATE TABLE … AS SELECT` cannot flow through the pipeline as whole
statements today. So v0 expands the inner `SELECT`; the DML/DDL wrapper is
composed outside the pipeline by hand. The wrapping story is deferred, with
the direction fixed: when statement coverage lands, it
lands by porting the `INSERT`/`CREATE` productions from ClickHouse's
upstream ANTLR grammar (`utils/antlr` in the ClickHouse repository — the
lineage grammar0/1/2 already derive from), not via a wrapper tool.

**Call forms** (names illustrative; the family/naming is a dialogue point):

```sql
-- plain column: expression → leeway-plain; the item type is mandatory (Q3)
SELECT LW_PLAIN(sum(x), 'total-revenue', 'u64', 'item:oq', 'enc:delta-encoding', 'sem:scale-of-measurement-metric-ratio')
-- expands to:  sum(x) AS "oq:total-revenue:u64:2:8:0:"     (name segments per §2)

-- tagged value column within a section
SELECT LW_TV(v, 'mysec', 'mycol', 'u64h', 'use:privacy', 'enc:light-general-compression')
-- expands to:  v AS "tv:mysec:mycol:val:u64h:g:8:0:0::"  (+ the len support column is NOT implied — see validation)

-- membership / support columns: channel named, properties machine-chosen
SELECT LW_TV_MEMB(m, 'mysec', 'low-card-ref')      -- → m AS "tv:mysec:lr:lr:u64:2q:8:0:0::"
SELECT LW_TV_SUPPORT(c, 'mysec', 'lrcard')
```

Design points inside that sketch:

1. **Aspect arguments carry a vocabulary prefix** (`enc:` / `sem:` / `use:`),
   because bare aspect names are ambiguous (§2.3) and because routing errors
   must be loud (§2.4: `use:privacy` on `LW_PLAIN` is a *rejection*, with the
   fix — "make it a tagged section" — in the error text). Alternatives: three
   positional list arguments (unreadable), nested wrapper calls
   (`LW_ENC(…)`) (parseable but noisy). All arguments are string literals —
   no lane names ride constructor arguments, so ADR-0162 §SD7's
   literal-argument hazard does not arise, though the expansion still emits
   an aliased expression and thus lives in an expression-capable pass, not
   the literal-only `MacroExpander`.
2. **The item type is always explicit** (`'item:oq'`, `'item:id'`, …) — an
   `oq` default was rejected: prefixes carry semantics, and a constructor
   call should read complete on its own. The remaining defaults
   are the genuinely table-level segments: `tableRowConfig 0` (the only
   value that exists), empty streaming and co-section groups. When the
   statement's target table is known (play pin path), the pass adopts the
   target's separator and row config exactly the way `NameConditions`
   already does.
3. **Support columns are constructed by channel, never by properties.**
   `LW_TV_MEMB(expr, section, channel)` — role, canonical type and hints come
   from `ResolveMembership`, ClickHouse-filtered. Hand-authoring `lr`'s type
   is exactly the mistake the machinery exists to prevent.
4. **The Go seam is a small exported API** over the `NameConditions` pattern
   (hand-built `IntermediateColumnContext`/`Props` →
   `MapIntermediateToPhysicalColumns`), shared by the pass, the CLI, and
   tests. Candidate homes: `lwsql` (it already owns the read direction) or a
   sibling `lwname`; not `ddl` (which would drag generator deps into the
   query path).
5. **Duality with handles, stated as a rule:** resolving an *existing*
   column = handle (`section:column`, ADR-0116); minting a *new* column =
   constructor. `INSERT INTO <existing leeway table>` therefore wants
   handles in its column list (resolution against the target), not
   constructors; constructors serve CTAS and new marts. This keeps one
   mental model and avoids two spellings for the same column.

**DDL paths.** (i) v0: CTAS over an expanded SELECT — names and types are
right; `CODEC` clauses are *not* applied by CTAS, so the encoding aspects
ride the name as recorded intent without taking effect. Acceptable for
ad-hoc marts; the self-describing name makes a later "apply codecs" `ALTER`
generator possible. (ii) Durable: the existing `leeway` CLI (precedent:
`leeway id udf`) gains a `ddl compose` subcommand taking the same
column-spec arguments and printing full `CREATE TABLE` through the real
generator — codecs, engine, `ORDER BY`. Both paths consume the same Go
micro-API, so the spec strings mean the same thing everywhere.

**Validation (the other half of the transform contract).** Constructors make
columns; nothing yet says the *set* of columns is a table. Two stages:

- *Static* (authoring time, in the same pass): parse every output name
  (minted or ridden-through), run `DiscoverTableFromColumnNames` + the
  existing `TableValidator`, and check section completeness against each
  section's channel (a `val` without its membership columns, a repeating
  channel without its `…card`, a dangling half of a co-section group — all
  detectable from names alone).
- *Runtime* (data invariants statics cannot see): a generated audit query per
  target table — co-length equality across a section's lanes, `card ≥ 1`
  positivity, membership-card sums consistent with membership-lane lengths.
  The second-substrate trial's `arm-x.sh` assertion is the prototype; the
  generator makes it schema-derived instead of hand-written.

## 6. Substrate options (QOC)

**Question.** What carries the DQL featureset?

- **O1 — Document idioms only.** The built-ins already express everything
  (the trial's H1). No new surface.
- **O2 — Server-UDF-maximal.** Extend the installed families to cover guards,
  extraction sugar, and name composition as string-returning functions.
- **O3 — Authoring-time portfolio.** Client passes for everything that
  touches *identifiers* (constructors, handles, schema-aware extraction
  sugar); server UDFs stay what they are (lane algebra + read-back);
  generators for durable artifacts (skip indexes, audits, DDL).
- **O4 — Codegen-only.** Extend the per-kind generators; no ad-hoc surface.
- **O5 — A query language** (lwq revival).

**Criteria.** C1 senior-SQL acceptance (no Go, no physical names, plain SQL
mental model); C2 explicit correctness contracts (FP direction, absent
semantics); C3 preserves index behaviour; C4 works across endpoints (incl.
zero-install servers); C5 drift/versioning safety; C6 cost; C7 portability
beyond ClickHouse.

|      | O1 | O2 | O3 | O4 | O5 |
|------|----|----|----|----|----|
| C1   | −  | +  | ++ | −− | ++ |
| C2   | −  | +  | ++ | ++ | ++ |
| C3   | +  | +  | ++ | ++ | ?  |
| C4   | ++ | −  | ++ | +  | −  |
| C5   | ++ | −  | ++ | +  | −  |
| C6   | ++ | +  | +  | +  | −− |
| C7   | +  | −− | +  | +  | −  |

O2 dies on structure, not taste: name composition cannot reach identifier
position from inside the engine, and every added server function widens the
drift surface ADR-0171 §SD2 exists to shrink. O5 re-opens a parked decision
and would still need everything O3 needs underneath. O4 fails the acceptance
criterion outright (the Go loop is the complaint). **O3, grounded on O1's
documentation layer, is the recommendation** — with the explicit statement
that authoring-time expansion means the *product* is portable SQL text, and
the pass needs to run where SQL is authored (play, CLI, generators), not
where it executes.

### Dialogue outcome

The design dialogue (2026-08-09) took O3 and settled every open question;
the outcomes are folded into §5/§7, the rejected options kept in §9, and the
decision set is [ADR-0181](../adr/0181-leeway-dql-authoring-surface.md). One
side-resolution worth naming: the substrate assignment also settles the
substrate half of the query-algebra doc's open "where are guards emitted"
question — the *audit* side, when it comes, is a pass; the emission side
stays in pack bodies per ADR-0162 §SD3.

## 7. The pass architecture, concretized

With the substrate settled, the featureset's client side is three passes and
one optional evaluator registration, slotting into the existing
`StagePreExecute` chain (today: `CanonicalizeFull` at 50,
`descriptiveStatistics` 75, `docsearch` 80, `LW_ID_*` 100, handles 200):

| Pass | Order | Kind | Schema | Properties |
| --- | --- | --- | --- | --- |
| `LwExtractExpand` (`LW_GET*`, ADR-0181 §SD3) | ~120 | factory, scope-aware | yes — `SchemaProviderI` + `lwsql` classification | `Idempotent`, Body→Body |
| `LwConstructExpand` (`LW_PLAIN`/`LW_TV*`, §SD2) | ~130 | node-rule rewrite (ADR-0098 core) | no (optional target-adoption variant) | `Idempotent`, Body→Body |
| `LwShapeCheck` (§SD5) | late, before validate | analytical (discard-marker) with hard-error mode | no — reads minted/ridden names | `Idempotent`, Body→Body (no rewrite) |
| `LW_NAME_*` (optional) | — | `FunctionEvaluator` registration | no | folds to string literals |

Mechanics the framework fixes:

- **These are passes, not `MacroExpander` macros.** A constructor's first
  argument is an arbitrary expression; the literal-only expander hard-fails
  exactly that shape by design (ADR-0162 §SD7). `LwConstructExpand` is a
  `LiftBodyPass` whose node rule matches `ColumnExprFunctionContext` with
  `NormalizeCallName ∈ {lw_plain, lw_tv, lw_tv_memb, lw_tv_support}` — the
  same idiom as the shipped `Canonicalize*` rules. The purely-literal
  `LW_NAME_*` mirrors (compose a name as a *string*, for
  `system.columns`-style meta-queries) are the one family that fits the
  `FunctionEvaluator` instead.
- **Position rule.** A constructor call is legal only as a whole projection
  item; anywhere else (`f(LW_PLAIN(…))`, WHERE, GROUP BY) the pass errors
  with the call's `SourceRange`. Replacement text is
  `<arg0 span> AS "<minted name>"` (`QuoteIdentifier`, double-quoted), which
  is the same shape ADR-0121's conditions pass already emits — hidden-channel
  trivia inside the kept expression span survives via the token rewriter.
- **No collision with handles, structurally.** Handle syntax is *exactly one
  colon*; a minted physical name carries six (plain) or ten (tagged), so
  ordering relative to the handle pass at 200 is a documentation choice, not
  a correctness constraint. `LW_GET` expansion emits physical names
  directly, so its output never re-enters handle resolution either.
- **Argument discipline.** Every argument after the wrapped expression must
  be a string literal; a non-literal is a loud error, as is an unknown
  aspect token, a vocabulary misroute (`use:` on `LW_PLAIN` — error text
  names the fix: "make it a tagged section"), an unparseable canonical type
  (reuse the `canonicaltypes` parser), or a name failing `StylableName`
  validation.
- **`LwExtractExpand` binds section→table through `BuildScopes`.** One table
  in scope carrying the section: expand; several: error demanding
  qualification; none: error naming the tables that were searched. The
  expansion consults the classified table exactly as handles do, which is
  also where the structural fast path lives (no `<role>card` column ⇒
  MC ≡ 1 ⇒ bare `indexOf` legal).
- **One expression builder, two renderers.** The per-channel extraction
  logic must not exist twice (generator and pass). The resolution is a
  shared builder — the read-back generator's emission refactored to a common
  seam — with two rendering modes: **pack-form** (calls
  `LW_VALUE_BY_TAG_EQUAL` etc.; readable; requires the installed families)
  and **inline-form** (builtins only; zero-install; portable). Inline-form
  is, in substance, the "client-side expression expander" ADR-0162 §SD8
  defers with a read-only-target trigger — v0 deliberately does not take
  that scope (§9).
- **Inertness when unused.** Both expansion passes should cheap-scan the
  body for their markers before parsing, so standard-set registration costs
  approximately nothing on the overwhelming majority of queries that carry
  no `LW_` authoring calls. `LwShapeCheck` stays opt-in like the conditions
  pass (it is only meaningful on transform-shaped queries).
- **Testing discipline is inherited, not invented.** `AssertProperties` over
  the shared corpus plus a leeway-authoring corpus extension (constructor
  and extractor calls, nested and pathological); golden input/output pairs;
  and `clickhouse-local` execution of expanded extraction against the
  read-back round-trip oracle — the same three-layer pattern the read-back
  family already uses.
- **CLI reuse.** `leeway ddl compose` (durable CREATE TABLE from the same
  column-spec tokens, through the real generator — codecs, engine, ORDER BY)
  consumes the same Go micro-API as the pass. A statement-level `sql expand`
  tool was considered and set aside with the wrapping deferral: authoring
  hosts are the pipeline hosts (play) until the grammar grows.

## 8. Decision skeleton — graduated to ADR-0181

The decision skeleton this analysis proposed became, with the dialogue
outcomes applied,
[ADR-0181](../adr/0181-leeway-dql-authoring-surface.md) §SD1–§SD8, and is
not restated here. Two sequencing dependencies to carry into
implementation: the read-back family's version marker (ADR-0171 §SD2)
should land before the extraction sugar adds a second caller of the
installed family; and the skip-index `set(N)` option interacts with
`countEqual`/`indexOf` shapes (ADR-0066's 2026-06-09 Update carries the
verified matrix).

## 9. Rejected options and kill-reasons

Every settled choice, with what was rejected and why:

1. **Family naming → `LW_PLAIN`/`LW_TV_*`, `LW_GET*`.** `LW_MK_*` rejected
   (constructor-family segment, but longer and less self-evident);
   `LW_AS_*` rejected (mental collision with the `AS` keyword);
   `LW_ATTR`/`LW_EXTRACT` rejected (opaque to outsiders / collides with SQL
   `EXTRACT` sugar).
2. **Aspect arguments → vocabulary-prefixed strings**
   (`enc:`/`sem:`/`use:`/`item:`). Grouped wrapper calls rejected (three
   more registered names, nested parsing, more typing); single micro-spec
   string rejected (a new mini-grammar with its own parser and
   error-position story, for no added reach).
3. **Item type → always explicit.** The `oq` default rejected: prefixes
   carry semantics and a constructor call should read complete on its own.
4. **Wrapping → deferred.** Neither the wrapper-CLI pattern nor immediate
   grammar growth was chosen; the recorded direction is porting the
   `INSERT`/`CREATE` productions from ClickHouse's upstream `utils/antlr`
   grammar when statement coverage becomes due. Until then the wrapper is
   composed by hand around the expanded SELECT.
5. **Section-level sugar → staged.** Per-column constructors +
   `LwShapeCheck` first; one-call-per-section expansion later, purely
   additive. Shipping it in v0 rejected (a one-to-many rewrite capability
   plus an argument convention, ahead of any authoring-pain evidence).
6. **Skip-index intent → `TableOptions` only.** Aspect-borne intent
   rejected: it would make index changes column-identity changes (renames
   on retrofit); deployment flexibility and name stability won over
   name-recoverable intent.
7. **Guard sugar → none in v0.** The FP-contract docs, the prunable-shape
   table, and skip-index emission are the guard surface; names follow
   demand. Shipping `LW_MAY_*` now rejected (registry names ahead of usage
   evidence); growing the generated HAS family rejected (helps only
   codegen consumers).
8. **Micro-API home → `lwsql`.** A new sibling package rejected (one more
   package to discover, no dependency win — `lwsql`→`ddl` already exists);
   `ddl` rejected (generator territory; wrong dependency direction for
   query-path consumers).
9. **Extraction emission → one shared builder, pack-form renderer only in
   v0.** Independent pass-side emission rejected (two sources of truth for
   the per-channel logic); shipping the inline builtins-only renderer now
   rejected (it would un-defer ADR-0162 §SD8-1 without its read-only-target
   trigger — the builder seam is designed for it, the scope is not taken).
10. **Registration → standard set + cheap-scan inertness**; `LwShapeCheck`
    opt-in. Per-host opt-in rejected: the conditions-pass precedent is
    opt-in because it changes query results — these passes are inert
    without their markers, so the caution buys little and costs
    discoverability.

## References

- [ADR-0181](../adr/0181-leeway-dql-authoring-surface.md) — the decisions
  this analysis fed.
- [ADR-0171](../adr/0171-leeway-sql-read-surface.md) — the read surface; SD2
  versioning, SD3 materialized projections, SD4 vocabulary lookup, SD5
  exploded companion.
- [ADR-0162](../adr/0162-leeway-co-ragged-function-pack.md) — the pack; SD3
  guard bundling, SD7 macro-expander hazard, SD8 deferrals.
- [ADR-0066](../adr/0066-leeway-dql-clickhouse-readback-generator.md) — the
  read-back generator; artefact ladder, absent semantics, skip-index gap.
- [ADR-0116](../adr/0116-play-leeway-column-handle-resolution.md) — handles;
  [ADR-0121](../adr/0121-selection-condition-columns.md) — `NameConditions`,
  the write-direction precedent.
- [ADR-0022](../adr/0022-leeway-lwq-flwor-query-language.md) — lwq, the road
  deliberately not taken.
- [ADR-0002](../adr/0002-nanopass-discipline.md),
  [ADR-0006](../adr/0006-nanopass-environment-and-first-class-pass.md),
  [ADR-0098](../adr/0098-nanopass-local-rewrite-combinator-core.md) — the pass
  substrate §7 builds on.
- [leeway-query-algebra](../explanation/leeway-query-algebra.md) — the (S,N)
  guard model, planes, primitives;
  [leeway-clickhouse-array-idioms](../howto/leeway-clickhouse-array-idioms.md)
  — the verified idiom kernel.
- [leeway-second-substrate trial](../trials/leeway-second-substrate/README.md)
  — effect-size ordering (representation ≫ engine), portability findings,
  packed↔exploded conversion;
  [jsonbench-on-facts trial](../trials/jsonbench-on-facts/README.md) — the
  discoverability ledger behind ADR-0171.
