//go:build llm_generated_opus47

package progressbar

import "testing"

func TestBarWidthFor(t *testing.T) {
	cases := []struct {
		term, want int
	}{
		{0, maxBarWidth},
		{-1, maxBarWidth},
		{10, minBarWidth}, // 10/3 = 3, clamped up to 5
		{15, 5},           // 15/3 = 5
		{30, 10},          // 30/3 = 10
		{90, maxBarWidth}, // 90/3 = 30
		{200, maxBarWidth},
	}
	for _, tc := range cases {
		if got := barWidthFor(tc.term); got != tc.want {
			t.Errorf("barWidthFor(%d) = %d; want %d", tc.term, got, tc.want)
		}
	}
}

func TestTruncateToWidth(t *testing.T) {
	cases := []struct {
		name, in string
		w        int
		want     string
	}{
		{"unknown width leaves string untouched", "hello world", 0, "hello world"},
		{"width larger than content passes through", "hello", 100, "hello"},
		{"width smaller than content truncates (headroom 1)", "hello world", 6, "hello"},
		{"width 1 yields empty", "hello", 1, ""},
		{"rune-aware truncation (multi-byte)", "αβγδε", 4, "αβγ"},
		{"block chars counted as runes", "█████", 4, "███"},
	}
	for _, tc := range cases {
		if got := truncateToWidth(tc.in, tc.w); got != tc.want {
			t.Errorf("%s: truncateToWidth(%q, %d) = %q; want %q", tc.name, tc.in, tc.w, got, tc.want)
		}
	}
}
