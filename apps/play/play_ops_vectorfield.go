package play

import (
	"errors"
	"math"
	"strconv"
	"strings"
	"time"

	"github.com/stergiotis/boxer/public/keelson/runtime/app"
	"github.com/stergiotis/boxer/public/keelson/runtime/appops"
	"github.com/stergiotis/boxer/public/science/geo/vectorfield"
	"github.com/stergiotis/boxer/public/science/geo/vectorfield/sqlfield"
	"github.com/stergiotis/boxer/public/thestack/imzero2/egui2/widgets/portolan"
	"github.com/stergiotis/boxer/public/thestack/imzero2/egui2/widgets/portolan/flowoverlay"
)

// The Vector field pane as an agent reads and moves it (ADR-0270, update of
// 2026-10-05). The pane never reads the field's rows; its guest describes
// the field and reduces it on the server, a window and a per-step summary
// at a time, so the read gives what only the pane knows: the field's
// description, each step's mean and maximum speed inside the settled view,
// the window statistics, the errors of every phase, and on request the last
// window statement. set_vectorfield_view moves the camera and the display
// time; the vf_* signals that follow, once the view rests, carry the task
// as their writer. The display options (density, opacity, pause, basemap,
// sites) change no reading and stay the person's, as do the time strip's
// drag and the "Window query…" button, which opens a window outside
// open_window.

const (
	opGetVectorfield       = "get_vectorfield"
	opSetVectorfieldView   = "set_vectorfield_view"
	vectorfieldPaneId      = "vectorfield"
	opsResVectorfield      = vectorfieldPaneId
	vfReadDefaultSteps     = 48
	vfReadMaxSteps         = 500
	vfReadMaxSites         = 50
	vfReadMaxRuns          = 20
	vfReadMaxSqlBytes      = 8 << 10
	vfMinRate, vfMaxRate   = 0.25, 4.0
	vfViewFitPaddingPoints = 24
)

// VectorFieldBox is a lat/lon box in degrees; east may exceed 180 for a
// box across the antimeridian.
type VectorFieldBox struct {
	South float64 `desc:"southern edge, degrees"`
	North float64 `desc:"northern edge, degrees"`
	West  float64 `desc:"western edge, degrees"`
	East  float64 `desc:"eastern edge, degrees; above west, and above 180 across the antimeridian"`
}

// VectorFieldDescription is the field as its source described it.
type VectorFieldDescription struct {
	Name       string         `json:",omitzero" desc:"the name, vector_field_opts' when it gives one"`
	Quantity   string         `json:",omitzero" desc:"what the field is of"`
	Unit       string         `json:",omitzero" desc:"the unit of both components and of speed"`
	Provenance string         `json:",omitzero" desc:"what the source did to reach a lat/lon grid"`
	Bounds     VectorFieldBox `desc:"the first and last sample positions"`
	DLon       *float64       `json:",omitzero" desc:"native longitude spacing, degrees"`
	DLat       *float64       `json:",omitzero" desc:"native latitude spacing, degrees"`
	Global     bool           `json:",omitzero" desc:"the field wraps in longitude"`
	SpeedMin   *float64       `json:",omitzero" desc:"the low end of the speed range"`
	SpeedMax   *float64       `json:",omitzero" desc:"the high end of the speed range, vector_field_opts' speed_max when it gives one"`
	Steps      int32          `desc:"time steps"`
	FirstValid string         `json:",omitzero" desc:"the first step's valid time, UTC"`
	LastValid  string         `json:",omitzero" desc:"the last step's valid time, UTC"`
	Runs       []string       `json:",omitzero" desc:"the distinct model runs (reference times) the steps come from, at most 20"`
}

