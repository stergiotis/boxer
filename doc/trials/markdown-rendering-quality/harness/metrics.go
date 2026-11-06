//go:build integration

package mdquality

import (
	"bufio"
	"encoding/json"
	"encoding/xml"
	"fmt"
	"image"
	"image/png"
	"io"
	"math"
	"os"
	"path/filepath"
	"regexp"
	"sort"
	"strconv"
	"strings"
	"unicode"

	"github.com/stergiotis/boxer/public/observability/eh"
	"github.com/stergiotis/boxer/public/observability/eh/eb"
)

// glyph is one drawn character of a capture, in the capture's own
// coordinates: logical points for the native SVG, CSS pixels for a browser.
// Both are one device pixel at the scale the arms run at.
type glyph struct {
	X, W     float64
	Baseline float64
	Size     float64
	Family   string
	Color    string
	Space    bool
	// List marks a glyph that opens a list row: the native arm's marker
	// run (a bullet, or digits and a period, drawn as a run of its own)
	// or any glyph inside an HTML list item.
	List bool
	// Cell marks a glyph inside an HTML table cell. A native table cell
	// shares its baseline with its neighbours, which splits the line.
	Cell bool
	// Block is the text block the glyph belongs to: the native arm's text
	// shape (one label — a paragraph, a cell, an item), a browser's
	// nearest block element. A paragraph never spans two blocks.
	Block int
}

// capture is one arm's rendering of one document, read back as glyphs.
type capture struct {
	Glyphs     []glyph
	Background string
	// ContentTop is where the document starts: below the toolbar in the
	// native arm, zero in a browser.
	ContentTop float64
}

// line is a run of glyphs sharing a baseline, after splitting at column
// gaps (a table row is several lines on one baseline, each Split).
type line struct {
	Baseline float64
	Size     float64
	Left     float64
	Right    float64
	Chars    int
	Words    int
	Mono     bool
	Split    bool
	Color    string
	// List says the line opens with a list glyph: a list item's first row
	// in the native arm, any row of one in a browser.
	List bool
	// Cell says the line opens inside a table cell: a browser's, or a
	// block of the native arm one of whose lines is split by a column
	// gap, which is a cell's row.
	Cell  bool
	Block int
}

// armMetrics is README §4's line-level record for one (document, arm).
type armMetrics struct {
	Doc string `json:"doc"`
	Arm string `json:"arm"`
	// Pixels says the arm was captured as a picture only — no geometry, so
	// every other field is zero.
	PixelsOnly bool `json:"pixelsOnly,omitempty"`

	BodySize   float64 `json:"bodySize"`
	LineHeight float64 `json:"lineHeight"`
	Leading    float64 `json:"leading"`

	RowsInView  int `json:"rowsInView"`
	WordsInView int `json:"wordsInView"`
	ProseLines  int `json:"proseLines"`
	// ListRows are prose rows that belong to a list item, kept out of the
	// paragraph metrics and the line-height estimate.
	ListRows int `json:"listRows"`
	// TableRows are prose rows inside a table cell that did not split
	// (a browser's vertically centred cells put neighbours on different
	// baselines), kept out of the same metrics.
	TableRows  int `json:"tableRows"`
	FullLines  int `json:"fullLines"`
	Paragraphs int `json:"paragraphs"`
	Widows     int `json:"widows"`

	CPLMedian    float64 `json:"cplMedian"`
	CPLP90       float64 `json:"cplP90"`
	CPLMax       float64 `json:"cplMax"`
	InBand45to75 float64 `json:"inBand45to75"`
	MeasurePx    float64 `json:"measurePx"`
	MeasureEm    float64 `json:"measureEm"`
	Raggedness   float64 `json:"raggedness"`

	HeadingRatios    []float64 `json:"headingRatios"`
	GapBeforeHeading float64   `json:"gapBeforeHeading"`
	GapAfterHeading  float64   `json:"gapAfterHeading"`
	GapParagraph     float64   `json:"gapParagraph"`

	TextColor  string  `json:"textColor"`
	Background string  `json:"background"`
	Contrast   float64 `json:"contrast"`
}

// browserArms are the HTML arms in capture-file order: <slug>-<style>-<engine>.
var browserEngines = []string{"chromium", "firefox"}

