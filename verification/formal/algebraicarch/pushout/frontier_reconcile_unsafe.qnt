// frontier_reconcile_unsafe.qnt
//
// Two counterfactuals that pin down WHY frontier reconciliation works, each a
// mechanical proof via a failing invariant (run with `quint run --invariant`).
//
//   HeadsOnlySufficient : exchanging only the tips, WITHOUT walking the
//                         dependency DAG, is incomplete -> you must reachDown.
//   FrontierNeedsClosure: frontier completeness requires dependency-closed
//                         sets -> it is the safety invariant that licenses it.
//
// Both are FALSE; `quint run` prints the counterexample subset.
module frontier_reconcile_unsafe {
  type Patch = int

  pure val Patches: Set[Patch] = Set(1, 2, 3, 4)
  pure val directDeps: Patch -> Set[Patch] =
    Map(1 -> Set(), 2 -> Set(1), 3 -> Set(2), 4 -> Set(2))
  pure def depsOf(p: Patch): Set[Patch] = directDeps.get(p)
  pure def isClosed(s: Set[Patch]): bool = s.forall(p => depsOf(p).subseteq(s))
  pure def headsOf(s: Set[Patch]): Set[Patch] =
    s.filter(p => not(s.exists(q => depsOf(q).contains(p))))
  pure def stepDown(r: Set[Patch]): Set[Patch] =
    r.union(r.fold(Set(), (acc, p) => acc.union(depsOf(p))))
  pure def reachDown(h: Set[Patch]): Set[Patch] =
    stepDown(stepDown(stepDown(stepDown(h))))

  var dummy: int
  action init = all { dummy' = 0 }
  action step = all { dummy' = dummy }

  // FALSE: heads alone do not recover the set — a peer that ships only its tips
  // and does NOT let the receiver walk down the deps drops every interior patch
  // (e.g. s = {1,2}, headsOf = {2} != {1,2}).
  val HeadsOnlySufficient =
    Patches.powerset().forall(s => isClosed(s) implies (headsOf(s) == s))

  // FALSE: drop the closure precondition and frontier completeness breaks — for
  // a non-closed s (e.g. {1,3}, which omits 2) the walk reaches deps that are
  // not actually in s, so reachDown(headsOf(s)) != s. Dependency closure (the
  // exchange/crash-recovery safety invariant) is exactly the precondition.
  val FrontierNeedsClosure =
    Patches.powerset().forall(s => reachDown(headsOf(s)) == s)
}
