package flowoverlay

import (
	"context"
	"errors"
	"math"
	"slices"
	"sort"
	"sync"
	"time"

	"github.com/stergiotis/boxer/public/science/geo/vectorfield"
	c "github.com/stergiotis/boxer/public/thestack/imzero2/egui2/bindings"
	"github.com/stergiotis/boxer/public/thestack/imzero2/egui2/widgets/color"
	"github.com/stergiotis/boxer/public/thestack/imzero2/egui2/widgets/colormap"
	"github.com/stergiotis/boxer/public/thestack/imzero2/egui2/widgets/portolan"
)

// Options tunes a [Layer]. It is read every frame, so changing a field is an
// assignment and not a rebuild; the zero value is usable.
type Options struct {
	// Density is particles per thousand square pixels of canvas, so a larger
	// window shows the same picture and not a sparser one. Zero takes 5.
	Density float32
	// MaxParticles caps the count whatever the canvas. Zero takes 10000: on
	// the one machine the ADR-0249 trial measured, a low-power APU, the Go
	// side and the host's dispatch together pass a 30 Hz tick between 10000
	// and 20000 particles on a GPU host, and a CPU-rasterizer host holds the
	// tick to about 5000 (doc/trials/flow-particles-frame-cost §0).
	MaxParticles int

	// TickHz is the simulation's rate, independent of the display's. Zero
	// takes 30.
	TickHz float32
	// TrailTicks is how many ticks of its path a particle shows. Zero takes 12.
	TrailTicks int
	// MaxAgeTicks is the longest a particle lives, which is what ends the ones
	// circling an eddy. Zero takes 150.
	MaxAgeTicks int
	// ResetBase is the probability per tick of re-seeding a particle, and
	// ResetBump how much more of it a particle at full speed gets. Zeros take
	// 0.004 and 0.012.
	ResetBase, ResetBump float32

	// SpeedMax is the magnitude that moves at MaxPacePx and tops the palette;
	// zero takes the source's Meta.SpeedMax.
	SpeedMax float32
	// MinPacePx and MaxPacePx bound a particle's pace in pixels per tick. The
	// floor keeps slow flow moving; the cap keeps a step inside a sample, so
	// it should not exceed SamplePx. Zeros take 0.35 and 3.
	MinPacePx, MaxPacePx float32
	// CalmSpeed is the vector mean below which the field has no direction to
	// follow. Zero takes a two-hundredth of SpeedMax.
	CalmSpeed float32

	// SamplePx is how many screen pixels one sample of a requested window
	// spans. Zero takes 4.
	SamplePx float32

	// Palette colours a trail by the scalar mean of the magnitude, 0xRRGGBBAA
	// stops from zero to SpeedMax. nil takes DefaultPalette, which is made for
	// a dark basemap; over light tiles pass a darker ramp.
	Palette []uint32
	// Opacity scales every trail's alpha. Zero takes 0.9.
	Opacity float32
	// LineWidth is the stroke width in pixels. Zero takes 1.5.
	LineWidth float32
	// Tessellated hints the host to draw feathered line shapes instead of one
	// mesh (see paintSegments); a host may ignore it.
	Tessellated bool
	// LinePerSegment paints a trail segment per paintLine opcode instead of
	// one paintSegments for the layer. It is the arm the ADR-0249 trial
	// measures the batched opcode against, and has no other use.
	LinePerSegment bool

	// Seed seeds the layer's own random stream.
	Seed uint64
	// Paused stops the simulation and the repaint requests; the last trails
	// stay on screen.
	Paused bool
	// FixedTicks, when positive, runs exactly that many ticks once a field is
	// there and then rests — the mode a deterministic capture uses. The
	// picture is then a function of the seed, the view and the field.
	FixedTicks int
	// Synchronous asks the source on the frame goroutine instead of on one of
	// the layer's own, so a slow source stalls the frame. It is for captures
	// and tests, where what is on screen must not depend on when a goroutine
	// ran; with FixedTicks the picture is complete on the first frame.
	Synchronous bool
}

// DefaultPalette runs from a pale blue through green and yellow to a hot
// magenta. Its slow end is light on purpose: a ramp that starts dark, as the
// perceptual sequential ones do, loses the slow half of the field against a
// dark map, and a trail's tail already fades into the background by alpha.
var DefaultPalette = []uint32{
	0x8fb8deff, 0x7fd6c2ff, 0xb5e48cff, 0xf3e17aff, 0xf6ae4eff, 0xee6c4dff, 0xd83f87ff,
}

