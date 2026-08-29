#!/usr/bin/env bash
#
# Convenience preset: the switzerland.sh recipe widened to cover Germany as
# well — a rolling week at full resolution for Switzerland ∪ Germany, into the
# local ClickHouse. Like switzerland.sh it is a wrapper over demo.sh, so every
# demo.sh env knob (ADSB_HOURS, ADSB_APPEND, CH, …) still overrides; the two
# things it adds are a TILING of the region and a little concurrency.
#
# Why tiles: the public `website` user caps one query result at 1,048,576 rows,
# and demo.sh's unit of work is one bbox × one UTC hour. The whole region at a
# Saturday-morning peak (2026-08-22 08:00 UTC) held 2.46 M full-res rows in one
# hour — 2.3× the cap — so a single bbox cannot load at full resolution. The
# tiles below are disjoint rectangles whose union is the Swiss bbox ∪ the German
# bbox; each held ≤ ~310 k rows in that peak hour (~3× headroom for busier
# weekdays). Per-cell counts that sized them, in k rows/hour: Zürich 152,
# Munich 105, Geneva 84, Frankfurt 72. Re-probe before widening a tile.
#
# The first tile runs alone, in demo.sh's default replace mode (schema, wake the
# remote, TRUNCATE, load); the remaining tiles then run ADSB_PARALLEL at a time
# in append mode. Each tile's demo.sh output goes to its own log under
# ADSB_LOG_DIR; this script prints one line per tile plus a final summary.
#
# Defaults (all overridable):
#   region          lat 45.8 .. 55.06, lon 5.87 .. 15.04 minus what is in neither
#                   country's bbox (the strip south of 47.85 east of 10.55 is kept
#                   — it is Bavaria's Alpine edge; a 0.03°-wide sliver of France
#                   west of 5.9 between 47.27 and 47.85 is dropped). ~7× the Swiss
#                   preset's area and traffic.
#   window          the last ADSB_WEEK_DAYS complete UTC days, ending yesterday
#   ADSB_WEEK_DAYS  length of that rolling window (default 7). Ignored if you set
#                   ADSB_FROM/ADSB_TO explicitly — those win — and left alone
#                   when ADSB_DAYS is set, so a single day/hour can be re-run.
#   ADSB_SRC        planes_mercator (full resolution). One run of this region
#                   loaded 30–38 M rows/day — 226 M rows and ~22 GiB on disk
#                   (all three tables) for the week, in 51 min at ADSB_PARALLEL=3.
#                   planes_mercator_sample10 is ~10× lighter.
#   ADSB_PARALLEL   tiles loaded concurrently after the first (default 3). The
#                   remote is a shared public instance — keep this small.
#   ADSB_TILES      space-separated tile indexes (1-based, see the list below) to
#                   load instead of all; with ADSB_APPEND=1 ADSB_DAYS=… ADSB_HOURS=…
#                   this re-runs a chunk that demo.sh reported as failed.
#   ADSB_LOG_DIR    where the per-tile logs go (default: a fresh mktemp dir).
#
# Rows exactly on a shared tile edge (the same mercator unit — ~1e-7 of rows)
# are loaded twice, because ingest.sql's BETWEEN is inclusive on both ends.
# Negligible for the raster; noted so nobody hunts for it.
#
# Examples:
#   apps/play/demo/adsb/switzerland-germany.sh                    # last 7 days, full res
#   ADSB_SRC=planes_mercator_sample10 apps/play/demo/adsb/switzerland-germany.sh
#   ADSB_PARALLEL=1 apps/play/demo/adsb/switzerland-germany.sh    # serial, gentler
#   ADSB_APPEND=1 ADSB_TILES=4 ADSB_DAYS=2026-08-21 ADSB_HOURS="9" \
#     apps/play/demo/adsb/switzerland-germany.sh                  # redo one failed chunk
#
set -euo pipefail
here="$(cd "$(dirname "$(readlink -f "$0")")" && pwd)"

: "${CH:=clickhouse client}"
read -r -a ch <<< "$CH"
: "${ADSB_WEEK_DAYS:=7}"
: "${ADSB_SRC:=planes_mercator}"
: "${ADSB_APPEND:=0}"
: "${ADSB_PARALLEL:=3}"
: "${ADSB_LOG_DIR:=$(mktemp -d -t adsb-ch-de.XXXXXX)}"

# Rolling window as in switzerland.sh — unless ADSB_DAYS names the days itself
# (demo.sh would let an ADSB_FROM/ADSB_TO range override ADSB_DAYS).
if [ -z "${ADSB_DAYS:-}" ]; then
  : "${ADSB_TO:=$(date -u -d 'yesterday' +%F)}"
  : "${ADSB_FROM:=$(date -u -d "$ADSB_TO - $((ADSB_WEEK_DAYS - 1)) days" +%F)}"
fi

# Region-wide map-view hint for the closing note (roughly central Germany, zoomed
# out enough to hold the Alps and the coast).
: "${ADSB_VIEW_CENTER:=50.4,10.4}"
: "${ADSB_VIEW_ZOOM:=6}"

