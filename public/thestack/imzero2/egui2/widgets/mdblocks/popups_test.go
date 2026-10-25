package mdblocks

import (
	"testing"

	"github.com/stergiotis/boxer/public/keelson/runtime/widgethandle"
	c "github.com/stergiotis/boxer/public/thestack/imzero2/egui2/bindings"
	"github.com/stergiotis/boxer/public/thestack/imzero2/egui2/keycodes"
)

func TestFindLinkAtKnowsBothSpellingsAndSkipsImages(t *testing.T) {
	buf := "see [docs](https://x.y/d \"t\") and [[Page|alias]] and ![img](i.png)"
	l, ok := findLinkAt(buf, 6)
	if !ok || buf[l.start:l.stop] != `[docs](https://x.y/d "t")` || buf[l.tStart:l.tStop] != "https://x.y/d" || l.label != "docs" {
		t.Fatalf("md link: %+v %v", l, ok)
	}
	if _, ok := findLinkAt(buf, 2); ok {
		t.Fatal("outside a link")
	}
	l, ok = findLinkAt(buf, 40)
	if !ok || buf[l.start:l.stop] != "[[Page|alias]]" || buf[l.tStart:l.tStop] != "Page" || l.label != "alias" {
		t.Fatalf("wiki link: %+v %v", l, ok)
	}
	if _, ok := findLinkAt(buf, len(buf)-3); ok {
		t.Fatal("an image is not a link")
	}
	// The edges count as inside, so a caret just after the link still
	// sees it.
	if _, ok := findLinkAt(buf, 29); !ok {
		t.Fatal("edge")
	}
}

func TestSlashQueryReadsALineStartSlash(t *testing.T) {
	cases := []struct {
		buf   string
		caret int
		at    int
		query string
		ok    bool
	}{
		{"/", 1, 0, "", true},
		{"/hea", 4, 0, "hea", true},
		{"  /x", 4, 2, "x", true},
		{"para\n/co", 8, 5, "co", true},
		{"a/b", 3, 0, "", false},
		{"/a.b", 4, 0, "", false},
		{"/head and more", 5, 0, "head", true},
		{"", 0, 0, "", false},
	}
	for _, tc := range cases {
		at, q, ok := slashQuery(tc.buf, tc.caret)
		if ok != tc.ok || (ok && (at != tc.at || q != tc.query)) {
			t.Errorf("%q@%d: at %d query %q ok %v", tc.buf, tc.caret, at, q, ok)
		}
	}
	if m := slashMatches("HEAD"); len(m) != 3 || slashItems[m[0]].name != "Heading 1" {
		t.Fatalf("matches: %v", m)
	}
	if m := slashMatches("zzz"); len(m) != 0 {
		t.Fatalf("no match: %v", m)
	}
}

// withRects gives the open block a geometry report, which is what lets a
// popup show: one row, the caret on it.
func withRects(e *Editor) {
	c.CurrentApplicationState.StateManager.ScriptTextRects(widgethandle.Make(e.editorId),
		c.TextRectsValue{Rows: 1, Primary: c.UiRectValue{MinX: 10, MinY: 20, MaxX: 11, MaxY: 36}})
}

func TestSlashMenuInsertsABlockTypeOnEnter(t *testing.T) {
	e := newTestEditor(t, "para\n\n")
	e.FocusAt(6)
	frame(e)
	typeInto(e, "/he", 3)
	withRects(e)
	frame(e)
	if !e.slash.open || len(e.slash.matches) != 3 {
		t.Fatalf("menu: open %v matches %v", e.slash.open, e.slash.matches)
	}
	if !e.applySlashKey(c.CapturedKey{Code: keycodes.ArrowDown}) || e.slash.sel != 1 {
		t.Fatalf("down: sel %d", e.slash.sel)
	}
	if !e.applySlashKey(c.CapturedKey{Code: keycodes.Enter}) {
		t.Fatal("enter")
	}
	if e.src != "para\n\n## " || e.caret != 9 || e.slash.open {
		t.Fatalf("inserted: %q caret %d open %v", e.src, e.caret, e.slash.open)
	}
	e.Undo()
	if e.src != "para\n\n/he" {
		t.Fatalf("undo: %q", e.src)
	}
}

func TestSlashMenuEscapeDismissesUntilTheCaretLeaves(t *testing.T) {
	e := newTestEditor(t, "/")
	e.FocusAt(1)
	frame(e)
	typeInto(e, "/", 1)
	withRects(e)
	frame(e)
	if !e.slash.open {
		t.Fatal("open")
	}
	if !e.applySlashKey(c.CapturedKey{Code: keycodes.Escape}) || e.slash.open {
		t.Fatal("escape")
	}
	withRects(e)
	frame(e)
	if e.slash.open {
		t.Fatal("dismissed slash reopened")
	}
	// Not a menu key once closed: Escape falls through to the editor.
	if e.applySlashKey(c.CapturedKey{Code: keycodes.Escape}) {
		t.Fatal("closed menu took a key")
	}
}

func TestSlashOnALaterLineStartsItsOwnBlock(t *testing.T) {
	e := newTestEditor(t, "one\ntwo")
	e.FocusAt(7)
	frame(e)
	typeInto(e, "one\ntwo\n/", 9)
	withRects(e)
	frame(e)
	if !e.slash.open {
		t.Fatal("open")
	}
	e.chooseSlash(0)
	if e.src != "one\ntwo\n\n# " || e.caret != len("one\ntwo\n\n# ") {
		t.Fatalf("%q caret %d", e.src, e.caret)
	}
}

func TestBubbleWrapsTheSelection(t *testing.T) {
	e := newTestEditor(t, "some words here")
	e.FocusAt(5)
	frame(e)
	e.bufCursor = c.PackCursorRange(5, 10)
	withRects(e)
	frame(e)
	if !e.bubble.shown {
		t.Fatal("bubble")
	}
	e.wrapSelection("bold", 5, 10)
	if e.src != "some **words** here" || e.caret != 14 {
		t.Fatalf("%q caret %d", e.src, e.caret)
	}
	frame(e)
	if e.buf != "some **words** here" || e.bubble.shown {
		t.Fatalf("rebound %q bubble %v", e.buf, e.bubble.shown)
	}
}

func TestLinkPopoverEditsTheTargetAndUnlinks(t *testing.T) {
	e := newTestEditor(t, "a [b](c) d")
	e.FocusAt(5)
	frame(e)
	typeInto(e, "a [b](c) d", 5)
	withRects(e)
	frame(e)
	if !e.link.keyOk || e.link.target != "c" {
		t.Fatalf("popover: %+v", e.link)
	}
	// The field wrote back a longer target; the source follows and the
	// caret past the target moves along.
	e.link.target = "cc"
	e.bufCursor = c.PackCursorRange(8, 8)
	withRects(e)
	frame(e)
	if e.src != "a [b](cc) d" || e.caret != 9 {
		t.Fatalf("retarget: %q caret %d", e.src, e.caret)
	}
	// The block's buffer follows the splice on the next frame, which is
	// the frame the popover reads it on.
	withRects(e)
	frame(e)
	l, ok := findLinkAt(e.buf, 5)
	if !ok || e.buf != "a [b](cc) d" {
		t.Fatalf("still a link: %q", e.buf)
	}
	e.splice(e.bufStart+l.start, e.buf[l.start:l.stop], l.label, e.bufStart+l.start+len(l.label), true)
	if e.src != "a b d" {
		t.Fatalf("unlink: %q", e.src)
	}
}
