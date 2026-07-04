---
type: explanation
audience: prospective consumers and integrators evaluating adoption
status: stable
reviewed-by: "@stergiotis"
reviewed-date: 2026-07-05
---

> Where this page and an ADR disagree, the ADR is the record. Regulatory
> references are engineering-grade context, not legal advice.

# Why boxer is shaped this way

Boxer reimplements and vendors much of what mainstream practice imports or
rents. Seen without its premises, that reads as not-invented-here; this page
states the premises explicitly — what each buys and what each costs — so a
prospective consumer can decide in minutes whether they share them. What the
packages do is in the [README](../../README.md); why each individual decision
went the way it did is in [doc/adr/](../adr/). This page carries only what no
single ADR states: the premises the decisions share.

The timing reads four external shifts, each moving the build-versus-rent
frontier in the same direction: AI assistance has collapsed the cost of
authoring and maintaining owned code; software-as-product liability and
cyber-resilience duties are arriving in EU law; supply-chain compromise is a
demonstrated attack class rather than a hypothetical; and memory-safe
implementation languages are moving from recommendation toward mandate.

## The bet, named

Boxer bets that a small operation can own its data-engineering stack end to
end — own in the sense usually called **sovereignty**: every load-bearing
part is vendored, ported, or reimplemented, so the whole builds and runs
without network access or rented capability and can be audited from source
([airgapped build](../howto/airgapped-build.md),
[ADR-0095](../adr/0095-airgapped-build-bundle.md)).

Sovereignty alone would not justify the cost; vendoring is cheaper than
rewriting. The premises below answer one another's cost objections in
sequence: owning everything is affordable only if the owned code stays small
(P2); small descriptions compound only if they share one machine-readable
data model (P3); a stack that shares one model can verify itself by running
on itself (P4); mechanical sympathy keeps the owned whole runnable on
hardware the operation controls (P5); a one-architect workforce can carry it
only because the earlier premises shrink and machine-check what must be
carried (P6); and interface effort splits by task complexity, up to agents,
so none of it is spent where it does not pay (P7).

## Premises

### P1 — Dependencies are owned

Every load-bearing dependency is treated as a liability the project must be
able to carry alone: referenced while it stays cheap to trust, and vendored,
ported into the tree, or reimplemented when it does not. Parts that can be
neither trusted nor owned are avoided.

The premise has legal, technical, and human faces. Legally, the EU's
revised product-liability regime (Directive (EU) 2024/2853) treats software
as a product whose defects carry manufacturer liability, and the Cyber
Resilience Act (Regulation (EU) 2024/2847) adds security-by-design,
vulnerability-handling, and SBOM duties — obligations that are hard to meet
for components one can neither audit nor patch. Technically, supply-chain
compromise is a demonstrated attack class. The human face is the least
discussed: every dependency imports its maintainers' incentives along with
its code. The 2024 xz-utils backdoor showed all three at once — a malicious
payload in release tarballs, delivered through a years-long pressure
campaign on a single overloaded maintainer. A minimal dependency set with
known, homogeneous incentives keeps trust tractable; whatever cannot be
trusted at the incentive level must be owned at the source level.

The working rule is about coupling, not code volume. A dependency is
referenced when it is boring in the load-bearing sense — a stable surface, a
vetted license, no cgo, no service tether, no wire format or callback shape
that binds boxer's internals to it — because such a dependency bills little
ongoing verification attention. It is vendored, ported, or reimplemented
when any of that fails, or when one of two boxer-specific tests eliminates
it: the part must work on both sides of the UI ⇄ data-engineering boundary
(one isomorphic implementation rather than two ecosystem ones), and its
surface must be introspectable by the observability loop (P4) — errors,
schemas, and metrics as machine-readable facts, not opaque strings. The
result is directional, not absolute: the module graph still references parts
that pass (Apache Arrow, embedded storage engines, a Markdown core, CLI
plumbing); the aim is a minimized, audited trust surface, not zero
dependencies.

- **Buys:** end-to-end auditability; reproducible offline builds; immunity to
  upstream drift, license changes, and service shutdowns; trust grounded in
  a small set of known incentives; a liability surface the project can
  actually inspect.
