#!/usr/bin/env bash
#
# Convenience preset: load a full week of ADS-B for the whole of Switzerland into
# the local ClickHouse, at full resolution — the recipe we settled on for the
# play Map panel. A thin wrapper over demo.sh: it only sets Swiss-national
# defaults (bbox, a rolling week, the full-res source) and hands off, so every
# demo.sh env knob (ADSB_HOURS, ADSB_APPEND, CH, …) still overrides.
#
# Defaults (all overridable):
#   bbox            lat 45.8 .. 47.85, lon 5.9 .. 10.55  (WGS84 national extent)
#   window          the last ADSB_WEEK_DAYS complete UTC days, ending yesterday
#   ADSB_WEEK_DAYS  length of that rolling window (default 7). Ignored if you set
#                   ADSB_FROM/ADSB_TO explicitly — those win.
#   ADSB_SRC        planes_mercator (full resolution; ~35 M rows for the week).
#                   Set planes_mercator_sample10 for a ~10× lighter/faster load.
#
# The public instance must actually hold that week. If a recent day comes back
# empty its data simply lags — shift the window back (e.g. ADSB_TO=<older day>)
# or shorten it; demo.sh treats an empty chunk as a no-op, not a failure.
#
# Examples:
#   apps/play/demo/adsb/switzerland.sh                        # last 7 days, full res
#   ADSB_WEEK_DAYS=14 apps/play/demo/adsb/switzerland.sh      # last two weeks
#   ADSB_SRC=planes_mercator_sample10 apps/play/demo/adsb/switzerland.sh
#   ADSB_APPEND=1 apps/play/demo/adsb/switzerland.sh          # accumulate, don't replace
#
set -euo pipefail
here="$(cd "$(dirname "$(readlink -f "$0")")" && pwd)"

: "${ADSB_WEEK_DAYS:=7}"
: "${ADSB_MIN_LAT:=45.8}" ; : "${ADSB_MAX_LAT:=47.85}"
: "${ADSB_MIN_LON:=5.9}"  ; : "${ADSB_MAX_LON:=10.55}"
: "${ADSB_SRC:=planes_mercator}"

# Rolling window: the last ADSB_WEEK_DAYS complete days, ending yesterday (UTC).
# The := only fills when unset, so an explicit ADSB_FROM/ADSB_TO still wins.
: "${ADSB_TO:=$(date -u -d 'yesterday' +%F)}"
: "${ADSB_FROM:=$(date -u -d "$ADSB_TO - $((ADSB_WEEK_DAYS - 1)) days" +%F)}"

# Country-wide map-view hint for demo.sh's closing note (Alpine centre, zoomed out).
: "${ADSB_VIEW_CENTER:=46.8,8.23}"
: "${ADSB_VIEW_ZOOM:=7}"

echo "· Switzerland preset — ${ADSB_WEEK_DAYS} day(s) ${ADSB_FROM}..${ADSB_TO}, src=${ADSB_SRC}"
export ADSB_MIN_LAT ADSB_MAX_LAT ADSB_MIN_LON ADSB_MAX_LON \
       ADSB_SRC ADSB_FROM ADSB_TO ADSB_VIEW_CENTER ADSB_VIEW_ZOOM
exec "$here/demo.sh"
