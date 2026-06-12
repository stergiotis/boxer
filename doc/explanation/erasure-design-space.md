---
type: explanation
audience: package maintainer
status: draft
# reviewed-by: "@<handle>"     # fill in and uncomment when flipping to stable
# reviewed-date: YYYY-MM-DD    # fill in and uncomment when flipping to stable
---

> **Status: draft — pre-human-review.** Not verified; do not cite as authoritative.

# Erasure Design Space for Append-Only, Content-Addressed Stores

This document defines the vocabulary shared by
[ADR-0025](../adr/0025-pushout-forget-architecture.md) (GDPR-scope erasure
architecture for the pushout VCS) and
[ADR-0027](../adr/0027-pushout-forget-swiss-fadp.md) (the superseded
FADP-scope variant): the two-store decomposition, the three operations,
the architectural families (a)/(b)/(c1)/(c2)/(d) with their secret-scope
axis, and the propagation modes. It is taxonomy, not decision record —
the decisions, criteria, and legal analysis live in the ADRs. The
properties described here hold for any store in which records are
content-addressed, immutable once written, and copied to peers the
controller does not operate (patch-theory VCSes, blockchains, append-only
event logs).

## The two-store decomposition

Every architecture in this space splits state into:

- **Store 1 — the propagated corpus.** Content-addressed, immutable,
  identity-bearing (here: patch envelopes and the graggle state derived
  from them). Mutating a record in place breaks the identity of the
  record and of everything downstream that hashed it. After propagation,
  copies exist on systems the controller does not control; no operation
  on the controller's store 1 reaches them.
- **Store 2 — the local mutable store.** Controller-scoped, never
  propagated, freely mutable (here: the vault, a sidecar, or a
  keystore). Erasure operations act here.

The design question every family answers differently: which bytes go to
which store, and what residue store 1 carries once a store-2 erasure has
run. An architecture is only as good as the post-erasure status of that
residue (anonymous vs. pseudonymous vs. personal data). That status is
assessed per holder: both Swiss law (Zurich HG190107-O) and, since CJEU
*EDPS v SRB* (C-413/23 P, 2025), EU law take a recipient-perspective
relative approach, so store-1 content can be non-personal in peers'
hands while remaining personal in the controller's (ADR-0025 legal
landscape).

## The three operations

Data-protection law distinguishes obligations that engineering must map
to distinct mechanisms:

- **Destruction** — irreversible elimination of the bytes, or of the
  secret that gives the bytes meaning. Not undoable by restore or
  re-sync.
- **Deletion** — removal from a store such that normal operations no
  longer retrieve the data. Restorable in principle (backups, peers,
  detached parts); legal sufficiency depends on what the restore paths
  look like.
- **Modification (rectification)** — replacing content with corrected
  content. In store 1 this is only expressible as *new* records (new
  patches), never in place; in store 2 it is an ordinary update.

GDPR Art 17 erasure can be discharged by destruction, or by deletion
combined with the residue being anonymous; Art 16 rectification maps to
modification; Art 5(1)(e) storage limitation maps to scheduled
destruction (e.g. tombstone sweeps).

## The families

- **(a) Keyed-commitment segregation.** Store 1 carries a keyed one-way
  commitment of the value (HMAC, keyed hash, MAC); cleartext and the
  key/nonce material live in store 2. Erase = destroy the secret (and
  the store-2 rows). The residue is a fixed-size string; whether it is
  anonymous depends on the secret scope (below) — deterministic schemes
  leave equality structure behind, per-occurrence schemes leave
  independent random-looking strings.
- **(b) Encryption with key destruction (crypto-shredding).** Store 1
  carries ciphertext; the key lives in store 2. Erase = destroy the key.
  WP216 files this as pseudonymisation under GDPR (the ciphertext
  persists and decryptability is one key-compromise or
  algorithm-break away); the Swiss FADP is more permissive (ADR-0027
  SQ8). Independently useful as defence-in-depth *inside* a family-(a)
  store 2 (ADR-0025 SD12).
