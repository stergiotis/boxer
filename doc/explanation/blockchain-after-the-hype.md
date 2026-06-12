# Blockchains after the hype: what survives, and what pushout should take from it

> **Status: research summary, 2026-06-12.** Web-verified facts were spot-checked
> on this date; single-source details are marked. Legal observations are
> engineering-grade context, not legal advice (same disclaimer as
> [ADR-0025](../adr/0025-pushout-forget-architecture.md)). Scope: how
> blockchain-derived techniques can (and cannot) increase robustness and trust
> in P2P-shaped data management — read against the pushout architecture
> (ADR-0079 seams, ADR-0025 erasure), OLAP storage (ClickHouse, DuckDB), and
> hierarchical NATS transport.

## 1. The post-hype ledger

Between 2022 and 2025 essentially every flagship "enterprise blockchain"
consortium shut down, wrote off, or pivoted away:

| Project | Stack | Outcome | Stated cause |
|---|---|---|---|
| TradeLens (Maersk/IBM) | Hyperledger Fabric | Shutdown announced 2022-11, offline Q1 2023 | "the need for full global industry collaboration has not been achieved" |
| we.trade (12 banks) | Fabric | Insolvent 2022-06 | member funding dried up |
| B3i (21 insurers) | Corda | Insolvent 2022-07 | shareholders declined re-funding |
| ASX CHESS replacement | Digital Asset / DAML | Paused 2022-11 after external review; ~A$250M pre-tax write-off; restarted on a conventional product (TCS BaNCS) | delivery/complexity — the rare *technical* failure |
| Libra/Diem (Meta) | bespoke | Assets sold 2022-01 to Silvergate, written off 2023 | regulators |
| Amazon QLDB | ledger DB | [Deprecated 2024-07 with no announcement post](https://www.infoq.com/news/2024/07/aws-kill-qldb/), shut down 2025-07-31 | [official migration target: Aurora PostgreSQL + audit tooling](https://aws.amazon.com/blogs/database/migrate-an-amazon-qldb-ledger-to-amazon-aurora-postgresql/) |
| R3 / Corda | Corda | [2025-05-22: Solana Foundation investment, repositioned as bridge onto a public chain](https://r3.com/r3-signals-strategic-shift-to-lead-the-convergence-of-public-and-private-blockchains-to-deliver-internet-capital-markets-through-collaboration-with-solana-foundation/) | standalone private DLT abandoned as a category |
| Hyperledger Foundation | umbrella | [Folded into "LF Decentralized Trust", 2024-09-16](https://www.linuxfoundation.org/press/linux-foundation-decentralized-trust-launches-with-17-projects-100-founding-members) | "blockchain" demoted to one technology among several |

Two readings of the same record:

- **The failure mode was governance, not cryptography.** TradeLens worked;
  competitors would not join a competitor's platform. we.trade and B3i died of
  consortium economics (who pays, who owns). The exception is ASX, where
  delivery itself failed. Post-mortems consistently locate the problem in
  *forcing shared global state and shared governance onto parties whose
  incentives don't support it*.
- **The surviving form is a feature, not a product.** AWS's official QLDB
  replacement is an ordinary RDBMS with an audit trail; Microsoft's
  counter-offer is [ledger *tables* inside Azure SQL](https://techcommunity.microsoft.com/blog/azuresqlblog/moving-from-amazon-quantum-ledger-database-qldb-to-ledger-in-azure-sql/4246237).
  The one prominent enterprise survivor, JPMorgan's Onyx/Kinexys, is a
  *single-operator* permissioned rail — no consortium governance problem, which
  is the point.

The decision frameworks that predicted this aged well: Wüst & Gervais,
["Do you need a Blockchain?"](https://eprint.iacr.org/2017/375) (CVCBT 2018) —
if writers are known and a trusted party is acceptable, you don't — and
[NIST IR 8202](https://nvlpubs.nist.gov/nistpubs/ir/2018/NIST.IR.8202.pdf) (2018).

**Relevance to pushout.** The consortium failure mode is sidestepped by
construction: each peer owns its repo; exchange is voluntary Push/Pull; there
is no shared platform whose governance can collapse. That is not an accident
of the patch-theory model — it is the property the failed systems lacked.

## 2. What survived: the tamper-evidence stack

The durable residue of the blockchain decade is a set of primitives that
provide **tamper-evidence without consensus**. All are mainstream
infrastructure now:

- **Verifiable append-only logs.** Certificate Transparency
  ([RFC 6962](https://www.rfc-editor.org/rfc/rfc6962) →
  [RFC 9162](https://www.rfc-editor.org/rfc/rfc9162)): Merkle-tree logs,
  inclusion + consistency proofs, no consensus — trust comes from monitors
  cross-checking. Mandatory in the WebPKI since 2018; caught real
  mis-issuance (the Symantec distrust).
- **Software supply-chain transparency.** Sigstore/Rekor: npm provenance
  (2023), PyPI attestations (2024), Go checksum database, Homebrew
  attestations. [Rekor v2 went GA in 2025](https://blog.sigstore.dev/rekor-v2-ga/)
  with the explicit goal of being *cheaper to run* — the post-hype signature
  of something that became plumbing.
- **Witness cosigning.** The anti-split-view mechanism, now a small ecosystem:
  [transparency.dev's witness network](https://blog.transparency.dev/can-i-get-a-witness-network)
  and [OmniWitness](https://github.com/transparency-dev/witness),
  [Sigsum](https://www.sigsum.org/) (deliberately minimal tlog + witness
  policies), the C2SP `tlog-witness`/`cosignature/v1` specs,
  [litetlog](https://github.com/FiloSottile/litetlog). A witness verifies a
  log evolves append-only and countersigns its checkpoint; a client requiring
  k independent cosignatures gets split-view protection with no ordering
  consensus. Conceptual ancestor: CoSi (Syta et al., IEEE S&P 2016).
- **Key transparency at billions-scale.** WhatsApp's Auditable Key Directory
  (2023), now [audited by Cloudflare with public proof dashboards](https://blog.cloudflare.com/key-transparency/);
  [Apple's iMessage Contact Key Verification](https://security.apple.com/blog/imessage-contact-key-verification/)
  does device-side consistency verification. No blockchain in the stack.
- **Content addressing — which predates and outlives the hype.** git (2005),
  Merkle anti-entropy in Dynamo-lineage stores (2007), Nix, OCI registry
  digests, BitTorrent v2. Bitcoin borrowed from this lineage, not the reverse.
  **Prolly trees** (Noms → [Dolt](https://www.dolthub.com/)) and Bluesky's
  Merkle Search Trees extend it to structured, diffable, mergeable data — the
  closest relatives of what pushout does. (Dolt even
  [pitched itself as a QLDB migration target](https://www.dolthub.com/blog/2024-08-22-migrating-from-qldb-to-dolt/).)
- **Timestamping.** What compliance actually uses is RFC 3161 / eIDAS
  qualified timestamps. OpenTimestamps survives as a cheap Bitcoin-anchored
  external witness; Roughtime stays niche. Public chains retain exactly one
  honest infrastructural role here: a free, very-hard-to-rewrite notary you
  don't have to operate or trust individually.

**The thesis the evidence supports:** tamper-*evidence* (verifiable logs,
cosigned checkpoints, fork accountability) survived and got boring;
tamper-*proofness* via global consensus retreated to open-membership money.
Counter-evidence worth keeping in view: public-chain anchoring as external
witness is real (OpenTimestamps, some KT designs), and stablecoins/tokenized
funds are genuine consensus-system adoption — but both are consistent with the
carve-out: open membership, money-shaped.

## 3. Trust without global consensus

For pushout's realistic threat model — organisational/contractual peers that
cooperate but should be able to audit each other — the literature gives a
four-layer primitive set, each buying one property:

| Property | Primitive | Canonical result | pushout status |
|---|---|---|---|
| Tamper-evident history | hash-linked DAG, deps inside the hash | git; Merkle DAGs | **already have** — `patch.ComputeHash` |
| Non-repudiable authorship | signature over (hash, producer) | — | ADR-0079 OQ-3 seam; Ed25519 suffices |
| Split-view / fork detection | signed frontier checkpoints + consistency proofs; witnesses or out-of-band gossip | fork consistency: SUNDR (Li & Mazières, OSDI 2004); CT consistency proofs; Sigsum policies | future; cheap once OQ-3 lands |
| Accountability for misbehavior | evidence-producing audit — detect, don't prevent | PeerReview (Haeberlen et al., SOSP 2007); [Kleppmann & Howard, *Byzantine Eventual Consistency*](https://arxiv.org/abs/2012.00472) | falls out of the above |

Three load-bearing observations:

1. **SUNDR's fork-consistency result** translates directly: an untrusted
   relay (a NATS hub tier) that equivocates about history can fork peers'
   views, but any two peers that ever compare signed frontier checkpoints
   detect the fork. Equivocation isn't prevented — it's made *detectable and
   attributable*. In a contractual federation the remedy is organisational
   (the contract), not cryptographic. This is the trust grade the failed
   consortium chains over-engineered past.
2. **Hash-DAG replication tolerates Byzantine peers for convergence.**
   Kleppmann's line of work (BEC;
   ["Making CRDTs Byzantine fault tolerant"](https://martin.kleppmann.com/papers/bft-crdt-papoc22.pdf),
   PaPoC 2022) shows honest peers that exchange heads converge regardless of
   how many peers lie; equivocation just creates more DAG branches. Matrix's
   event DAG is this in production; pushout's patch theory is the same shape
   with a stronger merge story (commutation and first-class conflicts instead
   of last-writer-wins).
3. **Reconciliation efficiency is orthogonal to trust.** IBLT
   (Eppstein et al.), Minisketch (Bitcoin's Erlay), and range-based set
   reconciliation (Meyer's RBSR; negentropy; Willow) reconcile sets of hashes
   and slot into OQ-1 independently of any signature layer. For a deps-in-hash
   DAG, git-style frontier/have-want exchange is the natural first step; RBSR
   is the escalation when histories get deep and divergence shallow.

What the model does **not** need: total ordering (patch dependencies +
commutation already encode all the causality the data model has), BFT
consensus (no open-membership writer set), proof-of-anything, tokens
(peers are contractual).

## 4. Case study: Catalonia, October 2017

The documented record — sharper than the folklore version:

- **Blocking.** Guardia Civil seized `referendum.cat` on 2017-09-13 under a
  court warrant; the [.cat registry (Fundació puntCAT) was raided 2017-09-20](https://www.eff.org/deeplinks/2017/10/no-justification-spanish-internet-censorship-during-catalonian-referendum);
  a [Sept 23 order allowed blocking of *future* referendum-related sites
  without further court review](https://xnet-x.net/en/digital-repression-and-resistance-catalan-referendum/)
  (140+ domains affected).
  [OONI measurements (published 2017-10-03)](https://ooni.org/post/internet-censorship-catalonia-independence-referendum)
  confirmed ≥25 sites blocked via DNS tampering (Orange España, Euskaltel) and
  HTTP transparent-proxy block pages (Telefónica); a later
  [study over 3M measurements across 17 ISPs](https://dl.acm.org/doi/fullHtml/10.1145/3447535.3462638)
  found the blocking applied Spain-wide.
- **The IPFS use was governmental, not just activist.** Per
  [Poblet, *First Monday* 23(12), 2018](https://firstmonday.org/ojs/index.php/fm/article/view/9402),
  the Catalan government launched the "OnVotar" polling-station lookup *on
  IPFS* on 2017-09-21 — with a privacy layer: no cleartext census; the
  browser iterated a hash over DNI + birthdate (1,714 rounds; the year is
  symbolic) so a user could look up only their own polling station.
  Authorities responded by blocking `gateway.ipfs.io` — collaterally blocking
  *all* gateway-routed IPFS content in Spain — but the content "survived
  across the IPFS network and could be replicated until the eve of the
  referendum." Puigdemont tweeted mirror addresses personally.
- **Sober assessment.** What content addressing bought: no single takedown
  point (DNS seizure is useless against CIDs); mirror integrity (a CID can't
  be tampered with by its host); enforcement friction (the censor must either
  over-block a shared gateway or chase individual nodes). What it did not:
  most users reach IPFS via HTTP gateways, which are ordinary blockable
  websites, so the casual-user majority stayed easy to cut off; pinning
  depended on volunteers; **no usage statistics exist** — resilience is
  documented, reach is not. The sharpest architectural datum: on voting day
  the component that actually went down was the *centralized* one — the
  census-validation backend, taken down by Amazon on request. Static
  content-addressed data survived; the centralized dynamic service was the
  kill point.
- **The 2019 follow-up is a different story.** GitHub
  [removed the Tsunami Democràtic APK on a Guardia Civil order](https://techcrunch.com/2019/10/30/github-removes-tsunami-democratics-apk-after-a-takedown-order-from-spain/)
  (the [takedown letter is public in GitHub's gov-takedowns repo](https://github.com/github/gov-takedowns/blob/master/Spain/2019/2019-10-23-GuardiaCivil.md));
  redistribution then ran mainly over Telegram, not IPFS. Accounts conflating
  the two episodes are wrong.

The general lesson matches §2 and §3: content addressing is a *robustness*
primitive (integrity + replicability), not an *availability* guarantee
(someone must still choose to host) and not an anonymity layer (DHT
participation is observable — measurement literature has since demonstrated
cheap surveillance of who provides what).

The IPFS project's own trajectory confirms the kernel/overreach split:
[Iroh](https://www.iroh.computer/) began as an IPFS implementation,
[deliberately broke kubo compatibility](https://github.com/n0-computer/iroh/discussions/707),
and kept exactly three things — BLAKE3 content addressing with incrementally
verified streaming ([iroh-blobs](https://github.com/n0-computer/iroh-blobs)),
dial-by-public-key connectivity, and
[range-based document sync](https://github.com/n0-computer/iroh-docs) — while
dropping the global DHT/namespace. Pushout independently chose the same hash
(BLAKE3) and the same posture (no global namespace; explicit peers).

## 5. Legal currency (as of 2026-06-12)

- **EDPB Guidelines 02/2025 (blockchain): the consultation version is still
  the operative text.** V1.1 was
  [adopted 2025-04-08 for consultation](https://www.edpb.europa.eu/our-work-tools/documents/public-consultations/2025/guidelines-022025-processing-personal-data_en);
  consultation closed 2025-06-09; **no final post-consultation version had
  been published as of this writing**. ADR-0025's citations remain current.
  Industry pushback in the consultation targeted the strictest
  public-chain expectations, not the off-chain-PII recommendation ADR-0025
  builds on — the design sits on the stable part of the guidance.
- **CJEU, *EDPS v SRB* (C-413/23 P, 2025-09-04) — the significant new
  development.** The Court endorsed a **relative/contextual approach to
  identifiability**: pseudonymised data is personal data for the holder of
  re-identification means, but *not necessarily for a recipient* who cannot
  reasonably re-identify, assessed on technical, organisational and legal
  factors
  ([curia press release](https://curia.europa.eu/site/upload/docs/application/pdf/2025-09/cp250107en.pdf),
  [Jones Day](https://www.jonesday.com/en/insights/2025/09/cjeu-clarifies-scope-of-personal-data-in-edps-v-srb-decision),
  [Bird & Bird](https://www.twobirds.com/en/insights/2025/eu-the-srb-decision-a-new-era-for-personal-data-and-data-processing-agreements)).
  Implication for ADR-0025: the "peers hold non-personal data from the moment
  of recording" argument — previously available only on the Swiss axis
  (Zurich HG190107-O; BGE 136 II 508) — is now arguable under EU law for
  vault-less peers holding only `(version, vaultRef, C)` carrier tokens.
  Caveats: the judgment interprets Regulation 2018/1725 (applied to GDPR by
  parallel reasoning), the assessment is per-recipient and fact-dependent,
  and it belongs in the same counsel bucket as OQ1. It strengthens
  Architecture A; nothing found weakens it.
- **Redactable blockchains (chameleon hashes, Ateniese et al.) remain
  research.** No DPA has endorsed hash-rewriting as an Art 17 mechanism;
  regulator-preferred practice remains keeping personal data off the
  immutable structure (CNIL 2018; EDPB 02/2025 Rec 9) — i.e., exactly the
  ADR-0025 pattern.

## 6. Mapping onto the pushout architecture

Per seam, in increasing order of ambition. The first two rows are the
80/20.

| Seam / open question | Primitive to adopt | What it buys |
|---|---|---|
| ADR-0079 OQ-3 (trust hardening) | Ed25519 envelope signatures over `(PatchHash, Producer)`; verify at `AcceptorI` ingress | non-repudiable authorship; broker (NATS) drops out of the TCB — transport never vouches for content |
| Exchange / hubs | **signed frontier checkpoints** (peer signs its applied-frontier + a monotonic counter); peers compare checkpoints opportunistically | SUNDR-style fork detection: an equivocating hub or peer is detectable and the evidence is signed — accountability without consensus |
| Cross-tenant trust (later) | witness cosigning of checkpoints (Sigsum/C2SP-style policy: k independent witnesses) | split-view protection between organisations that don't talk to each other directly |
| ADR-0079 OQ-1 (sync at scale) | frontier/have-want exchange over the dep DAG first; range-based set reconciliation (RBSR/negentropy) if histories get deep | bandwidth ∝ divergence, not history; orthogonal to the trust layer |
| Timestamping (only if disputes need *when*) | RFC 3161 / eIDAS TSA over checkpoint hashes; OpenTimestamps as a free secondary anchor | third-party time attestation without operating anything |
| NATS topology | accounts as tenant boundaries (operator→account→user JWT chains, NKeys); leaf nodes per site (local custody, selective subject bridging); JetStream for durable fan-out | the auth model consortium chains never had, decentralized by key delegation; hierarchy matches "site keeps custody, hub coordinates" |
| OLAP (ClickHouse / DuckDB) | CQRS: the patch log is the system of record; OLAP tables are derived read models, rebuildable from the log; vault per ADR-0025 (SD11 mutation finality); [ClickHouse's native NATS engine](https://clickhouse.com/docs/engines/table-engines/integrations/nats) for ingestion (JetStream support [still maturing](https://github.com/clickhouse/clickhouse/issues/87518)); DuckDB at leaves, ClickHouse at aggregation tiers | analytics plane sees only commitments, never PII, by construction; erasure stays a vault-side row operation, invisible to the DAG |

Deliberately **not** adopted, with reasons the evidence supports:

- **Global consensus / total ordering** — patch dependencies + commutation
  encode all causality the data model has; ordering disputes between
  contractual peers are an organisational problem with signed evidence
  available (§3), not a consensus problem.
- **BFT consensus machinery** — equivocation-*detection* gives the needed
  trust grade at a fraction of the complexity; there is no open-membership
  writer set to defend against.
- **Tokens / incentive layers** — peers are contractual; the incentive layer
  is the contract.
- **On-DAG personal data with redaction tricks** (chameleon hashes etc.) —
  unendorsed by any regulator; ADR-0025's vault split is both stronger and
  simpler, and *EDPS v SRB* just improved its legal posture.
- **JetStream as system of record** — it is a durable carrier; the repo log
  remains authoritative (ADR-0079 Q4/O4b), so transport storage can be
  re-provisioned freely.

One-line summary: pushout already is the part of the blockchain idea that
survived — a content-addressed DAG with explicit causality — and the upgrade
path is signatures, signed frontiers, and (eventually) witnesses: the
certificate-transparency playbook, not the consortium-chain one.

## Sources

Post-hype: [InfoQ on QLDB](https://www.infoq.com/news/2024/07/aws-kill-qldb/) ·
[AWS QLDB→Aurora migration](https://aws.amazon.com/blogs/database/migrate-an-amazon-qldb-ledger-to-amazon-aurora-postgresql/) ·
[Azure SQL ledger pitch](https://techcommunity.microsoft.com/blog/azuresqlblog/moving-from-amazon-quantum-ledger-database-qldb-to-ledger-in-azure-sql/4246237) ·
[R3×Solana announcement](https://r3.com/r3-signals-strategic-shift-to-lead-the-convergence-of-public-and-private-blockchains-to-deliver-internet-capital-markets-through-collaboration-with-solana-foundation/) ·
[Ledger Insights on the R3 pivot](https://www.ledgerinsights.com/r3-pivots-to-public-blockchain-with-solana-partnership/) ·
[LF Decentralized Trust launch](https://www.linuxfoundation.org/press/linux-foundation-decentralized-trust-launches-with-17-projects-100-founding-members) ·
[Wüst & Gervais](https://eprint.iacr.org/2017/375) ·
[NIST IR 8202](https://nvlpubs.nist.gov/nistpubs/ir/2018/NIST.IR.8202.pdf)

Tamper-evidence stack: [RFC 9162](https://www.rfc-editor.org/rfc/rfc9162) ·
[Rekor v2 GA](https://blog.sigstore.dev/rekor-v2-ga/) ·
[witness network](https://blog.transparency.dev/can-i-get-a-witness-network) ·
[OmniWitness](https://github.com/transparency-dev/witness) ·
[Sigsum](https://www.sigsum.org/) ([FOSDEM 2025](https://archive.fosdem.org/2025/schedule/event/fosdem-2025-5661-sigsum-detecting-rogue-signatures-through-transparency/)) ·
[litetlog / transparent keyserver](https://words.filippo.io/keyserver-tlog/) ·
[Cloudflare KT audit](https://blog.cloudflare.com/key-transparency/) ·
[iMessage Contact Key Verification](https://security.apple.com/blog/imessage-contact-key-verification/)

Trust without consensus: SUNDR (Li & Mazières, OSDI 2004) ·
PeerReview (Haeberlen et al., SOSP 2007) ·
[Kleppmann & Howard, BEC](https://arxiv.org/abs/2012.00472) ·
[BFT CRDTs](https://martin.kleppmann.com/papers/bft-crdt-papoc22.pdf) ·
CoSi (Syta et al., IEEE S&P 2016)

P2P lessons: [Iroh post-IPFS direction](https://github.com/n0-computer/iroh/discussions/707) ·
[iroh 1.0 roadmap](https://www.iroh.computer/blog/road-to-1-0) ·
[AT Protocol sync spec](https://atproto.com/specs/sync) ·
[Sync 1.5 proposal](https://github.com/bluesky-social/proposals/tree/main/0006-sync-iteration) ·
[Tap](https://docs.bsky.app/blog/introducing-tap) ·
[Dolt](https://www.dolthub.com/blog/2024-08-22-migrating-from-qldb-to-dolt/)

Catalonia: [OONI report](https://ooni.org/post/internet-censorship-catalonia-independence-referendum) ·
[EFF](https://www.eff.org/deeplinks/2017/10/no-justification-spanish-internet-censorship-during-catalonian-referendum) ·
[Xnet](https://xnet-x.net/en/digital-repression-and-resistance-catalan-referendum/) ·
[Poblet, First Monday 2018](https://firstmonday.org/ojs/index.php/fm/article/view/9402) ·
[ACM WebSci '21 Spain study](https://dl.acm.org/doi/fullHtml/10.1145/3447535.3462638) ·
[TechCrunch on Tsunami Democràtic](https://techcrunch.com/2019/10/30/github-removes-tsunami-democratics-apk-after-a-takedown-order-from-spain/) ·
[Guardia Civil takedown letter](https://github.com/github/gov-takedowns/blob/master/Spain/2019/2019-10-23-GuardiaCivil.md)

Legal: [EDPB 02/2025 consultation page](https://www.edpb.europa.eu/our-work-tools/documents/public-consultations/2025/guidelines-022025-processing-personal-data_en) ·
[EDPS v SRB press release](https://curia.europa.eu/site/upload/docs/application/pdf/2025-09/cp250107en.pdf) ·
[Jones Day](https://www.jonesday.com/en/insights/2025/09/cjeu-clarifies-scope-of-personal-data-in-edps-v-srb-decision) ·
[Bird & Bird](https://www.twobirds.com/en/insights/2025/eu-the-srb-decision-a-new-era-for-personal-data-and-data-processing-agreements)

OLAP/NATS: [ClickHouse NATS engine](https://clickhouse.com/docs/engines/table-engines/integrations/nats) ·
[JetStream integration issue](https://github.com/clickhouse/clickhouse/issues/87518)

In-repo: [ADR-0025](../adr/0025-pushout-forget-architecture.md) ·
[ADR-0027](../adr/0027-pushout-forget-swiss-fadp.md) ·
[ADR-0079](../adr/0079-pushout-production-storage-codec-exchange.md) ·
[erasure-design-space.md](erasure-design-space.md)