// VectorFieldStep is one step with what the pane has of it.
type VectorFieldStep struct {
	Index int32    `desc:"the step index set_vectorfield_view takes"`
	Valid string   `desc:"its valid time, UTC"`
	State string   `json:",omitzero" desc:"held, loading or missing: what the layer has of this step for the view"`
	Mean  *float64 `json:",omitzero" desc:"mean speed inside the settled view, weighted by area; absent until the summary lands, or where no node is valid"`
	Max   *float64 `json:",omitzero" desc:"largest speed among the nodes the summary read"`
	Nodes int64    `json:",omitzero" desc:"nodes the two were taken from"`
}

// VectorFieldSiteReading is one measurement site.
type VectorFieldSiteReading struct {
	Lat      float64  `desc:"latitude"`
	Lon      float64  `desc:"longitude"`
	Label    string   `json:",omitzero" desc:"its label"`
	RadiusKm *float64 `json:",omitzero" desc:"its reach, km"`
}

// VectorFieldWindow is the window on screen and the source's last request.
type VectorFieldWindow struct {
	Cols      int32  `json:",omitzero" desc:"columns of the window on screen"`
	Rows      int32  `json:",omitzero" desc:"rows of the window on screen"`
	Level     int32  `json:",omitzero" desc:"its pyramid level"`
	Requests  int64  `json:",omitzero" desc:"windows requested since the field was described"`
	InFlight  bool   `json:",omitzero" desc:"a window is on the way"`
	LastError string `json:",omitzero" desc:"the last window request's error"`
	LastRows  int64  `json:",omitzero" desc:"rows the last window statement returned"`
	LastMs    int64  `json:",omitzero" desc:"how long it took, milliseconds"`
}

// VectorFieldReading is get_vectorfield's result.
type VectorFieldReading struct {
	Drawn        PaneDraw                 `desc:"which draw this is of, its status line, and why it drew nothing when it did not"`
	ShapeError   string                   `json:",omitzero" desc:"the vector_field CTE's error when its shape was read"`
	FieldError   string                   `json:",omitzero" desc:"the error describing the field"`
	SummaryError string                   `json:",omitzero" desc:"the per-step summary's error"`
	Cancelled    bool                     `json:",omitzero" desc:"describing the field was cancelled; a run asks again"`
	Describing   bool                     `json:",omitzero" desc:"the field is being described"`
	Summarising  bool                     `json:",omitzero" desc:"the per-step summary is running"`
	Field        *VectorFieldDescription  `json:",omitzero" desc:"the field, once described"`
	Step         *int32                   `json:",omitzero" desc:"the step on display"`
	Valid        string                   `json:",omitzero" desc:"its valid time, UTC; vf_t carries it once the time rests"`
	Playing      bool                     `json:",omitzero" desc:"playback is running"`
	Rate         *float64                 `json:",omitzero" desc:"playback speed, steps per second"`
	View         *VectorFieldBox          `json:",omitzero" desc:"the settled view; the vf_min/max signals carry it and the summary is of it"`
	Window       VectorFieldWindow        `desc:"the window on screen and the last request"`
	Steps        []VectorFieldStep        `json:",omitzero" desc:"a page of the steps"`
	MoreSteps    int32                    `json:",omitzero" desc:"steps past the page"`
	Sites        int32                    `json:",omitzero" desc:"measurement sites from vector_field_sites; everything between them is interpolation"`
	SiteList     []VectorFieldSiteReading `json:",omitzero" desc:"the first 50 sites"`
	WindowSql    string                   `json:",omitzero" desc:"the last window statement as a buffer that runs on its own: its parameters as SETs, then the statement; adapt it with validate_sql or set_sql"`
	WindowSqlCut bool                     `json:",omitzero" desc:"the statement was cut at 8 KiB"`
}

// GetVectorFieldArgs is get_vectorfield's argument.
type GetVectorFieldArgs struct {
	Offset    int32 `json:",omitzero" desc:"steps to skip"`
	Limit     int32 `json:",omitzero" desc:"steps to list, 48 by default and at most 500"`
	WindowSql bool  `json:",omitzero" desc:"also give the last window statement"`
}