// typstArms are the typst renderings (README §3), pictures only: a capture
// a run did not make (the tools absent) leaves the arm out of that run.
var typstArms = []string{"typst-ids", "typst-book"}

// metrics reads every capture under work/captures, writes the cropped
// comparison pictures beside them, and writes metrics.json and metrics.md
// to runDir.
func metrics(work, runDir string) (err error) {
	caps := filepath.Join(work, "captures")
	var all []armMetrics
	for _, e := range corpus {
		// native
		m := armMetrics{Doc: e.Slug, Arm: "native"}
		var cp capture
		svgPath := filepath.Join(caps, e.Slug+"-native.svg")
		if cp, err = parseSVG(svgPath); err != nil {
			return err
		}
		if cp.ContentTop, err = nativeContentTop(filepath.Join(caps, e.Slug+"-native.tree.jsonl")); err != nil {
			return err
		}
		m = compute(m, cp)
		if err = cropPNG(filepath.Join(caps, e.Slug+"-native.png"), cp.ContentTop); err != nil {
			return err
		}
		all = append(all, m)
		for _, s := range styles {
			for _, eng := range browserEngines {
				base := e.Slug + "-" + string(s) + "-" + eng
				m = armMetrics{Doc: e.Slug, Arm: string(s) + "-" + eng}
				dom := filepath.Join(caps, base+".dom.html")
				if _, statErr := os.Stat(dom); statErr == nil {
					if cp, err = parseDOM(dom); err != nil {
						return err
					}
					m = compute(m, cp)
				} else {
					m.PixelsOnly = true
				}
				if err = cropPNG(filepath.Join(caps, base+".png"), 0); err != nil {
					return err
				}
				all = append(all, m)
			}
		}
		for _, arm := range typstArms {
			p := filepath.Join(caps, e.Slug+"-"+arm+".png")
			if _, statErr := os.Stat(p); statErr != nil {
				continue
			}
			if err = cropPNG(p, 0); err != nil {
				return err
			}
			all = append(all, armMetrics{Doc: e.Slug, Arm: arm, PixelsOnly: true})
		}
	}
	if err = os.MkdirAll(runDir, 0o755); err != nil {
		return eb.Build().Str("dir", runDir).Errorf("unable to create the run directory: %w", err)
	}
	var js []byte
	if js, err = json.MarshalIndent(all, "", "  "); err != nil {
		return eh.Errorf("unable to encode the metrics: %w", err)
	}
	if err = os.WriteFile(filepath.Join(runDir, "metrics.json"), js, 0o644); err != nil {
		return eh.Errorf("unable to write metrics.json: %w", err)
	}
	if err = os.WriteFile(filepath.Join(runDir, "metrics.md"), []byte(metricsTable(all)), 0o644); err != nil {
		return eh.Errorf("unable to write metrics.md: %w", err)
	}
	return
}

