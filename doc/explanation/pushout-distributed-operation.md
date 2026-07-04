---
type: explanation
audience: package maintainer, demonstrator author
status: stable
reviewed-by: "@stergiotis"
reviewed-date: 2026-06-12
---

> **Scope.** Synthesis of a design dialogue
> (2026-06-12) about operating `public/algebraicarch/pushout` repos as
> a distributed system. It explains what is already shipped and maps
> the design space for what is deliberately deferred; decisions live in
> [ADR-0079](../adr/0079-pushout-production-storage-codec-exchange.md),
> not here. Sketch-protocol details (§4) summarize external literature
> from memory of the papers cited in §8 and have not been re-verified
> against them; treat constants as order-of-magnitude.

# Distributed operation of pushout repos: time, versions, and synchronization

How do replicas of a patch-theory repo relate to each other when there
is no central coordinator — what orders patches, what a "version" is,
how two versions compare, what a sync must ship, and which delivery
infrastructures fit. The engine packages this document leans on:
`repo` (verbs, recovery, `ApplyEnvelope`), `exchange` (transport seam,
`Pull`/`Push`/`Compare`), `envelope` (wire framing). The pijul demo's
[EXPLANATION](../../public/algebraicarch/pushout/pijul/EXPLANATION.md)
covers the single-process semantics this builds on.

## 1. Shipped vs. design space

Shipped and tested today:

- Dependency-gated, idempotent `repo.ApplyEnvelope` (duplicates return
  `applied=false`, never an error).
- v1 exchange: full applied-list set difference, envelopes shipped in
  the sender's apply order (`exchange.Pull`/`Push`).
- Four-way version comparison (`exchange.Compare`).
- Conformance suites for third-party storage, codec, and transport
  implementations (`repo/storagetest`, `envelope/codectest`,
  `exchange/exchangetest`).

Design space, deferred with reasons recorded in ADR-0079:

- Difference-proportional sync (frontiers, set sketches) — OQ-1.
- A closure-fetch verb for selective patch transfer (§5).
- Broker-backed distribution (the NATS demonstrator, consumer-side;
  boxer itself takes no broker dependency).
- Signatures and decode limits for untrusted fleets — OQ-3.

## 2. Why there is no clock problem

The engine never consults a clock to order anything. Causality is the
dependency DAG: each patch's identity hash covers its canonicalized
dependency set, and apply gates on those dependencies being present.
P depends on Q exactly when P's changes touch context Q introduced —
a happens-before relation that is *tighter* than vector clocks, which
order a patch after everything its author had ever seen rather than
after what it actually reads.

Patches with no dependency path between them are causally independent
by construction, and the pushout property makes them commute: applying
them in either order yields the identical graggle. Commutation is not
a threat to causality; it is what the *absence* of a causal constraint
looks like, made explicit. Consequently peers never need to agree on
an order. The applied log is each repo's local linearization of the
DAG; two cross-pulling peers end up with differently-ordered logs over
the same set and converge anyway (pinned by the exhaustive pairwise
matrix and the state-machine converge oracle in `pijul`).

Even where a total order is needed — rendering — `algo.LinearOrder`
does not break ties: it returns an order only when the topological
sort is structurally unique (every consecutive pair joined by a direct
edge) and otherwise reports a conflict. Systems that resolve
concurrent writes by picking a winner (last-writer-wins) need
synchronized clocks precisely because their merge is order-sensitive;
here genuine ambiguity surfaces as first-class, deterministic conflict
state, and resolution is a new patch depending on both sides. No
semantic decision ever rests on a timestamp, so clock skew cannot
cause divergence.

Wall time keeps exactly two roles, both deliberately harmless under
skew:

- **Envelope timestamps** are provenance claims — display metadata,
  unread by apply/merge, and untrusted until the OQ-3 signature seam
  exists.
