#!/bin/bash
# The weekly fuzz run (ADR-0390): every Go fuzz target of one commit, for a
# bounded time each, on this machine.
#
#   scripts/ci/fuzz_weekly.sh [options] [<rev>]
#
#   <rev>             the commit to fuzz (default origin/main, after a fetch)
#   --no-fetch        do not fetch first; fuzz a local ref as it stands
#   --fuzztime D      per-target -fuzztime, a duration or Nx count (default 30s)
#   --match RE        only targets whose name matches (BOXER_FUZZ_MATCH)
#   --state-dir DIR   where each run's logs and failing inputs go (default
#                     ${XDG_STATE_HOME:-$HOME/.local/state}/boxer/nightly,
#                     beside the nightly runs)
#
# gofuzz.sh runs in a detached worktree of the commit (commit_worktree.sh), so
# a failing input it writes under <pkg>/testdata/fuzz/ lands there, not in a
# working tree. Before the worktree is removed, every such input is copied to
# <state-dir>/<UTC time>-<commit>-fuzz/failing-inputs/ at its repository
# path, ready to be committed with the fix as a regression seed; the per-target
# logs sit beside it. The default budget is 30s because FuzzAstRoundTrip is
# OOM-killed on budgets of minutes (see the note on that target). Exit status
# is non-zero when a target failed.
set -euo pipefail

here=$(dirname "$(readlink -f "${BASH_SOURCE[0]}")")
repo=$(readlink -f "$here/../..")
export GOWORK=off

fetch=1
fuzztime=30s
match=.
state_dir="${XDG_STATE_HOME:-$HOME/.local/state}/boxer/nightly"
rev=origin/main
while [ $# -gt 0 ]; do
    case "$1" in
        --no-fetch) fetch=0; shift ;;
        --fuzztime) fuzztime="$2"; shift 2 ;;
        --match) match="$2"; shift 2 ;;
        --state-dir) state_dir="$2"; shift 2 ;;
        -h|--help) sed -n '2,23p' "$0" | sed 's/^# \{0,1\}//'; exit 0 ;;
        -*) echo "fuzz_weekly: unknown option $1" >&2; exit 2 ;;
        *) rev="$1"; shift ;;
    esac
done

if [ "$fetch" = 1 ]; then
    git -C "$repo" fetch --quiet origin
fi
sha=$(git -C "$repo" rev-parse --verify "$rev^{commit}")
run="$state_dir/$(date -u +%Y%m%dT%H%M%SZ)-$(git -C "$repo" rev-parse --short "$sha")-fuzz"
mkdir -p "$run"
run=$(readlink -f "$run")

# shellcheck source=commit_worktree.sh
source "$here/commit_worktree.sh"
enter_commit_worktree fuzz "$sha"

echo "fuzz_weekly: $(git log -1 --format='%h %s' "$sha"); $fuzztime per target; results in $run"
rc=0
BOXER_FUZZ_TIME="$fuzztime" BOXER_FUZZ_MATCH="$match" BOXER_FUZZ_LOG_DIR="$run/logs" \
    scripts/ci/gofuzz.sh 2>&1 | tee "$run/gofuzz.log" || rc=1

mapfile -t inputs < <(git ls-files --others --exclude-standard -- '*/testdata/fuzz/*')
if [ ${#inputs[@]} -gt 0 ]; then
    for f in "${inputs[@]}"; do
        mkdir -p "$run/failing-inputs/$(dirname "$f")"
        cp -- "$f" "$run/failing-inputs/$f"
    done
    echo "fuzz_weekly: ${#inputs[@]} failing input(s) copied to $run/failing-inputs"
fi
ln -sfn "$run" "$state_dir/latest-fuzz"
exit "$rc"
