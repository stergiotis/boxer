---
type: adr
status: accepted
date: 2026-09-24
reviewed-by: "p@stergiotis"
reviewed-date: 2026-09-24
---

# ADR-0257: ClickHouse Arrow results into column structs — `chrows`

## Context

Go code in the tree reads ClickHouse results two ways, and both carried a
cost that repeated from caller to caller.

- **Readers that want Go values asked for JSONEachRow.** The
  `keelson.query.<table>` readers of ADR-0253 decoded rows through `json`
  tags because the repo had no ArrowStream-to-Go decoder, while the wire,
  the service and the engine were format-neutral and the engine's inputs
  were Arrow already. Whether a 64-bit integer arrives quoted in JSON is
  the engine setting `output_format_json_quote_64bit_integers`, whose
  default has differed across ClickHouse releases. The readers pinned
  nothing, and their hand-written JSON fixtures could not notice.
- **Readers that walk Arrow wrote their own cell switches.** play's panels,
  the gloss cell, tally, the vector-field decoder of ADR-0250 and others
  each carried a type switch per question — which arrays hold an integer,
  a text, a moment — and the copies had drifted apart: one read `Binary` as
  text and another did not, one read dictionaries and another did not, and
  one cast a `UInt64` to `int64` without a range check. Downstream modules
  had grown a further standalone cell package and a row decoder of their
  own.

ClickHouse's ArrowStream spells one SQL type several ways. A `String` is
utf8 or binary (`output_format_arrow_string_as_string`). A `LowCardinality`
column is plain or a dictionary
(`output_format_arrow_low_cardinality_as_dictionary`). A `DateTime` is a
bare `uint32` of seconds, a `Date` a `date32`, a `FixedString` a
fixed-size binary. The body is lz4-frame-compressed by default.

The house standard asks for struct-of-arrays over array-of-structs, and for
iterators to assemble row views where a row is what the caller needs.

## Decision

We will add `public/db/clickhouse/chrows` as the one reader of ClickHouse
Arrow results: per-cell readers that absorb ClickHouse's Arrow spellings,
and a decoder that lays a result into a struct of column slices or,
where rows are the caller's unit, a slice of row structs. The
`keelson.query` Go readers move to ArrowStream through it, and the cell
switches that duplicated its readers delegate to them.

### SD1 — Cell readers own the mechanics, callers own the policy

`Int64`, `Uint64`, `Float64`, `Bool`, `String`, `Bytes`, `EpochMillis` and
`Time` each take `(arrow.Array, row)` and return `(value, ok)`, with `ok`
false for NULL, a row out of range, an array the question does not apply
to, and an integer that does not fit. Each one dereferences a dictionary
first. The type predicates (`IsInteger`, `IsNumeric`, `IsDecimal`,
`IsStringLike`, `ValueType`) answer the same questions over a schema.

A reader decides what a cell *is*, never what a caller does with it:
treating a NaN as missing, making text UTF-8-safe for a wire, and reading
an integer as epoch seconds because a leeway column is temporal all stay
with the caller that has that policy. `String` and `Bytes` alias the
array's buffer and say so. `EpochMillis` does not read integers, because
only a column's origin says an integer is a moment. `Time` reads a
`uint32` as seconds, because that is the one spelling ClickHouse gives a
`DateTime` and a `time.Time` field has already stated the intent.

### SD2 — A result decodes into column slices, or into rows

`Decode(dst, rec)` and `DecodeStream(dst, r)` append a result to `dst`, a
pointer to a struct whose fields are slices tagged `ch:"<column>"`. The
element types are the scalars (string, []byte, bool, sized integers,
floats, `time.Time`, and named types over them) and slices of those for
`Array` columns. The decoder is strict, and binds once per schema, so a
mismatch is one error naming the column rather than one per row:

- A tagged column the result lacks is an error unless the tag says
  `,optional`.
- A column whose type the field cannot hold is an error. Integers widen and
  narrow only with a per-value range check.
