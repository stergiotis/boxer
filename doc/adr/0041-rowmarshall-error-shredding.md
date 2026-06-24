---
type: adr
status: accepted
date: 2026-05-17
reviewed-by: "p@stergiotis"
reviewed-date: 2026-06-08
---

# ADR-0041: rowmarshall — boxer error fully-shredded row layout

## Context

The `rowmarshall` package (`public/keelson/runtime/rowmarshall/`)
is a hand-coded ClickHouse RowBinary writer for `runtime.facts`. Until
this ADR it carried one fact kind (`CapabilityGrant`); each writer
produces one RowBinary row matching the producer-owned column subset
resolved by `colspec.go` from boxer's leeway IR.

Operational need: persist captured boxer errors (`eh.Errorf` chains
with PC-prefix-deduplicated stack streams, frame stubs, and optional
attached CBOR structured data from `eb.Build().Str(...)...Errorf(...)`)
into the same `runtime.facts` table, so error-capture observability
shares the leeway-shredded storage with the existing capability /
audit fact kinds.

Constraints in play:

- **Wire and storage discipline.** `runtime.facts` is one specific
  leeway-shredded table with a fixed section vocabulary; any new fact
  kind must route into existing sections (`string`, `symbol`, `u32`,
  `u64`, `blob`, …) without schema duplication.
- **Boxer egress is zerolog-coupled.** The canonical error egress
  `eh.MarshalError` returns a `zerolog.LogObjectMarshaler`, and the
  underlying `gatherFactsAndStacks` accessor is unexported. A
  non-zerolog consumer (a RowBinary writer) cannot walk the dedup'd
  stream tree without bouncing through zerolog or duplicating the
  PC-prefix dedup logic.
- **Boxer wire format is lossy by design.** Stack PCs and error-type
  identity do not survive `eh.MarshalError`; only frame strings
  (file/line/func), chain topology (id/parentId), and raw structured-
  data CBOR bytes do. Reconstruction into a real Go `error` that
  satisfies `errors.Is/As` would need new boxer-side constructors and
  is not required by any current consumer (logs and detail panes
  consume the presentation tree).
- **Tree-shaped payload, row-shaped storage.** A boxer error is a
  `Streams × Facts` tree of variable depth; `runtime.facts` is row-
  oriented. We must decide how to project a tree onto rows.
- **One-way wire.** rowmarshall's RowBinary path has no in-process
  Unmarshal counterpart by design — round-trip is via ClickHouse (or
  any RowBinary consumer); shredded rows are intended to be queried
  out of CH, not reconstituted in-memory.

## Design space (QOC)

**Question.** How should a boxer error chain map onto `runtime.facts`
rows and rowmarshall's section vocabulary?

**Options.**

