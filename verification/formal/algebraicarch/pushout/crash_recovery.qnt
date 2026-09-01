// crash_recovery.qnt
//
// Model of the SINGLE-REPO durability / crash-recovery layer of the boxer
// pushout engine (package public/algebraicarch/pushout/; this spec mirrors
// that path under verification/formal/). Go paths below are package-relative.
//
// TWO write verbs, each with its own durable-write ordering and commit point,
// and a crash (power loss) that can strike between ANY two steps:
//
//   Record/ApplyEnvelope (repo.Repo.commitPatchLocked):
//     1. clone+Apply            volatile
//     2. PutEnvelope            DURABLE
//     3. AppendApplied          DURABLE   <= commit point
//     4. in-memory commit       volatile
//
//   Unrecord (repo.Repo.Unrecord):
//     1. clone+Unapply          volatile  (pre-flight: no applied dependent)
//     2. SaveSnapshot(next, newApplied)  DURABLE
//     3. ReplaceApplied(newApplied)      DURABLE   <= commit point
//     4. in-memory commit       volatile
//   The envelope is KEPT (never deleted) so a rolled-back unrecord replays.
//
// On restart repo.Open recovers: load the log; if the snapshot's Applied set
// is COVERED by the log (a SUBSET test — independent patches commute, so the
// pushoutgraph state is a function of the applied set), restore the snapshot
// state and replay only the log entries the snapshot does not hold, in log
// order; otherwise discard the snapshot and replay the whole log from empty.
// A log that lists a patch twice is ErrCorruptStore. The coverage rule is the
// crux for Unrecord: a crash between steps 2 and 3 leaves a snapshot of
// newApplied while the log still holds the old list — newApplied is a subset
// of the old log (generally NOT a prefix: the unrecorded patch may sit in the
// middle), so Open restores the snapshot and replays the unrecorded patch's
// KEPT envelope on top: the unrecord rolls back atomically.
//
// REFINEMENT MAP (spec action -> Go):
//   recordBegin/putEnv/appendLog/memCommit -> repo.Repo.commitPatchLocked
//   unrecordBegin   -> repo.Repo.Unrecord pre-flight + clone+Unapply
//   unrSaveSnapshot -> repo.Repo.saveSnapshotLocked(next, newApplied)
//   unrReplaceLog   -> StorageI.ReplaceApplied (COMMIT POINT)
//   unrMemCommit    -> in-memory commit (tail of repo.Repo.Unrecord)
//   checkpoint      -> repo.Repo.Checkpoint
//   crash           -> power loss + repo.Open recovery (coverage = isSubset)
module crash_recovery {
  type Patch = int

  pure val Patches: Set[Patch] = Set(1, 2, 3)
  // 2 and 3 each depend on 1 (and are independent of each other) so that 2 can
  // be unrecorded from the MIDDLE of [1,2,3] -> a non-prefix snapshot.
  pure val directDeps: Patch -> Set[Patch] = Map(1 -> Set(), 2 -> Set(1), 3 -> Set(1))
  pure def depsOf(p: Patch): Set[Patch] = directDeps.get(p)

  pure def listToSet(l: List[Patch]): Set[Patch] =
    l.foldl(Set(), (acc, x) => acc.union(Set(x)))

  // remove every occurrence of p (= the single one, since the log has no dups);
  // models slices.Delete of the unrecorded patch from the applied list.
  pure def removeAll(l: List[Patch], p: Patch): List[Patch] =
    l.foldl(List(), (acc, x) => if (x == p) acc else acc.append(x))

