#!/bin/bash
# Check a release candidate, then print — never run — the commands that tag it
# (ADR-0390).
#
#   scripts/ci/release.sh [--version vX.Y.Z] [--fuzztime D] [<rev>]
#
# Checks out <rev> (default HEAD) into a detached worktree of its own and runs
# every gate in it, each to completion, in the order below. Modelled on
# scripts/dev/updatedeps.sh's gate(): a gate prints its output only when it
# fails, and a gate whose output says a part of it was skipped — the Rust and
# font steps exit 0 with a "skipped" line when a toolchain is missing — counts
# as a failure here, since a release is where every check has to have run.
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
#   api             scripts/ci/apidiff.sh, the stable tier against the last tag
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
        -h|--help) sed -n '2,31p' "$0" | sed 's/^# \{0,1\}//'; exit 0 ;;
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

failed=()

# gate LABEL CMD...: run CMD, keep its output in $logdir, print it only on
# failure. Output reporting a skipped part fails the gate.
gate() {
    local label="$1" log t0 code=0
    shift
    log="$logdir/$(tr -c 'A-Za-z0-9._-' '_' <<<"$label").log"
    t0=$SECONDS
    "$@" >"$log" 2>&1 || code=$?
    if [ "$code" -ne 0 ]; then
        printf 'gate %-28s FAILED (%ds)\n' "$label" $((SECONDS - t0))
        tail -n 60 "$log" | sed 's/^/    /'
        failed+=("$label")
    elif grep -qE 'skipped( \(|:)' "$log"; then
        printf 'gate %-28s SKIPPED, counts as failure (%ds)\n' "$label" $((SECONDS - t0))
        grep -E 'skipped( \(|:)' "$log" | sed 's/^/    /'
        failed+=("$label")
    else
        printf 'gate %-28s ok (%ds)\n' "$label" $((SECONDS - t0))
    fi
}

# not_run LABEL REASON: a gate that cannot start counts as a failure too.
not_run() {
    printf 'gate %-28s NOT RUN, counts as failure: %s\n' "$1" "$2"
    failed+=("$1")
}

# ---- module, tests ------------------------------------------------------------
gate "go mod tidy --diff" go mod tidy --diff
gate "go mod verify" go mod verify
gate "go build" go build -tags "$tags" ./...
gate "gotest" scripts/ci/gotest.sh --check-drift
gate "gotest binary_log" scripts/ci/gotest_binary_log.sh --check-drift

# ---- integration ---------------------------------------------------------------
ch_dir=""
if curl -fsS http://127.0.0.1:8123/ping >/dev/null 2>&1; then
    echo "release: using the ClickHouse server on 127.0.0.1:8123"
else
    ch_dir="$(dirname "$wt")/clickhouse"
    if scripts/ci/clickhouse_server.sh install "$ch_dir" >"$logdir/clickhouse.log" 2>&1 &&
        scripts/ci/clickhouse_server.sh start "$ch_dir" >>"$logdir/clickhouse.log" 2>&1; then
        echo "release: started the pinned throwaway ClickHouse server"
        export PATH="$ch_dir:$PATH"
    else
        ch_dir=""
    fi
fi
if curl -fsS http://127.0.0.1:8123/ping >/dev/null 2>&1; then
    gate "integration lane" scripts/ci/gotest-integration.sh
else
    not_run "integration lane" "no ClickHouse server on 127.0.0.1:8123 (see $logdir/clickhouse.log)"
fi
if [ -n "$ch_dir" ]; then
    scripts/ci/clickhouse_server.sh stop "$ch_dir" >/dev/null 2>&1 || true
fi

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

# ---- fuzz, API -------------------------------------------------------------------
gate "gofuzz ($fuzztime per target)" env BOXER_FUZZ_TIME="$fuzztime" BOXER_FUZZ_LOG_DIR="$logdir/fuzz" scripts/ci/gofuzz.sh
if [ -x scripts/ci/apidiff.sh ]; then
    gate "apidiff" scripts/ci/apidiff.sh
else
    not_run "apidiff" "scripts/ci/apidiff.sh is absent at this commit"
fi

# ---- the tree --------------------------------------------------------------------
gate "clean tree" scripts/ci/check_drift.sh

echo
if [ ${#failed[@]} -gt 0 ]; then
    echo "release: NOT releasable — ${#failed[@]} gate(s) failed: ${failed[*]}"
    exit 1
fi
v="${version:-vX.Y.Z}"
echo "release: $(git rev-parse --short "$sha") passed every gate. To tag it (doc/howto/release-signing.md):"
echo
echo "  git tag -s -a $v -m 'boxer $v' $sha"
echo "  git verify-tag $v"
echo "  git push origin $v"
