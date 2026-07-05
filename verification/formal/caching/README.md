---
type: explanation
audience: contributors
status: draft
---

> **Status: draft — pre-human-review.** Formal Quint model of the versioned
> write-through cache design. The specs and their invariants are
> machine-checked (Quint); this surrounding prose has not had human review.

# Formal spec: versioned write-through cache

A [Quint](https://quint-lang.org) model of the **versioned write-through
design** for `public/caching` and its recordstore integration, settled in the
2026-07-05 design dialogue and modelled **before implementation** (the Go
implementation followed the validated spec) — in the
`verification/formal/algebraicarch/pushout` tradition. This tree mirrors the package path of the code it constrains.

> Go file paths in this document are relative to `public/caching/`. The
> implementation landed after the spec (spec-first); the refinement map
> below names the real symbols.

## Why model this now

The 2026-07-04 adversarial review found that this package's historical bug
class is small-scope **state-interaction** defects (every confirmed defect
manifested with ≤ 3 keys and two tiers), and the versioned design adds three
interacting mechanisms at once: a version gate on admission, a sticky
dirty-window pin, and TTL freshness — each carrying state across the L1/L2
tier boundary where the previous defects lived. The two headline rules are
easy to get subtly wrong in exactly the way the counterfactual specs
demonstrate, and one hazard (the dirty-eviction regression) is invisible to
code review because each mechanism looks correct in isolation.

Eviction is modelled **adversarially**: `demote` / `dropL1` / `dropL2` may
strike any unpinned entry at any time. Every invariant therefore holds for
*any* eviction policy — the random policy shipped today and the SIEVE /
S3-FIFO candidates postponed to a later round.

## Files

| File | What |
|------|------|
| `versioned_cache.qnt` | The core machine: write-through Commit/Flush with per-key versions, the version gate on fetch delivery, the dirty pin, two tiers under adversarial eviction. Safety invariants + witness runs. |
| `versioned_cache_unsafe_lww.qnt` | Counterfactual — version gate removed (last-insert-wins admission, today's semantics). The resurrection race: a raced fetch overwrites a fresh Commit. |
| `versioned_cache_unsafe_nopin.qnt` | Counterfactual — dirty pin removed. The regression the gate alone cannot stop: an evicted dirty entry is refetched at the older durable version and admitted into a cold cache. |
| `freshness_ttl.qnt` | The `WithFreshnessTTL` half: age stamps, stale-while-revalidate onset, equal-version revalidation, stamps carried through demotion. |
| `freshness_ttl_unsafe.qnt` | Counterfactual — the stash envelope lacks the age field; a tier round-trip re-stamps the entry and it reads fresh past its true TTL. |

## Refinement map (spec action → Go symbol)

| Spec action | Go symbol | Modelled semantics |
|-------------|-----------|--------------------|
| `write` | recordstore `Commit` → `cache.AddItem` + `cache.Pin` (generated `notifyWrite` hook) | store assigns the next per-key version; write-through admission; sticky pin |
| `flushAll` | recordstore `Flush` → `cache.Unpin` per flushed key (generated `notifyFlush` hook) | publishes every dirty key upstream, releases the pins |
| `readHitL1` / `readHitL2` | `Get` / `GetAcceptStale` (caching_readthrough_stale.go `lookup`) | serve; a stash hit promotes, carrying entry state |
| `readMiss` | `lookup` miss path → `queueForFetch` | suspend/replay protocol unchanged |
| `fetchReturn` | `performFetch` → fetcher `AddItem` → `admit` (the version gate) | delivery of the upstream's durable version **at fetch execution** (the fetcher runs synchronously); admission only if strictly newer than any cached copy |
| `demote` / `dropL1` / `dropL2` | `ensureSpaceByEvictingOne`, spill-drop chains, stash overflow | adversarial stand-ins for any eviction policy; pinned entries immune (explicit `Delete` is out of scope — see below) |
| `admit` / `confirm` (freshness) | `admit`: the `>` and `==` gate outcomes | fresh data stamps now; equal-version revalidation restarts freshness without replacing the value |
| `markStale` | `MarkAsStale` | the external-writer signal |
| `readFresh` | `isStale` under `WithFreshnessTTL` | serve-as-fresh iff unexpired by the carried stamp |

Model abstractions to keep in mind (the Go model-based oracle covers what the
spec elides):

- **Values are their versions.** Value integrity collapses into version
  tracking; the Go oracle keeps the full value dimension.
- **Move semantics between tiers** — the implementation can hold transient
  stash shadows (overwritten before they are servable); the Go oracle covers
  shadow behavior.
- **Delivery at fetch execution.** `performFetch` calls the fetcher
  synchronously against a single upstream, so a fetch returns the durable
  version current at execution. Replica-lag delivery (a read replica serving
  older-than-durable rows) is out of scope; modelling it would need an
  interval-delivery variant and is future work if multi-replica reads arrive.
- **Flush is all-dirty and atomic**, matching recordstore's batched `Flush`.
- The circuit breaker, negative caching, and work-item bookkeeping are out of
  scope here — they are orthogonal to versioning and covered by the package's
  existing regression/model/fuzz suites.
- **Explicit invalidation overrides pins in the implementation.** The spec's
  `dropL1` models *eviction-driven* loss only (pinned entries immune);
  `cache.Delete`/`Clear` remove pinned entries too — a sanctioned monotonicity
  reset on the caller's explicit signal (the write-through discipline forbids
  invalidating keys with unflushed writes; the Go model driver enforces it).

## Safety invariants (must hold)

| Invariant | Meaning |
|-----------|---------|
| `MonotoneServe` | a key's served version never regresses (per-key monotonic reads) |
| `TiersExclusive` | an entry lives in at most one tier |
| `DirtyPinnedResident` | an unflushed version is L1-resident, pinned, and exactly the newest written one — the dirty window is airtight |
| `PinnedOnlyDirty` | pins exist only for the dirty window; `Flush` releases them all |
| `L2NeverPinned` | the stash never holds a pin |
| `CachedWasWritten` / `ServedWasWritten` | no fabricated versions anywhere |
| `AheadImpliesDirty` | the cache runs ahead of the upstream only inside the dirty window |
| `StampFaithful` (freshness) | the carried age stamp equals the key's true last-refresh time |
| `FreshnessHonest` (freshness) | no read is served as fresh past the key's true TTL |

Status: `versioned_cache.qnt` `Safety` and `freshness_ttl.qnt` `Safety` each
survive 20 000 randomized traces (`quint run`, depth 14); all witness runs
pass (`quint test`). Apalache bounded proofs (`npm run verify`, depth 10)
have not been run yet — pending a session with the verification budget.

## The findings: both rules are load-bearing

1. **`versioned_cache_unsafe_lww.qnt` — the gate.** Without the version gate,
   randomized search violates `MonotoneServe` almost immediately: a fetch
   queued before a `Commit` delivers the older durable row and overwrites the
   fresh write (the *resurrection race*). This is the race the current
   recordstore integration works around with its fetcher-side dirty-guard and
   delete-on-write; the gate retires both. `DirtyPinnedResident` breaks on
   the same trace.
2. **`versioned_cache_unsafe_nopin.qnt` — the pin.** With the gate but no
   pin, the violation needs a surgical 8-step interleaving (randomized search
   does not stumble into it; the witness run pins it): the dirty entry is
   evicted, and the refetch delivers the older durable row into a **cold**
   cache — the gate has nothing to compare against. Monotonicity needs
   *gate + pin*; the pin is why `MonotoneServe` becomes inductive (a pinned
   version cannot leave the cache before `Flush` makes it durable, so any
   later delivery is at least as new).
3. **`freshness_ttl_unsafe.qnt` — the stamp.** If the stash envelope lacks
   the age field, a demote/promote round-trip re-stamps the entry and it
   serves as fresh at three times its TTL. The age stamp must travel through
   `StashBackendI` — the same tier-boundary law as the stale flag (2026-07-04
   remediation) and the version (this design).

## Running

```sh
npm install         # pins quint locally
npm run check       # typecheck + witness runs + randomized Safety sweeps
npm run findings    # prints the counterexample traces (lww, nopin, ttl)
npm run verify      # Apalache bounded proofs of Safety (depth 10)
```

Per spec, e.g.:

```sh
npx quint run  versioned_cache.qnt            --invariant=Safety --max-steps=14 --max-samples=20000
npx quint run  versioned_cache_unsafe_lww.qnt --invariant=MonotoneServe   # counterexample
npx quint test versioned_cache_unsafe_nopin.qnt                           # witness regression trace
npx quint run  freshness_ttl_unsafe.qnt       --invariant=FreshnessHonest # counterexample
```

## Not yet modelled (next increments)

- **Liveness** — "every pending work item is eventually replayed under fair
  flushing" and "replay loops over absent keys quiesce" are `◇`/`◇□`
  properties; the pushout precedent checks these with a TLC-native module.
  Worth adding once the implementation lands.
- **The thrash boundary** — characterize for which working-set/capacity
  ratios the suspend/replay protocol completes (the livelock test in the Go
  suite witnesses one side).
- **Replica-lag delivery** (see abstractions above).
- **ITF trace bridge** — export witness/counterexample traces
  (`--out-itf`) and replay them through the Go model driver's `byteOpSource`
  as a committed corpus, binding this spec to the implementation once it
  exists.
