---
type: adr
status: accepted
date: 2026-06-12
reviewed-by: "@stergiotis"
reviewed-date: 2026-06-12
---

# ADR-0079: pushout production architecture — pluggable storage, wire codec, and transport seams

## Context

`public/algebraicarch/pushout` is graduating from a four-actor demo to a
building block of a large distributed system. The 2026-06-11 review
remediation made the *semantics* production-grade (deleter-set
tombstones, deps-in-hash identity, transactional verbs, an extensive
oracle battery — see the package EXPLANATION.md), but three demo-era
shortcuts remain load-bearing and block production use:

- **Persistence is theater.** `applied.txt` is written but never read;
  the graggle exists only in memory; a restart loses everything.
  Retention markers (`tombstoneAt`, `contentPurged`) are session-local,
  so a purge performed for storage-limitation compliance silently
  un-happens on restart — a compliance hole, not an inconvenience
  (verified: no code path reads `applied.txt`; `store.Graggle` has no
  serialization).
- **The engine is welded to the demo.** The reusable patch-log /
  dependency-gating / identity-disambiguation / sync logic lives inside
  `pijul/pijul_pushout_backend.go`, typed in terms of KV cells. The
  demo's GUI consumer (an external repository) locks
  the exported `PushoutRepo.Mu` and walks `PushoutRepo.Graggle`
  directly — exported-internals coupling that taxes every backend
  change.
- **Single wire format, single transport, errors as strings.** The
  envelope codec is hardcoded JSON; sync is in-process method calls;
  callers (including our own test harness) branch on error message
  substrings.

Forces:

