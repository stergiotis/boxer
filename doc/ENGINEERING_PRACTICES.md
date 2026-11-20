---
type: reference
audience: contributor
status: stable
reviewed-by: "p@stergiotis"
reviewed-date: 2026-09-23
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

**Nothing runs on an ordinary push or a pull request.** Every workflow runs on
manual dispatch; most also run on a `v*` tag, and the Trigger column records
what each adds to dispatch. Local runs of the entry-point scripts are therefore
the working gate, the nightly run is the gate on `main`, and the tag-triggered
workflows with [scripts/ci/release.sh](../scripts/ci/release.sh) are the gate at
a release. That follows from the AI-assisted, direct-to-`main` workflow §10
records, and it is the assumption to revisit first if the repo ever takes
external contributions.

**The local gate tests a commit, not the working tree**
([ADR-0390](./adr/0390-a-gate-that-tests-the-commit-and-a-ref-that-names-the-last-green-main.md)).
The working tree is often shared by concurrent sessions and rarely clean, so a
test run in it tests whatever is lying there.
[scripts/ci/gate.sh](../scripts/ci/gate.sh) `[<rev>]` checks the revision out
into a detached worktree under `$TMPDIR` and runs, there: `go mod tidy --diff`,
`go mod verify`, the build, gofmt over the changed Go files, `go vet` and
`go test -race -short` over the packages changed since the merge-base with
`origin/main` plus their direct importers (test imports count), and the drift
check; when the range touches the egui2 IDL, its bindings or `rust/imzero2`,
also the IDL check, the Rust crate check and the browser host digest. It drops
`-race` above a package cap and says so. The worktree has a stable path per
repository and user, so the Go build cache, which keys on the package
directory, carries over between runs. A committed hook runs the gate before a
push, once a clone opts in:

```sh
git config core.hooksPath scripts/dev/hooks   # git push --no-verify skips it once
```

**`refs/heads/last-green` names the newest `main` commit the nightly run passed.**
The nightly workflow moves it forward only — through the refs API with
`force=false` — and only when every job passed, so a consumer that wants a
commit something has tested can pin it.

| Workflow | Trigger adds | Entry point | Purpose |
|---|---|---|---|
| [lint.yaml](../.github/workflows/lint.yaml) | `v*` tag | [scripts/ci/lint.sh](../scripts/ci/lint.sh) | `gofmt`, `go vet`, staticcheck, errcheck, doclint, the IDL check, h3 wasm parity |
| [test.yaml](../.github/workflows/test.yaml) | `v*` tag | [scripts/ci/gotest.sh](../scripts/ci/gotest.sh) `--check-drift` | race + cover + JSON tests, tparse-formatted; post-test drift gate (generator tests rewrite in place, so the tree must end clean) |
| [nightly.yaml](../.github/workflows/nightly.yaml) | daily cron (02:17 UTC), no tag | the test, lint, integration and bench scripts | the default lane with a cross-package coverage profile (artifact + per-package table), the `binary_log` lane, the Go lint steps, the default and integration lanes against a pinned ClickHouse server, `gobench.sh smoke`, the viewer and watermark crates; advances `last-green` |
| [fuzz.yaml](../.github/workflows/fuzz.yaml) | weekly cron (Sat 04:23 UTC), no tag | [scripts/ci/gofuzz.sh](../scripts/ci/gofuzz.sh) | every fuzz target for a bounded time; uploads failing inputs and logs |
| [vuln.yaml](../.github/workflows/vuln.yaml) | `v*` tag | [scripts/ci/govuln.sh](../scripts/ci/govuln.sh) | `govulncheck -show verbose ./...`; OSV check of the Rust lockfiles |
| [licenses.yaml](../.github/workflows/licenses.yaml) | `v*` tag | [scripts/ci/license_gate.sh](../scripts/ci/license_gate.sh) | CycloneDX SBOM → in-tree policy gate |
| [codestat.yaml](../.github/workflows/codestat.yaml) | `v*` tag, weekly cron (Mon 06:00 UTC) | inline | `scc` line counts split human vs. LLM, dependency inventory, authorship attribution |
| [codeql.yaml](../.github/workflows/codeql.yaml) | `v*` tag, weekly cron (Tue 05:37 UTC) | GitHub CodeQL action | CodeQL security scan of the Go tree, built with the repo's build tags |
| [scorecard.yaml](../.github/workflows/scorecard.yaml) | `v*` tag, weekly cron (Tue 07:20 UTC), branch-protection changes | OSSF `scorecard-action` | supply-chain posture score, uploaded as SARIF to code scanning |
| [formal-pushout.yaml](../.github/workflows/formal-pushout.yaml) | `v*` tag | `npm run check` + `npm run findings` in [verification/formal/algebraicarch/pushout](../verification/formal/algebraicarch/pushout/README.md) | Quint typecheck + witness runs, Apalache bounded verifies, TLC liveness and the expected-violation counterexamples for the pushout protocol specs; ~15 min |

