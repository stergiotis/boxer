#!/bin/bash
# Fail when the working tree is not clean, and show what moved.
#
# Run after a test or generate step on a clean checkout: generator tests
# rewrite their *.out.* files in place, so a dirty tree means committed
# generated code drifted from its generator, or a test wrote into the tree.
# Used by gotest.sh --check-drift, gotest_binary_log.sh --check-drift, gate.sh
# and release.sh, so the workflow and the scripts apply one check.
set -e
set -o pipefail
here=$(dirname "$(readlink -f "$BASH_SOURCE")")
cd "$here/../.."
status="$(git status --porcelain)"
if [ -z "$status" ]; then
    echo "drift: tree clean"
    exit 0
fi
echo "$status"
git --no-pager diff
msg="The run left the tree dirty — committed generated code drifted from its generators (or a test wrote into the tree). Regenerate locally and commit the result."
if [ -n "${GITHUB_ACTIONS:-}" ]; then
    echo "::error::$msg"
else
    echo "drift: $msg" >&2
fi
exit 1
