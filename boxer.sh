#!/bin/bash
set -e
set -o pipefail
here=$(dirname "$(readlink -f "$BASH_SOURCE")")
cd "$here"
appfile=$(realpath "$here/$(basename "$(mktemp)")")
cleanup() {
    rv=$?
    rm -f -- "$appfile"
    exit $rv
}
trap 'cleanup' EXIT
# shellcheck source=/dev/null
source "$here/scripts/dev/go-build-env.sh"
# shellcheck disable=SC2086 # deliberate word splitting of the flag list
go build $BOXER_GO_FLAGS -tags "$BOXER_GO_TAGS" -o "$appfile" ./public/app 1>&2
cd - &> /dev/null
"$appfile" --logFormat console "$@"