// parseSVG reads the native capture's SVG sidecar: one <text> per run of
// glyphs sharing a baseline and a style, with an x per character.
func parseSVG(path string) (cp capture, err error) {
	var f *os.File
	if f, err = os.Open(path); err != nil {
		return cp, eb.Build().Str("path", path).Errorf("unable to open the SVG sidecar: %w", err)
	}
	defer func() { _ = f.Close() }()
	dec := xml.NewDecoder(f)
	dec.Strict = false
	var inText bool
	block := -1
	var cur struct {
		y, size      float64
		xs           []float64
		family, fill string
		text         strings.Builder
	}
	for {
		var tok xml.Token
		if tok, err = dec.Token(); err != nil {
			if err == io.EOF {
				err = nil
				break
			}
			return cp, eb.Build().Str("path", path).Errorf("unable to read the SVG sidecar: %w", err)
		}
		switch t := tok.(type) {
		case xml.StartElement:
			attrs := map[string]string{}
			for _, a := range t.Attr {
				attrs[a.Name.Local] = a.Value
			}
			switch t.Name.Local {
			case "g":
				if attrs["class"] == "imz-text" {
					block++
				}
			case "rect":
				x, _ := strconv.ParseFloat(attrs["x"], 64)
				y, _ := strconv.ParseFloat(attrs["y"], 64)
				w, _ := strconv.ParseFloat(attrs["width"], 64)
				if cp.Background == "" && x == 0 && y == 0 && w >= viewportW-1 {
					cp.Background = attrs["fill"]
				}
			case "text":
				inText = true
				cur.text.Reset()
				cur.y, _ = strconv.ParseFloat(attrs["y"], 64)
				cur.size, _ = strconv.ParseFloat(attrs["font-size"], 64)
				cur.family = attrs["font-family"]
				cur.fill = attrs["fill"]
				cur.xs = cur.xs[:0]
				for _, s := range strings.Fields(attrs["x"]) {
					v, _ := strconv.ParseFloat(s, 64)
					cur.xs = append(cur.xs, v)
				}
			}
		case xml.CharData:
			if inText {
				cur.text.Write(t)
			}
		case xml.EndElement:
			if t.Name.Local == "text" && inText {
				inText = false
				runes := []rune(cur.text.String())
				n := min(len(runes), len(cur.xs))
				marker := listMarkerRe.MatchString(cur.text.String())
				for i := 0; i < n; i++ {
					w := cur.size * 0.55
					if i+1 < n {
						w = cur.xs[i+1] - cur.xs[i]
					}
					cp.Glyphs = append(cp.Glyphs, glyph{
						X: cur.xs[i], W: w, Baseline: cur.y, Size: cur.size,
						Family: cur.family, Color: cur.fill, Space: unicode.IsSpace(runes[i]),
						List: marker, Block: block,
					})
				}
			}
		}
	}
	return
}

// nativeContentTop reads the tree sidecar and returns the bottom edge of
// the mode bar — the "Read" button — so the toolbar is not measured as
// document. Zero when the button is not found.
func nativeContentTop(path string) (top float64, err error) {
	var f *os.File
	if f, err = os.Open(path); err != nil {
		return 0, eb.Build().Str("path", path).Errorf("unable to open the tree sidecar: %w", err)
	}
	defer func() { _ = f.Close() }()
	sc := bufio.NewScanner(f)
	sc.Buffer(make([]byte, 1<<20), 1<<24)
	for sc.Scan() {
		var n struct {
			Role string  `json:"role"`
			Name string  `json:"name"`
			Y    float64 `json:"y"`
			H    float64 `json:"h"`
		}
		if json.Unmarshal(sc.Bytes(), &n) != nil {
			continue
		}
		if n.Role == "button" && strings.Contains(n.Name, "Read") {
			if b := n.Y + n.H + 2; b > top {
				top = b
			}
		}
	}
	err = sc.Err()
	return
}

var geomRe = regexp.MustCompile(`(?s)<script type="application/json" id="mdq-geom">(.*?)</script>`)

// parseDOM reads the geometry block geometry.js left in a dumped DOM.
func parseDOM(path string) (cp capture, err error) {
	var b []byte
	if b, err = os.ReadFile(path); err != nil {
		return cp, eb.Build().Str("path", path).Errorf("unable to read the DOM dump: %w", err)
	}
	m := geomRe.FindSubmatch(b)
	if m == nil {
		return cp, eb.Build().Str("path", path).Errorf("the DOM dump carries no geometry block")
	}
	var g struct {
		BG    string `json:"bg"`
		Nodes []struct {
			Text   string      `json:"text"`
			Size   float64     `json:"size"`
			Family string      `json:"family"`
			Color  string      `json:"color"`
			List   bool        `json:"list"`
			Cell   bool        `json:"cell"`
			Block  int         `json:"block"`
			Chars  [][]float64 `json:"chars"`
		} `json:"nodes"`
	}
	if err = json.Unmarshal(m[1], &g); err != nil {
		return cp, eb.Build().Str("path", path).Errorf("unable to decode the geometry block: %w", err)
	}
	cp.Background = g.BG
	for _, n := range g.Nodes {
		var prev *glyph
		pending := false // a whitespace seen since prev
		for _, c := range n.Chars {
			if c == nil {
				pending = prev != nil
				continue
			}
			gl := glyph{X: c[0], W: c[2], Baseline: c[1] + 0.8*c[3], Size: n.Size, Family: n.Family, Color: n.Color, List: n.List, Cell: n.Cell, Block: n.Block}
			if pending && math.Abs(prev.Baseline-gl.Baseline) < 1 {
				cp.Glyphs = append(cp.Glyphs, glyph{
					X: prev.X + prev.W, W: math.Max(gl.X-(prev.X+prev.W), 0), Baseline: prev.Baseline,
					Size: n.Size, Family: n.Family, Color: n.Color, Space: true, Block: n.Block,
				})
			}
			pending = false
			cp.Glyphs = append(cp.Glyphs, gl)
			prev = &cp.Glyphs[len(cp.Glyphs)-1]
		}
	}
	return
}