- **Pluggability is a hard requirement.** Storage, wire serialization,
  and transport must each be replaceable by third implementations with
  nothing but the interface contract and an executable conformance
  suite. The immediate forcing function: the next demonstrator uses
  **NATS as transport and a custom serialization format**. boxer itself
  must not grow a NATS dependency (transports live with their
  demonstrators; the consumer's module graph already carries NATS).
- **Identity must survive re-serialization.** Patch identity
  (`patch.ComputeHash`, BLAKE3 over canonicalized deps + changes) is
  content-addressed and wire-format-independent today; mixed-codec
  fleets must keep it that way or content addressing collapses.
- **Crash consistency is the new correctness frontier.** The existing
  in-memory transactionality (clone-and-swap, validate-before-mutate)
  must extend to disk: a crash at any point may lose unacknowledged
  work but must never corrupt acknowledged state, and purge markers
  must be durable once a sweep returns.
- **The oracle battery must carry over.** The rapid state machine,
  model oracle, differential references, goldens, and fault injection
  are the package's safety net; the refactor must land inside them, and
  recovery itself becomes a new verb under test.
- **No consumers yet.** The external GUI demo may be transformed
  freely; its capabilities (draft-diff preview, DOT visualizations,
  playbooks) must remain expressible through the new API.

Conventions this ADR follows (verified in-repo): interface names `*I`,
sentinel errors with `errors.Is` (established in `public/caching`,
`public/gov`), `eh`/`eb` error wrapping with no package-name message
prefixes, seam-with-multiple-impls precedent in `public/caching/`
(`StashBackendI` + pebble/pogreb under `caching/diskbacked/`).

## Design space (QOC)

**Q1: Where does the domain-neutral engine live?**

- O1a: grow `pijul.PushoutRepo` in place, keep KV coupling.
- O1b: new `pushout/repo` package: engine speaks patches and hashes
  only; `pijul` becomes a thin KV adapter.

Criteria: API tightness, consumer decoupling, demo-transform freedom.
O1b wins; O1a perpetuates the weld this ADR exists to cut.

**Q2: How do envelopes stay decodable across heterogeneous codecs?**

- O2a: per-deployment fixed codec; peers must agree out of band.
- O2b: self-describing frame (`magic | codec-name | payload`) plus a
  codec registry; repos store and ship framed bytes as received.
- O2c: logical store — decode at the boundary, re-encode in the local
  codec for storage and shipping.

Criteria: mixed-fleet interop, storage simplicity, identity stability.
O2b chosen: O2a makes the json↔custom demonstrator pair impossible
without flag days; O2c also works (identity is codec-independent, so
re-encoding is safe) but adds a mandatory re-encode on every hop and
makes "the bytes I received" unreproducible for audit. Identity codec
(the canonical form fed to BLAKE3 inside `ComputeHash`) is explicitly
NOT the wire codec and does not change here.

**Q3: What does a graggle snapshot contain?**

- O3a: full state including derived structures (union-find partition,
  pseudo-edges, reason maps, dirty set).
- O3b: essential state only (nodes, contents, deleted set, deleter
  sets, all edges with kind+introducedBy, tombstoneAt, contentPurged);
  rebuild derived state on load (re-partition by adjacency, mark all
  components dirty, resolve).

Criteria: codec surface, forward compatibility, self-healing.
O3b chosen: the derived structures are exactly the historically
fragile bookkeeping; rebuilding them on load shrinks the format,
heals any persisted drift, and the qc invariants validate the rebuilt
result. Cost: O(deleted) extra work per open — acceptable; snapshots
exist to avoid O(history) replay, not O(state) reconstruction.

**Q4: How does recovery relate snapshot to log?**

- O4a: snapshot is authoritative; log truncated to it.
- O4b: log is authoritative; snapshot is a cache valid only if its
  applied list is a *prefix* of the log; otherwise discard and
  full-replay from envelopes.

O4b chosen. The log is the system of record (append on apply, atomic
rewrite on unrecord); correctness never depends on snapshot freshness.
Replay asserts dependencies precede dependents and fails loudly
otherwise (corrupt store is an error, not a guess).

**Q5: When do purge markers become durable?**

- O5a: on next periodic checkpoint (window of loss).
- O5b: transactional sweep — sweep a clone, persist the snapshot, then
  swap; the sweep result is durable before the verb returns.

O5b chosen; the compliance property is the point of sweeping. Unrecord
likewise checkpoints after the log rewrite so recovery stays
O(suffix) — though by Q4/O4b its correctness does not depend on it.

**Q6: Transport seam shape?**

- O6a: message-schema protocol (defined wire messages per transport).
- O6b: Go interface pair — `PeerI` (read: applied list, fetch
  envelopes) + `AcceptorI` (write: apply envelope) — with
  `exchange.Pull/Push` orchestrating; each transport maps the
  interfaces onto its own carrier and serializes its own control
  messages; envelope bytes are opaque framed blobs.

O6b chosen: it keeps boxer transport-free, makes the in-process demo
transport trivial, and maps 1:1 onto NATS request-reply. The v1
protocol is full-applied-list exchange; smarter reconciliation is OQ-1.

## Decision

Adopt the layered seam architecture:

```
graggle/*            engine core (semantics unchanged; + sentinels, snapshot codec)
envelope             logical EnvelopeV1, codec-independent Validate,
                     CodecI + frame + Registry, jsonv1; codectest suite
repo                 domain-neutral engine: StorageI contract, Open/recovery,
                     Record/ApplyEnvelope/Unrecord/Sweep/Checkpoint/Close,
                     View read transactions, hooks, error taxonomy
repo/filestore       reference StorageI (atomic renames, fsync, sharded envelopes)
repo/storagetest     StorageI conformance suite
exchange             PeerI/AcceptorI + Pull/Push; exchange/inproc; exchangetest
pijul                demo KV adapter over repo + exchange/inproc (text backend untouched)
```

Layering: `repo` → graggle+envelope; `exchange` → repo; storage and
transport implementations import only their seam package. The NATS
transport and the custom codec are implemented consumer-side
against `exchangetest`/`codectest`.

### Subsidiary design decisions

- **SD1 — ack ordering.** Mutating verbs sequence: validate → apply to
  clone → `PutEnvelope` → `AppendApplied` → swap in memory. A crash
  leaves "never happened" or an orphan content-addressed envelope
  (harmless; GC is OQ-2).
- **SD2 — frame format.** `"PXE1"` magic, uvarint name length, codec
  name, payload. Unknown codec → `ErrUnknownCodec` at decode, not at
  transport.
- **SD3 — error taxonomy.** Exported sentinels per layer (store, patch,
  envelope, repo, demo), wrapped with `eh.Errorf("…: %w", Err…)`;
  callers use `errors.Is`. The test harness drops string matching.
- **SD4 — reads are lock-shared and mutation-free.** Verbs leave the
  graggle resolved; `repo.View` runs under RLock with a documented
  no-escape rule and asserts `DirtyRepCount()==0` instead of resolving
  defensively. `store.Render` keeps its documented mutating behavior
  for direct store users.
- **SD5 — conformance suites are part of the contract.** Each seam
  ships `…test.Run(t, factory)`; the in-tree reference implementations
  are themselves validated by the suites, keeping suite and contract
  honest. This introduces the exported-conformance-suite pattern to
  boxer deliberately.
- **SD6 — clone-and-swap stays.** In-place mutation with an undo
  journal is a Tier-3 performance refactor; the seams introduced here
  do not change when it lands.
- **SD7 — wire break accepted.** Framed envelopes invalidate previously
  persisted envelope files (none exist outside tests/demo state). The
  envelope golden regenerates once and pins the framed format.
- **SD8 — clock injection.** `repo.Options.Clock` feeds both envelope
  timestamps and the graggle tombstone clock; deterministic tests and
  the rapid harness depend on never reading wall time inside engine
  paths.

## Alternatives

Considered and rejected above per question; additionally: building on
an existing embedded store as the *only* backend (pebble) was rejected
for the reference implementation — a transparent file layout retains
the demo's debuggability and keeps the conformance suite grounded in
two very different storage shapes (files now, KV later, OQ-4).

## Consequences

### Positive

- Restart-safe repos with O(snapshot + suffix) open; purge durability.
- Three independently implementable seams with executable contracts;
  the NATS/custom-codec demonstrator needs no boxer changes.
- Engine API without exported mutexes or graph internals; consumers
  use read transactions and logical patch lookups.
- Error handling becomes programmatic (`errors.Is`).

### Negative

- More packages and indirection than the demo needed.
- Framed envelopes are not raw JSON on disk anymore (debuggability
  mitigated by `jsonv1` payload remaining human-readable after the
  short header).
- Full-replay fallback can be slow on large histories with stale
  snapshots (bounded by checkpoint-after-unrecord; OQ-1/OQ-5 address
  scale).

### Neutral

- The pijul demo keeps its `RepoI` surface; the text backend is
  untouched.
- The external GUI demo is rewritten onto `View`/`PatchInfo` —
  sanctioned by "no consumers yet".

### Derived practices

- New invariants and verbs must extend the conformance suites and the
  rapid harness in the same change.
- Engine code never reads wall time or randomness directly (SD8).

## Status

Accepted — 2026-06-12. Implementation phases P1–P7 landed, verified
(full battery incl. race, conformance suites, state-machine soak with
the reopen verb), and pushed; the external GUI demo consumes the
engine through the public read API.

## Update — 2026-06-25: retention-clock durability; conflict-ordering, identity-claim, and storage-lock fixes

A distributed-systems review of the package surfaced one residual design
gap and three smaller defects. All four are addressed in this update; the
gap's solution (the retention ledger) is described in full below, then
implemented with the conformance + rapid-harness coverage its `StorageI`-
contract change requires (per Derived Practices).

### Retention-clock durability (implemented)

Q5/O5b made the *purge result* durable: once `Sweep` returns,
`contentPurged` survives restart, because the swept clone is snapshotted
before the verb acks. But the *pending* retention horizon —
`tombstoneAt[id]` for tombstones not yet swept — is **not** durable
across the full-replay recovery path. `tombstoneAt` is stamped by
`DeleteNode` from `Options.Clock` at apply time and persisted only inside
the GRG1 snapshot; snapshot-prefix recovery restores it, but full replay
re-runs `DeleteNode` and re-stamps every tombstone to *replay time*. Full
replay is the crash fallback (a non-prefix snapshot, e.g. a crash
mid-unrecord) and the only path for a fresh clone (which has no
snapshot). So §Context's "purge un-happens on restart" hole was closed
for swept content but left open for the *un-swept horizon*: it resets to
"now" on those paths. Two consequences of different character:

- **Same-store restart — fixable.** A crash without a clean `Close`, or a
  non-prefix snapshot, resets horizons on the next `Open` even though the
  data has objectively been tombstoned since the original delete. A repo
  that crash-loops faster than its horizon could defer a compliance purge
  indefinitely.
- **Fresh clone — intrinsic.** A new replica receives only envelopes +
  log; envelopes carry no trusted time (identity is clock-free by
  design), so a clone has no replay-stable basis for an earlier stamp.
  Starting the horizon at clone time is defensible under a *replica-local*
  retention model ("I have held this data since I received it"), but it
  means re-cloning resets the fleet's erasure clock — a fleet-coordination
  problem, not a per-replica one.

**Decision.** Introduce a **replica-local, replay-stable retention
ledger**: a durable `NodeID → first-observed-deleted (unix-nanos)` map
owned by `StorageI`, written when a committing verb creates a tombstone
(and on sweep/unrecord), and *seeded into the graggle at `Open` instead of
re-stamping*. Because it persists independently of the snapshot and is
never reconstructed from envelopes, it survives full replay on the same
store, closing the same-store case. `StorageI` delta:
`SaveRetention(ctx, []RetentionEntry)` (atomic replace, mirroring
`ReplaceApplied`) + `LoadRetention(ctx)`; `storagetest` gains a Retention
check and folds the ledger into its reopen-durability case; the in-tree
fakes embed `StorageI` and inherit the pair (the fault store overrides
`SaveRetention` to inject failures). `Open` reconciles: adopt the ledger's
stamp for each current tombstone where present, keep the decode/replay
stamp otherwise, drop entries for nodes no longer tombstoned, and write
back only when the set changed. The graggle's `tombstoneAt` is a working
copy of the ledger, re-seeded via `SeedTombstoneStamps`.

**Rejected.** *Salvaging stamps from a discarded (non-prefix) snapshot* —
couples retention to topology the engine has decided not to trust, and
does nothing for a fresh clone (no snapshot exists). *Per-patch
first-applied-at* instead of per-node — adds a node→deleter→time
indirection without changing the durability story; per-node matches the
existing `tombstoneAt` shape.

The fresh-clone case is explicitly **not** closed by the ledger.
Fleet-wide erasure that survives re-cloning is **OQ-7**, owned by
ADR-0025's cooperative-purge layer (compensating patches / erasure
propagation), with the durable ledger as the per-replica primitive it
builds on. Code comments (`graggle.tombstoneAt`, `SweepTombstones`) and
`doc/explanation/pushout-distributed-operation.md` §2 are corrected to
state this durability boundary rather than implying session-local
retention is wholly benign. This refines, but does not contradict, SD8:
the clock stays injected (determinism preserved); the ledger adds
*durability* of the stamp, which SD8 never promised.