Splitting CI per concern is the convention in larger Go projects
(Kubernetes, etcd, Cockroach). Smaller Go projects more often consolidate
into a single workflow.

## 2. Static analysis

Analyzers are declared as `go tool` directives in
[go.mod](../go.mod) (the mechanism standardised in Go 1.24); no separate
`tools.go` build hack and no per-tool `go install` in CI. The orchestrating
script [scripts/ci/lint.sh](../scripts/ci/lint.sh) runs them sequentially and
emits a pass/warn/fail summary trailer.

**One step, one script.** `lint.sh` holds the step list, the banners, the
timing and the trailer; each step lives under
[scripts/ci/lint/](../scripts/ci/lint/), or beside the driver for the four that
predate the split (`h3_wasm_parity.sh`, `rust_imzero2_check.sh`,
`repro_build_parity.sh`, `fetcher-discipline.sh`). A step takes no argument,
prints its own findings, and reports its verdict through its exit status: `0`
pass, non-zero fail. A warn-only step exits `2` *and* carries the `warn` marker
on its line in `lint.sh` — both, because under `set -e` the status is whatever
tool died last (`tar` exits 2 on a write error), and because whether a check
blocks is the gate's decision rather than the checker's. So a step runs on its
own, `scripts/ci/lint.sh <step> ...` runs a subset, and the gate stays one list
in one file; `--list` names the steps and the scripts behind them.

