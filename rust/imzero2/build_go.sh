#!/bin/bash
set -ev
here=$(dirname "$(readlink -f "$BASH_SOURCE")")
cd "$here"
rm -f main_go
# The imzero2 host installs the keelson logbridge, whose wire format is CBOR
# (zerolog built under the binary_log tag). Append binary_log so the bridge
# decodes correctly at runtime; boxer's default ./tags omits it because it
# would flip global zerolog to CBOR and break observability/eh's JSON tests
# (a dedicated CI lane covers binary_log instead).
build_tags="$(cat ../../tags | tr -d $'\n'),binary_log"
#export CGO_ENABLED=1 # -race
#go build -race -tags "$build_tags" -o main_go ../../public/thestack/cmd/imzero2/
export CGO_ENABLED=0 # ensure a cgo-free build
go build -tags "$build_tags" -o main_go ../../public/thestack/cmd/imzero2/
