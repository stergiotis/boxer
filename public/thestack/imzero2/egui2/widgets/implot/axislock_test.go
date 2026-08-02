package implot

import "testing"

// unlockedWindow is the pre-gesture plot area every case below starts from.
func unlockedWindow() pxWindow { return pxWindow{x0: 100, x1: 300, y0: 50, y1: 250} }

func locksFor(xflags AxisFlags, yflags AxisFlags) gestureLocks {
	return locksOf(&axisState{flags: xflags}, &axisState{flags: yflags})
}

func TestPanSkipsNoPanAxis(t *testing.T) {
	cases := []struct {
		name           string
		xflags, yflags AxisFlags
		wantX, wantY   bool
	}{
		{"free", AxisFlagsNone, AxisFlagsNone, true, true},
		{"y locked to depth", AxisFlagsNone, AxisFlagsNoPan, true, false},
		{"x locked", AxisFlagsNoPan, AxisFlagsNone, false, true},
		{"both locked", AxisFlagsLock, AxisFlagsLock, false, false},
	}
	for _, tc := range cases {
		t.Run(tc.name, func(t *testing.T) {
			w := unlockedWindow()
			movedX, movedY := locksFor(tc.xflags, tc.yflags).pan(&w, 10, -20)
			if movedX != tc.wantX || movedY != tc.wantY {
				t.Fatalf("moved = (%v,%v), want (%v,%v)", movedX, movedY, tc.wantX, tc.wantY)
			}
			if tc.wantX {
				if w.x0 != 90 || w.x1 != 290 {
					t.Errorf("x window = (%v,%v), want (90,290)", w.x0, w.x1)
				}
			} else if w.x0 != 100 || w.x1 != 300 {
				t.Errorf("locked x window moved to (%v,%v)", w.x0, w.x1)
			}
			if tc.wantY {
				if w.y0 != 70 || w.y1 != 270 {
					t.Errorf("y window = (%v,%v), want (70,270)", w.y0, w.y1)
				}
			} else if w.y0 != 50 || w.y1 != 250 {
				t.Errorf("locked y window moved to (%v,%v)", w.y0, w.y1)
			}
		})
	}
}

// A NoZoom axis must not move at all under the wheel. The anchored zoom
// shifts an axis's centre as well as its span, so a naive "restore the
// span afterwards" would leave the axis panned — this pins that it does
// not.
func TestZoomLeavesNoZoomAxisExactlyWhereItWas(t *testing.T) {
	w := unlockedWindow()
	movedX, movedY := locksFor(AxisFlagsNone, AxisFlagsNoZoom).zoom(&w, 120, 60, 2)
	if !movedX || movedY {
		t.Fatalf("moved = (%v,%v), want (true,false)", movedX, movedY)
	}
	// x scales about the anchor at 120: 100 -> 110, 300 -> 210.
	if w.x0 != 110 || w.x1 != 210 {
		t.Errorf("x window = (%v,%v), want (110,210)", w.x0, w.x1)
	}
	if w.y0 != 50 || w.y1 != 250 {
		t.Errorf("y window = (%v,%v), want it untouched at (50,250)", w.y0, w.y1)
	}
}

// The depth-axis configuration the icicle widget uses: the wheel zooms the
// value axis only, while a drag still scrolls depth.
func TestNoZoomAxisStillPans(t *testing.T) {
	locks := locksFor(AxisFlagsNone, AxisFlagsNoZoom)
	w := unlockedWindow()
	if _, movedY := locks.zoom(&w, 120, 60, 2); movedY {
		t.Fatal("wheel moved the NoZoom axis")
	}
	w = unlockedWindow()
	if _, movedY := locks.pan(&w, 0, -20); !movedY {
		t.Fatal("drag did not scroll the NoZoom axis")
	}
	if w.y0 != 70 || w.y1 != 270 {
		t.Errorf("y window = (%v,%v), want (70,270)", w.y0, w.y1)
	}
}

// A drag along one axis only must leave the other axis's range untouched
// rather than round-tripping it through the transform every frame.
func TestPanIgnoresZeroDelta(t *testing.T) {
	w := unlockedWindow()
	movedX, movedY := locksFor(AxisFlagsNone, AxisFlagsNone).pan(&w, 0, -20)
	if movedX {
		t.Error("a purely vertical drag reported x as moved")
	}
	if !movedY {
		t.Error("a vertical drag did not move y")
	}
}

