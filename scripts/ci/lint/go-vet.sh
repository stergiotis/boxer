#!/bin/bash
# go vet over ./public/..., with generated files filtered out of the findings.
#
# One lint step. Runnable on its own; scripts/ci/lint.sh runs it with the
# others and documents the exit-status contract every step obeys — here a
# non-zero exit fails the gate.

set -e
set -o pipefail
here=$(dirname "$(readlink -f "$BASH_SOURCE")")
cd "$here/../../.."
tags="$(cat ./tags | tr -d "\n")"

# go vet has no built-in exclude for generated files, so filter output.
#
# `^warning: ` is dropped too, here and in the designlint step. Those are the go
# COMMAND's own lines about the machine, not the analysis's about the code —
# "warning: both GOPATH and GOROOT are the same directory" on a checkout where
# go is installed under $HOME/go being the one seen in practice. Both steps
# treat any surviving output as a finding, so without this a toolchain note
# about the contributor's install reads as a code defect. Nothing real is
# hidden: findings carry a file:line:col prefix, and a package that fails to
# build arrives as `# pkg` or `vet: ...`, none of which start with "warning: ".
vet_out=$(go vet -tags "$tags" ./public/... 2>&1 |
    grep -v '\.out\.go:' | grep -v '\.gen\.go:' | grep -v '^warning: ' || true)
if [ -n "$vet_out" ]; then
    printf '%s\n' "$vet_out"
    exit 1
fi
echo "passed"
