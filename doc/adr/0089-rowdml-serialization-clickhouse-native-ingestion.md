---
type: adr
status: accepted
date: 2026-06-16
reviewed-by: "@spx"
reviewed-date: 2026-06-17
---

# ADR-0089: Row-DML serialization — keep the bus wire and ClickHouse ingestion separate

## Context

[ADR-0042](./0042-keelson-leeway-codec-soa-generator.md) made **sparse, self-describing CBOR** the bus transport codec for `runtime.facts` rows (the `arrowrowcbor` shim feeding `dml_cbor`). ClickHouse ingestion of the facts store is a *separate* path: `chstore` buffers the generated `<Kind>Columns` SoA into **columnar Arrow IPC** and ships it via `chclient.InsertArrow` (`FORMAT Arrow`). The same in-memory buffer is serialized two ways, for two destinations.

A proposal asked whether to collapse that into **one** wire: adopt **protobuf** as the row-DML serialization target so the bytes are "1:1 ClickHouse-ingestible" (CH supports `FORMAT Protobuf` natively) and read them back with **hyperpb**. The appeal is unification — one wire that is both the bus codec and the CH ingest format.

This ADR records the investigation of that proposal. Its conclusion is not "pick a different format" but "**the unification premise itself is unsound**": the bus wire and the ClickHouse-ingest wire legitimately want different bytes, so no single wire serves both well. The right place to unify is the in-memory SoA, which is already where unification happens.

Two facts frame everything below:

- ClickHouse ingests **protobuf, MsgPack, BSON, CapnProto** natively, but **not CBOR**.
- Per leeway's own model, tree-shaped row formats — JSON, CBOR, BSON, MsgPack, protobuf — **cannot capture leeway's columnar dictionary structure**; that structure lives in ClickHouse storage regardless of which row format ingested it. So the row-format choice is a *transport / ingestion-convenience* decision, not a leeway-architecture decision.

[ADR-0036](./0036-runtime-buscodec.md) already evaluated protobuf and MsgPack for the generic bus codec and rejected both — but on criteria that never weighed ClickHouse ingestion. This ADR adds that criterion, measures it, and finds the unification it would enable is not worth having.

## Why a unified wire fails: a three-way tension

The bus shim does not emit column names. It emits **short keys**, deliberately dropping the leeway-encoded suffix to save bytes (`arrowrowcbor.shortKeyForFieldName`):

```
ClickHouse column name:  tv:symbol:value:val:s:m:0:24:0::data
bus CBOR key:            symbol.value      ← the suffix :val:s:m:0:24:0::data is dropped
```

That suffix (column role, canonical type `s`, low-cardinality flag `m`, base62 encoding hints) is **not recoverable** from `symbol.value`. So the bus CBOR cannot be name-matched to ClickHouse columns; a CH-ingestible CBOR would have to carry the *full* column names as keys — a different, bulkier wire than the bus uses. (It is not only the keys: the bus also encodes values for transport, e.g. timestamps as raw `int64` rather than ClickHouse `DateTime64`.)

Three consumers want three incompatible things from the same bytes:

| | Bus | ClickHouse | Leeway |
|---|---|---|---|
| **key shape** | short (`symbol.value`) | matches the column name | full schema-encoding name (`tv:symbol:value:val:s:m:0:24:0::data`) |
| **optimizes for** | wire size | name-matched ingestion | restoring the schema from physical column names |

No single wire satisfies all three. Every "unify the wire" option resolves the conflict by sacrificing one consumer:

- **Protobuf** is not self-describing at all: its field IDs are opaque integers and its wire-types are value-ambiguous, so without the descriptor a reader can *skip* fields but cannot name or type them — and it forces a column rename (`:`→`_`, because CH protobuf matches `_`≡`.` but not `:`).
- **BSONEachRow** keeps names and discoverability but needs the *full* column-name keys (not the bus short keys) and pays the bulkiest wire.
- **CBOREachRow** keeps the compact wire only if the bus stops using short keys (regressing bus size) or if ClickHouse learns leeway's short-key→column expansion (leaky), and needs a C++ contribution besides.
- **MsgPack** (current CH format) is positional — no names at all.

