package bindings

import "testing"

// The caret channel is symmetric: reportCursor packs a sorted CHAR range into
// one u64 (low half start, high half end) and setCursor takes the same shape
// back. These tests pin that the two halves agree, because a disagreement
// would place the caret somewhere plausible-but-wrong rather than failing.

func TestPackUnpackCursorRange_RoundTrips(t *testing.T) {
	cases := [][2]int{
		{0, 0},
		{0, 1},
		{5, 5},
		{3, 91},
		{0, 0xffff_ffff},
		{0xffff_ffff, 0xffff_ffff},
	}
	for _, want := range cases {
		start, end := UnpackCursorRange(PackCursorRange(want[0], want[1]))
		if start != want[0] || end != want[1] {
			t.Errorf("round trip of (%d, %d): got (%d, %d)", want[0], want[1], start, end)
		}
	}
}

// TestPackCursorRange_SortsItsArguments covers a selection dragged backwards,
// which arrives with start past end. The wire contract is a SORTED range, so
// the packing normalises rather than shipping an inverted one for Rust to
// puzzle over.
func TestPackCursorRange_SortsItsArguments(t *testing.T) {
	start, end := UnpackCursorRange(PackCursorRange(40, 12))
	if start != 12 || end != 40 {
		t.Errorf("reversed selection: got (%d, %d) want (12, 40)", start, end)
	}
}

// TestPackCursorRange_ClampsOutOfRange keeps a bad offset from corrupting the
// OTHER half of the word: an unclamped negative would sign-extend across the
// whole u64, and an over-32-bit value would spill into the high half. Rust
// clamps again against the buffer it actually holds.
func TestPackCursorRange_ClampsOutOfRange(t *testing.T) {
	start, end := UnpackCursorRange(PackCursorRange(-5, 10))
	if start != 0 || end != 10 {
		t.Errorf("negative start: got (%d, %d) want (0, 10)", start, end)
	}

	const half = 0xffff_ffff
	start, end = UnpackCursorRange(PackCursorRange(3, half+1000))
	if start != 3 || end != half {
		t.Errorf("oversized end: got (%d, %d) want (3, %d)", start, end, half)
	}

	// Both halves bad, and still no bleed between them.
	start, end = UnpackCursorRange(PackCursorRange(-1, -1))
	if start != 0 || end != 0 {
		t.Errorf("both negative: got (%d, %d) want (0, 0)", start, end)
	}
}

func TestPackCursorRange_CollapsedCaret(t *testing.T) {
	start, end := UnpackCursorRange(PackCursorRange(7, 7))
	if start != end || start != 7 {
		t.Errorf("collapsed caret: got (%d, %d) want (7, 7)", start, end)
	}
}
