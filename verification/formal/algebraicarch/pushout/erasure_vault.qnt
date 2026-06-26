// erasure_vault.qnt
//
// CONSTRUCTIVE COUNTERPART to erasure_dilemma.qnt. The dilemma shows that
// erasing a patch's CONTENT (its envelope) breaks convergence: once the
// envelope is destroyed, a peer that lacks the patch can never obtain it, so
// EnvelopeAvailable is false. This model shows how vault-by-design (ADR-0025
// Architecture A) DISSOLVES that impossibility — by separating the erasure
// unit from the convergence unit.
//
// Personal-data VALUES live in a controller-side VAULT (a single,
// NON-PROPAGATED store) keyed by a per-occurrence reference. The patch /
// envelope carries ONLY that reference (a carrier token); the value is never
// on the wire. Erasure = forget(ref): delete the vault row. It NEVER touches
// the envelope layer (applied / seen), so there is nothing to propagate and
// no peer to reach — convergence is undisturbed.
//
// THE RESULT: EnvelopeAvailable (convergence-safety) and PersonalDataErased
// (the value is recoverable nowhere) hold SIMULTANEOUSLY in every reachable
// state — the constructive opposite of the dilemma's mutual exclusion.
//
//   quint verify erasure_vault.qnt --invariant=Safe   # NoError: both hold
//
// PER-OCCURRENCE NOTE: `recorded` makes each vault reference authored at most
// once — modelling ADR-0025 SD6's per-occurrence nonce (a value is committed
// under a fresh nonce every time it is recorded). That is exactly the
// property that trades away VALUE-EQUALITY convergence: two identical values
// become two distinct references, so equal values do NOT collapse onto one
// patch the way plain content does. See ADR-0025 SD13.
module erasure_vault {
  type Node  = str
  type Patch = int

  pure val Nodes: Set[Node]    = Set("alice", "bob")
  pure val Patches: Set[Patch] = Set(1, 2)
  // 2 depends on 1, as in the dilemma: a peer lacking 1 cannot reach 2.
  pure val directDeps: Patch -> Set[Patch] = Map(1 -> Set(), 2 -> Set(1))
  pure def depsOf(p: Patch): Set[Patch] = directDeps.get(p)

  var applied:   Node -> Set[Patch]  // patches applied at each node (the DAG/log)
  var seen:      Node -> Set[Patch]  // envelopes (carrier tokens) held & re-shippable
  var vault:     Set[Patch]          // controller-side resolvable VALUES (one ref/patch); NEVER propagated
  var recorded:  Set[Patch]          // refs ever authored (per-occurrence: each authored once)
  var forgotten: Set[Patch]          // refs a forget (erasure) has been issued for

  action init = all {
    applied'   = Nodes.mapBy(n => Set()),
    seen'      = Nodes.mapBy(n => Set()),
    vault'     = Set(),
    recorded'  = Set(),
    forgotten' = Set(),
  }

  // record: author p at n. The envelope carries only a carrier token (p in
  // seen / applied); the VALUE is written to the controller vault. A fresh
  // per-occurrence reference, so authored at most once — and never re-minted
  // under a forgotten ref (its row is gone; a re-record is a NEW occurrence).
  action record(n: Node, p: Patch): bool = all {
    not(recorded.contains(p)),
    depsOf(p).subseteq(applied.get(n)),
    applied'   = applied.setBy(n, old => old.union(Set(p))),
    seen'      = seen.setBy(n, old => old.union(Set(p))),
    vault'     = vault.union(Set(p)),
    recorded'  = recorded.union(Set(p)),
    forgotten' = forgotten,
  }

  // propagate: ship an envelope token n -> m and apply it (offer + deliver).
  // Carries ONLY the token; the value stays controller-side (vault
  // unchanged), even if already forgotten. Dependency-gated on the applied set.
  action propagate(n: Node, m: Node, p: Patch): bool = all {
    n != m,
    seen.get(n).contains(p),
    not(applied.get(m).contains(p)),
    depsOf(p).subseteq(applied.get(m)),
    applied'   = applied.setBy(m, old => old.union(Set(p))),
    seen'      = seen.setBy(m, old => old.union(Set(p))),
    vault'     = vault,
    recorded'  = recorded,
    forgotten' = forgotten,
  }

