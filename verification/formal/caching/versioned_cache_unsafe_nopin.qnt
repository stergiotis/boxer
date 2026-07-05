// versioned_cache_unsafe_nopin.qnt
//
// COUNTERFACTUAL: versioned_cache.qnt with the DIRTY PIN removed —
// Commit inserts unpinned, so eviction can take a written-but-unflushed
// entry. The version gate is kept, isolating the pin as the broken rule.
//
//   quint run versioned_cache_unsafe_nopin.qnt --invariant=MonotoneServe
//
// finds the regression the pin exists to close: the dirty entry is
// evicted, the refetch delivers the OLDER durable row, and — the cache
// being cold for the key — the version gate has nothing to compare
// against and admits it. A reader observes v2 then v1. Together with
// versioned_cache_unsafe_lww.qnt this shows the gate alone is NOT
// sufficient: monotonicity needs gate + pin.
module versioned_cache_unsafe_nopin {
  pure val KEYS: Set[str] = Set("k1", "k2")
  pure val MAX_WRITES = 4

  type Entry = { present: bool, ver: int, pinned: bool }
  pure val emptyEntry: Entry = { present: false, ver: 0, pinned: false }

  var l1: str -> Entry
  var l2: str -> Entry
  var durable: str -> int
  var written: str -> int
  var fetchq: Set[str]
  var served: str -> int
  var monotone: bool
  var writesLeft: int

  def dirty(k: str): bool = written.get(k) > durable.get(k)

  def cachedVer(k: str): int =
    if (l1.get(k).present) l1.get(k).ver
    else if (l2.get(k).present) l2.get(k).ver
    else 0

  action init = all {
    l1' = KEYS.mapBy(k => emptyEntry),
    l2' = KEYS.mapBy(k => emptyEntry),
    durable' = KEYS.mapBy(k => 0),
    written' = KEYS.mapBy(k => 0),
    fetchq' = Set(),
    served' = KEYS.mapBy(k => 0),
    monotone' = true,
    writesLeft' = MAX_WRITES,
  }

  // THE BROKEN RULE: Commit does not pin.
  action write(k: str): bool = all {
    writesLeft > 0,
    written' = written.set(k, written.get(k) + 1),
    l1' = l1.set(k, { present: true, ver: written.get(k) + 1, pinned: false }),
    l2' = l2.set(k, emptyEntry),
    durable' = durable,
    fetchq' = fetchq,
    served' = served,
    monotone' = monotone,
    writesLeft' = writesLeft - 1,
  }

  action flushAll = all {
    KEYS.exists(k => dirty(k)),
    durable' = KEYS.mapBy(k => written.get(k)),
    l1' = l1,
    l2' = l2,
    written' = written,
    fetchq' = fetchq,
    served' = served,
    monotone' = monotone,
    writesLeft' = writesLeft,
  }

  action readHitL1(k: str): bool = all {
    l1.get(k).present,
    monotone' = (monotone and (l1.get(k).ver >= served.get(k))),
    served' = served.set(k, l1.get(k).ver),
    l1' = l1, l2' = l2, durable' = durable, written' = written,
    fetchq' = fetchq, writesLeft' = writesLeft,
  }

  action readHitL2(k: str): bool = all {
    not(l1.get(k).present),
    l2.get(k).present,
    monotone' = (monotone and (l2.get(k).ver >= served.get(k))),
    served' = served.set(k, l2.get(k).ver),
    l1' = l1.set(k, { present: true, ver: l2.get(k).ver, pinned: false }),
    l2' = l2.set(k, emptyEntry),
    durable' = durable, written' = written,
    fetchq' = fetchq, writesLeft' = writesLeft,
  }

  action readMiss(k: str): bool = all {
    not(l1.get(k).present),
    not(l2.get(k).present),
    not(fetchq.contains(k)),
    fetchq' = fetchq.union(Set(k)),
    l1' = l1, l2' = l2, durable' = durable, written' = written,
    served' = served, monotone' = monotone, writesLeft' = writesLeft,
  }

  // The version gate, exactly as in the safe spec.
  action fetchReturn(k: str): bool = all {
    fetchq.contains(k),
    fetchq' = fetchq.exclude(Set(k)),
    l1' = if (durable.get(k) > cachedVer(k) and durable.get(k) > 0)
        l1.set(k, { present: true, ver: durable.get(k), pinned: false })
      else l1,
    l2' = if (durable.get(k) > cachedVer(k) and durable.get(k) > 0)
        l2.set(k, emptyEntry)
      else l2,
    durable' = durable, written' = written,
    served' = served, monotone' = monotone, writesLeft' = writesLeft,
  }

  // With no pins, adversarial eviction can take anything.
  action demote(k: str): bool = all {
    l1.get(k).present,
    l2' = l2.set(k, { present: true, ver: l1.get(k).ver, pinned: false }),
    l1' = l1.set(k, emptyEntry),
    durable' = durable, written' = written, fetchq' = fetchq,
    served' = served, monotone' = monotone, writesLeft' = writesLeft,
  }

  action dropL1(k: str): bool = all {
    l1.get(k).present,
    l1' = l1.set(k, emptyEntry),
    l2' = l2, durable' = durable, written' = written, fetchq' = fetchq,
    served' = served, monotone' = monotone, writesLeft' = writesLeft,
  }

  action dropL2(k: str): bool = all {
    l2.get(k).present,
    l2' = l2.set(k, emptyEntry),
    l1' = l1, durable' = durable, written' = written, fetchq' = fetchq,
    served' = served, monotone' = monotone, writesLeft' = writesLeft,
  }

  action keyStep =
    nondet k = KEYS.oneOf()
    any {
      write(k), readHitL1(k), readHitL2(k), readMiss(k),
      fetchReturn(k), demote(k), dropL1(k), dropL2(k),
    }

  action step = any { flushAll, keyStep }

  val MonotoneServe = monotone

  // Witness: the dirty-eviction regression. The gate admits the older
  // durable row because nothing is left in the cache to compare against.
  run dirtyEvictionRegressionTest =
    init
      .then(write("k1"))       // v1
      .then(flushAll)          // durable = 1
      .then(write("k1"))       // v2 — dirty, but NOT pinned
      .then(readHitL1("k1"))   // serves 2
      .then(dropL1("k1"))      // eviction takes the dirty entry
      .then(readMiss("k1"))    // refetch queued
      .then(fetchReturn("k1")) // delivers durable=1; cache cold -> admitted
      .then(readHitL1("k1"))   // serves 1 after 2
      .expect(not(MonotoneServe))
}
