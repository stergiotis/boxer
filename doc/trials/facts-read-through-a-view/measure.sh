#!/bin/bash
# measure.sh — one run of the facts-read-through-a-view trial (README §4).
#
# Writes runs/<slug>/: environment.md, tables.tsv, cost.tsv and plans.txt.
# Everything runs against one ClickHouse server, in two databases the harness
# drops and creates (frv_facts, frv_carrier). Never point it at a server whose
# load matters: a throwaway one from scripts/ci/clickhouse_server.sh is what
# the logbook's runs used.
#
# Usage:
#   CLICKHOUSE_URL=http://127.0.0.1:18123/ doc/trials/facts-read-through-a-view/measure.sh <run-slug>
#
# Environment:
#   CLICKHOUSE_URL   the server; required, so that a run never lands on a
#                    default server by omission
#   FRV_ROWS         2000000   log rows written to the facts table
#   FRV_REPS         5         repetitions of each read on each arm
#   FRV_KEEP         unset     set to keep the two databases after the run
#   GOTMPDIR/TMPDIR  where go builds, when /tmp is small
set -euo pipefail

here=$(cd "$(dirname "$0")" && pwd)
root=$(cd "$here/../../.." && pwd)
slug=${1:?usage: measure.sh <run-slug>}
ch=${CLICKHOUSE_URL:?set CLICKHOUSE_URL to the server the run may load}
out="$here/runs/$slug"
mkdir -p "$out"

q() { curl -sS "$ch" --data-binary "$1"; }

{
	printf -- '---\ntype: reference\naudience: package maintainer\nstatus: draft\n---\n\n> **Status: draft — pre-human-review.** Not verified; do not cite as authoritative.\n\n'
	echo "# Environment — $slug"
	echo
	echo "- boxer commit: $(git -C "$root" rev-parse --short HEAD)$(git -C "$root" diff --quiet -- public/keelson/runtime/factsstore public/semistructured/leeway/factsschema || echo " (the facts store or schema modified in the working tree)")"
	echo "- ClickHouse: $(q 'SELECT version()') — $(q "SELECT value FROM system.settings WHERE name = 'max_threads'" | tr -d '\n') max_threads"
	echo "- Rows written to the facts table: ${FRV_ROWS:-2000000}; repetitions: ${FRV_REPS:-5}"
	echo "- Go: $(go version | cut -d' ' -f3-)"
	echo "- CPU: $(lscpu | sed -n 's/^Model name: *//p'), $(nproc) threads"
	echo "- Memory: $(free -h | awk '/^Mem:/ {print $2 " total, " $7 " available"}')"
	echo "- Kernel: $(uname -sr)"
	echo "- Load average before the run: $(cut -d' ' -f1-3 /proc/loadavg)"
} >"$out/environment.md"

cd "$root"
FRV_RUN_DIR="$out" go test -count=1 -timeout 60m -tags="$(cat ./tags) integration" \
	-run 'TestLoad|TestTables|TestReadCost' ./doc/trials/facts-read-through-a-view/harness/

echo "- Load average after the run: $(cut -d' ' -f1-3 /proc/loadavg)" >>"$out/environment.md"
if [ -z "${FRV_KEEP:-}" ]; then
	q 'DROP DATABASE IF EXISTS frv_facts SYNC'
	q 'DROP DATABASE IF EXISTS frv_carrier SYNC'
fi
