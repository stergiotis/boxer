---
type: adr
status: proposed
date: 2026-06-12
# reviewed-by: "@<handle>"     # fill in and uncomment when flipping to accepted
# reviewed-date: YYYY-MM-DD    # fill in and uncomment when flipping to accepted
---

> **Status: proposed — pre-human-review.** Engineering selection: **Architecture A** (see Decision); the front-matter `status` stays `proposed` until data-protection counsel signs off on OQ1 (destroyed-nonce commitment anonymisation) and OQ5 (audit-record retention). Pre-acceptance, this ADR is maintained in place (Tier 1) as a snapshot of current understanding — there is no code yet; dated `## Updates` entries begin once there is shipped behaviour to record.

> **Disclaimer.** This document is engineering-grade legal context assembled from primary sources to inform an architectural decision. It is not legal advice. The author is not a lawyer. Verify with qualified counsel before treating any specific position here as compliant.

# ADR-0025: Right-to-Erasure Architecture for the Pushout VCS

## Context

The pushout package (`public/algebraicarch/pushout/`) implements a patch-theory version control system in which patches are content-addressed, immutable, and propagated peer-to-peer via Push/Pull. Two properties of this design create a structural tension with European data-protection law:

- **Patches are identity-by-hash.** A patch's `PatchHash` is the BLAKE3-256 of its canonicalised dependency set plus its serialised changes (`Patch.ComputeHash`, `graggle/patch/patch.go`) — dependencies are inside the hash, so identity chains transitively. Every `NodeID` introduced by a patch carries that hash. Dependent patches reference it in their `Dependencies` slice and in their changes' context fields. The hash is the patch and cannot be changed without orphaning everything downstream. Deliberately *outside* the hash: `Author` / `Description` (patch level) and `Producer` / `Timestamp` (envelope level) are provenance, not identity — see inventory items 1–2 for what that buys.
- **Apply is monotonic and append-only.** The `Graggle` data structure tombstones deleted nodes rather than removing them; pseudo-edges bridge over deleted regions to keep the live subgraph connected. The current `Unrecord` (`pijul_pushout_backend.go:Unrecord`) explicitly preserves the patch envelope in `MetaByHash` so the patch can be reapplied after a Pull from a peer that still holds it. The whole correctness story (commutativity, associativity, frictionless cherry-pick) depends on patches being permanent objects. The graggle ships a storage-limitation mechanism: `Graggle.SweepTombstones(now, horizon)` destroys the content bytes of tombstoned nodes past a retention horizon, `NodeContentStatusE` (`Missing` / `Present` / `Purged`) lets data-protection audits distinguish never-recorded from deliberately destroyed, and `Unapply` of an affected patch fails closed past the horizon ("patch is permanent past retention"). The sweep operates on graggle state only — patch envelopes retain their `Change.Content` — so it discharges Art 5(1)(e), not Art 17.

The requirement under analysis: a controller of a pushout repository must be able to honour a data subject's request under GDPR Article 17 (right to erasure) and the operationally-equivalent rights under the Swiss Federal Act on Data Protection (FADP, SR 235.1, in force 1 September 2023). The system as it stands has no such mechanism. `Unrecord` is non-erasing by design.

This ADR maps the operations that touch personal data, identifies the risks each one creates, summarises the relevant legal landscape, and evaluates three architectural options against criteria derived from that landscape. Architecture A is selected (see Decision); counsel review of OQ1 / OQ5 remains the gate between `proposed` and `accepted`.

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
| `SweepTombstones` *(graggle-level)* | Destroys tombstoned nodes' content past a retention horizon; marks them `Purged`; returns purged IDs for audit. | Reduces ghost residue (R9). **Not erasure** — envelopes keep `Change.Content`. |
| `ExportLatest` | Returns the most-recent envelope as bytes. | Used by the demo's "Email Patch" feature; data leaves the system in a portable form. |
| `State` | Materialises live cells from the graggle plus the applied-patch log. | Reads everything currently visible. |

The graggle data structure underneath (`public/algebraicarch/pushout/graggle/store/graggle.go`) holds two node sets (live + tombstoned), a content map, forward / backward edge lists, and pseudo-edge bookkeeping. Tombstoned nodes retain their content because `Unapply` may resurrect them.

### Personal-data inventory

For this ADR, "personal data" is read in the GDPR / FADP sense: any information relating to an identified or identifiable natural person. Eight distinct touch-points exist in the current code:

