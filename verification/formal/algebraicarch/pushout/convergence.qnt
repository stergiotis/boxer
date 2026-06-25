// convergence.qnt
//
// LIVENESS for the boxer pushout exchange protocol (package
// public/algebraicarch/pushout/; this spec mirrors that path under
// verification/formal/). The safety side lives in pushout_exchange.qnt; this
// model answers the dual question: does the protocol actually MAKE PROGRESS?
//
// Claim: under fair scheduling, every recorded patch eventually reaches every
// node, so all repos converge to the same applied set — i.e.
//     <>[] (all nodes hold every recorded patch).
//
// This is a temporal property; it needs FAIRNESS (an enabled sync must not be
// starved forever) and is checked with TLC, not Apalache (whose liveness
// support is impractically slow here). The Quint model is the source; the
// TLA+ for TLC is generated from it (see README "Running -> liveness").
//
// Transport here is RELIABLE (no message loss): loss-/reorder-/dup-tolerance
// is a SAFETY concern, already covered in pushout_exchange.qnt. Mixing an
// adversarial `drop` into a liveness model would require strong fairness and a
// retransmission argument; the honest first liveness result assumes the
// carrier eventually delivers, which is exactly "fair scheduling".
//
// REFINEMENT MAP (spec action -> Go):
//   record  -> repo.Repo.Record           repo/repo.go:230  (authored once at its origin)
//   offer   -> exchange.Push/Pull diff     exchange/exchange.go:102/:59 (recomputed each sync => perpetual re-offer)
//   deliver -> repo.Repo.ApplyEnvelope     repo/repo.go:275  (idempotent, dependency-gated)
module convergence {
  type Node  = str
  type Patch = int

  pure val Nodes: Set[Node]    = Set("a", "b", "c")
  pure val Patches: Set[Patch] = Set(1, 2, 3)
  pure val directDeps: Patch -> Set[Patch] = Map(1 -> Set(), 2 -> Set(1), 3 -> Set(1))
  pure def depsOf(p: Patch): Set[Patch] = directDeps.get(p)
  // each patch is authored once, at a fixed origin node (bounds `record`).
  pure val origin: Patch -> Node = Map(1 -> "a", 2 -> "b", 3 -> "c")

  var applied: Node -> Set[Patch]    // each repo's applied set
  var inflight: Set[(Node, Patch)]   // envelopes addressed to a node (reliable carrier)

  action init = all {
    applied'  = Nodes.mapBy(n => Set()),
    inflight' = Set(),
  }

  // author p at its origin, once, when its deps are already applied there.
  action record(p: Patch): bool = all {
    not(applied.get(origin.get(p)).contains(p)),
    depsOf(p).subseteq(applied.get(origin.get(p))),
    applied'  = applied.set(origin.get(p), applied.get(origin.get(p)).union(Set(p))),
    inflight' = inflight,
  }

  // perpetual re-offer: a sync ships any patch the holder has and the target
  // lacks (Push/Pull recompute the diff every run). Reliable: it stays in
  // flight until delivered.
  action offer(n: Node, m: Node, p: Patch): bool = all {
    n != m,
    applied.get(n).contains(p),
    not(applied.get(m).contains(p)),
    not(inflight.contains((m, p))),
    inflight' = inflight.union(Set((m, p))),
    applied'  = applied,
  }

  // apply an in-flight envelope: idempotent, dependency-gated on the applied
  // set; consumes the envelope.
  action deliver(m: Node, p: Patch): bool = all {
    inflight.contains((m, p)),
    depsOf(p).subseteq(applied.get(m)),
    applied'  = applied.set(m, applied.get(m).union(Set(p))),
    inflight' = inflight.exclude(Set((m, p))),
  }

  action step =
    nondet n = Nodes.oneOf()
    nondet m = Nodes.oneOf()
    nondet p = Patches.oneOf()
    any {
      record(p),
      offer(n, m, p),
      deliver(m, p),
    }

  // ---- predicates --------------------------------------------------
  // every node holds the same applied set
  val converged = Nodes.forall(n => Nodes.forall(m => applied.get(n) == applied.get(m)))
  // every node holds every patch (the strong convergence target)
  val fullyReplicated = Nodes.forall(n => Patches.forall(p => applied.get(n).contains(p)))
  // safety sanity: every repo stays dependency-closed (deliver is gated)
  val DepClosed = Nodes.forall(n => applied.get(n).forall(p => depsOf(p).subseteq(applied.get(n))))

  // ---- liveness ----------------------------------------------------
  val vars = (applied, inflight)

  // weak fairness on every action instance: an action continuously enabled is
  // eventually taken (no enabled sync is starved forever).
  temporal Fairness = and {
    Patches.forall(p => weakFair(record(p), vars)),
    Nodes.forall(n => Nodes.forall(m => Patches.forall(p => weakFair(offer(n, m, p), vars)))),
    Nodes.forall(m => Patches.forall(p => weakFair(deliver(m, p), vars))),
  }

  // THE liveness result: under fairness, the system eventually reaches — and
  // stays at — full replication (hence convergence).
  temporal Convergence = Fairness implies eventually(always(fullyReplicated))

  // ---- bounded liveness WITNESS (runs fast; `quint test`) ----------
  // A concrete fair interleaving that reaches full replication. Note the
  // dependency coupling: b can only record p2 after p1 has propagated to b.
  run convergesTest =
    init
      .then(record(1))
      .then(offer("a", "b", 1)).then(deliver("b", 1))
      .then(offer("a", "c", 1)).then(deliver("c", 1))
      .then(record(2))
      .then(record(3))
      .then(offer("b", "a", 2)).then(deliver("a", 2))
      .then(offer("b", "c", 2)).then(deliver("c", 2))
      .then(offer("c", "a", 3)).then(deliver("a", 3))
      .then(offer("c", "b", 3)).then(deliver("b", 3))
      .expect(fullyReplicated and converged)
}
