package terrainscope

import (
	"fmt"
	"math"
	"strings"

	"github.com/stergiotis/boxer/public/keelson/runtime/app"
	"github.com/stergiotis/boxer/public/keelson/runtime/appops"
	"github.com/stergiotis/boxer/public/keelson/runtime/appops/fsmops"
	"github.com/stergiotis/boxer/public/keelson/runtime/appops/opfsm"
)

// The operations catalog (ADR-0269): what an agent under a task grant reads
// and sets in a terrainscope window. The points and the parameters are the
// two resources; the queries answer from a snapshot taken after the command
// stage; the commands set values and never toggle them. The sweep is a task
// already, so set_points is asynchronous and settles when the sweep it
// launched lands, fails or is cancelled (ADR-0358). The buttons of the
// window go through the same handlers (ADR-0269 §SD8).

const (
	resPoints     = "points"
	resParameters = "parameters"

	opGetView      = "get_view"
	opGetResult    = "get_result"
	opSetPoints    = "set_points"
	opSetParameter = "set_parameter"
	opClearPoints  = "clear_points"
	opRetry        = "retry"
	opCancel       = "cancel"

	// Result bounds: the rays one inline result may carry. A fan of ±30° at
	// 0.1° spacing has 601 rays; the default covers every fan the window
	// opens with (±2° at 0.5°, 9 rays) many times over.
	opsMaxRays     = 256
	opsDefaultRays = 64
)

// Parameter names set_parameter takes, with the range each slider has. One
// algorithm per name (F5): these are the sweep's own inputs, not a view.
const (
	paramObserverHeight = "observer_height"
	paramTargetHeight   = "target_height"
	paramSweepHalfDeg   = "sweep_half_deg"
	paramSweepStepDeg   = "sweep_step_deg"
	paramSigmaObsPos    = "sigma_obs_pos"
	paramSigmaTgtPos    = "sigma_tgt_pos"
	paramSigmaObsHeight = "sigma_obs_height"
	paramSigmaTgtHeight = "sigma_tgt_height"
	paramSamples        = "samples"
)

// paramRange is one named parameter's slider domain and where it lives.
type paramRange struct {
	name    string
	lo, hi  float64
	integer bool
	field   func(inst *App) *float64
}

// paramRanges lists every parameter in the order the window shows them.
var paramRanges = []paramRange{
	{paramObserverHeight, 0, 100, false, func(inst *App) *float64 { return &inst.observerH }},
	{paramSweepHalfDeg, 0, 30, false, func(inst *App) *float64 { return &inst.sweepHalfDeg }},
	{paramSamples, 0, 64, true, func(inst *App) *float64 { return &inst.samples }},
	{paramTargetHeight, 0, 100, false, func(inst *App) *float64 { return &inst.targetH }},
	{paramSweepStepDeg, 0.1, 5, false, func(inst *App) *float64 { return &inst.sweepStepDeg }},
	{paramSigmaObsPos, 0, 50, false, func(inst *App) *float64 { return &inst.sigmaObsPos }},
	{paramSigmaTgtPos, 0, 50, false, func(inst *App) *float64 { return &inst.sigmaTgtPos }},
	{paramSigmaObsHeight, 0, 30, false, func(inst *App) *float64 { return &inst.sigmaObsH }},
	{paramSigmaTgtHeight, 0, 30, false, func(inst *App) *float64 { return &inst.sigmaTgtH }},
}

func paramNames() (names []string) {
	names = make([]string, 0, len(paramRanges))
	for _, p := range paramRanges {
		names = append(names, p.name)
	}
	return
}

// Points is the points resource's value: the observer and the target in
// WGS84 degrees, and how many of them are placed.
type Points struct {
	Placed      int     `desc:"points placed: 0, 1 (the observer) or 2"`
	ObserverLat float64 `desc:"observer latitude, WGS84 degrees"`
	ObserverLon float64 `desc:"observer longitude, WGS84 degrees"`
	TargetLat   float64 `desc:"target latitude, WGS84 degrees"`
	TargetLon   float64 `desc:"target longitude, WGS84 degrees"`
}

