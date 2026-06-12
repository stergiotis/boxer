---
type: explanation
audience: package maintainer
status: draft
# reviewed-by: "@<handle>"     # fill in and uncomment when flipping to stable
# reviewed-date: YYYY-MM-DD    # fill in and uncomment when flipping to stable
---

> **Status: draft — pre-human-review.** Not verified; do not cite as authoritative.

# Commitments and Zero-Knowledge Proofs

[ADR-0025](../adr/0025-pushout-forget-architecture.md) builds its erasure
architecture on a *commitment scheme* (SD6: the carrier token in patch
content) and rejects a *zero-knowledge proof* layer as an erasure
requirement (Alternative G). This document explains the two notions and
the exact relationship between them, so the distinction the ADR relies on
— "opening a commitment proves the binding; zero-knowledge starts only
when you must prove *without* opening" — does not have to be re-derived.
Everything here is implementation-independent theory; the decisions live
in the ADR.

## The commitment abstraction

A commitment scheme is the cryptographic version of a sealed envelope,
used in two phases:

1. **Commit.** The committer computes `C = Com(m; r)` from a message `m`
   and fresh randomness `r`, and publishes `C`. (`C` goes into the
   immutable store; in ADR-0025, into `Change.Content`.)
2. **Open.** Later, the committer reveals the *opening* `(m, r)`. Anyone
   recomputes `Com(m; r)` and checks it equals `C`.

Two security properties define the scheme, and they protect *opposite*
parties:

- **Hiding** (protects the committer): `C` reveals nothing about `m`.
  Formally, commitments to any two messages are indistinguishable to an
  observer who does not hold `r`.
- **Binding** (protects the verifier): the committer cannot produce two
  openings `(m, r) ≠ (m′, r′)` with `m ≠ m′` for the same `C` — the
  envelope cannot be swapped after sealing.

Each property comes in a *computational* flavour (secure against
polynomially-bounded adversaries, under an assumption) and an
*information-theoretic* flavour (secure against unbounded adversaries).
A scheme cannot be information-theoretically hiding **and**
information-theoretically binding at the same time: if `C` is consistent
with only one message an unbounded adversary can find it (no perfect
hiding), and if `C` is consistent with several an unbounded committer can
equivocate (no perfect binding). Every scheme picks a side.

One consequence matters enough to state on its own: **a deterministic
commitment is never hiding across repeated messages.** If `Com` has no
per-use randomness (or reuses it), equal messages produce equal
commitments, and an observer learns the equality pattern even though no
individual message leaks. This is the theory behind ADR-0025 SD6
kill-reason 1 (per-actor salts leak equality classes) and behind the
general rule that hashing low-entropy personal data is pseudonymisation,
not anonymisation: determinism invites dictionary tests and linkage.

## Constructions

**Hash / PRF based** (what ADR-0025 SD6 specifies). `C = H_r(m)` where
`H_r` is a keyed hash (keyed BLAKE3; HMAC) and the 256-bit key `r` is the
per-occurrence nonce. With `r` random and secret, the output of a PRF is
indistinguishable from random bytes — *computationally hiding*. Binding
rests on collision resistance: equivocating means exhibiting
`(m, r) ≠ (m′, r′)` that collide under `H` — *computationally binding*.
Both properties are computational; both rest on the same primitive the
package already trusts for patch identity.

**Pedersen.** `C = g^m · h^r` in a prime-order group where nobody knows
`log_g(h)`. Because `r` is uniform, `C` is a uniformly random group
element whatever `m` is — *perfectly (information-theoretically) hiding*.
Equivocating yields `log_g(h)` — *computationally binding* under the
discrete-log assumption. Pedersen commitments are additively homomorphic
(`Com(m₁)·Com(m₂) = Com(m₁+m₂; r₁+r₂)`), which is what makes efficient
algebraic proof systems over them possible (see below). The cost is
elliptic-curve machinery and a trusted parameter (`h`).

**Content addressing is half a commitment.** A content hash —
`PatchHash`, a git object id, a Merkle node — is a *binding* commitment
with **no hiding at all**: it is deterministic and keyless, so anyone can
test candidate preimages. This is precisely why personal data inside
hashed patch content is structurally un-erasable (ADR-0025, inventory
item 3): the corpus is already a public, binding, non-hiding commitment
to every byte it contains. SD6's carrier token adds the missing hiding
property — a keyed commitment riding inside an unkeyed one — while
keeping the binding the system depends on. (Merkle trees and polynomial
commitments generalise the same idea to sets, vectors, and polynomials;
they are binding-first structures with hiding added only when needed.)

## What a commitment alone gives you

A commitment binds a party to a value *at a point in time* while keeping
it secret. The only native operation afterwards is **opening**, and
opening is *full disclosure*: the verifier learns `m` and `r`, checks by
recomputation, and is convinced because of binding. ADR-0025's
verification path — fetch `(value, nonce)` from the vault, recompute the
token, compare — is exactly this, an opening. It is interactive
disclosure to a party entitled to see the value. Nothing about it is
zero-knowledge, and nothing about it needs to be.

## Zero-knowledge proofs

A zero-knowledge proof lets a prover convince a verifier that a statement
is true without conveying anything beyond its truth. Three properties
define it:

- **Completeness** — an honest prover with a valid witness convinces the
  verifier.