// listMarkerRe matches a native list marker run: the widget draws the
// bullet or the number-and-period as a run of its own (renderItemMarker in
// the markdown widget), so a run that is nothing else is a marker.
var listMarkerRe = regexp.MustCompile(`^\s*(•|[0-9]+\.)\s*$`)

func isMono(family string) bool {
	f := strings.ToLower(family)
	for _, k := range []string{"mono", "courier", "consolas", "menlo", "code"} {
		if strings.Contains(f, k) {
			return true
		}
	}
	return false
}

// buildLines clusters the glyphs inside the viewport by baseline, then
// splits each baseline at column gaps wider than 2.5 em.
func buildLines(cp capture) (lines []line) {
	gs := make([]glyph, 0, len(cp.Glyphs))
	for _, g := range cp.Glyphs {
		if g.Baseline >= cp.ContentTop && g.Baseline <= viewportH && g.Size > 0 {
			gs = append(gs, g)
		}
	}
	sort.SliceStable(gs, func(i, j int) bool { return gs[i].Baseline < gs[j].Baseline })
	var clusters [][]glyph
	for _, g := range gs {
		if n := len(clusters); n > 0 {
			last := clusters[n-1][len(clusters[n-1])-1]
			if g.Baseline-last.Baseline <= 0.3*math.Min(g.Size, last.Size) {
				clusters[n-1] = append(clusters[n-1], g)
				continue
			}
		}
		clusters = append(clusters, []glyph{g})
	}
	for _, c := range clusters {
		sort.SliceStable(c, func(i, j int) bool { return c[i].X < c[j].X })
		var segs [][]glyph
		for i, g := range c {
			if i > 0 {
				p := c[i-1]
				if g.X-(p.X+p.W) > 2.5*p.Size {
					segs = append(segs, nil)
				}
			}
			if len(segs) == 0 {
				segs = append(segs, nil)
			}
			segs[len(segs)-1] = append(segs[len(segs)-1], g)
		}
		for _, s := range segs {
			if l, ok := segmentLine(s); ok {
				l.Split = len(segs) > 1
				lines = append(lines, l)
			}
		}
	}
	// A block one of whose lines shares a baseline with another block's
	// is a table cell: the native arm has no cell tag, and a wrapped
	// cell's second line sits alone on its baseline.
	cells := map[int]bool{}
	for _, l := range lines {
		if l.Split {
			cells[l.Block] = true
		}
	}
	for i := range lines {
		if cells[lines[i].Block] {
			lines[i].Cell = true
		}
	}
	return
}

func segmentLine(s []glyph) (l line, ok bool) {
	// trim spaces at both ends
	for len(s) > 0 && s[0].Space {
		s = s[1:]
	}
	for len(s) > 0 && s[len(s)-1].Space {
		s = s[:len(s)-1]
	}
	if len(s) == 0 {
		return l, false
	}
	sizes := map[float64]int{}
	colors := map[string]int{}
	mono := 0
	var sum float64
	inWord := false
	for _, g := range s {
		sum += g.Baseline
		if g.Space {
			inWord = false
			continue
		}
		if !inWord {
			l.Words++
			inWord = true
		}
		sizes[math.Round(g.Size*2)/2]++
		colors[g.Color]++
		if isMono(g.Family) {
			mono++
		}
	}
	l.Baseline = sum / float64(len(s))
	l.List = s[0].List
	l.Cell = s[0].Cell
	l.Block = s[0].Block
	l.Size = modeKey(sizes)
	l.Color = modeStr(colors)
	l.Mono = mono*2 > len(s)
	l.Left = s[0].X
	l.Right = s[len(s)-1].X + s[len(s)-1].W
	l.Chars = len(s)
	ok = true
	return
}

