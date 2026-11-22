package selector

import (
	"testing"

	"github.com/stergiotis/boxer/public/keelson/runtime/widgethandle"
	c "github.com/stergiotis/boxer/public/thestack/imzero2/egui2/bindings"
	"github.com/stergiotis/boxer/public/thestack/imzero2/egui2/egui2test"
)

type gran uint8

const (
	perRow gran = iota
	perAttr
	perCell
)

// TestCommit pins the egui radio_value change-rule the whole package rests on:
// assign (and report changed) only on a click that actually moves the value;
// a click on the already-selected value, or no click, is not a change.
func TestCommit(t *testing.T) {
	cases := []struct {
		name        string
		start       gran
		value       gran
		clicked     bool
		wantChanged bool
		wantAfter   gran
	}{
		{"click moves selection", perRow, perAttr, true, true, perAttr},
		{"click on already-selected is no change", perAttr, perAttr, true, false, perAttr},
		{"no click never assigns", perRow, perAttr, false, false, perRow},
		{"no click on matching value", perAttr, perAttr, false, false, perAttr},
	}
	for _, tc := range cases {
		t.Run(tc.name, func(t *testing.T) {
			cur := tc.start
			got := commit(&cur, tc.value, tc.clicked)
			if got != tc.wantChanged {
				t.Errorf("commit changed = %v, want %v", got, tc.wantChanged)
			}
			if cur != tc.wantAfter {
				t.Errorf("*current = %v, want %v", cur, tc.wantAfter)
			}
		})
	}
}

// TestSegmentedBuildsOptionsInOrder checks the fluent builder accumulates
// options in render order and carries icon/tooltip through OptionIcon — the
// state the render loop walks.
func TestSegmentedBuildsOptionsInOrder(t *testing.T) {
	var cur gran
	g := Segmented(nil, "granularity", &cur).
		Option(perRow, "per DB row").
		OptionIcon(perAttr, "", "per attribute", "un-pivot the leeway walk").
		Option(perCell, "per cell")

	if g.style != StyleSegmented {
		t.Fatalf("default style = %v, want StyleSegmented", g.style)
	}
	if len(g.opts) != 3 {
		t.Fatalf("opts = %d, want 3", len(g.opts))
	}
	if g.opts[0].value != perRow || g.opts[0].label != "per DB row" {
		t.Errorf("opts[0] = %+v", g.opts[0])
	}
	if g.opts[1].icon == "" || g.opts[1].tooltip == "" {
		t.Errorf("OptionIcon dropped icon/tooltip: %+v", g.opts[1])
	}
	if g.opts[2].value != perCell {
		t.Errorf("opts[2].value = %v, want perCell", g.opts[2].value)
	}
}

// TestLayoutFlags pins the layout knobs the render switch reads.
func TestLayoutFlags(t *testing.T) {
	var cur gran
	if g := Segmented(nil, "k", &cur); g.inline || g.vertical || g.frameless || g.gap != 0 {
		t.Errorf("defaults not all zero: %+v", g)
	}
	if g := Segmented(nil, "k", &cur).Inline(); !g.inline {
		t.Error("Inline() did not set inline")
	}
	if g := Segmented(nil, "k", &cur).Vertical(); !g.vertical {
		t.Error("Vertical() did not set vertical")
	}
	if g := Segmented(nil, "k", &cur).Frameless(); !g.frameless {
		t.Error("Frameless() did not set frameless")
	}
	if g := Segmented(nil, "k", &cur).Gap(8); g.gap != 8 {
		t.Errorf("Gap(8) = %v, want 8", g.gap)
	}
}

