package heatmapscroll

import (
	"github.com/stergiotis/boxer/public/thestack/imzero2/egui2/widgets/graphview/scenetest"
	"math"
	"testing"

	c "github.com/stergiotis/boxer/public/thestack/imzero2/egui2/bindings"
	"github.com/stergiotis/boxer/public/thestack/imzero2/egui2/widgets/colormap"
)

func newFixture(t *testing.T, w, h uint32) *HeatmapScroll {
	t.Helper()
	cfg := colormap.NewConfig(colormap.Viridis8, 0, 1)
	ids := c.NewWidgetIdStack()
	return New(ids, "t-"+t.Name(), cfg, Options{WidthSlots: w, HeightSlots: h})
}

func TestNewPanics(t *testing.T) {
	cfg := colormap.NewConfig(colormap.Viridis8, 0, 1)
	ids := c.NewWidgetIdStack()
	cases := []struct {
		name string
		fn   func()
	}{
		{"nil ids", func() { _ = New(nil, "k", cfg, Options{WidthSlots: 4, HeightSlots: 4}) }},
		{"nil cfg", func() { _ = New(ids, "k", nil, Options{WidthSlots: 4, HeightSlots: 4}) }},
		{"zero width", func() { _ = New(ids, "k", cfg, Options{WidthSlots: 0, HeightSlots: 4}) }},
		{"zero height", func() { _ = New(ids, "k", cfg, Options{WidthSlots: 4, HeightSlots: 0}) }},
	}
	for _, tc := range cases {
		t.Run(tc.name, func(t *testing.T) {
			defer func() {
				if r := recover(); r == nil {
					t.Errorf("expected panic")
				}
			}()
			tc.fn()
		})
	}
}

func TestDefaultsAreScientific(t *testing.T) {
	hs := newFixture(t, 4, 4)
	if hs.Opts.Orientation != ScrollLeft {
		t.Errorf("default orientation: want ScrollLeft, got %v", hs.Opts.Orientation)
	}
	if hs.Opts.Filter != FilterNearest {
		t.Errorf("default filter: want FilterNearest, got %v", hs.Opts.Filter)
	}
	// The "not hovered" sentinel must be u64::MAX so the first Render
	// reports the correct initial state before any r9 push lands.
	if _, _, hovered := c.UnpackHoverRc(hs.hoverRc); hovered {
		t.Errorf("freshly-constructed widget should report unhovered")
	}
}

func TestPushColumnAccumulates(t *testing.T) {
	const w, h = uint32(8), uint32(4)
	hs := newFixture(t, w, h)
	col := []float32{0.0, 0.25, 0.5, 1.0}

	for range 3 {
		stats := hs.PushColumn(col)
		if stats != (colormap.ColumnStats{}) {
			t.Errorf("in-range column should have empty stats, got %+v", stats)
		}
	}
	if hs.pendingCount != 3 {
		t.Errorf("pendingCount: want 3, got %d", hs.pendingCount)
	}
	if got, want := len(hs.pending), 3*int(h); got != want {
		t.Errorf("pending length: want %d, got %d", want, got)
	}
	// Verify each column's mapping is placed at the expected offset.
	for i := range 3 {
		if hs.pending[i*int(h)+0] != colormap.Viridis8[0] {
			t.Errorf("col %d row 0: want %08x, got %08x",
				i, colormap.Viridis8[0], hs.pending[i*int(h)+0])
		}
		last := colormap.Viridis8[len(colormap.Viridis8)-1]
		if hs.pending[i*int(h)+int(h)-1] != last {
			t.Errorf("col %d row H-1: want %08x, got %08x",
				i, last, hs.pending[i*int(h)+int(h)-1])
		}
	}
}

func TestPushColumnWrongSizePanics(t *testing.T) {
	hs := newFixture(t, 4, 4)
	defer func() {
		if r := recover(); r == nil {
			t.Errorf("expected panic on wrong-sized sample slice")
		}
	}()
	hs.PushColumn([]float32{0, 0, 0}) // len 3 ≠ heightSlots 4
}

