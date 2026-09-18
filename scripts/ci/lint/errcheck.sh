#!/bin/bash
# errcheck over ./public/..., with the write-to-a-buffer families excluded.
#
# One lint step. Runnable on its own; scripts/ci/lint.sh runs it with the
# others and documents the exit-status contract every step obeys. Findings here
# do not block: this step exits 2, which the `warn` marker on its line in that
# script's step list turns into a warning rather than a failure.

set -e
set -o pipefail
here=$(dirname "$(readlink -f "$BASH_SOURCE")")
cd "$here/../../.."
tags="$(cat ./tags | tr -d "\n")"

ec_out=$(go tool github.com/kisielk/errcheck -tags "$tags" \
    -exclude <(printf '%s\n' \
        'fmt.Fprintf' 'fmt.Fprintln' 'fmt.Fprint' \
        '(*strings.Builder).WriteString' '(*strings.Builder).WriteByte' '(*strings.Builder).WriteRune' \
        '(*bytes.Buffer).WriteString' '(*bytes.Buffer).WriteByte' '(*bytes.Buffer).Write') \
    ./public/... 2>&1 | grep -v '\.out\.go:' | grep -v '\.gen\.go:' || true)
if [ -n "$ec_out" ]; then
    printf '%s\n' "$ec_out"
    exit 2
fi
echo "passed"
