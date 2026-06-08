---
type: adr
status: accepted
date: 2026-05-15
reviewed-by: "p@stergiotis"
reviewed-date: 2026-06-08
---

# ADR-0036: Canonical bus codec via runtime/buscodec

## Context

[ADR-0026](./0026-app-runtime-and-capability-subjects.md) introduces the app runtime, the in-proc bus, and the cap-as-subject taxonomy. Every broker (`fsbroker`, `capbroker`, `persist`, `chlocalbroker`, `windowhost`) ferries typed request/reply payloads on `inprocbus.Client.Publish(subject, []byte)`. Until this ADR, each broker shaped its bytes by importing `encoding/json` directly:

- `fsbroker/service.go` carried a private `replyJSON` helper alongside inline `json.Marshal`/`json.Unmarshal` in the watch pump.
- `capbroker/payload.go`, `persist/payload.go`, `chlocalbroker/payload.go` each exposed near-identical `Marshal*`/`Unmarshal*` wrappers around `json.Marshal`/`json.Unmarshal`.
- `windowhost_test.go`, `capdemo/capdemo.go`, `boxerstaging/spinnaker/hmi/play/play_renderer.go` consumed those replies with hand-rolled `json.Unmarshal` calls — 27 call sites across 9 packages by the time this ADR was written.

Three forces made this untenable:

- **No central seam.** Switching the wire format meant rewriting 27 call sites in lockstep with the brokers. Tooling that needs canonical byte order — content-addressed replay, log-stream diffing, the future fact-store ingestor — had nowhere to enforce determinism even if every broker individually agreed to it.
- **JSON is unfit for the bus.** Float roundtripping is lossy on edges, `[]byte` requires base64 (paid even on the local-proc fast path), the spec gives several legal encodings of the same value (canonical-JSON exists but the stdlib does not emit it), and `interface{}` decoding allocates per-field. For payloads dominated by binary blobs (chlocalbroker `Body`, fsbroker watch events with cookies) the base64 tax is multiplicative.
- **A second codec is coming.** A parallel workstream is building a struct-field-based marshaller for *leeway fact rows* (see [[reference-leeway-scope]]) so that bus payloads can land directly into the columnar facts store without a re-encoding step. The seam shape must accommodate that codec as a drop-in replacement, not as a special case bolted onto the bus.

Constraints inherited from the rest of the stack:

- **CGO-free build** (ADR-0026 invariant). Rules out any codec that relies on a non-Go runtime.
- **No new mandatory IDL step.** The repo already runs `./generate.sh` for the FFFI2/egui2 IDL; adding a second mandatory codegen pass for every payload struct would be a tax we do not want to pay.
- **`json:` struct tags pervasive.** Every payload type in scope already carries `json:` tags. A format that cannot honour them forces a fleet retag — friction without payoff.
- **Replay-diffable bytes.** Several downstream consumers (the planned log-stream replay, the cache layer behind ADR-0028's chlocal stream cache) need same-input → same-bytes guarantees. JSON does not give this without a canonicaliser; the stdlib has none.

The memory note `reference_clickhouse_local` records that CBOR is already the codebase's go-to binary self-describing format (zerolog's CBOR writer, the boxer error-chain encoder, ClickHouse DSL parameter metadata). Adopting CBOR here aligns the bus with the prevailing encoding choice elsewhere in the stack.

## Design space (QOC)

**Question.** How should thestack expose bus-payload serialisation so that (a) one swap changes every broker's wire format, (b) existing `json:`-tagged structs need no retagging, (c) downstream tooling has canonical/deterministic bytes, and (d) the future leeway-fact-row codec can replace the default without touching brokers?

**Options.**

- **O1 — Status quo: `encoding/json` per broker.** Each broker keeps its private Marshal/Unmarshal wrapper over the stdlib. No central package.
- **O2 — `runtime/buscodec` with CBOR canonical default + `CodecI` seam (chosen).** New package owns serialisation; brokers route every payload through generic `Encode[T]/Decode[T]`. `cbor.CanonicalEncOptions` for deterministic bytes; fxamacker/cbor reads `cbor:` then `json:` tag fallback so no retag.
- **O3 — Leeway fact rows on the wire today.** Every payload is encoded as a one-row leeway table (memberships, cardinality columns, value aspects). Unifies wire and storage formats; no bridging cost when bus traffic lands in the facts store.
- **O4 — Protobuf with mandatory `.proto` per payload.** IDL-driven schema; codegen produces Marshal/Unmarshal alongside generated Go types.
- **O5 — MessagePack with stdlib-like API.** JSON-shaped binary format; smaller than JSON, lossless floats, native `[]byte`.

**Criteria.**