- **Retention** (`tombstoneAt`, `repo.Sweep`) is a replica-local
  policy: each replica stamps tombstones with its own clock at its own
  apply time and purges on its own schedule. Skew shifts *when* a
  replica purges, never *what* the converged live state is. The one
  cross-peer effect is capability divergence — a swept replica refuses
  to unrecord the final deleter (`ErrRetentionBlocked`) while an
  unswept one still can — which is intended and test-pinned. Sync
  cannot resurrect purged content (set difference runs over applied
  sets, so an applied patch is never re-shipped).

  Two boundaries to be honest about. First, *durability*: on the same
  store the horizon survives crash/restart. The purge **result** is
  durable (a sweep snapshots before it acks), and the **pending**
  horizon — `tombstoneAt` for a tombstone not yet swept — is backed by a
  replica-local retention ledger that the repo re-seeds at `Open`, so
  full replay no longer resets it
  ([ADR-0079](../adr/0079-pushout-production-storage-codec-exchange.md),
  retention ledger). Second, *scope*: a fresh clone carries full content
  but no ledger, so it starts the horizon at clone time — re-cloning
  resets the fleet's erasure clock. Fleet-wide erasure coordination is
  [ADR-0025](../adr/0025-pushout-forget-architecture.md)'s layer, with
  the durable per-replica ledger + sweep as its primitive.

## 3. Versions and how they compare

A **version** is a set of patch hashes that is downward-closed under
dependencies. Every applied log is one, by the dependency gate. Since
the graggle state is a function of the set (order-free, by
commutation), a version identifies a state.

"More advanced" is set inclusion — a partial order with four outcomes,
computed by `exchange.Compare`:

- `Equal` — same set (possibly reached through different apply
  orders).
- `Ahead` / `Behind` — one side strictly contains the other.
- `Diverged` — each side holds patches the other lacks. This is a
  verdict, not a failure: downward-closed sets form a lattice, the
  intersection is the greatest common ancestor, and the union — the
  join — is always materializable. Sync both ways and the replicas
  converge, with collisions surfacing as conflict state. The
  data-level resolution of divergence is the join, never a clock or
  counter tiebreak. Any total order imposed on diverged versions
  (cardinality, wall-clock labels, epochs) is policy with no data
  semantics.

Practical corollaries:

- `Compare`'s diffs preserve each input's order, so a missing list is
  already dependency-ordered and ready to ship.
- `Unrecord` makes a replica strictly *less* advanced even though it
  acted later in wall time — correct, because the effects are gone.
- A version can be named compactly: its **heads** (patches nothing
  else in the set depends on) determine the downward-closed set, and a
  canonical digest (hash of the sorted hashes) gives O(1) equality.
  "Repo state as of time T" is inherently site-relative; reproducible
  versions are tagged frontiers, not timestamps.

## 4. Discovering a delta: the ladder

Two peers that want to sync first need to learn *what differs*. The
protocol rungs, each degrading to the next:

1. **Digest equality** — O(1); equal digests certify equal sets and
   therefore equal states.
2. **Heads membership** — covers ancestor-shaped pairs (`Ahead`/
   `Behind`, the common case for a replica catching up): send the
   heads, the peer answers membership. O(|heads|).
3. **Set-reconciliation sketch** — the diverged case, communication
   proportional to the difference `d`, not the history `n`.
4. **Full applied lists** — what v1 ships today; the floor every rung
   falls back to, and the overflow recovery for rung 3.

Only rung 4 exists today. Rungs 1–3 are OQ-1, additive on the
transport seam.

### 4.1 The sketch families

- **IBLT** (invertible Bloom lookup table): an array of cells holding
  `(count, keyXOR, checksumXOR)`; each element folds into k cells.
  IBLTs subtract cell-wise — shared elements cancel, leaving a sketch
  of the symmetric difference — and decode by *peeling* pure cells.
  Linear-time decode, scales to huge differences; costs roughly
  1.3–2× `d` in cells and succeeds with high probability rather than
  certainty.
- **Minisketch** (PinSketch/BCH, the Bitcoin Erlay machinery): `c`
  syndromes over GF(2^b). Sketches XOR-combine; when `d ≤ c` decoding
  recovers the difference *exactly and deterministically* at
  information-theoretically optimal size (`c·b` bits, matching the
  Ω(d·b) lower bound). Decode is O(c²), so practical capacities are
  thousands, not millions.
