#!/bin/bash
# play-screenshot-tour.sh — capture a gallery of the play app's feature surface.
#
# One play launch per scene, through the HEADLESS host (ADR-0024): it links no
# window system, so there is no compositor anywhere in this script. The render
# host writes the PNGs itself, on request, and `imzero2 drive` decides when
# (ADR-0154).
#
# Each scene combines two things:
#
#   * launch state, seeded by the BOXER_PLAY_* knobs (ADR-0009 registry; see
#     doc/env-vars.md) — the SQL buffer, auto-run, which body tab is active,
#     the Map centre and zoom, the window size;
#   * an optional TRACE of gestures, replayed by `imzero2 drive` against the
#     accessibility tree — widgets resolved by name and actuated by node id,
#     so a menu, a toggle or a row click is reachable too.
#
# Every scene ends in at least one `capture` step, which is what writes the PNG.
#
# Usage:
#   scripts/dev/play-screenshot-tour.sh                 # every scene
#   scripts/dev/play-screenshot-tour.sh map timeline    # scenes matching a pattern
#   PLAYSHOT_OUT=/tmp/gallery scripts/dev/play-screenshot-tour.sh
#   PLAYSHOT_LIST=1 scripts/dev/play-screenshot-tour.sh # list scenes, capture none
#   PLAYSHOT_DRY=1  scripts/dev/play-screenshot-tour.sh # resolve anchors, capture nothing
#
# Prerequisites:
#   - a ClickHouse on $CLICKHOUSE_URL (default http://localhost:8123/)
#   - the demo fixtures the scenes read — see fixtures() below, and
#     `PLAYSHOT_CHECK_FIXTURES=0` to run anyway with the misses rendering as
#     error states (which are themselves worth a screenshot)
#   - no display, no compositor, no browser
set -uo pipefail

here=$(dirname "$(readlink -f "$BASH_SOURCE")")
root=$(cd "$here/../.." && pwd)

# ---------------------------------------------------------------- configuration

OUT="${PLAYSHOT_OUT:-$root/tmp/play-tour}"
SIZE="${PLAYSHOT_SIZE:-1920x1200}"
TIMEOUT="${PLAYSHOT_TIMEOUT:-120}"         # per-scene wall clock, seconds
SETTLE_MS="${PLAYSHOT_SETTLE_MS:-350}"     # settle after a step that sets none
# Hold before a scene's first capture, for the query and the panel to settle.
# Scenes with an async panel (a map raster, a projection) set their own.
SCENE_SETTLE_MS="${PLAYSHOT_SCENE_SETTLE_MS:-1800}"
# Build a PRIVATE binary pair by default. The shared rust/imzero2 binaries are
# rebuilt by whoever is working in the tree, and a Go host paired with a Rust
# client from a different egui2 codegen desyncs the FFFI wire mid-frame ("FFFI
# wire desync: opcode … does not decode to a known FuncProcId") — every scene
# then captures a broken window.
#
# PLAYSHOT_BUILD=0 does NOT mean "reuse the private pair": it switches BOTH
# binaries to the SHARED rust/imzero2/ ones, which are whatever someone last
# built there. So a run with it captures code that may be hours older than the
# working tree, silently — a change under test simply does not appear, and the
# capture looks like the feature is broken rather than absent. Use it only to
# re-drive a trace against a build you know is current.
BUILD="${PLAYSHOT_BUILD:-1}"
BIN="${PLAYSHOT_BIN:-$OUT/bin}"
CHECK_FIXTURES="${PLAYSHOT_CHECK_FIXTURES:-1}"
DRY="${PLAYSHOT_DRY:-}"
# Carrier port. Each scene binds it, drives, and exits; loopback only, since
# the host refuses a non-loopback bind without the auth and TLS of ADR-0082.
PORT="${PLAYSHOT_PORT:-8793}"
# Software H.264 by default: the carrier spawns an encoder for its video lane
# even though this tour never decodes a frame, and the software encoder is the
# one that needs no VAAPI driver on the box.
export IMZERO2_HEADLESS_ENCODER_ARGS="${IMZERO2_HEADLESS_ENCODER_ARGS:--c:v libopenh264 -rc_mode off -bf 0 -g 100000}"

export CLICKHOUSE_URL="${CLICKHOUSE_URL:-http://localhost:8123/}"
export CLICKHOUSE_USER="${CLICKHOUSE_USER:-default}"

# =============================================================================
# Scenes
# =============================================================================
# One function per scene, named scene_<NN>_<slug>. It sets:
#   desc  — one line for the generated index
#   sql   — the editor buffer (BOXER_PLAY_SQL)
#   senv  — extra environment, as NAME=VALUE words
#   steps — optional trace (JSON Lines) replacing the default single capture;
#           a scene that sets it must capture for itself
#   settle— optional milliseconds to hold before the scene's steps, for a panel
#           whose data arrives asynchronously
#
# Scene order is the function name's sort order. Add a scene by adding a
# function; nothing else needs touching.

