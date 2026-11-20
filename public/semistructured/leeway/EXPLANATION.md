---
type: explanation
audience: contributor
status: draft
# reviewed-by: "@<handle>"     # fill in and uncomment when flipping to stable
# reviewed-date: YYYY-MM-DD    # fill in and uncomment when flipping to stable
---

> **Status: draft — pre-human-review.** Not verified; do not cite as authoritative.

# `leeway` — Explanation

`leeway` is a schema-on-write, semi-structured columnar data-representation
pipeline. This document explains the problem it addresses by walking through a
familiar data-engineering scenario: ingesting JSON log events with nested fields
and schema drift, storing them in a columnar warehouse, and reading them back
reliably. The names and stages below are the ones used in the implementation.

## Background

A common batch/streaming pipeline receives events like this:

```json
{
  "ts": "2026-06-27T12:34:56Z",
  "event": "checkout",
  "user": { "id": 42, "geo": "EU" },
  "cart": [
    { "sku": "A1", "price": 9.99, "qty": 2 },
    { "sku": "B7", "price": 4.50, "qty": 1 }
  ],
  "flags": ["mobile", "returning"]
}
```

Different tenants, versions, or error paths may:

- omit `cart` entirely,
- represent `geo` as a string on one path and as an object on another,
- add new flags or new line-item fields next week,
- or emit the same logical event through different serialization shapes.

The traditional responses are painful: hand-written DDL migrations, fragile
JSON-extract queries, and generated code that drifts apart from the table it is
writing to. `leeway` tries to keep the schema, the serialized bytes, the
generated code, and the read path in sync by deriving all of them from one
source of truth.

## How it works

`leeway` splits the work into a six-stage pipeline. The same names appear in the
package layout under
[`github.com/stergiotis/boxer/public/semistructured/leeway`](.):

```text
Describe   →   IR   →   Map   →   DDL   →   Marshal   →   Query
```

The central choice is that values are **shredded by canonical type, not by
field**. A table does not get one column per logical field (`user.geo`,
`event`, …); it gets one *section* per value type, and each value in a
section carries a *membership* — the label that says which logical attribute
it is. `event` and `user.geo` are both strings, so both land in the same
string section, told apart by their memberships. A new string field next week
is a new membership, not a new column.

Every code block below is copied from an `Example` function that `go test`
compiles and runs; the example named under each block is the one to read
when the two disagree.

### 1. Describe

You describe the physical table with a `TableManipulator` in
[`github.com/stergiotis/boxer/public/semistructured/leeway/common`](common/).
Plain columns are present on every row; tagged-value sections hold the
labelled values (`ExampleTableManipulator` in
[common/example_test.go](common/example_test.go)):

```go
manip, err := common.NewTableManipulator()
if err != nil {
	panic(err)
}
manip.SetTableName("events")
manip.PlainValueColumn(common.PlainItemTypeEntityId, "id", ctabb.U64)
manip.PlainValueColumn(common.PlainItemTypeEntityTimestamp, "ts", ctabb.Z64)
manip.TaggedValueSection("symbol").
	AddSectionMembership(common.MembershipSpecLowCardVerbatim).
	TaggedValueColumn("value", ctabb.S)
manip.TaggedValueSection("f64").
	AddSectionMembership(common.MembershipSpecLowCardVerbatim).
	TaggedValueColumn("value", ctabb.F64)

tbl, err := manip.BuildTableDesc()
```

`ctabb` holds the canonical types as Go values (`U64`, `Z64` for a 64-bit
timestamp, `S` for a string, `F64`); the same types are written `u64`, `z64`,
`s`, `f64` in their string form (see the canonicaltypes skill). The
membership spec chooses how a membership is stored: `LowCardVerbatim` stores
the label itself as text; the `Ref` channels store a numeric id that a
registry resolves.

### 2. IR

`BuildTableDesc` returns a `TableDesc`; loading it into an
`IntermediateTableRepresentation` makes every physical column explicit,
including the support columns a membership channel costs on a given target
(`ExampleIntermediateTableRepresentation_LoadFromTable`):

```go
ir := common.NewIntermediateTableRepresentation()
err = ir.LoadFromTable(&tbl, clickhouse.NewTechnologySpecificCodeGenerator())
```

The IR is what the DDL, DML and read-access generators consume. It is
target-agnostic in shape; the technology passed to `LoadFromTable` only
decides which support columns a membership spec needs.

### 3. Map

