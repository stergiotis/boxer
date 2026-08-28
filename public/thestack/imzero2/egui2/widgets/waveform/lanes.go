package waveform

import (
	"math"
	"time"

	"github.com/stergiotis/boxer/public/science/audio/track"
	c "github.com/stergiotis/boxer/public/thestack/imzero2/egui2/bindings"
	"github.com/stergiotis/boxer/public/thestack/imzero2/egui2/widgets/timeline"
	"github.com/stergiotis/boxer/public/thestack/imzero2/egui2/widgets/timeline/layout"
)

// LaneUnit is the axis unit of a [Lanes] timeline: a microsecond, so a
// boundary does not quantise visibly at sample-level zoom and twelve hours
// stay far inside int64 (ADR-0208 SD8, ADR-0043 SD17).
const LaneUnit = time.Microsecond

// Lanes is the annotation-lane timeline stacked under a [Player]: the
// timeline widget on an offset axis in [LaneUnit], its view locked to the
// player's every frame. Interval and point events are given in lane units —
// [FrameToLaneUnit] converts — and the timeline's own API (selection,
// brush, annotations, playhead) is reachable through [Lanes.Timeline].
//
// Lane hints widen the timeline's label column and shift its axis start, so
// the lanes would no longer share the player's x origin: leave
// [layout.IntervalEvent.LaneHint] empty for lanes drawn under a waveform.
type Lanes struct {
	tl *timeline.Timeline
	tb track.TimeBase
}

// NewLanes builds the timeline over intervals in lane units. opts are the
// timeline's own options; the offset axis and the locked view are always set.
func NewLanes(ids *c.WidgetIdStack, scopeKey string, tb track.TimeBase, intervals []*layout.IntervalEvent, opts ...timeline.Option) (inst *Lanes) {
	all := make([]timeline.Option, 0, len(opts)+2)
	all = append(all, timeline.WithOffsetAxis(LaneUnit), timeline.WithLockedView(true))
	all = append(all, opts...)
	return &Lanes{tl: timeline.New(ids, scopeKey, intervals, all...), tb: tb}
}

// Timeline is the underlying widget.
func (inst *Lanes) Timeline() (tl *timeline.Timeline) { return inst.tl }

// FrameToLaneUnit converts a frame index to the lanes' axis value.
func FrameToLaneUnit(tb track.TimeBase, frame int64) (v int64) {
	return int64(tb.FrameToDuration(frame) / LaneUnit)
}

// LaneUnitToFrame converts a lanes' axis value back to a frame index.
func LaneUnitToFrame(tb track.TimeBase, v int64) (frame int64) {
	return tb.DurationToFrame(time.Duration(v) * LaneUnit)
}

// Render locks the lanes to the player's view and playhead and draws them
// at the current position of the enclosing Ui — call it right after
// [Player.Render] so they sit under the waveform.
func (inst *Lanes) Render(p *Player) {
	v := p.View()
	w, _ := p.Size()
	from := FrameToLaneUnit(inst.tb, int64(math.Floor(v.FromFrame)))
	to := FrameToLaneUnit(inst.tb, int64(math.Ceil(v.ToFrame(w))))
	if to <= from {
		to = from + 1
	}
	inst.tl.SetRangeUnits(from, to)
	inst.tl.SetPlayhead(FrameToLaneUnit(inst.tb, p.Position()))
	inst.tl.Render()
}