- **(c1) Structural rewrite.** Recompute store 1 without the offending
  record (BFG / git-filter-repo style): new identities for everything
  downstream, coordinated re-clone everywhere. Erases for real, at the
  cost of the identity invariant the store exists to provide.
- **(c2) Forward-secure schedule.** Per-epoch keys destroyed on a
  schedule; data ages into unreadability by construction. Satisfies
  storage limitation automatically; requires a protocol change and makes
  retention a one-way clock.
- **(d) Boundary anonymisation.** Personal data never enters store 1 in
  any form — not even committed. The strongest posture and the only one
  with no erasure mechanism to operate, but it requires schema-level
  control over what producers write (infeasible for free-form content
  like VCS cells).

## Secret scope (axis within (a) and (b))

The granularity at which the store-2 secret is held: **per-actor**,
**per-subject**, **per-patch**, **per-node**, **per-occurrence**,
**per-epoch**. The choice fixes three things at once:

- **Erasure granularity.** Destroying the secret erases everything in
  its scope — a per-actor secret cannot forget one attribute without
  collateral damage to the actor's other records.
- **Residue linkability.** Any deterministic reuse of a secret maps
  equal plaintexts to equal residues; those equality classes are public
  in store 1 and survive secret destruction (WP216's linkability
  criterion). Per-occurrence secrets produce pairwise-independent
  residues.
- **Rotation feasibility.** Store-1 records are immutable, so a secret
  that has produced commitments cannot be rotated for those records —
  ever. The scope must match the intended erasure unit *a priori*.

## Propagation modes

How an erasure decision reaches (or fails to reach) other holders of
store 1:

- **Unilateral** — the operation acts on store 2 only; store 1 is
  untouched everywhere and needs no peer cooperation (family (a)/(b)
  with off-store cleartext).
- **Cooperative** — peers honour signed purge/redact directives
  (ADR-0025 Architecture B); bounded by the peers that are reachable
  and willing.
- **Coordinator-driven** — a central party rewrites store 1 and
  redistributes (family (c1)).
- **Automatic** — the protocol itself ages data out (family (c2)).

## Instances

| Instance | Family | Secret scope | Propagation |
|---|---|---|---|
| ADR-0025 **A** (adopted) | (a) | per-occurrence nonce | unilateral |
| ADR-0025 B (rejected) | (a)-adjacent, plaintext store 1 | n/a | cooperative |
| ADR-0025 C (rejected) | (a) + B fallback | per-actor | cooperative |
| ADR-0025 D (rejected standalone; adopted as SD12 backstop) | (b) | per-subject key | unilateral |
| ADR-0025 E (rejected) | (c1) | n/a | coordinator-driven |
| ADR-0025 G (rejected as requirement) | proof layer over (a) | — | — |
| ADR-0027 S1–S4 (superseded) | see ADR-0027 variants table | per-actor (+per-node for S3) | unilateral / cooperative |
| Future: boundary anonymise | (d) | n/a | n/a |
| Future: forward-secure pushout | (c2) | per-epoch | automatic |

Both ADRs are pre-acceptance and maintained in sync; rejected and
superseded options are listed in the shape they were proposed.

## References

- [ADR-0025 — Right-to-Erasure Architecture for the Pushout VCS](../adr/0025-pushout-forget-architecture.md)
- [ADR-0027 — Swiss-Only Forget Architecture for the Pushout VCS](../adr/0027-pushout-forget-swiss-fadp.md)
- [Commitments and Zero-Knowledge Proofs](commitments-and-zero-knowledge.md) — the cryptographic abstraction behind family (a), and why proof systems are a separable layer
- [Article 29 WP Opinion 05/2014 on Anonymisation Techniques (WP216)](https://ec.europa.eu/justice/article-29/documentation/opinion-recommendation/files/2014/wp216_en.pdf)
- [EDPB Guidelines 02/2025 on blockchain](https://www.edpb.europa.eu/system/files/2025-04/edpb_guidelines_202502_blockchain_en.pdf)
- `public/algebraicarch/pushout/` — the system under analysis
