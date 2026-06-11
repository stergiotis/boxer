#!/usr/bin/env bash
# Per-package coverage floor for the pushout tree. Coverage is computed
# from one aggregated profile with -coverpkg over the whole tree, so
# cross-package tests (e.g. the qc checker exercised from store tests)
# are credited to the package that owns the statements. Blocks appear
# once per test binary in the merged profile; they are deduplicated by
# location and a block counts as covered if ANY binary hit it.
set -euo pipefail
cd "$(dirname "$0")/../.."

tree='./public/algebraicarch/pushout/...'
floor="${PUSHOUT_COVER_FLOOR:-60}"
profile="$(mktemp)"
trap 'rm -f "$profile"' EXIT

go test -tags="$(cat tags)" -count=1 -coverprofile="$profile" -coverpkg="$tree" "$tree" >/dev/null

awk -v floor="$floor" '
NR > 1 {
	# Line shape: <file>:<start>,<end> <numStmts> <hitCount>
	stmts[$1] = $2
	if ($3 > 0) hit[$1] = 1
}
END {
	fail = 0
	for (loc in stmts) {
		pkg = loc
		sub(/:[^:]*$/, "", pkg)          # drop :start,end -> file path
		sub(/\/[^\/]*$/, "", pkg)        # drop file name -> package
		total[pkg] += stmts[loc]
		if (loc in hit) covered[pkg] += stmts[loc]
	}
	for (pkg in total) {
		pct = 100 * covered[pkg] / total[pkg]
		printf "%6.1f%%  %s\n", pct, pkg
		if (pct < floor) fail = 1
	}
	if (fail) {
		printf "FAIL: package(s) below %d%% statement coverage\n", floor
		exit 1
	}
}' "$profile" | sort -k2