// Stats is what the layer did last frame.
type Stats struct {
	Particles  int
	Segments   int
	Ticks      uint64
	WindowCols int
	WindowRows int
	// WindowLevel is the pyramid level of the window on screen.
	WindowLevel int
	Fetches     uint64
	// DroppedReplies counts replies that answered a superseded request.
	DroppedReplies uint64
	InFlight       bool
	LastError      error
}

type stepData struct {
	win  vectorfield.Window
	grid *fieldGrid
}

// geometry is what a set of windows was asked for: the same bounds and
// resolution, whatever the step.
type geometry struct {
	id      uint64
	req     vectorfield.Request // Step unused
	cover   rect                // the request's bounds in world units
	allLons bool                // the request spans the whole circle
}

type fetchReply struct {
	gen  uint64
	geom geometry
	step int
	data stepData
	err  error
}

// Layer is a flow layer over one source.
//
// Draw and the setters belong to the frame goroutine. Window requests run on
// goroutines of the layer's own and hand their replies over through a
// mailbox, which Draw empties.
type Layer struct {
	Opts Options

	src  vectorfield.SourceI
	meta vectorfield.Meta

	sim       *sim
	simParams simParams
	simSeed   uint64
	lastFrame time.Time
	carry     float64 // seconds not yet spent on a tick

	pos     float64 // display time as a fractional step index
	lastPos float64

	geom    geometry
	steps   map[int]stepData
	missing map[int]bool
	native  bool // the window is as fine as the source gets

	gen         uint64
	geomSerial  uint64
	inflight    *geometry
	inflightFor [2]int
	inflightEnd int // the last step the request in flight will answer
	cancel      context.CancelFunc
	lastFetch   time.Time
	retryAfter  time.Time
	prefetched  map[int]bool

	mailMu sync.Mutex
	mail   []fetchReply
	closed bool

	lut        []uint32
	lutPalette []uint32
	lutMax     float32

	x0s, y0s, x1s, y1s []float32
	cols               color.Colors
	alphas             []uint32

	stats Stats
	now   func() time.Time
}

const (
	defaultDensity   = 5
	defaultMaxCount  = 10000
	defaultTickHz    = 30
	defaultTrail     = 12
	defaultMaxAge    = 150
	defaultResetBase = 0.004
	defaultResetBump = 0.012
	defaultMinPace   = 0.35
	defaultMaxPace   = 3
	defaultSamplePx  = 4
	defaultOpacity   = 0.9
	defaultLineWidth = 1.5

	// viewMargin is how far past the view a window is asked for, as a
	// fraction of the view, so that a pan is not a request per frame.
	viewMargin = 0.35
	// A window is asked for again when it has become coarser or finer than
	// wanted by these factors. A source serves between one and two times the
	// wanted spacing, so the band is wider than that on both sides: a view
	// resting near a level boundary does not alternate.
	coarserThan = 2.6
	finerThan   = 0.55
	// fetchDebounce is the shortest time between two requests.
	fetchDebounce = 120 * time.Millisecond
	errorBackoff  = 2 * time.Second
	// maxTicksPerFrame bounds the catching up after a stall.
	maxTicksPerFrame = 4
	// fixedTicksPerFrame is how many ticks a frame runs in FixedTicks mode.
	fixedTicksPerFrame = 64
)

// New makes a layer over src.
func New(src vectorfield.SourceI, opts Options) (inst *Layer) {
	inst = &Layer{
		Opts:       opts,
		src:        src,
		meta:       src.Describe(),
		steps:      make(map[int]stepData, 2),
		missing:    make(map[int]bool, 2),
		prefetched: make(map[int]bool, 4),
		now:        time.Now,
	}
	return
}

// Close cancels what is in flight. A layer is not drawn after it.
func (inst *Layer) Close() {
	inst.mailMu.Lock()
	inst.closed = true
	inst.mail = nil
	inst.mailMu.Unlock()
	if inst.cancel != nil {
		inst.cancel()
		inst.cancel = nil
	}
}

// Meta is the source's description.
func (inst *Layer) Meta() (meta vectorfield.Meta) { return inst.meta }

// Stats reports the last frame.
func (inst *Layer) Stats() (stats Stats) { return inst.stats }

