package leewaywidgets

import (
	"fmt"
	"math"
	"regexp"
	"slices"
	"strconv"
	"strings"
	"unicode/utf8"

	"github.com/stergiotis/boxer/public/keelson/designsystem/styletokens"
	"github.com/stergiotis/boxer/public/semistructured/leeway/lwlens"
	c "github.com/stergiotis/boxer/public/thestack/imzero2/egui2/bindings"
	"github.com/stergiotis/boxer/public/thestack/imzero2/egui2/widgets/color"
)

// LensView paints a lwlens.Plan: rows of a leeway batch in bands, each row
// drawn against a frame of slots or on its own, each cell at the plan's
// detail. Every text is monospace, so widths are computed rather than
// measured and a column of values lines up.
type LensView struct {
	ids *c.WidgetIdStack
}

// LensFormE is how the lens lays a plan out.
type LensFormE uint8

const (
	// LensFormRows draws every row, in bands.
	LensFormRows LensFormE = iota
	// LensFormArchetypes draws each band as its template and the rows that
	// break it.
	LensFormArchetypes
	// LensFormFocus draws one row as a list of its slots, in the context
	// of its cluster and beside its nearest peers.
	LensFormFocus
)

// NewLensView returns a view drawing on ids.
func NewLensView(ids *c.WidgetIdStack) *LensView {
	return &LensView{ids: ids}
}

// Geometry, in points. The advance is a monospace face's, a little generous
// so a label computed to fit does fit.
const (
	lensFont        float32 = 11
	lensSmallFont   float32 = 10
	lensAdvance     float32 = 0.62 * lensFont
	lensPad         float32 = 8
	lensSectionGap  float32 = 7
	lensBandGap     float32 = 10
	lensLabelMax            = 22
	lensShapePitch  float32 = 10
	lensShapeCell   float32 = 8
	lensFingerPitch float32 = 18
	lensFingerCell  float32 = 15
	lensGistMax             = 8
	lensValuesMax           = 18
	lensMissingMax          = 3
	// lensAlsoMax is how many of the rows a collapsed row stands for are
	// named: enough to recognise the group, not a list to read.
	lensAlsoMax = 4
)

// lensRowHeight is a row's pitch at a detail.
func lensRowHeight(d lwlens.DetailE) float32 {
	switch d {
	case lwlens.DetailShape:
		return 12
	case lwlens.DetailFingerprint:
		return 14
	}
	return 17
}

func lensTok(v styletokens.RGBA8) color.Color { return color.RGBA(v.R, v.G, v.B, v.A) }

// lensMix is a opaque blend of two tokens, t of the way from a to b: a tint
// that stays a tint on whichever surface the theme paints.
func lensMix(a, b styletokens.RGBA8, t float32) color.Color {
	m := func(x, y uint8) uint8 { return uint8(float32(x) + (float32(y)-float32(x))*t + 0.5) }
	return color.RGBA(m(a.R, b.R), m(a.G, b.G), m(a.B, b.B), 255)
}

// lensTextW is the width of s in the monospace face at size f.
func lensTextW(s string, f float32) float32 {
	return float32(utf8.RuneCountInString(s)) * 0.62 * f
}

// lensFit cuts s to n runes, marking a cut with an ellipsis.
func lensFit(s string, n int) string {
	if n <= 0 {
		return ""
	}
	if utf8.RuneCountInString(s) <= n {
		return s
	}
	if n == 1 {
		return "…"
	}
	rs := []rune(s)
	return string(rs[:n-1]) + "…"
}

// lensShortNum writes a number in at most about six characters.
func lensShortNum(v float64) string {
	a := math.Abs(v)
	switch {
	case a == 0:
		return "0"
	case a >= 1e9:
		return strconv.FormatFloat(v/1e9, 'f', 1, 64) + "G"
	case a >= 1e6:
		return strconv.FormatFloat(v/1e6, 'f', 1, 64) + "M"
	case a >= 1e4:
		return strconv.FormatFloat(v/1e3, 'f', 1, 64) + "k"
	case a >= 100 || v == math.Trunc(v):
		return strconv.FormatFloat(v, 'f', 0, 64)
	case a >= 1:
		return strconv.FormatFloat(v, 'f', 1, 64)
	}
	return strconv.FormatFloat(v, 'g', 2, 64)
}

// lensBandName is a band's letter; the unclustered rows have none.
func lensBandName(b *lwlens.Band, i int) string {
	if b.Cluster < 0 {
		return "·"
	}
	if i < 26 {
		return string(rune('A' + i))
	}
	return strconv.Itoa(i + 1)
}

// lensPainter holds one frame's drawing state.
type lensPainter struct {
	a      *lwlens.Analysis
	p      *lwlens.Plan
	w, h   float32
	y      float32
	labelW float32
	// sectionTone is each section's colour, by its index in the model.
	sectionTone []color.Color
	full        bool
	// short is each slot's member name less the prefix it shares with a
	// sibling in its section; one name per slot everywhere it is drawn.
	short []string
	form  LensFormE
	// maxGroup is the most rows one collapsed row stands for.
	maxGroup int
	// labelPrefix is, per band, the runes every row label of the band
	// begins with, left out of the label column and said in the band's
	// header.
	labelPrefix []int
}

