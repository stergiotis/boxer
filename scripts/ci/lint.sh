#!/bin/bash
set -e
set -o pipefail
here=$(dirname "$(readlink -f "$BASH_SOURCE")")
cd "$here/../.."
tags="$(cat "$here/../../tags" | tr -d "\n")"

rc=0

echo "=== go vet ==="
# go vet has no built-in exclude for generated files, so filter output.
if go vet -tags "$tags" ./public/... 2>&1 | grep -v '\.out\.go:' | grep -v '\.gen\.go:' | grep -q .; then
    go vet -tags "$tags" ./public/... 2>&1 | grep -v '\.out\.go:' | grep -v '\.gen\.go:'
    rc=1
else
    echo "passed"
fi

echo ""
echo "=== staticcheck ==="
# Exclude generated ANTLR parser files.
go tool honnef.co/go/tools/cmd/staticcheck -tags "$tags" \
    -checks "all,-ST1000,-ST1003,-ST1005,-ST1016,-ST1020,-ST1021,-ST1022,-S1023,-SA4011,-SA1019" \
    ./public/... 2>&1 | grep -v '\.out\.go:' | grep -v '\.gen\.go:' || true

echo ""
echo "=== errcheck ==="
go tool github.com/kisielk/errcheck -tags "$tags" \
    -exclude <(printf '%s\n' \
        'fmt.Fprintf' 'fmt.Fprintln' 'fmt.Fprint' \
        '(*strings.Builder).WriteString' '(*strings.Builder).WriteByte' '(*strings.Builder).WriteRune' \
        '(*bytes.Buffer).WriteString' '(*bytes.Buffer).WriteByte' '(*bytes.Buffer).Write') \
    ./public/... 2>&1 | grep -v '\.out\.go:' | grep -v '\.gen\.go:' || true

echo ""
echo "=== nilaway ==="
go tool go.uber.org/nilaway/cmd/nilaway -tags "$tags" ./public/... 2>&1 || true

echo ""
echo "=== doclint ==="
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
    else
        echo "passed"
    fi
else
    echo "$out"
    rc=1
fi

echo ""
echo "=== llmtag ==="
# Surfaces Go files whose git blame attributes a majority of lines to
# commits carrying an LLM Co-Authored-By trailer but which lack the
# corresponding //go:build llm_generated_<model> directive. A non-zero
# exit sets rc=1 so the gate fails until the tags are applied (or the
# attribution is corrected). Same `if out=$(...)` pattern as doclint
# above — required because the script runs under `set -e`.
if out=$("$here/../../boxer.sh" gov llmtag --diff --root . --repo . 2>/dev/null); then
    if [ -n "$out" ]; then
        echo "$out"
    else
        echo "passed"
    fi
else
    echo "$out"
    rc=1
fi

echo ""
echo "=== h3_wasm_parity ==="
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
else
    echo "$out"
    rc=1
fi

echo ""
echo "=== lint complete ==="
exit $rc
