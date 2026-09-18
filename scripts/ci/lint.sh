#!/bin/bash
# lint.sh — runs the repository's lint steps and prints a pass/warn/fail summary.
#
# Each step is a script of its own: the ones under scripts/ci/lint/ were
# extracted from this file, the rest already lived beside it and are called
# where the inline block used to be. Any of them runs on its own, prints its own
# findings, and needs no argument — this file contributes the order, the banner,
# the timing and the trailer, nothing a step depends on.
#
# STEP CONTRACT — a step reports its verdict through its exit status:
#
#   0             pass. A step that cannot run on this machine (no cargo, no
#                 Pillow) says so and exits 0: a missing toolchain is not a
#                 finding about the repository, and CI is the enforcer.
#   non-zero      fail. rc=1, and the step is named in the trailer.
#
# A step whose findings do not block says so with exit status 2 AND carries the
# `warn` marker in the list below. Both are required because a status is not
# only ever the step's own: under `set -e` whatever tool died last supplies it,
# and `tar` exits 2 on a write error. Without the marker, a step that ran out of
# disk would report its own findings as non-blocking. The marker is also where
# the policy belongs — whether a check blocks is the gate's decision, not the
# checker's — so which steps may report findings without failing is visible in
# one place.
#
# Usage:
#   scripts/ci/lint.sh                   every step below, in order
#   scripts/ci/lint.sh gofmt staticcheck only those, in the order given
#   scripts/ci/lint.sh --list            the steps and the scripts that run them

set -e
set -o pipefail
here=$(dirname "$(readlink -f "$BASH_SOURCE")")
cd "$here/../.."

# Execution order, and the whole of it: name, the script that runs it (relative
# to scripts/ci), and `warn` where exit status 2 means non-blocking findings. A
# step's name is its script's basename, the selector on the command line and
# what the summary prints — one identity, so a step cannot be renamed in one
# place and stay the old name in the others.
#
# nilaway is deliberately absent rather than commented out: scripts/ci/lint/
# nilaway.sh runs, but its findings have never been triaged. Add a line here
# (warn-capable) to put it back in the gate.
steps=(
    "go-vet             lint/go-vet.sh"
    "staticcheck        lint/staticcheck.sh     warn"
    "errcheck           lint/errcheck.sh        warn"
    "gov-gate           lint/gov-gate.sh        warn"
    "designlint         lint/designlint.sh"
    "packageprops       lint/packageprops.sh"
    "h3_wasm_parity     h3_wasm_parity.sh"
    "rust_imzero2_check rust_imzero2_check.sh"
    "repro_build_parity repro_build_parity.sh"
    "gofmt              lint/gofmt.sh"
    "rustfmt            lint/rustfmt.sh"
    "ids-fonts          lint/ids-fonts.sh"
    "fetcher-discipline fetcher-discipline.sh"
    "egui-persistence   lint/egui-persistence.sh"
    "glyph-coverage     lint/glyph-coverage.sh"
    "capslock           lint/capslock.sh"
)

declare -a all_names all_paths all_warn
for entry in "${steps[@]}"; do
    read -r _name _path _flag <<<"$entry"
    all_names+=("$_name")
    all_paths+=("$_path")
    all_warn+=("${_flag:-}")
done

usage() {
    sed -n '2,/^$/p' "$0" | sed 's/^# \?//'
}

case "${1:-}" in
    --list|-l)
        # Name and script, so a reader can run one directly without opening
        # this file.
        for i in "${!all_names[@]}"; do
            printf '%-18s scripts/ci/%s\n' "${all_names[i]}" "${all_paths[i]}"
        done
        exit 0
        ;;
    --help|-h)
        usage
        exit 0
        ;;
    -*)
        echo "lint.sh: unknown option $1" >&2
        echo "usage: lint.sh [--list] [step ...]" >&2
        exit 2
        ;;
esac

# With no argument every step runs; with arguments, only the named ones, in the
# order they were given — a subset is for working on one finding, and the caller
# knows better than this list which order that wants.
declare -a run_names run_paths run_warn
if [ $# -eq 0 ]; then
    run_names=("${all_names[@]}")
    run_paths=("${all_paths[@]}")
    run_warn=("${all_warn[@]}")
else
    for want in "$@"; do
        found=""
        for i in "${!all_names[@]}"; do
            if [ "${all_names[i]}" = "$want" ]; then
                run_names+=("${all_names[i]}")
                run_paths+=("${all_paths[i]}")
                run_warn+=("${all_warn[i]}")
                found=1
                break
            fi
        done
        if [ -z "$found" ]; then
            echo "lint.sh: no such step: $want" >&2
            echo "known steps: ${all_names[*]}" >&2
            exit 2
        fi
    done
fi

rc=0

# Per-step bookkeeping for the summary trailer. Parallel arrays indexed by
# step. status is one of: pass | fail | warn. fail means the step set rc=1
# (drove the script's non-zero exit); warn means the step produced findings
# that are non-blocking (staticcheck/errcheck/nilaway, doclint warn-only).
declare -a step_names step_durs step_statuses
overall_t0=$EPOCHREALTIME

for i in "${!run_names[@]}"; do
    name="${run_names[i]}"
    path="$here/${run_paths[i]}"
    echo ""
    echo "=== $name ==="
    t0=$EPOCHREALTIME
    if "$path"; then code=0; else code=$?; fi
    dur=$(awk -v s="$t0" -v e="$EPOCHREALTIME" 'BEGIN{printf "%.3f", e-s}')
    if [ "$code" -eq 0 ]; then
        status=pass
    elif [ "$code" -eq 2 ] && [ "${run_warn[i]}" = "warn" ]; then
        status=warn
    else
        status=fail
        rc=1
    fi
    step_names+=("$name")
    step_durs+=("$dur")
    step_statuses+=("$status")
done

# === summary trailer ===
overall_dur=$(awk -v s="$overall_t0" -v e="$EPOCHREALTIME" 'BEGIN{printf "%.2f", e-s}')

# Compute name column width for alignment.
max_w=4
for n in "${step_names[@]}"; do
    [ ${#n} -gt $max_w ] && max_w=${#n}
done

echo ""
echo "=== summary ==="
for i in "${!step_names[@]}"; do
    printf "%-*s  %-4s  %7.2fs\n" "$max_w" "${step_names[i]}" "${step_statuses[i]}" "${step_durs[i]}"
done

failed=()
warned=()
for i in "${!step_names[@]}"; do
    case "${step_statuses[i]}" in
        fail) failed+=("${step_names[i]}") ;;
        warn) warned+=("${step_names[i]}") ;;
    esac
done

trailer="total: ${overall_dur}s  exit $rc"
if [ ${#failed[@]} -gt 0 ]; then
    trailer="$trailer  failing: ${failed[*]}"
fi
if [ ${#warned[@]} -gt 0 ]; then
    trailer="$trailer  warnings: ${warned[*]}"
fi
echo ""
echo "$trailer"

exit $rc