This is why the options all feel bad: the question is asking one artifact to be transport-optimal, ingest-matched, and schema-encoding at once.

### What each wire yields without the schema

The "discoverability" the options trade on is a precise, *partial* thing, and the same ceiling binds both formats:

| Level | CBOR short keys | Protobuf field IDs |
|---|---|---|
| structure (iterate / skip fields) | ✓ from wire | ✓ from wire (tags self-delimit) |
| field identity | a meaningful *name* (`symbol.value`) | an opaque *integer* (`26`) |
| value type (string / int / float / array) | ✓ — CBOR values are self-typed | ✗ — wire-types are ambiguous (varint = which int / zigzag?; len-delimited = string / bytes / packed / message?) |
| leeway semantics (canonical type, role, column name) | ✗ — needs `TableDesc` | ✗ — needs `.proto` **and** `TableDesc` |

Without a schema, protobuf is a stream of *skippable opaque tokens*; CBOR is a *typed, named tree* (`{symbol.value: ['grant','app7'], id: 1}`) — human-readable, greppable, diffable. That is the whole of CBOR's discoverability edge. But **neither reaches the leeway meaning without the `TableDesc`** — the same point as the SoA pivot below: the schema is always the authority, so the wire's self-description is a secondary, debug-tier property. Its value is bounded to schema-less *inspection* (a generic viewer, log-diffing, an external producer eyeballing bytes); every in-tree consumer holds the `TableDesc` and gains little from it.

## The unification point that works: the SoA

The bus wire and the CH-ingest wire are both *projections* of one in-memory form — the generated `<Kind>Columns` SoA — and both are produced from the single `TableDesc`:

```
bus wire ──decode──▶  <Kind>Columns (SoA)  ──emit──▶  Arrow IPC → ClickHouse
 (short-key CBOR,         the pivot                   (columnar, what CH ingests best)
  transport-optimal)      one TableDesc
```

This is already the architecture today (the bus decode path is `CBOR → cborarrow → Arrow → ra → <Kind>Columns`). "Two serializers" is **two codegen outputs from one schema**, not a maintenance tax. Seen this way the current split is not a fallback — it is the correct factoring: unify the in-memory form, let each edge serialize how it likes. The wire diversity is a feature.

## Design space (QOC)

**Question.** Should row-DML output use a single wire that is both the bus codec and directly ClickHouse-ingestible (as proposed with protobuf), or keep the bus wire and the CH-ingest wire as separate projections of the SoA?

**Options.** **O1** keep separate (status quo: short-key CBOR bus + Arrow bulk ingest). **O2** protobuf (generated `.proto`, hand-rolled emit shim, generated reflection-free read). **O3** ClickHouse `MsgPack`. **O4** `BSONEachRow` (hand-rolled `arrowrowbson` shim). **O5** `CBOREachRow` (contribute a CBOR row-input format to ClickHouse).

**Criteria.** **C1** self-contained CH-native ingestion (no boxer-Go transcode). **C2** schema-less inspectability — does the wire read as a typed, named tree *without* the schema (this is *not* full interpretability; leeway semantics need the `TableDesc` for every option here — see "What each wire yields without the schema"). **C3** no column rename / descriptor coupling. **C4** wire size. **C5** encode/decode cost + allocations. **C6** TinyGo/wasm read portability. **C7** implementation + maintenance cost.

|    | O1 keep separate | O2 protobuf | O3 msgpack | O4 BSONEachRow | O5 CBOREachRow |
|----|:--:|:--:|:--:|:--:|:--:|
| C1 | −  | ++ | +  | ++ | ++ |
| C2 | ++ | −− | −  | ++ | ++ |
| C3 | ++ | −− | +  | ++ | +  |
| C4 | ++ | ++ | +  | −− | ++ |
| C5 | +  | ++ | +  | +  | ++ |
| C6 | +  | +  | +  | +  | +  |
| C7 | ++ | −  | −  | +  | −− |

The matrix ranks candidate *unified wires*, but it cannot express the decisive point — that C1 is only worth anything to an external producer (Decision below), and that no row carries one wire able to serve bus, CH, and leeway at once. That is the "why unification fails" section, not a cell.

### Measured evidence

