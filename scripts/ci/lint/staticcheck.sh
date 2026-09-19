#!/bin/bash
# staticcheck over ./public/..., minus the style checks and minus the packages
# in the skip list below.
#
# One lint step. Runnable on its own; scripts/ci/lint.sh runs it with the
# others and documents the exit-status contract every step obeys. Findings here
# do not block: this step exits 2, which the `warn` marker on its line in that
# script's step list turns into a warning rather than a failure.

set -e
set -o pipefail
here=$(dirname "$(readlink -f "$BASH_SOURCE")")
cd "$here/../../.."
tags="$(cat ./tags | tr -d "\n")"

# Exclude generated ANTLR parser files. Capture so we can mark warn vs pass;
# this trades streaming for status visibility (staticcheck batches anyway).
#
# SKIP LIST — packages withheld from the run because staticcheck aborts the
# whole PROCESS on them, taking every other package's analysis down with it.
# This is not a findings filter: nothing in a listed package gets checked, and
# the step reports warn rather than pass while the list is non-empty.
#
# The cause is upstream. staticcheck carries analysis facts between packages
# keyed by golang.org/x/tools/go/types/objectpath, whose method paths are
# ordinals into the receiver's method list ("SectionReaders.M4"). Under go1.27
# generic methods (ADR-0199) the two sides of the export-data boundary number
# those methods differently: analysing marshallreflect FROM SOURCE encodes
# (*SectionReaders).PlainColumn as M4 and .Section as M5, while a consumer
# reading the same type FROM EXPORT DATA resolves M4 to .DetectAll and M5 to
# .ReadComponent — export data groups the non-generic methods ahead of the
# generic ones, source order interleaves them. Every fact on such a type
# therefore lands on the wrong method. SA4023 reads the 1-result builder
# method's nilness fact as if it belonged to 3-result ReadComponent and indexes
# past the end:
#
#   panic: runtime error: index out of range [2] with length 1
#     nilness.(*Result).Nilness -> sa4023.run
#
# -checks cannot dodge it: staticcheck runs every analyzer and filters
# diagnostics afterwards, so "-SA4023" still panics. No published version
# helps either — v0.7.0, the pin this repo carried until the bump alongside
# this note, cannot read go1.27 export data at all ("export data version 4 is
# greater than maximum supported version 2"), and master as of 7dc2d7d2 has the
# same unguarded index as v0.8.0. Re-check on the next release; when the list
# can be emptied, delete it and the panic branch with it.
#
# Note what the skip does NOT fix: the mis-attribution is silent everywhere
# else. Any fact-carrying check (U1000 among them) reading a type that mixes
# generic and non-generic methods may be reading a sibling method's fact. Two
# types are exposed today — marshallreflect.SectionReaders and
# ecsdemo/stage2.FatRow — so treat findings that name their methods with
# suspicion until upstream numbers them consistently.
sc_skip='github.com/stergiotis/boxer/public/thestack/imzero2/egui2/widgets/componentview'
mapfile -t sc_pkgs < <(go list -tags "$tags" ./public/... | grep -vxF "$sc_skip")
if [ -n "$sc_skip" ]; then
    echo "skipped, NOT analysed (see the note at the top of $0):"
    printf '%s\n' "$sc_skip" | sed 's/^/  /'
fi
sc_out=$(go tool honnef.co/go/tools/cmd/staticcheck -tags "$tags" \
    -checks "all,-ST1000,-ST1003,-ST1005,-ST1016,-ST1020,-ST1021,-ST1022,-S1023,-SA4011,-SA1019" \
    "${sc_pkgs[@]}" 2>&1 | grep -v '\.out\.go:' | grep -v '\.gen\.go:' || true)
if printf '%s' "$sc_out" | grep -q '^panic:'; then
    echo "DID NOT RUN — staticcheck aborted, no analysis was performed:"
    printf '%s' "$sc_out" | grep -m1 '^panic:' | sed 's/^/  /'
    echo "  (a package outside the skip list now trips it — see the note at the top of $0)"
    exit 2
elif [ -n "$sc_out" ]; then
    printf '%s\n' "$sc_out"
    exit 2
elif [ -n "$sc_skip" ]; then
    echo "no findings, but the skip list is non-empty"
    exit 2
fi
echo "passed"
