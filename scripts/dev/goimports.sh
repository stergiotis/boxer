#!/bin/bash
here=$(dirname "$(readlink -f "$BASH_SOURCE")")
cd "$here/.."
go tool goimports-reviser -rm-unused -set-alias -format ./...