// SetStepPosition sets the display time as a fractional step index: 2.25 is
// a quarter of the way from step 2 to step 3. It is clamped to the steps.
func (inst *Layer) SetStepPosition(pos float64) {
	last := float64(len(inst.meta.Steps) - 1)
	if !(pos > 0) {
		pos = 0
	}
	if pos > last {
		pos = last
	}
	inst.pos = pos
}

// StepPosition is the display time as a fractional step index.
func (inst *Layer) StepPosition() (pos float64) { return inst.pos }

// SetTime sets the display time. Steps need not be evenly spaced; a time
// before the first or after the last shows that step.
func (inst *Layer) SetTime(t time.Time) {
	steps := inst.meta.Steps
	i := sort.Search(len(steps), func(k int) bool { return steps[k].Valid.After(t) })
	if i == 0 {
		inst.SetStepPosition(0)
		return
	}
	if i == len(steps) {
		inst.SetStepPosition(float64(len(steps) - 1))
		return
	}
	a, b := steps[i-1].Valid, steps[i].Valid
	frac := 0.0
	if span := b.Sub(a); span > 0 {
		frac = float64(t.Sub(a)) / float64(span)
	}
	inst.SetStepPosition(float64(i-1) + frac)
}

// Time is the display time.
func (inst *Layer) Time() (t time.Time) {
	steps := inst.meta.Steps
	if len(steps) == 0 {
		return
	}
	i := int(inst.pos)
	if i >= len(steps)-1 {
		return steps[len(steps)-1].Valid
	}
	a, b := steps[i].Valid, steps[i+1].Valid
	return a.Add(time.Duration((inst.pos - float64(i)) * float64(b.Sub(a))))
}

// StepStateE is what the layer has of one step for the view on screen.
type StepStateE uint8

const (
	// StepStateIdle is a step the layer has no window of and is not asking
	// for.
	StepStateIdle StepStateE = iota
	// StepStateHeld is a step whose window is there.
	StepStateHeld
	// StepStateLoading is a step the request in flight will answer.
	StepStateLoading
	// StepStateMissing is a step the source lists and could not serve.
	StepStateMissing
)

// StepState says what the layer has of a step. The layer keeps the windows of
// the steps around the display time and lets the others go, so at rest at
// most two steps are held. A time control reads it to show what a move will
// cost, and to hold playback until the step it is about to enter has arrived.
func (inst *Layer) StepState(step int) (state StepStateE) {
	if _, ok := inst.steps[step]; ok {
		return StepStateHeld
	}
	if inst.missing[step] {
		return StepStateMissing
	}
	if inst.inflight != nil && (step == inst.inflightFor[0] || step == inst.inflightFor[1]) {
		return StepStateLoading
	}
	return StepStateIdle
}

// bracket is the pair of steps around the display time and the weight of the
// second. b is -1 when the time sits on a step.
func (inst *Layer) bracket() (a, b int, weight float32) {
	a = int(inst.pos)
	frac := inst.pos - float64(a)
	if frac < 1e-6 || a+1 >= len(inst.meta.Steps) {
		return a, -1, 0
	}
	return a, a + 1, float32(frac)
}

// At reads the field at a place and the display time: the east and north
// components of the vector mean, and the scalar mean of the magnitude, which
// is the larger of the two lengths wherever directions disagree inside a
// sample. ok is false off the data and before the first window has arrived.
func (inst *Layer) At(ll portolan.LatLng) (u, v, speed float32, ok bool) {
	a, b, weight := inst.bracket()
	lon := ll.Lng
	sample := func(step int) (float32, float32, float32, bool) {
		d, has := inst.steps[step]
		if !has {
			return 0, 0, 0, false
		}
		x := lon
		if inst.meta.PeriodicLon {
			x = foldInto(lon, d.win.West, d.win.East(), 360)
		}
		return d.win.Sample(x, ll.Lat)
	}
	u, v, speed, ok = sample(a)
	if b < 0 {
		return
	}
	bu, bv, bs, bok := sample(b)
	if !bok {
		return
	}
	if !ok {
		return bu, bv, bs, true
	}
	return u + weight*(bu-u), v + weight*(bv-v), speed + weight*(bs-speed), true
}

// crsProjection is the view's projection without the view, which the frame
// goroutine goes on changing while a request runs.
type crsProjection struct{ crs portolan.CRSI }