- **O1 — One opaque blob row per error.** CBOR-encode the entire
  stream tree (matching `eh.MarshalError`'s wire shape verbatim) into a
  single `blob`-section entry; only plain identity columns are
  populated alongside. One row per error, one blob.
- **O2 — Shredded multi-row.** One `runtime.facts` row per fact (frame
  or message), sharing an `error_id` plain column and linked via a
  `parent_id` foreign key. Across-row joins reconstruct the tree.
- **O3 — One row per error, fully shredded into per-fact parallel
  arrays.** One row per error; each fact contributes one slot to each
  per-attribute section array (`string` for msg/source, `symbol` for
  func/streamName, `u32` for line, `u64` for factId/parentId, `blob`
  for data). Slots zip back into facts by position when queried.

**Criteria.**

- **C1 — SQL queryability.** Can a consumer answer "errors whose chain
  mentions file X" or "errors whose attached `trace_id` = Y" directly
  in ClickHouse without app-side CBOR decoding?
- **C2 — Code surface and complexity.** Lines of writer logic, number
  of new section emitters, drift-test burden.
- **C3 — Fidelity to the boxer egress shape.** Does a chlocal-driven
  round-trip (RowBinary in → JSONEachRow out) preserve the fields
  that `eh.WalkStreams` exposes?

**Assessment.** `++` strong positive, `+` positive, `−` negative, `−−` strong negative.

|    | O1 | O2 | O3 |
|----|----|----|----|
| C1 | −− | ++ | +  |
| C2 | ++ | −− | +  |
| C3 | ++ | +  | ++ |

O2 wins C1 but requires id allocation, multi-row writing, and a
significantly extended drift test. O1 trivially preserves the shape but
defeats the leeway-shredded philosophy and forces CBOR/JSON UDFs at
every query. O3 keeps the shredded discipline with bounded complexity
and a chlocal-validated round-trip — file-level and func-level queries
still work via ClickHouse array functions (`arrayJoin`, `arrayFilter`)
over the per-fact section arrays.

## Decision

We will adopt **O3 — one row per error, fully shredded into per-fact
parallel arrays**.

Each captured boxer error becomes one `runtime.facts` row with:

- Plain identity columns: `id` (caller-supplied correlation), `naturalKey`
  (opaque caller-supplied join key, optional), `ts` (capture timestamp).
  No `expiresAt`: errors have no TTL.
- Per-section parallel arrays in leeway-canonical layout
  (`val ‖ lr ‖ lrcard`):
  - `string` — `KindErrorMsg` (201), `KindErrorSource` (202)
  - `symbol` — `KindErrorFunc` (203), `KindErrorStreamName` (204)
  - `u32`    — `KindErrorLine` (205)
  - `u64`    — `KindErrorFactId` (206), `KindErrorParentId` (207)
  - `blob`   — `KindErrorData` (208)

Within each section:

- `val` is grouped by kind (all msgs, then all sources; all funcs,
  then all streamNames; all factIds, then all parentIds). One entry
  per fact per kind, so `len(val) = N × kindsInSection`.
- `lr` lists the **distinct kinds** in section in the writer's emission
  order. `len(lr) = kindsInSection`.
- `lrcard` lists the count of each kind. For Error every kind appears
  exactly N times, so `lrcard = [N, N, …]`.

The stream tree is reconstructed at query time by grouping facts with
the same `streamName` in first-seen order — there is no in-process
Unmarshal; consumers read via SQL over the parallel arrays.

To get the dedup'd stream tree out of a boxer error without going
through zerolog, we add a public **`eh.WalkStreams(err) []Stream`**
accessor to boxer (with `Stream` / `Fact` view types). The accessor
reuses the existing `gatherFactsAndStacks` + `addError` + `materialize`
pipeline so the dedup logic stays single-sourced with the zerolog
egress.

## Alternatives

- **Reconstruct a real `error` from a queried row.** Rejected for this
  ADR's scope — no current consumer needs it, and it requires new
  boxer-side constructors (`eh.NewFromFacts` or similar). Re-open if a
  use case emerges (e.g., RPC error propagation across process
  boundaries with `errors.Is/As` on the receiver).
- **In-process Unmarshal of the RowBinary wire.** Symmetric with the
  writer but adds a CH-format parser (or a streaming RowBinary
  tokenizer) the package otherwise doesn't need. clickhouse-local
  with `--input-format RowBinary` already provides the round-trip path
  for tests. If we later need fast in-process readers for non-CH
  consumers, that's a separate seam.
- **Sparse encoding with explicit fact-index back-references.** Each
  per-attribute section could carry a `factIdx` companion column,
  emitting slots only when the attribute is non-default. Saves wire
  bytes for typical errors (most facts have no attached data; frame
  stubs have no message). Adds complexity (every section becomes
  multi-column, no longer matching the simple value/lr/lrcard shape).
  Deferred to a future refinement gated on profiling.
- **Decode and shred attached data into polymorphic typed columns.**
  Today's `KindErrorData` carries opaque CBOR bytes; SQL queries
  against attached fields require CBOR UDFs. A full leeway treatment
  would decode each fact's `Data` map and shred k/v into per-type
  sections (`string:attachKey`, `string:attachStr`, `int64:attachI64`,
  …, with `u32:attachFactIdx` back-reference). Deferred — its own
  follow-up ADR / milestone, justified when SQL-side attached-data
  queries become a real consumer requirement.

## Consequences

### Positive

- **Single ingestion primitive for boxer errors.** Marshal via the
  existing `runtime.facts` row plumbing; no new table, no new
  CH-format path.
- **Boxer egress stays consistent.** A non-zerolog consumer
  (`eh.WalkStreams`) is now the documented seam. Future writers
  (other RowBinary tables, downstream RPC frames) consume the same
  view types without reimplementing dedup.
- **Per-attribute SQL queryability inside the row.** ClickHouse array
  functions (`arrayJoin`, `arrayFilter`) over the parallel arrays
  support "errors mentioning file X" or "errors whose fact with id=N
  has parentId=M" queries today, without CBOR/JSON UDFs.
- **No schema changes to `factsschema`.** All five required sections
  (`string` / `symbol` / `u32` / `u64` / `blob`) already exist; the
  writer just enumerates them in `errorRowBinaryColumnList` (colspec.go)
  and emits the RowBinary bytes.
- **Drift detector extends trivially.** `TestSchemaDrift` gained three
  entries (`blob`, `u32`, `u64`); a `TestError_RowBinary_Structure-
  Renders` guardrail asserts each section appears in the resolved
  `--structure` string.
- **chlocal round-trip is the test gate.** Five tests pipe wire bytes
  through `clickhouse-local --input-format RowBinary --output-format
  JSONEachRow` and assert the values appear — same pattern as the
  CapabilityGrant round-trip test. No second decoder to maintain.

### Negative

- **Always-emit per fact wastes wire bytes for typical sparse errors.**
  Every fact contributes one slot per kind regardless of presence —
  frame stubs leave `msg` empty, message facts leave `source/line/
  func` empty, most facts have empty `data`. For a 6-fact error the
  wire carries 6 × 8 = 48 per-fact entries (mostly empty). Empty
  strings cost 1 LEB128-zero byte; empty u32/u64 cost 4/8 bytes
  respectively. The cost is bounded and the alternative (sparse
  encoding with fact-index back-references) was explicitly deferred.
- **No in-process Unmarshal.** Round-trip is via clickhouse-local
  (or any RowBinary consumer); rebuilding `*Error` from a CH query
  result is the responsibility of a separate fact-reader. Documented
  in `error.go` type comment.
- **Lossy.** Stack PCs and error-type identity do not survive even on
  the wire boxer emits. Consumers using `errors.Is/As` must keep the
  original error.
- **Fact-id collision between frame stubs and the first message fact.**
  Frame stubs default to `Id=0`; `addError`'s `nextId` counter also
  starts at 0. The collision is inherited from boxer's existing
  zerolog egress and is disambiguated on the consumer side by field
  presence (Source/Line/Func vs Msg). Documented in `eh.Fact` view's
  comment.
- **Attached-data shredding deferred.** Querying inside structured
  data (e.g., `WHERE attached.trace_id = 'X'`) still requires a CBOR
  UDF in ClickHouse or app-side decode. The follow-up to shred CBOR
  maps into typed polymorphic columns is mentioned as a future ADR.

### Neutral

- **Boxer dependency.** This ADR ships alongside a public addition to
  `public/observability/eh` (`Stream`, `Fact`, `WalkStreams`).
  A downstream consumer bumps the boxer pin once the upstream PR lands; until
  then the change is consumed via the workspace go.work pin.
- **PC-prefix dedup behavior.** `isSubStack` is strict-prefix-only
  (`a >= b → false`), so most nested wrap chains produce one stream
  per distinct call site, not one per goroutine. Documented in the
  `feedback_eh_dedup_strict_prefix` memory; the writer is agnostic to
  how many streams `eh.WalkStreams` returns.

## Status

Accepted — 2026-06-08 (reviewed by p@stergiotis). The no-reflection-on-hot-path rule and the shredded parallel-array error layout are in force. The original hand-coded `rowmarshall` package was retired by ADR-0042's codegen retrofit; the encoding survives unchanged in [`codec/errkind`](../../public/keelson/runtime/codec/errkind/) and [`codec/capabilitygrant`](../../public/keelson/runtime/codec/capabilitygrant/).

Status lifecycle: `Proposed → Accepted → (Deferred | Deprecated | Superseded by ADR-XXXX)`.
See [`DOCUMENTATION_STANDARD.md`](../DOCUMENTATION_STANDARD.md) for the edit-policy tiers (Tier 1 in-place / Tier 2 dated `## Updates` entry / Tier 3 new superseding ADR).

<!--
## Updates

Tier-2 dated entries land here when implementation reveals a refinement, an aspirational
claim turns out false, or a milestone records what shipped. Single H2; add H3s dated
YYYY-MM-DD. Remove this HTML comment when the section first gains a real entry.
-->

## References

- [ADR-0010 — leeway-cbor-rpc-codec](0010-leeway-cbor-rpc-codec.md) — the `runtime.facts` codec referenced above.
- [ADR-0026 — app runtime + cap subjects](0026-app-runtime-and-capability-subjects.md) — the runtime layer that originally motivated `runtime.facts` schema.
- [`public/observability/eh/walk.go`](../../public/observability/eh/walk.go) — the upstream `WalkStreams` accessor.
- [`public/observability/eh/zerolog.go`](../../public/observability/eh/zerolog.go) — `MarshalError` / `gatherFactsAndStacks` (lines 230–445).
- [`public/keelson/runtime/codec/errkind/`](../../public/keelson/runtime/codec/errkind) — successor package (ADR-0042 M11 retrofit). The hand-coded `rowmarshall.Error` types and RowBinary writer this ADR introduced were replaced by codegen-emitted `ErrorColumns` + `Marshal` here; the shredded parallel-array encoding survives unchanged. The original `rowmarshall/{error,error_from_boxer,error_rowbinary,colspec}.go` files are retired.
- [`feedback_eh_dedup_strict_prefix`](../../) — agent memory documenting the PC-prefix dedup edge case.
