---
type: adr
status: proposed
date: 2026-07-10
# reviewed-by: "@<handle>"     # fill in and uncomment when flipping to accepted
# reviewed-date: YYYY-MM-DD    # fill in and uncomment when flipping to accepted
---

> **Status: proposed — pre-human-review.** Decision under consideration; do not implement as if accepted.

# ADR-0111: identity — technology-neutral leased id generation via an allocator seam

## Context

`public/identity/identifier` (ADR-0106) defines fibonacci-tagged 64-bit ids as
`tag | body`, and `public/identity/identgen` provides the generators behind
`identifier.IdGeneratorI`: an in-memory interner (`mem`), a persistent monotonic
sequence (`seq`), and a persistent interner (`internalized`). The two persistent
backends bind the **id source** to one embedded key-value store — each leases its
body values from that store's own sequence primitive.

Two needs fall outside that shape:

1. **Technology neutrality.** The block-leasing algorithm — reserve a range of
   body values durably, then hand them out from memory — is independent of
   *where* the range is reserved. Today it is entangled with one storage engine,
   so a different durable medium means a new generator, not a new backend.
2. **A networked id authority.** Independent processes that share no local store
   still need to draw from one id space. The natural mechanism is to lease
   body-value blocks from a network authority: coordinate once per block, then
   mint locally. This is the classic central-sequencer / range-lease design,
   applied to body values under a tag.

We also want get-or-assign **interning** to work against such a global source, as
far as it can without coordination.

## Decision

### SD1 — an `AllocatorI` block-reservation seam (`identgen`)

Introduce `identgen.AllocatorI`:
`AllocateBlock(ctx, tagValue, minSize, maxSize) (lo, hi, err)` reserves a
half-open range `[lo, hi)` of fresh body values for a tag, plus `Close`. The
contract is durability + monotonicity + exclusivity: across successful calls for
a tag, `lo` strictly increases and ranges never overlap, even across a crash or a
lost reply. A reserved block is spent whether or not the caller uses it — an
implementation in doubt **over-reserves (burns a block), never re-hands one**.
This is the single technology seam; a local counter and a remote authority
implement the same two methods.

### SD2 — neutral leased generators (`identgen/leased`)

`leased.Sequence` implements `identifier.IdGeneratorI` over an `AllocatorI`: a
shared in-memory cursor hands out body values from the current block and calls
`AllocateBlock` only when it is spent, clamping to the tag's body ceiling
(`identifier.IdTag.GetMaxPossibleIdIncl`) and returning `ErrIdSpaceExhausted` at
the end. The backend is touched once per block, not once per id. Because the
allocator serialises reservations, the leased factory imposes **no
one-generator-per-tag restriction** (contrast the store-native factories'
`ErrTagInUse`): any number of generators may share a tag and draw disjoint
blocks — exactly what independent processes behind one authority do.

### SD3 — internalizer: global sequence, local dedup (`identgen/leased`)

`leased.Internalizer` implements get-or-assign with a **local** key→id map whose
fresh ids come from the (possibly global) `AllocatorI`. Its dedup scope is one
instance: a key seen twice on the same instance resolves to the same id; two
instances over the same allocator resolve the same key to two **different but
globally unique** ids.

This is the deliberate coordination-free trade-off. Global uniqueness is free
(the allocator serialises the id source); global *dedup* is not — it would
require the authority to own the key→id mapping, i.e. every mint round-trips and
the authority stores every natural key. We keep the authority a dumb,
high-throughput range server and accept per-instance dedup. Callers wanting
process-wide dedup share one instance per tag; callers needing fleet-wide dedup
need the heavier interning-authority variant (out of scope — see Alternatives).

### SD4 — backends

`identgen/leased/memalloc` is the dependency-free reference `AllocatorI` (per-tag
monotonic counter; bodies from 1 so the zero id stays reserved). It is the test
backend and the model of an authority's per-tag server-side state. The intended
production backends behind the same seam are a store-backed allocator (durable
local counter) and a network-authority allocator — a client whose
`AllocateBlock` is an RPC, and a server that exposes any local `AllocatorI` over
a transport. The authority is then the root of durability: a store that loses an
acknowledged reservation re-hands a block and breaks uniqueness, so "reserve,
confirm, and on doubt advance" is a checked precondition, not an assumption.

### SD5 — per-call context on the generation seam

`identifier.IdGeneratorI.GetId` / `GetUntaggedId` and
`identgen.BatchInternalizerI.AppendIds` take a leading `context.Context`. It
bounds a store- or network-backed generator's work — a block lease or a
transaction — and can cancel it: the leased generators forward it straight to
`AllocatorI.AllocateBlock` (the actual network boundary), the store-backed
generators check it at entry, and the purely in-memory generator ignores it
(nothing to cancel, and its zero-alloc hot path stays intact). A leased
generator therefore holds no context of its own — the caller supplies one per
call. This is a breaking change to the seam and to its two in-tree consumers —
the natural-key encoder's `EndAndGenerate` / `EndAndGenerate2` and the blob
chunker's `Prepare`, which now take and forward a `ctx`; downstream modules adopt
it when they bump their pin.

## Consequences

- **Positive.** The generation algorithm is decoupled from storage; a new medium
  (or a network authority) is a new `AllocatorI`, not a new generator.
  Block-leasing amortises the (possibly remote) source to once per block.
  Multiple generators/processes share one id space with no shared local store.
  The seam is model-checkable: an adversarial monotonic allocator plus the cursor
  proves no-overlap without a live backend.
- **Availability is coupled at block boundaries.** If the authority is
  unreachable *and* the current block is spent, minting blocks or errors. Larger
  blocks buy runway; background pre-fetch (lease the next block before the current
  is spent) hides latency and is a follow-on — the cursor refills synchronously
  under its lock today.
- **Crash burns a block, never repeats.** A process that dies with a partly-used
  block loses the remainder (a gap), consistent with any block-leasing scheme.
- **Local-only dedup** (SD3) is a real limitation, not a bug: the same natural
  key can carry different ids across instances.
- **Flat body space.** Fibonacci tags carry no writer sub-field, so *offline*
  multi-writer under one tag is not expressible; with an authority it is
  unnecessary (the authority serialises). Only a fully-disconnected multi-writer
  deployment would need to partition the body space.
- **Batch and tail-reclaim are follow-ons.** `leased` does not yet implement
  `BatchInternalizerI`, and `Release` drops the unused tail rather than returning
  it (an allocator with a return method could reclaim it).

## Alternatives considered

- **Per-id RPC to a sequencer.** Correct but chatty; block-leasing amortises the
  round-trip. Kept the leasing idea, dropped the per-id call.
- **Global interning authority (true fleet-wide dedup).** The authority owns the
  key→id map; every mint round-trips and it stores every key. Real dedup, but
  full coordination — the opposite of the coordination-free goal. A separate seam
  (per-key / batch RPC), not this one.
- **Keep the source embedded per backend (status quo).** Simple, but not neutral
  and cannot reach a network authority.

## References

- ADR-0106 — fibonacci-coded tags (the id format these generators mint into).
- `public/identity/identgen/allocator.go` — the `AllocatorI` seam.
- `public/identity/identgen/leased/` — `Sequence`, `Internalizer`, and the
  `memalloc` reference backend.