- **C1 — Migration cost from today's JSON.** How many lines change per call site; whether existing `json:` tags survive.
- **C2 — Determinism / replay-diff fitness.** Same input → same bytes, no key reordering, no float-representation drift.
- **C3 — Per-call CPU + allocations at the bus hot path.** Cost of encoding a 5-field reply (`DialogReply`, `GrantReply`).
- **C4 — Codec-swap ergonomics for the upcoming leeway-row codec.** How much work to make leeway-row the new default once it exists.
- **C5 — `[]byte` and binary-blob carriage.** Whether non-text payloads (chlocalbroker `Body`, watch-event cookies) need an encoding round-trip.

**Assessment.** `++` strong positive, `+` positive, `−` negative, `−−` strong negative.

|    | O1 status quo | O2 buscodec+CBOR | O3 leeway rows | O4 protobuf | O5 msgpack |
|----|:--:|:--:|:--:|:--:|:--:|
| C1 | ++ | +  | −− | −− | +  |
| C2 | −  | ++ | ++ | ++ | +  |
| C3 | +  | ++ | −− | ++ | ++ |
| C4 | −− | ++ | ++ | −  | −  |
| C5 | −− | ++ | +  | ++ | ++ |

## Decision

We introduce `public/keelson/runtime/buscodec/` as the single seam for serialising every bus payload. The package exports:

- A `CodecI` interface with `Encode(any) ([]byte, error)`, `Decode([]byte, any) error`, plus `Name()` and `ContentType()` for diagnostics.
- A process-wide default codec accessed through `Default()` and replaced through `SetDefault(c CodecI)` — a single `atomic.Pointer` swap, safe for init-time use by tests and replay tools.
- Generic call-site helpers `Encode[T](v T)` and `Decode[T](b []byte)` that route through `Default()` and wrap errors with `eh.Errorf` naming the offending type, plus `Reply[T](pub PublishFunc, subject string, v T)` that folds the broker-side marshal-then-publish pattern that used to live as private `replyJSON` helpers.
- Two concrete codecs: `NewCBOR()` — fxamacker/cbor with `CanonicalEncOptions` (the default, registered in `init()`) — and `NewJSON()` — `encoding/json` for capture-replay debugging via `SetDefault(NewJSON())`.

Every broker exposes typed `Marshal{Name}`/`Unmarshal{Name}` one-liners in its own `payload.go` that delegate to `buscodec.Encode`/`Decode`. Brokers MUST NOT import `encoding/json` or call `cbor.Marshal`/`cbor.Unmarshal` directly when shaping bus traffic. The codec is wire-incompatible with the previous JSON format; the migration ships as a single commit (`03c2f95b`, 2026-05-15) covering 27 call sites across `fsbroker`, `capbroker`, `persist`, `chlocalbroker`, `windowhost`, `capdemo`, and `boxerstaging/spinnaker/hmi/play`.

## Alternatives

- **O1 status quo.** Rejected — no place to enforce determinism, no swap path for the leeway-row codec, every wire-format question becomes a 27-site rewrite.
- **O3 leeway fact rows on the wire today.** Rejected for the *bus* (not for the *facts store*). Leeway is bulk-columnar; encoding a 5-field `DialogReply` as a one-row table pays membership/cardinality column overhead with zero compression payoff and a row-grain codec API that does not exist yet. CBOR-encoded payloads still land cleanly inside the leeway columnar world via the existing `AspectCbor*` value aspects — bridging cost is "labelled bytes," not re-encoding. The leeway-row codec, when it exists, swaps in as `SetDefault(NewLeewayFactRow())` and changes nothing else.
- **O4 protobuf.** Rejected — mandates an IDL pass per payload type and forces a fleet retag (no `json:` honoured). The schema-evolution guarantees protobuf buys are not worth that friction for a pre-stable in-proc runtime where every consumer rebuilds anyway.
- **O5 MessagePack.** Rejected — comparable wire size and CPU profile to CBOR but with no canonical-encoding mode, weaker tooling in the broader Go ecosystem, and no presence elsewhere in the stack (boxer leans on CBOR already).

## Consequences

### Positive

- One swap (`SetDefault(c)`) changes the wire format for every broker. The leeway-row codec workstream has a stable target — implement `CodecI`, register at init, every broker picks it up.
- Existing `json:` tags honoured; no fleet retag for the migration.
- Canonical CBOR bytes are deterministic — same input → same wire bytes — making replay diffing, content-addressed caching, and stream-of-payloads dedup tractable downstream.
- Brokers shrink: per-broker `replyJSON` helpers fold into one `buscodec.Reply`; per-broker error-wrap boilerplate folds into the generic `Encode/Decode` helpers.
- `[]byte` payloads (chlocalbroker `Body`, watch-event cookies) ride as CBOR major-type-2 byte strings — no base64 round-trip.
- Generic `Encode[T]/Decode[T]` keep type-safety at the call site even though `CodecI` is non-generic.

### Negative

