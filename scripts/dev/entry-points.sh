#!/bin/bash
set -e
set -o pipefail
here=$(dirname "$(readlink -f "$BASH_SOURCE")")
cd "$here/../.."
tags="$(cat "$here/../../tags" | tr -d "\n")"
./boxer.sh dev entry-points --tags="$tags" "$@"