// Render draws plan p of analysis a in a w×h box, laid out as form; focus
// is the row LensFormFocus draws.
func (inst *LensView) Render(a *lwlens.Analysis, p *lwlens.Plan, form LensFormE, focus int32, w, h float32) {
	if a == nil || p == nil || len(a.Model.Rows) == 0 {
		c.Label("The batch has no rows.").Send()
		return
	}
	lp := &lensPainter{a: a, p: p, w: w, h: h, y: lensPad, form: form}
	lp.sectionTone = lensSectionTones(a)
	lp.labelPrefix = make([]int, len(a.Bands))
	longest := 0
	for _, r := range a.Model.Rows {
		longest = max(longest, utf8.RuneCountInString(r.Label))
	}
	for bi, b := range a.Bands {
		if longest <= lensLabelMax {
			// Labels that fit the column lose more by the cut than it saves.
			break
		}
		bl := make([]string, 0, len(b.Rows))
		for _, r := range b.Rows {
			bl = append(bl, a.Model.Rows[r].Label)
		}
		lp.labelPrefix[bi] = lwlens.CommonPrefix(bl, 6)
	}
	labelChars := 4
	for r := range a.Model.Rows {
		labelChars = max(labelChars, utf8.RuneCountInString(lp.rowLabel(int32(r))))
	}
	badge := 0
	for _, pb := range p.Bands {
		for _, pr := range pb.Rows {
			if len(pr.Also) > 0 {
				badge = max(badge, len(strconv.Itoa(len(pr.Also)+1))+2)
			}
			lp.maxGroup = max(lp.maxGroup, len(pr.Also)+1)
		}
	}
	lp.labelW = float32(min(labelChars, lensLabelMax)+badge)*lensAdvance + 12
	lp.short = make([]string, len(a.Model.Slots))
	for _, sec := range a.Model.Sections {
		var idx []int
		var names []string
		for s, sl := range a.Model.Slots {
			if sl.Section == sec {
				idx = append(idx, s)
				names = append(names, sl.Member)
			}
		}
		for k, n := range lensShortNames(names) {
			lp.short[idx[k]] = n
		}
	}
	switch form {
	case LensFormArchetypes:
		lp.paintArchetypes()
	case LensFormFocus:
		lp.paintFocus(focus)
	default:
		lp.paint()
	}
	for range c.IdScope(inst.ids.PrepareStr("lens")) {
		c.PaintCanvas(inst.ids.PrepareStr("canvas"), w, min(h, lp.y+lensPad)).Send()
	}
}

// lensSectionTones gives the tagged sections the qualitative colours in
// canonical order, as far as the cycle goes without repeating; the sections
// past it, and the plain one, are neutral. A repeated hue would say two
// sections are one.
func lensSectionTones(a *lwlens.Analysis) (tones []color.Color) {
	m := a.Model
	tones = make([]color.Color, len(m.Sections))
	for i := range tones {
		tones[i] = lensTok(styletokens.NeutralTextSecondary)
	}
	next := 0
	seen := make([]bool, len(m.Sections))
	for _, s := range a.Order {
		i := m.SectionOf(s)
		if seen[i] || m.Sections[i] == lwlens.PlainSection {
			continue
		}
		seen[i] = true
		if next < styletokens.QualitativeCycleLen {
			tones[i] = lensTok(styletokens.QualitativeCycle(next))
			next++
		}
	}
	return
}

func (inst *lensPainter) text(x, y float32, s string, f float32, col color.Color) {
	c.PaintText(x, y, 0, 1, s, f, col).Monospace().Send()
}

func (inst *lensPainter) textRight(x, y float32, s string, f float32, col color.Color) {
	c.PaintText(x, y, 2, 1, s, f, col).Monospace().Send()
}

func (inst *lensPainter) paint() {
	inst.paintSummary()
	inst.paintLegend()
	if inst.p.Scope == lwlens.ScopeGlobal {
		inst.paintConstants(inst.p.Constants, "every row:")
		inst.paintFrameHeader(inst.p.Frame)
	}
	for bi := range inst.p.Bands {
		if inst.full {
			break
		}
		inst.paintBand(&inst.p.Bands[bi])
	}
}

// paintSummary is one line saying what the picture is.
func (inst *lensPainter) paintSummary() {
	a, p := inst.a, inst.p
	clusters := 0
	for _, b := range a.Bands {
		if b.Cluster >= 0 && a.Clustered {
			clusters++
		}
	}
	scope := map[lwlens.ScopeE]string{
		lwlens.ScopeRow: "each row on its own terms", lwlens.ScopeBand: "one frame per cluster",
		lwlens.ScopeGlobal: "one frame for all rows",
	}[p.Scope]
	s := fmt.Sprintf("%d rows · %d slots in %d sections · %d clusters by slot presence · %s · %s",
		len(a.Model.Rows), len(a.Model.Slots), len(a.Model.Sections), clusters, p.Detail, scope)
	if k := inst.labelPrefix[0]; !a.Clustered && k > 0 {
		s += " · labels begin " + string([]rune(a.Model.Rows[0].Label)[:k])
	}
	inst.text(lensPad, inst.y+7, lensFit(s, int((inst.w-2*lensPad)/(0.62*lensSmallFont))), lensSmallFont, lensTok(styletokens.NeutralTextSecondary))
	inst.y += 18
}