// Parameters is the parameters resource's value, one field per name
// set_parameter takes.
type Parameters struct {
	ObserverHeight float64 `desc:"observer eye height above the terrain, metres, 0 to 100"`
	TargetHeight   float64 `desc:"target height above the terrain, metres, 0 to 100"`
	SweepHalfDeg   float64 `desc:"half-range of the bearing fan, degrees, 0 to 30"`
	SweepStepDeg   float64 `desc:"angular spacing of the fan, degrees, 0.1 to 5"`
	SigmaObsPos    float64 `desc:"1σ uncertainty of the observer's horizontal position, metres, 0 to 50"`
	SigmaTgtPos    float64 `desc:"1σ uncertainty of the target's horizontal position, metres, 0 to 50"`
	SigmaObsHeight float64 `desc:"1σ uncertainty of the observer height, metres, 0 to 30"`
	SigmaTgtHeight float64 `desc:"1σ uncertainty of the target height, metres, 0 to 30"`
	Samples        int64   `desc:"Monte-Carlo ensemble members, 0 to 64; 0 is the nominal sweep alone"`
}

// View is get_view's result: the phase, the inputs and what the last sweep
// left, without the rays.
type View struct {
	Phase       string     `desc:"the sweep's phase: idle, one point, two points, sweeping, result, failed or cancelled"`
	Points      Points     `desc:"the points placed"`
	Parameters  Parameters `desc:"the sweep's parameters"`
	Running     bool       `desc:"a sweep is in flight"`
	Error       string     `desc:"why the last sweep failed; empty otherwise"`
	Note        string     `desc:"the last outcome that was not a result, such as a cancel; empty otherwise"`
	HasResult   bool       `desc:"get_result has rays to return"`
	Rays        int        `desc:"rays in the result's fan"`
	Samples     int        `desc:"ensemble members the result was computed over"`
	ComputeMs   float64    `desc:"wall-clock of the result's ensemble simulation, milliseconds"`
	TilesDir    string     `desc:"the swissALTI3D tile directory the sweep reads"`
	TilesError  string     `desc:"why the tiles could not be opened, once a sweep tried; empty otherwise"`
	SeedProblem string     `desc:"why the seed variable was not applied; empty otherwise"`
}

// GetResultArgs bounds get_result.
type GetResultArgs struct {
	Limit int `json:",omitzero" desc:"rays to return, from the most negative bearing; default 64, at most 256"`
}

// Ray is one bearing of the fan.
type Ray struct {
	BearingOffsetDeg float64 `desc:"bearing relative to the observer→target line, degrees; 0 is the centre ray"`
	Visible          bool    `desc:"the nominal, un-jittered ray is clear of the terrain"`
	VisibleFraction  float64 `desc:"fraction of the ensemble in which this ray was clear; equals Visible with 0 samples"`
	ObstructionDistM float64 `desc:"distance along the ray to the first obstruction, metres; 0 when visible"`
	ObstructionElevM float64 `desc:"terrain elevation at the obstruction, metres a.s.l.; 0 when visible"`
}

// Result is get_result's result: the fan, bounded, with the method stated.
type Result struct {
	ObserverE     float64 `desc:"observer easting, LV95 metres"`
	ObserverN     float64 `desc:"observer northing, LV95 metres"`
	TargetE       float64 `desc:"target easting, LV95 metres"`
	TargetN       float64 `desc:"target northing, LV95 metres"`
	RangeM        float64 `desc:"observer→target ground distance, metres; every ray spans it"`
	Samples       int     `desc:"Monte-Carlo ensemble members behind VisibleFraction; 0 means the nominal sweep alone"`
	TotalRays     int     `desc:"rays in the fan"`
	Visible       int     `desc:"rays whose nominal line of sight is clear"`
	MeanVisible   float64 `desc:"mean of VisibleFraction over the fan"`
	CentreFrac    float64 `desc:"VisibleFraction of the centre ray, the observer→target bearing"`
	Rays          []Ray   `desc:"the first Limit rays, by bearing ascending"`
	Truncated     bool    `desc:"the fan has more rays than Rays holds"`
	Method        string  `desc:"what the numbers are and what they assume"`
	ComputeMs     float64 `desc:"wall-clock of the ensemble simulation, milliseconds"`
	Deterministic bool    `desc:"the ensemble is drawn from a fixed seed, so equal inputs give equal fractions"`
}

