---
type: explanation
audience: package maintainer
status: draft
# reviewed-by: "@<handle>"     # fill in and uncomment when flipping to stable
# reviewed-date: YYYY-MM-DD    # fill in and uncomment when flipping to stable
---

> **Status: draft — pre-human-review.** Not verified; do not cite as authoritative.

# Data catalog — implementation survey (ADR-0170 background work)

Companion to [ADR-0170](../adr/0170-data-catalog-competence.md): the symbol
inventory and per-milestone file plan, so the implementing session starts from
found code instead of re-deriving this survey. Line numbers are as of
2026-08-06 and will drift; symbol names are the stable handle.

## Symbol inventory

**Leeway core** — everything in
`public/semistructured/leeway/common/`:

- `lw_types.go` — `TableDesc` (L111), `TableDescDto` (L124; carries both
  `cbor` and `json` tags, so `encoding/json` serves the catalog's `desc_json`
  column directly), `NamingConventionBwdI` (L242) with
  `DiscoverTableFromColumnNames` (L253), `PhysicalColumnDesc` (L186).
- `lw_table_relation.go` — `TableRelationE`
  (disjoint/overlap/subset/superset/equal, L143), `IsSubset` (L213; the doc
  comment is the semantics: exact canonical types, aspect *coverage*,
  groups compared pin-when-set), `Relate` (L236). **Not concurrency-safe**;
  run the pair loop single-threaded.
- `lw_table_operations.go` — `NewTableOperations` (L18), `DeepCopy`.
  `normalizedCopy` (relation file, L268) is unexported; attribute-key
  extraction needs a normalized table, so the **one upstream addition** is a
  tiny exported wrapper (e.g. `NormalizedCopy`) beside it. Everything else is
  consumed as-is.
- `lw_table_marshaller.go` — CBOR encode/decode for `TableDesc`/`TableDescDto`
  (L75–102), if CBOR is ever preferred over JSON.

**Restoration / classification**:

- `public/semistructured/leeway/ddl/lw_ddl_gen_naming_human.go` —
  `NewHumanReadableNamingConvention(separator)` (L188), `ParseColumns` (L522),
  `DiscoverTableFromColumnNames` (L535). Discovery fails on the first column
  that does not parse — that failure *is* the opaque classification.
- `apps/play/play_card_driver.go` L100–120 — the reference classifier to lift
  into the engine package (then re-point play at it; pure move, no behavior
  change): default separator `_`; switch to `:` if the first
  non-`_`-prefixed column name contains `:`; `_`-prefixed columns are skipped
  as evidence; discovery failure is an expected non-leeway result.
- `apps/play/play_schema_provider.go` — `chSchemaProvider`: the
  `system.columns` probe shape (5 s timeout, TTL cache). The catalog needs the
  batch equivalent: one query over `system.tables`, one over `system.columns`
  ordered by `(database, table, position)`, excluding `system` and
  `INFORMATION_SCHEMA`/`information_schema`.

**keelson introspection plane** (`public/keelson/runtime/introspect/`):

- `introspect.Provider` — `Name() / Freshness() / Schema() / Snapshot(proj)`;
  build snapshots with `NewTable().String(...).Int32(...).Build(proj, n)`.
- `catalog.go` — smallest complete provider pair; `providers/coverage.go` —
  the freshest registered example, including the "nil source still registers,
  empty tables rather than absent names" convention.
- `introspecthost/introspecthost.go` — the wiring point: `RegisterCoverage`
  at L127, `RegisterCatalog` at L149; `panel_shapes` registers alongside.
- `keelsonsql/keelsonsql.go` — the `keelson('<name>')` macro: expands to a
  TEMPORARY table (in-process engine) or a `url('<live-base>/table/<name>',
  'ArrowStream')` source (external server), which is why a live session can
  join `keelson('panel_shapes')` against `boxer.tables_catalog` ad hoc.

**Writing to ClickHouse**:

- `public/keelson/data/chclient` — the HTTP client every CLI uses.
- `public/gov/capmapfacts/capmapfacts.go` — doc comment records that
  `chclient.Client` satisfies its one-method `RecordSinkI` (Arrow batches in).
  Two viable insert paths: Arrow via a `RecordSinkI`-shaped seam
  (capmapfacts precedent) or plain `INSERT … VALUES` literals via
  `public/db/clickhouse/dsl/marshalling` `TypedLiteral`. At ~100 rows per
  table either is fine; pick one and note it in the package doc.
- `apps/jsonbench/` — the CLI template: `jsonbench.go` (urfave/cli app),
  `ddl.go` (emit-or-apply with `--url/--user/--password`, guard rails),
  `chpack.go` (minimal chclient wiring).
- `public/keelson/runtime/factsschema/factsschema.go` L38 —
  `DatabaseName = "boxer"`; reference it, do not restate the string.

**play / sqlapplet surface**:

- `apps/play/play_sankey_panel.go` — the Sankey contract: CTE `flows`
  (`source`, `target`, `value`, optional `label`, `tone`), optional CTE
  `nodes` (`id`, optional `label`, `stage`, `order`, `group`, `tone`); the
  final SELECT remains the chapter's own.
- `apps/sqlapplet/sqlapplet.go` `parseEndpoint` (L386) — frontmatter
  `endpoint:` vocabulary is `default` (env-configured live ClickHouse — what
  the catalog chapters need) and `introspection`; `parseTabs` (L408) — `tabs:`
  pins panel set and order, first entry is the landing tab.
