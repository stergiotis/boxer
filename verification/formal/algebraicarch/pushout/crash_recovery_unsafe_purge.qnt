// crash_recovery_unsafe_purge.qnt
//
// COUNTERFACTUAL for ADR-0220's purge ledger: the crash-recovery core with
// NEITHER purge carrier — no retention ledger, no snapshots — which is the
// store shape "snapshots dropped, ledger absent". Every other invariant of
// crash_recovery.qnt still holds (the applied set recovers exactly), and
// PurgeDurable does NOT: a Sweep returns, a crash follows, and recovery
// re-materialises the purged content because replay re-runs the delete.
// The Go engine reports this shape as Guarantees.RetentionDurable == false
// and refuses it under RetentionCompliance.
//
//   quint run crash_recovery_unsafe_purge.qnt --invariant=PurgeDurable
//   -> [violation] expected (see package.json "findings")
module crash_recovery_unsafe_purge {
  import crash_recovery_core(LedgerEnabled = false, SnapshotsEnabled = false).* from "./crash_recovery"

  // The witness: the shortest trace that loses a purge.
  run purgeLostWithoutCarrierTest =
    init
      .then(recordBegin(1)).then(putEnv).then(appendLog).then(memCommit)
      .then(sweep(1))
      .then(crash)
      .expect(memPurged == Set() and memApplied == Set(1))
}
