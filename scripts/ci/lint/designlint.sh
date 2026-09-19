#!/bin/bash
# IDS Tier 1 mechanical design rules over the egui2 UI tree and the keelson
# runtime.
#
# One lint step. Runnable on its own; scripts/ci/lint.sh runs it with the
# others and documents the exit-status contract every step obeys — here a
# non-zero exit fails the gate.

set -e
set -o pipefail
here=$(dirname "$(readlink -f "$BASH_SOURCE")")
cd "$here/../../.."
tags="$(cat ./tags | tr -d "\n")"

# IDS Tier 1 mechanical rules (ADR-0029 §SD8), driven via `go vet -vettool=`
# over a tempfile-built multichecker binary — the tag-aware analyzer path
# (multichecker.Main's own -tags flag is a deprecated no-op). Hard gate since
# the M5 fleet backfill (2026-07-12) took every shipped rule to zero findings:
# ANY output — finding or compile error — fails the build. Intentional
# exceptions use the per-line `// designlint:ignore=<rule-id> (reason)`
# annotation (doc/design-system/policy/tier1-mechanical.md §Annotations).
# Scoped to the egui2 UI tree and keelson runtime, where IDS tokens apply;
# generated files are filtered out.
dl_bin=$(mktemp -t designlint.XXXXXX)
if ! go build -tags "$tags" -o "$dl_bin" ./public/keelson/designsystem/lint/cmd/designlint 2>/dev/null; then
    rm -f "$dl_bin"
    echo "designlint vettool failed to build"
    exit 1
fi
dl_out=$(go vet -vettool="$dl_bin" -tags "$tags" \
    ./public/thestack/imzero2/... ./public/keelson/runtime/... 2>&1 |
    grep -v '\.out\.go:' | grep -v '\.gen\.go:' | grep -v '^warning: ' || true)
rm -f "$dl_bin"
if [ -n "$dl_out" ]; then
    printf '%s\n' "$dl_out"
    exit 1
fi
echo "passed"
