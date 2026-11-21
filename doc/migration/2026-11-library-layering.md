---
type: how-to
audience: maintainer of a module that imports boxer packages
status: draft
# reviewed-by: "@<handle>"     # fill in and uncomment when flipping to stable
# reviewed-date: YYYY-MM-DD    # fill in and uncomment when flipping to stable
---

> **Status: draft — pre-human-review.** The rewrites below were applied to
> this repository; they have not been run against another module.

# How to follow the library layering moves

[ADR-0394](../adr/0394-library-packages-do-not-import-the-app-platform.md)
moves packages so that a library does not import the app platform, splits the
environment registry's CLI and test helpers out of its core, and changes a few
exported names. No stored table and no wire format changes. A consuming module
changes import paths and call spellings, then regenerates the code it generates
with boxer's generators.

## When to use this recipe

You import a boxer package and upgrade past this change. The symptom of
skipping it is a build failure: an import path that no longer resolves, or a
`use of internal package` error for an example that moved under `internal`.

## Import paths

Rewrite each prefix; package names are unchanged unless stated.

| Old import path | New import path |
| --- | --- |
| `public/keelson/runtime/factsschema` and its `ra`, `cborarrow`, `arrowrowcbor`, `dml`, `dml_cbor`, `ddl`, `codegen`, `bench`, `meshdemo` | `public/semistructured/leeway/factsschema/…` |
| `public/keelson/runtime/buscodec` | `public/semistructured/cbor/buscodec` |
| `public/keelson/data/chclient` | `public/db/clickhouse/chclient` |
| `public/keelson/runtime/runinfo` | `public/observability/runinfo` |
| `public/thestack/utfsafe` | `public/semistructured/utfsafe` |
| `public/slices` (package `slices`) | `public/sliceconv` (package `sliceconv`) |
| `public/db/clickhouse/dsl/env` (package `env`) | `public/db/clickhouse/dsl/grammarenv` (package `grammarenv`) |

`public/keelson/runtime/factsschema/factsread` and `…/storegen` stay where they
are. The facts table's layout names (`factsschema.DefaultEngineClause`,
`TsColumn`, `MigrationRecipe`) move with the package. Every path is under
`github.com/stergiotis/boxer/`.

`boxer code importmove` applies the table: one `--map old=new` per row (full
paths), and a `--keep` for each of the two paths that stay, which sit under a
moved prefix:

```sh
boxer code importmove --root . \
  --map github.com/stergiotis/boxer/public/keelson/runtime/factsschema=github.com/stergiotis/boxer/public/semistructured/leeway/factsschema \
  --keep github.com/stergiotis/boxer/public/keelson/runtime/factsschema/factsread \
  --keep github.com/stergiotis/boxer/public/keelson/runtime/factsschema/storegen \
  --dryRun
```

It matches at path-segment boundaries, gives an import whose last segment
changes its old name as an alias so the file still compiles, and reports
string literals, `//go:generate` lines and template files naming a moved path
without changing them. A `gofmt -r` rule per package rename then does the
qualifier, if the alias is to go: for example
`gofmt -r 'env.Extract -> grammarenv.Extract'` once the alias is removed.

## Calls

| Before | After |
| --- | --- |
| `X.AsCliFlag(opts…)` | `envcli.Flag(X, opts…)` (`public/config/env/envcli`) |
| `env.WithCliFlagName(n)` | `envcli.WithFlagName(n)` |
| `env.WithBoolAction`, `WithStringAction`, `WithPathAction`, `WithInt64Action`, `WithFloat64Action`, `WithDurationAction`, `env.FlagOption` | the same names in `envcli` |
| `env.NewCliCommand(subs…)` | `envcli.NewCommand(subs…)` |
| `X.SetForTest(t, v)` | `envtest.Set(t, X, v)` (`public/config/env/envtest`) |
| `nanopass.AssertProperties(t, p, corpus)` | `nanopasstest.AssertProperties(t, p, corpus)` |
| `eb.NewCliCommand()` | `errordemo.NewCliCommand()` (`public/app/commands/errordemo`) |
| `windowhost.RequestOpen`, `RequestOpenKind`, `RequestRaise`, `OpenSubject`, `RaiseSubject`, `RefusedError` | the same names in `public/keelson/runtime/windowopen` |
| `golay24.Encoding[w]` | `golay24.EncodeWord(w)` |
| `golay24.EncodingUint8Triples[3*w:3*w+3]` | `golay24.PutEncoded(dst, w)` |
| `esd.SetCBORStructuredData(b)` | removed; build the error with `eh.ErrorfWithData` or `eb.Build` |

Behaviour that changed without a signature change:

- Declaring an environment variable whose `Spec` is identical to one already
  declared returns the first handle; a conflicting declaration no longer
  panics, and `env.RegistrationErrors()` reports it.
- `containers.Stack`'s `Swap`, `Pop` and `Peek` return `containers.ErrEmptyStack`.
- Package-level `Err…` sentinels are `errors.New`: `errors.Is` matches as
  before, but the sentinel no longer carries a stack.

## Examples that left the importable surface

These moved under an `internal` directory and cannot be imported from another
module: `storage/recordstore/{example,cqrsexample,sharedsection,bypassguard}`,
`semistructured/leeway/readaccess/example`, `…/leeway/canonwire/example`,
`…/leeway/dml/example`, `…/leeway/test` (with `singledecl`) and
`…/leeway/canonicaltypes/test`. Copy what you used.

## Generated code

Regenerate code produced by boxer's leeway generators, the record-store
generator and `runtimecodegen` with the new boxer version. The import paths in
generated files change with the table above. DML and read-access output gains
`const _ = runtime.GeneratedABI1`; a file that fails on that line was generated
against another runtime and is regenerated, not edited.

The header line of leeway output names the generator and, for DML and
read-access, the runtime ABI generation (`Leeway DML (runtime ABI 1)`) instead
of a module version, and the generator-location comments before each section
no longer carry a line number. Output of one generator at one commit is then
the same bytes whether boxer is a pinned dependency, the main module or reached
through a workspace file. Regenerate once more after upgrading past this
change; a drift check then stays quiet when only the pin moves.

## Contract changes this note acknowledges

- `contract: cli/boxer datasource` — the empty placeholder command group is
  removed; it had no subcommands.
- `contract: membership/public/keelson/runtime/factsschema/meshdemo/testdata/membership-assignments.golden`
  — the demonstration vocabulary moved with the facts schema; its assignments
  are unchanged at `public/semistructured/leeway/factsschema/meshdemo`.

## Checking the result

`go build ./...` and `go vet ./...` in the consuming module; then the
module's own generator drift tests, which fail until the generated files are
regenerated.