**Implemented** as described above: `StorageI.SaveRetention`/
`LoadRetention` + the `filestore` `retention.txt` ledger + a `storagetest`
Retention check; `Open` seeds from the ledger then writes back when the
set changed; ledger writes happen on tombstone-changing commits *before*
the commit point (`AppendApplied`/`ReplaceApplied`), so a failed retention
write is crash-equivalent and an orphan ledger entry is dropped on
recovery. A public `Repo.RetentionStamps` accessor exposes the horizon for
audit, and the rapid harness's reopen verb now asserts horizon durability
across recovery. The fresh-clone case remains **OQ-7**.

### Fixes landed with this update

- **Deterministic conflict reporting.** `algo.DetectConflicts` iterated
  adjacency in apply order, so two replicas with identical converged
  state could emit the same conflict *set* in different list / intra-
  conflict order — a cross-peer-determinism asymmetry versus
  `LinearOrder`, which is already canonical. Output is now sorted
  (conflict list and member nodes) by `CompareNodeID`, matching
  `TopoSort`'s existing discipline. The conflict *set* is unchanged, so
  qc invariant 13 still holds.
- **Identity-disambiguation claim corrected.** `Repo.Record`'s
  collision shift is deterministic relative to the **local** applied
  set; the comment's "concurrent identical re-creations on different
  repos still converge on one patch" holds only when both repos carry
  the same colliding patches. With divergent collision histories the
  re-creation degrades to a benign duplicate (a fork conflict,
  indistinguishable from two actors typing the same line). Comment
  reworded; behaviour is already safe and unchanged.