// paintLegend keys the colours and marks the detail uses.
func (inst *lensPainter) paintLegend() {
	x, y := lensPad, inst.y+7
	sec := lensTok(styletokens.NeutralTextSecondary)
	put := func(s string, col color.Color) {
		inst.text(x, y, s, lensSmallFont, col)
		x += lensTextW(s, lensSmallFont) + 10
	}
	switch inst.p.Detail {
	case lwlens.DetailShape:
		for i, name := range inst.a.Model.Sections {
			if name == lwlens.PlainSection {
				continue
			}
			if x > inst.w-160 {
				put("…", sec)
				break
			}
			c.PaintRectFilled(x, y-4, x+8, y+4, 1, inst.sectionTone[i]).Send()
			x += 11
			put(name, sec)
		}
	case lwlens.DetailFingerprint:
		put("bar: rank of the value among its slot's values", sec)
		put("chip: one of the slot's most frequent values, same colour same value", sec)
	case lwlens.DetailGist:
		put("value, and its rank among the slot's values (bar)", sec)
	case lwlens.DetailValues:
		put("values; numbers right-aligned", sec)
	}
	if inst.p.Detail >= lwlens.DetailGist {
		put("accent: among the slot's most unusual values", lensTok(styletokens.AccentDefault))
	}
	if inst.form == LensFormArchetypes && inst.p.Detail >= lwlens.DetailGist {
		put("(n): no value holds half the rows, n distinct", sec)
	}
	if inst.p.Detail != lwlens.DetailValues {
		c.PaintRectStroke(x, y-4, x+8, y+4, 1, lensTok(styletokens.ErrorDefault), 1.5).Send()
		x += 11
		put("missing: its cluster nearly always has it", sec)
		c.PaintRectStroke(x, y-4, x+8, y+4, 1, lensTok(styletokens.WarningDefault), 1.5).Send()
		x += 11
		put("unusual: its cluster rarely has it", sec)
	}
	inst.y += 18
}

// cellW is a frame cell's width for slot s.
func (inst *lensPainter) cellW(s int32) float32 {
	switch inst.p.Detail {
	case lwlens.DetailShape:
		return lensShapePitch
	case lwlens.DetailFingerprint:
		return lensFingerPitch
	}
	return float32(inst.cellChars(s))*lensAdvance + 8
}

// cellChars is how many characters a frame cell of slot s holds.
func (inst *lensPainter) cellChars(s int32) int {
	sl := inst.a.Model.Slots[s]
	st := &inst.a.Stats[s]
	want := st.Width
	if inst.p.Detail == lwlens.DetailGist && st.Prefix > 0 {
		want = want - st.Prefix + 1
	}
	if len(st.Categories) > 0 {
		if _, ok := lensShortTime(st.Categories[0]); ok && inst.p.Detail == lwlens.DetailGist {
			return lensShortTimeLen
		}
	}
	if sl.Kind == lwlens.ValueKindNumeric {
		want = 0
		for _, v := range st.Sorted {
			want = max(want, len(inst.numText(s, v)))
		}
	}
	if sl.Kind == lwlens.ValueKindCategorical && inst.p.Detail == lwlens.DetailGist {
		want++ // the chip
	}
	want = max(want, min(utf8.RuneCountInString(inst.memberName(s)), 10))
	lim := lensValuesMax
	if inst.p.Detail == lwlens.DetailGist {
		lim = lensGistMax
	}
	return max(3, min(want, lim))
}

func (inst *lensPainter) numText(s int32, v float64) string {
	if inst.p.Detail == lwlens.DetailValues {
		return strconv.FormatFloat(v, 'f', -1, 64)
	}
	if d := inst.a.Stats[s].Decimals; d > 0 && math.Abs(v) < 1e4 {
		return strconv.FormatFloat(v, 'f', d, 64)
	}
	return lensShortNum(v)
}

// memberName is how a slot is named under its section.
func (inst *lensPainter) memberName(s int32) string {
	sl := inst.a.Model.Slots[s]
	if sl.Member == "" {
		return sl.Section
	}
	if inst.short != nil && inst.short[s] != "" {
		return inst.short[s]
	}
	return sl.Member
}

// frameFit is how many of frame's slots fit the width after the label
// column, leaving room for an overflow count.
func (inst *lensPainter) frameFit(frame []int32) int {
	x := lensPad + inst.labelW
	limit := inst.w - lensPad - 4*lensAdvance
	prev := ""
	for i, s := range frame {
		sec := inst.a.Model.Slots[s].Section
		if i > 0 && sec != prev {
			x += lensSectionGap
		}
		prev = sec
		x += inst.cellW(s)
		if x > limit {
			return i
		}
	}
	return len(frame)
}

// frameXs is where each fitted frame slot's cell starts.
func (inst *lensPainter) frameXs(frame []int32) (xs []float32) {
	x := lensPad + inst.labelW
	prev := ""
	for i, s := range frame {
		sec := inst.a.Model.Slots[s].Section
		if i > 0 && sec != prev {
			x += lensSectionGap
		}
		prev = sec
		xs = append(xs, x)
		x += inst.cellW(s)
	}
	return
}