// SetVectorFieldViewArgs is set_vectorfield_view's argument.
type SetVectorFieldViewArgs struct {
	FitField bool            `json:",omitzero" desc:"frame the field's own bounds"`
	Bounds   *VectorFieldBox `json:",omitzero" desc:"or frame this box"`
	Step     *int32          `json:",omitzero" desc:"show this step"`
	Time     string          `json:",omitzero" desc:"or show the time nearest this UTC instant (2006-01-02 15:04:05, a date, or RFC 3339)"`
	Playing  *bool           `json:",omitzero" desc:"start or stop playback"`
	Rate     *float64        `json:",omitzero" desc:"playback speed in steps per second, 0.25 to 4"`
}

// vectorfieldOpsView is what get_vectorfield reads, copied on the render
// goroutine. The field's steps, the summary and the sites are replaced by
// a new field, a new summary or a new frame, never edited, so they are
// shared; the per-step states are copied.
type vectorfieldOpsView struct {
	has                     bool
	meta                    vectorfield.Meta
	states                  []flowoverlay.StepStateE
	summary                 []sqlfield.StepSummary
	stats                   flowoverlay.Stats
	probeErr, fieldErr      error
	summaryErr              error
	cancelled               bool
	describing, summarising bool
	pos                     float64
	playing                 bool
	rate                    float64
	settled                 vectorfield.Request
	hasSettled              bool
	sites                   []vectorFieldSite
	opts                    vectorFieldOpts
	src                     *sqlfield.Source
	windowSql               string
}

func (inst *PlayApp) vectorfieldView() (v vectorfieldOpsView) {
	d := inst.vectorFieldDriver
	g := d.guest
	v = vectorfieldOpsView{probeErr: d.probeErr, fieldErr: g.err, summaryErr: g.summaryErr, cancelled: g.cancelled,
		describing: g.Loading(), summarising: g.SummaryLoading(), pos: d.pos, playing: d.playing,
		rate: d.scrubber.Transport.Rate, settled: d.settledView, hasSettled: d.hasSettled, sites: d.sites,
		opts: d.lastOpts, src: g.src, summary: g.summary}
	v.meta, v.has = g.Meta()
	if v.has {
		v.stats = g.layer.Stats()
		v.states = make([]flowoverlay.StepStateE, len(v.meta.Steps))
		for i := range v.states {
			v.states[i] = g.StepState(i)
		}
	}
	if g.src != nil {
		v.windowSql = d.servedBuffer()
	}
	return
}

func vfTime(t time.Time) string {
	if t.IsZero() {
		return ""
	}
	return t.UTC().Format(vectorFieldTimeLayout)
}

func vfStepStateName(s flowoverlay.StepStateE) string {
	switch s {
	case flowoverlay.StepStateHeld:
		return "held"
	case flowoverlay.StepStateLoading:
		return "loading"
	case flowoverlay.StepStateMissing:
		return "missing"
	}
	return ""
}

func vfFinite32(v float32) *float64 { return finite(float64(v)) }