  // forget: the controller erases a personal-data value (GDPR Art 17 / FADP
  // Art 32(2)(c)). Deletes the vault row ONLY — applied / seen are untouched,
  // so no peer reach is required and convergence is undisturbed. The
  // reference is never re-authored (record's guard), so the value stays gone.
  action forget(p: Patch): bool = all {
    vault.contains(p),
    vault'     = vault.exclude(Set(p)),
    forgotten' = forgotten.union(Set(p)),
    applied'   = applied,
    seen'      = seen,
    recorded'  = recorded,
  }

  // idle: a no-op step. The system can always do nothing (no pending work) —
  // a real behavior, and it keeps the finite 2-patch universe, once fully
  // recorded / propagated / forgotten, from registering as a "deadlock" in
  // the bounded check; the safety properties carry across it unchanged.
  action idle = all {
    applied'   = applied,
    seen'      = seen,
    vault'     = vault,
    recorded'  = recorded,
    forgotten' = forgotten,
  }

  action step =
    nondet n = Nodes.oneOf()
    nondet m = Nodes.oneOf()
    nondet p = Patches.oneOf()
    any {
      record(n, p),
      propagate(n, m, p),
      forget(p),
      idle,
    }

  // CONVERGENCE-SAFETY (identical to the dilemma's invariant): any patch
  // applied somewhere is held as an envelope somewhere. HOLDS here because
  // erasure (forget) never removes anything from `seen` — it has left the
  // envelope layer entirely.
  val EnvelopeAvailable =
    Patches.forall(p =>
      (Nodes.exists(n => applied.get(n).contains(p)))
        implies (Nodes.exists(n => seen.get(n).contains(p))))

  // ERASURE: every forgotten reference's value is resolvable NOWHERE. The
  // vault is the sole holder (envelopes carry only tokens), so deleting a
  // vault row erases the value with a single local, fleet-reach-free action.
  val PersonalDataErased =
    forgotten.forall(p => not(vault.contains(p)))

  // THE CONSTRUCTIVE THEOREM: both hold in every reachable state — the
  // erasure-vs-convergence mutual exclusion of erasure_dilemma.qnt is gone
  // once the erasure unit (a vault row) is separated from the convergence
  // unit (an envelope token). Prove with `quint verify --invariant=Safe`.
  //
  // Why it holds (the induction in words): no action ever removes from
  // `seen`, and record / propagate add to `applied` and `seen` together, so
  // applied is always backed by a held envelope (EnvelopeAvailable). And a
  // forgotten ref is always a recorded ref, so record's `not recorded` guard
  // forbids re-minting it — its vault row never returns (PersonalDataErased).
  val Safe = EnvelopeAvailable and PersonalDataErased

  // Witness: alice records p1 (value -> vault), bob pulls the token and
  // applies it, the controller forgets p1. p1 stays applied & seen at both
  // (convergence intact) yet its value is gone everywhere.
  run vaultErasureKeepsConvergenceTest =
    init
      .then(record("alice", 1))
      .then(propagate("alice", "bob", 1))
      .then(forget(1))
      .expect(EnvelopeAvailable and PersonalDataErased
          and applied.get("bob").contains(1)
          and not(vault.contains(1)))

  // Witness: erase BEFORE a lagging peer syncs. alice records then forgets
  // p1; bob later pulls the (now value-less) token and still converges onto
  // p1 — convergence never needed the value, only the token.
  run forgetThenPropagateTest =
    init
      .then(record("alice", 1))
      .then(forget(1))
      .then(propagate("alice", "bob", 1))
      .expect(applied.get("bob").contains(1)
          and EnvelopeAvailable and PersonalDataErased)
}
