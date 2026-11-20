#!/bin/bash
# Compares the exported Go API of every Stable package (ADR-0389) between a
# base ref and the working tree, and fails on an incompatible change that no
# breaking commit in the range owns.
#
#   scripts/ci/apidiff.sh [<base-ref>]
#
# <base-ref> defaults to the latest tag by version order (`git tag
# --sort=-v:refname`, not plain `git tag`, which sorts lexically). The set
# compared is every package Stable at HEAD or at the base: a package that was
# Stable at the base and is gone at HEAD is reported as removed. A package
# new since the base has nothing to break.
#
# An incompatible change is acknowledged when the range base..HEAD holds a
# commit whose subject carries `!` before the colon (CODINGSTANDARDS §
# Version Control) and which touches a file directly in that package's
# directory. Acknowledged changes are listed and do not fail; anything else
# exits 1. The listing, acknowledged or not, is input for the release notes.
#
# The promise starts at the first tag cut after the tiers exist
# (VERSIONING.md): against an older base no package was Stable there, so only
# HEAD's tiers select what is compared, and the run reports without failing.
#
# Uses golang.org/x/exp/cmd/apidiff from go.mod's tool block. The base tree is
# unpacked with `git archive` into a temporary directory, so no worktree is
# registered and the checkout is not touched.

set -euo pipefail
here=$(dirname "$(readlink -f "${BASH_SOURCE[0]}")")
cd "$here/../.."
root=$(pwd)
# shellcheck source=/dev/null
source "$root/scripts/dev/go-build-env.sh"

base=${1:-$(git tag --sort=-v:refname | head -n 1)}
if [ -z "$base" ]; then
    echo "apidiff: no base ref given and no tag found" >&2
    exit 2
fi
git rev-parse --verify --quiet "$base^{commit}" > /dev/null || {
    echo "apidiff: unknown base ref $base" >&2
    exit 2
}
mod=$(awk '$1=="module"{print $2; exit}' go.mod)

work=$(mktemp -d)
cleanup() {
    rm -rf -- "$work"
}
trap cleanup EXIT

go build -o "$work/apidiff" golang.org/x/exp/cmd/apidiff
mkdir -p "$work/base" "$work/old" "$work/new"
git archive "$base" | tar -x -C "$work/base"

# Stable at HEAD, from the tracked declarations (the harvest table's last
# column is the tier; it is blank when unset, so `stable` there is exact).
./boxer.sh code analysis golang wasmsurvey props harvest --tracked 2> /dev/null |
    awk '$NF=="stable"{print $1}' > "$work/stable.head"
# Stable at the base, from its generated table (absent before ADR-0389).
if [ -f "$work/base/public/packageprops/proptable/proptable.out.go" ]; then
    grep -o "ImportPath: \"$mod/[^\"]*\", Props: packageprops.Props{[^}]*StabilityStable" \
        "$work/base/public/packageprops/proptable/proptable.out.go" |
        sed -E "s#ImportPath: \"$mod/([^\"]*)\".*#\1#" > "$work/stable.base" || true
else
    : > "$work/stable.base"
fi
sort -u "$work/stable.head" "$work/stable.base" > "$work/stable"

base_tags=$(tr -d '\n' < "$work/base/tags" 2> /dev/null || true)
compared=0
compatible=0
acknowledged=0
unowned=0
added=0
while read -r rel; do
    [ -n "$rel" ] || continue
    key=${rel//\//_}
    if ! (cd "$work/base" && GOFLAGS="-tags=$base_tags" "$work/apidiff" -w "$work/old/$key" "$mod/$rel") 2> /dev/null; then
        added=$((added + 1))
        continue
    fi
    compared=$((compared + 1))
    if (GOFLAGS="-tags=$BOXER_GO_TAGS" "$work/apidiff" -w "$work/new/$key" "$mod/$rel") 2> /dev/null; then
        changes=$("$work/apidiff" -incompatible "$work/old/$key" "$work/new/$key")
    else
        changes="- package removed"
    fi
    if [ -z "$changes" ]; then
        compatible=$((compatible + 1))
        continue
    fi
    owners=$(git log --format='%h %s' "$base"..HEAD -- ":(glob)$rel/*" |
        grep -E '^[0-9a-f]+ [a-z]+(\([^)]*\))?!:' || true)
    if [ -n "$owners" ]; then
        acknowledged=$((acknowledged + 1))
        verdict="acknowledged"
    else
        unowned=$((unowned + 1))
        verdict="NOT acknowledged by any breaking commit"
    fi
    printf '\n## %s (%s)\n%s\n' "$rel" "$verdict" "$changes"
    if [ -n "$owners" ]; then
        printf 'breaking commits touching it:\n'
        printf '%s\n' "$owners" | sed 's/^/  /'
    fi
done < "$work/stable"

printf '\napidiff %s..HEAD: %d stable package(s) compared, %d compatible, %d incompatible and acknowledged, %d incompatible and not acknowledged; %d new since the base\n' \
    "$base" "$compared" "$compatible" "$acknowledged" "$unowned" "$added"
if [ ! -s "$work/stable.base" ]; then
    printf 'apidiff: %s declares no Stable package, so no promise was made at it; reporting only\n' "$base"
    exit 0
fi
if [ "$unowned" -gt 0 ]; then
    exit 1
fi
