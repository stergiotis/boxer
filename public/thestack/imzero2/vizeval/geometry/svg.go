// Package geometry measures a rendering from the SVG the imzero2 host writes
// beside a capture (ADR-0257, proposed, §SD6): where text sits, what it
// overlaps, what clips it, how it contrasts with what is painted under it.
//
// It reads the exporter's output, not SVG in general. Text is one
// `<g class="imz-text">` per egui text shape, carrying the string and the ink
// bounds of the glyphs drawn; every other element is a mark, in paint order.
// Paths are read for their straight segments only, which is what egui's
// shapes emit for fills; a curve's control points widen its bounds.
package geometry

import (
	"encoding/xml"
	"io"
	"math"
	"strconv"
	"strings"

	"github.com/stergiotis/boxer/public/observability/eh"
	"github.com/stergiotis/boxer/public/observability/eh/eb"
)

// Rect is an axis-aligned box in SVG user units (egui points).
type Rect struct {
	X0, Y0, X1, Y1 float64
}

func (inst Rect) Empty() bool { return !(inst.X1 > inst.X0 && inst.Y1 > inst.Y0) }
func (inst Rect) W() float64  { return inst.X1 - inst.X0 }
func (inst Rect) H() float64  { return inst.Y1 - inst.Y0 }
func (inst Rect) Area() float64 {
	if inst.Empty() {
		return 0
	}
	return inst.W() * inst.H()
}
func (inst Rect) Intersect(o Rect) Rect {
	return Rect{max(inst.X0, o.X0), max(inst.Y0, o.Y0), min(inst.X1, o.X1), min(inst.Y1, o.Y1)}
}
func (inst Rect) Contains(x, y float64) bool {
	return x >= inst.X0 && x <= inst.X1 && y >= inst.Y0 && y <= inst.Y1
}
func (inst Rect) Center() (x, y float64) { return (inst.X0 + inst.X1) / 2, (inst.Y0 + inst.Y1) / 2 }

// unbounded is the clip of content no clip path applies to.
var unbounded = Rect{-1e9, -1e9, 1e9, 1e9}

// TextRun is one text shape: a label, a cell, a line of a paragraph.
type TextRun struct {
	Text   string
	Box    Rect // ink bounds of the glyphs drawn
	Size   float64
	Fill   RGBA
	Elided bool
	Clip   Rect
	Order  int
}

// MarkKindE is the element a mark came from.
type MarkKindE uint8

const (
	MarkKindRect MarkKindE = iota
	MarkKindPolygon
	MarkKindPath
	MarkKindLine
	MarkKindCircle
	MarkKindImage
)

// Mark is one painted non-text element.
type Mark struct {
	Kind        MarkKindE
	Box         Rect
	Fill        RGBA
	Stroke      RGBA
	StrokeWidth float64
	// Poly is the filled outline of a polygon or path, for hit tests, and a
	// line's two ends; nil for the kinds whose box is their shape.
	Poly []Point
	// Cubic marks a path drawn as one cubic Bézier: Poly holds its start,
	// two control points and end.
	Cubic bool
	Clip  Rect
	Order int
}

type Point struct{ X, Y float64 }

// Drawing is an SVG export read back: the viewport, the text runs and the
// marks, both in paint order (Order is shared, so they interleave).
type Drawing struct {
	Viewport Rect
	Runs     []TextRun
	Marks    []Mark
}

