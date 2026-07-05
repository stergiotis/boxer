---
type: how-to
audience: engineer with a specific task
status: draft
# reviewed-by: "@<handle>"     # fill in and uncomment when flipping to stable
# reviewed-date: YYYY-MM-DD    # fill in and uncomment when flipping to stable
---

> **Status: draft — pre-human-review.** Not verified; do not cite as authoritative.

# How to mint, split, and query fibonacci-tagged identifiers

This recipe covers the fibonacci-coded tagged-id scheme end to end: picking
tag values, minting ids in Go, splitting them back, and decoding them in
ClickHouse SQL — through the `LW_ID_*` macro expansion or the equivalent
UDFs. The decision record behind the scheme is
[ADR-0106](../adr/0106-identity-fibonacci-tags-build-tag-retirement.md); this
document does not re-argue it.

## When to use this recipe

- You need compact surrogate ids that carry their category (the *tag*) and a
  per-category counter (the *body*) in one `UInt64`, minted in Go and queried
  in ClickHouse.
- You have a column of tagged ids and want the tag, the body, or a
  filter-by-tag predicate in SQL without shipping a lookup table.
- You maintain a query pipeline and want the `LW_ID_*` names to work either
  compiled away (nanopass expansion) or server-side (UDFs).

## The scheme in one page

A `TaggedId` is a 64-bit word: the tag occupies the high bits as the
fibonacci code of `tagValue - 1`, the body fills the rest.

```
tag value 12  →  code 101011           (6 bits, ends in the "11" comma)
                 ┌──────┬──────────────────────────────────────────┐
id (uint64):     │101011│ body: 58 bits, minted 1,2,3,…            │
                 └──────┴──────────────────────────────────────────┘
```

Three properties carry everything:

- **Self-delimiting.** A fibonacci code is a Zeckendorf representation
  (no two adjacent 1 bits) closed by a final `11` pair — the *comma*.
  Scanning from the most significant bit, the first adjacent `11` pair in a
  tagged id is therefore always the tag's comma, whatever the body bits are:
  the id needs no out-of-band width to split, in any language.
- **Prefix-free.** No tag's code is a prefix of another's, so ids of
  different tags can never collide, even though tags have different widths.
- **Frequency-adaptive.** Small tag values get short codes and huge bodies
  (tag value 1 leaves 62 body bits); the largest uint32 tag values pay 47
  bits and still keep 2^17 - 1 bodies. Give hot categories small tag values.

Why this fits here, in brief: ids are self-describing across the Go/SQL
boundary (the split below is *the same algorithm* on both sides, locked by a
golden test); same-tag ids are a contiguous `UInt64` range, so tag filters
are sargable and same-tag columns compress well; the tag width is a per-value
property, not a compile-time constant — which is what retired the old
`identifier_tag_fixed*` build-tag axis. Tag value 0 and body 0 are reserved
as invalid, so the zero id can never be mistaken for data.

## Prerequisites

- Repo build tags on every Go invocation: `-tags "$(cat ./tags)"`.
- `clickhouse` / `clickhouse-local` on `$PATH` for the SQL steps (the
  server-truth tests skip without it; the recipe below needs it).

## Steps

### 1. Pick tag values

Use the advisor to get the tag-value range whose codes still leave room for
the ids you expect, or enumerate compression-friendly values directly:

```go
lo, hiExcl, err := fibonacci.SelectFittingTagValueRange(maxExpectedIds)
```

```sh
# every tag value of code width 6, with its bit pattern:
go run -tags "$(cat ./tags)" ./public/app leeway id tagvalue leadingzero \
    --tagWidth 6 --leadingZeros 0
```

Values whose codes lead with zeros produce numerically small tags — helpful
when the tagged column should compress tightly.

### 2. Mint ids in Go

The seam is `identifier.IdGeneratorI` (get-or-assign by natural key) with
implementations in [identgen](../../public/identity/identgen/EXPLANATION.md):
`mem` (in-memory interner), `internalized` (Badger-persistent interner),
`seq` (Badger per-tag counter that ignores the key).

```go
gen, err := mem.NewIdInternalizer(identifier.TagValue(12), 1024)
id, fresh, err := gen.GetId([]byte("de305d54-75b4-431b-adb2-eb6b9e546013"))
// id.Value() == 12393906174523604993 for the first key (body 1; body 0 is reserved)
```

Sharp edges worth knowing: store-backed factories allow **one generator per
tag** (`identgen.ErrTagInUse`) and hold the slot until the factory closes;
a spent tag returns errors matching `identgen.ErrIdSpaceExhausted`; the batch
seam `identgen.BatchInternalizerI.AppendIds` resolves whole key columns and
survives arbitrarily large batches by committing in chunks.

### 3. Split ids in Go

Everything derives from the bits — these are total functions, and invalid
input (no comma anywhere) comes back as detectable zeros, never garbage:

```go
tid := identifier.TaggedId(12393906174523604994)
tag, body := tid.Split()       // tag bits 0xac00000000000000, body 2
tag.GetValue()                 // TagValue(12)
tag.GetTagWidth()              // 6
tid.IsValid()                  // true; TaggedId(42).IsValid() == false
```

