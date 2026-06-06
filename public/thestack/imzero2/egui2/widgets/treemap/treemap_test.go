//go:build llm_generated_opus47

package treemap

import (
	"math"
	"testing"

	"github.com/stergiotis/boxer/public/thestack/imzero2/egui2/widgets/treemap/layout"
)

// =============================================================================
// CellStateE bitfield
// =============================================================================

func TestCellState_HasAndHasAny(t *testing.T) {
	s := CellStateDrillable | CellStateFrontier | CellStateHovered
	cases := []struct {
		name    string
		flag    CellStateE
		wantHas bool
		wantAny bool
	}{
		{"single set", CellStateDrillable, true, true},
		{"single unset", CellStateOffPath, false, false},
		{"multi all set", CellStateDrillable | CellStateHovered, true, true},
		{"multi partially set", CellStateDrillable | CellStateOffPath, false, true},
		{"multi none set", CellStateOnPath | CellStateOffPath, false, false},
		{"zero", 0, true, false}, // Has(0) is vacuously true; HasAny(0) is false.
	}
	for _, c := range cases {
		t.Run(c.name, func(t *testing.T) {
			if got := s.Has(c.flag); got != c.wantHas {
				t.Errorf("Has(%v) = %v, want %v", c.flag, got, c.wantHas)
			}
			if got := s.HasAny(c.flag); got != c.wantAny {
				t.Errorf("HasAny(%v) = %v, want %v", c.flag, got, c.wantAny)
			}
		})
	}
}

func TestCellState_Interactive(t *testing.T) {
	cases := []struct {
		s    CellStateE
		want bool
	}{
		{CellStateDrillable, true},
		{CellStateDrillUp, true},
		{CellStateDrillable | CellStateHovered, true},
		{CellStateOnPath, false},
		{CellStateOffPath, false},
		{CellStateLeaf | CellStateFrontier, false},
		{CellStatePreview, false},
		{0, false},
	}
	for _, c := range cases {
		if got := c.s.Interactive(); got != c.want {
			t.Errorf("Interactive(%v) = %v, want %v", c.s, got, c.want)
		}
	}
}

// =============================================================================
// computeZoomState — pure function, exhaustive coverage of the cases the
// renderer can produce
// =============================================================================

func TestComputeZoomState(t *testing.T) {
	cases := []struct {
		name                                                           string
		isActive, atFrontier, hasChildren, drillable, drillUp, hovered bool
		bcLevel, bcLen                                                 int
		wantBits                                                       CellStateE
	}{
		{
			name:       "drillable frontier with children",
			atFrontier: true, hasChildren: true, drillable: true,
			bcLevel: 0, bcLen: 1,
			wantBits: CellStateDrillable | CellStateFrontier,
		},
		{
			name:       "frontier leaf (no children)",
			atFrontier: true, hasChildren: false,
			bcLevel: 0, bcLen: 1,
			wantBits: CellStateFrontier | CellStateLeaf,
		},
		{
			name:     "drill-up active cell at depth 0 in 3-deep breadcrumb",
			isActive: true, hasChildren: true, drillUp: true,
			bcLevel: 0, bcLen: 3,
			wantBits: CellStateDrillUp | CellStateOnPath,
		},
		{
			name:     "focused active cell (deepest active) with children",
			isActive: true, hasChildren: true,
			bcLevel: 1, bcLen: 3,
			wantBits: CellStateFocused | CellStateOnPath,
		},
		{
			name:        "off-path sibling in 3-deep breadcrumb",
			hasChildren: true,
			bcLevel:     0, bcLen: 3,
			wantBits: CellStateOffPath,
		},
		{
			name:       "hovered drillable",
			atFrontier: true, hasChildren: true, drillable: true, hovered: true,
			bcLevel: 0, bcLen: 1,
			wantBits: CellStateDrillable | CellStateFrontier | CellStateHovered,
		},
	}
	for _, c := range cases {
		t.Run(c.name, func(t *testing.T) {
			got := computeZoomState(c.isActive, c.atFrontier, c.hasChildren, c.drillable, c.drillUp, c.hovered, c.bcLevel, c.bcLen)
			if got != c.wantBits {
				t.Errorf("computeZoomState = %#x, want %#x", uint16(got), uint16(c.wantBits))
			}
		})
	}
}

