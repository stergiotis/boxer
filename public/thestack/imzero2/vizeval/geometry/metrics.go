package geometry

import (
	"image"
	"math"
	"regexp"
	"slices"
	"strings"
)

// Metric names. Each is a number, not a verdict: a scenario's gates decide
// what a value means (ADR-0257 §SD6).
const (
	// MetricTextRuns counts text runs visible in the area.
	MetricTextRuns = "text.runs"
	// MetricTextOverlapPairs counts pairs of visible runs whose ink boxes
	// overlap by more than a sliver.
	MetricTextOverlapPairs = "text.overlap_pairs"
	// MetricTextClipped counts runs cut by a clip inside the area — a cell or
	// widget too small for its text.
	MetricTextClipped = "text.clipped"
	// MetricTextCutAtEdge counts runs cut by the area's own edge — content
	// scrolled or laid out past the visible artifact.
	MetricTextCutAtEdge = "text.cut_at_edge"
	// MetricTextElided counts runs shortened to fit: egui elided them, or the
	// sink wrote an ellipsis into the text itself (the box-drawn tables cut
	// wide cells that way, and egui never sees a cut).
	MetricTextElided = "text.elided"
	// MetricTextMinSize is the smallest font size among visible runs, points.
	MetricTextMinSize = "text.min_size"
	// MetricTextMinContrast is the lowest WCAG contrast ratio of a visible
	// run against what is painted under its centre.
	MetricTextMinContrast = "text.min_contrast"
	// MetricTextLowContrast counts runs below WCAG AA for body text (4.5).
	MetricTextLowContrast = "text.low_contrast"
	// MetricTextRowPitchCV is the coefficient of variation of the vertical
	// gaps between distinct text baselines — 0 for a perfectly even rhythm.
	MetricTextRowPitchCV = "text.row_pitch_cv"
	// MetricMarks counts non-text marks visible in the area.
	MetricMarks = "marks.count"
	// MetricInkRatio is the share of the area's pixels that differ from its
	// most common colour.
	MetricInkRatio = "ink.ratio"
	// MetricColorDistinct counts distinct chromatic colours in use.
	MetricColorDistinct = "color.distinct"
	// MetricColorMinDeltaE is the smallest CIEDE2000 distance between two of
	// those colours; absent with fewer than two.
	MetricColorMinDeltaE = "color.min_delta_e"
	// MetricTableNumericColumns counts columns of numbers: three or more
	// numeric runs stacked in overlapping x-extents.
	MetricTableNumericColumns = "table.numeric_columns"
	// MetricTableNumericRightAligned counts those whose right edges align.
	MetricTableNumericRightAligned = "table.numeric_right_aligned"
)

const (
	// overlapMinArea ignores glyph boxes that merely touch: egui rows are
	// laid out edge to edge, and ascender/descender bounds of adjacent rows
	// may share a sub-point sliver.
	overlapMinArea = 2.0
	overlapMinSide = 1.0
	// clipTolerance is how far ink may pass a clip before it counts as cut;
	// antialiasing and rounding put glyph bounds a fraction outside.
	clipTolerance = 0.5
	// edgeTolerance decides whether a clip side is the area's side.
	edgeTolerance  = 1.5
	alignTolerance = 1.5
	// chromaMin separates a colour from a grey or a near-grey.
	chromaMin = 15.0
	// sameColorDeltaE merges colours a reader would not tell apart anyway,
	// so antialiased or blended variants of one colour count once.
	sameColorDeltaE = 3.0
	// wcagAA is the minimum contrast for body text.
	wcagAA = 4.5
	// inkChannelDelta is the per-channel difference from the modal colour
	// above which a pixel counts as ink.
	inkChannelDelta = 24
)

var numericRe = regexp.MustCompile(`^[-+−]?(\d[\d,' ]*)?\.?\d+([eE][-+]?\d+)?\s?%?$`)

// VisibleArea is the artifact's rect within the viewport. The artifact node
// already reports only its visible part (the accessibleRegion block clips it to
// the enclosing ui), so all that is left is the capture's own edge.
func VisibleArea(d Drawing, artifact Rect) (area Rect) {
	return artifact.Intersect(d.Viewport)
}

// Measure computes the metrics of the part of d inside area. png, when not
// nil, is the capture the drawing was exported beside, for the ink ratio; its
// scale to the viewport is taken from the two sizes.
func Measure(d Drawing, area Rect, png image.Image) (m map[string]float64) {
	m = make(map[string]float64, 16)
	runs := make([]TextRun, 0, len(d.Runs))
	for _, r := range d.Runs {
		if !r.Box.Intersect(r.Clip).Intersect(area).Empty() {
			runs = append(runs, r)
		}
	}
	m[MetricTextRuns] = float64(len(runs))
	measureText(d, runs, area, m)
	measureColor(d, runs, area, m)
	measureTable(runs, area, m)
	if png != nil {
		if ink, ok := inkRatio(png, d.Viewport, area); ok {
			m[MetricInkRatio] = ink
		}
	}
	return m
}

