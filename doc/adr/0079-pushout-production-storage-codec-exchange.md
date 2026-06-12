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
  demo's GUI consumer (`hackathon_2026/src/go/public/pijuldemo`) locks
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
  demonstrators; NATS is already in hackathon_2026's module graph).
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
- **No consumers yet.** hackathon_2026's `pijuldemo` may be transformed
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
transport and the custom codec are implemented in hackathon_2026
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
- hackathon_2026's pijuldemo is rewritten onto `View`/`PatchInfo` —
  sanctioned by "no consumers yet".

### Derived practices

- New invariants and verbs must extend the conformance suites and the
  rapid harness in the same change.
- Engine code never reads wall time or randomness directly (SD8).

## Status

Accepted — 2026-06-12. Implementation phases P1–P7 landed, verified
(full battery incl. race, conformance suites, state-machine soak with
the reopen verb), and pushed; the hackathon_2026 pijuldemo consumes the
engine through the public read API.

## Open questions

- **OQ-1 — sync at scale.** Full-list exchange is O(history) per
  round; frontier exchange over the dependency DAG or set
  reconciliation (IBLT-family) when histories grow.
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

## References

- Package documentation: `public/algebraicarch/pushout/pijul/EXPLANATION.md`
  (semantics, invariants, review-remediation history).
- Seam precedent: `public/caching/` (`StashBackendI`,
  `caching/diskbacked/`).
- ADR-0039 (pebble2impl): antiquing design space.
- ADR-0025 (pebble2impl): vault-by-design erasure architecture that the
  retention/purge mechanics here support.
