package cardgrid

import (
	"strings"
	"testing"
	"unicode/utf8"
)

func TestOneLine(t *testing.T) {
	cases := []struct {
		name, in string
		max      int
		want     string
	}{
		{"short passes through", "Sine sweep", 40, "Sine sweep"},
		{"empty", "", 40, ""},
		{"newlines and tabs fold", "a\nb\tc\r\nd", 40, "a b c d"},
		{"runs of spaces collapse", "  a   b  ", 40, "a b"},
		{"controls dropped", "a\x00b\x1bc\x7fd", 40, "abcd"},
		{"invalid utf-8 replaced", "a\xffb", 40, "a�b"},
		{"cut with ellipsis", "abcdefghij", 4, "abcd…"},
		{"cut counts runes not bytes", "äöüäöü", 3, "äöü…"},
		{"exact fit is not cut", "abcd", 4, "abcd"},
		{"no break opportunity", "/a/very/long/path/without/spaces", 10, "/a/very/lo…"},
	}
	for _, tc := range cases {
		if got := OneLine(tc.in, tc.max); got != tc.want {
			t.Errorf("%s: OneLine(%q, %d) = %q, want %q", tc.name, tc.in, tc.max, got, tc.want)
		}
	}
}

func TestLines(t *testing.T) {
	cases := []struct {
		name, in     string
		runes, lines int
		want         string
	}{
		{"keeps newlines", "a\nb", 40, 3, "a\nb"},
		{"crlf counts once", "a\r\nb", 40, 3, "a\nb"},
		{"cuts after the last line", "a\nb\nc\nd", 40, 2, "a\nb…"},
		{"trailing blank lines are not a cut", "a\nb\n\n", 40, 2, "a\nb"},
		{"one line behaves as OneLine", "a\nb", 40, 1, "a b"},
	}
	for _, tc := range cases {
		if got := Lines(tc.in, tc.runes, tc.lines); got != tc.want {
			t.Errorf("%s: Lines(%q) = %q, want %q", tc.name, tc.in, got, tc.want)
		}
	}
}

// A megabyte cell must cost what its head costs, and come out bounded and
// valid (ADR-0245 §SD4).
func TestClampIsBoundedOnHugeInput(t *testing.T) {
	huge := strings.Repeat("x\xff\n", 1<<20)
	got := OneLine(huge, MaxTitleRunes)
	if n := utf8.RuneCountInString(got); n > MaxTitleRunes+1 {
		t.Fatalf("%d runes out for a budget of %d", n, MaxTitleRunes)
	}
	if !utf8.ValidString(got) {
		t.Fatalf("invalid utf-8 out")
	}
	allocs := testing.AllocsPerRun(10, func() { _ = OneLine(huge, MaxTitleRunes) })
	if allocs > 4 {
		t.Errorf("%v allocations clamping a huge cell", allocs)
	}
	if allocs := testing.AllocsPerRun(10, func() { _ = OneLine("a short name", MaxTitleRunes) }); allocs != 0 {
		t.Errorf("%v allocations for a string that needs no work", allocs)
	}
}

func TestDisplayCut(t *testing.T) {
	if got, cut := displayCut("short", 10); got != "short" || cut {
		t.Errorf("displayCut(short) = %q, %v", got, cut)
	}
	if got, cut := displayCut("äöüäöü", 6); got != "äöüäöü" || cut {
		t.Errorf("an exact fit was cut: %q, %v", got, cut)
	}
	if got, cut := displayCut("hello world", 6); got != "hello…" || !cut {
		t.Errorf("displayCut = %q, %v", got, cut)
	}
}
