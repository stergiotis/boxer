package chatview

import (
	"math"
	"testing"
	"time"

	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"

	"github.com/stergiotis/boxer/public/keelson/designsystem/styletokens"
	c "github.com/stergiotis/boxer/public/thestack/imzero2/egui2/bindings"
)

// simHost stands in for the imzero2 host and its one egui pass per frame:
// it lays the planned units out at their true heights, keeps the scroll
// area's offset, answers the probes one frame late, and applies a scroll
// asked for during a frame at that frame's end — a correction at once, a
// jump or the tail by halving the distance each frame, as an animated
// scroll closes in.
type simHost struct {
	viewTop, viewH, sp float32
	offset             float32
	// wheel moves the offset at the end of every frame, after any scroll
	// asked for, as egui applies the wheel after a scroll target.
	wheel float32
	truth map[int32]float32
	rects map[uint64]c.UiRectValue
	// animTarget is an animated scroll under way; NaN none.
	animTarget float32
	// current is the ordinal of the unit last frame marked current; -1 none.
	current int32
	// pos is where each unit laid out last frame started, in screen
	// coordinates; virtual is where every unit started, a stand-in at its
	// place inside the space it is part of.
	pos, virtual map[int32]float32
}

const (
	simView, simContent, simUnits, simSeek uint64 = 1, 2, 3, 4
)

func simUnitProbes(key int32) (top, bot uint64) {
	return 1000 + 2*uint64(key), 1001 + 2*uint64(key)
}

func newSimHost(truth map[int32]float32) *simHost {
	return &simHost{viewTop: 100, viewH: 600, sp: 6, truth: truth, rects: map[uint64]c.UiRectValue{},
		animTarget: float32(math.NaN()), pos: map[int32]float32{}, virtual: map[int32]float32{}}
}

func (inst *simHost) read(seq uint64) (r c.UiRectValue, ok bool) {
	r, ok = inst.rects[seq]
	return
}

// frame runs one frame of Render's virtualization over rows against the
// host: plan, lay out, read back next frame.
func (inst *simHost) frame(t *testing.T, st *State, m *Model, est func(unit) float32) (fp framePlan) {
	t.Helper()
	n := m.Len()
	first := st.beginFrame(n)
	rows := plan(m, time.UTC, first)
	fp = st.planFrame(inst.read, simView, simContent, simUnits, m, rows, inst.sp, est)
	vt := &st.virt
	rects := map[uint64]c.UiRectValue{}
	contentTop := inst.viewTop - inst.offset
	rects[simView] = c.UiRectValue{MinY: inst.viewTop, MaxY: inst.viewTop + inst.viewH}
	rects[simContent] = c.UiRectValue{MinY: contentTop, MaxY: contentTop + inst.viewH}
	y := contentTop
	rects[simUnits] = c.UiRectValue{MinY: y}
	scrollTo, animated := float32(math.NaN()), false
	clear(inst.pos)
	clear(inst.virtual)
	inst.current = -1
	next := 0 // the first unit whose virtual position is not recorded yet
	vt.walk(fp.absorb, func(s float32) {
		// A run of stand-ins: each unit at its stand-in height.
		v := y
		for ; next < len(vt.units) && !vt.mask[next]; next++ {
			inst.virtual[vt.units[next].ord] = v
			v += vt.hs[next]
		}
		y += s
	}, func(i int) {
		for ; next < i; next++ {
			inst.virtual[vt.units[next].ord] = y
		}
		next = i + 1
		inst.virtual[vt.units[i].ord] = y
		u := vt.units[i]
		top, bot := simUnitProbes(u.ord)
		rects[top] = c.UiRectValue{MinY: y}
		inst.pos[u.ord] = y
		if i == fp.current {
			inst.current = u.ord
		}
		if i == fp.seekUnit {
			vt.seekProbe = simSeek
			rects[simSeek] = c.UiRectValue{MinY: y}
			want := inst.sp
			if st.seekAlign == scrollAlignCenter {
				want = inst.viewH / 2
			}
			scrollTo, animated = y-contentTop-want, true
		}
		h, ok := inst.truth[u.ord]
		require.True(t, ok, "unit %d has a true height", u.ord)
		y += h
		rects[bot] = c.UiRectValue{MinY: y}
		vt.drawn = append(vt.drawn, laidOut{key: u.key, ord: u.ord, assumed: vt.hs[i], top: top, bot: bot})
	})
	contentH := y - contentTop
	if st.Follow() {
		scrollTo, animated = contentH-inst.viewH, true
	}
	inst.rects = rects
	switch {
	case math.IsNaN(float64(scrollTo)):
	case animated:
		inst.animTarget = scrollTo
	default:
		inst.offset, inst.animTarget = scrollTo, float32(math.NaN())
	}
	if fp.correct {
		inst.offset += fp.delta
	}
	if !math.IsNaN(float64(inst.animTarget)) {
		inst.offset += (inst.animTarget - inst.offset) / 2
		if math.Abs(float64(inst.animTarget-inst.offset)) < 1 {
			inst.offset, inst.animTarget = inst.animTarget, float32(math.NaN())
		}
	}
	if inst.wheel != 0 {
		// The wheel ends an animated scroll, as egui's does.
		inst.offset += inst.wheel
		inst.animTarget = float32(math.NaN())
	}
	inst.offset = max(0, min(inst.offset, contentH-inst.viewH))
	return
}