### 4. Decode ids in ClickHouse

Two interchangeable routes; both are golden-locked against the Go split
(2,529 ids per run, including adversarial random words) by
`public/identity/identsql/identsql_servertruth_test.go`.

**Route A — expand in-process (no DDL needed).** Run SQL containing the
macros through the nanopass before sending it:

```go
sent, err := identsql.ExpandPass.Run("SELECT LW_ID_BODY(id) FROM events")
```

The pass rewrites every call into pure integer bit arithmetic (fixpoint, so
nested calls work; wrong arity is an error). Matching is case- and
quoting-insensitive.

**Route B — install the UDFs once, query with the names directly.**

```sh
go run -tags "$(cat ./tags)" ./public/app leeway id udf | clickhouse-client -n
```

The statements are `CREATE OR REPLACE FUNCTION`, so re-running is safe.
A self-contained smoke test with `clickhouse-local` (UDFs live only for the
invocation there):

```sh
UDFS="$(go run -tags "$(cat ./tags)" ./public/app leeway id udf)"
clickhouse-local -n --query "$UDFS
SELECT id,
       LW_ID_IS_VALID(id)  AS valid,
       LW_ID_TAG_VALUE(id) AS tag,
       LW_ID_BODY(id)      AS body,
       LW_ID_TAG_WIDTH(id) AS width
FROM values('id UInt64', (12393906174523604993), (12393906174523604994), (1729382256910270465), (42))
FORMAT PrettyCompactNoEscapes"
```

```
   ┌───────────────────id─┬─valid─┬─tag─┬─body─┬─width─┐
1. │ 12393906174523604993 │     1 │  12 │    1 │     6 │
2. │ 12393906174523604994 │     1 │  12 │    2 │     6 │
3. │  1729382256910270465 │     1 │   5 │    1 │     5 │
4. │                   42 │     0 │   0 │    0 │     0 │
   └──────────────────────┴───────┴─────┴──────┴───────┘
```

### 5. Filter by tag without decoding

Same-tag ids are one contiguous range, so the constant-tag predicate folds
into a plain `BETWEEN` — sargable, index-prunable, no bit arithmetic at query
time. `ExampleExpandPass` in
[example_test.go](../../public/identity/identsql/example_test.go) pins it:

```sql
-- LW_ID_HAS_TAG(id, 12) expands to:
(id) BETWEEN 12393906174523604992 AND 12682136550675316735
```

With a non-constant tag value the macro (and the UDF, always) falls back to
decode-and-compare — correct, but not index-prunable, and guarded so an
invalid id never matches a zero tag value.

## The LW_ID_* functions

| Name | Arguments | Returns | Invalid id |
| --- | --- | --- | --- |
| `LW_ID_IS_VALID` | id | true when a comma exists | false |
| `LW_ID_TAG_WIDTH` | id | full tag width incl. comma bit, UInt16 | 0 |
| `LW_ID_TAG_BITS` | id | tag in place, body zeroed, UInt64 | 0 |
| `LW_ID_BODY` | id | body, UInt64 | 0 |
| `LW_ID_TAG_VALUE` | id | decoded tag value, UInt32 | 0 |
| `LW_ID_HAS_TAG` | id, tagValue | id carries exactly that tag | false |

How they work: `bitAnd(x, bitShiftLeft(x, 1))` marks the upper bit of every
adjacent `11` pair; the highest marked bit is the comma, recovered exactly
with `roundToExp2` + `bitCount` (ClickHouse has no leading-zeros builtin);
width, masks and the fib-weighted tag-value sum all derive from it. The
encoder's two biases cancel, so the Zeckendorf sum of the tag bits *is* the
tag value. UDF bodies and macro expansions are generated from the same
templates — `identsql.UdfDdlStatements()` is the programmatic seam behind
`leeway id udf`.

## Sharp edges

- Expansions splice the argument expression several times; ClickHouse
  evaluates identical subexpressions once, but the SQL text grows — keep
  macro arguments simple, or use the UDFs.
- ClickHouse resolves UDF names case-sensitively, while the expansion pass
  matches case-insensitively: `lw_id_body(id)` expands fine in-process but
  fails against a UDF-only server. Spell the names upper-snake.
- A `TaggedId` that decodes to a tag value beyond uint32 (possible only for
  raw bit patterns never produced by the encoder) reads as invalid — 0 — on
  both sides, by design.
- Two server quirks shape the generated expressions (details in the
  2026-07-05 update of ADR-0106): `UInt64 - 1` widens to Int64, and
  `bitShiftRight` silently no-ops on signed shift amounts. If you hand-write
  variants, keep masks shift-only and force shift amounts to `UInt8`.

## Related

- [ADR-0106](../adr/0106-identity-fibonacci-tags-build-tag-retirement.md) —
  the scheme decision, the split contract, kill-reasons for alternatives.
- [identgen EXPLANATION](../../public/identity/identgen/EXPLANATION.md) —
  generator taxonomy and invariants.
- `public/identity/identsql` — the pass, the UDF emission, and the
  server-truth golden lock.
