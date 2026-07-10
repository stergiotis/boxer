#!/usr/bin/env bash
#
# Load a Zürich-centred ADS-B slice — one day by default, or several — from
# ClickHouse's public adsb.exposed instance into a LOCAL clickhouse-server, for
# the play Map panel (ADR-0096). No file download, no parsing — the rows arrive
# pre-shaped via remoteSecure(), one INSERT per (day, UTC hour).
#
# Assumes a local clickhouse-server is already running with the default user and
# no password (native 9000 for this script, HTTP 8123 for play). It does NOT
# start one.
#
# Tunables (env):
#   ADSB_MIN_LAT ADSB_MAX_LAT ADSB_MIN_LON ADSB_MAX_LON  bbox (default: Zürich)
#   ADSB_DAY     single UTC day to load (YYYY-MM-DD; default: yesterday). Feeds
#                ADSB_DAYS when that is unset.
#   ADSB_DAYS    UTC days to load, space-separated (default: just ADSB_DAY) —
#                the multi-day knob. Every (day, hour) pair is a separate INSERT,
#                so this multiplies the load: N days × the ADSB_HOURS count.
#   ADSB_FROM    inclusive UTC date range (YYYY-MM-DD). When BOTH ADSB_FROM and
#   ADSB_TO      ADSB_TO are set they expand into ADSB_DAYS (e.g. a whole week),
#                overriding it.
#   ADSB_APPEND  1 = keep existing rows (skip the initial TRUNCATE) and add this
#                slice on top, accumulating across runs; 0 = clear first
#                (default). Re-loading an overlapping day+bbox while appending
#                duplicates rows (MergeTree does not dedupe) — clear, or don't
#                overlap.
#   ADSB_SRC     remote source table (default: planes_mercator_sample10, ~10%).
#                The public `website` user caps a query result at 1,048,576 rows,
#                so the ~0.8M-rows/day sample stays under it; full-resolution
#                planes_mercator (~8.4M/day here) only loads for a much narrower
#                bbox or a sub-day window. planes_mercator_sample100 is the
#                always-safe fallback for busy days or wide boxes.
#   ADSB_HOURS   UTC hours to load, space-separated (default: 0..23). The day is
#                pulled one hour per INSERT, so each chunk is ~1/24 of the day —
#                small enough to survive the idled instance's slow link and stay
#                under the row cap (this also makes full-res planes_mercator
#                viable). Set e.g. "10 11 12" for a quick partial load.
#   CH           clickhouse-client binary (default: clickhouse-client)
#   ADSB_VIEW_CENTER ADSB_VIEW_ZOOM  only the play map-view hint printed at the
#                end (default: Zürich, zoom 8); presets like switzerland.sh set
#                a country-wide view.
#
set -euo pipefail
here="$(cd "$(dirname "$(readlink -f "$0")")" && pwd)"

: "${CH:=clickhouse-client}"
: "${ADSB_MIN_LAT:=45.5}" ; : "${ADSB_MAX_LAT:=49.0}"
: "${ADSB_MIN_LON:=5.5}"  ; : "${ADSB_MAX_LON:=12.0}"
: "${ADSB_DAY:=$(date -u -d 'yesterday' +%F)}"
: "${ADSB_DAYS:=$ADSB_DAY}"
: "${ADSB_APPEND:=0}"
: "${ADSB_SRC:=planes_mercator_sample10}"
: "${ADSB_HOURS:=$(seq -s' ' 0 23)}"
# Map-view hint printed at the end (center "lat,lon" + zoom); default = Zürich.
: "${ADSB_VIEW_CENTER:=47.3769,8.5417}"
: "${ADSB_VIEW_ZOOM:=8}"

# A closed [ADSB_FROM, ADSB_TO] date range expands into ADSB_DAYS, overriding it.
if [ -n "${ADSB_FROM:-}" ] || [ -n "${ADSB_TO:-}" ]; then
  if [ -z "${ADSB_FROM:-}" ] || [ -z "${ADSB_TO:-}" ]; then
    echo "! set BOTH ADSB_FROM and ADSB_TO to load a date range" >&2; exit 2
  fi
  if [ "$(date -u -d "$ADSB_FROM" +%s)" -gt "$(date -u -d "$ADSB_TO" +%s)" ]; then
    echo "! ADSB_FROM ($ADSB_FROM) is after ADSB_TO ($ADSB_TO)" >&2; exit 2
  fi
  ADSB_DAYS=""; d="$ADSB_FROM"
  while [ "$(date -u -d "$d" +%s)" -le "$(date -u -d "$ADSB_TO" +%s)" ]; do
    ADSB_DAYS="${ADSB_DAYS} $d"
    d="$(date -u -d "$d + 1 day" +%F)"
  done
  ADSB_DAYS="${ADSB_DAYS# }"
