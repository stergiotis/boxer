#!/bin/bash
here=$(dirname "$(readlink -f "$BASH_SOURCE")")
cd "$here/../.."
go tool govulncheck -show verbose ./public/...
