package implot

import (
	"strings"
	"testing"
)

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

func TestWrap(t *testing.T) {
	// At this size a Latin glyph is 6.2 px and a CJK one 10, as in TestElide.
	const size = 10
	cases := []struct {
		in      string
		availPx float32
		want    []string
	}{
		{"", 100, nil},
		{"short", 100, []string{"short"}},
		{"short", 0, nil},
		{"short", -1, nil},
		// 62 px holds "wrap me" (43.4) but not "wrap me now" (68.2).
		{"wrap me now", 62, []string{"wrap me", "now"}},
		// A word wider than the whole line starts fresh and breaks inside, at
		// the ten glyphs (62 px) that fit in 65.
		{"a superlongunbreakableword", 65, []string{"a", "superlongu", "nbreakable", "word"}},
		// No spaces at all — the rune break is what makes this terminate.
		{"日本語のフレーム", 30, []string{"日本語", "のフレ", "ーム"}},
		// Newlines are hard breaks, blank lines survive.
		{"one\ntwo", 100, []string{"one", "two"}},
		{"one\n\ntwo", 100, []string{"one", "", "two"}},
		// Runs of spaces collapse.
		{"a   b", 100, []string{"a b"}},
	}
	for _, tc := range cases {
		got := Wrap(tc.in, tc.availPx, size)
		if len(got) != len(tc.want) {
			t.Errorf("Wrap(%q, %v) = %q, want %q", tc.in, tc.availPx, got, tc.want)
			continue
		}
		for i := range got {
			if got[i] != tc.want[i] {
				t.Errorf("Wrap(%q, %v) = %q, want %q", tc.in, tc.availPx, got, tc.want)
				break
			}
		}
	}
}

// Every line has to fit the width it was broken for — the whole point of
// budgeting in pixels rather than characters. The exception the doc states is
// a line of one rune, which draws whether it fits or not.
func TestWrapLinesFitTheirWidth(t *testing.T) {
	const size = 12
	inputs := []string{
		"kanban card titles are sometimes rather long indeed",
		"averylongsingletokenwithnobreaksatall",
		"日本語のフレームバッファ",
		"mixed 日本語 and latin words together",
		"a b c d e f g h i j k l m n o p",
	}
	for _, in := range inputs {
		for _, avail := range []float32{1, 7, 20, 60, 140, 400} {
			for _, line := range Wrap(in, avail, size) {
				if len([]rune(line)) < 2 {
					continue // the documented one-rune exception
				}
				if w := EstimateTextWidth(line, size); w > avail {
					t.Errorf("Wrap(%q, %v) line %q is %v px wide", in, avail, line, w)
				}
			}
		}
	}
}

// Wrapping must not lose or invent characters: the lines carry the same runes
// in the same order as the input. Compared with the whitespace stripped from
// both sides, because a break inside an overlong word is a break the input did
// not have, and one at a space consumes the space it broke at.
func TestWrapPreservesTheText(t *testing.T) {
	const size = 11
	squash := func(s string) string {
		return strings.Map(func(r rune) rune {
			if r == ' ' || r == '\n' {
				return -1
			}
			return r
		}, s)
	}
	for _, in := range []string{
		"kanban card titles are sometimes rather long indeed",
		"averylongsingletokenwithnobreaksatall",
		"日本語のフレームバッファ",
		"trailing space ",
		"two\nparagraphs of words",
	} {
		for _, avail := range []float32{3, 25, 90, 500} {
			got := squash(strings.Join(Wrap(in, avail, size), ""))
			if want := squash(in); got != want {
				t.Errorf("Wrap(%q, %v) carries %q, want %q", in, avail, got, want)
			}
		}
	}
}