func modeKey(m map[float64]int) (k float64) {
	best := -1
	for key, n := range m {
		if n > best || (n == best && key > k) {
			best, k = n, key
		}
	}
	return
}

func modeStr(m map[string]int) (k string) {
	best := -1
	for key, n := range m {
		if n > best || (n == best && key < k) {
			best, k = n, key
		}
	}
	return
}

// row is one baseline of the capture: every segment on it, folded.
type row struct {
	Baseline float64
	Size     float64
	Heading  bool
	Prose    bool
	List     bool  // the line opens with a list glyph
	Cell     bool  // the line opens inside a table cell
	Line     *line // the one unsplit line on this baseline, if any
}

// compute derives the §4 record from a capture.
func compute(m armMetrics, cp capture) armMetrics {
	lines := buildLines(cp)
	if len(lines) == 0 {
		return m
	}
	// body size: the mode over non-mono glyph counts
	sizes := map[float64]int{}
	for _, l := range lines {
		if !l.Mono {
			sizes[l.Size] += l.Chars
		}
	}
	body := modeKey(sizes)
	m.BodySize = body
	isProse := func(l line) bool { return !l.Mono && !l.Split && math.Abs(l.Size-body) <= 0.75 }

	// rows
	var rows []row
	for i := range lines {
		l := &lines[i]
		if n := len(rows); n > 0 && math.Abs(rows[n-1].Baseline-l.Baseline) <= 0.3*math.Min(l.Size, rows[n-1].Size) {
			r := &rows[n-1]
			r.Size = math.Max(r.Size, l.Size)
			if !l.Split {
				r.Line = l
			}
			continue
		}
		r := row{Baseline: l.Baseline, Size: l.Size}
		if !l.Split {
			r.Line = l
		}
		rows = append(rows, r)
	}
	for i := range rows {
		r := &rows[i]
		r.Prose = r.Line != nil && isProse(*r.Line)
		r.Heading = r.Line != nil && !r.Line.Mono && r.Size > body*1.15
		r.List = r.Line != nil && r.Line.List
		r.Cell = r.Line != nil && r.Line.Cell
	}
	m.RowsInView = len(rows)
	for _, l := range lines {
		m.WordsInView += l.Words
		if isProse(l) {
			m.ProseLines++
		}
	}

	// line height: median delta between consecutive prose rows, bounded
	var deltas []float64
	for i := 1; i < len(rows); i++ {
		if rows[i].Prose && rows[i-1].Prose && !rows[i].List && !rows[i-1].List && !rows[i].Cell && !rows[i-1].Cell {
			if d := rows[i].Baseline - rows[i-1].Baseline; d <= 1.9*body {
				deltas = append(deltas, d)
			}
		}
	}
	lh := median(deltas)
	if lh == 0 {
		lh = body * 1.4
	}
	m.LineHeight = lh
	m.Leading = lh / body

	// paragraphs: runs of prose rows at line-height spacing. A list row
	// opens a list group instead; a prose row at the line step after a
	// list row is the item's wrapped continuation (the native marker sits
	// on the first row only) and stays in the group. Neither is a
	// paragraph line.
	var paras [][]*line
	inList := false
	for i := range rows {
		r := rows[i]
		if !r.Prose {
			inList = false
			continue
		}
		if r.Cell {
			inList = false
			m.TableRows++
			continue
		}
		atStep := i > 0 && rows[i-1].Prose && r.Baseline-rows[i-1].Baseline <= 1.3*lh
		if r.List || (inList && atStep) {
			inList = true
			m.ListRows++
			continue
		}
		inList = false
		if n := len(paras); n > 0 && atStep && paras[n-1][len(paras[n-1])-1].Block == r.Line.Block {
			paras[n-1] = append(paras[n-1], r.Line)
			continue
		}
		paras = append(paras, []*line{r.Line})
	}
	m.Paragraphs = len(paras)
	var cpls, widths, rags []float64
	for _, p := range paras {
		if len(p) < 2 {
			continue
		}
		if p[len(p)-1].Words <= 1 {
			m.Widows++
		}
		measure := 0.0
		for _, l := range p {
			measure = math.Max(measure, l.Right-l.Left)
		}
		for _, l := range p[:len(p)-1] {
			w := l.Right - l.Left
			cpls = append(cpls, float64(l.Chars))
			widths = append(widths, w)
			if len(p) >= 3 && measure > 0 {
				rags = append(rags, (measure-w)/measure)
			}
		}
	}
	m.FullLines = len(cpls)
	if len(cpls) > 0 {
		m.CPLMedian = median(cpls)
		m.CPLP90 = percentile(cpls, 0.9)
		m.CPLMax = percentile(cpls, 1)
		in := 0
		for _, c := range cpls {
			if c >= 45 && c <= 75 {
				in++
			}
		}
		m.InBand45to75 = float64(in) / float64(len(cpls))
		m.MeasurePx = percentile(widths, 0.95)
		m.MeasureEm = m.MeasurePx / body
	}
	if len(rags) > 0 {
		m.Raggedness = mean(rags)
	}

	// headings and gaps
	hs := map[float64]bool{}
	var before, after, para []float64
	for i := 1; i < len(rows); i++ {
		a, b := rows[i-1], rows[i]
		d := (b.Baseline - a.Baseline) / lh
		if b.Heading {
			before = append(before, d)
			hs[math.Round(b.Size/body*100)/100] = true
		}
		if a.Heading {
			after = append(after, d)
		}
		if a.Prose && b.Prose && b.Baseline-a.Baseline > 1.3*lh {
			para = append(para, d)
		}
	}
	for r := range hs {
		m.HeadingRatios = append(m.HeadingRatios, r)
	}
	sort.Sort(sort.Reverse(sort.Float64Slice(m.HeadingRatios)))
	m.GapBeforeHeading = median(before)
	m.GapAfterHeading = median(after)
	m.GapParagraph = median(para)

	// contrast
	colors := map[string]int{}
	for _, l := range lines {
		if isProse(l) {
			colors[l.Color] += l.Chars
		}
	}
	m.TextColor = modeStr(colors)
	m.Background = cp.Background
	if fg, ok := parseColor(m.TextColor); ok {
		if bg, ok := parseColor(cp.Background); ok {
			m.Contrast = contrast(fg, bg)
		}
	}
	return m
}

