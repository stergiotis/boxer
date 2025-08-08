#!/bin/bash
here=$(dirname "$(readlink -f "$BASH_SOURCE")")
cd "$here/.."
go tool gotestsum --format short-verbose -- --short ./...
