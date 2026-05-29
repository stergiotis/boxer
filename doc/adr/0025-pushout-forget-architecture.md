---
type: adr
status: proposed
date: 2026-05-10
# reviewed-by: "@<handle>"     # fill in and uncomment when flipping to accepted
# reviewed-date: YYYY-MM-DD    # fill in and uncomment when flipping to accepted
---

> **Status: proposed — pre-human-review.** Engineering selected **Architecture A** on 2026-05-17 (see Decision + Updates); the front-matter `status` stays `proposed` until data-protection counsel signs off on OQ1 (destroyed-salt HMAC anonymisation) and OQ5 (audit-record retention). The earlier Engineering recommendation (Architecture C, layered) is no longer current — see Updates §2026-05-17 for the supersession trail.

> **Disclaimer.** This document is engineering-grade legal context assembled from primary sources to inform an architectural decision. It is not legal advice. The author is not a lawyer. Verify with qualified counsel before treating any specific position here as compliant.

# ADR-0025: Right-to-Erasure Architecture for the Pushout VCS

## Context

The pushout package (`public/algebraicarch/pushout/`) implements a patch-theory version control system in which patches are content-addressed, immutable, and propagated peer-to-peer via Push/Pull. Two properties of this design create a structural tension with European data-protection law:

- **Patches are identity-by-hash.** A patch's `PatchHash` is the SHA-256 of its serialised changes. Every `NodeID` introduced by a patch carries that hash. Dependent patches reference it in their `Dependencies` slice and in their changes' context fields. The hash is the patch and cannot be changed without orphaning everything downstream.
- **Apply is monotonic and append-only.** The `Graggle` data structure tombstones deleted nodes rather than removing them; pseudo-edges bridge over deleted regions to keep the live subgraph connected. The current `Unrecord` (`pijul_pushout_backend.go:Unrecord`) explicitly preserves the patch envelope in `MetaByHash` so the patch can be reapplied after a Pull from a peer that still holds it. The whole correctness story (commutativity, associativity, frictionless cherry-pick) depends on patches being permanent objects.

The requirement under analysis: a controller of a pushout repository must be able to honour a data subject's request under GDPR Article 17 (right to erasure) and the operationally-equivalent rights under the Swiss Federal Act on Data Protection (FADP, SR 235.1, in force 1 September 2023). The system as it stands has no such mechanism. `Unrecord` is non-erasing by design.

This ADR maps the operations that touch personal data, identifies the risks each one creates, summarises the relevant legal landscape, and evaluates three architectural options against criteria derived from that landscape. It does not select one — that selection requires counsel review.

### System under analysis

The pushout VCS exposes the following operations as `RepoI` / `BackendI` methods (`public/algebraicarch/pushout/pijul/pijul_backend.go`, `pijul_pushout_backend.go`):

| Operation | Effect | Personal-data exposure |
|---|---|---|
| `Init` | Creates an empty `.pushout/` directory tree. | None at this step. |
| `Clone` | Deep-copies one repo's envelopes + applied list to a destination path. | Carries everything the source has. |
| `SetAndRecord` | Diffs the live subgraph against caller-supplied cells, builds a patch, applies it, persists the envelope. | Cell values, cell paths, author, description, timestamp all enter the patch. |
| `Apply` | Decodes a foreign envelope, validates dependencies, applies the patch. | Receives whatever the originating repo recorded. |
| `Push` | Ships hashes the destination doesn't have, in apply order. | Transfers all patch content to the peer. |
| `Pull` | Symmetric of Push. | Receives all patch content from the peer. |
| `Unrecord` | Removes a hash from `appliedHash`, rewrites `applied.txt`, keeps the envelope file and `MetaByHash` entry. | **Does not erase.** Patch is reapplicable. |
| `ExportLatest` | Returns the most-recent envelope as bytes. | Used by the demo's "Email Patch" feature; data leaves the system in a portable form. |
| `State` | Materialises live cells from the graggle plus the applied-patch log. | Reads everything currently visible. |

The graggle data structure underneath (`public/algebraicarch/pushout/graggle/store/graggle.go`) holds two node sets (live + tombstoned), a content map, forward / backward edge lists, and pseudo-edge bookkeeping. Tombstoned nodes retain their content because `Unapply` may resurrect them.

### Personal-data inventory

For this ADR, "personal data" is read in the GDPR / FADP sense: any information relating to an identified or identifiable natural person. Eight distinct touch-points exist in the current code:

1. **Patch metadata** — `Patch.Author` and `Patch.Description` (`graggle/patch/patch.go`). Plain UTF-8 strings recorded into every envelope. Author is typically a name or email; description is free-form. Propagated verbatim via Push/Pull.

2. **Envelope provenance** — `EnvelopeV1.Producer` and `EnvelopeV1.Timestamp` (`pushout/envelope/envelope.go`). Producer identifies the actor; timestamp pins the activity to a moment.