1. **Patch metadata** — `Patch.Author` and `Patch.Description` (`graggle/patch/patch.go`). Plain UTF-8 strings recorded into every envelope. Author is typically a name or email; description is free-form. Propagated verbatim via Push/Pull. Both fields sit deliberately *outside* the patch hash (`ComputeHash` doc comment): local envelopes can be rewritten without breaking patch identity, so provenance is rectifiable in place on systems the controller reaches — a lever the hashed payload does not have (peer copies are unaffected, so this is rectification, not erasure). Under the A decision the default policy (SD10) routes these fields through the vault like any other PII-bearing field.

2. **Envelope provenance** — `EnvelopeV1.Producer` and `EnvelopeV1.Timestamp` (`pushout/envelope/envelope.go`). Producer identifies the actor; timestamp pins the activity to a moment. Both are envelope-level precisely so they stay outside the patch hash (`envelope.go` doc comment); the same rectifiability note as item 1 applies.

3. **Patch change content** — `Change.Content` for `ChangeKindNewNode` (`graggle/patch/patch.go`). The bytes the user recorded — the substantive payload of the VCS. Cardinality unbounded; could contain anything. Unlike items 1–2, this field is *inside* the patch hash: whatever enters it is identity-bearing and structurally un-erasable, which is why Architecture A keeps identifiable data out of it entirely.

4. **Graggle node content** — `Graggle.contents[NodeID]` (`graggle/store/graggle.go`). Persists for both live and tombstoned nodes. `DeleteNode` does not remove content; `SweepTombstones` destroys tombstoned content past a retention horizon and records the fact in `contentPurged` (see R9, SD8).

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
| `State` / `Render` | Art 15 right of access; FADP Art 25 | Adequately covered — the user can read what's there. Under Architecture A, vaulted fields resolve through the vault's subject index (SD5). |
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
| R9 | Tombstoned graggle nodes retain content indefinitely (ghosts) | Medium | Low | Mitigated: `SweepTombstones` destroys tombstoned content past a retention horizon and returns purged IDs for audit (SD8). Residual: the sweep clock is session-local, and envelopes still carry `Change.Content` — the vault, not the sweep, is the Art 17 mechanism. |

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

- **Article 25 GDPR / FADP Art 7 (data protection by design) is non-negotiable.** Whatever architecture we choose must be defensible as having considered erasure at design time. Building a system that hard-blocks erasure and pleading architecture is foreclosed by EDPB 02/2025 p. 12.
- **The cooperative-peer model is the realistic threat model for the GDPR use case.** Peers are organisational or contractual; non-cooperation is exceptional, not normal. This is not the threat model for adversarial scenarios; those need a different architecture (likely cryptographic-shredding with all its WP216 caveats).
- **Audit-trail demonstrability is mandatory.** EDPB CEF 2025 explicitly demands controllers be able to demonstrate erasure. Any design must produce an inspectable record (signed, timestamped, propagable).
- **Backwards compatibility with existing repos must be considered, not assumed away.** Any new shape must be applicable to the existing patch envelopes already on disk in production deployments.
- **The patch identity invariant (hash = patch) is load-bearing.** Approaches that rewrite hashes (history rewrite, BFG-style) destroy the value proposition of the system. The cost of breaking it for an erasure feature is higher than the cost in a snapshot-based VCS like git.
- **Scope must be honest about what survives.** Bytes physically copied to backup tapes, archive media, peer screenshots, paper printouts cannot be reached by any technical mechanism. The deliverable is best-effort cooperative purge with documented residual risk, not "true" erasure.

## Design space (QOC)

**Question.** What architecture should the pushout VCS adopt to honour data-subject erasure requests under GDPR Article 17 and the equivalent FADP rights, given the constraints above?

**Taxonomy.** The vocabulary used below — architectural *families* (a)/(b)/(c1)/(c2)/(d), the two-store decomposition, operational modes, and the three operations (destruction / deletion / modification-rectification) — is defined in doc/explanation/erasure-design-space.md. The Architectures and Alternatives that follow are concrete instances within that framework; family labels are noted parenthetically. For a parallel FADP-scope analysis that exercises the same taxonomy with different trade-offs, see [ADR-0027](0027-pushout-forget-swiss-fadp.md).

**Options.**

