#!/bin/bash
# gofmt drift over the whole tree, generated files skipped by their header.
#
# One lint step. Runnable on its own; scripts/ci/lint.sh runs it with the
# others and documents the exit-status contract every step obeys — here a
# non-zero exit fails the gate.

set -e
set -o pipefail
here=$(dirname "$(readlink -f "$BASH_SOURCE")")
cd "$here/../../.."

# Go formatting enforcement — plain `gofmt`, the baseline §9 defers to. It is
# the Go counterpart of the rustfmt step, and closes the gap
# ENGINEERING_PRACTICES §10 recorded (gofumpt / gci stay unadopted). Drift had
# reached 175 files before the tree was cleared on 2026-08-06.
#
# Generated files are skipped by their `Code generated ... DO NOT EDIT.` header
# rather than by path: relying on the `.out.go` / `.gen.go` patterns the vet and
# staticcheck steps filter on would have missed every generated file that isn't
# suffixed that way — which, until the `file-naming` gate started enforcing it
# (ADR-0048 N2/N3; `gov/filenaming`'s "generated-suffix" rule), was a real,
# silent gap: `palette_generated.go` and `chaliases_gen.go` sat undetected for
# a while before being renamed to close it. That gate now catches a fresh case
# of the same shape before it can linger the same way, but this step still
# reads the header rather than the path: a generator can still choose to emit
# somewhere the pattern doesn't reach, and this step's job is to not choke on
# it regardless. The header is read from the first lines only, which is where
# Go's own convention puts it — a file merely quoting the phrase is still checked.
#
# FIXING A FAILURE — read the diff before running `gofmt -w`. gofmt reformats
# doc comments, and its parser takes two kinds of ordinary prose punctuation for
# markup: `` and '' become Unicode quotes, and a line-leading + becomes a -
# bullet. Both have already falsified comments in this repo — one documenting
# SQL's doubled-quote escape, one where the + continued a sum and became a
# minus. When `gofmt -d` changes what a comment SAYS rather than how it is
# spaced, fix the prose first (name the thing instead of spelling it; rewrap so
# a + ends a line rather than starting one), then format. See b8c1f701.
gofmt_dirty=$(gofmt -l . 2>/dev/null || true)
gofmt_out=""
for f in $gofmt_dirty; do
    head -n 5 "$f" | grep -qE '^// Code generated .* DO NOT EDIT\.$' && continue
    gofmt_out+="$f"$'\n'
done
if [ -n "$gofmt_out" ]; then
    echo "not gofmt-clean (run gofmt -d on each, then see the note at the top of $0):"
    printf '%s' "$gofmt_out"
    exit 1
fi
echo "passed"
