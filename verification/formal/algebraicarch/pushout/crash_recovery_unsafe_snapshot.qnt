// crash_recovery_unsafe_snapshot.qnt
//
// The counterfactual that justifies Open's snapshot PREFIX check
// (repo/repo.go:147, `isPrefix(snap.Applied, applied)`). Unrecord saves a
// snapshot of the post-unrecord state BEFORE it replaces the log
// (repo/repo.go:380 then :383); a crash in between leaves a snapshot whose
// applied list is generally NOT a prefix of the still-old log. The real Open
// discards such a snapshot and replays the whole log (rolling the unrecord
// back). This model instead TRUSTS any snapshot as a recovery base — and the
// model checker shows it silently drops a patch:
//
//   quint run --invariant=RecoveryCorrect crash_recovery_unsafe_snapshot.qnt
//
// So the prefix check is not an optimization guard — it is a correctness
// requirement.
module crash_recovery_unsafe_snapshot {
  type Patch = int

  pure val Patches: Set[Patch] = Set(1, 2, 3)
  // 2 and 3 each depend on 1 (independent of each other), so 2 can be
  // unrecorded from the middle of [1,2,3].
  pure val directDeps: Patch -> Set[Patch] = Map(1 -> Set(), 2 -> Set(1), 3 -> Set(1))
  pure def depsOf(p: Patch): Set[Patch] = directDeps.get(p)
  pure def listToSet(l: List[Patch]): Set[Patch] =
    l.foldl(Set(), (acc, x) => acc.union(Set(x)))
  pure def removeAll(l: List[Patch], p: Patch): List[Patch] =
    l.foldl(List(), (acc, x) => if (x == p) acc else acc.append(x))

  // UNSAFE recovery: trust an existing snapshot as a base WITHOUT checking that
  // its applied list is a prefix of the log (the omitted check is the bug).
  pure def recoveredUnsafe(hasS: bool, snapA: List[Patch], snapS: Set[Patch], log: List[Patch]): Set[Patch] =
    if (hasS) snapS.union(listToSet(log.slice(snapA.length(), log.length())))
    else listToSet(log)

  var appliedLog: List[Patch]
  var snapApplied: List[Patch]
  var snapState: Set[Patch]
  var hasSnap: bool
  var memApplied: Set[Patch]

  action init = all {
    appliedLog' = List(), snapApplied' = List(), snapState' = Set(),
    hasSnap' = false, memApplied' = Set(),
  }

  // record p fully (crash windows for record are covered in crash_recovery.qnt).
  action recordAtomic(p: Patch): bool = all {
    not(memApplied.contains(p)),
    depsOf(p).subseteq(memApplied),
    appliedLog' = appliedLog.append(p),
    memApplied' = memApplied.union(Set(p)),
    snapApplied' = snapApplied, snapState' = snapState, hasSnap' = hasSnap,
  }

  // The unrecord crash window: snapshot of the post-unrecord state is saved,
  // the log is NOT yet replaced, then a crash recovers via the UNSAFE rule.
  action unrecordSaveThenCrash(p: Patch): bool = all {
    memApplied.contains(p),
    not(memApplied.exists(q => depsOf(q).contains(p))),
    hasSnap' = true,
    snapApplied' = removeAll(appliedLog, p),
    snapState'   = memApplied.exclude(Set(p)),
    appliedLog' = appliedLog,
    memApplied' = recoveredUnsafe(true, removeAll(appliedLog, p), memApplied.exclude(Set(p)), appliedLog),
  }

  action step =
    nondet p = Patches.oneOf()
    any { recordAtomic(p), unrecordSaveThenCrash(p) }

  // Same atomicity property as the safe model — under unsafe recovery it FAILS.
  val RecoveryCorrect =
    recoveredUnsafe(hasSnap, snapApplied, snapState, appliedLog) == listToSet(appliedLog)

  // Witness: log [1,2,3]; the crash window for unrecording the MIDDLE patch 2
  // leaves snapshot [1,3] (not a prefix of [1,2,3]); trusting it recovers
  // {1,3} — patch 2 is silently lost even though the durable log still has it.
  run unsafeSnapshotDropsPatchTest =
    init
      .then(recordAtomic(1)).then(recordAtomic(2)).then(recordAtomic(3))
      .then(unrecordSaveThenCrash(2))
      .expect(memApplied == Set(1, 3) and appliedLog == List(1, 2, 3))
}
