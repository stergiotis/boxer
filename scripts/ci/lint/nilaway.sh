#!/bin/bash
# nilaway over ./public/... — NOT in scripts/ci/lint.sh's step list.
#
# The step is wired and runnable; what is missing is a triaged findings set, so
# the driver does not run it. Re-enable by adding it to the `steps` array in
# scripts/ci/lint.sh. scripts/dev/nilaway.sh is the wider local runner (./...).
#
# Exit-status contract as documented in scripts/ci/lint.sh. Findings here do
# not block: this step exits 2, and a line added to that script's step list
# would need the `warn` marker to keep it out of the failure list.

set -e
set -o pipefail
here=$(dirname "$(readlink -f "$BASH_SOURCE")")
cd "$here/../../.."
tags="$(cat ./tags | tr -d "\n")"

# nilaway's own -tags flag is deprecated/no-op; build tags must be passed via
# GOFLAGS so the analysis driver picks them up. Without this, tag-gated
# packages (e.g. gpu_intel, integration) are excluded and importers cascade into
# hundreds of bogus "could not import / undefined" lines.
# -include-pkgs restricts analysis to first-party code; stdlib and 3rd-party
# returns are then assumed non-nil, which suppresses the bulk of noise from
# os.Stdout/http.Response.Body/ANTLR-style false positives that we cannot
# fix locally.
na_out=$(GOFLAGS="-tags=$tags" go tool go.uber.org/nilaway/cmd/nilaway \
    -include-pkgs=github.com/stergiotis/boxer \
    ./public/... 2>&1 || true)
if [ -n "$na_out" ]; then
    printf '%s\n' "$na_out"
    exit 2
fi
echo "passed"
