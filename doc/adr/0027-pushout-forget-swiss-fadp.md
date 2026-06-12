---
type: adr
status: superseded
date: 2026-05-11
superseded-by: ADR-0025
superseded-date: 2026-05-17
# reviewed-by: "@<handle>"     # fill in and uncomment when flipping to accepted
# reviewed-date: YYYY-MM-DD    # fill in and uncomment when flipping to accepted
---

> **Status: superseded by [ADR-0025](./0025-pushout-forget-architecture.md)** (see front-matter for the date). Architecture A — the dual-scope (GDPR + FADP) vault-by-design selection in ADR-0025 — also clears the FADP axis under BGE 136 II 508's effort test and Art 32(4)'s anonymisation route, so the S2-tier optimisation this ADR proposed for Swiss-only scope is not load-bearing for the active deployment. This document is compacted to retain only the reference material that's hard to reconstruct — Swiss legal landscape, FADP↔GDPR comparison, taxonomy mapping — for any future deployment that revisits Swiss-only scope. Descriptions of the *adopted* architecture are kept in sync with ADR-0025; the superseded S-variants are listed as proposed.

> **Disclaimer.** This document is engineering-grade legal context. It is not legal advice. The author is not a lawyer. Verify with qualified Swiss counsel before treating any specific position here as compliant. The FADP and the Federal Council ordinance (OPDo) entered into force on 1 September 2023; the case law cited predates the revision but the underlying identifiability test has not changed.

# ADR-0027: Swiss-Only Forget Architecture for the Pushout VCS

## Context

ADR-0025 analysed the pushout VCS against GDPR + EDPB 02/2025 + WP216. This ADR addressed the complementary scope — a pushout deployment in which all data subjects, all peers, and the controller itself sit entirely under the Swiss FADP, with no EU touch-points — where the binding regulatory regime is materially more permissive than EDPB 02/2025 and ADR-0025's full apparatus is over-built.

The substantive proposal (**S2** — split-tier patch with local-sidecar cleartext, per-actor HMAC commitment, salt-destruction-as-anonymisation) is not the active path: ADR-0025 selected Architecture A, whose erasure story — per-occurrence destroyed-nonce commitments plus the SD12 key-shred backstop — clears Swiss compliance as a side effect of clearing the stricter GDPR bar. What remains useful in this ADR is the Swiss legal landscape and the FADP↔GDPR comparison, both retained below.

### Swiss authorities synthesised

