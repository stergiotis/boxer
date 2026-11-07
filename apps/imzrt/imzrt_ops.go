package imzrt

import (
	"strconv"
	"strings"
	"time"

	"github.com/stergiotis/boxer/public/keelson/runtime/app"
	"github.com/stergiotis/boxer/public/keelson/runtime/appops"
	"github.com/stergiotis/boxer/public/keelson/runtime/bgjob"
	"github.com/stergiotis/boxer/public/keelson/runtime/buscodec"
	"github.com/stergiotis/boxer/public/thestack/imzero2/egui2/widgets/trendsmooth"
)

// The operations catalog (ADR-0269, keelson-app A1). Queries read the
// sampler's latest published snapshot and the window's view, never a read of
// their own; commands set a value and never toggle it, so a repeat is
// applied. The sampler is one per process (ADR-0061 SD3), so set_interval
// and set_frozen move every imzrt window; smoothing and the tab are this
// window's. The two profile commands are the one act the dashboard performs
// on purpose, and they publish outside the window, so they are
// consequential and asynchronous (ADR-0358): a call stays running until the
// capture lands or the play window opens, and settles with what the row
// shows.

const (
	resView     = "view"
	resProfiles = "profiles"

	opGetView        = "get_view"
	opGetSample      = "get_sample"
	opListProfiles   = "list_profiles"
	opSetInterval    = "set_interval"
	opSetSmoothing   = "set_smoothing"
	opSetFrozen      = "set_frozen"
	opShowTab        = "show_tab"
	opCaptureProfile = "capture_profile"
	opOpenInPlay     = "open_in_play"
)

// The sampler's clamp (Sampler.SetInterval), as the refusal names it.
const (
	minIntervalMs = 100
	maxIntervalMs = 60_000
)

// smoothMinHalfWidth is the smallest half-width the smoothing kernel takes,
// read off the widget's own clamp so the refusal names the same bound.
var smoothMinHalfWidth = func() (w int32) {
	s := trendsmooth.New()
	s.SetHalfWidth(0)
	return s.HalfWidth()
}()

// Result bounds: what one inline result may carry.
const (
	opsMaxNoteLen = 256
)

const (
	noSampleReason     = "no sample has arrived yet; the sampler publishes one every interval, and get_view reports the interval"
	noBusReason        = "no bus on this host: capturing and opening in play need the app runtime"
	unknownKindReason  = "kind is one of: "
	captureRunningNote = " capture is running; list_profiles reports when it lands"
	openRunningNote    = " open is in flight; list_profiles reports the outcome"
	noCaptureReason    = " has no dataset yet; capture_profile publishes one"
)

// opsSampler is the sampler the catalog reads and sets; a test replaces it.
var opsSampler = ensureSampler

// dockTabs names the tabs show_tab takes, in the dock's order.
var dockTabs = []struct {
	name string
	tab  uint64
}{
	{"heap", dockTabHeap},
	{"gc", dockTabGC},
	{"scheduler", dockTabSched},
	{"profiles", dockTabProfiles},
}

// viewValue is the view resource's value: comparable, latched at the top
// of each frame so another window's write to the shared sampler lands
// inside this frame (ADR-0329 §SD3), and after each command so the write is
// observed as the caller's.
type viewValue struct {
	IntervalMs      int64
	Frozen          bool
	Smoothing       bool
	SmoothHalfWidth int32
	Tab             string
}

// profileState is one kind's row of the profiles resource.
type profileState struct {
	Kind      string
	Handle    string
	Revision  uint64
	Rows      uint64
	Capturing bool
	Opening   bool
}

func (inst *App) latchView() {
	v := viewValue{Smoothing: inst.smooth.On, SmoothHalfWidth: inst.smooth.HalfWidth(), Tab: inst.shownTab}
	if s, err := opsSampler(); err == nil {
		v.IntervalMs = s.Interval().Milliseconds()
		v.Frozen = s.IsPaused()
	}
	inst.latchedView = v
}

// profileStates reads the hub for the resource and the profiles query.
func profileStates() (states []profileState) {
	states = make([]profileState, 0, len(profileKinds))
	for _, spec := range profileKinds {
		e := profiles.entry(spec.key)
		profiles.mu.Lock()
		st := profileState{Kind: spec.key, Handle: e.pub.Handle(), Revision: e.revision, Rows: e.rows}
		profiles.mu.Unlock()
		st.Capturing = e.job.Running()
		st.Opening = e.open.Running()
		states = append(states, st)
	}
	return
}