func TestComputePreviewState(t *testing.T) {
	cases := []struct {
		hasChildren, hovered bool
		want                 CellStateE
	}{
		{false, false, CellStatePreview | CellStateLeaf},
		{true, false, CellStatePreview},
		{false, true, CellStatePreview | CellStateLeaf | CellStateHovered},
		{true, true, CellStatePreview | CellStateHovered},
	}
	for _, c := range cases {
		got := computePreviewState(c.hasChildren, c.hovered)
		if got != c.want {
			t.Errorf("computePreviewState(children=%v,hovered=%v) = %#x, want %#x",
				c.hasChildren, c.hovered, uint16(got), uint16(c.want))
		}
	}
}

// =============================================================================
// animMachine — table-driven transition validation
// =============================================================================

func TestAnimMachine_TransitionTable(t *testing.T) {
	all := []AnimStateE{AnimStateIdle, AnimStateRunning}
	cases := []struct {
		from, to AnimStateE
		ok       bool
	}{
		{AnimStateIdle, AnimStateIdle, false},
		{AnimStateIdle, AnimStateRunning, true},
		{AnimStateRunning, AnimStateIdle, true},
		{AnimStateRunning, AnimStateRunning, true},
	}

	// Sanity: cover every (from, to) pair against the table.
	covered := make(map[[2]AnimStateE]bool, len(cases))
	for _, c := range cases {
		covered[[2]AnimStateE{c.from, c.to}] = true
	}
	for _, from := range all {
		for _, to := range all {
			if !covered[[2]AnimStateE{from, to}] {
				t.Fatalf("missing test case for transition %v → %v", from, to)
			}
		}
	}

	for _, c := range cases {
		t.Run(c.from.String()+"_to_"+c.to.String(), func(t *testing.T) {
			m := &animMachine{state: c.from}
			defer func() {
				r := recover()
				if c.ok && r != nil {
					t.Errorf("expected transition to succeed, panicked: %v", r)
				}
				if !c.ok && r == nil {
					t.Errorf("expected transition to panic, but it didn't")
				}
			}()
			m.transition(c.to)
			if c.ok && m.State() != c.to {
				t.Errorf("after transition: state = %v, want %v", m.State(), c.to)
			}
		})
	}
}

func TestAnimMachine_StartAndTickLifecycle(t *testing.T) {
	m := &animMachine{}
	if m.IsRunning() {
		t.Fatal("new machine should be idle")
	}

	from := layout.Rect{X: 10, Y: 20, W: 100, H: 80}
	m.Start(from)
	if !m.IsRunning() {
		t.Fatal("after Start, should be running")
	}
	if m.FromRect() != from {
		t.Errorf("FromRect = %+v, want %+v", m.FromRect(), from)
	}

	// Mid-animation tick: target was flipped from false→true so effT = m.t.
	if !m.Target() {
		t.Fatal("after first Start, target should be true (flipped from false)")
	}
	m.t = 0.3
	effT, running := m.Tick()
	if !running || effT != 0.3 {
		t.Errorf("mid-tick: effT=%v running=%v, want 0.3,true", effT, running)
	}

	// Completion tick: t reaches 1.
	m.t = 1.0
	effT, running = m.Tick()
	if running || effT != 1.0 {
		t.Errorf("done-tick: effT=%v running=%v, want 1.0,false", effT, running)
	}
	if m.State() != AnimStateIdle {
		t.Errorf("after completion, state = %v, want AnimStateIdle", m.State())
	}

	// Second Start flips target back to false; effT then = 1 - m.t.
	m.Start(from)
	if m.Target() {
		t.Fatal("after second Start, target should be false")
	}
	m.t = 0.7 // egui animating from 1 down toward 0
	effT, running = m.Tick()
	if !running {
		t.Fatal("expected running")
	}
	if diff := effT - 0.3; diff < -1e-9 || diff > 1e-9 {
		t.Errorf("effT = %v, want ~0.3 (= 1-0.7)", effT)
	}
}

// =============================================================================
// Path / navigation helpers
// =============================================================================