// paintFrameHeader names the frame's sections, and at the text details its
// slots, above its cells.
func (inst *lensPainter) paintFrameHeader(frame []int32) {
	if len(frame) == 0 {
		return
	}
	frame = frame[:inst.frameFit(frame)]
	xs := inst.frameXs(frame)
	m := inst.a.Model
	secCol := lensTok(styletokens.NeutralTextSecondary)
	y := inst.y + 6
	// Section spans. Members of one span often share a prefix — a facts
	// table names its attributes by kind — and a narrow column would show
	// only that prefix, so the span's header carries it once and the
	// columns the rest.
	names := make([]string, len(frame))
	for i := 0; i < len(frame); {
		sec := m.Slots[frame[i]].Section
		j := i
		var members []string
		for j < len(frame) && m.Slots[frame[j]].Section == sec {
			members = append(members, inst.memberName(frame[j]))
			j++
		}
		cut := lwlens.CommonPrefix(members, 4)
		for k := range members {
			names[i+k] = members[k][len(string([]rune(members[k])[:cut])):]
		}
		x0 := xs[i]
		x1 := xs[j-1] + inst.cellW(frame[j-1])
		tone := inst.sectionTone[m.SectionOf(frame[i])]
		c.PaintLine(x0, y+7, x1-2, y+7, tone, 2).Send()
		head := sec
		if cut > 0 && inst.p.Detail >= lwlens.DetailFingerprint {
			head += " " + string([]rune(members[0])[:cut]) + "…"
		}
		if name := lensFit(head, int((x1-x0)/(0.62*lensSmallFont))); name != "" {
			inst.text(x0, y, name, lensSmallFont, secCol)
		}
		i = j
	}
	inst.y += 16
	if inst.p.Detail == lwlens.DetailFingerprint {
		// A fingerprint cell is too narrow for its name, so the names
		// alternate between two lines and each gets two cells' width.
		for i, s := range frame {
			n := int((2*inst.cellW(s) - 4) / (0.62 * lensSmallFont))
			inst.text(xs[i], inst.y+6+float32(i%2)*12, lensFit(names[i], n), lensSmallFont, secCol)
		}
		inst.y += 28
	}
	if inst.p.Detail >= lwlens.DetailGist {
		for i, s := range frame {
			n := int((inst.cellW(s) - 4) / (0.62 * lensSmallFont))
			inst.text(xs[i], inst.y+6, lensFit(names[i], n), lensSmallFont, secCol)
		}
		inst.y += 14
	}
}

// lensShortNames drops from each name the longest prefix, cut at a
// separator, that it shares with another name of the list: a column then
// shows what tells it from its neighbours, not what they all begin with.
func lensShortNames(names []string) (short []string) {
	short = make([]string, len(names))
	for i, n := range names {
		cut := 0
		for j, o := range names {
			if i != j {
				cut = max(cut, lwlens.CommonPrefix([]string{n, o}, 4))
			}
		}
		short[i] = string([]rune(n)[cut:])
	}
	// Ids rendered as hex share long runs of digits and no separator:
	// keep what differs, and four digits of context before it.
	var hexIdx []int
	var hexNames []string
	for i, n := range names {
		if strings.HasPrefix(n, "0x") {
			hexIdx = append(hexIdx, i)
			hexNames = append(hexNames, n)
		}
	}
	if cut := lensHexPrefix(hexNames); cut > 0 {
		for _, i := range hexIdx {
			short[i] = "…" + names[i][cut:]
		}
	}
	return
}

// lensHexPrefix is how much of a list of hex ids ("0x…", one length) can
// be dropped: their common prefix less four digits; 0 when they are not
// such a list.
func lensHexPrefix(names []string) int {
	if len(names) < 2 {
		return 0
	}
	for _, n := range names {
		if !strings.HasPrefix(n, "0x") || len(n) != len(names[0]) {
			return 0
		}
	}
	p := len(names[0])
	for _, n := range names[1:] {
		k := 0
		for k < p && n[k] == names[0][k] {
			k++
		}
		p = k
	}
	return max(0, p-4)
}

// paintConstants lists slots with one value throughout, as name=value, on a
// line of their own; it is how a band says what all its rows share.
func (inst *lensPainter) paintConstants(slots []int32, lead string) {
	if len(slots) == 0 || inst.p.Detail == lwlens.DetailShape {
		return
	}
	x, cy := lensPad+8, inst.y+7
	sec := lensTok(styletokens.NeutralTextSecondary)
	pri := lensTok(styletokens.NeutralTextPrimary)
	inst.text(x, cy, lead, lensSmallFont, sec)
	x += lensTextW(lead, lensSmallFont) + 8
	for i, s := range slots {
		var text string
		for _, r := range inst.a.Model.Rows {
			if c, ok := r.Cell(s); ok {
				text = c.Text
				break
			}
		}
		name := inst.memberName(s)
		val := lensFit(text, 28)
		need := lensTextW(name, lensSmallFont) + 4 + lensTextW(val, lensSmallFont) + 14
		if x+need > inst.w-lensPad {
			inst.text(x, cy, fmt.Sprintf("+%d", len(slots)-i), lensSmallFont, sec)
			break
		}
		inst.text(x, cy, name, lensSmallFont, sec)
		x += lensTextW(name, lensSmallFont) + 4
		inst.text(x, cy, val, lensSmallFont, pri)
		x += lensTextW(val, lensSmallFont) + 14
	}
	inst.y += 15
}

// paintBand draws a band's header, its frame header under ScopeBand, and
// its rows.
func (inst *lensPainter) paintBand(pb *lwlens.PlanBand) {
	a := inst.a
	b := &a.Bands[pb.Band]
	if pb.Band > 0 {
		inst.y += lensBandGap
	}
	if a.Clustered {
		inst.paintBandHeader(b, pb.Band)
	}
	inst.paintConstants(pb.Constants, "all:")
	frame := inst.p.Frame
	if inst.p.Scope == lwlens.ScopeBand {
		frame = pb.Frame
		inst.paintFrameHeader(frame)
	}
	rh := lensRowHeight(inst.p.Detail)
	for i := range pb.Rows {
		if inst.y+2*rh > inst.h-lensPad && i < len(pb.Rows)-1 {
			inst.text(lensPad, inst.y+rh/2, fmt.Sprintf("… %d more rows", len(pb.Rows)-i), lensSmallFont,
				lensTok(styletokens.NeutralTextSecondary))
			inst.y += rh
			inst.full = true
			return
		}
		inst.paintRow(&pb.Rows[i], frame, rh)
		inst.y += rh
	}
}