// A fit rewrites a span, so it is a zoom and a NoZoom axis declines it —
// whichever gesture asked. The double-click already went through the locks;
// the context menu's fit actions are the path that did not, and the icicle's
// depth axis is the configuration that notices.
func TestRequestFitSkipsNoZoomAxis(t *testing.T) {
	cases := []struct {
		name           string
		xflags, yflags AxisFlags
		wantX, wantY   bool // which axes the action asks to fit
		fitX, fitY     bool // which end up pending
	}{
		{"fit both, free", AxisFlagsNone, AxisFlagsNone, true, true, true, true},
		{"fit both, depth locked", AxisFlagsNone, AxisFlagsNoZoom, true, true, true, false},
		{"fit both, all locked", AxisFlagsNoZoom, AxisFlagsNoZoom, true, true, false, false},
		{"fit x only", AxisFlagsNone, AxisFlagsNoZoom, true, false, true, false},
		{"fit y only, locked", AxisFlagsNone, AxisFlagsNoZoom, false, true, false, false},
		{"fit y only, free", AxisFlagsNone, AxisFlagsNone, false, true, false, true},
		// NoPan is not NoZoom: a pinned-scroll axis still fits.
		{"fit both, pan locked", AxisFlagsNoPan, AxisFlagsNoPan, true, true, true, true},
	}
	for _, tc := range cases {
		t.Run(tc.name, func(t *testing.T) {
			x, y := axisState{flags: tc.xflags}, axisState{flags: tc.yflags}
			locksOf(&x, &y).requestFit(&x, &y, tc.wantX, tc.wantY)
			if x.fitNext != tc.fitX || y.fitNext != tc.fitY {
				t.Errorf("fitNext = (%v,%v), want (%v,%v)", x.fitNext, y.fitNext, tc.fitX, tc.fitY)
			}
		})
	}
}

// A declined fit must not clear one already pending — from FitNext, or from
// another gesture resolved earlier in the same frame.
func TestRequestFitNeverClearsAPendingFit(t *testing.T) {
	x, y := axisState{flags: AxisFlagsNoZoom}, axisState{flags: AxisFlagsNoZoom}
	x.fitNext, y.fitNext = true, true
	locksOf(&x, &y).requestFit(&x, &y, true, true)
	if !x.fitNext || !y.fitNext {
		t.Errorf("fitNext = (%v,%v), want both still pending", x.fitNext, y.fitNext)
	}
}

func TestAxisFlagsLockIsBothLocks(t *testing.T) {
	if AxisFlagsLock != AxisFlagsNoPan|AxisFlagsNoZoom {
		t.Fatalf("AxisFlagsLock = %d, want NoPan|NoZoom", AxisFlagsLock)
	}
	l := locksFor(AxisFlagsLock, AxisFlagsNone)
	if !l.noPanX || !l.noZoomX {
		t.Fatalf("AxisFlagsLock did not resolve to both locks: %+v", l)
	}
	// The lock bits must not collide with the flags that predate them.
	for _, f := range []AxisFlags{AxisFlagsAutoFit, AxisFlagsNoGrid, AxisFlagsNoTickLabels, AxisFlagsFollow} {
		if f&AxisFlagsLock != 0 {
			t.Errorf("flag %d overlaps the lock bits", f)
		}
	}
}

func TestAxisLimitsReadback(t *testing.T) {
	p := NewDetached()
	if _, _, ok := p.AxisLimits(AxisX1); ok {
		t.Error("AxisLimits reported ok before the plot resolved a range")
	}
	p.st.x.rng = Range{Min: -3, Max: 7}
	p.st.y.rng = Range{Min: 0, Max: 12}
	p.st.initialized = true
	if lo, hi, ok := p.AxisLimits(AxisX1); !ok || lo != -3 || hi != 7 {
		t.Errorf("x limits = (%v,%v,%v), want (-3,7,true)", lo, hi, ok)
	}
	if lo, hi, ok := p.AxisLimits(AxisY1); !ok || lo != 0 || hi != 12 {
		t.Errorf("y limits = (%v,%v,%v), want (0,12,true)", lo, hi, ok)
	}
	var nilPlot *Plot
	if _, _, ok := nilPlot.AxisLimits(AxisX1); ok {
		t.Error("nil plot reported limits")
	}
}
