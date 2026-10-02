//go:build integration

// Package maptiles is the harness of the map-tile-addressing trial
// (../README.md). It measures, and builds nothing: the Map pane's raster
// template and projection are copied here from apps/play, pinned to the commit
// the run's logbook entry names, so that a refactor of the pane cannot move a
// run's numbers silently. Every query goes to the ClickHouse server at
// CLICKHOUSE_URL (default the local one) and every result lands under
// MTA_RUN_DIR; without MTA_RUN_DIR the tests skip.
package maptiles

import (
	"bufio"
	"bytes"
	"context"
	"fmt"
	"io"
	"math"
	"net/http"
	"net/url"
	"os"
	"path/filepath"
	"strconv"
	"strings"
	"testing"
	"time"
)

// ---- copied from apps/play/play_map.go (see the package comment) ----------

const mercWorld = 4294967296.0 // 2^32
const mercUnitMax = 4294967295

func lonToMercX(lon float64) uint32 {
	return clampMerc(math.Round(mercWorld * (lon + 180.0) / 360.0))
}

func latToMercY(lat float64) uint32 {
	lat = clampLat(lat)
	return clampMerc(math.Round(mercWorld * (0.5 - math.Asinh(math.Tan(lat/180.0*math.Pi))/(2.0*math.Pi))))
}

func clampLat(lat float64) float64 {
	const lim = 85.05112878
	return max(min(lat, lim), -lim)
}

func clampMerc(v float64) uint32 {
	if v < 0 {
		return 0
	}
	if v > mercUnitMax {
		return mercUnitMax
	}
	return uint32(v)
}

// altitudeSpeedColorSQL is the panel's default render.
const altitudeSpeedColorSQL = `greatest(0, least(avg(altitude), 45000)) / 45000 AS alt_t,
    greatest(0, least(avg(ground_speed), 600)) / 600 AS spd_t,
    0.22 + 0.58 * transparency AS lum,
    0.19 * sqrt(lum / 0.8) * (0.25 + 0.75 * sqrt(spd_t)) AS chroma,
    30 + 230 * sqrt(alt_t) AS hue,
    colorOKLCHToSRGB(tuple(lum, chroma, hue)) AS rgb,
    greatest(0, least(255, tupleElement(rgb, 1))) AS red,
    greatest(0, least(255, tupleElement(rgb, 2))) AS green,
    greatest(0, least(255, tupleElement(rgb, 3))) AS blue`

// rasterHeader is the template's geometry and density block, unchanged.
func rasterHeader(sampling uint32) string {
	return fmt.Sprintf(`WITH
    toUInt64({vp_max_x:UInt32}) - {vp_min_x:UInt32} AS span_x,
    toUInt64({vp_max_y:UInt32}) - {vp_min_y:UInt32} AS span_y,
    mercator_x >= {vp_min_x:UInt32} AND mercator_x < {vp_max_x:UInt32}
        AND mercator_y >= {vp_min_y:UInt32} AND mercator_y < {vp_max_y:UInt32} AS in_view,
    least(intDiv(toUInt64(mercator_x - {vp_min_x:UInt32}) * {vp_w:UInt32}, span_x), {vp_w:UInt32} - 1) AS px,
    least(intDiv(toUInt64(mercator_y - {vp_min_y:UInt32}) * {vp_h:UInt32}, span_y), {vp_h:UInt32} - 1) AS py,
    py * {vp_w:UInt32} + px AS pos,
    (span_x / {vp_w:UInt32}) * (span_y / {vp_h:UInt32}) AS pixel_area,
    pow(2, 22) / sqrt(pixel_area) AS zoom_factor,
    count() AS total,
    greatest(1000000. / %d / zoom_factor, toFloat64(count())) AS max_total,
    pow(total / max_total, 1/5) AS transparency,
    `, sampling)
}

