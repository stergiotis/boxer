package imztop

import (
	"testing"

	"github.com/stergiotis/boxer/public/observability/sysmetrics/sysmsnap"
	"github.com/stergiotis/boxer/public/thestack/imzero2/egui2/widgets/treemap/layout"
)

func procs(ps ...sysmsnap.ProcInfo) []sysmsnap.ProcInfo { return ps }

func TestBuildProcForest_SimpleTree(t *testing.T) {
	// systemd(1) → {a(2) → c(4), b(3)}; systemd's parent (0) is absent → root.
	in := procs(
		sysmsnap.ProcInfo{PID: 1, PPID: 0},
		sysmsnap.ProcInfo{PID: 2, PPID: 1},
		sysmsnap.ProcInfo{PID: 3, PPID: 1},
		sysmsnap.ProcInfo{PID: 4, PPID: 2},
	)
	children, roots := buildProcForest(in)
	if len(roots) != 1 || roots[0] != 0 {
		t.Fatalf("roots = %v, want [0]", roots)
	}
	if got := children[0]; len(got) != 2 || got[0] != 1 || got[1] != 2 {
		t.Fatalf("children[0] = %v, want [1 2]", got)
	}
	if got := children[1]; len(got) != 1 || got[0] != 3 {
		t.Fatalf("children[1] = %v, want [3]", got)
	}
	if len(children[2]) != 0 || len(children[3]) != 0 {
		t.Fatalf("leaves should have no children: %v %v", children[2], children[3])
	}
}

func TestBuildProcForest_OrphanAndSelfParent(t *testing.T) {
	// a(2)'s parent 99 is absent (top-N truncation) → root. x(5) is its own
	// parent → root. Neither is dropped.
	in := procs(
		sysmsnap.ProcInfo{PID: 2, PPID: 99},
		sysmsnap.ProcInfo{PID: 5, PPID: 5},
	)
	_, roots := buildProcForest(in)
	if len(roots) != 2 {
		t.Fatalf("roots = %v, want both indices", roots)
	}
}

func TestBuildProcForest_CycleNotDropped(t *testing.T) {
	// a(2).ppid=3, b(3).ppid=2 — a PID-reuse cycle where both look like a
	// child. The guard must still surface both via a promoted root.
	in := procs(
		sysmsnap.ProcInfo{PID: 2, PPID: 3},
		sysmsnap.ProcInfo{PID: 3, PPID: 2},
	)
	children, roots := buildProcForest(in)
	if len(roots) == 0 {
		t.Fatalf("cycle produced no root; both members would be dropped")
	}
	// Every node must be reachable from the roots.
	seen := make([]bool, len(in))
	var walk func(i int)
	walk = func(i int) {
		if seen[i] {
			return
		}
		seen[i] = true
		for _, ci := range children[i] {
			walk(ci)
		}
	}
	for _, r := range roots {
		walk(r)
	}
	for i, s := range seen {
		if !s {
			t.Fatalf("node %d unreachable from roots %v", i, roots)
		}
	}
}

// newProcMapTestApp builds the minimal App state reconcileProcTree touches, with
// procTreemap left nil so the drill-heal (which needs the widget) is skipped.
func newProcMapTestApp() *App {
	return &App{
		procRoot:    &layout.Node{Name: "System"},
		procNodes:   make(map[procEWMAKey]*layout.Node),
		procNodeObj: make(map[*layout.Node]*procCell),
	}
}

