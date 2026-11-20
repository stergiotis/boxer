#!/bin/bash
# The nightly run (ADR-0390): tests one commit of main on this machine and,
# when every step passed, moves the local ref last-green to it.
#
#   scripts/ci/nightly.sh [options] [<rev>]
#
#   <rev>               the commit to test (default origin/main, after a fetch)
#   --no-fetch          do not fetch first; test a local ref as it stands
#   --start-clickhouse  when no ClickHouse server answers on 127.0.0.1:8123,
#                       install and start the pinned throwaway one
#                       (scripts/ci/clickhouse_server.sh, a ~240 MB download
#                       the first time). Without it nothing is started.
#   --only STEP,...     run only these steps; last-green is then left alone
#   --list              print the steps and exit
#   --state-dir DIR     where each run's summary, logs and coverage go
#                       (default ${XDG_STATE_HOME:-$HOME/.local/state}/boxer/nightly)
#
# The steps run in a detached worktree of the commit (commit_worktree.sh),
# through the gate() wrapper of gate_lib.sh, which is shared with release.sh:
#
#   module       go mod tidy --diff, go mod verify
#   gotest       gotest.sh --check-drift --coverprofile, plus a per-package
#                coverage table
#   binary-log   gotest_binary_log.sh --check-drift
#   lint         lint.sh over the Go steps and idl-check
#   integration  the default lane again and gotest-integration.sh, against a
#                ClickHouse server, then the drift check
#   bench        gobench.sh smoke
#   viewer       viewer_test.sh
#   watermark    watermark_test.sh
#
# What counts as a failure: a step that fails, a step whose output reports a
# skipped part (idl-check without rust/imzero2's rustfmt, say), and a step that
# could not start — the integration lane with no server answering and no
# --start-clickhouse. last-green names a commit on which everything ran and
# passed, so a run that skipped part of the work does not move it.
#
# On full green the local refs/heads/last-green is fast-forwarded to the
# tested commit — never forced, never moved backwards — and the script prints
# the command that publishes it; it pushes nothing itself. Each run writes
# <state-dir>/<UTC time>-<commit>/ holding summary.txt, the step logs and the
# coverage profile and table; <state-dir>/latest points at the newest. The
# exit status is non-zero when any selected step did not pass, so a timer unit
# shows the run red. Example units: scripts/dev/systemd/.
set -euo pipefail

here=$(dirname "$(readlink -f "${BASH_SOURCE[0]}")")
repo=$(readlink -f "$here/../..")
export GOWORK=off

all_steps=(module gotest binary-log lint integration bench viewer watermark)
lint_steps=(gofmt go-vet staticcheck errcheck gov-gate designlint packageprops fetcher-discipline idl-check)

fetch=1
start_ch=0
only=""
state_dir="${XDG_STATE_HOME:-$HOME/.local/state}/boxer/nightly"
rev=origin/main
while [ $# -gt 0 ]; do
    case "$1" in
        --no-fetch) fetch=0; shift ;;
        --start-clickhouse) start_ch=1; shift ;;
        --only) only="$2"; shift 2 ;;
        --state-dir) state_dir="$2"; shift 2 ;;
        --list) printf '%s\n' "${all_steps[@]}"; exit 0 ;;
        -h|--help) sed -n '2,45p' "$0" | sed 's/^# \{0,1\}//'; exit 0 ;;
        -*) echo "nightly: unknown option $1" >&2; exit 2 ;;
        *) rev="$1"; shift ;;
    esac
done

declare -A selected=()
if [ -n "$only" ]; then
    IFS=, read -ra want_list <<<"$only"
    for s in "${want_list[@]}"; do
        [[ " ${all_steps[*]} " == *" $s "* ]] || { echo "nightly: no such step: $s (--list names them)" >&2; exit 2; }
        selected[$s]=1
    done
else
    for s in "${all_steps[@]}"; do selected[$s]=1; done
fi
want() { [ -n "${selected[$1]:-}" ]; }

if [ "$fetch" = 1 ]; then
    git -C "$repo" fetch --quiet origin
fi
sha=$(git -C "$repo" rev-parse --verify "$rev^{commit}")
short=$(git -C "$repo" rev-parse --short "$sha")