func vectorfieldReading(sn *opsSnap, in GetVectorFieldArgs) (out VectorFieldReading, err error) {
	d, readable, err := paneDrawOf(sn, vectorfieldPaneId)
	if err != nil {
		return
	}
	v := &sn.paneViews.vectorfield
	out = VectorFieldReading{Drawn: d, ShapeError: errText(v.probeErr), FieldError: errText(v.fieldErr),
		Cancelled: v.cancelled, Describing: v.describing, Summarising: v.summarising, Playing: v.playing}
	if v.summaryErr != nil {
		if errors.Is(v.summaryErr, errVectorFieldCancelled) {
			out.SummaryError = "cancelled; moving the view asks again"
		} else {
			out.SummaryError = errText(v.summaryErr)
		}
	}
	rate := v.rate
	if !(rate > 0) {
		rate = 0.5 // the scrubber's own default
	}
	out.Rate = finite(rate)
	if !readable {
		return
	}
	if limit := in.Limit; limit < 0 || limit > vfReadMaxSteps {
		err = app.RefuseOperation("limit is at most " + strconv.Itoa(vfReadMaxSteps))
		return
	}
	if in.Offset < 0 {
		err = app.RefuseOperation("offset is a count of steps to skip, 0 or more")
		return
	}
	st := v.stats
	out.Window = VectorFieldWindow{Cols: int32(st.WindowCols), Rows: int32(st.WindowRows), Level: int32(st.WindowLevel),
		Requests: int64(st.Fetches), InFlight: st.InFlight, LastError: errText(st.LastError)}
	if v.src != nil {
		if served, _ := v.src.LastServed(sqlfield.PurposeWindow); served.Err == nil && served.Took > 0 {
			out.Window.LastRows, out.Window.LastMs = int64(served.Rows), served.Took.Milliseconds()
		}
	}
	if v.hasSettled {
		s := v.settled
		out.View = &VectorFieldBox{South: s.South, North: s.North, West: s.West, East: s.East}
	}
	out.Sites = int32(len(v.sites))
	for _, s := range v.sites[:min(len(v.sites), vfReadMaxSites)] {
		r := VectorFieldSiteReading{Lat: s.ll.Lat, Lon: s.ll.Lng, Label: opsLabel(s.label)}
		if s.radiusKm > 0 {
			r.RadiusKm = finite(s.radiusKm)
		}
		out.SiteList = append(out.SiteList, r)
	}
	if in.WindowSql && v.windowSql != "" {
		out.WindowSql = truncateBytes(v.windowSql, vfReadMaxSqlBytes)
		out.WindowSqlCut = len(out.WindowSql) < len(v.windowSql)
	}
	if !v.has {
		return
	}
	m := &v.meta
	f := &VectorFieldDescription{Name: opsLabel(m.Name), Quantity: opsLabel(m.Quantity), Unit: opsLabel(m.Unit),
		Provenance: truncateBytes(m.Provenance, opsStatusMaxBytes),
		Bounds:     VectorFieldBox{South: m.South, North: m.North, West: m.West, East: m.East},
		DLon:       finite(m.DLon), DLat: finite(m.DLat), Global: m.PeriodicLon,
		SpeedMin: vfFinite32(m.SpeedMin), SpeedMax: vfFinite32(m.SpeedMax), Steps: int32(len(m.Steps))}
	if v.opts.name != "" {
		f.Name = opsLabel(v.opts.name)
	}
	if v.opts.unit != "" {
		f.Unit = opsLabel(v.opts.unit)
	}
	if v.opts.speedMax > 0 {
		f.SpeedMax = vfFinite32(v.opts.speedMax)
	}
	if n := len(m.Steps); n > 0 {
		f.FirstValid, f.LastValid = vfTime(m.Steps[0].Valid), vfTime(m.Steps[n-1].Valid)
		step := int32(min(max(int(math.Round(v.pos)), 0), n-1))
		out.Step, out.Valid = &step, vfTime(m.Steps[step].Valid)
	}
	for i := range m.Steps {
		ref := m.Steps[i].Reference
		if ref.IsZero() || (i > 0 && ref.Equal(m.Steps[i-1].Reference)) {
			continue
		}
		if len(f.Runs) == vfReadMaxRuns {
			break
		}
		f.Runs = append(f.Runs, vfTime(ref))
	}
	out.Field = f
	limit := int(in.Limit)
	if limit == 0 {
		limit = vfReadDefaultSteps
	}
	start := min(int(in.Offset), len(m.Steps))
	end := min(start+limit, len(m.Steps))
	for i := start; i < end; i++ {
		s := VectorFieldStep{Index: int32(i), Valid: vfTime(m.Steps[i].Valid)}
		if i < len(v.states) {
			s.State = vfStepStateName(v.states[i])
		}
		if i < len(v.summary) {
			s.Mean, s.Max, s.Nodes = vfFinite32(v.summary[i].Mean), vfFinite32(v.summary[i].Max), int64(v.summary[i].Valid)
		}
		out.Steps = append(out.Steps, s)
	}
	out.MoreSteps = int32(len(m.Steps) - end)
	return
}