- **Soundness** — a prover without a valid witness cannot convince the
  verifier (except with negligible probability); in the stronger
  *proof-of-knowledge* form, a convincing prover demonstrably *has* the
  witness.
- **Zero-knowledge** — the verifier learns nothing else: formally, the
  interaction's transcript could have been simulated without the witness
  at all.

Interactive protocols (sigma protocols: commit–challenge–response) become
non-interactive via the Fiat–Shamir transform (derive the challenge from
a hash); general-purpose systems (SNARKs, STARKs, Bulletproofs) prove
arbitrary circuit statements at varying setup/size/verification
trade-offs.

## How the two relate

The relationship runs in both directions, and a duality sits underneath:

1. **Commitments are the building blocks of zero-knowledge proofs.** The
   classic result (Goldreich–Micali–Wigderson) shows every NP statement
   has a zero-knowledge proof *given a commitment scheme*: the prover
   commits to a witness encoding, the verifier challenges, the prover
   opens just the challenged subset. Modern systems are the same shape
   industrialised — sigma protocols prove relations among Pedersen
   commitments; SNARKs commit to polynomials and open them at challenge
   points. "Commit, then prove things about the committed values" is the
   standard paradigm.
2. **The properties are dual.** The commitment's *hiding* is what gives
   the proof its *zero-knowledge* (opened subsets reveal nothing about
   the rest); the commitment's *binding* is what gives the proof its
   *soundness* (the prover cannot adapt committed values to the
   challenge). A weak commitment breaks the proof system at exactly the
   corresponding joint.
3. **A ZKP extends what a commitment can express.** Alone, a commitment
   supports "I knew `m` when I published `C`" — demonstrated by opening,
   i.e. by revealing. A ZKP over the commitment supports statements
   *about* `m` with `m` undisclosed: range ("the committed age is ≥ 18"),
   set membership, equality of two commitments, any predicate in
   principle. This is the selective-disclosure layer ADR-0025
   Alternative G defers: nothing in the erasure architecture needs it.
4. **Opening is not a ZKP.** It is the trivial, everything-revealing
   proof. The zero-knowledge machinery earns its cost only when the
   verifier must *not* learn the value — a confidentiality requirement
   between prover and verifier, which is data minimisation (GDPR
   Art 5(1)(c) / Art 25; FADP Art 7), not erasure.
5. **The construction chosen constrains the proofs available.** Algebraic
   commitments (Pedersen) admit cheap sigma-protocol and Bulletproofs
   statements; hash-based commitments push the same statements into
   general-purpose circuits (proving a keyed-BLAKE3 preimage relation
   inside a SNARK/STARK), which is orders of magnitude heavier and drags
   in a proof-system dependency. This asymmetry is the technical content
   behind Alternative G's "disproportionate to any current consumer": if
   a selective-disclosure layer ever becomes real, the SD6 scheme choice
   would be revisited alongside it.

## The erasure interaction

A proof — zero-knowledge or otherwise — requires a witness. For
statements about a committed value the witness is the opening `(m, r)`.
ADR-0025's `Forget*` destroys the vault row holding both, so after
erasure **nobody can open the commitment and nobody can prove anything
about it**, the controller included. That unprovability is not a defect
of the design; it *is* the anonymisation working (ADR-0025 OQ1): the
binding property survives — the residue still binds to whatever was
committed — but it has gone permanently mute. For a perfectly hiding
scheme the statement is even stronger: a Pedersen residue is consistent
with *every* possible message, carrying literally zero information; the
hash-based residue is silent under computational assumptions (256-bit
keyspace). Conversely, this is why a ZKP cannot *be* an erasure
mechanism: it presupposes the witness still exists. Where post-erasure
provability is genuinely required, the answer is retaining data under a
legal ground (GDPR Art 17(3); ADR-0025 SD3 legal hold), not cryptography.

## References

In-repo:

- [ADR-0025 — Right-to-Erasure Architecture for the Pushout VCS](../adr/0025-pushout-forget-architecture.md) — SD6 (commitment scheme), Alternative G (ZKP layer rejected as requirement), OQ1 (destroyed-nonce residue).
- [Erasure design space](erasure-design-space.md) — where commitment schemes sit among the architectural families.

Literature:

- M. Blum, *Coin Flipping by Telephone* (CRYPTO '81 / SIGACT News 1983) — the original commitment application.
- S. Goldwasser, S. Micali, C. Rackoff, *The Knowledge Complexity of Interactive Proof Systems* (SIAM J. Comput., 1989) — defines zero-knowledge.
- O. Goldreich, S. Micali, A. Wigderson, *Proofs that Yield Nothing But their Validity* (JACM, 1991) — ZK for all of NP from commitments.
- T. Pedersen, *Non-Interactive and Information-Theoretic Secure Verifiable Secret Sharing* (CRYPTO '91) — the Pedersen commitment.
- A. Fiat, A. Shamir, *How to Prove Yourself* (CRYPTO '86) — non-interactivity transform.
- B. Bünz et al., *Bulletproofs: Short Proofs for Confidential Transactions and More* (IEEE S&P 2018) — range proofs over Pedersen commitments.
- D. Boneh, V. Shoup, *A Graduate Course in Applied Cryptography* — commitment and ZK chapters; freely available at <https://toc.cryptobook.us/>.