- **Storage single-writer enforcement.** `filestore.Open` took no
  inter-process lock; two processes on one root could interleave
  `applied.txt` / snapshot writes and corrupt acknowledged state. `Open`
  now takes an advisory `flock` (`LOCK_EX|LOCK_NB`, released on `Close`,
  auto-released on process death) and returns `ErrLocked` on contention.
  Unix-gated (`lock_unix.go`) with a no-op fallback elsewhere
  (`lock_other.go`); the crash-simulation engine tests now release the
  store lock as a process exit would.

## Update — 2026-06-26: formal model of the exchange protocol, recovery, and frontier reconciliation

A Quint / TLA⁺ model of the distributed layer now sits beside the code
at `verification/formal/algebraicarch/pushout/` (mirroring the package
path). It checks the protocol *design* ahead of the real transport: the
only carrier that exists today is the reliable in-process
`exchange/inproc`, so the loss / reorder / duplication the specs admit
describe the *future* NATS/gRPC wire, not the shipped one. The model
abstracts a repo to its applied *set* of patch ids — licensed by the
merge algebra's order-independence, already property-tested in graggle —
and machine-checks the protocol against this ADR's decisions:

- **Exchange safety** (`pushout_exchange.qnt`, Apalache) — every repo
  stays dependency-closed under loss / reorder / duplication / partial
  sync. The prefix a peer is left holding when `Push`/`Pull` stop on the
  first error (O6b) is always dependency-closed.
