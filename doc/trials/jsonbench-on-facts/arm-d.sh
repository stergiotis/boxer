#!/usr/bin/env bash
#
# Arm D — arm B plus the five benchmark backbone fields exposed as ClickHouse
# MATERIALIZED columns, built by cloning arm B so the two arms hold identical
# data under an identical sort key and only the read path differs.
#
# The materialised expression is *exactly* what queries-facts.sql evaluates
# inline: reconstruct the per-attribute path vector from `lmrcard`, and for
# array-valued sections the per-attribute value vector from `len`, then pick
# the attribute at the wanted path. Moving it into a MATERIALIZED column runs
# it once per part at merge time instead of once per row per query.
#
# This arm is not in the protocol's §4 table. It was added after the first run
# showed the path reconstruction — not the storage model — dominating latency
# and memory. Note it does NOT re-key: arm D is still ORDER BY ts and still
# prunes nothing. Re-keying is a separate, still-untested lever.
#
# Usage: DB_SRC=jsonbench_b_10m DB=jsonbench_d_10m ./arm-d.sh

set -euo pipefail

DB_SRC="${DB_SRC:-jsonbench_b_1m}"
DB="${DB:-jsonbench_d_1m}"
TABLE="${TABLE:-facts}"
CH=(clickhouse-client --allow_suspicious_low_cardinality_types=1)

SV='tv:symbol:value:val:s:m:0:24:0::data'
SP='tv:symbol:mrhp:mrhp:y:g:0:0:0::data'
SC='tv:symbol:lmrcard:lmrcard:u64:4gw:0:0:0::data'
TV='tv:stringArray:value:val:sh:g:0:x2:0::data'
TP='tv:stringArray:mrhp:mrhp:y:g:0:0:0::data'
TC='tv:stringArray:lmrcard:lmrcard:u64:4gw:0:0:0::data'
TL='tv:stringArray:len:len:u64:28o:0:0:0::data'
IV='tv:i64Array:value:val:i64h:g:0:0:0::data'
IP='tv:i64Array:mrhp:mrhp:y:g:0:0:0::data'
IC='tv:i64Array:lmrcard:lmrcard:u64:4gw:0:0:0::data'
IL='tv:i64Array:len:len:u64:28o:0:0:0::data'

# Per-attribute path vectors (membership half) and value vectors (value half).
symPaths="arrayMap((c,s)->if(c=0,'',\`$SP\`[toUInt32(s-c+1)]),\`$SC\`,arrayCumSum(\`$SC\`))"
strPaths="arrayMap((c,s)->if(c=0,'',\`$TP\`[toUInt32(s-c+1)]),\`$TC\`,arrayCumSum(\`$TC\`))"
strScal="arrayMap((l,s)->if(l=0,'',\`$TV\`[toUInt32(s-l+1)]),\`$TL\`,arrayCumSum(\`$TL\`))"
intPaths="arrayMap((c,s)->if(c=0,'',\`$IP\`[toUInt32(s-c+1)]),\`$IC\`,arrayCumSum(\`$IC\`))"
intScal="arrayMap((l,s)->if(l=0,toInt64(0),\`$IV\`[toUInt32(s-l+1)]),\`$IL\`,arrayCumSum(\`$IL\`))"

"${CH[@]}" -q "DROP DATABASE IF EXISTS $DB"
"${CH[@]}" -q "CREATE DATABASE $DB"
"${CH[@]}" -q "CREATE TABLE $DB.$TABLE AS $DB_SRC.$TABLE"
"${CH[@]}" -q "INSERT INTO $DB.$TABLE SELECT * FROM $DB_SRC.$TABLE"

"${CH[@]}" -q "ALTER TABLE $DB.$TABLE
  ADD COLUMN kind              LowCardinality(String) MATERIALIZED arrayFirst((v,p)->p='/kind',\`$SV\`,$symPaths),
  ADD COLUMN commit_operation  LowCardinality(String) MATERIALIZED arrayFirst((v,p)->p='/commit/operation',\`$SV\`,$symPaths),
  ADD COLUMN commit_collection LowCardinality(String) MATERIALIZED arrayFirst((v,p)->p='/commit/collection',\`$SV\`,$symPaths),
  ADD COLUMN did               String                 MATERIALIZED arrayFirst((v,p)->p='/did',$strScal,$strPaths),
  ADD COLUMN time_us           DateTime64(6)          MATERIALIZED fromUnixTimestamp64Micro(arrayFirst((v,p)->p='/time_us',$intScal,$intPaths))"

for c in kind commit_operation commit_collection did time_us; do
  "${CH[@]}" -q "ALTER TABLE $DB.$TABLE MATERIALIZE COLUMN $c" --mutations_sync=2
done

"${CH[@]}" -q "SELECT name, formatReadableSize(sum(data_compressed_bytes)) AS size
  FROM system.columns WHERE database='$DB' AND table='$TABLE'
   AND name IN ('kind','commit_operation','commit_collection','did','time_us')
  GROUP BY name ORDER BY sum(data_compressed_bytes) DESC FORMAT TSVWithNames"
