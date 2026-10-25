package mdblocks

import (
	"testing"
	"time"

	"github.com/stergiotis/boxer/public/thestack/imzero2/egui2/widgets/markdown"
)

func parseBlocks(src string) []block {
	return blocksOf(src, markdown.Parse([]byte(src)))
}

func TestBlocksOfListsFrontmatterFirstAndKeysStably(t *testing.T) {
	src := "---\na: 1\n---\n\n# H\n\npara\n\npara\n"
	bs := parseBlocks(src)
	if len(bs) != 4 || bs[0].kind != kindFrontmatter || bs[1].kind != kindHeading || bs[2].kind != kindParagraph {
		t.Fatalf("got %+v", bs)
	}
	if bs[2].key == bs[3].key {
		t.Fatal("identical blocks must take distinct keys")
	}
	// inserting text above keeps the later blocks' keys
	bs2 := parseBlocks("---\na: 1\n---\n\n# H\n\nnew\n\npara\n\npara\n")
	if bs2[3].key != bs[2].key || bs2[4].key != bs[3].key {
		t.Fatalf("keys moved: %v vs %v", bs2, bs)
	}
}

func TestBlockAtIsClosedAtBothEnds(t *testing.T) {
	src := "one\n\ntwo"
	bs := parseBlocks(src)
	if blockAt(bs, 0) != 0 || blockAt(bs, 3) != 0 || blockAt(bs, 4) != -1 || blockAt(bs, 5) != 1 || blockAt(bs, 8) != 1 {
		t.Fatalf("blocks %+v", bs)
	}
	if blockBefore(bs, 4) != 0 || blockAfter(bs, 4) != 1 || blockBefore(bs, 0) != -1 || blockAfter(bs, 9) != -1 {
		t.Fatal("neighbours")
	}
}

func TestEditRegion(t *testing.T) {
	at, o, n := editRegion("hello", "hellXo")
	if at != 4 || o != "" || n != "X" {
		t.Fatalf("insert: %d %q %q", at, o, n)
	}
	at, o, n = editRegion("hello", "helo")
	if (at != 2 && at != 3) || o != "l" || n != "" {
		t.Fatalf("delete: %d %q %q", at, o, n)
	}
	at, o, n = editRegion("héllo", "hállo")
	if at != 1 || o != "é" || n != "á" {
		t.Fatalf("utf8: %d %q %q", at, o, n)
	}
	at, o, n = editRegion("same", "same")
	if at != 4 || o != "" || n != "" {
		t.Fatalf("same: %d %q %q", at, o, n)
	}
}

func TestGapPadding(t *testing.T) {
	cases := []struct {
		src           string
		pos           int
		before, after string
	}{
		{"para\n\n", 6, "", ""},
		{"para\n", 5, "\n", ""},
		{"para", 4, "\n\n", ""},
		{"\n\nnext", 0, "", ""},
		{"\nnext", 0, "", "\n"},
		{"next", 0, "", "\n\n"},
		{"a\n\n\n\nb", 3, "", ""},
		{"a\n\n\nb", 2, "\n", ""},
	}
	for _, tc := range cases {
		b, a := gapPadding(tc.src, tc.pos)
		if b != tc.before || a != tc.after {
			t.Errorf("%q@%d: got %q %q want %q %q", tc.src, tc.pos, b, a, tc.before, tc.after)
		}
	}
}

func TestHistoryCoalescesTypingAndSealsStructure(t *testing.T) {
	now := time.Unix(0, 0)
	h := history{now: func() time.Time { return now }}
	h.record(entry{at: 0, old: "", new: "a", caretBefore: 0, caretAfter: 1})
	now = now.Add(100 * time.Millisecond)
	h.record(entry{at: 1, old: "", new: "b", caretBefore: 1, caretAfter: 2})
	if len(h.undo) != 1 || h.undo[0].new != "ab" || h.undo[0].caretAfter != 2 {
		t.Fatalf("typing did not coalesce: %+v", h.undo)
	}
	// a deletion right before the open entry joins too
	h.seal()
	h.record(entry{at: 1, old: "b", new: "", caretBefore: 2, caretAfter: 1})
	now = now.Add(100 * time.Millisecond)
	h.record(entry{at: 0, old: "a", new: "", caretBefore: 1, caretAfter: 0})
	if len(h.undo) != 2 || h.undo[1].old != "ab" || h.undo[1].at != 0 {
		t.Fatalf("backspacing did not coalesce: %+v", h.undo)
	}
	// past the window a new entry opens
	now = now.Add(2 * time.Second)
	h.record(entry{at: 0, old: "", new: "z"})
	if len(h.undo) != 3 {
		t.Fatalf("window: %+v", h.undo)
	}
	// undo pops to redo; a record clears redo
	if _, ok := h.pop(); !ok || len(h.redo) != 1 {
		t.Fatal("pop")
	}
	h.record(entry{at: 0, old: "", new: "q"})
	if len(h.redo) != 0 {
		t.Fatal("redo must clear on record")
	}
}

func TestApplyAndInvertRoundTrip(t *testing.T) {
	src := "hello world"
	e := entry{at: 6, old: "world", new: "there", caretBefore: 11, caretAfter: 11}
	next := apply(src, e.at, e.old, e.new)
	if next != "hello there" {
		t.Fatal(next)
	}
	inv := invert(e)
	if back := apply(next, inv.at, inv.old, inv.new); back != src {
		t.Fatal(back)
	}
}

func TestMarkerLen(t *testing.T) {
	if markerLen("## Title", kindHeading) != 3 || markerLen("#tag", kindHeading) != 1 || markerLen("- item", kindList) != 2 || markerLen("> q", kindQuote) != 2 || markerLen("para", kindParagraph) != 0 {
		t.Fatal("marker lengths")
	}
}