func visibleBox(r TextRun, area Rect) Rect {
	return r.Box.Intersect(r.Clip).Intersect(area)
}

func measureText(d Drawing, runs []TextRun, area Rect, m map[string]float64) {
	var overlaps, clipped, cutAtEdge, elided, lowContrast int
	minSize, minContrast := math.Inf(1), math.Inf(1)
	for i, r := range runs {
		vb := visibleBox(r, area)
		for _, o := range runs[i+1:] {
			x := vb.Intersect(visibleBox(o, area))
			if x.Area() > overlapMinArea && x.W() > overlapMinSide && x.H() > overlapMinSide {
				overlaps++
			}
		}
		switch cutKind(r, area) {
		case cutByClip:
			clipped++
		case cutByEdge:
			cutAtEdge++
		}
		if r.Elided || strings.Contains(r.Text, "…") {
			elided++
		}
		if r.Size > 0 {
			minSize = min(minSize, r.Size)
		}
		cx, cy := vb.Center()
		bg := backgroundAt(d, r.Order, cx, cy)
		c := Contrast(over(r.Fill, bg), bg)
		minContrast = min(minContrast, c)
		if c < wcagAA {
			lowContrast++
		}
	}
	m[MetricTextOverlapPairs] = float64(overlaps)
	m[MetricTextClipped] = float64(clipped)
	m[MetricTextCutAtEdge] = float64(cutAtEdge)
	m[MetricTextElided] = float64(elided)
	m[MetricTextLowContrast] = float64(lowContrast)
	if len(runs) > 0 {
		m[MetricTextMinSize] = minSize
		m[MetricTextMinContrast] = minContrast
	}
	if cv, ok := rowPitchCV(runs, area); ok {
		m[MetricTextRowPitchCV] = cv
	}
}

type cutE uint8

const (
	cutNone cutE = iota
	cutByClip
	cutByEdge
)

// cutKind decides whether a run's ink passes its clip or the area, and if so
// whether every side it passes is the area's own edge (content running past
// the visible artifact) or some side is a clip inside it (text too big for
// its box).
func cutKind(r TextRun, area Rect) cutE {
	lim := r.Clip.Intersect(area)
	b := r.Box
	type side struct {
		over     bool
		limit    float64
		areaSide float64
	}
	sides := [4]side{
		{b.X0 < lim.X0-clipTolerance, lim.X0, area.X0},
		{b.Y0 < lim.Y0-clipTolerance, lim.Y0, area.Y0},
		{b.X1 > lim.X1+clipTolerance, lim.X1, area.X1},
		{b.Y1 > lim.Y1+clipTolerance, lim.Y1, area.Y1},
	}
	cut := cutNone
	for _, s := range sides {
		if !s.over {
			continue
		}
		if math.Abs(s.limit-s.areaSide) > edgeTolerance {
			return cutByClip
		}
		cut = cutByEdge
	}
	return cut
}

// backgroundAt composites every filled mark painted before order that covers
// the point, over the opaque black the host clears to.
func backgroundAt(d Drawing, order int, x, y float64) (bg RGBA) {
	bg = RGBA{0, 0, 0, 1}
	for _, mk := range d.Marks {
		if mk.Order >= order {
			break
		}
		if mk.covers(x, y) {
			bg = over(mk.Fill, bg)
		}
	}
	return bg
}

func rowPitchCV(runs []TextRun, area Rect) (cv float64, ok bool) {
	base := make([]float64, 0, len(runs))
	for _, r := range runs {
		base = append(base, math.Round(r.Box.Y1*2)/2)
	}
	slices.Sort(base)
	base = slices.Compact(base)
	gaps := make([]float64, 0, len(base))
	for i := 1; i < len(base); i++ {
		if g := base[i] - base[i-1]; g > 2 {
			gaps = append(gaps, g)
		}
	}
	if len(gaps) < 2 {
		return 0, false
	}
	var sum, sq float64
	for _, g := range gaps {
		sum += g
	}
	mean := sum / float64(len(gaps))
	for _, g := range gaps {
		sq += (g - mean) * (g - mean)
	}
	return math.Sqrt(sq/float64(len(gaps))) / mean, true
}