- **Rateless IBLT** (RIBLT): removes the awkward part both share —
  sizing the sketch to an unknown `d` (estimator stages, retry with
  doubling, Erlay's bisection). The sender streams coded symbols until
  the receiver's decode completes. The streaming shape fits a broker
  subject naturally (publish symbols until ack).

Order-of-magnitude intuition for `n` = 1M patches, `d` = 100: full
lists ≈ 32 MB, a Bloom filter of the whole set ≈ 1.2 MB, an IBLT ≈
3 KB, minisketch ≈ 1 KB.

Adversarial caveat: sketches operate on truncated fingerprints (e.g.
64-bit), so an untrusted peer can craft collisions to stall decoding.
Per-session salts mitigate; signed frontiers (OQ-3) and the full-list
fallback are the backstops.

### 4.2 Approximate-membership structures

Bloom/cuckoo filters answer "probably have it" with false positives.
A false positive in a sync protocol is a patch silently never shipped
— a convergence violation — so an AMQ must never be the mechanism that
owns completeness. It earns a place as an optimization in front of an
exact rung: as a difference estimator (sizing a sketch), as a gossip
hint ("roughly what I have" on announcements, where a false positive
merely delays an offer), or Graphene-style — a Bloom filter prunes the
candidate set and a small IBLT repairs exactly the false positives.
The size asymptotics also disqualify it as the primary tool: O(n) bits
versus the sketches' O(d).

### 4.3 What never changes underneath

A sketch is derived, transient state — computable from the applied
set in one pass, maintainable incrementally in O(1) per
`ApplyEnvelope`/`Unrecord` (both families are homomorphic; removal is
the same XOR, so `Unrecord`'s set-shrinking is unproblematic), never
persisted. Patch identity, PXE1 framing, GRG1 snapshots, and
`StorageI` are untouched by any rung. Peers negotiate sketch
parameters (field size, fingerprint truncation, salt) as a protocol
capability; peers that lack it fall back to v1. That is the precise
sense in which difference-proportional sync is downward compatible:
it changes conversations, not data.

## 5. Cherry-picking

Two distinct things hide under the word:

- **Fetch only what I lack.** Already exact in v1: `Pull` computes the
  set difference and ships only missing envelopes. Sketches change the
  *discovery* cost of that step, never its answer.
- **Take patch P without everything else the peer has.** Supported by
  the data model itself: the minimal transfer is P's dependency
  closure minus what the receiver holds, because apply gates on
  dependencies and nothing else. There is no history-prefix welding —
  unlike a git commit, chained to its full ancestry, a patch keeps its
  identity when transplanted (identity = dependencies + changes), so a
  cherry-picked patch is *the same patch*: a later full sync
  recognizes it as already applied (a duplicate, not a conflicting
  twin). Commutation keeps a partial subset mergeable with everyone.

The ordering discipline survives in miniature: `closure(P) \
applied(receiver)` is relatively closed (any dependency of a missing
patch is either already applied or itself missing), so the sender
ships it dependencies-first like any pull. What v1 lacks is only the
verb — "P plus whatever of its closure I lack" — computable
sender-side from `PatchInfo` in one round trip, or receiver-side by
iterative dependency chasing. A modest, additive `exchange` extension
when a demonstrator needs it.

## 6. Two topologies

### 6.1 Trunk over an at-least-once broker (NATS/Kafka)

If all peers follow a shared mainline, publishing every envelope to
one totally-ordered stream is a sound distribution layer, and it works
*today* against the shipped engine:

- **At-least-once is the right delivery class** because apply is
  idempotent by content addressing; redelivery is absorbed silently.
  No exactly-once tier is needed.
- **The stream's total order substitutes for delta negotiation.**
  "Everything after sequence k" is an exact delta query with O(1)
  negotiation state; the consumer's position replaces set
  reconciliation. This is the structural reason OQ-1 can be deferred
  in broker deployments. Stream order is automatically a valid
  linearization of the dependency order: a producer records against
  patches it already applied, which landed earlier in the stream.
- **Keep trunk on one ordered stream** (single partition / one
  subject). Cross-partition delivery can present a dependent before
  its dependency; the gate makes that safe (`ErrMissingDependency`,
  rejection not corruption) but requires park-and-retry. One stream
  makes rejections structurally impossible.
- **Retention bounds bootstrap.** Offset catch-up works while the
  broker retains history; once it ages out, newcomers bootstrap by
  `exchange.Pull` from any caught-up peer (or a snapshot) and then
  join the stream. Mixed channels are safe — the same patch arriving
  via stream and via direct exchange deduplicates.
- **Sentinels map onto consumer semantics**: ack on applied or
  duplicate, park/nak on `ErrMissingDependency`, dead-letter on
  `ErrTampered`/`ErrBadFrame`. Envelopes stay opaque to the broker
  (the PXE1 frame names the codec; mixed-codec fleets interoperate).

Local cherry-picks and local-only patches compose: a peer's version
becomes trunk-prefix ∪ local patches, commutation keeps the local
patches from disturbing trunk application, and such a peer honestly
compares `Ahead` of (or `Diverged` from) pure trunk-followers.

### 6.2 Peer-to-peer mesh

Without a shared log there is no offset, and pairwise difference
discovery becomes the cost center. For resyncing n peers with minimal
communication, difference-proportional reconciliation is necessary in
the strong sense: Ω(d·b) bits is the lower bound and the BCH-sketch
family achieves it. The trunk assumption still helps — peers that
mostly follow a mainline relate as ancestor/descendant and resolve on
the cheap rungs (digest, heads); sketches earn their keep when sets
genuinely diverge on both sides. What gets minimized is the
negotiation; the missing envelopes are an irreducible payload either
way.

## 7. Where the pieces land

- Rungs 1–3, the closure-fetch verb, and broker adapters are all
  additive on the `exchange` seam (`PeerI`/`AcceptorI` plus negotiated
  capabilities); `exchange/exchangetest` grows conformance checks as
  they land, and transports must keep sentinel classification intact
  across the carrier (`errors.Is` on `repo.ErrMissingDependency`).
- Broker transports and custom codecs live with their demonstrators
  (consumer-side); boxer takes no broker dependency.
- IBLT/RIBLT are a few hundred lines of dependency-free Go —
  comfortably self-implementable; minisketch needs GF(2^64)
  Berlekamp–Massey, feasible but real work. If OQ-1 graduates to an
  ADR, the QOC starts at RIBLT-over-broker versus
  minisketch-with-estimator, framed by the ladder of §4.

## 8. References

- [ADR-0079](../adr/0079-pushout-production-storage-codec-exchange.md)
  — engine architecture, seams, OQ-1 (sync at scale), OQ-3 (trust
  hardening), OQ-5 (compaction), OQ-6 (antiquing, which bounds false
  dependencies and therefore sync traffic).
- [ADR-0025](../adr/0025-pushout-forget-architecture.md) — erasure
  coordination above session-local sweep.
- Minsky, Trachtenberg, Zippel: *Set Reconciliation with Nearly
  Optimal Communication Complexity* (IEEE Trans. Inf. Theory, 2003) —
  the characteristic-polynomial method minisketch descends from.
- Goodrich, Mitzenmacher: *Invertible Bloom Lookup Tables* (2011);
  Eppstein, Goodrich, Uyeda, Varghese: *What's the Difference?
  Efficient Set Reconciliation without Prior Context* (SIGCOMM 2011).
- Naumenko et al.: *Erlay: Efficient Transaction Relay for Bitcoin*
  (CCS 2019); BIP-330; `bitcoin-core/minisketch`.
- Ozisik et al.: *Graphene: Efficient Interactive Set Reconciliation
  Applied to Blockchain Propagation* (SIGCOMM 2019) — the AMQ+IBLT
  hybrid of §4.2.
- Yang, Gilad, Alizadeh: *Practical Rateless Set Reconciliation*
  (SIGCOMM 2024) — RIBLT.
- [blockchain-after-the-hype](blockchain-after-the-hype.md) — the
  trust/witness primitive path (signatures → signed frontiers →
  witnesses) that §4.1's adversarial caveat defers to.
