#!/usr/bin/env bash
#
# Measure one arm: sizes, pruning evidence, physical plans, query runtimes and
# memory, and query results. Arm-agnostic — arm A points it at the upstream
# queries, arms B/C at queries-facts.sql.
#
# Reimplements the pinned upstream run discipline (upstream/PIN.md § Run
# discipline): TRIES=3 per query, cache drop once before each query's tries,
# cold = try 1, hot = min(try 2, try 3).
#
# Usage:
#   DB=jsonbench_b_1m TABLE=facts QUERIES=./queries-facts.sql OUT=<dir> \
#   [DROP_CACHES=1] [TRIES=3] ./measure.sh [sizes|explain|bench|results|all]
#
# DROP_CACHES=1 needs passwordless sudo for /proc/sys/vm/drop_caches. With
# DROP_CACHES=0 try 1 is not a true cold run and the arm's cold column must be
# reported as absent, not merely noisy.

set -euo pipefail

DB="${DB:?}"
TABLE="${TABLE:?}"
QUERIES="${QUERIES:?}"
OUT="${OUT:?}"
TRIES="${TRIES:-3}"
DROP_CACHES="${DROP_CACHES:-0}"
# Extra client settings, space-separated `--name=value` flags. Used by the
# hint-free reference arm (a00), whose JSON paths are `Dynamic` and which
# ClickHouse refuses to GROUP BY without an explicit opt-in. Passing the
# setting keeps the upstream queries verbatim; casting in the query would not.
read -r -a CH_EXTRA <<< "${CH_SETTINGS:-}"
CH=(clickhouse-client "${CH_EXTRA[@]}")

mkdir -p "$OUT"

drop_caches() {
  sync
  if [[ "$DROP_CACHES" == "1" ]]; then
    echo 3 | sudo -n tee /proc/sys/vm/drop_caches >/dev/null
  fi
}

# One statement per line, whatever the file's formatting. `clickhouse format
# --oneline -n` does the normalisation, so query files can be written to be
# read — multi-line, indented, commented — instead of as single 2,000-character
# lines the runner loop happens to need. (Borrowed from the prior-art harness;
# see runs/2026-08-05-m0-m3-1m/prior-art.md.)
#
# RESOLVE, when set to a `jsonbench` binary, first expands leeway column
# handles (`symbol:value`) to physical names via ADR-0116's ResolveColumnNames
# pass. That is what lets the facts query files be written against section and
# column names instead of `tv:symbol:value:val:s:124::I:0::data`. Arms whose
# queries carry no handles are unaffected, so it is safe to leave set.
statements() {
  if [[ -n "${RESOLVE:-}" ]]; then
    "$RESOLVE" resolve --database "$DB" "$QUERIES" | clickhouse format --oneline -n | grep -v '^\s*$'
  else
    clickhouse format --oneline -n < "$QUERIES" | grep -v '^\s*$'
  fi
}

case "${1:-all}" in
sizes | all)
  {
    echo "count $("${CH[@]}" --database="$DB" -q "SELECT count() FROM \`$TABLE\`")"
    for expr in "sum(bytes_on_disk)|total_size" \
                "sum(data_compressed_bytes)|data_size" \
                "sum(primary_key_size) + sum(marks_bytes)|index_size" \
                "sum(data_uncompressed_bytes)|uncompressed" \
                "count()|parts" \
                "sum(marks)|marks"; do
      v=$("${CH[@]}" -q "SELECT ${expr%%|*} FROM system.parts \
        WHERE database = '$DB' AND table = '$TABLE' AND active FORMAT TSV")
      echo "${expr##*|} $v"
    done
  } | tee "$OUT/sizes.txt"
  ;;&

explain | all)
  : > "$OUT/explain.txt"
  n=1
  while IFS= read -r q; do
    {
      printf '=== Q%d indexes=1 ===\n' "$n"
      "${CH[@]}" --database="$DB" --query="EXPLAIN indexes=1 $q"
      printf '\n=== Q%d PIPELINE ===\n' "$n"
      "${CH[@]}" --database="$DB" --query="EXPLAIN PIPELINE $q"
      echo
    } >> "$OUT/explain.txt"
    n=$((n + 1))
  done < <(statements)
  ;;&

bench | all)
  : > "$OUT/timings.tsv"
  n=1
  while IFS= read -r q; do
    drop_caches
    for i in $(seq 1 "$TRIES"); do
      read -r secs mem < <("${CH[@]}" --database="$DB" --time --memory-usage \
        --format=Null --query="$q" --progress 0 2>&1 >/dev/null | paste -sd' ')
      printf 'Q%d\t%d\t%s\t%s\n' "$n" "$i" "$secs" "$mem" >> "$OUT/timings.tsv"
    done
    n=$((n + 1))
  done < <(statements)
  cat "$OUT/timings.tsv"
  ;;&

results | all)
  : > "$OUT/query-results.txt"
  n=1
  while IFS= read -r q; do
    {
      printf -- '--- Q%d ---\n' "$n"
      "${CH[@]}" --database="$DB" --format=PrettyCompactMonoBlock \
        --query="$q" --progress 0
    } >> "$OUT/query-results.txt"
    n=$((n + 1))
  done < <(statements)
  ;;
esac
