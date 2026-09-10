# boxer

[![Go Reference](https://pkg.go.dev/badge/github.com/stergiotis/boxer.svg)](https://pkg.go.dev/github.com/stergiotis/boxer)

Working in this tree, human or agent? [`AGENTS.md`](AGENTS.md) is the router: it carries the few repo-specific things that are easy to miss and points at the authoritative document for everything else.

## Maturity
Alpha, incomplete test coverage, unstable, API may still change heavily.

## Why
For a small but ambitious data team ready to bet on the compounding of a composable stack it owns — and needing air-gapped operability and auditability from source — boxer is a sovereign, data-centric data-engineering toolkit and app stack over ClickHouse (the query engine and database): one Go host process carrying the apps, the bus and the data paths, a Rust client beside it that only renders, and ClickHouse as the one place anything durable lives.

Rather than assembling best-of-breed parts with fragmented models and logs, boxer generates tables, codecs, ingestion and readers from small, problem-oriented languages; turns a query into a table, chart, map or board — or, in a markdown file, an app; runs that app unchanged on a desktop, in a browser, or from a roughly 110 MB appliance image; and lands every query, error, grant and app state — even file trees — in one queryable table shape, operable by people and agents alike — trading ecosystem breadth and a stable API for a stack a small team can read from source, end to end.

Boxer reimplements and vendors much of what mainstream practice imports or rents. The premises behind that trade — dependencies as owned liabilities with known incentives, problem-oriented description languages on a boring host, one machine-readable data model projected across memory / wire / storage, a toolkit that observes itself (runtime and code), mechanical sympathy as the efficiency posture, one machine-checked architect, and interfaces split by task complexity up to agentic operation — are stated, with their costs and failure modes, in [doc/explanation/why-boxer.md](doc/explanation/why-boxer.md). The full form of the statement above, and what each clause rests on, is in [doc/explanation/positioning-statement.md](doc/explanation/positioning-statement.md); the drawing behind its process and host clauses is [doc/ARCHITECTURE.md](doc/ARCHITECTURE.md).

## Installation
```
go get github.com/stergiotis/boxer
```

## What's inside
Boxer is a collection of packages under `public/`. The larger subsystems:

* `algebraicarch/pushout` — patch-based merge of line graphs: an ojo-style graph with tombstones over a content-addressed patch log, where a merge is the set union of applied patches and conflicts are detected after the fact (the pushout vocabulary is Mimram & Di Giusto's; no pushout object is computed): `pushoutgraph/store` for the line-graph data structure, `pushoutgraph/patch` for the patch DAG, `envelope` for transmittable patches; the line-graph (upstream: *graggle*) and pseudo-edge constructs follow Joe Neeman's [ojo](https://github.com/jneem/ojo) design (see [`pushout/pushoutgraph/NOTICE`](public/algebraicarch/pushout/pushoutgraph/NOTICE)); Includes a `BackendI`/`RepoI` seam under `algebraicarch/pushout/pijul` with two realisations — a native pushout backend (no external binary) and a text backend that shells out to `pijul`.
* `semistructured/leeway` — code-driven entity-attribute-value data model with a staged codegen pipeline (DDL / DML / read-access / streaming read-access / marshalling).
* `semistructured/markdown/obsidian` — goldmark-based parser for Obsidian-flavored markdown (callouts, wikilinks, embeds, tags, highlights, frontmatter).
* `db/clickhouse/dsl` — typed ClickHouse SQL DSL with an AST, marshalling, and nanopass rewrite passes (ADR-0002, ADR-0006).
* `streaming/persisted/kafka` — embedded Kafka producer/consumer derived from Redpanda Connect's franz-go integration (ADR-0005).
* `caching` — read-through batch cache aimed at ETL / build / graph-traversal pipelines: latency-hidden via dependency accumulation and partition-aware bulk fetches, with optional disk-backed L2.
* `analytics/similarity/compression` — compression-based similarity metrics (NCD, CCC) over any `Reset`-able compressor.
* `math/numerical/finddivisions` and `math/numerical/timeticks` — axis-tick layout: Heckbert / Wilkinson / Talbot for numeric and log axes; a uPlot-derived calendar ladder with locale-aware boundary snapping for time axes.
* `science/geo/h3` — H3 geospatial indexing via a Rust→WASM→wazero bridge (ADR-0003); Rust source under `rust/h3bridge`.
* `thestack/imzero2` — ImZero v2: an egui-based immediate-mode UI stack (egui2 FFI bindings, widgets, the IDS design system, demo apps), rendered by the Rust backend under `rust/imzero2`.
* `thestack/fffi2` — Framed Foreign Function Interface: the typed FFI / IR layer imzero2 builds on (ADR-0049).
* `keelson` — application runtime for imzero2 apps: an `AppI` registry with dock/CLI hosts, an in-process bus, a bus-codec, a facts store, background-task supervision, and a help system.
* `fs/lading` — filesystem snapshot store: an `io/fs` tree landed into three facts-shaped ClickHouse tables addressed by `(mount, snapshot, path)`, with per-mount content policy, declarative retention, and read paths via `io/fs`, SQL macros and an SFTP head (ADR-0198). `apps/tally` browses it.
* `observability/sysmetrics` — Linux system-metrics collectors (cpu, mem, disk, net, proc, sensors, battery, container, opt-in GPU backends) (ADR-0019).
* `science/geo/swisstopo` — Swiss LV95 ⇄ WGS84 coordinate transforms, GeoTIFF elevation sampling, and line-of-sight queries.
* `fec` — forward error correction (e.g. `fec/ea/golay24`).
* `eb`, `eh` — structured error building and error handling.
* `batching`, `containers`, `hashing`, `identity`, `logical`, `observability`, `parsing`, `slices`, `statespace`, `unsafeperf`, … — utility packages.

`internal/` carries vendored third-party ports.

ImZero **v1** (Dear ImGui-based) lives in [`imzero_imgui`](https://github.com/stergiotis/imzero_imgui). ImZero **v2** (egui-based) is part of this module: the Go side under `public/thestack/{imzero2,fffi2}`, its Rust egui renderer under `rust/imzero2`, and runnable demo applications under `apps/` (`play`, `imztop`, `tally`, `capdemo`, `capinspector`, `taskdemo`, …).

## Building
Boxer requires **Go 1.27** (ADR-0199) and needs no build tags to compile. The set boxer itself builds with lives in [`./tags`](tags), which has been **empty** since ADR-0212. Pass it anyway to every `go build`, `go test`, and `go vet` invocation, so that what you build matches what CI builds and a tag added later reaches you:

```
go build -tags="$(cat ./tags)" ./...
go test  -tags="$(cat ./tags)" ./...
go vet   -tags="$(cat ./tags)" ./...
```

The file is empty because both of the tags it used to carry were retired: `boxer_enable_profiling`, which compiled out the pprof capture paths, gave way to splitting the HTTP listener into its own package, so that a binary pays for `net/http` only if it imports one ([ADR-0212](doc/adr/0212-split-pprof-http-listener.md)); and `goexperiment.jsonv2`, needed until Go 1.27 and without which the build failed outright with misleading *undefined identifier* errors, went with `encoding/json/v2` graduating.

Shipped binaries are built one way, from a sourced environment file rather than per-script flags, so that two builds of one commit come out byte-identical across machines, paths and clocks ([ADR-0215](doc/adr/0215-retire-mimalloc-reproducible-builds.md)). `source scripts/dev/go-build-env.sh` before a `go build` you intend to ship or compare, and `scripts/dev/rust-repro-env.sh` before a `cargo build`. CI builds the Go host and the lean Rust render head twice, at different paths, and byte-compares them, and checks the committed h3 wasm blob against a rebuild; what the property covers and what it does not, including the air-gapped case, is in [ENGINEERING_PRACTICES §7](doc/ENGINEERING_PRACTICES.md#7-reproducible-builds).

[`./boxer.sh`](boxer.sh) is the repository's own entry point: it sources `go-build-env.sh`, builds `./public/app` and runs it. `./boxer.sh --help` lists the command groups — code generation (`egui2gen`, `keelsoncodec`, `protogen`), governance and code analysis (`gov`, `code`), ingestion and stores (`markdown`, `fs`, `datacatalog`), the env-var registry (`env`), and the durable-work worker (`watchbill`).

## Documentation
Boxer follows the [Diátaxis](https://diataxis.fr/) framework (ADR-0001). Docs live next to the code they describe:

* **Architecture overview** — [`doc/ARCHITECTURE.md`](doc/ARCHITECTURE.md) draws how the pieces fit: the operation modes (desktop, headless, appliance images) and the data architecture (the two ClickHouse engines, the `boxer.*` tables, ad-hoc datasets, the filesystem snapshot store and its rclone seam).
* **Architecture decisions** — [`doc/adr/`](doc/adr/) records the *why* behind cross-cutting choices (nanopass discipline, h3 WASM bridge, license gate, Kafka port, leeway membership-role classifier, …).
* **Changelog** — [`doc/changelog/`](doc/changelog/) compiles window-bounded change summaries, one entry per two-to-four-week window, each opening with a hash-free *window in brief*; [`INDEX.md`](doc/changelog/INDEX.md) is a generated table of contents over the entries.
* **Per-package docs** — larger subsystems co-locate `TUTORIAL.md` / `HOWTO.md` / `EXPLANATION.md` / reference docs with their source (e.g. [`public/db/clickhouse/dsl/EXPLANATION.md`](public/db/clickhouse/dsl/EXPLANATION.md)).
* **How-to guides** — [`doc/howto/`](doc/howto/) carries task recipes: ingest a markdown vault and query its graph, snapshot a file tree into ClickHouse, launch apps headlessly, diagnose render jank, adopt boxer's standards downstream.
* **Trials** — [`doc/trials/`](doc/trials/) holds reproducible measurement protocols in the sea-trials sense: standardized runs repeated against later builds. Each trial's README §0 states the one claim its numbers carry and the condition that claim holds under; the tables under `runs/` are per-arm evidence, not citable figures.
* **Standards** — [`CODINGSTANDARDS.md`](CODINGSTANDARDS.md) and [`doc/DOCUMENTATION_STANDARD.md`](doc/DOCUMENTATION_STANDARD.md).
* **Engineering practices** — [`doc/ENGINEERING_PRACTICES.md`](doc/ENGINEERING_PRACTICES.md) catalogues CI workflows, static analysis, build-tag discipline, supply-chain gates, and in-tree governance.

## Style Conventions
### File Extensions
Boxer uses chained file extensions (e.g. `file.docx.pdf.txt`):
<dl>
<dt><code>.out.&lt;ext&gt;</code></dt>
<dd>Generated source code checked into the repository, e.g. <code>myfile.out.go</code>.</dd>
<dt><code>.gen.&lt;ext&gt;</code></dt>
<dd>Source code generated during the regular build (part of the binary distribution, not the source distribution), e.g. <code>myfile.gen.go</code>.</dd>
<dt><code>.idl.go</code></dt>
<dd>A (Framed) Foreign Function Interface (FFI) Interface Definition Language file — a subset of the Go language.</dd>
</dl>

### Folders
Path specificity increases with depth. Example: `public/analytics/timeseries/matrixprofile` — `analytics` is the kind of work, `timeseries` the shape of data it is done on, `matrixprofile` the one algorithm. The same descent reads elsewhere: `public/semistructured/markdown/obsidian`, `public/science/geo/h3`, `public/math/numerical/timeticks`.

Ideally the leaf package name is discriminative enough to drive IDE autocompletion.

### House names

A subsystem large enough to earn a name of its own gets a nautical one. The
convention is recorded in [ADR-0035](doc/adr/0035-keelson-namespace-introduction.md)
and restated in [ADR-0204 §SD1](doc/adr/0204-leaflet-map-core-port.md); it buys
a name that is discriminative in autocompletion and that does not collide with
whatever the plain word already means here — `lading` rather than `fs`, because
a membership named `fsMode` reads as something `io/fs` defines. Packages named
for a well-known technical term (`fec`, `dsl`) keep it; the metaphor is
for the parts that have no such term. New house names belong in this table.

| Name | At sea | In boxer |
| --- | --- | --- |
| `keelson` | the internal timber running along the keel, tying the floor frames to it | the application runtime — `public/keelson` ([ADR-0035](doc/adr/0035-keelson-namespace-introduction.md)) |
| `leeway` | the margin of sea-room kept to leeward, between a vessel and the shore it must not drift onto | the columnar data-mapping engine — `public/semistructured/leeway` |
| `anchor` | the fixed point a ship rides to | leeway's showcase schema and shared test fixture — `public/semistructured/leeway/anchor` |
| `lading` | a bill of lading is issued once per voyage, lists exactly what was loaded, and is never amended — only superseded by the next one | the filesystem snapshot store — `public/fs/lading` ([ADR-0198](doc/adr/0198-fs-snapshot-store.md)) |
| `tally` | the count of cargo checked against the bill of lading | the browser over the lading store — `apps/tally` ([ADR-0200](doc/adr/0200-tally-lading-browser.md)) |
| `portolan` | a chart ruled with rhumb lines | the Web-Mercator map widget — `public/thestack/imzero2/egui2/widgets/portolan` ([ADR-0204](doc/adr/0204-leaflet-map-core-port.md)) |
| `watchbill` | the roster that assigns each hand a duty on each watch | durable work as job rows claimed by writing — `public/keelson/runtime/watchbill` ([ADR-0223](doc/adr/0223-watchbill-durable-work-on-facts.md)) |

### Glossary
<dl>
<dt>e2e</dt><dd>End-to-end.</dd>
<dt>ea</dt><dd>Input/output; kept distinct from <code>io</code> so the standard library's package stays reachable beside it.</dd>
<dt>fec</dt><dd>Forward error correction.</dd>
<dt>inst</dt><dd>Instance (similar to self / this).</dd>
<dt>vcs</dt><dd>Version control system (git, svn, hg, perforce, …).</dd>
</dl>

## Compliance
Third-party licenses are vetted by a CI gate that builds a CycloneDX SBOM with `cyclonedx-gomod` and enforces the project policy (ADR-0004). Inline ports of third-party code, the bundled `h3.wasm` artifact's license chain, and the gate's policy are documented in [`THIRD_PARTY_NOTICES.md`](THIRD_PARTY_NOTICES.md). [`NOTICE`](NOTICE) carries the project's own attribution.

## Contributing
Currently, no third-party contributions are accepted.

## AI Codegen Declaration
Code and documentation up to commit [`aa78183`](https://github.com/stergiotis/boxer/commit/aa78183adc2de0b0266d34f476b543d122af04a7) is 100% human-generated; subsequent work includes substantial LLM contributions. Per-commit authorship is recorded in the git history via `Co-Authored-By` trailers — the provenance source of record — and summarised over time by `boxer gov repo authorship`. Earlier revisions additionally gated LLM-authored files behind `llm_generated_*` build tags so an AI-free build stayed possible; that gate was retired once it no longer described a useful build (see [ADR-0083](doc/adr/0083-retire-llm-generated-build-tags.md)). The provenance it cached remains derivable from the trailers.

## License
The MIT License (MIT) 2023-2026 — [Panos Stergiotis](https://github.com/stergiotis/). See [LICENSE](LICENSE) for full terms.
