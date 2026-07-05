// versioned_cache.qnt
//
// The versioned write-through cache design (2026-07-05 dialogue) for
// public/caching + its recordstore consumer, BEFORE implementation —
// forward-looking design validation in the pushout-spec tradition.
//
// Modelled machine: one single-goroutine ReadThroughCache with two tiers
// (L1 primary / L2 stash), a single writer (the recordstore store: Commit
// assigns monotonically increasing per-key versions, buffers dirty keys,
// Flush publishes them to the upstream), and a read-through fetch queue
// whose delivery returns the upstream's durable version AT FETCH EXECUTION
// (performFetch runs the fetcher synchronously; single upstream server —
// replica-lag delivery is out of scope, noted in the README).
//
// The two rules under proof:
//   1. the VERSION GATE — a fetch result is admitted only if strictly
//      newer than any cached copy (write-through races bounce off);
//   2. the DIRTY PIN — Commit pins the key in L1 until Flush, so a
//      written-but-unflushed version can never be evicted and then
//      shadowed by an older durable refetch.
//
// Eviction is ADVERSARIAL: demote / dropL1 / dropL2 may strike any
// unpinned entry at any time. Every invariant therefore holds for ANY
// eviction policy (SIEVE in L1 today; any stash backend in L2).
//
//   quint run versioned_cache.qnt --invariant=Safety
//
// The counterfactuals versioned_cache_unsafe_lww.qnt (gate removed) and
// versioned_cache_unsafe_nopin.qnt (pin removed) each break MonotoneServe
// — together they show both rules are load-bearing.
module versioned_cache {
  pure val KEYS: Set[str] = Set("k1", "k2")
  pure val MAX_WRITES = 4 // bounds the version domain / state space

  // A cache entry: ver identifies the value (values ARE their versions
  // here); pinned is the sticky dirty-window pin (L1 only).
  type Entry = { present: bool, ver: int, pinned: bool }
  pure val emptyEntry: Entry = { present: false, ver: 0, pinned: false }

  var l1: str -> Entry
  var l2: str -> Entry
  var durable: str -> int // upstream (ClickHouse) newest version, 0 = absent
  var written: str -> int // store's latest assigned version (Commit counter)
  var fetchq: Set[str]    // read-through queue (misses awaiting a flush)
  var served: str -> int  // newest version ever served per key (0 = none)
  var monotone: bool      // trips false if a serve ever regresses
  var writesLeft: int

  // A key is dirty iff a written version has not been flushed yet.
  def dirty(k: str): bool = written.get(k) > durable.get(k)

  // The version a read of k would serve, 0 if uncached.
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

  // --- the write path (recordstore Commit): write-through + pin --------

  // Commit: the store assigns the next version, the cache admits it
  // (necessarily newest — single writer) and pins it until Flush. Any
  // stash copy is superseded (move semantics).
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

  // Flush: publish every dirty key upstream and release its pin.
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

  // --- the read path ---------------------------------------------------

  // serve(k, v) bookkeeping: any serve of a version below an earlier
  // serve of the same key trips the monotone flag (the invariant).
  action readHitL1(k: str): bool = all {
    l1.get(k).present,
    monotone' = (monotone and (l1.get(k).ver >= served.get(k))),
    served' = served.set(k, l1.get(k).ver),
    l1' = l1, l2' = l2, durable' = durable, written' = written,
    fetchq' = fetchq, writesLeft' = writesLeft,
  }

  // A stash hit serves and promotes (GetAndRemove + insert), carrying
  // the version.
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

  // A full miss queues the key for the next batch fetch.
  action readMiss(k: str): bool = all {
    not(l1.get(k).present),
    not(l2.get(k).present),
    not(fetchq.contains(k)),
    fetchq' = fetchq.union(Set(k)),
    l1' = l1, l2' = l2, durable' = durable, written' = written,
    served' = served, monotone' = monotone, writesLeft' = writesLeft,
  }

  // Fetch delivery: the fetcher runs synchronously at flush time and
  // reads the upstream's CURRENT durable version. THE VERSION GATE:
  // admit only strictly newer than any cached copy; a durable version of
  // 0 is "no row" and admits nothing.
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

  // --- adversarial memory pressure (any eviction policy) ---------------

  // Demotion L1 -> L2, carrying the version. Pinned entries are immune.
  action demote(k: str): bool = all {
    l1.get(k).present,
    not(l1.get(k).pinned),
    l2' = l2.set(k, { present: true, ver: l1.get(k).ver, pinned: false }),
    l1' = l1.set(k, emptyEntry),
    durable' = durable, written' = written, fetchq' = fetchq,
    served' = served, monotone' = monotone, writesLeft' = writesLeft,
  }