[`github.com/stergiotis/boxer/public/semistructured/leeway/mappingplan`](mappingplan/)
models the logical shape of a DTO. A `Plan` is not written by hand: it is
derived from `lw:` struct tags, either by reflection
([`github.com/stergiotis/boxer/public/semistructured/leeway/marshall/go/marshallreflect`](marshall/go/marshallreflect/)) or by code generation
([`github.com/stergiotis/boxer/public/semistructured/leeway/marshall/go/marshallgen`](marshall/go/marshallgen/))
(`ExamplePlanFor` in
[marshall/go/marshallreflect/example_plan_test.go](marshall/go/marshallreflect/example_plan_test.go)):

```go
type checkout struct {
	_     struct{}  `kind:"checkout"`
	Id    uint64    `lw:",id"`
	Ts    time.Time `lw:",ts"`
	Event string    `lw:"event,symbol,verbatim"`
	Geo   string    `lw:"user.geo,symbol,verbatim"`
	Total float64   `lw:"cart.total,f64,verbatim"`
}
```

```go
plan, err := marshallreflect.PlanFor[checkout]()
```

A tag reads `membership,section,flags`; an empty membership marks a plain
column. The plan names sections and memberships, never physical columns, so
the same DTO can be marshalled into any table that has those sections, and
several DTOs (kinds) can share one table.

### 4. DDL

[`github.com/stergiotis/boxer/public/semistructured/leeway/ddl/clickhouse`](ddl/clickhouse/)
renders the ClickHouse table from the IR. Table-level clauses refer to
columns by their leeway coordinates (`ExampleComposeCreateTable` in
[ddl/clickhouse/example_test.go](ddl/clickhouse/example_test.go)):

```go
sql, err := clickhouse.ComposeCreateTable("events", ir, common.TableRowConfigMultiAttributesPerRow, conv, clickhouse.TableOptions{
	Engine:  "MergeTree()",
	OrderBy: []clickhouse.ColumnRef{{Plain: "ts"}, {Plain: "id"}},
})
```

which produces:

```sql
CREATE TABLE events (
	"id:id:u64:::0:" UInt64,
	"ts:ts:z64:::0:" DateTime64(9,'UTC'),
	"tv:symbol:value:val:s::::0::" Array(String),
	"tv:symbol:lv:lv:y:124:::0::" Array(LowCardinality(String)) CODEC(ZSTD(3)),
	"tv:symbol:lvcard:lvcard:u64:4E:::0::" Array(UInt64) CODEC(T64,ZSTD(3)),
	"tv:f64:value:val:f64::::0::" Array(Float64),
	"tv:f64:lv:lv:y:124:::0::" Array(LowCardinality(String)) CODEC(ZSTD(3)),
	"tv:f64:lvcard:lvcard:u64:4E:::0::" Array(UInt64) CODEC(T64,ZSTD(3))
) ENGINE = MergeTree()
ORDER BY ("ts:ts:z64:::0:", "id:id:u64:::0:")
```

- Each section is a group of parallel arrays: one element per attribute the
  row carries in that section.
- `value` holds the values; `lv` holds the verbatim membership labels;
  `lvcard` says how many memberships each attribute has (an attribute may
  carry more than one).
- The physical names encode section, role, canonical type and encoding
  hints, separated by the naming convention's separator (`:` here). They are
  generated; code never spells them by hand.

### 5. Marshal

A schema's write side and read side are generated Go code: the DML driver
([`github.com/stergiotis/boxer/public/semistructured/leeway/dml`](dml/)) emits a table builder, the read-access driver
([`github.com/stergiotis/boxer/public/semistructured/leeway/readaccess`](readaccess/)) emits typed readers. The reflection codec then
moves DTOs through them. Writing produces an Arrow record whose columns match
the DDL (`Example` in
[tutorial/firsttable/example_test.go](tutorial/firsttable/example_test.go),
where `NewInEntityEvents` is generated):

```go
table := firsttable.NewInEntityEvents(memory.NewGoAllocator(), len(rows))
err := marshallreflect.Marshal(table, rows, marshallreflect.NoLookup{})
if err != nil {
	panic(err)
}
recs, err := table.TransferRecords(nil)
```

`NoLookup` suffices because every membership in the DTO is verbatim; a `Ref`
channel needs a lookup that maps each membership name to its id.

### 6. Query

There are two read paths. In Go, the generated readers load a record and
`Unmarshal` rebuilds the DTOs (same example):

```go
readers := marshallreflect.NewSectionReaders(ra.GetNumberOfEntities()).
	PlainColumn("id", ra.EntityId.ValueId).
	PlainColumn("ts", ra.EntityTimestamp.ValueTs).
	Section("symbol", ra.Symbol.GetAttributes(), ra.Symbol.GetMemberships()).
	Section("f64", ra.F64.GetAttributes(), ra.F64.GetMemberships())
var got []Checkout
err = marshallreflect.Unmarshal(readers, &got, marshallreflect.NoLookup{})
```

