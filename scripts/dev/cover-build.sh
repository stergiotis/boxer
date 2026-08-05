#!/bin/bash
# ADR-0169 M0 cover lane: build instrumented and baseline binaries, report
# size and build-time deltas, and sanity-check exit-time counter emission.
# Runtime counter snapshots (WriteCounters) require -covermode=atomic — set
# and count modes refuse them — so atomic is the lane's covermode.
# Atomic per-block instrumentation costs 1.5-7.5x on tight hot loops and ~0
# on uninstrumented packages (ADR-0169 M0 Update); narrow the instrumented
# set via BOXER_COVERLANE_COVERPKG (a -coverpkg pattern list) when watching
# specific subsystems in long interactive sessions.
set -eu
here=$(dirname "$(readlink -f "$BASH_SOURCE")")
cd "$here/../.."
tags="$(cat ./tags)"
out="${BOXER_COVERLANE_DIR:-${TMPDIR:-/tmp}/boxer-coverlane}"
mkdir -p "$out"

cover_flags=(-cover -covermode=atomic)
if [ -n "${BOXER_COVERLANE_COVERPKG:-}" ]; then
  cover_flags+=("-coverpkg=${BOXER_COVERLANE_COVERPKG}")
fi

build() {
  local name="$1" pkg="$2"
  shift 2
  local t0=$SECONDS
  go build -tags "$tags" "$@" -o "$out/$name" "$pkg"
  local dt=$((SECONDS - t0))
  local sz
  sz=$(stat -c %s "$out/$name")
  printf '%-14s %11d bytes %5ds  %s\n' "$name" "$sz" "$dt" "$pkg"
}

echo "cover lane -> $out"
build boxer         ./public/app
build boxer-cover   ./public/app "${cover_flags[@]}"
build imzero2       ./public/thestack/cmd/imzero2
build imzero2-cover ./public/thestack/cmd/imzero2 "${cover_flags[@]}"

# An instrumented binary must drop covmeta + covcounters files at exit; this
# also yields the real blob sizes (the periodic payload magnitude, pre-decode).
covdir="$out/covdir"
rm -rf "$covdir"
mkdir -p "$covdir"
GOCOVERDIR="$covdir" "$out/boxer-cover" --help >/dev/null
ls -l "$covdir"