- Wire-incompatible with the previous JSON format. Any peer (a hypothetical external NATS consumer not in this tree, captured-payload fixtures from before this commit) must rebuild or be re-encoded.
- CBOR is not human-readable in raw form. Debug introspection requires either `SetDefault(NewJSON())` at startup or a `cbor`-aware viewer (`fxamacker/cbor`'s diagnostic notation; `cbor2diag`).
- `SetDefault` is intentionally process-wide. Per-subject codec routing (if ever needed) is not in scope; it would require an `inprocbus.Msg` codec header field, which this ADR explicitly defers until a concrete use case shows up.

### Neutral

- Per-message envelope versioning (`chlocalbroker`'s `V uint8`) stays a payload concern, not a transport concern. The codec does not impose a version tag — payload types that need it keep carrying it on the struct.
- The JSON fallback codec remains shipped for capture-replay debugging but is not the default. Removing it later would be a one-line decision if it ever becomes a maintenance burden.

## Status

Accepted — 2026-06-08 (reviewed by p@stergiotis). Filed retrospectively: the codec landed in [`runtime/buscodec`](../../public/keelson/runtime/buscodec/) ahead of review, so the *decision* is recorded here independently of the diff.

Status lifecycle: `Proposed → Accepted → (Deprecated | Superseded by ADR-XXXX)`.
ADRs are append-only; supersession is recorded, not deleted.

## Updates

### 2026-05-20 — Per-type codec registry (ADR-0042 M12)

buscodec gains a per-type registry so leeway fact-row payloads can
opt into the sparse-RB / sparse-CBOR codecs landing alongside ADR-0042
M10's driver path. The original `Default()` seam stays — it's still the
single swap point for capture-replay / debug builds — but
`Encode[T] / Decode[T] / Reply[T]` now consult the per-type registry
first.

Surface added:

- `Register[T any](codec CodecI)` — install or overwrite the codec
  routed to T. Passing `nil` unregisters T (the next dispatch falls
  back to `Default()`).
- `Lookup[T any]() CodecI` — returns the codec currently routed to T:
  the registered codec if one exists, else `Default()`. Exposed for
  tests and diagnostics.

Dispatch order for every payload type:

1. Per-type registration (sync.Map keyed by `reflect.TypeFor[T]()`).
2. Process-wide `Default()` (CBOR by default, swappable via
   `SetDefault`).
3. Error (handled inside the registered or default CodecI).

Goroutine-safe: `sync.Map` for the registry; the existing
`atomic.Pointer` for `Default()`. Re-registering overwrites silently —
the common idiom is once-per-type init in a fact-kind's package
`init()`.

#### Why not "default = sparse-RB"?

The M8 / M10 work in ADR-0042 makes sparse-RB the obvious wire format
for runtime.facts row payloads, but sparse-RB only knows how to encode
*registered* fact-kind types (it needs the kind's active-section /
active-field hints to drive `dml_rowbinary.InEntityFacts`). A generic
"any Go struct → sparse-RB" path doesn't exist and isn't planned.

Keeping CBOR as the generic fallback means non-fact-row payloads
(broker request/reply DTOs, capture-replay fixtures) continue to work
unchanged after M12 lands. Fact-row types opt in by calling
`buscodec.Register` with their `CodecI` in their package `init` —
typically inside a one-line hook the per-kind codegen will eventually
emit.

#### Migration story

M12 is the *seam* milestone. Per-kind `CodecI` implementations + the
codegen hook that registers them are mechanical follow-up — the
generated `<Kind>Columns.Marshal` already produces dml_rowbinary
bytes; wrapping it as a buscodec.CodecI is ~20 lines of single-row
adapter (build `<Kind>Columns`, append the row, `Marshal` to a
buffer, return bytes; symmetric on the read side via
`rowbinaryarrow.Convert + Unmarshal`). The ADR-0042 round-trip
plumbing closed in the same session as M12 — see ADR-0042's
"2026-05-20 — Convert section-coverage extension" entry — so the
read path is unblocked.

#### ADR-0042 sequencing

This Updates entry corresponds to ADR-0042 M12 in that ADR's revised
milestone sequence. The two ADRs cooperate: ADR-0036 owns the seam
shape (this dispatch order, `CodecI` contract, error wraps);
ADR-0042 owns the codec implementations that plug into it.

## References

- [ADR-0026](./0026-app-runtime-and-capability-subjects.md) — app runtime, in-proc bus, cap-as-subject taxonomy.
- [ADR-0028](./0028-chlocal-low-latency-sql-cap.md) — chlocalbroker, one of the first consumers; its `V` field versioning illustrates the payload-side versioning pattern the codec deliberately does not subsume.
- `public/keelson/runtime/buscodec/` — package source.
- `fxamacker/cbor/v2` — the CBOR implementation (`CanonicalEncOptions` for deterministic output, `cbor:`-then-`json:` tag fallback).
- Leeway fact-row codec workstream (parallel) — the second `CodecI` implementation this seam exists to accept.
