---
type: explanation
audience: package maintainer
status: draft
# reviewed-by: "@<handle>"     # fill in and uncomment when flipping to stable
# reviewed-date: YYYY-MM-DD    # fill in and uncomment when flipping to stable
---

> **Status: draft — pre-human-review.** Compiled 2026-10-31 as background
> reading on risk management practice across domains. No ADR in this tree
> took a decision from it on the compile date, and nothing here is a
> decision. **Every claim about the status of a standard, regulation or
> framework is as of 2026-10-31** and will age; where the issuing body's own
> page disagrees with this one, the issuing body wins. §9 lists the
> conflicts the reading could not resolve; they are left open rather than
> settled here.
>
> **Provenance.** The page rests on standards catalogue pages, regulator and
> supervisor publications, published papers and their abstracts, publisher
> listings and reviews of books, and practitioner and vendor material. The
> reading was done on the compile date by delegated research passes, one per
> domain in §3–§7. Several standards bodies' sites refused automated
> fetches, so some status claims rest on catalogue excerpts and secondary
> summaries. A claim is unmarked only where a pass read it on a fetched page
> or in a search result quoting the source. Otherwise it is marked:
>
> - *(snippet)* — seen only as a search-result excerpt of the official page;
> - *(secondary)* — confirmed only through a summary, trade press or study notes;
> - *(recalled)* — a pass's background knowledge, linked to the canonical
>   page but not reopened on the compile date;
> - *(vendor)* — the source sells a product or service the claim favours;
> - *(inference)* — drawn by a pass or here, not sourced.
>
> Where the evidence behind a method is at issue it is labelled
> **empirical** (a controlled study or a measurement), **analytical** (a
> mathematical or sensitivity argument) or **guidance** (standard text,
> practitioner or advocate opinion). Figures the passes could not trace to a
> source were left out.

# Risk management practice in late 2026 — methods, standards, visualizations, and the evidence under them

## 0 In short

Risk management in late 2026 has not adopted a new paradigm. It has been
revised in four directions at once, mostly by **layering new methods over
old ones rather than retiring them**. Specialist practice has moved from
ordinal scoring (the 5×5 likelihood × impact matrix, the FMEA risk priority
number, CVSS read as a risk score) toward calibrated ranges, simulated
distributions and explicit decision rules, while enterprise GRC tooling
still centres on the heat map. Financial and operational regulators have
moved from estimating how likely a loss is to **assuming the disruption
happens and capping its impact**, and in 2025–26 they began regulating
concentrated cloud providers directly. Safety engineering has added
systems-theoretic and resilience methods on top of component-failure
methods that certification still requires. Across domains, risk artifacts
are turning from documents written once into **living records** tied to
monitoring: dynamic safety cases, continuous control monitoring, AI use-case
inventories, frontier-model risk reports. The evidence under all of this is
uneven. The argument against ordinal matrices is strong analytically, and
the uncertainty-visualization experiments replicate well. But the passes
found **no controlled study showing that any replacement method reduces
realised losses or accidents**, and much of the adoption data comes from the
methods' own advocates or from vendors.

| Shift | Older practice | Where practice moved (examples, §3–§6) | Evidence kind |
| --- | --- | --- | --- |
| Ordinal scores → distributions and decision rules | 5×5 matrix; RPN = S×O×D; CVSS as risk; VaR | FAIR loss exceedance curves; NASA 70% joint confidence level; FRTB expected shortfall; AIAG-VDA action priority; KEV + EPSS + SSVC | Analytical against the old; little outcome evidence for the new |
| Probability → resilience and impact tolerance | Probability-weighted operational-loss estimates | Basel 2021 resilience principles; DORA; UK impact tolerances; critical-third-party regimes; SRE error budgets; DMDU robustness | Guidance; no outcome evidence found |
| Component failure → systems thinking | FMEA, FTA, PRA, SIL/ASIL from failure rates | STPA; ISO 21448 SOTIF; ISO/PAS 8800; Safety-II and FRAM; network models of systemic risk | Case-study counts of hazards found |
| Static documents → living artifacts | Certification safety case; annual register review | Dynamic assurance cases; UL 4600 safety performance indicators; COSO GenAI continuous monitoring; risk registers rolled up into an enterprise portfolio | Guidance; open research problems |

## 1 Four shifts run across every domain

### 1.1 Ordinal scores are giving way to distributions — in specialist practice, not in GRC suites