// rasterTemplateSQL is the panel's template for the default render and no
// extra WHERE: sparse (pos, r, g, b, a) per non-empty pixel.
func rasterTemplateSQL(table string, sampling uint32) string {
	return rasterHeader(sampling) + altitudeSpeedColorSQL + `,
    255 AS alpha
SELECT toUInt32(pos), round(red)::UInt8, round(green)::UInt8, round(blue)::UInt8, round(alpha)::UInt8
FROM ` + table + `
WHERE in_view
GROUP BY pos`
}

// ---- trial-only shapes ------------------------------------------------------

// pngTemplateSQL is the same geometry and colour, with explicit x/y columns
// named for ClickHouse's PNG output format; the image size comes from
// output_format_image_width/height.
func pngTemplateSQL(table string, sampling uint32) string {
	return rasterHeader(sampling) + altitudeSpeedColorSQL + `,
    255 AS alpha
SELECT toUInt16(pos % {vp_w:UInt32}) AS x, toUInt16(intDiv(pos, {vp_w:UInt32})) AS y,
    round(red)::UInt8 AS r, round(green)::UInt8 AS g, round(blue)::UInt8 AS b, round(alpha)::UInt8 AS a
FROM ` + table + `
WHERE in_view
GROUP BY pos`
}

// densitySQL returns the normaliser's inputs per non-empty pixel, for the
// brightness check.
func densitySQL(table string, sampling uint32) string {
	return rasterHeader(sampling) + `1 AS unused
SELECT toUInt32(pos) AS p, total, max_total, transparency
FROM ` + table + `
WHERE in_view
GROUP BY pos`
}

// level is one rung of the sampling ladder, as play_map_ladder.go derives it
// for planes_mercator.
type level struct {
	table    string
	sampling uint32
}

var ladder = []level{
	{"planes_mercator_sample100", 100},
	{"planes_mercator_sample10", 10},
	{"planes_mercator", 1},
}

// box is a raster request: a mercator bbox and a raster size. maxX/maxY are
// held wide because a tile on the world's right or bottom edge ends at 2^32,
// which the template's {vp_*:UInt32} slots cannot carry.
type box struct {
	minX, maxX, minY, maxY uint64
	w, h                   uint32
}

func (b box) params() map[string]string {
	return map[string]string{
		"vp_min_x": strconv.FormatUint(b.minX, 10),
		"vp_max_x": strconv.FormatUint(b.maxX, 10),
		"vp_min_y": strconv.FormatUint(b.minY, 10),
		"vp_max_y": strconv.FormatUint(b.maxY, 10),
		"vp_w":     strconv.FormatUint(uint64(b.w), 10),
		"vp_h":     strconv.FormatUint(uint64(b.h), 10),
	}
}

func (b box) String() string {
	return fmt.Sprintf("%d-%d/%d-%d@%dx%d", b.minX, b.maxX, b.minY, b.maxY, b.w, b.h)
}

// tileBox is slippy tile (z, x, y) in the 2^32 mercator space at size px
// per side — what MVTBoundingBoxMercator(z, x, y) returns.
func tileBox(z, x, y int, size uint32) box {
	side := uint64(1) << (32 - z)
	return box{minX: uint64(x) * side, maxX: uint64(x+1) * side, minY: uint64(y) * side, maxY: uint64(y+1) * side, w: size, h: size}
}

// ---- server access ---------------------------------------------------------

func chURL() string {
	if u := os.Getenv("CLICKHOUSE_URL"); u != "" {
		return u
	}
	return "http://127.0.0.1:8123/"
}

func runDir(t *testing.T) string {
	t.Helper()
	d := os.Getenv("MTA_RUN_DIR")
	if d == "" {
		t.Skip("MTA_RUN_DIR unset")
	}
	if err := os.MkdirAll(d, 0o755); err != nil {
		t.Fatal(err)
	}
	return d
}

// runTag distinguishes this process's query ids and cache tags from any
// earlier run's.
var runTag = strconv.FormatInt(time.Now().Unix(), 36)

