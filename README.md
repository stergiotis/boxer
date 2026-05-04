# boxer

[![Go Reference](https://pkg.go.dev/badge/github.com/stergiotis/boxer.svg)](https://pkg.go.dev/github.com/stergiotis/boxer) [![Go Report Card](https://goreportcard.com/badge/github.com/stergiotis/boxer)](https://goreportcard.com/report/github.com/stergiotis/boxer)

## Maturity
Alpha, incomplete test coverage, unstable, API may still change heavily.

## Installation
```
go get github.com/stergiotis/boxer
```

## What's inside
Boxer is a collection of packages under `public/`. The larger subsystems:

* `algebraicarch/pushout` — algebraic three-way merge for line-graphs via categorical pushouts: `graggle/store` for the line-graph data structure, `graggle/patch` for the patch DAG, `envelope` for transmittable patches; the *graggle* and pseudo-edge constructs follow Joe Neeman's [ojo](https://github.com/jneem/ojo) design (see [`pushout/graggle/NOTICE`](public/algebraicarch/pushout/graggle/NOTICE)); ported from pebble2impl with full history. Includes a `BackendI`/`RepoI` seam under `algebraicarch/pushout/pijul` with two realisations — a native pushout backend (no external binary) and a text backend that shells out to `pijul`.
* `semistructured/leeway` — code-driven entity-attribute-value data model with a staged codegen pipeline (DDL / DML / read-access / streaming read-access).
* `semistructured/markdown/obsidian` — goldmark-based parser for Obsidian-flavored markdown (callouts, wikilinks, embeds, tags, highlights, frontmatter).
* `db/clickhouse/dsl` — typed ClickHouse SQL DSL with an AST, marshalling, and nanopass rewrite passes (ADR-0002, ADR-0006).
* `streaming/persisted/kafka` — embedded Kafka producer/consumer derived from Redpanda Connect's franz-go integration (ADR-0005).
* `caching` — read-through batch cache aimed at ETL / build / graph-traversal pipelines: latency-hidden via dependency accumulation and partition-aware bulk fetches, with optional disk-backed L2.
* `analytics/similarity/compression` — compression-based similarity metrics (NCD, CCC) over any `Reset`-able compressor.
* `math/numerical/finddivisions` and `math/numerical/timeticks` — axis-tick layout: Heckbert / Wilkinson / Talbot for numeric and log axes; a uPlot-derived calendar ladder with locale-aware boundary snapping for time axes.
* `science/geo/h3` — H3 geospatial indexing via a Rust→WASM→wazero bridge (ADR-0003); Rust source under `rust/h3bridge`.
* `fec` — forward error correction (e.g. `fec/ea/golay24`).
* `eb`, `eh` — structured error building and error handling.
* `batching`, `containers`, `hashing`, `identity`, `logical`, `observability`, `parsing`, `slices`, `statespace`, `unsafeperf`, … — utility packages.

`internal/` carries vendored third-party ports.

`imzero` and `fffi` were extracted into [`imzero_imgui`](https://github.com/stergiotis/imzero_imgui) (ImZero1) and are no longer part of this module.

## Building
Boxer uses Go build tags to gate optional features, Go experiments, and AI-generated code paths. The canonical tag set lives in [`./tags`](tags); pass it to every `go build`, `go test`, and `go vet` invocation:

```
go build -tags="$(cat ./tags)" ./...
go test  -tags="$(cat ./tags)" ./...
go vet   -tags="$(cat ./tags)" ./...
```

Without these tags, packages fail to compile with misleading *undefined identifier* errors.

## Documentation
Boxer follows the [Diátaxis](https://diataxis.fr/) framework (ADR-0001). Docs live next to the code they describe:

* **Architecture decisions** — [`doc/adr/`](doc/adr/) records the *why* behind cross-cutting choices (nanopass discipline, h3 WASM bridge, license gate, Kafka port, leeway membership-role classifier, …).
* **Per-package docs** — larger subsystems co-locate `TUTORIAL.md` / `HOWTO.md` / `EXPLANATION.md` / reference docs with their source (e.g. [`public/db/clickhouse/dsl/EXPLANATION.md`](public/db/clickhouse/dsl/EXPLANATION.md)).
* **Standards** — [`CODINGSTANDARDS.md`](CODINGSTANDARDS.md) and [`doc/DOCUMENTATION_STANDARD.md`](doc/DOCUMENTATION_STANDARD.md).

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
Path specificity increases with depth. Example: `./fec/ea/golay24` —
`fec` is forward error correction (a [well-known technical term](https://simple.wikipedia.org/wiki/Forward_error_correction)); `ea` is *Eingabe-Ausgabe* (German for input/output, chosen to avoid clashing with stdlib `io`); `golay24` is the specific algorithm.

Ideally the leaf package name is discriminative enough to drive IDE autocompletion.

### Glossary
<dl>
<dt>e2e</dt><dd>End-to-end.</dd>
<dt>ea</dt><dd>Input-output (German abbreviation, to distinguish from core packages).</dd>
<dt>fec</dt><dd>Forward error correction.</dd>
<dt>inst</dt><dd>Instance (similar to self / this).</dd>
<dt>vcs</dt><dd>Version control system (git, svn, hg, perforce, …).</dd>
</dl>

## Compliance
Third-party licenses are vetted by a CI gate that builds a CycloneDX SBOM with `cyclonedx-gomod` and enforces the project policy (ADR-0004). Inline ports of third-party code, the bundled `h3.wasm` artifact's license chain, and the gate's policy are documented in [`THIRD_PARTY_NOTICES.md`](THIRD_PARTY_NOTICES.md). [`NOTICE`](NOTICE) carries the project's own attribution.

## Contributing
Currently, no third-party contributions are accepted.

## AI Codegen Declaration
Code and documentation up to commit [`aa78183`](https://github.com/stergiotis/boxer/commit/aa78183adc2de0b0266d34f476b543d122af04a7) is 100% human-generated. Subsequent code with substantial LLM contributions is gated by `llm_generated_*` build tags (see [Building](#building)) so AI-free builds remain possible.

## License
The MIT License (MIT) 2023-2026 — [Panos Stergiotis](https://github.com/stergiotis/). See [LICENSE](LICENSE) for full terms.
