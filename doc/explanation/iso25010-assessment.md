---
type: explanation
audience: contributor
status: stable
reviewed-by: "p@stergiotis"
reviewed-date: 2026-09-14
---

# How boxer's bets meet the ISO 25010 product-quality model

A reading of the boxer repository's premises, paradigms, methods, and standing commitments against the nine product-quality characteristics of ISO/IEC 25010 — grading each pairing as facilitating, neutral, or counteracting, with the evidence drawn from the repository's own documents, ADR corpus, gates, and trials.

## Properties

| field | value |
| --- | --- |
| repo | github.com/stergiotis/boxer @ main (7d0870b8) |
| assessed | 2026-08-24 |
| corrected | 2026-09-14 — fn4's read-side figures did not survive checking against the trials they cited, so Performance efficiency × P3 moved from counteracting to neutral (Σ +42 → +43); fn1's trial tier and §b's data-tier sweep were corrected for a tier that was descoped. Every other figure was re-verified at 7d0870b8 and stands. |
| standard | ISO/IEC 25010:2023 product quality model |
| sources | [README](../../README.md), [why-boxer](./why-boxer.md), [ARCHITECTURE](../ARCHITECTURE.md), [CODINGSTANDARDS](../../CODINGSTANDARDS.md), [ENGINEERING_PRACTICES](../ENGINEERING_PRACTICES.md), 199 ADRs, [doc/trials](../trials/README.md), `scripts/ci`, code survey |
| companion | [iso25010-temperament.md](./iso25010-temperament.md) — the timeless reading: dispositions and tendencies, no figures; this page holds the dated evidence |

## Assessments

One row per reading. A figure here never outlives the row that produced it.

| date | tree | what it did |
| --- | --- | --- |
| 2026-08-24 | `7d0870b8` | Initial assessment: the fourteen-practice × nine-characteristic matrix, the footnotes, and the section-d marginals. |
| 2026-09-14 | `7d0870b8` (re-read) | Every figure re-checked against the tree at the assessed commit. Three did not survive: fn 4's read-side ratios, fn 1's trial tier, and the data-tier sweep in §b — all three leaning on a tier the trials had descoped. Performance efficiency × P3 moved from counteracting to neutral (Σ +42 → +43). The other ~32 checkable figures stand. |

## a — The grading matrix