| Authority | Operative holding | Bearing on architecture |
|---|---|---|
| **BGE 136 II 508** (*Logistep*, 2010) | Identifiability is **effort-based**: "if the effort is so great that, according to general life experience, it cannot be expected that any interested party would undertake it, no identifiability is given." The test weighs the *interest* of the data processor or a third party. | Establishes the relative-approach floor. Where re-identification effort exceeds what an interested party would reasonably undertake, the residual is not personal data. |
| **BGE 138 II 346** (*Google Street View*, 31 May 2012) | A **~1 % error rate** in automatic anonymisation is acceptable, provided the software is continuously improved and sensitive contexts (prisons, hospitals) get full anonymisation. | Anonymisation does not have to be perfect. Best-effort proportional to risk is the Swiss standard, not WP216's "permanent as erasure". |
| **BGE 1C_257/2015** (anonymised-judgment-leaked-via-crawl, 2015) | The Bundesgericht itself published an unanonymised judgment, was crawled within four days, and later anonymised retroactively. The court's *own* database now holds the redacted version; third-party crawled copies were not pursued. | Implicit Swiss precedent for "do your part on the systems you control, accept residual on third-party copies." Directly analogous to the pushout offline-peer / archived-copy residual. |
| **Zurich Handelsgericht HG190107-O** (4 May 2021) | Applies the *relative approach* explicitly: "pseudonymised data are no longer considered personal data falling under the scope of the FADP" — for parties without the re-identification key. For key-holders, it remains personal data. | Vault construction (ADR-0025 SD5/SD6): peers receive only the envelope — carrier tokens, never values or nonces — so they hold non-personal-data under HG190107-O from the moment of recording. The controller (vault-holder) remains within FADP scope until `Forget*` destroys the rows. |
| **FADP Art 6(4) (nLPD Art 6 al. 4)** | "Personal data are destroyed or anonymised as soon as they are no longer needed for the purposes of processing." | Statutory storage-limitation. Applies regardless of erasure requests. Tombstone GC obligations follow. |
| **FADP Art 6(5)** | Correction/destruction measures must be **proportionate to the type and scope of the processing and the risk** to personality and fundamental rights. | Statutory proportionality lever: the "best-effort with documented residual" model is *built into* the law, not a defence carved out by case law. |
| **FADP Art 30 + 31** | Personality-rights framing: processing is lawful unless it constitutes an unlawful infringement of personality. Justification by **consent, prevailing private/public interest, or law** (Art 31). | Architecturally, processing patch metadata is presumptively lawful; the controller bears the burden only to justify if challenged. Prevailing interest in system integrity is invocable. |
| **FADP Art 32 al. 2(c)** | The data subject may demand "**l'effacement ou la destruction de données personnelles**" — erasure or destruction. Framed as a personality-rights remedy, not an enumerated right with six grounds. | Single remedy class with no `Art 17(1)(a)-(f)` enumeration. Easier to satisfy via anonymisation-as-erasure (Walder Wyss: "Anonymisation is a viable alternative for deletion, regardless of the purpose of anonymisation"). |
| **FADP Art 60–62** | Criminal penalties: max **CHF 250,000 on natural persons only**, only for **wilful** breaches, only **upon complaint**. No corporate liability except CHF 50k when individual perpetrator unidentified. | Enforcement risk for engineered best-effort systems is materially lower than GDPR Art 83(4)/(5)'s turnover-based fines. The Criteo SAN-2023-009 (€40M) precedent ADR-0025 cites is structurally impossible under FADP. |
| **Kettiger 2021 §28** | "From a purely (data protection) legal perspective, anonymisation of decisions and judgments is **only a pseudonymisation as long as the decision number or case number remains**." | Inverse reading: when the linkage key (in our case, the vault row holding value and per-occurrence nonce — ADR-0025 SD6) is destroyed, what remains is anonymisation *in the legal sense*. The `Forget*` step (row deletion plus SD12 key shred) crosses from pseudonymisation into anonymisation under Swiss law. |
| **Walder Wyss commentary (datenrecht.ch)** | "Deletion means deleting personal data in a manner that **prevents restoring them under normal circumstances**" — distinct from destruction (irreversible destruction of the data carrier). Art 32(4) gives the controller a choice between "**delete, destroy or anonymise**". | The Swiss "deletion" standard is the *normal-circumstances* test, materially weaker than EDPB 02/2025 ¶50's "technical impossibility cannot be invoked". |
| **FDPIC (EDÖB) blockchain guidance** | None as of 2026-05. FDPIC publishes general guidance on encryption / anonymisation / authentication; no position paper on distributed/append-only systems. | The architecture is unsupervised by sector-specific guidance. Bench-mark to BGE case law + nLPD statutory text. |

### Comparison table — FADP (nLPD) vs GDPR on the axes that drive the architecture

Adapted and condensed from Livio di Tria, *Tableau comparatif nLPD/RGPD*, swissprivacy.law (12 February 2021), filtered to the rows that bear on the erasure architecture.