// screenY is where unit key was laid out last frame, relative to the
// viewport's top; ok false when it was not laid out.
func (inst *simHost) screenY(key int32) (y float32, ok bool) {
	p, ok := inst.pos[key]
	return p - inst.viewTop, ok
}

// virtualY is where unit key started last frame relative to the viewport's
// top, laid out or standing in.
func (inst *simHost) virtualY(key int32) (y float32) {
	return inst.virtual[key] - inst.viewTop
}

func uniform(n int, h float32) map[int32]float32 {
	out := make(map[int32]float32, n)
	for i := range n {
		out[int32(i)] = h
	}
	return out
}

func constEstimate(h float32) func(unit) float32 { return func(unit) float32 { return h } }

// The cache answers from what it holds; a unit it does not hold takes the
// estimate once, and stands in with that same estimate until a readback
// replaces it with the measured height.
func TestHeightCacheHitsAndMisses(t *testing.T) {
	var vt virt
	calls := 0
	est := func(unit) float32 { calls++; return 40 }
	u := unit{key: 7}
	assert.Equal(t, float32(40), vt.height(u, est))
	assert.Equal(t, 1, calls, "a miss estimates")
	assert.False(t, vt.heights[7].measured)
	assert.Equal(t, float32(40), vt.height(u, est))
	assert.Equal(t, 1, calls, "an estimate is kept, so the unit stands in with the same height every frame")

	vt.drawn = []laidOut{{key: 7, assumed: 40, top: 10, bot: 11}}
	rects := map[uint64]c.UiRectValue{
		simView: {MinY: 0, MaxY: 600}, simContent: {MinY: -100, MaxY: 500},
		10: {MinY: 200}, 11: {MinY: 263},
	}
	read := func(seq uint64) (r c.UiRectValue, ok bool) { r, ok = rects[seq]; return }
	grew, _, ok := vt.readBack(read, simView, simContent, simUnits, -1)
	require.True(t, ok)
	assert.Equal(t, float32(0), grew, "a unit inside the viewport is not a change above it")
	assert.Equal(t, rowHeight{h: 63, measured: true}, vt.heights[7])
	assert.Equal(t, float32(63), vt.height(u, est))
	assert.Equal(t, 1, calls, "a measured unit is a hit")
	assert.Equal(t, float32(100), vt.view.offset)
	assert.Equal(t, float32(600), vt.view.h)

	// A frame that did not reach the screen reads nothing back and keeps
	// what was known.
	vt.drawn = []laidOut{{key: 7, assumed: 63, top: 10, bot: 11}}
	_, _, ok = vt.readBack(func(uint64) (c.UiRectValue, bool) { return c.UiRectValue{}, false }, simView, simContent, simUnits, -1)
	assert.False(t, ok)
	assert.Equal(t, rowHeight{h: 63, measured: true}, vt.heights[7])
	assert.True(t, vt.view.ok)
}