// parseVfTime reads a UTC instant in the forms the pane and a model are
// likely to write.
func parseVfTime(raw string) (t time.Time, ok bool) {
	raw = strings.TrimSpace(raw)
	for _, layout := range []string{vectorFieldTimeLayout, "2006-01-02 15:04:05", "2006-01-02 15:04", "2006-01-02"} {
		if t, err := time.ParseInLocation(layout, raw, time.UTC); err == nil {
			return t, true
		}
	}
	if t, err := time.Parse(time.RFC3339Nano, raw); err == nil {
		return t.UTC(), true
	}
	return time.Time{}, false
}

// setView is set_vectorfield_view on the driver: everything is checked
// before anything moves. follow says the vf_* writes that result belong
// to the caller's writer.
func (inst *VectorFieldDriver) setView(in SetVectorFieldViewArgs, writer string, follow bool) (err error) {
	fit := in.FitField || in.Bounds != nil
	timed := in.Step != nil || in.Time != ""
	if !fit && !timed && in.Playing == nil && in.Rate == nil {
		return app.RefuseOperation("name what to move: fit_field or bounds, step or time, playing, rate")
	}
	if in.FitField && in.Bounds != nil {
		return app.RefuseOperation("fit_field frames the field and bounds a box; give one of them")
	}
	if in.Step != nil && in.Time != "" {
		return app.RefuseOperation("step and time both name the step to show; give one of them")
	}
	meta, has := inst.guest.Meta()
	if fit && (inst.pm == nil || !inst.pm.View().Loaded()) {
		return app.RefuseOperation("the map has not laid out yet: show_pane vectorfield, and call again once it has drawn")
	}
	if (in.FitField || timed || in.Playing != nil) && !has {
		return app.RefuseOperation("the field has not been described yet; get_vectorfield says why")
	}
	n := len(meta.Steps)
	var box VectorFieldBox
	switch {
	case in.FitField:
		box = VectorFieldBox{South: meta.South, North: meta.North, West: foldLon(meta.West)}
		box.East = box.West + (meta.East - meta.West)
	case in.Bounds != nil:
		box = *in.Bounds
		if !(box.South < box.North) || box.South < -90 || box.North > 90 || !(box.West < box.East) ||
			box.East-box.West > 360 || math.IsNaN(box.West) {
			return app.RefuseOperation("bounds is south < north within ±90, and west < east no more than 360 apart")
		}
	}
	if in.Rate != nil && !(*in.Rate >= vfMinRate && *in.Rate <= vfMaxRate) {
		return app.RefuseOperation("rate is 0.25 to 4 steps per second")
	}
	pos := inst.pos
	var at time.Time
	switch {
	case in.Step != nil:
		if *in.Step < 0 || int(*in.Step) >= n {
			return app.RefuseOperation("step is 0 to " + strconv.Itoa(n-1))
		}
		pos = float64(*in.Step)
	case in.Time != "":
		var ok bool
		if at, ok = parseVfTime(in.Time); !ok {
			return app.RefuseOperation("time " + strconv.Quote(in.Time) + " is not a UTC instant: 2006-01-02 15:04:05, a date, or RFC 3339")
		}
	}

	now := inst.now()
	if !at.IsZero() {
		pos = inst.guest.SetTime(at)
	}
	if fit {
		_ = inst.pm.View().FitBounds(
			portolan.LatLngBoundsOf(portolan.LL(box.South, box.West), portolan.LL(box.North, box.East)),
			portolan.FitOptions{Padding: portolan.Point{X: vfViewFitPaddingPoints, Y: vfViewFitPaddingPoints}})
		inst.viewStableAt = now
		inst.followView = follow
	}
	if timed {
		inst.scrubber.Transport.Seek(pos, n)
		inst.pos = inst.scrubber.Transport.Pos
		inst.guest.SetStepPosition(inst.pos)
		inst.posChangedAt = now
		inst.followTime = follow
	}
	if in.Playing != nil {
		inst.scrubber.Transport.Playing = *in.Playing
		inst.playing = *in.Playing
		inst.followTime = inst.followTime || (follow && *in.Playing)
	}
	if in.Rate != nil {
		inst.scrubber.Transport.Rate = *in.Rate
	}
	if follow && (inst.followTime || inst.followView) {
		inst.followWriter = writer
	}
	return nil
}

