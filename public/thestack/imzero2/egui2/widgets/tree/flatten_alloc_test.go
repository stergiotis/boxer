package tree

import (
	"strconv"
	"testing"
)

// wideTree is a forest of fanout roots, each with fanout children: the
// shape of a file list grouped by directory.
func wideTree(fanout int) (t Tree) {
	n := fanout + fanout*fanout
	t.Labels = make([]string, 0, n)
	t.Parents = make([]int32, 0, n)
	for r := range fanout {
		root := int32(len(t.Labels))
		t.Labels = append(t.Labels, "dir"+strconv.Itoa(r))
		t.Parents = append(t.Parents, -1)
		for c := range fanout {
			t.Labels = append(t.Labels, "file"+strconv.Itoa(c))
			t.Parents = append(t.Parents, root)
		}
	}
	return
}

// TestFlattenReusesStateScratch: once a State has flattened a tree, flattening
// a tree of the same size again allocates nothing.
func TestFlattenReusesStateScratch(t *testing.T) {
	tr := wideTree(64)
	var st State
	st.ExpandAll()
	rows, err := Flatten(tr, &st, nil)
	if err != nil {
		t.Fatal(err)
	}
	allocs := testing.AllocsPerRun(20, func() {
		rows, err = Flatten(tr, &st, rows[:0])
	})
	if err != nil {
		t.Fatal(err)
	}
	if allocs != 0 {
		t.Errorf("Flatten allocated %v times per call with a warm State", allocs)
	}
	if len(rows) != len(tr.Labels) {
		t.Errorf("rows = %d, want every node of an expanded tree (%d)", len(rows), len(tr.Labels))
	}
}

func BenchmarkFlattenWarmState(b *testing.B) {
	tr := wideTree(73) // 5402 nodes
	var st State
	st.ExpandAll()
	rows, _ := Flatten(tr, &st, nil)
	b.ReportAllocs()
	for b.Loop() {
		rows, _ = Flatten(tr, &st, rows[:0])
	}
}
