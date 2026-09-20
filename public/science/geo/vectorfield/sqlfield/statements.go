package sqlfield

import (
	"regexp"
	"strconv"
	"strings"
)

// The reserved column names of a field relation (ADR-0250 §SD1).
const (
	// ColTime is the step: a Date, DateTime or DateTime64. Optional.
	ColTime = "t"
	// ColLat is degrees north.
	ColLat = "lat"
	// ColLon is degrees east, in either the -180…180 or the 0…360 convention.
	ColLon = "lon"
	// ColU is the earth-relative eastward component.
	ColU = "u"
	// ColV is the earth-relative northward component.
	ColV = "v"
)

// ParamPrefix starts every parameter and alias the package's statements use.
// A relation must not define a column or read a parameter with it.
const ParamPrefix = "ff_"

// Relation is where a field's rows come from.
//
// Both fields are statement text and are trusted as the caller's own SQL;
// nothing a user types as a value belongs in them.
type Relation struct {
	// Head precedes the package's SELECT: SET statements each ended by a
	// semicolon, then at most one WITH list without a trailing comma. Empty
	// for a plain table.
	Head string
	// From is what the SELECT reads: a table, a table function, or the name
	// of an item of Head's WITH list.
	From string
}

func (inst Relation) statement(tail string) string {
	if strings.TrimSpace(inst.Head) == "" {
		return tail
	}
	return inst.Head + "\n" + tail
}

// timeTypePattern is the closed set of type names the step slot may be typed
// with. The name comes from the server's toTypeName and becomes statement
// text (ADR-0250 §SD3), so it is matched whole and never merely escaped.
var timeTypePattern = regexp.MustCompile(`^(Date|Date32|DateTime(\('[A-Za-z0-9_/+\-]+'\))?|DateTime64\([0-9](, ?'[A-Za-z0-9_/+\-]+')?\))$`)

// ProbeStatement reads the relation's schema and no rows. What it returns is
// what [ShapeOf] judges.
func ProbeStatement(rel Relation) string {
	return rel.statement("SELECT * FROM " + rel.From + " LIMIT 0")
}

// stepsStatement lists the steps: the server's own text of each value, which
// is what the step slot is later filled with, its instant, the column's type
// and how many rows the step holds.
func stepsStatement(rel Relation) string {
	return rel.statement(`SELECT
    toString(t) AS ff_text,
    toUnixTimestamp64Milli(toDateTime64(t, 3, 'UTC')) AS ff_ms,
    toTypeName(t) AS ff_type,
    count() AS ff_count
FROM ` + rel.From + `
GROUP BY t
ORDER BY t ASC
LIMIT {ff_cap:UInt64}`)
}

func stepPredicate(timeType string) string {
	if timeType == "" {
		return "1"
	}
	return "t = {ff_t:" + timeType + "}"
}

// geometryStatement reads one step's extent and node counts, and a high
// quantile of the magnitude for a palette to span.
func geometryStatement(rel Relation, timeType string) string {
	return rel.statement(`SELECT
    min(ff_lat) AS ff_south,
    max(ff_lat) AS ff_north,
    min(ff_lon) AS ff_west,
    max(ff_lon) AS ff_east,
    uniqExact(ff_lat) AS ff_rows,
    uniqExact(ff_lon) AS ff_cols,
    count() AS ff_count,
    toFloat64(quantileIf(0.995)(ff_speed, isFinite(ff_speed))) AS ff_speed_high
FROM (
    SELECT
        toFloat64(lat) AS ff_lat,
        toFloat64(lon) AS ff_lon,
        sqrt(toFloat64(u) * toFloat64(u) + toFloat64(v) * toFloat64(v)) AS ff_speed
    FROM ` + rel.From + `
    WHERE ` + stepPredicate(timeType) + `
)`)
}

// regularityStatement measures how far, in cells, the step's nodes stand off
// the regular grid the geometry implies.
func regularityStatement(rel Relation, timeType string) string {
	return rel.statement(`SELECT
    max(abs(ff_x - round(ff_x))) AS ff_off_x,
    max(abs(ff_y - round(ff_y))) AS ff_off_y
FROM (
    SELECT
        (toFloat64(lon) - {ff_west:Float64}) / {ff_dlon:Float64} AS ff_x,
        ({ff_north:Float64} - toFloat64(lat)) / {ff_dlat:Float64} AS ff_y
    FROM ` + rel.From + `
    WHERE ` + stepPredicate(timeType) + `
)`)
}