// setVectorfieldView is set_vectorfield_view: a task's camera or time move
// makes the follow-on vf_* writes the task's.
func (inst *PlayApp) setVectorfieldView(in SetVectorFieldViewArgs, writer string) error {
	return inst.vectorFieldDriver.setView(in, writer, writer != vectorfieldPaneId && writer != signalWriterApp)
}

// vectorfieldViewDigest is the vectorfield resource: the settled view, the
// step on display and playback. The animated camera between rests is left
// out, so a pan in progress invalidates nothing.
func vectorfieldViewDigest(p *PlayApp) string {
	d := p.vectorFieldDriver
	if d == nil {
		return ""
	}
	s := d.settledView
	return strconv.FormatFloat(s.South, 'g', 8, 64) + "," + strconv.FormatFloat(s.North, 'g', 8, 64) + "," +
		strconv.FormatFloat(s.West, 'g', 8, 64) + "," + strconv.FormatFloat(s.East, 'g', 8, 64) +
		"|step=" + strconv.Itoa(int(math.Round(d.pos))) + "|play=" + strconv.FormatBool(d.scrubber.Transport.Playing) +
		"|rate=" + strconv.FormatFloat(d.scrubber.Transport.Rate, 'g', -1, 64)
}

func addVectorfieldOps(s *appops.Set[*PlayLauncher, opsSnap]) {
	s.Resource(opsResVectorfield, "the Vector field pane's view: the settled camera, the step on display and playback",
		func(inst *PlayLauncher) any {
			if inst.inner == nil {
				return ""
			}
			return vectorfieldViewDigest(inst.inner)
		})
	appops.Query(s, app.OperationSpec{Name: opGetVectorfield, Version: 1,
		Summary: "read what the Vector field pane last drew: the field's description, the step on display, " +
			"each step's mean and maximum speed inside the settled view, the window and the errors of every phase, " +
			"the sites, and on request the last window statement",
		Reads: []string{opsResVectorfield, opsResResult, opsResPanes}, Agents: true, Untrusted: true},
		func(sn opsSnap, in GetVectorFieldArgs) (VectorFieldReading, error) {
			return vectorfieldReading(&sn, in)
		})
	appops.Command(s, app.OperationSpec{Name: opSetVectorfieldView, Version: 1,
		Summary: "move the Vector field pane: frame the field or a lat/lon box, show a step or a time, start or stop playback, set its speed",
		Effect:  app.OperationEffectView, Writes: []string{opsResVectorfield}, Agents: true,
		Follows: []string{"once the view rests the pane fetches a window and a per-step summary for it, and writes the vf_min/max signals",
			"once the time rests the pane writes vf_t; both carry the task as writer",
			"Live reruns a query that reads them"}},
		func(inst *PlayLauncher, call app.OperationCall, in SetVectorFieldViewArgs) (appops.None, error) {
			p := inst.inner
			if p == nil {
				return appops.None{}, app.RefuseOperation("the window has not mounted")
			}
			if err := p.setVectorfieldView(in, paneSignalWriter(call, vectorfieldPaneId)); err != nil {
				return appops.None{}, err
			}
			p.markAgent(call.OnBehalfOf)
			return appops.None{}, nil
		})
}
