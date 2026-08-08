package skeleton

// DefaultFiles is the published skeleton — the artifact this package exists to
// stop consumers from copying by hand.
//
// Generated members carry no repository-specific content beyond the parameters:
// a consumer's decisions live in the seeded members and in scripts/ci/
// lint-local.sh, which is neither generated nor checked.
func DefaultFiles() (files []File) {
	return []File{
		{
			Path:      "{{.Name}}.sh",
			Ownership: OwnershipGenerated,
			Mode:      0o755,
			Template:  tmplLauncher,
		},
		{
			Path:      "scripts/boxer-path.sh",
			Ownership: OwnershipGenerated,
			Mode:      0o755,
			Template:  tmplBoxerPath,
		},
		{
			Path:      "scripts/new-doc.sh",
			Ownership: OwnershipGenerated,
			Mode:      0o755,
			Template:  tmplNewDoc,
		},
		{
			Path:      "scripts/ci/lint.sh",
			Ownership: OwnershipGenerated,
			Mode:      0o755,
			Template:  tmplLint,
		},
		{
			Path:      "CLAUDE.md",
			Ownership: OwnershipGenerated,
			Mode:      0o644,
			Template:  tmplClaudeMd,
		},
		{
			Path:      "AGENTS.md",
			Ownership: OwnershipSeeded,
			Mode:      0o644,
			Template:  tmplAgentsMd,
		},
		{
			Path:      "tags",
			Ownership: OwnershipSeeded,
			Mode:      0o644,
			Template:  tmplTags,
		},
		{
			Path:         "doc/adr/0001-adopt-boxer-standards.md",
			Ownership:    OwnershipSeeded,
			Mode:         0o644,
			Template:     tmplAdoptionAdr,
			SeedGuardDir: "doc/adr",
		},
	}
}

const tmplLauncher = `#!/bin/bash
# ` + GeneratedMarker + `
#
# {{.Name}}'s single entry point: build {{.AppPackage}} under this
# repository's build tags and run it. Every CLI surface — utilities, linters,
# code generators — is a subcommand there rather than an ad-hoc main().
# Repository-local build flags and environment go in
# scripts/dev/launcher-local.sh, which is sourced when present and is never
# generated or reconciled. It may export variables and may append to
# EXTRA_BUILD_FLAGS -- coverage instrumentation is the usual reason.
set -euo pipefail

here=$(dirname "$(readlink -f "$BASH_SOURCE")")
cd "$here"

EXTRA_BUILD_FLAGS=()
if [ -f scripts/dev/launcher-local.sh ]; then
    # shellcheck source=/dev/null
    . scripts/dev/launcher-local.sh
fi

app=$(mktemp)
trap 'rm -f -- "$app"' EXIT

go build "${EXTRA_BUILD_FLAGS[@]+"${EXTRA_BUILD_FLAGS[@]}"}" \
    -tags "$(tr -d '\n' < tags)" -o "$app" {{.AppPackage}} 1>&2
exec "$app" "$@"
`

const tmplBoxerPath = `#!/bin/bash
# ` + GeneratedMarker + `
#
# Print the on-disk root of the pinned github.com/stergiotis/boxer: a go.work
# sibling checkout locally, the GOMODCACHE path of the go.mod version in CI.
# Either way it matches what the module pin compiles from, so the coding and
# documentation standards read from here are the ones in force.
set -euo pipefail
exec go list -m -f '{{"{{"}}.Dir{{"}}"}}' github.com/stergiotis/boxer
`

