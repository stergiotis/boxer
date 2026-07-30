package windowhost

import (
	"testing"

	"github.com/rs/zerolog"
)

// pickActiveWindow routes process-global input, so each branch is a claim
// about who receives a keystroke — worth pinning individually.
func TestPickActiveWindow(t *testing.T) {
	cases := []struct {
		name  string
		prev  WindowKeyT
		facts []windowFocusFact
		want  WindowKeyT
	}{{
		name:  "no windows, no answer",
		prev:  7,
		facts: nil,
		want:  0,
	}, {
		name: "the topmost window wins regardless of history",
		prev: 1,
		facts: []windowFocusFact{
			{key: 1, topmost: false},
			{key: 2, topmost: true},
		},
		want: 2,
	}, {
		// A tethered child window or the SVG picker holds egui's top
		// layer: no host window reports topmost, and the user's working
		// window must not change under them.
		name: "no topmost report keeps the previous answer",
		prev: 2,
		facts: []windowFocusFact{
			{key: 1, topmost: false},
			{key: 2, topmost: false},
		},
		want: 2,
	}, {
		name: "a closed previous answer falls to the newest window",
		prev: 9,
		facts: []windowFocusFact{
			{key: 1, topmost: false},
			{key: 2, topmost: false},
		},
		want: 2,
	}, {
		// First frame: handles have no reports yet. egui opens new
		// windows on top, so the newest is the honest guess.
		name: "no history at all falls to the newest window",
		prev: 0,
		facts: []windowFocusFact{
			{key: 1, topmost: false},
			{key: 2, topmost: false},
			{key: 3, topmost: false},
		},
		want: 3,
	}, {
		name: "a single window is active unconditionally",
		prev: 0,
		facts: []windowFocusFact{
			{key: 4, topmost: false},
		},
		want: 4,
	}}
	for _, tc := range cases {
		t.Run(tc.name, func(t *testing.T) {
			if got := pickActiveWindow(tc.prev, tc.facts); got != tc.want {
				t.Errorf("pickActiveWindow(%d, %v) = %d, want %d", tc.prev, tc.facts, got, tc.want)
			}
		})
	}
}

// OpenOrRaise is the recurring-global-shortcut shape (F1 → help): the first
// request opens, every further one raises the existing window instead of
// stacking another. The raise emit itself needs the render loop; what is
// pinned here is the decision — same key back, no second window, and the
// queued raise naming the window that already shows the app.
func TestOpenOrRaise(t *testing.T) {
	reg, _ := mkRegistryWithSingleton(t, "app-a", "app-b")
	h := NewInst(reg, zerolog.Nop())

	k1, opened, err := h.OpenOrRaise("app-a")
	if err != nil {
		t.Fatalf("first OpenOrRaise: %v", err)
	}
	if !opened {
		t.Error("the first request must open")
	}
	k2, opened2, err2 := h.OpenOrRaise("app-a")
	if err2 != nil {
		t.Fatalf("second OpenOrRaise: %v", err2)
	}
	if opened2 {
		t.Error("the second request must raise, not open")
	}
	if k1 != k2 {
		t.Errorf("raise returned key %d, want the existing window %d", k2, k1)
	}
	if n := h.Len(); n != 1 {
		t.Errorf("Len = %d after open+raise, want 1", n)
	}
	h.mu.Lock()
	queued := h.pendingRaise
	h.mu.Unlock()
	if queued != k1 {
		t.Errorf("pendingRaise = %d, want %d", queued, k1)
	}

	// A different app opens fresh alongside.
	if _, openedB, errB := h.OpenOrRaise("app-b"); errB != nil || !openedB {
		t.Errorf("other app: opened=%v err=%v, want a fresh open", openedB, errB)
	}

	// A window already queued for close no longer counts as showing the
	// app: the next request opens a fresh window rather than raising a
	// dying one.
	h.Close(k1, "test")
	k3, opened3, err3 := h.OpenOrRaise("app-a")
	if err3 != nil {
		t.Fatalf("OpenOrRaise after Close: %v", err3)
	}
	if !opened3 || k3 == k1 {
		t.Errorf("after Close: key=%d opened=%v, want a fresh window", k3, opened3)
	}
}