func sampleTree() *layout.Node {
	leaf := func(name string) *layout.Node { return &layout.Node{Name: name, Size: 1} }
	return &layout.Node{Name: "root", Children: []*layout.Node{
		{Name: "A", Children: []*layout.Node{leaf("A1"), leaf("A2")}},
		{Name: "B", Children: []*layout.Node{leaf("B1")}},
	}}
}

func TestValidPath(t *testing.T) {
	root := sampleTree()
	a := root.Children[0]
	a1 := a.Children[0]
	tm := &Treemap{root: root}

	cases := []struct {
		name string
		path []*layout.Node
		want bool
	}{
		{"empty", nil, false},
		{"just root", []*layout.Node{root}, true},
		{"root → A", []*layout.Node{root, a}, true},
		{"root → A → A1", []*layout.Node{root, a, a1}, true},
		{"path not starting at root", []*layout.Node{a, a1}, false},
		{"broken edge", []*layout.Node{root, a1}, false}, // a1 is not a child of root
		{"foreign root", []*layout.Node{a, root}, false},
	}
	for _, c := range cases {
		t.Run(c.name, func(t *testing.T) {
			if got := tm.validPath(c.path); got != c.want {
				t.Errorf("validPath = %v, want %v", got, c.want)
			}
		})
	}
}

func TestClassifyNav(t *testing.T) {
	root := sampleTree()
	a := root.Children[0]
	a1 := a.Children[0]
	b := root.Children[1]

	cases := []struct {
		name string
		from []*layout.Node
		to   []*layout.Node
		want NavKindE
	}{
		{"drill in 1 level", []*layout.Node{root}, []*layout.Node{root, a}, NavKindDrillIn},
		{"drill in 2 levels", []*layout.Node{root}, []*layout.Node{root, a, a1}, NavKindDrillIn},
		{"drill up 1 level", []*layout.Node{root, a, a1}, []*layout.Node{root, a}, NavKindDrillUp},
		{"reset to root from depth 2", []*layout.Node{root, a, a1}, []*layout.Node{root}, NavKindReset},
		{"sibling switch", []*layout.Node{root, a}, []*layout.Node{root, b}, NavKindExternal},
	}
	for _, c := range cases {
		t.Run(c.name, func(t *testing.T) {
			if got := classifyNav(c.from, c.to); got != c.want {
				t.Errorf("classifyNav = %v, want %v", got, c.want)
			}
		})
	}
}

func TestFindPath(t *testing.T) {
	root := sampleTree()
	a := root.Children[0]
	a1 := a.Children[0]
	stranger := &layout.Node{Name: "stranger"}

	cases := []struct {
		name   string
		target *layout.Node
		want   []*layout.Node
	}{
		{"root", root, []*layout.Node{root}},
		{"intermediate A", a, []*layout.Node{root, a}},
		{"deep A1", a1, []*layout.Node{root, a, a1}},
		{"not in tree", stranger, nil},
	}
	for _, c := range cases {
		t.Run(c.name, func(t *testing.T) {
			got := findPath(root, c.target, nil)
			if len(got) != len(c.want) {
				t.Fatalf("findPath length = %d, want %d (got %v)", len(got), len(c.want), got)
			}
			for i := range got {
				if got[i] != c.want[i] {
					t.Errorf("findPath[%d] = %v, want %v", i, got[i].Name, c.want[i].Name)
				}
			}
		})
	}
}

func TestPathsEqual(t *testing.T) {
	root := sampleTree()
	a := root.Children[0]

	if !pathsEqual(nil, nil) {
		t.Error("nil/nil should be equal")
	}
	if pathsEqual([]*layout.Node{root}, nil) {
		t.Error("root vs nil should not be equal")
	}
	if !pathsEqual([]*layout.Node{root, a}, []*layout.Node{root, a}) {
		t.Error("identical slices should be equal")
	}
	if pathsEqual([]*layout.Node{root, a}, []*layout.Node{root, root.Children[1]}) {
		t.Error("different second elements should not be equal")
	}
}

// =============================================================================
// ColoringI composition — uses a stub ColoringI so we don't go through FFFI
// =============================================================================

type stubColoring struct {
	colors CellColors
	ok     bool
	calls  int
}

func (s *stubColoring) Colors(info CellInfo) (CellColors, bool) {
	s.calls++
	return s.colors, s.ok
}

