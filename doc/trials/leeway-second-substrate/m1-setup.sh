#!/usr/bin/env bash
#
# M1 setup — build the 1M corpus in both renderings and export both to Parquet.
#
# Everything downstream reads the Parquet files, so the files *are* the M1
# corpus: which 1M documents they hold does not matter, only that both
# renderings hold the same ones. A dense document id stamped once, before the
# split, is what guarantees that.
#
# Compression is pinned to ZSTD on the Parquet side. README §7 Q1 asks for that
# decision before M2 rather than after seeing numbers, and M1 is when the files
# get written, so it is made here: ZSTD on both sides, matching the ClickHouse
# tables' CODEC(ZSTD(3)), so a later size comparison is between like and like.
#
# Usage: [TIER=1000000] [DB=jsonbench_m1] [OUT=<dir>] ./m1-setup.sh

set -euo pipefail

TIER="${TIER:-1000000}"
DB_SRC="${DB_SRC:-jsonbench_j2_10m}"
SRC_TABLE="${SRC_TABLE:-json}"
DB="${DB:-jsonbench_m1}"
OUT="${OUT:-./runs/2026-08-07-m1/corpus}"
CH=(clickhouse-client --allow_suspicious_low_cardinality_types=1)
mkdir -p "$OUT"

c() { printf 'tv:%s:%s:%s:%s:0:0:0::' "$1" "$2" "$3" "$4"; }
sym_v="$(c symbol value val s:m)"; sym_p="$(c symbol lmv lmv y:m)"; sym_h="$(c symbol mvhp mvhp y:g)"
str_v="$(c string value val s:g)"; str_p="$(c string lmv lmv y:m)"; str_h="$(c string mvhp mvhp y:g)"
i64_v="$(c int64 value val i64:4o)"; i64_p="$(c int64 lmv lmv y:m)"; i64_h="$(c int64 mvhp mvhp y:g)"
bol_v="$(c bool value val b:g)";    bol_p="$(c bool lmv lmv y:m)";    bol_h="$(c bool mvhp mvhp y:g)"

"${CH[@]}" -q "DROP DATABASE IF EXISTS $DB"
"${CH[@]}" -q "CREATE DATABASE $DB"

echo "== packed, $TIER documents, with a dense document id =="
"${CH[@]}" -q "CREATE TABLE $DB.packed ENGINE = MergeTree ORDER BY doc AS
  SELECT rowNumberInAllBlocks() AS doc, * FROM $DB_SRC.\`$SRC_TABLE\` LIMIT $TIER"

echo "== exploded, the same documents =="
"${CH[@]}" -q "CREATE TABLE $DB.attrs (
  doc     UInt64                 CODEC(DoubleDelta, ZSTD(3)),
  section LowCardinality(String) CODEC(ZSTD(3)),
  path    LowCardinality(String) CODEC(ZSTD(3)),
  mvhp    String                 CODEC(ZSTD(3)),
  sym     LowCardinality(String) CODEC(ZSTD(3)),
  str     String                 CODEC(ZSTD(3)),
  i64     Int64                  CODEC(DoubleDelta, ZSTD(3)),
  f64     Float64                CODEC(ZSTD(3)),
  b       Bool                   CODEC(ZSTD(3))
) ENGINE = MergeTree ORDER BY (path, doc)"

ins() { # section, slot, path col, mvhp col, value col
  local sel="doc, '$1', p, h, "
  case "$2" in
  sym) sel+="v, '', 0, 0, false" ;;
  str) sel+="'', v, 0, 0, false" ;;
  i64) sel+="'', '', v, 0, false" ;;
  b)   sel+="'', '', 0, 0, v" ;;
  esac
  "${CH[@]}" -q "INSERT INTO $DB.attrs (doc, section, path, mvhp, sym, str, i64, f64, b)
    SELECT $sel FROM $DB.packed
    ARRAY JOIN \`$5\` AS v, \`$3\` AS p, \`$4\` AS h" --max_memory_usage=0
}
ins symbol sym "$sym_p" "$sym_h" "$sym_v"
ins string str "$str_p" "$str_h" "$str_v"
ins int64  i64 "$i64_p" "$i64_h" "$i64_v"
ins bool   b   "$bol_p" "$bol_h" "$bol_v"

echo "== export =="
# The `doc` column is dropped from the packed export: the packed rendering has
# no use for it (a document *is* a row) and carrying it would make the two
# files differ by a column that no query reads.
P=(--output_format_parquet_compression_method=zstd)
"${CH[@]}" "${P[@]}" --database="$DB" \
  -q "SELECT * EXCEPT doc FROM packed ORDER BY doc FORMAT Parquet" > "$OUT/packed.parquet"
"${CH[@]}" "${P[@]}" --database="$DB" \
  -q "SELECT * FROM attrs ORDER BY path, doc FORMAT Parquet" > "$OUT/exploded.parquet"

echo "== result =="
{
  printf 'file\tbytes\trows\n'
  printf 'packed.parquet\t%s\t%s\n' "$(stat -c%s "$OUT/packed.parquet")" \
    "$("${CH[@]}" -q "SELECT count() FROM $DB.packed")"
  printf 'exploded.parquet\t%s\t%s\n' "$(stat -c%s "$OUT/exploded.parquet")" \
    "$("${CH[@]}" -q "SELECT count() FROM $DB.attrs")"
  printf 'documents_in_exploded\t\t%s\n' "$("${CH[@]}" -q "SELECT uniqExact(doc) FROM $DB.attrs")"
} | tee "$OUT/corpus.tsv" | column -t
