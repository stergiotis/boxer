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

## Alternatives

- **`eh` reads its three variables with `os.Getenv`.** Rejected: it would
  need an exemption from codelint CS011 and would drop the variables from the
  registry and its generated reference, while splitting the registry removes
  the edge without either cost.
- **Build tags around the CLI and test helpers.** Rejected: a tag that a
  consumer must set to get a smaller binary is a tag nobody sets, and it
  splits the package's API by build configuration.
- **Panic on any duplicate declaration (the former rule).** Rejected: two
  copies of one declaration linked into one binary are harmless, and a panic
  in `init` gives the host no chance to report which package declared what.

## Consequences

### Positive

- A binary that uses only foundation packages no longer links the test
  harness or the CLI library.

### Negative

- Every flag and test call site changes its spelling (`X.AsCliFlag()` →
  `envcli.Flag(X)`, `X.SetForTest(t, v)` → `envtest.Set(t, X, v)`), here and
  in every module that uses the registry.
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

## Status

Proposed — awaiting review.

Status lifecycle: `Proposed → Accepted → (Deferred | Deprecated | Superseded by ADR-XXXX)`.
See [DOCUMENTATION_STANDARD §1 ADR](../DOCUMENTATION_STANDARD.md#architecture-decision-records-why-it-is-this-way) for the edit-policy tiers.

## References

- [ADR-0009](./0009-environment-variable-registry.md) — the environment registry this splits.
- [ADR-0389](./0389-stability-tiers-per-package-and-what-a-version-number-promises.md) — the stability tiers an import edge can block.
