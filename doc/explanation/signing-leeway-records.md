---
type: explanation
audience: package maintainer
status: draft
# reviewed-by: "@<handle>"     # fill in and uncomment when flipping to stable
# reviewed-date: YYYY-MM-DD    # fill in and uncomment when flipping to stable
---

> **Status: draft — pre-human-review.** Not verified; do not cite as authoritative.
>
> **Not legal advice.** Article/level citations orient engineering; a deployment
> must have its certificate policy, jurisdiction and lawful basis confirmed by
> counsel and the chosen (qualified) trust-service provider.

# Signing leeway records — electronic signatures (EU / Switzerland)

This page explains what it takes to apply an **advanced (AES)**, **regulated
(RES)** or **qualified (QES)** electronic signature — under the EU **eIDAS**
Regulation and the Swiss **ZertES** — to a *leeway record*, and the requirements
that places on leeway's serialization. It is the **general case**: it says nothing
about what a record *means*. A consumer that stores content it must later erase
layers further constraints on top (see [§Signing and erasure](#signing-and-erasure-interact));
this document only states the generic hook and leaves the consumer to discharge it.

The load-bearing idea: **the signature tier (AES / RES / QES) is a property of the
credential and the process, not of the bytes.** Every tier needs the same thing
from leeway — a **canonical, reproducible preimage**. Because boxer owns the leeway
wire, boxer must provide it.

## Background

### The three-pillar model

Both jurisdictions build the strongest signature from three cumulative pillars; drop
one and you fall back to a merely *advanced* signature, which does **not** carry the
"equivalent to a handwritten signature" legal effect.

```
QES = AES  +  qualified certificate  +  qualified signature-creation device (QSCD)
       │              │                        │
   integrity +   a QTSP vouches for       the private key lives in
   sole-control  the signer's identity    certified, tamper-resistant HW
   binding       (Annex I)                (Annex II / HSM)
```

Only **AES** constrains the *artifact*; the other two pillars constrain the
signer's *key material*. That is why the canonical-serialization requirements below
are tier-independent — they serve AES, and AES is inherited by RES and QES.

### EU — eIDAS

**Legal basis:** Regulation (EU) No 910/2014 (**eIDAS**), amended by Regulation (EU)
2024/1183 (**"eIDAS 2.0"**). eIDAS 2.0 did not change the QES definition; it added
the EUDI Wallet, remote-signing rules and new trust services.

| Pillar | Requirement | Reference |
| --- | --- | --- |
| **AES** | (a) uniquely linked to the signatory; (b) capable of identifying them; (c) created with data under the signatory's **sole control**; (d) linked to the signed data so **any subsequent change is detectable**. | Art. 3(11), **Art. 26** |
| **Qualified certificate** | Issued by a **QTSP**; identifies QTSP + signatory, carries validation data, validity window, unique id, the QTSP's seal, a revocation service. | Art. 3(15), **Annex I** |
| **QSCD** | Confidentiality + single-use of the signing key; key not derivable; sole control; must not alter the data-to-be-signed; **certified**, Member-State-listed. | Art. 3(23), **Annex II**, Art. 30–31 |

- **QTSP + Trusted Lists.** Qualified certificates come only from Qualified Trust
  Service Providers, re-audited **≥ every 24 months**; each Member State publishes a
  machine-readable **Trusted List**, rooted in the EU **LOTL** — the object a
  verifier checks (Art. 22).
- **Legal effect (Art. 25).** A QES has the **equivalent legal effect of a
  handwritten signature** and is **recognised across all Member States**.
- **Timestamp (Art. 41–42) & preservation (Art. 34).** A qualified timestamp gives a
  presumption of time + integrity — not required for QES handwritten-equivalence in
  the EU, but essential for evidentiary weight and mandatory at timestamped/long-term
  levels. A qualified **preservation** service keeps a QES verifiable past its crypto
  horizon.
- **Seals for legal persons (Art. 35–40).** Signatures bind a *natural person*; the
  organisation-level analogue is a **qualified electronic seal** — proof of *origin +
  integrity* for a legal person. For machine-emitted records this is usually the
  right instrument.
- **eIDAS 2.0 deltas.** Remote QSCDs managed by a QTSP with a Signature Activation
  Module (align by **21 May 2026**); the **EUDI Wallet** can create QES; new
  qualified services include **electronic ledgers** and **attestation of attributes**.

### Switzerland — ZertES

**Legal basis:** Federal Act on Electronic Signatures (**ZertES**, SR 943.03),
ordinance **VZertES** (SR 943.032), technical ordinance **TAV**. The
handwritten-equivalence rule is in the **Code of Obligations, Art. 14 para. 2^bis**.

Swiss law defines **four** tiers — one more than the EU:

| Tier | Note |
| --- | --- |
| SES – simple | basic electronic signature |
| AES – advanced | as eIDAS AES |
| **RES – regulated** | Swiss-specific tier between AES and QES; a *regulated certificate* **may be issued to legal persons** (incl. UID). Switzerland's answer to the EU "seal". |
| **QES – qualified** | issued **only to a natural person** |

- **🔑 The decisive EU/CH difference — the timestamp is mandatory in Switzerland.**
  Under CO Art. 14(2^bis) a QES equals a handwritten signature **only when combined
  with a qualified electronic timestamp**. Budget one on every record needing that
  weight. Recognised providers (via the Swiss Accreditation Service): Swisscom,
  SwissSign, QuoVadis/DigiCert, BIT.

### Recognition and the signer decision

**No automatic EU↔CH mutual recognition** (mid-2026; a Swiss negotiating mandate was
opened 29 Jan 2025). Cross-border records need a dual-qualified provider or two
signatures. Before any bytes are hashed, decide *who signs*: a **system/node** on an
organisation's behalf → **qualified seal** (EU) / **RES** (CH); a **person** taking
responsibility → **QES**.

## The signature-format layer

The law says "advanced signature"; the interoperable *how* is the ETSI **AdES**
family. Use a standard format so any third-party validator can verify without your
code — never invent a bespoke embedding.

| Format | Payload | Standard | Fit for a leeway record |
| --- | --- | --- | --- |
| **CAdES** | CMS / binary | EN 319 122 | **best** detached signer over arbitrary bytes |
| **XAdES** | XML | EN 319 132 | detached works, but XML-centric |
| **JAdES** | JSON (JWS) | TS 119 182-1 | only if the record *is* JSON; **not** in the ASiC baseline |
| **PAdES** | PDF | EN 319 142 | only if rendered to PDF |
| **ASiC** | ZIP container | EN 319 162 | **the wrapper** — associates detached CAdES/XAdES with data objects |

**Baseline levels** (apply across formats): **B-B** (cert), **B-T** (+timestamp —
minimum to target; CH-mandatory), **B-LT** (+long-term validation material), **B-LTA**
(+archival timestamps — for records verifiable for years).

### Container choice: **ASiC-E + detached CAdES, level B-LTA**

The ASiC baseline (EN 319 162-1) associates **detached CAdES or XAdES** signatures
with one or more data objects in a ZIP, with a manifest and timestamp/evidence-record
support. **JAdES is not in the ASiC baseline**, removing it from a container design. A
leeway record is naturally several byte objects signed as one unit:

```
record.asice  (ZIP)
├── mimetype
├── schema.cbor            ← canonical TableDesc (the schema plane)
├── data.bin               ← canonical row encoding (the data plane)
├── card.json              ← optional human-facing projection + blake3 fingerprint
└── META-INF/
    ├── ASiCManifest.xml    ← lists the objects + their digests
    └── signature.p7s       ← ONE detached CAdES-B-LTA signature/seal over the manifest
```

The leeway bytes are wrapped **untouched**; one detached CAdES signature covers the
manifest (hence all digests); B-LTA + a qualified timestamp give long-term,
CH-valid integrity. A Merkle-root variant seals many records with one operation.

## The canonical-serialization problem

AES requirement **26(d)** — *"any subsequent change in the data is detectable"* — is
a statement about **bytes**: a signature is computed over an exact octet string, so
the same logical record must **always** produce the **same** octet string, on any
machine, in any build, forever. That is a **canonical serialization**, and it is the
one hard prerequisite for AES/RES/QES on leeway records.

Two caveats frame it:

1. **A record that changes over time cannot be signed as a moving target.** You sign
   an immutable thing: a **content-addressed unit** (already canonical by
   construction), an **immutable snapshot** at time *T*, or the **schema** alone.
2. **leeway serializes two planes with different determinism stories** — the schema
   descriptor and the row data.

### Current state in leeway

**Schema plane — canonical *after* an explicit Normalize, but not yet
signing-grade.** `common.TableMarshaller.EncodeTableCbor`
(`public/semistructured/leeway/common/lw_table_marshaller.go`) CBOR-encodes the
`TableDescDto`. The DTO is **map-free** (scalars + co-ordered slices,
`common/lw_types.go`), so map-iteration nondeterminism cannot leak in — but the
encoder uses `Sort: SortNone`, so the layout is pinned to **Go struct declaration
order**, and equal *logical* content off a raw `TableDesc` still differs by authoring
order and naming style. `common.TableNormalizer.Normalize(style)`
(`common/lw_table_normalizer.go`) closes that: it rewrites every name to one
`naming.NamingStyleE` and sorts columns/sections by name, after which two logically
identical tables produce **byte-identical** CBOR (proven in
`common/../test/lw_table_normalizer_test.go`; used as the equality primitive by
`TableOperations.Compare`). Residual gaps (own review
[`doc/leeway-map/REVIEW-2026-06-11.md`](../leeway-map/), finding **A-14**): no format
version / magic; layout pinned to struct order; the naming style is an **out-of-band
parameter**; some carried fields historically dropped on the compare path.

**Card schema document — closest to signing-grade today.**
`card.JsonCardSchemaEmitter` (`card/leeway_card_json_schema.go`, per
[ADR-0018](../adr/0018-leeway-card-json-canonical-format.md)) sorts sections, blanks a
`fingerprint` field, computes `blake3.Sum256`, prefixes `"blake3:"`, re-emits — a
**versioned** (`"leewayCardSchema":"1"`), content-addressed artifact. Limits: it
canonicalises at the *section* level only (column order left to an upstream
`Normalize`), and is computed over the *card-JSON projection*, a **different byte
stream** than the CBOR DTO.

**Data plane — not canonical; do not sign.** The row path emits Arrow IPC / Parquet /
sparse formats (`dml/…`), carrying buffer padding, alignment, optional dictionary
encoding and compression; multi-membership columns are **set-semantic**
(`common/lw_intermediate.go`), i.e. element order is undefined. **A canonical row
encoding does not exist in the tree today.**

### Requirements on leeway's canonical serialization (CS-1 … CS-9)

These are **tier-independent** (they serve AES 26(d), inherited by RES/QES). They are
requirements **on leeway/boxer**.

| # | Requirement | Legal driver | State today |
| --- | --- | --- | --- |
| **CS-1** | **Canonical form.** Equal logical content ⇒ byte-identical output, independent of authoring order, naming style, map iteration, build. | AES 26(d) | Schema: yes *after* `Normalize`. Data: **no**. |
| **CS-2** | **No out-of-band parameters.** Every input that changes the bytes (naming style, set/array order, numeric encoding) is fixed by the spec or embedded — never a caller argument a verifier could set differently. | reproducible verification | `Normalize` takes the style as an **argument** → violated. |
| **CS-3** | **Versioned, spec-pinned layout + magic.** A version/magic and a layout pinned to a written spec (not Go struct order), so a refactor or enum-value change cannot silently alter bytes. | B-LTA long-term verifiability | Card doc: versioned. CBOR DTO: **no version, struct-order layout** (A-14). |
| **CS-4** | **Total, loss-free coverage.** The preimage covers every field of the signed meaning; **nothing silently dropped**; a verifier reconstructs the exact preimage from what it holds. | integrity / non-repudiation | `Compare` deliberately ignores some fields; a signing encoder must not. |
| **CS-5** | **Canonical data-plane encoding.** Fixed column order; a **total order on set/multi-membership elements**; no Arrow padding/dictionary/compression variance; canonical numerics (fixed int width; one float form — resolve −0.0/NaN). | AES 26(d) over the **values** | **Does not exist** — net-new; the long pole. |
| **CS-6** | **Fingerprint over the signed bytes themselves** — not a separate projection. | signed digest ≡ artifact | Only fingerprint is over the *card-JSON projection* ≠ CBOR DTO. |
| **CS-7** | **Hash-addressable & detachable.** A standalone octet string, hashable for a **detached** CAdES signature in an ASiC-E manifest and usable as a **Merkle leaf**. | container / batch pattern | Achievable once CS-1/CS-5 hold. |
| **CS-8** | **Schema↔data binding.** A signed data preimage transitively commits its schema fingerprint, so a signature cannot be transplanted onto a different schema. | integrity of *interpretation* | No binding today. |
| **CS-9** | **Algorithm-agility metadata.** The artifact records which canonicalization + hash version produced it, so an algorithm migration does not break historic verification. | B-LTA longevity | `"blake3:"` tag on the card doc only. |

**Note — general shredded tables.** When the signed rows belong to a *shared, general*
type-shredded table (a facts-style table whose physical columns are canonical-type
lanes, not domain fields), the `TableDesc` alone does **not** fix their meaning: a
**mappingplan** (`marshall`/`mappingplan`) assigns each value's semantic identity via
**memberships**, and governance metadata that would otherwise sit on a column has no
per-attribute home on such a table. CS-8 then requires binding the **mappingplan
fingerprint** as well as the physical schema — and neither the mappingplan nor the
membership vocabulary is fingerprinted today, so for this class of table CS-8 is
entirely net-new.

**Two routes to a signing-grade preimage:** (a) harden `Normalize + EncodeTableCbor`
with CS-2/CS-3/CS-4 (pin the style into the spec, add magic+version, freeze the
layout, audit for dropped fields); or (b) promote the **card schema document**
(already CS-3/CS-6-shaped) by extending its sort to column level (CS-1) and folding
CBOR-only fields in (CS-4). Either way the **data plane (CS-5) is unavoidable new
work**.

## Signing and erasure interact

A signature is the sharpest instance of boxer's **two-store decomposition**
([erasure-design-space](./erasure-design-space.md)): a signed preimage is
**Store-1 content** — immutable once written, identity-bearing, and, after
distribution, present on systems the signer does not control. Anything a controller
must be able to **erase** belongs in **Store 2** (local, mutable) and must therefore
appear in the signed preimage **only as a commitment or handle**, never inline —
otherwise the signature binds the erasable content into an artifact that cannot be
changed without invalidating it, re-creating the erasure obstruction the two-store
split exists to avoid.

The generic recipe (a consumer discharges the specifics):

- Represent erasable content in the signable record as a **commitment**
  ([commitments-and-zero-knowledge](./commitments-and-zero-knowledge.md)) — a keyed,
  hiding+binding digest — plus an opaque **handle** into Store 2.
- Sign the record; the signature covers the commitment, not the content.
- Erasure destroys the Store-2 secret ([ADR-0025](../adr/0025-pushout-forget-architecture.md)),
  leaving the commitment a binding reference to a value that can no longer be opened —
  and **every signature stays valid**, because none ever covered the content.

## Invariants

- **Canonical-before-sign.** Every signature is over a CS-canonical preimage a
  verifier recomputes independently, bit-for-bit.
- **Sign only immutable content.** The preimage is a content-addressed unit, a
  point-in-time snapshot, or a schema — never a still-mutating record.
- **Erasable content enters the preimage only as a commitment/handle** (see above).
- **Schema↔data binding** (CS-8): a data signature transitively commits the schema.
- **Keys outlive content.** Value-encryption keys are shreddable; signing/verification
  keys are retained for the archival life of the signatures.

## Trade-offs

- **CS-5 is net-new.** The schema plane is mostly there; a byte-canonical row
  encoding must be built — it is the long pole.
- **Two fingerprint preimages must converge** (CS-6): CBOR DTO vs card-JSON.
- **Naming-style portability vs. a frozen canonical style** (CS-2) — freeze one
  (e.g. `naming.DefaultNamingStyle`) for reproducibility.
- **Merkle batching** amortises the qualified operation but makes per-record
  verification need an inclusion proof rather than a standalone signature.
- **Timestamp policy = Swiss-superset:** always timestamp (B-T minimum) and one
  artifact satisfies both regimes on that axis.

## Mapping to code

- **Exists:** `common.TableNormalizer.Normalize`, `common.TableMarshaller.EncodeTableCbor`
  (schema plane), `card.JsonCardSchemaEmitter` + `Fingerprint()` (content-addressed
  doc, [ADR-0018](../adr/0018-leeway-card-json-canonical-format.md)),
  `stopa/naturalkey` (deterministic identity) — all under
  `public/semistructured/leeway/`. Governance metadata that a consumer can attach to
  columns lives in `useaspects` / `valueaspects`
  ([leeway-column-names](./leeway-column-names.md)).
- **Net-new:** the CS-5 canonical row encoding; the CS-3 magic+version; the CS-8
  schema↔data binding; and a signing/attestation seam (canonical preimage → detached
  CAdES-B-LTA → ASiC-E), which may live in a dedicated package or in a consumer. Each
  warrants an ADR.

## Further reading

- Neighbours: [erasure-design-space](./erasure-design-space.md),
  [commitments-and-zero-knowledge](./commitments-and-zero-knowledge.md),
  [leeway-column-names](./leeway-column-names.md),
  [pushout-distributed-operation](./pushout-distributed-operation.md).
- Decisions: [ADR-0018 (card canonical format)](../adr/0018-leeway-card-json-canonical-format.md),
  [ADR-0025 (forget architecture)](../adr/0025-pushout-forget-architecture.md).
- EU: eIDAS [Reg (EU) 910/2014](https://eur-lex.europa.eu/legal-content/EN/TXT/?uri=CELEX%3A02014R0910)
  amending [2024/1183](https://eur-lex.europa.eu/eli/reg/2024/1183/oj) — Art. 3, 25,
  26, 34, 35–40, 41–42, Annex I/II.
- CH: [ZertES SR 943.03](https://www.fedlex.admin.ch/eli/cc/2016/752/en), CO Art. 14(2^bis).
- ETSI: CAdES EN 319 122, XAdES EN 319 132, PAdES EN 319 142,
  [JAdES TS 119 182-1](https://www.etsi.org/deliver/etsi_ts/119100_119199/11918201/),
  ASiC EN 319 162, creation/validation EN 319 102-1; validation library: EU **DSS**.
