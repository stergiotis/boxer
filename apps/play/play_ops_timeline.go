package play

import (
	"fmt"
	"strconv"
	"strings"
	"time"

	"github.com/dustin/go-humanize"
	"github.com/stergiotis/boxer/public/keelson/runtime/app"
	"github.com/stergiotis/boxer/public/keelson/runtime/appops"
	"github.com/stergiotis/boxer/public/keelson/runtime/appops/opwire"
	"github.com/stergiotis/boxer/public/thestack/imzero2/egui2/widgets/timeline"
)

// The Timeline pane as an agent reads and sets it (ADR-0270, update of
// 2026-10-05): the mode it resolved, its events, lanes and skipped rows,
// the extent, the brushed window, the view and the bands. The brushed
// window is tl_from/tl_to, which the pane publishes every frame it draws,
// so set_signal cannot hold it; set_timeline_window moves the brush and the
// two signals together, and the person's brush goes through it.

const (
	opGetTimeline          = "get_timeline"
	opSetTimelineOptions   = "set_timeline_options"
	opSetTimelineWindow    = "set_timeline_window"
	opsResTimeline         = timelinePaneId
	timelinePaneId         = "timeline"
	timelineMaxLaneReads   = 50
	timelineMaxBandReads   = 100
	timelineWindowSpelling = "UTC text such as 2026-10-05 14:00:00.000"
)

// TimelineLaneReading is one lane an interval timeline's `_tl_lane` names.
type TimelineLaneReading struct {
	Lane   string `desc:"the lane's value, cut at 64 bytes; empty for intervals with no lane"`
	Events int32  `desc:"intervals drawn in it"`
}

// TimelineRange is a span of time as the signals spell it.
type TimelineRange struct {
	From string `desc:"UTC, as DateTime64(3) text"`
	To   string `desc:"UTC, as DateTime64(3) text"`
}

// TimelineBand is one shaded range of the bands overlay.
type TimelineBand struct {
	From  string `desc:"UTC"`
	To    string `desc:"UTC"`
	Label string `json:",omitzero" desc:"the band's label, cut at 64 bytes"`
}

// TimelineBandsReading is the bands overlay: a second query the person
// writes in the pane, run on its own lane.
type TimelineBandsReading struct {
	SqlSet  bool           `desc:"whether the pane holds a bands query"`
	Status  string         `desc:"the bands line as the pane shows it: count, skipped, pending or the error"`
	Count   int32          `json:",omitzero" desc:"bands drawn"`
	Skipped int32          `json:",omitzero" desc:"bands rows skipped (unknown colour or to before from)"`
	Bands   []TimelineBand `json:",omitzero" desc:"the bands, at most 100"`
}

// TimelineReading is get_timeline's result.
type TimelineReading struct {
	Drawn       PaneDraw              `desc:"which draw this is of, its status line, and why it drew nothing when it did not"`
	Mode        string                `json:",omitzero" desc:"points (_tl_time), intervals (_tl_time and _tl_time_end) or annotations (_tl_time and _tl_label)"`
	Events      int64                 `desc:"events drawn"`
	Skipped     int64                 `json:",omitzero" desc:"rows not drawn: a required slot was null, or an interval ended before it began"`
	Lanes       []TimelineLaneReading `json:",omitzero" desc:"for intervals with a _tl_lane, the events per lane in the order the lanes first appear, at most 50"`
	LanesHidden int32                 `json:",omitzero" desc:"lanes past the 50 listed"`
	Intensity   bool                  `json:",omitzero" desc:"whether _tl_intensity colours the events"`
	Extent      *TimelineRange        `json:",omitzero" desc:"the first and last instant of the events: what tl_min and tl_max carry"`
	Window      *TimelineRange        `json:",omitzero" desc:"the brushed window, what tl_from and tl_to carry; absent when nothing is brushed and they span every instant"`
	View        *TimelineRange        `json:",omitzero" desc:"the time range the person is looking at; the wheel and drag move it"`
	SelectedRow *int64                `json:",omitzero" desc:"the row of the clicked interval or annotation"`
	NowLine     bool                  `desc:"whether the pane draws a line at the current time"`
	Bands       TimelineBandsReading  `desc:"the bands overlay"`
}

// SetTimelineOptionsArgs is set_timeline_options' argument.
type SetTimelineOptionsArgs struct {
	NowLine *bool `json:",omitzero" desc:"draw a line at the current time"`
}

