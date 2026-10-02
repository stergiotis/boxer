package play

import (
	"testing"
	"unicode/utf8"
)

func TestTruncateRunes(t *testing.T) {
	cases := []struct {
		s    string
		max  int
		want string
	}{
		{"hello", 10, "hello"}, // shorter than max — unchanged
		{"hello", 5, "hello"},  // exactly max — no ellipsis
		{"hello world", 5, "hello…"},
		{"hello", 0, "hello"},  // max <= 0 — unchanged
		{"hello", -3, "hello"}, // negative max — unchanged
		{"", 5, ""},
	}
	for _, tc := range cases {
		if got := truncateRunes(tc.s, tc.max); got != tc.want {
			t.Errorf("truncateRunes(%q, %d) = %q, want %q", tc.s, tc.max, got, tc.want)
		}
	}
}

// The regression that motivated truncateRunes: a byte slice (s[:n]) can cut a
// multi-byte rune in half and ship invalid UTF-8 to the FFI wire. truncateRunes
// must always cut on a rune boundary and return valid UTF-8.
func TestTruncateRunesMultiByteBoundary(t *testing.T) {
	s := "ééééé" // 5 runes, 10 bytes (é = 0xC3 0xA9)
	got := truncateRunes(s, 3)
	if !utf8.ValidString(got) {
		t.Fatalf("result %q is not valid UTF-8", got)
	}
	if got != "ééé…" {
		t.Errorf("got %q, want ééé…", got)
	}
	if n := utf8.RuneCountInString(got); n != 4 { // 3 runes + ellipsis
		t.Errorf("rune count = %d, want 4", n)
	}
}

// Invalid UTF-8 input must be sanitised (EnsureUTF8 hex-encodes), never passed
// through raw.
func TestTruncateRunesInvalidInput(t *testing.T) {
	got := truncateRunes("ab\xffcd", 100)
	if !utf8.ValidString(got) {
		t.Errorf("result %q not valid UTF-8 — EnsureUTF8 should sanitise", got)
	}
}

func TestTruncateBytesBacksOffToARuneBoundary(t *testing.T) {
	cases := []struct {
		s    string
		max  int
		want string
	}{
		{"hello", 10, "hello"},
		{"hello", 3, "hel"},
		{"aé", 2, "a"}, // é is two bytes; the cut lands inside it
		{"aé", 3, "aé"},
		{"é", 1, ""},
	}
	for _, tc := range cases {
		got := truncateBytes(tc.s, tc.max)
		if got != tc.want || !utf8.ValidString(got) {
			t.Errorf("truncateBytes(%q, %d) = %q, want %q", tc.s, tc.max, got, tc.want)
		}
	}
}