run="$state_dir/$(date -u +%Y%m%dT%H%M%SZ)-$short"
mkdir -p "$run/logs"
run=$(readlink -f "$run")

# shellcheck source=commit_worktree.sh
source "$here/commit_worktree.sh"
# shellcheck source=gate_lib.sh
source "$here/gate_lib.sh"
gate_logdir="$run/logs"
enter_commit_worktree nightly "$sha"
trap 'gate_stop_clickhouse; _cw_remove' EXIT

subject=$(git log -1 --format='%h %s' "$sha")
echo "nightly: $subject"
echo "nightly: steps ${only:-all}; results in $run"
t_start=$SECONDS

if want module; then
    gate "go mod tidy --diff" go mod tidy --diff
    gate "go mod verify" go mod verify
fi
if want gotest; then
    gate "gotest" scripts/ci/gotest.sh --check-drift --coverprofile "$run/cover.out"
    if [ -s "$run/cover.out" ]; then
        scripts/ci/cover_table.sh "$run/cover.out" >"$run/cover-table.md"
        echo "nightly: coverage $(tail -n 1 "$run/cover-table.md")"
    fi
fi
if want binary-log; then
    gate "gotest binary_log" scripts/ci/gotest_binary_log.sh --check-drift
fi
if want lint; then
    gate "lint (Go steps, idl-check)" scripts/ci/lint.sh "${lint_steps[@]}"
fi
if want integration; then
    if ensure_clickhouse "$start_ch" "$(dirname "$wt")/clickhouse"; then
        gate "gotest against ClickHouse" scripts/ci/gotest.sh --check-drift
        gate "integration lane" scripts/ci/gotest-integration.sh
        gate "drift after integration" scripts/ci/check_drift.sh
    else
        not_run "integration lane" "no ClickHouse server answers on 127.0.0.1:8123; pass --start-clickhouse to start the pinned one"
    fi
    gate_stop_clickhouse
fi
if want bench; then
    gate "bench smoke" env BOXER_BENCH_OUT="$run/bench" scripts/ci/gobench.sh smoke
fi
if want viewer; then
    gate "viewer" scripts/ci/viewer_test.sh
fi
if want watermark; then
    gate "watermark" scripts/ci/watermark_test.sh
fi

# ---- last-green ------------------------------------------------------------------
verdict=passed
[ ${#gate_failed[@]} -eq 0 ] || verdict="FAILED: ${gate_failed[*]}"
ref_note="last-green: not moved (a subset ran)"
if [ ${#gate_failed[@]} -gt 0 ]; then
    ref_note="last-green: not moved (the run did not pass)"
elif [ -z "$only" ]; then
    cur=$(git -C "$repo" rev-parse -q --verify refs/heads/last-green || true)
    if [ -z "$cur" ]; then
        git -C "$repo" update-ref refs/heads/last-green "$sha" "$(git -C "$repo" hash-object --stdin </dev/null | tr '0-9a-f' '0')"
        ref_note="last-green: created at $short"
    elif [ "$cur" = "$sha" ]; then
        ref_note="last-green: already at $short"
    elif git -C "$repo" merge-base --is-ancestor "$cur" "$sha"; then
        git -C "$repo" update-ref refs/heads/last-green "$sha" "$cur"
        ref_note="last-green: fast-forwarded $(git -C "$repo" rev-parse --short "$cur") -> $short"
    else
        ref_note="last-green: NOT moved — $short does not descend from $(git -C "$repo" rev-parse --short "$cur"), and the ref only moves forward"
    fi
fi

{
    echo "nightly run $(date -u +%Y-%m-%dT%H:%M:%SZ)"
    echo "commit:  $sha"
    echo "subject: ${subject#* }"
    echo "steps:   ${only:-all}"
    echo "wall:    $((SECONDS - t_start))s"
    echo
    printf '%s\n' "${gate_results[@]}"
    echo
    echo "verdict: $verdict"
    echo "$ref_note"
} >"$run/summary.txt"
ln -sfn "$run" "$state_dir/latest"

echo
cat "$run/summary.txt"
if [ ${#gate_failed[@]} -gt 0 ]; then
    exit 1
fi
if [ -z "$only" ]; then
    echo
    echo "to publish it:  git push origin last-green"
fi
