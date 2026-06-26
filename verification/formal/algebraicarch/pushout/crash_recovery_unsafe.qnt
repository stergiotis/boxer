// crash_recovery_unsafe.qnt
//
// The counterfactual that justifies the ack-ordering in crash_recovery.qnt.
// Here the two durable writes are SWAPPED: the applied-log entry is appended
// BEFORE the envelope is persisted (append-then-put instead of put-then-
// append). A crash in the window between them leaves the durable log
// referencing a patch whose envelope was never stored — exactly the
// ErrCorruptStore that Open refuses to recover from (repo/repo.go:169-195).
//
//   quint run --invariant=NoCorruption crash_recovery_unsafe.qnt
//
// prints the counterexample. So the put-before-append order in
// commitPatchLocked (repo/repo.go:346 before :358) is not stylistic — it is
// the reason recovery is total.
module crash_recovery_unsafe {
  type Patch = int

  pure val Patches: Set[Patch] = Set(1, 2)
  pure val directDeps: Patch -> Set[Patch] = Map(1 -> Set(), 2 -> Set(1))
  pure def depsOf(p: Patch): Set[Patch] = directDeps.get(p)
  pure def listToSet(l: List[Patch]): Set[Patch] =
    l.foldl(Set(), (acc, x) => acc.union(Set(x)))

  var envStore: Set[Patch]
  var appliedLog: List[Patch]
  var memApplied: Set[Patch]
  var opPatch: Patch
  var opPhase: int
  var corrupt: bool

  action init = all {
    envStore' = Set(), appliedLog' = List(), memApplied' = Set(),
    opPatch' = 0, opPhase' = 0, corrupt' = false,
  }

  action recordBegin(p: Patch): bool = all {
    opPhase == 0,
    not(memApplied.contains(p)),
    depsOf(p).subseteq(memApplied),
    opPatch' = p, opPhase' = 1,
    envStore' = envStore, appliedLog' = appliedLog, memApplied' = memApplied, corrupt' = corrupt,
  }

  // WRONG ORDER: append to the log first ...
  action appendLogFirst = all {
    opPhase == 1,
    appliedLog' = appliedLog.append(opPatch),
    opPhase' = 2,
    envStore' = envStore, memApplied' = memApplied, opPatch' = opPatch, corrupt' = corrupt,
  }

  // ... then persist the envelope.
  action putEnvSecond = all {
    opPhase == 2,
    envStore' = envStore.union(Set(opPatch)),
    opPhase' = 3,
    appliedLog' = appliedLog, memApplied' = memApplied, opPatch' = opPatch, corrupt' = corrupt,
  }

  action memCommit = all {
    opPhase == 3,
    memApplied' = memApplied.union(Set(opPatch)),
    opPhase' = 0, opPatch' = 0,
    envStore' = envStore, appliedLog' = appliedLog, corrupt' = corrupt,
  }

  action crash = {
    val logged = listToSet(appliedLog)
    all {
      corrupt' = not(logged.subseteq(envStore)),
      memApplied' = logged,
      opPhase' = 0, opPatch' = 0,
      envStore' = envStore, appliedLog' = appliedLog,
    }
  }

  action step =
    nondet p = Patches.oneOf()
    any { recordBegin(p), appendLogFirst, putEnvSecond, memCommit, crash }

  // Same headline invariant as the safe model — under this ordering it FAILS.
  val NoCorruption = not(corrupt)
  val LoggedImpliesStored = listToSet(appliedLog).subseteq(envStore)

  // Witness: record p1, append it, crash before persisting its envelope ->
  // the log references a patch with no envelope -> recovery is corrupt.
  run unsafeOrderCorruptsTest =
    init
      .then(recordBegin(1))
      .then(appendLogFirst)
      .then(crash)
      .expect(corrupt and appliedLog == List(1) and envStore == Set())
}
