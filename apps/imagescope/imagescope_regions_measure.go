package imagescope

// Measuring regions and profiling lines (ADR-0399 §SD7, §SD8, §M2): each
// runs as a keelson task over the samples of the image shown at full
// resolution, never on the frame; a result names the method, the coverage
// rule of each figure, how the region reached the image's pixels and the
// image and selection it was computed on, and is marked stale once any of
// those changes.

import (
	"context"
	"fmt"
	"math"
	"slices"
	"strconv"

	"github.com/stergiotis/boxer/public/keelson/runtime/app"
	"github.com/stergiotis/boxer/public/keelson/runtime/bgjob"
	"github.com/stergiotis/boxer/public/science/imaging/regions"
	"github.com/stergiotis/boxer/public/science/imaging/tiles"
)

const (
	regionMeasureKind  = "imagescope.region-measure"
	regionMeasureTitle = "measure regions"
	profileKind        = "imagescope.profile"
	profileTitle       = "profile a line"

	// profilePageMax bounds a page of profile_region.
	profilePageMax = 1024
	// regionsSummaryMax bounds the regions measure_regions lists; the
	// regions it measures are bounded by those the window holds.
	regionsSummaryMax = 32
)

// methodRegions states what a region's figures are, where they are shown
// (keelson-app F3).
const methodRegions = "Computed from the stored samples of the image shown at full resolution, never from what is drawn, and stated in values. " +
	"A point covers the pixel that contains it; a line the pixels it passes through; a rectangle, an ellipse or a polygon the pixels whose centre lies inside it — a centre exactly on the outline is inside on a left or top edge and outside on a right or bottom one, so two regions sharing an edge count each pixel along it once. " +
	"Invalid samples are counted apart and left out of every other figure. The standard deviation has divisor n; percentiles are nearest rank. " +
	"Asked for, the area figures weight every pixel the shape touches by the exact fraction of its area inside the shape: a sum, a mean and the covered area, which for a small shape can differ from the figures by centre by several percent. Percentiles and extremes are by centre only."

// methodProfile states what a line's profile is.
const methodProfile = "The pixels the line passes through, in order from its start, each once: for each, the length of the line inside it and the distance along the line to the middle of that length, and every plane's stored sample there. Nothing is interpolated and no spacing is chosen, so the points are unevenly spaced. A part of the line along a pixel edge belongs to the pixel below or right of it; a line through a corner passes between the two diagonal pixels."

// regionMeasure is a region's figures over one image, with what they were
// computed on. It is never modified once made.
type regionMeasure struct {
	regionId  string
	key       string
	m         regions.Measurement
	err       error
	area      bool
	imageId   string
	selection *SelectionInfo
}

// regionKey names what a region's figures depend on: the region and its
// revision, the store measured — a stack's selection is a store of its own —
// and whether the area figures were asked for. It reads no sample.
func regionKey(e regionEntry, pic *tiles.Store, area bool) string {
	return fmt.Sprintf("%s|%d|%p|%t", e.r.Id, e.rev, pic, area)
}

// measureBatch is what one measurement task leaves: a result per region.
type measureBatch struct {
	results []*regionMeasure
}

// measureTarget is one region to measure, decided on the render goroutine.
type measureTarget struct {
	e   regionEntry
	key string
}

// startRegionMeasure measures regions over the image shown as one task:
// the regions of ids, or every region drawn on it for none. A region that
// is not drawn on the image shown is refused when named, naming why, and
// left out when not.
func (inst *App) startRegionMeasure(ids []string, area bool) error {
	if inst.pic == nil {
		return errNoImage
	}
	if inst.regionJob.Running() {
		return app.RefuseOperation("a measurement of regions is under way; read get_view until RegionMeasuring is false")
	}
	pic, stk, imageId := inst.pic, inst.stackShown(), inst.shownId
	var targets []measureTarget
	for _, e := range inst.regs {
		if len(ids) > 0 && !slices.Contains(ids, e.r.Id) {
			continue
		}
		d := placeRegion(e.r, pic, stk)
		if !d.Drawn {
			if len(ids) > 0 {
				return app.RefuseOperation("region " + strconv.Quote(e.r.Label) + " is not measured on the image shown: it is " + d.Reason)
			}
			continue
		}
		targets = append(targets, measureTarget{e: e, key: regionKey(e, pic, area)})
	}
	for _, id := range ids {
		if regionIndex(inst.regs, id) < 0 {
			return errNoRegion(id)
		}
	}
	if len(targets) == 0 {
		return app.RefuseOperation("no region is drawn on the image shown; add one with add_region, or show an image it applies to")
	}
	inst.regionBatch = make([]string, len(targets))
	for i, t := range targets {
		inst.regionBatch[i] = t.e.r.Id
	}
	inst.regionNotDrawn = len(inst.regs) - len(targets)
	if len(ids) > 0 {
		inst.regionNotDrawn = 0
	}
	sel := shownSelection(stk)
	inst.regionJob.StartReporting(inst.tasks, bgjob.Spec{Kind: regionMeasureKind, Title: regionMeasureTitle},
		func(ctx context.Context, report bgjob.Reporter) (*measureBatch, error) {
			src := regions.StoreSource(pic)
			out := &measureBatch{results: make([]*regionMeasure, 0, len(targets))}
			for i, t := range targets {
				report(uint64(i), uint64(len(targets)), "measuring "+t.e.r.Label)
				m, err := regions.Measure(ctx, src, t.e.r, regions.MeasureOptions{Area: area})
				if err != nil && ctx.Err() != nil {
					return nil, ctx.Err()
				}
				out.results = append(out.results, &regionMeasure{regionId: t.e.r.Id, key: t.key, m: m, err: err, area: area, imageId: imageId, selection: sel})
			}
			report(uint64(len(targets)), uint64(len(targets)), "")
			return out, nil
		})
	inst.regionCallGen++
	inst.regionActive = true
	return nil
}

