---
type: reference
audience: contributor
status: stable
reviewed-by: "p@stergiotis"
reviewed-date: 2026-06-24
---

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

**Every workflow runs on manual dispatch and on a `v*` tag, and nothing runs on
an ordinary push or a pull request.** The Trigger column below records only what
each adds to that baseline. Local runs of the same entry-point scripts are
therefore the working gate; CI is the gate at a release. That follows from the
AI-assisted, direct-to-`main` workflow §10 records, and it is the assumption to
revisit first if the repo ever takes external contributions.

| Workflow | Trigger adds | Entry point | Purpose |
|---|---|---|---|
| [lint.yaml](../.github/workflows/lint.yaml) | — | [scripts/ci/lint.sh](../scripts/ci/lint.sh) | `gofmt`, `go vet`, staticcheck, errcheck, doclint, h3 wasm parity |
| [test.yaml](../.github/workflows/test.yaml) | — | [scripts/ci/gotest.sh](../scripts/ci/gotest.sh) | race + cover + JSON tests, tparse-formatted; post-test drift gate (generator tests rewrite in place, so the tree must end clean) |
| [vuln.yaml](../.github/workflows/vuln.yaml) | — | [scripts/ci/govuln.sh](../scripts/ci/govuln.sh) | `govulncheck -show verbose ./public/...` |
| [licenses.yaml](../.github/workflows/licenses.yaml) | — | [scripts/ci/license_gate.sh](../scripts/ci/license_gate.sh) | CycloneDX SBOM → in-tree policy gate |
| [codestat.yaml](../.github/workflows/codestat.yaml) | weekly cron (Mon 06:00 UTC) | inline | `scc` line counts split human vs. LLM, dependency inventory, authorship attribution |
| [codeql.yaml](../.github/workflows/codeql.yaml) | weekly cron (Tue 05:37 UTC) | GitHub CodeQL action | CodeQL security scan of the Go tree, built with the repo's build tags |
| [scorecard.yaml](../.github/workflows/scorecard.yaml) | weekly cron (Tue 07:20 UTC), branch-protection changes | OSSF `scorecard-action` | supply-chain posture score, uploaded as SARIF to code scanning |

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
| `gofmt` | `-l` over the tree, generated files skipped by their header | error on drift |
| `go vet` | direct, with build tags | error on findings |
| [honnef.co/go/tools/cmd/staticcheck](https://pkg.go.dev/honnef.co/go/tools/cmd/staticcheck) | `-checks "all,-ST1000,-ST1003,..."` (style checks suppressed), one package withheld | warn — see below |
| [github.com/kisielk/errcheck](https://pkg.go.dev/github.com/kisielk/errcheck) | exclusions for `fmt.Fprintf` / `strings.Builder` writers | warn |
| [go.uber.org/nilaway](https://pkg.go.dev/go.uber.org/nilaway) | available, currently disabled in CI; runnable via [scripts/dev/nilaway.sh](../scripts/dev/nilaway.sh) | (disabled) |
| [github.com/dkorunic/betteralign](https://pkg.go.dev/github.com/dkorunic/betteralign) | dev-only via [scripts/dev/betteralign.sh](../scripts/dev/betteralign.sh) | dev |
| [github.com/incu6us/goimports-reviser/v3](https://pkg.go.dev/github.com/incu6us/goimports-reviser/v3) | dev-only via [scripts/dev/goimports.sh](../scripts/dev/goimports.sh) | dev |

**staticcheck withholds one package under Go 1.27 (2026-08-21).** The pin is
`v0.8.0`; `v0.7.0` cannot read 1.27 export data at all, failing every package
with `export data version 4 is greater than maximum supported version 2` before
an analyzer sees it. `v0.8.0` analyses 538 of the 539 packages under
`./public/...`. The exception is `egui2/widgets/componentview`, where Go 1.27
generic methods meet an upstream bug: staticcheck keys cross-package facts by
ordinal method paths, source order and export-data order number a type's methods
differently once generic and non-generic ones mix, and `SA4023` then indexes
past the end of a fact belonging to another method — taking the process, and
every other package's analysis, with it. `-checks` cannot dodge it, since
staticcheck runs every analyzer and filters diagnostics afterwards.
[lint.sh](../scripts/ci/lint.sh) therefore carries a named skip list, prints what
it withheld, and stays `warn` while the list is non-empty; the `DID NOT RUN`
branch remains as the backstop for a package the list does not name yet. The
mis-attribution is silent elsewhere, so treat a fact-carrying finding (`U1000`)
naming a method of `marshallreflect.SectionReaders` or `ecsdemo/stage2.FatRow`
with suspicion. The measurements are in
[ADR-0199](./adr/0199-adopt-go-1-27.md) §Updates. Re-check on the next
staticcheck release.

Generated files (`*.gen.go`, `*.out.go`) are filtered post-hoc by grep since
`go vet` has no native exclude flag. The `gofmt` step filters on the
`// Code generated ... DO NOT EDIT.` header instead: two generated files match
neither path pattern, and a generator owns the layout of what it emits.

**When the `gofmt` step fails, read `gofmt -d` before running `gofmt -w`.**
gofmt reformats doc comments, and its parser takes two kinds of ordinary prose
punctuation for markup: `` `` `` and two single quotes become Unicode quotes,
and a line-leading `+` becomes a `-` bullet. Both have falsified comments in
this repo — one documenting SQL's doubled-quote escape, one where the `+`
continued a sum and would have become a minus. Where the diff changes what a
comment *says* rather than how it is spaced, fix the prose first: name the thing
instead of spelling it, or rewrap so a `+` ends a line rather than starting one.

Most reference Go repos (Kubernetes, GitLab Runner, Grafana) drive these
checks through a meta-runner that bundles staticcheck/errcheck/govet and
others behind a single YAML config. This repo runs each analyzer directly
and aggregates results in a shell script. The direct-invocation approach
trades the meta-runner's parallel scheduling and unified config for simpler
debuggability and individually-versioned tool pins.

## 3. Build-tag discipline

The file [./tags](../tags) is a single-line, comma-separated set of build tags
read by every build, test, and lint script. Its one active tag is
`boxer_enable_profiling`, which selects a compile-out arm rather than gating
compilation: omit it and the disabled arm builds cleanly.

**Nothing is required any more.** Until 2026-08 the set also carried
`goexperiment.jsonv2`, which gated `encoding/json/v2` across the tree and failed
the build with misleading "undefined" errors when omitted. `encoding/json/v2`
graduated in Go 1.27 and the tag was retired
([ADR-0199](adr/0199-adopt-go-1-27.md)); `gov buildtags` now publishes an empty
required set, and a consuming repository needs no tags at all — which is what
makes `go tool` delivery of boxer's CLI work for one.

Until 2026-07 the set also carried an `identifier_tag_fixed<N>` tag selecting
a compile-time identifier tag width; that axis was retired with the switch to
self-describing fibonacci-coded tags
([ADR-0106](adr/0106-identity-fibonacci-tags-build-tag-retirement.md)) — do
not reintroduce scheme-selection tags. Until 2026-06 the set also carried
per-model `llm_generated_*` author tags
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
- Integration tests are explicitly tagged (`//go:build integration`) and run by
  their own runner, [scripts/ci/gotest-integration.sh](../scripts/ci/gotest-integration.sh):

  ```sh
  go test -race -json -count=1 -p 1 -tags "$tags,integration" ./... \
    | go tool tparse -progress -trimpath -slow 20
  ```

  The default `go test ./...` neither compiles nor runs them. A test belongs in
  the lane for either of two reasons:

  - **Heavy dependency.** [The Kafka integration test](../public/streaming/persisted/kafka/integration_test.go)
    boots a Redpanda container via
    [testcontainers-go](https://pkg.go.dev/github.com/testcontainers/testcontainers-go).
    The tag doubles as a dependency-isolation gate: under the default tag set,
    the testcontainers + Moby + OCI + containerd + gopsutil chain (29
    transitive modules, ≈41 MB in `$GOMODCACHE`, 59 entries in
    [go.sum](../go.sum)) is absent from `go list -deps ./...`, so default
    `go build` and `go test -short` do not compile or download it. Gate any
    future test that would introduce a comparably heavy dependency the same
    way, rather than letting it leak into every developer's build.
  - **Shared live server.** The ClickHouse tests reach one server at
    localhost:8123 and share `system.query_log` with each other. They create
    and drop scratch databases and read back rows they just wrote, bounded by
    wall-clock polls — so run beside the rest of the suite they fail on
    contention rather than on behaviour. This is why the runner passes `-p 1`;
    it is a correctness requirement of the lane, not a preference.

  A member must not inherit an unbounded workload from the host. The
  queryrunsvc pipeline test used to: a fresh destination makes the capture
  extract backfill the machine's whole `system.query_log` retention,
  oldest-first at a batch cap per refresh, and the probe it waits for is the
  newest row — so its duration was proportional to however much history the
  machine happened to hold (119k rows here), and it failed under load and under
  `-race`, which slowed the drain ~20x. It now starts its capture at `now`
  (`queryrunsvc.Config.BackfillFrom`), which is O(1) in host history. Prefer
  that shape: a live-server test should bound what it asks the server to do.

  Skipping when the server is unreachable is a *capability* gate, not lane
  membership: on a developer machine that happens to be running ClickHouse, a
  probe-and-skip test executes for real. Both gates belong on such a test — the
  tag decides which lane it is in, the probe decides whether it can run there.
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
Implements numbered rules over Markdown front-matter, draft banners, ADR
section completeness and sub-item declarations, link resolution, banned
filenames, Go doc-comment hygiene, and stale review stamps. The numbering has
gaps: an id is reserved when a check is planned and assigned when it is
implemented, so `DL013` and `DL014` name checks that do not exist yet while
`DL015` ships (the register is DOCUMENTATION_STANDARD §8's table). Findings carry one of three severities:

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
- **Release signing.** Release tags are SSH-signed; anything that builds or
  deploys from a tag verifies the signature against a trusted public key first
  (`git verify-tag`). The imzero2 on-box deploy enforces it — it refuses to build
  an unsigned tag ([ADR-0085](./adr/0085-imzero2-demo-pull-build-atomic-deploy.md)
  SD8). Recipe: [How to sign and verify boxer releases](./howto/release-signing.md).

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
  involves ≥3 options × ≥3 criteria. The corpus is itself queryable:
  [`boxer adr`](./howto/adr-overview.md) loads every ADR's front-matter together
  with the `ADR-NNNN` markers found in source code into Apache Arrow tables and
  runs `clickhouse-local` over them, crossing the decision state machine against
  an implementation-degree signal read from how the code cites each ADR — which
  the [ADR-reference coding standard](../CODINGSTANDARDS.md#adr-references) keeps
  trustworthy.
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

- `gofumpt` / `gci` are not adopted. Plain `gofmt` *is* enforced in CI as of
  2026-08-06 (§ the linter table above); the stricter pair, and import grouping
  in particular, remain a dev-only convenience via
  [scripts/dev/goimports.sh](../scripts/dev/goimports.sh).
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
