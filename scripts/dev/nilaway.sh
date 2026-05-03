#!/bin/bash
set -ev
here=$(dirname "$(readlink -f "$BASH_SOURCE")")
cd "$here/../.."
tags="$(cat "$here/../../tags" | tr -d "\n")"
# nilaway's own -tags flag is deprecated/no-op; pass tags via GOFLAGS so the
# analysis driver honors them and tag-gated packages are not silently excluded.
GOFLAGS="-tags=$tags" go tool nilaway ./...