// takeRegionJobs moves what the measurement and the profile left onto the
// window, at the top of the frame.
func (inst *App) takeRegionJobs() {
	if res, _, ok := inst.regionJob.TakeResult(); ok {
		if inst.regMeasures == nil {
			inst.regMeasures = make(map[string]*regionMeasure, len(res.results))
		}
		for _, r := range res.results {
			inst.regMeasures[r.regionId] = r
		}
		inst.regionJobErr = ""
		inst.regionLanded = inst.regionCallGen
		inst.regionActive = false
	} else if snap := inst.regionJob.Snapshot(); snap.State == bgjob.StateFailed || inst.regionActive && snap.State == bgjob.StateIdle {
		inst.regionJobErr = "the measurement did not finish: " + refusalText(snap.Err)
		if snap.State == bgjob.StateIdle || errorsIsCancelled(snap.Err) {
			inst.regionJobErr = "the measurement was cancelled"
		}
		inst.regionLanded, inst.regionActive = inst.regionCallGen, false
		inst.regionJob.Invalidate()
	}
	if res, _, ok := inst.profileJob.TakeResult(); ok {
		inst.profile, inst.profileErr = res, ""
		inst.profileLanded, inst.profileActive = inst.profileCallGen, false
	} else if snap := inst.profileJob.Snapshot(); snap.State == bgjob.StateFailed || inst.profileActive && snap.State == bgjob.StateIdle {
		inst.profileErr = "the profile did not finish: " + refusalText(snap.Err)
		if snap.State == bgjob.StateIdle || errorsIsCancelled(snap.Err) {
			inst.profileErr = "the profile was cancelled"
		}
		inst.profileLanded, inst.profileActive = inst.profileCallGen, false
		inst.profileJob.Invalidate()
	}
}

// measureState is a region's figures as the window has them: none, the
// figures, and whether they are of the region and image as they stand.
func (inst *App) measureState(e regionEntry) (m *regionMeasure, stale bool) {
	m = inst.regMeasures[e.r.Id]
	if m == nil {
		return nil, false
	}
	return m, m.key != regionKey(e, inst.pic, m.area)
}

// regionProfile is a line's profile over one image. It is never modified
// once made.
type regionProfile struct {
	regionId  string
	key       string
	p         regions.LineProfile
	imageId   string
	selection *SelectionInfo
	// xs and ys are the chart's points per plane: the distance to the
	// middle of each pixel's part, and the value; an invalid sample has no
	// point.
	xs, ys [][]float64
}

// startProfile profiles a line region over the image shown as a task.
func (inst *App) startProfile(id string) error {
	if inst.pic == nil {
		return errNoImage
	}
	i := regionIndex(inst.regs, id)
	if i < 0 {
		return errNoRegion(id)
	}
	e := inst.regs[i]
	if e.r.Shape.Kind() != regions.ShapeKindLine {
		return app.RefuseOperation("region " + strconv.Quote(e.r.Label) + " is a " + e.r.Shape.Kind().String() + "; a profile is of a line")
	}
	if inst.profileJob.Running() {
		return app.RefuseOperation("a profile is under way; read get_view until Profiling is false")
	}
	pic, stk, imageId := inst.pic, inst.stackShown(), inst.shownId
	if d := placeRegion(e.r, pic, stk); !d.Drawn {
		return app.RefuseOperation("region " + strconv.Quote(e.r.Label) + " is not profiled on the image shown: it is " + d.Reason)
	}
	key := regionKey(e, pic, false)
	sel := shownSelection(stk)
	inst.profileJob.StartReporting(inst.tasks, bgjob.Spec{Kind: profileKind, Title: profileTitle},
		func(ctx context.Context, report bgjob.Reporter) (*regionProfile, error) {
			report(0, 0, "profiling "+e.r.Label)
			p, err := regions.Profile(ctx, regions.StoreSource(pic), e.r, regions.ProfileOptions{})
			if err != nil {
				return nil, err
			}
			return newRegionProfile(id, key, p, imageId, sel), nil
		})
	inst.profileCallGen++
	inst.profileActive = true
	return nil
}