- **A — PII-segregation by design** *(taxonomy: family (a) keyed commitment, per-occurrence nonce, vault, unilateral)*. Patches carry only opaque keyed commitments of any field that may contain personal data. Real personal data lives in a controller-side "personal-data vault" — realised as a **Leeway facts table in ClickHouse** — keyed by a random `vaultRef` and indexed by data subject. "Forget" = delete the matching fact rows (CH row-level mutation, finality semantics per SD11; crypto-shred backstop per SD12); the per-occurrence nonce dies with its row, so each surviving commitment is a 32-byte string nobody can reproduce or correlate. The patch DAG and graggle state are unchanged. Propagation: vault rows are scoped to the controller's tenant CH instance and never pushed alongside patches.

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

Under greenfield scope C3 carries no weight (no legacy patches to reach back to), which makes A dominant. See the Decision section below.

### Detailed treatment

**Architecture A — PII-segregation by design.**

The vault layer interposes between cell content and patch construction. When `SetAndRecord` is invoked, each field flagged as containing personal data (SD10) is written to the controller-side vault — realised as a **Leeway facts table in ClickHouse** (SD5) — and the patch's `Change.Content` carries a fixed-size carrier token `(version, vaultRef, C)` instead of the raw bytes, where `C` is the per-occurrence keyed commitment defined in SD6. Diff stability: before minting a new row, the interpose reads the cell's current carrier token from the live graggle state and compares the vault row's value with the incoming one — an unchanged value keeps its existing token byte-for-byte (the diff records nothing), a changed value mints a fresh `(vaultRef, nonce, C)` and a new row. On `State` / `Render`, the vault is consulted to materialise actual values; a vault miss renders as an explicit placeholder, distinguishable from access-denied and from swept (`Purged`) graggle content. Access requests (GDPR Art 15 / FADP Art 25) resolve through the same subject index that drives erasure.

The vault is *not* propagated. `Push` ships only patches; vault rows live in the controller's tenant ClickHouse instance and are reachable only through that instance's access controls. To "forget", the controller shreds the subject's at-rest data-encryption key (SD12, the immediate step) and issues a row-level mutation against the vault table (`ALTER TABLE … DELETE WHERE …`, finality per SD11); the patch chain and graggle state are unchanged. Because every occurrence carries its own high-entropy nonce, and the nonce is stored nowhere but the deleted row, the surviving commitment is a 32-byte value independent of every other commitment in the corpus — there is no salt whose later compromise could re-personalise it, and no equality structure among residues for an attacker to exploit. This is the closest engineering realisation of the EDPB CEF 2025 best practice of replacing erased data with "strings of random characters", while preserving a verifiable binding *until* erasure (OQ1). On the Swiss axis the same residue clears BGE 136 II 508's effort test a fortiori, and under the relative approach (Zurich HG190107-O) peers — who never receive vault rows or nonces — hold non-personal data from the moment of recording, not merely after erasure.

This pattern matches CNIL 2018's recommendation order (commitment → keyed hash → ciphertext → unkeyed hash → cleartext) and EDPB Guidelines 02/2025 Recommendation 9 explicitly. It is the regulator-preferred approach.

The cost is twofold: every controller must run the vault, and the vault is a new failure mode (lose the vault, lose the data — the patch chain alone cannot reconstruct it). The Leeway-on-CH realisation amortises the operational cost where Leeway is already deployed for analytics; schema design follows Leeway conventions (per [ADR-0018](./0018-leeway-card-json-canonical-format.md)), so vault rows participate in the usual fact-store retention and access-control tooling.

**Architectures B and C** are dropped from detailed treatment under the A decision. B (cooperative `PurgeRecord` envelope + peer-side `PurgedSet`) would have applied if patches needed to carry PII directly; C (A + B layered) would have applied if A had to retrofit a legacy corpus. Neither situation obtains under the greenfield scope. The short rejection rationale is recorded in *Alternatives* below.

## Decision

**Architecture A — PII-segregation by design** (vault-by-design, no cooperative purge).

Scope: greenfield deployment, multi-actor from day one, single-controller-per-tenant, dual regulatory target (GDPR + FADP). The vault is realised as a **Leeway facts table in ClickHouse** (see [ADR-0018](./0018-leeway-card-json-canonical-format.md) for fact-shape conventions); per-occurrence 256-bit nonces back the commitment scheme (SD6). The `ForgetSubject` / `ForgetRefs` operations (SD9) shred the subject's at-rest key (SD12) and delete the matching vault rows under SD11's CH-mutation-finality contract. Destruction of the per-occurrence nonces — which exist nowhere but the deleted rows — is the load-bearing anonymisation step on the GDPR axis; the same step clears FADP Art 32(2)(c) and Art 6(4) a fortiori under BGE 136 II 508's effort test.

