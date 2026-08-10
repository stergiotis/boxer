#!/usr/bin/env bash
#
# Arm C — arm B plus data-skipping indices, built by cloning arm B's table so
# the two arms hold byte-identical data and only the index differs.
#
# What can actually be indexed here is the finding, not a detail. Arm B's
# filters read a value out of an array by its path
# (`arrayFirst((v,p) -> p = '/commit/collection', vals, paths) = '…'`), and no
# skipping index can serve that expression: the index would have to know the
# result of a lambda over two co-indexed arrays. What a bloom filter *can*
# serve is `has(vals, '…')` — "this granule contains that string somewhere in
# the section". So arm C pairs each index with a redundant `has()` conjunct in
# the query (queries-facts-skip.sql): the conjunct prunes granules, the
# original `arrayFirst` predicate still decides correctness.
#
# Usage: DB_SRC=jsonbench_b_1m DB=jsonbench_c_1m ./arm-c.sh

set -euo pipefail

DB_SRC="${DB_SRC:-jsonbench_b_1m}"
DB="${DB:-jsonbench_c_1m}"
TABLE="${TABLE:-facts}"
CH=(clickhouse-client --allow_suspicious_low_cardinality_types=1)

SYM_VAL='tv:symbol:value:val:s:124::I:0::data'
SYM_PARAM='tv:symbol:mrhp:mrhp:y:4:::0::data'
STR_PARAM='tv:stringArray:mrhp:mrhp:y:4:::0::data'

"${CH[@]}" -q "DROP DATABASE IF EXISTS $DB"
"${CH[@]}" -q "CREATE DATABASE $DB"
"${CH[@]}" -q "CREATE TABLE $DB.$TABLE AS $DB_SRC.$TABLE"

# bloom_filter over an Array(String) column indexes the set of elements present
# in each granule, which is exactly what has() asks about.
"${CH[@]}" -q "ALTER TABLE $DB.$TABLE ADD INDEX idxSymbolValues \`$SYM_VAL\`   TYPE bloom_filter(0.01) GRANULARITY 1"
"${CH[@]}" -q "ALTER TABLE $DB.$TABLE ADD INDEX idxSymbolPaths  \`$SYM_PARAM\` TYPE bloom_filter(0.01) GRANULARITY 1"
"${CH[@]}" -q "ALTER TABLE $DB.$TABLE ADD INDEX idxStringPaths  \`$STR_PARAM\` TYPE bloom_filter(0.01) GRANULARITY 1"

"${CH[@]}" -q "INSERT INTO $DB.$TABLE SELECT * FROM $DB_SRC.$TABLE"
"${CH[@]}" -q "ALTER TABLE $DB.$TABLE MATERIALIZE INDEX idxSymbolValues" --mutations_sync=2
"${CH[@]}" -q "ALTER TABLE $DB.$TABLE MATERIALIZE INDEX idxSymbolPaths"  --mutations_sync=2
"${CH[@]}" -q "ALTER TABLE $DB.$TABLE MATERIALIZE INDEX idxStringPaths"  --mutations_sync=2

"${CH[@]}" -q "SELECT name, type_full, formatReadableSize(data_compressed_bytes) AS size
  FROM system.data_skipping_indices WHERE database = '$DB' AND table = '$TABLE' FORMAT TSVWithNames"
