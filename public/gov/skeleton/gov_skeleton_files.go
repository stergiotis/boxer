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
	}
}

const tmplLauncher = `#!/bin/bash
# ` + GeneratedMarker + `
#
# {{.Name}}'s single entry point: build {{.AppPackage}} under this
# repository's build tags and run it. Every CLI surface — utilities, linters,
# code generators — is a subcommand there rather than an ad-hoc main().
set -euo pipefail

here=$(dirname "$(readlink -f "$BASH_SOURCE")")
cd "$here"

app=$(mktemp)
trap 'rm -f -- "$app"' EXIT

go build -tags "$(tr -d '\n' < tags)" -o "$app" {{.AppPackage}} 1>&2
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
if ! ./{{.Name}}.sh gov gate --tags "$tags"; then
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