| Tool | Invocation | Status |
|---|---|---|
| `gofmt` | `-l` over the tree, generated files skipped by their header | error on drift |
| `go vet` | `./...`, with build tags | error on findings |
| [honnef.co/go/tools/cmd/staticcheck](https://pkg.go.dev/honnef.co/go/tools/cmd/staticcheck) | `./...`, `-checks "all,-ST1000,-ST1003,..."` (style checks suppressed), one package withheld | error on a finding outside the baseline; warn on stale baseline entries and while the skip list is non-empty — see below |
| [github.com/kisielk/errcheck](https://pkg.go.dev/github.com/kisielk/errcheck) | `./...`, exclusions for `fmt.Fprintf` / `strings.Builder` writers | error on a finding outside the baseline; warn on stale baseline entries |
| `boxer-idl-check.sh --force` | the egui2 IDL regenerated into a scratch copy and compared with both sides' committed code; skips without rust/imzero2's toolchain | error on drift |
| [go.uber.org/nilaway](https://pkg.go.dev/go.uber.org/nilaway) | available, currently disabled in CI; runnable via [scripts/dev/nilaway.sh](../scripts/dev/nilaway.sh) | (disabled) |
| [github.com/dkorunic/betteralign](https://pkg.go.dev/github.com/dkorunic/betteralign) | dev-only via [scripts/dev/betteralign.sh](../scripts/dev/betteralign.sh) | dev |
| [github.com/incu6us/goimports-reviser/v3](https://pkg.go.dev/github.com/incu6us/goimports-reviser/v3) | dev-only via [scripts/dev/goimports.sh](../scripts/dev/goimports.sh) | dev |

**staticcheck and errcheck are held to a baseline, not to zero**
([ADR-0390](./adr/0390-a-gate-that-tests-the-commit-and-a-ref-that-names-the-last-green-main.md)).
Each step compares its findings with a committed file —
[staticcheck-baseline.txt](../scripts/ci/staticcheck-baseline.txt),
[errcheck-baseline.txt](../scripts/ci/errcheck-baseline.txt) — keyed by file and
message without line numbers, so an edit elsewhere in a file does not move an
entry. A finding outside the baseline fails the step; an entry that no longer
occurs is reported as a warning, to be deleted, so the files only shrink.
`--update-baseline` on either step rewrites its file; a new entry is a decision
for review, not a way past the gate. The baselines were generated with the
pinned `go tool` versions when the steps widened from `./public/...` to the
whole module, which is where most entries come from.

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
The [staticcheck step](../scripts/ci/lint/staticcheck.sh) therefore carries a
named skip list, prints what it withheld, and stays `warn` while the list is non-empty; the `DID NOT RUN`
branch remains as the backstop for a package the list does not name yet, and
fails the step. The
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
read by every build, test, and lint script. **It is empty**, and the scripts
still read it: `-tags ""` and the concatenated `-tags ",binary_log"` both parse,
so the mechanism survives its last member and a future gate lands back in the
same place.

**Nothing is required, and nothing is optional either.** Until 2026-08 the set
carried `goexperiment.jsonv2`, which gated `encoding/json/v2` across the tree
and failed the build with misleading "undefined" errors when omitted; it
graduated in Go 1.27 and the tag was retired
([ADR-0199](adr/0199-adopt-go-1-27.md)). `boxer_enable_profiling` outlasted it
by days: it selected a compile-out arm rather than gating compilation, and it
was retired once the pprof HTTP listener moved into its own package
([ADR-0212](adr/0212-split-pprof-http-listener.md)) and left the arm with
nothing to select. `gov buildtags` publishes both sets empty, so a consuming
repository needs no tags at all — which is what makes `go tool` delivery of
boxer's CLI work for one.

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

One tag is opt-in and stays out of the file: `boxer_unattended` compiles in
the agent dispatcher's unattended mode
([ADR-0298](adr/0298-an-unattended-mode-the-host-decides-within-preset-bounds.md)),
added by the build that wants it — `-tags="$(cat ./tags),boxer_unattended"`.
A binary built without it cannot be put in the mode.

Centralising tags in a tracked file is uncommon — most Go projects either
avoid tags or scatter them across per-package `Makefile` targets. The
pattern here resembles Cockroach's env-driven `*_GOFLAGS` but more
explicit.

## 4. Tests

The CI test runner is [scripts/ci/gotest.sh](../scripts/ci/gotest.sh):

```sh
go test -race -json -short -cover -tags "$tags" ./... \
  | go tool tparse -progress -trimpath auto -slow 20
```

`--check-drift` makes it fail when the run left the tree dirty
([scripts/ci/check_drift.sh](../scripts/ci/check_drift.sh), which the workflows,
`gate.sh` and `release.sh` share): generator tests rewrite their output in
place, so a dirty tree is committed generated code that drifted from its
generator. It is only meaningful on a clean checkout. `--coverprofile FILE`
swaps `-cover` for `-coverpkg=./... -coverprofile=FILE`, one profile across
packages; the nightly run uploads it with a per-package table from
[scripts/ci/cover_table.sh](../scripts/ci/cover_table.sh).

- Generated code is compared with its generator by tests, so the drift shows up
  in the default lane: every keelsoncodec DTO has a
  `TestGeneratorMatchesCheckedInOutput`, and the runtime facts-schema outputs
  and the phosphor icon tables each have a test that regenerates into memory or
  a temporary directory. The egui2 bindings are compared by the `idl-check`
  lint step. [scripts/dev/generate.sh](../scripts/dev/generate.sh) is the one
  script that regenerates everything (the root `generate.sh` execs it); it
  discovers the keelsoncodec sources from the committed `.out.go` files instead
  of listing them.
- A corpus test that counts the entries its pipeline rejects as skips fails
  above a pinned ceiling, which only goes down: a skip passes, so without the
  ceiling a regression rejecting every entry would pass too.
- [github.com/mfridman/tparse](https://pkg.go.dev/github.com/mfridman/tparse)
  surfaces progress and the 20 slowest tests.
- Coverage HTML is produced locally via
  [scripts/dev/coveragehtml.sh](../scripts/dev/coveragehtml.sh), which reads
  `$GOCOVERDIR` and pipes `go tool covdata textfmt` into `go tool cover -html`.
- Integration tests are explicitly tagged (`//go:build integration`) and run by
  their own runner, [scripts/ci/gotest-integration.sh](../scripts/ci/gotest-integration.sh):

  ```sh
  go test -race -json -count=1 -p 1 -tags "$tags,integration" ./... \
    | go tool tparse -progress -trimpath auto -slow 20
  ```

  The nightly workflow runs it, and the default lane a second time, against a
  throwaway ClickHouse server from
  [scripts/ci/clickhouse_server.sh](../scripts/ci/clickhouse_server.sh): the
  26.8 LTS static build, pinned by version and SHA-256, on `127.0.0.1:8123`
  with `query_log` on and a passwordless default user. The same binary serves
  `clickhouse local`.

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
- Fuzz targets (`func Fuzz…`) run in two lanes. The default runner replays
  each target's seed corpus — its `f.Add` seeds plus any
  `testdata/fuzz/<Target>/` files — as ordinary tests. Generating new inputs
  is the job of [scripts/ci/gofuzz.sh](../scripts/ci/gofuzz.sh), which runs
  every target under `go test -fuzz` one at a time (the toolchain accepts one
  package and one target per invocation) for `BOXER_FUZZ_TIME` each, and
  narrows by package pattern or `BOXER_FUZZ_MATCH`. A failing input is left in
  the package's `testdata/fuzz/` and listed at the end, so it becomes a
  permanent regression seed when committed with the fix. The weekly
  [fuzz workflow](../.github/workflows/fuzz.yaml) runs it at 30 s per target and
  uploads failing inputs as an artifact.

  A fuzz oracle has to hold on every input the fuzzer can reach, which a
  tolerance chosen for friendly table data usually does not: state the input
  domain the property covers and derive the tolerance from it. The comment on
  `FuzzStreamStats` works one through for floating-point error.
- Benchmarks are compiled by the default runner and executed only by
  [scripts/ci/gobench.sh](../scripts/ci/gobench.sh). Its `smoke` mode runs
  every benchmark once (`-benchtime 1x`, `-short`) and lists the ones that
  skipped, since a skip passes without running; the nightly run includes it. Its `compare` mode builds each
  named package's test binary from a base ref (a detached worktree) and from
  the working tree, runs the two alternately for `BOXER_BENCH_COUNT` rounds,
  and summarises with
  [golang.org/x/perf/cmd/benchstat](https://pkg.go.dev/golang.org/x/perf/cmd/benchstat),
  declared as a `go tool`. Alternating spreads drift in machine load over both
  sides; it does not remove load, so a comparison taken beside other builds or
  fuzzing on the same host says little. Both sides build with `GOWORK=off`, so
  a workspace cannot select different dependencies for one of them. A
  benchmark figure that is meant to be cited belongs in a trial under
  [doc/trials/](./trials/README.md), which states its conditions; a `compare`
  table is evidence for one change on one machine.
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

The project-specific checks are subcommands of the project binary and run
together as one composite gate, `boxer gov gate` — build-tag discipline,
doclint, entry points, file naming and codelint, with the step list pinned in
Go so a consuming repository runs the same artifact
([ADR-0179](adr/0179-downstream-consumption-gate-and-skeleton.md)). The two
with no off-the-shelf equivalent are described below; `gov codelint` is
[ADR-0011](adr/0011-codelint.md) and §9.

### `boxer gov doclint`

Sources under [github.com/stergiotis/boxer/public/gov/doclint](../public/gov/doclint).
Implements numbered rules over Markdown front-matter, draft banners, ADR
section completeness and sub-item declarations, link resolution, banned
filenames, Go doc-comment hygiene, stale review stamps, and proposed ADRs
that accepted ones link to. The numbering has gaps: an id is reserved when a check is planned and assigned when it is
implemented, so `DL013` and `DL014` name checks that do not exist yet while
`DL015` through `DL018` ship (the register is DOCUMENTATION_STANDARD §8's table). Findings carry one of three severities:

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

- `govulncheck` runs against the whole module on `v*` tags and manual dispatch
  via the [vuln workflow](../.github/workflows/vuln.yaml), and as a gate of
  every Go dependency update.
- [scripts/ci/cargo_advisories.sh](../scripts/ci/cargo_advisories.sh) checks the
  crates.io packages in the Rust lockfiles against OSV: every lockfile in that
  workflow, and the updated one as a gate of a Rust dependency update. Advisories
  fail it; informational entries such as "unmaintained" are listed and pass.
- Dependency updates are local, gated commits made by
  [scripts/dev/updatedeps.sh](../scripts/dev/updatedeps.sh), one target per
  run (`go` or a `rust/<crate>` lockfile), with a publish-age cooldown.
  Dependabot only raises alerts for Go and Cargo, and opens PRs only for
  GitHub Actions ([ADR-0362](./adr/0362-dependency-updates-land-as-gated-local-commits.md)).
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
  [test workflow](../.github/workflows/test.yaml), the nightly run, `gate.sh`
  and `release.sh` each run `go mod tidy --diff` and `go mod verify`.
- **Release check.** [scripts/ci/release.sh](../scripts/ci/release.sh) checks a
  candidate in a worktree of its own: the module checks, both test lanes with
  the drift check, the integration lane against a ClickHouse server, `lint.sh`
  with the baselines, the viewer and watermark crates, govulncheck, cargo
  advisories, the license gate, the IDL check, the host digest, one fuzz pass,
  `scripts/ci/apidiff.sh` and a clean-tree check. As in `updatedeps.sh`, a step
  that reports a skipped part counts as a failure. It prints the signed-tag
  commands and runs none of them.
- **Release signing.** The tags cut so far (`v0.0.1` to `v0.0.23`) are
  lightweight and unsigned. The documented procedure — an SSH-signed annotated
  tag that whatever builds or deploys from it verifies first (`git verify-tag`)
  — applies from the next tag. The imzero2 on-box deploy enforces it and refuses
  to build an unsigned tag
  ([ADR-0085](./adr/0085-imzero2-demo-pull-build-atomic-deploy.md) SD8), so it
  cannot deploy any existing tag. Recipe:
  [How to sign and verify boxer releases](./howto/release-signing.md).

CycloneDX SBOMs are increasingly standard for SLSA / supply-chain conscious
projects (Kubernetes, OpenTelemetry, GitHub itself). Many repos delegate
license enforcement to external tools (`fossa-cli`, `licensed`); the in-tree
policy gate here is less common.

## 7. Reproducible builds

Two builds of one commit are meant to be byte-identical regardless of
machine, checkout path, user or wall clock, given the pinned toolchains
([ADR-0215](./adr/0215-retire-mimalloc-reproducible-builds.md)). The recipe
lives in two sourced files rather than in each build script, so no two
scripts can disagree:

- [scripts/dev/go-build-env.sh](../scripts/dev/go-build-env.sh) — the tags,
  `-trimpath -buildvcs=auto`, `CGO_ENABLED=0`, and `GOTOOLCHAIN` pinned to
  the go.mod version unless the caller set one. Every Go build site sources
  it, the airgap unbundler included.
- [scripts/dev/rust-repro-env.sh](../scripts/dev/rust-repro-env.sh) — the
  `--remap-path-prefix` pair for the registry and the crate root, beside
  `--locked` on every cargo invocation and patch-level `rust-toolchain` pins.

What is enforced and what is only implied by a shared recipe are different
things, and the difference matters when the property is quoted:

- **Gated in CI:** the committed h3 wasm blob.
  [scripts/ci/h3_wasm_parity.sh](../scripts/ci/h3_wasm_parity.sh) rebuilds
  [rust/h3bridge](../rust/h3bridge) for `wasm32-unknown-unknown` with a
  pinned `CONST_RANDOM_SEED`, passes it through `wasm-strip` and `wasm-opt`,
  and byte-compares against the committed
  [h3.wasm artifact](../public/science/geo/h3/internal/h3o_wasm/h3.wasm).
  It skips on machines without the toolchain, so local lint stays green for
  contributors not touching the bridge; the lint workflow installs what it
  needs and is the enforcer.
- **Gated in CI:** the Go host and the lean Rust render head.
  [scripts/ci/repro_build_parity.sh](../scripts/ci/repro_build_parity.sh)
  builds each twice, from two copies of the tree at paths of different
  lengths into cold caches, and byte-compares the pair. Two paths rather than
  one checkout twice, because every drift ADR-0215 found was a leaked path,
  which a same-path rebuild cannot see. The other render heads share the
  recipe and are not separately compared.

The scope of the claim has two edges:

- **The C-compiler environment is an input.** `blake3` compiles assembly when
  a host C compiler is present and falls back to pure Rust when none is, so
  "same toolchain" includes "same C-compiler presence".
- **An airgapped build reproduces against another airgapped build**, not
  against a connected one. The bundle is a `git archive` export with no VCS
  stamp, `-mod=vendor` omits module hashes from the embedded build info, and
  vendored crate paths remap differently from registry paths. It also ships
  whichever Go SDK the packing host had, so the toolchain is pinned by the
  bundle rather than by go.mod. Details in
  [How to build air-gapped § Notes and limits](./howto/airgapped-build.md#notes-and-limits).

Byte-equality drift checks on embedded native artifacts are uncommon outside
reproducible-build communities (Bazel ecosystems, Bitcoin Core); reproducible
host binaries are a stated goal in Go (`-trimpath`) and Rust
(`--remap-path-prefix`, the nightly `trim-paths` profile) but few application
repos pin the recipe in one sourced file. The local-skip / CI-enforce split
keeps contribution friction low.

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

The subset with a `CS`-numbered rule is checked by `boxer gov codelint`
([ADR-0011](adr/0011-codelint.md)), with per-line disables that carry a
reason; the rest are human-enforced.

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
- `nilaway` is wired up but absent from
  [lint.sh](../scripts/ci/lint.sh)'s step list: the step itself,
  [scripts/ci/lint/nilaway.sh](../scripts/ci/lint/nilaway.sh), runs, and the
  `dev/` script preserves the wider local runner over `./...`.
- No CI on push or pull request. The nightly run tests `main` once a day and
  the local gate is opt-in, so a broken commit can sit on `main` until the next
  nightly run reports it; `last-green` is what a consumer pins to avoid one.
- No `CODEOWNERS`, PR template, or branch-protection automation. The
  documentation standard records an explicit "AI-assisted, direct-to-`main`"
  workflow assumption
  ([§4 of DOCUMENTATION_STANDARD.md](./DOCUMENTATION_STANDARD.md#front-matter-and-document-state-markdown-only)).
- No release automation (`goreleaser` or equivalent) and no container build
  pipeline: `release.sh` checks a candidate, and a person tags it.
- Fuzzing is scheduled weekly at a short budget per target; much of the parser
  and codec surface has no fuzz target.
- Coverage is uploaded as a workflow artifact, not to a coverage service
  (Codecov, Coveralls, etc.).

## 11. Summary

The combination is internally consistent: a Go-tool-driven, single-binary,
single-shell-orchestrator pipeline with custom in-tree governance for
documentation and LLM authorship. The trade-off versus a meta-runner setup
is broader linter coverage in exchange for tighter control over individual
tool versions and bespoke checks that no off-the-shelf runner provides.

What this pipeline buys and what it costs, graded against ISO/IEC 25010 and
dated, is in [doc/explanation/iso25010-assessment.md](./explanation/iso25010-assessment.md);
§10 above is its most-cited source, and the two counteractions it presses
hardest are the release-only CI gate and the bus factor.
