---
type: adr
status: accepted
date: 2026-08-06
reviewed-by: "p@stergiotis"
reviewed-date: 2026-08-06
---

# ADR-0170: the data-catalog competence — schema discovery, leeway restoration, and shape matching as boxer tables

## Context

A live boxer ClickHouse instance accumulates on the order of a hundred
leeway-mapped tables plus assorted opaque (non-leeway) tables, and nothing
enumerates them: which tables are leeway, what leeway schema a table carries,
which tables share a schema or contain one another, and which opaque tables a
play panel could render. Each of those questions is answered today by a person
probing `system.tables` by hand. Wanted: a **data catalog** — the answers as
queryable `boxer.*` tables, data-centric like the rest of the system
([ADR-0126](0126-appliance-topology-as-data.md)). "Competence" is the
[ADR-0168](0168-capmap-business-capability-corpus.md) corpus unit; the vault
entry for this competence is authored there, outside this repo.

Nearly every mechanism the catalog needs already exists; this ADR is mostly
wiring and two schema contracts:

- **Restoration.** The leeway naming convention is bidirectional:
  [`NamingConventionBwdI.DiscoverTableFromColumnNames`](../../public/semistructured/leeway/common/lw_types.go)
  rebuilds a `TableDesc` from physical column names alone (canonical types are
  encoded in the names), implemented by `HumanReadableNamingConvention`
  ([lw_ddl_gen_naming_human.go](../../public/semistructured/leeway/ddl/lw_ddl_gen_naming_human.go)).
  play's card driver
  ([play_card_driver.go](../../apps/play/play_card_driver.go)) already
  classifies ad-hoc result sets exactly this way: sniff the separator (`_` or
  `:`, skipping `_`-prefixed implicit columns), attempt discovery, treat
  failure as "opaque, expected".
- **Comparison.** [`TableOperations.Relate`](../../public/semistructured/leeway/common/lw_table_relation.go)
  classifies two `TableDesc`s as equal / subset / superset / overlap /
  disjoint on normalized copies — naming style and column order never decide
  the answer — and `IsSubset` reports the mismatches.
- **Probing.** play's `chSchemaProvider`
  ([play_schema_provider.go](../../apps/play/play_schema_provider.go)) fetches
  physical column lists from `system.columns`; the catalog needs the same two
  queries in batch.
- **Serving small in-process tables.** The keelson introspection plane
  ([ADR-0094](0094-keelson-introspection-tables.md)): a `Provider` snapshots
  Arrow, `keelson('<name>')` reaches it from SQL, and the plane's catalog
  lists itself.
- **Rendering.** play's Sankey tab ([ADR-0159](0159-imzero2-sankey-flow-widget.md),
  [play_sankey_panel.go](../../apps/play/play_sankey_panel.go)) consumes a
  `flows(source, target, value)` CTE; sqlapplet books
  ([ADR-0132](0132-sqlapplet-sql-defined-applets.md)) package such queries as
  navigable chapters.
- **A CLI shape to copy.** `apps/jsonbench`: urfave/cli +
  [the `chclient` HTTP client](../../public/keelson/data/chclient), DDL
  emit-or-apply.

What does not exist: persistence of discovery results, the pairwise relation
matrix, and a shape vocabulary for opaque tables.

## Design space (QOC)

**Q1 — where do classification, restoration, and shape matching evaluate?**
Criteria: single source of truth for the naming grammar; usable as a batch
tool with no live boxer process; testability without ClickHouse.

- *O1 — a Go batch tool reusing the leeway packages.* All three criteria met;
  the grammar has exactly one implementation. **Chosen.**
- *O2 — SQL over `system.columns`.* Requires re-implementing the column-name
  grammar and the normalization rules in SQL — a second implementation that
  drifts. Killed.
- *O3 — evaluate inside a live keelson process, reading the shape table via
  `keelson('panel_shapes')`.* Couples a batch tool to a running process.
  Killed as the mechanism — but the shape table is still served on the plane
  (§SD5), so a live session can run the same join ad hoc.

