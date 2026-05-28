#!/bin/bash
set -ev
here=$(dirname "$(readlink -f "$BASH_SOURCE")")
cd "$here"
rm -f main_go
build_tags=$(cat ../../tags | tr -d $'\n')
#export CGO_ENABLED=1 # -race
#go build -race -tags "$build_tags" -o main_go ../go/public/thestack/cmd/imzero2/
export CGO_ENABLED=0 # ensure a cgo-free build
go build -tags "$build_tags" -o main_go ../go/public/thestack/cmd/imzero2/