// SetPointsArgs is set_points's argument: both points, always.
type SetPointsArgs struct {
	ObserverLat float64 `desc:"observer latitude, WGS84 degrees, -90 to 90"`
	ObserverLon float64 `desc:"observer longitude, WGS84 degrees, -180 to 180"`
	TargetLat   float64 `desc:"target latitude, WGS84 degrees, -90 to 90"`
	TargetLon   float64 `desc:"target longitude, WGS84 degrees, -180 to 180"`
}

// SweepOutcome is what set_points settles with: the phase the sweep it
// launched ended in.
type SweepOutcome struct {
	Phase   string `desc:"result, failed, cancelled, or idle when the points were cleared meanwhile"`
	Rays    int    `desc:"rays in the result; 0 without one"`
	Samples int    `desc:"ensemble members behind the result"`
	Error   string `desc:"why the sweep failed; empty otherwise"`
}

// SetParameterArgs is set_parameter's argument.
type SetParameterArgs struct {
	Name  string  `desc:"one of observer_height, target_height, sweep_half_deg, sweep_step_deg, sigma_obs_pos, sigma_tgt_pos, sigma_obs_height, sigma_tgt_height, samples"`
	Value float64 `desc:"the new value, within the parameter's range; samples is rounded to an integer"`
}

// methodLine states the method and its assumptions where the number is
// (keelson-app F3). It reads the spec the result carries, so the σ it quotes
// are the ones that were drawn.
func methodLine(res *sweepResult) (s string) {
	sp := res.ens.Spec
	return fmt.Sprintf("Straight sight lines over swissALTI3D 2 m terrain, no earth curvature or refraction, "+
		"terrain samples the only obstruction. VisibleFraction is the fraction of %d Monte-Carlo members in which the ray was clear, "+
		"each member drawing independent Gaussians (1σ, metres) for observer position %.1f, target position %.1f, "+
		"observer height %.1f, target height %.1f, from a fixed seed; the bearing fan itself is deterministic. "+
		"A fraction is a statement about these inputs and this terrain, not a calibrated probability.",
		res.ens.Samples, sp.SigmaObsPosM, sp.SigmaTgtPosM, sp.SigmaObsHeightM, sp.SigmaTgtHeightM)
}

// opsSnap is what queries read: a copy taken after the command stage. The
// result pointer is to an immutable sweepResult, replaced and never edited.
type opsSnap struct {
	points   Points
	params   Parameters
	phase    phaseE
	running  bool
	err      string
	note     string
	result   *sweepResult
	tilesErr string
	seedErr  string
}

func (inst *App) points() (p Points) {
	p = Points{Placed: int(inst.stage), ObserverLat: inst.pt1Lat, ObserverLon: inst.pt1Lon, TargetLat: inst.pt2Lat, TargetLon: inst.pt2Lon}
	return
}

func (inst *App) parameters() (p Parameters) {
	p = Parameters{ObserverHeight: inst.observerH, TargetHeight: inst.targetH, SweepHalfDeg: inst.sweepHalfDeg,
		SweepStepDeg: inst.sweepStepDeg, SigmaObsPos: inst.sigmaObsPos, SigmaTgtPos: inst.sigmaTgtPos,
		SigmaObsHeight: inst.sigmaObsH, SigmaTgtHeight: inst.sigmaTgtH, Samples: int64(inst.samples + 0.5)}
	return
}

func (inst *App) opsSnapshot() (sn opsSnap) {
	sn = opsSnap{points: inst.points(), params: inst.parameters(), phase: inst.observePhase(),
		running: inst.sweep.Running(), note: inst.sweepNote, result: inst.result, seedErr: inst.seedErr}
	if inst.sweepErr != nil {
		sn.err = inst.sweepErr.Error()
	}
	if samplerErr != nil {
		sn.tilesErr = samplerErr.Error()
	}
	return
}

// applyParameters puts a Parameters value on the instance (the restore path
// and set_parameter's bulk sibling).
func (inst *App) applyParameters(p Parameters) {
	inst.observerH, inst.targetH = p.ObserverHeight, p.TargetHeight
	inst.sweepHalfDeg, inst.sweepStepDeg = p.SweepHalfDeg, p.SweepStepDeg
	inst.sigmaObsPos, inst.sigmaTgtPos = p.SigmaObsPos, p.SigmaTgtPos
	inst.sigmaObsH, inst.sigmaTgtH = p.SigmaObsHeight, p.SigmaTgtHeight
	inst.samples = float64(p.Samples)
}