func TestTotalStatsAccumulates(t *testing.T) {
	cfg := colormap.NewConfig(colormap.Viridis8, 0, 1)
	ids := c.NewWidgetIdStack()
	hs := New(ids, "ts", cfg, Options{WidthSlots: 4, HeightSlots: 3})

	bad := []float32{float32(math.NaN()), 0.5, 0.5} // 1 bad
	under := []float32{-1, -2, -3}                  // 3 underflow
	over := []float32{2, 3, 4}                      // 3 overflow
	fine := []float32{0.1, 0.5, 0.9}                // 0
	hs.PushColumn(bad)
	hs.PushColumn(under)
	hs.PushColumn(over)
	hs.PushColumn(fine)

	total := hs.TotalStats()
	want := colormap.ColumnStats{BadSamples: 1, Underflow: 3, Overflow: 3}
	if total != want {
		t.Errorf("TotalStats: want %+v, got %+v", want, total)
	}

	hs.ResetTotalStats()
	if hs.TotalStats() != (colormap.ColumnStats{}) {
		t.Errorf("ResetTotalStats should zero the counter")
	}
}

func TestUnpackHoverRc(t *testing.T) {
	cases := []struct {
		name    string
		packed  uint64
		wantRow uint32
		wantCol uint32
		wantOk  bool
	}{
		{"sentinel", ^uint64(0), 0, 0, false},
		{"zero", 0, 0, 0, true},
		{"row only", uint64(17) << 32, 17, 0, true},
		{"col only", 42, 0, 42, true},
		{"both", (uint64(5) << 32) | 7, 5, 7, true},
	}
	for _, tc := range cases {
		t.Run(tc.name, func(t *testing.T) {
			row, col, ok := c.UnpackHoverRc(tc.packed)
			if row != tc.wantRow || col != tc.wantCol || ok != tc.wantOk {
				t.Errorf("UnpackHoverRc(%016x) = (%d, %d, %v), want (%d, %d, %v)",
					tc.packed, row, col, ok, tc.wantRow, tc.wantCol, tc.wantOk)
			}
		})
	}
}

func TestSetters(t *testing.T) {
	hs := newFixture(t, 4, 4)
	cfg2 := colormap.NewConfig(colormap.Magma8, 0, 10)
	hs.SetConfig(cfg2)
	if hs.cfg != cfg2 {
		t.Errorf("SetConfig failed to update cfg")
	}
	defer func() {
		if r := recover(); r == nil {
			t.Errorf("SetConfig(nil) should panic")
		}
	}()
	hs.SetConfig(nil)
}

func TestSize(t *testing.T) {
	hs := newFixture(t, 17, 29)
	w, h := hs.Size()
	if w != 17 || h != 29 {
		t.Errorf("Size: want (17, 29), got (%d, %d)", w, h)
	}
}

// TestRenderHeadless renders one frame without a host: the widget must not
// panic, must report a quiet frame, and a ring-shape change through Opts must
// restart the ring.
func TestRenderHeadless(t *testing.T) {
	t.Cleanup(scenetest.Install())
	hs := newFixture(t, 4, 3)
	hs.PushColumn([]float32{0.1, 0.5, 0.9})
	ev := hs.Render(0, 0)
	if ev.Hovered || ev.Clicked {
		t.Fatalf("quiet frame reported %+v", ev)
	}
	if hs.Head() != 1 {
		t.Fatalf("head = %d after one column, want 1", hs.Head())
	}
	hs.Opts.WidthSlots = 8
	hs.RenderFill(100, 50)
	if hs.Head() != 0 {
		t.Fatalf("a ring-shape change did not restart the ring: head = %d", hs.Head())
	}
	if w, _ := hs.Size(); w != 8 {
		t.Fatalf("Size follows Opts: got %d", w)
	}
}