| Axis | nLPD (FADP) | GDPR (RGPD) | Implication for the architecture |
|---|---|---|---|
| **Lawfulness model** (Art 30/31 vs Art 6) | Presumption of lawfulness. Processing infringes personality only when it breaches the principles in Art 6/8 and is not justified by consent, prevailing interest, or law. *Author's commentary: "En droit suisse, un traitement de données n'est pas per se illicite. […] Il s'agit d'une différence fondamentale."* | Prohibition unless one of six enumerated grounds applies. | Operating the pushout VCS does not need a pre-declared "lawful basis" matching an enumerated ground. Prevailing interest in system integrity is invocable as a justification. |
| **Definition of "personal data"** (Art 5(a) vs Art 4(1)) | "Toutes les informations concernant une personne physique identifiée ou identifiable." Restricted to natural persons under the 2020 reform. | Identical in substance; restricted to natural persons. | Equivalent definition; the divergence is in the identifiability *test* (next row), not the definition itself. |
| **Identifiability test** | *Relative approach* (BGE 136 II 508): effort + interest test. Identifiability fails when "the effort is so great that, according to general life experience, it cannot be expected that any interested party would undertake it." | Stricter, somewhat absolute in EDPB practice; "all means reasonably likely to be used" (Recital 26). | Under nLPD, the residual commitment after nonce destruction (ADR-0025 SD6) is non-personal-data on the BGE 136 II 508 effort test. Under GDPR, the EDPB 02/2025 ¶52 conditional standard applies and is more demanding (ADR-0025 OQ1). |
| **Pseudonymisation definition** | **None** in nLPD. Kettiger §13: "Für die Pseudonymisierung besteht im schweizerischen Recht ebenfalls keine Legaldefinition." | Art 4(5): formal definition with technical/organisational separation requirement. | The Swiss law does not statutorily distinguish pseudonymisation from anonymisation; the line is drawn by case law (Zurich HG190107-O) on the relative approach. |
| **Storage limitation** | Art 6(4): "Elles sont détruites ou anonymisées dès qu'elles ne sont plus nécessaires." Disjunctive: either remedy is acceptable. | Art 5(1)(e): retention limited to what is necessary; erasure or anonymisation. | Identical substance; nLPD makes the disjunction more explicit. Tombstone-GC obligation applies under both. |
| **Proportionality of correction/erasure measures** | **Art 6(5) explicit**: "Le caractère approprié de la mesure dépend notamment du type de traitement et de son étendue, ainsi que du risque que le traitement […] présente." | Implicit in Art 25 + Recital 75/76 risk-based approach. | nLPD gives a *statutory* proportionality lever for engineered best-effort. This is the textual foundation for accepting BGE 138 II 346's ~1 % tolerance. |
| **Right to erasure** | Art 32 al. 2(c): "**l'effacement ou la destruction de données personnelles**" as a personality-rights remedy. No enumerated grounds. Walder Wyss: anonymisation is a viable alternative. | Art 17 ("droit à l'oubli"): six enumerated grounds in Art 17(1)(a)–(f); exceptions in Art 17(3). | nLPD lets the controller satisfy the right via anonymisation. The legal route via Art 6(4) (storage-limitation duty) and Art 32 (personality-rights remedy) is independent of GDPR's grounds analysis. |
| **Data protection by design** | Art 7: technical and organisational measures appropriate to the risk. | Art 25: identical substance, more detailed prescriptions. | Both require design-time consideration. The ADR itself functions as the Art 7 / Art 25 evidence. |
| **Cross-border transfers** | Art 16: adequate-protection requirement; SCC-equivalent. | Art 44–49: comprehensive. | Swiss-only by definition: none. If any peer is non-Swiss, this ADR's scope no longer applies and ADR-0025 governs. |
| **Sanctions on natural persons** | Art 60–62: **CHF 250,000 max**, wilful + on complaint. No corporate liability except CHF 50k residual. | Art 83(4)/(5): up to 4 % global turnover or EUR 20M, on legal entities. | Enforcement risk gap is enormous. Designing to FADP-only is materially less expensive in expected-loss terms. |

The structural difference is captured by di Tria's commentary on Art 30: **"En droit suisse, un traitement de données n'est pas *per se* illicite. En droit européen, le responsable du traitement est tenu de remplir l'une des conditions de l'art. 6 RGPD pour traiter des données. Il s'agit d'une différence fondamentale entre la législation suisse et la législation européenne en matière de protection des données."**

### Variants mapped onto the taxonomy

The full erasure design-space taxonomy is defined in doc/explanation/erasure-design-space.md. The S-variants below were this ADR's concrete instances within that framework; included as a comparative inventory.

