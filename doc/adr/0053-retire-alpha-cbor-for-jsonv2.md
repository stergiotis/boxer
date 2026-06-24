---
type: adr
status: accepted
date: 2026-04-22
reviewed-by: "p@stergiotis"
reviewed-date: 2026-04-22
---

# ADR-0053: Retire `alpha/cbor` in Favor of `jsonv2` + Leeway Ingestion

## Context

The package `public/alpha/cbor` is a hand-written, pull-style CBOR parser stack (~3.3k LoC, 51% line coverage) layered on top of `github.com/stergiotis/boxer/public/semistructured/cbor`. It is imported by 20+ files across `public/semistructured/cbor`, `public/chunk`, `public/app/datasource/imap`, and the `app/commands/cbor/*` CLI tools. The components:

- **`tokenizer.go`** — byte-level RFC 8949 lexer producing a flat `TokenE` stream.
- **`pathstatemachine.go` (PSM)** — tracks container depth plus JSON-pointer-style path elements as tokens stream in.
- **`pullparser.go`** — glues tokenizer + PSM + reader + tags into a `PullParserI`.
- **`checkingencoder.go`** — encoder wrapper that uses a PSM to reject ill-formed emission; same invariant set as the decoder.
- **`reader/{buffered,inmem,windowed}.go`** — three `ParseReaderI` implementations with a retained-buffer API for path-qualified temporary slices.

### Why the package was built

At the time, the Go standard library offered `encoding/json` with `json.Decoder.Token()`, which was clumsy for streaming ingestion and exposed neither a path nor a skip-value primitive. CBOR offered wire-level advantages that JSON-via-stdlib did not:

- **O(1) skip.** CBOR scalars are `header + explicit-length bytes`; the reader advances by N without scanning content. JSON requires byte-by-byte scanning of strings (escape handling) and numbers (delimiter search).
- **Type fidelity.** CBOR distinguishes `int64` / `uint64` / `float16/32/64` / byte-string / UTF-8 string / timestamp at the wire; JSON collapses numeric types to a single `Number` token and has no byte-string.
- **Semantic tags.** CBOR tags attached at the wire level let a producer declare "this string is a UUID / URL / hash" without schema negotiation. `TagSet` was the in-memory registry of recognised tags.

The pipeline was shaped around these properties: JSON → canonical CBOR (via `fxamacker/cbor`) → token stream → PSM → columnar indexing into Leeway / ClickHouse.

### What has changed

Two independent developments invalidate the premise:

1. **`encoding/json/v2` + `jsontext` (Go experimental, enabled in this repo via `goexperiment.jsonv2`).** The new `jsontext.Decoder` exposes a streaming pull API with direct equivalents for every alpha/cbor abstraction: `ReadToken`, `ReadValue` / `SkipValue`, `StackPointer`, `StackDepth`, `StackIndex`, `InputOffset`. The PSM capability is no longer unique to alpha/cbor.

2. **Measured Leeway vs. ClickHouse-JSON benchmark, run on the `jsonv2` + Leeway ingest path.** Leeway wins the benchmark *without* routing through alpha/cbor. The CBOR intermediate is not load-bearing for the performance win, and the vertical-integration story (security, process mining, semantic data model, controlled vocabulary/ontology, data profiling) lives entirely above the format layer.

Combined: the API-design value alpha/cbor once provided is now free from the standard library, and the wire-format value it *could* provide (O(1) skip, type fidelity, tags) is not what the production ingest path relies on to win. The package has become a maintenance cost without a differentiating benefit.

### Package-local health

An audit during ADR drafting surfaced concrete debt:

- `TokenE.IsPositiveInt` / `IsNegativeInt` silently drop the 8-bit variant (`tokenizer.go:96,100`). This leaks into `public/semistructured/cbor/indexedcborrepr.go:82`, causing 1-byte integers (values 24–255) to be misclassified.
- `BenchmarkCborConsumer` loop condition is `err != nil` (`pullparser_test.go:113`); the benchmark has never measured anything.
- Indefinite-length string chunks in the PSM are double-counted (scalar at the header and again at each chunk); masked because `fxamacker`'s canonical encoding never emits indef strings, so the test suite does not exercise the path.
- Dead branch at `pathstatemachine.go:676`.
- `reader/*` at 0% coverage; `tagset.go` at 0% coverage; ~50% of `CheckingEncoder` methods at 0% coverage.
- `// FIXME support non-scalar map keys` in `pullparser.go:258` — RFC 8949 permits compound keys; the parser does not.