Architecture B (cooperative purge) and Architecture C (layered) are **not adopted**. With a greenfield corpus there is no legacy PII inside patch envelopes to reach back to, so C3 (retroactive coverage) is not engaged; A alone satisfies GDPR Art 17 + Art 25 under the destroyed-nonce reading of WP216 (subject to OQ1), and FADP Art 32(2)(c) + Art 6(4) + Art 7 under the landscape retained in ADR-0027. The SD4 / SD7 subsidiary decisions are marked *inapplicable* for traceability.

Counsel-review status: this decision records the engineering selection. Data-protection counsel sign-off on OQ1 (destroyed-nonce commitments as anonymisation) and OQ5 (audit-record retention) remains the gating step before the vault layer ships to production.

If a different architecture later proves necessary (e.g., counsel rejects OQ1, or scope expands to include legacy data ingested under a non-A pipeline), a follow-up ADR supersedes this one.

## Alternatives

- **B — Cooperative purge** *(family (a) plaintext + cooperative propagation)*. Patches keep PII in cleartext; a `PurgeRecord` envelope propagates "delete patch X" to peers, which honour it by dropping the envelope and adding the hash to a persistent `PurgedSet`. Optional compensating patch redacts graggle state. Legal position rests on Art 17(2) "reasonable steps" + ICO "put beyond use". Rejected under greenfield + Architecture A: no legacy patches need reach-back; A's by-design segregation is the stronger Art 25 posture; and B's propagation gap (offline / decommissioned peers receiving no `PurgeRecord`) is structurally unfixable.

- **C — Layered A + B**. A primary, B as fallback for legacy / accidental PII in patch envelopes. Rejected under greenfield: there is no legacy to cover and A by itself satisfies Art 17; the doubled implementation surface (C4 −−) buys nothing.

- **D — Cryptographic shredding alone** *(taxonomy: family (b) symmetric encryption + key destruction)*. Encrypt patch content with a per-patch key; "forget" by destroying the key. Rejected as a sole mechanism *under GDPR scope*: WP216 treats encryption-with-key-destruction as pseudonymisation, not anonymisation; the legal position is materially weaker than A or B. **Note:** ADR-0027 SQ8 records that family (b) is *defensible under FADP-only scope* (BGE 136 II 508 effort test + Walder Wyss "prevents restoring under normal circumstances"); the WP216 ground bites only when GDPR binds. D **is adopted as a building block** within A's vault layer — SD12 uses per-subject key destruction as the finality backstop on top of row deletion.

- **E — History rewrite (BFG / git-filter-repo style)** *(taxonomy: family (c1) structural rewrite)*. Recompute every patch downstream of the target without it; new hashes; everyone re-clones. Rejected because (i) it destroys the patch identity invariant that makes pushout's cherry-pick story work, (ii) no DPA has endorsed git-style history rewrite as Article 17–compliant, and (iii) coordination cost across peers is unbounded.

- **F — Status-quo Unrecord with stronger documentation.** Keep `Unrecord` as the only mechanism, document that it is not erasure, accept the compliance risk. Rejected because the Article 17 right (and the FADP Art 32(2)(c) remedy) exists whether or not the system supports it; not building the mechanism does not extinguish the legal obligation. The CNIL Criteo precedent confirms regulators do not accept "we documented the limitation" as a defence.

- **G — Zero-knowledge proof layer for attribute↔node binding.** Considered because ZKPs recur in the GDPR-blockchain literature (EDPB 02/2025 lists them among data-minimisation PETs), occasionally misread as an erasure prerequisite. Rejected as a requirement: neither GDPR Art 17 nor FADP Art 32(2)(c) demands provable bindings — they demand erasable data. While a vault row lives, "this attribute belongs to this node" is proven by *opening* the SD6 commitment (reveal `(value, nonce)`, recompute `C`): interactive disclosure to a party entitled to see the value, with no zero-knowledge property involved. After `Forget*`, nobody — the controller included — can prove anything about the residue; that unprovability *is* the anonymisation working as intended, and any genuine need to prove things post-erasure is a retention question (Art 17(3); SD3 legal hold), not a cryptographic one. The real ZKP use-case would be proving predicates about a committed value to a verifier who must not learn it (selective disclosure: "age ≥ 18", set membership). That is a separable future layer over the same commitments and is deferred — it adds a proof-system dependency disproportionate to any current consumer.

