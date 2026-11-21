---
type: adr
status: proposed
date: 2026-11-20
# reviewed-by: "@<handle>"     # fill in and uncomment when flipping to accepted
# reviewed-date: YYYY-MM-DD    # fill in and uncomment when flipping to accepted
---

> **Status: proposed — pre-human-review.** Decision under consideration; do not implement as if accepted.

# ADR-0394: Library packages do not import the app platform; where generated code finds its runtime

## Context

A package's import closure is what every binary that uses it links. Two
edges made that closure much larger than the package:

- `eh` reads `GOPATH`, `GOROOT` and `HOME` through the environment registry
  (ADR-0009), and the registry carried its CLI-flag derivation and its test
  helper. Every package that returns an error therefore linked `testing`,
  `flag` and the CLI library.
- Libraries reached up into the app platform (`public/keelson`,
  `public/thestack`) for small pieces: the runtime that generated code calls,
  the ClickHouse client, a window-open request. A library then could not be
  used, or promoted to a stability tier, without the platform.

## Decision

1. **The foundation links no harness.** `public/config/env` keeps
   declaration, resolution and the registry; the flag derivation lives in
   `public/config/env/envcli` and the test helper in
   `public/config/env/envtest`. The same holds for other foundation
   packages: a test helper goes in a `…test` package (`nanopasstest`), a demo
   command goes in `public/app/commands`. A test pins it: `go list -deps` of
   `config/env`, `eh`, `containers` and `functional/option` contains none of
   `testing`, `flag`, `os/exec` or the CLI library.
2. **A duplicate declaration is not fatal.** Declaring an identical `Spec`
   again returns the first handle. A conflicting declaration keeps the
   first, is collected for `env.RegistrationErrors()`, and is logged once by
   `logging.Apply`; it no longer panics in `init`, where no caller can handle
   it.
3. **A library package does not import the app platform.** A package outside
   `public/keelson`, `public/thestack`, `public/app`, `public/gov`, `apps` and
   `showcase` does not import `public/keelson` or `public/thestack` (non-test
   imports). `props verify` fails a new edge; the remaining adapters are
   listed there with their deferred fix. Pieces the libraries reached up for
   move down, keeping their package names so emitted code changes only its
   import path: the facts schema runtime (`factsschema` and its `ra`,
   `cborarrow`, `arrowrowcbor`, `dml`, `dml_cbor`, `ddl`, `codegen`) to
   `semistructured/leeway/factsschema`; `buscodec`, the CBOR envelope
   convention every payload and config uses, to `semistructured/cbor`; the
   ClickHouse client to `db/clickhouse/chclient`; `runinfo` to
   `observability`; `utfsafe` to `semistructured`. A piece that reads the
   platform's own vocabulary (`factsread`, `storegen`) stays in it.
4. **A client half does not link its server.** The open-window client
   (subjects, `RequestOpen`, `RequestOpenKind`, `RequestRaise`) is the leaf
   `keelson/runtime/windowopen`; the window host imports it for the
   subjects.
5. **Examples and fixtures are not importable API.** Worked examples and
   generator fixtures move under an `internal` directory just high enough for
   their users, keeping their package names.
6. **Generated code names its generator and the runtime ABI it targets, and
   nothing that depends on the build.** A leeway generator's header names the
   generator and, for DML and read-access, the ABI generation
   (`Leeway DML (runtime ABI 1)`); the comment before each emitted section
   names the generating function and file without a line number. The DML and
   read-access generators emit `const _ = runtime.GeneratedABI1` after the
   imports; a change to what generated code may call renames the constant
   with the generator change, so a stale file fails to compile on a line whose
   comment says to regenerate.

## Alternatives

- **`eh` reads its three variables with `os.Getenv`.** Rejected: it would
  need an exemption from codelint CS011 and would drop the variables from the
  registry and its generated reference, while splitting the registry removes
  the edge without either cost.
- **Build tags around the CLI and test helpers.** Rejected: a tag that a
  consumer must set to get a smaller binary is a tag nobody sets, and it
  splits the package's API by build configuration.
- **Forwarding packages at the old import paths.** Rejected: the house rule
  forbids compatibility shims, and a forwarding package keeps the platform
  edge this decision removes.