// SetTimelineWindowArgs is set_timeline_window's argument.
type SetTimelineWindowArgs struct {
	From  string `json:",omitzero" desc:"the window's first instant, as UTC text such as 2026-10-05 14:00:00.000 (a date alone, or RFC 3339, also reads)"`
	To    string `json:",omitzero" desc:"the window's last instant, after from"`
	Clear bool   `json:",omitzero" desc:"drop the window: tl_from and tl_to then span every instant"`
}

// timelineOpsView is what get_timeline reads, copied on the render
// goroutine; the lane tally is shared, built once per rebuild.
type timelineOpsView struct {
	mode        timelineMode
	events      int
	skipped     int
	lanes       []TimelineLaneReading
	lanesHidden int
	intensity   bool
	extent      *TimelineRange
	window      *TimelineRange
	view        *TimelineRange
	selected    *int64
	nowLine     bool
	bands       TimelineBandsReading
}

func timelineModeName(m timelineMode) string {
	switch m {
	case timelineModePoints:
		return "points"
	case timelineModeIntervals:
		return "intervals"
	case timelineModeAnnotations:
		return "annotations"
	}
	return ""
}

func timelineRangeOf(fromMS, toMS int64) *TimelineRange {
	return &TimelineRange{From: formatExtentParam(fromMS), To: formatExtentParam(toMS)}
}

// opsView copies what get_timeline reads. Render-goroutine state: the
// widget's brush, view and selection are its own.
func (inst *TimelineDriver) opsView() (v timelineOpsView) {
	v = timelineOpsView{mode: inst.seenContract.Mode, events: inst.eventsN, skipped: inst.eventsSkipped,
		lanes: inst.laneTally, lanesHidden: inst.lanesHidden, intensity: inst.seenContract.ColIntensity >= 0}
	if inst.seenRec == nil {
		v.mode = timelineModeNone
	}
	if inst.nowLinePtr != nil {
		v.nowLine = *inst.nowLinePtr
	}
	if inst.dataExtentValid {
		v.extent = timelineRangeOf(inst.dataMinMS, inst.dataMaxMS)
	}
	if r, ok := inst.tl.Brush(); ok {
		v.window = timelineRangeOf(r.FromMS, r.ToMS)
	}
	if from, to, ok := inst.tl.ViewRange(); ok {
		v.view = &TimelineRange{From: from.UTC().Format(timelineTimeLayout), To: to.UTC().Format(timelineTimeLayout)}
	}
	if row, ok := timelineSelectedRow(inst.tl.Selection()); ok {
		v.selected = &row
	}
	b := &v.bands
	b.SqlSet = inst.bandsSQLPtr != nil && strings.TrimSpace(*inst.bandsSQLPtr) != ""
	b.Status = truncateBytes(inst.bandsStatusLine(), opsStatusMaxBytes)
	b.Count, b.Skipped = int32(len(inst.bands)), int32(inst.bandsSkipped)
	n := min(len(inst.bands), timelineMaxBandReads)
	if n > 0 {
		b.Bands = make([]TimelineBand, 0, n)
		for _, band := range inst.bands[:n] {
			b.Bands = append(b.Bands, TimelineBand{From: formatExtentParam(band.FromMS), To: formatExtentParam(band.ToMS),
				Label: opsLabel(band.Label)})
		}
	}
	return
}

// timelineTimeLayout is the DateTime64(3) text the signals carry.
const timelineTimeLayout = "2006-01-02 15:04:05.000"

// timelineSelectedRow is the row a click selection names; a rug bucket or
// a lane names none.
func timelineSelectedRow(sel timeline.SelectionInfo) (row int64, ok bool) {
	switch sel.Kind {
	case timeline.SelectionInterval:
		if sel.Interval != nil {
			return int64(sel.Interval.KindID), true
		}
	case timeline.SelectionAnnotation:
		if sel.Annotation != nil {
			return int64(sel.Annotation.Number), true
		}
	}
	return
}

// tallyLanes counts the intervals per lane, in first-seen order, after a
// rebuild. Only intervals carry a lane.
func (inst *TimelineDriver) tallyLanes(ct timelineContract, ivsLanes func(yield func(string) bool)) {
	inst.laneTally, inst.lanesHidden = nil, 0
	if ct.Mode != timelineModeIntervals || ct.ColLane < 0 {
		return
	}
	at := map[string]int{}
	var tally []TimelineLaneReading
	for lane := range ivsLanes {
		i, seen := at[lane]
		if !seen {
			i = len(tally)
			at[lane] = i
			tally = append(tally, TimelineLaneReading{Lane: opsLabel(lane)})
		}
		tally[i].Events++
	}
	if len(tally) > timelineMaxLaneReads {
		inst.lanesHidden = len(tally) - timelineMaxLaneReads
		tally = tally[:timelineMaxLaneReads]
	}
	inst.laneTally = tally
}

