#!/bin/sh
# expect_violation.sh — run a checker and succeed only if it reports a violation.
#
#   sh ./expect_violation.sh [--exit=N] <pattern> <command> [args...]
#
# The counterexample specs (*_unsafe*.qnt, erasure_dilemma.qnt, the
# `ErasureComplete` horn, convergence_nofair.cfg, and the caching directory's
# counterfactuals) are correct precisely when the checker FINDS a violation,
# so their tools' non-zero exit is the expected outcome. This wrapper inverts
# that: exit 0 iff <pattern> appears in the checker's output (a fixed string,
# not a regex), and, with --exit=N, the checker also exited with status N.
#
# quint run prints "[violation]". For TLC pass --exit=13, its liveness
# violation status (tlc2.output.EC.ExitStatus.VIOLATION_LIVENESS), and a
# pattern for the counterexample's shape ("Stuttering", "Back to state"):
# TLC's summary line is not stable across releases (older builds print
# "Temporal properties were violated.", newer ones "Temporal property <name>
# was violated."), and the status keeps a parse error or a safety violation
# from passing as the expected counterexample.
set -u
want_exit=
case "${1:-}" in
  --exit=*) want_exit="${1#--exit=}"; shift ;;
esac
pattern="$1"; shift
out="$("$@" 2>&1)"
rc=$?
printf '%s\n' "$out"
if [ -n "$want_exit" ] && [ "$rc" -ne "$want_exit" ]; then
  echo "expect_violation: FAILED — exit status $rc, expected $want_exit, from: $*" >&2
  exit 1
fi
if printf '%s\n' "$out" | grep -qF -- "$pattern"; then
  echo "expect_violation: OK — counterexample reported by: $*"
  exit 0
fi
echo "expect_violation: FAILED — no '$pattern' in output of: $*" >&2
exit 1