- **Costs:** reimplementation and maintenance burden that mainstream projects
  amortize across an ecosystem; fewer external hands exercising the same code
  paths.
- **Enacted by:** the CI license gate over a CycloneDX SBOM
  ([ADR-0004](../adr/0004-license-gate-cyclonedx.md)), the embedded Kafka
  port ([ADR-0005](../adr/0005-streaming-persisted-kafka-from-connect.md)),
  the vendored ports under `internal/`, the
  [airgapped build](../howto/airgapped-build.md)
  ([ADR-0095](../adr/0095-airgapped-build-bundle.md)).

### P2 — Problem-oriented languages, boring host

The size hypothesis of Alan Kay's STEPS program
([STEPS Toward the Reinvention of Programming](https://tinlizzie.org/VPRIPapers/tr2012001_steps.pdf))
holds that most of a system's bulk is accidental, and that problem-oriented
languages matched to a domain's shape shrink it by orders of magnitude.
Boxer runs a conservative variant of that experiment: the leverage sits in
small description languages — leeway schema descriptions and mapping plans,
the fffi2 IDL, the nanopass pass vocabulary — while the host stays
deliberately boring: Go, mainstream toolchains, CI discipline, no
meta-circular runtime. Roughly a third of the repository's Go is generated
from those descriptions; the descriptions, not the emitted code, are the
editing surface.

- **Buys:** a small maintained surface; a schema change propagates by
  regeneration rather than by hand-editing call sites; checked-in generated
  sources (`.out.go`) make drift diffable.
- **Costs:** each description language must be learned here — nothing
  transfers from elsewhere; generators are compilers, with compiler-grade
  defect surface; a wrong generator is wrong everywhere at once.
- **Enacted by:** the leeway codegen pipeline (DDL / DML / read-access /
  marshalling;
  [ADR-0066](../adr/0066-leeway-dql-clickhouse-readback-generator.md),
  [ADR-0101](../adr/0101-leeway-marshall-mixed-shape-sections.md)), the
  nanopass discipline ([ADR-0002](../adr/0002-nanopass-discipline.md),
  [ADR-0006](../adr/0006-nanopass-environment-and-first-class-pass.md)), the
  framed FFI IDL
  ([ADR-0049](../adr/0049-fffi2-deferred-block-scope-buffer-memoization.md)).

### P3 — One machine-readable data spine

The underlying wager is data-centric: schemas and the data they describe
outlive the applications that produce them, so the durable investment is the
model, not the app. Data therefore has one model — leeway — and every shape
a value takes (in-memory struct, bus message, ClickHouse row) is a generated
projection of that model, not a hand-maintained sibling. New subsystems
consume the spine; they do not fork it.

The model is machine-readable at runtime — canonical type signatures, table
descriptors, the aspect system — so tools can be generic: a schema
inspector, a mapping-plan playground, or an agent works against any
conforming schema instead of being hand-integrated per application.

- **Buys:** isomorphism across memory, wire, and storage — one described
  schema yields DDL, DML, marshalling, and read-back; each new vertical costs
  a projection, not a data layer; generic tooling amortizes across every
  conforming schema.
- **Costs:** the spine fronts every task — its concepts (backbone/payload,
  sections, memberships) must be learned before the leverage appears.
- **Enacted by:** data contracts
  ([ADR-0060](../adr/0060-leeway-data-contracts-odcs.md)), the row-DML
  wire/ingestion split
  ([ADR-0089](../adr/0089-rowdml-serialization-clickhouse-native-ingestion.md)),
  runtime introspection tables
  ([ADR-0094](../adr/0094-keelson-introspection-tables.md)), the generated
  record store
  ([ADR-0100](../adr/0100-recordstore-generated-leeway-clickhouse-store.md)).

### P4 — The toolkit is its own first workload

Boxer observes, stores, and renders its own behaviour with the machinery it
offers: system and runtime metrics flow as leeway facts over the bus into
ClickHouse and back onto boxer-rendered dashboards.

The premise extends from the runtime to the code itself: much of a codebase
is measurable — profiles, coverage, lint and design-review findings,
authorship and provenance — and the bet is that it should be measured
continuously and recorded as data, not sampled in one-off reports. Opt-in
continuous profiling is a build tag away, review findings land as JSON
artifacts, and provenance is queryable from the history.

- **Buys:** the pipeline is exercised end to end before any external workload
  touches it; a defect in the spine surfaces on the project's own screens.
- **Costs:** breadth. A pure library would outsource collectors, storage
  plumbing, and UI; boxer carries all three, including a Rust-rendered
  immediate-mode UI stack.
- **Enacted by:** the resource monitor
  ([ADR-0020](../adr/0020-imzero2-imztop-resource-monitor.md)), the
  observability pipeline
  ([ADR-0050](../adr/0050-clickhouse-observability-pipeline.md)), the runtime
  dashboard ([ADR-0061](../adr/0061-imzero2-imzrt-go-runtime-dashboard.md)),
  the metrics data plane
  ([ADR-0090](../adr/0090-sysmetrics-pubsub-data-plane.md)).

### P5 — Mechanical sympathy

Resource efficiency comes from designing with the machine's grain rather
than from after-the-fact optimization: structure-of-arrays shapes end to end
(leeway's SoA columns, the columnar sink), padding- and cache-conscious
layouts, render budgets in the UI stack. The target is reasonably good
efficiency as a default property of the design — not peak performance as a
heroic exception.

- **Buys:** the whole stack stays operable on hardware a small operation
  actually controls, which is what makes P1's self-hosting real rather than
  nominal; performance regressions surface as structural surprises, not as
  routine drift.
- **Costs:** data-oriented shapes are less ergonomic than idiomatic object
  graphs; a small amount of unsafe, performance-motivated code exists and
  must carry its justification.
- **Enacted by:** the SoA wire-format pivot
  ([ADR-0089](../adr/0089-rowdml-serialization-clickhouse-native-ingestion.md)),
  the columnar sink commitment, the opt-in continuous-profiling build tag,
  the `unsafeperf` package.

### P6 — One architect, machine-checked

Boxer is developed by one architect with substantial AI assistance under
recorded governance: no third-party contributions, per-commit provenance in
git trailers, and correctness carried by a machine-checkable mesh — build
tags, linters, golden files, conformance suites, server-truth harnesses —
rather than by reviewer headcount. The premise reads a shift in software
economics: when assisted authorship is cheap, verification and recorded
intent become the scarce assets, so the project invests in those (the ADR
corpus, the lint tiers) rather than in review capacity it does not have.

- **Buys:** coherence — every decision passes through one set of premises; a
  written *why* instead of tribal knowledge; provenance that survives its
  author.
- **Costs:** a bus factor of one, mitigated but not removed by the records;
  throughput bounded by one person and their tooling; a take-it-or-leave-it
  surface — no support channel, no roadmap influence.
- **Enacted by:** the README's
  [AI codegen declaration](../../README.md#ai-codegen-declaration),
  trailer-based provenance
  ([ADR-0083](../adr/0083-retire-llm-generated-build-tags.md)), the
  governance tooling under `public/gov/`.

### P7 — Interfaces split by task complexity

User interfaces divide into three domains, each earning a different
investment. Simple tasks get classic UIs — discoverable, consistent, direct
manipulation; that is what the design system and its policy tiers exist for.
Medium-complexity tasks get one-off, prototypical UIs: direct manipulation
of a few hyperparameters over standard building blocks for output
visualization — here the building blocks are the product, and the assembled
UI is allowed to be disposable. Highly complex tasks go to agentic systems
that operate the same machinery through machine-readable surfaces (P3)
rather than through pixels.

- **Buys:** interface effort lands where each class needs it — polish where
  discoverability pays, a widget kit where speed of assembly pays,
  machine-readable seams where no static UI can span the task space.
- **Costs:** the middle tier deliberately ships prototypes, not products;
  the agentic tier inherits LLM failure modes and leans on the verification
  mesh (P6) and the machine-readable model (P3).
- **Enacted by:** the design system and its policy tiers
  ([ADR-0029](../adr/0029-imzero2-design-system-and-policy-as-code.md)), the
  widget library and the play workbench
  ([ADR-0097](../adr/0097-play-reactive-query-graph.md)), the text-to-SQL
  orchestration under `public/db/clickhouse/`, the introspection tables
  ([ADR-0094](../adr/0094-keelson-introspection-tables.md)).

## Standing commitments

Below the premises sit infrastructure commitments a consumer inherits with
the integrated stack; they are argued in their ADRs, not here.

- **Go and Rust** — a memory-safe pair: Go hosts the toolkit, Rust renders
  the UI and selected bridges; the first-party tree avoids linked C/C++
  where an alternative exists (the C-derived H3 library runs as sandboxed
  WASM rather than through cgo, [ADR-0003](../adr/0003-h3-wasm-bridge.md)).
  Regulatory trajectory — CISA memory-safety roadmaps, the CRA's
  security-by-design expectations — treats memory safety as a coming
  requirement rather than a taste.
- **ClickHouse** as the analytical sink — an external C++ server process,
  deliberately outside the memory-safety boundary rather than linked into it.
- **Immediate-mode UI** (egui) over a framed FFI, not a web stack.
- **Code generation over reflection** — generated sources are checked in and
  diffed like any other source.

## What this costs you

Consequences that land on a consumer regardless of which premise attracted
them:

- Every `go build` / `go test` / `go vet` needs the repository's build tags
  ([README § Building](../../README.md#building)); without them, packages
  fail with misleading `undefined` errors.
- House idioms replace ecosystem defaults: structured error building and
  handling (`eb` / `eh`) instead of `fmt.Errorf` chains; house caching, bus,
  and app-runtime layers.
- Data-oriented shapes (structure-of-arrays columns, columnar thinking)
  where idiomatic Go would reach for object graphs.
- Novel vocabulary (backbone/payload, sections, memberships, cards, passes)
  documented in-repo but existing nowhere else.
- Alpha surface: APIs break, sometimes in batches; there is no deprecation
  policy yet.
- No third-party contributions and no support channel.
- The integrated stack pulls in Go and Rust toolchains plus a ClickHouse
  instance.

## Who this is for — and not

Boxer fits an operation that shares P1 — that must build offline, audit from
source, or outlive its vendors — and wants the compounding of P2–P5 rather
than a parts bin. It also serves readers who want a worked example of the
premises; the ADR corpus is half the artifact.

Several leaf packages are deliberately ordinary Go libraries and can be
imported without adopting any premise — the Obsidian-markdown parser,
axis-tick layout, Golay codes, compression-based similarity, Swiss geodesy,
among others. The further a package sits toward the integrated stack
(leeway, keelson, imzero2), the more of the premises an importer inherits.

Boxer does not fit a team that wants a stable API, a supported product, a
community, or best-of-breed composition. A team in that position is better
served importing mainstream parts directly than adopting a stack whose value
is the premises it imposes.

## How this bet fails

The premises are falsifiable, and each names its failure mode. P2 fails if
the generators cost more to maintain than the code they emit. P3 fails the
moment new verticals stop consuming the spine — hand-rolled data shapes
beside leeway, observability that does not round-trip through the facts
path; the compounding then stops, the repository degrades into a collection
of independent engines, and the not-invented-here reading becomes the
correct one. P5 fails silently if profiles and budgets stop being read —
efficiency claims must remain measured claims, which is P4's job to keep
true. P6 fails with its bus factor; the written record and the machine mesh
are the mitigation, and a mitigation is not an absence. P7 fails if the
prototypical middle tier calcifies into unowned products, or if the agentic
tier's surfaces drift out of machine-readability — which P3 exists to
prevent.

Warning signs are checkable from the outside: recent ADRs that no longer
cite or consume the spine; new subsystems with bespoke persistence; vendored
code growing without consuming decisions.

## Further reading

- [doc/adr/](../adr/) — the per-decision record; the premises above in
  action.
- [ENGINEERING_PRACTICES](../ENGINEERING_PRACTICES.md) — the machine-checkable
  mesh.
- [CODINGSTANDARDS](../../CODINGSTANDARDS.md) — including the
  design-before-code norm.
- [README](../../README.md) — inventory, build instructions, the AI codegen
  declaration.
