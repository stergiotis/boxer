// crash_recovery.qnt
//
// Model of the SINGLE-REPO durability / crash-recovery layer of the boxer
// pushout engine (package public/algebraicarch/pushout/; this spec mirrors
// that path under verification/formal/). Go paths below are package-relative.
//
// The engine's write verbs commit through a fixed "ack ordering"
// (commitPatchLocked, repo/repo.go:309):
//     1. g.Clone(); p.Apply(clone)     -- volatile, on a clone
//     2. st.PutEnvelope(h, framed)     -- DURABLE (filestore: temp+fsync+rename)
//     3. st.AppendApplied(h)           -- DURABLE (O_APPEND+fsync)   <= COMMIT POINT
//     4. g = clone; appliedSet[h] = {} -- volatile in-memory commit
// A crash (power loss) can strike between any two steps; volatile state is
// lost. On restart, Open (repo/repo.go:109) recovers: LoadApplied, and if a
// snapshot's Applied list is a PREFIX of the log, restore it and replay only
// the suffix envelopes, else replay everything from empty. Replay refuses to
// open if a logged patch has no envelope / identity mismatch / a dependency
// that does not precede it (ErrCorruptStore, repo/repo.go:170-191).
//
// THE POINT: recovery is safe *because* the envelope (step 2) is durable
// BEFORE the log entry (step 3). So "logged => stored" is an invariant, and
// replay never starves. crash_recovery_unsafe.qnt swaps the two writes and
// the model checker exhibits the ErrCorruptStore that results.
//
// REFINEMENT MAP (spec action -> Go):
//   recordBegin -> commitPatchLocked clone+Apply   repo/repo.go:310-314
//   putEnv      -> st.PutEnvelope                   repo/repo.go:315
//   appendLog   -> st.AppendApplied (COMMIT POINT)  repo/repo.go:318
//   memCommit   -> in-memory commit                 repo/repo.go:321-326
//   checkpoint  -> repo.Repo.Checkpoint             repo/repo.go:430
//   crash       -> power loss + repo.Open recovery  repo/repo.go:109
module crash_recovery {
  type Patch = int

  pure val Patches: Set[Patch] = Set(1, 2, 3)
  // 2 and 3 depend on 1; 1 is a root.
  pure val directDeps: Patch -> Set[Patch] = Map(1 -> Set(), 2 -> Set(1), 3 -> Set(1))
  pure def depsOf(p: Patch): Set[Patch] = directDeps.get(p)

  pure def listToSet(l: List[Patch]): Set[Patch] =
    l.foldl(Set(), (acc, x) => acc.union(Set(x)))

  // pre is a prefix of full  (Open's isPrefix, repo/repo.go:209)
  pure def isPrefix(pre: List[Patch], full: List[Patch]): bool =
    pre.length() <= full.length() and full.slice(0, pre.length()) == pre

  // ---- Durable storage (survives a crash) --------------------------
  var envStore: Set[Patch]      // PutEnvelope'd, content-addressed (HasEnvelope)
  var appliedLog: List[Patch]   // AppendApplied / ReplaceApplied, apply order
  var snapApplied: List[Patch]  // snapshot's Applied prefix
  var hasSnap: bool             // a snapshot exists

  // ---- Volatile in-memory (lost on crash) --------------------------
  var memApplied: Set[Patch]    // Repo.appliedSet
  var opPatch: Patch            // patch being committed (0 = none in flight)
  var opPhase: int              // 0 none, 1 cloneApplied, 2 envPut, 3 logAppended

  // ---- Recovery outcome -------------------------------------------
  var corrupt: bool             // recovery would return ErrCorruptStore

  action init = all {
    envStore'    = Set(),
    appliedLog'  = List(),
    snapApplied' = List(),
    hasSnap'     = false,
    memApplied'  = Set(),
    opPatch'     = 0,
    opPhase'     = 0,
    corrupt'     = false,
  }

  // step 1: apply on a clone (volatile). deps must already be applied.
  action recordBegin(p: Patch): bool = all {
    opPhase == 0,
    not(memApplied.contains(p)),
    depsOf(p).subseteq(memApplied),
    opPatch'     = p,
    opPhase'     = 1,
    envStore' = envStore, appliedLog' = appliedLog, snapApplied' = snapApplied,
    hasSnap' = hasSnap, memApplied' = memApplied, corrupt' = corrupt,
  }

  // step 2: persist the envelope (DURABLE).
  action putEnv = all {
    opPhase == 1,
    envStore'    = envStore.union(Set(opPatch)),
    opPhase'     = 2,
    appliedLog' = appliedLog, snapApplied' = snapApplied, hasSnap' = hasSnap,
    memApplied' = memApplied, opPatch' = opPatch, corrupt' = corrupt,
  }

