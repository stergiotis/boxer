// erasure_dilemma.qnt
//
// Minimal model isolating the erasure-vs-convergence tension that the
// retention sweep creates ONCE THE SYSTEM IS ACTUALLY DISTRIBUTED.
//
// The current Sweep (modelled in pushout_exchange.qnt) destroys the
// in-graggle tombstone content but KEEPS the wire envelope, so it does
// not actually erase the data (envelope still carries the bytes) — but
// convergence is safe, because any gap can be re-shipped.
//
// True GDPR Art.5(1)(e) / FADP Art.6(4) erasure must destroy the CONTENT
// EVERYWHERE, including the envelope. `sweepErase` models that. The
// catch: the exchange protocol re-ships envelopes to close sync gaps, so
// a patch that is erased everywhere can never again be obtained by a node
// that lacks it. If anything depends on it, that node is stuck forever.
//
//   quint run --invariant=EnvelopeAvailable erasure_dilemma.qnt
//
// prints the counterexample. The two specs together are the theorem:
//   ErasureComplete  and  EnvelopeAvailable  cannot both hold.
// That is an ARCHITECTURE decision the protocol must make explicit
// (e.g. per-patch crypto-erasure: keep the envelope, throw away the key).
module erasure_dilemma {
  type Node  = str
  type Patch = int

  pure val Nodes: Set[Node]    = Set("alice", "bob")
  pure val Patches: Set[Patch] = Set(1, 2)
  // patch 2 depends on patch 1 — so a node lacking p1's envelope can
  // never get to a state that includes p2.
  pure val directDeps: Patch -> Set[Patch] = Map(1 -> Set(), 2 -> Set(1))
  pure def depsOf(p: Patch): Set[Patch] = directDeps.get(p)

  var applied: Node -> Set[Patch]
  var seen: Node -> Set[Patch]
  var purged: Node -> Set[Patch]

  action init = all {
    applied' = Nodes.mapBy(n => Set()),
    seen'    = Nodes.mapBy(n => Set()),
    purged'  = Nodes.mapBy(n => Set()),
  }

  action record(n: Node, p: Patch): bool = all {
    not(applied.get(n).contains(p)),
    depsOf(p).subseteq(applied.get(n)),
    applied' = applied.setBy(n, old => old.union(Set(p))),
    seen'    = seen.setBy(n, old => old.union(Set(p))),
    purged'  = purged,
  }

  // real erasure: destroy the envelope too (and mark permanent).
  action sweepErase(n: Node, p: Patch): bool = all {
    applied.get(n).contains(p),
    seen'    = seen.setBy(n, old => old.exclude(Set(p))),
    purged'  = purged.setBy(n, old => old.union(Set(p))),
    applied' = applied,
  }

  action step =
    nondet n = Nodes.oneOf()
    nondet p = Patches.oneOf()
    any {
      record(n, p),
      sweepErase(n, p),
    }

  // Same convergence-safety invariant as the main spec. Under real
  // erasure it is FALSE: a patch can be applied somewhere yet held
  // (as an envelope) nowhere.
  val EnvelopeAvailable =
    Patches.forall(p =>
      (Nodes.exists(n => applied.get(n).contains(p)))
        implies (Nodes.exists(n => seen.get(n).contains(p))))

  // Witness: alice records p1, erases it; p1 is now applied at alice but
  // no envelope survives anywhere -> bob can never converge onto p1 (nor
  // onto p2, which depends on it).
  run erasureBreaksConvergenceTest =
    init
      .then(record("alice", 1))
      .then(sweepErase("alice", 1))
      .expect(not(EnvelopeAvailable))
}
