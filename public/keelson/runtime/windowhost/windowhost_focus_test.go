package windowhost

import "testing"

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
