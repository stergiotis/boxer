# Formal spec: pushout exchange protocol

A [Quint](https://quint-lang.org) model of the **distributed layer** of the
boxer pushout engine — the package `public/algebraicarch/pushout`. This spec
tree deliberately **mirrors that package path** under `verification/formal/`,
so the model sits beside the code it constrains. The engine backs the
`pijuldemo` app (a consumer, in the separate hackathon repo). The goal is to
pin down a *long-term-correct* protocol **before** the real distribution
(NATS / gRPC behind the `PeerI`/`AcceptorI` seam) is built, while the seams are
still clean and there is no legacy wire protocol to preserve.

> Go file paths in this document are relative to the package root
> `public/algebraicarch/pushout/`.

## Why model the protocol and not the merge algebra

The merge algebra — graggle pushout, commutativity, associativity,
apply/unapply inverse — is the part *least* likely to be wrong: it rests on
pijul's published patch theory and is already guarded by

- `graggle/store/property_test.go` — commutativity / associativity / inverse;
- `graggle/qc/invariants.go` — 14 structural + conflict invariants.

That correctness is exactly what lets this model abstract a repo's state to its
**applied set of patch ids**: if order does not matter, convergence is just set
equality. The risk that remains lives one level up — in the protocol that moves
patches between many crashing, retrying, garbage-collecting nodes. That is a
concurrent state machine, which is what a model checker is for.

## Files

| File | What |
|------|------|
| `pushout_exchange.qnt` | The distributed protocol: record / offer / deliverApply / unrecord / sweep / drop, with safety invariants and executable witness runs. |
| `erasure_dilemma.qnt`  | A 2-node, 2-patch model isolating the erasure-vs-convergence tension. |
| `crash_recovery.qnt`   | The single-repo durability layer: the commit ack-ordering + `Open` recovery, with a crash possible between any two steps. |
| `crash_recovery_unsafe.qnt` | The counterfactual (writes swapped) that shows the ack-ordering is what makes recovery total. |

## Refinement map (spec action → Go symbol)

The model is faithful only insofar as each action mirrors a real code path:

| Spec action    | Go symbol | Modelled semantics |
|----------------|-----------|--------------------|
| `record`       | `repo.Repo.Record` (repo/repo.go:230) | deps computed from referenced (applied) nodes |
| `offer`        | `exchange.Push` / `Pull` (exchange/exchange.go:102/:59) | ship a held envelope toward a peer |
| `deliverApply` | `repo.Repo.ApplyEnvelope` (repo/repo.go:275) | **idempotent**, **dependency-gated on the applied set** |
| `unrecord`     | `repo.Repo.Unrecord` (repo/repo.go:339) | refused if a dependent is applied or the patch was made permanent; **envelope kept** |
| `sweep`        | `repo.Repo.Sweep` (repo/repo.go:406) | purge tombstone content, make permanent, **keep the envelope** |
| `drop`         | carrier loss | `PeerI`/`AcceptorI` are best-effort; no single Go symbol |

Faults the model admits: message **loss** (`drop`), **reordering** and
**duplication** (envelopes are an unordered set; `deliverApply` is idempotent),
and **partial sync** (because `Push`/`Pull` stop on first error, a peer can be
left holding any dependency-closed prefix — and that prefix-safety is exactly
the `DependencyClosure` invariant).

## Safety invariants (must hold)

| Invariant | Meaning |
|-----------|---------|
| `DependencyClosure` | every applied patch's declared deps are applied — the partial-sync prefix is always valid |
| `AppliedSubsetSeen` | you can always re-ship what you've applied |
| `PurgedSubsetApplied` | a purged patch is permanent and stays applied |
| `EnvelopeAvailable` | any patch applied somewhere is held (as an envelope) somewhere → gaps are always closable |
| `Safety` | conjunction of the above |

Status: all four survive 2000 randomized traces each (`quint run`), all witness
runs pass (`quint test`), and `Safety` is **proved to depth 6 with Apalache**
(`quint verify`, ~16 s, `NoError`). Depth 10 did not finish inside an 8-minute
budget at this model size (state-space growth) — push the bound further with
more time, a tighter `step`, or Apalache tuning.

## The finding: erasure vs. convergence

Two invariants that **cannot both hold**, surfaced mechanically:

1. **`pushout_exchange.qnt` — `ErasureComplete` is false.**
   `quint run --invariant=ErasureComplete` produces: a node records a patch,
   `sweep`s it (`purged={p}`) — yet `seen={p}` still. The current Sweep purges
   the in-graggle tombstone but **keeps the wire envelope**, so it does not
   actually erase the data. Convergence is safe; GDPR/FADP erasure is not.

2. **`erasure_dilemma.qnt` — `EnvelopeAvailable` is false under real erasure.**
   `quint run --invariant=EnvelopeAvailable` produces: a node records `p1`,
   `sweepErase`s it (destroying the envelope, as true erasure demands) — now
   `p1` is *applied* but its envelope exists **nowhere**. Any node lacking `p1`
   (and anything depending on it, e.g. `p2`) can never converge.

Together: **`ErasureComplete ∧ EnvelopeAvailable` is unsatisfiable.** That is an
architecture decision the protocol must make explicitly, not an implementation
detail. The usual escape is **per-patch crypto-erasure**: keep the (encrypted)
envelope so structure/deps survive and re-ship works, throw away the key to
satisfy erasure. The spec is where that design gets validated.

## Crash recovery: the ack-ordering is load-bearing

`crash_recovery.qnt` models one repo's durability. The commit path
(`commitPatchLocked`, repo/repo.go:309) is four steps —
apply-on-clone → `PutEnvelope` (durable) → `AppendApplied` (durable, the
**commit point**) → in-memory commit — and `crash` can strike between any two,
wiping volatile state. `Open` (repo/repo.go:109) then recovers: if a snapshot's
applied list is a prefix of the log, restore it and replay only the suffix,
else replay the whole log from empty.

Proved with Apalache to depth 12 (`NoError`):

| Invariant | Meaning |
|-----------|---------|
| `NoCorruption` | recovery never returns `ErrCorruptStore` |
| `LoggedImpliesStored` | every logged patch has a durable envelope |
| `LogDepClosed` | the durable log is dependency-closed |
| `LogNoDup` | no patch is committed twice |
| `StableConsistency` | with no op in flight, memory equals the durable log |
| `SnapshotIsPrefix` | a snapshot never disagrees with the log it accelerates |

The witness runs pin the three crash windows: before the commit point the patch
is lost (its envelope a harmless orphan); after it the patch survives even
though the in-memory commit never ran; and a snapshot prefix plus a replayed
suffix reconstructs the state.

**Why the order matters.** `crash_recovery_unsafe.qnt` swaps the two durable
writes (append-then-put). `quint run --invariant=NoCorruption
crash_recovery_unsafe.qnt` then finds a crash in the window that leaves the log
pointing at a patch with no envelope — `ErrCorruptStore`. So put-before-append
(repo/repo.go:315 before :318) is *why* recovery is total, proved by the contrast.

## Running

```sh
npm install        # pins quint locally
npm run check      # typecheck + test + randomized Safety sweeps, ALL specs
npm run verify     # Apalache bounded proofs of Safety (exchange + crash_recovery)
npm run findings   # prints the counterexample traces (erasure, unsafe ordering)
```

Per spec, e.g.:

```sh
npx quint verify crash_recovery.qnt        --invariant=Safety --max-steps=12  # NoError
npx quint run    crash_recovery_unsafe.qnt --invariant=NoCorruption           # counterexample
npx quint run    pushout_exchange.qnt      --invariant=ErasureComplete        # counterexample
```

## Not yet modelled (next increments)

- ✓ **Crash-recovery ack-ordering** — modelled in `crash_recovery.qnt` (+ the
  `_unsafe` counterfactual); see the section above.
- **Unrecord + `ReplaceApplied` atomicity.** `Unrecord` (repo/repo.go:339) writes
  a snapshot then atomically replaces the log; a crash between them leaves a
  non-prefix snapshot that `Open` must discard. Extend `crash_recovery.qnt` with
  `unrecord` and assert the discard is safe.
- **Liveness / convergence under fairness.** Convergence is a `◇□`(all applied
  sets equal) property needing weak fairness on `deliverApply`+`offer` and
  quiescence on `record`/`drop`. Apalache's liveness support is thin; export to
  TLA⁺ (`quint compile --target tlaplus`) and check with TLC.
- **Smarter reconciliation** (frontiers / set sketches) — ADR-0079 OQ-1 — vs.
  the current full applied-list exchange.
- **Authentication / Byzantine peers.** `envelope.Validate` (envelope/envelope.go:67)
  already does hash tamper-detection; add signatures and model a peer that ships
  well-formed-but-unauthorized envelopes.
- **Clock skew.** `Sweep` / retention horizons read wall-clock time
  (`Options.Clock`); across nodes that is untrustworthy. Model a logical clock.