// ruleText reads a band's rule as has/lacks literals.
func (inst *lensPainter) ruleText(b *lwlens.Band) string {
	var parts []string
	for _, t := range b.Rule {
		lbl := inst.a.Model.Slots[t.Slot].Label()
		if t.Has {
			parts = append(parts, "has "+lbl)
		} else {
			parts = append(parts, "lacks "+lbl)
		}
	}
	return strings.Join(parts, " ∧ ")
}

func (inst *lensPainter) paintBandHeader(b *lwlens.Band, i int) {
	y := inst.y + 8
	name := lensBandName(b, i)
	pri := lensTok(styletokens.NeutralTextPrimary)
	sec := lensTok(styletokens.NeutralTextSecondary)
	c.PaintRectFilled(lensPad, y-7, lensPad+3, y+7, 0, pri).Send()
	x := lensPad + 8
	head := fmt.Sprintf("%s  %d rows", name, len(b.Rows))
	if b.Cluster < 0 {
		head = fmt.Sprintf("unclustered  %d rows", len(b.Rows))
	}
	inst.text(x, y, head, lensFont, pri)
	x += lensTextW(head, lensFont) + 12
	if k := inst.labelPrefix[i]; k > 0 {
		lead := "labels begin " + string([]rune(inst.a.Model.Rows[b.Rows[0]].Label)[:k])
		inst.text(x, y, lead, lensSmallFont, sec)
		x += lensTextW(lead, lensSmallFont) + 12
	}
	if rule := inst.ruleText(b); rule != "" && inst.p.Detail < lwlens.DetailValues {
		if b.Precision < 0.995 {
			rule += fmt.Sprintf("  (%.0f%% of those matching)", 100*b.Precision)
		}
		inst.text(x, y, lensFit(rule, int((inst.w-x-lensPad)/lensAdvance)), lensFont, sec)
	}
	inst.y += 20
}

// paintRow draws one row: its label, its frame cells, then its own slots
// inline and, below the value detail, its missing slots.
func (inst *lensPainter) paintRow(pr *lwlens.PlanRow, frame []int32, rh float32) {
	a := inst.a
	row := &a.Model.Rows[pr.Row]
	cy := inst.y + rh/2
	labelChars := int((inst.labelW - 12) / lensAdvance)
	if inst.maxGroup > 1 {
		// How many rows the line stands for, as a bar under its label: the
		// lines of a band compare as the set sizes of an UpSet plot do.
		frac := float32(len(pr.Also)+1) / float32(inst.maxGroup)
		c.PaintRectFilled(lensPad, cy+4.5, lensPad+(inst.labelW-12)*frac, cy+6, 0,
			lensMix(styletokens.NeutralBgSurface, styletokens.NeutralTextSecondary, 0.45)).Send()
	}
	if len(pr.Also) > 0 {
		count := fmt.Sprintf("×%d", len(pr.Also)+1)
		inst.textRight(lensPad+inst.labelW-8, cy, count, lensSmallFont, lensTok(styletokens.NeutralTextSecondary))
		labelChars -= utf8.RuneCountInString(count) + 1
	}
	inst.text(lensPad, cy, lensFit(inst.rowLabel(pr.Row), labelChars), lensFont, lensTok(styletokens.NeutralTextPrimary))
	x := lensPad + inst.labelW
	missing := map[int32]bool{}
	for _, s := range pr.Missing {
		missing[s] = true
	}
	unexpected := map[int32]bool{}
	for _, s := range pr.Unexpected {
		unexpected[s] = true
	}
	if frame != nil {
		n := inst.frameFit(frame)
		xs := inst.frameXs(frame[:n])
		for i, s := range frame[:n] {
			cell, has := row.Cell(s)
			inst.paintFrameCell(xs[i], cy, s, cell, has, missing[s], unexpected[s])
		}
		if n > 0 {
			x = xs[n-1] + inst.cellW(frame[n-1])
		}
		hidden := 0
		for _, s := range frame[n:] {
			if _, has := row.Cell(s); has {
				hidden++
			}
		}
		if hidden > 0 {
			inst.text(x+2, cy, fmt.Sprintf("+%d", hidden), lensSmallFont, lensTok(styletokens.NeutralTextSecondary))
			x += 4 * lensAdvance
		}
		if len(pr.Own) > 0 {
			x += 6
			c.PaintLine(x, cy-rh/2+2, x, cy+rh/2-2, lensTok(styletokens.NeutralBorderFaint), 1).Send()
			x += 6
		}
	}
	x = inst.paintInline(x, cy, row, pr.Own, unexpected)
	if inst.p.Detail < lwlens.DetailValues {
		x = inst.paintDeviations(x, cy, pr)
	}
	if len(pr.Also) > 0 {
		inst.paintAlso(x, cy, pr)
	}
}

// rowLabel is a row's label less the prefix every label shares.
func (inst *lensPainter) rowLabel(r int32) string {
	l := inst.a.Model.Rows[r].Label
	if k := inst.labelPrefix[inst.a.BandOf[r]]; k > 0 {
		return "…" + string([]rune(l)[k:])
	}
	return l
}

// paintAlso names the rows a collapsed row stands for.
func (inst *lensPainter) paintAlso(x, cy float32, pr *lwlens.PlanRow) {
	x += lensSectionGap
	room := int((inst.w - lensPad - x) / (0.62 * lensSmallFont))
	if room < 8 {
		return
	}
	names := make([]string, 0, lensAlsoMax)
	for _, r := range pr.Also {
		lbl := inst.rowLabel(r)
		if lbl == inst.rowLabel(pr.Row) || slices.Contains(names, lbl) {
			continue
		}
		if len(names) == lensAlsoMax {
			names = append(names, "…")
			break
		}
		names = append(names, lbl)
	}
	if len(names) == 0 {
		return
	}
	inst.text(x, cy, lensFit("with "+strings.Join(names, ", "), room), lensSmallFont, lensTok(styletokens.NeutralTextSecondary))
}

