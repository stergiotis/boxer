// pushout_exchange.qnt
//
// Abstract model of the boxer pushout *exchange protocol*
//   github.com/stergiotis/boxer/public/algebraicarch/pushout
//
// SCOPE: the DISTRIBUTED layer — many repos shipping patches to one
// another under loss / reordering / duplication / crash-driven PARTIAL
// sync, plus local record / unrecord / retention-sweep.
//
// The merge ALGEBRA (graggle pushout: commutativity, associativity,
// apply/unapply inverse) is ASSUMED correct — it is property-tested in
// graggle/store/property_test.go and guarded by the 14 invariants in
// graggle/qc/invariants.go. That assumption is exactly what lets us
// abstract a repo's state to its applied *set* of patch ids: order does
// not matter, so convergence reduces to set equality.
//
// This spec lives at verification/formal/algebraicarch/pushout/, mirroring
// the Go package path public/algebraicarch/pushout/. Go paths below are
// relative to that package root.
//
// REFINEMENT MAP (spec action -> Go symbol):
//   record       -> repo.Repo.Record         repo/repo.go:230
//   offer        -> exchange.Push / Pull ship exchange/exchange.go:102 / :59
//   deliverApply -> repo.Repo.ApplyEnvelope  repo/repo.go:275 (idempotent, dep-gated)
//   unrecord     -> repo.Repo.Unrecord       repo/repo.go:339
//   sweep        -> repo.Repo.Sweep          repo/repo.go:406 (keeps envelope)
//   drop         -> carrier loses a framed envelope (PeerI/AcceptorI are
//                   best-effort; no single Go symbol)
module pushout_exchange {
  // ---- Patch universe: a fixed acyclic dependency DAG --------------
  type Node  = str
  type Patch = int

  pure val Nodes: Set[Node]    = Set("server", "alice", "bob")
  pure val Patches: Set[Patch] = Set(1, 2, 3, 4)

  // directDeps(p): the patches p declares as dependencies — exactly the
  // envelope.Patch.Dependencies list that ApplyEnvelope gates on.
  // 3 depends on 1; 4 depends on 1 and 2; 1,2 are roots.
  pure val directDeps: Patch -> Set[Patch] =
    Map(1 -> Set(), 2 -> Set(), 3 -> Set(1), 4 -> Set(1, 2))

  pure def depsOf(p: Patch): Set[Patch] = directDeps.get(p)

  // ---- State -------------------------------------------------------
  var applied: Node -> Set[Patch]   // patches in n's applied log (set; order abstracted)
  var seen: Node -> Set[Patch]      // envelopes n holds & can re-ship (kept across unrecord)
  var purged: Node -> Set[Patch]    // patches made retention-permanent at n (Sweep)
  var inflight: Set[(Node, Patch)]  // framed envelopes addressed to a node, in the carrier

  action init = all {
    applied'  = Nodes.mapBy(n => Set()),
    seen'     = Nodes.mapBy(n => Set()),
    purged'   = Nodes.mapBy(n => Set()),
    inflight' = Set(),
  }

  // record: author p locally; its declared deps must already be applied
  // (Record computes deps from referenced, hence applied, nodes).
  action record(n: Node, p: Patch): bool = all {
    not(applied.get(n).contains(p)),
    depsOf(p).subseteq(applied.get(n)),
    applied'  = applied.setBy(n, old => old.union(Set(p))),
    seen'     = seen.setBy(n, old => old.union(Set(p))),
    purged'   = purged,
    inflight' = inflight,
  }

  // offer: n ships an envelope it holds toward m (Push/Pull transfer).
  action offer(n: Node, m: Node, p: Patch): bool = all {
    n != m,
    seen.get(n).contains(p),
    inflight' = inflight.union(Set((m, p))),
    applied'  = applied,
    seen'     = seen,
    purged'   = purged,
  }

  // deliverApply: m applies an inflight envelope. IDEMPOTENT (re-apply of
  // an already-applied patch is a no-op = ApplyEnvelope applied=false) and
  // DEPENDENCY-GATED on the APPLIED set ("merely having seen an envelope
  // is not enough", repo.go:272). A dep-missing envelope is simply not
  // enabled here — it waits in flight until its deps are applied.
  action deliverApply(m: Node, p: Patch): bool = all {
    inflight.contains((m, p)),
    depsOf(p).subseteq(applied.get(m)),
    applied'  = applied.setBy(m, old => old.union(Set(p))),
    seen'     = seen.setBy(m, old => old.union(Set(p))),
    purged'   = purged,
    inflight' = inflight.exclude(Set((m, p))),
  }

  // drop: the carrier loses an envelope (best-effort transport).
  action drop(m: Node, p: Patch): bool = all {
    inflight.contains((m, p)),
    inflight' = inflight.exclude(Set((m, p))),
    applied'  = applied,
    seen'     = seen,
    purged'   = purged,
  }

