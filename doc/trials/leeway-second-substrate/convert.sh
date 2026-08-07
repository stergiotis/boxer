#!/usr/bin/env bash
#
# Measure the packed -> exploded conversion in ClickHouse: one pass, no staging.
#
# arm-x.sh builds the exploded table in five steps — stage a dense document id,
# then one INSERT per section — because four independent `rowNumberInAllBlocks`
# calls would not agree on which document is which. That is fine for building an
# experiment and wrong for measuring a conversion: it charges the conversion a
# full extra copy of the source.
#
# This does it in one statement instead. Each section's three lanes are zipped
# into a tuple array, the four arrays are concatenated, and the result is
# ARRAY JOINed once — so `rowNumberInAllBlocks()` is evaluated once per source
# row and every section sees the same document id. One read of the source, one
# write of the target.
#
# Reported: wall clock, rows/s, peak memory, and the resulting size. These are
# the numbers that decide whether an exploded companion table is affordable as a
# maintained redundancy rather than a one-off experiment.
#
# Usage: DB_SRC=jsonbench_j2_100m DB=jsonbench_conv_100m OUT=<dir> ./convert.sh

set -euo pipefail

DB_SRC="${DB_SRC:-jsonbench_j2_10m}"
SRC_TABLE="${SRC_TABLE:-json}"
DB="${DB:-jsonbench_conv_10m}"
TABLE="${TABLE:-attrs}"
ORDER_BY="${ORDER_BY:-(path, doc)}"
OUT="${OUT:-}"
CH=(clickhouse-client --allow_suspicious_low_cardinality_types=1)

c() { printf 'tv:%s:%s:%s:%s:0:0:0::' "$1" "$2" "$3" "$4"; }
# section | value | path | mvhp, in the tuple order the INSERT expects.
zip() { # section, value col, path col, mvhp col, slot
  local sec="$1" v="$2" p="$3" h="$4" slot="$5"
  local sym="''" str="''" i64="toInt64(0)" b="false"
  case "$slot" in
  sym) sym="v" ;; str) str="v" ;; i64) i64="v" ;; b) b="v" ;;
  esac
  printf "arrayMap((v, p, h) -> ('%s', p, h, %s, %s, %s, %s), \`%s\`, \`%s\`, \`%s\`)" \
    "$sec" "$sym" "$str" "$i64" "$b" "$v" "$p" "$h"
}

lanes="$(zip symbol "$(c symbol value val s:m)" "$(c symbol lmv lmv y:m)" "$(c symbol mvhp mvhp y:g)" sym),
       $(zip string "$(c string value val s:g)" "$(c string lmv lmv y:m)" "$(c string mvhp mvhp y:g)" str),
       $(zip int64  "$(c int64 value val i64:4o)" "$(c int64 lmv lmv y:m)" "$(c int64 mvhp mvhp y:g)" i64),
       $(zip bool   "$(c bool value val b:g)"   "$(c bool lmv lmv y:m)"   "$(c bool mvhp mvhp y:g)"   b)"

"${CH[@]}" -q "DROP DATABASE IF EXISTS $DB"
"${CH[@]}" -q "CREATE DATABASE $DB"
"${CH[@]}" -q "CREATE TABLE $DB.$TABLE (
  doc     UInt64                 CODEC(DoubleDelta, ZSTD(3)),
  section LowCardinality(String) CODEC(ZSTD(3)),
  path    LowCardinality(String) CODEC(ZSTD(3)),
  mvhp    String                 CODEC(ZSTD(3)),
  sym     LowCardinality(String) CODEC(ZSTD(3)),
  str     String                 CODEC(ZSTD(3)),
  i64     Int64                  CODEC(DoubleDelta, ZSTD(3)),
  f64     Float64                CODEC(ZSTD(3)),
  b       Bool                   CODEC(ZSTD(3))
) ENGINE = MergeTree ORDER BY $ORDER_BY"

srcRows=$("${CH[@]}" -q "SELECT count() FROM $DB_SRC.\`$SRC_TABLE\`")
echo "== converting $srcRows documents, one pass =="

# --time --memory-usage give the two numbers the decision turns on.
read -r secs mem < <("${CH[@]}" --time --memory-usage --progress 0 --max_memory_usage=0 \
  --query "INSERT INTO $DB.$TABLE (doc, section, path, mvhp, sym, str, i64, f64, b)
    SELECT doc, t.1, t.2, t.3, t.4, t.5, t.6, 0, t.7
    FROM (SELECT rowNumberInAllBlocks() AS doc, * FROM $DB_SRC.\`$SRC_TABLE\`)
    ARRAY JOIN arrayConcat($lanes) AS t" 2>&1 >/dev/null | paste -sd' ')

attrs=$("${CH[@]}" -q "SELECT count() FROM $DB.$TABLE")
docs=$("${CH[@]}" -q "SELECT uniqExact(doc) FROM $DB.$TABLE")
[ "$docs" = "$srcRows" ] || { echo "ABORT: document count not conserved ($docs != $srcRows)" >&2; exit 1; }

srcB=$("${CH[@]}" -q "SELECT sum(bytes_on_disk) FROM system.parts WHERE active AND database='$DB_SRC' AND table='$SRC_TABLE'")
dstB=$("${CH[@]}" -q "SELECT sum(bytes_on_disk) FROM system.parts WHERE active AND database='$DB'")

# Merges keep running after the INSERT returns; the size below is pre-merge
# unless this waits, and a pre-merge size is not the size you plan capacity on.
"${CH[@]}" -q "OPTIMIZE TABLE $DB.$TABLE FINAL" --mutations_sync=2 || true
dstFinal=$("${CH[@]}" -q "SELECT sum(bytes_on_disk) FROM system.parts WHERE active AND database='$DB'")

{
  printf 'metric\tvalue\n'
  printf 'source_documents\t%s\n' "$srcRows"
  printf 'target_attributes\t%s\n' "$attrs"
  printf 'attributes_per_document\t%s\n' "$(awk -v a="$attrs" -v d="$srcRows" 'BEGIN{printf "%.3f", a/d}')"
  printf 'convert_seconds\t%s\n' "$secs"
  printf 'convert_peak_bytes\t%s\n' "$mem"
  printf 'documents_per_second\t%s\n' "$(awk -v d="$srcRows" -v s="$secs" 'BEGIN{printf "%.0f", d/s}')"
  printf 'attributes_per_second\t%s\n' "$(awk -v a="$attrs" -v s="$secs" 'BEGIN{printf "%.0f", a/s}')"
  printf 'source_bytes\t%s\n' "$srcB"
  printf 'target_bytes_premerge\t%s\n' "$dstB"
  printf 'target_bytes\t%s\n' "$dstFinal"
  printf 'target_over_source\t%s\n' "$(awk -v t="$dstFinal" -v s="$srcB" 'BEGIN{printf "%.4f", t/s}')"
  printf 'source_mib_per_second\t%s\n' "$(awk -v s="$srcB" -v t="$secs" 'BEGIN{printf "%.1f", s/1048576/t}')"
} | tee ${OUT:+"$OUT/convert.tsv"} | column -t