// opsSnap is what queries read. pub is shared with the render path, which
// is safe because a PublishedSnapshot is never written after it is
// published.
type opsSnap struct {
	pub      *PublishedSnapshot
	view     viewValue
	sampling bool
	hasBus   bool
	profiles []ProfileRow
}

func (inst *App) opsSnapshot() (sn opsSnap) {
	sn.view = inst.latchedView
	sn.hasBus = inst.bus != nil
	if s, err := opsSampler(); err == nil {
		sn.sampling = true
		sn.pub = s.Latest()
	}
	sn.profiles = profileRows()
	return
}

// View is get_view's result.
type View struct {
	IntervalMs      int64  `desc:"the sampling interval, milliseconds; process-wide"`
	Frozen          bool   `desc:"the sampler is stopped and the plots hold their last sample; process-wide"`
	Smoothing       bool   `desc:"the trend-smoothing overlay is on in this window"`
	SmoothHalfWidth int32  `desc:"the smoothing kernel's half-width, in samples"`
	Tab             string `desc:"the tab show_tab last brought to the front: heap, gc, scheduler or profiles; empty when none, and the person's own tab clicks are not tracked"`
	Sampling        bool   `desc:"the sampler runs; false when it could not start"`
	SampledAtUnixMs int64  `desc:"when the latest sample was taken; 0 before the first"`
}

// Sample is get_sample's result: the latest sample's scalars. The history
// behind the plots stays in the window; it is not an operation.
type Sample struct {
	SampledAtUnixMs      int64   `desc:"when the sample was taken"`
	Goroutines           uint64  `desc:"live goroutines"`
	GOMAXPROCS           uint64  `desc:"the scheduler's P count"`
	HeapLiveBytes        uint64  `desc:"live heap after the last GC"`
	HeapObjectsBytes     uint64  `desc:"bytes in heap objects"`
	HeapObjects          uint64  `desc:"heap objects"`
	HeapGoalBytes        uint64  `desc:"the GC trigger target"`
	TotalMappedBytes     uint64  `desc:"memory mapped from the OS"`
	ReleasedBytes        uint64  `desc:"memory released to the OS"`
	GCCyclesTotal        uint64  `desc:"GC cycles since the process started"`
	GCCyclesForced       uint64  `desc:"of them, forced by the application"`
	GOGCPercent          uint64  `desc:"GOGC; 0 when off"`
	GOMemLimitBytes      uint64  `desc:"GOMEMLIMIT; 0 when unset"`
	AllocRateBytesPerSec float64 `desc:"allocation rate over the last interval"`
	AllocObjPerSec       float64 `desc:"objects allocated per second over the last interval"`
	GCPerSec             float64 `desc:"GC cycles per second over the last interval"`
	PauseP50Ms           float64 `desc:"GC pause p50 over the last interval, milliseconds"`
	PauseP99Ms           float64 `desc:"GC pause p99 over the last interval, milliseconds"`
	PauseMaxMs           float64 `desc:"the longest GC pause of the last interval, milliseconds"`
	PausesInWindow       uint64  `desc:"GC pauses in the last interval"`
	SchedP99Ms           float64 `desc:"scheduling latency p99 over the last interval, milliseconds"`
	STWGCCount           uint64  `desc:"stop-the-world pauses for GC in the last interval; 0 where the Go version lacks the metric"`
	STWOtherCount        uint64  `desc:"stop-the-world pauses for other causes in the last interval"`
	STWAvailable         bool    `desc:"the stop-the-world counts are available on this Go version"`
	MissingMetrics       int     `desc:"curated metrics this Go version lacks"`
}

// ProfileRow is one kind of list_profiles, and what capture_profile and
// open_in_play settle with.
type ProfileRow struct {
	Kind      string `desc:"the kind: cpu, heap, allocs, goroutine or goroutineleak"`
	Label     string `desc:"the row's label on the Profiles tab"`
	Alias     string `desc:"the dataset alias keelson('adhoc') lists, pprof_<kind>"`
	Handle    string `desc:"the dataset handle; empty before the first capture"`
	Revision  uint64 `desc:"the dataset's revision, raised by each capture"`
	Rows      uint64 `desc:"rows in the current dataset"`
	Capturing bool   `desc:"a capture of this kind is running"`
	Opening   bool   `desc:"an open in play of this kind is in flight"`
	LastNote  string `desc:"the last handover's outcome, as the row shows it"`
	LastError string `desc:"the last failure the row shows, empty when none"`
}

