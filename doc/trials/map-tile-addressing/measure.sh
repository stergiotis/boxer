#!/bin/bash
# measure.sh — one run of the map-tile-addressing trial (README §4).
#
# Writes runs/<slug>/: environment.md, and the TSVs the harness tests write
# (counts, cost, querycache, querycache-entries, first-pixels, shape,
# brightness). Everything runs against one ClickHouse server, the local one
# unless CLICKHOUSE_URL says otherwise; never point it at a shared server.
#
# Usage:
#   doc/trials/map-tile-addressing/measure.sh <run-slug>
#
# Environment:
#   CLICKHOUSE_URL   http://127.0.0.1:8123/   the server holding planes_mercator
#                    and its _sample10/_sample100 companions
#   MTA_REPS         cost 3, first-pixels 7   repetitions (one value for both)
#   GOTMPDIR/TMPDIR  where go builds, when /tmp is small
set -euo pipefail

here=$(cd "$(dirname "$0")" && pwd)
root=$(cd "$here/../../.." && pwd)
slug=${1:?usage: measure.sh <run-slug>}
out="$here/runs/$slug"
mkdir -p "$out"
ch=${CLICKHOUSE_URL:-http://127.0.0.1:8123/}

q() { curl -sS "$ch" --data-binary "$1"; }

{
	printf -- '---\ntype: reference\naudience: package maintainer\nstatus: draft\n---\n\n> **Status: draft — pre-human-review.** Not verified; do not cite as authoritative.\n\n'
	echo "# Environment — $slug"
	echo
	echo "- boxer commit: $(git -C "$root" rev-parse --short HEAD)$(git -C "$root" diff --quiet -- apps/play/play_map.go apps/play/play_map_ladder.go public/thestack/imzero2/egui2/widgets/portolan || echo " (the Map pane or portolan modified in the working tree)")"
	echo "- ClickHouse: $(q 'SELECT version()') — $(q "SELECT value FROM system.settings WHERE name = 'max_threads'" | tr -d '\n') max_threads"
	echo "- Tables: $(q "SELECT arrayStringConcat(groupArray(name || ' ' || toString(total_rows) || ' rows'), ', ') FROM (SELECT name, total_rows FROM system.tables WHERE database = currentDatabase() AND name LIKE 'planes_mercator%' ORDER BY name)")"
	echo "- Go: $(go version | cut -d' ' -f3-)"
	echo "- CPU: $(lscpu | sed -n 's/^Model name: *//p'), $(nproc) threads"
	echo "- Memory: $(free -h | awk '/^Mem:/ {print $2 " total, " $7 " available"}')"
	echo "- Kernel: $(uname -sr)"
	echo "- Load average before the run: $(cut -d' ' -f1-3 /proc/loadavg)"
} >"$out/environment.md"

cd "$root"
MTA_RUN_DIR="$out" go test -count=1 -timeout 30m -tags="$(cat ./tags) integration" \
	-run 'TestSequenceCounts|TestServerCost|TestQueryCache|TestFirstPixels|TestResultShape|TestBrightness' \
	./doc/trials/map-tile-addressing/harness/

echo "- Load average after the run: $(cut -d' ' -f1-3 /proc/loadavg)" >>"$out/environment.md"
