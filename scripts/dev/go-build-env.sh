#!/bin/bash
# Source this before `go build` so every boxer binary is built one way, and so
# two builds of one commit are byte-identical (ADR-0215). Exports:
#
#   BOXER_GO_TAGS   the contents of ./tags, newline-stripped — the one definition
#                   the four build sites used to each re-derive.
#   BOXER_GO_FLAGS  -trimpath -buildvcs=auto
#                   -trimpath strips the checkout and module-cache paths out of
#                   the binary. Without it a host carried thousands of absolute
#                   paths and could only be reproduced from the same directory
#                   by the same user. -buildvcs stays on: the stamp is
#                   provenance, and for a given commit with a clean tree it is
#                   identical, so it costs nothing here. `auto` rather than
#                   `true`: an airgap tarball is a `git archive` export with no
#                   .git, where `true` is a hard error.
#   CGO_ENABLED=0   forced. boxer imports no cgo, but the flag still decides
#                   which net and os/user implementations are compiled in, so
#                   leaving it to the environment made one source tree produce
#                   two different hosts.
#   GOTOOLCHAIN     pinned to exactly the version in go.mod — but only when the
#                   caller has not set it. The `go` directive is a FLOOR:
#                   GOTOOLCHAIN=auto builds happily with a newer local toolchain,
#                   and a different compiler is a different binary. No go.mod
#                   directive pins downward, so it has to be the environment.
#                   The airgap environment exports GOTOOLCHAIN=local (it ships
#                   one toolchain and must not reach the network); that setting
#                   is respected rather than overwritten.
#
# Usage — from anywhere, having cd'd to the repo root:
#
#     source scripts/dev/go-build-env.sh
#     # shellcheck disable=SC2086 # deliberate word splitting
#     go build $BOXER_GO_FLAGS -tags "$BOXER_GO_TAGS" -o "$out" ./public/app

_gbe_root=$(cd "$(dirname "$(readlink -f "${BASH_SOURCE[0]}")")/../.." && pwd)

BOXER_GO_TAGS="$(tr -d '\n' < "$_gbe_root/tags")"
BOXER_GO_FLAGS="-trimpath -buildvcs=auto"
export BOXER_GO_TAGS BOXER_GO_FLAGS

CGO_ENABLED=0
export CGO_ENABLED

if [ -z "${GOTOOLCHAIN:-}" ]; then
    _gbe_go="$(awk '$1=="go"{print $2; exit}' "$_gbe_root/go.mod")"
    if [ -n "$_gbe_go" ]; then
        GOTOOLCHAIN="go$_gbe_go"
        export GOTOOLCHAIN
    fi
    unset _gbe_go
fi

unset _gbe_root
