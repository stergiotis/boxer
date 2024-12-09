#!/bin/bash
here=$(dirname "$(readlink -f "$BASH_SOURCE")")
cd "$here/.."
golangci-lint run -v ./...
