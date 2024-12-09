#!/bin/bash
set -ev
here=$(dirname "$(readlink -f "$BASH_SOURCE")")
cd "$here"
go install github.com/golangci/golangci-lint/cmd/golangci-lint@v1.62.2
go install gotest.tools/gotestsum@latest
go install github.com/incu6us/goimports-reviser/v3@latest