func (p crsProjection) ProjectAt(ll portolan.LatLng, zoom float64) portolan.Point {
	return p.crs.LatLngToPoint(ll, zoom)
}

func (p crsProjection) UnprojectAt(pt portolan.Point, zoom float64) portolan.LatLng {
	return p.crs.PointToLatLng(pt, zoom)
}

// Draw advances the particles and paints them. It is called inside the map's
// overlay callback, with that frame's projector.
func (inst *Layer) Draw(p portolan.Projector) {
	view := p.View()
	size := view.Size()
	if !(size.X > 0) || !(size.Y > 0) {
		return
	}
	proj := crsProjection{crs: view.CRS()}
	scale := view.ZoomScaleAt(view.Zoom(), worldZoom) // pixels per world unit
	origin := view.PixelOrigin()
	viewport := rect{origin.X / scale, origin.Y / scale, (origin.X + size.X) / scale, (origin.Y + size.Y) / scale}

	inst.takeReplies()
	inst.ensureWindow(proj, viewport, size, scale)

	f := inst.field(proj)
	inst.advance(&f, viewport, size, scale)
	inst.paint(origin, scale, size)

	inst.stats.Particles = 0
	if inst.sim != nil {
		inst.stats.Particles = inst.sim.n
		inst.stats.Ticks = inst.sim.ticks
	}
	inst.stats.InFlight = inst.inflight != nil

	// Pace the repaints: at the tick rate while animating, slowly while only
	// a reply is awaited, and not at all at rest.
	o := &inst.Opts
	animating := !o.Paused && !f.isEmpty() && (o.FixedTicks <= 0 || inst.sim == nil || inst.sim.ticks < uint64(o.FixedTicks))
	if animating {
		c.RequestRepaintAfter(1 / float64(inst.tickHz()))
	} else if inst.inflight != nil || f.isEmpty() {
		c.RequestRepaintAfter(0.05)
	}
}

func (inst *Layer) tickHz() float32 {
	if inst.Opts.TickHz > 0 {
		return inst.Opts.TickHz
	}
	return defaultTickHz
}

// field assembles what the simulation samples this frame.
func (inst *Layer) field(proj crsProjection) (f field) {
	a, b, weight := inst.bracket()
	if d, ok := inst.steps[a]; ok {
		f.a = d.grid
	}
	if b >= 0 {
		if d, ok := inst.steps[b]; ok {
			f.b, f.weight = d.grid, weight
		}
	}
	if f.a == nil {
		// The nearer step is still loading or missing: show the other alone.
		f.a, f.b, f.weight = f.b, nil, 0
	}
	if inst.meta.PeriodicLon {
		if lo, hi, ok := proj.crs.WrapLng(); ok {
			f.wrap = proj.ProjectAt(portolan.LL(0, hi), worldZoom).X - proj.ProjectAt(portolan.LL(0, lo), worldZoom).X
		}
	}
	return
}

func (inst *Layer) resolveSimParams() (params simParams) {
	o := &inst.Opts
	pick := func(v, def float32) float32 {
		if v > 0 {
			return v
		}
		return def
	}
	params.trail = o.TrailTicks
	if params.trail < 2 {
		params.trail = defaultTrail
	}
	params.maxAge = int32(o.MaxAgeTicks)
	if params.maxAge <= 0 {
		params.maxAge = defaultMaxAge
	}
	params.resetBase = pick(o.ResetBase, defaultResetBase)
	params.resetBump = pick(o.ResetBump, defaultResetBump)
	params.minPace = pick(o.MinPacePx, defaultMinPace)
	params.maxPace = max(pick(o.MaxPacePx, defaultMaxPace), params.minPace)
	params.speedMax = pick(o.SpeedMax, inst.meta.SpeedMax)
	if !(params.speedMax > 0) {
		params.speedMax = 1
	}
	params.calm = pick(o.CalmSpeed, params.speedMax/200)
	params.seedTries = 4
	params.killMargin = 0.1
	return
}

