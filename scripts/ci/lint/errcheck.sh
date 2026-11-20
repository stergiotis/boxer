#!/bin/bash
# errcheck over the whole module, with the write-to-a-buffer families excluded,
# held to a committed baseline.
#
#   scripts/ci/lint/errcheck.sh                     check
#   scripts/ci/lint/errcheck.sh --update-baseline   rewrite the baseline
#
# One lint step. Runnable on its own; scripts/ci/lint.sh runs it with the
# others and documents the exit-status contract every step obeys. A finding
# that is not in scripts/ci/errcheck-baseline.txt fails the step; baseline
# entries that no longer occur exit 2, which the `warn` marker on this step's
# line in lint.sh turns into a warning. scripts/ci/lint/ratchet.sh holds the
# comparison.

set -e
set -o pipefail
here=$(dirname "$(readlink -f "$BASH_SOURCE")")
cd "$here/../../.."
tags="$(cat ./tags | tr -d "\n")"
baseline=$(readlink -f "$here/../errcheck-baseline.txt")
update=0
case "${1:-}" in
    --update-baseline) update=1 ;;
    "") ;;
    *) echo "errcheck.sh: unknown argument $1" >&2; exit 2 ;;
esac

ec_out=$(go tool github.com/kisielk/errcheck -tags "$tags" \
    -exclude <(printf '%s\n' \
        'fmt.Fprintf' 'fmt.Fprintln' 'fmt.Fprint' \
        '(*strings.Builder).WriteString' '(*strings.Builder).WriteByte' '(*strings.Builder).WriteRune' \
        '(*bytes.Buffer).WriteString' '(*bytes.Buffer).WriteByte' '(*bytes.Buffer).Write') \
    ./... 2>&1 | grep -v '\.out\.go:' | grep -v '\.gen\.go:' || true)
# "path:line:col:<TAB>call" -> "path: call": the baseline keys a finding by
# file and the unchecked call, so unrelated edits do not move it.
findings=$(mktemp)
trap 'rm -f -- "$findings"' EXIT
printf '%s\n' "$ec_out" | sed -E 's/^([^:]+):[0-9]+:[0-9]+:[[:space:]]*/\1: /' >"$findings"
if [ "$update" = 1 ]; then
    "$here/ratchet.sh" --update "$baseline" "$findings"
    exit 0
fi
exec "$here/ratchet.sh" "$baseline" "$findings"
