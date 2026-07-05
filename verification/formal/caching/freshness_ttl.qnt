// freshness_ttl.qnt
//
// The WithFreshnessTTL half of the versioned-cache design (2026-07-05
// dialogue): age-based staleness onset for stale-while-revalidate.
// Orthogonal to versions/pins (versioned_cache.qnt), so this model keeps
// only what freshness needs: one entry per key with an age stamp and a
// stale flag, two tiers (the stamp must SURVIVE demotion — the same
// tier-boundary lesson the stale flag taught during the 2026-07-04
// remediation), a logical clock, and revalidation.
//
// The oracle trick: `truth` records each key's real last-refresh time
// independently of the stamp the cache carries. A read served as fresh
// while `clock - truth > TTL` is a freshness lie; the safe spec never
// lies because stamp == truth in every reachable state (StampFaithful).
//
//   quint run freshness_ttl.qnt --invariant=Safety
//
// The counterfactual freshness_ttl_unsafe.qnt drops the stamp at
// demotion (a stash record without the age field) and re-stamps at
// promotion — the modelled failure mode for forgetting the field in the
// stash envelope. It serves entries as fresh long past their TTL.
module freshness_ttl {
  pure val KEYS: Set[str] = Set("k1", "k2")
  pure val TTL = 2
  pure val CLOCK_MAX = 6

  type Slot = { present: bool, inL1: bool, stamp: int, stale: bool }
  pure val emptySlot: Slot = { present: false, inL1: true, stamp: 0, stale: false }

  var cache: str -> Slot
  var truth: str -> int // ground truth: the key's real last-refresh time
  var clock: int
  var honest: bool      // trips false on a fresh serve past the true TTL

  // What the implementation's read path would decide from carried state.
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

  // Fresh data lands (write-through Commit or a newer fetch delivery):
  // stamped now, truth refreshed.
  action admit(k: str): bool = all {
    cache' = cache.set(k, { present: true, inL1: true, stamp: clock, stale: false }),
    truth' = truth.set(k, clock),
    clock' = clock, honest' = honest,
  }

  // Equal-version revalidation (the admission rule's `==` outcome): the
  // upstream confirmed the cached value is still current — freshness
  // restarts without replacing the value.
  action confirm(k: str): bool = all {
    cache.get(k).present,
    cache' = cache.set(k, {
      present: true, inL1: cache.get(k).inL1, stamp: clock, stale: false }),
    truth' = truth.set(k, clock),
    clock' = clock, honest' = honest,
  }

  // The external-writer signal.
  action markStale(k: str): bool = all {
    cache.get(k).present,
    cache' = cache.set(k, {
      present: true, inL1: cache.get(k).inL1,
      stamp: cache.get(k).stamp, stale: true }),
    truth' = truth, clock' = clock, honest' = honest,
  }

  // Demotion carries the stamp (the stash envelope stores the age field).
  action demote(k: str): bool = all {
    cache.get(k).present,
    cache.get(k).inL1,
    cache' = cache.set(k, {
      present: true, inL1: false,
      stamp: cache.get(k).stamp, stale: cache.get(k).stale }),
    truth' = truth, clock' = clock, honest' = honest,
  }

  // Promotion carries it back.
  action promote(k: str): bool = all {
    cache.get(k).present,
    not(cache.get(k).inL1),
    cache' = cache.set(k, {
      present: true, inL1: true,
      stamp: cache.get(k).stamp, stale: cache.get(k).stale }),
    truth' = truth, clock' = clock, honest' = honest,
  }

  // Adversarial loss from either tier.
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

  // A read served as fresh: honest only if the key's REAL age is within
  // the TTL.
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

  // --- invariants -------------------------------------------------------

  // No fresh serve past the key's true age.
  val FreshnessHonest = honest

  // The carried stamp never diverges from the truth — the inductive
  // reason FreshnessHonest holds.
  val StampFaithful =
    KEYS.forall(k => cache.get(k).present implies
      (cache.get(k).stamp == truth.get(k)))

  val Safety = FreshnessHonest and StampFaithful

  // --- witnesses ---------------------------------------------------------

  // SWR lifecycle: fresh data ages out, becomes unservable-as-fresh, and
  // an equal-version revalidation restores freshness legitimately.
  run swrRevalidationTest =
    init
      .then(admit("k1"))
      .then(tick).then(tick).then(tick)
      .expect(not(canServeFresh("k1"))) // aged out (3 > TTL)
      .then(confirm("k1"))              // upstream: still current
      .then(readFresh("k1"))
      .expect(FreshnessHonest and Safety)

  // The stamp survives the tier round-trip: a demoted-then-promoted
  // entry is exactly as old as it really is.
  run demotionCarriesAgeTest =
    init
      .then(admit("k1"))
      .then(demote("k1"))
      .then(tick).then(tick).then(tick)
      .then(promote("k1"))
      .expect(not(canServeFresh("k1")) and StampFaithful)
}
