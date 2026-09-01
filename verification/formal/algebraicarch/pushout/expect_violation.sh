#!/bin/sh
# expect_violation.sh — run a checker and succeed only if it reports a violation.
#
#   sh ./expect_violation.sh <pattern> <command> [args...]
#
# The counterexample specs (*_unsafe*.qnt, erasure_dilemma.qnt, the
# `ErasureComplete` horn, convergence_nofair.cfg) are correct precisely when
# the checker FINDS a violation, so their tools' non-zero exit is the expected
# outcome. This wrapper inverts that: exit 0 iff <pattern> appears in the
# checker's output (quint run prints "[violation]", TLC prints "Temporal
# properties were violated"), so `npm run findings` / `npm run liveness:all`
# can assert in CI that every counterexample still exists.
set -u
pattern="$1"; shift
out="$("$@" 2>&1)"
printf '%s\n' "$out"
if printf '%s\n' "$out" | grep -qF -- "$pattern"; then
  echo "expect_violation: OK — counterexample reported by: $*"
  exit 0
fi
echo "expect_violation: FAILED — no '$pattern' in output of: $*" >&2
exit 1