// advance runs the ticks this frame owes.
func (inst *Layer) advance(f *field, viewport rect, size portolan.Point, scale float64) {
	o := &inst.Opts
	params := inst.resolveSimParams()
	if inst.sim == nil || params.trail != inst.simParams.trail || o.Seed != inst.simSeed {
		// The trail length is the ring's size; a new one starts over.
		inst.sim = newSim(o.Seed, params)
		inst.simSeed = o.Seed
	}
	inst.sim.params = params
	inst.simParams = params

	density := o.Density
	if !(density > 0) {
		density = defaultDensity
	}
	limit := o.MaxParticles
	if limit <= 0 {
		limit = defaultMaxCount
	}
	inst.sim.resize(min(int(float64(density)*size.X*size.Y/1000), limit))

	now := inst.now()
	elapsed := now.Sub(inst.lastFrame).Seconds()
	first := inst.lastFrame.IsZero()
	inst.lastFrame = now
	if o.Paused || f.isEmpty() {
		inst.carry = 0
		return
	}
	worldPerPixel := 1 / scale
	if o.FixedTicks > 0 {
		perFrame := fixedTicksPerFrame
		if o.Synchronous {
			perFrame = o.FixedTicks
		}
		for n := 0; n < perFrame && inst.sim.ticks < uint64(o.FixedTicks); n++ {
			inst.sim.tick(f, viewport, worldPerPixel)
		}
		return
	}
	if first {
		elapsed = 0
	}
	period := 1 / float64(inst.tickHz())
	inst.carry += elapsed
	ticks := int(inst.carry / period)
	inst.carry -= float64(ticks) * period
	if ticks > maxTicksPerFrame {
		// A stall is not caught up on: the particles would leap.
		ticks = maxTicksPerFrame
		inst.carry = 0
	}
	for range ticks {
		inst.sim.tick(f, viewport, worldPerPixel)
	}
}

// ensureLUT builds the palette lookup when the palette or its range changed.
func (inst *Layer) ensureLUT(speedMax float32) {
	palette := inst.Opts.Palette
	if len(palette) < 2 {
		palette = DefaultPalette
	}
	if inst.lut != nil && inst.lutMax == speedMax && len(palette) == len(inst.lutPalette) && &palette[0] == &inst.lutPalette[0] {
		return
	}
	cfg := colormap.NewConfig(palette, 0, float64(speedMax))
	if inst.lut == nil {
		inst.lut = make([]uint32, 256)
	}
	for i := range inst.lut {
		inst.lut[i] = cfg.At(float64(speedMax)*float64(i)/255) | 0xff
	}
	inst.lutPalette, inst.lutMax = palette, speedMax
}

// paint emits every trail as one batch of segments. A segment's alpha falls
// with its age and never with the particle's, so a stroke fades into the map
// at its tail and is brightest at its head — which is what makes its
// direction readable in a still frame.
func (inst *Layer) paint(origin portolan.Point, scale float64, size portolan.Point) {
	inst.stats.Segments = 0
	s := inst.sim
	if s == nil || s.n == 0 {
		return
	}
	o := &inst.Opts
	params := inst.simParams
	inst.ensureLUT(params.speedMax)
	opacity := o.Opacity
	if !(opacity > 0) {
		opacity = defaultOpacity
	}
	opacity = min(opacity, 1)
	width := o.LineWidth
	if !(width > 0) {
		width = defaultLineWidth
	}

	k := params.trail
	inst.alphas = slices.Grow(inst.alphas[:0], k)[:k]
	alphas := inst.alphas
	for j := range alphas {
		t := 1 - float64(j)/float64(k-1)
		alphas[j] = uint32(math.Round(255 * float64(opacity) * math.Pow(t, 1.3)))
	}

	inst.x0s, inst.y0s = inst.x0s[:0], inst.y0s[:0]
	inst.x1s, inst.y1s = inst.x1s[:0], inst.y1s[:0]
	inst.cols = inst.cols[:0]
	w, h := float32(size.X), float32(size.Y)
	const margin = 8
	for i := range s.n {
		count := int(s.count[i])
		if count < 2 {
			continue
		}
		nx, ny, ns := s.point(i, 0)
		ax, ay := float32(nx*scale-origin.X), float32(ny*scale-origin.Y)
		for j := range count - 1 {
			ox, oy, os := s.point(i, j+1)
			bx, by := float32(ox*scale-origin.X), float32(oy*scale-origin.Y)
			visible := (ax > -margin || bx > -margin) && (ax < w+margin || bx < w+margin) &&
				(ay > -margin || by > -margin) && (ay < h+margin || by < h+margin)
			if visible && (ax != bx || ay != by) && alphas[j] > 0 {
				idx := int(min(max(ns/params.speedMax, 0), 1) * 255)
				inst.x0s = append(inst.x0s, bx)
				inst.y0s = append(inst.y0s, by)
				inst.x1s = append(inst.x1s, ax)
				inst.y1s = append(inst.y1s, ay)
				inst.cols = append(inst.cols, inst.lut[idx]&^0xff|alphas[j])
			}
			ax, ay, ns = bx, by, os
		}
	}
	inst.stats.Segments = len(inst.cols)
	if len(inst.cols) == 0 {
		return
	}
	if o.LinePerSegment {
		for i := range inst.cols {
			c.PaintLine(inst.x0s[i], inst.y0s[i], inst.x1s[i], inst.y1s[i], color.Hex(inst.cols[i]), width).Send()
		}
		return
	}
	seg := c.PaintSegments(inst.x0s, inst.y0s, inst.x1s, inst.y1s, inst.cols, width)
	if o.Tessellated {
		seg = seg.Tessellated()
	}
	seg.Send()
}

