#!/bin/bash
here=$(dirname "$(readlink -f "$BASH_SOURCE")")
cd "$here/.."
go get -u ./...
go mod tidy