Rows are the nine top-level characteristics of ISO/IEC 25010:2023 (the revision boxer's own trials protocol classifies findings against). Columns are the fourteen practices, premises, and bets that carry the project. Grades judge the *directional influence of the practice on the product quality characteristic*. A superscript marks a cell whose load-bearing caveat is in the footnotes; the recorded rationale for every graded cell is listed under [The rationales](#the-rationales).

### The practices

- **P1 · Dependency sovereignty** — Vendor, port, or reimplement every load-bearing dependency; airgapped builds; SBOM + license gate; the `extbin` chokepoint; signed tags.
- **P2 · Description languages & codegen** — Small problem-oriented languages (leeway plans, fffi2 IDL, nanopass passes); ~28% of the Go tree generated, checked in, drift-gated. Codegen over reflection.
- **P3 · One data spine** — Everything durable lands facts-shaped in ClickHouse; a new fact kind is a DTO plus vocabulary, never a schema migration; the model is machine-readable at runtime.
- **P4 · Dogfooding & self-measurement** — The toolkit observes itself; metrics, profiles, review findings, provenance, and ADR implementation-degree land as queryable data.
- **P5 · Mechanical sympathy** — Structure-of-arrays end to end, cache-conscious layout, pre-allocation discipline, render budgets as design defaults.
- **P6 · One architect, machine-checked** — Single author, AI-assisted, no external contributions; correctness carried by a verification mesh and recorded intent instead of reviewer headcount.
- **P7 · Tiered interfaces** — Polished classic UI for simple tasks; deliberately disposable prototypes for medium ones; agentic machine-readable surfaces for complex ones.
- **SC1/2 · Memory-safe pair, quarantined engine** — Go + Rust first-party tree; C-derived code sandboxed as WASM; ClickHouse deliberately outside the boundary as a separate process.
- **SC3 · Immediate-mode UI over framed FFI** — egui, explicitly not a web stack; one interpreter body shared verbatim across desktop, remote, and appliance hosts.
- **ADR · Decision record & design-before-code** — 199 ADRs, Diátaxis docs, QOC analysis, honest-status discipline, decision↔code traceability as data.
- **ENF · Enforcement-as-code & default-deny** — Bespoke gates (codelint CS001–CS012, designlint, capslock, glyph coverage, wasm parity), mandatory registries (env vars, extbin, entry points), fail-closed contracts (ADR-0082/0142/0145).
- **TRI · Trials & measured claims** — Reproducible measurement protocols with logbooks and ISO-25010-classified findings; 131 benchmarks; opt-in profiling; TLA+/Quint formal models.
- **TBD · Trunk-based, CI at release only** — Direct-to-main, no PRs or branch protection; every workflow triggers on tags, schedules, or dispatch — never on push; staticcheck/errcheck warn-only.
- **RET · Retirement discipline, alpha surface** — No compatibility shims, no lying stubs, wire-incompatible cuts in one commit; alpha maturity, no deprecation policy yet.

### The matrix

`+` facilitating · `○` neutral / balanced · `−` counteracting. A superscript links the footnote carrying that cell's load-bearing caveat; every recorded rationale is listed under *The rationales* below.

| ISO 25010 ↓ · practice → | P1 | P2 | P3 | P4 | P5 | P6 | P7 | SC1/2 | SC3 | ADR | ENF | TRI | TBD | RET | Σ |
| --- | :-: | :-: | :-: | :-: | :-: | :-: | :-: | :-: | :-: | :-: | :-: | :-: | :-: | :-: | --- |
| **Functional suitability** | ○ | + | +¹ | + | ○ | ○ | + | ○ | ○ | + | + | + | −² | −³ | 7+ 5○ 2− **+5** |
| **Performance efficiency** | ○ | + | ○⁴ | + | + | ○ | ○ | + | + | ○ | ○ | + | ○ | ○ | 6+ 8○ 0− **+6** |
| **Compatibility** | −⁵ | ○ | +⁶ | ○ | ○ | ○ | + | + | −⁷ | ○ | ○ | + | ○ | − | 4+ 7○ 3− **+1** |
| **Interaction capability** | ○ | −⁸ | −⁸ | + | ○ | − | + | ○ | +⁹ | + | + | + | ○ | − | 6+ 4○ 4− **+2** |
| **Reliability** | + | ○¹⁰ | + | + | ○ | −¹¹ | ○ | + | ○ | ○ | + | + | − | ○ | 6+ 6○ 2− **+4** |
| **Security** | + | ○ | + | + | ○ | ○ | ○ | + | + | + | + | ○ | −¹² | ○ | 7+ 6○ 1− **+6** |
| **Maintainability** | +¹³ | + | + | + | −¹⁴ | + | ○ | ○ | ○ | + | + | + | ○ | + | 9+ 4○ 1− **+8** |
| **Flexibility** | + | + | + | ○ | + | ○ | ○ | + | + | ○ | + | + | ○ | ○ | 8+ 6○ 0− **+8** |
| **Safety** | ○ | ○ | ○ | ○ | ○ | ○ | ○ | ○ | ○ | + | + | + | ○ | ○ | 3+ 11○ 0− **+3** |
| **Σ practice (net)** | +3 | +3 | +5 | +6 | +1 | −1 | +3 | +5 | +3 | +5 | +7 | +8 | −3 | −2 | **Σ +43** |

Safety row: boxer is a data-engineering toolkit, not a safety-related system, so ISO 25010's Safety characteristic applies only by analogy; cells are graded only where a practice concretely implements a fail-safe, hazard-warning, or risk-identification behaviour, and left neutral elsewhere.

### The rationales

One line per graded cell that carries a reason; a cell with no line was graded without a recorded rationale. This content was hover text in the page this document replaces.

#### Functional suitability

*completeness · correctness · appropriateness*

| practice | | rationale |
| --- | :-: | --- |
| P2 | + | One source of truth per surface: a generator defect is loud and central; golden files and drift gates pin emitted behaviour. |
| P3 | +¹ | One model keeps memory, wire, and storage semantically identical; the trial loop caught silent read defects and produced ADR-0171. |
| P4 | + | The pipeline is exercised end to end on the project's own workload before any external one touches it. |
| P6 | ○ | The verification mesh carries correctness while single-author throughput bounds completeness — roughly a wash. |
| P7 | + | Appropriateness by design: interface investment matched to task complexity. |
| ADR | + | QOC and design-before-code weigh alternatives before code exists; appropriateness is argued, not assumed. |
| ENF | + | Golden corpora (Go↔SQL bit-identical), the stateful-widget gate, and the default-deny result-frame contract catch wrong behaviour at build/test time. |
| TRI | + | Fourteen queries reproduce a cross-engine oracle; findings are classified by functional correctness and completeness. |
| TBD | −² | Lint and test gates are not a merge barrier: they run on tags, schedules, and dispatch only. |
| RET | −³ | Capabilities are removed outright (parquet, puffin) rather than shimmed; completeness yields to honesty. |

#### Performance efficiency

*time · resources · capacity*

| practice | | rationale |
| --- | :-: | --- |
| P2 | + | Codegen over reflection: no runtime reflection cost; per-call-site buffer memoization is generated in (ADR-0049). |
| P3 | ○⁴ | A balanced cell, and the balance is measured: at 10M reads are at parity with native JSON held to the same declared schema (0.72–1.09×) at 0.958× its storage, and 1.03–1.38× against an indexed reference; the cost is on write — ~2× on insert in the fs store, and loosely so. |
| P4 | + | Opt-in continuous profiling; the slow-frame signal gates on what the app can actually control. |
| P5 | + | SoA end to end, cache-conscious layout, pre-allocation discipline, render budgets as design defaults — efficiency as a default property. |
| SC1/2 | + | A columnar C++ engine does the heavy queries; native Go/Rust hosts; warm chlocal workers at 7.8 ms p50 vs 41.3 ms cold. |
| SC3 | + | Immediate mode with measured frame costs (1.22 ms/frame software raster; reactive cadence cut idle CPU from 96.8% to 0.1% of a core). |
| TRI | + | Performance is a measured claim: trials with acceptance gates, 131 benchmarks, a DFA cache bounded by actual size (ADR-0084). |

#### Compatibility

*co-existence · interoperability*

| practice | | rationale |
| --- | :-: | --- |
| P1 | −⁵ | The positioning states it: ecosystem breadth and a stable API are traded away. Standard escape hatches remain (Arrow, SQL, CBOR, CycloneDX). |
| P2 | ○ | House description languages, but they emit standard targets: SQL DDL, Arrow, CBOR. |
| P3 | +⁶ | Second-substrate trial: all fourteen queries reproduce on DuckDB and DataFusion within a 2–3× band — with four measured gaps. |
| P7 | + | The agentic tier exists so machines can interoperate with the stack through introspectable, machine-readable surfaces. |
| SC1/2 | + | ClickHouse co-exists as a separate self-hosted process speaking standard HTTP, SQL, and Arrow IPC. |
| SC3 | −⁷ | Not a web stack, by commitment; the browser is reached through a video/mesh transport, not the DOM. |
| TRI | + | Technology-neutrality is itself a measured property (the leeway-second-substrate trial exists to test it). |
| RET | − | Wire-incompatible cuts ship in one commit; no dual-format grace periods, no compatibility shims. |

#### Interaction capability

*learnability · operability · inclusivity · …*

| practice | | rationale |
| --- | :-: | --- |
| P2 | −⁸ | Each description language must be learned here — nothing transfers from elsewhere. |
| P3 | −⁸ | The spine fronts every task: backbone/payload, sections, and memberships must be learned before the leverage appears. |
| P4 | + | The system self-describes at runtime: dashboards, introspection tables, queryable state. |
| P6 | − | No support channel, no roadmap influence — user assistance is explicitly not offered. |
| P7 | + | The premise is interaction-capability policy: polish where discoverability pays, disposable UIs where assembly speed pays. |
| SC3 | +⁹ | A design system enforced as code: OKLCh tokens, an Okabe-Ito colour-blind-safe palette gate, glyph-coverage gate, accessibility-tree export. |
| ADR | + | For its developer-users, Diátaxis plus generated references keep documentation findable, executable, and honest. |
| ENF | + | Design lint, glyph coverage, and palette gates make UI consistency and inclusivity CI properties. |
| TRI | + | Operability and self-descriptiveness are finding categories in the trials protocol — usability is measured, not assumed. |
| RET | − | Alpha surface: APIs break, sometimes in batches; there is no deprecation policy yet. |

#### Reliability

*faultlessness · availability · tolerance · recovery*

| practice | | rationale |
| --- | :-: | --- |
| P1 | + | No service tether: nothing rented can disappear; the supply-chain compromise class is reduced (xz-utils is the cited case). |
| P2 | ○¹⁰ | A wrong generator is wrong everywhere at once — offset by loud central fixes and drift gates on every generated file. |
| P3 | + | Append-only facts, one durable store, and commit protocols where a snapshot is complete exactly when its root row exists. |
| P4 | + | Defects in the spine surface on the project's own screens before external workloads hit them. |
| P6 | −¹¹ | A bus factor of one, mitigated but not removed by the records. |
| SC1/2 | + | Memory safety removes whole crash classes; the one large unsafe component is quarantined out-of-process. |
| SC3 | ○ | One interpreter body across hosts prevents divergence; a mismatched id stack still compiles clean and panics at render. |
| ENF | + | Default-deny by construction: a missing terminal frame reads as incomplete; a non-loopback bind fails closed and loud. |
| TRI | + | Bounded-workload rules for live-server tests; behaviour under load is measured, not assumed. |
| TBD | − | Nothing gates an ordinary push; staticcheck and errcheck are warn-only; a red main is possible by design. |

#### Security

*confidentiality · accountability · resistance · …*

| practice | | rationale |
| --- | :-: | --- |
| P1 | + | A minimized, audited trust surface: SBOM + license gate, the extbin chokepoint, signed tags, airgapped builds. |
| P3 | + | Accountability by architecture: grants, audit, and lifecycle land in one queryable, append-only fact trail. |
| P4 | + | The audit trail is the same machinery the product ships; provenance is queryable from history. |
| P5 | ○ | A small amount of unsafe, performance-motivated code exists and must carry its justification. |
| P6 | ○ | Provenance trailers and adversarial review on one side; no independent second reviewer on the other. |
| P7 | ○ | The agentic tier inherits LLM failure modes — stated, and leaned against the verification mesh. |
| SC1/2 | + | Memory-safe first-party tree; C-derived code sandboxed as WASM; the C++ engine kept outside the safety boundary. |
| SC3 | + | No DOM/JS injection surface; the browser receives pixels and meshes, and the served page carries no secret. |
| ADR | + | Claims are scoped, not inflated: capabilities are 'hygiene, not security'; ADR-0087 refuses to claim isolation it does not have. |
| ENF | + | capslock capability cross-check, fail-closed bind gate, sensitive-variable redaction, AEAD-sealed ad-hoc data, registry-derived confinement. |
| TBD | −¹² | Direct-to-main with no branch protection concentrates write authority; accountability is preserved, control is not. |

#### Maintainability

*modularity · analysability · modifiability · testability*

| practice | | rationale |
| --- | :-: | --- |
| P1 | +¹³ | Everything load-bearing is in-tree and auditable — at the price of carrying maintenance an ecosystem would amortize. |
| P2 | + | The editing surface is the description, not the ~263k generated lines; a schema change is a regeneration, not a hand-edit sweep. |
| P3 | + | A new durable fact kind is a DTO plus vocabulary entries, never a schema migration. |
| P4 | + | Code volume, coverage, review state, provenance, and ADR implementation-degree are queryable data about the repo itself. |
| P5 | −¹⁴ | 'Data-oriented shapes are less ergonomic than idiomatic object graphs' — an admitted modifiability cost. |
| P6 | + | Recorded intent replaces tribal knowledge: 199 ADRs, adversarial-review markers, drift-triggered re-review. |
| SC3 | ○ | The custom UI stack is owned maintenance burden, but the IDL, headless drivers, and frame-hash captures make it unusually testable. |
| ADR | + | Analysability par excellence: append-only decisions, supersession discipline, decision↔code traceability as data. |
| ENF | + | Naming, error style, iterators, mutexes, env access, exec access — machine-checked (CS001–CS012); drift gates on all generated code. |
| TRI | + | Claims that decay must be dated or live; frozen numbers are never refreshed — the record stays trustworthy. |
| TBD | ○ | Small single-concern commits with rationale help analysis; the absent merge gate means regressions can sit until release. |
| RET | + | Removal goes all the way through the public API; no shims, no lying stubs, and the rationale is captured in an ADR. |

#### Flexibility

*adaptability · scalability · installability · replaceability*

| practice | | rationale |
| --- | :-: | --- |
| P1 | + | Offline and airgapped installability; a single binary; 109–119 MB appliance images. |
| P2 | + | One IDL emits Rust, Go, and docs; codecs regenerate against new targets (the anchor schema proved generator portability). |
| P3 | + | The model is the durable investment: projections adapt it; the second substrate ran it on DuckDB and DataFusion. |
| P5 | + | SoA shapes scale with the columnar engine; measured across 1M→100M tiers with a footprint ratio stable to 0.2%. |
| SC1/2 | + | CGO_ENABLED=0 static Go; the musl-static path cleared; the engine replaceable in principle at a measured 2–3× cost band. |
| SC3 | + | The same app source runs unchanged on desktop, browser, and appliance — placement is the host's call, not the app's. |
| ENF | + | Per-package wasm-amenability (packageprops) makes portability a drift-checked property of every package. |
| TRI | + | Replaceability is tested, not asserted: the second-substrate trial exists to measure it. |

#### Safety

*fail safe · hazard warning · risk identification*

| practice | | rationale |
| --- | :-: | --- |
| ADR | + | Risk identification as practice: every premise names its failure mode; withdrawn and deferred ADRs record dead ends; unbuilt security is labelled 'decided but unbuilt'. |
| ENF | + | Fail-safe defaults (fail-closed binds, default-deny contracts); hazard warnings live at the knob (sensitive env vars document their own hazard). |
| TRI | + | Severity-classified findings; formal TLA+/Quint models including deliberately unsafe variants checked for divergence. |

### Footnotes — the load-bearing caveats

1. **Functional suitability × P3.** The spine's correctness is earned, not free: the [jsonbench-on-facts](../trials/jsonbench-on-facts/README.md) trial — 10M tier, the 100M tier having been descoped — found two *silent* read defects in leeway's own read path. The grade is facilitating because the finding loop exists and produced a structural fix (ADR-0171 names the single read surface); the same evidence shows the defect class is real.

2. **Functional suitability × TBD.** All seven CI workflows trigger on `v*` tags, weekly schedules, or manual dispatch — never on push or PR. The documented compensation is "local is the working gate", which relies on author discipline rather than mechanism.

3. **Functional suitability × RET.** ADR-0195 removes an opcode entirely rather than leaving a no-op, "a stub that lies to its caller"; ADR-0202 removes parquet support and its flags. Deliberate completeness reductions in exchange for an honest surface.

4. **Performance efficiency × P3.** Measured by the project itself, and — per the trial's own rule — no figure travels without the pair of arms it compares ([jsonbench-on-facts](../trials/jsonbench-on-facts/README.md) §0, 10M tier, the five backbone paths `MATERIALIZED`): against native `JSON` declaring the same five typed paths and no index, facts runs the five queries at **0.72–1.09×** the latency — parity — at 0.99–1.15× its peak memory and 0.958× its storage; against that reference *plus* a clustered index on exactly the five queried columns, 1.03–1.38× slower. The 1M tier is a harness smoke test, not evidence, and the trial disclaims by name the reading that the model is 5–16× slower than native JSON — that was its own open-coded read path, and rewriting the query removed all of it. So the counteraction sits on the write side: the [fs snapshot store](../trials/fs-snapshot-store-m0/README.md) measures the facts shape "roughly 2× slower" on insert at 1 MiB blocks, loosely — six repeats ranged 1.3×–5.4× and the trial declares the ratio unpinned, with a re-run on a quiet host as its own open milestone — while the 184 side columns compress to 39 KiB beside 7.7 MiB of block data. Neither trial is reviewed or replicated: one workstation, one run per configuration. Hence the neutral grade: measured read parity and a favourable storage ratio against a real but unpinned write cost. This cell read *counteracting* until 2026-09-14, on a read-side tax the trials it cited do not support.

5. **Compatibility × P1.** The foil is stated in the positioning: boxer trades "ecosystem breadth and a stable API" for a stack one team can read end to end. House idioms (eb/eh errors, house bus, house caching) replace ecosystem defaults; a consumer inherits them. Interchange still runs on standards: Arrow IPC, SQL, CBOR, CycloneDX SBOMs.

6. **Compatibility × P3.** The [second-substrate trial](../trials/leeway-second-substrate/README.md)'s own verdict: "reasonably technology-neutral, yes, with a 2–3× band" — and "unlocking the technology's full potential, no, not yet", with four measured gaps (bytes lanes typed BLOB break two of fourteen queries on leeway's own writer output; encoding aspects don't cross the Parquet seam).

7. **Compatibility × SC3.** The browser is a supported *transport target* (WebSocket + WebCodecs video, or WebGL2 meshes), not a web platform integration; embedding boxer UI into an existing web app, or web tooling into boxer, is out of scope by design (ADR-0024, SC3).

8. **Interaction capability × P2/P3.** Both costs are the project's own words: "each description language must be learned here — nothing transfers from elsewhere"; "the spine fronts every task — its concepts must be learned before the leverage appears." The adoption-cost register in why-boxer is effectively a self-graded learnability assessment.

9. **Interaction capability × SC3.** Facilitating for inclusivity and consistency (enforced palette and glyph gates, AccessKit accessibility-tree export, coordinate-free actuation); the cost is forgoing the web platform's mature assistive-technology ecosystem, which the accessibility-tree work partly rebuilds.

10. **Reliability × P2.** "Generators are compilers, with compiler-grade defect surface; a wrong generator is wrong everywhere at once." The balancing forces: a central fix propagates everywhere at once too, and every generated file is drift-checked in CI (post-test clean-tree gate, wasm byte-parity).

11. **Reliability × P6.** The premise names its own failure mode: "P6 fails with its bus factor; the written record and the machine mesh are the mitigation, and a mitigation is not an absence." This grades maintenance availability, not runtime availability.

12. **Security × TBD.** No branch protection, no second reviewer, direct-to-main — one compromised credential or workstation writes to trunk. Compensations are detective, not preventive: signed release tags with verify-before-build, provenance trailers, weekly Scorecard/CodeQL. Accountability is strong; access control is thin.

13. **Maintainability × P1.** Facilitating for the ISO sub-characteristics (everything is analysable and modifiable from source, in one tree); the offsetting cost is economic, not structural: "reimplementation and maintenance burden mainstream projects amortize across an ecosystem; fewer external hands exercising the same code paths."

14. **Maintainability × P5.** CODINGSTANDARDS itself concedes the ergonomics: SoA layouts, pre-allocation ceremony, and scratch-buffer fields make routine modification heavier than idiomatic Go, and a small amount of justified unsafe code exists. Uniform conventions and codelint recover part of the cost.

## b — Conclusions

Read as a whole, boxer is a system that has chosen its quality profile explicitly rather than inherited one: it concentrates its bets on maintainability, security, and flexibility, deliberately pays for them in compatibility and learnability, and — unusually — measures its own counteractions. One structural observation frames everything below: the repository's trials protocol already classifies findings by ISO 25010 (2023) characteristic, so quality measurement in this project is endogenous, not something an external audit bolts on.

### Where the practices converge: maintainability as the centre of gravity

Every column except two grades neutral-or-facilitating on the maintainability row, and the concentration is on analysability: the ADR corpus records *why* (199 decisions, append-only, with a supersession discipline), the `boxer adr` tooling makes decision↔code traceability queryable, the "claims that decay" rules keep prose contradictable, and the project measures its own code volume, provenance, and review state as data. Testability is carried by the enforcement mesh — 8,808 test functions, golden corpora that must decode bit-identically across Go and SQL, frame-hash deterministic screenshot capture, a headless UI driver — and modifiability by the description-language bet: roughly 28% of the Go tree is generated, and the editing surface is the description, not the emitted code. The two dissents are honest ones: mechanical sympathy's data-oriented shapes are admittedly less ergonomic to modify (fn 14), and dependency sovereignty converts ecosystem maintenance into owned maintenance (fn 13).

### Security: strong by construction, thin at one process seam

The sovereignty premise is, at bottom, a security premise — authenticity and supply-chain integrity via SBOM + license gate, signed tags with verify-before-build, the `extbin` chokepoint for every external binary, and airgapped builds. Accountability and non-repudiation fall out of the data spine: grants, audits, and lifecycle events land in one append-only fact trail, and authorship provenance lives in git trailers. Confidentiality mechanisms are unusually literal — ad-hoc datasets are AEAD-sealed with keys that never touch disk, "ephemeral by cryptography" even across a crash. Just as valuable for an assessor: the claims are scoped, not inflated ("hygiene, not security" for capabilities; ADR-0087's refusal to claim compositor isolation). The genuine counteraction sits in process, not architecture: direct-to-main with no branch protection or second reviewer concentrates write authority in one credential (fn 12) — and some decided security (carrier auth/TLS, ADR-0082) is candidly labelled built-nowhere-yet.

### Reliability and functional correctness: facilitated by construction, counteracted by process

The default-deny pattern recurs independently in three places — a query result without a terminal frame reads as *incomplete*, a non-loopback bind fails closed and loud, confinement is derived from a registry rather than sniffed from SQL text — and that is precisely what ISO means by fault tolerance: nothing has to go right for a failure to be visible. Recoverability is architectural (append-only facts; a snapshot is complete exactly when its root row exists). Functional correctness is guarded by drift gates, golden files, and cross-engine oracles rather than by reviewers. The counteracting column is the same in both rows: CI is a release gate, not a merge gate — nothing runs on an ordinary push, staticcheck and errcheck are warn-only, staticcheck additionally skips a named list of packages, and nilaway is commented out (fn 2). The project declares this openly (ENGINEERING_PRACTICES §10's register of deliberate absences) and names it "the assumption to revisit first if the repo ever takes external contributions"; declared or not, it is the widest gap between the quality the practices could enforce and the quality they do enforce. A second, quieter counteraction: ~33% of packages carry no test file, and fuzz targets exist with no scheduled fuzz lane — faultlessness coverage is uneven beneath a strong headline apparatus.

### Performance efficiency: a method that measures, a bet that prices itself

Mechanical sympathy makes resource efficiency a default property (SoA end to end, pre-allocation discipline, render budgets), and the trials culture makes time behaviour a dated, reproducible claim rather than folklore — down to details like bounding the ANTLR DFA cache by actual size so adversarial SQL cannot exhaust memory (capacity). Against that stands the central data bet — though the trials price it lower than the bet's reputation: at the 10M tier with the backbone materialized, reads are at parity with native `JSON` held to the same declared schema (0.72–1.09×), and 1.03–1.38× against a reference additionally indexed on the queried columns; the measured cost is on write, ~2× on insert in the fs store and loosely so (fn 4). What is methodologically notable is that every one of those numbers comes from the project's own published trials, complete with the conditions that bound it — and that checking them against those trials is what moved this cell from counteracting to balanced: the cost is priced, bounded, and paid where the measurements actually put it.

### Compatibility and interaction capability: the deliberately paid bill

These are the two rows where the premises knowingly counteract. Interoperability with the surrounding ecosystem is traded for ownership — house idioms replace ecosystem defaults, the UI is explicitly not a web stack, retirements cut wires in one commit with no shims (fn 5, fn 7) — while interoperability *of the data* is actively cultivated and even trial-verified across DuckDB and DataFusion (fn 6). On interaction capability, learnability takes the hit: novel vocabulary, description languages that "transfer from nowhere", a spine that fronts every task (fn 8), no support channel, and an alpha surface with no deprecation policy. The compensations are real but partial: self-descriptiveness through runtime introspection, inclusivity through an enforced colour-blind-safe palette and accessibility-tree export, and the tiered-interface premise, which is itself an operability policy. The why-boxer "what this costs you" register amounts to a self-graded assessment of this row — the project agrees with the minus signs.

### Flexibility: the quiet second winner

The 2023 model's Flexibility characteristic (formerly Portability) is facilitated from more directions than one would expect for a stack this opinionated: installability by airgapped bundles, static binaries, and 109–119 MB appliance images; adaptability by one app source running unchanged on desktop, browser, and appliance; replaceability of the engine measured at a 2–3× cost band rather than asserted; scalability measured across a 10× data-tier sweep (1M smoke to the 10M reportable tier; 100M descoped). The wasm-amenability of every package is tracked as drift-checked metadata — portability treated as a queryable property, the same move the project makes everywhere else.

### Safety: applicable only by analogy — but the analogues are present

Boxer is not a safety-related system, so this row stays mostly neutral. Where ISO's vocabulary does map, the project happens to comply by temperament: fail-safe behaviour in the fail-closed defaults, hazard warning at the point of use (sensitive env vars document their own hazard in the registry), and risk identification as a standing discipline — every premise names its own failure mode, and the formal-verification corner even model-checks deliberately unsafe variants to confirm the checker would catch divergence.

### The meta-conclusion

Three patterns generalize across the table. First, **enforcement over exhortation**: nearly every convention that matters ships with a gate that fails the build, which converts style rules into testability and correctness properties. Second, **measurement over assertion**: performance, portability, and even the project's own defects are dated, reproducible claims — the counteracting cells in this table are largely *the project's own numbers*. Third, **declared counteractions**: the absences register, the adoption-cost list, and the per-premise failure modes mean almost no minus sign in this matrix was discovered by this analysis; nearly all were disclosed. Disclosure does not neutralize a counteraction — the CI-at-release-only gap and the bus factor remain the two findings a certification-minded assessor would press hardest — but it converts unknown risk into priced risk, which is itself the behaviour ISO 25010's measurement model exists to produce.

## c — The finer-grained criteria, as I read them

ISO/IEC 25010:2023 defines nine product-quality characteristics, each decomposed into sub-characteristics. The 2023 revision renamed Usability to Interaction Capability and Portability to Flexibility, added Safety as a ninth characteristic, and added Resistance under Security. The summaries below are my working understanding, condensed for this assessment — not the normative text.

### Functional suitability

How well the delivered functions match stated and implied needs — is everything there, is it right, is it fit for the task.

- **Functional completeness** — The function set covers all specified tasks and intended user objectives — nothing the user needs is missing.
- **Functional correctness** — The product produces correct results, at the degree of precision the use requires.
- **Functional appropriateness** — The functions actually facilitate the tasks — accomplishing a goal takes no unnecessary steps or detours.

### Performance efficiency

Performance relative to the resources consumed, under stated conditions.

- **Time behaviour** — Response times, processing times, and throughput meet requirements while the product performs its functions.
- **Resource utilization** — The amounts and types of resources (CPU, memory, storage, energy) used are appropriate to the requirements.
- **Capacity** — The maximum limits — items stored, concurrent users, data volume, bandwidth — meet requirements and degrade predictably at the edge.

### Compatibility

How well the product lives alongside and exchanges with other systems.

- **Co-existence** — The product performs its functions efficiently while sharing an environment and resources with other products, without harming them.
- **Interoperability** — Two or more systems can exchange information and — the harder half — actually use the information that was exchanged.

### Interaction capability · 2011: Usability

How well specified users can interact with the product to achieve their goals — effectively, efficiently, safely, and satisfyingly.

- **Appropriateness recognizability** — Users can tell, before committing, whether the product suits their need.
- **Learnability** — Specified users can learn to use the product within a specified amount of time — the on-ramp is proportionate.
- **Operability** — The product has attributes that make it easy to operate and control in day-to-day use.
- **User error protection** — The product protects users against making errors, and against errors having irreversible consequences.
- **User engagement** — The presentation of functions and information invites and motivates continued interaction.
- **Inclusivity** — The product is usable by people with the widest practical range of characteristics and capabilities — accessibility in the broad sense.
- **User assistance** — Users can get help — documentation, guidance, support — proportionate to their needs when they are stuck.
- **Self-descriptiveness** — The product presents appropriate information about itself — its state, its capabilities, what it is doing — so users can operate it without external help.

### Reliability

How well the product performs its functions under stated conditions for a stated period.

- **Faultlessness · 2011: Maturity** — The product performs its functions without faults under normal operation — few defects escape into use.
- **Availability** — The product is operational and accessible when it is required for use.
- **Fault tolerance** — The product operates as intended despite hardware or software faults and malformed input — failures are contained and visible rather than silent.
- **Recoverability** — After an interruption or failure, the product can recover its data and re-establish its state.

### Security

How well the product protects information and data so that access is appropriate to authorization, and defends itself while doing so.

- **Confidentiality** — Data is accessible only to those authorized to have access.
- **Integrity** — The system and its data are protected against unauthorized modification — accidental or malicious.
- **Non-repudiation** — Actions or events can be proven to have taken place, so they cannot be denied later.
- **Accountability** — Actions of an entity can be traced uniquely back to that entity.
- **Authenticity** — The identity of a subject or resource can be proved to be the one claimed.
- **Resistance · new in 2023** — The product sustains its operation while under attack — brute force, denial of service, resource exhaustion.

### Maintainability

How effectively and efficiently the product can be modified — corrected, improved, adapted — by its intended maintainers.

- **Modularity** — The product is composed of discrete components such that a change to one has minimal impact on the others.
- **Reusability** — Assets can be used in more than one system, or in building other assets.
- **Analysability** — How easily one can assess the impact of an intended change, diagnose a deficiency or failure cause, or identify what must be modified — the quality of the system's legibility to its maintainers.
- **Modifiability** — The product can be modified without introducing defects or degrading existing quality.
- **Testability** — Test criteria can be established for the product, and tests can be performed effectively to determine whether they are met.

### Flexibility · 2011: Portability

How well the product adapts to changes in requirements, contexts of use, and environments.

- **Adaptability** — The product can be adapted to different or evolving hardware, software, operational, and usage environments.
- **Scalability** — The product handles growing or shrinking workloads, and its resource demand adapts accordingly.
- **Installability** — The product can be installed and uninstalled effectively and efficiently in a specified environment.
- **Replaceability** — The product can replace another product for the same purpose in the same environment — and, in mirror, its own components can be swapped for equivalents.

### Safety · new in 2023

How well the product avoids states that endanger life, health, property, or environment under defined conditions — primarily aimed at safety-related systems, applicable to others by analogy.

- **Operational constraint** — The product constrains its operation to within safe parameters when facing an operational hazard.
- **Risk identification** — The product (and its engineering) identifies events and operations that could expose people, property, or environment to unacceptable risk.
- **Fail safe** — On failure, the product automatically places itself in a safe operating mode, or reverts to a safe condition.
- **Hazard warning** — The product warns of unacceptable risks in time for operators to react safely.
- **Safe integration** — The product maintains safety during and after integration with other components or systems.

> **Method and scope caveats.** ISO 25010 models *product* quality; most of boxer's premises are *process and architecture* stances, so each grade judges the practice's directional influence on the product characteristic, not a conformance measurement. Grades are qualitative and evidence comes from the repository's committed documents, ADRs, CI scripts, and code as of 2026-08-24 (commit 7d0870b8) — including numbers the project measured about itself, quoted with their original dates per its own "never refresh a frozen number" rule. The criteria summaries in this section are paraphrases for working use, not the ISO normative text.

## d — Marginals & top strengths

### Marginalized characteristics — the row sums

Each characteristic marginalized over the fourteen practice columns, counting facilitating as +1, neutral as 0, counteracting as −1 (the same totals shown in the matrix's Σ column). The net is a crude ordinal aggregate: it weights every practice equally and hides magnitude — a −1 from the absent merge gate and a −1 from a missing support channel count the same — so read it as a shape, not a score.

| rank | characteristic | facilitating | neutral | counteracting | net |
| --- | --- | --- | --- | --- | --- |
| 1 | Maintainability | 9 | 4 | 1 | +8 |
| 1 | Flexibility | 8 | 6 | 0 | +8 |
| 3 | Security | 7 | 6 | 1 | +6 |
| 3 | Performance efficiency | 6 | 8 | 0 | +6 |
| 5 | Functional suitability | 7 | 5 | 2 | +5 |
| 6 | Reliability | 6 | 6 | 2 | +4 |
| 7 | Safety | 3 | 11 | 0 | +3 |
| 8 | Interaction capability | 6 | 4 | 4 | +2 |
| 9 | Compatibility | 4 | 7 | 3 | +1 |

The row marginals confirm the section-b reading with numbers: maintainability and flexibility tie at the top (+8 — maintainability with the broadest support at 9 facilitations), security and performance efficiency follow at +6, three characteristics draw no counteraction at all (flexibility, performance efficiency, safety), and the two deliberately-paid bills land exactly where the premises put them: interaction capability (+2, and the row with the most counteractions at 4) and compatibility (+1, the lowest net). Safety's +3 with eleven neutrals reflects limited applicability, not weakness.

### Marginalized practices — the column sums

The same marginalization over the nine characteristic rows, per practice (shown in the matrix's footer row).

| rank | practice | facilitating | neutral | counteracting | net |
| --- | --- | --- | --- | --- | --- |
| 1 | **TRI** · trials & measured claims | 8 | 1 | 0 | +8 |
| 2 | **ENF** · enforcement-as-code & default-deny | 7 | 2 | 0 | +7 |
| 3 | **P4** · dogfooding & self-measurement | 6 | 3 | 0 | +6 |
| 4 | **SC1/2** · memory-safe pair, quarantined engine | 5 | 4 | 0 | +5 |
| 4 | **ADR** · decision record & design-before-code | 5 | 4 | 0 | +5 |
| 4 | **P3** · one data spine | 6 | 2 | 1 | +5 |
| 7 | **P1** · dependency sovereignty | 4 | 4 | 1 | +3 |
| 7 | **P2** · description languages & codegen | 4 | 4 | 1 | +3 |
| 7 | **SC3** · immediate-mode UI over framed FFI | 4 | 4 | 1 | +3 |
| 7 | **P7** · tiered interfaces | 3 | 6 | 0 | +3 |
| 11 | **P5** · mechanical sympathy | 2 | 6 | 1 | +1 |
| 12 | **P6** · one architect, machine-checked | 1 | 6 | 2 | −1 |
| 13 | **RET** · retirement discipline, alpha surface | 1 | 5 | 3 | −2 |
| 14 | **TBD** · trunk-based, CI at release only | 0 | 6 | 3 | −3 |

The column marginals expose a clean split: the three strongest facilitators (TRI, ENF, P4) are the *meta*-practices — how the project measures, enforces, and records — while the substantive architectural bets net positive but pay real costs somewhere; the one data spine reaches the +5 tie with SC1/2 and ADR only because its remaining cost lands on learnability rather than on performance. The only net-negative columns are pure process choices: trunk-based CI-at-release (−3, the sole practice with zero facilitations), the alpha/no-shim surface (−2), and the single-architect model (−1). Every *architectural* commitment nets positive.

### Top five fine-grained quality strengths

Ranked from the fine-grained evidence — the per-cell rationales, footnotes, and section-b analysis — not the coarse marginals. A sub-characteristic ranks high when three things coincide: several *independent* practices' facilitating evidence names it, the property is held by a gate that fails the build rather than by convention, and there is measured or recorded evidence in the tree. The count in each entry is the number of distinct practice columns whose cell rationale lands on that sub-characteristic.

#### 1. Analysability — Maintainability

*6 converging practices · enforced · measured*

The single strongest quality in the repository. Six columns converge on it: the ADR corpus records why (199 append-only decisions with supersession discipline); `boxer adr` makes decision↔code traceability queryable data (mandatory ADR-NNNN markers, implementation-degree crossed against status); dogfooding lands code volume, coverage, review state, and provenance as facts; the trials rules keep quantitative prose contradictable ("dated or live", "never refresh a frozen number"); sovereignty puts every load-bearing line in-tree and readable; and the single-architect premise compensates itself with recorded intent. Enforced by doclint's invariant→enforcer table and drift-triggered re-review; measured by the weekly codestat lane.

#### 2. Testability — Maintainability

*6 converging practices · enforced · measured*

8,808 test functions and — more telling — the bespoke apparatus: golden corpora that must decode bit-identically across Go and SQL (ADR-0106), byte-reproducible WASM parity, the stateful-widget gate that fails `go test` before a rendering bug can surface (ADR-0013), frame-hash deterministic screenshot capture (ADR-0057), a headless accessibility-tree driver that tests the GUI without a compositor (ADR-0154), property-based testing, and Tier-1 ADRs that must name "the lane that goes red". The mirror caveat from the fine-grained analysis stands: the apparatus is deep where it exists, and ~33% of packages carry no test file.

#### 3. Integrity & authenticity of the supply chain — Security

*4 converging practices · enforced · measured*

The sovereignty premise operationalized end to end: CycloneDX SBOM feeding an in-tree license gate, SSH-signed release tags with verify-before-build refusal (ADR-0085), every external binary resolved through the audited `extbin` chokepoint (codelint CS012 at Error), SHA256-pinned fonts and assets, pinned toolchains with Dependabot cooldowns, and airgapped build bundles whose non-vendorable residue is named rather than hidden. The xz-utils class of attack is the explicitly cited adversary, and the whole chain is mechanized, not policy prose.

#### 4. Fault tolerance — Reliability

*4 converging practices · enforced by construction*

Default-deny recurs independently in three subsystems — a query result without its terminal frame reads as *incomplete*, never complete (ADR-0142); a non-loopback bind refuses to start, failing closed and loud (ADR-0082); confinement of sealed data derives from a registry lookup, never a heuristic over SQL text (ADR-0145). Around that: memory safety removes whole crash classes, the one large unsafe component runs quarantined out-of-process, and commit protocols make partial state unrepresentable ("a snapshot is complete exactly when its root row exists"). The failing party is never required to report its own failure.

#### 5. Installability — Flexibility

*4 converging practices · measured*

An unusually strong showing for a stack this deep: fully offline/airgapped builds (`GOPROXY=off`, vendored sysroots, `GOTOOLCHAIN=local`), `CGO_ENABLED=0` static Go binaries with the musl-static path cleared, an empty required build-tag set so a consumer needs no flags, and bootable appliance images at a measured 109–119 MB whose runtime surface is countable (`ldd` lists four files for the software rasterizer). The A/B image pair even isolates its one variable — the two images differ by exactly one file — so the installation story is itself experimentally controlled.

Near misses, in order: accountability (the append-only fact trail plus per-commit provenance trailers — kept out of the five only because it overlaps the supply-chain entry), adaptability (one app source unchanged across desktop, browser, and appliance), self-descriptiveness (runtime introspection tables, generated registries), and resource utilization (mechanical sympathy's measured defaults). The symmetric bottom of the fine-grained ranking, for contrast: learnability and ecosystem-facing interoperability — the two costs the premises explicitly agreed to pay.

## Further reading

- [why-boxer](./why-boxer.md) — the seven premises with their costs and failure modes; the source of the P-columns.
- [positioning-statement](./positioning-statement.md) — the same bets stated for a prospective adopter.
- [iso25010-temperament](./iso25010-temperament.md) — the companion: the same reading with the figures removed, written to age differently.
- [ENGINEERING_PRACTICES](../ENGINEERING_PRACTICES.md) — the CI surface, the gates, and §10's register of deliberate absences.
- [doc/trials](../trials/README.md) — the measurement protocols every quantitative cell here draws on.