// valueTone is the fingerprint colour of a cell: a category's chip colour,
// or nothing for a number or text.
func (inst *lensPainter) chipTone(s int32, cell *lwlens.Cell) (col color.Color, ok bool) {
	if inst.a.Model.Slots[s].Kind != lwlens.ValueKindCategorical {
		return
	}
	st := &inst.a.Stats[s]
	if len(st.Categories) < 2 {
		return lensTok(styletokens.NeutralTextSecondary), true
	}
	r := st.CategoryRank(cell.Text)
	if r < 0 || r >= styletokens.QualitativeCycleLen-1 {
		return lensTok(styletokens.NeutralTextDisabled), true
	}
	return lensTok(styletokens.QualitativeCycle(r)), true
}

// lensSparkW is the width of a sparkline drawn beside its summary.
const lensSparkW float32 = 48

// paintSpark draws a numeric array as a line over [x0, x1], scaled to its
// own range; a non-finite item breaks the line. It reports false when the
// array has fewer than two finite items and nothing was drawn.
func (inst *lensPainter) paintSpark(x0, x1, cy, hh float32, series []float64, col color.Color) bool {
	lo, hi, n := math.Inf(1), math.Inf(-1), 0
	for _, v := range series {
		if !math.IsNaN(v) && !math.IsInf(v, 0) {
			lo, hi, n = math.Min(lo, v), math.Max(hi, v), n+1
		}
	}
	if n < 2 {
		return false
	}
	span := hi - lo
	var xs, ys []float32
	flush := func() {
		if len(xs) >= 2 {
			c.PaintPolyline(xs, ys, col, 1.2).Send()
		}
		xs, ys = xs[:0], ys[:0]
	}
	for i, v := range series {
		if math.IsNaN(v) || math.IsInf(v, 0) {
			flush()
			continue
		}
		t := float32(0.5)
		if span > 0 {
			t = float32((v - lo) / span)
		}
		xs = append(xs, x0+(x1-x0)*float32(i)/float32(len(series)-1))
		ys = append(ys, cy+hh-2*hh*t)
	}
	flush()
	return true
}

// seriesSummary is an array's length and range, as text.
func seriesSummary(series []float64) string {
	lo, hi := math.Inf(1), math.Inf(-1)
	for _, v := range series {
		if !math.IsNaN(v) && !math.IsInf(v, 0) {
			lo, hi = math.Min(lo, v), math.Max(hi, v)
		}
	}
	if math.IsInf(lo, 1) {
		return fmt.Sprintf("%d×", len(series))
	}
	return fmt.Sprintf("%d× %s…%s", len(series), lensShortNum(lo), lensShortNum(hi))
}

// paintSeriesCell draws an array cell in [x0, x1]: its sparkline, and at the
// values detail its length and range beside it.
func (inst *lensPainter) paintSeriesCell(x0, x1, cy float32, cell *lwlens.Cell) bool {
	col := lensTok(styletokens.NeutralTextPrimary)
	if inst.p.Detail < lwlens.DetailValues {
		return inst.paintSpark(x0, x1, cy, 5, cell.Series, col)
	}
	sw := min(lensSparkW, (x1-x0)/2)
	if !inst.paintSpark(x0, x0+sw, cy, 5, cell.Series, col) {
		return false
	}
	room := int((x1 - x0 - sw - 6) / lensAdvance)
	inst.text(x0+sw+6, cy, lensFit(seriesSummary(cell.Series), room), lensFont, lensTok(styletokens.NeutralTextSecondary))
	return true
}

// paintGlyph draws the fingerprint of a cell in [x0, x1] around cy: a bar
// whose length is the value's rank for a number, a chip for a category, a
// neutral block for text.
func (inst *lensPainter) paintGlyph(x0, x1, cy, hh float32, s int32, cell *lwlens.Cell) {
	if len(cell.Series) >= 2 && inst.paintSpark(x0, x1, cy, hh, cell.Series, lensTok(styletokens.NeutralTextPrimary)) {
		return
	}
	if col, ok := inst.chipTone(s, cell); ok {
		c.PaintRectFilled(x0, cy-hh, x1, cy+hh, 2, col).Send()
		return
	}
	if inst.a.Model.Slots[s].Kind == lwlens.ValueKindNumeric && cell.HasNum {
		pct := float32(inst.a.Stats[s].Percentile(cell.Num))
		c.PaintRectFilled(x0, cy-hh, x1, cy+hh, 1, lensTok(styletokens.NeutralSubtle)).Send()
		c.PaintRectFilled(x0, cy-hh, x0+max(2, (x1-x0)*pct), cy+hh, 1, lensTok(styletokens.NeutralTextSecondary)).Send()
		return
	}
	c.PaintRectFilled(x0, cy-hh*0.5, x1, cy+hh*0.5, 1, lensTok(styletokens.NeutralBorderFaint)).Send()
}

// lensTimeRe matches a timestamp's text: a date, then a time.
var lensTimeRe = regexp.MustCompile(`^(\d{4})-(\d\d)-(\d\d)[T ](\d\d):(\d\d)`)

// lensShortTimeLen is the width of lensShortTime's form.
const lensShortTimeLen = 11