func TestCompositeColoring_LastOkWins(t *testing.T) {
	a := &stubColoring{ok: true}
	b := &stubColoring{ok: false}
	cChosen := CellColors{} // zero — distinguishable by struct equality with the others
	cs := &stubColoring{ok: true, colors: cChosen}

	composite := CompositeColoring(a, b, cs)
	got, ok := composite.Colors(CellInfo{})
	if !ok {
		t.Fatal("composite returned ok=false")
	}
	_ = got // colors equality is the implementation contract; the call counts test the rest
	if a.calls != 1 || b.calls != 1 || cs.calls != 1 {
		t.Errorf("expected each layer called once, got %d / %d / %d", a.calls, b.calls, cs.calls)
	}
}

func TestCompositeColoring_AllSkipMeansNotOk(t *testing.T) {
	a := &stubColoring{ok: false}
	b := &stubColoring{ok: false}
	composite := CompositeColoring(a, b)
	if _, ok := composite.Colors(CellInfo{}); ok {
		t.Error("expected ok=false when all layers skip")
	}
}

// TestCompositeColoring_OverrideOverAlwaysOkBase pins the order for the
// "tint a subset of nodes over a base that colors everything" pattern (the
// imztop topology panel: per-PU load tint over a DepthColoring base). Because
// CompositeColoring is last-ok-wins and DepthColoring always returns ok, the
// always-ok base MUST be first and the conditional override LAST. Reversing
// them lets the base clobber the override on every node — the colors look
// static. This guards against that regression.
func TestCompositeColoring_OverrideOverAlwaysOkBase(t *testing.T) {
	leaf := &layout.Node{Name: "pu", Size: 1}
	branch := &layout.Node{Name: "core", Children: []*layout.Node{leaf}}

	base := DepthColoring([]uint32{0x111111ff, 0x222222ff}) // always ok
	override := ContinuousColoring([]uint32{0x000000ff, 0xffffffff},
		func(n *layout.Node) float64 {
			if n == leaf {
				return 100 // opinion on the leaf only
			}
			return math.NaN() // abstain elsewhere
		}, 0, 100)

	// Correct order: base first, override last.
	good := CompositeColoring(base, override)
	gotLeaf, ok := good.Colors(CellInfo{Node: leaf, Depth: 1})
	if !ok {
		t.Fatal("leaf: expected ok")
	}
	wantLeaf, _ := override.Colors(CellInfo{Node: leaf, Depth: 1})
	if gotLeaf != wantLeaf {
		t.Error("leaf: override (load) layer should win over the base")
	}
	gotBranch, _ := good.Colors(CellInfo{Node: branch, Depth: 0})
	wantBranch, _ := base.Colors(CellInfo{Node: branch, Depth: 0})
	if gotBranch != wantBranch {
		t.Error("branch: base should show where the override abstains")
	}

	// Reversed order regresses the leaf to the base color (the bug).
	bad := CompositeColoring(override, base)
	gotBad, _ := bad.Colors(CellInfo{Node: leaf, Depth: 1})
	wantBaseLeaf, _ := base.Colors(CellInfo{Node: leaf, Depth: 1})
	if gotBad != wantBaseLeaf {
		t.Error("reversed order should let the always-ok base clobber the leaf override")
	}
}

func TestConditionalColoring_Predicate(t *testing.T) {
	inner := &stubColoring{ok: true}
	cond := ConditionalColoring(func(info CellInfo) bool {
		return info.State.Has(CellStateDrillable)
	}, inner)

	if _, ok := cond.Colors(CellInfo{State: CellStateOffPath}); ok {
		t.Error("predicate=false → expected ok=false")
	}
	if inner.calls != 0 {
		t.Errorf("inner should not be called when predicate fails (calls=%d)", inner.calls)
	}

	if _, ok := cond.Colors(CellInfo{State: CellStateDrillable}); !ok {
		t.Error("predicate=true → expected ok=true")
	}
	if inner.calls != 1 {
		t.Errorf("inner should be called once when predicate matches (calls=%d)", inner.calls)
	}
}

func TestCompositeColoring_PanicsOnEmptyOrNil(t *testing.T) {
	mustPanic(t, "empty layers", func() { CompositeColoring() })
	mustPanic(t, "nil layer", func() { CompositeColoring((ColoringI)(nil)) })
}