// setPoints places both points and arms a sweep over them: the stage moves
// to two points, the map recentres, and the reactive launch runs on this
// frame even when the points are the ones already swept, so a repeat is
// applied, never a toggle. The sweep's generation at the call is kept so
// the asynchronous call settles on the sweep it caused.
func (inst *App) setPoints(in SetPointsArgs) {
	inst.pt1Lat, inst.pt1Lon = in.ObserverLat, in.ObserverLon
	inst.pt2Lat, inst.pt2Lon = in.TargetLat, in.TargetLon
	inst.stage = selectionStagePt2
	inst.haveLaunched = false
	inst.armRecenter()
	inst.pointsCallGen = inst.launchGen
}

// checkPoints refuses a point outside the WGS84 domain; whether the terrain
// covers it is the sweep's to say.
func checkPoints(in SetPointsArgs) (err error) {
	for _, p := range []struct {
		name string
		lat  float64
		lon  float64
	}{{"observer", in.ObserverLat, in.ObserverLon}, {"target", in.TargetLat, in.TargetLon}} {
		switch {
		case math.IsNaN(p.lat) || math.IsNaN(p.lon):
			return app.RefuseOperation(p.name + " is not a number")
		case p.lat < -90 || p.lat > 90:
			return app.RefuseOperation(p.name + " latitude is between -90 and 90 degrees")
		case p.lon < -180 || p.lon > 180:
			return app.RefuseOperation(p.name + " longitude is between -180 and 180 degrees")
		}
	}
	return
}

// setParameter sets one named parameter, refusing a name it does not know
// (naming the ones it does) and a value outside the range (naming it).
func (inst *App) setParameter(in SetParameterArgs) (err error) {
	for _, p := range paramRanges {
		if p.name != in.Name {
			continue
		}
		v := in.Value
		if p.integer {
			v = math.Round(v)
		}
		if math.IsNaN(v) || v < p.lo || v > p.hi {
			return app.RefuseOperation(fmt.Sprintf("%s is between %g and %g", p.name, p.lo, p.hi))
		}
		*p.field(inst) = v
		return
	}
	return app.RefuseOperation("no parameter named " + in.Name + "; the names are " + strings.Join(paramNames(), ", "))
}

// retry relaunches the last sweep with the same parameters: the Retry
// button's handler.
func (inst *App) retry() {
	inst.haveLaunched = false
}

// cancelSweep stops the running sweep and notes it: the row's Cancel.
func (inst *App) cancelSweep() {
	inst.sweep.Cancel()
	inst.sweepNote = "sweep cancelled"
	inst.landedGen = inst.launchGen
}

// resultOf renders a sweep as get_result's bounded answer.
func resultOf(res *sweepResult, limit int) (out Result) {
	ens := res.ens
	rays := ens.Nominal.Rays
	out = Result{ObserverE: res.fromLV.E, ObserverN: res.fromLV.N, TargetE: res.toLV.E, TargetN: res.toLV.N,
		Samples: ens.Samples, TotalRays: len(rays), Method: methodLine(res), Deterministic: true,
		ComputeMs: float64(res.computeDur.Microseconds()) / 1000}
	if len(rays) == 0 {
		return
	}
	centre := centerRayIndex(ens.AngleDeg)
	if d := rays[centre].ProfileDist; len(d) > 0 {
		out.RangeM = d[len(d)-1]
	}
	out.CentreFrac = ens.VisProb[centre]
	out.Rays = make([]Ray, 0, min(limit, len(rays)))
	for i, ray := range rays {
		if ray.Visible {
			out.Visible++
		}
		out.MeanVisible += ens.VisProb[i]
		if i < limit {
			out.Rays = append(out.Rays, Ray{BearingOffsetDeg: ens.AngleDeg[i], Visible: ray.Visible, VisibleFraction: ens.VisProb[i],
				ObstructionDistM: ray.ObstructionDist, ObstructionElevM: float64(ray.ObstructionElev)})
		}
	}
	out.MeanVisible /= float64(len(rays))
	out.Truncated = len(rays) > limit
	return
}