type result struct {
	body        []byte
	ttfb, total time.Duration
}

// exec POSTs sql with params on the param_* channel and settings as URL
// parameters, the way play's client ships a statement; format is appended.
// The body is read whole and its length is what came over the wire (no HTTP
// compression, as play's map lane).
func exec(ctx context.Context, sql string, params, settings map[string]string, format, queryID string) (r result, err error) {
	vals := url.Values{}
	vals.Set("query_id", queryID)
	for k, v := range params {
		vals.Set("param_"+k, v)
	}
	for k, v := range settings {
		vals.Set(k, v)
	}
	req, err := http.NewRequestWithContext(ctx, http.MethodPost, chURL()+"?"+vals.Encode(), strings.NewReader(sql+"\nFORMAT "+format))
	if err != nil {
		return
	}
	start := time.Now()
	resp, err := http.DefaultClient.Do(req)
	if err != nil {
		return
	}
	defer resp.Body.Close()
	br := bufio.NewReader(resp.Body)
	if _, err = br.Peek(1); err != nil && err != io.EOF {
		return
	}
	r.ttfb = time.Since(start)
	r.body, err = io.ReadAll(br)
	r.total = time.Since(start)
	if resp.StatusCode != http.StatusOK {
		err = fmt.Errorf("status %d: %s", resp.StatusCode, truncate(r.body))
	}
	return
}

func truncate(b []byte) string {
	if len(b) > 400 {
		b = b[:400]
	}
	return string(b)
}

func plain(ctx context.Context, sql string) (string, error) {
	req, err := http.NewRequestWithContext(ctx, http.MethodPost, chURL(), strings.NewReader(sql))
	if err != nil {
		return "", err
	}
	resp, err := http.DefaultClient.Do(req)
	if err != nil {
		return "", err
	}
	defer resp.Body.Close()
	b, err := io.ReadAll(resp.Body)
	if resp.StatusCode != http.StatusOK {
		return "", fmt.Errorf("status %d: %s", resp.StatusCode, truncate(b))
	}
	return string(b), err
}

// logRow is one finished query from system.query_log.
type logRow struct {
	durUs                   int64
	readRows, readBytes     int64
	resultRows, resultBytes int64
	cacheHits, cacheMisses  int64
}

// queryLog flushes the logs and returns the finished queries whose id starts
// with prefix, by id.
func queryLog(ctx context.Context, prefix string) (map[string]logRow, error) {
	if _, err := plain(ctx, "SYSTEM FLUSH LOGS"); err != nil {
		return nil, err
	}
	out, err := plain(ctx, fmt.Sprintf(`SELECT query_id,
    dateDiff('microsecond', query_start_time_microseconds, event_time_microseconds),
    read_rows, read_bytes, result_rows, result_bytes,
    ProfileEvents['QueryCacheHits'], ProfileEvents['QueryCacheMisses']
FROM system.query_log
WHERE type = 'QueryFinish' AND startsWith(query_id, '%s') AND event_date >= today() - 1
FORMAT TSV`, prefix))
	if err != nil {
		return nil, err
	}
	m := map[string]logRow{}
	for _, line := range strings.Split(strings.TrimSpace(out), "\n") {
		f := strings.Split(line, "\t")
		if len(f) != 8 {
			continue
		}
		n := make([]int64, 7)
		for i := range n {
			n[i], _ = strconv.ParseInt(f[i+1], 10, 64)
		}
		m[f[0]] = logRow{n[0], n[1], n[2], n[3], n[4], n[5], n[6]}
	}
	return m, nil
}

// tsv writes rows to name under the run directory.
func tsv(t *testing.T, name string, header string, rows []string) {
	t.Helper()
	var b bytes.Buffer
	b.WriteString(header + "\n")
	for _, r := range rows {
		b.WriteString(r + "\n")
	}
	if err := os.WriteFile(filepath.Join(runDir(t), name), b.Bytes(), 0o644); err != nil {
		t.Fatal(err)
	}
}