| Variant | Family | Secret scope | Store-2 location | Propagation | Defensible under |
|---|---|---|---|---|---|
| **S1** status-quo `Unrecord` | none | n/a | n/a | n/a | neither — non-compliant |
| **S2** this ADR's recommendation | (a) HMAC | per-actor | local sidecar | unilateral | FADP |
| **S3** S2 + node-level purge | (a) + cooperative | per-actor + per-node | local + propagated `RedactedSet` | cooperative | FADP (stronger demonstrability) |
| **S4** = ADR-0025 Architecture C | (a) HMAC | per-actor | vault (separate DB) | cooperative | FADP + GDPR |
| ADR-0025 Architecture A *(adopted)* | (a) keyed commitment | per-occurrence nonce | vault (Leeway/CH) | unilateral | FADP + GDPR |
| ADR-0025 Architecture B | (a) / plaintext + propagation | n/a | none | cooperative purge | FADP + GDPR — by-design weak |
| ADR-0025 Alternative D | (b) symmetric | per-patch | sidecar | unilateral | rejected standalone under WP216; **defensible under FADP — see SQ8**; adopted as defence-in-depth inside A's vault (ADR-0025 SD12, per-subject key) |
| ADR-0025 Alternative E | (c1) rewrite | n/a | n/a | coordinator-driven | rejected (no DPA endorsement) |
| Future direction — *boundary anonymise* | (d) | n/a | empty by design | n/a | strongest posture; not feasible for free-form pushout cells |
| Future direction — *forward-secure pushout* | (c2) | per-epoch | local key schedule | automatic | satisfies Art 6(4) by construction; needs protocol change |

## Decision

Superseded by [ADR-0025](./0025-pushout-forget-architecture.md). This ADR's recommendation (S2 — split-tier with local sidecar) is preserved in the variants table above; it is not pursued.

## Alternatives

