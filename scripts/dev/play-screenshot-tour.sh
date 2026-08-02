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
# then captures a broken window. PLAYSHOT_BUILD=0 reuses what is already built.
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
	cp -f "$root/rust/imzero2/target/headless/release/imzero2" "$BIN/imzero2" ||
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
	took=$((SECONDS - start))

	shots=$(ls "$OUT/$name"*.png 2>/dev/null | wc -l)
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
