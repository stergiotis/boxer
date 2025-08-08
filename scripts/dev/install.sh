#!/bin/bash
set -ev
here=$(dirname "$(readlink -f "$BASH_SOURCE")")
cd "$here"
"$here/../ci/install.sh"
go get -tool github.com/golangci/golangci-lint/cmd/golangci-lint@latest
go get -tool github.com/incu6us/goimports-reviser/v3@latest
go get -tool github.com/dkorunic/betteralign/cmd/betteralign@latest
