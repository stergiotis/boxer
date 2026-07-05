// versioned_cache_unsafe_lww.qnt
//
// COUNTERFACTUAL: versioned_cache.qnt with the VERSION GATE removed —
// fetch delivery admits whatever it got (last-insert-wins, exactly the
// pre-versioning AddItem semantics). The dirty pin is kept, isolating the
// gate as the broken rule.
//
//   quint run versioned_cache_unsafe_lww.qnt --invariant=MonotoneServe
//
// finds the resurrection race the gate exists to kill: a fetch queued
// before a Commit delivers the older durable row and OVERWRITES the
// freshly written version — a reader observes v2 then v1. This is the
// race the current recordstore integration works around with its
// fetcher-side dirty-guard; the gate replaces that guard by
// construction. DirtyPinnedResident breaks on the same trace (the pinned
// entry no longer carries the newest written version).
module versioned_cache_unsafe_lww {
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

  action write(k: str): bool = all {
    writesLeft > 0,
    written' = written.set(k, written.get(k) + 1),
    l1' = l1.set(k, { present: true, ver: written.get(k) + 1, pinned: true }),
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
    l1' = KEYS.mapBy(k =>
      if (dirty(k))
        { present: l1.get(k).present, ver: l1.get(k).ver, pinned: false }
      else l1.get(k)),
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

  // THE BROKEN RULE: no version gate — delivery overwrites any cached
  // copy (the pin flag survives, as AddItem only replaces the value).
  action fetchReturn(k: str): bool = all {
    fetchq.contains(k),
    fetchq' = fetchq.exclude(Set(k)),
    l1' = if (durable.get(k) > 0)
        l1.set(k, { present: true, ver: durable.get(k), pinned: l1.get(k).pinned })
      else l1,
    l2' = if (durable.get(k) > 0) l2.set(k, emptyEntry) else l2,
    durable' = durable, written' = written,
    served' = served, monotone' = monotone, writesLeft' = writesLeft,
  }

  action demote(k: str): bool = all {
    l1.get(k).present,
    not(l1.get(k).pinned),
    l2' = l2.set(k, { present: true, ver: l1.get(k).ver, pinned: false }),
    l1' = l1.set(k, emptyEntry),
    durable' = durable, written' = written, fetchq' = fetchq,
    served' = served, monotone' = monotone, writesLeft' = writesLeft,
  }

  action dropL1(k: str): bool = all {
    l1.get(k).present,
    not(l1.get(k).pinned),
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

  val DirtyPinnedResident =
    KEYS.forall(k => dirty(k) implies
      (l1.get(k).present and l1.get(k).pinned
        and l1.get(k).ver == written.get(k)))

  // Witness: the resurrection trace. A reader observes version 2, then
  // the raced fetch overwrites it with 1, and the reader observes 1.
  run resurrectionTest =
    init
      .then(write("k1"))
      .then(flushAll)          // durable = 1
      .then(demote("k1"))
      .then(dropL2("k1"))      // cache cold
      .then(readMiss("k1"))    // fetch queued (upstream still at v1)
      .then(write("k1"))       // Commit v2 (dirty, pinned)
      .then(readHitL1("k1"))   // serves 2
      .then(fetchReturn("k1")) // delivers 1 — ADMITTED (no gate)
      .expect(l1.get("k1").ver == 1 and not(DirtyPinnedResident))
      .then(readHitL1("k1"))   // serves 1 after 2
      .expect(not(MonotoneServe))
}
