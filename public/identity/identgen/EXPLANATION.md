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
  - `mem` — in-memory (a Go map): no external dependency, not safe for concurrent
    use. Only `internalized` has an in-memory backend; the in-memory sequential
    case is a trivial atomic counter and is not provided here.
  - `badger` — an embedded Badger key-value store: durable, and safe for
    concurrent use within one process.

The Redis (distributed) backend for both flavours lives out-of-tree in
`pebble2impl`, to keep a `go-redis` dependency out of boxer.

## Invariants shared across backends

- **Ids are tagged.** Every id is an `identifier.TaggedId`: the configured
  `TagValue` occupies the high bits, a dense body the low bits. One store
  (Badger) or process may host many tags; their id streams never collide because
  the tag partitions the word.
- **Body 0 is reserved.** Bodies are minted from 1 so the zero id stays available
  as invalid/NULL, matching `identifier.UntaggedId.IsValid`. The Badger backends
  achieve this by emitting `raw+1` over a zero-based `badger.Sequence`.
- **Exhaustion is matchable.** When a tag's body range is spent, every backend
  returns an error wrapping `identgen.ErrIdSpaceExhausted`, so callers can
  `errors.Is` it regardless of implementation.
- **Interners reject empty keys.** `internalized` backends reject a nil or empty
  natural key with `ErrEmptyNaturalKey` (an interner must dedupe by key); `seq`
  ignores the key entirely.
- **Tag values are validated at `Create`.** An out-of-range `TagValue` is
  rejected up front rather than silently producing a malformed id.

## Trade-offs and rough edges

- **`mem` is co-located with `badger` in `internalized`.** Importing the package
  for the in-memory generator therefore links Badger and marks the package
  WASM-blocked. Badger is already a boxer dependency so the cost is small, but a
  split into a dependency-free package would restore WASM-compilability.
- **Reverse resolution is `mem`-only.** `MemIdInternalizer` additionally offers
  `Resolve` / `ResolveUntagged` / `All` (a full dictionary); the Badger backends
  do not, because a reverse index would roughly double their on-disk footprint.
  Code that needs decoding should type-assert to the concrete in-memory type.
- **Compaction is caller-driven.** The Badger factories expose `Compact()` (one
  value-log GC pass); generators no longer compact implicitly, so long-lived
  stores should schedule it.

## Provenance

These implementations were donated from `pebble2impl`'s `identity` tree — see its
`EXPLANATION.md` for the variable-width (Fibonacci-coded) sibling of boxer's
fixed-width tag scheme.

## Further reading

- The seam and tag arithmetic:
  <https://pkg.go.dev/github.com/stergiotis/boxer/public/identity/identifier>