3. **Patch change content** — `Change.Content` for `ChangeNewNode` (`graggle/patch/patch.go`). The bytes the user recorded — the substantive payload of the VCS. Cardinality unbounded; could contain anything.

4. **Graggle node content** — `Graggle.contents[NodeID]` (`graggle/store/graggle.go`). Persists for both live and tombstoned nodes. Tombstones are not garbage-collected; deletion of a node by `DeleteNode` does not remove its content.

5. **NodeID hash references** — `NodeID = (PatchHash, Index)`. Even after a patch envelope is removed, every NodeID derived from it still encodes the patch's hash. The hash itself is metadata about which patch a node came from, not the patch content, but it is a stable identifier that can be linked back to the original patch given a copy from elsewhere.

6. **Audit log strings** — every operation in `pijul_pushout_backend.go` produces an `audit string` returned alongside its result and appended to the per-actor `CliLogs` ring buffer (`DemoStore`). Audit lines may contain author names, patch descriptions, and timestamps.

7. **Demo cell representation** — `KVLine.Path`, `KVLine.Value`, `KVLine.Conflict.AliceValue` etc. (`pijul/pijul_types.go`). The demo's flat-KV schema turns paths and values into the substrate of patch content. A `customer.txt`-shaped fixture can carry email addresses, names, account statuses, IPs.

8. **Rendered output** — the text file produced by `Graggle.Render` (`graggle/store/render.go`). Includes all live node content and conflict markers; this is the form most consumers see.

The first three travel in patch envelopes; the fourth lives in graggle state; the fifth is structural; the sixth is local-only by default but could be persisted; the seventh and eighth are derived. Erasure that targets only one of these (e.g., the envelope on disk) leaves residue in the others.

### Processing operations mapped to articles

The mapping below uses GDPR article numbers; FADP equivalents are noted where they diverge. Article numbers without prefix refer to GDPR; "FADP Art" prefixes the Swiss text.

