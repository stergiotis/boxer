---
type: explanation
audience: package maintainer
status: draft
# reviewed-by: "@<handle>"     # fill in and uncomment when flipping to stable
# reviewed-date: YYYY-MM-DD    # fill in and uncomment when flipping to stable
---

> **Status: draft — pre-human-review.** Not verified; do not cite as authoritative.

# identgen — concrete identifier.IdGeneratorI backends

`identity/identifier` defines the natural-key → surrogate-id seam
(`IdGeneratorI.GetId(naturalKey []byte) (TaggedId, fresh, err)`) but ships no
implementation. `identgen` supplies them, plus the errors they share. This
document explains the taxonomy and the invariants that hold across backends;
per-symbol behaviour lives in the GoDoc comments.

## Taxonomy: flavour × backend

Two orthogonal choices.

- **Flavour** — what the generator does with the natural key.
  - `seq/` — a per-tag *sequential* counter that **ignores** the natural key and
    hands out a dense, monotonically increasing stream. Use it when you only need
    a fresh id per call (e.g. a row number).
  - `internalized/` — an *internalizing* generator (interner) that maps a natural
    key to a **stable** id, minting a fresh one on first sight and returning the
    same id thereafter. Use it to deduplicate high-entropy keys (UUIDs, hashes)
    to compact surrogates.
- **Backend** — where the state lives.
  - `mem/` — in-memory (a Go map): no external dependency and WASM-compilable,
    not safe for concurrent use. Only the internalizing flavour has an in-memory
    backend; the in-memory sequential case is a trivial atomic counter and is not
    provided here.
  - `badger` — an embedded Badger key-value store: durable, and safe for
    concurrent use within one process. Backs both the `seq/` and `internalized/`
    packages.

The three packages are therefore `seq/` (sequential, Badger), `internalized/`
(internalizing, Badger) and `mem/` (internalizing, in-memory).

## Invariants shared across backends

- **Ids are tagged.** Every id is an `identifier.TaggedId`: the configured
  `TagValue` occupies the high bits as a fibonacci code (ADR-0106), a dense
  body the low bits. One store (Badger) or process may host many tags; their
  id streams never collide because the codes are prefix-free. The body
  capacity is per-tag: wide for short codes, down to 2^17-1 for the widest
  uint32 tag values.
- **Body 0 is reserved.** Bodies are minted from 1 so the zero id stays available
  as invalid/NULL, matching `identifier.UntaggedId.IsValid`. The Badger backends
  achieve this by emitting `raw+1` over a zero-based `badger.Sequence`.
- **Exhaustion is matchable.** When a tag's body range is spent, every backend
  returns an error wrapping `identgen.ErrIdSpaceExhausted`, so callers can
  `errors.Is` it regardless of implementation.
- **Interners reject empty keys.** The `internalized` and `mem` interners reject a
  nil or empty natural key with `identgen.ErrEmptyNaturalKey` (an interner must
  dedupe by key); `seq` ignores the key entirely (silently — the contract lives
  on `identifier.IdGeneratorI`, not in per-call log noise).
- **Tag values are validated at `Create`.** The zero (invalid) `TagValue` is
  rejected up front rather than silently producing a malformed id; every
  non-zero uint32 value is encodable.
- **One generator per tag and store.** The Badger factories refuse a second
  `Create` for a tag that already has a generator
  (`identgen.ErrTagInUse`): the per-generator mutex cannot span two
  instances, so a duplicate interner could mint two different ids for one
  natural key inside the get-or-assign window. The slot is held until the
  factory closes — `Release` keeps its generator usable, so it cannot free
  the slot. `mem` is exempt: each of its generators is its own store.

## Batch resolution

Both interners implement `identgen.BatchInternalizerI.AppendIds`, which resolves a
whole column of natural keys under the tag in one shot and appends the ids to a
caller-provided slice. Keys arrive columnar (`KeysColumn`: concatenated bytes plus
end offsets — the Arrow/Leeway varbinary layout) and the output aligns
positionally, so `ids[i]` is the id of `keys.At(i)`. The Badger backend resolves
the whole column in one read transaction plus as few write transactions as fit
the store's transaction-size limit — usually one; a batch of several hundred
thousand fresh keys rolls over into further commits instead of failing.
Splitting is safe because interning is idempotent get-or-assign: if a later
chunk fails, the keys already committed resolve as existing on retry. Keys that
repeat within one batch resolve to the same id, matching single-key `GetId`.
This is the seam for columnar bulk ingest.

A batch whose distinct fresh keys exceed the tag's remaining id space fails
with `ErrIdSpaceExhausted`. The backends differ in what remains: `mem` counts
the space up front and assigns nothing, the Badger interner persists the
mappings minted before the overrun — consumed sequence values cannot be
returned, so dropping them would burn the space with nothing mapped, whereas
the persisted prefix serves idempotent retries.

## Trade-offs and rough edges

- **Reverse resolution is `mem`-only.** `mem.IdInternalizer` additionally offers
  `Resolve` / `ResolveUntagged` / `All` (a full dictionary); the Badger backends
  do not, because a reverse index would roughly double their on-disk footprint.
  Code that needs decoding should use the concrete `mem.IdInternalizer` type.
- **Compaction is caller-driven.** The Badger factories expose `Compact()` (one
  value-log GC pass); generators no longer compact implicitly, so long-lived
  stores should schedule it.

## Further reading

- The seam and tag arithmetic:
  <https://pkg.go.dev/github.com/stergiotis/boxer/public/identity/identifier>
