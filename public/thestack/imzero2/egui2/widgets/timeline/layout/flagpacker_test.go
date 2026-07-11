package layout

import (
	"slices"
	"testing"
)

func TestPackFlagRows_EmptyInput(t *testing.T) {
	rows, rowCount := PackFlagRows(nil, 26, 3)
	if rows != nil {
		t.Errorf("rows: got %v want nil", rows)
	}
	if rowCount != 0 {
		t.Errorf("rowCount: got %d want 0", rowCount)
	}
}

func TestPackFlagRows_SingleFlag(t *testing.T) {
	rows, rowCount := PackFlagRows([]float64{100}, 26, 3)
	if !slices.Equal(rows, []int32{0}) || rowCount != 1 {
		t.Errorf("got rows=%v rowCount=%d want [0] 1", rows, rowCount)
	}
}

func TestPackFlagRows_FarApart_OneRow(t *testing.T) {
	rows, rowCount := PackFlagRows([]float64{100, 200, 300}, 26, 3)
	if !slices.Equal(rows, []int32{0, 0, 0}) || rowCount != 1 {
		t.Errorf("got rows=%v rowCount=%d want [0,0,0] 1", rows, rowCount)
	}
}

func TestPackFlagRows_TouchingBoundary_SameRowAllowed(t *testing.T) {
	// Centers exactly flagW apart: rects share an edge, no overlap.
	rows, rowCount := PackFlagRows([]float64{100, 126}, 26, 3)
	if !slices.Equal(rows, []int32{0, 0}) || rowCount != 1 {
		t.Errorf("got rows=%v rowCount=%d want [0,0] 1", rows, rowCount)
	}
}

func TestPackFlagRows_Colliding_TwoRows(t *testing.T) {
	rows, rowCount := PackFlagRows([]float64{100, 110}, 26, 3)
	if !slices.Equal(rows, []int32{0, 1}) || rowCount != 2 {
		t.Errorf("got rows=%v rowCount=%d want [0,1] 2", rows, rowCount)
	}
}

func TestPackFlagRows_FirstFitReusesTopRow(t *testing.T) {
	// Staircase: 0/10/20 need three rows; 30 fits row 0 again (30-0 >= 26);
	// 40 fits row 1 (40-10 >= 26). First-fit keeps the top row densest.
	rows, rowCount := PackFlagRows([]float64{0, 10, 20, 30, 40}, 26, 5)
	if !slices.Equal(rows, []int32{0, 1, 2, 0, 1}) || rowCount != 3 {
		t.Errorf("got rows=%v rowCount=%d want [0,1,2,0,1] 3", rows, rowCount)
	}
}

func TestPackFlagRows_UnsortedInput_IndexAligned(t *testing.T) {
	// Same staircase permuted: row assignments must follow the values, not
	// the input positions.
	rows, rowCount := PackFlagRows([]float64{30, 0, 40, 20, 10}, 26, 5)
	if !slices.Equal(rows, []int32{0, 0, 1, 2, 1}) || rowCount != 3 {
		t.Errorf("got rows=%v rowCount=%d want [0,0,1,2,1] 3", rows, rowCount)
	}
}

func TestPackFlagRows_EqualX_StableInputOrder(t *testing.T) {
	rows, rowCount := PackFlagRows([]float64{50, 50, 50}, 26, 3)
	if !slices.Equal(rows, []int32{0, 1, 2}) || rowCount != 3 {
		t.Errorf("got rows=%v rowCount=%d want [0,1,2] 3", rows, rowCount)
	}
}

func TestPackFlagRows_CapOverflow_LeastCrowdedRow(t *testing.T) {
	// Four coincident flags, cap 3: the fourth degrades to the row whose
	// rightmost center is smallest — all equal here, tie toward row 0.
	rows, rowCount := PackFlagRows([]float64{50, 50, 50, 50}, 26, 3)
	if !slices.Equal(rows, []int32{0, 1, 2, 0}) || rowCount != 3 {
		t.Errorf("got rows=%v rowCount=%d want [0,1,2,0] 3", rows, rowCount)
	}
}

func TestPackFlagRows_CapOverflow_PicksSmallestRightmost(t *testing.T) {
	// Cap 2. 0→row0, 1→row1, 2→overflow (row0 rightmost=0 smallest),
	// 3→overflow (row1 rightmost=1 < row0 rightmost=2).
	rows, rowCount := PackFlagRows([]float64{0, 1, 2, 3}, 26, 2)
	if !slices.Equal(rows, []int32{0, 1, 0, 1}) || rowCount != 2 {
		t.Errorf("got rows=%v rowCount=%d want [0,1,0,1] 2", rows, rowCount)
	}
}

func TestPackFlagRows_MaxRowsBelowOne_ClampedToOne(t *testing.T) {
	rows, rowCount := PackFlagRows([]float64{50, 50}, 26, 0)
	if !slices.Equal(rows, []int32{0, 0}) || rowCount != 1 {
		t.Errorf("got rows=%v rowCount=%d want [0,0] 1", rows, rowCount)
	}
}

func TestPackFlagRows_ZeroWidth_NoCollisions(t *testing.T) {
	rows, rowCount := PackFlagRows([]float64{50, 50, 50}, 0, 3)
	if !slices.Equal(rows, []int32{0, 0, 0}) || rowCount != 1 {
		t.Errorf("got rows=%v rowCount=%d want [0,0,0] 1", rows, rowCount)
	}
}