**Q2 — what is the storage form of the catalog?**
Criteria: honesty about being derived (rebuildable, no history obligation);
query ergonomics for joins with `system.*`; implementation weight.

- *O1 — plain `boxer.*` tables, rebuilt whole per run.* The source of truth is
  the physical schema itself; a full rebuild states that. **Chosen.**
- *O2 — `boxer.facts` rows.* An append trail is the wrong shape for a snapshot
  whose readers want current state; workingset filtering would be ceremony
  with no payer. A per-run *event* fact is deferred (§Deferrals).
- *O3 — leeway-modelled catalog tables.* The nested `TableDesc` payload fits
  the tagged-section model poorly, and circularity buys nothing: the catalog
  listing its own tables as opaque rows is correct and free, the same way the
  introspection catalog lists itself.

## Decision

**SD1 — Discovery: one snapshot pass over `system.tables` × `system.columns`,
classified by the leeway naming grammar.** The cataloger reads every table
outside the system databases (`system`, `INFORMATION_SCHEMA` in both
spellings), fetches column names and types in `position` order, and classifies
each table by the card driver's probe: sniff the separator, attempt
`DiscoverTableFromColumnNames`, leeway iff discovery succeeds. The probe is
lifted out of play into the engine package so play and the catalog share one
classifier. The catalog's own output tables are discovered like any others.

One correction the lift forced: discovery accepts an *empty* column list and
returns an empty table, which is vacuously a valid leeway schema. A table with
no columns carries no evidence and is classified opaque instead
(`datacatalog.ErrNoColumns`) — calling it leeway would put an attribute-less row
into `tables_leeway` and an empty-set node into every pair it takes part in.

**SD2 — Four derived tables in `boxer`, rebuilt whole per run.** Each run
writes complete replacements (`CREATE OR REPLACE TABLE`, then insert), stamped
`run_id` + `discovered_at`. The user-visible contract is three tables; a
fourth (the inventory) exists so that "matches no known shape" and "looks
leeway but does not parse" are visible rows rather than absences:

```text
boxer.tables_catalog              -- every discovered table, both kinds
  database, name, engine String
  kind Enum8('opaque'=0,'leeway'=1)
  n_columns UInt32
  normalized_schema String        -- §SD4 string, emitted for both kinds
  classify_detail String          -- why this table has no tables_leeway row
  run_id String, discovered_at DateTime      ORDER BY (database, name)

boxer.tables_leeway               -- restoration payload, kind='leeway' only
  database, name String
  table_row_config String
  schema_hash UInt64              -- fnv64a over the sorted attr keys
  n_attrs UInt32
  attr_keys Array(String)         -- normalized "scope/section/name:type"
  desc_json String                -- TableDescDto (JSON tags exist)
  run_id, discovered_at                      ORDER BY (database, name)

boxer.tables_leeway_compatibility -- every unordered pair, (a) < (b)
  database_a, name_a, database_b, name_b String
  relation Enum8('disjoint'=0,'overlap'=1,'subset'=2,'superset'=3,'equal'=4)
  shape_id UInt64                 -- fnv64a of intersection attr keys; 0 if disjoint
  n_common UInt32, jaccard Float32
  run_id, discovered_at            ORDER BY (database_a, name_a, database_b, name_b)

boxer.tables_opaque_shapes        -- one row per satisfied (opaque table, shape)
  database, name, shape String
  run_id, discovered_at                      ORDER BY (shape, database, name)
```

Two details the schema fixes rather than leaving to insertion order. The `Enum8`
values are pinned to the Go enums' numbers, so the number ClickHouse stores and
the number the analysis computed are the same one; and `classify_detail` carries
a single meaning throughout — *why this table has no `tables_leeway` row* — which
is the parse failure for an opaque table, a normalization failure for a leeway
one, and empty exactly when the row is there. A leeway table that fails to
normalize is therefore a visible row rather than a silent absence, and one bad
table does not cost the run.