scene_01_table_leeway() {
	desc="Table — a leeway-encoded result: backtick handles resolve to physical column names, typed cells, the row grid"
	senv=(BOXER_PLAY_FOCUS_TABLE=1)
	sql="SELECT \`id:id\`, \`symbol:value\`, \`text:text\`, \`geoPoint:pointLat\`, \`geoPoint:pointLng\`, \`timeRange:beginIncl\`
FROM anchor.facts
ORDER BY \`id:id\`
LIMIT 200"
}

scene_02_table_adhoc() {
	desc="Table — an ordinary aggregate result (the non-leeway path): plain grid, column headers, row count in the status bar"
	senv=(BOXER_PLAY_FOCUS_TABLE=1)
	sql="SELECT t AS aircraft, count() AS positions, round(avg(altitude)) AS avg_altitude, max(ground_speed) AS top_speed
FROM default.planes_mercator_sample100
WHERE t != ''
GROUP BY t
ORDER BY positions DESC
LIMIT 40"
}

scene_02_table_glosses() {
	desc="Table — glosses (ADR-0186): the gloss(…) macro and explicit \`label@gloss/…\` aliases per column, a \`-- play: gloss\` directive binding a plain column by name, a Luhn ✓/✗ tone, a masked secret, and a mistyped parameter refused out loud"
	# Table-free, so it needs no fixture. The last column carries a typo in
	# its parameter name (unti=C) on purpose: it must render plain with the
	# refusal on the header hover, never as a temperature. The first glossed
	# column goes through the gloss(…) macro (§SD7), the rest through aliases.
	senv=(BOXER_PLAY_FOCUS_TABLE=1)
	sql="-- play: gloss gloss/length;unit=m name:height
SELECT number AS n,
       gloss(20 + number * 1.7, 'gloss/temperature', 'unit', 'C', 'label', 'temp'),
       1.5 + number * 0.31 AS height,
       ['4111111111111111', '4111111111111112', '378282246310005'][1 + number % 3] AS \`card@gloss/luhn\`,
       1024 * number * number AS \`size@gloss/bytes\`,
       'hunter2' AS \`pw@gloss/masked\`,
       toUnixTimestamp(now()) + number * 86400 AS \`when@gloss/epoch\`,
       number * 90500 AS \`took@gloss/duration;unit=ms\`,
       'https://example.com/' || toString(number) AS \`link@gloss/url\`,
       20 + number * 1.7 AS \`oops@gloss/temperature;unti=C\`
FROM numbers(12)"
}

scene_03_detail_card() {
	desc="Detail — the leeway entity card for a single row: the plain id section, every tagged section, membership chips"
	# Detail claims its channel from the `selection` signal, so a scripted
	# capture needs a selected row: BOXER_PLAY_OBSERVE cannot set it, but a
	# pinned `SET param_selection` puts the value in the same signal env the
	# panel reads. Row 0 of a one-row result is the whole result.
	senv=(BOXER_PLAY_FOCUS_TABLE=1)
	sql="SET param_selection = 0;
SELECT * FROM anchor.facts WHERE \`id:id\` = 10005"
}

scene_03_detail_glosses() {
	desc="Detail + Glosses — the rule route on a leeway result (ADR-0186): \`-- play: gloss\` lines bind glosses to physical columns by their spec line, the leeway card and both grids render the faces, and the Glosses tab shows the catalog, the rules and each column's resolution"
	senv=(BOXER_PLAY_FOCUS_TABLE=1 BOXER_PLAY_FOCUS_GLOSSES=1)
	sql="-- play: gloss gloss/masked name:text section:text
-- play: gloss gloss/bytes name:wordLength
SET param_selection = 0;
SELECT \`id:id\`, \`symbol:value\`, \`text:text\`, \`text:wordLength\`, \`geoPoint:pointLat\`
FROM anchor.facts
ORDER BY \`id:id\`
LIMIT 20"
	# Two captures: the per-DB-row grid (a masked list cell, a byte count that
	# does not apply to a whole list), then the per-attribute grid, where the
	# same rules reach the items.
	steps='{"do":"capture","text":"03_detail_glosses","settleMs":600}
{"do":"click","name":"per attribute"}
{"do":"capture","text":"03_detail_glosses_per_attribute","settleMs":600}'
	settle=1800
}

scene_04_timeline() {
	desc="Timeline — the interval contract (_tl_time + _tl_time_end + _tl_lane) drawn as lanes, with a background bands channel"
	# The events shape is Intervals, so NO _tl_label: _tl_time_end and
	# _tl_label select mutually exclusive modes, and returning both is a
	# contract reject ("Ambiguous: remove …"), which is what the pane draws
	# instead of a timeline. Bands ride their own _tl_band_* slots — the
	# events slot names do not carry over — and are sized off the
	# {tl_min}/{tl_max} extent the events render publishes, so the shading
	# lands in view whatever the data's era.
	senv=(
		BOXER_PLAY_FOCUS_TIMELINE=1
		"BOXER_PLAY_TIMELINE_BANDS_SQL=WITH {tl_min:DateTime64(3, 'UTC')} AS lo,
     {tl_max:DateTime64(3, 'UTC')} AS hi
SELECT lo                                                                AS _tl_band_from,
       addMilliseconds(lo, toInt64(0.5 * dateDiff('millisecond', lo, hi))) AS _tl_band_to,
       'info.subtle'                                                     AS _tl_band_color,
       'first half'                                                      AS _tl_band_label
UNION ALL
SELECT addMilliseconds(lo, toInt64(0.5 * dateDiff('millisecond', lo, hi))),
       hi,
       'accent.subtle',
       'second half'"
	)
	sql="SELECT
  \`timeRange:beginIncl\`[1] AS _tl_time,
  \`timeRange:endExcl\`[1]   AS _tl_time_end,
  \`symbol:value\`[1]        AS _tl_lane
FROM anchor.facts
WHERE length(\`timeRange:beginIncl\`) > 0
ORDER BY _tl_time"
}

scene_05_map_raster() {
	desc="Map — an in-database mercator raster (ADR-0096) over 350k ADS-B positions, rendered server-side and panned/zoomed client-side"
	senv=(
		BOXER_PLAY_FOCUS_MAP=1
		BOXER_PLAY_MAP_TABLE=planes_mercator_sample100
		BOXER_PLAY_MAP_CENTER=47.4,8.5
		BOXER_PLAY_MAP_ZOOM=7
	)
	sql="SELECT icao, r, t, lat, lon, altitude, ground_speed
FROM default.planes_mercator_sample100
LIMIT 500"
	settle=4000
}

scene_06_world_choropleth() {
	desc="World — the country choropleth (ADR-0114): a string column detected as country-shaped, values binned onto the atlas"
	senv=(BOXER_PLAY_FOCUS_WORLD=1)
	sql="SELECT country, count() AS flights, round(avg(altitude)) AS avg_alt
FROM (
  SELECT multiIf(lon < 6, 'FR', lon < 10, 'CH', lon < 14, 'AT', lon < 20, 'IT', 'DE') AS country, altitude
  FROM default.planes_mercator_sample100
  WHERE altitude > 0
  LIMIT 200000
)
GROUP BY country
ORDER BY flights DESC"
	settle=3000
}

scene_07_kanban() {
	desc="Kanban — the result as a board (ADR-0122): the lane/title columns by name, an optional lanes node fixing the column order"
	senv=(BOXER_PLAY_FOCUS_KANBAN=1)
	sql="WITH lanes AS (
  SELECT arrayJoin(['A320', 'B738', 'A20N']) AS lane
)
SELECT t AS lane, r AS title, any(\`desc\`) AS subtitle
FROM default.planes_mercator_sample100
WHERE t IN ('A320', 'B738', 'A20N') AND r != ''
GROUP BY t, r
ORDER BY lane, title
LIMIT 20 BY lane"
}

scene_08_network() {
	desc="Network — the result as a layered node-link graph (ADR-0129): an edges channel, plus an optional vertices node carrying labels and groups"
	senv=(BOXER_PLAY_FOCUS_NETWORK=1)
	# Operator → aircraft-type is bipartite, which is what a LAYERED graph
	# wants: two ranks, edges only between them. The registered-owner field
	# is US-only in this fixture, so a handful of operators keeps the node
	# count legible. Derive the vertices FROM the edge set, not from the
	# whole table: a vertex the edges never mention is an isolated node, and
	# enough of them collapse the layout into a single squashed rank.
	sql="WITH
  top_ops AS (
    SELECT ownOp
    FROM default.planes_mercator_sample100
    WHERE ownOp != '' AND t != ''
    GROUP BY ownOp
    ORDER BY uniq(icao) DESC
    LIMIT 6
  ),
  fleets AS (
    SELECT ownOp, t
    FROM default.planes_mercator_sample100
    WHERE t != '' AND ownOp IN (SELECT ownOp FROM top_ops)
    GROUP BY ownOp, t
  ),
  vertices AS (
    SELECT DISTINCT ownOp AS id, ownOp AS label, 'operator' AS \`group\` FROM fleets
    UNION ALL
    SELECT DISTINCT t, t, 'aircraft type' FROM fleets
  ),
  edges AS (
    SELECT ownOp AS source, t AS target FROM fleets
  )
SELECT * FROM edges"
	settle=3000
}

# Shares 08 with the Network scene rather than renumbering the tail: order is
# the function name's sort order, so this lands immediately after it, which is
# where the other result-shape panel belongs.
scene_08_sankey() {
	desc="Sankey — the result as a flow-quantity diagram (ADR-0159): a required flows channel carrying source/target/value, plus an optional nodes channel whose stage column selects the alluvial reading"
	senv=(BOXER_PLAY_FOCUS_SANKEY=1)
	# Three stages of the same population — operator, aircraft type, altitude
	# band — so every position counted flows the full width exactly once and the
	# node bars are exact subdivisions rather than three bar charts. Both the
	# operator set and the type set are capped: a Sankey is a tens-of-nodes
	# instrument, and the layout reports flows too thin to read rather than
	# quietly widening them.
	#
	# The hops are written out and stacked with UNION ALL, which is the readable
	# way when the stages are few and named; the array pivot in the help corpus
	# is for when the stage count is a property of the data.
	#
	# Node ids carry their stage (`1:B763`), because a value recurring in two
	# stages would otherwise fuse into one node — and a flow returning to an
	# earlier stage is a cycle, which this form rejects rather than draws. The
	# nodes channel hands the readable name back as a label, and its `stage`
	# column is what puts the panel in alluvial mode without being told.
	sql="WITH
  ops AS (
    SELECT ownOp
    FROM default.planes_mercator_sample100
    WHERE ownOp != '' AND t != '' AND altitude > 0
    GROUP BY ownOp
    ORDER BY uniq(icao) DESC
    LIMIT 4
  ),
  models AS (
    SELECT t
    FROM default.planes_mercator_sample100
    WHERE t != '' AND altitude > 0 AND ownOp IN (SELECT ownOp FROM ops)
    GROUP BY t
    ORDER BY count() DESC
    LIMIT 8
  ),
  legs AS (
    SELECT ownOp AS op,
           t     AS model,
           multiIf(altitude < 10000, 'below 10k',
                   altitude < 25000, '10-25k',
                   altitude < 35000, '25-35k',
                   'above 35k')     AS band
    FROM default.planes_mercator_sample100
    WHERE ownOp IN (SELECT ownOp FROM ops)
      AND t IN (SELECT t FROM models)
      AND altitude > 0
  ),
  flows AS (
    SELECT concat('0:', op) AS source, concat('1:', model) AS target, count() AS value
    FROM legs
    GROUP BY source, target
    UNION ALL
    SELECT concat('1:', model) AS source, concat('2:', band) AS target, count() AS value
    FROM legs
    GROUP BY source, target
  ),
  nodes AS (
    SELECT DISTINCT concat('0:', op) AS id, op AS label, 0 AS stage FROM legs
    UNION ALL
    SELECT DISTINCT concat('1:', model) AS id, model AS label, 1 AS stage FROM legs
    UNION ALL
    SELECT DISTINCT concat('2:', band) AS id, band AS label, 2 AS stage FROM legs
  )
SELECT * FROM flows ORDER BY value DESC"
	# Both channels run on their own lanes, off the split of this query — the
	# capture has to wait for the second, not just for the main result.
	settle=3000
}

scene_08_icicle() {
	desc="Icicle — the result as an icicle plot (ADR-0160): one row per depth, width as value, over the folded stack/value contract; the same population the Sankey draws, read as a containment hierarchy rather than a flow"
	senv=(BOXER_PLAY_FOCUS_ICICLE=1)
	# The folded contract, which is the one a profile arrives in: one row per
	# root-to-leaf path, the path as an array, `value` the path's own quantity.
	# Four levels — operator, manufacturer, type, altitude band — so the depth
	# axis has something to show; the Sankey scene reads three of the same
	# columns as a flow, and the contrast between the two forms over one
	# population is the point of putting them side by side.
	#
	# Operators are capped, not because the widget cannot draw the tail (it
	# culls what is too narrow to see) but because a root row of two hundred
	# slivers is a texture rather than a reading. `unit` labels the value axis;
	# it is a plain column, so a query that has no unit simply omits it.
	sql="WITH
  ops AS (
    SELECT ownOp
    FROM default.planes_mercator_sample100
    WHERE ownOp != '' AND t != '' AND desc != '' AND altitude > 0
    GROUP BY ownOp
    ORDER BY count() DESC
    LIMIT 8
  ),
  legs AS (
    SELECT ownOp AS op,
           splitByChar(' ', desc)[1] AS maker,
           t AS model,
           concat(toString(intDiv(altitude, 10000) * 10), 'k ft') AS band
    FROM default.planes_mercator_sample100
    WHERE ownOp IN (SELECT ownOp FROM ops)
      AND t != '' AND desc != '' AND altitude > 0
  )
SELECT [op, maker, model, band] AS stack, count() AS value, 'positions' AS unit
FROM legs
GROUP BY stack
ORDER BY value DESC"
	# Two captures: the icicle proper, then the flamegraph the orientation
	# switch inverts it into. They are one layout and one switch, and the second
	# shot is also what shows the view reset the flip has to trigger — implot
	# retains a plot's ranges, so without it the flipped tree would be viewed
	# through the old orientation's window (ADR-0160 §SD3).
	steps='{"do":"capture","text":"08_icicle","settleMs":600}
{"do":"click","name":"flame"}
{"do":"capture","text":"08_icicle_flame","settleMs":600}'
	settle=2500
}

scene_08_treemap() {
	desc="Treemap — the same population the Icicle scene draws, read as nested areas (ADR-0166): area is the position count, colour is the typical altitude, and the drill navigation replaces the depth axis"
	senv=(BOXER_PLAY_FOCUS_TREEMAP=1)
	# Deliberately the icicle scene's query, one level shallower and with a
	# `color`. Same contract, same population, so the pair shows what the two
	# forms trade: the icicle keeps the order and the depth of a path, the
	# treemap spends both dimensions on magnitude and has room for a second
	# measure. Three levels rather than four because a treemap nests rather
	# than stacking rows, and a fourth level lands below the minimum cell size
	# at this window width.
	#
	# `color` is an average, so it is independent of the area: a small cell in
	# a light colour is a fleet that is rare and flies high, which is the thing
	# the area alone cannot say.
	sql="WITH
  ops AS (
    SELECT ownOp
    FROM default.planes_mercator_sample100
    WHERE ownOp != '' AND t != '' AND desc != '' AND altitude > 0
    GROUP BY ownOp
    ORDER BY count() DESC
    LIMIT 8
  ),
  legs AS (
    SELECT ownOp AS op,
           splitByChar(' ', desc)[1] AS maker,
           t AS model,
           altitude AS alt
    FROM default.planes_mercator_sample100
    WHERE ownOp IN (SELECT ownOp FROM ops)
      AND t != '' AND desc != '' AND altitude > 0
  )
SELECT [op, maker, model] AS stack,
       count()            AS value,
       round(avg(alt))    AS color,
       'positions'        AS unit
FROM legs
GROUP BY stack
ORDER BY value DESC"
	# Three captures: the root view, the same tree with every level nested at
	# once, and a drill-in. The drill is what a treemap has instead of a depth
	# axis, and the breadcrumb it leaves is how you get back out.
	#
	# The drill click is the ladder's LAST rung — a coordinate (ADR-0127 §SD4)
	# — because a cell is an egui Frame with no accessible name, so no locator
	# reaches one. It lands on the top-left container's HEADER strip, above its
	# first child: leaf-click sensing is on, so a click inside a child pins that
	# leaf instead of drilling the parent.
	steps='{"do":"capture","text":"08_treemap","settleMs":600}
{"do":"click","name":"full"}
{"do":"capture","text":"08_treemap_all","settleMs":600}
{"do":"click","name":"drill"}
{"do":"click","x":200,"y":719}
{"do":"capture","text":"08_treemap_drill","settleMs":600}'
	settle=2500
}

scene_08_treemap_category() {
	desc="Treemap — a CATEGORICAL colour column (ADR-0166 §SD2): the qualitative key below the control row, and the inheritance rule that colours a container only when its descendants agree"
	senv=(BOXER_PLAY_FOCUS_TREEMAP=1)
	# The same fleets as 08_treemap, coloured by a BAND rather than a measure,
	# so the other arm of `color` and the other legend both draw. It is also
	# the clearest picture of the categorical inheritance rule: an operator
	# flying one band throughout takes that band's hue, and one flying several
	# — Delta, with Boeings in the middle band and Airbuses above it — stays on
	# the neutral depth ramp, which is what "look inside" looks like.
	sql="WITH
  ops AS (
    SELECT ownOp
    FROM default.planes_mercator_sample100
    WHERE ownOp != '' AND t != '' AND desc != '' AND altitude > 0
    GROUP BY ownOp
    ORDER BY count() DESC
    LIMIT 8
  ),
  legs AS (
    SELECT ownOp AS op,
           splitByChar(' ', desc)[1] AS maker,
           t AS model,
           altitude AS alt
    FROM default.planes_mercator_sample100
    WHERE ownOp IN (SELECT ownOp FROM ops)
      AND t != '' AND desc != '' AND altitude > 0
  )
SELECT [op, maker, model] AS stack,
       count()            AS value,
       multiIf(avg(alt) < 10000, 'low',
               avg(alt) < 25000, 'mid',
                                 'high') AS color,
       'positions'        AS unit
FROM legs
GROUP BY stack
ORDER BY value DESC"
	steps='{"do":"capture","text":"08_treemap_category","settleMs":600}
{"do":"click","name":"full"}
{"do":"capture","text":"08_treemap_category_full","settleMs":600}'
	settle=2500
}

scene_08_treemap_self() {
	desc="Treemap — a container with a quantity of its own (ADR-0166 §SD3): every table under a mebibyte is rolled up into its database, which then gets a cell of its own inside its own box"
	senv=(BOXER_PLAY_FOCUS_TREEMAP=1)
	# The node contract, and the case the SELF CELL exists for. A treemap is
	# subdivided by its children, so a container that also carries a quantity
	# needs a rectangle for it — without one that quantity is redistributed
	# among the children and every one of them reads too large.
	#
	# system.parts needs no fixture, and the roll-up is a real idiom rather
	# than a contrivance: spend cells on the tables worth one, keep the tail's
	# bytes at the database so the total still adds up.
	#
	# The threshold is 400 MiB rather than the snippet library's 1 MiB because
	# this is a PICTURE of the invariant: it leaves `default` one child and an
	# own value of ~12%, which is a cell you can see. At 1 MiB the own value is
	# 0.003% of a box one 3 GiB table dominates — present, correct, and below
	# the minimum cell size, which demonstrates nothing.
	sql="WITH p AS (
    SELECT database AS db, table AS tbl, sum(bytes_on_disk) AS bytes
    FROM system.parts
    WHERE active
    GROUP BY db, tbl
)
SELECT concat('db:', db)                        AS id,
       ''                                       AS parent,
       db                                       AS label,
       toFloat64(sumIf(bytes, bytes < 419430400)) AS value,
       'bytes'                                    AS unit
FROM p
GROUP BY db
UNION ALL
SELECT concat('tbl:', db, '.', tbl) AS id,
       concat('db:', db)            AS parent,
       tbl                          AS label,
       toFloat64(bytes)             AS value,
       'bytes'                      AS unit
FROM p
WHERE bytes >= 419430400"
	steps='{"do":"click","name":"full"}
{"do":"capture","text":"08_treemap_self","settleMs":600}'
	settle=2500
}

scene_08_distribution() {
	desc="Distribution — a result read as a distribution rather than a table (ADR-0161): an ECDF with its simultaneous band per series, the shift function against a chosen baseline, and the letter-value boxen ladder — three readings of the same three series"
	senv=(BOXER_PLAY_FOCUS_DIST=1)
	# Table-free, so this scene needs no fixture and runs against whatever the
	# Endpoint menu is pointed at. That is a property of the macro, not a
	# contrivance: descriptiveStatistics expands BEFORE the query ships, into
	# one UNION ALL branch per argument column, each carrying the original
	# FROM/WHERE/GROUP BY — so what reaches the panel is the ordinary named
	# contract (`series`, `n`, `ps`, `qs`), and hand-written SQL emitting those
	# four columns is on exactly the same footing as the macro here.
	#
	# Three arms of one latency: a baseline, the baseline plus a constant, and
	# the baseline scaled. The constants are chosen so both treatments land on
	# the SAME mean (150 ms) — and, because E[Q] is the baseline mean, on the
	# same W₁ as well. So the two scalar summaries the legend and the moments
	# offer both report the two arms as identical, and only the shape of Δ(p)
	# says that one arm moved everything by 30 ms while the other multiplied:
	# the curves cross at the median and diverge either side of it. That is the
	# reason this scene is a controlled comparison rather than a real dataset.
	#
	# Gaussian rather than the lognormal the snippets corpus uses. The grid
	# runs to p = 1 − 1.5e-5, so a heavy right tail puts x_max two orders of
	# magnitude above the bulk and every curve collapses onto the axis — the
	# panel is being honest and the picture is unreadable, which is the wrong
	# thing for a screenshot to teach. randNormal's second argument is the
	# standard deviation, not the variance its reference names.
	#
	# Three series is also the band policy's boundary: up to three, every
	# series carries its confidence band; beyond, only the selected one does.
	sql="WITH
  draws AS (
    SELECT ['A baseline', 'B shift +30ms', 'C scale x1.25'][1 + (number % 3)] AS arm,
           randNormal(120, 25)                                                AS draw
    FROM numbers(3000000)
  ),
  trial AS (
    SELECT arm,
           multiIf(arm = 'B shift +30ms', draw + 30,
                   arm = 'C scale x1.25', draw * 1.25,
                   draw) AS latency_ms
    FROM draws
  )
SELECT descriptiveStatistics(latency_ms)
FROM trial
GROUP BY arm"
	# Three captures, one per view. The baseline is the SHARED selection, so
	# the chip click is not decoration: it pins which arm the Δ curves are
	# measured against, and it has to go by NAME because the row order out of
	# a GROUP BY across the expansion's branches is not guaranteed — trusting
	# row 0 would silently re-base the picture between runs.
	#
	# The click also publishes `selection`, which is what puts the same series
	# in the Detail pane: this is a main-result panel, so the chip IS a row.
	steps='{"do":"capture","text":"08_distribution","settleMs":600}
{"do":"click","contains":"A baseline","comment":"pin the baseline by name, not by row order"}
{"do":"click","name":"Shift"}
{"do":"capture","text":"08_distribution_shift","settleMs":600}
{"do":"click","name":"Boxen"}
{"do":"capture","text":"08_distribution_boxen","settleMs":600}'
	settle=2500
}

scene_08_distribution_small_pane() {
	desc="Distribution in a SMALL window: the plot box follows the pane instead of a fixed height, so the ECDF and the boxen ladder each keep the x tick labels implot draws along their BOTTOM edge"
	# The rest of the tour runs at 1920x1200, where a fixed-height plot box
	# always fits and this class of bug is invisible. An applet window is
	# ~900x660 and not resizable out of, which is where it was found — on the
	# Chart tab first (ADR-0172), then here and on Series, the two tabs that
	# fix carried the same fixed height.
	senv=(BOXER_PLAY_FOCUS_DIST=1 BOXER_PLAY_WINDOW_SIZE=900x640)
	# Response sizes rather than the sibling scene's latencies: ~340..550 kB,
	# so nothing sits near zero. That matters for the boxen view, whose y axis
	# IS the value — when the box overflowed its pane the clipped-off bottom
	# read as a raised axis minimum and the lower letter-values looked absent.
	# A distribution whose values reach zero puts the interesting end at the
	# axis bottom and hides the clip entirely.
	sql="WITH
  draws AS (
    SELECT ['A control', 'B treatment'][1 + (number % 2)] AS arm,
           randNormal(420000, 18000)                      AS draw
    FROM numbers(1000000)
  ),
  trial AS (
    SELECT arm,
           multiIf(arm = 'B treatment', draw * 1.08, draw) AS response_bytes
    FROM draws
  )
SELECT descriptiveStatistics(response_bytes)
FROM trial
GROUP BY arm"
	# Both views, because they clip differently: the ECDF loses the bottom of
	# a curve whose y range is fixed at 0..1, the boxen loses the bottom of a
	# value axis — which is the one that reads as missing data rather than as
	# a cropped picture.
	steps='{"do":"capture","text":"08_distribution_small_pane","settleMs":600}
{"do":"click","name":"Boxen"}
{"do":"capture","text":"08_distribution_small_pane_boxen","settleMs":600}'
	settle=2500
}

scene_08_series() {
	desc="Series — numbers against a time axis (ADR-0163 M0): the typed claim (first temporal column, every numeric column a lane), the Δt classification with the scaffold its finding offers, and modified-sinc smoothing with its extrapolated tail drawn faded"
	senv=(BOXER_PLAY_FOCUS_SERIES=1)
	# One day of ADS-B traffic in five-minute buckets. Two lanes because the
	# claim takes every numeric column, and these two are both counts — a
	# shared y axis is honest only between comparable magnitudes, which is the
	# tab's one composition rule.
	#
	# The window is a day rather than the fixture's whole week for the sake of
	# the second capture: smoothing's extrapolated tail is a half-width of
	# samples, and at ~290 points that is a visible stretch of the line rather
	# than a few pixels.
	#
	# 287 buckets, not 288 — the fixture is missing one, which is the point.
	# The status line says "regular with gaps" at 5 minutes and the pane offers
	# the WITH FILL scaffold with the measured step already substituted; the
	# line BREAKS at the hole rather than being drawn across it.
	# toDateTime64 because toStartOfInterval yields a DateTime, which reaches
	# Arrow as a bare UInt32 of epoch seconds — indistinguishable from a count,
	# so the claim cannot take it (ADR-0163 Update 2026-08-05).
	sql="SELECT toDateTime64(toStartOfInterval(time, INTERVAL 5 MINUTE), 3) AS t,
       count()            AS positions,
       uniqExact(icao)    AS aircraft
FROM default.planes_mercator_sample100
WHERE time >= '2026-07-05 00:00:00' AND time < '2026-07-06 00:00:00'
GROUP BY t
ORDER BY t"
	# Two captures: the series as it lands, then with smoothing on. The second
	# is what shows the live edge — the trailing half-width the boundary
	# extrapolation defines rather than measures, faded so it is not read as
	# the same claim as the rest of the curve.
	steps='{"do":"capture","text":"08_series","settleMs":600}
{"do":"click","name":"smooth"}
{"do":"capture","text":"08_series_smoothed","settleMs":600}'
	settle=2500
}

scene_08_series_vocabulary() {
	desc="The ts* client vocabulary (ADR-0163 M1): a CTE whose body is tsAnomalyScores runs IN PLAY, not on ClickHouse — observed in the result panels, with the Graph tab's engine badge and the honesty caption naming what was actually sent"
	# BOXER_PLAY_OBSERVE points the result panels at the client node. That is
	# the sanctioned way to see one: a client call is a terminal leaf, so
	# `SELECT * FROM scored` is a loud split error naming this fix instead.
	senv=(BOXER_PLAY_FOCUS_TABLE=1 BOXER_PLAY_OBSERVE=scored)
	# The input CTE is what the server runs; `scored` is what play computes
	# from its result. Until M2 renders the overlays, the score lane arrives
	# as an ordinary table — which is exactly what M1 claims to deliver.
	#
	# The whole week rather than one day, because DAMP trains on 8×window
	# before it scores anything: at a 24-sample window that is 192 samples of
	# warm-up, which would be most of a single day's 288 and would show a
	# table of `warm_up true` rather than the scores.
	sql="WITH
  base AS (
    SELECT toDateTime64(toStartOfInterval(time, INTERVAL 5 MINUTE), 3) AS t,
           count()                                                     AS v
    FROM default.planes_mercator_sample100
    GROUP BY t
    ORDER BY t
  ),
  scored AS (SELECT tsAnomalyScores(t, v, 24) FROM base)
SELECT 1"
	settle=3000
}

scene_08_series_overlays() {
	desc="Series overlays (ADR-0163 M2): the detector's score on its own x-linked plot with the warm-up region shaded, the moving-average baseline drawn beside it BY DEFAULT, and the flagged extents as bands behind both"
	senv=(BOXER_PLAY_FOCUS_SERIES=1)
	# Three CTEs and a sink. `base` is the series the panel charts and the
	# only thing ClickHouse runs; `scores` and `spans` are client nodes,
	# filled into the panel's optional channels BY NAME (§SD1). The sink
	# selects from base — NOT from a client node, which is a terminal leaf.
	#
	# A 24-sample window over the week's 2012 buckets leaves DAMP's 8×window
	# training as a visible but small shaded prefix, which is what the warm-up
	# chrome is for.
	sql="WITH
  base AS (
    SELECT toDateTime64(toStartOfInterval(time, INTERVAL 5 MINUTE), 3) AS t,
           count()                                                     AS v
    FROM default.planes_mercator_sample100
    GROUP BY t
    ORDER BY t
  ),
  scores AS (SELECT tsAnomalyScores(t, v, 24) FROM base),
  spans  AS (SELECT tsAnomalySpans(t, v, 24, 3) FROM base)
SELECT * FROM base"
	settle=4000
}

scene_08_series_small_pane() {
	desc="Series in a SMALL window with a score plot: the pane's height is a BUDGET the two x-linked plots split, so both keep the UTC tick labels implot draws along their bottom edge instead of the lower one being pushed under the fold"
	# The hard case of the class the Chart tab surfaced (ADR-0172). This leaf
	# can hold TWO plots, so the pane is not a height to assign but a budget to
	# divide: with fixed boxes (200 + 150) the pair simply overflowed together,
	# and the score plot — the lower one — lost its whole time axis first.
	#
	# 900x900 rather than the 900x640 its Distribution and Chart siblings use,
	# and the difference is measured rather than chosen. This tab spends ~210pt
	# above its plots — status line, smoothing controls, the score's honesty
	# paragraph, the fixture-lab row — so the probe reports a 124pt pane at
	# 900x820 and about 25pt at 900x640, where two floored boxes plus the slack
	# (128pt) do not fit however they are split and the scene would picture the
	# ScrollArea fallback instead of the fix. At 900x900 the probe reports
	# 168pt, which is still far under the 350pt the two fixed boxes wanted: the
	# bug reproduces here, it is simply also legible here.
	senv=(BOXER_PLAY_FOCUS_SERIES=1 BOXER_PLAY_WINDOW_SIZE=900x900)
	# Table-free, like the Chart small-pane scene, so it runs against whatever
	# the Endpoint menu is pointed at: two days of five-minute buckets built
	# from numbers(). toDateTime64 because the typed claim takes DateTime64 /
	# Date / Arrow timestamp only (ADR-0163 Update 2026-08-05).
	#
	# ~360..490 kB per bucket, so the lane never approaches zero — a series
	# whose values reach the axis bottom hides a clipped box, which is why the
	# first charts looked at did not show the bug.
	sql="WITH
  base AS (
    SELECT toDateTime64('2026-07-05 00:00:00', 3) + toIntervalMinute(5 * number) AS t,
           420000 + 60000 * sin(number / 22.0) + (rand() % 9000)                 AS bytes_per_min
    FROM numbers(576)
  ),
  scores AS (SELECT tsAnomalyScores(t, bytes_per_min, 24) FROM base)
SELECT * FROM base"
	settle=3000
}

scene_08_series_adjudication() {
	desc="Adjudication (ADR-0163 M3): one row per flagged extent with confirm / false-alarm, writing an append-only tslabels row — and, once a span is confirmed, the VUS readout scoring the detector against its own baseline on the adjudicated spans"
	senv=(BOXER_PLAY_FOCUS_SERIES=1)
	# The same buffer as the overlays scene: adjudication is about the spans,
	# so it needs them on screen.
	sql="WITH
  base AS (
    SELECT toDateTime64(toStartOfInterval(time, INTERVAL 5 MINUTE), 3) AS t,
           count()                                                     AS v
    FROM default.planes_mercator_sample100
    GROUP BY t
    ORDER BY t
  ),
  scores AS (SELECT tsAnomalyScores(t, v, 24) FROM base),
  spans  AS (SELECT tsAnomalySpans(t, v, 24, 3) FROM base)
SELECT * FROM base"
	# Confirm the first extent, then capture: the write is append-only and the
	# read lane forgets its memo on completion, so the recorded verdict and the
	# readout it enables both appear without a re-run. The settle after the
	# click is the round trip.
	steps='{"do":"capture","text":"08_series_adjudication","settleMs":600}
{"do":"click","name":"confirm #1"}
{"do":"capture","text":"08_series_adjudication_recorded","settleMs":2500}'
	settle=4000
}

scene_08_series_fixture() {
	desc="The fixture lab (ADR-0163 M4) from an EMPTY workbench: kind and seed, generating a labelled synthetic series published as ORDINARY ad-hoc datasets — fixture_series and fixture_truth, queried with keelson() like anything else, with no demo mode anywhere"
	# An EMPTY autorun is what makes this the empty workbench, and it has to be
	# the autorun rather than the buffer: BOXER_PLAY_SQL="" is deliberately
	# treated as UNSET (app_register.go, tier 2), so an empty value falls
	# through to the restored workingset and the pane opens on whatever the
	# last session left. No run means no result, which is the state the Series
	# tab shows its empty branch — and the lab — in.
	#
	# EMPTY, not 0: the knob is a non-empty test ("non-empty enables auto-run"),
	# so BOXER_PLAY_AUTORUN=0 would turn it ON. The assignment lands after the
	# runner's own BOXER_PLAY_AUTORUN=1 in the env list, and the last
	# assignment for a name wins.
	senv=(BOXER_PLAY_FOCUS_SERIES=1 BOXER_PLAY_AUTORUN=)
	# A buffer that says what the window is showing. It never runs, so its only
	# job is to not look like a stale query someone forgot to clear.
	sql="-- Nothing has run yet. The fixture lab below generates a labelled
-- series with known ground truth, and publishes it as ordinary tables."
	# Three captures: the empty workbench with the lab, what one click
	# publishes — including the scaffold it splices into the buffer — and the
	# scaffold RUN, which is the claim in the desc and the only capture that
	# makes it good: the fixture is queried with keelson() like any table.
	#
	# WAIT on Run rather than sleeping a guessed duration. `settleMs` pauses
	# AFTER its own step (carrierclient/trace.go), so a settleMs on the capture
	# delays nothing before it — the shutter fires at the DEFAULT settle after
	# the click, ~350 ms, and catches a query that is legitimately still in
	# flight. That misread cost a day on 2026-08-06: the loading spinner it
	# caught was recorded here as a hang in the introspection read path, and
	# raising the number 4000 → 9000 changed nothing because the number was
	# never in the path. There is no hang; the read finishes in ~0.3 s.
	# play swaps Run for Cancel while a run is in flight, so "Run resolves
	# again" is a precise "the result landed" — and the wait polls for it.
	# apps/play/play_adhoc_read_e2e_test.go pins the same read headlessly.
	steps='{"do":"capture","text":"08_series_fixture","settleMs":600}
{"do":"click","name":"generate"}
{"do":"capture","text":"08_series_fixture_generated","settleMs":3000}
{"do":"click","name":"Run"}
{"do":"wait","name":"Run","comment":"Run is Cancel while the query runs, so this resolves when it lands"}
{"do":"sleep","settleMs":800}
{"do":"capture","text":"08_series_fixture_queried"}'
	settle=2000
}

scene_07_keelson_introspection() {
	desc="A keelson() read routed to the in-process introspection plane (ADR-0094 / ADR-0141): the dispatch resolver moves a keelson-only statement off the pinned ClickHouse and answers it from this process, which the toolbar names"
	# The one path the tour never covered. Everything else here queries a
	# server; this queries the HOST — same editor, same panes, a different
	# engine — and the resolver is what decides that, per statement.
	#
	# keelson('env') is the canonical in-process table (the carousel wires
	# /query specifically so a co-resident play can ask for it), it needs no
	# fixture, and it is small enough to read in a screenshot.
	senv=(BOXER_PLAY_FOCUS_TABLE=1)
	# Two columns, ten rows. The pane emits accessibility nodes per cell and
	# this table is wide (a registry entry carries its whole description), so
	# an unbounded read pushed the tree past 2000 nodes and took the headless
	# client down with it — the capture never got an acknowledgement.
	sql="SELECT name, category FROM keelson('env') ORDER BY name LIMIT 10"
	# The introspection round trip is a chlocal exec rather than a scan, so it
	# wants more than the tour default.
	#
	# Not much more, though: at settle=9000 the headless client dropped the
	# connection before the first capture and the driver failed with "unable
	# to write frame". That ceiling is on the PRELUDE sleep — the idle stretch
	# before any capture — not on settles generally; a 9 s settle on a later
	# capture step, after the connection has carried traffic, is fine (the
	# fixture scene does exactly that). Worth knowing before blaming a scene's
	# query for a capture that never happened.
	settle=4000
}

scene_08_series_vocabulary_graph() {
	desc="The same buffer read as a graph: the client node badged 'computed in play', the honesty caption naming what was actually sent, and the input CTE beneath it as ordinary SQL"
	# A second launch rather than a click: the dock's tab strip is drawn by
	# egui_dock on the Rust side and is not in the accessibility tree, so the
	# FOCUS knob is how a scripted capture reaches another tab.
	senv=(BOXER_PLAY_FOCUS_GRAPH=1 BOXER_PLAY_OBSERVE=scored)
	sql="WITH
  base AS (
    SELECT toDateTime64(toStartOfInterval(time, INTERVAL 5 MINUTE), 3) AS t,
           count()                                                     AS v
    FROM default.planes_mercator_sample100
    GROUP BY t
    ORDER BY t
  ),
  scored AS (SELECT tsAnomalyScores(t, v, 24) FROM base)
SELECT 1"
	settle=3000
}

scene_08_chart_bars() {
	desc="Chart — the lanes reading (ADR-0172): a categorical x with two numeric columns, each labelled by its own column name, drawn as grouped bars and then as a line from the same claim"
	senv=(BOXER_PLAY_FOCUS_CHART=1)
	# The WIDE idiom: one lane per numeric column, no `series` needed. Both
	# lanes are aircraft counts, which is the composition rule a shared y axis
	# imposes — two magnitudes that are not comparable would flatten one.
	# `t` is a LowCardinality(String), so the axis is CATEGORICAL and the
	# distinct values take positions in the order ORDER BY put them.
	sql="SELECT t                                      AS x,
       uniqExactIf(icao, altitude >= 10000)   AS above_10k,
       uniqExactIf(icao, altitude <  10000)   AS below_10k
FROM default.planes_mercator_sample100
WHERE t != ''
GROUP BY x
ORDER BY above_10k + below_10k DESC
LIMIT 12"
	# Two captures from one claim: the type-seeded default (Bar, because x is
	# a category) and the same data under the Line chip.
	steps='{"do":"capture","text":"08_chart_bars","settleMs":600}
{"do":"click","name":"Line"}
{"do":"capture","text":"08_chart_line","settleMs":600}'
	settle=2000
}

scene_08_chart_series() {
	desc="Chart — the LONG idiom (ADR-0172 §SD1): a series column splits the rows into one drawn series each, over a temporal x that takes implot's time axis"
	senv=(BOXER_PLAY_FOCUS_CHART=1)
	# toDateTime64 for the Series tab's reason (ADR-0163 Update 2026-08-05): a
	# plain DateTime reaches Arrow as a bare UInt32 and nothing tells it from a
	# count, so the temporal axis needs the cast to be recognised.
	sql="SELECT toDateTime64(toStartOfInterval(time, INTERVAL 30 MINUTE), 3) AS x,
       t                                                             AS series,
       count()                                                       AS y
FROM default.planes_mercator_sample100
WHERE time >= '2026-07-05 00:00:00' AND time < '2026-07-06 00:00:00'
  AND t IN ('A320', 'B738', 'A21N')
GROUP BY x, series
ORDER BY x"
	settle=2000
}

scene_08_chart_heatmap() {
	desc="Chart — the GRID reading (ADR-0172 §SD1): the x, y and z columns pivoted into a heatmap over the distinct keys, with the colorscale legend bound to the same colormap"
	senv=(BOXER_PLAY_FOCUS_CHART=1)
	# 24 x 7 ordinal cells. Both keys are numeric, so they sort ascending —
	# a GROUP BY without an ORDER BY would otherwise arrive shuffled and the
	# axes would read as noise.
	sql="SELECT toHour(time)      AS x,
       toDayOfWeek(time) AS y,
       count()           AS z
FROM default.planes_mercator_sample100
GROUP BY x, y
ORDER BY y, x"
	settle=2000
}

scene_08_chart_heatmap_sparse() {
	desc="Chart — a SPARSE grid: cells no row filled stay holes in the colormap's transparent BadColor rather than being drawn as a zero, which is a different claim"
	senv=(BOXER_PLAY_FOCUS_CHART=1)
	# Only the fastest traffic, which is not airborne in every hour of every
	# weekday: 72 of the 168 cells have a row and the rest have none. Those 96
	# fold to NaN and colorize through the BadColor, so the surface shows
	# through — "nothing was observed here" reads differently from "zero were",
	# and a heatmap that painted the low end of the ramp would say the second.
	#
	# This shape is also what crashed the panel before colormap.Config.At
	# substituted for a non-number (index [-9223372036854775808] out of
	# int(NaN)), so the scene is a regression capture as much as a feature one.
	sql="SELECT toHour(time)      AS x,
       toDayOfWeek(time) AS y,
       count()           AS z
FROM default.planes_mercator_sample100
WHERE ground_speed > 560
GROUP BY x, y
ORDER BY y, x"
	# The second capture points at a cell. A heatmap's legend row stands for
	# the whole grid, so the built-in series highlight has nothing to single
	# out — the CELL is what a reader is pointing at, and it outlines and reads
	# out its two keys and its value. Coordinates rather than an anchor because
	# the cells are painter-lane geometry with no accessibility node; hover
	# state is a register read one frame behind, so the capture that follows
	# sees it.
	# Both readings of a cell: one that a row filled, and one that none did.
	steps='{"do":"capture","text":"08_chart_heatmap_sparse","settleMs":600}
{"do":"hover","x":576,"y":790}
{"do":"capture","text":"08_chart_heatmap_hover","settleMs":600}
{"do":"hover","x":690,"y":820}
{"do":"capture","text":"08_chart_heatmap_hover_hole","settleMs":600}'
	settle=2000
}

scene_08_chart_numeric() {
	desc="Chart — a CONTINUOUS x (ADR-0172 §SD2): bar slots sized from the smallest gap between distinct x values, a NULL ending a lane rather than being drawn as a zero, and the Scatter and log-y chips"
	senv=(BOXER_PLAY_FOCUS_CHART=1)
	# 5000-foot altitude bands. x is an ordinary integer, so the axis is
	# CONTINUOUS — the case the categorical scene above cannot show, and the one
	# §SD4's bar geometry is defined against (slot = the smallest positive gap
	# between distinct x, which is 5 here; the 50 -> 90 jump leaves a real hole
	# on the axis rather than a compressed one).
	#
	# nullIf turns an EMPTY band into a NULL rather than a zero, which is the
	# distinction the panel refuses to blur: no fast aircraft were seen above
	# 50k feet, and "none observed" is not "zero of them". The fast lane ends at
	# its last measured band instead of being drawn down to the axis.
	sql="SELECT intDiv(altitude, 5000) * 5                            AS x,
       nullIf(uniqExactIf(icao, ground_speed >= 400), 0)      AS fast,
       nullIf(uniqExactIf(icao, ground_speed <  400), 0)      AS slow
FROM default.planes_mercator_sample100
WHERE altitude > 0
GROUP BY x
ORDER BY x"
	# Four captures, each one click apart: the continuous-axis default (Line),
	# the Scatter chip, then log y — offered here because every drawn value is
	# strictly positive, and worth engaging because the lanes span two orders of
	# magnitude — and then log y OFF again.
	#
	# The last pair is the point of the last two steps. A toggle clicked ONCE
	# only ever evidences the direction that works, and turning a control back
	# off is a different path whenever the consumer keeps state: this scene
	# clicked log y once for a week while unchecking it left the axis
	# logarithmic, because implot's axis scale was retained rather than
	# re-declared each frame (ADR-0172 verification item 17).
	steps='{"do":"capture","text":"08_chart_numeric","settleMs":600}
{"do":"click","name":"Scatter"}
{"do":"capture","text":"08_chart_scatter","settleMs":600}
{"do":"click","name":"log y"}
{"do":"capture","text":"08_chart_logy","settleMs":600}
{"do":"click","name":"log y"}
{"do":"capture","text":"08_chart_logy_off","settleMs":600}'
	settle=2000
}

scene_08_chart_small_pane() {
	desc="Chart in a SMALL window: the plot box follows the pane instead of a fixed height, so a top-N ranking keeps its zero baseline and every category label"
	# The rest of the tour runs at 1920x1200, where a fixed-height plot box
	# always fits and this class of bug is invisible. An applet window is
	# ~900x660 and not resizable out of, which is where it was found.
	senv=(BOXER_PLAY_FOCUS_CHART=1 BOXER_PLAY_WINDOW_SIZE=900x640)
	# A ranking, so nothing sits near zero: when the box overflowed its pane
	# the clipped-off bottom read as a raised axis minimum and the short bars
	# looked absent. Data whose values reach zero hides the symptom, which is
	# why the charts looked at first did not show it.
	sql="SELECT * FROM values(
  'x String, rows UInt64',
  ('alpha', 1200000), ('bravo', 980000), ('charlie', 870000), ('delta', 610000),
  ('echo', 520000), ('foxtrot', 348603), ('golf', 300000), ('hotel', 280000),
  ('india', 250000), ('juliett', 210000), ('kilo', 190000), ('lima', 170000),
  ('mike', 150000), ('november', 130000), ('oscar', 110000))"
	settle=1800
}

scene_08_chart_rownumber() {
	desc="Chart — the IMPLICIT abscissa (ADR-0172 §SD2): a result with no x column numbers its own rows 1, 2, 3 in the order the query returned them, so an ordinary top-N draws as the ranking curve it is"
	senv=(BOXER_PLAY_FOCUS_CHART=1)
	# The shape that motivated the rule, and the commonest SELECT there is: a
	# GROUP BY ranked by its own aggregate. Nothing here is named x, so before
	# §SD2 this pane was a reject.
	#
	# `aircraft` is a string, so it is neither the abscissa (which is claimed by
	# NAME) nor a lane (which must be a number) — the panel ignores it, and the
	# picture is `positions` against rank. Aliasing it `AS x` is how you get the
	# type labelled on the axis instead; the numbering is what happens when
	# there is nothing to label with, not a substitute for asking.
	sql="SELECT t       AS aircraft,
       count() AS positions
FROM default.planes_mercator_sample100
WHERE t != ''
GROUP BY aircraft
ORDER BY positions DESC
LIMIT 30"
	# The continuous default (Line — a numbered result shows the shape of an
	# ordered sequence), then Bar, which is what a short ranking usually wants
	# and the reason the chips stay offered on an invented axis.
	steps='{"do":"capture","text":"08_chart_rownumber","settleMs":600}
{"do":"click","name":"Bar"}
{"do":"capture","text":"08_chart_rownumber_bar","settleMs":600}'
	settle=2000
}

scene_08_chart_rownumber_series() {
	desc="Chart — the implicit abscissa with a series column (ADR-0172 §SD2): the numbering restarts inside each group, so unequal groups overlay rather than being laid end to end"
	senv=(BOXER_PLAY_FOCUS_CHART=1)
	# Two runs of unequal length, deliberately INTERLEAVED: the counter is per
	# group, not per run of rows, so run A's fifth measurement is at x=5 even
	# though row 9 of the result is where it arrived.
	#
	# The alignment is positional and nothing measured it — the k-th of one run
	# is drawn beside the k-th of the other because they are both k-th — which
	# is exactly what the status line says. The shorter run ENDS rather than
	# being padded or stretched, which is the same no-fabrication rule a NULL
	# gets.
	sql="SELECT * FROM values(
  'series String, latency_ms Float64',
  ('run A', 82), ('run B', 140),
  ('run A', 74), ('run B', 132),
  ('run A', 79), ('run B', 121),
  ('run A', 71), ('run B', 118),
  ('run A', 68),
  ('run A', 65))"
	settle=1800
}

scene_08_chart_reject() {
	desc="Chart — the SCHEMA-level reject (ADR-0172 §SD1): a result satisfying neither reading draws the contract and names its own columns back, and the dock strip carries the shape mark"
	senv=(BOXER_PLAY_FOCUS_CHART=1)
	# A result with no NUMBER in it, which is what a schema-level reject takes
	# now that §SD2 numbers the rows of one that has no x. This scene used to
	# be "numbers but no x" — the commonest miss — and that result draws today.
	#
	# The pane names the contract, offers a query shape that satisfies it, and
	# then lists what THIS result actually carries, which is the half that makes
	# the message about the query rather than about the documentation. The strip
	# shows "Chart -" beside the other shape-contract tabs.
	sql="SELECT database, name, engine
FROM system.tables
WHERE database NOT IN ('INFORMATION_SCHEMA', 'information_schema')
ORDER BY database, name
LIMIT 20"
	settle=1500
}

scene_08_chart_duplicate() {
	desc="Chart — the DATA-level reject (ADR-0172 §SD1): a repeated (x, y) cell rejects the WHOLE heatmap, naming the row and the cell, rather than letting the last row win"
	senv=(BOXER_PLAY_FOCUS_CHART=1)
	# The incomplete-GROUP-BY mistake: grouping by a third dimension the
	# projection does not carry, so each (hour, day) cell arrives many times
	# over — 12,217 rows for a 24 x 7 grid. The panel claims the result on its
	# schema and then refuses it on its data, because last-write-wins would
	# paint a matrix the query never asked for.
	sql="SELECT toHour(time)      AS x,
       toDayOfWeek(time) AS y,
       count()           AS z
FROM default.planes_mercator_sample100
GROUP BY x, y, t
ORDER BY y, x"
	settle=1500
}

scene_09_projection() {
	desc="Projection — dimensionality reduction over the numeric columns of a result, with the point cloud tied to the selection signal"
	senv=(BOXER_PLAY_FOCUS_PROJECTION=1)
	sql="SELECT icao, altitude, ground_speed, track_degrees, lat, lon, vertical_rate
FROM default.planes_mercator_sample100
WHERE altitude > 0 AND ground_speed > 0
LIMIT 1500"
	settle=4000
}

scene_10_schema() {
	desc="Schema — the leeway schema inspector: sections, their value columns and the physical names a handle resolves to"
	senv=(BOXER_PLAY_FOCUS_SCHEMA=1)
	sql="SELECT * FROM anchor.facts LIMIT 50"
}

scene_11_graph() {
	desc="Graph — the reactive query graph (ADR-0097): every top-level CTE is a node, CTE references are data edges, unbound placeholders are signal edges"
	senv=(BOXER_PLAY_FOCUS_GRAPH=1)
	sql="WITH
  recent AS (
    SELECT \`id:id\` AS id, \`symbol:value\`[1] AS kind, \`timeRange:beginIncl\`[1] AS t
    FROM anchor.facts WHERE length(\`timeRange:beginIncl\`) > 0
  ),
  by_kind AS (
    SELECT kind, count() AS n, min(t) AS first_seen FROM recent GROUP BY kind
  ),
  busiest AS (
    SELECT kind FROM by_kind ORDER BY n DESC LIMIT 3
  )
SELECT r.id, r.kind, r.t
FROM recent AS r
WHERE r.kind IN (SELECT kind FROM busiest)
ORDER BY r.t
LIMIT 100"
}

scene_12_passes() {
	desc="Passes — the nanopass pre-execute sequence: each rewrite that ran on the buffer, in order, with the ones that were skipped marked"
	senv=(BOXER_PLAY_FOCUS_PASSES=1)
	sql="SELECT \`id:id\`, \`symbol:*\`
FROM anchor.facts
WHERE hasAny(\`symbol:value\`, ['DDOS', 'PORT_SCAN', 'SQL_INJECTION'])
ORDER BY \`id:id\`"
}

scene_13_diagnostics() {
	desc="Diagnostics — a handle that names no known section: flagged before Run, with suggestions, and the pre-execute rewrites that were skipped"
	senv=(BOXER_PLAY_FOCUS_DIAGNOSTICS=1)
	sql="SELECT \`id:id\`, \`symbal:value\`, \`geoPoint:lattitude\`
FROM anchor.facts
LIMIT 20"
}

scene_14_docs() {
	desc="Docs — the reference for what the caret is on, read from the server actually being queried (system.documentation): the look-up box that reaches a name the buffer does not have yet, and the kind selector a name carrying several kinds gets"
	senv=(BOXER_PLAY_FOCUS_DOCS=1)
	# The buffer is an ordinary aggregation, not the subject: the pane reads
	# the editor's published caret entity rather than the result, so what is
	# on screen beside it only has to look like work in progress.
	sql="SELECT t AS aircraft_type, count() AS pings, argMax(icao, ground_speed) AS fastest
FROM default.planes_mercator_sample100
WHERE altitude > 0 AND t != ''
GROUP BY t
ORDER BY pings DESC
LIMIT 15"
	# The pane follows the caret by default; this scene drives the MANUAL
	# look-up instead, for two reasons. It is the affordance for reaching a
	# name that is not in the buffer yet — the case the box exists for — and
	# it is the half a trace can actuate, since moving the caret onto a
	# particular token means a coordinate click into the editor, the ladder's
	# last rung, for a pane whose whole point is which NAME it resolved.
	#
	# focus and type are separate steps, per scene 30: egui applies an
	# injected AccessKit focus request on the frame after it arrives, so text
	# sent in the same batch goes to whatever had focus before and is dropped
	# without an error.
	#
	# `Merge` is the name because it carries FIVE kinds on a 26.x server —
	# aggregate-function combinator, table engine, table function, and two
	# one-line metric entries — so the kind selector draws. The pane opens on
	# the combinator and not on the engine: the lookup renders the kind enum
	# to text inside its subquery, so the outer ORDER BY sorts the kind list
	# alphabetically rather than by ordinal, and the first entry wins. Reading
	# the enum's ordering off `system.documentation` directly predicts the
	# wrong one — the tour captured the same page twice before this was
	# checked against the pane.
	#
	# The second capture switches to the table engine, where the same name is
	# a different thing entirely and the body carries several SQL fences —
	# which is also what shows a doc block offering the Insert/Replace actions
	# the Snippets tab uses.
	#
	# The look-up box is nameless — an untouched TextEdit has neither an
	# accessible name nor a value — so it is addressed by role. It is the only
	# text input in this scene, so a second one appearing is an ambiguity
	# error rather than a silent pick of the wrong field.
	#
	# The settle rides on the TYPE step, not on the capture: settleMs pauses
	# AFTER its step, so a capture carrying it shoots first and waits
	# afterwards. The lookup is a lane round trip behind a debounce, and at the
	# tour default of 350 ms this scene captured "Looking up Merge…" — an empty
	# pane, with the trace still reporting ok because a PNG was written.
	steps='{"do":"focus","role":"text_input","comment":"the look-up box, nameless so by role","settleMs":400}
{"do":"type","role":"text_input","text":"Merge","settleMs":2500}
{"do":"capture","text":"14_docs","settleMs":600}
{"do":"click","name":"Table Engine","comment":"the same name, a different kind — not the one the pane opened on"}
{"do":"capture","text":"14_docs_kind","settleMs":800}'
}

scene_14_snippets() {
	desc="Snippets — the ready-to-run fragment library, insertable into the editor at the caret"
	senv=(BOXER_PLAY_FOCUS_SNIPPETS=1)
	sql="SELECT * FROM anchor.facts LIMIT 25"
}

scene_15_flow() {
	desc="Flow — clause-level dataflow inside one statement (ADR-0153), where the Graph tab's per-CTE boxes end"
	senv=(BOXER_PLAY_FOCUS_FLOW=1)
	sql="WITH busy AS (
  SELECT icao, count() AS pings, avg(altitude) AS avg_alt
  FROM default.planes_mercator_sample100
  WHERE altitude > 0
  GROUP BY icao
  HAVING pings > 20
)
SELECT b.icao, b.pings, round(b.avg_alt) AS avg_alt, p.t AS aircraft_type
FROM busy AS b
LEFT JOIN default.planes_mercator_sample100 AS p ON p.icao = b.icao
ORDER BY b.pings DESC
LIMIT 100"
}

scene_16_preview_canonical() {
	desc="Preview — the canonical form the nanopass pipeline rewrites the buffer into, before any handle resolution"
	senv=(BOXER_PLAY_FOCUS_TABLE=1 BOXER_PLAY_FOCUS_PREVIEW=1)
	sql="SELECT t, countIf(altitude > 30000) AS cruising, countIf(altitude <= 30000) AS lower, count() AS total
FROM default.planes_mercator_sample100
WHERE t != ''
GROUP BY t
HAVING total > 5
ORDER BY total DESC"
}

scene_17_preview_as_sent() {
	desc="Preview — 'as sent to server': the post-pass wire SQL, with the appended FORMAT ArrowStream and the statement selection"
	senv=(BOXER_PLAY_FOCUS_TABLE=1 BOXER_PLAY_FOCUS_PREVIEW=1 BOXER_PLAY_PREVIEW_AS_SENT=1)
	sql="SELECT \`id:id\`, \`symbol:value\`
FROM anchor.facts
WHERE hasAny(\`symbol:value\`, ['DELIVERED'])
LIMIT 25"
}

scene_18_params_live() {
	desc="Parameters — an unbound {name:Type} placeholder is a LIVE signal: the widget above the editor writes the shared value"
	senv=(BOXER_PLAY_FOCUS_TABLE=1)
	sql="SELECT \`id:id\`, \`symbol:value\`[1] AS kind
FROM anchor.facts
ORDER BY \`id:id\`
LIMIT {lim:UInt64}"
}

scene_19_params_pinned() {
	desc="Parameters — a SET param_ line PINS the value into the buffer: a constant that shadows any signal of the same name, plus a folded range pair"
	senv=(BOXER_PLAY_FOCUS_TABLE=1)
	sql="SET param_kind = 'DELIVERED';
SET param_id_min = 10000;
SET param_id_max = 10030;
SELECT \`id:id\`, \`symbol:value\`, \`text:text\`
FROM anchor.facts
WHERE has(\`symbol:value\`, {kind:String})
  AND \`id:id\` BETWEEN {id_min:UInt64} AND {id_max:UInt64}
ORDER BY \`id:id\`"
}

scene_20_multi_statement() {
	desc="Editor — a multi-statement buffer: the caret's statement is tinted, the gutter marks it, and Run ships just that one with its SET prelude"
	senv=(BOXER_PLAY_FOCUS_TABLE=1)
	sql="SET param_kind = 'PORT_SCAN';

SELECT count() FROM anchor.facts;

SELECT \`id:id\`, \`symbol:value\`, \`text:text\`
FROM anchor.facts
WHERE has(\`symbol:value\`, {kind:String});

SELECT 'a third statement, not run' AS note"
}

scene_21_error_state() {
	desc="Error state — a server-side failure surfaced in the status bar and the result pane, with Diagnostics carrying the detail"
	senv=(BOXER_PLAY_FOCUS_TABLE=1)
	sql="SELECT no_such_column, 1 / 0 AS boom
FROM anchor.facts
WHERE this_is_not_sql"
}

scene_22_empty_result() {
	desc="Empty result — the zero-row rendering across the panels, distinct from 'nothing has run yet'"
	senv=(BOXER_PLAY_FOCUS_TABLE=1)
	sql="SELECT * FROM anchor.facts WHERE 1 = 0"
}

scene_23_wide_result() {
	desc="Table — a wide leeway result: column-width resolution and horizontal scrolling over every physical column of the fixture"
	senv=(BOXER_PLAY_FOCUS_TABLE=1)
	sql="SELECT * FROM anchor.facts ORDER BY \`id:id\` LIMIT 100"
	settle=2500
}

scene_24_big_scan() {
	desc="Progress — a long scan: the live progress/ETA readout in the status bar, driven by the server's own elapsed and row counters"
	senv=(BOXER_PLAY_FOCUS_TABLE=1)
	sql="SELECT t AS aircraft_type, count() AS pings, round(avg(altitude)) AS avg_alt, round(max(ground_speed)) AS max_speed
FROM default.planes_mercator
GROUP BY t
ORDER BY pings DESC
LIMIT 50"
	settle=3000
}

# --- gesture scenes ----------------------------------------------------------
# These reach states that are not launch state at all — a menu, a toggle, a
# selection. No environment knob can seed them: they exist only as the
# consequence of an interaction, which is why the tour needs a driver and not
# just more knobs.

scene_25_panes_menu() {
	desc="Panes menu — the ADR-0097 prose surface: every pane, and for a rejecting one the reason its channels cannot be filled"
	senv=(BOXER_PLAY_FOCUS_TABLE=1)
	sql="SELECT t, count() AS n FROM default.planes_mercator_sample100 WHERE t != '' GROUP BY t ORDER BY n DESC"
	steps='{"do":"click","name":"Panes","comment":"a menu, not a launch state"}
{"do":"capture","text":"25_panes_menu","settleMs":600}'
}

scene_26_subquery_toggle() {
	desc="Subquery toggle — the editor's account of the query the caret is in: the tinted extent, its underlined environment, and the Run subquery button the toggle adds"
	senv=(BOXER_PLAY_FOCUS_TABLE=1)
	sql="WITH busy AS (
  SELECT t, count() AS n FROM default.planes_mercator_sample100 WHERE t != '' GROUP BY t
)
SELECT t, n FROM busy WHERE n > (SELECT avg(n) FROM busy) ORDER BY n DESC"
	steps='{"do":"click","name":"Subquery","role":"check_box"}
{"do":"capture","text":"26_subquery_toggle","settleMs":600}'
}

scene_27_conditions_toggle() {
	desc="Conditions — the ADR-0121 selection-conditions chrome, off by default and reachable only by clicking it on"
	senv=(BOXER_PLAY_FOCUS_TABLE=1)
	sql="SELECT icao, r, t, altitude FROM default.planes_mercator_sample100 WHERE altitude > 30000 AND ground_speed > 400 ORDER BY icao LIMIT 100"
	steps='{"do":"click","name":"Conditions","role":"check_box"}
{"do":"capture","text":"27_conditions_toggle","settleMs":600}'
}

scene_28_endpoint_menu() {
	desc="Endpoint menu — the connection switcher, including the ad-hoc dataset endpoints of ADR-0134"
	senv=(BOXER_PLAY_FOCUS_TABLE=1)
	sql="SELECT 1 AS hello, now() AS ts"
	steps='{"do":"click","name":"Endpoint"}
{"do":"capture","text":"28_endpoint_menu","settleMs":600}'
}

scene_29_history_two_runs() {
	desc="History — more than one run in the list, which needs a second Run and so cannot be seeded at launch"
	senv=(BOXER_PLAY_FOCUS_TABLE=1)
	sql="SELECT t, count() AS n FROM default.planes_mercator_sample100 WHERE t != '' GROUP BY t ORDER BY n DESC LIMIT 20"
	# The dock's tab strip is custom-painted and carries no accessibility nodes,
	# so a tab cannot be resolved by name — this is the ladder's last rung, and
	# the honest use for it. The coordinate holds because the window size is
	# pinned; it is also why the BOXER_PLAY_FOCUS_* knobs still earn their keep
	# for body tabs, which they can select at launch without any of this.
	steps='{"do":"click","name":"Run","comment":"a second run, so History has two entries"}
{"do":"click","x":164,"y":149,"comment":"History tab — no node, so by position"}
{"do":"capture","text":"29_history_two_runs","settleMs":800}'
}

scene_30_regex_affordance() {
	desc="Affordances — the inline regex tester the editor attaches to a recognised multiMatch* call: each pattern compiled in-process and counted against a shared test input"
	# The tester is driven by the DEBOUNCED parse, not by Run: the call site is
	# reported by the FunctionEvaluator that rides the canonicalisation
	# pipeline, which registers the multiMatch* family with a discard handler —
	# it observes the call and folds nothing, so the arguments survive verbatim
	# in the buffer for the affordance to slice. Nothing has to run for the
	# block to appear; the Run below is only what fills the result panes.
	senv=(BOXER_PLAY_FOCUS_TABLE=1)
	# The haystack is a column reference, so the call cannot be constant-folded
	# — hence the "(non-literal haystack)" hint beside the header. The patterns
	# are chosen to separate on one test string: 'AIR ?LINES' hits twice
	# (AIRLINES and AIR LINES), the other two once each.
	#
	# The query is spread over eight lines on purpose. The scroll below takes
	# the pane to its end, which cuts the first three editor lines out of frame;
	# folding this onto four lines would take the multiMatchAny call — the one
	# line the capture is about — with them.
	sql="SELECT ownOp     AS operator,
       uniq(icao) AS aircraft,
       count()    AS pings
FROM default.planes_mercator_sample100
WHERE multiMatchAny(ownOp, ['AIR ?LINES', '^(FEDERAL|UNITED)', 'TRUSTEE\$'])
GROUP BY operator
ORDER BY pings DESC
LIMIT 20"
	# The test input is addressed by index, not by name. An untouched TextEdit
	# carries neither an accessible name nor a value, so it is nameless in the
	# tree — and `--dumpTree` does not even list it, since that skips nodes with
	# neither. Two of them are on screen: the Docs pane's look-up field first,
	# this one second. An out-of-range index is an error rather than a silent
	# pick of the wrong field, so the tour fails loudly if that order ever
	# changes.
	#
	# focus and type are separate steps ON PURPOSE. egui applies an injected
	# AccessKit focus request on the frame after it arrives, so text sent in the
	# same batch is delivered to whatever had focus before — nothing — and is
	# dropped without an error. The settle between them is what makes the text
	# land. Patterns are compiled here by Go's regexp (RE2) while the server
	# runs the same strings through VectorScan (ADR-0054 §SD1), so the counts
	# are a tuning aid, not the server's own verdict.
	#
	# The scroll is not decoration: the editor reserves a FIXED strip under
	# itself for the affordance block, and a third pattern row overflows it. The
	# pane scrolls, the strip does not grow, so without this the capture ends at
	# pattern [0]. The pointer has to be parked inside the pane first — a wheel
	# event goes to whatever is hovered — and below the editor box, or the
	# editor's own internal scroll eats it.
	steps='{"do":"focus","role":"text_input","nth":1,"comment":"the shared test input — nameless, so by index","settleMs":400}
{"do":"type","role":"text_input","nth":1,"text":"UNITED AIRLINES INC / DELTA AIR LINES INC TRUSTEE"}
{"do":"click","x":600,"y":520,"comment":"park the pointer over the affordance block"}
{"do":"scroll","x":0,"y":-200,"settleMs":500}
{"do":"capture","text":"30_regex_affordance","settleMs":800}'
}

scene_31_experiments_topology() {
	desc="Experiments — the leeway sink playground: the built-in fixture driven through the TopologySink, whose treemap draws an entity's shape (plain sections, the co-section group, attributes) with value/tag presence as cell colour"
	senv=(BOXER_PLAY_FOCUS_EXPERIMENTS=1)
	# The pane's default source is the built-in leeway fixture, so it draws
	# without a result — that is the point of defaulting to it. The query below
	# exists only to satisfy the prelude, which waits on Run and then holds; the
	# scene reads nothing from the result panes.
	sql="SELECT 1 AS ok"
	# The sink defaults to `card`, so the topology treemap needs one click. The
	# segmented selector renders each option as a named button, so it is
	# addressable by label rather than by index.
	steps='{"do":"click","name":"topology","comment":"switch the sink from the default card view","settleMs":600}
{"do":"capture","text":"31_experiments_topology","settleMs":800}'
}

scene_32_experiments_sparks() {
	desc="Experiments — the same fixture through the text sinks: the topology sparkline, one line per entity, encoding section arity, column canonical types and membership counts without printing a single value"
	senv=(BOXER_PLAY_FOCUS_EXPERIMENTS=1)
	sql="SELECT 1 AS ok"
	# Three captures from one launch: the sinks are cheap to switch and each is
	# a different reading of the same callback sequence, which is the pane's
	# whole argument.
	steps='{"do":"click","name":"topo","comment":"the single-line topology sparkline","settleMs":600}
{"do":"capture","text":"32_experiments_sparks_topo","settleMs":600}
{"do":"click","name":"braille","comment":"the braille density variant","settleMs":600}
{"do":"capture","text":"32_experiments_sparks_braille","settleMs":600}
{"do":"click","name":"json","comment":"the canonical card-JSON of ADR-0018","settleMs":600}
{"do":"capture","text":"32_experiments_sparks_json","settleMs":800}'
}

scene_33_experiments_result_card() {
	desc="Experiments — the card sink over the CURRENT result rather than the fixture, drawn beside the Detail tab that renders the same emitter: the pane owns its own CardDriver so the two do not share widget ids"
	senv=(BOXER_PLAY_FOCUS_EXPERIMENTS=1)
	# Detail claims its channel from `selection`, so the pinned param puts a row
	# in the same signal env it reads — Detail then renders its own card in the
	# same frame the pane renders its own. Both drive a Table2CardEmitter, which
	# derives cell ids from a per-section counter, so sharing one CardDriver
	# between them emits every id twice and egui reports the clash.
	# LIMIT 1 rather than a pinned id: the scene has to actually RETURN a row or
	# both cards short-circuit to their empty notice and the frame proves
	# nothing about id sharing.
	sql="SET param_selection = 0;
SELECT * FROM anchor.facts ORDER BY \`id:id\` LIMIT 1"
	steps='{"do":"click","name":"result","comment":"switch the source off the fixture","settleMs":800}
{"do":"capture","text":"33_experiments_result_card","settleMs":800}'
}

# =============================================================================
# Fixtures the scenes read. Checked once, up front, so a missing one is a line
# of output rather than twenty screenshots of an error state.
# =============================================================================
fixtures() {
	cat <<-'EOF'
	anchor.facts|apps/play/help/howto-example-queries.md — go test -tags="$(cat ./tags),integration" -run TestLeewayClickHouse ./public/semistructured/leeway/anchor/
	default.planes_mercator_sample100|apps/play/demo/adsb/demo.sh
	default.planes_mercator|apps/play/demo/adsb/demo.sh
	system.documentation|nothing to load, but the server must be new enough — ClickHouse ships this table from 26.x, and on an older one the Docs scene captures the pane saying it has no answer
	EOF
}

# =============================================================================
# Runner
# =============================================================================

log() { printf '%s\n' "$*" >&2; }
die() { log "play-screenshot-tour: $*"; exit 1; }

list_scenes() {
	declare -F | awk '{print $3}' | grep '^scene_' | sort
}

if [[ -n "${PLAYSHOT_LIST:-}" ]]; then
	for fn in $(list_scenes); do
		desc=""; sql=""; senv=(); steps=""; settle=""
		"$fn"
		printf '%-28s %s\n' "${fn#scene_}" "$desc"
	done
	exit 0
fi

# Scene filter: any positional argument is a substring pattern.
selected=()
for fn in $(list_scenes); do
	if (($# == 0)); then
		selected+=("$fn")
		continue
	fi
	for pat in "$@"; do
		if [[ "$fn" == *"$pat"* ]]; then selected+=("$fn"); break; fi
	done
done
((${#selected[@]})) || die "no scene matches: $*"

mkdir -p "$OUT/logs" || die "cannot create $OUT"

# --- ClickHouse + fixtures ---------------------------------------------------
ch() { curl -sS --max-time 10 --get --data-urlencode "query=$1" "$CLICKHOUSE_URL" 2>&1; }
ch "SELECT 1" >/dev/null 2>&1 || die "no ClickHouse at $CLICKHOUSE_URL (set CLICKHOUSE_URL)"
if [[ "$CHECK_FIXTURES" == 1 ]]; then
	missing=0
	while IFS='|' read -r tbl how; do
		[[ -n "$tbl" ]] || continue
		n=$(ch "SELECT count() FROM $tbl")
		if [[ ! "$n" =~ ^[0-9]+$ ]] || ((n == 0)); then
			log "missing/empty fixture: $tbl — load with: $how"
			missing=1
		fi
	done < <(fixtures)
	((missing == 0)) || die "load the fixtures above, or re-run with PLAYSHOT_CHECK_FIXTURES=0"
fi

# --- binaries ----------------------------------------------------------------
if [[ "$BUILD" == 1 ]]; then
	mkdir -p "$BIN"
	log "building a private host into $BIN (PLAYSHOT_BUILD=0 to reuse rust/imzero2/)"
	# Same tags as rust/imzero2/build_go.sh: the repo tags plus binary_log,
	# which the imzero2 host's keelson logbridge decodes.
	( cd "$root" && CGO_ENABLED=0 go build -tags "$(tr -d '\n' < ./tags),binary_log" \
		-o "$BIN/main_go" ./public/thestack/cmd/imzero2/ ) || die "go build failed"
	# The Rust client only replays Go-emitted opcodes, so it needs no rebuild —
	# but it MUST come from the same codegen as the Go host. Copying pins it
	# against a concurrent rebuild of the shared tree mid-tour.
	# The HEADLESS client: it links no window system, which is what lets this
	# tour run with no compositor. Built by rust/imzero2/build_rust_headless.sh
	# into its own target dir, separate from the desktop build.
	[[ -x "$root/rust/imzero2/target/headless/release/imzero2" ]] ||
		die "no headless client at rust/imzero2/target/headless/release/imzero2 — run rust/imzero2/build_rust_headless.sh"
	# -p preserves the mtime, which the staleness guard below compares against.
	# Without it the copy is stamped NOW, is therefore newer than any codegen,
	# and the guard can never fire — which is how a stale client reached a tour
	# silently on 2026-08-05 and read as a panel bug for half an hour.
	cp -fp "$root/rust/imzero2/target/headless/release/imzero2" "$BIN/imzero2" ||
		die "cannot copy the headless client"
	# Staleness guard. The Go host is rebuilt from the tree just above; the
	# Rust client is not, and a client older than the last egui2 codegen
	# desyncs the FFFI wire ("unable to convert from representation") on
	# whichever opcode moved — often only on some scenes, which reads like a
	# scene bug rather than a build one.
	for gen in "$root/rust/imzero2/src/imzero2/enums_out.rs" \
	           "$root/rust/imzero2/src/imzero2/interpreter.rs"; do
		if [[ "$gen" -nt "$BIN/imzero2" ]]; then
			log "WARNING: $(basename "$gen") is newer than the headless client —"
			log "         rebuild it with rust/imzero2/build_rust_headless.sh, or expect an FFFI desync"
			break
		fi
	done
fi
MAIN_GO="${PLAYSHOT_MAIN_GO:-$BIN/main_go}"
CLIENT="${PLAYSHOT_CLIENT:-$BIN/imzero2}"
[[ "$BUILD" == 1 ]] || { MAIN_GO="${PLAYSHOT_MAIN_GO:-$root/rust/imzero2/main_go}"
                         CLIENT="${PLAYSHOT_CLIENT:-$root/rust/imzero2/target/headless/release/imzero2}"; }
[[ -x "$MAIN_GO" ]] || die "no Go host at $MAIN_GO"
[[ -x "$CLIENT" ]] || die "no Rust client at $CLIENT"

# --- fonts (mirrors rust/imzero2/hmi.sh) -------------------------------------
resolve_font() { # resolve_font <family> <substring the match must contain>
	local line file fam
	command -v fc-match >/dev/null 2>&1 || return 0
	line=$(fc-match -f '%{file}\t%{family}\n' "$1" 2>/dev/null) || return 0
	file="${line%%$'\t'*}"; fam="${line#*$'\t'}"
	[[ "$fam" == *"$2"* && -f "$file" ]] && printf '%s' "$file"
}
MAIN_FONT="${MAIN_FONT:-$(resolve_font 'Noto Sans' 'Noto Sans')}"
MONO_FONT="${MONO_FONT:-$(resolve_font 'DejaVu Sans Mono' 'DejaVu Sans Mono')}"
FALLBACK_FONT="${FALLBACK_FONT:-$(resolve_font 'Noto Sans Mono CJK JP' 'CJK')}"
PHOSPHOR_FONT="${PHOSPHOR_FONT:-$root/rust/imzero2/assets/fonts/phosphor/Phosphor.ttf}"

# --- geometry ----------------------------------------------------------------
W=${SIZE%%[xX]*}; H=${SIZE##*[xX]}
# The app opens as a window inside the host viewport. Size it to the viewport
# minus the host's own chrome so the capture is the app and almost nothing
# else; without the hint it falls back to a generic archetype narrow enough
# that play's tab strip truncates.
WIN_W=$((W - 2 * 16))
WIN_H=$((H - 100))

# --- capture -----------------------------------------------------------------
ok=0; failed=0
index="$OUT/index.md"
{
	printf '# play — feature tour\n\n'
	printf 'Captured by `scripts/dev/play-screenshot-tour.sh` at %s.\n' "$(date -Is)"
	printf 'One headless launch per scene (ADR-0024), gestures replayed by\n'
	printf '`imzero2 drive` against the accessibility tree (ADR-0154). No compositor.\n\n'
} >"$index"

for fn in "${selected[@]}"; do
	desc=""; sql=""; senv=(); steps=""; settle=""
	"$fn"
	name="${fn#scene_}"
	png="$OUT/$name.png"
	trace="$OUT/logs/$name.jsonl"
	rm -f "$png"
	# Default trace: capture the launch-seeded state. A scene that sets `steps`
	# replaces it — the gestures still have to end in a capture of their own.
	# Built in two statements, not one `${steps:-…}`: the default is JSON, and
	# its own `}` would close the parameter expansion early.
	if [[ -z "$steps" ]]; then
		steps="{\"do\":\"capture\",\"text\":\"$name\"}"
	fi
	# Prelude, prepended to every trace: wait until the app has mounted (the Run
	# button is the earliest thing with a stable name), then hold for the query
	# and the panel to settle. Taking capture control away from the app means
	# nothing else knows when a result has landed — without this the driver
	# captures a half-mounted window, which is what it did the first time.
	{
		printf '{"do":"wait","name":"Run","comment":"the app has mounted"}\n'
		printf '{"do":"sleep","settleMs":%s}\n' "${settle:-$SCENE_SETTLE_MS}"
		printf '%s\n' "$steps"
	} >"$trace"

	start=$SECONDS
	# The host renders offscreen and answers the driver; it exits when the
	# driver's run is over and we tear it down. IMZERO2_HEADLESS_DUMP_DIR is
	# the directory the host writes captures into — the trace names the file,
	# the host owns the directory (ADR-0154 SD4) — and DUMP_EVERY is pushed out
	# of the way so nothing lands except what the trace asked for.
	env -u DISPLAY -u WAYLAND_DISPLAY \
		IMZERO2_HEADLESS_LISTEN="127.0.0.1:$PORT" \
		IMZERO2_HEADLESS_DUMP_DIR="$OUT" \
		IMZERO2_HEADLESS_DUMP_EVERY=1000000 \
		IMZERO2_HEADLESS_FPS=30 \
		IMZERO2_SCREENSHOT_SIZE="$SIZE" \
		BOXER_COMPONENT=play-screenshot-tour \
		BOXER_PLAY_WINDOW_SIZE="${WIN_W}x${WIN_H}" \
		BOXER_PLAY_SQL="$sql" \
		BOXER_PLAY_AUTORUN=1 \
		"${senv[@]}" \
		"$MAIN_GO" --logFormat=console --logLevel=warn \
			imzero2 demo \
			--clientBinary "$CLIENT" \
			--clientInitialMainWindowWidth "$W" \
			--clientInitialMainWindowHeight "$H" \
			${MAIN_FONT:+--mainFontTTF "$MAIN_FONT"} \
			${MONO_FONT:+--monoFontTTF "$MONO_FONT"} \
			${PHOSPHOR_FONT:+--phosphorFontTTF "$PHOSPHOR_FONT"} \
			${FALLBACK_FONT:+--fallbackFontTTF "$FALLBACK_FONT"} \
			--launch play \
		>"$OUT/logs/$name.log" 2>&1 &
	app_pid=$!

	# Wait for the carrier to accept connections before driving it. The host
	# has a Rust client to spawn and a query to run first.
	for _ in $(seq 1 $((TIMEOUT * 4))); do
		(exec 3<>"/dev/tcp/127.0.0.1/$PORT") 2>/dev/null && { exec 3<&-; break; }
		kill -0 "$app_pid" 2>/dev/null || break
		sleep 0.25
	done

	timeout "$TIMEOUT" "$MAIN_GO" --logFormat=console --logLevel=info \
		imzero2 drive --url "ws://127.0.0.1:$PORT/" --trace "$trace" \
		--settle "$SETTLE_MS" ${DRY:+--dryRun} \
		>>"$OUT/logs/$name.log" 2>&1
	rc=$?

	# Kill THIS run only, by pid: a pattern kill over the shared binary path
	# would take out a concurrent session's client (and match the poll shell).
	# The Rust client is a child of the Go host, so reap it explicitly.
	kids=$(pgrep -P "$app_pid" 2>/dev/null)
	kill "$app_pid" $kids 2>/dev/null
	wait "$app_pid" 2>/dev/null
	# The RUST client owns the carrier socket and outlives the Go host it is a
	# child of. Without waiting for the port to actually free, the next scene's
	# client cannot bind, the connect-poll above finds the OLD listener still
	# up, and the driver attaches to the PREVIOUS scene's app: it captures that
	# scene's window and then fails to find this scene's widgets, which reads
	# as a panel bug rather than a teardown race.
	for _ in $(seq 1 40); do
		(exec 3<>"/dev/tcp/127.0.0.1/$PORT") 2>/dev/null || break
		sleep 0.25
	done
	took=$((SECONDS - start))

	# Only what THIS scene wrote. A plain $name*.png glob also matches a
	# longer-named sibling scene (08_treemap vs 08_treemap_self) and counted
	# stale files from earlier runs, so a scene that captured nothing could
	# still report png.
	shots=$(find "$OUT" -maxdepth 1 -name "$name*.png" -newer "$trace" 2>/dev/null | wc -l)
	if [[ -n "$DRY" ]]; then
		if ((rc == 0)); then
			ok=$((ok + 1)); log "  ok   $name  (${took}s, dry run — anchors resolved)"
		else
			failed=$((failed + 1)); log "  FAIL $name  (${took}s, rc=$rc) — see $OUT/logs/$name.log"
		fi
	elif ((shots > 0 && rc == 0)); then
		ok=$((ok + 1))
		log "  ok   $name  (${took}s, $shots png)"
	else
		failed=$((failed + 1))
		log "  FAIL $name  (${took}s, rc=$rc, $shots png) — see $OUT/logs/$name.log"
	fi

	{
		printf '## %s\n\n%s\n\n' "$name" "$desc"
		for f in "$OUT/$name"*.png; do
			[[ -s "$f" ]] && printf '![%s](%s)\n\n' "$name" "$(basename "$f")"
		done
		printf 'Knobs: `%s`\n\n' "${senv[*]:-—}"
		[[ -n "$steps" ]] && printf 'Trace:\n```json\n%s\n```\n\n' "$steps"
		printf '```sql\n%s\n```\n\n' "$sql"
	} >>"$index"
done

# The periodic dump sink always writes frame 0 (0 % anything is 0), so one
# stray frame lands beside the named captures however far DUMP_EVERY is pushed.
rm -f "$OUT/frame_000000.png"

log ""
log "captured $ok scene(s), $failed failed → $OUT"
log "index: $index"
((failed == 0))