Indicative measurements from a representative synthetic harness modelling `runtime.facts` row shapes (a lean ~16-field "Grant"-like row, a rich ~51-field "Log5"-like row) — **not** boxer-integrated benchmarks. Tooling: `clickhouse-local` 26.5; Go 1.26 native decode; TinyGo 0.39 + Go 1.25 → wasm under node WASI. Medians; order-of-magnitude.

**Wire size (raw bytes/row).** BSON is bulkiest — it encodes every array as an index-keyed sub-document (`["a","b"]` → `{"0":"a","1":"b"}`), a per-element tax that hits leeway's all-array rows hardest.

| fields | protobuf | CBOR | BSON |
|---|---|---|---|
| lean (16) | 150 | 326 | 663 |
| rich (51) | 339 | 1035 | 1828 |

**Decode (ns/op, allocs/op).** The large spread is *reflection-avoidance*, not the format: a generated reflection-free reader is ~10–20× faster than schema-less `map` decode for **any** format, and ~6× faster than hyperpb (which is amd64/arm64-only — unusable on the wasm read path, so it drops out).

| decoder | lean | rich |
|---|---|---|
| protobuf, reflection-free (generated-class) | 324 / 7 | 705 / 13 |
| BSON, reflection-free (hand-rolled) | 517 / 7 | 1 435 / 13 |
| hyperpb (dynamic) | 1 995 / 13 | 3 740 / 19 |
| CBOR → `map` (fxamacker, schema-less) | 3 646 / 69 | 9 967 / 198 |
| BSON → mongo-driver (reflection) | 11 400 / 212 | 32 600 / 630 |

**Encode (ns/op, allocs/op).** Hand-rolled emit vs the mongo-driver library — any of these shims would be hand-rolled, matching `arrowrowcbor`.

| encoder | lean | rich |
|---|---|---|
| BSON, hand-rolled (reused buffer) | 512 / 0 | 1 209 / 0 |
| BSON, mongo-driver `Marshal` | 3 558 / 34 | 9 846 / 108 |

**BSONEachRow ingestion (clickhouse-local 26.5, verified).** Columns named with leeway's literal colon convention (`tv:symbol:value`) matched BSON keys **verbatim**; documents that omitted fields filled those columns with defaults (`[]` / `0` — sparse-tolerant); BSON arrays mapped to `Array(T)`. Zero ClickHouse changes. This confirms a CH-native, by-exact-name, sparse ingest path *exists* — the question this ADR answers is whether the bus should produce it, and the answer is no (the bus emits short keys, not these names).

**Compression context.** At batch=1000, raw protobuf is ~2× smaller than CBOR, but **zstd-compressed it is ~3.8× larger** (CBOR's repeated keys compress to almost nothing; protobuf's dense rows resist). The bus is uncompressed and batches go via Arrow, so protobuf's raw-size edge neither applies to bulk nor survives compression.

## Decision

**Do not unify the row-DML wire with ClickHouse ingestion.** Keep the bus wire (short-key, transport-optimal CBOR) and the CH bulk-ingest wire (Arrow columnar, storage-optimal) as two deliberately-different projections of one in-memory `<Kind>Columns` SoA, both generated from `TableDesc`. The SoA is the unification point; the wire is not.

**Reject protobuf (O2)** as a bus/row-DML serialization target: its only unique benefit is a self-contained CH-native wire, and it buys that by discarding exactly the leeway-stack values — self-description and the schema-encoding physical names — while its raw-size edge does not survive compression and is moot for bulk (Arrow).

A self-contained CH-native wire (**BSONEachRow** today, zero CH changes; or a contributed **CBOREachRow** for compactness on the exact existing wire) is scoped **only to a future external, non-boxer-Go producer** that needs to write directly into the facts table without going through `<Kind>Columns`. That is a separate concern, decoupled from the bus, and is built only when such a producer exists. For every in-tree path, the SoA pivot already serves it.

## Alternatives