// lensShortTime writes a timestamp as month-day hour:minute, which is what
// tells timestamps of one batch apart; the year and the seconds rarely do.
func lensShortTime(t string) (short string, ok bool) {
	g := lensTimeRe.FindStringSubmatch(t)
	if g == nil {
		return "", false
	}
	return g[2] + "-" + g[3] + " " + g[4] + ":" + g[5], true
}

// lensExtremeAt is the value surprise over which a value is drawn in the
// accent tone: for a number, the outer 7.5% at either end of its slot.
const lensExtremeAt = 0.85

// valueTone is a value's text colour: the accent for an extreme one.
func (inst *lensPainter) valueTone(s int32, cell *lwlens.Cell) color.Color {
	if lwlens.ValueSurprise(inst.a, s, cell) >= lensExtremeAt && len(inst.a.Stats[s].Categories) > 2 {
		return lensTok(styletokens.AccentDefault)
	}
	return lensTok(styletokens.NeutralTextPrimary)
}

// valueText is a cell's text at the current detail, cut to n characters.
func (inst *lensPainter) valueText(s int32, cell *lwlens.Cell, n int) string {
	t := cell.Text
	if inst.a.Model.Slots[s].Kind == lwlens.ValueKindNumeric && cell.HasNum {
		t = inst.numText(s, cell.Num)
	} else if inst.p.Detail == lwlens.DetailGist {
		if ts, ok := lensShortTime(t); ok {
			t = ts
		} else {
			t = inst.a.Stats[s].Elide(t)
		}
	}
	if cell.Arity > 1 {
		t = fmt.Sprintf("%s ×%d", t, cell.Arity)
	}
	return lensFit(t, n)
}

// paintFrameCell draws one frame cell.
func (inst *lensPainter) paintFrameCell(x, cy float32, s int32, cell *lwlens.Cell, has, missing, unexpected bool) {
	w := inst.cellW(s)
	m := inst.a.Model
	markErr := lensTok(styletokens.ErrorDefault)
	markWarn := lensTok(styletokens.WarningDefault)
	switch inst.p.Detail {
	case lwlens.DetailShape:
		x0, x1 := x, x+lensShapeCell
		switch {
		case has:
			c.PaintRectFilled(x0, cy-lensShapeCell/2, x1, cy+lensShapeCell/2, 1, inst.sectionTone[m.SectionOf(s)]).Send()
			if unexpected {
				c.PaintRectStroke(x0-1, cy-lensShapeCell/2-1, x1+1, cy+lensShapeCell/2+1, 1, markWarn, 1.5).Send()
			}
		case missing:
			c.PaintRectStroke(x0+0.5, cy-lensShapeCell/2+0.5, x1-0.5, cy+lensShapeCell/2-0.5, 1, markErr, 1.5).Send()
		default:
			c.PaintRectFilled(x0+3, cy-1, x1-3, cy+1, 0, lensTok(styletokens.NeutralBorderFaint)).Send()
		}
	case lwlens.DetailFingerprint:
		x0, x1 := x, x+lensFingerCell
		switch {
		case has:
			inst.paintGlyph(x0, x1, cy, 4, s, cell)
			if unexpected {
				c.PaintRectStroke(x0-1, cy-6, x1+1, cy+6, 1, markWarn, 1.5).Send()
			}
		case missing:
			c.PaintRectStroke(x0+0.5, cy-4.5, x1-0.5, cy+4.5, 1, markErr, 1.5).Send()
		}
	default:
		n := inst.cellChars(s)
		if !has {
			if missing && inst.p.Detail == lwlens.DetailGist {
				c.PaintRectStroke(x+1, cy-6, x+w-6, cy+6, 1, markErr, 1.5).Send()
			}
			return
		}
		if len(cell.Series) >= 2 {
			if !inst.paintSeriesCell(x, x+w-8, cy, cell) {
				inst.text(x, cy, lensFit(fmt.Sprintf("%d× NaN", len(cell.Series)), n), lensSmallFont, lensTok(styletokens.NeutralTextSecondary))
			}
			return
		}
		txt := inst.valueText(s, cell, n)
		pri := inst.valueTone(s, cell)
		numeric := m.Slots[s].Kind == lwlens.ValueKindNumeric && cell.HasNum
		if inst.p.Detail == lwlens.DetailGist {
			// The standing, behind the text: a data bar for a number, a chip
			// for a label.
			if col, ok := inst.chipTone(s, cell); ok {
				c.PaintRectFilled(x, cy-5, x+3, cy+5, 1, col).Send()
				inst.text(x+6, cy, lensFit(txt, n-1), lensFont, pri)
			} else if numeric {
				pct := float32(inst.a.Stats[s].Percentile(cell.Num))
				c.PaintRectFilled(x, cy-7, x+max(2, (w-6)*pct), cy+7, 1,
					lensMix(styletokens.NeutralBgSurface, styletokens.NeutralTextSecondary, 0.28)).Send()
				inst.textRight(x+w-8, cy, txt, lensFont, pri)
			} else {
				inst.text(x, cy, txt, lensFont, pri)
			}
		} else if numeric {
			inst.textRight(x+w-8, cy, txt, lensFont, pri)
		} else {
			inst.text(x, cy, txt, lensFont, pri)
		}
		if unexpected && inst.p.Detail == lwlens.DetailGist {
			c.PaintRectStroke(x-2, cy-7, x+w-6, cy+8, 1, markWarn, 1.2).Send()
		}
	}
}