// Units never laid out stand in with their estimate, the ones near the
// viewport are measured, and the transcript lays out a few viewports of
// units rather than the window.
func TestEstimateStandsInForUnmeasuredUnits(t *testing.T) {
	m := longModel(300)
	truth := uniform(300, 50)
	host := newSimHost(truth)
	var st State
	st.SetFollow(false)
	vt := &st.virt
	var prev []bool
	for range 10 {
		prev = append(prev[:0], vt.mask...)
		host.frame(t, &st, m, constEstimate(80))
	}
	require.Equal(t, prev, vt.mask, "the laid-out units settle once the ones near the viewport are measured")
	laid := 0
	for i, u := range vt.units {
		h := vt.heights[u.key]
		if vt.mask[i] {
			laid++
			assert.True(t, h.measured, "unit %d is laid out and measured", u.key)
			assert.Equal(t, float32(50), vt.hs[i])
			continue
		}
		if !h.measured {
			assert.Equal(t, float32(80), vt.hs[i], "unit %d stands in with its estimate", u.key)
		}
	}
	assert.Less(t, laid, 60, "laid out %d of %d units", laid, len(vt.units))
	assert.True(t, vt.mask[0], "a view that never followed is at the top")
	assert.False(t, vt.mask[len(vt.mask)-1])
}

func TestEstimate(t *testing.T) {
	m := longModel(4)
	m.Body[1] = "a much longer body that wraps over several lines at a narrow width, " +
		"and then some more words so that it certainly does"
	m.Flags[2] = FlagDeleted
	m.Body[2] = m.Body[1]
	m.ReplyTo[3] = 0
	rows := plan(m, time.UTC, 0)
	units := unitsOf(m, rows, nil)
	require.Len(t, units, 4)
	d := styletokens.DensityStandard
	h := func(i int) float32 { return estimate(m, rows, units[i], LayoutDialogue, 0, 200, d) }
	assert.Greater(t, h(1), h(2), "a wrapping body is taller than a deleted one")
	assert.Greater(t, h(0), h(2)-1, "the first unit carries the day separator")
	assert.Greater(t, h(3), h(2), "a reply carries the quote strip")
	assert.Equal(t, 1, wrappedLines("", 100))
	assert.Equal(t, 2, wrappedLines("a\nb", 100))
	assert.Equal(t, 3, wrappedLines("012345678901234567890123456789", 70))
	assert.Equal(t, 4, wrappedLines("0123456789012345678901234567890", 70))
}

// The units of a plan: a day separator joins the row after it.
func TestUnitsOf(t *testing.T) {
	m := twoParty()
	rows := plan(m, time.UTC, 0)
	units := unitsOf(m, rows, nil)
	require.Len(t, units, 6)
	assert.Equal(t, unit{from: 0, to: 2, ord: 0, key: m.key(0)}, units[0])
	assert.Equal(t, unit{from: 2, to: 3, ord: 1, key: m.key(1)}, units[1])
	assert.Equal(t, unit{from: 6, to: 8, ord: 5, key: m.key(5)}, units[5])
}

