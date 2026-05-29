//go:build llm_generated_opus47

package imztop

import (
	"testing"
)

func TestSlidingWindowPushBeforeFull(t *testing.T) {
	r := NewSlidingWindow[int](4)
	r.Push(1)
	r.Push(2)
	r.Push(3)

	if got := r.Len(); got != 3 {
		t.Fatalf("Len after 3 pushes: got %d want 3", got)
	}
	if got := r.Cap(); got != 4 {
		t.Fatalf("Cap: got %d want 4", got)
	}
	got := r.Values()
	want := []int{1, 2, 3}
	if len(got) != len(want) {
		t.Fatalf("Values len: got %d want %d", len(got), len(want))
	}
	for i, v := range want {
		if got[i] != v {
			t.Errorf("Values[%d]: got %d want %d", i, got[i], v)
		}
	}
}

func TestSlidingWindowPushOverCap(t *testing.T) {
	r := NewSlidingWindow[int](3)
	for i := 1; i <= 5; i++ {
		r.Push(i)
	}

	if got := r.Len(); got != 3 {
		t.Fatalf("Len after over-cap pushes: got %d want 3", got)
	}
	got := r.Values()
	want := []int{3, 4, 5}
	for i, v := range want {
		if got[i] != v {
			t.Errorf("Values[%d]: got %d want %d (got=%v)", i, got[i], v, got)
		}
	}
}

func TestSlidingWindowBackingArrayStable(t *testing.T) {
	r := NewSlidingWindow[int](3)
	r.Push(1)
	r.Push(2)
	r.Push(3)
	firstAddr := &r.Values()[0]

	for i := 0; i < 100; i++ {
		r.Push(i)
	}
	secondAddr := &r.Values()[0]

	if firstAddr != secondAddr {
		t.Errorf("backing array re-allocated under sustained Push; addrs %p vs %p", firstAddr, secondAddr)
	}
}

func TestSlidingWindowChronologicalOrder(t *testing.T) {
	r := NewSlidingWindow[int](5)
	for i := 1; i <= 10; i++ {
		r.Push(i)
	}
	got := r.Values()
	for i := 1; i < len(got); i++ {
		if got[i-1] >= got[i] {
			t.Errorf("Values not chronological at %d: %v", i, got)
			break
		}
	}
}

func TestSlidingWindowZeroAndOneCap(t *testing.T) {
	r0 := NewSlidingWindow[int](0)
	if got := r0.Cap(); got != 1 {
		t.Errorf("zero cap clamped to 1: got %d", got)
	}
	r0.Push(42)
	r0.Push(43)
	got := r0.Values()
	if len(got) != 1 || got[0] != 43 {
		t.Errorf("cap=1 should keep latest: got %v", got)
	}
}