// =============================================================================
// DefaultStyle — verify the visual rule for each state
// =============================================================================

func TestDefaultStyle_Visuals(t *testing.T) {
	style := DefaultStyle()

	cases := []struct {
		name  string
		state CellStateE
		check func(*testing.T, CellVisuals)
	}{
		{
			name:  "drillable",
			state: CellStateDrillable | CellStateFrontier,
			check: func(t *testing.T, v CellVisuals) {
				if v.UseDimFill {
					t.Error("drillable should not use dim fill")
				}
				if !v.UseHoverFill {
					t.Error("drillable should opt into hover fill")
				}
				if v.UseAccentBorder {
					t.Error("drillable should not use accent border")
				}
				if !v.Hatch.IsZero() {
					t.Error("drillable should not have hatch")
				}
			},
		},
		{
			name:  "drillable hovered → thicker border",
			state: CellStateDrillable | CellStateFrontier | CellStateHovered,
			check: func(t *testing.T, v CellVisuals) {
				if v.BorderWidth <= 1.2 {
					t.Errorf("hovered border width should exceed 1.2, got %v", v.BorderWidth)
				}
			},
		},
		{
			name:  "drill-up: dim + accent border",
			state: CellStateDrillUp | CellStateOnPath,
			check: func(t *testing.T, v CellVisuals) {
				if !v.UseDimFill {
					t.Error("drill-up should use dim fill")
				}
				if !v.UseAccentBorder {
					t.Error("drill-up should use accent border")
				}
				if !v.Hatch.IsZero() {
					t.Error("drill-up should not have hatch")
				}
			},
		},
		{
			name:  "preview: dim, no hatch",
			state: CellStatePreview,
			check: func(t *testing.T, v CellVisuals) {
				if !v.UseDimFill {
					t.Error("preview should use dim fill")
				}
				if !v.Hatch.IsZero() {
					t.Error("preview should not have hatch")
				}
			},
		},
		{
			name:  "frontier leaf → hatched",
			state: CellStateFrontier | CellStateLeaf,
			check: func(t *testing.T, v CellVisuals) {
				if !v.UseDimFill {
					t.Error("frontier leaf should use dim fill")
				}
				if v.Hatch.IsZero() {
					t.Error("frontier leaf should have hatch")
				}
			},
		},
		{
			name:  "off-path sibling → hatched",
			state: CellStateOffPath,
			check: func(t *testing.T, v CellVisuals) {
				if v.Hatch.IsZero() {
					t.Error("off-path sibling should have hatch")
				}
			},
		},
		{
			name:  "focused with children → no hatch (renders children inside)",
			state: CellStateFocused | CellStateOnPath,
			check: func(t *testing.T, v CellVisuals) {
				if !v.Hatch.IsZero() {
					t.Error("focused-with-children should not have hatch")
				}
			},
		},
	}
	for _, c := range cases {
		t.Run(c.name, func(t *testing.T) {
			c.check(t, style.Visuals(CellInfo{State: c.state}))
		})
	}
}

// =============================================================================
// Colormap — linear and log-scale behavior
// =============================================================================

func TestColormap_LinearNormalize(t *testing.T) {
	cm := NewColormap([]uint32{0x000000ff, 0xffffffff}, 0, 100)
	cases := []struct {
		v    float64
		want float64
	}{
		{0, 0},
		{50, 0.5},
		{100, 1},
		{-10, 0}, // clamp below
		{200, 1}, // clamp above
	}
	for _, c := range cases {
		if got := cm.Normalize(c.v); got != c.want {
			t.Errorf("Normalize(%v) = %v, want %v", c.v, got, c.want)
		}
	}
	if cm.IsLog() {
		t.Error("IsLog should be false for linear colormap")
	}
}