The destination database is a parameter (`TargetDatabase`, empty meaning
`boxer`) even though the decision is that the catalog lives in `boxer`: what a
run *reads* is not configurable, only where it writes. The payer is the
integration lane — a run reads the whole server, so a test that also wrote where
the real catalog lives would rebuild production state as a side effect.

**SD3 — Restoration and relation reuse leeway's machinery verbatim, brute
force.** `Relate` runs once per unordered pair — ~5,000 calls at n≈100, each
re-normalizing both sides, accepted at this scale, single-threaded
(`TableOperations` is not concurrency-safe). The comparison semantics are
*inherited*, not redefined here: style- and order-insensitive, exact canonical
types, aspect coverage. Attribute keys are the canonical serialization of the
normalized (scope, section, name, canonical type) tuples; `schema_hash` /
`shape_id` are fnv64a over the sorted key list, so tables with equal schemas
and pair-intersections that coincide unify on one id. For equal/subset pairs
the intersection is the contained side, so its `shape_id` equals that side's
`schema_hash`. All pairs are stored, disjoint included — consumers filter.

**SD4 — Opaque tables get a normalized schema string; known shapes are
AND-batteries of RE2 patterns over it.** The string: columns in
`system.columns.position` order, `;` sentinels at both ends, each column
`name:type;`. `LowCardinality(…)` is stripped, `Nullable(T)` becomes `T?`,
types are otherwise ClickHouse-canonical verbatim; `\` escapes `;`, `:`, `\` —
in the type as well as the name, since an `Enum8` literal can carry a `;` and
would otherwise split the string into a column that does not exist. The two
readings coincide for every type that contains none of the three, which is
every type in practice. The rewrite is top-level only: `Array(Nullable(String))`
stays verbatim, because descending would need a real ClickHouse type parser
(`Enum` literals, `Map`, named `Tuple`) and no seed shape needs it.
Example: `;ts:DateTime64(3);label:String?;value:Float64;`. A *known
shape* names the result contract a play panel could render with a trivial
query, as a battery of Go-RE2 patterns that must **all** match — RE2 has no
lookahead, so conjunction lives outside the regex (the position-independent
"has a `lane` column and a `title` column" is two patterns, not one). Shapes
are matched against opaque tables only: leeway physical names would match
nonsense, and leeway tables reach panels through handles and UDF read forms
instead. Seeds come from the shipped panel contracts, read out of the panels'
own column constants rather than remembered: series (a DateTime-family column
plus a numeric), sankey flows (`source`, `target`, numeric `value`), network
edges (`source`, `target`), kanban cards (`lane`, `title`), the two hierarchy
forms (`id`+`parent`+numeric `value`; Array-typed `stack`+numeric `value`), and
distribution — which turned out to be the quantile-grid contract `series`, `n`,
`ps`, `qs` rather than the "any numeric" this ADR first guessed. The battery
itself is the source of truth, not this list.

**SD5 — The battery is one definition with two faces.** The shapes live in
`public/gov/datacatalog/panelshapes`, which the catalog run imports and
evaluates in Go; a `panel_shapes` introspection provider (columns `shape`,
`pattern`, `ordinal`, `note`) serves the same list as data, so
`keelson('panel_shapes')` answers what this build can recognise. Since the
live-server expansion of `keelson(…)` is a `url()` source, a play session can
join `keelson('panel_shapes')` against `boxer.tables_catalog` ad hoc, while
`boxer.tables_opaque_shapes` is that join materialized for sessions where no
introspection plane is up.

The provider is registered in the
[introspection provider package](../../public/keelson/runtime/introspect/providers/),
with every other introspection table, rather than inside `panelshapes`: capmap, adr
and codevol all keep their data in a `public/gov` package and their provider
there, and a reader looking for where a keelson table comes from looks there.
The one-definition-two-faces point is unaffected — only the file moved.

**SD6 — The runner is a `boxer` subcommand**, `boxer datacatalog`:
`chclient`, a `refresh` verb, `--url/--user/--password/--database` flags,
`--dry-run` printing DDL and would-be row counts, plus `shapes` and `ddl` verbs
that need no server. Scheduling is out of scope (§Deferrals); a run is explicit.

Not a standalone `apps/datacatalog` binary, which is what this ADR first
specified on the jsonbench pattern:
[CODINGSTANDARDS § Entry Points](../../CODINGSTANDARDS.md#entry-points) forbids
new `main()`s for utilities, and `boxer capmap` — the same shape of tool, from
the neighbouring ADR — is the precedent that rule produces. jsonbench is a trial
artifact and says so in its own package doc; this is not one.

**SD7 — Rendering rides the existing Sankey tab via a sqlapplet book.** A
`bookcatalog` suite with three chapters: an inventory overview, the leeway
hierarchy as a Sankey — `flows` built from non-disjoint pairs, shape nodes
(`shape:<id>`) flowing into the tables that contain them, `value = n_common`,
a `HAVING` floor keeping the diagram legible — and an unmatched-opaque-tables
chapter (the catalog's to-do list). Shape attribute listings are recovered on
demand with `arrayIntersect` over `tables_leeway.attr_keys`; no third shape
table is materialized.

The overview **measures its own staleness** rather than only stamping it. A
`run_id` column is something a reader has to think to check, and a dropped
database looks exactly like a live one until they do — which is how the first
reader of this book was misled. Since the chapters run against a live server
(`endpoint: default`), the overview re-reads `system.tables` and counts, per
database, the tables the catalog lists that are gone and the ones on the server
it has never seen, sorting the drifted databases to the top. Both directions,
because a table created since the run is as misleading an absence as a dropped
one is a presence. The remaining columns stay as old as the run, which is the
honest reading: a chapter that silently corrected itself would under-report
instead of saying how out of date it is.

## Alternatives

Beyond the QOC kills: a separate `tables_leeway_shapes` table (derivable via
`arrayIntersect`, and premature until a book chapter needs it — descoped); a
structured shape spec instead of regex (a second schema language to maintain,
and the regex form is what makes shapes *rows in a table*, which is the
point); folding everything into one wide table (readers of the pair matrix
and the inventory share no columns); computing shape matches in SQL `match()`
at book-query time only (leaves "which tables satisfy no shape" unanswerable
without the introspection plane up).

## Consequences

- Four new tables appear in `boxer` and answer schema questions in SQL, at
  the cost of being a **snapshot**: stale until the next `refresh`, and
  `CREATE OR REPLACE` means readers mid-rebuild can observe a fresh-but-empty
  instant. Acceptable for a catalog; a scheduled tee is deferred.
- The relation matrix is O(n²) rows by design; at n≈100 that is ~5k rows.
  The brute-force posture should be revisited if table counts grow 10×.
- The naming-grammar probe gains a second caller, pinning
  `DiscoverTableFromColumnNames` and `Relate` as load-bearing surfaces.
- The shape battery will start small and wrong in places; it is data, so
  refining it is editing one package and re-running `refresh`.

## Surfaces — Tier 1

Additive, with one exception noted below. The `boxer` database gains the four
tables above (`tables_catalog`, `tables_leeway`, `tables_leeway_compatibility`,
`tables_opaque_shapes`); the keelson dataset namespace gains `panel_shapes` (the
plane's catalog lists it automatically); sqlapplet gains the `bookcatalog` suite
(book id becomes a nav label); `boxer` gains a `datacatalog` subcommand, and
`public/gov/` a new engine package plus its `panelshapes` battery. No new
environment variables.

The exception is the one upstream addition: `common.TableOperations` exports
`NormalizedCopy`, a wrapper over the existing private `normalizedCopy`, so the
attribute keys are derived from the same normalized form `Relate` compares —
otherwise two tables `Relate` calls equal could carry different schema hashes.

## Migration — Tier 1

Nothing breaks: every surface is additive. Consumers must treat the catalog
tables as rebuildable derived data — joins against them are valid at any
instant but not across a `refresh` boundary; `run_id` detects that.

## Verification plan — Tier 1

- **Round-trip unit test** (the load-bearing one): fixture `TableDesc` →
  DDL-generated physical names → `DiscoverTableFromColumnNames` →
  `Relate(original, restored)` = `equal`, under both separators.
- **Battery unit tests**: normalized-string builder fixtures (escaping,
  `Nullable`/`LowCardinality` rules); one positive and one negative fixture
  per seed shape.
- **Integration lane** (`//go:build integration`,
  [gotest-integration.sh](../../scripts/ci/gotest-integration.sh)): scratch
  database with two related leeway tables and an opaque series-shaped table;
  run `refresh`; assert kinds, one `subset` pair, one shape row.
