---
type: explanation
audience: contributors
status: draft
---

> **Status: draft — pre-human-review.** Formal Quint/TLA⁺ model of the
> pushout exchange protocol. Every result claimed below is reproduced by a
> named `npm run` script and tabulated with its tool, depth and outcome in
> [Status by file](#status-by-file); this surrounding prose has not had human
> review.

# Formal spec: pushout exchange protocol

A [Quint](https://quint-lang.org) model of the **distributed layer** of the
boxer pushout engine — the package
[`github.com/stergiotis/boxer/public/algebraicarch/pushout`](../../../../public/algebraicarch/pushout).
This spec tree deliberately **mirrors that package path** under
`verification/formal/`, so the model sits beside the code it constrains. The
engine backs an external demo app (a consumer, in a separate repository). The
goal is to check the protocol's design **before** the real distribution
(NATS / gRPC behind the `PeerI`/`AcceptorI` seam) is built, while the seams are
still clean and there is no legacy wire protocol to preserve. The only carrier
that exists today is `exchange/inproc` — reliable, ordered, in-process — so the
loss / reordering / duplication this model admits describe that *future*
best-effort transport, not the shipped one; this is forward-looking design
validation, not verification of the demo as it runs now.

> Go file paths in this document are relative to the package root
> [`public/algebraicarch/pushout/`](../../../../public/algebraicarch/pushout).
> Code is referenced by symbol name, never by line number.

## Why model the protocol and not the merge algebra

The merge algebra — pushoutgraph pushout, commutativity, associativity,
apply/unapply inverse — is the part *least* likely to be wrong: it rests on
pijul's published patch theory and is already guarded by

- [`pushoutgraph/store/property_test.go`](../../../../public/algebraicarch/pushout/pushoutgraph/store/property_test.go) — commutativity / associativity / inverse;
- [`pushoutgraph/qc/invariants.go`](../../../../public/algebraicarch/pushout/pushoutgraph/qc/invariants.go) — structural + conflict invariants.

That correctness is exactly what lets this model abstract a repo's state to its
**applied set of patch ids**: if order does not matter, convergence is just set
equality. The risk that remains lives one level up — in the protocol that moves
patches between many crashing, retrying, garbage-collecting nodes. That is a
concurrent state machine, which is what a model checker is for.

## Status by file

Three kinds of check appear below, in decreasing strength:

- **Apalache** (`quint verify`) — symbolic bounded model checking: the
  invariant holds in *every* state reachable in at most *depth* steps
  (`NoError`). The bound is the only caveat.
- **TLC** — explicit-state model checking of the complete (finite) state
  space, the tool for the `◇□` liveness property.
- **Simulation** (`quint run`) — *N* random traces of up to *depth* steps;
  it finds violations but proves nothing. It is what establishes the
  counterexamples (a violation found is a proof that the property is false)
  and it is only a smoke test for the positive results.

Timings are from one run on 2026-09-01 (AMD Custom APU 0932, 8 threads,
Quint 0.32.0, Apalache 0.56.1, TLC from tla2tools v1.8.0, OpenJDK 23); CI
runs the same scripts on `ubuntu-24.04`.

| File | Models | Property | Tool | Depth / samples | Result | Script |
|------|--------|----------|------|-----------------|--------|--------|
| `pushout_exchange.qnt` | record / offer / deliverApply / unrecord / sweep / drop over a lossy carrier | `Safety` (= `DependencyClosure` ∧ `AppliedSubsetSeen` ∧ `PurgedSubsetApplied` ∧ `EnvelopeAvailable`) | Apalache | depth 6 | `NoError`, 168 s | `verify:exchange` |
| | | `Safety` | simulation | 20 000 × depth 12 | no violation, 3 s | `sweep` |
| | | `ErasureComplete` | simulation | depth 8 | **violated** (expected) | `findings` |
| | | 3 witness runs | `quint test` | — | pass | `test` |
| `erasure_dilemma.qnt` | 2 nodes, 2 patches, erasure that destroys the envelope | `EnvelopeAvailable` | simulation | depth 6 | **violated** (expected) | `findings` |
| `erasure_vault.qnt` | vault-by-design (ADR-0025): values in a non-propagated vault, patches carry references | `Safe` (= `EnvelopeAvailable` ∧ `PersonalDataErased`) | Apalache | depth 10 | `NoError`, 35 s | `verify:vault` |
| | | `Safe` | simulation | 20 000 × depth 12 | no violation, 2 s | `sweep` |
| `crash_recovery.qnt` | one repo's record + unrecord write orderings, `Open` recovery, crash between any two steps | `Safety` (7 invariants, see below) | Apalache | depth 12 | `NoError`, 221 s | `verify:crash` |
| | | `Safety` | simulation | 20 000 × depth 14 | no violation, 3 s | `sweep` |
| | | 5 witness runs (crash windows) | `quint test` | — | pass | `test` |
| `crash_recovery_unsafe.qnt` | counterfactual: record's writes swapped (append before put) | `NoCorruption` | simulation | depth 6 | **violated** (expected) | `findings` |
| `crash_recovery_unsafe_snapshot.qnt` | counterfactual: snapshot trusted and the log's positional suffix replayed, instead of coverage-based replay | `RecoveryCorrect` | simulation | depth 6 | **violated** (expected) | `findings` |
| `convergence.qnt` | record / offer / deliver over a reliable carrier | `DepClosed` | Apalache | depth 12 | `NoError`, 85 s | `verify:convergence` |
| | | `DepClosed` | simulation | 20 000 × depth 18 | no violation, 6 s | `sweep` |
| | | bounded liveness witness | `quint test` | — | pass | `test` |
| `convergence.tla` + `convergence.cfg` | the same model, TLA⁺ for TLC, weak fairness on every action | `Convergence` = `<>[]FullyReplicated`, `DepClosed` | TLC | complete state space (133 distinct states) | holds, 3 s | `liveness` |
| `convergence.tla` + `convergence_nofair.cfg` | the same model without fairness | `Convergence` | TLC | complete state space (133 distinct states) | **violated** (expected: a stuttering counterexample) | `liveness:nofair` |
| `frontier_reconcile.qnt` | frontier (DAG-head) exchange + dependency walk (ADR-0079 OQ-1) | `Safety` (= `DepClosed` ∧ `FrontierComplete` ∧ `FrontierEqualsFull` ∧ `FrontierCompact`) | Apalache | depth 5 | `NoError`, 152 s | `verify:frontier` |
| | | `FrontierComplete`, `FrontierCompact` (state-independent, quantified over all 16 subsets) | simulation, one evaluation | depth 0 × 1 | no violation | `theorem` |
| | | `Safety` | simulation | 20 000 × depth 14 | no violation, 16 s | `sweep` |
| | | 2 witness runs | `quint test` | — | pass | `test` |
| `frontier_reconcile_unsafe.qnt` | counterfactuals: heads without the walk; a non-closed set | `HeadsOnlySufficient`, `FrontierNeedsClosure` | simulation | depth 1 | both **violated** (expected) | `findings` |
| all `*.qnt` | — | well-typed | `quint typecheck` | — | pass, 24 s | `typecheck` |

`check` runs `typecheck`, `test`, `sweep`, `theorem`, `verify:all` and
`liveness:all` in that order and is what CI runs
([`.github/workflows/formal-pushout.yaml`](../../../../.github/workflows/formal-pushout.yaml)),
followed by `findings`. `findings` and `liveness:nofair` go through
[`expect_violation.sh`](./expect_violation.sh), which exits 0 **only if** the
checker reports a violation — so CI fails if a counterexample stops existing,
which would mean the property it refutes had silently become true (or the
counterfactual spec had drifted).

Depth is the only caveat on the Apalache rows: each bound was chosen as the
largest that finished within roughly five minutes on the machine above. An
earlier, undated attempt at `pushout_exchange.qnt` depth 10 did not finish
inside eight minutes; push a bound further with more time, a tighter `step`,
or Apalache tuning.

## Files

| File | What |
|------|------|
| `pushout_exchange.qnt` | The distributed protocol: record / offer / deliverApply / unrecord / sweep / drop, with safety invariants and executable witness runs. |
| `erasure_dilemma.qnt`  | A 2-node, 2-patch model isolating the erasure-vs-convergence tension (the impossibility horn). |
| `erasure_vault.qnt`    | The constructive resolution: vault-by-design (ADR-0025) — `EnvelopeAvailable` and `PersonalDataErased` hold together once the value moves to a non-propagated vault. |
| `crash_recovery.qnt`   | The single-repo durability layer: the record + unrecord commit ack-orderings + `Open` recovery, with a crash possible between any two steps. |
| `crash_recovery_unsafe.qnt` | Counterfactual (record writes swapped) — shows put-before-append is what makes recovery total. |
| `crash_recovery_unsafe_snapshot.qnt` | Counterfactual (snapshot trusted, positional suffix replay) — shows coverage-based replay is what makes unrecord atomic. |
| `convergence.qnt` | Liveness model (record / offer / deliver, reliable carrier) with a bounded witness and an Apalache-checked `DepClosed`; the readable source for the TLA⁺ below. |
| `convergence.tla` + `.cfg` | TLC-native companion: `<>[]FullyReplicated` holds under weak fairness (`convergence.cfg`) and is violated without it (`convergence_nofair.cfg`). Needs `tla2tools.jar`. |
| `frontier_reconcile.qnt` | The ADR-0079 OQ-1 optimization: frontier (DAG-head) exchange + dep walk, checked complete vs full-list exchange (Apalache + the powerset theorem). |
| `frontier_reconcile_unsafe.qnt` | Counterfactuals — heads-only (no walk) is incomplete, and completeness needs the closure invariant. |
| `expect_violation.sh` | Wrapper that succeeds only when a checker reports a violation; used by `findings` and `liveness:nofair`. |

## Refinement map (spec action → Go symbol)

The model is faithful only insofar as each action mirrors a real code path:

| Spec action    | Go symbol | Modelled semantics |
|----------------|-----------|--------------------|
| `record`       | `Repo.Record` ([repo/repo.go](../../../../public/algebraicarch/pushout/repo/repo.go)) | deps computed from referenced (applied) nodes |
| `offer`        | `exchange.Push` / `exchange.Pull` ([exchange/exchange.go](../../../../public/algebraicarch/pushout/exchange/exchange.go)) | ship a held envelope toward a peer |
| `deliverApply` | `Repo.ApplyEnvelope` | **idempotent**, **dependency-gated on the applied set** |
| `unrecord`     | `Repo.Unrecord` | refused if a dependent is applied or the patch was made permanent; **envelope kept** |
| `sweep`        | `Repo.Sweep` | purge tombstone content, make permanent, **keep the envelope** |
| `drop`         | carrier loss | a *future* best-effort transport (NATS/gRPC); `exchange/inproc` is reliable, so nothing drops today |

Faults the model admits — none exhibited by today's reliable `inproc` carrier,
all anticipated for the future wire transport: message **loss** (`drop`),
**reordering** and **duplication** (envelopes are an unordered set;
`deliverApply` is idempotent),
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

Status (see the table above): `Safety` holds in every state up to depth 6
under Apalache (`verify:exchange`), survives 20 000 randomized traces of
depth 12 (`sweep`), and all witness runs pass (`test`).

## The finding: erasure vs. convergence

Two invariants that **cannot both hold**, surfaced mechanically:

1. **`pushout_exchange.qnt` — `ErasureComplete` is false.**
   `quint run --invariant=ErasureComplete` produces: a node records a patch,
   `sweep`s it (`purged={p}`) — yet `seen={p}` still. The current Sweep purges
   the in-pushoutgraph tombstone but **keeps the wire envelope**, so it does not
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

**The constructive resolution — `erasure_vault.qnt`.** That escape is modelled
in full: move each personal-data *value* into a controller-side **vault** that
is never propagated, and have the patch carry only a reference (a carrier
token). Erasure (`forget`) deletes the vault row and never touches the envelope
layer, so `EnvelopeAvailable` and a new `PersonalDataErased` invariant hold
**simultaneously** in every reachable state up to depth 10 (`verify:vault`,
`NoError`) — the unsatisfiability above dissolves once the erasure
unit (a vault row) is separated from the convergence unit (an envelope token).
The model also pins the cost: its per-occurrence `recorded`-once guard is the
value-equality tradeoff — identical values become distinct references, so they
no longer content-converge (ADR-0025 SD13).

## Crash recovery: ack-ordering + unrecord atomicity

`crash_recovery.qnt` models one repo's durability for **both** write verbs, with
`crash` able to strike between any two steps (volatile state is then lost and
`repo.Open` recovers from durable storage):

- **Record** (`Repo.commitPatchLocked`): apply-on-clone →
  `PutEnvelope` (durable) → `AppendApplied` (durable, **commit point**) →
  in-memory commit.
- **Unrecord** (`Repo.Unrecord`): pre-flight (no applied dependent) +
  clone+Unapply → `SaveSnapshot(newApplied)` (durable) → `ReplaceApplied`
  (durable, **commit point**) → in-memory commit. The envelope is **kept**.

`Open` recovers by restoring a snapshot if its applied set is **covered** by
the log (`isSubset(snap.Applied, applied)` — a subset test, because
independent patches commute and the pushoutgraph state is a function of the
applied set), then replaying only the log entries the snapshot does not hold,
in log order; otherwise it discards the snapshot and replays the whole log
from empty. A log listing a patch twice is `ErrCorruptStore`.

Checked with Apalache to depth 12 (`verify:crash`, `NoError`):

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
precedes `ReplaceApplied` (the middle-patch snapshot `[1,3]` is covered by
`[1,2,3]`, so `Open` restores it and replays `2` from its kept envelope,
giving `{1,2,3}`); **unrecord durable** when the crash follows it.

**Why the orderings matter** (two counterfactuals, each a machine-found
counterexample — random simulation finds it within the first traces, and
`findings` asserts it keeps being found):

- `crash_recovery_unsafe.qnt` swaps record's writes (append-then-put);
  `quint run --invariant=NoCorruption` finds the crash that logs a patch whose
  envelope was never stored — `ErrCorruptStore`. So put-before-append
  (`PutEnvelope` before `AppendApplied` in `Repo.commitPatchLocked`) is *why*
  recovery is total.
- `crash_recovery_unsafe_snapshot.qnt` trusts a snapshot and replays the
  log's *positional* suffix (as a prefix-shaped recovery would);
  `quint run --invariant=RecoveryCorrect` finds a crash mid-unrecord whose
  snapshot is a subset but not a prefix of the log, **silently dropping the
  middle patch**. So `Open`'s coverage-based replay (restore a covered
  snapshot, replay exactly the entries it lacks) is a correctness
  requirement, not an optimization.

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
full replication, and its `DepClosed` safety invariant is checked with Apalache
to depth 12 (`verify:convergence`).

Results (TLC, 133 distinct states, complete state space):

- **`convergence.cfg` (with `WF` on every action) → holds** (`liveness`).
  "Model checking completed. No error has been found." Under weak fairness
  the dependency-coupled chain always completes: `record 1 → propagate →
  record 2,3 → propagate`, so every repo ends with `{1,2,3}`.
- **`convergence_nofair.cfg` (no fairness) → violated** (`liveness:nofair`,
  expected). TLC returns a counterexample that does some work then
  **stutters forever** before replicating. That is the mechanical proof that
  **fairness is required** — the liveness analogue of the safety
  counterfactuals.

Needs `tla2tools.jar` (TLC); see Running.

## Frontier reconciliation: a complete, cheaper exchange (ADR-0079 OQ-1)

`Pull`/`Push` ship the **full applied list** and diff it (`minus` in
`exchange/exchange.go`) — O(history) per round. ADR-0079 OQ-1 proposes
exchanging only the **frontier** (the DAG heads — patches nothing else depends
on) and walking the dependency DAG to fetch what's missing, like git's
have/want. `frontier_reconcile.qnt` checks that optimization is **complete**:

```
FrontierComplete == for every dependency-closed S:  reachDown(headsOf(S)) = S
```

i.e. advertising `headsOf(S)` and walking deps recovers exactly `S`, so a
frontier sync transfers the same missing set (`applied[a] \ applied[b]`) a
full-list sync would. How each invariant is established:

- `FrontierComplete` and `FrontierCompact` are **state-independent**: each is
  a single `forall` over the entire `2^|Patches|` powerset (16 subsets on the
  fixed 4-patch DAG), so evaluating either once is a complete check of every
  dependency-closed subset of that DAG. The `theorem` script does exactly that
  — one `quint run` at depth 0 with one sample, i.e. one evaluation of each
  formula in the initial state — and `verify:frontier` additionally has
  Apalache establish both symbolically in every state up to its bound
  (a state-independent formula is checked at every depth, including 0).
  (`FrontierCompact` records the saving: the frontier is strictly smaller
  whenever there's any depth.) Note the scope: this is a theorem about the
  model's fixed DAG, not about all DAGs.
- `FrontierEqualsFull` is then a **corollary** — `FrontierComplete` instantiated
  at each repo's applied set, which is closed by `DepClosed`.
- `DepClosed` (the one genuinely inductive obligation) and `FrontierEqualsFull`
  are checked with Apalache to depth 5 as part of `Safety`
  (`verify:frontier`), by 20 000 randomized traces of depth 14 (`sweep`), and
  by the witness runs.

Witnesses show a peer reconstructing a 3-deep chain from a single head, and
two diverged repos reconciling by swapping frontiers.

**The keystone:** completeness holds *because* every repo is
dependency-closed — the very invariant checked in `pushout_exchange.qnt`
(`DependencyClosure`) and `crash_recovery.qnt` (`LogDepClosed`). The safety
invariant is what licenses the optimization. Two counterfactuals in
`frontier_reconcile_unsafe.qnt` make that precise:

- `HeadsOnlySufficient` is false — shipping only the tips without walking deps
  drops every interior patch (e.g. `S = {1,2}`, `headsOf = {2} ≠ {1,2}`). You
  must walk the DAG.
- `FrontierNeedsClosure` is false — on a non-closed set (e.g. `{1,3}`, missing
  `2`) `reachDown(headsOf(S)) ≠ S`. Drop dependency-closure and frontier
  reconciliation becomes unsound.

## Running

```sh
npm ci                   # pins quint locally (package-lock.json is tracked)
npm run check            # typecheck + test + sweep + theorem + verify:all + liveness:all
npm run findings         # the expected violations: exit 0 only if every counterexample is found
npm run verify:all       # only the Apalache bounded verifies (slowest part; ~11 min total)
npm run liveness:all     # only the two TLC runs

# liveness needs TLC (one-time): grab tla2tools.jar to ~/.tlaplus/ (or set $TLA_TOOLS)
mkdir -p ~/.tlaplus && curl -fL -o ~/.tlaplus/tla2tools.jar \
  https://github.com/tlaplus/tlaplus/releases/download/v1.8.0/tla2tools.jar
npm run liveness         # TLC: Convergence holds under fairness -> "No error has been found"
npm run liveness:nofair  # TLC: Convergence fails without fairness -> stuttering counterexample (expected)
```

`quint verify` downloads Apalache into `~/.quint` on first use and needs a
JVM (Java 17+); TLC needs the same JVM. Each row of the status table names
the script that reproduces it; the per-file scripts are `verify:exchange`,
`verify:crash`, `verify:vault`, `verify:frontier`, `verify:convergence`,
`theorem`, `liveness`, `liveness:nofair`. To reproduce a single row by hand,
copy its command out of [`package.json`](./package.json).

The CI lane
[`formal-pushout.yaml`](../../../../.github/workflows/formal-pushout.yaml)
runs `npm run check` and `npm run findings` on manual dispatch and on `v*`
tags (the trigger every lane in this repo uses; nothing runs on an ordinary
push), caching `~/.quint` and `tla2tools.jar`. No
verify is skipped in CI: the whole lane is budgeted at 45 minutes and the
Apalache steps sum to roughly eleven minutes on the machine above.

## Not yet modelled (next increments)

- ✓ **Crash-recovery ack-ordering + unrecord atomicity** — modelled in
  `crash_recovery.qnt` (+ the two `_unsafe` counterfactuals); see the section
  above. `RecoveryCorrect` establishes each verb is all-or-nothing across any
  crash, to depth 12.
- ✓ **Liveness / convergence under fairness** — modelled in `convergence.qnt`
  and checked with TLC (`convergence.tla`): `<>[]FullyReplicated` holds under
  weak fairness, fails without it. See the section above.
- ✓ **Frontier reconciliation** (ADR-0079 OQ-1) — modelled in
  `frontier_reconcile.qnt` (+ counterfactuals): frontier exchange is complete
  vs full-list, *because* repos are dependency-closed. See the section above.
  (Set-sketch / IBLT-family reconciliation, the other OQ-1 branch, is still
  open — it trades exactness for probabilistic recovery and would need a
  collision/failure-rate model.)
- **Authentication / Byzantine peers.** `envelope.Validate`
  ([envelope/envelope.go](../../../../public/algebraicarch/pushout/envelope/envelope.go))
  already does hash tamper-detection; add signatures and model a peer that
  ships well-formed-but-unauthorized envelopes.
- **Clock skew.** `Sweep` / retention horizons read wall-clock time
  (`Options.Clock`); across nodes that is untrustworthy. Model a logical clock.