  // unrecord: back out p locally. Refused if any applied patch depends on
  // p (ErrDependentExists) or p was made permanent (ErrRetentionBlocked).
  // Envelope is KEPT (seen unchanged) so a later pull reapplies cleanly.
  action unrecord(n: Node, p: Patch): bool = all {
    applied.get(n).contains(p),
    not(purged.get(n).contains(p)),
    not(applied.get(n).exists(q => depsOf(q).contains(p))),
    applied'  = applied.setBy(n, old => old.exclude(Set(p))),
    seen'     = seen,
    purged'   = purged,
    inflight' = inflight,
  }

  // sweep: retention purge. The current Sweep destroys tombstone CONTENT
  // (the patch becomes permanent: unrecord refuses it) but KEEPS the
  // envelope for re-ship (repo.go:406; Unrecord docstring repo.go:334).
  action sweep(n: Node, p: Patch): bool = all {
    applied.get(n).contains(p),
    not(purged.get(n).contains(p)),
    purged'   = purged.setBy(n, old => old.union(Set(p))),
    applied'  = applied,
    seen'     = seen,
    inflight' = inflight,
  }

  action step =
    nondet n = Nodes.oneOf()
    nondet m = Nodes.oneOf()
    nondet p = Patches.oneOf()
    any {
      record(n, p),
      offer(n, m, p),
      deliverApply(n, p),
      drop(n, p),
      unrecord(n, p),
      sweep(n, p),
    }

  // ===== SAFETY INVARIANTS (must hold; prove with `quint verify`) =====

  // Every applied patch's declared deps are also applied — each repo is
  // dependency-closed. This is BOTH the engine's core log invariant AND
  // the PARTIAL-SYNC safety property: because Push/Pull stop on the first
  // error (exchange.go), a repo can be left holding a *prefix* of a sync;
  // that prefix must still be dependency-closed. deliverApply's gate is
  // what guarantees it.
  val DependencyClosure =
    Nodes.forall(n =>
      applied.get(n).forall(p => depsOf(p).subseteq(applied.get(n))))

  // You can always re-ship anything you've applied (envelopes retained).
  val AppliedSubsetSeen =
    Nodes.forall(n => applied.get(n).subseteq(seen.get(n)))

  // Purge only ever marks applied patches, and a purged patch stays
  // applied — it is permanent (unrecord refuses it).
  val PurgedSubsetApplied =
    Nodes.forall(n => purged.get(n).subseteq(applied.get(n)))

  // CONVERGENCE-SAFETY: any patch applied anywhere can still be obtained
  // by anyone (some node holds its envelope). With the current
  // keep-the-envelope Sweep this HOLDS — re-sync can always close a gap.
  val EnvelopeAvailable =
    Patches.forall(p =>
      (Nodes.exists(n => applied.get(n).contains(p)))
        implies (Nodes.exists(n => seen.get(n).contains(p))))

  // Conjunction of the safety invariants — the single property to prove
  // with `quint verify --invariant=Safety`.
  val Safety = and {
    DependencyClosure,
    AppliedSubsetSeen,
    PurgedSubsetApplied,
    EnvelopeAvailable,
  }

  // ===== A FALSIFIABLE CLAIM (find a counterexample with `quint run`) ==
  // "The current Sweep erases purged content everywhere." It does NOT —
  // the envelope is kept, so this is VIOLATED:
  //   quint run --invariant=ErasureComplete pushout_exchange.qnt
  // exhibits the trace. That counterexample IS the erasure side of the
  // erasure-vs-convergence finding (see erasure_dilemma.qnt for the
  // other horn).
  val ErasureComplete =
    Nodes.forall(n =>
      purged.get(n).forall(p =>
        Nodes.forall(m => not(seen.get(m).contains(p)))))

  // ===== WITNESS RUNS (executable scenarios; `quint test`) ============

  // Concurrent independent edits converge to the same applied set.
  run convergeTest =
    init
      .then(record("alice", 1))
      .then(record("bob", 2))
      .then(offer("alice", "bob", 1))
      .then(deliverApply("bob", 1))
      .then(offer("bob", "alice", 2))
      .then(deliverApply("alice", 2))
      .expect(applied.get("alice") == Set(1, 2)
          and applied.get("bob")   == Set(1, 2))

  // Dependency gating: an envelope shipped out of order WAITS. p3 (dep p1)
  // is offered to bob before p1; bob can only apply p3 AFTER p1 is applied.
  // Reordering the last two .then() lines makes this run fail by
  // construction — that gate is the partial-sync safety net.
  run gatingTest =
    init
      .then(record("alice", 1))
      .then(record("alice", 3))
      .then(offer("alice", "bob", 3))
      .then(offer("alice", "bob", 1))
      .then(deliverApply("bob", 1))
      .then(deliverApply("bob", 3))
      .expect(applied.get("bob") == Set(1, 3))

  // Idempotent redelivery: shipping + delivering p1 twice is a no-op the
  // second time (ApplyEnvelope returns applied=false).
  run idempotentTest =
    init
      .then(record("alice", 1))
      .then(offer("alice", "bob", 1))
      .then(deliverApply("bob", 1))
      .then(offer("alice", "bob", 1))
      .then(deliverApply("bob", 1))
      .expect(applied.get("bob") == Set(1))
}
