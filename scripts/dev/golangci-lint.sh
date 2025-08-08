#!/bin/bash
here=$(dirname "$(readlink -f "$BASH_SOURCE")")
cd "$here/../.."
go tool golangci-lint run -v ./...
