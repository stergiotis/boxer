//go:build llm_generated_opus48

package imztop

import (
	"testing"

	"github.com/stergiotis/boxer/public/observability/sysmetrics/proc"
)

func TestBuildProcOrder_Forest(t *testing.T) {
	// 1 → {2 → 4, 3}; plus orphan 9 whose parent 99 is absent (→ a root).
	infos := []proc.Info{
		{PID: 1, PPID: 0},
		{PID: 2, PPID: 1},
		{PID: 3, PPID: 1},
		{PID: 4, PPID: 2},
		{PID: 9, PPID: 99},
	}
	order, depth := buildProcOrder(infos)

	if len(order) != len(infos) || len(depth) != len(infos) {
		t.Fatalf("lengths: order=%d depth=%d want %d", len(order), len(depth), len(infos))
	}
	seen := make([]bool, len(infos))
	for _, oi := range order {
		if seen[oi] {
			t.Fatalf("infos index %d emitted twice", oi)
		}
		seen[oi] = true
	}

	depthOf := map[uint32]int{}
	posOf := map[uint32]int{}
	for k, oi := range order {
		depthOf[infos[oi].PID] = depth[k]
		posOf[infos[oi].PID] = k
	}
	for pid, want := range map[uint32]int{1: 0, 2: 1, 3: 1, 4: 2, 9: 0} {
		if depthOf[pid] != want {
			t.Errorf("pid %d depth=%d want %d", pid, depthOf[pid], want)
		}
	}
	// DFS invariant: a child is emitted after its parent.
	for _, p := range infos {
		if pp, ok := posOf[p.PPID]; ok && p.PPID != p.PID && posOf[p.PID] < pp {
			t.Errorf("pid %d emitted before parent %d", p.PID, p.PPID)
		}
	}
}

func TestBuildProcOrder_CycleSafe(t *testing.T) {
	// PID-reuse cycle 5↔6 (each names the other as parent): must terminate
	// and include both exactly once.
	infos := []proc.Info{{PID: 5, PPID: 6}, {PID: 6, PPID: 5}}
	order, depth := buildProcOrder(infos)
	if len(order) != 2 || len(depth) != 2 {
		t.Fatalf("got order=%d depth=%d want 2/2", len(order), len(depth))
	}
}

func TestTreeIndent(t *testing.T) {
	cases := []struct {
		d    int
		in   string
		want string
	}{
		{0, "init", "init"},
		{1, "child", "└ child"},
		{2, "grandchild", "  └ grandchild"},
	}
	for _, tc := range cases {
		if got := treeIndent(tc.d, tc.in); got != tc.want {
			t.Errorf("treeIndent(%d,%q)=%q want %q", tc.d, tc.in, got, tc.want)
		}
	}
}