// windowStatement reduces the native nodes of one step inside a plan's bounds
// to its bins: the vector mean, the scalar mean of the magnitude and the
// count of valid nodes per bin.
//
// The text depends on the relation and the type of t alone — every value of a
// request is a parameter — so one relation is one statement to a query log or
// a cache (ADR-0250 §SD3).
//
// The inner predicates are written on the relation's own lat, lon and t so
// that they can reach a primary key through the relation; the outer ones are
// the exact integer bounds. Integer division is intDiv and not the DIV
// operator, which the canonicaliser in front of play's executor does not
// round-trip (the constraint ADR-0096's raster template records).
//
// A NULL or non-finite component makes ff_ok NULL or 0, and the -If
// aggregates count neither.
func windowStatement(rel Relation, timeType string) string {
	return rel.statement(`SELECT
    intDiv(ff_ri - {ff_row_start:Int64}, {ff_factor:Int64}) AS ff_r,
    intDiv(ff_ciu - {ff_col_start:Int64}, {ff_factor:Int64}) AS ff_c,
    toFloat32(avgIf(ff_u, ff_ok)) AS ff_mean_u,
    toFloat32(avgIf(ff_v, ff_ok)) AS ff_mean_v,
    toFloat32(avgIf(sqrt(ff_u * ff_u + ff_v * ff_v), ff_ok)) AS ff_mean_speed,
    toUInt32(countIf(ff_ok)) AS ff_valid
FROM (
    SELECT
        toFloat64(u) AS ff_u,
        toFloat64(v) AS ff_v,
        isFinite(ff_u) AND isFinite(ff_v) AS ff_ok,
        toInt64(round(({ff_north:Float64} - toFloat64(lat)) / {ff_dlat:Float64})) AS ff_ri,
        toInt64(round((toFloat64(lon) - {ff_west:Float64}) / {ff_dlon:Float64})) + arrayJoin({ff_turns:Array(Int64)}) AS ff_ciu
    FROM ` + rel.From + `
    WHERE ` + stepPredicate(timeType) + `
        AND lat >= {ff_lat_min:Float64} AND lat <= {ff_lat_max:Float64}
        AND ((lon >= {ff_lon_min1:Float64} AND lon <= {ff_lon_max1:Float64})
            OR (lon >= {ff_lon_min2:Float64} AND lon <= {ff_lon_max2:Float64}))
)
WHERE ff_ri >= {ff_row_start:Int64} AND ff_ri <= {ff_row_end:Int64}
    AND ff_ciu >= {ff_col_start:Int64} AND ff_ciu <= {ff_col_end:Int64}
GROUP BY ff_r, ff_c
LIMIT {ff_cap:UInt64}`)
}

func formatFloat(v float64) string { return strconv.FormatFloat(v, 'g', -1, 64) }

func formatInt(v int64) string { return strconv.FormatInt(v, 10) }

func formatInts(vs []int64) string {
	var b strings.Builder
	b.WriteByte('[')
	for i, v := range vs {
		if i > 0 {
			b.WriteByte(',')
		}
		b.WriteString(formatInt(v))
	}
	b.WriteByte(']')
	return b.String()
}

// windowParams are the values of a plan's window statement, by bare name.
func (inst grid) windowParams(p *windowPlan, stepText string, hasTime bool) (params map[string]string) {
	params = map[string]string{
		"ff_north":     formatFloat(inst.north),
		"ff_west":      formatFloat(inst.west),
		"ff_dlat":      formatFloat(inst.dLat),
		"ff_dlon":      formatFloat(inst.dLon),
		"ff_factor":    formatInt(int64(p.factor)),
		"ff_row_start": formatInt(p.rowStart),
		"ff_row_end":   formatInt(p.rowEnd),
		"ff_col_start": formatInt(p.colStart),
		"ff_col_end":   formatInt(p.colEnd),
		"ff_turns":     formatInts(p.turns),
		"ff_lat_min":   formatFloat(p.latRange[0]),
		"ff_lat_max":   formatFloat(p.latRange[1]),
		"ff_lon_min1":  formatFloat(p.lonRanges[0][0]),
		"ff_lon_max1":  formatFloat(p.lonRanges[0][1]),
		"ff_lon_min2":  formatFloat(p.lonRanges[1][0]),
		"ff_lon_max2":  formatFloat(p.lonRanges[1][1]),
		// One past what the plan can hold, so a reply that overruns it is
		// seen as one and not cut to fit.
		"ff_cap": formatInt(int64(p.cols)*int64(p.rows) + 1),
	}
	if hasTime {
		params["ff_t"] = stepText
	}
	return
}

// Statements lists every statement a source over rel sends, for a host whose
// executor rewrites SQL to check that the rewrite keeps them whole. timeType
// is the type of t, or empty for a relation without one.
func Statements(rel Relation, timeType string) (statements []string) {
	statements = []string{
		ProbeStatement(rel),
		geometryStatement(rel, timeType),
		regularityStatement(rel, timeType),
		windowStatement(rel, timeType),
	}
	if timeType != "" {
		statements = append(statements, stepsStatement(rel))
	}
	return
}