func newRegionProfile(id, key string, p regions.LineProfile, imageId string, sel *SelectionInfo) *regionProfile {
	out := &regionProfile{regionId: id, key: key, p: p, imageId: imageId, selection: sel,
		xs: make([][]float64, len(p.Labels)), ys: make([][]float64, len(p.Labels))}
	for k := range p.Labels {
		out.xs[k] = make([]float64, 0, p.Len())
		out.ys[k] = make([]float64, 0, p.Len())
		for i, st := range p.All() {
			if _, v, ok := p.Sample(k, i); ok {
				out.xs[k] = append(out.xs[k], st.Distance)
				out.ys[k] = append(out.ys[k], v)
			}
		}
	}
	return out
}

// profileStale reports a profile that is not of the line and image as they
// stand.
func (inst *App) profileStale(p *regionProfile) bool {
	i := regionIndex(inst.regs, p.regionId)
	return i < 0 || p.key != regionKey(inst.regs[i], inst.pic, false)
}

// RegionPlane is one plane's figures over a region.
type RegionPlane struct {
	Plane    string     `desc:"the plane's name"`
	Rule     string     `desc:"the coverage the figures below were computed over: centre, point or line"`
	Valid    uint64     `desc:"valid samples covered"`
	Invalid  uint64     `desc:"invalid samples covered, left out of every other figure"`
	Min      float64    `desc:"smallest value; NaN when no valid sample is covered"`
	Max      float64    `desc:"largest value"`
	Mean     float64    `desc:"arithmetic mean"`
	StdDev   float64    `desc:"standard deviation, divisor n"`
	Median   float64    `desc:"50th percentile, nearest rank"`
	P01      float64    `desc:"1st percentile, nearest rank"`
	P99      float64    `desc:"99th percentile, nearest rank"`
	Exact    bool       `json:",omitzero" desc:"the percentiles are sample values; false for a binned kind, whose percentiles are right to BinWidth"`
	BinWidth float64    `json:",omitzero" desc:"the width of a bin in stored units, for a binned kind"`
	Area     *AreaPlane `json:",omitzero" desc:"the area figures, when asked for and the shape encloses an area"`
}

// AreaPlane is a plane's figures by covered area (ADR-0399 §SD4).
type AreaPlane struct {
	Rule        string  `desc:"area: every pixel weighted by the fraction of its area inside the shape"`
	Area        float64 `desc:"covered area of valid samples, in pixels"`
	InvalidArea float64 `desc:"covered area of invalid samples, left out of Sum and Mean"`
	Sum         float64 `desc:"the sum of fraction times value"`
	Mean        float64 `desc:"Sum / Area; NaN when Area is zero"`
}

// MappingInfo is how a region reached the image's pixels.
type MappingInfo struct {
	How       string    `desc:"identity (it was in pixels), exact (mapped exactly, its kind kept), polygon (mapped exactly to a polygon), approximated (a polygon within Tolerance of the mapped outline)"`
	From      FrameArgs `desc:"the region's frame"`
	To        FrameArgs `desc:"the frame of the image's placement; pixel for identity"`
	Tolerance float64   `json:",omitzero" desc:"for approximated: the largest distance in pixels an edge strays from the outline"`
	Vertices  int       `json:",omitzero" desc:"the vertices of a shape that became a polygon"`
	Shape     ShapeArgs `desc:"the shape in the image's pixels: what was measured"`
}

func mappingInfoOf(m regions.Mapping, px regions.Shape) MappingInfo {
	return MappingInfo{How: m.How.String(), From: frameArgsOf(m.From), To: frameArgsOf(m.To), Tolerance: m.Tolerance, Vertices: m.Vertices, Shape: shapeArgsOf(px)}
}

// RegionMeasurement is a region's figures over the image shown.
type RegionMeasurement struct {
	Region    string         `desc:"the region's id"`
	Label     string         `desc:"the region's label"`
	Method    string         `desc:"the identifier of the rules that produced the figures; a change to them is a new identifier"`
	Statement string         `desc:"what the figures are, in words"`
	Image     string         `desc:"the id of the image measured"`
	Selection *SelectionInfo `json:",omitzero" desc:"for a stack's selection: the selection measured"`
	Mapping   MappingInfo    `desc:"how the region reached the image's pixels"`
	Rule      string         `desc:"the coverage of every figure but the area figures"`
	Pixels    int64          `desc:"pixels covered by that rule"`
	Level     int            `desc:"the level read: 0, full resolution"`
	Area      bool           `json:",omitzero" desc:"the area figures were asked for"`
	Planes    []RegionPlane  `desc:"one entry per plane"`
	Stale     bool           `json:",omitzero" desc:"the region, the image shown or its selection changed since; measure_region again"`
}

