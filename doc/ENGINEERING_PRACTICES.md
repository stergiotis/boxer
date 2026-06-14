---
type: reference
audience: contributor
status: draft
# reviewed-by: "@<handle>"   # fill in and uncomment when flipping to stable
# reviewed-date: YYYY-MM-DD  # fill in and uncomment when flipping to stable
---

> **Status: draft — pre-human-review.** Not verified; do not cite as authoritative.

# Engineering Practices

This document catalogues the software-engineering practices wired into the
boxer repository: continuous integration, static analysis, tests, supply-chain
gates, custom in-tree governance, documentation enforcement, and the
project-specific coding standard. It is descriptive — a reference for
contributors orienting themselves to the toolchain — and cross-references the
norms common to comparable Go projects where the comparison helps frame the
choice.

For the documentation rules themselves, see
[doc/DOCUMENTATION_STANDARD.md](./DOCUMENTATION_STANDARD.md). For coding
conventions, see [CODINGSTANDARDS.md](../CODINGSTANDARDS.md).

## 1. CI surface

GitHub Actions workflows live under
[.github/workflows](../.github/workflows). Each workflow handles one concern;
none combine multiple gates.

| Workflow | Trigger | Entry point | Purpose |
|---|---|---|---|
| [lint.yaml](../.github/workflows/lint.yaml) | push to `main`, PRs | [scripts/ci/lint.sh](../scripts/ci/lint.sh) | `go vet`, staticcheck, errcheck, doclint, h3 wasm parity |
| [test.yaml](../.github/workflows/test.yaml) | every push | [scripts/ci/gotest.sh](../scripts/ci/gotest.sh) | race + cover + JSON tests, tparse-formatted |
| [vuln.yaml](../.github/workflows/vuln.yaml) | every push | [scripts/ci/govuln.sh](../scripts/ci/govuln.sh) | `govulncheck -show verbose ./public/...` |
| [licenses.yaml](../.github/workflows/licenses.yaml) | every push | [scripts/ci/license_gate.sh](../scripts/ci/license_gate.sh) | CycloneDX SBOM → in-tree policy gate |
| [codestat.yaml](../.github/workflows/codestat.yaml) | push, PR, weekly cron | inline | `scc` line counts split human vs. LLM, dependency inventory, authorship attribution |

Splitting CI per concern is the convention in larger Go projects
(Kubernetes, etcd, Cockroach). Smaller Go projects more often consolidate
into a single workflow.

## 2. Static analysis

Analyzers are declared as `go tool` directives in
[go.mod](../go.mod) (the mechanism standardised in Go 1.24); no separate
`tools.go` build hack and no per-tool `go install` in CI. The orchestrating
script [scripts/ci/lint.sh](../scripts/ci/lint.sh) runs them sequentially and
emits a pass/warn/fail summary trailer.

