#!/bin/bash
# License compliance gate. Walks the transitive dependency graph of all
# packages under ./public/... and rejects any dependency whose license
# falls into go-licenses' "forbidden" (AGPL/SSPL/...) or "restricted"
# (GPL/LGPL/...) categories. boxer is MIT-licensed and cannot accept
# copyleft inbound dependencies; the gate enforces this prospectively.
#
# The gate intentionally does NOT fail on "unknown" classifications.
# Some upstream Go modules ship a single LICENSE at the module root and
# go-licenses cannot always resolve it for subpackages (e.g. golang/freetype
# raster/ and truetype/). Such cases are surfaced at the end of the run
# for periodic manual review but do not block CI. See
# THIRD_PARTY_NOTICES.md section 3.1 for the policy rationale.
set -e
set -o pipefail
here=$(dirname "$(readlink -f "$BASH_SOURCE")")
cd "$here/../.."
tags="$(cat tags | tr -d "\n")"

log=$(mktemp)
trap 'rm -f "$log"' EXIT

set +e
GOFLAGS="-tags=$tags" go tool github.com/google/go-licenses check \
    --disallowed_types=forbidden,restricted \
    --ignore github.com/stergiotis/boxer \
    ./public/... 2>&1 | tee "$log"
rc=${PIPESTATUS[0]}
set -e

unresolved=$(grep -c '^E.*Failed to find license' "$log" || true)
if [ "$unresolved" -gt 0 ]; then
    echo ""
    echo "=== unresolved licenses ($unresolved) -- review manually ==="
    grep '^E.*Failed to find license' "$log" \
        | sed 's|.*license for \([^:]*\):.*|  - \1|' \
        | sort -u
fi

exit "$rc"