## Subsidiary design decisions

These decisions become live when an architecture is selected. They are sketched here so the chosen architecture can be filled in without re-opening structural questions.

- **SD1 — Authority for issuing a forget request.** Three viable models: (a) anyone with write access to the repo, (b) GPG-signed by a designated "data controller" key per repo, (c) co-signed by data subject + controller (matches GDPR's chain of custody). Recommendation: (b) for v1, with (c) as a hardening upgrade. (a) fails the audit-trail demonstrability requirement.

- **SD2 — Audit-record shape.** A signed `VaultErasureRecord` carrying `{vaultRefs, timestamp, signingKeyID, lawfulBasisInvoked, ticketRef?, signature}`. By construction it contains no natural-person identifiers: the subject is referenced only through the now-dangling random `vaultRefs`, the requester through the controller's signing-key ID (SD1(b)), and the reason through an enumerated `lawfulBasisInvoked` (spanning both regimes: Art 17(1)(a)–(f) grounds and the FADP Art 32(2)(c) personality-rights remedy) plus an optional opaque ticket reference. Free-text reason fields are excluded — they invite personal data into the one artefact that must outlive erasure. The record serves erasure demonstrability (EDPB CEF 2025; GDPR Art 5(2)) and feeds the FADP Art 12 processing register. This is the engineering half of OQ5; counsel validates the remainder.

- **SD3 — Resistance to legal hold.** Peers may need to refuse a forget request because the data is under litigation hold. The protocol should support a `freezeList` per peer; forget requests for frozen hashes are rejected with an explicit response. The requester is notified. This is required for compliance with retention obligations that may conflict with erasure requests.

- **SD4 + SD7 (Architecture B subsidiary decisions) — *inapplicable*.** SD4 (compensating-patch construction) and SD7 (`PurgedSet` GC) are B-specific and not engaged under A; the numbers are retained for cross-reference stability. Consequence: antiquing ([ADR-0039](./0039-pushout-antiquing.md)) is not a compliance blocker for this ADR — it remains a freestanding correctness / theoretical-alignment question.

- **SD5 — Vault persistence (Architecture A).** The vault is per-controller, multi-actor, realised as a **Leeway facts table in ClickHouse**. Fact shape (Leeway encoding per [ADR-0018](./0018-leeway-card-json-canonical-format.md)): `(vaultRef, subjectID, actorID, fieldPath, valueBytes, nonce, recordedAt, retentionExpiry)`. `vaultRef` is random (≥128-bit), never derived from the value or the subject. `subjectID` and `actorID` are distinct dimensions on purpose: the actor is whoever *recorded* the patch, the subject is whom the data is *about*, and erasure/access requests (GDPR Art 17 / Art 15; FADP Art 32(2)(c) / Art 25) arrive keyed by subject — a vault keyed only by recording actor cannot answer them. Primary lookup by `vaultRef` (the `State` / `Render` path); secondary index on `subjectID` (access + erasure); secondary index on `retentionExpiry` to drive the storage-limitation sweep (Art 5(1)(e) / FADP Art 6(4)) — value changes mint new rows (SD6), so superseded rows are exactly the sweep's targets. Backups of the vault must follow the same retention/erasure policy as the live store; with SD12 adopted, restored backups are dead ciphertext for shredded subjects, which discharges the EDPB CEF 2025 Issue 6 erasure-on-restore demand by construction. The CH ENGINE choice (`ReplicatedMergeTree` vs. `MergeTree`) determines whether a `DROP TABLE … SYNC` clears replicated copies at once or relies on Keeper-coordinated mutation propagation — see SD11.

- **SD6 — Commitment scheme (Architecture A): per-occurrence nonce, keyed BLAKE3.** Each vault row stores a fresh 256-bit CSPRNG nonce `r`; the patch-side carrier token is `(version, vaultRef, C)` with

  `C = BLAKE3-keyed(r, "boxer/pushout/pii-commitment/v1" ‖ lp(vaultRef) ‖ lp(fieldPath) ‖ lp(value))`

  where `lp(·)` is length-prefixing (no concatenation ambiguity) and the full 32-byte output is kept. Keyed BLAKE3 keeps the package on its single hash primitive (`types.HashBytes` rationale, `graggle/types/types.go`); HMAC-SHA-256 is an acceptable substitute if an external crypto profile demands one. The binding covers `vaultRef` and `fieldPath`, so a vault row cannot be replayed against a different slot; the token is fixed-size, so committed values leak no length information. While a value is unchanged its existing token is reused byte-for-byte (diff stability — see the Architecture A treatment); every value *change* mints a fresh `(vaultRef, r, C)` and a new vault row, and superseded rows become the Art 5(1)(e) / FADP Art 6(4) sweep's targets (SD5). Verifying that an attribute belongs to a node while its row lives is recomputation from `(value, r)` — a commitment *opening*; no proof machinery is involved (Alternative G).

  **Rejected variant — per-actor salt** (HMAC-SHA-256 under one salt per recording actor, HSM-backed). Kill-reasons, recorded so the variant is not re-derived:

  1. *Determinism leaks equality.* One salt per actor maps equal values to equal commitments. The equality classes are public in every peer's patch corpus and survive salt destruction — failing WP216's no-linkability criterion exactly where EDPB CEF 2025 Issue 7 reports weak pseudonymisation failing, and lowering the re-identification effort that FADP's BGE 136 II 508 test weighs.
  2. *Rotation is an illusion.* Commitments live inside hashed patch content; re-issuing them under a new salt would change patch bytes and break the identity invariant this ADR holds load-bearing. A salt, once used, is unrotatable for everything already recorded — the variant's maintenance story contradicts the system invariant.
  3. *All-of-actor granularity.* Salt destruction anonymises every commitment the actor ever recorded; forgetting one subject's one attribute would collaterally destroy binding verifiability for all the actor's other rows. With per-occurrence nonces the erasure unit equals the vault row.
  4. *Actor ≠ subject.* The salt is keyed to the recording actor, but erasure requests arrive keyed by data subject (GDPR Art 17; FADP Art 32(2)(c)); the secret's scope does not match the right's scope (SD5).

  Cost accepted: 32 extra bytes per row, and no equality joins over committed fields — deliberately, since that join structure is the privacy leak; a consumer with a legitimate need for equality matching on a PII field takes it to the vault side under access control, not to the commitments. A side benefit under the Swiss relative approach (Zurich HG190107-O): peers hold neither value nor nonce for *any* row, so what they hold is non-personal data for them from the moment of recording, not only after erasure.

- **SD8 — Tombstone garbage collection. Shipped.** `Graggle.SweepTombstones(now, horizon)` (`graggle/store/graggle.go`) destroys tombstoned nodes' content past a retention horizon, marks them `Purged` (distinguishable from `Missing` for data-protection audits via `NodeContentStatus`), fails `Unapply` closed past the horizon, and returns the purged `NodeID`s in deterministic order for structured audit logging. Its doc comment carries the same dual framing as this ADR (GDPR Art 5(1)(e) / FADP Art 6(4): mechanism here, legal framing the consumer's concern). Residuals: the tombstone clock is session-local (graggles rebuilt from envelopes reset it — applications needing persistent retention must layer it), and envelopes retain `Change.Content`, so the sweep discharges storage limitation for graggle state only; the vault remains the Art 17 / FADP Art 32(2)(c) mechanism. R9 reflects this.