// paneStatus is the Timeline's last draw: its events, skipped rows and
// bands, or why it drew none.
func (inst *TimelineDriver) paneStatus() (line string, reject string) {
	if inst.seenRec == nil {
		return
	}
	if inst.eventsN == 0 {
		if inst.eventsSkipped > 0 {
			reject = "Every row was skipped: a required slot was null, or an interval ended before it began."
		} else {
			reject = "The query returned no rows, so there is nothing on the timeline."
		}
		return
	}
	var b strings.Builder
	fmt.Fprintf(&b, "%s %s", humanize.Comma(int64(inst.eventsN)), timelineModeName(inst.seenContract.Mode))
	if inst.eventsSkipped > 0 {
		fmt.Fprintf(&b, " · %s row(s) skipped (null or end<start)", humanize.Comma(int64(inst.eventsSkipped)))
	}
	if n := inst.tl.LaneCount(); n > 0 {
		fmt.Fprintf(&b, " · %d lane(s)", n)
	}
	if inst.bandsSQLPtr != nil && strings.TrimSpace(*inst.bandsSQLPtr) != "" {
		b.WriteString(" · " + inst.bandsStatusLine())
	}
	line = b.String()
	return
}

// timelineView is the Timeline's view for the snapshot.
func (inst *PlayApp) timelineView() timelineOpsView { return inst.timeline.opsView() }

// timelineReading is get_timeline.
func timelineReading(sn *opsSnap, _ appops.None) (out TimelineReading, err error) {
	d, readable, err := paneDrawOf(sn, timelinePaneId)
	if err != nil {
		return
	}
	v := sn.paneViews.timeline
	out = TimelineReading{Drawn: d, NowLine: v.nowLine, Window: v.window, Bands: v.bands}
	if !readable {
		return
	}
	out.Mode, out.Events, out.Skipped = timelineModeName(v.mode), int64(v.events), int64(v.skipped)
	out.Lanes, out.LanesHidden, out.Intensity = v.lanes, int32(v.lanesHidden), v.intensity
	out.Extent, out.View, out.SelectedRow = v.extent, v.view, v.selected
	return
}

// timelineTimeLayouts are the spellings set_timeline_window reads; a value
// without a zone is UTC, as the signals are.
var timelineTimeLayouts = []string{timelineTimeLayout, "2006-01-02 15:04:05", "2006-01-02T15:04:05", "2006-01-02 15:04", "2006-01-02"}

// parseTimelineInstant reads one bound of a window as epoch milliseconds.
func parseTimelineInstant(s string) (ms int64, ok bool) {
	s = strings.TrimSpace(s)
	if t, err := time.Parse(time.RFC3339Nano, s); err == nil {
		return t.UnixMilli(), true
	}
	for _, layout := range timelineTimeLayouts {
		if t, err := time.ParseInLocation(layout, s, time.UTC); err == nil {
			return t.UnixMilli(), true
		}
	}
	return
}

// windowOf checks set_timeline_window's argument: a clear, or a window
// whose bounds read and are in order.
func windowOf(in SetTimelineWindowArgs) (fromMS, toMS int64, clear bool, err error) {
	from, to := strings.TrimSpace(in.From), strings.TrimSpace(in.To)
	if in.Clear {
		if from != "" || to != "" {
			err = app.RefuseOperation("clear drops the window; give it alone, or give from and to without it")
		}
		return 0, 0, true, err
	}
	if from == "" || to == "" {
		err = app.RefuseOperation("a window needs both from and to, as " + timelineWindowSpelling + "; clear drops it")
		return
	}
	var ok bool
	if fromMS, ok = parseTimelineInstant(from); !ok {
		err = app.RefuseOperation("from " + strconv.Quote(from) + " is not a time; write it as " + timelineWindowSpelling)
		return
	}
	if toMS, ok = parseTimelineInstant(to); !ok {
		err = app.RefuseOperation("to " + strconv.Quote(to) + " is not a time; write it as " + timelineWindowSpelling)
		return
	}
	if fromMS >= toMS {
		err = app.RefuseOperation("from must come before to, to the millisecond")
	}
	return
}

// applyWindow moves the widget's brush; the caller writes the signals.
func (inst *TimelineDriver) applyWindow(in SetTimelineWindowArgs) (err error) {
	fromMS, toMS, clear, err := windowOf(in)
	if err != nil {
		return
	}
	if clear {
		inst.tl.ClearBrush()
		return
	}
	inst.tl.SetBrush(fromMS, toMS)
	return
}

