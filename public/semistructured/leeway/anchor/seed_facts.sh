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
# Prerequisite: a ClickHouse server reachable over HTTP, by default at
# localhost:8123. The endpoint comes from the CLICKHOUSE_* registry entries
# (ADR-0009), resolved once below and exported, so the loader this script
# spawns inherits the same coordinates its pre-flight and verify used.
# CLICKHOUSE_USER / CLICKHOUSE_PASSWORD are honoured too.
set -euo pipefail

# CLICKHOUSE_ENDPOINT wins over CLICKHOUSE_URL; both are optional. The trailing
# slash is stripped because the paths below add their own. Exported so the
# child `go test` sees one resolved endpoint rather than re-deriving it from a
# precedence it may not implement the same way.
CH_ENDPOINT="${CLICKHOUSE_ENDPOINT:-${CLICKHOUSE_URL:-http://localhost:8123}}"
CH_ENDPOINT="${CH_ENDPOINT%/}"
CH_USER="${CLICKHOUSE_USER:-default}"
export CLICKHOUSE_ENDPOINT="$CH_ENDPOINT"

# Credential headers for every curl below. The key header is omitted entirely
# when no password is set, which is what the unauthenticated `default` account
# expects.
ch_auth=(-H "X-ClickHouse-User: $CH_USER")
if [ -n "${CLICKHOUSE_PASSWORD:-}" ]; then
	ch_auth+=(-H "X-ClickHouse-Key: $CLICKHOUSE_PASSWORD")
fi

script_dir="$(cd "$(dirname "${BASH_SOURCE[0]}")" && pwd)"
repo_root="$(git -C "$script_dir" rev-parse --show-toplevel)"
# boxer build tags; the package won't compile without them. `integration` is
# appended because the loader is a member of the integration lane
# (`//go:build integration`, ENGINEERING_PRACTICES §4) — without that tag the
# test is not compiled in, `-run` matches nothing, and `go test` exits 0 having
# inserted nothing at all.
tags="$(cat "$repo_root/tags"),integration"

# 1. Pre-flight. If ClickHouse is down the test would SKIP silently and leave
#    the table untouched, so fail loudly here instead.
if ! curl -fsS "${ch_auth[@]}" "$CH_ENDPOINT/ping" >/dev/null 2>&1; then
	echo "error: ClickHouse not reachable at $CH_ENDPOINT" >&2
	echo "       start one first, e.g.:" >&2
	echo "         docker run -d --rm -p 8123:8123 clickhouse/clickhouse-server" >&2
	echo "       or point CLICKHOUSE_ENDPOINT at an existing server" >&2
	exit 1
fi

# 2. Run the loader. -count=1 defeats Go's test cache so it actually re-inserts
#    rather than replaying a cached PASS; -v surfaces the demo DQL query output.
echo "Seeding anchor.facts via TestLeewayClickHouse ..."
go test -tags="$tags" -count=1 -v -run '^TestLeewayClickHouse$' "$script_dir"

# 3. Verify. Checked rather than just reported: a `-run` pattern that matches
#    nothing is not an error to `go test`, so without this guard a renamed or
#    re-tagged loader leaves the table empty and the script still exits 0.
count="$(curl -fsS "${ch_auth[@]}" "$CH_ENDPOINT/" \
	--data-binary 'SELECT count() FROM anchor.facts')"
if [ "$count" -eq 0 ]; then
	echo "error: anchor.facts is still empty — the loader inserted nothing" >&2
	echo "       check that TestLeewayClickHouse still exists under that name," >&2
	echo "       that the build tags above still match the ones it carries, and" >&2
	echo "       that it dialled $CH_ENDPOINT rather than a server of its own" >&2
	exit 1
fi
echo "anchor.facts now holds ${count} row(s)."