fi

# The public staging instance idles to zero: the first connect is slow, and
# hedged requests race it to a premature 10s timeout. Disable hedging and widen
# the connect/receive windows so the cold-start wake-up (~30–60s) succeeds.
remote_flags=(
  --use_hedged_requests=0
  --connect_timeout_with_failover_secure_ms=60000
  --receive_timeout=180 --send_timeout=180
  # While the instance wakes, throughput is 0 rows/s; don't let the min-speed
  # guard abort the read before it spins up.
  --min_execution_speed=0 --timeout_before_checking_execution_speed=600
)

echo "· schema (idempotent) …"
"$CH" --multiquery < "$here/setup.sql"

# Wake the idled staging instance (and fail fast if it is unreachable) BEFORE we
# TRUNCATE, so a network problem never leaves the local tables empty.
echo "· waking the public instance (idled to zero — first connect ~30–60s) …"
"$CH" --progress "${remote_flags[@]}" --query \
  "SELECT 1 FROM remoteSecure('kvzqttvc2n.eu-west-1.aws.clickhouse-staging.com:9440', default.planes_mercator_sample100, 'website', '') LIMIT 1" >/dev/null

if [ "$ADSB_APPEND" = 1 ]; then
  echo "· append mode (ADSB_APPEND=1): keeping existing rows — no TRUNCATE"
else
  echo "· clearing any previous slice …"
  "$CH" --multiquery --query "
    TRUNCATE TABLE IF EXISTS planes_mercator;
    TRUNCATE TABLE IF EXISTS planes_mercator_sample10;
    TRUNCATE TABLE IF EXISTS planes_mercator_sample100;"
fi

# Optional lighter source (swap the remoteSecure table).
ingest_sql="$(cat "$here/ingest.sql")"
if [ "$ADSB_SRC" != "planes_mercator" ]; then
  ingest_sql="${ingest_sql//default.planes_mercator,/default.${ADSB_SRC},}"
fi

# Word-count the day/hour lists without clobbering the positional parameters.
read -ra _days <<<"$ADSB_DAYS"; read -ra _hours <<<"$ADSB_HOURS"
echo "· ingesting bbox [${ADSB_MIN_LAT},${ADSB_MIN_LON} .. ${ADSB_MAX_LAT},${ADSB_MAX_LON}] from ${ADSB_SRC}"
echo "  ${#_days[@]} day(s) [${ADSB_DAYS}] × ${#_hours[@]} hour(s) = one INSERT per (day, UTC hour);"
echo "  idled instance — first connect ~30–60s …"
failed=""
for day in ${ADSB_DAYS}; do
  for h in ${ADSB_HOURS}; do
    ok=0
    for attempt in 1 2 3; do
      if "$CH" --progress "${remote_flags[@]}" \
          --param_min_lat="$ADSB_MIN_LAT" --param_max_lat="$ADSB_MAX_LAT" \
          --param_min_lon="$ADSB_MIN_LON" --param_max_lon="$ADSB_MAX_LON" \
          --param_day="$day" --param_hour="$h" \
          --query "$ingest_sql"; then
        ok=1; break
      fi
      echo "  ${day} hour ${h}: attempt ${attempt} failed; retrying …" >&2
      sleep 3
    done
    if [ "$ok" -ne 1 ]; then
      echo "  ! ${day} hour ${h}: failed after 3 attempts — skipping" >&2
      failed="${failed} ${day}T${h}"
    fi
  done
done
[ -n "$failed" ] && echo "· WARNING: chunks failed:${failed} — slice is partial (re-run to retry)" >&2 || true

echo "· loaded:"
"$CH" --format PrettyCompact --query "
  SELECT * FROM (
    SELECT 'planes_mercator'            AS tbl, count() AS rows, uniqExact(icao) AS aircraft, min(date) AS first_day, max(date) AS last_day FROM planes_mercator
    UNION ALL SELECT 'planes_mercator_sample10',  count(), uniqExact(icao), min(date), max(date) FROM planes_mercator_sample10
    UNION ALL SELECT 'planes_mercator_sample100', count(), uniqExact(icao), min(date), max(date) FROM planes_mercator_sample100
  ) ORDER BY rows DESC"

cat <<EOF

Done. View it in play (its default endpoint is already http://localhost:8123/):

  SPINNAKER_PLAY_MAP_TABLE=planes_mercator \\
  SPINNAKER_PLAY_MAP_CENTER=${ADSB_VIEW_CENTER} \\
  SPINNAKER_PLAY_MAP_ZOOM=${ADSB_VIEW_ZOOM} \\
  <launch the play HMI>   # then open the Map panel, "no basemap" for offline

EOF