- **Crash-recovery atomicity** (`crash_recovery.qnt`, Apalache) — Record
  and Unrecord are each all-or-nothing across a crash between any two
  steps. Two counterfactuals show the SD1 ack-ordering and the Q4/O4b
  prefix-or-discard rule are load-bearing, not stylistic: swapping the
  durable writes to append-then-put yields the `ErrCorruptStore`
  recovery refuses (`crash_recovery_unsafe.qnt`), and trusting a
  non-prefix snapshot silently drops a patch
  (`crash_recovery_unsafe_snapshot.qnt`). Q5/O5b's snapshot-before-ack
  is the same put-before-commit shape.
- **Convergence liveness** (`convergence.tla`, TLC) —
  `<>[]FullyReplicated` holds under weak fairness and fails without it,
  making the fairness requirement mechanical.
- **Frontier reconciliation completeness** (`frontier_reconcile.qnt`) —
  an exhaustive powerset check that advertising the frontier (DAG heads)
  and walking the dependency DAG recovers exactly what full-list
  exchange would, *because* every repo is dependency-closed. This
  settles the completeness half of OQ-1.

The model is design validation, not a substitute for the package's test
battery (rapid state machine, conformance suites, goldens); it pins the
protocol the seams will carry before a wire transport exists to constrain
it. The retention-ledger durability added in the 2026-06-25 update is not
part of this model. `README.md` in that tree is the index.

## Update — 2026-07-04: consumer references redacted

Consumer-identifying details (repository and application names, file
paths) are replaced with generic descriptions per the coding standard's
privacy rule. Decisions and the evidence they rest on are unchanged.

## Open questions

- **OQ-1 — sync at scale.** Full-list exchange is O(history) per
  round; frontier exchange over the dependency DAG or set
  reconciliation (IBLT-family) when histories grow. *(Update
  2026-06-26: the frontier-exchange branch is now formally proved
  complete vs full-list — `frontier_reconcile.qnt`; the
  set-reconciliation / IBLT branch remains open.)*
- **OQ-2 — orphan-envelope GC.** Crash-orphaned envelopes accumulate;
  a mark-and-sweep against the applied closure is mechanical once
  needed.
- **OQ-3 — trust hardening.** Decode limits (size, change count,
  context fan-out) and an envelope signature seam over
  `(Hash, Producer)`; Tier-4 scope.
- **OQ-4 — pebble StorageI implementation.** Deferred; `storagetest`
  makes it a mechanical exercise when a deployment needs it.
- **OQ-5 — history compaction / archival.** Unbounded patch logs need
  a cold-storage story; belongs with OQ-1.
- **OQ-6 — antiquing (ADR-0039).** Without it, false dependencies
  amplify sync traffic at scale; the deferred design dialogue is now
  load-bearing for the distributed roadmap.
- **OQ-7 — fleet-wide erasure across re-cloning.** The durable
  retention ledger (2026-06-25 update) closes only the *same-store*
  case: a fresh clone receives only envelopes + log and has no
  replay-stable basis for an earlier retention stamp, so re-cloning
  resets the fleet's erasure clock. Fleet erasure that survives
  re-cloning is owned by ADR-0025's cooperative-purge layer
  (compensating patches / erasure propagation), with the durable ledger
  as the per-replica primitive it builds on.

## References

- Package documentation: `public/algebraicarch/pushout/pijul/EXPLANATION.md`
  (semantics, invariants, review-remediation history).
- Seam precedent: `public/caching/` (`StashBackendI`,
  `caching/diskbacked/`).
- ADR-0039: antiquing design space.
- ADR-0025: vault-by-design erasure architecture that the
  retention/purge mechanics here support.
- Formal model: `verification/formal/algebraicarch/pushout/` — exchange
  safety, crash-recovery atomicity, convergence liveness, and
  frontier-reconciliation completeness (see Update 2026-06-26).