// ListProfilesArgs bounds the kinds returned.
type ListProfilesArgs struct {
	Limit int `json:",omitzero" desc:"kinds to return, in the tab's order; 0 returns every kind"`
}

// ListProfilesResult is list_profiles's result.
type ListProfilesResult struct {
	Matched   int          `desc:"kinds the window offers"`
	Profiles  []ProfileRow `desc:"the first limit kinds, in the tab's order"`
	Truncated bool         `desc:"kinds were left out by the limit"`
}

type SetIntervalArgs struct {
	IntervalMs int64 `desc:"the sampling interval, milliseconds, 100 to 60000"`
}

type SetSmoothingArgs struct {
	On        bool  `desc:"true draws the smoothed trend over the raw series"`
	HalfWidth int32 `json:",omitzero" desc:"the kernel's half-width in samples; 0 keeps the current one"`
}

type SetFrozenArgs struct {
	Frozen bool `desc:"true stops the sampler and holds the last sample; false restarts it"`
}

type ShowTabArgs struct {
	Tab string `desc:"heap, gc, scheduler or profiles"`
}

type ProfileKindArgs struct {
	Kind string `desc:"cpu, heap, allocs, goroutine or goroutineleak"`
}

func profileRows() (rows []ProfileRow) {
	rows = make([]ProfileRow, 0, len(profileKinds))
	for _, spec := range profileKinds {
		rows = append(rows, profileRowOf(spec))
	}
	return
}

func profileRowOf(spec profileKindSpec) (row ProfileRow) {
	e := profiles.entry(spec.key)
	profiles.mu.Lock()
	row = ProfileRow{Kind: spec.key, Label: spec.label, Alias: e.pub.Alias(), Handle: e.pub.Handle(),
		Revision: e.revision, Rows: e.rows,
		LastNote: appops.CutText(e.lastNote, opsMaxNoteLen), LastError: appops.CutText(e.lastErr, opsMaxNoteLen)}
	profiles.mu.Unlock()
	row.Capturing = e.job.Running()
	row.Opening = e.open.Running()
	return
}

func profileKindOf(kind string) (spec profileKindSpec, err error) {
	for _, s := range profileKinds {
		if s.key == kind {
			return s, nil
		}
	}
	names := make([]string, 0, len(profileKinds))
	for _, s := range profileKinds {
		names = append(names, s.key)
	}
	err = app.RefuseOperation(unknownKindReason + strings.Join(names, ", "))
	return
}

func intervalReason() (reason string) {
	return "interval_ms is between 100 and 60000; the − and + buttons move it by 500 ms within the same clamp"
}

func smoothingReason() (reason string) {
	return "half_width is between " + strconv.Itoa(int(smoothMinHalfWidth)) + " and " + strconv.Itoa(int(trendsmooth.MaxHalfWidth)) + " samples"
}

// settleRunner is the shape both asynchronous commands settle with: not
// while the runner runs, with the run's error when it failed, with the
// kind's row otherwise (ADR-0358 §SD1). It runs on the render goroutine,
// where the row's own sync lands the result into the entry first.
func settleRunner(kind string, runner interface{ Snapshot() bgjob.Snapshot }, running func() bool) (settled bool, result []byte, err error) {
	spec, err := profileKindOf(kind)
	if err != nil {
		settled = true
		return
	}
	e := profiles.entry(spec.key)
	profiles.syncEntry(e)
	if running() {
		return
	}
	settled = true
	if snap := runner.Snapshot(); snap.State == bgjob.StateFailed && snap.Err != nil {
		err = snap.Err
		return
	}
	result, err = buscodec.Encode(profileRowOf(spec))
	return
}