// ReadSVG parses an imzero2 SVG export.
func ReadSVG(r io.Reader) (d Drawing, err error) {
	dec := xml.NewDecoder(r)
	clips := make(map[string]Rect, 64)
	// Each open element pushes the clip in force inside it and whether it is
	// a text group, so an end tag restores both.
	type frame struct {
		clip   Rect
		isText bool
	}
	stack := make([]frame, 0, 16)
	clip := unbounded
	var curClipID string
	inDefs := 0
	order := 0
	textRun := -1
	for {
		var tok xml.Token
		tok, err = dec.Token()
		if err == io.EOF {
			d.Runs = collapseHalos(d.Runs)
			return d, nil
		}
		if err != nil {
			return Drawing{}, eh.Errorf("unable to read svg: %w", err)
		}
		switch t := tok.(type) {
		case xml.StartElement:
			a := attrs(t.Attr)
			stack = append(stack, frame{clip: clip, isText: false})
			name := t.Name.Local
			switch {
			case name == "svg":
				vb := strings.Fields(a["viewBox"])
				if len(vb) == 4 {
					x, _ := strconv.ParseFloat(vb[0], 64)
					y, _ := strconv.ParseFloat(vb[1], 64)
					w, _ := strconv.ParseFloat(vb[2], 64)
					h, _ := strconv.ParseFloat(vb[3], 64)
					d.Viewport = Rect{x, y, x + w, y + h}
				}
			case name == "defs":
				inDefs++
			case name == "clipPath":
				curClipID = a["id"]
			case inDefs > 0 || curClipID != "":
				if name == "rect" && curClipID != "" {
					clips[curClipID] = rectOf(a)
				}
			case name == "g":
				if ref, ok := strings.CutPrefix(a["clip-path"], "url(#"); ok {
					if c, known := clips[strings.TrimSuffix(ref, ")")]; known {
						clip = clip.Intersect(c)
					}
				}
				if a["class"] == "imz-text" {
					stack[len(stack)-1].isText = true
					run, e := textRunOf(a)
					if e != nil {
						return Drawing{}, e
					}
					run.Clip, run.Order = clip, order
					order++
					d.Runs = append(d.Runs, run)
					textRun = len(d.Runs) - 1
				}
			case name == "text":
				if textRun >= 0 && d.Runs[textRun].Fill.A == 0 {
					d.Runs[textRun].Fill = paint(a["fill"], a["fill-opacity"])
				}
			default:
				if m, ok := markOf(name, a); ok {
					m.Clip, m.Order = clip, order
					order++
					d.Marks = append(d.Marks, m)
				}
			}
		case xml.EndElement:
			switch t.Name.Local {
			case "defs":
				inDefs--
			case "clipPath":
				curClipID = ""
			}
			if n := len(stack); n > 0 {
				top := stack[n-1]
				if top.isText {
					textRun = -1
				}
				clip = top.clip
				stack = stack[:n-1]
			}
		}
	}
}

// haloOffset is how far a halo copy of a label sits from the label.
const haloOffset = 2.5

// collapseHalos keeps one run per haloed label. A label drawn with a halo —
// graphview's labels are — is the same text painted several times in a row,
// each copy a point or two off the last, the final one on top in the text
// colour. Measured as they are, the copies overlap each other and a single
// label counts five times; the reader sees one label, the topmost.
func collapseHalos(runs []TextRun) (out []TextRun) {
	out = runs[:0:0]
	for i := 0; i < len(runs); {
		j := i + 1
		for j < len(runs) && runs[j].Text == runs[i].Text &&
			math.Abs(runs[j].Box.X0-runs[i].Box.X0) <= haloOffset &&
			math.Abs(runs[j].Box.Y0-runs[i].Box.Y0) <= haloOffset {
			j++
		}
		out = append(out, runs[j-1])
		i = j
	}
	return out
}

func attrs(as []xml.Attr) map[string]string {
	m := make(map[string]string, len(as))
	for _, a := range as {
		m[a.Name.Local] = a.Value
	}
	return m
}

func num(s string) float64 {
	f, _ := strconv.ParseFloat(strings.TrimSpace(s), 64)
	return f
}

func rectOf(a map[string]string) Rect {
	x, y := num(a["x"]), num(a["y"])
	return Rect{x, y, x + num(a["width"]), y + num(a["height"])}
}

func textRunOf(a map[string]string) (run TextRun, err error) {
	bb := strings.Fields(a["data-bbox"])
	if len(bb) != 4 {
		return TextRun{}, eb.Build().Str("bbox", a["data-bbox"]).Errorf("text group without a 4-number data-bbox")
	}
	x, y := num(bb[0]), num(bb[1])
	run = TextRun{
		Text:   a["data-text"],
		Box:    Rect{x, y, x + num(bb[2]), y + num(bb[3])},
		Size:   num(a["data-size"]),
		Elided: a["data-elided"] == "1",
	}
	return run, nil
}

