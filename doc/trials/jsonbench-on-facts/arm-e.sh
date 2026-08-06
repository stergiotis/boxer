#!/usr/bin/env bash
#
# Arm E — arm D re-keyed for the workload.
#
# Arm D materialises the five backbone paths but keeps the live store's own
# `ORDER BY ts`, so it prunes nothing: it reads every granule, just of cheaper
# columns. Arm E is the same table with the same materialised columns, sorted
# on those columns in the order the reference entry uses
# (kind, operation, collection, did, ts). Everything else is identical, so
# E minus D isolates **what a workload-shaped clustered index is worth to the
# facts model** — the one lever the trial had never spent.
#
# This is the protocol's §4 arm D ("facts clone re-keyed for the workload").
# It is named E here because the trial had already spent the letter D on the
# read-path arm the protocol did not anticipate.
#
# The DDL is derived from the arm D table rather than re-declared, so the two
# arms provably differ in the sort key and nothing else.
#
# Usage: DB_SRC=jsonbench_d_10m DB=jsonbench_e_10m ./arm-e.sh

set -euo pipefail

DB_SRC="${DB_SRC:-jsonbench_d_10m}"
DB="${DB:-jsonbench_e_10m}"
TABLE="${TABLE:-facts}"
CH=(clickhouse-client --allow_suspicious_low_cardinality_types=1)

# The sort key: the reference entry's, expressed over the materialised
# columns. `did` before `time_us` for the same reason upstream orders them so —
# cardinality ascending, the cheap discriminators first.
ORDER_BY='ORDER BY (kind, commit_operation, commit_collection, did, time_us)'

"${CH[@]}" -q "DROP DATABASE IF EXISTS $DB"
"${CH[@]}" -q "CREATE DATABASE $DB"

ddl=$("${CH[@]}" -q "SHOW CREATE TABLE $DB_SRC.$TABLE" --format=TSVRaw)
# Retarget the database and swap the sort key; leave every column untouched.
ddl=${ddl/"$DB_SRC.$TABLE"/"$DB.$TABLE"}
ddl=$(printf '%s' "$ddl" | sed -E "s|^ORDER BY .*$|$ORDER_BY|")
printf '%s' "$ddl" | grep -q "^$ORDER_BY$" || { echo "sort key not substituted" >&2; exit 1; }
printf '%s\n' "$ddl" | "${CH[@]}" -n

"${CH[@]}" -q "INSERT INTO $DB.$TABLE SELECT * FROM $DB_SRC.$TABLE"

"${CH[@]}" -q "SELECT sorting_key FROM system.tables WHERE database='$DB' AND name='$TABLE' FORMAT TSVRaw"
"${CH[@]}" -q "SELECT count() FROM $DB.$TABLE"
