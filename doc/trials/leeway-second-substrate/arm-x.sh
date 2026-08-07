#!/usr/bin/env bash
#
# Arm X — the exploded rendering, in ClickHouse: one row per attribute.
#
# M0 found that DataFusion has no higher-order array functions at all, yet the
# leeway lane algebra survives there by explosion — `unnest` plus ordinary
# relational operators. That raised a question the trial had not asked: if the
# algebra has a lambda-free rendering, what does that rendering *cost*, on an
# engine where both forms can be measured side by side over identical data?
#
# This arm answers it by materialising the explosion. The canonical mapping
# makes it lossless: `lmvcard` is uniformly 1 (one verbatim membership per
# attribute) and every section's `value` co-lengths with its `lmv`, so
# ARRAY JOIN over (value, lmv, mvhp) yields exactly one row per attribute with
# no reconstruction. Both properties are asserted below rather than assumed.
#
# What the comparison isolates, against arm J over the same corpus:
#   - storage: does repeating a document id per attribute cost more than the
#     addressing lanes it replaces?
#   - single-path queries: explosion should win — the path becomes a sort-key
#     prefix instead of an array scan.
#   - multi-path queries: explosion should lose — what arm J gets for free by
#     co-indexing within a row, this arm must reassemble with GROUP BY doc.
#
# Usage: DB_SRC=jsonbench_j2_10m DB=jsonbench_x_10m [ORDER=path|doc] ./arm-x.sh

set -euo pipefail

DB_SRC="${DB_SRC:-jsonbench_j2_10m}"
SRC_TABLE="${SRC_TABLE:-json}"
DB="${DB:-jsonbench_x_10m}"
TABLE="${TABLE:-attrs}"
# path = ORDER BY (path, doc), the shape that makes explosion attractive.
# doc  = ORDER BY (doc, path), the shape that favours per-document reassembly.
# They are opposites, and which one is chosen is the arm's single tuning knob.
ORDER="${ORDER:-path}"
CH=(clickhouse-client --allow_suspicious_low_cardinality_types=1)

case "$ORDER" in
path) ORDER_BY="(path, doc)" ;;
doc) ORDER_BY="(doc, path)" ;;
*) echo "ORDER must be 'path' or 'doc'" >&2; exit 2 ;;
esac

# Physical column names of the canonical mapping. Written out rather than
# resolved through `jsonbench resolve`, because that pass runs on a ClickHouse
# parser and this trial's whole point is not to depend on one (README §3a).
c() { printf 'tv:%s:%s:%s:%s:0:0:0::' "$1" "$2" "$3" "$4"; }
sym_v="$(c symbol value val s:m)"; sym_p="$(c symbol lmv lmv y:m)"; sym_h="$(c symbol mvhp mvhp y:g)"; sym_c="$(c symbol lmvcard lmvcard u64:4gw)"
str_v="$(c string value val s:g)"; str_p="$(c string lmv lmv y:m)"; str_h="$(c string mvhp mvhp y:g)"; str_c="$(c string lmvcard lmvcard u64:4gw)"
i64_v="$(c int64 value val i64:4o)"; i64_p="$(c int64 lmv lmv y:m)"; i64_h="$(c int64 mvhp mvhp y:g)"; i64_c="$(c int64 lmvcard lmvcard u64:4gw)"
bol_v="$(c bool value val b:g)";    bol_p="$(c bool lmv lmv y:m)";    bol_h="$(c bool mvhp mvhp y:g)";  bol_c="$(c bool lmvcard lmvcard u64:4gw)"

# The explosion is lossless only if, for every section, the value lane
# co-lengths with the path lane and every membership cardinality is exactly 1.
# Assert it rather than trust the mapping: a violation here would silently
# mis-pair values with paths, which is finding 10 of the sibling trial's ledger
# in a new costume.
check() { # section, value col, path col, lmvcard col
  local bad
  bad=$("${CH[@]}" --database="$DB_SRC" -q "
    SELECT countIf(length(\`$2\`) != length(\`$3\`))
         + countIf(arrayExists(x -> x != 1, \`$4\`))
    FROM \`$SRC_TABLE\`")
  echo "  $1: violations=$bad"
  [ "$bad" = "0" ] || { echo "ABORT: $1 is not 1:1 / co-length; ARRAY JOIN would not be lossless" >&2; exit 1; }
}
echo "== preconditions: the explosion is only lossless if these hold =="
check symbol "$sym_v" "$sym_p" "$sym_c"
check string "$str_v" "$str_p" "$str_c"
check int64  "$i64_v" "$i64_p" "$i64_c"
check bool   "$bol_v" "$bol_p" "$bol_c"

"${CH[@]}" -q "DROP DATABASE IF EXISTS $DB"
"${CH[@]}" -q "CREATE DATABASE $DB"

# A dense document id, materialised once. The canonical mapping's only row
# identity is `id:blake3hash` — 32 incompressible bytes, which the USP document
# already charges the leeway table 20.3% for. Repeating *that* per attribute
# would dominate the exploded table, so this arm mints a dense UInt64 instead.
# It has to be stamped before the ARRAY JOIN and reused across all four section
# inserts, hence a staging table rather than four independent rowNumber calls.
echo "== staging a dense document id =="
"${CH[@]}" -q "CREATE TABLE $DB.src ENGINE = MergeTree ORDER BY doc AS
  SELECT rowNumberInAllBlocks() AS doc, * FROM $DB_SRC.\`$SRC_TABLE\`"

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

echo "== exploding =="
ins() { # section, value-expr, value-column-slot, path col, mvhp col
  local sec="$1" vexpr="$2" slot="$3" pcol="$4" hcol="$5" vcol="$6"
  local cols="doc, section, path, mvhp, sym, str, i64, f64, b"
  local sel="doc, '$sec', p, h, "
  case "$slot" in
  sym) sel+="$vexpr, '', 0, 0, false" ;;
  str) sel+="'', $vexpr, 0, 0, false" ;;
  i64) sel+="'', '', $vexpr, 0, false" ;;
  b)   sel+="'', '', 0, 0, $vexpr" ;;
  esac
  "${CH[@]}" -q "INSERT INTO $DB.$TABLE ($cols)
    SELECT $sel FROM $DB.src
    ARRAY JOIN \`$vcol\` AS v, \`$pcol\` AS p, \`$hcol\` AS h" --max_memory_usage=0
  echo "  $sec done"
}
ins symbol v sym "$sym_p" "$sym_h" "$sym_v"
ins string v str "$str_p" "$str_h" "$str_v"
ins int64  v i64 "$i64_p" "$i64_h" "$i64_v"
ins bool   v b   "$bol_p" "$bol_h" "$bol_v"

"${CH[@]}" -q "DROP TABLE $DB.src"
"${CH[@]}" -q "OPTIMIZE TABLE $DB.$TABLE FINAL" --mutations_sync=2 || true

echo "== result =="
"${CH[@]}" -q "SELECT sorting_key FROM system.tables WHERE database='$DB' AND name='$TABLE' FORMAT TSVRaw"
"${CH[@]}" -q "SELECT section, count() FROM $DB.$TABLE GROUP BY section ORDER BY count() DESC FORMAT TSVWithNames"
"${CH[@]}" -q "SELECT count() AS attributes, uniqExact(doc) AS documents FROM $DB.$TABLE FORMAT TSVWithNames"