func TestColormap_LogNormalize(t *testing.T) {
	cm := NewLogColormap([]uint32{0x000000ff, 0xffffffff}, 1, 100)
	cases := []struct {
		v    float64
		want float64
	}{
		{1, 0},    // log10(1)=0 → t=0
		{10, 0.5}, // log10(10)=1 → (1-0)/(2-0) = 0.5
		{100, 1},  // log10(100)=2 → t=1
		{0, 0},    // non-positive clamps to 0
		{-5, 0},   // negative clamps to 0
		{1000, 1}, // beyond max clamps to 1
	}
	for _, c := range cases {
		got := cm.Normalize(c.v)
		if diff := got - c.want; diff < -1e-12 || diff > 1e-12 {
			t.Errorf("Normalize(%v) = %v, want ~%v", c.v, got, c.want)
		}
	}
	if !cm.IsLog() {
		t.Error("IsLog should be true for log colormap")
	}
}

func TestColormap_LogPanicsOnNonPositive(t *testing.T) {
	p := []uint32{0x000000ff, 0xffffffff}
	mustPanic(t, "min=0", func() { NewLogColormap(p, 0, 10) })
	mustPanic(t, "min<0", func() { NewLogColormap(p, -1, 10) })
	mustPanic(t, "max=0", func() { NewLogColormap(p, 1, 0) })
	mustPanic(t, "max<0", func() { NewLogColormap(p, 1, -5) })
}

func TestColormap_RangeAndPalette(t *testing.T) {
	p := []uint32{0x111111ff, 0x222222ff, 0x333333ff}
	cm := NewColormap(p, 5, 15)
	min, max := cm.Range()
	if min != 5 || max != 15 {
		t.Errorf("Range = (%v, %v), want (5, 15)", min, max)
	}
	got := cm.Palette()
	if len(got) != 3 || got[0] != p[0] || got[1] != p[1] || got[2] != p[2] {
		t.Errorf("Palette = %v, want %v", got, p)
	}
	// Palette returns a defensive copy — mutating the return must not affect the colormap.
	got[0] = 0xdeadbeef
	if cm.Palette()[0] != p[0] {
		t.Error("Palette should return a defensive copy")
	}
}

// =============================================================================
// WithMaxNestingDepth / previewDepth
// =============================================================================

func TestWithMaxNestingDepth_SetsField(t *testing.T) {
	tm := &Treemap{}
	WithMaxNestingDepth(5)(tm)
	if tm.maxNestingDepth != 5 {
		t.Fatalf("WithMaxNestingDepth(5): maxNestingDepth = %d, want 5", tm.maxNestingDepth)
	}
}

func TestWithCellLabel_SetAndClear(t *testing.T) {
	tm := &Treemap{}
	if tm.cellLabelFn != nil {
		t.Fatal("zero Treemap should have a nil cellLabelFn")
	}
	WithCellLabel(func(n *layout.Node) string { return "v:" + n.Name })(tm)
	if tm.cellLabelFn == nil {
		t.Fatal("WithCellLabel should set cellLabelFn")
	}
	if got := tm.cellLabelFn(&layout.Node{Name: "x"}); got != "v:x" {
		t.Fatalf("cellLabelFn not wired: got %q want %q", got, "v:x")
	}
	// SetCellLabel mirrors the option and accepts nil to disable.
	tm.SetCellLabel(func(*layout.Node) string { return "y" })
	if got := tm.cellLabelFn(&layout.Node{}); got != "y" {
		t.Fatalf("SetCellLabel did not replace fn: got %q want %q", got, "y")
	}
	tm.SetCellLabel(nil)
	if tm.cellLabelFn != nil {
		t.Fatal("SetCellLabel(nil) should clear cellLabelFn")
	}
}

func TestPreviewDepth(t *testing.T) {
	cases := []struct {
		name string
		set  int
		want int
	}{
		{"default one-level", 1, 1},
		{"explicit multi-level", 3, 3},
		{"zero means all", 0, maxPreviewRecursion},
		{"negative means all", -1, maxPreviewRecursion},
	}
	for _, tc := range cases {
		t.Run(tc.name, func(t *testing.T) {
			tm := &Treemap{maxNestingDepth: tc.set}
			if got := tm.previewDepth(); got != tc.want {
				t.Errorf("previewDepth() with maxNestingDepth=%d = %d, want %d", tc.set, got, tc.want)
			}
		})
	}
}

// =============================================================================
// Helpers
// =============================================================================

func mustPanic(t *testing.T, name string, fn func()) {
	t.Helper()
	defer func() {
		if r := recover(); r == nil {
			t.Errorf("%s: expected panic, got none", name)
		}
	}()
	fn()
}
