package implot

import "testing"

func TestElide(t *testing.T) {
	// At this size a Latin glyph is 6.2 px and a CJK one 10, which is what
	// makes the budgets below readable.
	const size = 10
	cases := []struct {
		in      string
		availPx float32
		want    string
	}{
		{"runtime.mallocgc", 200, "runtime.mallocgc"},
		{"runtime.mallocgc", 100, "runtime.mallocgc"}, // 99.2 px, fits exactly
		{"runtime.mallocgc", 50, "runtime…"},
		{"runtime.mallocgc", 13, "r…"},
		{"runtime.mallocgc", 8, ""}, // no room for a glyph beside the ellipsis
		{"runtime.mallocgc", 0, ""},
		{"runtime.mallocgc", -3, ""},
		{"", 10, ""},
		// Multi-byte: the cut lands on a rune boundary, not a byte one.
		{"日本語のフレーム", 40, "日本語…"},
		{"日本語", 30, "日本語"},
		// And the reason the budget is pixels rather than characters: a box
		// 49.6 px wide holds eight Latin glyphs, so a character budget would
		// have kept all eight of these — 80 px of them. It keeps four.
		{"日本語のフレーム", 49.6, "日本語の…"},
	}
	for _, tc := range cases {
		if got := Elide(tc.in, tc.availPx, size); got != tc.want {
			t.Errorf("Elide(%q, %v) = %q, want %q", tc.in, tc.availPx, got, tc.want)
		}
	}
	// Whatever comes back must actually fit what it was cut for — which is
	// the whole reason it budgets with the estimate rather than a count.
	for _, tc := range cases {
		if got := Elide(tc.in, tc.availPx, size); got != "" {
			if w := EstimateTextWidth(got, size); w > tc.availPx {
				t.Errorf("Elide(%q, %v) = %q, which is %v px wide", tc.in, tc.availPx, got, w)
			}
		}
	}
}

// A label that fits comes back untouched, byte for byte: an ellipsis where
// none was needed is a lie about the data.
func TestElideLeavesAFittingLabelAlone(t *testing.T) {
	const size = 12
	for _, s := range []string{"", "a", "runtime.mallocgc", "日本語"} {
		w := EstimateTextWidth(s, size)
		if got := Elide(s, w, size); got != s {
			t.Errorf("Elide(%q, %v) = %q at exactly its own width", s, w, got)
		}
	}
}