- **SD9 — Forget operation API surface (Architecture A).** Two verbs on the controller's vault interface: `ForgetSubject(ctx, subjectID)` — the GDPR Art 17 / FADP Art 32(2)(c) entry point, resolving all of the subject's `vaultRef`s via the SD5 subject index — and `ForgetRefs(ctx, vaultRefs…)` for targeted erasure (single attribute; rectification fallout). Sequence per SD11/SD12: shred the subject's at-rest key first (instant backstop), then `ALTER TABLE vault DELETE WHERE …` plus `OPTIMIZE TABLE … FINAL` to materialise the deletion. Returns the signed `VaultErasureRecord` (SD2) listing the dangling `vaultRef`s for caller-side propagation to the audit log. Where vault verbs are exposed over the in-process bus, they go through the audited request/reply path, not publish.

- **SD10 — PII-flag policy surface.** Which cell paths route to the vault is an application-side policy decision (in this repo: the pijul demo and any future pushout consumer). Implementation: a `FieldPolicyI` interface consumed by `SetAndRecord` — `IsPersonalData(cellPath) bool`. v1 ships a static `Spec`-driven implementation (cell paths listed in a typed Go struct alongside the cell schema); v2 can promote it to a per-actor / per-tenant runtime policy. Default-on for provenance: `Author` / `Producer` route through the vault unless policy explicitly opts them out (inventory items 1–2). Out of scope for ADR-0025; will be covered in a follow-up ADR.

