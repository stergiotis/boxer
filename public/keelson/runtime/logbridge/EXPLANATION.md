---
type: explanation
audience: package maintainer
status: draft
# reviewed-by: "@<handle>"     # fill in and uncomment when flipping to stable
# reviewed-date: YYYY-MM-DD    # fill in and uncomment when flipping to stable
---

> **Status: draft — pre-human-review.** Not verified; do not cite as authoritative.

# logbridge — Explanation

logbridge turns the zerolog data plane into a producer of `runtime.facts`
rows. Apps already log through `zerolog.Logger`; logbridge intercepts the
configured writer, parses each event from its on-the-wire form, and
delivers a `factsstore.LogRow` to the same FactsStoreI that grants, audit
records, and persisted state flow through. The result: a single SQL
surface (`SELECT … FROM runtime.facts WHERE has(symbol.lr, MembKindLog…)`)
covers every operational signal the runtime emits, ADR-0026 §SD6.

## Background

The project builds with the `binary_log` build tag (see `./tags`), which
flips zerolog's encoder from JSON to RFC-7049 CBOR. Each event becomes a
single indefinite-length map (`0xBF … 0xFF`) handed to the configured
`io.Writer` as one `Write([]byte)` call. The CBOR wire format is
self-describing for primitives (ints, floats, bools, byte strings, text
strings) and preserves type fidelity — strings stay strings, integers
keep their signedness, byte slices don't suffer base64 round-trip — which
is why the leeway columnar fan-out is a more useful destination for it
than JSON would have been.

Zerolog exposes a `zerolog.LevelWriter` interface whose `WriteLevel`
delivers the level out-of-band, sparing us a CBOR walk just to discover
the severity. logbridge implements both `io.Writer` (for compatibility)
and `LevelWriter` (preferred via `zerolog.MultiLevelWriter`).

The destination, `factsstore.FactsStoreI.WriteLog`, lands a row carrying
the well-known envelope memberships (`MembKindLog`, `MembRuntimeApp`,
`MembLogLevel`, `MembLogMessage`, `MembLogCaller`, `MembLogError`,
`MembLogStack`, `MembLogService`) on the appropriate typed sections, plus
a `MembLogField` mixed-membership entry per arbitrary user-supplied
context field — value placed in `tv:string` / `tv:i64` / `tv:u64` /
`tv:f64` / `tv:bool` / `tv:blob` / `tv:time` according to the CBOR type
the decoder produced.

## How it works

The Sink is a producer/consumer pair separated by a fixed-size ring:

1. **Decode.** Inside `Write` / `WriteLevel` the CBOR buffer is unmarshalled
   into `map[string]any` via `fxamacker/cbor/v2`. The well-known keys
   (zerolog's `LevelFieldName` / `MessageFieldName` / `TimestampFieldName`
   / `CallerFieldName` / `ErrorFieldName` / `ErrorStackFieldName`, plus
   the project-local `"service"` tag) populate the `LogRow` envelope;
   every other key becomes a typed `LogField` driven by the value's Go
   type.
2. **Enqueue.** The decoded row is placed at the ring's tail under a
   mutex. On overflow (ring at capacity) the head advances — drop-oldest
   — and a counter is incremented; the producer never blocks. When the
   queued count crosses `FlushN`, a non-blocking nudge is sent on
   `wakeFlush`.
3. **Drain.** A single background goroutine selects on `wakeFlush`, a
   `time.Ticker` set to `FlushInterval`, and `stopCh`. On any signal it
   copies up to `FlushN` rows out under the lock, drops the lock, and
   calls `FactsStoreI.WriteLog` once per row. Calling unlocked keeps a
   slow CH round-trip from stalling producers.
4. **Close.** Closing the Sink trips `stopCh`, drains the ring
   synchronously, and waits for the flusher to exit before returning —
   so a process tearing down can rely on all already-accepted events
   reaching the store.

The ring deliberately holds fully-decoded `LogRow`s, not raw CBOR. The
CPU spent on decode is paid on the producer thread, which is the same
thread that already paid the CBOR encode cost — there's no aggregate
overhead shift, but the flusher gets to do its work without re-parsing.

## Invariants

- **Producer non-blocking.** `Write` / `WriteLevel` return `len(p), nil`
  even when the ring is full. Drops are bookkept on `Dropped()`. Errors
  are never returned to zerolog (it would print them to stderr in a tight
  loop, defeating the back-pressure isolation).
- **No data after Close.** Once `Close()` has returned, no further writes
  reach the store and no flusher goroutine is alive. Re-entering `Close`
  is safe and a no-op.
- **Defensive copies at the FactsStoreI boundary.** `InMemoryFactsStore`
  copies `LogField.Bytes` payloads before retention so the producer can
  recycle scratch slices; `chstore`'s Arrow builder copies into its own
  arenas. Callers may reuse buffers freely.
- **Ring ordering.** Within a single drain call, rows are written in
  enqueue order. Across drains, ordering is preserved unless overflow
  drops occurred — those rows are gone and the surviving sequence
  reflects the original temporal order.

## Trade-offs

- **One row per event, not batched at CH level.** The flusher calls
  `WriteLog` once per row; each call ships one Arrow `RecordBatch`. The
  decoupling buys producer non-blocking and orderly shutdown, but does
  not yet amortise the CH-side round-trip. A future `WriteLogBatch` on
  `FactsStoreI` would cut that overhead — left to a later sub-phase
  because the current call-site count is too low to justify the change.
- **Decoded shape over raw CBOR.** Holding `LogRow`s in the ring costs
  more memory per entry than holding raw bytes; the win is that the
  flusher does no per-row parse. For 4 096 rows at ~200 bytes apiece
  that's ≈ 1 MiB — acceptable for runtime telemetry on the same process
  serving CH-backed application data.
- **Drop-oldest, not drop-newest.** When the ring overflows, the oldest
  entries are evicted. Rationale: under burst, the most recent context
  is usually what an operator needs ("what happened just before the
  crash"); historical context is recoverable from any external sink
  attached to the same logger via `MultiLevelWriter`. The decision can
  be revisited per host without rewriting consumers.

## Further reading

- ADR-0026 §SD6 — [`doc/adr/0026-app-runtime-and-capability-subjects.md`](../../../../../../doc/adr/0026-app-runtime-and-capability-subjects.md)
- zerolog CBOR encoder — `internal/cbor/README.md` at
  `github.com/rs/zerolog/internal/cbor`
- fxamacker/cbor decoder — https://github.com/fxamacker/cbor
- Leeway membership vocabulary — [`doc/skills/leeway-advanced/SKILLS.md`](../../../../../../doc/skills/leeway-advanced/SKILLS.md)
