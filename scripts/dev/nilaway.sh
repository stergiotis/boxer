#!/bin/bash
set -ev
here=$(dirname "$(readlink -f "$BASH_SOURCE")")
cd "$here/../.."
tags="$(cat "$here/../../tags" | tr -d "\n")"
# nilaway's own -tags flag is deprecated/no-op; pass tags via GOFLAGS so the
# analysis driver honors them and tag-gated packages are not silently excluded.
# -include-pkgs restricts analysis to first-party code; stdlib/3rd-party
# returns are assumed non-nil, suppressing unfixable false positives.
GOFLAGS="-tags=$tags" go tool nilaway \
    -include-pkgs=github.com/stergiotis/boxer \
    ./...