// A unit above the viewport that grows moves what the reader looks at for
// the one frame the host has not yet reported it; the frame after takes the
// change out of the space above, and from the next the scroll has moved by
// as much. The reading position is back after one frame and stays.
func TestAnchorHoldsWhenAUnitAboveChangesHeight(t *testing.T) {
	m := longModel(200)
	truth := uniform(200, 60)
	host := newSimHost(truth)
	host.offset = 6000
	var st State
	st.SetFollow(false)
	for range 3 {
		host.frame(t, &st, m, constEstimate(60))
	}
	const anchor, grown int32 = 101, 95
	p0, ok := host.screenY(anchor)
	require.True(t, ok)
	require.GreaterOrEqual(t, p0, float32(0), "the anchor is in view")
	_, ok = host.screenY(grown)
	require.True(t, ok, "the unit that grows is laid out, inside the margin")
	g, _ := host.screenY(grown)
	require.LessOrEqual(t, g+60, float32(0), "and entirely above the viewport")

	truth[grown] = 100
	fp := host.frame(t, &st, m, constEstimate(60))
	assert.False(t, fp.correct, "the change is not known yet")
	p, _ := host.screenY(anchor)
	assert.Equal(t, p0+40, p, "the frame the unit grows in shows it")

	fp = host.frame(t, &st, m, constEstimate(60))
	assert.True(t, fp.correct)
	assert.Equal(t, float32(40), fp.absorb)
	p, _ = host.screenY(anchor)
	assert.Equal(t, p0, p, "the space above takes the change while the scroll is asked for")
	for range 5 {
		fp = host.frame(t, &st, m, constEstimate(60))
		assert.False(t, fp.correct, "one correction, not a chain")
		p, _ = host.screenY(anchor)
		assert.Equal(t, p0, p, "the scroll has moved with the unit")
	}
	assert.Equal(t, float32(6040), host.offset)

	// A unit that shrinks is the same in the other direction.
	truth[grown] = 30
	host.frame(t, &st, m, constEstimate(60))
	for range 4 {
		host.frame(t, &st, m, constEstimate(60))
	}
	p, _ = host.screenY(anchor)
	assert.Equal(t, p0, p)
	assert.Equal(t, float32(5970), host.offset)
}

// Scrolling up through history that was estimated too short: every unit is
// measured inside the margin above the viewport, and once the wheel stops
// the reader's units sit where the wheel alone put them. A correction is
// relative, so it keeps the wheel of the frame it lands in.
func TestScrollingUpThroughUnmeasuredHistory(t *testing.T) {
	m := longModel(300)
	truth := uniform(300, 75)
	host := newSimHost(truth)
	host.offset = 300 * 75
	var st State
	for range 3 {
		host.frame(t, &st, m, constEstimate(50))
	}
	st.SetFollow(false)
	host.frame(t, &st, m, constEstimate(50))
	// The anchor is a unit in view at the start; its position stands for
	// the content's once it has scrolled out at the bottom.
	const anchor int32 = 295
	_, ok := host.screenY(anchor)
	require.True(t, ok)
	p0 := host.virtualY(anchor)
	const wheel = 100
	host.wheel = -wheel
	corrections := 0
	frames := 40
	for range frames {
		if host.frame(t, &st, m, constEstimate(50)).correct {
			corrections++
		}
	}
	host.wheel = 0
	for range 4 {
		host.frame(t, &st, m, constEstimate(50))
	}
	moved := host.virtualY(anchor) - p0
	t.Logf("wheel %d px over %d frames, the anchor moved %.0f px; %d corrections", wheel*frames, frames, moved, corrections)
	assert.Greater(t, corrections, 0, "units entered the margin unmeasured")
	assert.InDelta(t, float32(wheel*frames), moved, 1, "the wheel moved the content by as much as it turned")
}

// Following the tail is unchanged: the newest unit is always laid out, the
// view ends at the tail as messages arrive, and following asks for no
// correction even when units above the viewport change.
func TestFollowingTheTail(t *testing.T) {
	m := longModel(120)
	truth := uniform(400, 60)
	host := newSimHost(truth)
	var st State
	require.True(t, st.Follow())
	for i := range 40 {
		if i%4 == 0 {
			m = longModel(m.Len() + 1)
		}
		if i == 20 {
			truth[int32(m.Len()-12)] = 140
		}
		fp := host.frame(t, &st, m, constEstimate(45))
		assert.False(t, fp.correct)
		assert.True(t, st.virt.mask[len(st.virt.mask)-1], "the tail is laid out")
	}
	for range 10 {
		host.frame(t, &st, m, constEstimate(45))
	}
	y, ok := host.screenY(int32(m.Len() - 1))
	require.True(t, ok)
	assert.InDelta(t, host.viewH-60, y, 0.5, "the newest message sits at the bottom of the view")
	assert.True(t, st.Follow())
}

