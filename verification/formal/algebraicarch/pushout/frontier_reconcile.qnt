// frontier_reconcile.qnt
//
// Correctness of FRONTIER-BASED reconciliation, the ADR-0079 OQ-1 optimization
// of the exchange protocol (package public/algebraicarch/pushout/; this spec
// mirrors that path under verification/formal/). Go paths are package-relative.
//
// Today exchange.Pull/Push transfer the FULL applied list and diff it
// (exchange/exchange.go:68/111, `minus`): O(history) per round. ADR-0079 OQ-1
// proposes exchanging only the FRONTIER (the DAG heads — patches nothing else
// depends on) and walking the dependency DAG to fetch what's missing, like
// git's have/want negotiation.
//
// The question this model answers: is frontier exchange COMPLETE — does it
// transfer exactly what full-list exchange would, missing nothing? The answer
// is yes, and the reason is precisely the DEPENDENCY-CLOSURE invariant already
// proved for every repo in pushout_exchange.qnt (`DependencyClosure`) and
// crash_recovery.qnt (`LogDepClosed`): on a dependency-closed set, the frontier
// plus the DAG recovers the whole set. So the safety invariant is what licenses
// the optimization. crash/loss are out of scope here (safety/liveness models
// cover them); this is a purely combinatorial correctness result plus a small
// operational wrapper.
//
// REFINEMENT MAP:
//   headsOf       -> the frontier a peer would advertise (vs full Applied())
//   reachDown     -> walking envelope.Dependencies down from advertised heads
//   frontierSync  -> one fetch step of the have/want walk (dependency-gated,
//                    mirroring repo.ApplyEnvelope's gate, repo/repo.go:320)
module frontier_reconcile {
  type Node  = str
  type Patch = int

  pure val Nodes: Set[Node]    = Set("a", "b")
  pure val Patches: Set[Patch] = Set(1, 2, 3, 4)
  // chain 1 <- 2 <- {3, 4}: depth 3, with two heads {3,4} over interior {1,2}.
  pure val directDeps: Patch -> Set[Patch] =
    Map(1 -> Set(), 2 -> Set(1), 3 -> Set(2), 4 -> Set(2))
  pure def depsOf(p: Patch): Set[Patch] = directDeps.get(p)

  pure def isClosed(s: Set[Patch]): bool = s.forall(p => depsOf(p).subseteq(s))

  // the frontier of s: members nothing else in s depends on (the DAG heads).
  pure def headsOf(s: Set[Patch]): Set[Patch] =
    s.filter(p => not(s.exists(q => depsOf(q).contains(p))))

  // one downward step: a set plus the direct deps of its members.
  pure def stepDown(r: Set[Patch]): Set[Patch] =
    r.union(r.fold(Set(), (acc, p) => acc.union(depsOf(p))))
  // dependency-down-closure: iterate stepDown to a fixpoint. |Patches| = 4
  // iterations cover the longest chain (1<-2<-3, depth 3).
  pure def reachDown(h: Set[Patch]): Set[Patch] =
    stepDown(stepDown(stepDown(stepDown(h))))

  // ---- operational model: build closed sets, reconcile via frontier --------
  var applied: Node -> Set[Patch]

  action init = all { applied' = Nodes.mapBy(n => Set()) }

  // author p at node n (dependency-gated, so applied sets stay closed).
  action record(n: Node, p: Patch): bool = all {
    not(applied.get(n).contains(p)),
    depsOf(p).subseteq(applied.get(n)),
    applied' = applied.set(n, applied.get(n).union(Set(p))),
  }

  // one frontier-sync fetch step: b learns a's frontier, walks the DAG
  // (reachDown), and applies one missing patch whose deps it already has.
  // reachDown(headsOf(applied[a])) is what a's *frontier* (not its full list)
  // lets b discover — and equals applied[a] because applied[a] is closed.
  action frontierSync(a: Node, b: Node): bool = {
    val avail = reachDown(headsOf(applied.get(a)))
    val ready = avail.exclude(applied.get(b)).filter(p => depsOf(p).subseteq(applied.get(b)))
    nondet p = ready.oneOf()
    all {
      a != b,
      ready != Set(),
      applied' = applied.set(b, applied.get(b).union(Set(p))),
    }
  }

  action step =
    nondet n = Nodes.oneOf()
    nondet m = Nodes.oneOf()
    nondet p = Patches.oneOf()
    any {
      record(n, p),
      frontierSync(n, m),
    }

  // ===== INVARIANTS =====

  // every repo stays dependency-closed (record + dep-gated sync preserve it).
  val DepClosed = Nodes.forall(n => isClosed(applied.get(n)))

  // THE THEOREM: for every dependency-closed set, the frontier plus the DAG
  // recovers the whole set. This is what makes frontier exchange complete —
  // advertising heads(S) loses nothing versus advertising all of S.
  val FrontierComplete =
    Patches.powerset().forall(s => isClosed(s) implies (reachDown(headsOf(s)) == s))

  // operational form on live repos: each repo's frontier recovers its own
  // applied set, so a frontier sync transfers exactly what a full-list sync
  // would (same missing set = applied[a] \ applied[b]).
  val FrontierEqualsFull =
    Nodes.forall(n => reachDown(headsOf(applied.get(n))) == applied.get(n))

  // the saving: whenever a closed set has any depth (a patch with a dependent),
  // its frontier is strictly smaller than the set.
  val FrontierCompact =
    Patches.powerset().forall(s =>
      (isClosed(s) and s.exists(p => s.exists(q => depsOf(q).contains(p))))
        implies (headsOf(s).size() < s.size()))

  val Safety = and { DepClosed, FrontierComplete, FrontierEqualsFull, FrontierCompact }

  // ===== WITNESS RUNS =====

  // b reconstructs a's whole chain {1,2,3} from a's frontier {3} by walking the
  // dependency DAG (fetching deps before dependents).
  run frontierWalkTest =
    init
      .then(record("a", 1)).then(record("a", 2)).then(record("a", 3))
      .then(frontierSync("a", "b"))  // 1 (only dep-ready member of reachDown({3}))
      .then(frontierSync("a", "b"))  // 2
      .then(frontierSync("a", "b"))  // 3
      .expect(applied.get("b") == Set(1, 2, 3))

  // two diverged repos ({1,2,3} vs {1,2,4}) reconcile by exchanging frontiers
  // ({3} and {4}) and fetching the one missing head each lacks.
  run divergedReconcileTest =
    init
      .then(record("a", 1)).then(record("a", 2)).then(record("a", 3))
      .then(record("b", 1)).then(record("b", 2)).then(record("b", 4))
      .then(frontierSync("a", "b"))  // b gets 3
      .then(frontierSync("b", "a"))  // a gets 4
      .expect(applied.get("a") == Set(1, 2, 3, 4) and applied.get("b") == Set(1, 2, 3, 4))
}
