---
type: adr
status: proposed
date: 2026-05-12
# reviewed-by: "@<handle>"     # fill in and uncomment when flipping to accepted
# reviewed-date: YYYY-MM-DD    # fill in and uncomment when flipping to accepted
---

> **Status: proposed — pre-human-review.** Decision under consideration; do not implement as if accepted.

The decision is deferred pending design dialogue on the subsidiary decisions
(SD1–SD10) and open questions (OQ1–OQ6) below. The engineering recommendation
appears at the end of the Design space section but is not the Decision; the
Decision section is intentionally empty until those resolve.

# ADR-0039: Antiquing Architecture for the Pushout VCS

## Context

The pushout package
([`public/algebraicarch/pushout`](../../../boxer/public/algebraicarch/pushout))
implements a patch-theory version control system in which patches are
content-addressed and propagated peer-to-peer via Push/Pull. The package's
[`public/algebraicarch/pushout/pijul/EXPLANATION.md`](../../../boxer/public/algebraicarch/pushout/pijul/EXPLANATION.md)
adopts the framing of Pijul (and Joe Neeman's `ojo` prototype): files are
objects in a category, merges are categorical pushouts, and patches commute
when they don't depend on each other.

That framing names a specific operation — **antiquing** — that boxer's current
implementation does not perform. Joe Neeman's
[pijul post](https://jneem.github.io/pijul/) introduces it as the rewrite of a
recorded patch `q` into its "most-antique" form `a(q)`, defined as a patch
starting from an earlier state such that the perfect merge of `p` and `a(q)`
reproduces `q`. Repeated antiquing converges (perfect merges are
associative), so every patch has a unique most-antique form, and the
dependencies that the most-antique form still references *are* `q`'s true
dependencies. Patches that have been antiqued past each other are parallel —
applicable in either order — which is the property that makes cherry-pick
across diverged history work.

### Current state of the code

- `patch.NewPatch` in
  [`public/algebraicarch/pushout/graggle/patch/patch.go`](../../../boxer/public/algebraicarch/pushout/graggle/patch/patch.go)
  records whatever the caller hands it.
- `ComputeDependencies` in the same file extracts dependencies as literally
  referenced in change context fields.
- No pass rewrites changes to reduce the dependency set.
- `LineDiff` in
  [`public/algebraicarch/pushout/graggle/patch/diff.go`](../../../boxer/public/algebraicarch/pushout/graggle/patch/diff.go)
  anchors new insertions at the LCS-immediate neighbours. In the linearly
  ordered case the LCS-immediate anchors *are* the minimal anchors, so
  `LineDiff`'s output is incidentally near-antique. This is an accidental
  property of the LCS choice, not a guarantee.
- `changesForResolution`'s `commonAnchors` in
  [`public/algebraicarch/pushout/pijul/pijul_pushout_backend.go`](../../../boxer/public/algebraicarch/pushout/pijul/pijul_pushout_backend.go)
  picks the first live parent and the first live child of a conflict sibling —
  a deterministic but arbitrary choice that can be more conservative than the
  antique form requires.

The gap has not surfaced as a bug in the current demo workflows. It is a
latent semantic divergence from the patch-theory framing the package adopts.

### Why the gap eventually matters

**Original framing (pre-2026-05-17).**
[ADR-0025](./0025-pushout-forget-architecture.md) §SD4 originally named
antiquing as a prerequisite for the compensating-patch construction path of
cooperative-purge erasure (Architecture B and the layered Architecture C): to
construct a compensating patch that overwrites only the affected nodes, the
system needs to know which dependent patches genuinely require the
to-be-forgotten patch's content — which is exactly what the antiqued
dependency set records. [ADR-0027](./0027-pushout-forget-swiss-fadp.md)'s
S2 → S4 upgrade path inherited the same prerequisite.

**Current framing (2026-05-17 onward).**
ADR-0025 selected **Architecture A** (vault by design); ADR-0027 is
superseded by that decision. Architecture A does not construct compensating
patches — the patch DAG is never rewritten — so the dependency-minimisation
output of antiquing is no longer needed as a compliance prerequisite. SD4
and SD12 in ADR-0025 are marked *inapplicable*; this ADR's relationship to
the forget architecture is reduced to: "if a future ADR re-introduces a
compensating-patch path (e.g., scope expansion to legacy PII-bearing
repositories), antiquing becomes a prerequisite again."

What remains live: antiquing is still **desirable** for alignment with the
stated patch-theory framing, and the over-anchoring in `commonAnchors`
(option B in the design space below) is still a real correctness
quality-of-implementation gap that can be tightened independently of the
forget architecture. It is no longer, today, blocking any compliance
workflow, and no user-visible workflow is gated on it.

### What the cited source specifies, and what it doesn't

jneem's post is sufficient to recognise antiquing in hindsight. It is not
sufficient to write the algorithm on our data structures. Specifically:

- **Equivalence semantics.** The post says the merge "involves `q`". Three
  readings are coherent: (a) the resulting graggle state is identical (nodes,
  edges, tombstones, `IntroducedBy` provenance); (b) the rendered output
  matches; (c) the partial order on live content matches. Each admits
  different rewrites.
- **Perfect-merge primitive.** The post borrows the categorical pushout from
  Mimram & Di Giusto 2013. Our code computes pushouts *implicitly* via
  `Apply` + `ResolvePseudoEdges`; we don't expose a "perfect merge of two
  patches" primitive that an antiquing correctness argument can be written
  against.
- **`ChangeKindDeleteNode` and `ChangeKindNewEdge` are unaddressed.** The
  worked examples are line insertions. A `DeleteNode` targeting a `p`-node is
  intrinsically `p`-dependent — but the post doesn't say so.
- **Cycle and order conflicts.** The post predates the
  [cycles post](https://jneem.github.io/cycles/) and the pseudo-edge
  mechanism. Antiquing past a conflicted region is unspecified.
- **NodeID identity.** If `a(q)` has different `UpContext` than `q`, then
  `a(q).Hash ≠ q.Hash` under our `ComputeHash` (structural fields are
  hashed). Pijul handles this because pijul antiques *before* assigning
  identity; our code computes the hash on whatever the caller hands it. The
  rewrite must happen pre-hash or we get identity instability.
- **Uniqueness up to isomorphism vs. uniqueness of representation.** jneem's
  "every patch has a unique most-antique form" is the categorical claim from
  Mimram & Di Giusto, holding at the level of the abstract patch algebra
  (isomorphism classes). Our representation hashes concrete `NodeID`s, so
  two isomorphic antique forms with different `UpContext` / `DownContext`
  picks become different envelopes with different hashes. Uniqueness of the
  *representation* — needed to make `Antique` a deterministic function on
  our data — requires a tie-breaker that the blog post does not specify
  (SD10).

### Canonicity, properly scoped

"Canonical" in this ADR means **record-time canonicity given a fixed input**:
when patch `q` is recorded against graggle state `G`, `most-antique(q, G)`
must return a single deterministic answer. Two implementations of antiquing
agree iff they return the same `[]Change` given the same `(q, G)` pair. This
is the property the rest of the ADR optimises for, and SD9 names it
explicitly.

What this ADR explicitly does *not* try to guarantee:

- **Cross-peer canonicity.** If Alice records "the same edit" against `G_A`
  and Bob records "the same edit" against `G_B` with `G_A ≠ G_B`, the two
  resulting patches will be different envelopes with different hashes.
  This is by design: independently-recorded patches with different ancestor
  sets *are* different patches, and the system merges them as such. Pijul
  does not claim cross-peer canonicity either.
- **Global "oldest" or "earliest" patch ordering.** Patches do not carry a
  global timestamp, and the order in which two peers applied a given pair
  of patches can differ. Wherever an informal reading of "antique" might
  suggest a temporal ordering, the precise concept on our data structure is
  **minimal-dependency-set** with SD10's deterministic tie-breaker.

### Forces the decision must respect

- **Patch identity is load-bearing.** Approaches that change `q.Hash` after
  the patch has been published would orphan downstream references.
  Antiquing must complete before `Hash` is computed.
- **The cooperative-purge prerequisite is genuine.** ADR-0025 SD4 names this
  directly. Implementing antiquing later is fine; pretending it isn't needed
  is not.
- **Pijul's source is read-for-understanding only.** Pijul is GPL-2.0. The
  conceptual lineage (Mimram & Di Giusto 2013; jneem's blog series; reading
  Pijul's code to understand its choices) is fine; verbatim porting is not.
  This matches the existing constraint recorded in
  [`public/algebraicarch/pushout/graggle/NOTICE`](../../../boxer/public/algebraicarch/pushout/graggle/NOTICE).
- **The graggle representation is fixed.** Antiquing must work on the data
  structures we have (NodeID, Edge, EdgeKindE, pseudo-edge bookkeeping,
  tombstone retention). Changes to the graph representation are out of scope.
- **The current near-antique behaviour of `LineDiff` is approximately fine in
  production.** Whatever we build must not regress the linear case.

## Design space (QOC)

**Question.** What antiquing architecture should the pushout package adopt,
given the gaps above and the cooperative-purge prerequisite?

**Options.**

- **A — Status quo + documentation.** Do not implement. Document the gap
  (already done in `pijul/EXPLANATION.md` "Open design questions /
  Antiquing"). Defer until ADR-0025 SD4 is being implemented.

- **B — Conservative pass: tighten `commonAnchors` only.** Replace
  `commonAnchors`'s "first live parent / first live child" pick with the
  anchor (parent / child) that minimises the new patch's dependency set,
  with SD10's deterministic tie-breaker resolving multiple equally-minimal
  candidates. No change to `LineDiff` or `NewPatch`. No new exported
  types. Scope: conflict resolution only.

- **C — Full `Antique(changes, graggle) []Change` pass post-`LineDiff`.** A
  new function in the `patch` package that takes a tentative change-list
  and rewrites each `NewNode` change's `UpContext` / `DownContext` to the
  minimal-dependency-set anchors (SD3 scope), with SD10's deterministic
  tie-breaker. Called between `LineDiff` and `NewPatch`. `commonAnchors`
  becomes a special-case caller of this function.

- **D — Antiquing inside `LineDiff`.** Modify `LineDiff` to pick
  minimal-dependency anchors directly during the diff. Couples antiquing to
  the textual-diff path; conflict resolution (which doesn't go through
  `LineDiff`) still needs separate treatment.

- **E — Antiquing inside `NewPatch`.** Hide the rewrite behind patch
  construction. Matches jneem's framing ("pijul automatically records the
  most antique form"), but `NewPatch` would need a graggle parameter and the
  result of patch construction would be a function of graggle state rather
  than of the inputs alone.

**Criteria.**

- **C1 — Theoretical alignment.** Does it match the patch-theory framing the
  package adopts?
- **C2 — ADR-0025 SD4 prerequisite.** Does it provide the
  dependency-minimisation cooperative-purge compensating-patch construction
  needs?
- **C3 — Implementation surface.** LOC, new types, tests, design effort.
- **C4 — Identity stability.** Preserves `q.Hash = ComputeHash(q.Changes)`
  under rewrite?
- **C5 — Coverage.** Handles `NewNode`, `DeleteNode`, `NewEdge` uniformly?
- **C6 — Conflict-path coverage.** Covers `LineDiff` (linear edits) and
  `changesForResolution` (conflict resolution)?
- **C7 — Backward compatibility.** Leaves envelopes recorded under the
  current behaviour valid?

**Assessment.** `++` strong positive, `+` positive, `−` negative, `−−` strong
negative.

|    | A  | B  | C  | D  | E  |
|----|----|----|----|----|----|
| C1 | −  | +  | ++ | +  | ++ |
| C2 | −  | +  | ++ | +  | +  |
| C3 | ++ | +  | −  | −  | −− |
| C4 | ++ | ++ | ++ | ++ | +  |
| C5 | −  | −  | ++ | +  | ++ |
| C6 | −  | +  | ++ | −  | +  |
| C7 | ++ | ++ | ++ | +  | −  |

**Reading the matrix.** A is the cheapest but doesn't move us forward. B
addresses the worst over-anchoring case (`commonAnchors`) at modest cost;
coverage is limited. C is the most complete but the most code. D couples
antiquing to a single code path. E is structurally the most pijul-like but
the change to `NewPatch`'s signature ripples through callers and the
most-antique form is no longer a pure function of `changes`.

### Engineering recommendation

Staged **B → C**:

- **B first.** Tighten `commonAnchors` so the demo's conflict-resolution path
  stops over-anchoring. Small, localised, no new exported surface. Surfaces
  the right questions (minimal-dep-anchor selection, SD10's tie-breaker
  rule, dependency-closure comparison) on a contained problem.
- **C second.** Once the rewrite rules and equivalence choice are pinned down
  on B, generalise to a full `Antique(changes, graggle)` pass between
  `LineDiff` and `NewPatch`. At that point `commonAnchors` becomes a
  special-case caller of `Antique`.

D and E are not recommended: D's `LineDiff`-only scope is the wrong axis (it
misses conflict resolution); E rewires `NewPatch`'s contract in a way that
ties patch construction to graggle state, which the current API treats as
orthogonal.

The recommendation is provisional. SD1–SD10 below must be resolved before B
can be implemented confidently.

## Decision

*Deferred pending further design dialogue.* The engineering recommendation is
staged B → C with SD1–SD10 and OQ1–OQ6 resolved before B ships. The Decision
section will be completed once those resolve; if a different option is
selected, a follow-up ADR supersedes this one.

## Alternatives

- **Patch-algebra rewrite (Mimram & Di Giusto 2013 directly).** Implement
  antiquing as the categorical-pushout operation from the original paper,
  with formal correctness proofs. Rejected as v1: the formalism is
  well-developed but porting it to our representation (with deletes,
  pseudo-edges, BLAKE3 identity, tombstone retention) is a multi-quarter
  research effort. Kept as a long-term direction for any deployment that
  requires formal verification.

- **Pijul source line-by-line port.** Read Pijul's Rust implementation and
  translate. Rejected: Pijul is GPL-2.0, and verbatim translation creates a
  derivative-work obligation that pushout's licence model does not accept.
  Reading for understanding is allowed and recommended; the line is "no
  copied structure beyond what the blog post already specifies".

- **Lazy antiquing on `Apply`.** Keep recorded patches as-is; antique them at
  `Apply` time when checking whether dependencies are satisfied. Rejected:
  this hides the rewrite from inspection and audit, and the dependency check
  in `Apply` becomes graggle-state-dependent rather than a pure function of
  the envelope.

## Subsidiary design decisions

These decisions become live when option B is started. They are sketched here
so the chosen architecture can be filled in without re-opening structural
questions.

- **SD1 — Equivalence semantics.** Three readings of "reproduces `q`": (a)
  graggle-state equality including `IntroducedBy` provenance, (b)
  rendered-output equality, (c) partial-order-on-live-content equality.
  Recommendation: (c). (a) is too strict to admit any rewrite (changing
  `UpContext` changes provenance); (b) is too weak (it admits rewrites that
  lose audit-relevant structure). (c) matches what the patch-theory framing
  actually cares about and admits the rewrites the blog post motivates.
  Confirm against Mimram & Di Giusto 2013 §3–§4 (OQ1).

- **SD2 — NodeID identity under rewrite.** Antiquing must complete before
  `Patch.Hash` is computed. Two viable shapes: (i) `NewPatch` calls a private
  `antique(changes, graggle)` before `ComputeHash`, with a graggle parameter
  added to `NewPatch`'s signature; (ii) a parallel constructor
  `NewAntiquePatch(graggle, ...)` co-exists with the current `NewPatch`,
  preserving call sites that don't have a graggle (e.g. envelope decoding).
  Recommendation: (ii). It preserves existing call sites and keeps the
  "patch is a function of its inputs" contract for the loader path.

- **SD3 — Per-`ChangeKind` scope.** `NewNode` admits antiquing on
  `UpContext` / `DownContext`. `DeleteNode` targeting a `p`-node is
  `p`-intrinsic — no rewrite possible. `NewEdge` between two patches' nodes
  is intrinsic on the endpoint patches — no rewrite possible. The pass
  therefore operates only on `NewNode` change contexts.

- **SD4 — Pseudo-edge interaction.** Pseudo-edges are computed, not authored.
  Antiquing operates on authored changes only. Pseudo-edges are recomputed
  by `ResolvePseudoEdges` after `Apply`; the antique form should produce the
  same pseudo-edge set as the original. This is a property test (SD7), not a
  code obligation in `Antique`.

- **SD5 — Conflict-region antiquing.** A `NewNode` change inserted into a
  conflicted region (multi-edge / cycle) has `UpContext` / `DownContext`
  picks that depend on the conflict structure. B's `commonAnchors` is
  exactly this case. C must handle it uniformly with the linear case. Open:
  when no further dependency reduction is possible (conflict siblings'
  dependency closures don't shrink under any rewrite), is the resulting
  form still meaningfully "antique" or just "as recorded"? (OQ2.)

- **SD6 — Staging order.** B before C. B's tests pin the minimal-dep-anchor
  selection and SD10's tie-breaker rule on a contained problem; C
  generalises the same routines.

- **SD7 — Test corpus.** Property tests over random graggle + patch
  sequences, asserting `Apply(antiqued) ≡_(c) Apply(original)` under SD1(c)
  and `Dependencies(antiqued) ⊆ Dependencies(original)` with equality only
  when no reduction was possible. A Pijul-comparison corpus (apply the same
  edit sequence in pijul, record the resulting `pijul log` dep set, compare)
  is desirable but adds a runtime dependency on the `pijul` binary; defer to
  a tagged test.

- **SD8 — Relationship to `LineDiff`.** `LineDiff` continues to produce its
  current output (near-antique by LCS accident). `Antique` operates on
  `LineDiff`'s output; in the linear case it should be a no-op (a useful
  invariant to assert in tests).

- **SD9 — Scope of canonicity.** This ADR targets *record-time canonicity
  given a fixed input* (see "Canonicity, properly scoped" in Context).
  `Antique(changes, graggle)` is a pure function: same `(changes, graggle)`
  always returns the same `[]Change`. Cross-peer canonicity is not a goal —
  Alice and Bob recording against different graggle states produce
  different patches by design. This stance is consistent with pijul's own
  semantics and with the rest of the pushout package, where patch identity
  is content-addressed on the recorder's inputs.

- **SD10 — Deterministic tie-breaker.** When multiple anchor candidates
  yield the same minimal dependency set, the antiquing pass must pick one
  deterministically — otherwise two implementations running on the same
  inputs could produce different antique envelopes with different hashes
  for what jneem treats as "the same" antique form. Candidate rules:

  - **(a) Lex-smallest NodeID.** Among equally-minimal-dep anchors, pick
    the `(NodeID.Patch, NodeID.Index)` that compares smallest under
    `types.CompareNodeID`. Cheap, fully deterministic; the choice is
    structurally arbitrary among equivalent candidates.
  - **(b) Longest path to root.** Prefer the anchor at the deepest
    position in the live partial order (i.e. furthest from `RootNodeID`).
    Has "most-specific anchor wins" semantics; ties resolved by a sub-rule
    such as (a).
  - **(c) BFS-from-root traversal order.** First anchor encountered in a
    deterministic BFS from `RootNodeID` over the live subgraph. Depends on
    a stable edge-ordering rule but is implementable.

  Recommendation: **(a)** for v1. It is the cheapest to implement, fully
  deterministic, and the arbitrariness of the choice among equally-minimal
  candidates is not a defect — those candidates are equivalent under
  SD1(c) by construction. (b) and (c) are kept as fallbacks if (a)'s lex
  choice surfaces an observable failure mode (e.g., property tests that
  exhibit non-isomorphic results across rule choices). Pijul presumably
  uses one of these — or another rule entirely — internally; reading for
  understanding is allowed, copying is not (GPL-2.0 caveat noted under
  Forces).

## Open questions

- **OQ1 — Is SD1(c) the equivalence Mimram & Di Giusto use?** Engineering
  judgement is yes, but a careful re-read of §3–§4 of the 2013 paper is
  needed before B is built.
- **OQ2 — What is the right semantics for picking among multiple
  minimal-dependency anchors when conflict siblings' dependency closures
  cannot be shrunk?** Engineering answer: fall back to SD10's tie-breaker
  applied to the recorded form (accept the dependency, but make the pick
  deterministic). A more principled answer may be available from the
  cycles post's treatment of conflict structures.
- **OQ3 — Does antiquing interact with tombstone GC?** A patch whose antique
  form references a node whose content has been purged by `SweepTombstones`
  may be unrecoverable. The retention check in `Patch.Unapply` ("patch is
  permanent past retention") may need to be replicated for antique-form
  lookups.
- **OQ4 — Should `Antique` be a separate exported function or a private
  helper inside `NewAntiquePatch`?** API-surface decision; defer until the
  algorithm is concrete.
- **OQ5 — How does antiquing interact with the envelope codec?** Envelopes
  carry the recorded patch; if antiquing happens at construction time,
  envelopes record the antique form directly. Old envelopes (pre-antiquing)
  remain valid because they were recorded against their original anchors.
  No envelope-schema change is required, only a behaviour change at
  construction. Confirm before B ships.
- **OQ6 — Is SD9's "no cross-peer canonicity" stance consistent with
  ADR-0025 SD4's compensating-patch construction?** Engineering judgement:
  yes — SD4 needs each peer to identify locally which dependents need the
  forgotten content, based on locally-recorded antique forms. It does not
  require two peers to converge on a globally-canonical patch identity for
  "the same" antique form. Confirm with the SD4 author before ADR-0025
  Architecture C is implemented.

## Consequences

### Positive

- Closes the latent semantic gap between the package's stated patch-theory
  framing and its actual behaviour.
- Removes the prerequisite block on ADR-0025 SD4's compensating-patch
  construction. ADR-0025 Architecture C and ADR-0027 S4 both become
  implementable.
- Enables cherry-pick scenarios that today fail on spurious
  missing-dependency errors.
- The conservative B-only stage is small enough to ship without commitment
  to C; if subsequent design dialogue rejects C, B is still a net
  improvement.

### Negative

- A parallel constructor `NewAntiquePatch(graggle, ...)` adds API surface
  and a maintenance obligation to keep its semantics aligned with `NewPatch`.
- Property tests for antiquing are non-trivial. The equivalence under SD1(c)
  is subtle; naive tests may pass under "rendered output equality" (SD1(b))
  but admit rewrites that lose audit-relevant structure.
- The `pijul_text_backend` (the CLI-shelling realisation) does not express
  antiquing because the `pijul` binary does it internally. Behavioural
  parity between the two backends already depends on trusting `pijul`'s
  output; this is a known divergence to record, not a blocker.

### Neutral

- The patch hash semantics (`ComputeHash` = BLAKE3 over the de-fixed-up
  structural skeleton) survives unchanged. NodeIDs of antique-form patches
  differ from naive recordings, but the recordings are different patches;
  identity remains content-addressed.
- The on-disk envelope format does not change. Tombstone GC is unaffected.
- The qc invariants in
  [`public/algebraicarch/pushout/graggle/qc/invariants.go`](../../../boxer/public/algebraicarch/pushout/graggle/qc/invariants.go)
  are unaffected. Antiquing operates pre-`Apply`; qc operates on the
  resulting graggle.

## Status

Proposed — awaiting design dialogue on SD1–SD10 and OQ1–OQ6, then review by a
code owner of [`public/algebraicarch/pushout`](../../../boxer/public/algebraicarch/pushout).

Status lifecycle: `Proposed → Accepted → (Deprecated | Superseded by ADR-XXXX)`.
ADRs are append-only; supersession is recorded, not deleted.

## Updates

### 2026-05-17 — compliance-prerequisite framing dissolved

ADR-0025 selected **Architecture A** (vault by design) for the active
greenfield, multi-actor deployment. Architecture A does not construct
compensating patches — the patch DAG is never rewritten — so the
dependency-minimisation output of antiquing is no longer needed as a
compliance prerequisite. ADR-0025 SD4 and SD12 are marked *inapplicable*;
[ADR-0027](./0027-pushout-forget-swiss-fadp.md) is superseded by the same
decision.

The "Why the gap eventually matters" section has been reframed accordingly.
Antiquing remains a real correctness / theoretical-alignment question for
the pushout package, and the staged B → C engineering recommendation in the
Design space section is unchanged on its own terms. What has changed is the
urgency story: antiquing is no longer gated by a downstream ADR, and no
user-visible workflow is currently blocked on it.

If a future ADR reintroduces a compensating-patch path (for example, scope
expansion to legacy PII-bearing repositories or an Architecture C overlay on
top of A), antiquing becomes a compliance prerequisite again and this
ADR's Decision section should be revisited at that point.

### 2026-05-17 — relocated from boxer

This ADR was previously hosted in the boxer repository as `boxer/doc/adr/0008-pushout-antiquing.md`. It now lives here as ADR-0039 because the decision's motivation — at the time, the compensating-patch construction required by [ADR-0025](./0025-pushout-forget-architecture.md) and [ADR-0027](./0027-pushout-forget-swiss-fadp.md) — was owned by a downstream consumer, even though the pushout package code remains in `../../../boxer/public/algebraicarch/pushout`. Content was unchanged apart from path rewrites; the original proposal date (2026-05-12) is preserved above. The compliance motivation was reframed later the same day (see entry above).

## References

Primary sources:

- Joe Neeman, *Pijul* — introduces antiquing. [Live](https://jneem.github.io/pijul/) / [Wayback (20260220064237)](https://web.archive.org/web/20260220064237/https://jneem.github.io/pijul/)
- Joe Neeman, *Merging* — introduces the graggle model. [Live](https://jneem.github.io/merging/) / [Wayback (20260208054945)](https://web.archive.org/web/20260208054945/https://jneem.github.io/merging/)
- Joe Neeman, *Pseudo-edges across deletions* — bookkeeping for tombstoned regions. [Live](https://jneem.github.io/pseudo/) / [Wayback (20260221144337)](https://web.archive.org/web/20260221144337/https://jneem.github.io/pseudo/)
- Joe Neeman, *Line identifiers for patches*. [Live](https://jneem.github.io/ids/) / [Wayback (20260512103933)](https://web.archive.org/web/20260512103933/https://jneem.github.io/ids/)
- Joe Neeman, *Cycles in merged graggles*. [Live](https://jneem.github.io/cycles/) / [Wayback (20260512104117)](https://web.archive.org/web/20260512104117/https://jneem.github.io/cycles/)
- [Samuel Mimram & Cinzia Di Giusto, *A Categorical Theory of Patches* (2013)](https://arxiv.org/abs/1311.3903) — categorical-pushout grounding for the perfect-merge primitive.

Related ADRs:

- [ADR-0025 — Right-to-Erasure Architecture for the Pushout VCS](./0025-pushout-forget-architecture.md) — SD4 names antiquing as a prerequisite for compensating-patch construction.
- [ADR-0027 — Swiss-Only Forget Architecture for the Pushout VCS](./0027-pushout-forget-swiss-fadp.md) — inherits SD4's antiquing prerequisite via the S2→S4 upgrade path.

In-repo siblings (under `../../../boxer/`):

- [`public/algebraicarch/pushout/pijul/EXPLANATION.md`](../../../boxer/public/algebraicarch/pushout/pijul/EXPLANATION.md) — package overview; the "Open design questions / Antiquing" section is the user-facing explanation that this ADR formalises.
- [`public/algebraicarch/pushout/graggle/patch/patch.go`](../../../boxer/public/algebraicarch/pushout/graggle/patch/patch.go) — patch construction; `NewPatch` and `ComputeDependencies` live here.
- [`public/algebraicarch/pushout/graggle/patch/diff.go`](../../../boxer/public/algebraicarch/pushout/graggle/patch/diff.go) — `LineDiff`.
- [`public/algebraicarch/pushout/pijul/pijul_pushout_backend.go`](../../../boxer/public/algebraicarch/pushout/pijul/pijul_pushout_backend.go) — `commonAnchors` (the B-stage target).
- [`public/algebraicarch/pushout/graggle/NOTICE`](../../../boxer/public/algebraicarch/pushout/graggle/NOTICE) — Pijul/ojo provenance and the GPL-2.0 read-for-understanding constraint.