// takeReplies empties the mailbox. A reply is taken only if it answers the
// latest request; the first reply for a new geometry replaces every window of
// the old one in one assignment.
func (inst *Layer) takeReplies() {
	inst.mailMu.Lock()
	mail := inst.mail
	inst.mail = nil
	inst.mailMu.Unlock()
	for i := range mail {
		r := &mail[i]
		if r.gen != inst.gen {
			inst.stats.DroppedReplies++
			continue
		}
		if r.err != nil && !errors.Is(r.err, vectorfield.ErrStepMissing) {
			if !errors.Is(r.err, context.Canceled) {
				inst.stats.LastError = r.err
				inst.retryAfter = inst.now().Add(errorBackoff)
			}
			inst.inflight = nil
			continue
		}
		if r.geom.id != inst.geom.id {
			inst.geom = r.geom
			inst.steps = make(map[int]stepData, 2)
			inst.missing = make(map[int]bool, 2)
		}
		if r.err != nil {
			inst.missing[r.step] = true
		} else {
			inst.steps[r.step] = r.data
			inst.native = r.data.win.DLon <= inst.meta.DLon*1.01
			inst.stats.WindowCols, inst.stats.WindowRows = r.data.win.Cols, r.data.win.Rows
			inst.stats.WindowLevel = r.data.win.Level
			inst.stats.LastError = nil
		}
		if r.step == inst.inflightEnd {
			inst.inflight = nil
		}
	}
	// Windows of steps the display time has left are let go.
	a, b, _ := inst.bracket()
	for step := range inst.steps {
		if step != a && step != b {
			delete(inst.steps, step)
		}
	}
}

// ensureWindow asks for windows when the ones on screen no longer serve the
// view: it has moved out of them, the zoom has left their resolution behind,
// or the display time has moved to steps they do not hold.
func (inst *Layer) ensureWindow(proj crsProjection, viewport rect, size portolan.Point, scale float64) {
	o := &inst.Opts
	samplePx := float64(o.SamplePx)
	if !(samplePx > 0) {
		samplePx = defaultSamplePx
	}
	a, b, weight := inst.bracket()
	need := [2]int{a, b}
	if b >= 0 && weight > 0.5 {
		need = [2]int{b, a} // the nearer step first
	}
	inst.prefetch(a, b)

	covers := func(g *geometry) bool {
		if g == nil || g.id == 0 {
			return false
		}
		inX := g.allLons || (viewport.x0 >= g.cover.x0 && viewport.x1 <= g.cover.x1)
		return inX && viewport.y0 >= g.cover.y0 && viewport.y1 <= g.cover.y1
	}
	have := func(step int) bool {
		if step < 0 {
			return true
		}
		_, ok := inst.steps[step]
		return ok || inst.missing[step]
	}

	fresh := false // whether a new geometry is needed, or only a step of this one
	switch {
	case !covers(&inst.geom):
		fresh = true
	case inst.resolutionIsOff(samplePx / scale):
		fresh = true
	case have(need[0]) && have(need[1]):
		return
	}

	if inst.inflight != nil {
		// Let it finish unless it no longer answers the view or the time.
		if covers(inst.inflight) && inst.inflightFor == need {
			return
		}
	}
	now := inst.now()
	if now.Sub(inst.lastFetch) < fetchDebounce || now.Before(inst.retryAfter) {
		return
	}

	geom := inst.geom
	if fresh {
		inst.geomSerial++
		geom = inst.newGeometry(proj, viewport, size, samplePx)
		geom.id = inst.geomSerial
	}
	var want []int
	for _, step := range need {
		if step >= 0 && (fresh || !have(step)) {
			want = append(want, step)
		}
	}
	if len(want) == 0 {
		return
	}
	if inst.cancel != nil {
		inst.cancel()
	}
	inst.gen++
	ctx, cancel := context.WithCancel(context.Background())
	inst.cancel = cancel
	inst.inflight = &geom
	inst.inflightFor = need
	inst.inflightEnd = want[len(want)-1]
	inst.lastFetch = now
	inst.stats.Fetches++
	if o.Synchronous {
		inst.fetch(ctx, inst.gen, geom, want, proj)
		inst.takeReplies()
		return
	}
	go inst.fetch(ctx, inst.gen, geom, want, proj)
}

