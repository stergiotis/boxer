#!/usr/bin/env bash
#
# M2 — time one query file on one engine.
#
# Mirrors the sibling trial's measure.sh discipline so the two are comparable:
# TRIES=3 per statement, cold = try 1, hot = min(try 2, try 3). Cache-dropping
# needs passwordless sudo, which this box does not grant, so DROP_CACHES
# defaults off and the cold column must be reported **absent**, not noisy.
#
# Every engine is timed the same way — `/usr/bin/time` around one CLI
# invocation — because the three do not report a comparable figure themselves:
# ClickHouse has a query-scoped peak, the other two have none. So the numbers
# here are **process** wall clock and **process** maxRSS, which for DuckDB and
# DataFusion means one query plus one startup, and for ClickHouse means one
# client plus a server-side query the client's RSS does not see. Both caveats
# are real and neither is fixable by measuring harder:
#
#   - startup is measured separately per engine (`SELECT 1`) and recorded in
#     baseline.tsv, so it can be subtracted or at least acknowledged;
#   - ClickHouse maxRSS is the *client's*, so it is not comparable to the
#     targets' at all. clickhouse.mem.tsv carries the server-side figure from
#     --memory-usage beside it; that is the one to quote for ClickHouse.
#
# Usage:
#   ENGINE=duckdb FILE=m1-packed.duckdb.sql OUT=<dir> CWD=<corpus> [TRIES=3] ./m2-bench.sh

set -uo pipefail

ENGINE="${ENGINE:?}"
FILE="${FILE:?}"
OUT="${OUT:?}"
TRIES="${TRIES:-3}"
CWD="${CWD:-$(pwd)/runs/2026-08-07-m2/corpus}"
DB="${DB:-jsonbench_m2}"
DUCKDB="${DUCKDB:-$HOME/.local/bin/duckdb}"
DATAFUSION="${DATAFUSION:-$HOME/.local/bin/datafusion-cli}"
LABEL="${LABEL:-$(basename "$FILE" .sql)}"

mkdir -p "$OUT"
FILE_ABS=$(cd "$(dirname "$FILE")" && pwd)/$(basename "$FILE")
TIMEFMT='%e %M'

invoke() { # sql -> runs it, output discarded; /usr/bin/time to stderr
  case "$ENGINE" in
  clickhouse) /usr/bin/time -f "$TIMEFMT" clickhouse-client --database="$DB" \
                --use_query_condition_cache=0 --min_execution_speed=0 \
                --session_timezone=UTC --format=Null --query="$1" ;;
  duckdb)     (cd "$CWD" && /usr/bin/time -f "$TIMEFMT" "$DUCKDB" -csv -noheader -c "$1" >/dev/null) ;;
  datafusion) (cd "$CWD" && /usr/bin/time -f "$TIMEFMT" "$DATAFUSION" --format csv -q -c "$1" >/dev/null) ;;
  esac
}

# Startup baseline, so a 20 ms query and a 20 ms process launch are not confused.
base=$(mktemp)
for i in 1 2 3; do invoke "SELECT 1" 2>>"$base" >/dev/null; done
printf 'engine\tstartup_s\tstartup_maxrss_kb\n' > "$OUT/baseline.tsv"
awk -v e="$ENGINE" 'NF==2 { if (min=="" || $1+0 < min) { min=$1+0; rss=$2 } } END { printf "%s\t%s\t%s\n", e, min, rss }' \
  "$base" >> "$OUT/baseline.tsv"
rm -f "$base"

if grep -qx -- '-- @@' "$FILE_ABS"; then
  PRELUDE="$(sed -n '1,/^-- @@$/p' "$FILE_ABS" | sed '$d' | sed '/^[[:space:]]*--/d;/^[[:space:]]*$/d')
"
  BODY=$(mktemp); sed '1,/^-- @@$/d' "$FILE_ABS" > "$BODY"
else
  PRELUDE=""; BODY="$FILE_ABS"
fi

: > "$OUT/timings.tsv"
[ "$ENGINE" = clickhouse ] && : > "$OUT/clickhouse.mem.tsv"
n=0
while IFS= read -r -d '' stmt; do
  [ -z "$(printf '%s' "$stmt" | tr -d '[:space:]')" ] && continue
  n=$((n + 1))
  for i in $(seq 1 "$TRIES"); do
    m=$(mktemp)
    invoke "$PRELUDE$stmt" 2>"$m" >/dev/null
    read -r secs rss < <(grep -E '^[0-9.]+ [0-9]+$' "$m" | tail -1)
    printf '%s\tQ%d\t%d\t%s\t%s\n' "$LABEL" "$n" "$i" "${secs:-NA}" "${rss:-NA}" >> "$OUT/timings.tsv"
    rm -f "$m"
  done
  if [ "$ENGINE" = clickhouse ]; then
    read -r s2 mem2 < <(clickhouse-client --database="$DB" --use_query_condition_cache=0 \
      --min_execution_speed=0 --session_timezone=UTC --time --memory-usage --format=Null \
      --query="$PRELUDE$stmt" --progress 0 2>&1 >/dev/null | paste -sd' ')
    printf 'Q%d\t%s\t%s\n' "$n" "$s2" "$mem2" >> "$OUT/clickhouse.mem.tsv"
  fi
done < <(python3 - "$BODY" <<'PY'
import re, sys
src = re.sub(r"(?m)^\s*--.*$", "", open(sys.argv[1]).read())
for part in re.split(r";\s*(?:\n|$)", src):
    sys.stdout.write(part); sys.stdout.write("\0")
PY
)

echo "== $LABEL — hot = min(try2, try3) =="
awk -F'\t' '$3>=2 { k=$2; if (!(k in m) || $4+0 < m[k]) { m[k]=$4+0; r[k]=$5 } }
  END { for (k in m) printf "%s\t%.3f\t%s\n", k, m[k], r[k] }' "$OUT/timings.tsv" \
  | sort -t Q -k2 -n | column -t
