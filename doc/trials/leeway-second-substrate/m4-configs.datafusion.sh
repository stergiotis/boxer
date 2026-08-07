#!/usr/bin/env bash
#
# Committed as the recipe behind m4-datafusion-configs.tsv. The in-memory
# configuration must exclude id:blake3hash — DataFusion refuses to materialise
# the ClickHouse-written Parquet's Utf8-declared digest column (see the logbook).
# Paths are parameterised at the top; the corpus itself is not committed.
# Three DataFusion configurations over the same 10M documents, eight queries.
set -uo pipefail
F="${DATAFUSION:-$HOME/.local/bin/datafusion-cli}"
# CORPUS: the M2 run's corpus directory. WORK: scratch holding nstruct.parquet.
C="${CORPUS:?set CORPUS to the M2 corpus directory}"
W="${WORK:?set WORK to a disk-backed scratch directory}"
OUT="${1:-$W/df.tsv}"
: > "$OUT"

LWX="CREATE EXTERNAL TABLE lwx STORED AS PARQUET LOCATION '$C/packed.nobloom.parquet';"
NSX="CREATE EXTERNAL TABLE nsx STORED AS PARQUET LOCATION '$W/nstruct.parquet';"
MEM="$LWX CREATE TABLE lw AS SELECT * FROM lwx;"

SV='"tv:symbol:value:val:s:m:0:0:0::"'; SP='"tv:symbol:lmv:lmv:y:m:0:0:0::"'
ST='"tv:string:value:val:s:g:0:0:0::"'; STP='"tv:string:lmv:lmv:y:m:0:0:0::"'
IV='"tv:int64:value:val:i64:4o:0:0:0::"'; IP='"tv:int64:lmv:lmv:y:m:0:0:0::"'
g() { printf "coalesce(array_element(%s, CAST(array_position(%s, '%s') AS BIGINT)), %s)" "$1" "$2" "$3" "$4"; }
COLL=$(g "$SV" "$SP" '/commit/collection' "''")
KIND=$(g "$SV" "$SP" '/kind' "''")
OPER=$(g "$SV" "$SP" '/commit/operation' "''")
DID=$(g  "$ST" "$STP" '/did' "''")
TS=$(g   "$IV" "$IP" '/time_us' "0")
RTYPE=$(g "$SV" "$SP" '/commit/record/$type' "''")

# leeway-shaped queries, parameterised on the table name
lw_q() { local T="$1"; cat <<EOF
SELECT $COLL e, count(*) c FROM $T GROUP BY e ORDER BY c DESC, e
@SELECT $COLL e, count(*) c, count(DISTINCT $DID) u FROM $T WHERE $KIND='commit' AND $OPER='create' GROUP BY e ORDER BY c DESC, e
@SELECT $COLL e, date_part('hour', to_timestamp_micros($TS)) h, count(*) c FROM $T WHERE $KIND='commit' AND $OPER='create' AND $COLL IN ('app.bsky.feed.post','app.bsky.feed.repost','app.bsky.feed.like') GROUP BY e, h ORDER BY h, e
@SELECT $DID u, min(to_timestamp_micros($TS)) f FROM $T WHERE $KIND='commit' AND $OPER='create' AND $COLL='app.bsky.feed.post' GROUP BY u ORDER BY f ASC, u LIMIT 3
@SELECT $DID u, max($TS)/1000 - min($TS)/1000 s FROM $T WHERE $KIND='commit' AND $OPER='create' AND $COLL='app.bsky.feed.post' GROUP BY u ORDER BY s DESC, u LIMIT 3
@SELECT count(*) FROM $T WHERE array_has($STP, '/commit/record/text')
@SELECT $RTYPE t, count(*) c FROM $T GROUP BY t ORDER BY c DESC, t LIMIT 5
@SELECT count(*) FROM $T WHERE array_position($STP, '/commit/record/createdAt') IS NOT NULL
EOF
}
ns_q() { cat <<'EOF'
SELECT coalesce(commit['collection'],'') e, count(*) c FROM nsx GROUP BY e ORDER BY c DESC, e
@SELECT coalesce(commit['collection'],'') e, count(*) c, count(DISTINCT did) u FROM nsx WHERE kind='commit' AND commit['operation']='create' GROUP BY e ORDER BY c DESC, e
@SELECT coalesce(commit['collection'],'') e, date_part('hour', to_timestamp_micros(time_us)) h, count(*) c FROM nsx WHERE kind='commit' AND commit['operation']='create' AND commit['collection'] IN ('app.bsky.feed.post','app.bsky.feed.repost','app.bsky.feed.like') GROUP BY e, h ORDER BY h, e
@SELECT did u, min(to_timestamp_micros(time_us)) f FROM nsx WHERE kind='commit' AND commit['operation']='create' AND commit['collection']='app.bsky.feed.post' GROUP BY u ORDER BY f ASC, u LIMIT 3
@SELECT did u, max(time_us)/1000 - min(time_us)/1000 s FROM nsx WHERE kind='commit' AND commit['operation']='create' AND commit['collection']='app.bsky.feed.post' GROUP BY u ORDER BY s DESC, u LIMIT 3
@SELECT count(*) FROM nsx WHERE map_extract(commit['record'],'text')[1] IS NOT NULL
@SELECT map_extract(commit['record'],'$type')[1] t, count(*) c FROM nsx GROUP BY t ORDER BY c DESC, t LIMIT 5
@SELECT count(*) FROM nsx WHERE map_extract(commit['record'],'createdAt')[1] IS NOT NULL
EOF
}

bench() { # label, prelude, query-stream
  local label="$1" prelude="$2" n=0
  while IFS= read -r -d '@' q || [ -n "${q:-}" ]; do
    [ -z "${q//[[:space:]]/}" ] && continue
    n=$((n+1)); local best=999
    for i in 1 2 3; do
      local m; m=$(mktemp)
      /usr/bin/time -f "%e %M" "$F" --format tsv -q -c "$prelude $q" >/dev/null 2>"$m"
      read -r secs rss < <(grep -E '^[0-9.]+ [0-9]+$' "$m" | tail -1)
      [ "$i" -ge 2 ] && best=$(awk -v a="${secs:-999}" -v b="$best" 'BEGIN{print (a<b)?a:b}')
      rm -f "$m"
    done
    printf '%s\t%d\t%s\n' "$label" "$n" "$best" >> "$OUT"
  done
}

cd "$W"
lw_q lwx | bench df_parquet "$LWX"
lw_q lw  | bench df_memory  "$MEM"
ns_q     | bench df_struct  "$NSX"
column -t "$OUT"
