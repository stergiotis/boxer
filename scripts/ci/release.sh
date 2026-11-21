#!/bin/bash
# Check a release candidate, then print — never run — the commands that tag it
# (ADR-0390).
#
#   scripts/ci/release.sh [--version vX.Y.Z] [--fuzztime D] [<rev>]
#
# Checks out <rev> (default HEAD) into a detached worktree of its own and runs
# every gate in it, each to completion, in the order below, through the gate()
# wrapper of scripts/ci/gate_lib.sh: a gate prints its output only when it
# fails, and a gate whose output says a part of it was skipped — the Rust and
# font steps exit 0 with a "skipped" line when a toolchain is missing — counts
# as a failure, since a release is where every check has to have run.
#
#   module          go mod tidy --diff, go mod verify, go build ./...
#   tests           gotest.sh, gotest_binary_log.sh (both with --check-drift)
#   integration     gotest-integration.sh against a ClickHouse server: the one
#                   on 127.0.0.1:8123 if it answers, else the pinned throwaway
#                   from clickhouse_server.sh (a download of ~240 MB)
#   lint            lint.sh, every step, baselines enforced
#   rust            viewer_test.sh, watermark_test.sh
#   supply chain    govuln.sh, cargo_advisories.sh, license_gate.sh
#   generated code  boxer-idl-check.sh --force, the browser host digest
#   fuzz            one gofuzz.sh pass, --fuzztime per target (default 10s)
#   api             scripts/ci/apidiff.sh, the stable tier against the
#                   previous tag: the latest by version order that does not
#                   already contain the candidate
#   release notes   boxer gov releasenotes for previous-tag..candidate, with
#                   the apidiff listing, written to release-notes.md beside
#                   the logs (never into the tree)
#   clean tree      nothing above left the worktree dirty
#
# Exit status 0 means every gate ran and passed; the script then prints the
# signed-tag commands for --version (doc/howto/release-signing.md). It tags,
# signs and pushes nothing itself.
set -euo pipefail

here=$(dirname "$(readlink -f "${BASH_SOURCE[0]}")")
repo=$(readlink -f "$here/../..")
export GOWORK=off

version=""
fuzztime=10s
rev=HEAD
while [ $# -gt 0 ]; do
    case "$1" in
        --version) version="$2"; shift 2 ;;
        --fuzztime) fuzztime="$2"; shift 2 ;;
        -h|--help) sed -n '2,34p' "$0" | sed 's/^# \{0,1\}//'; exit 0 ;;
        -*) echo "release: unknown option $1" >&2; exit 2 ;;
        *) rev="$1"; shift ;;
    esac
done
if [ -n "$version" ] && ! [[ "$version" =~ ^v[0-9]+\.[0-9]+\.[0-9]+$ ]]; then
    echo "release: --version must look like v1.2.3" >&2
    exit 2
fi
sha=$(git -C "$repo" rev-parse --verify "$rev^{commit}")

# shellcheck source=commit_worktree.sh
source "$here/commit_worktree.sh"
enter_commit_worktree release "$sha"
tags="$(tr -d '\n' <tags)"
logdir="$(dirname "$wt")/logs"
rm -rf -- "$logdir"
mkdir -p "$logdir"

echo "release: candidate $(git log -1 --format='%h %s' "$sha")"
echo "release: full logs in $logdir"

# shellcheck source=gate_lib.sh
source "$here/gate_lib.sh"
gate_logdir="$logdir"

# ---- module, tests ------------------------------------------------------------
gate "go mod tidy --diff" go mod tidy --diff
gate "go mod verify" go mod verify
gate "go build" go build -tags "$tags" ./...
gate "gotest" scripts/ci/gotest.sh --check-drift
gate "gotest binary_log" scripts/ci/gotest_binary_log.sh --check-drift

# ---- integration ---------------------------------------------------------------
if ensure_clickhouse 1 "$(dirname "$wt")/clickhouse"; then
    gate "integration lane" scripts/ci/gotest-integration.sh
else
    not_run "integration lane" "no ClickHouse server on 127.0.0.1:8123 (see $logdir/clickhouse.log)"
fi
gate_stop_clickhouse

# ---- lint, Rust crates -----------------------------------------------------------
gate "lint" scripts/ci/lint.sh
gate "viewer" scripts/ci/viewer_test.sh
gate "watermark" scripts/ci/watermark_test.sh

# ---- supply chain --------------------------------------------------------------
gate "govulncheck" scripts/ci/govuln.sh
gate "cargo advisories" scripts/ci/cargo_advisories.sh
gate "license gate" scripts/ci/license_gate.sh

# ---- generated code -------------------------------------------------------------
gate "idl-check" scripts/dev/boxer-idl-check.sh --force --state release-idl-check
host_digest() {
    local sum=public/thestack/imzero2/browserhost/tabhost/browserhost.sum got want
    got=$(go run -tags "$tags" ./public/thestack/cmd/imzero2tab hostdigest | grep -E '^(sha256|idl) ')
    want=$(grep -E '^(sha256|idl) ' "$sum")
    echo "built:    $(tr '\n' ' ' <<<"$got")"
    echo "recorded: $(tr '\n' ' ' <<<"$want")"
    [ "$got" = "$want" ] || { echo "browserhost.sum is stale"; return 1; }
}
gate "browser host digest" host_digest

# ---- fuzz, API, release notes -------------------------------------------------------
gate "gofuzz ($fuzztime per target)" env BOXER_FUZZ_TIME="$fuzztime" BOXER_FUZZ_LOG_DIR="$logdir/fuzz" scripts/ci/gofuzz.sh
# The previous release: the newest tag by version order that the candidate
# is not itself, so a re-run on a tagged commit compares with the one before.
prev_tag=$(git tag --sort=-v:refname --no-contains "$sha" | head -n 1)
apidiff_listing="$logdir/apidiff.txt"
apidiff_against() {
    scripts/ci/apidiff.sh "$prev_tag" | tee "$apidiff_listing"
}
release_notes() {
    ./boxer.sh gov releasenotes --from "$prev_tag" --to "$sha" --apidiff "$apidiff_listing" >"$logdir/release-notes.md"
    echo "release notes for $prev_tag..$(git rev-parse --short "$sha") in $logdir/release-notes.md"
}
if [ -z "$prev_tag" ]; then
    not_run "apidiff" "no earlier tag to compare with"
    not_run "release notes" "no earlier tag to start the range at"
elif [ ! -x scripts/ci/apidiff.sh ]; then
    not_run "apidiff" "scripts/ci/apidiff.sh is absent at this commit"
    not_run "release notes" "no apidiff listing to include"
else
    gate "apidiff against $prev_tag" apidiff_against
    gate "release notes" release_notes
fi

# ---- the tree --------------------------------------------------------------------
gate "clean tree" scripts/ci/check_drift.sh

echo
if [ ${#gate_failed[@]} -gt 0 ]; then
    echo "release: NOT releasable — ${#gate_failed[@]} gate(s) failed: ${gate_failed[*]}"
    exit 1
fi
v="${version:-vX.Y.Z}"
echo "release: $(git rev-parse --short "$sha") passed every gate; release notes in $logdir/release-notes.md. To tag it (doc/howto/release-signing.md):"
echo
echo "  git tag -s -a $v -m 'boxer $v' $sha"
echo "  git verify-tag $v"
echo "  git push origin $v"
