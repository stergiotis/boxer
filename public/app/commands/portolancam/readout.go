package portolancam

import (
	"bufio"
	"encoding/json/v2"
	"math"
	"os"
	"regexp"
	"strconv"

	"github.com/stergiotis/boxer/public/observability/eh/eb"
)

// Readout is one reading of the portolan demo's camera, recovered from the
// label nodes of an accessibility-tree dump. The JSON field names are the
// interchange format between `read` and the checking verbs, which the scene
// keeps on disk under its logs directory.
type Readout struct {
	Lat  float64 `json:"lat"`
	Lon  float64 `json:"lon"`
	Zoom float64 `json:"zoom"`

	// OX, OY, W, H are the canvas rect in screen pixels; `at` turns a
	// canvas-relative point into the screen point a gesture is aimed at.
	OX float64 `json:"ox"`
	OY float64 `json:"oy"`
	W  int     `json:"w"`
	H  int     `json:"h"`

	Requested int    `json:"requested"`
	Loaded    int    `json:"loaded"`
	Errors    int    `json:"errors"`
	Loading   string `json:"loading"`
	Reships   int    `json:"reships"`
}

// The four readout lines the demo paints, in the order this file reads them.
// A change to the demo's wording is a change here: the scene has no other
// instrument, so a silently unmatched line would read as a missing camera.
var (
	centreRe = regexp.MustCompile(`^centre (-?[\d.]+), (-?[\d.]+)\s+zoom ([\d.]+)`)
	canvasRe = regexp.MustCompile(`^canvas at (-?[\d.]+),(-?[\d.]+) · (\d+) × (\d+) px`)
	tilesRe  = regexp.MustCompile(`(\d+) requested · (\d+) loaded · (\d+) errors .* loading (\w+)`)
	reshipRe = regexp.MustCompile(`re-ships (\d+)`)
)

// node is the subset of a dumped accessibility node this reads — one object
// per line, as `imzero2 drive --dumpTree --treeFormat jsonl` prints them.
// Anything that does not parse as an object, or is not a label, is skipped
// rather than reported.
type node struct {
	Role  string `json:"role"`
	Value any    `json:"value"`
}

// present records which of the four lines were seen, so a missing field is
// reported by name instead of surfacing as a zero camera.
type present struct {
	centre bool
	canvas bool
	tiles  bool
	reship bool
}

// ReadTree parses an accessibility-tree dump into a Readout.
func ReadTree(path string) (r Readout, seen present, err error) {
	f, err := os.Open(path)
	if err != nil {
		err = eb.Build().Str("path", path).Errorf("open the tree dump: %w", err)
		return
	}
	defer func() { _ = f.Close() }()
	sc := bufio.NewScanner(f)
	// A dumped node carries its whole label text; the default 64 KiB token is
	// not guaranteed to hold one.
	sc.Buffer(make([]byte, 0, 64*1024), 4*1024*1024)
	for sc.Scan() {
		line := sc.Bytes()
		if len(line) == 0 || line[0] != '{' {
			continue
		}
		var n node
		if json.Unmarshal(line, &n) != nil || n.Role != "label" {
			continue
		}
		v, _ := n.Value.(string)
		if m := centreRe.FindStringSubmatch(v); m != nil {
			r.Lat, r.Lon, r.Zoom = atof(m[1]), atof(m[2]), atof(m[3])
			seen.centre = true
		}
		if m := canvasRe.FindStringSubmatch(v); m != nil {
			r.OX, r.OY, r.W, r.H = atof(m[1]), atof(m[2]), atoi(m[3]), atoi(m[4])
			seen.canvas = true
		}
		if m := tilesRe.FindStringSubmatch(v); m != nil {
			r.Requested, r.Loaded, r.Errors, r.Loading = atoi(m[1]), atoi(m[2]), atoi(m[3]), m[4]
			seen.tiles = true
		}
		if m := reshipRe.FindStringSubmatch(v); m != nil {
			r.Reships = atoi(m[1])
			seen.reship = true
		}
	}
	if scanErr := sc.Err(); scanErr != nil {
		err = eb.Build().Str("path", path).Errorf("scan the tree dump: %w", scanErr)
	}
	return
}

// Complete reports the first readout line that never matched, so the scene
// says which part of the camera the demo did not paint.
func (p present) Complete() (missing string, ok bool) {
	switch {
	case !p.centre:
		missing = "centre/zoom"
	case !p.canvas:
		missing = "canvas rect"
	case !p.tiles:
		missing = "tile counts"
	case !p.reship:
		missing = "re-ships"
	default:
		ok = true
	}
	return
}

// LoadReadout reads back a reading written by `read`.
func LoadReadout(path string) (r Readout, err error) {
	b, err := os.ReadFile(path)
	if err != nil {
		err = eb.Build().Str("path", path).Errorf("open the reading: %w", err)
		return
	}
	if err = json.Unmarshal(b, &r); err != nil {
		err = eb.Build().Str("path", path).Errorf("decode the reading: %w", err)
	}
	return
}

// pixel projects a geographic point to web-mercator pixels at a zoom — the
// EPSG:3857 pyramid every raster tile server is cut on, 256 px at zoom 0.
//
// This is deliberately a second implementation of what
// [github.com/stergiotis/boxer/public/thestack/imzero2/egui2/widgets/portolan]
// EPSG3857 does, not a call into it: the scene asserts the widget's camera,
// and an oracle sharing the widget's projection would cancel a fault in it.
// A test cross-checks the two so the copy cannot drift silently.
func pixel(lat, lon, zoom float64) (x, y float64) {
	s := 256 * math.Pow(2, zoom)
	rad := lat * math.Pi / 180
	x = (lon + 180) / 360 * s
	y = (1 - math.Log(math.Tan(rad)+1/math.Cos(rad))/math.Pi) / 2 * s
	return
}

// centreShift is how far the camera centre travelled between two readings, in
// pixels at the zoom of the first — the unit every gesture is specified in.
func centreShift(a, b Readout) (dx, dy float64) {
	ax, ay := pixel(a.Lat, a.Lon, a.Zoom)
	bx, by := pixel(b.Lat, b.Lon, a.Zoom)
	return bx - ax, by - ay
}

func atof(s string) float64 { v, _ := strconv.ParseFloat(s, 64); return v }
func atoi(s string) int     { v, _ := strconv.Atoi(s); return v }
