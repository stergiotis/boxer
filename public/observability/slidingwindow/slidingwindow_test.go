package slidingwindow_test

import (
	"slices"
	"testing"

	"github.com/stergiotis/boxer/public/observability/slidingwindow"
)

func TestWindowFillUnderCap(t *testing.T) {
	w := slidingwindow.New[int](3)
	if w.Cap() != 3 {
		t.Fatalf("Cap() = %d, want 3", w.Cap())
	}
	if w.Len() != 0 {
		t.Fatalf("Len() = %d, want 0", w.Len())
	}
	w.Push(1)
	w.Push(2)
	if w.Len() != 2 {
		t.Fatalf("Len() = %d, want 2", w.Len())
	}
	if got := w.Values(); !slices.Equal(got, []int{1, 2}) {
		t.Fatalf("Values() = %v, want [1 2]", got)
	}
}

func TestWindowOverflowDropsOldestChronological(t *testing.T) {
	w := slidingwindow.New[int](3)
	for i := 1; i <= 5; i++ {
		w.Push(i)
	}
	if w.Len() != 3 {
		t.Fatalf("Len() = %d, want 3 (capped)", w.Len())
	}
	// Oldest two (1,2) dropped; remaining in chronological order.
	if got := w.Values(); !slices.Equal(got, []int{3, 4, 5}) {
		t.Fatalf("Values() = %v, want [3 4 5]", got)
	}
}

func TestWindowCapClampedToMinimumOne(t *testing.T) {
	w := slidingwindow.New[float64](0)
	if w.Cap() != 1 {
		t.Fatalf("Cap() = %d, want 1 (clamped)", w.Cap())
	}
	w.Push(1.5)
	w.Push(2.5)
	if got := w.Values(); !slices.Equal(got, []float64{2.5}) {
		t.Fatalf("Values() = %v, want [2.5]", got)
	}
}

func TestWindowResetEmptiesAndKeepsCapacity(t *testing.T) {
	w := slidingwindow.New[int](3)
	for i := 1; i <= 5; i++ {
		w.Push(i)
	}
	w.Reset()
	if w.Len() != 0 {
		t.Fatalf("Len() = %d, want 0 after Reset", w.Len())
	}
	if w.Cap() != 3 {
		t.Fatalf("Cap() = %d, want 3 — Reset keeps the capacity", w.Cap())
	}
	if got := w.Values(); len(got) != 0 {
		t.Fatalf("Values() = %v, want empty", got)
	}
	// Refilling behaves as it does on a fresh window, dropping nothing until
	// the capacity is reached again.
	w.Push(7)
	w.Push(8)
	if got := w.Values(); !slices.Equal(got, []int{7, 8}) {
		t.Fatalf("Values() = %v, want [7 8]", got)
	}
}

// TestWindowResetDropsReferences pins that Reset does not leave the dropped
// values reachable behind the truncated slice, which would keep whatever a
// pointer-carrying T points at alive for as long as the window lives.
func TestWindowResetDropsReferences(t *testing.T) {
	w := slidingwindow.New[*int](2)
	a, b := 1, 2
	w.Push(&a)
	w.Push(&b)
	w.Reset()
	for i, p := range w.Values()[:2] { // past Len, still inside the backing array
		if p != nil {
			t.Fatalf("backing slot %d still holds %v, want nil", i, *p)
		}
	}
}