func median(v []float64) float64 { return percentile(v, 0.5) }

func percentile(v []float64, p float64) float64 {
	if len(v) == 0 {
		return 0
	}
	s := append([]float64(nil), v...)
	sort.Float64s(s)
	i := int(math.Round(p * float64(len(s)-1)))
	return s[i]
}

func mean(v []float64) float64 {
	if len(v) == 0 {
		return 0
	}
	var s float64
	for _, x := range v {
		s += x
	}
	return s / float64(len(v))
}

var rgbRe = regexp.MustCompile(`rgba?\(\s*(\d+)\s*,\s*(\d+)\s*,\s*(\d+)`)

func parseColor(s string) (c [3]float64, ok bool) {
	s = strings.TrimSpace(s)
	if strings.HasPrefix(s, "#") && (len(s) == 7 || len(s) == 9) {
		for i := 0; i < 3; i++ {
			v, err := strconv.ParseUint(s[1+2*i:3+2*i], 16, 8)
			if err != nil {
				return c, false
			}
			c[i] = float64(v) / 255
		}
		return c, true
	}
	if m := rgbRe.FindStringSubmatch(s); m != nil {
		for i := 0; i < 3; i++ {
			v, _ := strconv.Atoi(m[i+1])
			c[i] = float64(v) / 255
		}
		return c, true
	}
	return c, false
}

// contrast is the WCAG 2 contrast ratio.
func contrast(a, b [3]float64) float64 {
	lum := func(c [3]float64) float64 {
		ch := func(v float64) float64 {
			if v <= 0.03928 {
				return v / 12.92
			}
			return math.Pow((v+0.055)/1.055, 2.4)
		}
		return 0.2126*ch(c[0]) + 0.7152*ch(c[1]) + 0.0722*ch(c[2])
	}
	la, lb := lum(a), lum(b)
	if la < lb {
		la, lb = lb, la
	}
	return (la + 0.05) / (lb + 0.05)
}