func markOf(name string, a map[string]string) (m Mark, ok bool) {
	m.Fill = paint(a["fill"], a["fill-opacity"])
	m.Stroke = paint(a["stroke"], a["stroke-opacity"])
	m.StrokeWidth = num(a["stroke-width"])
	switch name {
	case "rect":
		m.Kind, m.Box = MarkKindRect, rectOf(a)
		if a["fill"] == "" {
			// SVG's default fill is black; the exporter always writes one,
			// except on the root backdrop, which is black anyway.
			m.Fill = RGBA{0, 0, 0, 1}
		}
	case "polygon":
		m.Kind, m.Poly = MarkKindPolygon, pointsOf(a["points"])
		m.Box = boundsOf(m.Poly)
	case "path":
		m.Kind, m.Poly = MarkKindPath, pathPoints(a["d"])
		m.Box = boundsOf(m.Poly)
		m.Cubic = len(m.Poly) == 4 && strings.Contains(a["d"], "C")
	case "line", "polyline":
		pts := []Point{{num(a["x1"]), num(a["y1"])}, {num(a["x2"]), num(a["y2"])}}
		if name == "polyline" {
			pts = pointsOf(a["points"])
		}
		m.Kind, m.Box, m.Poly = MarkKindLine, boundsOf(pts), pts
		m.Fill = RGBA{}
	case "circle", "ellipse":
		cx, cy := num(a["cx"]), num(a["cy"])
		rx, ry := num(a["r"]), num(a["r"])
		if name == "ellipse" {
			rx, ry = num(a["rx"]), num(a["ry"])
		}
		m.Kind, m.Box = MarkKindCircle, Rect{cx - rx, cy - ry, cx + rx, cy + ry}
	case "image":
		m.Kind, m.Box = MarkKindImage, rectOf(a)
		m.Fill = RGBA{}
	default:
		return Mark{}, false
	}
	// A stroke widens the box by half its width on every side.
	if m.Stroke.A > 0 && m.StrokeWidth > 0 {
		h := m.StrokeWidth / 2
		m.Box = Rect{m.Box.X0 - h, m.Box.Y0 - h, m.Box.X1 + h, m.Box.Y1 + h}
	}
	return m, true
}

func pointsOf(s string) (pts []Point) {
	fs := strings.FieldsFunc(s, func(r rune) bool { return r == ' ' || r == ',' || r == '\n' || r == '\t' })
	pts = make([]Point, 0, len(fs)/2)
	for i := 0; i+1 < len(fs); i += 2 {
		pts = append(pts, Point{num(fs[i]), num(fs[i+1])})
	}
	return pts
}

// pathPoints reads the coordinates of a path's absolute commands in order.
// The exporter writes absolute M/L/Q/C/Z; control points are kept, which only
// widens the bounds of a curve.
func pathPoints(d string) (pts []Point) {
	fs := strings.FieldsFunc(d, func(r rune) bool { return r == ' ' || r == ',' })
	nums := make([]float64, 0, len(fs))
	for _, f := range fs {
		f = strings.TrimLeft(f, "MLQCZmlqcz")
		if f == "" {
			continue
		}
		v, err := strconv.ParseFloat(f, 64)
		if err == nil {
			nums = append(nums, v)
		}
	}
	pts = make([]Point, 0, len(nums)/2)
	for i := 0; i+1 < len(nums); i += 2 {
		pts = append(pts, Point{nums[i], nums[i+1]})
	}
	return pts
}

func boundsOf(pts []Point) (r Rect) {
	if len(pts) == 0 {
		return Rect{}
	}
	r = Rect{pts[0].X, pts[0].Y, pts[0].X, pts[0].Y}
	for _, p := range pts[1:] {
		r.X0, r.Y0 = min(r.X0, p.X), min(r.Y0, p.Y)
		r.X1, r.Y1 = max(r.X1, p.X), max(r.Y1, p.Y)
	}
	return r
}

// covers reports whether the mark's filled area contains the point.
func (inst Mark) covers(x, y float64) bool {
	if inst.Fill.A <= 0 || !inst.Box.Contains(x, y) || !inst.Clip.Contains(x, y) {
		return false
	}
	switch inst.Kind {
	case MarkKindPolygon, MarkKindPath:
		return insidePoly(inst.Poly, x, y)
	case MarkKindCircle:
		cx, cy := inst.Box.Center()
		rx, ry := inst.Box.W()/2, inst.Box.H()/2
		if rx <= 0 || ry <= 0 {
			return false
		}
		dx, dy := (x-cx)/rx, (y-cy)/ry
		return dx*dx+dy*dy <= 1
	case MarkKindRect:
		return true
	}
	return false
}

// insidePoly is the even-odd rule.
func insidePoly(pts []Point, x, y float64) (in bool) {
	n := len(pts)
	for i, j := 0, n-1; i < n; j, i = i, i+1 {
		pi, pj := pts[i], pts[j]
		if (pi.Y > y) != (pj.Y > y) && x < (pj.X-pi.X)*(y-pi.Y)/(pj.Y-pi.Y)+pi.X {
			in = !in
		}
	}
	return in
}