## Design space (QOC)

**Question.** What should happen to `alpha/cbor` now that `jsontext` covers the streaming-pull API and the Leeway-vs-ClickHouse-JSON benchmark shows the CBOR intermediate is not load-bearing?

**Options.**

- **O1** — Maintain `alpha/cbor` indefinitely. Keep the package, fix bugs as they surface.
- **O2** — Promote to production grade in place. Fix the defects, add fuzzing, dedupe readers, decide the indef-string and non-scalar-key semantics, ship.
- **O3** — Rebuild as a smaller format-agnostic event-stream library. Extract the PSM, wrap both `jsontext` and a new thin CBOR frontend, migrate callers to the new API.
- **O4** — Attic the package; route consumers through `jsonv2` + Leeway-native paths *(chosen)*.

**Criteria.**

- **C1 — Ongoing maintenance cost.** Engineer-hours per quarter to keep the code correct as Go evolves and call sites grow.
- **C2 — Performance on the flagship workload.** Does the option preserve the measured Leeway-vs-ClickHouse-JSON win?
- **C3 — Conceptual surface of the ingestion stack.** How many parser abstractions must a new contributor learn?
- **C4 — Migration cost.** One-time engineering cost to reach the option's end state.
- **C5 — Preserves optionality for future CBOR-native ingestion.** Can CBOR input be re-introduced later if a customer segment demands it?

**Assessment.** `++` strong positive, `+` positive, `−` negative, `−−` strong negative.

|    | O1 | O2 | O3 | O4 |
|----|----|----|----|----|
| C1 | −− | −  | −  | ++ |
| C2 | +  | +  | +  | +  |
| C3 | −  | −  | +  | ++ |
| C4 | ++ | −− | −  | +  |
| C5 | +  | ++ | ++ | −  |

O4 dominates on C1 and C3, is neutral on C2 (all options leave the flagship path untouched), is `+` on C4 (one-time migration of 20+ importers, but no new code to write beyond adapters already planned), and is the only option that is `−` on C5. The tradeoff is explicit: we give up preserving a capability we are not using, and buy a smaller stack with lower maintenance cost in return. If CBOR ingestion becomes necessary later, `jsontext` is a working template for the rebuild.

## Decision

We move `public/alpha/cbor` to `public/attic/alpha/cbor` and migrate its 20+ consumers to the `jsonv2` + Leeway ingestion path over a bounded deprecation window. The package becomes read-only for historical reference; no further bug fixes, tests, or feature work land on the attic copy.

The CBOR-as-IR concept and the CBOR → columnar ingestion capability are *not* promised to be restored. If a future customer segment (IoT / edge / COSE-adjacent) requires native CBOR ingestion, it is rebuilt as a thin adapter against the Leeway event API, modelled on `jsontext`, not on `alpha/cbor`.

### Subsidiary design decisions

- **SD1 — Per-caller migration, no compatibility shim.** Each of the 20+ importers migrates to its replacement entrypoint in a dedicated commit. A shim that re-exports alpha/cbor symbols from the attic is explicitly rejected: the whole point is to stop paying for the API, and a shim perpetuates the import graph.
- **SD2 — Attic location follows existing convention.** The repository already uses `public/attic/` for retired code (e.g. `public/attic/hmi/semistructured/jsonindexeddtovis.go`). The move keeps git history intact and signals "retired, do not extend." A `doc.go` in the attic'd package cross-links to this ADR.
- **SD3 — `boxer/public/semistructured/cbor` is untouched.** Only a downstream consumer's `alpha/cbor` and the derived `public/semistructured/cbor` indexing layer retire; the upstream boxer CBOR encoder remains a supported dependency for low-level CBOR encoding needs unrelated to semi-structured ingestion.
- **SD4 — No further bug fixes on the attic copy.** The `IsPositiveInt` / `IsNegativeInt` 8-bit gap and related defects are recorded in this ADR's *Package-local health* section for the record; they are not fixed. Downstream code that depended on the buggy behavior is rewritten during migration, not bug-compatible-ported.
- **SD5 — CLI tool triage is part of the migration.** `app/commands/cbor/{convert,parse,tokenize,index,experiments,integrate}` each receive an independent keep-or-retire decision during migration. Expectation: roughly half retire as collateral, since their reason to exist was exercising the alpha/cbor API.
- **SD6 — ADR is the load-bearing narrative.** Any design rationale, oral history, or PSM semantics understanding captured in `// design:` comments or contributor memory is condensed into this document's *Why the package was built* and *Architectural benefits being retired* sections. The attic'd source is the remaining artifact; this ADR is the remaining narrative.