  // --- Open's recovery, as pure functions of the durable state -------
  // A snapshot is usable only if its applied set is covered by the log
  // (Open's isSubset(snap.Applied, applied)).
  pure def snapUsable(hasS: bool, snapA: List[Patch], log: List[Patch]): bool =
    hasS and listToSet(snapA).subseteq(listToSet(log))
  // The set of patches whose envelopes must be replayed: the log entries the
  // snapshot does not cover when a snapshot is used; the whole log otherwise.
  pure def replayNeedsEnv(hasS: bool, snapA: List[Patch], log: List[Patch]): Set[Patch] =
    if (snapUsable(hasS, snapA, log)) listToSet(log).exclude(listToSet(snapA))
    else listToSet(log)
  // Open rejects a log that lists a patch twice (ErrCorruptStore).
  pure def logHasDup(log: List[Patch]): bool = log.length() != listToSet(log).size()
  // The state Open reconstructs: snapshot base + replayed uncovered entries,
  // or full replay.
  pure def recoveredSet(hasS: bool, snapA: List[Patch], snapS: Set[Patch], log: List[Patch]): Set[Patch] =
    if (snapUsable(hasS, snapA, log)) snapS.union(listToSet(log).exclude(listToSet(snapA)))
    else listToSet(log)

  // ---- Durable storage (survives a crash) --------------------------
  var envStore: Set[Patch]      // PutEnvelope'd, content-addressed; KEPT across unrecord
  var appliedLog: List[Patch]   // AppendApplied / ReplaceApplied, apply order
  var snapApplied: List[Patch]  // snapshot's Applied list
  var snapState: Set[Patch]     // snapshot's pushoutgraph state (abstracted to its applied set)
  var hasSnap: bool             // a snapshot exists

  // ---- Volatile in-memory (lost on crash) --------------------------
  var memApplied: Set[Patch]    // Repo.appliedSet
  var opKind: int               // 0 none, 1 record, 2 unrecord
  var opPhase: int              // step within the in-flight verb (1..3)
  var opPatch: Patch            // the verb's target patch
  var pendingApplied: List[Patch] // unrecord's computed newApplied, carried across its steps
  var pendingState: Set[Patch]    // unrecord's computed post-state

  // ---- Recovery outcome -------------------------------------------
  var corrupt: bool             // recovery would return ErrCorruptStore

  action init = all {
    envStore' = Set(), appliedLog' = List(), snapApplied' = List(),
    snapState' = Set(), hasSnap' = false, memApplied' = Set(),
    opKind' = 0, opPhase' = 0, opPatch' = 0,
    pendingApplied' = List(), pendingState' = Set(), corrupt' = false,
  }

  // ===== RECORD pipeline =====

  action recordBegin(p: Patch): bool = all {
    opKind == 0,
    not(memApplied.contains(p)),
    depsOf(p).subseteq(memApplied),
    opKind' = 1, opPhase' = 1, opPatch' = p,
    envStore' = envStore, appliedLog' = appliedLog, snapApplied' = snapApplied,
    snapState' = snapState, hasSnap' = hasSnap, memApplied' = memApplied,
    pendingApplied' = pendingApplied, pendingState' = pendingState, corrupt' = corrupt,
  }

  action putEnv = all {
    opKind == 1, opPhase == 1,
    envStore' = envStore.union(Set(opPatch)), opPhase' = 2,
    appliedLog' = appliedLog, snapApplied' = snapApplied, snapState' = snapState,
    hasSnap' = hasSnap, memApplied' = memApplied, opKind' = opKind, opPatch' = opPatch,
    pendingApplied' = pendingApplied, pendingState' = pendingState, corrupt' = corrupt,
  }

  action appendLog = all {
    opKind == 1, opPhase == 2,
    appliedLog' = appliedLog.append(opPatch), opPhase' = 3,
    envStore' = envStore, snapApplied' = snapApplied, snapState' = snapState,
    hasSnap' = hasSnap, memApplied' = memApplied, opKind' = opKind, opPatch' = opPatch,
    pendingApplied' = pendingApplied, pendingState' = pendingState, corrupt' = corrupt,
  }

  action memCommit = all {
    opKind == 1, opPhase == 3,
    memApplied' = memApplied.union(Set(opPatch)),
    opKind' = 0, opPhase' = 0, opPatch' = 0,
    envStore' = envStore, appliedLog' = appliedLog, snapApplied' = snapApplied,
    snapState' = snapState, hasSnap' = hasSnap,
    pendingApplied' = pendingApplied, pendingState' = pendingState, corrupt' = corrupt,
  }

  // ===== UNRECORD pipeline =====

