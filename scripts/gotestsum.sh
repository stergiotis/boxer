#!/bin/bash
here=$(dirname "$(readlink -f "$BASH_SOURCE")")
cd "$here/.."
gotestsum --format short-verbose -- --short ./...
