#!/bin/bash
# The ADR-0026 capability cross-check: what each app's code exercises against
# what its manifest declares, in `compare` mode.
#
# One lint step. Runnable on its own; scripts/ci/lint.sh runs it with the
# others and documents the exit-status contract every step obeys — here a
# non-zero exit fails the gate.

set -e
set -o pipefail
here=$(dirname "$(readlink -f "$BASH_SOURCE")")
cd "$here/../../.."
tags="$(cat ./tags | tr -d "\n")"

# ADR-0026 §SD10: cross-checks the capabilities each app's own code
# exercises against its manifest declarations, in `compare` mode — a
# finding already accepted in baseline.go is reported and passes; a new
# one, or a baseline entry that no longer reproduces, fails.
#
# Runs as a plain Go test rather than a script over a JSON report, so it
# cannot drift from the code it checks. It is excluded from the default
# test gate (-short) because it costs ~15s and several GB of RSS building
# SSA over the apps' dependency cones; this is the step that runs it.
#
# Captured rather than piped: under `set -o pipefail` a `go test | grep`
# pipeline reports the grep's status too, so a run whose output the filter
# happens to drop entirely would read as a failure.
if cl_out=$(go test -tags "$tags" -count=1 \
    -run 'TestAnalyse_MatchesBaseline|TestAppSetIsComplete' \
    ./public/keelson/security/capslock/ 2>&1); then
    echo "passed"
    exit 0
fi
printf '%s\n' "$cl_out" | grep -vE '^\{"level":"(warn|info|debug)"' || true
exit 1