- **O2 protobuf** — rejected (Decision). [ADR-0036](./0036-runtime-buscodec.md) had already rejected it for the bus on IDL-friction grounds; ADR-0042's collapse onto one facts schema revived it (one generated `.proto`, not one per payload), but that does not outweigh the discoverability / rename / coupling costs once the premise is seen to be unsound.
- **O3 MsgPack** — rejected. ClickHouse's `MsgPack` is positional and schema-less: values mapped by column *order*, every column required, no field names ([ClickHouse #64261](https://github.com/ClickHouse/ClickHouse/issues/64261) requests a named variant; it does not exist). Loses both sparseness and names. Consistent with ADR-0036's rejection, now also failing the ingestion-shape test.
- **O4 BSONEachRow / O5 CBOREachRow as the bus wire** — rejected for the bus, retained only for the external-producer niche. BSON needs the full column-name keys the bus deliberately drops, and pays ~2× wire. CBOREachRow needs either a bus-size regression (full-name keys) or CH-side leeway knowledge, plus a ClickHouse C++ contribution. Neither earns its cost for an in-tree path the SoA already serves.
- **mongo-driver/bson as the codec** — rejected. Slowest decode and encode measured (212–630 allocs/row, 1.7 MB wasm); a hand-rolled shim is ~8× faster to encode (zero-alloc) and ~22× faster to decode.

## Consequences

### Positive

- The protobuf direction is closed with measured kill-reasons, and the *reason the options looked bad* — the three-way key-shape tension — is recorded so it need not be re-derived.
- The SoA-as-pivot invariant is named: future "make the wire X-ingestible" proposals are answered by re-serializing the SoA at the edge, not by changing the bus wire.
- No churn: the working short-key-CBOR-bus + Arrow-bulk-ingest split is unchanged, and is now framed as correct rather than provisional.
- A transferable finding: the decode win is reflection-avoidance, not protobuf — a generated reflection-free CBOR reader would retire the current `cborarrow → Arrow → ra` decode detour independently of this decision.

### Negative

- There is no single self-contained wire. An external, non-boxer-Go producer wanting direct facts ingestion must adopt one of BSONEachRow / CBOREachRow (with their costs); ClickHouse still cannot ingest the bus CBOR as-is.
- The "two serializers" framing depends on both staying generated from `TableDesc`; a hand-edited divergence would reintroduce a real maintenance cost.

### Neutral

- Arrow remains the bulk-ingest path (correct for columnar storage); untouched.
- hyperpb is shelved for this use, retained only as an option for genuinely dynamic-schema tooling.

## Status

Accepted — 2026-06-17 (reviewed by @spx). The decision in force is "do not unify the wire; keep the SoA pivot; reject protobuf." **Trigger to build a CH-native wire:** an external, non-boxer-Go producer needing to write directly into the facts table. At that point O4 (BSONEachRow, zero-CH-change) is the first rung and O5 (CBOREachRow) the compact long-term form — for *that* producer's path only, never the bus.

Status lifecycle: `Proposed → Accepted → (Deferred | Deprecated | Superseded by ADR-XXXX)`. See `doc/DOCUMENTATION_STANDARD.md` for the edit-policy tiers.

## References

- [ADR-0042](./0042-keelson-leeway-codec-soa-generator.md) — SoA codec generator; CBOR bus codec; `arrowrowcbor` / `dml_cbor`; the `<Kind>Columns` SoA that is the pivot here.
- [ADR-0036](./0036-runtime-buscodec.md) — bus codec seam; prior protobuf + MsgPack rejection on non-ingestion criteria.
- [ADR-0026](./0026-app-runtime-and-capability-subjects.md) — `runtime.facts` and the in-proc bus.
- [ADR-0066](./0066-leeway-dql-clickhouse-readback-generator.md) — ClickHouse read-back generator.
- [ADR-0077](./0077-keelson-browser-wasm-execution.md), [ADR-0078](./0078-tinygo-wasm-amenability-survey.md) — wasm/TinyGo read-path context (hyperpb's amd64/arm64 limit lands here).
- `public/keelson/runtime/factsstore/chstore` — current Arrow IPC ingest path; `arrowrowcbor.shortKeyForFieldName` — the short-key derivation that the tension turns on.
- External: ClickHouse `Protobuf`, `MsgPack`, `BSONEachRow` formats; `buf.build/go/hyperpb`; `go.mongodb.org/mongo-driver/v2/bson`.