  // Outright loss from L1 — EVICTION-driven only (spill-drop chains,
  // policy victims). Pinned entries are immune. Explicit invalidation
  // (cache.Delete/Clear) is out of scope here: the implementation lets it
  // override pins as a sanctioned monotonicity reset (see the README).
  action dropL1(k: str): bool = all {
    l1.get(k).present,
    not(l1.get(k).pinned),
    l1' = l1.set(k, emptyEntry),
    l2' = l2, durable' = durable, written' = written, fetchq' = fetchq,
    served' = served, monotone' = monotone, writesLeft' = writesLeft,
  }

  // Stash overflow victim loss.
  action dropL2(k: str): bool = all {
    l2.get(k).present,
    l2' = l2.set(k, emptyEntry),
    l1' = l1, durable' = durable, written' = written, fetchq' = fetchq,
    served' = served, monotone' = monotone, writesLeft' = writesLeft,
  }

  action keyStep =
    nondet k = KEYS.oneOf()
    any {
      write(k),
      readHitL1(k),
      readHitL2(k),
      readMiss(k),
      fetchReturn(k),
      demote(k),
      dropL1(k),
      dropL2(k),
    }

  action step = any { flushAll, keyStep }

  // --- invariants -------------------------------------------------------

  // The headline: a key's served version never regresses (the
  // monotonic-reads session guarantee).
  val MonotoneServe = monotone

  // An entry lives in at most one tier (move semantics).
  val TiersExclusive =
    KEYS.forall(k => not(l1.get(k).present and l2.get(k).present))

  // The dirty window is airtight: an unflushed version is L1-resident,
  // pinned, and exactly the newest written one.
  val DirtyPinnedResident =
    KEYS.forall(k => dirty(k) implies
      (l1.get(k).present and l1.get(k).pinned
        and l1.get(k).ver == written.get(k)))

  // Pins exist only for the dirty window (Flush releases them all).
  val PinnedOnlyDirty =
    KEYS.forall(k => l1.get(k).pinned implies dirty(k))

  // The stash never holds a pin.
  val L2NeverPinned = KEYS.forall(k => not(l2.get(k).pinned))

  // No fabricated versions, cached or served.
  val CachedWasWritten = KEYS.forall(k => cachedVer(k) <= written.get(k))
  val ServedWasWritten = KEYS.forall(k => served.get(k) <= written.get(k))

  // The cache runs ahead of the upstream only inside the dirty window.
  val AheadImpliesDirty =
    KEYS.forall(k => (cachedVer(k) > durable.get(k)) implies dirty(k))

  val Safety = and {
    MonotoneServe,
    TiersExclusive,
    DirtyPinnedResident,
    PinnedOnlyDirty,
    L2NeverPinned,
    CachedWasWritten,
    ServedWasWritten,
    AheadImpliesDirty,
  }

  // --- witnesses ---------------------------------------------------------

  // Write-through: a Commit is readable immediately (no invalidation
  // miss), survives Flush, demotion and stash promotion at the same
  // version.
  run writeThroughReadBackTest =
    init
      .then(write("k1"))
      .then(readHitL1("k1"))
      .then(flushAll)
      .then(demote("k1"))
      .then(readHitL2("k1"))
      .expect(served.get("k1") == 1 and monotone and Safety)

  // The resurrection race is dead: a fetch queued before a Commit
  // delivers the older durable row and bounces off the version gate.
  run racedFetchRejectedTest =
    init
      .then(write("k1"))
      .then(flushAll)          // durable = 1
      .then(demote("k1"))
      .then(dropL2("k1"))      // cache cold
      .then(readMiss("k1"))    // fetch queued
      .then(write("k1"))       // Commit v2 lands first (dirty, pinned)
      .then(readHitL1("k1"))   // serves 2
      .then(fetchReturn("k1")) // delivers durable=1 -> gate rejects
      .then(readHitL1("k1"))   // still serves 2
      .expect(l1.get("k1").ver == 2 and served.get("k1") == 2
        and monotone and Safety)

  // The pin holds the dirty window: after Commit the entry is immune to
  // eviction until Flush releases it.
  run dirtyPinWindowTest =
    init
      .then(write("k1"))
      .expect(DirtyPinnedResident and l1.get("k1").pinned)
      .then(flushAll)
      .expect(not(l1.get("k1").pinned) and Safety)
}
