#!/bin/bash
set -e
set -o pipefail
here=$(dirname "$(readlink -f "$BASH_SOURCE")")
cd "$here/../.."
tags="$(cat "$here/../../tags" | tr -d "\n")"
go test -race -json -short -cover -tags "$tags" ./... | go tool tparse -progress -trimpath -slow 20
# The opt-in boxer_unattended tag compiles in the agent dispatcher's
# unattended mode (ADR-0298); its tests skip without it.
go test -race -json -short -tags "$tags,boxer_unattended" ./public/keelson/runtime/agent/ | go tool tparse -progress -trimpath
