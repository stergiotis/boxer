package notebookcells

import (
	"strconv"
	"time"

	"github.com/apache/arrow-go/v18/arrow"

	"github.com/stergiotis/boxer/public/db/clickhouse/chrows"
	"github.com/stergiotis/boxer/public/keelson/designsystem/styletokens"
	"github.com/stergiotis/boxer/public/semistructured/markdown/mdnotebook"
	c "github.com/stergiotis/boxer/public/thestack/imzero2/egui2/bindings"
	"github.com/stergiotis/boxer/public/thestack/imzero2/egui2/widgets/color"
	"github.com/stergiotis/boxer/public/thestack/imzero2/egui2/widgets/notebookview"
	"github.com/stergiotis/boxer/public/thestack/imzero2/egui2/widgets/timeline"
	"github.com/stergiotis/boxer/public/thestack/imzero2/egui2/widgets/timeline/layout"
)

const (
	// RendererMarks draws a relation's rows on one time axis: a row with
	// [ColTime] is a numbered flag, one with [ColTimeEnd] as well is a bar.
	// The rows of one [ColLabel] share a number, a hue and a line of the
	// legend under the axis. The columns are the ones play's timeline
	// reads, so a record fits both.
	RendererMarks = "marks"
	ColTime       = "_tl_time"
	ColTimeEnd    = "_tl_time_end"
	ColLabel      = "_tl_label"
)

const (
	// maxMarks bounds what is drawn; the legend says how many were left out.
	maxMarks = 64
	// lonePad is the half-width of the axis around one instant.
	lonePad = 12 * time.Hour
)

func init() {
	notebookview.RegisterRenderer(RendererMarks, func(ids *c.WidgetIdStack) (notebookview.CellRenderer, func()) {
		m := &marks{ids: ids}
		return m.render, nil
	})
}

// markGroup is the marks of one label.
type markGroup struct {
	label    string
	n        int
	lo, hi   int64
	interval bool
}

type marks struct {
	ids *c.WidgetIdStack
	tl  *timeline.Timeline
	// seen is the relation the widget was last filled from.
	seen    arrow.RecordBatch
	groups  []markGroup
	dropped int
	reason  string
}

func (inst *marks) render(res *mdnotebook.Result) (reason string) {
	if res.Main != inst.seen {
		inst.fill(res.Main)
	}
	if inst.reason != "" {
		return inst.reason
	}
	inst.tl.Render()
	for i := range inst.groups {
		g := &inst.groups[i]
		for range c.IdScope(inst.ids.PrepareSeq(uint64(i))) {
			for range c.Horizontal().KeepIter() {
				for rt := range c.RichTextLabelColored(color.Hex(styletokens.QualitativeCycle(i).AsHex()), color.Transparent, "●") {
					rt.Small()
				}
				for rt := range c.RichTextLabel(strconv.Itoa(i + 1)) {
					rt.Small().Weak()
				}
				if g.label != "" {
					for rt := range c.RichTextLabel(g.label) {
						rt.Small()
					}
				}
				for rt := range c.RichTextLabel(g.summary()) {
					rt.Small().Weak()
				}
			}
		}
	}
	if inst.dropped > 0 {
		for rt := range c.RichTextLabel("+" + strconv.Itoa(inst.dropped) + " more not shown") {
			rt.Small().Weak()
		}
	}
	return ""
}

func stamp(ms int64) string { return time.UnixMilli(ms).UTC().Format("2006-01-02 15:04:05") }

func (inst *markGroup) summary() string {
	switch {
	case inst.n == 1 && !inst.interval:
		return stamp(inst.lo)
	case inst.n == 1:
		return stamp(inst.lo) + " … " + stamp(inst.hi)
	}
	return strconv.Itoa(inst.n) + " · " + stamp(inst.lo) + " … " + stamp(inst.hi)
}

// fill reads the relation into the timeline: its marks, its legend and the
// axis that holds them all.
func (inst *marks) fill(rec arrow.RecordBatch) {
	inst.seen = rec
	inst.groups, inst.dropped, inst.reason = inst.groups[:0], 0, ""
	timeCol, endCol, labelCol := -1, -1, -1
	for i, f := range rec.Schema().Fields() {
		switch f.Name {
		case ColTime:
			timeCol = i
		case ColTimeEnd:
			endCol = i
		case ColLabel:
			labelCol = i
		}
	}
	if timeCol < 0 {
		inst.reason = "marks reads a time column named " + ColTime
		return
	}
	var anns []*layout.Annotation
	var ivs []*layout.IntervalEvent
	groupOf := make(map[string]int)
	for row := 0; row < int(rec.NumRows()); row++ {
		from, ok := chrows.EpochMillis(rec.Column(timeCol), row)
		if !ok {
			continue
		}
		if len(anns)+len(ivs) == maxMarks {
			inst.dropped++
			continue
		}
		label := ""
		if labelCol >= 0 {
			label, _ = chrows.String(rec.Column(labelCol), row)
		}
		gi, known := groupOf[label]
		if !known {
			gi = len(inst.groups)
			groupOf[label] = gi
			inst.groups = append(inst.groups, markGroup{label: label, lo: from, hi: from})
		}
		g := &inst.groups[gi]
		g.n++
		to, isInterval := int64(0), false
		if endCol >= 0 {
			to, isInterval = chrows.EpochMillis(rec.Column(endCol), row)
			isInterval = isInterval && to > from
		}
		g.lo = min(g.lo, from)
		if isInterval {
			g.interval = true
			g.hi = max(g.hi, to)
			lane := label
			if lane == "" {
				lane = strconv.Itoa(gi + 1)
			}
			ivs = append(ivs, &layout.IntervalEvent{FromMS: from, ToMS: to, KindID: int32(gi % styletokens.QualitativeCycleLen), LaneHint: lane})
			continue
		}
		g.hi = max(g.hi, from)
		text := stamp(from)
		if label != "" {
			text = label + ": " + text
		}
		anns = append(anns, &layout.Annotation{TMS: from, Number: int32(gi + 1), PaletteIdx: int32(gi % styletokens.QualitativeCycleLen), Label: text})
	}
	if len(inst.groups) == 0 {
		inst.reason = "no row has a time in " + ColTime
		return
	}
	if inst.tl == nil {
		// Read-only: the report scrolls under the pointer, and the flags
		// stagger where they collide.
		palette := make([]color.Color, styletokens.QualitativeCycleLen)
		for i := range palette {
			palette[i] = color.Hex(styletokens.QualitativeCycle(i).AsHex())
		}
		inst.tl = timeline.New(inst.ids, "marks", nil, timeline.Options{NotInteractive: true, IntervalColors: palette})
	}
	inst.tl.SetAnnotations(anns)
	inst.tl.SetIntervals(ivs)
	lo, hi := inst.groups[0].lo, inst.groups[0].hi
	for _, g := range inst.groups[1:] {
		lo, hi = min(lo, g.lo), max(hi, g.hi)
	}
	pad := (hi - lo) / 10
	if pad <= 0 {
		pad = lonePad.Milliseconds()
	}
	inst.tl.SetRange(time.UnixMilli(lo-pad).UTC(), time.UnixMilli(hi+pad).UTC())
}