- Silent regression of the naming grammar or relation semantics turns the
  round-trip test red; doclint covers this document.

## Milestones

- **M0 — engine package.** ✓ `public/gov/datacatalog`: snapshot types, a
  `system.tables`/`system.columns` fetcher behind an interface, the classifier
  lifted from the card driver (play re-pointed at it), the normalized-string
  builder. Unit tests only, no live ClickHouse.
- **M1 — analysis.** ✓ Attribute keys, `schema_hash`/`shape_id`, the pair
  matrix; `panelshapes` battery package with the seed set.
- **M2 — persistence + CLI.** ✓ DDL, writer, `boxer datacatalog refresh`,
  `--dry-run`; the integration-lane test.
- **M3 — keelson surface.** ✓ `panel_shapes` provider, registered with the
  rest of the static set.
- **M4 — book.** ✓ `bookcatalog` chapters (inventory, Sankey hierarchy,
  unmatched opaque), the overview measuring its own staleness. The screenshot
  the milestone originally carried is descoped to §Deferrals rather than left
  as a footnote on a shipped chapter.
- **M5 — deferred**: see Deferrals.

## Deferrals

A book screenshot via the play recipe — it needs a private weston and a
matching Go/Rust FFI pair, neither of which the implementing session had;
scheduled refresh (bgjob or bus tee); a per-run event row in `boxer.facts`;
transitive reduction of the subset graph (the book query's `HAVING` floor
stands in); shape matching for *leeway* tables via their UDF read forms; a
`tables_leeway_shapes` materialization.

