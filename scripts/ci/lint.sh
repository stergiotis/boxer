#!/bin/bash
set -e
set -o pipefail
here=$(dirname "$(readlink -f "$BASH_SOURCE")")
cd "$here/../.."
tags="$(cat "$here/../../tags" | tr -d "\n")"

rc=0

# Per-step bookkeeping for the summary trailer. Parallel arrays indexed by
# step. status is one of: pass | fail | warn. fail means the step set rc=1
# (drove the script's non-zero exit); warn means the step produced findings
# that are non-blocking (staticcheck/errcheck/nilaway, doclint warn-only).
declare -a step_names step_durs step_statuses
overall_t0=$EPOCHREALTIME

step_begin() {
    _step_name="$1"
    _step_t0=$EPOCHREALTIME
    echo ""
    echo "=== $_step_name ==="
}

step_end() {
    local status="$1"
    local dur
    dur=$(awk -v s="$_step_t0" -v e="$EPOCHREALTIME" 'BEGIN{printf "%.3f", e-s}')
    step_names+=("$_step_name")
    step_durs+=("$dur")
    step_statuses+=("$status")
}

step_begin "go vet"
# go vet has no built-in exclude for generated files, so filter output.
if go vet -tags "$tags" ./public/... 2>&1 | grep -v '\.out\.go:' | grep -v '\.gen\.go:' | grep -q .; then
    go vet -tags "$tags" ./public/... 2>&1 | grep -v '\.out\.go:' | grep -v '\.gen\.go:'
    rc=1
    step_end fail
else
    echo "passed"
    step_end pass
fi

step_begin "staticcheck"
# Exclude generated ANTLR parser files. Capture so we can mark warn vs pass;
# this trades streaming for status visibility (staticcheck batches anyway).
sc_out=$(go tool honnef.co/go/tools/cmd/staticcheck -tags "$tags" \
    -checks "all,-ST1000,-ST1003,-ST1005,-ST1016,-ST1020,-ST1021,-ST1022,-S1023,-SA4011,-SA1019" \
    ./public/... 2>&1 | grep -v '\.out\.go:' | grep -v '\.gen\.go:' || true)
if [ -n "$sc_out" ]; then
    printf '%s\n' "$sc_out"
    step_end warn
else
    echo "passed"
    step_end pass
fi

step_begin "errcheck"
ec_out=$(go tool github.com/kisielk/errcheck -tags "$tags" \
    -exclude <(printf '%s\n' \
        'fmt.Fprintf' 'fmt.Fprintln' 'fmt.Fprint' \
        '(*strings.Builder).WriteString' '(*strings.Builder).WriteByte' '(*strings.Builder).WriteRune' \
        '(*bytes.Buffer).WriteString' '(*bytes.Buffer).WriteByte' '(*bytes.Buffer).Write') \
    ./public/... 2>&1 | grep -v '\.out\.go:' | grep -v '\.gen\.go:' || true)
if [ -n "$ec_out" ]; then
    printf '%s\n' "$ec_out"
    step_end warn
else
    echo "passed"
    step_end pass
fi

# nilaway disabled — re-enable by uncommenting the block below.
# step_begin "nilaway"
# # nilaway's own -tags flag is deprecated/no-op; build tags must be passed via
# # GOFLAGS so the analysis driver picks them up. Without this, tag-gated
# # packages (e.g. llm_generated_*) are excluded and importers cascade into
# # hundreds of bogus "could not import / undefined" lines.
# # -include-pkgs restricts analysis to first-party code; stdlib and 3rd-party
# # returns are then assumed non-nil, which suppresses the bulk of noise from
# # os.Stdout/http.Response.Body/ANTLR-style false positives that we cannot
# # fix locally.
# na_out=$(GOFLAGS="-tags=$tags" go tool go.uber.org/nilaway/cmd/nilaway \
#     -include-pkgs=github.com/stergiotis/boxer \
#     ./public/... 2>&1 || true)
# if [ -n "$na_out" ]; then
#     printf '%s\n' "$na_out"
#     step_end warn
# else
#     echo "passed"
#     step_end pass
# fi

step_begin "codelint"
# Surfaces CS-prefixed violations of CODINGSTANDARDS.md detected via
# go/analysis-based AST passes (ADR-0011). Warn-only while the in-tree
# fallout is being cleared; promotion of individual rules to error
# severity is per-rule, in a separate commit, once residual count
# reaches zero. Same `if out=$(...)` pattern as doclint below —
# required because the script runs under `set -e`.
if out=$("$here/../../boxer.sh" gov codelint --tags "$tags" --min-severity warn ./public/... 2>/dev/null); then
    if [ -n "$out" ]; then
        echo "$out"
        step_end warn
    else
        echo "passed"
        step_end pass
    fi
else
    echo "$out"
    rc=1
    step_end fail
fi

step_begin "doclint"
# Surfaces all warn-and-above doclint findings. Only error-severity
# findings set rc=1 (warnings are visible but non-blocking, consistent
# with the staticcheck/errcheck/nilaway sections above). See doc/
# DOCUMENTATION_STANDARD.md §8 for the full invariant table.
#
# The 'if' wrapper is required because the script runs under `set -e`:
# a direct `out=$(...)` assignment would abort the script when doclint
# exits non-zero on error-severity findings.
if out=$("$here/../../boxer.sh" gov doclint --min-severity warn . 2>/dev/null); then
    if [ -n "$out" ]; then
        echo "$out"
        step_end warn
    else
        echo "passed"
        step_end pass
    fi
else
    echo "$out"
    rc=1
    step_end fail
fi

step_begin "llmtag"
# Surfaces Go files whose //go:build llm_generated_<model> directive is
# missing, stale (dominant model changed), no longer warranted (substantial
# human edits), or in conflict with another build tag. The check is
# bidirectional: it both adds tags to LLM-dominated files and flags tags on
# files now dominated by humans. Pre-cutoff trailerless commits on
# already-tagged files are attributed to the existing tag (auto-detected
# from the earliest LLM-trailered commit), so legitimately-Gemini-authored
# code is not flagged. A non-zero exit sets rc=1. Same `if out=$(...)`
# pattern as doclint above — required because the script runs under `set -e`.
if out=$("$here/../../boxer.sh" gov llmtag --diff --root . --repo . 2>/dev/null); then
    if [ -n "$out" ]; then
        echo "$out"
    else
        echo "passed"
    fi
    step_end pass
else
    echo "$out"
    rc=1
    step_end fail
fi

step_begin "h3_wasm_parity"
# Rebuilds rust/h3bridge to wasm and byte-compares against the committed
# public/science/geo/h3/internal/h3o_wasm/h3.wasm. Gracefully skipped when
# cargo or the wasm32-unknown-unknown target is absent so local lint stays
# green for contributors not touching the bridge; CI is the enforcer.
if out=$("$here/h3_wasm_parity.sh" 2>&1); then
    if [ -n "$out" ]; then
        echo "$out"
    else
        echo "passed"
    fi
    step_end pass
else
    echo "$out"
    rc=1
    step_end fail
fi

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