var ops = func() (s *appops.Set[*App, opsSnap]) {
	s = appops.NewSet((*App).opsSnapshot)

	s.Resource(resView, "the sampling interval and freeze (process-wide), this window's smoothing, and the tab show_tab last brought to the front",
		func(inst *App) any { return inst.latchedView })
	s.Resource(resProfiles, "per profile kind: the dataset's handle, revision and rows, and whether a capture or an open is in flight; process-wide",
		func(inst *App) any { return profileStates() })

	// --- queries ---

	appops.Query(s, app.OperationSpec{Name: opGetView, Version: 1,
		Summary: "read the interval, freeze, smoothing and the shown tab, and when the latest sample was taken",
		Reads:   []string{resView}, Agents: true, Fork: app.OperationForkReadShared},
		func(sn opsSnap, _ appops.None) (out View, err error) {
			out = View{IntervalMs: sn.view.IntervalMs, Frozen: sn.view.Frozen, Smoothing: sn.view.Smoothing,
				SmoothHalfWidth: sn.view.SmoothHalfWidth, Tab: sn.view.Tab, Sampling: sn.sampling}
			if sn.pub != nil {
				out.SampledAtUnixMs = sn.pub.SampledAtUnixMs
			}
			return
		})

	appops.Query(s, app.OperationSpec{Name: opGetSample, Version: 1,
		Summary: "read the latest sample's scalars: goroutines, heap, GC and scheduler; the history behind the plots is not an operation",
		Agents:  true, Fork: app.OperationForkReadShared},
		func(sn opsSnap, _ appops.None) (out Sample, err error) {
			p := sn.pub
			if p == nil {
				err = app.RefuseOperation(noSampleReason)
				return
			}
			out = Sample{SampledAtUnixMs: p.SampledAtUnixMs, Goroutines: p.Goroutines, GOMAXPROCS: p.GomaxProcs,
				HeapLiveBytes: p.HeapLiveBytes, HeapObjectsBytes: p.HeapObjectsBytes, HeapObjects: p.HeapObjectsCount,
				HeapGoalBytes: p.HeapGoalBytes, TotalMappedBytes: p.TotalMappedBytes, ReleasedBytes: p.ReleasedBytes,
				GCCyclesTotal: p.GCCyclesTotal, GCCyclesForced: p.GCCyclesForced, GOGCPercent: p.GOGCPercent,
				AllocRateBytesPerSec: p.AllocRateBytesPerSec, AllocObjPerSec: p.AllocObjPerSec, GCPerSec: p.GCPerSec,
				PauseP50Ms: p.PauseP50Sec * 1e3, PauseP99Ms: p.PauseP99Sec * 1e3, PauseMaxMs: p.PauseMaxSec * 1e3,
				PausesInWindow: p.PausesInWindow, SchedP99Ms: p.SchedP99Sec * 1e3,
				STWGCCount: p.STWGCCount, STWOtherCount: p.STWOtherCount, STWAvailable: p.STWAvailable,
				MissingMetrics: p.MissingMetrics}
			if p.GOGCPercent >= 1<<63 {
				out.GOGCPercent = 0
			}
			if p.MemLimitSet() {
				out.GOMemLimitBytes = p.GOMemLimitBytes
			}
			return
		})

	appops.Query(s, app.OperationSpec{Name: opListProfiles, Version: 1,
		Summary: "list the profile kinds with their dataset's handle, revision and rows, and what is in flight; at most the five kinds",
		Reads:   []string{resProfiles}, Agents: true, Untrusted: true, Fork: app.OperationForkReadShared},
		func(sn opsSnap, in ListProfilesArgs) (out ListProfilesResult, err error) {
			if in.Limit < 0 {
				err = app.RefuseOperation("limit is not negative")
				return
			}
			out.Matched = len(sn.profiles)
			limit := in.Limit
			if limit == 0 || limit > len(sn.profiles) {
				limit = len(sn.profiles)
			}
			out.Profiles = sn.profiles[:limit]
			out.Truncated = limit < len(sn.profiles)
			return
		})

	// --- commands over the view ---

	appops.Command(s, app.OperationSpec{Name: opSetInterval, Version: 1,
		Summary: "set the sampling interval in milliseconds, 100 to 60000; every imzrt window follows",
		Effect:  app.OperationEffectView, Writes: []string{resView}, Agents: true,
		Gesture: "the − and + buttons beside the interval", Rebuild: app.OperationRebuildVolatile},
		func(inst *App, _ app.OperationCall, in SetIntervalArgs) (out appops.None, err error) {
			if in.IntervalMs < minIntervalMs || in.IntervalMs > maxIntervalMs {
				err = app.RefuseOperation(intervalReason())
				return
			}
			s, sErr := opsSampler()
			if sErr != nil {
				err = app.RefuseOperation("the sampler did not start: " + sErr.Error())
				return
			}
			s.SetInterval(time.Duration(in.IntervalMs) * time.Millisecond)
			inst.latchView()
			return
		})

	appops.Command(s, app.OperationSpec{Name: opSetSmoothing, Version: 1,
		Summary: "turn the trend-smoothing overlay on or off, and set its half-width; this window only",
		Effect:  app.OperationEffectView, Writes: []string{resView}, Agents: true,
		Gesture: "the smooth check box and its ± stepper", Rebuild: app.OperationRebuildVolatile},
		func(inst *App, _ app.OperationCall, in SetSmoothingArgs) (out appops.None, err error) {
			if in.HalfWidth != 0 && (in.HalfWidth < smoothMinHalfWidth || in.HalfWidth > trendsmooth.MaxHalfWidth) {
				err = app.RefuseOperation(smoothingReason())
				return
			}
			inst.smooth.On = in.On
			if in.HalfWidth != 0 {
				inst.smooth.SetHalfWidth(in.HalfWidth)
			}
			inst.latchView()
			return
		})

	appops.Command(s, app.OperationSpec{Name: opSetFrozen, Version: 1,
		Summary: "stop the sampler and hold the last sample, or let it run again; every imzrt window follows",
		Effect:  app.OperationEffectView, Writes: []string{resView}, Agents: true,
		Gesture: "the Freeze / Go live button", Rebuild: app.OperationRebuildVolatile},
		func(inst *App, _ app.OperationCall, in SetFrozenArgs) (out appops.None, err error) {
			s, sErr := opsSampler()
			if sErr != nil {
				err = app.RefuseOperation("the sampler did not start: " + sErr.Error())
				return
			}
			s.Pause(in.Frozen)
			inst.latchView()
			return
		})

	appops.Command(s, app.OperationSpec{Name: opShowTab, Version: 1,
		Summary: "bring a dock tab to the front: heap, gc, scheduler or profiles",
		Effect:  app.OperationEffectView, Writes: []string{resView}, Agents: true,
		Gesture: "clicking a dock tab", Rebuild: app.OperationRebuildVolatile},
		func(inst *App, _ app.OperationCall, in ShowTabArgs) (out appops.None, err error) {
			for _, t := range dockTabs {
				if t.name == in.Tab {
					inst.pendingTab, inst.shownTab = t.tab, t.name
					inst.latchView()
					return
				}
			}
			names := make([]string, 0, len(dockTabs))
			for _, t := range dockTabs {
				names = append(names, t.name)
			}
			err = app.RefuseOperation("tab is one of: " + strings.Join(names, ", "))
			return
		})

	// --- the profile commands: consequential, asynchronous ---

	needsBus := func(sn opsSnap) (bool, string) {
		return sn.hasBus, noBusReason
	}

	appops.Command(s, app.OperationSpec{Name: opCaptureProfile, Version: 1,
		Summary: "capture a pprof profile of this process and publish it as the ad-hoc dataset pprof_<kind>; a re-capture raises the dataset's revision",
		Effect:  app.OperationEffectConsequential, Writes: []string{resProfiles}, Agents: true, Async: true,
		Gesture: "the Capture button of the kind's row", Rebuild: app.OperationRebuildVolatile,
		Follows: []string{"the call stays running while the capture runs (ten seconds for cpu) and settles with the kind's row, or fails with the capture's error (ADR-0358)",
			"goroutineleak forces a garbage collection, which steps the heap and GC series the other tabs plot"}},
		func(inst *App, _ app.OperationCall, in ProfileKindArgs) (out appops.None, err error) {
			spec, err := profileKindOf(in.Kind)
			if err != nil {
				return
			}
			if inst.bus == nil {
				err = app.RefuseOperation(noBusReason)
				return
			}
			if profiles.entry(spec.key).job.Running() {
				err = app.RefuseOperation("a " + spec.key + captureRunningNote)
				return
			}
			profiles.startCapture(spec, inst.bus, inst.tasks)
			return
		})
	s.Available(opCaptureProfile, needsBus)
	appops.SettlesRaw(s, opCaptureProfile, func(_ *App, _ app.OperationCall, args []byte) (settled bool, result []byte, err error) {
		in, err := buscodec.Decode[ProfileKindArgs](args)
		if err != nil {
			settled = true
			return
		}
		e := profiles.entry(in.Kind)
		return settleRunner(in.Kind, &e.job, e.job.Running)
	})

	appops.Command(s, app.OperationSpec{Name: opOpenInPlay, Version: 1,
		Summary: "open a play window on the kind's dataset, bound to the introspection endpoint and focused on the icicle panel",
		Effect:  app.OperationEffectConsequential, Writes: []string{resProfiles}, Agents: true, Async: true,
		Gesture: "the Open in play button of the kind's row", Rebuild: app.OperationRebuildVolatile,
		Follows: []string{"the call stays running while the host opens the window and settles with the kind's row, whose last_note names the window, or fails with the host's refusal (ADR-0358)"}},
		func(inst *App, _ app.OperationCall, in ProfileKindArgs) (out appops.None, err error) {
			spec, err := profileKindOf(in.Kind)
			if err != nil {
				return
			}
			if inst.bus == nil {
				err = app.RefuseOperation(noBusReason)
				return
			}
			e := profiles.entry(spec.key)
			profiles.mu.Lock()
			handle := e.pub.Handle()
			profiles.mu.Unlock()
			if handle == "" {
				err = app.RefuseOperation(spec.key + noCaptureReason)
				return
			}
			if e.open.Running() {
				err = app.RefuseOperation("a " + spec.key + openRunningNote)
				return
			}
			profiles.openInPlay(spec, inst.bus, inst.tasks)
			return
		})
	s.Available(opOpenInPlay, needsBus)
	appops.SettlesRaw(s, opOpenInPlay, func(_ *App, _ app.OperationCall, args []byte) (settled bool, result []byte, err error) {
		in, err := buscodec.Decode[ProfileKindArgs](args)
		if err != nil {
			settled = true
			return
		}
		e := profiles.entry(in.Kind)
		return settleRunner(in.Kind, &e.open, e.open.Running)
	})
	return
}()