// paintInline draws slots outside any frame, each named: a section's run
// starts with its name in the section's tone, then its members. It returns
// where it stopped.
func (inst *lensPainter) paintInline(x, cy float32, row *lwlens.Row, slots []int32, unexpected map[int32]bool) float32 {
	m := inst.a.Model
	limit := inst.w - lensPad
	sec := lensTok(styletokens.NeutralTextSecondary)
	prev := ""
	for i, s := range slots {
		sl := m.Slots[s]
		cell, _ := row.Cell(s)
		var need float32
		name := inst.memberName(s)
		var val string
		switch inst.p.Detail {
		case lwlens.DetailShape:
			need = lensShapePitch
		case lwlens.DetailFingerprint:
			name = lensFit(name, 8)
			need = lensTextW(name, lensSmallFont) + 3 + lensFingerCell + 8
		default:
			name = lensFit(name, 14)
			lim := max(lensGistMax, inst.cellChars(s))
			if inst.p.Detail == lwlens.DetailValues {
				lim = lensValuesMax + 6
			}
			val = inst.valueText(s, cell, lim)
			if len(cell.Series) >= 2 {
				val = ""
				if inst.p.Detail == lwlens.DetailValues {
					val = seriesSummary(cell.Series)
				}
				need = lensTextW(name, lensSmallFont) + 4 + lensSparkW + 6 + lensTextW(val, lensFont) + 12
			} else {
				need = lensTextW(name, lensSmallFont) + 4 + lensTextW(val, lensFont) + 12
			}
		}
		if sl.Section != prev {
			need += lensTextW(sl.Section, lensSmallFont) + 6
			if prev != "" {
				need += lensSectionGap
			}
		}
		if x+need > limit {
			inst.text(x, cy, fmt.Sprintf("+%d", len(slots)-i), lensSmallFont, sec)
			return x + 4*lensAdvance
		}
		if sl.Section != prev {
			if prev != "" {
				x += lensSectionGap
			}
			// The name in neutral text over a rule in the section's tone: a
			// tone is picked to tell sections apart, not to be read on.
			tone := inst.sectionTone[m.SectionOf(s)]
			inst.text(x, cy, sl.Section, lensSmallFont, sec)
			c.PaintLine(x, cy+6, x+lensTextW(sl.Section, lensSmallFont), cy+6, tone, 1.5).Send()
			x += lensTextW(sl.Section, lensSmallFont) + 6
			prev = sl.Section
		}
		switch inst.p.Detail {
		case lwlens.DetailShape:
			c.PaintRectFilled(x, cy-lensShapeCell/2, x+lensShapeCell, cy+lensShapeCell/2, 1, inst.sectionTone[m.SectionOf(s)]).Send()
			if unexpected[s] {
				c.PaintRectStroke(x-1, cy-lensShapeCell/2-1, x+lensShapeCell+1, cy+lensShapeCell/2+1, 1, lensTok(styletokens.WarningDefault), 1.5).Send()
			}
			x += lensShapePitch
		case lwlens.DetailFingerprint:
			inst.text(x, cy, name, lensSmallFont, sec)
			x += lensTextW(name, lensSmallFont) + 3
			inst.paintGlyph(x, x+lensFingerCell, cy, 4, s, cell)
			if unexpected[s] {
				c.PaintRectStroke(x-1, cy-6, x+lensFingerCell+1, cy+6, 1, lensTok(styletokens.WarningDefault), 1.5).Send()
			}
			x += lensFingerCell + 8
		default:
			inst.text(x, cy, name, lensSmallFont, sec)
			x += lensTextW(name, lensSmallFont) + 4
			if len(cell.Series) >= 2 {
				inst.paintSpark(x, x+lensSparkW, cy, 5, cell.Series, lensTok(styletokens.NeutralTextPrimary))
				x += lensSparkW + 6
			} else if col, ok := inst.chipTone(s, cell); ok && inst.p.Detail == lwlens.DetailGist {
				c.PaintRectFilled(x, cy-5, x+3, cy+5, 1, col).Send()
				x += 6
			}
			inst.text(x, cy, val, lensFont, inst.valueTone(s, cell))
			if unexpected[s] && inst.p.Detail == lwlens.DetailGist {
				c.PaintRectStroke(x-2, cy-7, x+lensTextW(val, lensFont)+2, cy+7, 1, lensTok(styletokens.WarningDefault), 1.2).Send()
			}
			x += lensTextW(val, lensFont) + 12
		}
	}
	return x
}

// paintDeviations names a row's deviations from its band after the rest of
// the row: what it lacks, and what it unusually has where the cell drawing
// it does not already carry its name. It returns where it stopped.
func (inst *lensPainter) paintDeviations(x, cy float32, pr *lwlens.PlanRow) float32 {
	type dev struct {
		label string
		col   color.Color
	}
	var devs []dev
	for _, s := range pr.Missing {
		devs = append(devs, dev{"−" + inst.a.Model.Slots[s].Label(), lensTok(styletokens.ErrorDefault)})
	}
	for _, s := range pr.Unexpected {
		if inst.p.Detail != lwlens.DetailShape && slices.Contains(pr.Own, s) {
			continue // drawn inline, with its name
		}
		devs = append(devs, dev{"+" + inst.a.Model.Slots[s].Label(), lensTok(styletokens.WarningDefault)})
	}
	if len(devs) == 0 {
		return x
	}
	x += lensSectionGap
	for i, d := range devs {
		lbl := d.label
		if i == lensMissingMax && len(devs) > lensMissingMax+1 {
			lbl = fmt.Sprintf("%d more", len(devs)-i)
		}
		if x+lensTextW(lbl, lensSmallFont) > inst.w-lensPad {
			break
		}
		inst.text(x, cy, lbl, lensSmallFont, d.col)
		x += lensTextW(lbl, lensSmallFont) + 8
		if i == lensMissingMax {
			break
		}
	}
	return x
}
