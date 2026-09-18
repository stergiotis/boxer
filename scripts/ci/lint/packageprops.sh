#!/bin/bash
# The two ADR-0080 package-props reconcilers: the committed table against the
# declarations, and the declarations against a freshly computed static verdict.
#
# One lint step. Runnable on its own; scripts/ci/lint.sh runs it with the
# others and documents the exit-status contract every step obeys — here a
# non-zero exit fails the gate.

set -e
set -o pipefail
here=$(dirname "$(readlink -f "$BASH_SOURCE")")
cd "$here/../../.."

# Two ADR-0080 gates, one step. Both are boxer-specific (they are about this
# repo's own generated table and declarations), so they stay out of the
# `gov gate` composite, which publishes to consuming repositories.
#
#   props drift  — the committed proptable.out.go agrees with the git-tracked
#                  package_props.go declarations. Needs no survey and no
#                  TinyGo: it parses declarations and compares. A missing row
#                  makes every query over keelson's go_package_props table
#                  return a short answer with no error and no null, which is
#                  the failure mode a reader cannot see.
#   props verify — declarations agree with the freshly computed verdict.
#                  Static mode only proves red, which is sound (ADR-0078), so
#                  this fails on regressions and stays quiet about what it
#                  cannot judge. It is ~10s.
#
# `--mode static` is load-bearing, not decoration: the empirical mode shells
# out to `tinygo build` once per candidate package (minutes), and its verdict
# depends on whether this runner carries a TinyGo whose Go ceiling clears the
# repo's toolchain. The flag is not defended by this comment — `props verify`
# has no mode default and refuses to run without one, so dropping it here fails
# in a second rather than turning the lint step into a TinyGo sweep.
if out=$(./boxer.sh code analysis golang wasmsurvey props drift 2>&1) &&
   out2=$(./boxer.sh code analysis golang wasmsurvey props verify --mode static 2>&1); then
    printf '%s\n%s\n' "$out" "$out2"
    exit 0
fi
printf '%s\n%s\n' "$out" "${out2:-}"
exit 1