// regionMeasurementOf is a result as measure_region gives it.
func regionMeasurementOf(rm *regionMeasure, label string, stale bool) RegionMeasurement {
	m := rm.m
	out := RegionMeasurement{Region: rm.regionId, Label: label, Method: m.Method, Statement: methodRegions, Image: rm.imageId,
		Selection: rm.selection, Mapping: mappingInfoOf(m.Mapping, m.Shape), Rule: m.Rule.String(), Pixels: m.Pixels,
		Level: m.Level, Area: rm.area, Stale: stale}
	for _, p := range m.Planes {
		rp := RegionPlane{Plane: p.Label, Rule: p.Rule.String(), Valid: p.Valid, Invalid: p.Invalid, Min: p.Min, Max: p.Max,
			Mean: p.Mean, StdDev: p.StdDev, Median: math.NaN(), P01: math.NaN(), P99: math.NaN(), Exact: p.Exact}
		if !p.Exact {
			rp.BinWidth = p.BinWidth
		}
		for _, pc := range p.Percentiles {
			switch pc.P {
			case 1:
				rp.P01 = pc.Value
			case 50:
				rp.Median = pc.Value
			case 99:
				rp.P99 = pc.Value
			}
		}
		if p.Area != nil {
			rp.Area = &AreaPlane{Rule: p.Area.Rule.String(), Area: p.Area.Area, InvalidArea: p.Area.InvalidArea, Sum: p.Area.Sum, Mean: p.Area.Mean}
		}
		out.Planes = append(out.Planes, rp)
	}
	return out
}

// ProfileRow is one pixel of a line's profile.
type ProfileRow struct {
	Col           int       `desc:"the pixel's column"`
	Row           int       `desc:"the pixel's row"`
	Length        float64   `desc:"the length of the line inside the pixel, in pixels"`
	Distance      float64   `desc:"the distance along the line from its start to the middle of that length, in pixels"`
	FrameDistance float64   `json:",omitzero" desc:"Distance in the frame's unit, when the image's placement has one unit for both axes"`
	Values        []float64 `desc:"one value per plane, in the order of Planes; 0 for an invalid sample"`
	Valid         []bool    `desc:"one per plane: false for an invalid sample"`
}

// ProfileRows is profile_region's result: a page of the profile.
type ProfileRows struct {
	Region    string         `desc:"the line's id"`
	Method    string         `desc:"the identifier of the rules that produced the profile"`
	Statement string         `desc:"what the profile is, in words"`
	Image     string         `desc:"the id of the image profiled"`
	Selection *SelectionInfo `json:",omitzero" desc:"for a stack's selection: the selection profiled"`
	Mapping   MappingInfo    `desc:"how the line reached the image's pixels"`
	Planes    []string       `desc:"plane names"`
	Length    float64        `desc:"the length of the part of the line inside the image, in pixels"`
	Unit      string         `json:",omitzero" desc:"the unit of FrameDistance; empty when the placement has none for both axes"`
	Total     int            `desc:"pixels in the profile"`
	From      int            `desc:"the first pixel listed"`
	Rows      []ProfileRow   `desc:"the pixels from From, at most 1024"`
	Truncated bool           `json:",omitzero" desc:"pixels follow the ones listed; ask again from From plus the rows listed"`
}

// profileRowsOf is a page of a profile.
func profileRowsOf(rp *regionProfile, from, count int) ProfileRows {
	p := rp.p
	out := ProfileRows{Region: rp.regionId, Method: p.Method, Statement: methodProfile, Image: rp.imageId, Selection: rp.selection,
		Mapping: mappingInfoOf(p.Mapping, p.Line), Planes: p.Labels, Length: p.Length, Total: p.Len()}
	if u, ok := p.FrameUnit(); ok {
		out.Unit = u
	}
	from = min(max(from, 0), p.Len())
	to := min(p.Len(), from+count)
	out.From, out.Truncated = from, to < p.Len()
	for i := from; i < to; i++ {
		st := p.Step(i)
		row := ProfileRow{Col: st.Col, Row: st.Row, Length: st.Length, Distance: st.Distance,
			Values: make([]float64, len(p.Labels)), Valid: make([]bool, len(p.Labels))}
		if !math.IsNaN(st.FrameDistance) {
			row.FrameDistance = st.FrameDistance
		}
		for k := range p.Labels {
			_, v, ok := p.Sample(k, i)
			row.Values[k], row.Valid[k] = v, ok
		}
		out.Rows = append(out.Rows, row)
	}
	return out
}
