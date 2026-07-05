// freshness_ttl_unsafe.qnt
//
// COUNTERFACTUAL: freshness_ttl.qnt where the stash envelope has NO age
// field — demotion drops the stamp and promotion re-stamps at the
// current clock, so a round-trip through the stash makes an entry look
// freshly refreshed.
//
//   quint run freshness_ttl_unsafe.qnt --invariant=FreshnessHonest
//
// finds the freshness lie: an entry admitted long ago is demoted, ages
// past its TTL in the stash, is promoted (re-stamped "now"), and a read
// serves it as fresh. The mechanical proof that the age stamp must
// travel through StashBackendI — the same tier-boundary law the stale
// flag obeys since the 2026-07-04 remediation, and the version since
// versioned_cache.qnt.
module freshness_ttl_unsafe {
  pure val KEYS: Set[str] = Set("k1", "k2")
  pure val TTL = 2
  pure val CLOCK_MAX = 6

  type Slot = { present: bool, inL1: bool, stamp: int, stale: bool }
  pure val emptySlot: Slot = { present: false, inL1: true, stamp: 0, stale: false }

  var cache: str -> Slot
  var truth: str -> int
  var clock: int
  var honest: bool

  def canServeFresh(k: str): bool = and {
    cache.get(k).present,
    not(cache.get(k).stale),
    clock - cache.get(k).stamp <= TTL,
  }

  action init = all {
    cache' = KEYS.mapBy(k => emptySlot),
    truth' = KEYS.mapBy(k => 0),
    clock' = 0,
    honest' = true,
  }

  action admit(k: str): bool = all {
    cache' = cache.set(k, { present: true, inL1: true, stamp: clock, stale: false }),
    truth' = truth.set(k, clock),
    clock' = clock, honest' = honest,
  }

  action confirm(k: str): bool = all {
    cache.get(k).present,
    cache' = cache.set(k, {
      present: true, inL1: cache.get(k).inL1, stamp: clock, stale: false }),
    truth' = truth.set(k, clock),
    clock' = clock, honest' = honest,
  }

  action markStale(k: str): bool = all {
    cache.get(k).present,
    cache' = cache.set(k, {
      present: true, inL1: cache.get(k).inL1,
      stamp: cache.get(k).stamp, stale: true }),
    truth' = truth, clock' = clock, honest' = honest,
  }

  // THE BROKEN RULE, half 1: the stash record has no age field — the
  // stamp is lost at demotion.
  action demote(k: str): bool = all {
    cache.get(k).present,
    cache.get(k).inL1,
    cache' = cache.set(k, {
      present: true, inL1: false, stamp: 0, stale: cache.get(k).stale }),
    truth' = truth, clock' = clock, honest' = honest,
  }

  // THE BROKEN RULE, half 2: promotion re-inserts through the normal
  // insert path, stamping the entry as if it were fresh NOW.
  action promote(k: str): bool = all {
    cache.get(k).present,
    not(cache.get(k).inL1),
    cache' = cache.set(k, {
      present: true, inL1: true, stamp: clock, stale: cache.get(k).stale }),
    truth' = truth, clock' = clock, honest' = honest,
  }

  action drop(k: str): bool = all {
    cache.get(k).present,
    cache' = cache.set(k, emptySlot),
    truth' = truth, clock' = clock, honest' = honest,
  }

  action tick = all {
    clock < CLOCK_MAX,
    clock' = clock + 1,
    cache' = cache, truth' = truth, honest' = honest,
  }

  action readFresh(k: str): bool = all {
    canServeFresh(k),
    honest' = (honest and (clock - truth.get(k) <= TTL)),
    cache' = cache, truth' = truth, clock' = clock,
  }

  action step =
    any {
      tick,
      {
        nondet k = KEYS.oneOf()
        any {
          admit(k), confirm(k), markStale(k),
          demote(k), promote(k), drop(k), readFresh(k),
        }
      },
    }

  val FreshnessHonest = honest

  val StampFaithful =
    KEYS.forall(k => cache.get(k).present implies
      (cache.get(k).stamp == truth.get(k)))

  // Witness: the freshness lie. True age 3 > TTL 2, yet the re-stamped
  // entry reads as fresh.
  run stashRoundTripLiesTest =
    init
      .then(admit("k1"))               // truth = 0
      .then(demote("k1"))              // stamp lost
      .then(tick).then(tick).then(tick) // clock = 3, true age 3 > TTL
      .then(promote("k1"))             // re-stamped at 3: looks new
      .expect(canServeFresh("k1") and not(StampFaithful))
      .then(readFresh("k1"))           // fresh serve of an expired entry
      .expect(not(FreshnessHonest))
}
