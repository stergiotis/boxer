---
type: how-to
audience: engineer with a specific task
status: stable
reviewed-by: "@spx"
reviewed-date: 2026-07-05
---

# How to survey call-site dispatch and gate on generics misuse

You want to know where a package tree dispatches dynamically, where generic
instantiations degrade into dictionary-mediated calls, and whether the
compiler devirtualized the calls you care about. `gov callsites` classifies
every call expression statically and joins the compiler's own
devirtualization/inlining decisions onto each site. Design and vocabulary:
[ADR-0107](../adr/0107-callsites-compiler-adjudicated-dispatch-classification.md);
cost-model background:
[EXPLANATION.md](../../public/gov/callsites/EXPLANATION.md).

## When to use this recipe

- Auditing a hot path (leeway marshalling, caching) before optimizing —
  find the interface dispatches and dictionary-degraded generics first.
- Checking generics hygiene across a subtree, or gating it in CI
  (`--fail-on interface-type-arg`).
- Verifying an optimization assumption: "the compiler devirtualizes this
  call" is a claim the adjudication columns answer per site.

## Prerequisites

- A checkout of this repository; the pinned Go toolchain from `go.mod`
  resolves automatically.
- The repository's build tags — most packages here do not compile without
  them (see [`./tags`](../../tags)).

## Steps

### 1. Run a survey

From the repository root, pass the tags and the package patterns; the
`go-stringer` format prints one grep-friendly line per call site:

```sh
./boxer.sh gov callsites --tags "$(cat tags | tr -d '\n')" \
    --format go-stringer ./public/gov/callsites/
```

Each line reads position, classification, callee origin, callee, and — when
adjudication is on (the default) — the compiler's verdicts:

```text
…/gov_callsites_analyzer.go:58:20 DynPoly Local OnLoadStats devirt=false inlined=false
…/gov_callsites_cli.go:119:10 Mono 3rdParty github.com/rs/zerolog/log.Info devirt=false inlined=true
…/gov_callsites_cli.go:44:23 StaticPoly StdLib slices.Concat args=[Stenciled([]github.com/urfave/cli/v2.Flag) Interface(github.com/urfave/cli/v2.Flag)]
```

The position's column is the call's opening parenthesis — the same position
the compiler's `-m` diagnostics use, which is what makes the join exact.
Classifications: `Mono` (direct static call), `StaticPoly` (generic
instantiation — `args=` lists the callee's type arguments, `recvArgs=` the
receiver's), `DynPoly` (interface dispatch or func-value call), plus
`Conversion` and `Builtin`, which are not dispatches at all and are kept out
of the other buckets.

### 2. Read the summary line

Every run ends with an aggregate:

```text
INF callsites survey complete builtins=26 checked=187 conversions=1
    devirtualized=1 dynPoly=11 ignoredFiles=0 inlinedCalls=58 interfaceArgs=1
    mono=148 packages=1 … total=187 unknown=0
```

`interfaceArgs` / `pointerArgs` / `typeParamArgs` count the
dictionary-degraded type arguments (the expensive ones); `stenciledArgs` the
harmless ones. `checked` / `devirtualized` / `inlinedCalls` summarize the
compiler join. `unknown` should be zero — non-zero means sites whose callee
could not be resolved, which the tool refuses to guess about.

### 3. Mind the coverage warning

A survey is always per build configuration. Files excluded by build
constraints load without error, so the tool reports them instead:

```text
WRN callsites: build constraints excluded Go files from the survey — verify --tags ignoredFiles=7
```

Seeing this usually means missing `--tags`. On packages that keep several
tag-gated variants deliberately, a residual count is expected — the warning
then states which configuration you surveyed, not a mistake.

### 4. Find the expensive generic call sites

The shape classes in `args=` / `recvArgs=` are the governance axis
(measured, not folklore — see the EXPLANATION):

```sh
./boxer.sh gov callsites --tags "$(cat tags | tr -d '\n')" \
    --format go-stringer ./public/semistructured/... 2>/dev/null \
  | grep -E 'Interface\(|Pointer\('
```

`Interface(T)` is the documented worst case (dictionary plus interface
indirection); `Pointer(T)` collapses into the shared pointer shape and loses
devirtualization; `TypeParam(T)` defers the verdict to the outer
instantiation; `Stenciled(T)` costs ~nothing extra.

### 5. Check what the compiler actually did

Adjudication is on by default and costs one `go build -gcflags=-m` over the
same patterns (warm-cache safe). `devirt=true` on a `DynPoly` site means the
compiler proved the concrete type and rewrote the call — classified-dynamic
but optimized-static is a finding, not a contradiction. Pass
`--adjudicate=false` to skip the build when you only need the static
classification (the `devirt=`/`inlined=` fields then disappear rather than
reporting false).

### 6. Gate in CI

```sh
./boxer.sh gov callsites --tags "$(cat tags | tr -d '\n')" \
    --fail-on interface-type-arg ./public/foo/... >/dev/null
```

The run exits non-zero when any scanned call site passes an interface as a
type argument, with the count in the error:

```text
Error: callsites: fail-on rule matched
    data: {"interface-type-arg": 1}
```

The rule is site-scoped: it flags the call expression in *scanned* code
regardless of where the callee lives, so scope the patterns to the tree you
own.

### 7. Use the API instead of the CLI

```go
svc := &callsites.AnalyzerService{
    Dir:        moduleDir,             // where patterns resolve; CWD when empty
    Patterns:   []string{"./..."},
    BuildTags:  tags,
    Adjudicate: true,
    OnLoadStats: func(s callsites.LoadStats) { /* coverage: s.IgnoredFiles */ },
}
for site, err := range svc.All(ctx) {
    if err != nil {
        return err // load/adjudication failure — the stream never degrades silently
    }
    // breaking out early is safe
}
```

## Verification

The package's own test suite is the behavioral contract: a marker-annotated
fixture module under `public/gov/callsites/testdata/fixmod` pins every
classification and the adjudication join against the pinned toolchain. A
quick self-check that the tool is alive in your checkout:

```sh
go test -tags="$(cat ./tags)" ./public/gov/callsites/
```

## Gotchas and limits

- **The adjudication layer parses human-readable `-m` output.** The two line
  forms it consumes have been stable for years, and the toolchain is pinned
  in `go.mod`; if a toolchain bump rewords them, `TestAdjudication` fails
  loudly and joins degrade to `devirt=false` — never to wrong static
  classifications.
- **Test files are never compiler-checked** — `go build` does not compile
  `_test.go`, so with `--include-tests` those sites report
  `Compiler.Checked == false`.
- **Shape classes judge the argument's top level only**: `[]*T` is a
  stenciled slice whose *element* collapses; per-element cost is out of
  scope for the per-argument verdict.
- **Empty `--tags` leaves `GOFLAGS` untouched** (no `-tags=` is injected),
  so an environment that exports tags still applies. Pass `--tags`
  explicitly when you need a specific configuration.
- **Dynamic ≠ slow**: a `DynPoly` site the compiler devirtualized
  (`devirt=true`) costs nothing at runtime. Read the two layers together
  before acting on either.
