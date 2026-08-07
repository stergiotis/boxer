#!/usr/bin/env bash
#
# Arm Y — explosion pushed to its limit: one table per section.
#
# Arm X put every attribute in one table with a `section` discriminator and a
# value column per type, of which exactly one is populated per row — a tagged
# union, laid out in columns. Arm Y removes the union: each section gets its own
# table, whose `value` column is exactly its type and always populated. No
# discriminator, no unused lanes.
#
# The three arms form one axis, packed to fully shredded:
#
#   J   one row per document, attributes in co-indexed arrays
#   X   one row per attribute, tagged union over sections
#   Y   one row per attribute, one table per section
#
# What Y changes, and why it is not simply "X but tidier":
#   - single-section queries read a table holding only that section, so U5/U8
#     stop filtering 121.2M rows down to 11.5M and just read 11.5M;
#   - cross-section queries must now JOIN, where X could co-locate every
#     section's attributes in one GROUP BY doc. Q2-Q5 span symbol, string and
#     int64, so this is where Y pays;
#   - path-census queries (U1/U2/U4/U6/U9) must UNION ALL the section tables,
#     and the reader has to know the section roster to write that union at all.
#
# Derived from arm X rather than rebuilt from arm J, so the two provably hold
# the same rows under the same document ids and differ only in layout.
#
# Usage: DB_SRC=jsonbench_x_10m DB=jsonbench_y_10m ./arm-y.sh

set -euo pipefail

DB_SRC="${DB_SRC:-jsonbench_x_10m}"
SRC_TABLE="${SRC_TABLE:-attrs}"
DB="${DB:-jsonbench_y_10m}"
# Same key as arm X's primary, so X and Y differ in layout and nothing else.
ORDER_BY="${ORDER_BY:-(path, doc)}"
CH=(clickhouse-client --allow_suspicious_low_cardinality_types=1)

"${CH[@]}" -q "DROP DATABASE IF EXISTS $DB"
"${CH[@]}" -q "CREATE DATABASE $DB"

# section | value column type | source column in arm X
#
# Only the sections the corpus actually populates get a table. That is itself
# a property of this layout worth noticing: arm X's schema is fixed by the
# mapping, arm Y's is fixed by the *data*, so a section appearing later is a
# DDL change here and a no-op there.
mk() { # section, value type, value codec, source column
  "${CH[@]}" -q "CREATE TABLE $DB.$1 (
     doc   UInt64                 CODEC(DoubleDelta, ZSTD(3)),
     path  LowCardinality(String) CODEC(ZSTD(3)),
     mvhp  String                 CODEC(ZSTD(3)),
     value $2                     CODEC($3)
   ) ENGINE = MergeTree ORDER BY $ORDER_BY"
  "${CH[@]}" -q "INSERT INTO $DB.$1 SELECT doc, path, mvhp, $4
     FROM $DB_SRC.\`$SRC_TABLE\` WHERE section = '$1'" --max_memory_usage=0
  echo "  $1: $("${CH[@]}" -q "SELECT count() FROM $DB.$1") rows"
}

echo "== splitting arm X by section =="
mk symbol  "LowCardinality(String)" "ZSTD(3)"              sym
mk string  "String"                 "ZSTD(3)"              str
mk int64   "Int64"                  "DoubleDelta, ZSTD(3)" i64
mk bool    "Bool"                   "ZSTD(3)"              b

for t in symbol string int64 bool; do
  "${CH[@]}" -q "OPTIMIZE TABLE $DB.$t FINAL" --mutations_sync=2 || true
done

echo "== conservation check against the source =="
src=$("${CH[@]}" -q "SELECT count() FROM $DB_SRC.\`$SRC_TABLE\`")
dst=$("${CH[@]}" -q "SELECT (SELECT count() FROM $DB.symbol) + (SELECT count() FROM $DB.string)
                          + (SELECT count() FROM $DB.int64)  + (SELECT count() FROM $DB.bool)")
echo "  arm X rows=$src  arm Y rows=$dst"
[ "$src" = "$dst" ] || { echo "ABORT: row count not conserved by the split" >&2; exit 1; }

echo "== result =="
"${CH[@]}" -q "SELECT name, sum(rows) AS rows, formatReadableSize(sum(bytes_on_disk)) AS on_disk,
                      sum(bytes_on_disk) AS bytes
               FROM system.parts WHERE active AND database='$DB'
               GROUP BY name ORDER BY bytes DESC FORMAT TSVWithNames"
"${CH[@]}" -q "SELECT formatReadableSize(sum(bytes_on_disk)) AS total, sum(bytes_on_disk) AS bytes
               FROM system.parts WHERE active AND database='$DB' FORMAT TSVWithNames"
