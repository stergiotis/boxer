---
type: adr
status: proposed
date: 2026-07-04
---

> **Status: proposed — pre-human-review.** The seam is implemented and its
> consumers migrated; the decision text below records the design dialogue.
> Do not cite as settled.

# ADR-0102: ClickHouse table-clause seam for leeway DDL

## Context

The leeway DDL generator emits only the column body. Every table-level
clause — ENGINE, ORDER BY, PARTITION BY, TTL, SETTINGS, data-skipping
indexes — was hand-wrapped as strings by each consumer independently:
the anchor/ecsdemo generation tests concatenate
`"CREATE TABLE … (" + columns + ") ENGINE = …"`, the recordstore
generator carried an opaque `DDLTail` (ADR-0100 Deferred), and keelson's
`factsschema/codegen.ComposeCreateTableSql(engineClause)` takes the whole
engine-plus-clauses string verbatim.

Two costs followed:

- **The physical-name footgun.** Plain columns are leeway-encoded like
  every other column (`"id:id:u64:2k:0:0:"`), so a hand-written
  `ORDER BY (id, ts)` names columns that do not exist — a failure mode
  actually hit while building ADR-0100 S1, discovered only at CREATE
  time.
- **The skip-index gap.** The ADR-0066 read-back Filter artefacts'
  `has()`/`hasAll()` presence conjuncts are shaped to prune via
  bloom-filter data-skipping indexes on the membership identity columns —
  and nothing could emit those indexes (the 2026-06-11 review's open
  D-6/P7 finding).

## Design space (QOC)

**Question.** Where do table-level DDL clauses live, and how are the
columns they reference named?

**Options.**

- **O1 — Status quo.** Per-consumer clause strings. Kill: the footgun and
  the index gap are structural; every new consumer re-derives both.
- **O2 — Generation-time options on the ClickHouse target** (chosen). A
  `TableOptions` passed to a new `ddl/clickhouse.ComposeCreateTable`,
  with **typed column references** resolved to physical names through
  the IR. The neutral `TableDesc` stays untouched.
- **O3 — Neutral ordering block in `TableDesc`.** Ordering is the one
  arguably target-agnostic clause (parquet/arrow sort metadata). Kill:
  it is materialization policy, not data model; it costs a
  TableDesc/DTO/CBOR migration; and the main consumer (recordstore)
  derives its ordering from the envelope roles anyway.
- **O4 — Full physique block in `TableDesc`** (engine/partition/TTL raw
  strings included). Kill: raw ClickHouse text inside the neutral source
  of truth, against the ADR-0074/ADR-0089 target discipline.

Sub-fork, skip-index declaration: as new `encodingaspects` (per-column
hints beside the codec aspects) versus an explicit index list in the
options. **The options list was chosen**: `encodingaspects` stays purely
codec-shaped, and an index is a table-level DDL object — declared with
the table's materialization, not with a column's encoding.

## Decision

Adopt **O2**. Specific decisions:

- **SD1 — `ComposeCreateTable`.**
  `ddl/clickhouse.ComposeCreateTable(tableName, ir, tableRowConfig, conv,
  opts)` renders the complete statement: the generated column body, the
  `INDEX` clauses, and the table-level clauses in ClickHouse's canonical
  order (ENGINE, PARTITION BY, ORDER BY, TTL, SETTINGS), plus a final raw
  `Tail`. It lives with the target (`ddl/clickhouse`), never in the
  neutral `ddl` core or `TableDesc`.
- **SD2 — Typed column references.** `ColumnRef` addresses a column by
  leeway coordinates — plain name, `PlainItemTypeE` lane (for role-bound
  callers), section + value column, or section + `ColumnRoleE` (e.g.
  `ColumnRoleLowCardRef`, the membership identity column read-back prunes
  on) — and resolution walks the IR through the naming convention.
  Zero-match and ambiguous references fail at generation time, which
  retires the physical-name footgun.