const tmplNewDoc = `#!/bin/bash
# ` + GeneratedMarker + `
#
# Seed a new Diátaxis document from boxer's canonical template.
#
# Usage: scripts/new-doc.sh <explanation|howto|tutorial|adr> <destination>
#
# Templates are read from the pinned boxer rather than copied here, so this
# repository always seeds from the standard currently in force. Fill in the
# placeholders, then flip status draft -> stable with reviewed-by and
# reviewed-date after human review.
set -euo pipefail

if [ $# -ne 2 ]; then
    echo "usage: $0 <explanation|howto|tutorial|adr> <destination>" >&2
    exit 2
fi

kind=$1
dest=$2
here=$(dirname "$(readlink -f "$BASH_SOURCE")")
boxer=$("$here/boxer-path.sh")

case "$kind" in
    explanation) src="$boxer/doc/templates/EXPLANATION.md.tmpl" ;;
    howto)       src="$boxer/doc/templates/HOWTO.md.tmpl" ;;
    tutorial)    src="$boxer/doc/templates/TUTORIAL.md.tmpl" ;;
    adr)         src="$boxer/doc/templates/adr/0000-template.md" ;;
    *) echo "unknown type: $kind" >&2; exit 2 ;;
esac

if [ ! -f "$src" ]; then
    echo "template not found: $src" >&2
    exit 1
fi
if [ -e "$dest" ]; then
    echo "refusing to overwrite existing file: $dest" >&2
    exit 1
fi

mkdir -p "$(dirname "$dest")"
cp "$src" "$dest"
echo "seeded $dest from $src"
`

const tmplLint = `#!/bin/bash
# ` + GeneratedMarker + `
#
# The lint gate. The step list is not spelled out here — it comes from the
# pinned boxer via ` + "`gov gate`" + `, so a check added upstream reaches this
# repository at its next bump instead of needing a copy edited.
#
# gofmt and go vet run before the gate, deliberately: they must still work on a
# tree too broken to build the binary the gate lives in.
#
# Repository-local steps go in scripts/ci/lint-local.sh, which is sourced below
# when present and is never generated or reconciled. That file is the one seam
# in this skeleton — put a Rust crate, a capture pipeline or a hardware gate
# there rather than editing this file, which is overwritten on reconciliation.
set -euo pipefail
here=$(dirname "$(readlink -f "$BASH_SOURCE")")
cd "$here/../.."
tags="$(tr -d '\n' < tags)"

rc=0

echo "=== gofmt ==="
fmt_dirty=$(gofmt -l . 2>/dev/null | grep -v '\.out\.go$' | grep -v '\.gen\.go$' || true)
if [ -n "$fmt_dirty" ]; then
    echo "not gofmt-clean:"
    printf '%s\n' "$fmt_dirty"
    echo "read 'gofmt -d' before running 'gofmt -w' — it rewrites doc comments,"
    echo "and has falsified prose in this ecosystem before."
    rc=1
else
    echo "passed"
fi

echo ""
echo "=== go vet ==="
if go vet -tags "$tags" ./... 2>&1 | grep -v '\.out\.go:' | grep -v '\.gen\.go:' | grep -q .; then
    go vet -tags "$tags" ./... 2>&1 | grep -v '\.out\.go:' | grep -v '\.gen\.go:'
    rc=1
else
    echo "passed"
fi

echo ""
# Repository-local gate configuration — package patterns for a tree that is not
# laid out like boxer's, exclusions, baselines — goes in scripts/ci/
# gate-flags.sh, which is sourced when present and is never generated or
# reconciled. It may append to GATE_FLAGS, e.g.
#
#   GATE_FLAGS+=(--code-pattern ./src/go/...)
#   GATE_FLAGS+=(--exclude 'attic/' --exclude 'experiments/')
#   GATE_FLAGS+=(--naming-baseline scripts/ci/naming-baseline.txt)
#
# Keeping it out of this file is what lets this file stay generated.
GATE_FLAGS=()
if [ -f "$here/gate-flags.sh" ]; then
    # shellcheck source=/dev/null
    . "$here/gate-flags.sh"
fi

if ! ./{{.Name}}.sh gov gate --tags "$tags" "${GATE_FLAGS[@]+"${GATE_FLAGS[@]}"}"; then
    rc=1
fi

if [ -f "$here/lint-local.sh" ]; then
    echo ""
    echo "=== local steps ==="
    # shellcheck source=/dev/null
    if ! . "$here/lint-local.sh"; then
        rc=1
    fi
fi

exit $rc
`