In SQL,
[`github.com/stergiotis/boxer/public/semistructured/leeway/marshall/clickhouse/readback`](marshall/clickhouse/readback/)
joins a plan against the physical table and generates the presence test,
the projection and a validator for that kind (`ExampleGenerator_Generate` in
[marshall/clickhouse/readback/example_test.go](marshall/clickhouse/readback/example_test.go)).
The filter for the `checkout` DTO is:

```sql
hasAll("tv:symbol:lv:lv:y:124:::0::", ['event', 'user.geo']) AND has("tv:f64:lv:lv:y:124:::0::", 'cart.total') AND countEqual("tv:symbol:lv:lv:y:124:::0::", 'event') = 1 AND countEqual("tv:symbol:lv:lv:y:124:::0::", 'user.geo') = 1 AND countEqual("tv:f64:lv:lv:y:124:::0::", 'cart.total') = 1
```

The `has` / `hasAll` terms select rows that carry the memberships at all, and
are the ones a skip index can prune on; the `countEqual … = 1` terms check
that each mandatory field occurs exactly once. A query that asks for
`user.geo` therefore does not return a default value on rows that never
carried it. For hand-written SQL over leeway tables, start at
[the SQL read surface](../../../doc/explanation/leeway-sql-read-surface.md)
rather than at the physical names.

## Invariants

- A `TableDesc` is the single source of truth for physical layout. DDL,
  generated codecs, and read-back all consume it.
- Physical column names are self-describing. The mapping from column name back to
  section and type is deterministic.
- An attribute a row does not carry has no element in that row's section
  arrays. There is no nullable column per logical field, and a new field is a
  new membership, not a schema change.
- Plain values are row metadata; tagged values are shredded payload. Consumers
  must not mix the two representations.

## Trade-offs

- **Shredding vs. JSON blobs.** Shredding gives columnar compression and
  type-aware query performance, but it makes the write path more complex and
  requires generated codecs or reflection to stay consistent.
- **Self-describing column names.** The names are long and carry metadata that a
  pure schema registry would hide, but they let code read tables independently
  of the registry that created them.
- **Sections by type, not by field.** One string section serves every string
  field, so the column count does not grow with the number of fields; the cost
  is that reading one field means locating its membership inside an array
  rather than naming a column.
- **Plan separate from `TableDesc`.** This decouples logical DTOs from physical
  storage, but it means there are two objects to keep aligned.

## Maturity notes

As of 2026-08, the write/marshal/DDL spine remains the most exercised part of
the pipeline. Read-back has since grown its own surfaces — a generated
per-component read path (ADR-0146), a ClickHouse read-back generator
(ADR-0066), and an installed SQL vocabulary (ADR-0171/0181) — so "read-back is
partial", which this section said through mid-2026, now understates it. Stream
read access and complete table-level DDL clauses are still partial.

## Further reading

- Reference:
  [`github.com/stergiotis/boxer/public/semistructured/leeway/common`](common/),
  [`github.com/stergiotis/boxer/public/semistructured/leeway/mappingplan`](mappingplan/),
  [`github.com/stergiotis/boxer/public/semistructured/leeway/marshall/go/marshallgen`](marshall/go/marshallgen/),
  [`github.com/stergiotis/boxer/public/semistructured/leeway/marshall/clickhouse/readback`](marshall/clickhouse/readback/)
- Decisions:
  [ADR-0042: Keelson leeway codec SoA generator](../../../doc/adr/0042-keelson-leeway-codec-soa-generator.md),
  [ADR-0066: leeway DQL ClickHouse read-back generator](../../../doc/adr/0066-leeway-dql-clickhouse-readback-generator.md),
  [ADR-0070: leeway entity assembly](../../../doc/adr/0070-leeway-entity-assembly.md),
  [ADR-0071: leeway value and emission](../../../doc/adr/0071-leeway-value-and-emission.md),
  [ADR-0072: leeway membership carriage](../../../doc/adr/0072-leeway-membership-carriage.md),
  [ADR-0073: leeway membership role](../../../doc/adr/0073-leeway-membership-role.md),
  [ADR-0074: leeway marshall package layout](../../../doc/adr/0074-leeway-marshall-package-layout.md),
  [ADR-0075: leeway typed component views](../../../doc/adr/0075-leeway-typed-component-views.md)
- The layer above this one — components: flat DTOs projected out of these
  rows, who may share a section, and where membership ids come from — is the
  [leeway-components skill](../../../doc/skills/leeway-components/SKILL.md).
  This document explains the pipeline; that one explains consuming it.
- Reading a table back from SQL rather than through Go:
  [the SQL read surface](../../../doc/explanation/leeway-sql-read-surface.md).
- Orientation:
  [Why boxer](../../../doc/explanation/why-boxer.md)