// resolutionIsOff says the window on screen is too coarse or too fine for
// the zoom. want is the wanted sample spacing in world units.
func (inst *Layer) resolutionIsOff(want float64) (off bool) {
	for _, d := range inst.steps {
		if d.grid == nil {
			continue
		}
		ratio := d.grid.dx / want
		// Too coarse does not count once the source has nothing finer.
		return (ratio > coarserThan && !inst.native) || ratio < finerThan
	}
	return false
}

// newGeometry is the request for the view as it stands: the view grown by a
// margin, at one sample per samplePx pixels.
func (inst *Layer) newGeometry(proj crsProjection, viewport rect, size portolan.Point, samplePx float64) (geom geometry) {
	cover := viewport.grown(viewMargin)
	nw := proj.UnprojectAt(portolan.Pt(cover.x0, cover.y0), worldZoom)
	se := proj.UnprojectAt(portolan.Pt(cover.x1, cover.y1), worldZoom)
	north := math.Min(math.Max(nw.Lat, se.Lat), latLimit)
	south := math.Max(math.Min(nw.Lat, se.Lat), -latLimit)
	west, east := nw.Lng, se.Lng
	if east-west >= 360 {
		// The view shows the world more than once; one turn serves it all.
		mid := (west + east) / 2
		west, east = mid-180, mid+180
		geom.allLons = inst.meta.PeriodicLon
	}
	grow := 1 + 2*viewMargin
	geom.req = vectorfield.Request{
		West: west, East: east, South: south, North: north,
		MaxCols: max(int(math.Ceil(size.X*grow/samplePx)), 2),
		MaxRows: max(int(math.Ceil(size.Y*grow/samplePx)), 2),
	}
	geom.cover = cover
	return
}

// fetch asks the source for each step in turn and posts each reply as it
// comes, so the nearer step is on screen before the farther one has loaded.
func (inst *Layer) fetch(ctx context.Context, gen uint64, geom geometry, steps []int, proj crsProjection) {
	for _, step := range steps {
		req := geom.req
		req.Step = step
		win, err := inst.src.SampleE(ctx, req)
		reply := fetchReply{gen: gen, geom: geom, step: step, err: err}
		if err == nil {
			reply.data = stepData{win: win, grid: newFieldGrid(&win, proj)}
		}
		inst.mailMu.Lock()
		if !inst.closed {
			inst.mail = append(inst.mail, reply)
		}
		inst.mailMu.Unlock()
		if ctx.Err() != nil {
			return
		}
	}
}

// prefetcherI is a source that can load a step ahead of its first request.
type prefetcherI interface {
	PrefetchE(ctx context.Context, step int) (err error)
}

// prefetch loads the step after the bracket in the direction the display
// time is moving, once the bracket's far step is the nearer one.
func (inst *Layer) prefetch(a, b int) {
	dir := 0
	if inst.pos > inst.lastPos {
		dir = 1
	} else if inst.pos < inst.lastPos {
		dir = -1
	}
	inst.lastPos = inst.pos
	pre, ok := inst.src.(prefetcherI)
	if !ok || dir == 0 {
		return
	}
	next := a - 1
	if dir > 0 {
		next = max(a, b) + 1
	}
	if next < 0 || next >= len(inst.meta.Steps) || inst.prefetched[next] {
		return
	}
	if len(inst.prefetched) > 64 {
		clear(inst.prefetched)
	}
	inst.prefetched[next] = true
	go func() { _ = pre.PrefetchE(context.Background(), next) }()
}
