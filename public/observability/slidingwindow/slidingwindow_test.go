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
