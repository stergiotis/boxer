#!/bin/bash
# Serve the local module's godoc at http://localhost:6060 (requires
# `go install golang.org/x/tools/cmd/godoc@latest` on PATH).
here=$(dirname "$(readlink -f "$BASH_SOURCE")")
cd "$here/../.."
godoc -http=:6060
