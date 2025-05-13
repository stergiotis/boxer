#!/bin/bash
set -e
set -o pipefail
here=$(dirname "$(readlink -f "$BASH_SOURCE")")
cd "$here"
appfile="$here/$(basename "$(mktemp)")"
cleanup() {
    rv=$?
    rm -f -- "$appfile"
    exit $rv
}
trap 'cleanup' EXIT
cd "public/app"
go build -v -buildvcs=true -tags $(cat "$here/tags" | tr -d "\n") ./main.go  -o "$appfile"
cd -
"$appfile" --logFormat console "$@"