The active alternative set is owned by [ADR-0025 § Alternatives](./0025-pushout-forget-architecture.md#alternatives); the variants table above carries the comparative inventory.

## Open questions for counsel

SQ1–SQ7 (S2-specific decision-shaping questions) are not carried in this reference document; the numbering gap is deliberate. The one question worth keeping open:

- **SQ8 — Does symmetric-encryption + key-destruction (taxonomy family (b)) satisfy FADP Art 32(2)(c) where WP216 treats it as pseudonymisation under GDPR?** BGE 136 II 508's effort test and Walder Wyss's "prevents restoring under normal circumstances" deletion standard suggest yes. ADR-0025 Alternative D was rejected on the WP216 ground and merits re-examination under FADP-only scope. Material because (b) becomes a viable alternative to ADR-0025 Architecture A's (a) for deployments where peer-side cleartext is required and the controller still wants to retain a deletion lever. ADR-0025 SD12 adopts family (b) as defence-in-depth *inside* A's vault (per-subject key shred as the first `Forget*` step), where the WP216 objection does not bite; the standalone question — (b) alone, with peer-side cleartext, under FADP-only scope — remains open.

## Consequences

The active consequence set is owned by [ADR-0025 § Consequences](./0025-pushout-forget-architecture.md#consequences). The Swiss-specific point worth keeping is the enforcement-risk axis: Architecture A's apparatus (vault, mutation-finality contract) is over-built relative to FADP-only enforcement exposure (CHF 250k cap on natural persons, wilful + on complaint, vs. GDPR Art 83 turnover-based fines); the overspend is accepted because A is the chosen dual-scope path.

## Status

Superseded by [ADR-0025](./0025-pushout-forget-architecture.md) (Architecture A).
Retained as a comparative reference for any future Swiss-only-scope
deployment that might revisit the S2-tier optimisation. While the pushout
ADR pair is pre-acceptance it is maintained in place (Tier 1) and kept in
sync with ADR-0025's adopted architecture.

Status lifecycle: `Proposed → Accepted → (Deferred | Deprecated | Superseded by ADR-XXXX)`.
See [DOCUMENTATION_STANDARD §1 ADR](../DOCUMENTATION_STANDARD.md#architecture-decision-records-why-it-is-this-way)
for the edit-policy tiers.

<!--
## Updates

Tier-2 dated entries land here when implementation reveals a refinement, an aspirational
claim turns out false, or a milestone records what shipped. Single H2; add H3s dated
YYYY-MM-DD. Remove this HTML comment when the section first gains a real entry.
-->

## References

Primary law and binding authority:

- [Swiss FADP (nLPD), in force 1 September 2023 (Fedlex SR 235.1)](https://www.fedlex.admin.ch/eli/cc/2022/491/en)
- Federal Council ordinance (OPDo / DSV) implementing FADP — minimum security standards.

Swiss case law:

- BGE 136 II 508 (*Logistep*, 2010) — relative approach + effort test for identifiability. [Bundesgericht record](https://www.bger.ch/) (search by docid `136-II-508`).
- [BGE 138 II 346 (*Google Street View*, 31 May 2012) — ~1 % anonymisation tolerance](https://www.bger.ch/ext/eurospider/live/de/php/clir/http/index.php?highlight_docid=atf://138-II-346:de&lang=de&type=show_document)
- BGE 1C_257/2015 (29 September 2015) — anonymised-judgment leak via crawl; implicit do-your-part precedent.
- Zurich Handelsgericht HG190107-O (4 May 2021) — pseudonymised data + relative approach. [Pestalozzi commentary](https://pestalozzilaw.com/en/insights/news/legal-insights/spotlight-ruling-zurich-commercial-court-anonymisation-and-pseudonymisation-personal-data/).
- [BGE 1C_562/2024 (13 January 2025)](http://relevancy.bger.ch/cgi-bin/JumpCGI?id=13.01.2025_1C_562%2F2024) — recent anonymisation ruling on procedural numbers.

Swiss commentary:

- [Daniel Kettiger, *Anonymisierung: Rechtliche Aspekte*, in Hürlimann/Kettiger (eds.), *Anonymisierung von Urteilen*, Helbing Lichtenhahn, Basel 2021, pp. 21–30](https://www.kettiger.ch/fileadmin/user_upload/Dokumente/Downloads/Kettiger_Anonymisierung_2021.pdf).
- [Walder Wyss / *datenrecht.ch* — *Swiss Data Protection Act in a Nutshell*](https://datenrecht.ch/en/datenschutzrecht/nutshell/).
- [Onlinekommentar — Art. 6 para. 3-5 FADP commentary](https://onlinekommentar.ch/en/kommentare/dsg6abs3-5).
- [Livio di Tria, *Comparaison entre la nLPD et le RGPD*, swissprivacy.law, 12 February 2021](https://swissprivacy.law/55/) and the [associated comparative table (PDF)](https://swissprivacy.law/wp-content/uploads/2021/02/20210211-Tableau-comparatif-nLPD-et-RGPD.pdf) — source for the comparison rows in this ADR.
- [PwC Switzerland — *Data Deletion*](https://www.pwc.ch/en/insights/regulation/data-deletion.html).
- [LEXR — *Pseudonymisation vs. Anonymisation* (Swiss perspective)](https://www.lexr.com/en-ch/blog/pseudonymisation-vs-anonymisation-of-data/).
- [Privacy Legal CH — *What counts as personal data under the FADP*](https://privacylegal.ch/download/89/2_What_counts_as_personal_data_under_the_FADP.pdf?inline=true).

EU sources (for the comparison and adequacy framing only):

- [GDPR Article 17 (gdpr-info.eu mirror)](https://gdpr-info.eu/art-17-gdpr/) — for the comparison rows.
- [EDPB Guidelines 02/2025 on processing of personal data through blockchain technologies (April 2025)](https://www.edpb.europa.eu/system/files/2025-04/edpb_guidelines_202502_blockchain_en.pdf) — applied in ADR-0025.
- [EDPB Coordinated Enforcement Framework 2025 — Right to Erasure (report February 2026)](https://www.edpb.europa.eu/system/files/2026-02/edpb_cef-report_2025_right-to-erasure_en.pdf) — applied in ADR-0025.

Related ADRs and code:

- [ADR-0025 — Right-to-Erasure Architecture for the Pushout VCS (GDPR + FADP dual-scope)](0025-pushout-forget-architecture.md). This ADR is the (now-superseded) FADP-only-scope variant.
- `public/algebraicarch/pushout/pijul/pijul_pushout_backend.go` — current `Unrecord` implementation.
- `public/algebraicarch/pushout/graggle/store/graggle.go` — graggle data structure, `SweepTombstones`.
- `public/algebraicarch/pushout/graggle/patch/patch.go` — patch construction, `ComputeHash`.
- `public/algebraicarch/pushout/envelope/envelope.go` — envelope codec.