| Tool | Invocation | Status |
|---|---|---|
| `go vet` | direct, with build tags | error on findings |
| [honnef.co/go/tools/cmd/staticcheck](https://pkg.go.dev/honnef.co/go/tools/cmd/staticcheck) | `-checks "all,-ST1000,-ST1003,..."` (style checks suppressed) | warn |
| [github.com/kisielk/errcheck](https://pkg.go.dev/github.com/kisielk/errcheck) | exclusions for `fmt.Fprintf` / `strings.Builder` writers | warn |
| [go.uber.org/nilaway](https://pkg.go.dev/go.uber.org/nilaway) | available, currently disabled in CI; runnable via [scripts/dev/nilaway.sh](../scripts/dev/nilaway.sh) | (disabled) |
| [github.com/dkorunic/betteralign](https://pkg.go.dev/github.com/dkorunic/betteralign) | dev-only via [scripts/dev/betteralign.sh](../scripts/dev/betteralign.sh) | dev |
| [github.com/incu6us/goimports-reviser/v3](https://pkg.go.dev/github.com/incu6us/goimports-reviser/v3) | dev-only via [scripts/dev/goimports.sh](../scripts/dev/goimports.sh) | dev |

Generated files (`*.gen.go`, `*.out.go`) are filtered post-hoc by grep since
`go vet` has no native exclude flag.

Most reference Go repos (Kubernetes, GitLab Runner, Grafana) drive these
checks through a meta-runner that bundles staticcheck/errcheck/govet and
others behind a single YAML config. This repo runs each analyzer directly
and aggregates results in a shell script. The direct-invocation approach
trades the meta-runner's parallel scheduling and unified config for simpler
debuggability and individually-versioned tool pins.

## 3. Build-tag discipline

The file [./tags](../tags) is a single-line, comma-separated set of build tags
read by every build, test, and lint script. Its active tags are
`identifier_tag_fixed16`, `boxer_enable_profiling`, and `goexperiment.jsonv2`;
packages that opt into one of these fail to type-check with misleading
"undefined" errors when the tags are omitted.

Until 2026-06 the set also carried per-model `llm_generated_*` author tags
(`gemini3pro`, `opus46`, `opus47`, `opus48`) that gated every LLM-authored file
so an AI-free build stayed possible. That scheme was retired
([ADR-0083](adr/0083-retire-llm-generated-build-tags.md)): authorship provenance
now lives in git `Co-Authored-By` trailers, surfaced by `gov repo authorship`,
and the directives were stripped from ~1130 files.

Centralising tags in a tracked file is uncommon — most Go projects either
avoid tags or scatter them across per-package `Makefile` targets. The
pattern here resembles Cockroach's env-driven `*_GOFLAGS` but more
explicit.

## 4. Tests

The CI test runner is [scripts/ci/gotest.sh](../scripts/ci/gotest.sh):

```sh
go test -race -json -short -cover -tags "$tags" ./... \
  | go tool tparse -progress -trimpath -slow 20
```

- [github.com/mfridman/tparse](https://pkg.go.dev/github.com/mfridman/tparse)
  surfaces progress and the 20 slowest tests.
- Coverage HTML is produced locally via
  [scripts/dev/coveragehtml.sh](../scripts/dev/coveragehtml.sh), which reads
  `$GOCOVERDIR` and pipes `go tool covdata textfmt` into `go tool cover -html`.
- Integration tests are explicitly tagged
  (`//go:build integration`) and use
  [testcontainers-go](https://pkg.go.dev/github.com/testcontainers/testcontainers-go);
  see [the Kafka integration test](../public/streaming/persisted/kafka/integration_test.go),
  which boots a Redpanda container. The tag doubles as a dependency-isolation
  gate: under the default tag set, the testcontainers + Moby + OCI +
  containerd + gopsutil chain (29 transitive modules, ≈41 MB in `$GOMODCACHE`,
  59 entries in [go.sum](../go.sum)) is absent from `go list -deps ./...`, so
  default `go build` and `go test -short` do not compile or download it. CI
  opts into the chain only when running integration jobs. The same pattern
  applies to any future test that would introduce a comparably heavy
  dependency — gate it with a build tag rather than letting it leak into
  every developer's build.
- `example_test.go` files are reserved for the *How-To* quadrant of Diátaxis
  per [§1 of DOCUMENTATION_STANDARD.md](./DOCUMENTATION_STANDARD.md#how-to-guides-problem-oriented);
  current count is low, representing an under-served convention rather than an
  absent one.

`tparse` for test progress is uncommon (most Go CIs accept raw `go test -v`).
The `-race` + `-short` combination matches the Go standard library's own CI
defaults. Testcontainers is the modern alternative to home-grown Docker
harnesses, used by, e.g., the Kafka Go clients, Temporal, and the NATS test
suites.

## 5. Custom in-tree governance (`boxer gov`)

Two project-specific checks have no off-the-shelf equivalent and are
implemented as subcommands of the project binary.

### `boxer gov doclint`

Sources under [github.com/stergiotis/boxer/public/gov/doclint](../public/gov/doclint).
Implements numbered rules `DL001`–`DL011` over Markdown front-matter, draft
banners, ADR section completeness, link resolution, banned filenames, and Go
doc-comment hygiene. Findings carry one of three severities:

- `error` — sets the script's exit code to 1.
- `warn` — visible in output but non-blocking.
- `info` — surfaced for baseline cleanup; non-blocking.

The full invariant-to-rule mapping is in
[§8 of DOCUMENTATION_STANDARD.md](./DOCUMENTATION_STANDARD.md#enforcement).

### `boxer gov llmtag`

Sources under [github.com/stergiotis/boxer/public/gov/llmtag](../public/gov/llmtag).
Attributes line authorship via `git blame` plus `Co-Authored-By` trailers and
can apply or strip `//go:build llm_generated_<model>` directives. This
per-model build-tag governance was **retired**
([ADR-0083](adr/0083-retire-llm-generated-build-tags.md)): the directives were
stripped tree-wide, the CI gate dropped, and human-vs-LLM attribution moved to
`gov repo authorship` reading the `Co-Authored-By` trailers directly. The tool
is kept dormant as the reversal path — `gov llmtag --apply` reconstructs the
tags from history at any time.

Documentation-as-code linting is well-developed in the Python ecosystem
(`sphinx-build -W`, `interrogate`) but rare in Go — most repos rely on
`gofmt` / `go vet` for doc comments and human review for Markdown. LLM
authorship tracking by build tag was idiosyncratic to this project; it has
since been retired in favour of trailer-based attribution.

## 6. Supply-chain and license gates

- `govulncheck` runs against `./public/...` on every push via the
  [vuln workflow](../.github/workflows/vuln.yaml).
- The license gate
  ([scripts/ci/license_gate.sh](../scripts/ci/license_gate.sh)) generates a
  CycloneDX 1.6 SBOM via
  [cyclonedx-gomod](https://pkg.go.dev/github.com/CycloneDX/cyclonedx-gomod)
  (`mod -licenses -test`), then feeds it to
  [`boxer gov license-gate`](../public/gov/licensegate) (subcommand registered under boxer's top-level CLI),
  which applies a forbidden/restricted policy. The driver and rationale are
  documented in [ADR-0004](./adr/0004-license-gate-cyclonedx.md): boxer is
  MIT-licensed and cannot accept copyleft inbound dependencies. Unknown
  classifications surface as advisories, not failures, so detector gaps do
  not block CI.
- No vendoring; [go.mod](../go.mod) is authoritative. The
  [test workflow](../.github/workflows/test.yaml) contains a (currently
  commented-out) `go mod tidy --diff` drift check.

CycloneDX SBOMs are increasingly standard for SLSA / supply-chain conscious
projects (Kubernetes, OpenTelemetry, GitHub itself). Many repos delegate
license enforcement to external tools (`fossa-cli`, `licensed`); the in-tree
policy gate here is less common.

## 7. Reproducibility of native artifacts

[scripts/ci/h3_wasm_parity.sh](../scripts/ci/h3_wasm_parity.sh) rebuilds the
Rust crate at [rust/h3bridge](../rust/h3bridge) targeting
`wasm32-unknown-unknown` with a pinned `CONST_RANDOM_SEED`, optionally
passes the output through `wasm-strip` and `wasm-opt`, and byte-compares
against the committed
[h3.wasm artifact](../public/science/geo/h3/internal/h3o_wasm/h3.wasm).

The check skips gracefully on machines without the Rust toolchain (cargo or
the wasm target absent), so local lint stays green for contributors not
touching the bridge; CI is the enforcer. Drift exits non-zero with a diff of
section headers when `wasm-objdump` is available.

Byte-equality drift checks on embedded native artifacts are uncommon outside
reproducible-build communities (Bazel ecosystems, Bitcoin Core). The
local-skip / CI-enforce split keeps contribution friction low.

## 8. Documentation architecture

- **Diátaxis** (Reference / How-To / Explanation / Tutorial) is the operative
  taxonomy. Reference lives in Go doc comments and `doc.go`; How-To in
  `example_test.go`; Explanation in `EXPLANATION.md`; tutorials at module
  roots.
- **Architecture Decision Records** under
  [doc/adr](./adr), monotonically numbered and append-only, with a state
  machine (`proposed → accepted / deferred / deprecated / superseded`). A
  Questions–Options–Criteria (QOC) matrix is required when a decision
  involves ≥3 options × ≥3 criteria.
- **Front-matter** (YAML stanza with `type`, `status`, `reviewed-by`,
  `reviewed-date`) is mandatory on every Markdown doc except the root
  [README.md](../README.md) and per-module `README.md` landing pages. The
  stanza is mechanically checked by `doclint` rule DL001.
- **Migration guides** under
  [doc/migration](./migration) (e.g. quarterly
  `YYYY-MM-qN.md` files) rather than scattered changelog entries.

Diátaxis adoption is well-established in Django, NumPy, and the Cloudflare
docs site; it is rare in Go server projects, which more often default to
"godoc plus a top-level README." The ADR convention is widely adopted
(Thoughtworks-popularised; analogues include the Kubernetes Enhancement
Proposals); few Go projects formalise the state machine or mechanically
enforce front-matter.

## 9. Coding standard

[CODINGSTANDARDS.md](../CODINGSTANDARDS.md) codifies non-idiomatic project
conventions, including:

- `eh.Errorf` and `eb` error builders in place of `fmt.Errorf`,
- `I` suffix on interface names, `E` suffix on enum types,
- struct-of-arrays preferred over array-of-structs,
- `iter.Seq2` iterators preferred over slice returns,
- zero-value usability when feasible, otherwise an exported `New` constructor
  with unexported fields.

These are human-enforced; no linter checks them.

Most Go projects defer to *Effective Go* and `gofmt`. Project-specific style
guides at this depth are more common in C++ ecosystems (Google, LLVM) than
in Go; the closest Go analogue is the published Uber Go style guide.

## 10. Notably absent

The following are widely used in comparable Go projects but are not wired
into this repository's CI:

- `gofumpt` / `gci` formatting enforcement is absent.
- `nilaway` is wired up but currently commented out in
  [scripts/ci/lint.sh](../scripts/ci/lint.sh); the `dev/` script preserves
  the local runner.
- No `CODEOWNERS`, PR template, or branch-protection automation. The
  documentation standard records an explicit "AI-assisted, direct-to-`main`"
  workflow assumption
  ([§4 of DOCUMENTATION_STANDARD.md](./DOCUMENTATION_STANDARD.md#front-matter-and-document-state-markdown-only)).
- No release automation (`goreleaser` or equivalent) and no container build
  pipeline.
- No fuzz-test workflow despite parser and codec surface (`go test -fuzz` is
  supported by the toolchain but not scheduled).
- Coverage is computed but not uploaded to a coverage service (Codecov,
  Coveralls, etc.).

## 11. Summary

The combination is internally consistent: a Go-tool-driven, single-binary,
single-shell-orchestrator pipeline with custom in-tree governance for
documentation and LLM authorship. The trade-off versus a meta-runner setup
is broader linter coverage in exchange for tighter control over individual
tool versions and bespoke checks that no off-the-shelf runner provides.