// cropPNG writes <name>.crop.png beside path: the picture from top down,
// bounded to the common viewport, so every arm's comparison picture shows
// the same region of the same document.
func cropPNG(path string, top float64) (err error) {
	var f *os.File
	if f, err = os.Open(path); err != nil {
		return eb.Build().Str("path", path).Errorf("unable to open a capture: %w", err)
	}
	img, derr := png.Decode(f)
	_ = f.Close()
	if derr != nil {
		return eb.Build().Str("path", path).Errorf("unable to decode a capture: %w", derr)
	}
	b := img.Bounds()
	r := image.Rect(b.Min.X, b.Min.Y+int(top), min(b.Max.X, b.Min.X+viewportW), min(b.Max.Y, b.Min.Y+viewportH))
	type subImager interface {
		SubImage(image.Rectangle) image.Image
	}
	si, ok := img.(subImager)
	if !ok {
		return eb.Build().Str("path", path).Errorf("the capture's image type cannot be cropped")
	}
	out := strings.TrimSuffix(path, ".png") + ".crop.png"
	var of *os.File
	if of, err = os.Create(out); err != nil {
		return eb.Build().Str("path", out).Errorf("unable to create a cropped capture: %w", err)
	}
	defer func() { _ = of.Close() }()
	if err = png.Encode(of, si.SubImage(r)); err != nil {
		return eb.Build().Str("path", out).Errorf("unable to encode a cropped capture: %w", err)
	}
	return
}

// metricsTable renders one table per document, arms as rows.
func metricsTable(all []armMetrics) string {
	var b strings.Builder
	b.WriteString("---\ntype: reference\naudience: package maintainer\nstatus: draft\n---\n\n")
	b.WriteString("> **Status: draft — pre-human-review.** Per-arm evidence of one run; the citable claim is the trial README's §0. Do not cite these cells on their own.\n\n")
	b.WriteString("# Line-level metrics, per document and arm\n\n")
	b.WriteString("Columns: body font size in px; leading as line height over body size; rows and words inside the common viewport; prose lines; list rows (prose rows inside a list item) and table rows (unsplit prose rows inside a table cell), both kept out of the paragraph metrics; median and p90 characters per full line (a full line is every line of a paragraph but its last); the share of full lines carrying 45–75 characters; measure as the p95 full-line width in em; raggedness as the mean slack of full lines against their paragraph's widest line, in paragraphs of three lines or more; widows as paragraphs of two lines or more whose last line is one word, over paragraphs; heading sizes as ratios to body; the median gap before and after a heading and between paragraphs, in line heights (a value of 1 is the ordinary line step); the WCAG contrast of body text against the page. A row marked pixels only was captured as a picture and carries no geometry.\n\n")
	for _, e := range corpus {
		b.WriteString("## " + e.Slug + " — `" + e.Path + "`\n\n")
		b.WriteString("| arm | body px | leading | rows | words | prose | list | table | cpl med | cpl p90 | 45–75 | measure em | rag | widows | headings | gap ↑H | gap H↓ | gap ¶ | contrast |\n")
		b.WriteString("| --- | ---: | ---: | ---: | ---: | ---: | ---: | ---: | ---: | ---: | ---: | ---: | ---: | ---: | --- | ---: | ---: | ---: | ---: |\n")
		for _, m := range all {
			if m.Doc != e.Slug {
				continue
			}
			if m.PixelsOnly {
				fmt.Fprintf(&b, "| %s | pixels only | | | | | | | | | | | | | | | | | |\n", m.Arm)
				continue
			}
			hs := make([]string, 0, len(m.HeadingRatios))
			for _, r := range m.HeadingRatios {
				hs = append(hs, strconv.FormatFloat(r, 'f', 2, 64))
			}
			fmt.Fprintf(&b, "| %s | %.1f | %.2f | %d | %d | %d | %d | %d | %.0f | %.0f | %.0f%% | %.0f | %.2f | %d/%d | %s | %.1f | %.1f | %.1f | %.1f |\n",
				m.Arm, m.BodySize, m.Leading, m.RowsInView, m.WordsInView, m.ProseLines, m.ListRows, m.TableRows,
				m.CPLMedian, m.CPLP90, m.InBand45to75*100, m.MeasureEm, m.Raggedness,
				m.Widows, m.Paragraphs, strings.Join(hs, " "), m.GapBeforeHeading, m.GapAfterHeading, m.GapParagraph, m.Contrast)
		}
		b.WriteString("\n")
	}
	return b.String()
}
