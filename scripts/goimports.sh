#!/bin/bash
here=$(dirname "$(readlink -f "$BASH_SOURCE")")
cd "$here/.."
goimports-reviser -rm-unused -set-alias -format ./...
