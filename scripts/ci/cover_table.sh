#!/bin/bash
# Per-package statement coverage from a Go cover profile, as a Markdown table.
#
#   scripts/ci/cover_table.sh PROFILE
#
# Reads the profile gotest.sh --coverprofile writes (-coverpkg=./..., so one
# block can appear once per test binary that ran it): a block counts as covered
# when any binary reached it, and its statements are counted once. Packages
# are listed by import path, lowest coverage first; the last row is the module.
set -euo pipefail
[ $# -eq 1 ] || { echo "usage: cover_table.sh PROFILE" >&2; exit 2; }
awk '
    NR == 1 && /^mode:/ { next }
    {
        # file:startLine.startCol,endLine.endCol numStmts count
        block = $1; stmts[block] = $2
        if ($3 > 0) hit[block] = 1
    }
    END {
        for (b in stmts) {
            file = b; sub(/:.*/, "", file)
            pkg = file; sub(/\/[^\/]*$/, "", pkg)
            total[pkg] += stmts[b]; all += stmts[b]
            if (b in hit) { covered[pkg] += stmts[b]; allc += stmts[b] }
        }
        for (p in total) {
            pct = total[p] ? 100 * covered[p] / total[p] : 0
            printf "%08.3f\t| %s | %d | %d | %.1f%% |\n", pct, p, total[p], covered[p] + 0, pct
        }
        printf "999999\t| **module** | %d | %d | %.1f%% |\n", all, allc, all ? 100 * allc / all : 0
    }
' "$1" | LC_ALL=C sort -t$'\t' -k1,1 | cut -f2- | {
    echo "| package | statements | covered | coverage |"
    echo "|---|---:|---:|---:|"
    cat
}
