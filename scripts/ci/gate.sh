#!/bin/bash
# The local gate (ADR-0390): tests a commit, not the working tree.
#
#   scripts/ci/gate.sh [--base REF] [--race-cap N] [<rev>]
#
# The working tree of this repository is often shared by more than one session
# and is rarely clean, so a test run in it tests whatever everyone has left
# there. This script checks the commit out into a detached worktree of its own
# and runs, in it:
#
#   go mod tidy --diff, go mod verify, go build ./...
#   gofmt over the Go files the commit range changed
#   go vet and go test -race -short over the packages the range changed plus
#     their direct importers (test imports and external _test packages count)
#   the drift check: the tests left the tree clean
#   when the range touches the egui2 IDL, its generated bindings or
#     rust/imzero2: boxer-idl-check.sh --force, rust_imzero2_check.sh and the
#     browser host digest against browserhost.sum
#
# The range runs from the merge-base of <rev> (default HEAD) with --base
# (default origin/main) to <rev>. A change to go.mod, go.sum or ./tags selects
# every package. Above --race-cap packages (default 150) the tests run without
# -race, and the output says so.
#
# The worktree sits at one path per repository and user under $TMPDIR, so the
# shared Go build cache, whose keys include the package directory, carries over
# from one run to the next; a lock serialises concurrent runs. The script
# removes the worktree on every exit, and removes nothing else: a leftover at
# that path is removed only when it carries the marker this script writes.
#
# A step that cannot run — a missing Rust toolchain on a range that needs it —
# fails the gate. Exit status 0 means every selected step ran and passed.
set -euo pipefail

here=$(dirname "$(readlink -f "${BASH_SOURCE[0]}")")
repo=$(readlink -f "$here/../..")
export GOWORK=off

base_ref=origin/main
race_cap=150
rev=HEAD
while [ $# -gt 0 ]; do
    case "$1" in
        --base) base_ref="$2"; shift 2 ;;
        --race-cap) race_cap="$2"; shift 2 ;;
        -h|--help) sed -n '2,31p' "$0" | sed 's/^# \{0,1\}//'; exit 0 ;;
        -*) echo "gate: unknown option $1" >&2; exit 2 ;;
        *) rev="$1"; shift ;;
    esac
done

sha=$(git -C "$repo" rev-parse --verify "$rev^{commit}")
git -C "$repo" rev-parse --verify --quiet "$base_ref^{commit}" >/dev/null ||
    { echo "gate: base $base_ref does not resolve; pass --base REF" >&2; exit 2; }
base=$(git -C "$repo" merge-base "$sha" "$base_ref")

# shellcheck source=commit_worktree.sh
source "$here/commit_worktree.sh"
enter_commit_worktree gate "$sha"
tags="$(tr -d '\n' <tags)"

echo "gate: $(git log -1 --format='%h %s' "$sha")"
echo "gate: range $(git rev-parse --short "$base")..$(git rev-parse --short "$sha") (merge-base with $base_ref)"

failed=()
declare -a names durs statuses

# step LABEL CMD...: run CMD in the worktree, print its output, record the verdict.
step() {
    local label="$1" t0 code dur
    shift
    echo
    echo "=== $label ==="
    t0=$EPOCHREALTIME
    if "$@"; then code=0; else code=$?; fi
    dur=$(awk -v s="$t0" -v e="$EPOCHREALTIME" 'BEGIN{printf "%.1f", e-s}')
    names+=("$label"); durs+=("$dur")
    if [ "$code" -eq 0 ]; then
        statuses+=(pass)
    else
        statuses+=(FAIL); failed+=("$label")
    fi
}

# not_run LABEL REASON: a selected step that could not run counts as a failure.
not_run() {
    echo
    echo "=== $1 ==="
    echo "not run: $2"
    names+=("$1"); durs+=("0.0"); statuses+=(FAIL); failed+=("$1")
}

mapfile -t changed < <(git diff --name-only "$base" "$sha")
echo "gate: ${#changed[@]} files changed in the range"

# ---- module ----------------------------------------------------------------
step "go mod tidy --diff" go mod tidy --diff
step "go mod verify" go mod verify
build_ok=1
step "go build" go build -tags "$tags" ./...
[ "${statuses[-1]}" = pass ] || build_ok=0

# ---- gofmt over the changed Go files ----------------------------------------
gofmt_changed() {
    local f out=""
    for f in "${changed[@]}"; do
        [[ "$f" == *.go ]] && [ -f "$f" ] || continue
        head -n 5 "$f" | grep -qE '^// Code generated .* DO NOT EDIT\.$' && continue
        [ -z "$(gofmt -l "$f")" ] || out+="$f"$'\n'
    done
    if [ -n "$out" ]; then
        echo "not gofmt-clean (see scripts/ci/lint/gofmt.sh before running gofmt -w):"
        printf '%s' "$out"
        return 1
    fi
    echo "passed"
}
step "gofmt (changed files)" gofmt_changed

