#!/usr/bin/env bash
#
# seed_facts.sh — populate the ClickHouse `anchor.facts` demo table.
#
# `anchor.facts` is a demo/test fixture, not a live table: its engine is
# `Memory` (so rows are wiped whenever the ClickHouse server restarts) and its
# DDL is `CREATE OR REPLACE TABLE` (so it is recreated empty on each setup).
# Nothing fills it at rest. This script drives the TestLeewayClickHouse
# integration test, which creates the schema + unflatten UDF and inserts the
# Alpine + Cyber + Drone demo events via Arrow IPC, then prints the row count.
#
# Prerequisite: a ClickHouse server reachable over HTTP at localhost:8123.
# Note the Go test *hardcodes* that endpoint and ignores CLICKHOUSE_ENDPOINT,
# so CH_ENDPOINT below is used only for this script's own pre-flight and verify.
set -euo pipefail

CH_ENDPOINT="http://localhost:8123"

script_dir="$(cd "$(dirname "${BASH_SOURCE[0]}")" && pwd)"
repo_root="$(git -C "$script_dir" rev-parse --show-toplevel)"
tags="$(cat "$repo_root/tags")" # boxer build tags; the package won't compile without them

# 1. Pre-flight. If ClickHouse is down the test would SKIP silently and leave
#    the table untouched, so fail loudly here instead.
if ! curl -fsS "$CH_ENDPOINT/ping" >/dev/null 2>&1; then
	echo "error: ClickHouse not reachable at $CH_ENDPOINT" >&2
	echo "       start one first, e.g.:" >&2
	echo "         docker run -d --rm -p 8123:8123 clickhouse/clickhouse-server" >&2
	exit 1
fi

# 2. Run the loader. -count=1 defeats Go's test cache so it actually re-inserts
#    rather than replaying a cached PASS; -v surfaces the demo DQL query output.
echo "Seeding anchor.facts via TestLeewayClickHouse ..."
go test -tags="$tags" -count=1 -v -run '^TestLeewayClickHouse$' "$script_dir"

# 3. Verify.
count="$(curl -fsS -H 'X-ClickHouse-User: default' "$CH_ENDPOINT/" \
	--data-binary 'SELECT count() FROM anchor.facts')"
echo "anchor.facts now holds ${count} row(s)."