// requestWindow is the person's brush: through set_timeline_window when
// play's launcher routes it, directly otherwise (the pane's publishWindow
// then writes the signals as it always did).
func (inst *TimelineDriver) requestWindow(in SetTimelineWindowArgs) {
	if inst.onWindow != nil {
		inst.onWindow(in)
		return
	}
	_ = inst.applyWindow(in)
}

// brushWindowArgs is a finished brush gesture as set_timeline_window's
// argument.
func brushWindowArgs(r timeline.BrushRange, ok bool) SetTimelineWindowArgs {
	if !ok {
		return SetTimelineWindowArgs{Clear: true}
	}
	return SetTimelineWindowArgs{From: formatExtentParam(r.FromMS), To: formatExtentParam(r.ToMS)}
}

// setTimelineWindow is set_timeline_window: the brush and tl_from/tl_to
// together, so the window holds whether or not the pane draws this frame,
// and the pane's own publish the same frame finds the values in place.
func (inst *PlayApp) setTimelineWindow(in SetTimelineWindowArgs, writer string) (err error) {
	if err = inst.timeline.applyWindow(in); err != nil {
		return
	}
	from, to := timelineWindowFloor, timelineWindowCeil
	if r, ok := inst.timeline.tl.Brush(); ok {
		from, to = formatExtentParam(r.FromMS), formatExtentParam(r.ToMS)
	}
	inst.graph.setSignalRawFrom(signalTimelineFrom, from, writer)
	inst.graph.setSignalRawFrom(signalTimelineTo, to, writer)
	return
}

// paneSignalWriter is the signal writer of a pane's command: the pane, as
// its in-frame gesture always wrote, when the person made it; the task
// otherwise.
func paneSignalWriter(call app.OperationCall, pane string) string {
	switch call.Writer {
	case opwire.WriterPerson:
		return pane
	case "":
		return signalWriterApp
	}
	return call.Writer
}

// timelineOptionsDigest is the timeline resource: the now line and the
// brushed window.
func timelineOptionsDigest(p *PlayApp) string {
	d := p.timeline
	if d == nil {
		return ""
	}
	var b strings.Builder
	if d.nowLinePtr != nil {
		b.WriteString("now=" + strconv.FormatBool(*d.nowLinePtr))
	}
	if r, ok := d.tl.Brush(); ok {
		fmt.Fprintf(&b, "|brush=%d-%d", r.FromMS, r.ToMS)
	}
	return b.String()
}

func addTimelineOps(s *appops.Set[*PlayLauncher, opsSnap]) {
	addPaneOps(s, paneOpsSpec[TimelineReading, appops.None, SetTimelineOptionsArgs]{
		pane:     timelinePaneId,
		resource: "the Timeline pane's settings: the now line and the brushed window",
		digest:   timelineOptionsDigest,
		get:      opGetTimeline,
		getSummary: "read what the Timeline pane last drew: its mode, events and skipped rows, events per lane, " +
			"the extent, the brushed window, the view, the selected row and the bands overlay",
		set:        opSetTimelineOptions,
		setSummary: "set the Timeline pane's now line",
		gesture:    "the Now line box above the timeline",
		follows:    []string{"the pane draws with it from its next frame; the result is not rerun"},
		read:       timelineReading,
		apply: func(p *PlayApp, in SetTimelineOptionsArgs) error {
			if in.NowLine == nil {
				return noOptionsRefusal(timelinePaneId, "now_line")
			}
			if p.timeline.nowLinePtr != nil {
				*p.timeline.nowLinePtr = *in.NowLine
			}
			return nil
		},
	})
	appops.Command(s, app.OperationSpec{Name: opSetTimelineWindow, Version: 1,
		Summary: "set or clear the Timeline's brushed window, which tl_from and tl_to carry to any query that filters on them",
		Effect:  app.OperationEffectDocument, Writes: []string{opsResSignals, opsResTimeline}, Agents: true,
		Gesture: "brushing the strip under the timeline's axis",
		Follows: []string{"Live reruns a query that reads tl_from or tl_to",
			"the pane shows the brush from its next frame; with nothing brushed the two signals span every instant"}},
		func(inst *PlayLauncher, call app.OperationCall, in SetTimelineWindowArgs) (appops.None, error) {
			p := inst.inner
			if p == nil {
				return appops.None{}, app.RefuseOperation("the window has not mounted")
			}
			if err := p.setTimelineWindow(in, paneSignalWriter(call, timelinePaneId)); err != nil {
				return appops.None{}, err
			}
			p.markAgent(call.OnBehalfOf)
			return appops.None{}, nil
		})
}
