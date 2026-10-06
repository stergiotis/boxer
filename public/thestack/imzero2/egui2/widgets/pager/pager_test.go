package pager

import (
	"testing"

	c "github.com/stergiotis/boxer/public/thestack/imzero2/egui2/bindings"
	"github.com/stergiotis/boxer/public/thestack/imzero2/egui2/widgets/graphview/scenetest"
)

// TestRenderHeadless renders one frame of a pager without a host: the widget
// must not panic, a quiet frame reports no change, and a programmatic page
// move is the next frame's change.
func TestRenderHeadless(t *testing.T) {
	t.Cleanup(scenetest.Install())
	ids := c.NewWidgetIdStack()
	sm := c.CurrentApplicationState.StateManager
	p := New(ids, "t", Options{PageSize: 10, Unit: "items"})
	p.Configure(95)
	if ev := p.Render(); ev.Changed {
		t.Fatalf("quiet frame reported a change: %+v", ev)
	}
	sm.ScriptReset() // the frame boundary
	if n := p.NumPages(); n != 10 {
		t.Fatalf("NumPages = %d, want 10", n)
	}
	// A programmatic move is the host's own doing, so Render does not report
	// it as a change; the range moves.
	p.GoToLast()
	p.Render()
	sm.ScriptReset()
	if start, end := p.Range(); start != 90 || end != 95 {
		t.Fatalf("Range = [%d,%d), want [90,95)", start, end)
	}
	// Options are re-read every frame.
	p.Opts.HideSizeCombo = true
	p.Render()
}

// SetPageSize keeps the row that was mid-page in view, clamps to the last
// page, and ignores a size that is not positive.
func TestSetPageSizeKeepsTheMiddleRowInView(t *testing.T) {
	p := New(c.NewWidgetIdStack(), "t", Options{PageSize: 10})
	p.Configure(95)
	p.GoToIndex(45) // page 4: rows 40–49, middle row 45
	p.SetPageSize(24)
	if start, end := p.Range(); start > 45 || end <= 45 {
		t.Fatalf("range %d–%d lost row 45", start, end)
	}
	if p.PageSize() != 24 || p.NumPages() != 4 {
		t.Fatalf("size %d, pages %d; want 24 and 4", p.PageSize(), p.NumPages())
	}
	p.SetPageSize(0)
	if p.PageSize() != 24 {
		t.Fatalf("a zero size changed the page size to %d", p.PageSize())
	}
	p.GoToLast()
	p.SetPageSize(48)
	if p.CurrentPage() != p.NumPages()-1 {
		t.Fatalf("page %d past the last %d", p.CurrentPage(), p.NumPages()-1)
	}
}