The case against the ordinal matrix is old and mostly mathematical. Cox
showed that typical matrices can **assign identical ratings to
quantitatively very different risks**, can rate smaller risks higher, and
can be "worse than useless" when frequency and severity are negatively
correlated ([Cox 2008, RePEc](https://ideas.repec.org/a/wly/riskan/v28y2008i2p497-512.html)).
Thomas, Bratvold and Bickel showed on real oil-and-gas matrices that
reversing the scoring direction or moving a bin edge reorders priorities,
and found no published study showing that matrices improve decisions
([UiS record](https://uis.brage.unit.no/uis-xmlui/handle/11250/183575);
[JPT summary](https://jpt.spe.org/inherent-flaws-risk-matrices-may-preclude-them-being-best-practices))
*(secondary)*. Independent domains have reached matching conclusions. The
AIAG-VDA FMEA handbook (2019) **replaced the RPN with an action-priority
lookup** because multiplying ratings lets a severity-10 and a severity-4
failure share a score
([BioProcess Online](https://www.bioprocessonline.com/doc/is-it-time-to-say-goodbye-to-fmea-risk-priority-number-rpn-scores-0001)).
FIRST frames CVSS base scores as severity inputs rather than risk *(recalled)*, and
vulnerability triage now layers exploitation evidence and decision trees on
top (§4.4). Basel's FRTB replaces 99% VaR with a stressed 97.5% expected
shortfall because VaR ignores tail size (§6.3). NASA budgets major projects
at a 70% joint cost–schedule confidence level from Monte Carlo (§3.4).

The limits of this shift matter as much as its direction. Gartner reports
that **85% of its assurance clients run more than one GRC tool and only
slightly more than half of ERM tool users are satisfied**
([Gartner, Sept 2025](https://www.gartner.com/en/newsroom/press-releases/2025-09-09-gartner-hype-cycle-highlights-rise-in-gen-ai-and-automation-as-legal-risk-and-compliance-leaders-tackle-global-regulatory-complexity)).
Those suites are still built around the register and the heat map
*(recalled)*. And the replacements' advantage is argued, not measured: the
case for them is that the old method is incoherent, not that the new one has
been shown to lower losses (§8).

### 1.2 Regulators stopped asking "how likely" and started asking "how bad, for how long"

Since 2021 the financial-sector regimes have shared three moves
*(inference, from §3.3)*. The unit of analysis became the **important
business service and its end-to-end dependencies** rather than the risk
event. The control target became an **impact tolerance** — a maximum
tolerable disruption that firms assume will be tested by a
severe-but-plausible scenario — rather than a probability-weighted loss
estimate. And **concentration in technology providers** became a systemic
risk regulated at the provider. The FCA states the distinction plainly:
impact tolerance assumes the disruption occurs and differs from both risk
tolerance and recovery-time objectives
([FCA insights](https://www.fca.org.uk/firms/operational-resilience/insights-observations/printable/print)).
Power argued in 2009 that business continuity management offered the clue
for rebuilding risk management after the crisis
([Power 2009, LSE](https://eprints.lse.ac.uk/36828/)); the 2021–26 regimes
read as that suggestion institutionalised *(inference)*.

The same move appears outside finance. An SRE error budget converts a
reliability target into an allowance for failure that **halts releases when
spent** — arguably the most operational quantified risk appetite in IT,
because it has an automatic consequence
([Google SRE, "Embracing Risk"](https://sre.google/sre-book/embracing-risk/))
*(recalled)*. Decision making under deep uncertainty (DMDU) replaces
"optimise expected value" with "find strategies that perform acceptably
across many futures" (§6.5). The Bank of England, after the 2024 Bernanke
review called its fan charts conceptually weak, moved toward scenario-based
presentation alongside a baseline (§7.2; one detail is in conflict, §9).

### 1.3 Safety and systemic-risk analysis moved from failed components to interactions

STAMP/STPA treats losses as the result of **unsafe control actions and
inadequate feedback**, not only component failures; ISO 21448 (SOTIF)
covers hazards that arise with no fault at all; Safety-II and FRAM study
how everyday performance variability both sustains and threatens safety
(§5). In finance, Haldane and May showed in a deliberately simplified model
that **homogeneity can minimise each bank's risk while maximising the
probability of system collapse**
([Haldane & May 2011, RePEc](https://ideas.repec.org/a/nat/nature/v469y2011i7330d10.1038_nature09659.html)).
The common lesson is that firm-level or component-level metrics, even good
ones, can mislead at system level. In regulated industries the systems
methods are **added at concept phase and the classic methods kept for
certification evidence** *(inference, §5.7)*.

### 1.4 Risk records are becoming living artifacts tied to monitoring

UL 4600 Ed. 3 requires a safety case maintained through the lifecycle with
defined safety performance indicators
([UL news](https://www.ul.com/news/ul-4600-edition-3-updates-incorporate-autonomous-trucking)).
Dynamic safety cases attach runtime monitors to the argument
([Denney, Pai & Habli 2015](https://core.ac.uk/works/24785939)). COSO's
February 2026 generative-AI guidance stresses **live AI use-case inventories
and continuous monitoring because outputs are probabilistic**
([Journal of Accountancy](https://www.journalofaccountancy.com/news/2026/feb/coso-creates-audit-ready-guidance-for-governing-generative-ai/)).
NIST's revised IR 8286 suite makes the cybersecurity risk register the unit
that rolls up into an enterprise risk portfolio
([NIST CSRC news](https://csrc.nist.gov/news/2025/nist-revises-ir-8286-suite-of-reports)).
Frontier AI developers publish periodic risk reports (§4.7). The open
problem common to all of these is **how to keep confidence in a living
argument after the system or its context changes**
([arXiv 2401.16633](https://arxiv.org/pdf/2401.16633)).

## 2 Standards and regulatory status on 2026-10-31

Each row is a status claim dated to the compile date. "Current" means the
passes found no published successor, not that none is in progress.

| Instrument | Status as of 2026-10-31 | Source |
| --- | --- | --- |
| ISO 31000:2018 Risk management — Guidelines | Current. Revision ISO/CD 31000 at "CD approved for registration as DIS" (2026-10-26); no DIS published. A new edition before 2027 is unlikely *(inference)* | [ISO 88574](https://www.iso.org/standard/88574.html) *(snippet)* |
| IEC 31010:2019 Risk assessment techniques (Ed. 2) | Current; no third edition found | [IEC webstore](https://webstore.iec.ch/publication/59809) |
| ISO/TS 31050:2023 Emerging risk and resilience | Current (Technical Specification, Ed. 1) | [SCC](https://scc-ccn.ca/standardsdb/standards/8186987) |
| ISO 31073:2022 Vocabulary | Current; replaced ISO Guide 73:2009 | [ISO 79637](https://www.iso.org/standard/79637.html) *(recalled)* |
| COSO ERM 2017 | Current; no revision found. GenAI internal-control guidance Feb 2026 (built on the 2013 IC framework) | [Journal of Accountancy](https://www.journalofaccountancy.com/news/2026/feb/coso-creates-audit-ready-guidance-for-governing-generative-ai/) |
| IIA Global Internal Audit Standards | Effective 9 Jan 2025. Topical Requirements: Cybersecurity 5 Feb 2026; Third-Party 15 Sept 2026; Organizational Behavior 15 Dec 2026 | [Plante Moran](https://www.plantemoran.com/explore-our-thinking/insight/2026/10/iias-topical-requirements-new-baseline) |
| PMBOK Guide 8th ed. | Published Nov 2025 | [ProjectManagement.com](https://www.projectmanagement.com/articles/1134510/pmi-launches-the-pmbok--guide---eighth-edition) *(snippet / secondary)* |
| ISO 9001:2026 | Published 16 Sept 2026; ~3-year transition | [NSF](https://www.nsf.org/in/en/knowledge-library/iso-90012026-published-on-september-16-2026-what-quality-leaders-need-to-know) |
| NIST CSF 2.0 | Final Feb 2024 | [NIST](https://www.nist.gov/cyberframework) *(recalled)* |
| NIST IR 8286 suite | Rev. 1 Dec 2025, aligned with CSF 2.0 | [NIST CSRC](https://csrc.nist.gov/news/2025/nist-revises-ir-8286-suite-of-reports) |
| NIST SP 800-30 Rev. 1 / SP 800-37 Rev. 2 | No final successor found | [800-30r1](https://csrc.nist.gov/pubs/sp/800/30/r1/final) *(recalled)* |
| ISO/IEC 27005:2022 | Current | [ISO 80585](https://www.iso.org/standard/80585.html) *(recalled)* |
| CVSS v4.0 | Published 1 Nov 2023 | [FIRST](https://www.first.org/blog/20241104-CVSS-v4_0-Turns-One-Year-Old) |
| EPSS v4 | Released March 2025 (16 or 17 March) | [Empirical Security](https://research.empiricalsecurity.com/research/introducing-epss-version-4) |
| NIST SSDF (SP 800-218) | v1.1 (Feb 2022) final; Rev. 1 (v1.2) initial public draft Dec 2025 | [NIST PDF](https://nvlpubs.nist.gov/nistpubs/specialpublications/nist.sp.800-218.pdf); draft *(secondary)* |
| SLSA | v1.2, 24 Nov 2025, adds a Source track | [SLSA blog](https://slsa.dev/blog/2025/11/announce-slsa-v1.2) |
| CISA SBOM Minimum Elements | Draft Aug 2025; whether a final replaced it in 2026 is disputed (§9) | [CISA](https://www.cisa.gov/news-events/news/cisa-issues-draft-software-bill-materials-guide-public-comment) |
| EU Cyber Resilience Act | In force Dec 2024; reporting duties from Sept 2026; full application Dec 2027 | [EUR-Lex](https://eur-lex.europa.eu/eli/reg/2024/2847/oj) *(recalled)* |
| EU DORA | Applies from 17 Jan 2025; 19 critical ICT third-party providers designated 18 Nov 2025 | [ESMA](https://www.esma.europa.eu/press-news/esma-news/european-supervisory-authorities-designate-critical-ict-third-party-providers) |
| UK Critical Third Parties regime | First four designations 10 Jul 2026, effective 13 Jul 2026 | [HSF Kramer](https://www.hsfkramer.com/notes/fsrandcorpcrime/2026-posts/hm-treasury-makes-first-designations-under-uk-ctp-regime) |
| Basel third-party risk principles | Final, published 10 Dec 2025 | [BIS](https://www.bis.org/press/p251210.htm) |
| FRTB (market risk, ES 97.5%) | EU proposed for 1 Jan 2027, with further relief toward ~2029/30 reported; UK internal models proposed for 1 Jan 2028; US re-proposal pending | [EC](https://finance.ec.europa.eu/news/commission-proposes-postpone-one-additional-year-market-risk-prudential-requirements-under-basel-iii-2025-06-12_en); [BoE CP17/25](https://www.bankofengland.co.uk/prudential-regulation/publication/2025/july/basel-3-1-adjustments-to-the-market-risk-framework-consultation-paper); [ISDA](https://www.isda.org/2026/01/12/striking-a-pragmatic-balance-on-eu-market-risk-capital/) |
| NIST AI RMF | 1.0 (Jan 2023) current; revision announced, no successor draft found | [NIST](https://www.nist.gov/itl/ai-risk-management-framework) |
| NIST AI 600-1 GenAI Profile | Final Jul 2024 | [doi](https://doi.org/10.6028/NIST.AI.600-1) *(recalled)* |
| ISO/IEC 42001:2023; 23894:2023; 42005:2025; 42006:2025 | Published | [ISO 42001](https://www.iso.org/standard/81230.html) *(recalled)*; [iso42k](https://www.iso42k.com/nist-ai-rmf-and-iso-42001) *(secondary)* |
| EU AI Act | Prohibitions from 2 Feb 2025; GPAI duties from 2 Aug 2025. Digital Omnibus (Reg. (EU) 2026/1744, in force 27 Jul 2026) moved Annex III high-risk duties to 2 Dec 2027 and Annex I to 2 Aug 2028. Art. 50 grace period disputed (§9) | [Gibson Dunn](https://www.gibsondunn.com/eu-ai-act-omnibus-agreement-postponed-high-risk-deadlines-and-other-key-changes/); [Orrick](https://www.orrick.com/en/Insights/2026/07/EU-AI-Act-Update-Digital-Omnibus-Finalizes-8-Compliance-Changes) |
| EU GPAI Code of Practice | Published 10 Jul 2025 | [European Commission](https://digital-strategy.ec.europa.eu/en/node/13953/printable/pdf) |
| OWASP Top 10 for LLM Applications | 2026 edition, early Aug 2026 | [CSA](https://labs.cloudsecurityalliance.org/research/csa-research-note-owasp-genai-top10-2026-agent-control-stand/) |
| OWASP Top 10 for Agentic Applications | Released 9 Dec 2025 | [OWASP](https://genai.owasp.org/resource/owasp-top-10-for-agentic-applications-for-2026) |
| Anthropic RSP / OpenAI Preparedness / GDM FSF | RSP v3.0 effective 24 Feb 2026, later versions to v3.4 (8 Jul 2026) listed; Preparedness v2 15 Apr 2025; FSF v3 date disputed (§9) | [Anthropic](https://www.anthropic.com/responsible-scaling-policy); [OpenAI PDF](https://cdn.openai.com/pdf/18a02b5d-6b67-4cec-ab64-68cdfbddebcd/preparedness-framework-v2.pdf) |
| IEC 61508 | Ed. 2 (2010) current; Ed. 3 in development, no date | [Genorma](https://genorma.com/en/standards/iec-61508-3-2010-ed2) |
| ISO 26262 | Ed. 3 targeted for ~2027 (no ISO statement found) | [UL SIS](https://www.ul.com/sis/blog/what-to-expect-with-version-3-of-iso-26262) *(secondary)* |
| ISO 21448:2022 (SOTIF); ISO/PAS 8800:2024 (AI in road vehicles) | Published | [ANSI preview](https://webstore.ansi.org/preview-pages/ISO/preview_ISO+21448-2022.pdf); [Model Engineers](https://model-engineers.com/en/blog/iso-pas-8800-the-safety-of-ai-in-road-vehicles/) |
| ISO 14971:2019; ISO/TR 24971:2020 | Current; the TR is under review (stage 90.92) | [ISO TR 24971](https://www.iso.org/ru/standard/74437.html) |
| SAE ARP4761A | Revised 20 Dec 2023 | [SAE](https://saemobilus.sae.org/standards/arp4761a-guidelines-conducting-safety-assessment-process-civil-aircraft-systems-equipment) |
| UL 4600 | Ed. 3, 17 Mar 2023 | [UL](https://www.ul.com/news/ul-4600-edition-3-updates-incorporate-autonomous-trucking) |
| GSN Community Standard | v3, May 2021 (SCSC-141C) | [SCSC](https://scsc.uk/scsc-141c) |
| IEC 60812:2018 (FMEA); IEC 61882:2016 (HAZOP); IEC 61025:2006 (FTA) | Current | [DIN](https://din.de/en/getting-involved/standards-committees/dke/publications/wdc-beuth:din21:261560539/DEFAULT/quote); [EVS](https://www.evs.ee/en/evs-en-61882-2016); [ANSI](https://webstore.ansi.org/standards/iec/iec61025ed2006) |
| SRA Glossary | "Updated August 2018"; no later revision found | [SRA](https://www.sra.org/wp-content/uploads/2020/04/SRA-Glossary-FINAL.pdf) |

## 3 Enterprise, governance and project risk: frameworks stood still while regulators moved

### 3.1 ISO 31000 and COSO remain the reference frameworks, and practice combines them

ISO 31000:2018 defines risk as **"the effect of uncertainty on objectives"**
and is organised as principles, framework and process; it is short, generic
and non-certifiable
([open-exam-prep comparison](https://open-exam-prep.com/study-guides/iso-31000-risk-manager/principles-and-core-concepts/comparing-iso-31000-with-other-frameworks))
*(secondary)*. COSO's 2017 *Integrating with Strategy and Performance*
starts instead from mission and strategy, with **five components and 20
principles**, and US listed companies gravitate to it through SOX alignment
([TechTarget](https://www.techtarget.com/it-strategy/definition/What-are-the-COSO-frameworks);
[Pojasek, CERM Academy](https://insights.cermacademy.com/215-left-coso-erm-2017-right-iso-310002018-bob-pojasek/)).
Common practice uses ISO 31000 for method and COSO for governance and board
reporting *(secondary)*; the two differ in orientation more than in
substance *(inference)*. COSO meets new risk areas through application
guidance mapped onto existing frameworks rather than new editions: its
February 2026 generative-AI guidance maps the 2013 internal-control
framework's 17 principles onto eight GenAI capability types and a six-step
roadmap (govern, inventory, assess, design, implement, monitor)
([IIA Internal Auditor](https://internalauditor.theiia.org/en/articles/2026/march/coso-issues-genai-guidance/)).
That it attaches to internal control rather than ERM suggests the audit
lens dominates COSO's recent output *(inference)*.

IEC 31010:2019 enlarged its catalogue of assessment techniques and added
guidance on validating their use
([IEC webstore](https://webstore.iec.ch/publication/59809)). It lists the
consequence/likelihood matrix as a **screening and ranking** technique
rather than a full analysis
([Antwerp paper citing it](https://repository.uantwerpen.be/docman/irua/5de90d/129721_2018_12_19.pdf))
*(secondary)*. The standards, in short, offer more quantitative tools than
common practice uses *(inference)*. ISO 9001:2026 keeps "risk-based
thinking" central and separates risks from opportunities more clearly
([NSF](https://www.nsf.org/in/en/knowledge-library/iso-90012026-published-on-september-16-2026-what-quality-leaders-need-to-know));
the 2015 edition's version of it, with no required method, drew complaints
of uneven implementation (an [ASQ forum thread](https://my.asq.org/discussion/what-went-wrong-with-the-risk-based-approach-in-iso-9001215),
low weight).

### 3.2 Governance structures are settled; their maturity is not

The IIA's Three Lines Model (2020) replaced "three lines of defense" with a
principles-based model of governing body, management and internal audit,
and was updated in September 2024 to the new Standards glossary
([IIA](https://www.theiia.org/ThreeLines)). The Global Internal Audit
Standards took effect on 9 January 2025 and are now supplemented by
**mandatory Topical Requirements** — cybersecurity and third-party risk
already effective, organisational behaviour due 15 December 2026
([UHY](https://www.uhy-us.com/insights/news/2026/may/from-principles-to-practice-the-next-phase-of-internal-audit-standards)).
These set a minimum audit scope on exactly the areas regulators target
*(inference)*. Risk-appetite practice still traces to the FSB's 2013
principles ([FSB](https://www.fsb.org/2013/11/principles-for-an-effective-risk-appetite-framework/))
*(recalled)*.

Self-assessed maturity is low and flat. In the NC State/AICPA 2025 survey of
273 US finance leaders, **61% reported rising risk complexity but only 32%
rated their risk oversight "mature" or "robust"** (30% the year before), and
about 35% reported comprehensive ERM processes
([NC State Poole](https://poole.ncsu.edu/thought-leadership/article/ongoing-uncertainty-growing-global-risks-continue-to-outpace-risk-management-processes-of-u-s-organizations/)).
No primary source or empirical evaluation of key risk indicator design was
found; KRIs appear in COSO, ISO and Basel guidance without a dedicated
standard.

### 3.3 Operational resilience in finance assumes the disruption and caps its impact

The Basel Committee's 2021 *Principles for Operational Resilience* cover
governance, risk management, continuity testing, **mapping
interdependencies**, third-party dependency, incident management and ICT
([BIS](https://www.bis.org/press/p210331a.htm)), and its revised
operational-risk principles added an ICT principle
([BIS FSI](https://bis.org/fsi/fsisummaries/psmor.htm)). UK firms had to
identify important business services, set an impact tolerance for each, map
and test them, and be able to stay within tolerance by 31 March 2025
([Sidley](https://datamatters.sidley.com/2025/01/07/uk-operational-resilience-rules-are-you-ready-for-31-march-2025/)).
The FCA observed that firms set **widely varying tolerances with "limited
rationale"**, mostly time-bound, and encouraged non-time metrics
([FCA](https://www.fca.org.uk/firms/operational-resilience/insights-observations/printable/print)).

Concentration is now regulated at the provider. The European Supervisory
Authorities designated **19 critical ICT third-party providers** under DORA
in November 2025, using registers of information that firms submitted and
assessing systemic importance and substitutability; the list includes the
major hyperscalers ([ESMA](https://www.esma.europa.eu/press-news/esma-news/european-supervisory-authorities-designate-critical-ict-third-party-providers);
[Morgan Lewis](https://www.morganlewis.com/de/blogs/sourcingatmorganlewis/2025/11/dora-eu-regulators-announce-list-of-critical-ict-third-party-providers)).
HM Treasury designated **four** providers under the UK regime in July 2026
([Linklaters](https://financialregulation.linklaters.com/post/102nbgo/uk-designates-four-tech-firms-as-critical-to-financial-system)),
and the Basel Committee finalised third-party risk principles built on a
third-party life cycle in December 2025
([BCBS d605](https://www.bis.org/bcbs/publ/d605.htm)). No outcome evidence —
fewer or shorter outages attributable to these regimes — was found; it is
probably too early.

### 3.4 Project risk: PMBOK restored a Risk domain, and confidence levels are set at portfolio level

PMI's PMBOK Guide 8th edition (November 2025) has 6 principles, **7
performance domains including a named Risk domain**, and 40 non-prescriptive
processes, partly reversing the principles-only 7th edition
([ProjectManagement.com](https://www.projectmanagement.com/articles/1134510/pmi-launches-the-pmbok--guide---eighth-edition);
[Learning Tree](https://www.learningtree.com/blog/pmbok-guide-8th-edition-whats-new/))
*(snippet / secondary)*. One practitioner reading stresses its distinction
between "overall project risk" and the sum of individual risks
([Project Management Compass](https://projectmanagementcompass.substack.com/p/inside-pmbok-8-risk-performance-domain))
*(secondary)*.

The clearest quantitative anchor is NASA's **Joint Cost and Schedule
Confidence Level**: a Monte Carlo simulation combining cost uncertainty,
schedule uncertainty and discrete risks, with policy to **budget at 70% JCL
and never below 50%**, written justification required below 70%, and the
rationale that projects at lower confidence still give agency-level ~70%
confidence ([NASA NTRS 2018](https://ntrs.nasa.gov/api/citations/20180005523/downloads/20180005523.pdf);
[NID 7120.122](https://nodis3.gsfc.nasa.gov/OPD_Docs/NID_7120_122_.pdf)).
It shows contingency being derived from a portfolio confidence target rather
than a percentage per risk *(inference)*. The base rates behind this are
stark: Flyvbjerg and Gardner's database of more than 16,000 projects
(explicitly not all megaprojects) has **8.5% on budget and on time and 0.5%
also delivering promised benefits**, with fat-tailed outcomes
([The Independent Review](https://www.independent.org/tir/2023-fall/how-big-things-get-done/))
*(secondary)*; IT cost overruns follow a power law
([Flyvbjerg et al. 2022](https://doi.org/10.1080/07421222.2022.2096544))
*(recalled)*. Those data are observational and come from the advocate of
reference-class forecasting, whose theoretical basis is Kahneman and
Lovallo's inside/outside view ([1993](https://doi.org/10.1287/mnsc.39.1.17))
*(recalled)*.

### 3.5 The evidence that ERM adds value is correlational and contested

Hoyt and Liebenberg found US insurers using ERM carry a **Tobin's Q premium
of about 20%**, with a treatment-effects model for self-selection
([CAS abstract](https://casact.org/abstract/value-enterprise-risk-management-evidence-us-insurance-industry)),
but also that ERM users are systematically larger, less leveraged and less
volatile ([JACF 2015](https://web.actuaries.ie/sites/default/files/erm-resources/31_Hoyt_et_al-2015-Journal_of_Applied_Corporate_Finance.pdf.pdf)).
Gatzert and Martin's review finds most studies positive but concentrated in
geography and industry ([FAU CRIS](https://cris.fau.de/publications/107876604)).
Mikes and Kaplan, from a decade of fieldwork and 250+ interviews, argue the
findings are **"inconsistent and inconclusive" because ERM is poorly
specified**, and propose a contingency theory
([SAI ERM database](https://web.actuaries.ie/press/erm-resource-database/towards-contingency-theory-enterprise-risk-management)).
Power's sociological critique calls ERM's security "at best limited to
certain states of the world and at worst illusory", shaped by "the logic of
the audit trail" ([LSE](https://eprints.lse.ac.uk/36828/)). The fair reading
is a positive correlation in mostly financial samples, with endogeneity
unresolved and "ERM" measured crudely (often by hiring a CRO)
*(inference)*. The practitioner phrase "risk register theatre" has no
systematic study behind it; its scholarly substance is Power's
audit-trail argument plus Cox's matrix results *(inference)*.

## 4 Cyber, IT and AI: registers roll up, scores became layered decisions, AI got its own stack

### 4.1 NIST and ISO converge on a register that rolls up into the enterprise portfolio

CSF 2.0 (February 2024) added a sixth function, **Govern**, covering risk
strategy, roles, oversight and supply-chain risk, and widened scope to all
organisations ([NIST](https://www.nist.gov/cyberframework)) *(recalled)*.
The IR 8286 suite, revised in December 2025, describes recording cyber risk
in **cybersecurity risk registers** and aggregating them into the
**enterprise risk portfolio** so leaders can track "both threats and
opportunities" ([IR 8286A](https://www.nist.gov/publications/identifying-and-estimating-cybersecurity-risk-enterprise-risk-management);
[CSRC news](https://csrc.nist.gov/news/2025/nist-revises-ir-8286-suite-of-reports)).
SP 800-221/221A extend the same model to all ICT risk
([SP 800-221](https://csrc.nist.gov/pubs/sp/800/221/final)) *(recalled)*.
The assessment methods underneath — SP 800-30 Rev. 1 (2012) and the RMF in
SP 800-37 Rev. 2 (2018) — have no final successors *(recalled)*. A CRQ
vendor reads the revised 8286 as friendly to monetary register entries
([Kovrr](https://www.kovrr.com/blog-post/quantified-cyber-risk-through-an-erm-lens-in-nist-ir-8286-rev-1))
*(vendor)*. ISO/IEC 27005:2022 aligns to ISO 31000 and adds an
**event-based (scenario-first)** identification route beside the classic
asset–threat–vulnerability route
([ISO](https://www.iso.org/standard/80585.html)) *(recalled)*. The direction
is from control-compliance baselines toward governance plus a comparable,
ideally quantified, register *(inference)*.

### 4.2 FAIR is the reference model for cyber quantification; its adoption evidence is self-reported

Open FAIR decomposes risk into loss event frequency (threat event frequency ×
vulnerability) and loss magnitude
([The Open Group](https://www.opengroup.org/certifications/openfair))
*(recalled)*. The FAIR Institute extended it with a controls model,
**FAIR-CAM v1.0 (January 2025)**, which models how control reliability and
coverage move frequency and magnitude rather than treating controls as a
checklist ([FAIR-CAM standard](https://www.fairinstitute.org/hubfs/Standards%20Artifacts/FAIR%20Controls%20Analytics%20Model%20(FAIR-CAM)%20Standard%20V1.0%20(January%202025).pdf)),
and a materiality model, **FAIR-MAM**, that splits loss into cost modules
for disclosure judgments ([FAIRCON25](https://www.fairinstitute.org/blog/faircon25-welcome-lifting-the-curtain-on-a-breakthrough-year-in-standards-education-and-collaboration)).
All three were packaged as a 2025 cyber risk management framework, and
FAIR-CAM is being applied to agentic AI
([FAIR Institute](https://www.fairinstitute.org/blog/taming-agentic-ai-risks-with-fair-cam)).
Outputs are Monte Carlo loss distributions shown as **loss exceedance curves
against a risk-tolerance curve**, the presentation Hubbard and Seiersen
advocate alongside calibrated 90% intervals and Bayesian updating
([Wiley](https://www.wiley.com/en-us/How+to+Measure+Anything+in+Cybersecurity+Risk%2C+2nd+Edition-p-9781119892304))
*(recalled)*.

All of these sources are self-published by the method's institute or by
vendors, and the adoption signals are training uptake and conference
traffic, **not evidence of operational use**
([FAIR Institute, Black Hat 2025](https://www.fairinstitute.org/blog/fair-institute-black-hat-usa-2025)).
No independent peer-reviewed critique of FAIR-CAM or FAIR-MAM was found. The
recurring critiques of CRQ — thin loss data and heavy reliance on
elicitation, fat tails where models are least validated, false precision in
single annualised figures, and systemic correlation that per-scenario models
miss — were recalled by the pass rather than read in an independent source
*(recalled)*. The July 2024 CrowdStrike update, which Microsoft estimated
crashed about 8.5 million Windows hosts, is the standard illustration of
that last point ([Microsoft](https://blogs.microsoft.com/blog/2024/07/20/helping-our-customers-through-the-crowdstrike-outage/))
*(recalled)*. FAIR is best described as **the dominant reference model for
CRQ, not as a demonstrably widespread practice** *(inference)*.

### 4.3 Threat modelling supplies the scenario list that quantification needs

Threat modelling is now framed by the Threat Modeling Manifesto's four
questions — what are we working on, what can go wrong, what will we do
about it, did we do a good job
([MDN](https://developer.mozilla.org/en-US/docs/Web/Security/Threat_modeling/Frameworks)).
The methods differ in what they enumerate: STRIDE (security threat
categories per data-flow element,
[Microsoft](https://learn.microsoft.com/en-us/azure/security/develop/threat-modeling-tool-threats),
*(recalled)*), LINDDUN (privacy threats,
[NIST resource listing](https://www.nist.gov/node/1608826)), PASTA (a
seven-stage risk-centric process ending in business impact,
[Wiley](https://www.wiley.com/en-us/Risk+Centric+Threat+Modeling%3A+Process+for+Attack+Simulation+and+Threat+Analysis-p-9780470500965),
*(recalled)*), attack trees (AND/OR goal decomposition with quantifiable
leaves, [Schneier 1999](https://www.schneier.com/academic/archives/1999/12/attack_trees.html),
*(recalled)*), and MITRE ATT&CK, whose threat-informed-defence project
overlays adversary techniques on defensive capabilities "to show residual
risk" ([CTID](https://ctid.mitre.org/blog/2024/07/08/turn-your-threat-model-to-supermodel-with-attack/)).
MITRE ATLAS extends the same idea to AI systems
([atlas.mitre.org](https://atlas.mitre.org/)) *(recalled)*. In a full
pipeline, threat modelling produces scenarios, ATT&CK coverage gives
evidence on control strength, and FAIR or SP 800-30 puts frequency and
magnitude on each scenario *(inference)*. No empirical evidence on
threat-modelling effectiveness or adoption was found.

### 4.4 Vulnerability prioritisation became a layered decision, and its data supply nearly failed

Risk-based vulnerability management now layers **severity** (CVSS v4.0,
November 2023, which renamed temporal metrics "Threat" and added a BTE
nomenclature, [FIRST](https://www.first.org/blog/20241104-CVSS-v4_0-Turns-One-Year-Old)),
**probability of exploitation** (EPSS v4, March 2025, the probability of
exploitation activity in the next 30 days, [Empirical Security](https://research.empiricalsecurity.com/research/introducing-epss-version-4)),
**evidence of exploitation** (CISA's KEV catalogue,
[CISA](https://www.cisa.gov/known-exploited-vulnerabilities-catalog),
*(recalled)*) and **stakeholder-specific decision trees** (SSVC, whose CISA
tree outputs Track / Track* / Attend / Act,
[CISA](https://www.cisa.gov/stakeholder-specific-vulnerability-categorization-ssvc),
*(recalled)*). KEV and EPSS have different data-generating processes, so a
KEV entry can legitimately carry a low EPSS score, and KEV is incomplete by
construction ([Risk-Based Prioritization guide](https://riskbasedprioritization.github.io/epss/EPSS_and_CISA_KEV/)).
EPSS v4 reportedly widened its ground-truth exploitation data roughly
six-fold ([Aptori](https://www.aptori.com/blog/epss-version-4-a-smarter-way-to-prioritize-vulnerabilities))
*(vendor)*. A 2026 preprint argues prioritisation should be evaluated as a
**temporal decision process** using only evidence available at each point,
because retrospective "was it exploited" scoring flatters models
([arXiv 2608.22306](https://arxiv.org/pdf/2608.22306)).

The data supply under this stack proved fragile. The MITRE CVE contract
nearly lapsed in April 2025 and was saved by an 11-month extension, and
board members formed a CVE Foundation in response
([CSO Online](https://www.csoonline.com/article/3963190/cve-program-faces-swift-end-after-dhs-fails-to-renew-contract-leaving-security-flaw-tracking-in-limbo.html)).
NVD enrichment backlog estimates conflict, from ~13,000 to over 30,000
records, and NIST reportedly moved to **risk-based enrichment** in April 2026
([NHI guide](https://nhimg.org/cve-programme-and-nvd-backlog-guide))
*(secondary)*. EPSS v4 now falls back on CNA/ADP data where NVD has no
score. A prioritisation stack hard-wired to NVD CVSS degrades silently when
enrichment lags, which is itself a risk-management finding *(inference)*.

### 4.5 Supply-chain risk moved from listing components to proving provenance

The stack has four parts: process attestation (SSDF), build and now source
provenance (SLSA), component transparency (SBOM plus VEX) and project-health
heuristics (OpenSSF Scorecard). SSDF v1.1 remains final; a Rev. 1 draft
from December 2025 reportedly adds continuous process improvement and
robust software updates ([NHI SSDF guide](https://nhimg.org/nist-secure-software-development-framework-guide))
*(secondary)*. **SLSA v1.2 (November 2025) added a Source track** for threats
in authoring, review and management, with build-environment and dependency
tracks still in development ([SLSA](https://slsa.dev/blog/2025/11/announce-slsa-v1.2)).
CISA's 2025 SBOM minimum-elements update adds component hash, licence, tool
name and generation context ([SC World](https://www.scworld.com/news/cisa-releases-draft-changes-to-sbom-minimum-requirements-for-comment));
its final status is in conflict (§9). Scorecard runs automated heuristic
checks and is meant as a risk signal, not a guarantee
([OpenSSF Scorecard](https://github.com/ossf/scorecard)) *(recalled)*. The
EU Cyber Resilience Act turns SBOMs and product risk assessment into a
market-access requirement, with reporting duties from September 2026
([EUR-Lex](https://eur-lex.europa.eu/eli/reg/2024/2847/oj)) *(recalled)*.

### 4.6 Reliability engineering treats acceptable risk as a budget with consequences

SLOs and error budgets, blameless postmortems
([Google SRE](https://sre.google/sre-book/postmortem-culture/)), chaos
engineering's hypothesis-driven fault injection
([principlesofchaos.org](https://principlesofchaos.org/)) and risk-based
testing under ISO/IEC/IEEE 29119
([ISO](https://www.iso.org/standard/79428.html)) are the reliability
repertoire; all of it is *(recalled)* and no 2024–26 standard revision was
checked. The point of interest for risk management is the contrast already
drawn in §1.2: an error budget is a quantified appetite **with an automatic
consequence**, where most cyber risk appetites are advisory
*(inference)*.

### 4.7 AI risk management has three layers: management systems, regulation, and capability thresholds

**Management-system and lifecycle standards.** NIST's AI RMF 1.0 organises
practice into Govern, Map, Measure and Manage across seven trustworthiness
characteristics ([NIST](https://www.nist.gov/itl/ai-risk-management-framework))
*(recalled for content)*; NIST's page says it is being revised under the July
2025 AI Action Plan, and no successor draft was found. Its Generative AI
Profile (AI 600-1) names **12 GenAI risks**, from confabulation to value-chain
integration ([doi](https://doi.org/10.6028/NIST.AI.600-1)) *(recalled)*.
ISO/IEC 42001:2023 is a certifiable AI management system with AI risk and
impact assessment ([ISO](https://www.iso.org/standard/81230.html))
*(recalled)*; 23894 adapts ISO 31000 to AI; 42005 (impact assessment) and
42006 (certification bodies) followed in 2025
([iso42k](https://www.iso42k.com/nist-ai-rmf-and-iso-42001)) *(secondary)*.

**Regulation.** The EU AI Act sorts systems into prohibited, high-risk,
transparency and minimal tiers, with separate duties for general-purpose
models and more for those with systemic risk
([EUR-Lex](https://eur-lex.europa.eu/eli/reg/2024/1689/oj)) *(recalled)*.
The Digital Omnibus on AI **delays enforcement without removing the
requirements**: Annex III high-risk duties to 2 December 2027, Annex I to
2 August 2028, public-authority legacy systems to 2 August 2030
([Cooley](https://cdp.cooley.com/digital-ai-omnibus-delays-key-deadlines-introduces-new-rules/);
[CSA](https://labs.cloudsecurityalliance.org/research/csa-research-note-eu-ai-act-omnibus-vii-deadline-delay-20260/)).
GPAI duties have applied since August 2025. The GPAI Code of Practice's
Safety & Security chapter, for systemic-risk models only, requires a
**Safety and Security Framework** and a **Model Report** before release
([CSET](https://cset.georgetown.edu/article/eu-ai-code-safety/)).

**Catalogues and thresholds.** The MIT AI Risk Repository codes **over 1,700
risks from 74 frameworks** under a causal taxonomy (entity, intent, timing)
and a domain taxonomy of 7 domains
([MIT, Dec 2025 update](https://airisk.mit.edu/blog/repository-update-december-2025)).
OWASP split application-level risk in two: the LLM Top 10 (2026 edition,
August 2026) covers the model inside an application and routes tool, memory
and autonomy risks to the **Top 10 for Agentic Applications** (December
2025, ASI01–ASI10) ([OWASP](https://genai.owasp.org/resource/owasp-top-10-for-agentic-applications-for-2026);
[CSA](https://labs.cloudsecurityalliance.org/research/csa-research-note-owasp-genai-top10-2026-agent-control-stand/)).
An OWASP Agent Control Standard exists at specification v0.1, which CSA
advises piloting rather than deploying (same CSA note).

Frontier developers' safety frameworks are a different **form** of risk
threshold: not likelihood × impact but **"if capability reaches a threshold,
then these safeguards are required, or no deployment"**, with evaluations as
leading indicators *(inference)*. Anthropic's RSP v3.0 (February 2026) was a
rewrite that **dropped the unilateral pause commitment**, separated
unilateral commitments from industry-wide recommendations, and introduced
Risk Reports; v3.2 lets the Long-Term Benefit Trust request external review
of them ([Anthropic](https://www.anthropic.com/responsible-scaling-policy/rsp-v3-0);
[GovAI](https://www.governance.ai/analysis/anthropics-rsp-v3-0-how-it-works-whats-changed-and-some-reflections)).
OpenAI's Preparedness Framework v2 (April 2025) collapsed its tiers into
**High** (safeguards before deployment) and **Critical** (safeguards during
development) ([OpenAI](https://cdn.openai.com/pdf/18a02b5d-6b67-4cec-ab64-68cdfbddebcd/preparedness-framework-v2.pdf));
one analysis reads it as moving from numeric to qualitative thresholds
([futureagi](https://futureagi.com/blog/frontier-model-safety-analysis-2026/))
*(secondary)*. Google DeepMind's framework is built on Critical Capability
Levels; its version history is in conflict (§9). METR catalogues the common
elements: capability thresholds, weight security, deployment mitigations,
halting conditions, elicitation, evaluation frequency, accountability
([METR](https://metr.org/common-elements)). The 2025–26 trend across them is
more judgement-based thresholds, unilateral pledges softened in favour of
transparency artifacts, and **quasi-mandatory status via the EU Code**
*(inference)*.

## 5 Safety and systems engineering: the classic toolkit was revised, systems methods were layered on

### 5.1 Classic techniques were revised for scoring flaws, not for their causal model

FMEA/FMECA (IEC 60812:2018), HAZOP (IEC 61882:2016, aligned to ISO 31000
terminology), fault tree analysis (IEC 61025:2006), LOPA (CCPS, tied to IEC
61511 SIL allocation) and PRA (NASA/SP-2011-3421, descended from nuclear
NUREG practice) are all current
([EVS](https://www.evs.ee/en/evs-en-61882-2016);
[NASA NTRS](https://ntrs.nasa.gov/citations/20120001369);
[Purdue LOPA deck](https://engineering.purdue.edu/P2SAC/presentations/documents/12.23.lopa)).
The most significant revision is the AIAG-VDA handbook's **replacement of
RPN by an Action Priority table** that weights severity first, then
occurrence, then detection, and outputs High/Medium/Low with obligations
attached ([ReliaMag](https://reliamag.com/guides/how-to-perform-fmea/))
*(secondary)*. Aviation's ARP4761A (December 2023) runs to 692 pages and
**explicitly excludes security threats**
([SAE](https://saemobilus.sae.org/standards/arp4761a-guidelines-conducting-safety-assessment-process-civil-aircraft-systems-equipment)).
These revisions address known scoring and terminology problems, not the
methods' shared assumption that accidents are chains of failures
*(inference)*; LOPA and PRA quantify from failure rates and so are weak
where none exists, as for software and design flaws *(inference)*.

The **bow-tie** — threats and prevention barriers left of a top event,
consequences and mitigation barriers right of it — has a hazy origin (ICI
course notes, 1979), was made Shell company standard in the early 1990s
after Piper Alpha, and has a reference text from CCPS and the Energy
Institute (2018) ([TÜV SÜD](https://solutions.tuv.com/wp-content/uploads/2018/10/practical-uses-of-bowtie-enhancing-traditional-pha-issue-1.0.pdf);
[Wolters Kluwer](https://www.wolterskluwer.com/en/solutions/enablon/bowtie/expert-insights/barrier-based-risk-management-knowledge-base/the-historie-of-bowtie)
*(vendor)*; [AIChE](https://www.aiche.org/ccps/resources/publications/books/bow-ties-risk-management-concept-book-process-safety)).
It is a fault tree and an event tree joined at the top event and simplified
for **barrier management rather than quantification** *(inference)*.

### 5.2 STPA finds more hazard causes than FMEA or FTA; the evidence is counts, not accidents

STAMP treats safety as a control problem. STPA (Handbook 2018) is its
forward analysis, CAST its accident analysis, STPA-Sec its security variant
([STPA Handbook](https://www.flighttestsafety.org/images/STPA_Handbook.pdf);
[Air University](https://www.airuniversity.af.edu/News/Display/Article/1714123/first-stpa-cybersecurity-and-safety-technical-interchange-hosted-by-afit/)).
Comparisons consistently report that STPA finds more interaction,
design-flaw and software causes: in a 21-student experiment it produced
more varied software safety requirements than FTA or FMEA but took novices
longer ([arXiv 1612.00330](https://arxiv.org/pdf/1612.00330)); in an
automotive case it found the FMEA causes plus design-flaw and interaction
causes FMEA missed ([Springer](https://link.springer.com/article/10.1007/s11219-017-9396-0));
on a radiotherapy device it found 99 unsafe control actions against 74 FMEA
failure modes ([MIT DSpace](https://dspace.mit.edu/handle/1721.1/105318)).
The evidence is **single-system case studies, MIT theses and one small
student experiment**, and "finds more" counts items found, not accidents
prevented *(inference)*. ISO 21448 lists STPA in informative Annex B, and
SAE J3187 gives recommendations for automotive use
([arXiv 2403.06680](https://www.arxiv.org/pdf/2403.06680)). STPA has been
extended to frontier-AI hazard analysis (2025) and loss-of-control risk
(2026) ([arXiv 2506.01782](https://arxiv.org/html/2506.01782v1);
[arXiv 2606.13474](https://arxiv.org/pdf/2606.13474)).

### 5.3 Assurance cases became the argument format for autonomy and ML

GSN v3 (2021, free under Creative Commons) and its SCSC guidance are the
notation reference ([SCSC-141C](https://scsc.uk/scsc-141c);
[SCSC-159](https://scsc.uk/SCSC-159)). UL 4600 Ed. 3 is goal-based and
technology-neutral, and adds a more precise safety performance indicator
definition and post-incident behaviour requirements
([ULSE](https://ulse.org/ul-standards-engagement/autonomous-vehicle-technology/autonomous-trucking-incorporated-third)).
York's **AMLAS** gives GSN patterns over six ML lifecycle stages (online
edition October 2025) and SACE covers system–environment interaction
([arXiv 2102.01564](https://arxiv.org/pdf/2102.01564);
[York](https://www.york.ac.uk/assuring-autonomy/research/research/)).
Dynamic safety cases keep the argument under runtime monitoring, but
**"providing ongoing confidence in the efficacy of the assurance measures"**
is an open challenge ([arXiv 2401.16633](https://arxiv.org/pdf/2401.16633)),
and retraining after a change of operating context undermines assurance
carried over from before (York). No evidence on whether assurance cases
catch flaws, or suffer confirmation bias, was found.

### 5.4 Domain standards each turn risk into an integrity target their own way

IEC 61508 uses SIL 1–4; ISO 26262 derives **ASIL A–D from severity ×
exposure × controllability**, with only S3+E4+C3 giving ASIL D and low
exposure able to lower the ASIL even for fatal, uncontrollable hazards
([arXiv 1804.04349](https://arxiv.org/pdf/1804.04349);
[Synopsys](https://www.synopsys.com/automotive/what-is-asil.html))
*(secondary; the authoritative table is ISO 26262-3)*. For driverless
vehicles controllability reportedly defaults to C3 and is under review
([arXiv 2410.23207](https://arxiv.org/pdf/2410.23207)). ISO 21448 covers
**hazards with no fault** — functional insufficiencies and foreseeable
misuse — and ISO/PAS 8800:2024 extends both to AI components
([Model Engineers](https://model-engineers.com/en/blog/iso-pas-8800-the-safety-of-ai-in-road-vehicles/)).
ISO 14971:2019 judges otherwise-unacceptable residual risk against benefit,
defined as health benefit and excluding economic advantage
([Greenlight Guru](https://www.greenlight.guru/blog/changes-to-iso-14971-2019-tr-24971)).
The UK ALARP framework (R2P2, 2001) sets **1 in 1,000 per year for workers
as the limit of tolerability and 1 in 1,000,000 per year as broadly
acceptable**, with risk between them tolerable only if ALARP
([IChemE Hazards XIX](https://www.icheme.org/media/9842/xix-paper-55.pdf);
[IAEA](https://nucleus.iaea.org/sites/graphiteknowledgebase/wiki/Guide_to_Graphite/Tolerability%20of%20Risk%20the%20ALARP%20Philosophy.aspx)),
and a measure is "not reasonably practicable" only at **gross
disproportion** of cost to benefit
([HSE](https://www.hse.gov.uk/foi/internalops/hid_circs/permissioning/spc_perm_37/)).
The split is between cheap, repeatable, coarse lookups (ASIL, risk graphs,
LOPA) and defensible explicit targets that need frequency data software and
ML do not have; SOTIF and PAS 8800 exist because failure-rate logic does
not cover a system working as designed with an insufficient specification
*(inference)*.

### 5.5 Resilience engineering reframed safety, and its framing is contested

Safety-II and FRAM (Hollnagel), High Reliability Organisations (Weick and
Sutcliffe, "authority follows expertise"), and drift into failure (Dekker:
organisations drift "precisely because they are doing well") reframe safety
as the capacity to succeed under varying conditions
([MRO](https://www.mro.net/wp-content/uploads/2022/07/Five-Principles-of-High-Reliability-Organizations-1.pdf);
[review of Dekker](https://codearcana.com/posts/2016/03/02/a-review-of-drift-into-failure.html)
*(secondary)*). Leveson argues the "Safety-I" Hollnagel describes bears
"very little or no resemblance" to 70 years of safety engineering and
proposes a systems-theoretic "Safety-III"
([Leveson 2020](https://maritimesafetyinnovationlab.org/wp-content/uploads/2021/12/MIT-Safety-III-A-Approach-to-Safety-and-Resilience-Leveson-2020.pdf));
a 2026 *Safety Science* paper calls the Safety-I contrast "contested" while
crediting it with moving patient safety away from linear error models
([White Rose](https://eprints.whiterose.ac.uk/id/eprint/243227/)). Both
camps reject linear failure-chain causality; the dispute is over framing
more than substance *(inference)*.

### 5.6 Safety–security co-analysis is immature, and ML assurance after deployment is unsolved

A review of 33 publications found safety–security co-analysis still
developing and concentrated on early phases
([MDU](https://www.es.mdu.se/publications/5298-Safety_and_Security_Co_Analyses__A_Systematic_Literature_Review)),
and a survey of **68 co-engineering methods found the field "far from
mature" with little tool support**
([PMC10975927](https://www.ncbi.nlm.nih.gov/pmc/articles/PMC10975927/)).
STPA-Sec on its own showed "notable shortcomings" for co-assurance
([arXiv 1903.01220](https://arxiv.org/pdf/1903.01220)). ISO 26262 Ed. 3 is
planned to address ML and align with ISO TS 5083 and PAS 8800
([UL SIS](https://www.ul.com/sis/blog/what-to-expect-with-version-3-of-iso-26262))
*(secondary)*. Generative AI is being trialled to automate hazard and risk
assessment ([arXiv 2410.23207](https://arxiv.org/pdf/2410.23207)).

### 5.7 The pattern is layering

Regulated industries keep the classic methods for certification evidence,
add STPA at concept phase, cover non-fault hazards with SOTIF, and put a
lifecycle assurance case with operational indicators on top *(inference)*.
**No study found links method choice to accident outcomes.**

## 6 Quantitative and decision-science methods: elicitation evidence is strongest, outcome evidence weakest

### 6.1 Risk science distinguishes risk from any one metric of it

The SRA Glossary lists **seven qualitative definitions of risk** and
separately a set of metrics (probability × severity, the Kaplan–Garrick
triplet, (C′, Q, K), expected disutility), warning that **"none of these
examples can be viewed as risk itself"** and that expected consequences are
informative "for large populations and individual risk, but not otherwise"
([SRA Glossary](https://www.sra.org/wp-content/uploads/2020/04/SRA-Glossary-FINAL.pdf)).
In (C′, Q, K), K is background knowledge including a judgement of its
**strength**; ISO 31000's definition reads as a special case of two of the
seven. The glossary also defines robustness through info-gap — goals
achieved "despite large info-gaps" — which links it to §6.5. Aven's
industry checklist asks whether the strength of knowledge behind assigned
probabilities is assessed and reported
([Aven slides](https://offshorenorge.no/globalassets/dokumenter/drift/presentasjonerarrangementer/well-integrity-seminar-2017/6.0-risk-management---terje-aven-uis.pdf)).
This position is **normative**: no study was found showing that
strength-of-knowledge annotations improve decisions.

### 6.2 The case against matrices is analytical; the replacement stack is argued, not trialled

Beyond Cox and Thomas et al. (§1.1), Budescu and colleagues showed that
readers interpret verbal probability terms far more variably than intended,
and that numeric ranges beside the words narrow the gap
([Budescu et al. 2009](https://doi.org/10.1111/j.1467-9280.2009.02284.x))
*(recalled)*; matrix axes usually rest on such words. Commentators note Cox's
own scope condition — matrices can be useful when probability and
consequence are positively correlated — and that the paper is often
misread as a blanket condemnation
([Wolters Kluwer](https://www.wolterskluwer.com/en/expert-insights/what-is-in-a-number-risk-matrices-and-how-we-are-being-misled)).
The critics' recommended stack is consistent: **elicit calibrated ranges,
run Monte Carlo, present loss exceedance curves against a stated tolerance,
use value of information to decide what to measure next**
([ICAEW guide](https://icaew.com/technical/business/strategy-risk-and-innovation/risk-management/risk-management-guide))
*(guidance)*. Hubbard, its most prominent advocate, also sells training and
consulting, and the HDR random-number generator in the SIPmath standard is
his ([ProbabilityManagement.org](https://probabilitymanagement.org/blog/2021/9/6/the-sipmath-30-standard-and-analytic-solver-v20215));
that is a conflict-of-interest note, not a reason to dismiss the argument.
**No randomised comparison of losses under matrix-based versus quantitative
risk management exists** in what the passes found.

### 6.3 Tail measures, dependence and network contagion are the post-2008 lessons

FRTB replaces 10-day 99% VaR with a **stressed 97.5% expected shortfall**
because VaR ignores tail size
([AnalystPrep](https://analystprep.com/study-notes/?p=737)) *(secondary)*.
The 97.5% level was chosen to roughly equal 99% VaR under normality, so ES
exceeds it under fat tails
([BPI](https://bpi.com/why-is-the-frtb-expected-shortfall-calculation-designed-as-it-is/)).
ES is coherent where VaR is not
([Artzner et al. 1999](https://doi.org/10.1111/1467-9965.00068)) but is not
elicitable, which makes it harder to backtest
([Gneiting 2011](https://doi.org/10.1198/jasa.2011.r10138)) *(both
recalled)*; FRTB still backtests against VaR. Repeated postponement through
2026 reflects coordination to avoid competitive disadvantage, not a dispute
over the method *(inference)*. Basel III's 2017 reforms replaced the
operational-risk advanced measurement approach with a standardised one,
partly over model variability
([BCBS d424](https://www.bis.org/bcbs/publ/d424.htm)) *(recalled)*.
Network models (§1.3) argue that system-level risk can rise as each firm's
falls; companion commentary warned that simple models predict unreliably
([Discover](https://www.discovermagazine.com/planet-earth/how-was-the-financial-meltdown-of-2008-like-a-collapsing-ecosystem)).
No empirical comparison of model performance across these methods was
found; they are tools with known theoretical properties, not
evidence-ranked methods.

### 6.4 Structured expert judgement has the best empirical base — with contested effect sizes

Cooke's classical-model database spans **49 studies (2006–2018) with 530
experts** ([Cooke, Marti & Mazzuchi 2021](https://doi.org/10.1016/j.ijforecast.2020.06.007)).
In the 33 studies Colson and Cooke analysed, **fewer than a third of
individual experts were statistically accurate**
([Colson & Cooke 2018](https://doi.org/10.1093/reep/rex022)).
Performance weighting beat equal weighting in-sample in 32 of the 33 studies
and on an out-of-sample index in 26 of 33
([Colson & Cooke 2017](https://doi.org/10.1016/j.ress.2017.02.003)); other
summaries of the out-of-sample comparison disagree on how large the
advantage is. The IDEA
protocol (investigate, discuss, estimate, aggregate) is a remote-capable
modified Delphi ([Methods blog](https://methodsblog.com/2018/03/27/idea-protocol-2/)).
In the Good Judgment Project, probability training, teaming and tracking the
top 2% each improved accuracy
([Mellers et al. 2014](https://www.psychologicalscience.org/journals/psychological-science/0956797614524255/)),
about 70% of superforecasters kept their status the next year
([AI Impacts](https://aiimpacts.org/evidence-on-good-forecasting-practices-from-the-good-judgment-project/))
*(secondary)*, and extremizing aggregates helped
([Satopää et al.](https://arxiv.org/pdf/1501.06943)). A **2025 item-response
reanalysis found that modelling extraneous variables "substantially
eliminated, reduced, and, in some cases, even reversed" the teaming and
training effects** ([Psychological Science 2025](https://www.psychologicalscience.org/journals/psychological-science/09567976241266481/)).
The defensible reading: unaided experts are overconfident, structured
elicitation with feedback and mathematical aggregation beats informal
consensus, and forecasting skill is measurable and partly stable — but a
short training course is **not settled evidence** of creating it
*(inference)*.

### 6.5 Deep uncertainty methods trade optimality for robustness and adaptivity

Where parties cannot agree on the system model, the input distributions or
the valuation of outcomes, DMDU methods — Robust Decision Making, Dynamic
Adaptive Planning, Dynamic Adaptive Policy Pathways, Info-Gap, Engineering
Options Analysis — seek strategies that perform acceptably across many
futures and plan adaptations with signposts and triggers
([Marchau et al. 2019, open access](https://link.springer.com/book/10.1007/978-3-030-05252-2)).
DAPP underlies Dutch Delta Programme planning
([Haasnoot et al. 2013](https://doi.org/10.1016/j.gloenvcha.2012.12.006))
*(recalled)*. Uptake is in water and climate adaptation; little was found in
enterprise or financial risk. Weak strength of knowledge in the SRA sense is
exactly the regime where DMDU recommends robustness over expected-value
optimisation *(inference)*. Evidence of better outcomes is necessarily weak
because the futures planned for have not happened; the claims are about
process quality and avoided regret.

### 6.6 The biases are well replicated; the debiasing workshops are not

Heuristics and biases ([Tversky & Kahneman 1974](https://doi.org/10.1126/science.185.4157.1124))
and the dread/unknown dimensions of risk perception
([Slovic 1987](https://doi.org/10.1126/science.3563507)) are foundational
*(recalled)*. The pre-mortem rests on **essentially one controlled study**:
178 participants, in which it reduced plan overconfidence more than critique
or pros/cons ([ScienceBlog on Veinott, Klein & Wiggins 2010](https://scienceblog.com/g-stay-a-step-ahead-premortem-technique-prospective-hindsight-research/))
*(secondary)*; it has not been shown to improve real outcomes
([Rework summary](https://resources.rework.com/libraries/project-management/pre-mortem)).
No controlled evidence on red teaming or on reference-class forecasting
versus conventional estimates was found. Taleb's fat-tail critique is
accepted as a warning against Gaussian thinking but faulted for adding
little operational method ([Aldous review](https://www.stat.berkeley.edu/~aldous/157/Books/Black_Swan.pdf);
[Kay review](https://www.johnkay.com/?p=27981)); Taleb himself limits it to
fat-tailed domains ([Taleb](https://risknet.de/elibrary/paper/black-swans-and-the-domains-of-statistics)).
It has been absorbed in tamer forms: ES over VaR, strength-of-knowledge
qualifiers, DMDU robustness *(inference)*.

### 6.7 Tooling splits into probabilistic programming and spreadsheet "probability management"

The Bayesian workflow (Gelman et al.) codifies iterative building, checking
and comparison of models, and explicitly leaves "after-inference decision
making" out of scope ([arXiv 2011.01808](https://arxiv.org/pdf/2011.01808)).
SIPmath 3.0 stores simulation results as shareable data (Metalog
distributions, a cross-platform reproducible random stream, a copula layer
for dependence), frozen in 2022
([ProbabilityManagement.org](https://probabilitymanagement.org/blog/2021/9/6/the-sipmath-30-standard-and-analytic-solver-v20215);
[2022 report](https://www.probabilitymanagement.org/s/2022-PM-Annual-Report.pdf))
*(advocacy)*. Its distinctive idea — distributions as auditable data that
aggregate across units — has no independent evaluation in what was found.

## 7 Visualization catalogue

Each table gives what the visual encodes and the data it needs, the
interaction that serves it, the evidence behind it, and how it fails. The
"data shape" and "interaction" entries are *(inference)* unless cited; they
are written for whoever builds analytics tooling.

### 7.1 The ordinal matrix and its repairs

The 5×5 coloured matrix remains the most common enterprise risk visual
*(recalled)* and is still taught in, for example, ICAO safety management
material ([ICAO](https://icao.int/TrainingDocumentation/SMI%20Website/Chapter%202%20Safety%20Management%20Fundamentals/2.5-12.%20SRM%20Methodology_Risk%20Matrix.pdf)).
The one controlled design study is Sutherland, Recchia, Dryhurst and
Freeman: two online RCTs with UK quota samples (n = 2,699). A matrix whose
cells **grow toward the high-risk corner beat text** on risk comparisons,
while standard and square-cell matrices did not differ from text;
**geometric axis labels (1, 5, 25, 125, 625) had the largest effect**, and
putting the key on the axes helped matrices but hurt text. Effects were
generally small, and readers familiar with matrices preferred the linear
format. The authors conclude "risk matrices are not always superior to
text" and recommend "ordinal, explicitly nonlinear scale labels"
([PMC9544625](https://pmc.ncbi.nlm.nih.gov/articles/PMC9544625/)). Duijm
recommends monotone colouring, logarithmic scales and continuous colour
gradients instead of bands
([via Antwerp paper](https://repository.uantwerpen.be/docman/irua/5de90d/129721_2018_12_19.pdf))
*(guidance)*. Preliminary evidence suggests cell colour itself shifts
perceived risk (Proto et al. 2023, reported second-hand in the Sutherland
record).

| Visual | Encodes / data shape | Interaction | Evidence | Failure modes |
| --- | --- | --- | --- | --- |
| 5×5 P–I matrix (heat map) | Ordinal likelihood × impact bins; dots or IDs per risk | Click cell → risk list; hover → detail | Analytical critique (Cox; Thomas et al.); one design RCT (Sutherland et al.) | Range compression; rank reversal; arbitrary bins and scoring direction; L×I arithmetic on ordinals; red/green inaccessible |
| Matrix with geometric labels, growing cells, key on axes | Same, with numeric band labels ("1-in-10/yr", "£1m–£10m") | As above | **Empirical** improvement in comprehension, small effects | Still ordinal underneath; familiarity bias toward the linear version |
| Continuous log–log P–I plot with ranges | Each risk as a box or ellipse spanning likelihood and impact intervals; iso-risk lines of slope −1 | Toggle inherent / residual / target; time slider | Guidance only; no controlled study found | Requires numeric intervals the register may not hold |
| Inherent → residual arrows; target marker | Two or three positions per risk | Toggle per control | Guidance only | Implies precision in control effect |
| Velocity / trend markers | Speed of onset or direction since last period, as marker size or ▲▼ | Period comparison | Guidance only (COSO discusses velocity, [COSO](https://www.coso.org/enterprise-risk-management) *(recalled)*) | Adds a third ordinal channel to an already ordinal plot |
| Appetite curve on continuous axes | Curved iso-risk boundary instead of staircase bands | Draggable boundary | Guidance only | Hard edge invites containment thinking (§7.3) |

### 7.2 Distributional visuals: one sample, several views

Loss exceedance curves, catastrophe-model exceedance-probability curves,
S-curves, P-value tables, histograms and tornado charts are **views of the
same object**: an LEC is one minus the CDF and an S-curve is the CDF
*(inference)*. They all need simulation samples or a quantile table, not a
score.

| Visual | Encodes / data shape | Interaction | Evidence | Failure modes |
| --- | --- | --- | --- | --- |
| Loss exceedance curve (LEC); insurance EP curve | P(annual loss ≥ x) against loss x (log-x); Monte Carlo samples per scenario. Overlay inherent, residual and a tolerance curve ([FAIR Institute](https://www.fairinstitute.org/blog/announcing-loss-exceedance-charts-in-the-fair-u-training-app); [Verisk](https://docs.risksolutions.verisk.com/TouchstoneRe/10.0/ts-tsre_all/help_tsre_analysis_exceedance-probability-curve.html)) | Hover read-out of (x, P>x); draggable tolerance curve; log-axis toggle | Guidance; no readability study for executives found | Tail is where models are least validated; one curve hides model uncertainty |
| S-curve with P50/P80; contingency = Pxx − base | Cumulative probability vs cost or finish date from a cost-loaded schedule simulation | Brush a percentile → contributing iterations | Guidance (NASA practice, §3.4) | Correlation between activities often under-modelled *(inference)* |
| JCL scatter | Simulated (cost, date) pairs with a confidence frontier ([NASA NTRS 2025](https://ntrs.nasa.gov/api/citations/20250002084/downloads/UnderstandingResultsIntegratedCostScheduleRisk_JKJ.pdf)) | Select frontier point → budget/date pair | Institutional policy, not an evaluation | Unfamiliar to non-specialists |
| Tornado chart | Output swing per input, sorted by width; needs per-input samples | Bar click → filter | Guidance | One-at-a-time swings ignore interactions; correlation-based versions conflate variance with leverage |
| F-N curve | Log–log cumulative frequency F of accidents with ≥ N fatalities; criterion lines for intolerable / ALARP / broadly acceptable ([Risktec for Traficom](https://traficom.fi/sites/default/files/17845-L6_Risktec.pdf)) | Compare installations | Guidance; HSE has "not formally declared a policy on F-N curves apart from the one point in R2P2" ([HSE/UKOPA note](https://ukopa.co.uk/wp-content/uploads/2025/07/012-HSE-UKOPA-Societal-Risk-Meeting-30-March-07-1.pdf)) | Cumulative form hides shape; slope encodes risk aversion, and the common −1 is not independently endorsed by R2P2 ([IChemE Hazards XXII](https://www.icheme.org/media/9272/xxii-paper-60.pdf)); anchor figure unverified (§9) |
| Fan chart | Central projection with probability bands, skew allowed | — | Bernanke review (2024) found "weak conceptual foundations" and recommended scenarios ([CNBC](https://www.cnbc.com/2024/04/10/bernanke-review-how-the-former-fed-boss-could-shake-up-the-bank-of-england.html); [MNI](https://www.mnimarkets.com/mni-bernanke-review-tells-boe-to-shift-to-scenario-analysis)) | Bands condition on one model and one policy path; cannot show structural uncertainty |
| Scenario set beside a baseline | A few named, internally consistent futures | Scenario toggle | The BoE moved toward this in 2026 ([BoE speech, June 2026](https://www.bankofengland.co.uk/speech/2026/june/alan-taylor-speech-at-the-barclays-and-centre-for-economic-policy-research)); one detail in conflict (§9) | No probabilities attached; readers may treat one scenario as the forecast *(inference)* |
| Hurricane cone of uncertainty | Area containing the track centre ~2/3 of the time; says nothing about size or impact | — | **Empirical**: novices misread it as the storm growing; ensembles reduce that but create a point-on-line bias ([Padilla et al. 2017](https://www.sciencedaily.com/releases/2017/10/171005151140.htm)); locations just inside the edge judged far more at risk than just outside ([Ruginski et al.](https://journalpub.escholarship.org/cognitivesciencesociety/article/25925/galley/15549/download)) | Containment heuristic; NHC keeps revising the product (inland warnings operational in 2026, [NHC](https://www.nhc.noaa.gov/pdf/2026NHCNewProductsAndServices.pdf)) |

### 7.3 Uncertainty formats with experimental evidence

This is the best-evidenced corner of the subject. The replicated findings
are that **frequency-framed displays help both lay and expert readers**,
that **intervals are misread** (within-the-bar bias, binary containment,
effect inflation when only inferential uncertainty is shown), and that every
format has a characteristic bias. A 2026 review warns that the field lacks a
shared definition of "uncertainty", which produces conflicting results
([Mason et al., arXiv 2411.10482](https://arxiv.org/abs/2411.10482)); a
survey of 86 user studies found evaluations assume more statistical
judgement than people exercise
([Hullman et al. 2018](https://www.tableau.com/research/publications/pursuit-error-survey-uncertainty-visualization-evaluation)).

| Visual | Encodes / data shape | Interaction | Evidence | Failure modes |
| --- | --- | --- | --- | --- |
| Quantile dotplot | n dots, each at a quantile of the predictive distribution | Static | **Empirical**: more precise probability estimates than densities ([Kay et al. 2016](https://mucollective.northwestern.edu/project/when-ish-is-my-bus)); in an incentivised task, payoffs ≈97% of optimal, ~5 points above no-uncertainty control, CDFs nearly as good, both beating textual intervals ([Fernandes et al. 2018](https://mucollective.northwestern.edu/project/uncertainty-bus)) | Dot count is a design choice; coarse for extreme tails *(inference)* |
| Hypothetical outcome plots (HOPs) | Animated draws from the distribution | Animation speed and frame count | **Empirical**: better than error bars and violins for multivariate judgements such as P(B > A) ([Hullman et al. 2015](https://doi.org/10.1371/journal.pone.0142444)) *(recalled)* | Anchoring on a few frames; not printable |
| CDF plot | Cumulative probability; answers threshold questions | Hover read-out | **Empirical**: nearly as good as dotplots for decisions (Fernandes et al.) | Unfamiliar to lay readers *(inference)* |
| Icon array / natural frequencies | Count of affected per N | Static | **Empirical**: improves understanding of risk reductions and reduces denominator and base-rate neglect, most for low-numeracy readers ([SMDM 2007](https://smdm.confex.com/smdm/2007pa/techprogram/P3596.HTM); [PMC2775772](https://pmc.ncbi.nlm.nih.gov/articles/PMC2775772); [Garcia-Retamero & Cokely 2017](https://doi.org/10.1177/0018720817690634) *(recalled)*) | Awkward for very small probabilities *(inference)* |
| Gradient / violin plots | Density shown continuously | Static | **Empirical**: reduce within-the-bar bias relative to bar + error bar ([Correll & Gleicher 2014](https://doi.org/10.1109/TVCG.2014.2346298)) *(recalled)* | Density shape hard to read numerically |
| Bar with error bars | Point estimate and interval | — | **Empirical**: within-the-bar bias and binary in/out reading (Correll & Gleicher) | Avoid for risk quantities |
| 95% CI vs 95% prediction interval | Parameter uncertainty vs outcome variability | Label which one | **Empirical**: CIs led participants to pay ~60% more for a treatment and overestimate its effect ([Hofman, Goldstein & Hullman 2020](https://www.hullmanlab.northwestern.edu/paper/2020/02/01/visualizing-inferential-uncertainty.html)); experts too, in preregistered follow-ups ([Microsoft Research](https://www.microsoft.com/en-us/research/?p=959307)), whose outcome measure has been criticised ([Gelman](https://statmodeling.stat.columbia.edu/2023/08/16/confusions-about-inference-prediction-and-probability-of-superiority/)) | Showing inferential intervals when the decision is about outcomes |
| Ensemble (spaghetti) display | Individual simulated paths | Member count | **Empirical**: fewer size misreadings than summaries, but point-on-line bias (Padilla et al. 2017) | Forecasters choose how many members to draw |

Two further findings shape practice. Authors omit uncertainty because they
fear it costs credibility ([Hullman 2019](https://doi.org/10.1109/TVCG.2019.2934287))
*(recalled)*. And a dual-process account explains the errors: salient visual
features such as boundaries trigger heuristics that effortful reading must
override, so design should make the decision-relevant feature the salient
one ([Padilla et al. 2018](https://doi.org/10.1186/s41235-018-0120-9))
*(recalled)*.

### 7.4 Structural and causal visuals

These show how a loss arises rather than how big it is. They share one data
model — **typed nodes, typed directed edges, attributes** (probability,
barrier status, owner, link to evidence) — with notation-specific layouts
*(inference)*. **No controlled study comparing their comprehension was
found**; their value claims come from standards bodies and practitioners.

| Visual | Encodes / data shape | Interaction | Evidence | Failure modes |
| --- | --- | --- | --- | --- |
| Bow-tie | Threats → prevention barriers → top event → mitigation barriers → consequences; escalation factors on barriers | Barrier nodes coloured by live inspection or KRI status | Guidance (CCPS/EI 2018) | Barrier inflation (listing training as a barrier); static wallpaper when unlinked to barrier data |
| Fault tree / event tree | AND/OR DAG to basic events with probabilities; minimal cut sets. Event tree: branching on safety-function success | Expand/collapse; cut-set highlight | Standard method (IEC 61025; NASA PRA guide) | Needs failure rates software lacks |
| STPA control structure | Hierarchy of controllers, control actions down, feedback up | Select control action → unsafe control actions and scenarios | Case-study evidence for the analysis, none for the diagram | Not probabilistic; can grow large |
| GSN assurance case | Goals, strategies, solutions, context, assumptions, justifications | Solution nodes linked to test evidence | Guidance | Confirmation bias untested; argument goes stale after change (§5.3) |
| Attack tree / attack–defence tree | Attacker goal decomposed AND/OR; leaves with cost or feasibility | Map leaves to ATT&CK techniques and detection coverage | Guidance | Enumeration incompleteness *(inference)* |
| Bayesian network | DAG with conditional probability tables; node marginals as bar monitors | Set evidence → propagate → posterior bars; per-node sensitivity | Guidance | Elicitation burden for CPTs *(inference)* |
| Risk interconnection map | Nodes are risks sized by perceived severity; edges are survey-identified links (WEF Global Risks Report 2026, >1,300 respondents, [WEF](https://www.weforum.org/publications/global-risks-report-2026/infographics/); [ERA](https://era.gv.at/news-items/world-economic-forum-publishes-global-risks-report-2026/)) | Hover node → linked risks | Descriptive | Force-directed "hairball"; edges are perception counts, not causal estimates |

### 7.5 Monitoring visuals

Monitoring compares indicators with thresholds over time. Good practice
converges on **indicator against appetite band, with trend**, rather than a
single RAG light *(guidance)*. BCBS 239 requires risk reports to be
accurate, comprehensive, clear and useful and to "balance data with
qualitative interpretation" ([BIS](https://www.bis.org/publ/bcbs239.pdf))
*(recalled)*. No empirical study of KRI dashboard formats, risk radars or
ATT&CK heat maps for decision quality was found.

| Visual | Encodes / data shape | Interaction | Evidence | Failure modes |
| --- | --- | --- | --- | --- |
| KRI tile / bullet graph | Current value vs target with qualitative bands ([Few, bullet graph spec](https://www.perceptualedge.com/articles/misc/Bullet_Graph_Design_Spec.pdf) *(recalled)*); long-format time series plus a **versioned** thresholds table | Drill from tile → indicator history → contributing records; annotate breaches | Guidance | Historical breaches drawn against today's threshold when thresholds are not versioned *(inference)* |
| Risk burndown / stacked exposure area | Total exposure over iterations | Stack by risk | Practitioner convention, no source | Summing ordinal scores |
| Risk radar / bullseye | Rings = horizon or proximity; sectors = category; dots = risks | Filter by category | Guidance | Area distortion; axis-order dependence |
| Treemap of exposure | Rectangles sized by exposure through a hierarchy | Zoom into hierarchy | Guidance | Area judged less accurately than length *(recalled perceptual ranking)* |
| ATT&CK coverage heat map | Tactic × technique matrix scored for detection or control coverage ([ATT&CK Navigator](https://github.com/mitre-attack/attack-navigator) *(recalled)*) | Layers, gradients, comments | Guidance | Coverage cells say nothing about efficacy; techniques not equally likely; a green matrix invites false assurance |
| Control-coverage matrix | Risks × controls or controls × frameworks; cell = mapped / tested / effective, with test date | Filter by gap | Guidance | Mapping is not effectiveness *(inference)* |
| Incident timeline | Loss events on a time axis, dot size = loss, control changes annotated, KRI overlay | Swimlanes per business line | Guidance | Survivorship in loss data *(inference)* |

### 7.6 Executive and analyst tiers, and the anti-patterns

The recommended split *(guidance)* is an executive tier that answers **"are
we within appetite, what changed, what decision is needed"** with few,
threshold-referenced or frequency-framed visuals carrying explicit
uncertainty — one LEC or S-curve per decision with its tolerance line, a
short top-risks list with ranges and trend arrows, scenarios rather than a
single forecast under deep uncertainty — and an analyst tier with full
distributions, structure and drill-down, both drawing on the same data so
the numbers agree. The anti-patterns, with the strength of what backs each:

| Anti-pattern | Backing |
| --- | --- |
| Matrices with linear 1–5 labels hiding orders of magnitude | **Empirical** (Sutherland et al.) |
| Verbal likelihood words without numbers | **Empirical** (Budescu et al.) |
| Inferential intervals shown for outcome decisions | **Empirical** (Hofman et al.) |
| Uncertainty only as a hard-edged band | **Empirical** (containment: Padilla, Ruginski) |
| Multiplying, summing or averaging ordinal scores | **Analytical** (Cox; Hubbard & Evans, [doi](https://doi.org/10.1147/JRD.2010.2042914) *(recalled)*) |
| RAG overuse; "watermelon" reporting; red/green palettes | Guidance |
| Single-point scores or annualised losses to the dollar | Guidance |
| Pie charts of register entries by category | Guidance (a count is not exposure) |
| 3D and pie charts generally | Perceptual-ranking evidence, extrapolated to risk *(recalled)* |

### 7.7 What the catalogue implies for a data model

*(inference throughout)* Three storage decisions recur across §7.1–§7.6.
**Store simulation samples, one row per iteration, aligned across risks**,
rather than percentiles: aligned samples can be summed per iteration to
aggregate while preserving correlation, and every distributional view in
§7.2 recomputes from them; stored percentiles cannot be aggregated
correctly. **Store likelihood and impact as numeric intervals**, and render
the 5×5 matrix as one binning of them, so banding choices are visible and
changeable and the same data plots on continuous axes. **Version thresholds
and appetite statements** so each historical value renders against the
threshold in force at the time. Structural notations need a property graph
with typed nodes and edges plus links to monitoring data. A tool that
refuses a point estimate without an interval, and labels interval type
(predictive or inferential), guards against two of the best-evidenced
failure modes. The quantitative vocabulary (LEC, S-curve, tornado) and the
GRC vocabulary (register, heat map, KRI tiles) live in separate products in
what the passes saw; one store serving both is a gap a pass inferred, not a
sourced claim.

## 8 Evidence quality: what is measured, what is argued, what is advocated

| Claim | Kind | Strength and caveat |
| --- | --- | --- |
| Ordinal matrices rank risks inconsistently | Analytical | Strong as logic; Cox's own scope condition (positive correlation) is often dropped |
| Quantitative replacements produce better outcomes | Guidance / advocacy | No controlled outcome study found; leading advocates sell training and tools |
| Matrix design (geometric labels) improves comprehension | Empirical | Two RCTs, small effects, online samples; comprehension, not decisions |
| Frequency framing beats interval summaries | Empirical | Replicated across labs; field-wide definitional noise (Mason et al.) |
| Unaided experts are overconfident; structured aggregation helps | Empirical | Large databases, partly by the method's authors; out-of-sample weighting gain contested |
| Training and teaming create forecasting skill | Empirical, contested | 2014 RCT headline weakened or reversed by 2025 reanalysis |
| ERM raises firm value | Empirical, correlational | Selection and measurement problems; mostly insurers and listed firms |
| STPA finds more hazard causes than FMEA/FTA | Empirical, weak | Case studies, theses, one student experiment; counts, not accidents |
| Pre-mortems reduce overconfidence | Empirical, single study | No outcome evidence |
| Reference-class forecasting corrects optimism | Observational | Large datasets from the method's advocate; no controlled comparison |
| FAIR / CRQ adoption is widespread | Self-reported | Institute and vendor sources only |
| Resilience regimes reduce disruption | None found | Probably too early to measure |
| DMDU yields better decisions | Case studies | Futures have not occurred; claims are about process and regret |
| AI-enabled GRC and continuous monitoring improve outcomes | Vendor surveys | E.g. 72% of CISOs report having implemented continuous controls monitoring ([Hyperproof](https://hyperproof.io/2025-it-compliance-benchmark-report/)) *(vendor)*; Gartner's independent signal is tool sprawl and middling satisfaction |

The pattern is consistent across domains: **the psychology of judgement and
of reading uncertainty displays is the best-measured layer; the
organisational methods built on it are the least measured.** The claims that
a newer method is better are, almost everywhere, either analytical
arguments against the older method or counts of what the newer method finds.

## 9 Open items and unresolved conflicts

These were left open on the compile date. Each is a place where sources
disagree or where a status could not be confirmed on the issuing body's
page.

| Item | What the sources say | What would settle it |
| --- | --- | --- |
| Google DeepMind Frontier Safety Framework v3 date | Secondary sources date v3.0 to 17 Apr 2026, one naming v3.1 current ([comparativeai](https://comparativeai.org/companies/google-deepmind/safety-framework/)); a pass's recollection is v3.0 on 22 Sept 2025, with the April 2026 date possibly a later revision | The framework's page on deepmind.google |
| EU AI Act Art. 50 transparency after the Omnibus | One source says Art. 50 is not amended ([Usercentrics](https://usercentrics.com/knowledge-hub/eu-ai-act-high-risk-delay-article-50-transparency-consent/)); another reports a marking grace period to 2 Dec 2026 ([Technology.org](https://www.technology.org/2026/10/13/eu-ai-act-deadlines-after-the-digital-omnibus-what-applies-from-december-2026-to-august-2028/)) | Reg. (EU) 2026/1744 text in the Official Journal |
| CISA 2025 SBOM Minimum Elements | Draft Aug 2025 ([CISA](https://www.cisa.gov/news-events/news/cisa-issues-draft-software-bill-materials-guide-public-comment)); a vendor says a final with 23 elements replaced the 2021 baseline on 29 Jul 2026 ([sbomify](https://sbomify.com/compliance/)) *(vendor)*; other 2026 sources still call it a draft | cisa.gov |
| Bank of England April 2026 report and the central forecast | A June 2026 MPC speech compares a February 2026 baseline with April 2026 scenarios and says the central forecast "will remain central" ([BoE](https://www.bankofengland.co.uk/speech/2026/june/alan-taylor-speech-at-the-barclays-and-centre-for-economic-policy-research)); a search summary claimed the April report contained nothing describable as a central forecast. Whether the fan chart was formally retired is unconfirmed | The April 2026 Monetary Policy Report itself |
| HSE R2P2 F-N anchor point | Commonly cited as ≥50 fatalities in a single event more often than 1 in 5,000 per year being intolerable; not verified on a primary HSE page (the research report page returned 404) | R2P2 text |
| NIST Cyber AI Profile (IR 8596) | Preliminary draft 16 Dec 2025 ([NHI](https://nhimg.org/nist-ai-rmf-guide)) vs initial public draft 19 Aug 2026 ([safeaiforbusiness](https://safeaiforbusiness.com/blog/nist-ai-rmf-update-2026-what-changed-and-what-your-business-must-do-now)) | csrc.nist.gov |
| NIST SSDF Rev. 1 draft content | Reported to add PO.6 and PS.4; not confirmed on NIST's site | csrc.nist.gov |
| NIST risk-based NVD enrichment (15 Apr 2026) and backlog size | Secondary source only; backlog estimates range ~13,000 to 30,000+ | nvd.nist.gov announcements |
| CVSS ≥ 7 coverage figure | A widely repeated "fix ~57% to cover ~82% of exploited" figure attributed to FIRST; not traced to a FIRST source, so not used above | FIRST EPSS publications |
| Colson & Cooke out-of-sample result | Summaries differ on whether performance weighting wins out-of-sample overall | The paper |
| FRTB capital impact | A 63.2% average increase for Group 1 banks appears in study notes without a dated monitoring exercise; not used above | BCBS Basel III monitoring report |
| GPAI Code signatories | One source cites 22 signatories as of 26 Oct 2026, with xAI signing only the Safety & Security chapter; other lists differ | The Commission's signatory page |
| Smaller date conflicts | EPSS v4 16 vs 17 Mar 2025; DORA designation 18 vs 19 Nov 2025; OWASP LLM 2026 on 3 vs 4 Aug; IIA cybersecurity requirement "January" vs 5 Feb 2026; GIAS standards count 53 vs 54; MIT repository 24 vs 25 subdomains | Issuing bodies' pages |
| Not retrieved | ISO/CD 31000 scope of changes; NIST AI RMF revision content; Anthropic RSP v3.3/v3.4 changes; CEN-CENELEC JTC 21 harmonised standards status; ISO 21448 and ISO/SAE 21434 revision status; McShane, Nair & Rustambekov (2011) on whether ERM adds value beyond traditional risk management | — |

## 10 What follows for a reader

The useful reframing is that "state of the art" in risk management now
names **a direction of revision rather than a method**: from scores toward
distributions, from probability toward tolerated impact, from components
toward interactions, from documents toward monitored records. Each domain
moves along those lines at its own pace, and nearly always by adding a
layer while the regulator or certifier still asks for the old one. Anyone
choosing a method should expect to run both, and should judge a new method
by whether its argument against the old one is sound, since outcome evidence
for the new one will usually not exist.

For anyone building tooling, the evidence points in an unexpected direction:
the firmest results are not about which risk method to use but about **how
people read uncertainty** — frequency framing, numeric labels, predictive
rather than inferential intervals, soft rather than hard boundaries. Those
results transfer to any method's output. A store that keeps intervals and
simulation samples rather than ordinal cells can render the familiar heat
map and the quantitative views from one source, which lets an organisation
move between the two without re-collecting its data *(inference)*.