  // step 3: append to the applied log (DURABLE) -- the COMMIT POINT.
  action appendLog = all {
    opPhase == 2,
    appliedLog'  = appliedLog.append(opPatch),
    opPhase'     = 3,
    envStore' = envStore, snapApplied' = snapApplied, hasSnap' = hasSnap,
    memApplied' = memApplied, opPatch' = opPatch, corrupt' = corrupt,
  }

  // step 4: in-memory commit (volatile).
  action memCommit = all {
    opPhase == 3,
    memApplied'  = memApplied.union(Set(opPatch)),
    opPhase'     = 0,
    opPatch'     = 0,
    envStore' = envStore, appliedLog' = appliedLog, snapApplied' = snapApplied,
    hasSnap' = hasSnap, corrupt' = corrupt,
  }

  // Checkpoint: snapshot the current stable durable state (no op in flight).
  action checkpoint = all {
    opPhase == 0,
    hasSnap'     = true,
    snapApplied' = appliedLog,
    envStore' = envStore, appliedLog' = appliedLog, memApplied' = memApplied,
    opPatch' = opPatch, opPhase' = opPhase, corrupt' = corrupt,
  }

  // crash: power loss wipes volatile state; Open recovers from durable
  // storage. If a snapshot is a prefix of the log, only the suffix envelopes
  // are replayed; otherwise the snapshot is discarded and the whole log is
  // replayed. Recovery fails (corrupt) iff a patch that must be replayed has
  // no stored envelope.
  action crash = {
    val logged   = listToSet(appliedLog)
    val useSnap  = hasSnap and isPrefix(snapApplied, appliedLog)
    val toReplay = if (useSnap) listToSet(appliedLog.slice(snapApplied.length(), appliedLog.length()))
                   else logged
    all {
      corrupt'     = not(toReplay.subseteq(envStore)),
      memApplied'  = logged,
      opPhase'     = 0,
      opPatch'     = 0,
      envStore' = envStore, appliedLog' = appliedLog,
      snapApplied' = snapApplied, hasSnap' = hasSnap,
    }
  }

  action step =
    nondet p = Patches.oneOf()
    any {
      recordBegin(p),
      putEnv,
      appendLog,
      memCommit,
      checkpoint,
      crash,
    }

  // ===== SAFETY INVARIANTS =====

  // Recovery never returns ErrCorruptStore. THE headline crash-safety result.
  val NoCorruption = not(corrupt)

  // Every logged patch has a durable envelope — the ack-ordering guarantee
  // (put before append) that MAKES recovery safe.
  val LoggedImpliesStored = listToSet(appliedLog).subseteq(envStore)

  // The durable log is dependency-closed (deps precede dependents).
  val LogDepClosed =
    listToSet(appliedLog).forall(p => depsOf(p).subseteq(listToSet(appliedLog)))

  // The log never contains a patch twice (idempotent apply / no double-commit).
  val LogNoDup = appliedLog.length() == listToSet(appliedLog).size()

  // With no operation in flight, the volatile set matches the durable log.
  val StableConsistency = (opPhase == 0) implies (memApplied == listToSet(appliedLog))

  // A snapshot is only ever a real prefix of the log's state (so Open's
  // prefix check never restores a snapshot that disagrees with the log).
  val SnapshotIsPrefix = hasSnap implies isPrefix(snapApplied, appliedLog)

  val Safety = and {
    NoCorruption,
    LoggedImpliesStored,
    LogDepClosed,
    LogNoDup,
    StableConsistency,
    SnapshotIsPrefix,
  }

  // ===== WITNESS RUNS =====

  // Crash AFTER PutEnvelope, BEFORE AppendApplied: the patch did not commit.
  // Recovery yields the empty log; the envelope is a harmless orphan.
  run crashBeforeCommitTest =
    init
      .then(recordBegin(1))
      .then(putEnv)
      .then(crash)
      .expect(memApplied == Set() and appliedLog == List() and envStore == Set(1)
          and not(corrupt))

  // Crash AFTER AppendApplied, BEFORE the in-memory commit: the patch IS
  // durable. Recovery brings it back even though memCommit never ran.
  run crashAfterCommitTest =
    init
      .then(recordBegin(1))
      .then(putEnv)
      .then(appendLog)
      .then(crash)
      .expect(memApplied == Set(1) and appliedLog == List(1) and not(corrupt))

  // Snapshot-accelerated recovery: a snapshot prefix [1] plus a replayed
  // suffix [2] reconstructs {1,2}.
  run snapshotRecoveryTest =
    init
      .then(recordBegin(1)).then(putEnv).then(appendLog).then(memCommit)
      .then(checkpoint)
      .then(recordBegin(2)).then(putEnv).then(appendLog).then(memCommit)
      .then(crash)
      .expect(memApplied == Set(1, 2) and not(corrupt))
}