# Tiles: "min_lat max_lat min_lon max_lon  label". Disjoint; union = CH ∪ DE bbox
# (see the header). Edges: 47.85 is the Swiss preset's north edge, 10.55 its east
# edge; 8.25 / 9.0 / 49.0 / 51.0 / 53.0 split by the measured density.
tiles=(
  "45.80 47.85  5.90  8.25  CH west (Geneva, Bern, Basel)"
  "45.80 47.85  8.25 10.55  CH east (Zürich, Graubünden)"
  "47.85 49.00  5.87 10.55  DE Baden-Württemberg"
  "47.27 49.00 10.55 15.04  DE Bavaria + Alpine strip"
  "49.00 51.00  5.87  9.00  DE Rhineland-Palatinate, Saarland, Hesse west"
  "49.00 51.00  9.00 15.04  DE Hesse east, Thuringia, Saxony"
  "51.00 53.00  5.87  9.00  DE North Rhine-Westphalia, Lower Saxony west"
  "51.00 53.00  9.00 15.04  DE Lower Saxony east, Berlin, Saxony-Anhalt"
  "53.00 55.06  5.87 15.04  DE north (Hamburg, Schleswig-Holstein, coast)"
)
: "${ADSB_TILES:=$(seq -s' ' 1 ${#tiles[@]})}"
read -r -a sel <<< "$ADSB_TILES"

tile_label() { local _a _b _c _d label; read -r _a _b _c _d label <<< "${tiles[$1-1]}"; echo "$label"; }

run_tile() {  # $1 = 1-based index, $2 = append flag → runs demo.sh, logs to a file
  local i="$1" append="$2" log="$ADSB_LOG_DIR/tile-$1.log" min_lat max_lat min_lon max_lon label
  read -r min_lat max_lat min_lon max_lon label <<< "${tiles[i-1]}"
  if ADSB_MIN_LAT="$min_lat" ADSB_MAX_LAT="$max_lat" ADSB_MIN_LON="$min_lon" ADSB_MAX_LON="$max_lon" \
     ADSB_APPEND="$append" ADSB_SRC="$ADSB_SRC" ADSB_FROM="${ADSB_FROM:-}" ADSB_TO="${ADSB_TO:-}" \
     "$here/demo.sh" > "$log" 2>&1; then
    echo "  ✓ tile $i done: $label"
  else
    echo "  ✗ tile $i FAILED (exit $?) — see $log" >&2
    echo "$i" >> "$ADSB_LOG_DIR/failed-tiles"
  fi
}

echo "· Switzerland ∪ Germany preset — ${ADSB_FROM:-days: $ADSB_DAYS}${ADSB_TO:+..$ADSB_TO}, src=${ADSB_SRC}"
echo "  ${#sel[@]} tile(s) [${ADSB_TILES}], ${ADSB_PARALLEL} at a time after the first; logs in ${ADSB_LOG_DIR}"

# Tile #1 of the selection runs alone: in replace mode it is the one that wakes
# the remote and TRUNCATEs, and the appending tiles must not start before that.
first="${sel[0]}"; mode=replace; [ "$ADSB_APPEND" = 1 ] && mode=append
echo "· tile $first ($mode mode, alone): $(tile_label "$first") …"
run_tile "$first" "$ADSB_APPEND"
if [ -e "$ADSB_LOG_DIR/failed-tiles" ]; then
  echo "! the first tile failed — not starting the others (remote unreachable?)" >&2
  tail -n 5 "$ADSB_LOG_DIR/tile-$first.log" >&2; exit 1
fi

if [ "${#sel[@]}" -gt 1 ]; then
  echo "· remaining tiles, append mode, ${ADSB_PARALLEL} concurrent …"
  running=0
  for i in "${sel[@]:1}"; do
    if [ "$running" -ge "$ADSB_PARALLEL" ]; then wait -n; running=$((running - 1)); fi
    echo "  → tile $i: $(tile_label "$i")"
    run_tile "$i" 1 &
    running=$((running + 1))
  done
  wait
fi

# demo.sh exits 0 on a partially loaded slice and only warns; surface those.
if grep -q 'WARNING: chunks failed' "$ADSB_LOG_DIR"/tile-*.log 2>/dev/null; then
  echo "· WARNING: some tiles have failed chunks — re-run each with ADSB_APPEND=1 ADSB_TILES=<i> ADSB_DAYS=<day> ADSB_HOURS=<h>:" >&2
  grep -H 'WARNING: chunks failed' "$ADSB_LOG_DIR"/tile-*.log >&2 || true
fi
if [ -e "$ADSB_LOG_DIR/failed-tiles" ]; then
  echo "· WARNING: tiles that failed outright: $(tr '\n' ' ' < "$ADSB_LOG_DIR/failed-tiles")" >&2
fi

echo "· loaded:"
"${ch[@]}" --format PrettyCompact --query "
  SELECT * FROM (
    SELECT 'planes_mercator'            AS tbl, count() AS rows, uniqExact(icao) AS aircraft, min(date) AS first_day, max(date) AS last_day FROM planes_mercator
    UNION ALL SELECT 'planes_mercator_sample10',  count(), uniqExact(icao), min(date), max(date) FROM planes_mercator_sample10
    UNION ALL SELECT 'planes_mercator_sample100', count(), uniqExact(icao), min(date), max(date) FROM planes_mercator_sample100
  ) ORDER BY rows DESC"

cat <<EOT

Done. View it in play (its default endpoint is already http://localhost:8123/):

  BOXER_PLAY_MAP_TABLE=planes_mercator \\
  BOXER_PLAY_MAP_CENTER=${ADSB_VIEW_CENTER} \\
  BOXER_PLAY_MAP_ZOOM=${ADSB_VIEW_ZOOM} \\
  <launch the play HMI>   # then open the Map panel, "no basemap" for offline

EOT