- **Renaming the moved packages (`buscodec`, `ra`) after their new homes.**
  Rejected: generated code in consuming modules spells the package name in
  every call; keeping it limits a consumer's change to the import path.
- **An `EnforceVersion`-style arithmetic constant instead of a renamed
  identifier.** Rejected: a renamed identifier fails with "undefined" on the
  guard line, whose comment carries the instruction, at no runtime cost.
- **Stamping the module version (`vcs.BoxerModuleVersion`) into headers.**
  Rejected: one generator at one commit reports a release tag when boxer is a
  dependency and `(devel)` when it is the main module or reached through a
  workspace, so the same output differed by build and a consumer's drift
  check flipped when its pin or workspace changed. No version string is
  agreed by every build at a commit; the ABI guard catches staleness.
- **Panic on any duplicate declaration (the former rule).** Rejected: two
  copies of one declaration linked into one binary are harmless, and a panic
  in `init` gives the host no chance to report which package declared what.

## Consequences

### Positive

- A binary that uses only foundation packages no longer links the test
  harness or the CLI library.
- A library and the code generated against it can be used, and tiered,
  without the app platform.

### Negative

- Every flag and test call site changes its spelling (`X.AsCliFlag()` →
  `envcli.Flag(X)`, `X.SetForTest(t, v)` → `envtest.Set(t, X, v)`), here and
  in every module that uses the registry.
- Every moved package changes its import path in every consuming module;
  generated code there is regenerated (the ABI guard makes a missed
  regeneration fail to compile rather than misbehave).
- Each typed variable gains three exported seams (`RecordFlag`,
  `ClearCache`, `DefaultValue`) that only `envcli` and `envtest` are meant
  to call.

## Choices for review

- Kept `eh` → `config/env` (the registry stays complete) rather than
  reading the variables directly.
- Named the helpers `envcli.Flag`, `envcli.WithFlagName`,
  `envcli.NewCommand` and `envtest.Set`; the `With*Action` options keep
  their names under `envcli`.
- An identical redeclaration ignores `Origin`; a conflicting one is logged at
  error level and the process continues with the first declaration.
- `containers` returns a new `ErrEmptyStack` sentinel instead of three
  distinct messages.
- New homes: `semistructured/leeway/factsschema`, `semistructured/cbor/buscodec`,
  `db/clickhouse/chclient`, `observability/runinfo`, `semistructured/utfsafe`,
  `keelson/runtime/windowopen`; `public/slices` became `public/sliceconv` and
  `dsl/env` became `dsl/grammarenv`.
- Left public, with their tiers as they are: `canonicaltypes/sample` (a type
  sampler used by production code, not an example) and `leeway/anchor` with
  its demos (imported by `apps/play` tests and the widget demo, so it would
  need a module-root `internal/`; deferred).
- Deferred adapters (listed in `props verify`): `code/analysis/sccapplet`,
  `science/geo/vectorfield/keelsonfield`, `semistructured/markdown/mdnotebook`,
  `semistructured/markdown/obsidian`, `observability/profiling/pprofhttp`.
- The ABI guard covers the DML and read-access generators; canonwire, the
  record-store generator, keelsoncodec and egui2gen do not emit one yet, and
  egui2gen keeps its header (it reaches the Rust host).
- Path references in older ADRs were updated where a package moved; their
  decisions are unchanged.
- Codelint `CS019` (error) rejects a package-level error built with `eh` or
  `eb`; the 84 existing ones became `errors.New`.

## Status

Proposed — awaiting review.

Status lifecycle: `Proposed → Accepted → (Deferred | Deprecated | Superseded by ADR-XXXX)`.
See [DOCUMENTATION_STANDARD §1 ADR](../DOCUMENTATION_STANDARD.md#architecture-decision-records-why-it-is-this-way) for the edit-policy tiers.

## References

- [ADR-0009](./0009-environment-variable-registry.md) — the environment registry this splits.
- [ADR-0389](./0389-stability-tiers-per-package-and-what-a-version-number-promises.md) — the stability tiers an import edge can block.
- [ADR-0036](./0036-runtime-buscodec.md) — the bus codec that moves.
- [ADR-0135](./0135-app-launch-requests.md) — the open service whose client becomes `windowopen`.
