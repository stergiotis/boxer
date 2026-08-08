---
type: how-to
audience: contributor
status: draft
# reviewed-by: "@<handle>"     # fill in and uncomment when flipping to stable
# reviewed-date: YYYY-MM-DD    # fill in and uncomment when flipping to stable
---

> **Status: draft — pre-human-review.** Not verified; do not cite as authoritative.

# How to adopt boxer's standards in another repository

For a repository that depends on boxer as a Go module and wants its coding
standard, documentation standard and lint gate — without copying any of them.
The decision this implements is [ADR-0179](../adr/0179-downstream-consumption-gate-and-skeleton.md).

Everything policy-bearing arrives through the `go.mod` pin. What you write by
hand is roughly 130 lines, none of it rules.

## Before you start

The repository needs a `go.mod` requiring `github.com/stergiotis/boxer`. Locally
a `go.work` `use` entry resolves it to a sibling checkout; in CI it resolves to
the pinned version in `GOMODCACHE`. Neither the skeleton nor the gate needs a
sibling checkout to exist.

## 1. Emit the skeleton

```sh
boxer gov skeleton --root /path/to/repo --write
```

Seven files. Five are generated, carry a `DO NOT EDIT` header, and are
reconciled on every lint run; two are yours, seeded once and never overwritten:

```sh
boxer gov skeleton --root /path/to/repo --list
```

| | |
| --- | --- |
| generated | `<repo>.sh`, `scripts/boxer-path.sh`, `scripts/new-doc.sh`, `scripts/ci/lint.sh`, `CLAUDE.md` |
| seeded | `AGENTS.md`, `tags`, `doc/adr/0001-adopt-boxer-standards.md` |

Parameters come from `go.mod` — module path, and the repository name that names
the launcher. Override with `--name` or `--app-package` if your layout differs.

## 2. Write the entry point

The one piece the skeleton cannot generate, because it is where your own
commands go. `public/app/main.go`, composing boxer's governance surface into
your binary:

```go
package main

import (
	"os"

	"github.com/rs/zerolog/log"
	"github.com/stergiotis/boxer/public/gov"
	cli2 "github.com/stergiotis/boxer/public/hmi/cli"
	"github.com/stergiotis/boxer/public/observability/logging"
	"github.com/stergiotis/boxer/public/observability/vcs"
	"github.com/urfave/cli/v2"
)

func main() {
	app := cli.App{
		Name:     vcs.ModuleInfo(),
		Version:  vcs.BuildVersionInfo(),
		Flags:    cli2.FlagsNilRemoved(logging.LoggingFlags),
		Commands: cli2.CommandsNilRemoved(gov.NewCliCommand()),
		Before:   logging.Apply,
	}
	if err := app.Run(os.Args); err != nil {
		log.Error().Stack().Err(err).Msg("an error occurred")
		os.Exit(1)
	}
}
```

That gives you `<repo> gov gate`, `<repo> gov doclint`, `<repo> gov skeleton`
and the rest, under your own build tags and your own logging wiring — and it
satisfies boxer's own [Entry Points](../../CODINGSTANDARDS.md#entry-points) rule
rather than working around it. Add `adr.NewCliCommand()` and
`env.NewCliCommand(envdoc.NewGenDocsCommand())` if you want the ADR corpus and
the environment registry too.

The dependency cone is real: a binary linking `gov` + `adr` + `dev` + `env` and
nothing else comes to roughly 28 MB. That is fine for a development CLI and
worth thinking about before it is also your application binary.

## 3. Run the gate

```sh
scripts/ci/lint.sh
```

`gofmt` and `go vet` run first, from the wrapper — they have to work on a tree
too broken to build the binary the gate lives in. Then five steps from the
pinned boxer:

```
buildtags     pass     0.00s
doclint       pass     0.01s
entry-points  pass     0.11s
file-naming   pass     0.00s
codelint      pass     0.11s
```

## 4. Accept the adoption ADR

`doc/adr/0001-adopt-boxer-standards.md` is seeded as `proposed`. Read it, fill
in the date, and flip it:

```sh
$(scripts/boxer-path.sh)/scripts/dev/adr-accept.sh 1
```

Record anything you do differently in the **Repository-local supplement**
section of `AGENTS.md`, not by editing a generated file.

## Local steps

A repository with a Rust crate, a capture pipeline or a hardware gate puts those
in `scripts/ci/lint-local.sh`, which the emitted wrapper sources when present
and which is never generated or reconciled. That file is the only seam — edit a
generated file instead and the next reconciliation reverts it.

Repository-specific exclusions are a flag rather than a filter you maintain in
two places:

```sh
<repo> gov gate --exclude 'attic/' --exclude '*.gen.md'
```

A bare name or glob matches a basename anywhere; a trailing slash matches that
directory at any depth; a pattern containing a separator matches the whole
relative path.

## Keeping up

On every boxer bump:

```sh
go get -u github.com/stergiotis/boxer
scripts/ci/lint.sh
```

Step two is the point of all of this. It fails on a retired build tag, on a
generated file that has drifted, and on any rule the bump introduced — at your
desk, rather than in review or not at all.

## Troubleshooting

**Packages fail with "undefined" symbols.** The build tags are missing. Every
`go build`/`test`/`vet` needs `-tags "$(cat ./tags)"`; editors and `gopls` need
`GOFLAGS` set, which `gov buildtags --print-env` will render for you.

**`skeleton: drift` on a file you edited deliberately.** That is the tool
working. Move the change into `scripts/ci/lint-local.sh`, or upstream it into
boxer if it is generally useful, then `gov skeleton --write`.

**doclint flags a file you do not maintain.** Exclude it (above). If it is
untracked, note that doclint already skips git-ignored paths — adding it to
`.gitignore` may be the more honest fix.

## Related

- [ADR-0179](../adr/0179-downstream-consumption-gate-and-skeleton.md) — the decision, and the options rejected.
- [CODINGSTANDARDS.md](../../CODINGSTANDARDS.md) and [doc/DOCUMENTATION_STANDARD.md](../DOCUMENTATION_STANDARD.md) — what is being adopted.
- [doc/ENGINEERING_PRACTICES.md](../ENGINEERING_PRACTICES.md) — the toolchain behind the gate.