- **SD11 — CH mutation finality contract (Architecture A).** ClickHouse `ALTER TABLE … DELETE` is asynchronous: the mutation is queued, applied during the next merge cycle, and only becomes physically irrecoverable once the parent parts are dropped from `system.detached_parts`. For the `Forget` operations to be defensible under GDPR Art 17 / FADP Art 32(2)(c), finality must be observable. v1 contract: `Forget*` blocks until (a) the mutation row in `system.mutations` reports `is_done = 1`, (b) `OPTIMIZE TABLE … FINAL` has merged the affected parts, and (c) `SYSTEM DROP DETACHED PARTITION` has cleared the corresponding detached parts. Backups are erased on the next scheduled rotation per SD5. The per-occurrence nonce lives *inside* the row being deleted — no out-of-row secret's destruction can substitute for physical finality — which is why SD12's key shred runs first: even while (a)–(c) grind, the row and every backup or replica copy of it are already ciphertext under a destroyed key. That intermediate state alone meets the FADP deletion standard ("prevents restoring under normal circumstances" — Walder Wyss; ADR-0027 SQ8); the GDPR position rests on (a)–(c) completing.

- **SD12 — Vault-at-rest encryption and crypto-shred backstop (Architecture A).** The `valueBytes` and `nonce` columns are envelope-encrypted under a per-subject data-encryption key held in a small mutable keystore (vault-adjacent table or external KMS). `Forget*` destroys the subject's key as its first step: instantly, every copy of the subject's rows — live parts, detached parts, replicas, backups, future restores — degrades to ciphertext under a nonexistent key, discharging the EDPB CEF 2025 Issue 6 erasure-on-restore demand by construction rather than by procedure. WP216's objection to encryption-with-key-destruction (taxonomy family (b): pseudonymisation, not anonymisation) does not bite here, because key destruction is defence-in-depth *on top of* the primary mechanism — row deletion destroying value and nonce — not the sole mechanism; this is the layering Alternative D anticipated. Under FADP-only scope the shred would even suffice alone (ADR-0027 SQ8). The keystore is a materially smaller secret perimeter than a per-actor salt store (SD6 rejected variant) would be: a leaked key re-personalises nothing by itself (the ciphertext rows are vault-internal and mutation-deleted), whereas a leaked salt would re-personalise commitments that are public in every peer's patch corpus. Recommended for v1.

## Open questions for counsel

These are the questions engineering cannot resolve and that materially shape the architecture choice. They should be answered before the Decision section is filled in.

- **OQ1 — Do destroyed-nonce per-occurrence commitments clear WP216's anonymisation bar?** WP216 equates keyed-hash-with-key-deletion to "selecting a random number as a pseudonym … and then deleting the correspondence table" and files it under pseudonymisation. That passage addresses *deterministic* schemes, where one key spans many records and equality structure (linkability) survives key deletion. The SD6 scheme deletes a *per-occurrence* correspondence: after `Forget*`, each residue is a 32-byte string independent of every other commitment and of the value — no singling out, no linkability, no inference — and coincides with the EDPB CEF 2025 noted best practice of replacing erased data with "strings of random characters". The FADP axis is not the open part: BGE 136 II 508's effort test and the Walder Wyss deletion standard are cleared comfortably (ADR-0027 landscape). The engineering position is that the per-occurrence distinction meets the GDPR bar too; counsel should validate, since no DPA has opined on it explicitly.

OQ2 / OQ3 / OQ4 were B-specific (ICO "put beyond use" travel, Art 17(2) reasonable steps for offline peers, peer-acknowledgement countersignature). Dropped under the A decision since no cooperative-purge propagation occurs.

- **OQ5 — Does the audit record itself constitute personal data?** SD2 now excludes natural-person identifiers by construction (random `vaultRefs`, controller signing-key ID, enumerated basis, opaque ticket ref). The residual question for counsel: where the signing key is operated by an identifiable officer rather than an organisational role, is the key ID personal data under GDPR — and under the FADP's relative approach, for whom? If yes, what retention schedule applies to erasure records, given they must outlive the erasure they evidence (Art 5(2); FADP Art 12)?

- **OQ6 — Are there sector-specific requirements (financial, health, legal) that override the general erasure architecture?** The current scope is generic GDPR/FADP; sector overrides would shape SD3 (legal hold) significantly.

- **OQ7 — Is FADP-specific guidance from the FDPIC available in any form not surveyed here?** The agent's research did not locate authoritative FDPIC guidance on distributed/append-only systems; absence may reflect search depth rather than non-existence.