func measureColor(d Drawing, runs []TextRun, area Rect, m map[string]float64) {
	var marks int
	cols := make([]Lab, 0, 16)
	add := func(c RGBA) {
		if c.A < 0.5 {
			return
		}
		lab := c.Lab()
		if lab.Chroma() < chromaMin {
			return
		}
		for _, k := range cols {
			if DeltaE2000(k, lab) < sameColorDeltaE {
				return
			}
		}
		cols = append(cols, lab)
	}
	for _, mk := range d.Marks {
		if mk.Box.Intersect(mk.Clip).Intersect(area).Empty() {
			continue
		}
		marks++
		add(mk.Fill)
		add(mk.Stroke)
	}
	for _, r := range runs {
		add(r.Fill)
	}
	m[MetricMarks] = float64(marks)
	m[MetricColorDistinct] = float64(len(cols))
	if len(cols) >= 2 {
		best := math.Inf(1)
		for i := range cols {
			for j := i + 1; j < len(cols); j++ {
				best = min(best, DeltaE2000(cols[i], cols[j]))
			}
		}
		m[MetricColorMinDeltaE] = best
	}
}

// measureTable finds columns of numbers and whether they align on the right,
// the convention that lets a reader compare magnitudes down a column.
func measureTable(runs []TextRun, area Rect, m map[string]float64) {
	nums := make([]Rect, 0, len(runs))
	for _, r := range runs {
		if numericRe.MatchString(strings.TrimSpace(r.Text)) {
			nums = append(nums, visibleBox(r, area))
		}
	}
	// Union-find over x-extent overlap: runs stacked in one column share
	// horizontal extent, runs in different columns do not.
	parent := make([]int, len(nums))
	for i := range parent {
		parent[i] = i
	}
	var find func(int) int
	find = func(i int) int {
		for parent[i] != i {
			parent[i] = parent[parent[i]]
			i = parent[i]
		}
		return i
	}
	for i := range nums {
		for j := i + 1; j < len(nums); j++ {
			if nums[i].X0 < nums[j].X1 && nums[j].X0 < nums[i].X1 {
				parent[find(i)] = find(j)
			}
		}
	}
	groups := make(map[int][]Rect, len(nums))
	for i, b := range nums {
		groups[find(i)] = append(groups[find(i)], b)
	}
	var columns, right int
	for _, g := range groups {
		if len(g) < 3 {
			continue
		}
		columns++
		lo, hi := math.Inf(1), math.Inf(-1)
		for _, b := range g {
			lo, hi = min(lo, b.X1), max(hi, b.X1)
		}
		if hi-lo <= alignTolerance {
			right++
		}
	}
	m[MetricTableNumericColumns] = float64(columns)
	m[MetricTableNumericRightAligned] = float64(right)
}

// inkRatio counts the area's pixels that differ from its modal colour. The
// modal colour is taken over 5-bit-per-channel buckets so antialiasing does not
// split the background into many near-identical colours.
func inkRatio(img image.Image, viewport Rect, area Rect) (ratio float64, ok bool) {
	b := img.Bounds()
	if viewport.W() <= 0 || viewport.H() <= 0 {
		return 0, false
	}
	sx, sy := float64(b.Dx())/viewport.W(), float64(b.Dy())/viewport.H()
	px := image.Rect(
		b.Min.X+int(math.Floor((area.X0-viewport.X0)*sx)), b.Min.Y+int(math.Floor((area.Y0-viewport.Y0)*sy)),
		b.Min.X+int(math.Ceil((area.X1-viewport.X0)*sx)), b.Min.Y+int(math.Ceil((area.Y1-viewport.Y0)*sy)),
	).Intersect(b)
	if px.Empty() {
		return 0, false
	}
	hist := make(map[uint32]int, 256)
	for y := px.Min.Y; y < px.Max.Y; y++ {
		for x := px.Min.X; x < px.Max.X; x++ {
			r, g, bb, _ := img.At(x, y).RGBA()
			hist[(r>>11)<<10|(g>>11)<<5|bb>>11]++
		}
	}
	var mode uint32
	best := -1
	for k, n := range hist {
		if n > best || (n == best && k < mode) {
			mode, best = k, n
		}
	}
	mr, mg, mb := int((mode>>10&31)<<3+4), int((mode>>5&31)<<3+4), int((mode&31)<<3+4)
	var ink int
	for y := px.Min.Y; y < px.Max.Y; y++ {
		for x := px.Min.X; x < px.Max.X; x++ {
			r, g, bb, _ := img.At(x, y).RGBA()
			if absInt(int(r>>8)-mr) > inkChannelDelta || absInt(int(g>>8)-mg) > inkChannelDelta ||
				absInt(int(bb>>8)-mb) > inkChannelDelta {
				ink++
			}
		}
	}
	return float64(ink) / float64(px.Dx()*px.Dy()), true
}

func absInt(v int) int {
	if v < 0 {
		return -v
	}
	return v
}