- **SD3 — v1 vocabulary.** Structured: create mode, Engine (required,
  raw engine expression), OrderBy (refs), Indexes (`IndexSpec{Ref, Type,
  Granularity, Name}` — Type is the raw index-type expression; an empty
  Name derives a stable identifier), Settings. Raw passthrough only:
  PartitionBy and TTL (structured treatment deferred until a consumer
  partitions). `Tail` remains the escape hatch of last resort.
- **SD4 — recordstore migration.** The store generator composes the full
  CREATE TABLE at generation time: defaults derived from the envelope
  roles (IF NOT EXISTS, `MergeTree()`, ORDER BY Key then Order, the
  low-cardinality settings), merged with `Input.DDL` overrides; the
  emitted `.out.sql` is the complete statement and `EnsureTable` executes
  it verbatim. The runtime `DDLTail` config survives only as a raw suffix
  appended after the composed statement. The example store declares one
  bloom-filter index on its symbol section's LowCardRef column — the
  shape the baked Scan filters prune on — exercised by every
  clickhouse-local suite run.
- **SD5 — keelson stays.** `factsschema/codegen.ComposeCreateTableSql`
  keeps its own composer; migrating it is its own change, triggered when
  the facts schema next needs a clause the string form makes awkward
  (e.g. skip indexes on the facts membership columns).

## Consequences

### Positive

- One clause composer replaces three per-consumer string wraps; ORDER BY
  and INDEX references are validated and physically resolved at
  generation time.
- The ADR-0066 skip-index gap is closable per table: the index the
  Filter artefacts were shaped for is now one `IndexSpec` away, and the
  recordstore example emits it.
- recordstore's `DDLTail` shrinks from "the whole engine clause" to a
  genuine escape hatch.

### Negative

- `TableOptions` is ClickHouse-vocabulary (raw engine/index-type
  expressions); a second DDL target would need its own options type —
  accepted, that is what "with the target" means.
- PartitionBy/TTL as raw strings keep a slice of the footgun (they may
  reference physical columns); structured treatment is deferred.

### Neutral

- The neutral `TableDesc`, its CBOR form and the schema docs are
  untouched.
- Index *selection* policy (which columns deserve indexes by default)
  stays with consumers; the seam only makes declaration possible.

## Alternatives

The QOC options carry the rankings; nuance:

- **O3** would let one schema carry its preferred sort order to parquet
  and arrow targets too. If that need materializes, a neutral ordering
  hint can be added later and rendered by this seam — the options struct
  would then default from it, which is forward-compatible with O2.
- **Aspects-based indexes** would have reused the per-column hint
  channel and its implementation-status filter. Rejected to keep the
  aspect vocabulary codec-only; revisit only if per-column index
  declarations start duplicating across many `TableOptions` literals.

## Status

Proposed — 2026-07-04. Implemented alongside this document (the
composer, its tests, the recordstore migration and the example index);
promote to `accepted` after review.

Status lifecycle: `Proposed → Accepted → (Deprecated | Superseded by ADR-XXXX)`.
See `doc/DOCUMENTATION_STANDARD.md` for the edit-policy tiers.

## References

- [ADR-0100: recordstore](0100-recordstore-generated-leeway-clickhouse-store.md)
  — the Deferred item this resolves (see its dated Update).
- [ADR-0066: leeway DQL ClickHouse read-back generator](0066-leeway-dql-clickhouse-readback-generator.md)
  — the Filter artefacts whose pruning the indexes serve.
- [ADR-0074: leeway marshall package layout](0074-leeway-marshall-package-layout.md)
  — the neutral-IR / target-tier discipline SD1 follows.
- [`public/semistructured/leeway/ddl/clickhouse/lw_ddl_clickhouse_table.go`](../../public/semistructured/leeway/ddl/clickhouse/lw_ddl_clickhouse_table.go)
  — the composer.
