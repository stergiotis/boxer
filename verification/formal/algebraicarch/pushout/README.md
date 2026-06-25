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
| `crash_recovery.qnt`   | The single-repo durability layer: the record + unrecord commit ack-orderings + `Open` recovery, with a crash possible between any two steps. |
| `crash_recovery_unsafe.qnt` | Counterfactual (record writes swapped) — shows put-before-append is what makes recovery total. |
| `crash_recovery_unsafe_snapshot.qnt` | Counterfactual (snapshot trusted without the prefix check) — shows the prefix-or-discard rule is what makes unrecord atomic. |
| `convergence.qnt` | Liveness model (record / offer / deliver, reliable carrier) with a bounded witness; the readable source for the TLA⁺ below. |
| `convergence.tla` + `.cfg` | TLC-native companion proving `<>[]FullyReplicated` under fairness; `convergence_nofair.cfg` shows it fails without fairness. |

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

## Crash recovery: ack-ordering + unrecord atomicity

`crash_recovery.qnt` models one repo's durability for **both** write verbs, with
`crash` able to strike between any two steps (volatile state is then lost and
`Open`, repo/repo.go:109, recovers from durable storage):

- **Record** (`commitPatchLocked`, repo/repo.go:309): apply-on-clone →
  `PutEnvelope` (durable) → `AppendApplied` (durable, **commit point**) →
  in-memory commit.
- **Unrecord** (repo/repo.go:339): pre-flight (no applied dependent) +
  clone+Unapply → `SaveSnapshot(newApplied)` (durable) → `ReplaceApplied`
  (durable, **commit point**) → in-memory commit. The envelope is **kept**.

`Open` recovers by restoring a snapshot only if its applied list is a **prefix**
of the log (replaying just the suffix), otherwise discarding it and replaying
the whole log from empty.

Proved with Apalache to depth 12 (`NoError`):

| Invariant | Meaning |
|-----------|---------|
| `NoCorruption` | recovery never returns `ErrCorruptStore` |
| `LoggedImpliesStored` | every logged patch has a durable envelope |
| `LogDepClosed` | the durable log is dependency-closed |
| `LogNoDup` | no patch is committed twice |
| `StableConsistency` | with no op in flight, memory equals the durable log |
| `SnapshotConsistent` | a snapshot's stored state equals its own applied list |
| `RecoveryCorrect` | **after any crash, `Open` reconstructs exactly the durable log's state** |

`RecoveryCorrect` is the atomicity result: each verb either fully took effect or
not at all, across any crash. The witnesses pin the windows — record lost before
/ durable after its commit point; **unrecord rolled back** when the crash
precedes `ReplaceApplied` (the middle-patch snapshot `[1,3]` is not a prefix of
`[1,2,3]`, so `Open` discards it and the kept envelope lets the full replay
restore `{1,2,3}`); **unrecord durable** when the crash follows it.

**Why the orderings matter** (two counterfactuals, each a mechanical proof):

- `crash_recovery_unsafe.qnt` swaps record's writes (append-then-put);
  `quint run --invariant=NoCorruption` finds the crash that logs a patch whose
  envelope was never stored — `ErrCorruptStore`. So put-before-append
  (repo/repo.go:315 before :318) is *why* recovery is total.
- `crash_recovery_unsafe_snapshot.qnt` trusts a snapshot without the prefix
  check; `quint run --invariant=RecoveryCorrect` finds a crash mid-unrecord
  whose non-prefix snapshot is used as a base, **silently dropping a patch**. So
  `Open`'s prefix-or-discard (repo/repo.go:147) is a correctness requirement,
  not an optimization.

## Convergence: liveness under fairness

Safety says nothing bad happens; **liveness** says something good eventually
does. `convergence.qnt` models progress — `record` (each patch authored once at
its origin), perpetual `offer` (Push/Pull recompute the diff each run), and
dependency-gated `deliver` over a **reliable** carrier (loss/reorder/dup are a
safety concern, covered in `pushout_exchange.qnt`). The property:

```
Convergence == <>[]FullyReplicated   (eventually, every node holds every patch)
```

This is a `◇□` property: it needs **fairness** (an enabled sync must not be
starved forever) and is checked with **TLC**, not Apalache (whose liveness
search is impractically slow even on a toy here). `convergence.tla` is a
TLC-native module kept in lockstep with the Quint source (same Nodes / Patches /
deps / origin and the same three actions); `convergence.qnt` additionally
carries a fast bounded witness (`quint test`) of one fair interleaving reaching
full replication.

Results (TLC, 133 distinct states):

- **`convergence.cfg` (with `WF` on every action) → holds.** "Model checking
  completed. No error has been found." Under weak fairness the dependency-
  coupled chain always completes: `record 1 → propagate → record 2,3 →
  propagate`, so every repo ends with `{1,2,3}`.
- **`convergence_nofair.cfg` (no fairness) → violated.** TLC returns a
  counterexample that does some work then **stutters forever** before
  replicating. That is the mechanical proof that **fairness is required** — the
  liveness analogue of the safety counterfactuals.

Needs `tla2tools.jar` (TLC); see Running.

## Running

```sh
npm install         # pins quint locally
npm run check       # typecheck + test + randomized Safety sweeps, ALL specs
npm run verify      # Apalache bounded proofs of Safety (exchange + crash_recovery)
npm run findings    # prints the counterexample traces (erasure, unsafe ordering)

# liveness needs TLC (one-time): grab tla2tools.jar to ~/.tlaplus/ (or set $TLA_TOOLS)
curl -fL -o ~/.tlaplus/tla2tools.jar \
  https://github.com/tlaplus/tlaplus/releases/latest/download/tla2tools.jar
npm run liveness         # TLC: Convergence holds under fairness -> "No error has been found"
npm run liveness:nofair  # TLC: Convergence fails without fairness -> stuttering counterexample
```

Per spec, e.g.:

```sh
npx quint verify crash_recovery.qnt                 --invariant=Safety --max-steps=12  # NoError
npx quint run    crash_recovery_unsafe.qnt          --invariant=NoCorruption           # counterexample
npx quint run    crash_recovery_unsafe_snapshot.qnt --invariant=RecoveryCorrect        # counterexample
npx quint run    pushout_exchange.qnt               --invariant=ErasureComplete        # counterexample
```

## Not yet modelled (next increments)

- ✓ **Crash-recovery ack-ordering + unrecord atomicity** — modelled in
  `crash_recovery.qnt` (+ the two `_unsafe` counterfactuals); see the section
  above. `RecoveryCorrect` proves each verb is all-or-nothing across any crash.
- ✓ **Liveness / convergence under fairness** — modelled in `convergence.qnt`
  and proved with TLC (`convergence.tla`): `<>[]FullyReplicated` holds under weak
  fairness, fails without it. See the section above.
- **Smarter reconciliation** (frontiers / set sketches) — ADR-0079 OQ-1 — vs.
  the current full applied-list exchange. This is where liveness gets
  interesting again: a frontier protocol must still guarantee progress.
- **Authentication / Byzantine peers.** `envelope.Validate` (envelope/envelope.go:67)
  already does hash tamper-detection; add signatures and model a peer that ships
  well-formed-but-unauthorized envelopes.
- **Clock skew.** `Sweep` / retention horizons read wall-clock time
  (`Options.Clock`); across nodes that is untrustworthy. Model a logical clock.