  // Pre-flight + clone+Unapply (volatile): refuse if any applied patch depends
  // on p. Compute the post-unrecord applied list and state, carried as pending.
  action unrecordBegin(p: Patch): bool = all {
    opKind == 0,
    memApplied.contains(p),
    not(memApplied.exists(q => depsOf(q).contains(p))),
    opKind' = 2, opPhase' = 1, opPatch' = p,
    pendingApplied' = removeAll(appliedLog, p),
    pendingState'   = memApplied.exclude(Set(p)),
    envStore' = envStore, appliedLog' = appliedLog, snapApplied' = snapApplied,
    snapState' = snapState, hasSnap' = hasSnap, memApplied' = memApplied, corrupt' = corrupt,
  }

  // SaveSnapshot of the post-unrecord state (DURABLE).
  action unrSaveSnapshot = all {
    opKind == 2, opPhase == 1,
    hasSnap' = true, snapApplied' = pendingApplied, snapState' = pendingState, opPhase' = 2,
    envStore' = envStore, appliedLog' = appliedLog, memApplied' = memApplied,
    opKind' = opKind, opPatch' = opPatch,
    pendingApplied' = pendingApplied, pendingState' = pendingState, corrupt' = corrupt,
  }

  // ReplaceApplied with newApplied (DURABLE) -- the unrecord COMMIT POINT.
  action unrReplaceLog = all {
    opKind == 2, opPhase == 2,
    appliedLog' = pendingApplied, opPhase' = 3,
    envStore' = envStore, snapApplied' = snapApplied, snapState' = snapState,
    hasSnap' = hasSnap, memApplied' = memApplied, opKind' = opKind, opPatch' = opPatch,
    pendingApplied' = pendingApplied, pendingState' = pendingState, corrupt' = corrupt,
  }

  // in-memory commit (volatile). Envelope is kept: envStore unchanged.
  action unrMemCommit = all {
    opKind == 2, opPhase == 3,
    memApplied' = pendingState,
    opKind' = 0, opPhase' = 0, opPatch' = 0,
    pendingApplied' = List(), pendingState' = Set(),
    envStore' = envStore, appliedLog' = appliedLog, snapApplied' = snapApplied,
    snapState' = snapState, hasSnap' = hasSnap, corrupt' = corrupt,
  }

  // Checkpoint: snapshot the current stable state (no op in flight).
  action checkpoint = all {
    opKind == 0,
    hasSnap' = true, snapApplied' = appliedLog, snapState' = memApplied,
    envStore' = envStore, appliedLog' = appliedLog, memApplied' = memApplied,
    opKind' = opKind, opPhase' = opPhase, opPatch' = opPatch,
    pendingApplied' = pendingApplied, pendingState' = pendingState, corrupt' = corrupt,
  }