## Alternatives

Rejection rationale for the top-level options is in the QOC matrix; the notes below capture detail not visible in the ratings.

- **O1 — Maintain indefinitely.** Pays maintenance tax for a capability not on the critical path. The package was labelled `alpha/` for good reason and has never earned promotion; leaving it in place is choosing the status quo by default.
- **O2 — Promote to production grade in place.** The earlier audit estimated ~90% of the gap was tractable in a bounded engineering session; the remaining 10% requires semantic judgment calls (indef-string PSM semantics, non-scalar map keys) and fuzz campaigns long enough to surface edge cases. All of that work is done to preserve a capability the measured benchmark shows is unused. Net: spend to maintain, not to build.
- **O3 — Rebuild smaller, format-agnostic.** Tempting, and is effectively what would happen if CBOR ingestion returns. Doing it *now*, without a concrete customer for the CBOR frontend, builds optionality we have not been asked to pay for. Defer until demand is concrete.

## Consequences

### Architectural benefits being retired

This section records what the retired design *did* offer, so a future contributor considering revival has a faithful starting point rather than re-deriving the design from the attic'd source.

- **Pull-based streaming parser with a live path and byte offset.** The consumer receives a token stream; at any point it can ask "where am I in the document?" and "how many bytes have I consumed?". `jsontext.Decoder` reproduces this for JSON.
- **O(1) scalar skip.** `SkipScalarIfNecessary()` advanced the reader by an amount known from the CBOR header, without touching the bytes. This is a *wire-format* property; no JSON library can match it because JSON encodes neither string nor number length.
- **Wire-level type fidelity.** Distinct tokens for `int64`, `uint64`, `float16/32/64`, byte-string, UTF-8 string, timestamp, null, undefined, and bool. Downstream consumers did not have to re-derive types from content heuristics. `jsontext` collapses numeric types to a single `Number`; Leeway's type-inference layer takes over the re-derivation role.
- **Semantic tags as schema-less annotation.** A CBOR tag attached to a value let a producer declare "this string is a UUID / URL / hash" at the wire level; the consumer could honor or skip the tag without schema negotiation. `TagSet` was the in-memory registry. JSON has no equivalent; Leeway's controlled-vocabulary / ontology layer subsumes the role at a higher level.
- **Indefinite-length containers for unknown-size streams.** `TokenIndefLengthArray` / `TokenIndefLengthMap` + `TokenBreak` let producers emit containers without knowing their size in advance. JSON has the same capability syntactically; the distinction is irrelevant when the consumer streams.
- **Symmetric well-formedness checking via PSM.** `CheckingEncoder` used the same PSM to validate *emission*, so encoder and decoder shared one invariant set. This is an unusual and elegant property; if a CBOR frontend is rebuilt later, preserving the symmetry is recommended.
- **`ParseReaderI` / `DiscarderI` / retained-buffer readers.** Three reader implementations (`inmem`, `buffered`, `windowed`) with a "temporary data by byte range" accessor for zero-copy path-qualified slice retention. This layer is the single most reusable part of the package if a rebuild happens; the pattern is not specific to CBOR.

### Positive

- **Smaller ingestion surface.** One parser abstraction (`jsontext.Decoder`) plus Leeway-native event emitters replaces two (alpha/cbor + jsontext), simplifying the stack a new contributor must learn.
- **No more `alpha/` tax.** The 51%-covered, partially-buggy, under-specified package stops being a maintenance item.
- **Measured performance preserved.** The Leeway-vs-ClickHouse-JSON benchmark already validates the non-CBOR path; no regression risk at the benchmark level.
- **Go stdlib carries the parser.** `jsontext` maintenance, fuzzing, and evolution is someone else's job; we inherit the work for free as the experimental package moves to stable.
- **Migration surfaces dead code.** Several of the 20+ importers are themselves in `app/commands/cbor/*` (CLI tooling for CBOR-specific tasks). Per SD5, each forces an independent keep-or-retire decision.