// A jump brings its target to where it asks — the middle of the view, or
// its top — even when the units it passes, and the ones above it, measure
// differently from their estimates on the way; then it lets go of the
// scroll.
func TestJumpArrivesAtItsTarget(t *testing.T) {
	for _, tc := range []struct {
		name   string
		target int32
		top    bool
		from   float32
		// atStart: the target opens the window, and the scroll range
		// ends above it, so it sits at the view's top.
		atStart bool
	}{
		{"up, centred", 220, false, -1, false},
		{"up past the window, as far as the range goes", 3, false, -1, true},
		{"down, to the top", 250, true, 1000, false},
	} {
		t.Run(tc.name, func(t *testing.T) {
			m := longModel(400)
			truth := map[int32]float32{}
			for i := range 400 {
				truth[int32(i)] = float32(40 + (i*37)%90)
			}
			host := newSimHost(truth)
			var st State
			st.SetWindow(200)
			if tc.from >= 0 {
				st.SetFollow(false)
				host.offset = tc.from
			}
			for range 4 {
				host.frame(t, &st, m, constEstimate(60))
			}
			if tc.top {
				st.ScrollToStart(tc.target)
			} else {
				st.JumpTo(tc.target)
			}
			frames := 0
			for frames = 1; frames <= seekFrames+5; frames++ {
				host.frame(t, &st, m, constEstimate(60))
				if st.seek == 0 {
					break
				}
			}
			assert.Equal(t, int32(0), st.seek, "the jump let go after %d frames", frames)
			assert.False(t, st.Follow())
			// The frame after it let go lays the target out where it arrived.
			host.frame(t, &st, m, constEstimate(60))
			y, ok := host.screenY(tc.target)
			require.True(t, ok, "the target is laid out")
			want := host.viewH / 2
			switch {
			case tc.atStart:
				want = 0
				assert.Equal(t, 400-int(tc.target), st.Window(), "the window widened to the target")
			case tc.top:
				want = host.sp
			}
			assert.InDelta(t, want, y, 1, "the target sits where the jump put it")
			t.Logf("arrived in %d frames", frames)
		})
	}
}

// A correction moves by the change, within the scroll range, and takes out
// of the space above no more than there is.
func TestCorrection(t *testing.T) {
	delta, absorb, ok := correction(40, 1000, 600, 5000, 800)
	assert.True(t, ok)
	assert.Equal(t, float32(40), delta)
	assert.Equal(t, float32(40), absorb)

	delta, absorb, ok = correction(40, 1000, 600, 5000, 10)
	assert.True(t, ok)
	assert.Equal(t, float32(40), delta)
	assert.Equal(t, float32(10), absorb, "only as much as stands in above")

	_, _, ok = correction(0.2, 1000, 600, 5000, 800)
	assert.False(t, ok, "below a pixel's half is not worth a scroll")

	delta, absorb, ok = correction(-50, 30, 600, 5000, 0)
	assert.True(t, ok)
	assert.Equal(t, float32(-30), delta, "the scroll range ends at the top")
	assert.Equal(t, float32(-30), absorb)

	delta, absorb, ok = correction(60, 4370, 600, 5000, 900)
	assert.True(t, ok)
	assert.Equal(t, float32(30), delta, "and at the bottom")
	assert.Equal(t, float32(30), absorb)
}

// A frame that scrolled to a cursor in its own layout — following the tail,
// or a jump — already holds what that layout changed; the frame after it
// does not correct for it again, even when the tail has been let go.
func TestNoCorrectionForAFrameThatFollowed(t *testing.T) {
	m := longModel(200)
	truth := uniform(200, 60)
	host := newSimHost(truth)
	var st State
	for range 8 {
		host.frame(t, &st, m, constEstimate(60))
	}
	truth[185] = 120 // above the viewport, inside the margin
	host.frame(t, &st, m, constEstimate(60))
	st.SetFollow(false)
	for range 8 {
		fp := host.frame(t, &st, m, constEstimate(60))
		assert.False(t, fp.correct)
	}
	y, ok := host.screenY(199)
	require.True(t, ok)
	assert.InDelta(t, host.viewH-60, y, 0.5, "the tail stays where following left it")
}

