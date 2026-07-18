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

`leeway` splits the work into a six-stage pipeline. The same names appear in
[`doc/leeway-map/VALUE-PROPOSITION.md`](../../../doc/leeway-map/VALUE-PROPOSITION.md)
and in the package layout under
[`github.com/stergiotis/boxer/public/semistructured/leeway`](.):

```text
Describe   →   IR   →   Map   →   DDL   →   Marshal   →   Query
```

### 1. Describe

You describe the physical table with a `TableManipulator` in
[`github.com/stergiotis/boxer/public/semistructured/leeway/common`](common/).
The description declares which values are plain (present on every row) and
which are tagged (optional or nested):

```go
tm := common.NewTableManipulator().
  WithName("events").
  WithMembershipChannel(common.MembershipChannelFacts).
  AddPlain("ts", canonicaltypes.MustParse("timestamp")).
  AddPlain("event", canonicaltypes.MustParse("text")).
  AddTagged("user", canonicaltypes.MustParse("group(id:uint64, geo:text)")).
  AddTagged("cart", canonicaltypes.MustParse("list(group(sku:text, price:float64, qty:uint32))")).
  AddTagged("flags", canonicaltypes.MustParse("list(text)"))
```

`ts` and `event` are **plain values** — backbone columns. `user`, `cart`, and
`flags` are **tagged values**: their payload is shredded into typed sections and
membership is encoded separately in a bitmask so missing sections cost one bit,
not a null column family.

### 2. IR

The manipulator produces an `IntermediateTableRepresentation`. This is the
target-agnostic contract: which sections exist, what canonical type each
section carries, and how memberships are organized. Consumers of the IR do not
need to know whether the eventual target is ClickHouse, Arrow, or generated Go
structs.

### 3. Map

[`github.com/stergiotis/boxer/public/semistructured/leeway/mappingplan`](mappingplan/)
models the logical shape of the incoming DTO. A `Plan` says how input fields
land in the physical table:

```go
p := mappingplan.NewPlan().
  Plain("ts").FromField("ts").
  Plain("event").FromField("event").
  Tagged("user").FromField("user").
  Tagged("cart").FromField("cart").
  Tagged("flags").FromField("flags")
```

The plan is deliberately not the physical schema. The same logical shape can map
to multiple physical layouts, and the mapping can be revised while the stored
columns remain addressable because the column names carry their own schema
metadata.

### 4. DDL

[`github.com/stergiotis/boxer/public/semistructured/leeway/ddl/clickhouse`](ddl/clickhouse/)
consumes the `TableDesc` and emits the ClickHouse table. The physical column
names encode the structure:

```sql
CREATE TABLE events (
    ts DateTime64(9),
    event LowCardinality(String),
    user_p_present UInt8,
    user_v_id Nullable(UInt64),
    user_v_geo Nullable(String),
    cart_p_present UInt8,
    cart_vlen UInt32,
    cart_v_sku Array(String),
    cart_v_price Array(Float64),
    cart_v_qty Array(UInt32),
    flags_p_present UInt8,
    flags_vlen UInt32,
    flags_v Array(String)
) ENGINE = MergeTree()
ORDER BY ts
```

- `_p_present` is the **membership** flag: does this row carry the section?
- `_v_*` are the shredded payload columns.
- `_vlen` carries list cardinality.

Because the column names are self-describing, the table can be read back even
when the code that wrote it has moved on.

### 5. Marshal

A producer such as Keelson uses a generated struct-of-arrays codec from
[`github.com/stergiotis/boxer/public/semistructured/leeway/marshall/go/marshallgen`](marshall/go/marshallgen/),
or the reflection-based runtime codec in
[`github.com/stergiotis/boxer/public/semistructured/leeway/marshall/go/marshallreflect`](marshall/go/marshallreflect/).
The write side flattens the logical event into the physical column arrays:

```go
batch := events.NewBatch()
batch.Ts = append(batch.Ts, ts)
batch.Event = append(batch.Event, "checkout")
batch.User.Append(present, id, geo)
batch.Cart.AppendItems(skus, prices, quantities)
batch.Flags.Append(flags)
```

The result is an Arrow record or RowBinary block that matches the DDL exactly.
`dml/runtime` handles nullability, length alignment, and membership bookkeeping.

### 6. Query

Rather than hand-writing SQL, read-back uses
[`github.com/stergiotis/boxer/public/semistructured/leeway/marshall/clickhouse/readback`](marshall/clickhouse/readback/)
to generate SQL from the plan and the physical `TableDesc`:

```go
sql, err := readback.Generate(p, plan, readback.Options{
    Projection: []string{"ts", "user.id", "cart.sku", "flags"},
    Validator:  readback.Exact,
})
```

The generated query might look like:

```sql
SELECT
    ts,
    user_v_id AS "user.id",
    cart_v_sku AS "cart.sku",
    flags_v AS "flags"
FROM events
WHERE has(user_p_present, 0)
```

The generator knows which physical columns implement each projected path, which
membership checks are needed, and how to validate that rows satisfy the query
shape. A query that asks for `user.id` cannot silently return default values on
rows that never carried `user`; it filters them out.

## Invariants

- A `TableDesc` is the single source of truth for physical layout. DDL,
  generated codecs, and read-back all consume it.
- Physical column names are self-describing. The mapping from column name back to
  section and type is deterministic.
- Missing sections are encoded by the membership bitmask, not by nullable
  families. The bit is cheap; the payload columns are written only for rows that
  carry them.
- Plain values are row metadata; tagged values are shredded payload. Consumers
  must not mix the two representations.

## Trade-offs

- **Shredding vs. JSON blobs.** Shredding gives columnar compression and
  type-aware query performance, but it makes the write path more complex and
  requires generated codecs or reflection to stay consistent.
- **Self-describing column names.** The names are long and carry metadata that a
  pure schema registry would hide, but they let code read tables independently
  of the registry that created them.
- **Plan separate from `TableDesc`.** This decouples logical DTOs from physical
  storage, but it means there are two objects to keep aligned.

## Maturity notes

As of mid-2026, the write/marshal/DDL spine is the most exercised part of the
pipeline. Read-back beyond scalars, stream read access, the leeway query
language, and complete table-level DDL clauses are still partial. See
[`doc/leeway-map/REVIEW-2026-06-11.md`](../../../doc/leeway-map/REVIEW-2026-06-11.md)
for a fuller gap list.

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
- Orientation:
  [`doc/leeway-map/VALUE-PROPOSITION.md`](../../../doc/leeway-map/VALUE-PROPOSITION.md)