## Consequences

### Positive

- Addresses a present legal exposure (Art 17 / FADP Art 32(2)(c)) that the system carries inherently; doing nothing is the most expensive option once an erasure request arrives.
- The DPIA-style framing produced by this ADR is itself an Art 25 / Art 35 artefact: it documents that data-protection considerations were weighed at design time, which is independently required.
- Combined with SD8 tombstone GC, the steady-state PII residual on a cooperating peer becomes bounded by `vault retention horizon × peer-count` rather than `patch-history-depth × peer-count`.
- Erasure granularity equals the vault row: one attribute of one subject can be forgotten without touching anything else — no salt-scope collateral (SD6). With SD12, erasure extends to backups and future restores by construction (EDPB CEF 2025 Issue 6), and a single mechanism discharges both regimes (GDPR Art 17; FADP Art 32(2)(c) + Art 6(4)).
- Under the Swiss relative approach, peers hold non-personal data from the moment of recording (they never receive vault rows or nonces) — the cross-border posture (FADP Art 16; GDPR Art 44–49) for patch propagation improves correspondingly, subject to counsel confirming the residue classification (OQ1).
- The audit-record and signing infrastructure (SD1, SD2) is reusable for unrelated provenance use cases (commit signing, supply-chain attestation).

### Negative

- The vault and audit log introduce new persistent state with backup and retention obligations; both become part of the controller's compliance perimeter.
- Render-time degradation: graggles whose vault rows are missing render with placeholders. Consumers must handle this gracefully; UIs must distinguish "redacted" from "missing" from "unauthorized".
- `SetAndRecord` now reads the vault on every PII-flagged cell to keep unchanged values byte-stable (SD6 diff stability): vault unavailability degrades *recording*, not just rendering — a new availability coupling. The SD12 keystore is a secret-handling perimeter, though a materially smaller one than the rejected per-actor-salt design would have required (a leaked key re-personalises nothing without the vault's ciphertext rows).
- The signing infrastructure (SD1, SD2) is greenfield. Key management, rotation, and revocation are non-trivial and have their own data-protection implications.

### Neutral

- The decision changes the package's contract surface but not its core algorithms. Tarjan, TopoSort, ResolvePseudoEdges, the apply / unapply machinery, the patch construction path remain unchanged.
- The pijul backend pair (`pijul-text` + `pushout-native`) means Architecture A must be implementable behind the same `BackendI` interface. The text backend may not support the vault interpose natively; this is a feature-parity gap to record, not a blocker for `pushout-native`.

## Status

Engineering-decided — Architecture A; awaiting counsel sign-off on OQ1 / OQ5
before flipping the front-matter `status` to `accepted`.

Status lifecycle: `Proposed → Accepted → (Deferred | Deprecated | Superseded by ADR-XXXX)`.
See [DOCUMENTATION_STANDARD §1 ADR](../DOCUMENTATION_STANDARD.md#architecture-decision-records-why-it-is-this-way)
for the edit-policy tiers; pre-acceptance, this ADR is maintained Tier-1 (in place).

<!--
## Updates

Tier-2 dated entries land here when implementation reveals a refinement, an aspirational
claim turns out false, or a milestone records what shipped. Single H2; add H3s dated
YYYY-MM-DD. Remove this HTML comment when the section first gains a real entry.
-->

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

- doc/explanation/erasure-design-space.md — taxonomy (families, two-store decomposition, operational modes, three operations); referenced throughout this ADR for vocabulary.
- doc/explanation/commitments-and-zero-knowledge.md — what a commitment is and how it relates to zero-knowledge proofs; theory behind SD6 and Alternative G.
- [ADR-0027 — Swiss-Only Forget Architecture for the Pushout VCS](0027-pushout-forget-swiss-fadp.md) — FADP-scope variant; uses the same taxonomy with different trade-offs.

In-repo code:

- `public/algebraicarch/pushout/pijul/EXPLANATION.md` — package overview
- `public/algebraicarch/pushout/pijul/pijul_pushout_backend.go` — current `Unrecord` implementation
- `public/algebraicarch/pushout/graggle/store/graggle.go` — graggle data structure, `SweepTombstones`
- `public/algebraicarch/pushout/graggle/patch/patch.go` — patch construction, `ComputeHash` (provenance outside the hash)
- `public/algebraicarch/pushout/envelope/envelope.go` — envelope codec (`Producer` / `Timestamp` outside the hash)