- A NULL is an error unless a `[]bool` field tagged `ch:"<column>,valid"`
  exists to record it. The value slice then takes the zero value.
- A result column that no field names is ignored.
- Values are copied out of the batch, so the destination outlives the
  record. A NULL-free primitive column whose Go type matches the field is
  copied as a block.

`DecodeRows[T]` and `DecodeRowsStream[T]` decode a batch or a stream into `[]T` for a
row struct `T`, whose fields hold one value per column — a `bool`
`,valid` field, a slice for an `Array` column. They share the binding and
the cell path with `Decode`, so the two shapes cannot disagree on what a
column holds. The column struct is the default; the row struct is for a
caller whose rows are what it hands on, such as a query helper whose
callers each keep their own row type.

`EncodeStream(w, src)` is the inverse over the same struct, writing one
Arrow IPC stream batch. It serves fixtures, which then produce the body
shape a real reply has, and any publisher that holds columns.

### SD3 — One reader, and what stays outside it

Code that reads a scalar out of a ClickHouse result — an integer, a text, a
moment, a flag — goes through SD1 or SD2, in this module and downstream.
Four kinds of Arrow code stay outside, because they answer a different
question:

- leeway's structural codecs (canonical forms, the stream read-access
  driver, `marshallreflect`), which read leeway's typed sections rather
  than a SQL result;
- display formatting (`gloss.FormatArrowElem`) and play's type-preserving
  sort comparator;
- walkers of list structure (play's hierarchy, network and timeline
  panels) and checks that a column *is* a given type as part of a panel's
  contract;
- builders, which write Arrow rather than read it.

### SD4 — `keelson.query` Go readers ask for ArrowStream

`keelsonquery.Columns` / `ColumnsWith` replace `Rows[T]` / `RowsWith[T]`.
They fix the request's FORMAT to ArrowStream and decode through SD2. The
wire of ADR-0253 §SD2 is unchanged. JSONEachRow stays the format for a body
read as text rather than decoded, such as a model's tool result
(ADR-0254).

The windows keep their columns as their storage and assemble a row view
(`Row(i)`, `All()`) only where a row is the unit of the interaction, as
with a Delete that names its entry.

## Surfaces — Tier 1

| Surface | Change | Moves with it |
| --- | --- | --- |
| `public/db/clickhouse/chrows` (exported Go API) | added | package props entry in the harvested proptable |
| `keelsonquery.Rows`, `RowsWith` | removed; `Columns`, `ColumnsWith` and `FormatArrowStream` added | the watchbill and app-state windows and their fixtures, ADR-0253 §SD6 (dated update there) |
| `keelsonQueryRequest` / `keelsonQueryReply` wire | unchanged | — |
| `gloss.ArrowCell` numeric and raw accessors | reimplemented over SD1; reads decimals of every width, dictionaries and view layouts it did not before | — |

## Alternatives

- **Keep JSONEachRow and pin the quoting setting.** Killed: it closes the
  integer hazard and nothing else. The per-caller Arrow switches stay, and
  the Go reader still round-trips Arrow through text.
- **Decode into `[]T` rows only.** Killed by the struct-of-arrays standard
  as the default: rows are the view a caller assembles where it needs one.
  Kept as a second shape over the same binding (SD2).
- **arrow-go's `arreflect`** (`ToSlice`, `RecordToSlice`), the decoder a
  downstream query helper used. It is strict and range-checked. Run against
  the golden bodies of the verification plan, it refuses ClickHouse's
  default spellings: a `binary` or dictionary-of-binary string into a
  `string`, a `UInt8` flag into a `bool`, a `uint32` `DateTime` into a
  `time.Time`, a decimal or an integer into a `float64`, and a signed
  integer into an unsigned field even when the value is in range. It reads
  a NULL as the zero value unless the field is a pointer. Killed as the
  decoder: every caller would re-grow the conversions SD1 holds.
- **Reuse `json` tags.** Killed: JSON options (`omitempty`, `,string`) would
  be silently meaningless here, and a separate key keeps a struct's two
  encodings from being confused.
