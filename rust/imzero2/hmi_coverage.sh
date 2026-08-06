#!/bin/bash
# Launch the desktop GUI in the ADR-0169 continuous-coverage lane: build
# main_go with -cover -covermode=atomic (runtime counter snapshots are
# atomic-only) and hand off to hmi.sh with HMI_BUILD=0, so hmi.sh does not
# rebuild uninstrumented over the instrumented binary. ./build_go.sh
# restores the plain binary afterwards.
#
# Usage:
#   ./hmi_coverage.sh                     # carousel, whole module sampled
#   ./hmi_coverage.sh --launch play       # one app
#   BOXER_COVERLANE_COVERPKG='./apps/play/...,./public/keelson/...' \
#     ./hmi_coverage.sh                   # narrow the instrumented set —
#                                         # uninstrumented packages run at
#                                         # baseline speed
#
# Knobs at runtime: IMZERO2_COVERAGE_INTERVAL (default 5s; 0 disables).
# Expect ~3x Go frame time under full-tree instrumentation (measured in
# ADR-0169's fps gate) — a diagnosis lane, not a daily driver. See
# doc/howto/continuous-coverage.md.
set -ev
here=$(dirname "$(readlink -f "$BASH_SOURCE")")
cd "$here"
build_tags="$(cat ../../tags | tr -d $'\n'),binary_log"
cover_flags=(-cover -covermode=atomic)
if [ -n "${BOXER_COVERLANE_COVERPKG:-}" ]; then
	cover_flags+=("-coverpkg=${BOXER_COVERLANE_COVERPKG}")
fi
rm -f main_go
export CGO_ENABLED=0
go build -tags "$build_tags" "${cover_flags[@]}" -o main_go ../../public/thestack/cmd/imzero2/
HMI_BUILD=0 exec ./hmi.sh "$@"