- `apps/sqlapplet/sqlapplet_register.go` — books are `//go:embed` FS vars;
  add `bookcatalog` next to `bookcapmap`/`booktopo`/`bookgodep`.
  `apps/sqlapplet/bookcapmap/comp-map.md` — chapter front-matter example.
- Seed-shape sources (read the constants, not the memory of them):
  `play_series_panel.go`, `play_kanban_panel.go`, `play_sankey_panel.go`,
  `play_layeredgraph_panel.go`, `play_dist_panel.go`, `play_flow_panel.go`,
  `play_hierarchy.go` (treemap/icicle hierarchy contract).

## Milestone plan

Target layout: engine in `public/gov/datacatalog` (a governance concern, the
capmap precedent), battery in `public/gov/datacatalog/panelshapes`, CLI in
`apps/datacatalog`, book in `apps/sqlapplet/bookcatalog`.

**M0 — engine package.** Files: `snapshot.go` (types: table coordinate,
column meta `{Name, Type, Position}`, fetcher interface), `classify.go` (the
lifted card-driver probe returning kind + `TableDesc` + row config +
separator), `normstring.go` (ADR §SD4: `;name:type;` with sentinels, `\`
escapes `;:\` in names, strip `LowCardinality(…)` then map `Nullable(T)` →
`T?` — mind the nesting order `LowCardinality(Nullable(T))`; zero columns
yield `;`). Re-point `play_card_driver.go` at `classify.go`. Unit tests: both
separators, `_`-prefix skipping, escaping, nesting; no live ClickHouse.

**M1 — analysis.** `attrs.go`: attribute keys off a normalized copy —
`plain/<itemType>/<name>:<canonicaltype>` and
`tagged/<section>/<name>:<canonicaltype>` — sorted; `schema_hash` = fnv64a
over the joined list (stdlib `hash/fnv`, the sankey panel precedent).
`pairs.go`: lexicographic `(database, name)` ordering, `Relate` per unordered
pair, intersection keys → `shape_id`, `n_common`, jaccard. For equal/subset
pairs the intersection equals the contained side's key set, so its `shape_id`
must come out equal to that side's `schema_hash` — assert that in tests.
`panelshapes/`: `Shape{Name, Patterns, Note}`, seed set from the panel files
above, a compile-all test, one positive and one negative fixture per shape.
RE2 has no lookahead — conjunction is the battery, never one pattern.

**M2 — persistence + CLI.** `ddl.go` (four `CREATE OR REPLACE TABLE`
statements per ADR §SD2, MergeTree, ORDER BY as listed), `writer.go`
(chclient inserts), `run.go` (orchestrate: fetch → classify → analyze → match
→ write; mint `run_id`, stamp `discovered_at`). `apps/datacatalog`: urfave/cli
`refresh` with `--url/--user/--password/--dry-run` (dry-run prints DDL and
would-be row counts). Integration test (`//go:build integration`, run by
`scripts/ci/gotest-integration.sh`): scratch database, two related leeway
tables + one opaque series-shaped table, run refresh, assert kinds, the
`subset` pair, one shape row.

**M3 — keelson surface.** `panel_shapes` provider (columns `shape`,
`pattern`, `ordinal`, `note`; `FreshnessStatic`) implemented in
`panelshapes` itself (it may import `introspect`; the reverse edge is the one
to avoid) and registered in `introspecthost.go` beside `RegisterCoverage`.

**M4 — book.** `bookcatalog` chapters, `endpoint: default`: an inventory
overview; the Sankey hierarchy — sketch, to be refined against the panel:

```sql
WITH flows AS (
  SELECT concat('shape:', lower(hex(shape_id))) AS source,
         concat(database_b, '.', name_b)        AS target,
         toFloat64(n_common)                    AS value
  FROM boxer.tables_leeway_compatibility
  WHERE relation != 'disjoint' AND n_common >= 2
)
SELECT * FROM flows ORDER BY value DESC
```

— and an unmatched-opaque chapter (`tables_catalog` anti-joined against
`tables_opaque_shapes`). Shape attribute listings on demand via
`arrayIntersect(attr_keys, attr_keys)` over `tables_leeway`. Screenshot per
AGENTS.md § Screenshots path 2 (play's `BOXER_PLAY_SCREENSHOT` family,
`apps/play/play_renderer.go`).

## Verification commands

```sh
go build -tags="$(cat ./tags)" ./public/gov/datacatalog/... ./apps/datacatalog/...
go test  -tags="$(cat ./tags)" ./public/gov/datacatalog/... ./apps/play/...
go mod tidy --diff
./scripts/ci/gotest-integration.sh        # integration lane (live ClickHouse)
```

Never blanket-`gofmt -w` — the repo carries doc comments that gofmt mangles;
format only the files touched and audit the diff.

## Open questions for the implementer

- **`NormalizedCopy` export** — confirm the wrapper is acceptable upstream or
  find an existing exported path to a normalized `TableDesc`.
- **Insert path** — Arrow-batch sink vs SQL literal INSERTs (both cited
  above); either is fine at this scale.
- **Separator ambiguity** — a table parsing under both separators takes the
  sniffed one; the card driver's choice is the contract. Worth one fixture.
- **Engine coverage** — Views, MaterializedViews, Dictionary-engine tables
  are discovered and land as `opaque` with whatever `system.columns` reports;
  no special-casing in the first cut.
- **`CREATE OR REPLACE` visibility** — readers mid-rebuild can see a
  fresh-but-empty table; accepted by the ADR, but the book chapters should
  surface `run_id`/`discovered_at` so staleness is visible.