## Status

Accepted 2026-08-06, with M0–M4 already shipped: the decision was implemented
ahead of review, so the §SD text above states what was built rather than what
was proposed before building corrected it (the CLI's home, the provider's home,
the zero-column case, the distribution contract, the escaping rule). Changes now
arrive as dated `## Update` sections. The milestone survey with exact symbols and
file paths is
[data-catalog-competence.md](../adr-background-work/data-catalog-competence.md).

## References

- [ADR-0094](0094-keelson-introspection-tables.md) — introspection plane, `keelson(…)` macro
- [ADR-0117](0117-passthrough-table-classifier.md) — prior table-classification seam
- [ADR-0122](0122-play-kanban-panel.md), [ADR-0129](0129-play-layered-graph-panel.md) — named-CTE panel contracts
- [ADR-0132](0132-sqlapplet-sql-defined-applets.md) — books; [ADR-0159](0159-imzero2-sankey-flow-widget.md) — Sankey widget
- [ADR-0168](0168-capmap-business-capability-corpus.md) — the competence vocabulary
- Code: `public/semistructured/leeway/common/lw_table_relation.go`,
  `public/semistructured/leeway/ddl/lw_ddl_gen_naming_human.go`,
  `apps/play/play_card_driver.go`, `apps/play/play_schema_provider.go`,
  `public/keelson/runtime/introspect/`, `apps/jsonbench/`