  // crash: power loss wipes volatile state; Open recovers from durable storage.
  action crash = all {
    // (parenthesised: `x' = a or b` would parse as `(x' = a) or b`)
    corrupt'    = (logHasDup(appliedLog)
                   or not(replayNeedsEnv(hasSnap, snapApplied, appliedLog).subseteq(envStore))),
    memApplied' = recoveredSet(hasSnap, snapApplied, snapState, appliedLog),
    opKind' = 0, opPhase' = 0, opPatch' = 0,
    pendingApplied' = List(), pendingState' = Set(),
    envStore' = envStore, appliedLog' = appliedLog, snapApplied' = snapApplied,
    snapState' = snapState, hasSnap' = hasSnap,
  }

  action step =
    nondet p = Patches.oneOf()
    any {
      recordBegin(p), putEnv, appendLog, memCommit,
      unrecordBegin(p), unrSaveSnapshot, unrReplaceLog, unrMemCommit,
      checkpoint, crash,
    }

  // ===== SAFETY INVARIANTS =====

  // Recovery never returns ErrCorruptStore (the log never duplicates, and the
  // kept envelope makes every replay — including an unrecord rollback's
  // replay of the unrecorded patch — total).
  val NoCorruption = not(corrupt)

  // Every logged patch has a durable envelope (put-before-append; unrecord
  // keeps envelopes and only shrinks the log).
  val LoggedImpliesStored = listToSet(appliedLog).subseteq(envStore)

  // The durable log is dependency-closed (unrecord refuses while a dependent
  // is applied, so removing a patch cannot orphan a dependency).
  val LogDepClosed =
    listToSet(appliedLog).forall(p => depsOf(p).subseteq(listToSet(appliedLog)))

  val LogNoDup = appliedLog.length() == listToSet(appliedLog).size()

  // With no op in flight, volatile memory matches the durable log.
  val StableConsistency = (opKind == 0) implies (memApplied == listToSet(appliedLog))

  // A snapshot's stored state always equals its own applied list's set, so the
  // snapshot never lies about the applied set it represents.
  val SnapshotConsistent = hasSnap implies (snapState == listToSet(snapApplied))

  // THE ATOMICITY RESULT: whenever a crash happens, Open reconstructs EXACTLY
  // the state of the durable log — never a half-applied or half-unrecorded
  // mess. This is what coverage-based replay buys (restore a covered snapshot,
  // replay exactly the log entries it lacks); replay the log's positional
  // suffix instead (crash_recovery_unsafe_snapshot.qnt) and this fails.
  val RecoveryCorrect =
    recoveredSet(hasSnap, snapApplied, snapState, appliedLog) == listToSet(appliedLog)

  val Safety = and {
    NoCorruption,
    LoggedImpliesStored,
    LogDepClosed,
    LogNoDup,
    StableConsistency,
    SnapshotConsistent,
    RecoveryCorrect,
  }

  // ===== WITNESS RUNS =====

  // Record crash windows (unchanged behaviour from the record-only model).
  run crashBeforeCommitTest =
    init.then(recordBegin(1)).then(putEnv).then(crash)
      .expect(memApplied == Set() and appliedLog == List() and envStore == Set(1) and not(corrupt))

  run crashAfterCommitTest =
    init.then(recordBegin(1)).then(putEnv).then(appendLog).then(crash)
      .expect(memApplied == Set(1) and appliedLog == List(1) and not(corrupt))

  // Unrecord happy path: 2 is backed out; the log becomes [1].
  run unrecordCommitsTest =
    init
      .then(recordBegin(1)).then(putEnv).then(appendLog).then(memCommit)
      .then(recordBegin(2)).then(putEnv).then(appendLog).then(memCommit)
      .then(unrecordBegin(2)).then(unrSaveSnapshot).then(unrReplaceLog).then(unrMemCommit)
      .expect(memApplied == Set(1) and appliedLog == List(1))

  // ATOMICITY — crash BEFORE ReplaceApplied, unrecording the MIDDLE patch 2.
  // The snapshot's applied list [1,3] is a subset (not a prefix) of the
  // durable log [1,2,3], so Open RESTORES it and replays the one uncovered
  // entry, 2, from its kept envelope: the unrecord rolls back to {1,2,3}.
  run unrecordRollbackOnCrashTest =
    init
      .then(recordBegin(1)).then(putEnv).then(appendLog).then(memCommit)
      .then(recordBegin(2)).then(putEnv).then(appendLog).then(memCommit)
      .then(recordBegin(3)).then(putEnv).then(appendLog).then(memCommit)
      .then(unrecordBegin(2)).then(unrSaveSnapshot)
      .then(crash)
      .expect(memApplied == Set(1, 2, 3) and appliedLog == List(1, 2, 3) and not(corrupt))

  // ATOMICITY — crash AFTER ReplaceApplied: the unrecord is durable; recovery
  // keeps it backed out.
  run unrecordDurableAfterCrashTest =
    init
      .then(recordBegin(1)).then(putEnv).then(appendLog).then(memCommit)
      .then(recordBegin(2)).then(putEnv).then(appendLog).then(memCommit)
      .then(recordBegin(3)).then(putEnv).then(appendLog).then(memCommit)
      .then(unrecordBegin(2)).then(unrSaveSnapshot).then(unrReplaceLog)
      .then(crash)
      .expect(memApplied == Set(1, 3) and appliedLog == List(1, 3) and not(corrupt))
}