var ops = func() (s *appops.Set[*App, opsSnap]) {
	s = appops.NewSet((*App).opsSnapshot)

	s.Resource(resPoints, "the observer and the target, WGS84 degrees, and how many are placed", func(inst *App) any { return inst.points() })
	s.Restorable(resPoints, func(inst *App, v any) bool {
		p, ok := v.(Points)
		if !ok {
			return false
		}
		switch p.Placed {
		case 0:
			inst.resetSelection()
		case 1:
			inst.resetSelection()
			inst.pt1Lat, inst.pt1Lon = p.ObserverLat, p.ObserverLon
			inst.stage = selectionStagePt1
		default:
			inst.setPoints(SetPointsArgs{ObserverLat: p.ObserverLat, ObserverLon: p.ObserverLon, TargetLat: p.TargetLat, TargetLon: p.TargetLon})
		}
		return true
	})
	s.Resource(resParameters, "the sweep's parameters: heights, the fan, the four σ and the sample count", func(inst *App) any { return inst.parameters() })
	s.Restorable(resParameters, func(inst *App, v any) bool {
		p, ok := v.(Parameters)
		if ok {
			inst.applyParameters(p)
		}
		return ok
	})
	s.Editing(resParameters, func(inst *App) bool {
		for _, h := range inst.paramHandles {
			if appops.WidgetEditing(h) {
				return true
			}
		}
		return false
	})

	fsmops.Mount(s, "sweep", "the sweep's phase, from the first click to the result", func(inst *App) opfsm.SourceI {
		if inst.phaseMachine == nil {
			return nil
		}
		return inst.phaseMachine
	}, fsmops.Options{History: 16})

	appops.Query(s, app.OperationSpec{Name: opGetView, Version: 1,
		Summary: "read the phase, the points, the parameters and what the last sweep left, without the rays",
		Reads:   []string{resPoints, resParameters}, Agents: true, Fork: app.OperationForkReadShared},
		func(sn opsSnap, _ appops.None) (out View, err error) {
			out = View{Phase: sn.phase.String(), Points: sn.points, Parameters: sn.params, Running: sn.running,
				Error: sn.err, Note: sn.note, TilesDir: tilesDir(), TilesError: sn.tilesErr, SeedProblem: sn.seedErr}
			if r := sn.result; r != nil {
				out.HasResult = len(r.ens.Nominal.Rays) > 0
				out.Rays = len(r.ens.Nominal.Rays)
				out.Samples = r.ens.Samples
				out.ComputeMs = float64(r.computeDur.Microseconds()) / 1000
			}
			return
		})

	appops.Query(s, app.OperationSpec{Name: opGetResult, Version: 1,
		Summary: "read the last sweep's fan: per-bearing visibility fractions, at most 256 rays, with the method and the σ it drew stated",
		Reads:   []string{resPoints, resParameters}, Agents: true, Fork: app.OperationForkReadShared},
		func(sn opsSnap, in GetResultArgs) (out Result, err error) {
			limit := in.Limit
			switch {
			case limit < 0:
				err = app.RefuseOperation("limit is not negative")
				return
			case limit == 0:
				limit = opsDefaultRays
			case limit > opsMaxRays:
				limit = opsMaxRays
			}
			if sn.result == nil {
				switch sn.phase {
				case phaseSweeping:
					err = app.RefuseOperation("the sweep is running; read get_view until its phase is result")
				case phaseFailed:
					err = app.RefuseOperation("the last sweep failed: " + sn.err + "; retry relaunches it")
				default:
					err = app.RefuseOperation("there is no result yet; set_points launches a sweep")
				}
				return
			}
			out = resultOf(sn.result, limit)
			return
		})

	appops.Command(s, app.OperationSpec{Name: opSetPoints, Version: 1,
		Summary: "place the observer and the target and sweep between them; the call settles when the sweep lands, fails or is cancelled",
		Effect:  app.OperationEffectDocument, Writes: []string{resPoints}, Agents: true, Async: true,
		Gesture: "the second click on the Map tab", Rebuild: app.OperationRebuildVolatile, Fork: app.OperationForkPerInstance,
		Follows: []string{"the previous result is dropped at once; get_result refuses until the new sweep lands",
			"the same points again sweep again; a point outside WGS84 is refused naming the range"}},
		func(inst *App, _ app.OperationCall, in SetPointsArgs) (out appops.None, err error) {
			if err = checkPoints(in); err != nil {
				return
			}
			inst.setPoints(in)
			return
		})
	appops.Settles(s, opSetPoints, func(inst *App, _ app.OperationCall) (out SweepOutcome, settled bool, err error) {
		if inst.stage != selectionStagePt2 {
			return SweepOutcome{Phase: inst.observePhase().String()}, true, nil
		}
		if inst.landedGen <= inst.pointsCallGen || inst.sweep.Running() {
			return out, false, nil
		}
		out.Phase = inst.observePhase().String()
		if r := inst.result; r != nil {
			out.Rays, out.Samples = len(r.ens.Nominal.Rays), r.ens.Samples
		}
		if inst.sweepErr != nil {
			out.Error = inst.sweepErr.Error()
		}
		return out, true, nil
	})

	appops.Command(s, app.OperationSpec{Name: opSetParameter, Version: 1,
		Summary: "set one named parameter of the sweep; out of its range is refused naming the range",
		Effect:  app.OperationEffectDocument, Writes: []string{resParameters}, Agents: true,
		Gesture: "the parameter's slider", Rebuild: app.OperationRebuildVolatile, Fork: app.OperationForkPerInstance,
		Follows: []string{"with two points placed the sweep relaunches with the new value; its landing is read through get_view"}},
		func(inst *App, _ app.OperationCall, in SetParameterArgs) (out appops.None, err error) {
			err = inst.setParameter(in)
			return
		})

	appops.Command(s, app.OperationSpec{Name: opClearPoints, Version: 1,
		Summary: "clear both points and any result; a sweep in flight is discarded",
		Effect:  app.OperationEffectDocument, Writes: []string{resPoints}, Agents: true,
		Gesture: "the Reset button", Rebuild: app.OperationRebuildVolatile, Fork: app.OperationForkPerInstance},
		func(inst *App, _ app.OperationCall, _ appops.None) (out appops.None, err error) {
			inst.resetSelection()
			return
		})

	appops.Command(s, app.OperationSpec{Name: opRetry, Version: 1,
		Summary: "run the failed or cancelled sweep again with the same points and parameters",
		Effect:  app.OperationEffectRun, Reads: []string{resPoints, resParameters}, Agents: true,
		Gesture: "the Retry button", Fork: app.OperationForkPerInstance,
		Follows: []string{"the sweep relaunches at once; get_view says when it lands"}},
		func(inst *App, _ app.OperationCall, _ appops.None) (out appops.None, err error) {
			inst.retry()
			return
		})
	s.Available(opRetry, func(sn opsSnap) (ok bool, reason string) {
		if sn.phase == phaseFailed || sn.phase == phaseCancelled {
			return true, ""
		}
		return false, "nothing to retry: no sweep has failed or been cancelled; set_points or set_parameter launches one"
	})

	appops.Command(s, app.OperationSpec{Name: opCancel, Version: 1,
		Summary: "cancel the running sweep, the person's decision: the host asks them to confirm it every time; the result it would have replaced stays dropped",
		Effect:  app.OperationEffectConsequential, Writes: []string{resPoints}, Agents: true,
		Gesture: "Cancel on the progress row", Rebuild: app.OperationRebuildVolatile, Fork: app.OperationForkPerInstance},
		func(inst *App, _ app.OperationCall, _ appops.None) (out appops.None, err error) {
			if !inst.sweep.Running() {
				err = app.RefuseOperation("no sweep is running")
				return
			}
			inst.cancelSweep()
			return
		})
	s.Available(opCancel, func(sn opsSnap) (ok bool, reason string) {
		if sn.running {
			return true, ""
		}
		return false, "no sweep is running"
	})
	return
}()

// Operations serves the catalog for this window (app.OperationsAppI).
func (inst *App) Operations() (h app.OperationsHandlerI) { return ops.Bind(inst) }

var _ app.OperationsAppI = (*App)(nil)

// gestureOr routes the person's control through the catalog (ADR-0269
// §SD8), so the command log sees it as theirs, and applies it directly where
// no host serves the catalog (the tour, tests).
func gestureOr[In any](inst *App, op string, in In, direct func()) {
	if inst.frameCtx != nil {
		if _, err := appops.Gesture[In, appops.None](inst.frameCtx, op, in); err == nil {
			return
		}
	}
	direct()
}