- **leeway's `marshallreflect`.** Killed: it maps leeway-shaped tables
  (sections, memberships), not a flat SQL result.
- **Leave the cell switches where they are.** Killed: the drift described
  in Context is the cost of leaving them.

## Consequences

### Positive

- Integer width and signedness come from the column type, not from an
  engine setting, and a value that does not fit its field is an error
  rather than a truncation.
- One place knows ClickHouse's Arrow spellings. A setting that changes a
  spelling is absorbed in one reader.
- Fixtures produce real reply bodies through `EncodeStream`, so the tests
  of the consumers exercise the decoder they ship with.

### Negative

- A small result is larger as ArrowStream than as JSONEachRow: the schema
  and the compression frame precede the rows. For the windows' row counts
  this is a few hundred bytes a reply.
- The decoder reads through reflection per cell, except in the block-copy
  case. That is proportionate for window-sized reads, not for a bulk path.
- An `Array` column decodes into `[][]T`, one allocation per row.
  *Deferred:* a flat values-plus-offsets field shape, when a bulk consumer
  asks for it.

### Neutral

- The consolidation makes some readers wider and some stricter. A reader
  that accepted only `Boolean` for a flag now also reads a `UInt8`; one
  that wrapped a negative integer into a `uint64` now refuses it. Text a
  caller keeps past its batch is cloned where the former reader copied
  it, since SD1's `String` aliases.

## Migration — Tier 1

- **Breaks.** `keelsonquery.Rows[T]` and `RowsWith[T]` no longer exist.
  Downstream row structs tagged `arrow:"…"` for `arreflect` bind by the
  `ch` tag instead, and a field that bound by its Go name needs a tag.
- **Path.** Replace the row struct's `json:"col"` tags with a struct of
  slices tagged `ch:"col"`, and call `Columns(ctx, cli, table, sql, &cols)`.
  A NULL-able column gains a `ch:"col,valid"` `[]bool` companion.
- **Regeneration.** None; no generated code is involved. The package props
  table is re-harvested once the package is tracked.
- **Old shape.** Removed outright; both in-tree callers moved in the same
  change.

## Verification plan — Tier 1

- **Lane.** Default `go test`. Golden ArrowStream bodies captured from
  clickhouse-local under both string and dictionary spellings; `rapid`
  properties that `EncodeStream` / `Decode` round-trip and that `Decode`
  and `DecodeRows` read one body alike; the `keelsonquery`
  service tests, which run the real service over clickhouse-local and
  skip without the binary.
- **What would fail.** A reader that drops a spelling, a range check that
  truncates, or a NULL that decodes silently fails the golden or refusal
  tests. A service that stops honouring the request's FORMAT fails the
  service tests.
- **Gap.** The goldens pin one ClickHouse release's output. A release that
  changes a default spelling is caught only when the goldens are
  regenerated against it, or by the service tests on a machine with that
  release.

## Status

Accepted 2026-09-24. Proposed, built across this module and the downstream
modules that read ClickHouse results, and accepted on the same day.

Status lifecycle: `Proposed → Accepted → (Deferred | Deprecated | Superseded by ADR-XXXX)`.
See [DOCUMENTATION_STANDARD §1 ADR](../DOCUMENTATION_STANDARD.md#architecture-decision-records-why-it-is-this-way) for the edit-policy tiers (Tier 1 in-place / Tier 2 dated `## Updates` entry / Tier 3 new superseding ADR).

## References

- [ADR-0253](./0253-introspection-table-reads-as-a-bus-capability.md) — §SD2 the format-neutral wire, §SD6 the readers this moves.
- [ADR-0250](./0250-a-sql-backed-vector-field-source-and-plays-vector-field-pane.md) — the vector-field reply decoder that delegates to SD1.
- [ADR-0254](./0254-model-inference-as-a-keelson-capability.md) — the model tool that keeps JSONEachRow.
- [ADR-0042](./0042-keelson-leeway-codec-soa-generator.md) — the struct-of-arrays stance on the bus codecs.
