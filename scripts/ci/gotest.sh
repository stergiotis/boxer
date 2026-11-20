#!/bin/bash
# The default test lane: every package, -race -short, formatted by tparse.
#
#   scripts/ci/gotest.sh [--check-drift] [--coverprofile FILE]
#
# --check-drift  after the tests, fail when the tree is not clean. Generator
#                tests rewrite their *.out.* files in place, so a dirty tree
#                means committed generated code drifted from its generator (or
#                a test wrote into the tree). Only meaningful on a clean
#                checkout — CI, or a worktree made by scripts/ci/gate.sh.
# --coverprofile write one cross-package profile (-coverpkg=./...) to FILE,
#                which belongs outside the tree when --check-drift is on;
#                slower, since every test binary instruments the whole module.
set -e
set -o pipefail
here=$(dirname "$(readlink -f "$BASH_SOURCE")")
cd "$here/../.."
tags="$(cat "$here/../../tags" | tr -d "\n")"

check_drift=0
cover=(-cover)
while [ $# -gt 0 ]; do
    case "$1" in
        --check-drift) check_drift=1; shift ;;
        --coverprofile) cover=(-coverpkg=./... "-coverprofile=$2"); shift 2 ;;
        *) echo "gotest.sh: unknown argument $1" >&2; exit 2 ;;
    esac
done

rc=0
go test -race -json -short "${cover[@]}" -tags "$tags" ./... | go tool tparse -progress -trimpath auto -slow 20 || rc=1
# The opt-in boxer_unattended tag compiles in the agent dispatcher's
# unattended mode (ADR-0298); its tests skip without it.
go test -race -json -short -tags "$tags,boxer_unattended" ./public/keelson/runtime/agent/ | go tool tparse -progress -trimpath auto || rc=1

if [ "$check_drift" = 1 ]; then
    "$here/check_drift.sh" || rc=1
fi
exit "$rc"
