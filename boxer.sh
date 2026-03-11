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
go build -buildvcs=true -tags "$(cat "$here/tags" | tr -d "\n")" -o "$appfile" ./public/app 1>&2
cd -
"$appfile" --logFormat console "$@"