// A message's key stays with it when a marker is inserted before it — the
// "new since you looked" line — and when its time changes, as a pending
// reply's does; it is the host's own when the model carries one.
func TestMessageKeys(t *testing.T) {
	m := longModel(6)
	before := make([]uint64, m.Len())
	for i := range before {
		before[i] = m.key(i)
	}
	// A marker inserted before message 3, at its time, as the chat app does.
	ins := func(s []int64, v int64) []int64 { return append(s[:3:3], append([]int64{v}, s[3:]...)...) }
	m.TimeMS = ins(m.TimeMS, m.TimeMS[3])
	m.Sender = append(m.Sender[:3:3], append([]int32{-1}, m.Sender[3:]...)...)
	m.Body = append(m.Body[:3:3], append([]string{"new since you looked"}, m.Body[3:]...)...)
	m.ReplyTo = append(m.ReplyTo[:3:3], append([]int32{-1}, m.ReplyTo[3:]...)...)
	m.Flags = append(m.Flags[:3:3], append([]FlagsE{FlagSystem | FlagMarker}, m.Flags[3:]...)...)
	m.Status = append(m.Status[:3:3], append([]StatusE{0}, m.Status[3:]...)...)
	require.NoError(t, m.Validate())
	for i := range 3 {
		assert.Equal(t, before[i], m.key(i))
	}
	for i := 3; i < len(before); i++ {
		assert.Equal(t, before[i], m.key(i+1), "message %d keeps its key one ordinal on", i)
	}
	seen := map[uint64]bool{}
	for i := range m.Len() {
		assert.False(t, seen[m.key(i)], "key of %d is distinct", i)
		seen[m.key(i)] = true
	}
	// The units of a plan carry the same keys.
	rows := plan(m, time.UTC, 2)
	for _, u := range unitsOf(m, rows, nil) {
		assert.Equal(t, m.key(int(u.ord)), u.key)
	}

	// A pending reply carries the current time; its key does not move.
	last := m.Len() - 1
	k := m.key(last)
	m.TimeMS[last] += 1500
	assert.Equal(t, k, m.key(last))

	m.Key = make([]uint64, m.Len())
	for i := range m.Key {
		m.Key[i] = uint64(100 + i)
	}
	assert.Equal(t, uint64(101), m.key(1))
	assert.Equal(t, uint64(102), unitsOf(m, plan(m, time.UTC, 0), nil)[2].key)
	m.Key = []uint64{7}
	assert.Error(t, m.Validate())
}

// The current row is the newest message while following — not the marker
// after it — and the one at the viewport's top otherwise.
func TestCurrentUnit(t *testing.T) {
	m := longModel(5)
	m.Flags[4] = FlagSystem | FlagMarker
	m.Sender[4] = -1
	rows := plan(m, time.UTC, 0)
	units := unitsOf(m, rows, nil)
	hs := []float32{100, 100, 100, 100, 100}
	all := []bool{true, true, true, true, true}
	assert.Equal(t, 3, currentUnit(m, units, hs, all, 0, true))
	assert.Equal(t, 0, currentUnit(m, units, hs, all, 0, false))
	assert.Equal(t, 1, currentUnit(m, units, hs, all, 150, false), "the unit the viewport's top cuts")
	assert.Equal(t, 2, currentUnit(m, units, hs, all, 200, false), "a unit that ends at the top is above it")
	some := []bool{false, false, true, true, false}
	assert.Equal(t, 2, currentUnit(m, units, hs, some, 50, false), "only a laid-out unit is current")
	assert.Equal(t, -1, currentUnit(m, units, hs, make([]bool, 5), 0, false))
}

// While scrolling, the current row is a laid-out unit at the viewport's
// top; while following, the newest message.
func TestCurrentFollowsTheReader(t *testing.T) {
	m := longModel(200)
	host := newSimHost(uniform(200, 60))
	var st State
	for range 4 {
		host.frame(t, &st, m, constEstimate(60))
	}
	assert.Equal(t, int32(199), host.current)
	st.SetFollow(false)
	host.wheel = -300
	for range 6 {
		host.frame(t, &st, m, constEstimate(60))
	}
	host.wheel = 0
	host.frame(t, &st, m, constEstimate(60))
	host.frame(t, &st, m, constEstimate(60))
	y, ok := host.screenY(host.current)
	require.True(t, ok, "the current unit is laid out")
	assert.LessOrEqual(t, y, float32(0))
	assert.Greater(t, y+60, float32(0), "it is the unit the viewport's top cuts")
}
