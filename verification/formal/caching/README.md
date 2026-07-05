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
*any* eviction policy — SIEVE ships in L1 and the stash backends vary,
all refinements of the adversarial choice.

## Files

| File | What |
|------|------|
| `versioned_cache.qnt` | The core machine: write-through Commit/Flush with per-key versions, the version gate on fetch delivery, the dirty pin, two tiers under adversarial eviction. Safety invariants + witness runs. |
| `versioned_cache_unsafe_lww.qnt` | Counterfactual — version gate removed (last-insert-wins admission, today's semantics). The resurrection race: a raced fetch overwrites a fresh Commit. |
| `versioned_cache_unsafe_nopin.qnt` | Counterfactual — dirty pin removed. The regression the gate alone cannot stop: an evicted dirty entry is refetched at the older durable version and admitted into a cold cache. |
| `freshness_ttl.qnt` | The `WithFreshnessTTL` half: age stamps, stale-while-revalidate onset, equal-version revalidation, stamps carried through demotion. |
| `freshness_ttl_unsafe.qnt` | Counterfactual — the stash envelope lacks the age field; a tier round-trip re-stamps the entry and it reads fresh past its true TTL. |
| `replay_liveness.tla` + `.cfg`/`_nonegcache.cfg` | TLC liveness pair for the suspend/replay loop: `Quiescence` (`<>[]` nothing pending or queued) holds WITH negative caching even for keys the upstream lacks, and is violated WITHOUT it despite weak fairness on every action — the absent-key livelock as a mechanical theorem. `SatisfiableDone`: the livelock never starves satisfiable items. |

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

Status: `versioned_cache.qnt` `Safety` is **proved to depth 6 with
Apalache** (`quint verify`, ~35 s, `NoError`); depth 10 did not finish
inside a 15-minute budget at this model size — push the bound with more
time or Apalache tuning. `freshness_ttl.qnt` `Safety` is **proved to depth
10** (~22 s, `NoError`). Both additionally survive 20 000 randomized traces
each (`quint run`, depth 14) and all witness runs pass (`quint test`). The
liveness pair is checked with TLC (`npm run liveness` /
`liveness:nonegcache`): `Quiescence` and `SatisfiableDone` hold under
negative caching ("No error has been found", 14 distinct states); without
it TLC returns the livelock lasso (`Back to state: Replay`) violating
`Quiescence` despite full weak fairness.

**Spec↔code binding (ITF conformance).** `npm run traces` exports eight
seeded `quint run` traces to `public/caching/testdata/itf/`;
`TestSpecTraceConformance` (caching_spectrace_test.go) infers the action
sequence from the state diffs and replays it against the real
implementation, asserting tier/version/pin/queue agreement after every
step. A drift on either side fails the Go suite.

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
npm run verify      # Apalache bounded proofs of Safety (depth 6 / 10)
npm run traces      # regenerate the ITF conformance traces (Go testdata)

# liveness needs TLC (one-time): grab tla2tools.jar to ~/.tlaplus/ (or set $TLA_TOOLS)
npm run liveness             # TLC: Quiescence holds under negative caching
npm run liveness:nonegcache  # TLC: the absent-key livelock lasso without it
```

Per spec, e.g.:

```sh
npx quint run  versioned_cache.qnt            --invariant=Safety --max-steps=14 --max-samples=20000
npx quint run  versioned_cache_unsafe_lww.qnt --invariant=MonotoneServe   # counterexample
npx quint test versioned_cache_unsafe_nopin.qnt                           # witness regression trace
npx quint run  freshness_ttl_unsafe.qnt       --invariant=FreshnessHonest # counterexample
```

## Not yet modelled (next increments)

- ✓ **Liveness (replay quiescence)** — modelled in `replay_liveness.tla`
  and proved with TLC; the negative-caching counterfactual shows fairness
  alone cannot prevent the absent-key livelock. See the status above.
  (Eventual replay of every *satisfiable* item is the `SatisfiableDone`
  half; finer-grained progress properties remain open.)
- **The thrash boundary** — characterize for which working-set/capacity
  ratios the suspend/replay protocol completes (the livelock test in the Go
  suite witnesses one side).
- **Replica-lag delivery** (see abstractions above).
- **ITF trace bridge** — export witness/counterexample traces
  (`--out-itf`) and replay them through the Go model driver's `byteOpSource`
  as a committed corpus, binding this spec to the implementation once it
  exists.
