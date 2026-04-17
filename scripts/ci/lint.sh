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
# Enforces DOCUMENTATION_STANDARD invariants (front-matter presence + valid
# type/status, banned filenames). See standard §8 for the full rule list.
if "$here/../../boxer.sh" gov doclint --min-severity error . 2>/dev/null; then
    echo "passed"
else
    rc=1
fi

echo ""
echo "=== lint complete ==="