func TestReconcileProcTree_ShapeAndSelfLeaf(t *testing.T) {
	inst := newProcMapTestApp()
	in := procs(
		sysmsnap.ProcInfo{PID: 1, PPID: 0, Name: "systemd", RSSBytes: 14 << 20, StartedAtUnixMs: 1},
		sysmsnap.ProcInfo{PID: 2, PPID: 1, Name: "a", RSSBytes: 100 << 20, StartedAtUnixMs: 2},
		sysmsnap.ProcInfo{PID: 3, PPID: 1, Name: "b", RSSBytes: 200 << 20, StartedAtUnixMs: 3},
		sysmsnap.ProcInfo{PID: 4, PPID: 2, Name: "c", RSSBytes: 50 << 20, StartedAtUnixMs: 4},
	)
	inst.reconcileProcTree(in, nil, procMetricRSS)

	if len(inst.procRoot.Children) != 1 {
		t.Fatalf("root children = %d, want 1 (systemd)", len(inst.procRoot.Children))
	}
	sd := inst.procRoot.Children[0]
	if sd.Name != "systemd" {
		t.Fatalf("root child name = %q, want systemd", sd.Name)
	}
	// systemd is a parent → self-leaf + a + b.
	if len(sd.Children) != 3 {
		t.Fatalf("systemd children = %d, want 3 (self + a + b)", len(sd.Children))
	}
	self := sd.Children[0]
	if self.Name != "systemd" || self.Size != float64(14<<20) || len(self.Children) != 0 {
		t.Fatalf("self-leaf = %+v, want name systemd size 14MiB no children", self)
	}
	// 'b' is a leaf sized by RSS.
	b := sd.Children[2]
	if b.Name != "b" || b.Size != float64(200<<20) {
		t.Fatalf("b = %+v, want name b size 200MiB", b)
	}
	// 'a' is a parent (self-leaf + c).
	a := sd.Children[1]
	if a.Name != "a" || len(a.Children) != 2 {
		t.Fatalf("a = %+v, want name a with 2 children (self + c)", a)
	}
	// Payload maps back to the source process for hover/tint.
	if pc := inst.procNodeObj[b]; pc == nil || pc.info.PID != 3 {
		t.Fatalf("b payload = %+v, want proc PID 3", pc)
	}
}

func TestReconcileProcTree_StableIdentityAndEviction(t *testing.T) {
	inst := newProcMapTestApp()
	first := procs(
		sysmsnap.ProcInfo{PID: 1, PPID: 0, Name: "systemd", RSSBytes: 10 << 20, StartedAtUnixMs: 1},
		sysmsnap.ProcInfo{PID: 2, PPID: 1, Name: "a", RSSBytes: 20 << 20, StartedAtUnixMs: 2},
	)
	inst.reconcileProcTree(first, nil, procMetricRSS)
	key1 := procEWMAKey{PID: 1, StartedAt: 1}
	key2 := procEWMAKey{PID: 2, StartedAt: 2}
	node1 := inst.procNodes[key1]
	if node1 == nil || inst.procNodes[key2] == nil {
		t.Fatalf("expected both process nodes pooled")
	}

	// Second sample: same processes, different sizes. Node identity must hold
	// (drill state keys off it).
	second := procs(
		sysmsnap.ProcInfo{PID: 1, PPID: 0, Name: "systemd", RSSBytes: 11 << 20, StartedAtUnixMs: 1},
		sysmsnap.ProcInfo{PID: 2, PPID: 1, Name: "a", RSSBytes: 21 << 20, StartedAtUnixMs: 2},
	)
	inst.reconcileProcTree(second, nil, procMetricRSS)
	if inst.procNodes[key1] != node1 {
		t.Fatalf("systemd node pointer changed across reconcile; drill state would reset")
	}

	// Third sample: 'a' is gone → its pooled node must be evicted.
	third := procs(
		sysmsnap.ProcInfo{PID: 1, PPID: 0, Name: "systemd", RSSBytes: 12 << 20, StartedAtUnixMs: 1},
	)
	inst.reconcileProcTree(third, nil, procMetricRSS)
	if _, ok := inst.procNodes[key2]; ok {
		t.Fatalf("vanished process 'a' still pooled; the node map would leak")
	}
	if inst.procNodes[key1] != node1 {
		t.Fatalf("surviving systemd node pointer changed")
	}
}

func TestReconcileProcTree_CPUMetricSizesLeaves(t *testing.T) {
	inst := newProcMapTestApp()
	in := procs(
		sysmsnap.ProcInfo{PID: 7, PPID: 0, Name: "hot", RSSBytes: 5 << 20, CPUPercent: 40, StartedAtUnixMs: 7},
	)
	inst.reconcileProcTree(in, []float32{63.0}, procMetricCPU)
	if len(inst.procRoot.Children) != 1 {
		t.Fatalf("root children = %d, want 1", len(inst.procRoot.Children))
	}
	leaf := inst.procRoot.Children[0]
	// Leaf sized by the SMOOTHED cpu (63), not the raw 40.
	if leaf.Size != 63.0 {
		t.Fatalf("leaf size = %v, want 63 (smoothed CPU)", leaf.Size)
	}
}