### Negative

- **Capability loss: wire-level type fidelity at ingest.** Leeway's type inference now bears the full load for heterogeneous JSON input. If a workload appears that stresses this (high-cardinality mixed int/float/string columns where wire types would disambiguate cleanly), a problem alpha/cbor solved by construction reappears.
- **Capability loss: O(1) skip.** If Leeway grows a "index only these N paths out of M≫N in the document" fast path, JSON ingestion cost becomes O(payload) per document instead of O(indexed bytes). No current workload demands this; future demand would require a CBOR or columnar-at-source frontend.
- **Capability loss: wire-level semantic tags.** Customers that *want* to pass semantic annotation at wire level (a real scenario in IoT/edge/COSE contexts) lose that door. Leeway's ontology / controlled-vocabulary layer provides a functional replacement at a different layer; the migration story for such a customer is "express annotations through Leeway's schema, not the wire format."
- **Migration cost.** 20+ consumer files plus the derived indexing subsystem under `public/semistructured/cbor/*`. Bounded, but not trivial; scoping happens during migration rather than here.
- **One-way in practice.** Re-introducing CBOR ingestion later, even as a thin jsontext-shaped adapter, requires re-earning institutional familiarity with CBOR wire format, RFC 8949 edge cases, tag registries, and the PSM semantics this ADR is retiring. The attic'd source and this ADR mitigate but do not eliminate the re-familiarisation cost.

### Neutral

- **`boxer/public/semistructured/cbor` continues as an upstream dependency for low-level CBOR encoding.** Code that needs to emit CBOR bytes (e.g., content-addressable hashing, wire-level test fixtures) keeps the boxer API; only the consumer-local parser stack retires.
- **Historical audit findings are captured here, not on a branch.** Anyone reading the attic'd source can recover the known-buggy list from this ADR's *Package-local health* section without re-running the audit.

### Derived practices

- **Do not extend the attic.** `public/attic/alpha/cbor` is read-only. A bug fix or feature landing there is a signal that migration is incomplete; migrate the caller instead.
- **Future CBOR ingestion is a Leeway event-API frontend, not a port.** If CBOR returns, it is a `jsontext.Decoder`-shaped thin adapter against the current Leeway event model, not a restoration of the retired PSM. The Leeway event model is the stable contract; the format frontend is replaceable.
- **Wire-level semantic annotation is out; Leeway schema is in.** Customers reaching for "typed / tagged wire hints" are redirected to Leeway's controlled vocabulary and ontology features. The decision not to carry semantics at the wire level is now codified.

## Status

Accepted — 2026-04-22. Migration to the attic begins; see the accompanying plan for caller-by-caller ordering and rollout criteria.

Status lifecycle: `Proposed → Accepted → (Deprecated | Superseded by ADR-XXXX)`. ADRs are append-only; supersession is recorded, not deleted.

## References

- [`doc/adr/0001-clickhouse-observability-pipeline.md`](0050-clickhouse-observability-pipeline.md), [`0002-query-categorization-provenance.md`](0051-query-categorization-provenance.md), [`0003-imzero2-unified-color-type.md`](0052-imzero2-unified-color-type.md) — prior ADRs; template shape followed here.
- [`DOCUMENTATION_STANDARD.md`](../DOCUMENTATION_STANDARD.md) — Diátaxis + ADR conventions this document follows.
- [`0002-nanopass-discipline.md`](0002-nanopass-discipline.md) — retrospective-ADR style reference used here.
- `public/alpha/cbor/` — package being retired, relocating to `public/attic/alpha/cbor/`.
- [`public/semistructured/cbor/`](../../public/semistructured/cbor) — downstream indexing subsystem; migrated off alpha/cbor during retirement.
- `encoding/json/v2` / `encoding/json/jsontext` — Go experimental packages (enabled in this repo via `goexperiment.jsonv2`) providing the streaming-pull API that replaces alpha/cbor's role.
- RFC 8949 — CBOR specification; cited for the wire-format properties enumerated in *Architectural benefits being retired*.