const tmplClaudeMd = `<!-- ` + GeneratedMarker + ` -->
@AGENTS.md
`

// tmplAgentsMd is seeded, not generated: its local-supplement section is where
// a repository records what it does differently, which is by definition content
// boxer cannot own.
const tmplAgentsMd = `---
type: reference
audience: contributor
status: draft
# reviewed-by: "@<handle>"     # fill in and uncomment when flipping to stable
# reviewed-date: YYYY-MM-DD    # fill in and uncomment when flipping to stable
---

> **Status: draft — pre-human-review.** Not verified; do not cite as authoritative.

# AGENTS.md

Orientation for AI coding agents and new contributors to ` + "`{{.Name}}`" + `.
This file is a **router**. The coding and documentation standards are boxer's,
adopted by reference through the ` + "`go.mod`" + ` pin — the canonical text is
never copied here. Where this file disagrees with a linked document, the linked
document wins.

## The standards live in boxer

` + "`scripts/boxer-path.sh`" + ` prints the on-disk root of the pinned boxer.
Read these in full before authoring Go or Markdown; do not infer the rules from
the surrounding code.

| You want to… | Read |
| --- | --- |
| Write Go in the house style | ` + "`$(scripts/boxer-path.sh)/CODINGSTANDARDS.md`" + ` |
| Write or edit a doc / ADR | ` + "`$(scripts/boxer-path.sh)/doc/DOCUMENTATION_STANDARD.md`" + ` |
| Understand the toolchain | ` + "`$(scripts/boxer-path.sh)/doc/ENGINEERING_PRACTICES.md`" + ` |
| Seed a new doc | ` + "`scripts/new-doc.sh <explanation\\|howto\\|tutorial\\|adr> <dest>`" + ` |

Bumping ` + "`github.com/stergiotis/boxer`" + ` bumps the standards with it. Run
` + "`scripts/ci/lint.sh`" + ` after any bump: new rules and retired build tags
surface there, not in review.

## Build & test

Always pass the build tags, or packages fail with misleading "undefined":

` + "```sh" + `
go test -tags="$(cat ./tags)" ./...
./{{.Name}}.sh gov gate --tags "$(cat ./tags)"    # the full gate
` + "```" + `

## Entry point

One binary, ` + "`./{{.Name}}.sh`" + `, wired in ` + "`{{.AppPackage}}`" + `.
New utilities, linters and code generators are registered there as
` + "`urfave/cli/v2`" + ` subcommands — never as ad-hoc ` + "`main()`" + `
functions.

## Generated files

` + "`scripts/ci/lint.sh`" + `, ` + "`scripts/boxer-path.sh`" + `,
` + "`scripts/new-doc.sh`" + `, ` + "`./{{.Name}}.sh`" + ` and
` + "`CLAUDE.md`" + ` are emitted by ` + "`gov skeleton`" + ` and carry a
DO NOT EDIT header; editing one is reverted at the next reconciliation. Local
lint steps belong in ` + "`scripts/ci/lint-local.sh`" + `, which is never
generated.

This file, ` + "`tags`" + ` and ` + "`{{.AppPackage}}`" + ` are yours: seeded
once when absent, never overwritten.

## Repository-local supplement

Deviations from and extensions to boxer's standards. Keep this short — anything
of general applicability belongs upstream in boxer instead.

- *(none yet)*
`

// tmplTags is seeded with exactly what boxer requires of a consumer, read from
// the build-tag contract rather than restated here — a second copy of that list
// is the failure this whole package exists to prevent. Repository-local tags are
// appended by the consumer and never touched by a reconciliation.
const tmplTags = `{{.RequiredTags}}
`

