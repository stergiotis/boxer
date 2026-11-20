# Sourced, not run: a detached worktree of one commit, for scripts that must
# test the commit rather than the shared working tree (gate.sh, release.sh;
# ADR-0390).
#
#   source scripts/ci/commit_worktree.sh
#   enter_commit_worktree NAME SHA     # needs $repo; sets $wt and cds into it
#
# The worktree sits at one path per NAME, repository and user under $TMPDIR:
# the Go build cache keys on the package directory, so a stable path lets one
# run reuse the previous run's compilation. A lock serialises runs of the same
# NAME. The EXIT trap removes the worktree, and only that one: a leftover at
# the path is removed only when it carries the marker this file writes, and
# nothing outside the state directory is ever deleted.

enter_commit_worktree() {
    local name="$1" sha="$2" common key state
    common=$(readlink -f "$(git -C "$repo" rev-parse --git-common-dir)")
    key=$(printf '%s' "$common" | sha256sum | cut -c1-12)
    state="${TMPDIR:-/tmp}/boxer-$name-$(id -u)-$key"
    wt="$state/tree"
    _cw_marker="$state/created-by-$name"
    _cw_name="$name"
    mkdir -p "$state"

    exec 9>"$state/lock"
    if ! flock -n 9; then
        echo "$name: another run holds $state/lock; waiting"
        flock 9
    fi

    if [ -e "$wt" ] || _cw_registered; then
        if [ -f "$_cw_marker" ]; then
            echo "$name: removing the worktree an interrupted run left at $wt"
            _cw_remove
        fi
        if [ -e "$wt" ] || _cw_registered; then
            echo "$name: $wt exists and was not created by this script; remove it, then rerun" >&2
            exit 2
        fi
    fi

    trap _cw_remove EXIT
    trap 'exit 130' INT TERM
    : >"$_cw_marker"
    git -C "$repo" worktree add --quiet --detach "$wt" "$sha"
    cd "$wt"
}

_cw_registered() {
    git -C "$repo" worktree list --porcelain | grep -qxF "worktree $wt"
}

_cw_remove() {
    cd "$repo" || return 0
    if [ -f "$_cw_marker" ]; then
        if _cw_registered; then
            git -C "$repo" worktree remove --force "$wt" || true
        elif [ -d "$wt" ]; then
            rm -rf -- "$wt"
        fi
    fi
    if _cw_registered; then
        echo "$_cw_name: could not remove the worktree at $wt; remove it with: git worktree remove --force $wt" >&2
        return 0
    fi
    rm -f -- "$_cw_marker"
}