// Operations serves the catalog for this window (app.OperationsAppI).
func (inst *App) Operations() (h app.OperationsHandlerI) { return ops.Bind(inst) }

var _ app.OperationsAppI = (*App)(nil)

// gestureOr routes the person's control through the catalog (ADR-0269
// §SD8), so the command log sees it as theirs, and applies it directly
// where no host serves the catalog (tour, tests).
func gestureOr[In any](inst *App, op string, in In, direct func()) {
	if inst.frameCtx != nil {
		if _, err := appops.Gesture[In, appops.None](inst.frameCtx, op, in); err == nil {
			return
		}
	}
	direct()
}

func (inst *App) freezeGesture(s *Sampler, frozen bool) {
	gestureOr(inst, opSetFrozen, SetFrozenArgs{Frozen: frozen}, func() { s.Pause(frozen); inst.latchView() })
}

// intervalGesture moves the interval by one step, clamped as the sampler
// clamps it, so the command never refuses the person's click at the edge.
func (inst *App) intervalGesture(s *Sampler, step time.Duration) {
	target := min(max(s.Interval()+step, minIntervalMs*time.Millisecond), maxIntervalMs*time.Millisecond)
	gestureOr(inst, opSetInterval, SetIntervalArgs{IntervalMs: target.Milliseconds()}, func() { s.SetInterval(target); inst.latchView() })
}

// smoothingGesture records the smoothing row's change as the person's:
// the shared widget has applied it, so the command sets the value already
// set and the log carries the writer.
func (inst *App) smoothingGesture() {
	gestureOr(inst, opSetSmoothing, SetSmoothingArgs{On: inst.smooth.On, HalfWidth: inst.smooth.HalfWidth()}, inst.latchView)
}

func (inst *App) captureGesture(spec profileKindSpec) {
	gestureOr(inst, opCaptureProfile, ProfileKindArgs{Kind: spec.key}, func() { profiles.startCapture(spec, inst.bus, inst.tasks) })
}

func (inst *App) openInPlayGesture(spec profileKindSpec) {
	gestureOr(inst, opOpenInPlay, ProfileKindArgs{Kind: spec.key}, func() { profiles.openInPlay(spec, inst.bus, inst.tasks) })
}