| Operation | Primary articles engaged | Gap from current implementation |
|---|---|---|
| `SetAndRecord` | Art 5(1)(a–c) lawfulness/purpose-limit, Art 5(1)(e) storage-limit, Art 6 lawful basis, FADP Art 6 principles | No mechanism to refuse or pseudonymise PD at ingest. |
| `Push` / `Pull` | Art 28 processor relationships, Art 26 joint controllers, Art 44–49 international transfers, FADP Art 16 cross-border | Backend is type-checked but identity of the peer is not negotiated; transfer is unilateral. |
| `Apply` (foreign) | Art 6 (controller's basis to receive), Art 25 by-design | No filter or content-side validation; whatever the peer sent is applied. |
| `State` / `Render` | Art 15 right of access | Adequately covered — the user can read what's there. |
| `Unrecord` | Insufficient for Art 17, Art 16 (rectification), FADP Art 32(2)(c) | Local-only; preserves envelope; reversible; not erasure. |
| `ExportLatest` | Art 5(1)(f) integrity/confidentiality, Art 32 security | Plaintext envelope leaves the system. |
| `Clone` | Art 28 if to a processor, Art 6 if to another controller | No agreement layer; trust is implicit in the backend type-check. |
| **(Future) `Forget`** | **Art 17** (and Art 5(2) accountability), **FADP Art 32(2)(c)** | **Subject of this ADR.** |

### Identified risks

For each risk: a short identifier, the affected operation(s), an indicative likelihood (Low / Medium / High) and severity (Low / Medium / High) read against EDPB CEF 2025 enforcement patterns, and the residual after a notional Tier-1 cooperative purge (Architecture B in the next section).

| ID | Risk | Likelihood | Severity | Notes |
|---|---|---|---|---|
| R1 | Personal data enters patches as cell content during normal use | High | High | Default behaviour. No segregation. CNIL Criteo (SAN-2023-009, €40M, 2023) confirms regulators reject systems that don't separate identifiers from outputs. |
| R2 | Personal data persists on remote peers after the data subject withdraws consent | High | High | Inherent to the propagation model. Mitigated only by cooperative purge. |
| R3 | Patch hash references survive in NodeIDs of dependent patches even after envelope removal | Medium | Medium | The hash alone is not personal data, but combined with a copy of the original envelope held elsewhere it is a re-identifier. |
| R4 | Audit log itself contains personal data (author names, descriptions) | Medium | Medium | Logs are not currently subject to a retention schedule. |
| R5 | Backups / mirrors retain pre-purge state past the purge moment | High | Medium | Acknowledged in EDPB CEF 2025 Issue 6: backup modification "is not always advisable", but erasure must apply on restore. No grace period codified. |
| R8 | The `Unrecord` operation is mistaken for erasure by an operator | High | Medium | The current name and behaviour invite confusion. Documentation only goes so far. |
| R9 | Tombstoned graggle nodes retain content indefinitely (ghosts) | High | Medium | `DeleteNode` does not purge `g.contents[id]`. Compaction is not implemented. |

R6/R7 (peer-side propagation-gap risks under Architecture B) are not engaged under the A decision and have been dropped from this inventory.

### Legal landscape

The decision is shaped by five primary instruments and one piece of jurisprudence:

**GDPR Article 17 (Regulation (EU) 2016/679).** Paragraph 1 conditions the right to erasure on six enumerated grounds (no longer necessary, withdrawn consent, objection, unlawful processing, legal obligation, child-of-information-society-services). Paragraph 2 — directly relevant — states that where data has been made public, the controller "taking account of available technology and the cost of implementation, shall take reasonable steps, including technical measures, to inform controllers which are processing the personal data that the data subject has requested the erasure by such controllers of any links to, or copy or replication of, those personal data." This is the closest GDPR comes to acknowledging the propagation gap. Paragraph 3 carves out exceptions (freedom of expression, legal obligation, public interest, archiving/research, legal claims).

**EDPB Guidelines 02/2025 on blockchain (adopted 8 April 2025).** The most directly applicable post-blockchain guidance. Verbatim from p. 12: "technical impossibility cannot be invoked to justify non-compliance with GDPR requirements". Recommendation 16: "Data subjects' rights cannot be restricted — neither by choice of technical implementation nor by the data subjects' consent. […] In particular, personal data needs to be erased or rendered anonymous in the event of an objection […] or a request for erasure". Recommendation 9: where consent is the lawful basis, "no personal data is stored on the blockchain that cannot be rendered anonymous by the erasure of off-chain data".

**EDPB CEF 2025 Coordinated Enforcement Action on Right to Erasure (report adopted 10 February 2026).** 32 supervisory authorities surveyed 764 controllers. Issue 6 (p. 20–21) on backups: "Depending on the technical settings and risks, it might not always be advisable to modify or delete information from back-ups. But, in that case, organisations should have appropriate procedures to keep track of erasure requests and comply with them on restored systems". Demands controllers "be able to demonstrate such erasure". Best practice noted: "replace the personal data they wish to delete with strings of random characters". Issue 7 flags that anonymisation as a substitute for deletion frequently fails when controllers implement only basic pseudonymisation.

**Article 29 WP Opinion 05/2014 (WP216).** Sets the anonymisation bar: result must be "as permanent as erasure, i.e. making it impossible to process personal data". Three criteria: no singling out, no linkability, no inference. Important for our design: "Deterministic encryption or keyed-hash function with deletion of the key […] may be equated to selecting a random number as a pseudonym for each attribute […] and then deleting the correspondence table" — i.e., this is treated as **pseudonymisation, not anonymisation**. Encryption-with-key-destruction does not by itself satisfy Article 17.

**Swiss FADP (SR 235.1).** Art 6(4) requires destruction or anonymisation when data is no longer needed; Art 6(5) imposes a measure of correction/deletion proportionate to risk; Art 32(2)(c) recognises both "delete" and "destroy" as legal remedies. The substantive bar is not lower than GDPR; the personality-rights basis (Art 30) is broader than GDPR's six enumerated grounds, while sanctions are criminal-style fines on natural persons (max CHF 250 000) rather than turnover-based. **Designing to GDPR + EDPB 02/2025 satisfies FADP in practice.** No FDPIC public guidance specifically on distributed/append-only systems exists at this writing.

**ICO "put beyond use" doctrine.** UK ICO accepts that information is treated as erased — even when not actually deleted — if four conditions hold: (a) controller will not use the data to inform any decision, (b) does not give other organisations access to it, (c) surrounds it with appropriate technical and organisational protection, (d) commits to permanent deletion when possible. This is the most practically useful position for distributed/append-only systems but only ICO has formalised it; whether it travels to other DPAs is unsettled (see EU Parliament EPRS Study PE 634.445, 2019, p. 76).

**CNIL v Criteo SAN-2023-009 (June 2023, €40 M).** Sanctioned a controller for stopping ad display without deleting underlying identifiers — confirming that "soft deletion" leaving linkable identifiers in place is rejected by regulators in practice. No published case law accepts "architectural impossibility" as a defence to Article 17.

### Forces the decision must respect

- **Article 25 (data protection by design) is non-negotiable.** Whatever architecture we choose must be defensible as having considered erasure at design time. Building a system that hard-blocks erasure and pleading architecture is foreclosed by EDPB 02/2025 p. 12.
- **The cooperative-peer model is the realistic threat model for the GDPR use case.** Peers are organisational or contractual; non-cooperation is exceptional, not normal. This is not the threat model for adversarial scenarios; those need a different architecture (likely cryptographic-shredding with all its WP216 caveats).
- **Audit-trail demonstrability is mandatory.** EDPB CEF 2025 explicitly demands controllers be able to demonstrate erasure. Any design must produce an inspectable record (signed, timestamped, propagable).
- **Backwards compatibility with existing repos must be considered, not assumed away.** Any new shape must be applicable to the existing patch envelopes already on disk in production deployments.
- **The patch identity invariant (hash = patch) is load-bearing.** Approaches that rewrite hashes (history rewrite, BFG-style) destroy the value proposition of the system. The cost of breaking it for an erasure feature is higher than the cost in a snapshot-based VCS like git.
- **Scope must be honest about what survives.** Bytes physically copied to backup tapes, archive media, peer screenshots, paper printouts cannot be reached by any technical mechanism. The deliverable is best-effort cooperative purge with documented residual risk, not "true" erasure.

## Design space (QOC)

**Question.** What architecture should the pushout VCS adopt to honour data-subject erasure requests under GDPR Article 17 and the equivalent FADP rights, given the constraints above?

**Taxonomy.** The vocabulary used below — architectural *families* (a)/(b)/(c1)/(c2)/(d), the two-store decomposition, operational modes, and the three operations (destruction / deletion / modification-rectification) — is defined in [doc/explanation/erasure-design-space.md](../explanation/erasure-design-space.md). The Architectures and Alternatives that follow are concrete instances within that framework; family labels are noted parenthetically. For a parallel FADP-scope analysis that exercises the same taxonomy with different trade-offs, see [ADR-0027](0027-pushout-forget-swiss-fadp.md).

**Options.**

- **A — PII-segregation by design** *(taxonomy: family (a) HMAC, per-actor salt, vault, unilateral)*. Patches carry only opaque commitments (e.g. salted HMAC) of any field that may contain personal data. Real personal data lives in a per-actor "personal-data vault" — realised as a **Leeway facts table in ClickHouse** — keyed by `(actorID, vaultRef)`. "Forget" = delete the matching fact rows (CH row-level mutation, finality semantics per SD11) and destroy the actor's salt; the patch DAG and graggle state are unchanged; the commitment becomes a hash of bytes nobody can produce. Propagation: vault rows are scoped to the controller's tenant CH instance and never pushed alongside patches.

- **B — Forget-as-operation on patches (cooperative purge)** *(taxonomy: plaintext in store 1 + cooperative-purge propagation; closest to family (a) with deletion implemented via an inter-peer directive)*. Patches continue to carry personal data in cell content. A new `PurgeRecord` envelope kind propagates an instruction: "delete patch with hash X". Cooperating peers honour it: remove envelope file, remove from `applied.txt`, add to a `PurgedSet` to refuse re-application on Push, optionally apply a compensating patch for residual graggle state. Audit log records the issuance and acknowledgements.

- **C — Layered: A as primary, B as retroactive fallback** *(taxonomy: family (a) + cooperative propagation; the engineered combination of A and B)*. New PII flows through the vault layer (Architecture A). Patches that already contain PII before the vault is deployed, or accidents that bypass it, are handled by the cooperative purge mechanism (Architecture B). The two are complementary: A reduces the surface where B is needed.

**Criteria.**

- **C1 — Article 17 defensibility.** Does the architecture credibly satisfy the right to erasure under GDPR + EDPB 02/2025? Assessed against the "technical impossibility is no defence" standard (EDPB 02/2025 p. 12) and WP216's anonymisation bar.
- **C2 — Article 25 by-design defensibility.** Was erasure considered at the architecture level, not bolted on? Assessed against whether the design makes the easy path the compliant path.
- **C3 — Retroactive coverage.** Does it handle patches that already exist in deployed repos, or only future patches?
- **C4 — Implementation surface.** How much code, protocol design, and test work is required to ship a viable v1?
- **C5 — Compatibility with the patch identity invariant.** Does the architecture preserve the property that hash = patch, dependents stay valid, cherry-pick still works across diverged history?
- **C6 — Audit demonstrability.** Can the architecture produce signed, timestamped, propagable evidence that an erasure was requested, propagated, and applied?

**Assessment.** `++` strong positive, `+` positive, `−` negative, `−−` strong negative.

|    | A | B | C |
|----|---|---|---|
| C1 | ++ | + | ++ |
| C2 | ++ | − | ++ |
| C3 | −− | ++ | ++ |
| C4 | + | + | −− |
| C5 | ++ | + | + |
| C6 | + | ++ | ++ |

Greenfield scope removed the weighting on C3 (no legacy patches to reach back to), which is what made A dominant. See the Decision section below.

### Detailed treatment

**Architecture A — PII-segregation by design.**

The vault layer interposes between cell content and patch construction. When `SetAndRecord` is invoked, fields flagged as containing personal data are written to a per-actor vault — realised as a **Leeway facts table in ClickHouse** — under `(actorID, vaultRef)`; the patch's `Change.Content` carries the salted commitment `HMAC(actorSalt, fieldValue)` instead of the raw bytes. On `State` / `Render`, the controller's local vault is consulted to materialise the actual values; absent vault rows render as a placeholder ("[redacted]" or similar).

The vault is *not* propagated. `Push` ships only patches; vault rows live in the controller's tenant ClickHouse instance and are reachable only through that instance's access controls. Multi-actor deployments use per-actor salts (SD6) so that one actor's `Forget` cannot expose another actor's commitments. To "forget", the controller issues a row-level mutation against the vault table (`ALTER TABLE … DELETE WHERE actorID = ? AND vaultRef = ?`) and destroys / rotates the per-actor salt; the patch chain and graggle state are unchanged; the commitment in the patch is now a useless hash that cannot be reversed (HMAC with a forgotten salt is, per WP216, anonymous if the salt was high-entropy and never written down). SD11 spells out the mutation-finality contract because CH mutations are asynchronous.

This pattern matches CNIL 2018's recommendation order (commitment → keyed hash → ciphertext → unkeyed hash → cleartext) and EDPB Guidelines 02/2025 Recommendation 9 explicitly. It is the regulator-preferred approach.

The cost is twofold: every controller must run the vault, and the vault is a new failure mode (lose the vault, lose the data — the patch chain alone cannot reconstruct it). The Leeway-on-CH realisation amortises the operational cost where Leeway is already deployed for analytics; schema design follows Leeway conventions (per ADR-0060 / ADR-0018), so vault rows participate in the usual fact-store retention and access-control tooling.

**Architectures B and C** are dropped from detailed treatment under the A decision. B (cooperative `PurgeRecord` envelope + peer-side `PurgedSet`) would have applied if patches needed to carry PII directly; C (A + B layered) would have applied if A had to retrofit a legacy corpus. Neither situation obtains under the greenfield scope. The short rejection rationale is recorded in *Alternatives* below.

## Decision

**Architecture A — PII-segregation by design** (vault-by-design, no cooperative purge).

Scope: greenfield deployment, multi-actor from day one, single-controller-per-tenant. The vault is realised as a **Leeway facts table in ClickHouse** (see [ADR-0060](./0011-leeway-data-contracts-odcs.md) for the data-contract envelope and [ADR-0018](./0018-leeway-card-json-canonical-format.md) for fact-shape conventions); per-actor HMAC-SHA-256 salts back the commitment scheme (SD6). The `Forget(actorID, vaultRef)` operation deletes the matching vault rows under SD11's CH-mutation-finality contract and rotates / destroys the actor's salt; salt destruction is the load-bearing anonymisation step.

Architecture B (cooperative purge) and Architecture C (layered) are **not adopted**. With a greenfield corpus there is no legacy PII inside patch envelopes to reach back to, so C3 (retroactive coverage) is not engaged; A alone satisfies Art 17 + Art 25 under WP216's destroyed-salt-HMAC reading (subject to OQ1). The SD4 / SD7 / SD12 subsidiary decisions are marked *inapplicable* for traceability.

Counsel-review status: this decision records the engineering selection. Data-protection counsel sign-off on OQ1 (destroyed-salt HMAC as anonymisation) and OQ5 (audit-record retention) remains the gating step before the vault layer ships to production.

If a different architecture later proves necessary (e.g., counsel rejects OQ1, or scope expands to include legacy data ingested under a non-A pipeline), a follow-up ADR supersedes this one.

## Alternatives

- **B — Cooperative purge** *(family (a) plaintext + cooperative propagation)*. Patches keep PII in cleartext; a `PurgeRecord` envelope propagates "delete patch X" to peers, which honour it by dropping the envelope and adding the hash to a persistent `PurgedSet`. Optional compensating patch redacts graggle state. Legal position rests on Art 17(2) "reasonable steps" + ICO "put beyond use". Rejected under greenfield + Architecture A: no legacy patches need reach-back; A's by-design segregation is the stronger Art 25 posture; and B's propagation gap (offline / decommissioned peers receiving no `PurgeRecord`) is structurally unfixable.

- **C — Layered A + B**. A primary, B as fallback for legacy / accidental PII in patch envelopes. Rejected under greenfield: there is no legacy to cover and A by itself satisfies Art 17; the doubled implementation surface (C4 −−) buys nothing.

- **D — Cryptographic shredding alone** *(taxonomy: family (b) symmetric encryption + key destruction)*. Encrypt patch content with a per-patch key; "forget" by destroying the key. Rejected as a sole mechanism *under GDPR scope*: WP216 treats encryption-with-key-destruction as pseudonymisation, not anonymisation; the legal position is materially weaker than A or B. **Note:** ADR-0027 SQ8 records that family (b) is *defensible under FADP-only scope* (BGE 136 II 508 effort test + Walder Wyss "prevents restoring under normal circumstances"); the WP216 ground bites only when GDPR binds. D could also be a building block within A's vault layer if the vault stores ciphertexts.

- **E — History rewrite (BFG / git-filter-repo style)** *(taxonomy: family (c1) structural rewrite)*. Recompute every patch downstream of the target without it; new hashes; everyone re-clones. Rejected because (i) it destroys the patch identity invariant that makes pushout's cherry-pick story work, (ii) no DPA has endorsed git-style history rewrite as Article 17–compliant, and (iii) coordination cost across peers is unbounded.

- **F — Status-quo Unrecord with stronger documentation.** Keep `Unrecord` as the only mechanism, document that it is not erasure, accept the compliance risk. Rejected because the Article 17 right exists whether or not the system supports it; not building the mechanism does not extinguish the legal obligation. The CNIL Criteo precedent confirms regulators do not accept "we documented the limitation" as a defence.

## Subsidiary design decisions

These decisions become live when an architecture is selected. They are sketched here so the chosen architecture can be filled in without re-opening structural questions.

- **SD1 — Authority for issuing a forget request.** Three viable models: (a) anyone with write access to the repo, (b) GPG-signed by a designated "data controller" key per repo, (c) co-signed by data subject + controller (matches GDPR's chain of custody). Recommendation: (b) for v1, with (c) as a hardening upgrade. (a) fails the audit-trail demonstrability requirement.

- **SD2 — Audit-record shape.** A signed `PurgeRecord` (Architecture B) or `VaultErasureRecord` (Architecture A) carrying `{targetHash or vaultRef, timestamp, requesterIdentity, reason, signature}`. Reason is a free-text field for human audit; an enumerated `lawfulBasisInvoked` field could supplement it. The audit record itself must not contain personal data beyond the requester's identity.

- **SD3 — Resistance to legal hold.** Peers may need to refuse a forget request because the data is under litigation hold. The protocol should support a `freezeList` per peer; forget requests for frozen hashes are rejected with an explicit response. The requester is notified. This is required for compliance with retention obligations that may conflict with erasure requests.

- **SD4 + SD7 (Architecture B subsidiary decisions) — *inapplicable*.** SD4 (compensating-patch construction) and SD7 (`PurgedSet` GC) were B-specific and are dropped under the A decision. The antiquing prerequisite SD4 originally named ([ADR-0039](./0039-pushout-antiquing.md)) is therefore no longer a compliance blocker — antiquing remains desirable for theoretical alignment but is unblocked from any forget-architecture milestone. Retained here for traceability; see Updates §2026-05-17.

- **SD5 — Vault persistence (Architecture A).** The vault is per-controller, multi-actor, realised as a **Leeway facts table in ClickHouse**. Fact shape (Leeway encoding per ADR-0060 / ADR-0018): `(actorID, vaultRef, fieldPath, valueBytes, recordedAt, retentionExpiry, saltEpoch)`. Indexing by `(actorID, vaultRef)` for the `State` / `Render` lookup path; secondary index on `(actorID, retentionExpiry)` to drive the storage-limitation sweep (Art 5(1)(e)). Backups of the vault must follow the same retention/erasure policy as the live store; the CH ENGINE choice (`ReplicatedMergeTree` vs. `MergeTree`) determines whether a `DROP TABLE … SYNC` clears replicated copies at once or relies on Keeper-coordinated mutation propagation — see SD11.

- **SD6 — Commitment scheme (Architecture A).** HMAC-SHA-256 with a per-actor salt that is itself protected (HSM-backed if available). The commitment goes into the patch as bytes; salt rotation requires re-issuing commitments and is therefore a heavyweight operation. An alternative is per-field commitment with a per-field nonce stored in the vault; this trades complexity for finer-grained erasure. Multi-actor deployments hold one salt per `actorID`; the salt-keystore is itself a vault-adjacent CH table with the same erasure discipline (deleting an actor's salt is the irrecoverable step that makes that actor's commitments anonymous).

- **SD8 — Tombstone garbage collection.** Independent of erasure architecture: `Graggle.DeleteNode` does not currently remove the tombstoned node's content. R9 in the risk inventory points at this. A separate compaction operation that walks the graggle and removes tombstoned content past a retention threshold is a useful auxiliary feature and reduces residual exposure under Architecture A — the commitment hash stays in the patch but the structural node it referenced no longer exists in the live graggle.

- **SD9 — Forget operation API surface (Architecture A).** Exposed as `Forget(ctx, actorID, vaultRef)` on the controller's vault interface. Atomic on the CH side via `ALTER TABLE vault DELETE WHERE …` plus a follow-up `OPTIMIZE TABLE … FINAL` to materialise the deletion; on the salt side via key-store rotation. Returns the audit-record envelope (signed `VaultErasureRecord` per SD2) for caller-side propagation to the audit log.

- **SD10 — PII-flag policy surface.** Which cell paths route to the vault is a pebble2impl-side policy decision. Implementation: a `FieldPolicyI` interface consumed by `SetAndRecord` — `IsPersonalData(cellPath) bool`. v1 ships a static `Spec`-driven implementation (cell paths listed in a typed Go struct alongside the cell schema); v2 can promote it to a per-actor / per-tenant runtime policy. Out of scope for ADR-0025; will be covered in a follow-up ADR.

- **SD11 — CH mutation finality contract (Architecture A).** ClickHouse `ALTER TABLE … DELETE` is asynchronous: the mutation is queued, applied during the next merge cycle, and only becomes physically irrecoverable once the parent parts are dropped from `system.detached_parts`. For the `Forget` operation to be defensible under Art 17, finality must be observable. v1 contract: `Forget` blocks until (a) the mutation row in `system.mutations` reports `is_done = 1`, (b) `OPTIMIZE TABLE … FINAL` has merged the affected parts, and (c) `SYSTEM DROP DETACHED PARTITION` has cleared the corresponding detached parts. Backups are erased on the next scheduled rotation per SD5. Salt destruction is the load-bearing step — even if (a)–(c) lag, the HMAC commitment is already useless once the salt is gone.

## Open questions for counsel

These are the questions engineering cannot resolve and that materially shape the architecture choice. They should be answered before the Decision section is filled in.

- **OQ1 — Does WP216's anonymisation bar apply to HMAC commitments with destroyed salts?** WP216 treats encryption-with-key-destruction as pseudonymisation. HMAC is not encryption but a one-way function; if the salt was high-entropy and never persisted, the result is presumptively anonymous in a cryptographic sense. Has any DPA opined on this distinction explicitly? The engineering position is that destroyed-salt HMAC meets the bar; counsel should validate.

OQ2 / OQ3 / OQ4 were B-specific (ICO "put beyond use" travel, Art 17(2) reasonable steps for offline peers, peer-acknowledgement countersignature). Dropped under the A decision since no cooperative-purge propagation occurs.

- **OQ5 — Does the audit record itself constitute personal data (it identifies the requester)?** If yes, the audit record needs its own retention schedule and erasure mechanism, raising a recursion the design must handle.

- **OQ6 — Are there sector-specific requirements (financial, health, legal) that override the general erasure architecture?** The current scope is generic GDPR/FADP; sector overrides would shape SD3 (legal hold) significantly.

- **OQ7 — Is FADP-specific guidance from the FDPIC available in any form not surveyed here?** The agent's research did not locate authoritative FDPIC guidance on distributed/append-only systems; absence may reflect search depth rather than non-existence.

## Consequences

### Positive

- Addresses a present legal exposure (Art 17 / FADP Art 32(2)(c)) that the system carries inherently; doing nothing is the most expensive option once an erasure request arrives.
- The DPIA-style framing produced by this ADR is itself an Art 25 / Art 35 artefact: it documents that data-protection considerations were weighed at design time, which is independently required.
- Combined with SD8 tombstone GC, the steady-state PII residual on a cooperating peer becomes bounded by `vault retention horizon × peer-count` rather than `patch-history-depth × peer-count`.
- The audit-record and signing infrastructure (SD1, SD2) is reusable for unrelated provenance use cases (commit signing, supply-chain attestation).

### Negative

- The vault and audit log introduce new persistent state with backup and retention obligations; both become part of the controller's compliance perimeter.
- Render-time degradation: graggles whose vault rows are missing render with placeholders. Consumers must handle this gracefully; UIs must distinguish "redacted" from "missing" from "unauthorized".
- Per-actor salt management is the new load-bearing secret-handling perimeter. Salt compromise re-personalises every commitment for that actor. HSM-backing is recommended where available; rotation is heavyweight (re-commit every record).
- The signing infrastructure (SD1, SD2) is greenfield. Key management, rotation, and revocation are non-trivial and have their own data-protection implications.

### Neutral

- The decision changes the package's contract surface but not its core algorithms. Tarjan, TopoSort, ResolvePseudoEdges, the apply / unapply machinery, the patch construction path remain unchanged.
- The pijul backend pair (`pijul-text` + `pushout-native`) means Architecture A must be implementable behind the same `BackendI` interface. The text backend may not support the vault interpose natively; this is a feature-parity gap to record, not a blocker for `pushout-native`.

## Status

Engineering-decided — Architecture A; awaiting counsel sign-off on OQ1 / OQ5
before flipping the front-matter `status` to `accepted`.

Status lifecycle: `Proposed → Accepted → (Deprecated | Superseded by ADR-XXXX)`.
ADRs are append-only; supersession is recorded, not deleted.

## Updates

### 2026-05-17 — Architecture A selected (greenfield, multi-actor)

The Decision section was filled in: **Architecture A** (PII-segregation by
design) for a greenfield, multi-actor deployment with the vault realised as
a **Leeway facts table in ClickHouse**. Architectures B and C are not
adopted; C3 (retroactive coverage) is unengaged because no legacy PII-bearing
patches exist.

Consequent subsidiary-decision changes:

- SD4 (compensating-patch construction, Architecture B) → *inapplicable*.
- SD7 (`PurgedSet` GC, Architecture B) → *inapplicable*.
- SD5 (vault persistence) rewritten for Leeway / CH.
- SD6 (commitment scheme) augmented with multi-actor language.
- SD9 (Forget API surface), SD10 (PII-flag policy), SD11 (CH mutation
  finality contract) added.

Downstream: [ADR-0027](./0027-pushout-forget-swiss-fadp.md) is superseded by
this decision because Architecture A also clears the FADP axis (BGE 136 II
508 effort test + Art 32(4) anonymisation route); the S2-tier optimisation
ADR-0027 explored is no longer needed. The antiquing prerequisite SD4
originally named (formalised as [ADR-0039](./0039-pushout-antiquing.md)) is
no longer a compliance blocker; ADR-0039 has been softened accordingly.

### 2026-05-12

SD4's "antiquing of dependents (the operation deferred earlier)"
prerequisite was formalised as
[ADR-0039 — Antiquing Architecture for the Pushout VCS](./0039-pushout-antiquing.md).
(Superseded by 2026-05-17: SD4 is now inapplicable; ADR-0039 is retained for
theoretical alignment but no longer gated on this ADR.)

## References

Primary law and authoritative guidance:

- [GDPR Article 17 (gdpr-info.eu mirror)](https://gdpr-info.eu/art-17-gdpr/)
- [Regulation (EU) 2016/679 — full text on EUR-Lex](https://eur-lex.europa.eu/legal-content/EN/TXT/?uri=CELEX:32016R0679)
- [Swiss FADP, in force 1 September 2023 (Fedlex SR 235.1)](https://www.fedlex.admin.ch/eli/cc/2022/491/en)
- [EDPB Guidelines 02/2025 on processing of personal data through blockchain technologies (April 2025)](https://www.edpb.europa.eu/system/files/2025-04/edpb_guidelines_202502_blockchain_en.pdf)
- [EDPB Coordinated Enforcement Framework 2025 — Right to Erasure (report February 2026)](https://www.edpb.europa.eu/system/files/2026-02/edpb_cef-report_2025_right-to-erasure_en.pdf)
- [EDPB Guidelines 5/2019 on the criteria of the right to be forgotten in the search engine cases (July 2020)](https://www.edpb.europa.eu/sites/default/files/files/file1/edpb_guidelines_201905_rtbfsearchengines_afterpublicconsultation_en.pdf)
- [Article 29 Working Party Opinion 05/2014 on Anonymisation Techniques (WP216)](https://ec.europa.eu/justice/article-29/documentation/opinion-recommendation/files/2014/wp216_en.pdf)
- [CNIL — Blockchain and the GDPR: solutions for a responsible use of the blockchain (2018, English)](https://www.cnil.fr/sites/default/files/atoms/files/blockchain_en.pdf)
- [ICO Right to Erasure guidance (UK GDPR)](https://ico.org.uk/for-organisations/uk-gdpr-guidance-and-resources/individual-rights/individual-rights/right-to-erasure/)

Background and context:

- [EU Parliament EPRS Study PE 634.445 — Blockchain and the General Data Protection Regulation (2019)](https://www.europarl.europa.eu/RegData/etudes/STUD/2019/634445/EPRS_STU(2019)634445_EN.pdf)
- [CNIL v Criteo SAN-2023-009 (June 2023, €40 M) — case summary on GDPRhub](https://gdprhub.eu/index.php?title=CNIL_(France)_-_SAN-2023-009)
- [Swiss kmu.admin.ch — overview of the new FADP](https://www.kmu.admin.ch/kmu/en/home/facts-and-trends/digitization/data-protection/new-federal-act-on-data-protection-nfadp.html)
- [AEPD (Spain) Tech note — Blockchain and the right to erasure](https://www.aepd.es/guias/Tech-note-blockchain.pdf)
- [GitHub Docs — Removing sensitive data from a repository (industry-practice reference, not legally endorsed)](https://docs.github.com/en/authentication/keeping-your-account-and-data-secure/removing-sensitive-data-from-a-repository)

In-repo siblings:

- [doc/explanation/erasure-design-space.md](../explanation/erasure-design-space.md) — taxonomy (families, two-store decomposition, operational modes, three operations); referenced throughout this ADR for vocabulary.
- [ADR-0027 — Swiss-Only Forget Architecture for the Pushout VCS](0027-pushout-forget-swiss-fadp.md) — FADP-scope variant; uses the same taxonomy with different trade-offs.

Cross-repo (sibling repos under `..`; go.work resolves these locally):

- `../boxer/public/algebraicarch/pushout/pijul/EXPLANATION.md` — package overview
- `../boxer/public/algebraicarch/pushout/pijul/pijul_pushout_backend.go` — current `Unrecord` implementation
- `../boxer/public/algebraicarch/pushout/graggle/store/graggle.go` — graggle data structure
- `../boxer/public/algebraicarch/pushout/graggle/patch/patch.go` — patch construction