# ---- packages: changed plus direct importers --------------------------------
pkgs=()
select_packages() {
    local listing f d ip all=0
    declare -A dir2pkg=() hit=()
    while read -r d ip; do dir2pkg["$d"]="$ip"; done < <(go list -tags "$tags" -e -f '{{.Dir}} {{.ImportPath}}' ./...)
    for f in "${changed[@]}"; do
        case "$f" in go.mod|go.sum|tags) all=1 ;; esac
        d=$(dirname "$f")
        while :; do
            if [ -n "${dir2pkg[$wt/$d]:-}" ]; then hit["${dir2pkg[$wt/$d]}"]=1; break; fi
            [ "$d" = . ] && break
            d=$(dirname "$d")
        done
    done
    if [ "$all" = 1 ]; then
        echo "go.mod, go.sum or tags changed: every package is selected"
        mapfile -t pkgs < <(printf '%s\n' "${dir2pkg[@]}" | LC_ALL=C sort)
        return
    fi
    [ ${#hit[@]} -gt 0 ] || return 0
    listing=$(go list -tags "$tags" -e \
        -f '{{.ImportPath}} {{join .Imports " "}} {{join .TestImports " "}} {{join .XTestImports " "}}' ./...)
    mapfile -t pkgs < <(awk -v set="$(printf '%s\n' "${!hit[@]}")" '
        BEGIN { n = split(set, a, "\n"); for (i = 1; i <= n; i++) if (a[i] != "") s[a[i]] = 1 }
        { for (i = 1; i <= NF; i++) if ($i in s) { print $1; next } }
    ' <<<"$listing" | LC_ALL=C sort -u)
    echo "${#hit[@]} changed packages, ${#pkgs[@]} with their direct importers"
}
select_packages

vet_pkgs() {
    local out
    out=$(go vet -tags "$tags" "${pkgs[@]}" 2>&1 |
        grep -v '\.out\.go:' | grep -v '\.gen\.go:' | grep -v '^warning: ' || true)
    if [ -n "$out" ]; then
        printf '%s\n' "$out"
        return 1
    fi
    echo "passed"
}

test_pkgs() {
    local race=(-race)
    if [ ${#pkgs[@]} -gt "$race_cap" ]; then
        race=()
        echo "NOTE: ${#pkgs[@]} packages exceed --race-cap $race_cap; testing WITHOUT -race"
    fi
    go test "${race[@]}" -json -short -tags "$tags" "${pkgs[@]}" |
        go tool tparse -progress -trimpath auto -slow 10
}

if [ ${#pkgs[@]} -eq 0 ]; then
    echo
    echo "gate: no Go package changed in the range; vet and test have nothing to select"
elif [ "$build_ok" = 0 ]; then
    not_run "go vet" "the build failed"
    not_run "go test" "the build failed"
else
    step "go vet (${#pkgs[@]} packages)" vet_pkgs
    step "go test (${#pkgs[@]} packages)" test_pkgs
fi

# ---- drift --------------------------------------------------------------------
step "drift" scripts/ci/check_drift.sh

# ---- egui2 IDL, bindings and the Rust host -----------------------------------
touches_idl=0
for f in "${changed[@]}"; do
    case "$f" in
        public/thestack/imzero2/egui2/definition/*|public/thestack/imzero2/egui2/bindings/*|rust/imzero2/*) touches_idl=1 ;;
    esac
done

# noskip CMD...: the Rust CI scripts exit 0 with a "skipped" line when their
# toolchain is missing; for a range that needs them that is not a pass.
noskip() {
    local out code=0
    out=$("$@" 2>&1) || code=$?
    printf '%s\n' "$out"
    if [ "$code" -eq 0 ] && grep -qi 'skipped' <<<"$out"; then
        echo "a skip counts as a failure here"
        return 1
    fi
    return "$code"
}

host_digest() {
    local sum=public/thestack/imzero2/browserhost/tabhost/browserhost.sum got want
    got=$(go run -tags "$tags" ./public/thestack/cmd/imzero2tab hostdigest 2>/dev/null | grep -E '^(sha256|idl) ') || {
        echo "hostdigest did not run (it needs cargo and the wasm32-unknown-unknown target)"
        return 1
    }
    want=$(grep -E '^(sha256|idl) ' "$sum")
    echo "built:    $(tr '\n' ' ' <<<"$got")"
    echo "recorded: $(tr '\n' ' ' <<<"$want")"
    if [ "$got" != "$want" ]; then
        echo "browserhost.sum is stale: run go run ./public/thestack/cmd/imzero2tab hostdigest --write and commit it"
        return 1
    fi
}

if [ "$touches_idl" = 1 ]; then
    step "boxer-idl-check" noskip scripts/dev/boxer-idl-check.sh --force
    step "rust_imzero2_check" noskip scripts/ci/rust_imzero2_check.sh
    step "browser host digest" host_digest
fi

# ---- summary -------------------------------------------------------------------
echo
echo "=== gate summary: $(git rev-parse --short "$sha") ==="
for i in "${!names[@]}"; do
    printf '%-28s %-4s %7ss\n' "${names[i]}" "${statuses[i]}" "${durs[i]}"
done
if [ ${#failed[@]} -gt 0 ]; then
    echo "gate: FAILED: ${failed[*]}"
    exit 1
fi
echo "gate: passed"