// tmplAdoptionAdr seeds the decision every consumer has to record: that boxer's
// standards are authoritative here, and by reference rather than by copy.
//
// Seeded as proposed, not accepted — adopting is the consumer's decision to
// make and review, not one boxer can make on their behalf. Flip it with
// boxer's scripts/dev/adr-accept.sh once it has been read.
//
// It exists because the alternative is what happened: consumers wrote their own
// adoption ADRs from a sibling repository's paraphrase, and one of them ended
// up mandating a build-tag scheme boxer had already retired.
const tmplAdoptionAdr = `---
type: adr
status: proposed
date: YYYY-MM-DD
# reviewed-by: "@<handle>"     # fill in and uncomment when flipping to accepted
# reviewed-date: YYYY-MM-DD    # fill in and uncomment when flipping to accepted
---

> **Status: proposed — pre-human-review.** Decision under consideration; do not implement as if accepted.

# ADR-0001: Adopt boxer's standards by reference

## Context

` + "`{{.Name}}`" + ` depends on
[` + "`github.com/stergiotis/boxer`" + `](https://github.com/stergiotis/boxer)
as a Go module. boxer carries a coding standard, a documentation standard
(Diátaxis plus ADRs plus YAML front-matter), a template library, and the
governance tooling that enforces them. This repository needs those to be
*the* standards here, and needs a reader to know where the authoritative text
lives.

The choice this records is not whether to adopt them but how: by reference
through the existing ` + "`go.mod`" + ` pin, or by copying the text in.

## Decision

We adopt boxer's ` + "`CODINGSTANDARDS.md`" + `,
` + "`doc/DOCUMENTATION_STANDARD.md`" + ` and ` + "`doc/ENGINEERING_PRACTICES.md`" + `
as authoritative, referenced through the ` + "`go.mod`" + ` pin. **No standard
text is copied into this repository.** It is read at the resolved module
directory:

` + "```sh" + `
scripts/boxer-path.sh     # go list -m -f '{{"{{"}}.Dir{{"}}"}}' github.com/stergiotis/boxer
` + "```" + `

Enforcement comes through the same pin rather than through copied scripts:
` + "`scripts/ci/lint.sh`" + ` runs ` + "`gov gate`" + `, whose step list, rule
sets and build-tag contract live upstream. The repository mechanics are emitted
and reconciled by ` + "`gov skeleton`" + ` (boxer ADR-0179).

The standard applies to **new code and meaningful rewrites**. Code predating
this decision migrates in its own pass; drive-by renaming is not part of it.

## Alternatives

- **Copy the text in.** Rejected: drift is the failure this decision exists to
  prevent. Bumping the pin would not bump a copy, and a verbatim copy would
  import boxer-specific references that do not resolve here.
- **Symlink to a sibling checkout.** Rejected as a committed artifact: a CI
  checkout has no sibling boxer and the links dangle.
- **Extract a third standards repository.** Rejected for now — it adds a
  repository without a consumer that needs it.

## Consequences

### Positive

- Bumping ` + "`github.com/stergiotis/boxer`" + ` picks up the current standards
  and rules with no second edit here.
- No duplicated standard text to let rot.

### Negative

- Reading the standard is not a single ` + "`cat`" + `; it needs the
  ` + "`scripts/boxer-path.sh`" + ` resolution above.
- A bump can introduce findings nobody here authored. Run
  ` + "`scripts/ci/lint.sh`" + ` immediately after one.

## Status

Proposed — awaiting review.

Status lifecycle: ` + "`Proposed → Accepted → (Deferred | Deprecated | Superseded by ADR-XXXX)`" + `.

## References

- [boxer ` + "`CODINGSTANDARDS.md`" + `](https://github.com/stergiotis/boxer/blob/main/CODINGSTANDARDS.md)
- [boxer ` + "`doc/DOCUMENTATION_STANDARD.md`" + `](https://github.com/stergiotis/boxer/blob/main/doc/DOCUMENTATION_STANDARD.md)
- [Diátaxis](https://diataxis.fr/)
`