// TestSegmentedIdsAreScopedAndDistinct pins the id derivation: every option
// id is relative under the bar's scope, so two bars differ by scope key
// alone and options within one bar never collide (ADR-0267 W4, W5).
func TestSegmentedIdsAreScopedAndDistinct(t *testing.T) {
	var cur gran
	ids := c.NewWidgetIdStack()
	idsUnder := func(scope string) (a, b uint64) {
		g := Segmented(ids, scope, &cur).Option(perRow, "a").Option(perAttr, "b")
		for range c.IdScope(ids.PrepareStr(scope)) {
			a, b = g.idFor(0).Derive(), g.idFor(1).Derive()
		}
		return
	}
	a0, b0 := idsUnder("tab")
	a1, _ := idsUnder("other")
	if a0 == b0 {
		t.Errorf("two options of one bar share id %#x", a0)
	}
	if a0 == a1 {
		t.Errorf("the same option under two scopes shares id %#x", a0)
	}
}

// TestStyleDefaults documents the two entry points' default skins.
func TestStyleDefaults(t *testing.T) {
	var cur gran
	if rv := RadioValue(nil, &cur, perRow); rv.style != StyleRadio {
		t.Errorf("RadioValue default style = %v, want StyleRadio", rv.style)
	}
	if sg := Segmented(nil, "k", &cur); sg.style != StyleSegmented {
		t.Errorf("Segmented default style = %v, want StyleSegmented", sg.style)
	}
}

// TestJoinedIsTheFramedSegmentedStyle pins which bars take the one-outline
// look (ADR-0404 SD5): the segmented style with its frames on, whatever the
// layout knobs say.
func TestJoinedIsTheFramedSegmentedStyle(t *testing.T) {
	var cur gran
	cases := []struct {
		name string
		g    GroupFluid[gran]
		want bool
	}{
		{"default", Segmented(nil, "k", &cur), true},
		{"inline", Segmented(nil, "k", &cur).Inline(), true},
		{"vertical", Segmented(nil, "k", &cur).Vertical(), true},
		{"gap", Segmented(nil, "k", &cur).Gap(4), true},
		{"frameless", Segmented(nil, "k", &cur).Frameless(), false},
		{"selectable", Segmented(nil, "k", &cur).Style(StyleSelectable), false},
		{"radio", Segmented(nil, "k", &cur).Style(StyleRadio), false},
	}
	for _, tc := range cases {
		if got := tc.g.joined(); got != tc.want {
			t.Errorf("%s: joined = %v, want %v", tc.name, got, tc.want)
		}
	}
}

// TestEveryLayoutRendersAndAJoinedClickCommits plays one frame of each knob
// combination under the discard channel, then clicks an option of a joined
// bar, whose ids sit under the outline's frame.
func TestEveryLayoutRendersAndAJoinedClickCommits(t *testing.T) {
	t.Cleanup(egui2test.Install())
	sm := c.CurrentApplicationState.StateManager
	ids := c.NewWidgetIdStack()
	cur := perRow
	bar := func(scope string) GroupFluid[gran] {
		return Segmented(ids, scope, &cur).Option(perRow, "a").Option(perAttr, "b").Option(perCell, "c")
	}
	for _, g := range []GroupFluid[gran]{
		bar("default"), bar("inline").Inline(), bar("vertical").Vertical(), bar("gap").Gap(4),
		bar("frameless").Frameless(), bar("selectable").Style(StyleSelectable), bar("radio").Style(StyleRadio).Vertical(),
	} {
		sm.ScriptReset()
		if g.SendResp() {
			t.Errorf("a frame with no click changed the selection")
		}
	}
	var h widgethandle.WidgetHandle
	for range c.IdScope(ids.PrepareStr("default")) {
		for range c.IdScope(ids.PrepareStr("outline")) {
			h = widgethandle.Make(ids.PrepareSeq(1).Derive())
		}
	}
	sm.ScriptReset()
	sm.ScriptResponse(h, c.PrimaryClickedResponseFlags)
	if !bar("default").SendResp() || cur != perAttr {
		t.Errorf("a click on the joined bar's second option did not commit: cur = %v", cur)
	}
}
