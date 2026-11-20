#!/bin/bash
# Compare a step's findings with its committed baseline (ADR-0390).
#
#   ratchet.sh BASELINE FINDINGS            compare
#   ratchet.sh --update BASELINE FINDINGS   rewrite BASELINE from FINDINGS
#
# Not a lint step of its own: staticcheck.sh and errcheck.sh call it. Both files
# hold one normalised finding per line — "path: message", no line or column, so
# an edit elsewhere in a file does not move its entries. They are compared as
# multisets: two identical findings in one file need two baseline lines.
#
# Exit status, in the lint.sh step contract:
#   0  every finding is in the baseline and every baseline line still occurs
#   1  a finding is not in the baseline — fix it; baselining a new finding is a
#      decision to make in review, with --update, not a way to pass the gate
#   2  only stale baseline lines: findings that no longer occur. Delete them, so
#      the baseline only shrinks.
set -e
set -o pipefail

update=0
if [ "${1:-}" = --update ]; then
    update=1
    shift
fi
[ $# -eq 2 ] || { echo "usage: ratchet.sh [--update] BASELINE FINDINGS" >&2; exit 2; }
baseline="$1"
findings="$2"

sorted() { grep -v '^[[:space:]]*$' "$1" | grep -v '^#' | LC_ALL=C sort || true; }

if [ "$update" = 1 ]; then
    {
        sed -n '/^#/p' "$baseline" 2>/dev/null || true
        sorted "$findings"
    } >"$baseline.new"
    mv -- "$baseline.new" "$baseline"
    echo "baseline rewritten: $baseline ($(sorted "$baseline" | wc -l) entries)"
    exit 0
fi

[ -f "$baseline" ] || { echo "no baseline at $baseline" >&2; exit 1; }
new=$(LC_ALL=C comm -13 <(sorted "$baseline") <(sorted "$findings"))
stale=$(LC_ALL=C comm -23 <(sorted "$baseline") <(sorted "$findings"))
held=$(sorted "$findings" | wc -l)

rc=0
if [ -n "$new" ]; then
    echo "new findings, not in $baseline:"
    printf '%s\n' "$new" | sed 's/^/  /'
    rc=1
fi
if [ -n "$stale" ]; then
    echo "baseline entries that no longer occur — delete them from $baseline:"
    printf '%s\n' "$stale" | sed 's/^/  /'
    [ "$rc" -eq 1 ] || rc=2
fi
if [ "$rc" -eq 0 ]; then
    echo "no new findings ($held held by the baseline)"
fi
exit "$rc"
