package mdblocks

import (
	"regexp"
	"strings"

	"github.com/stergiotis/boxer/public/keelson/runtime/widgethandle"
	"github.com/stergiotis/boxer/public/semistructured/markdown/mdline"
	"github.com/stergiotis/boxer/public/thestack/fffi2/typed"
	c "github.com/stergiotis/boxer/public/thestack/imzero2/egui2/bindings"
	"github.com/stergiotis/boxer/public/thestack/imzero2/egui2/keycodes"
)

// The three popups over an open text block (ADR-0334 §SD4), each anchored
// at the caret's reported geometry (ADR-0130, reportCursorRects): a bubble
// toolbar above a selection with the inline wraps; a link popover under a
// caret that sits inside a link, with the target as a field; and a slash
// menu under a caret that follows a `/` at a line's start, listing the
// block types to insert. One shows at a time, in that order of claim.
//
// Each is an egui window without a title bar, placed by DefaultPos, which
// egui honours only when the window has no position yet — so the window's
// id carries a sequence that bumps whenever the anchor moves, and a moved
// popup is a fresh window at the new place. What a popup does to the text
// is a splice of the source with the caret set after it and focus asked
// back for the block, the path a line gesture takes (applyLine): the
// buffer is re-derived and rebound on the next frame, so the click that
// took focus to a button leaves no trace.

// anchorSeq is a popup's place on screen and the window sequence that
// re-anchors it when the place moves.
type anchorSeq struct {
	x, y  float32
	seq   uint64
	shown bool
}

// at reports the sequence for a popup at (x, y), fresh when the anchor
// moved by more than a point since it was last shown.
func (a *anchorSeq) at(x, y float32) uint64 {
	if !a.shown || abs32(x-a.x) > 1 || abs32(y-a.y) > 1 {
		a.seq++
		a.x, a.y = x, y
	}
	a.shown = true
	return a.seq
}

func (a *anchorSeq) hide() { a.shown = false }

func abs32(v float32) float32 {
	if v < 0 {
		return -v
	}
	return v
}

// popup is the frame the three popups share: no title bar, no resize,
// above the app window, at the anchor; the window's id is the sequence
// under the popup's own scope, and the body's ids live in that scope.
func (e *Editor) popup(key string, seq uint64, x, y float32, body func()) {
	for range c.IdScope(e.ids.PrepareStr(key)) {
		for range c.Window(e.ids.PrepareSeq(seq), atomsPopupTitle).
			TitleBar(false).Resizable(false).Movable(false).AlwaysOnTop(true).
			DefaultPos(x, y).KeepIter() {
			body()
		}
	}
}

var atomsPopupTitle = c.WidgetText().Text("editing").Keep()

// textRects is the open block's geometry report, when there is one.
func (e *Editor) textRects() (r c.TextRectsValue, ok bool) {
	if !e.editing || e.sub.kind != subNone || e.editorId == 0 {
		return
	}
	return c.CurrentApplicationState.StateManager.GetTextRects(widgethandle.Make(e.editorId))
}

// renderPopups draws whichever popup the caret claims this frame.
func (e *Editor) renderPopups() {
	r, ok := e.textRects()
	if !ok {
		e.bubble.hide()
		e.link.hide()
		e.slash.close()
		return
	}
	lo, hi := c.UnpackCursorRange(e.bufCursor)
	caret := mdline.CharToByte(e.buf, lo)
	if at, query, isSlash := slashQuery(e.buf, caret); isSlash && !e.slash.dismissedAt(e.bufStart+at) {
		e.bubble.hide()
		e.link.hide()
		e.renderSlash(r, at, query)
		return
	}
	e.slash.close()
	if lo != hi {
		e.link.hide()
		e.renderBubble(r, lo, hi)
		return
	}
	e.bubble.hide()
	if l, found := findLinkAt(e.buf, caret); found {
		e.renderLink(r, l)
		return
	}
	e.link.hide()
}

// --- the bubble toolbar ---

// bubbleWraps are the toolbar's buttons: the inline wraps of mdline.Wraps,
// labelled as mdedit's formatting bar labels them.
var bubbleWraps = []struct {
	key   string
	atoms typed.RetainedFffiHolderTyped[c.AtomsS]
}{
	{"bold", c.Atoms().Text("B").Keep()},
	{"italic", c.Atoms().Text("I").Keep()},
	{"code", c.Atoms().Text("`").Keep()},
	{"strike", c.Atoms().Text("S").Keep()},
	{"mark", c.Atoms().Text("H").Keep()},
	{"link", c.Atoms().Text("link").Keep()},
}

// bubbleRise is how far above the selection's top row the toolbar sits.
const bubbleRise = 34

func (e *Editor) renderBubble(r c.TextRectsValue, lo, hi int) {
	x := min(r.Primary.MinX, r.Secondary.MinX)
	y := min(r.Primary.MinY, r.Secondary.MinY) - bubbleRise
	seq := e.bubble.at(x, y)
	e.popup("bubble", seq, x, y, func() {
		for range c.Horizontal().KeepIter() {
			for _, w := range bubbleWraps {
				if c.Button(e.ids.PrepareStr(w.key), w.atoms).Small().SendResp().HasPrimaryClicked() {
					e.wrapSelection(w.key, lo, hi)
				}
			}
		}
	})
}

// wrapSelection replaces the selection [lo, hi) of the buffer, in chars,
// with its wrap, the caret after it; an undo entry of its own.
func (e *Editor) wrapSelection(key string, lo, hi int) {
	if lo > hi {
		lo, hi = hi, lo
	}
	snippet := mdline.WrapSnippet(e.buf, mdline.Wraps[key], lo, hi)
	a, b := mdline.CharToByte(e.buf, lo), mdline.CharToByte(e.buf, hi)
	e.splice(e.bufStart+a, e.buf[a:b], snippet, e.bufStart+a+len(snippet), true)
	e.pendingCaretOk = true
	e.pendingFocus = true
}

// --- the link popover ---

// linkAt is an inline link in the buffer: its byte range, its target's
// byte range, and the text that stands for it without the link.
type linkAt struct {
	start, stop   int
	tStart, tStop int
	label         string
}

var (
	// mdLinkRe is `[label](target "title")`; the leading `!` of an image
	// is matched so the match can be skipped rather than found inside.
	mdLinkRe = regexp.MustCompile(`!?\[([^\]\n]*)\]\(([^)\s]*)(?:\s+"[^"\n]*")?\)`)
	// wikiLinkRe is `[[target|alias]]`, the alias optional.
	wikiLinkRe = regexp.MustCompile(`!?\[\[([^\]|\n]*)(?:\|([^\]\n]*))?\]\]`)
)

// findLinkAt is the link the caret (a byte offset) sits in or at the edge
// of, images excluded.
func findLinkAt(buf string, caret int) (l linkAt, ok bool) {
	for _, m := range mdLinkRe.FindAllStringSubmatchIndex(buf, -1) {
		if buf[m[0]] == '!' || caret < m[0] || caret > m[1] {
			continue
		}
		return linkAt{start: m[0], stop: m[1], tStart: m[4], tStop: m[5], label: buf[m[2]:m[3]]}, true
	}
	for _, m := range wikiLinkRe.FindAllStringSubmatchIndex(buf, -1) {
		if buf[m[0]] == '!' || caret < m[0] || caret > m[1] {
			continue
		}
		label := buf[m[2]:m[3]]
		if m[4] >= 0 {
			label = buf[m[4]:m[5]]
		}
		return linkAt{start: m[0], stop: m[1], tStart: m[2], tStop: m[3], label: label}, true
	}
	return
}

// linkState is the popover: which link it is over, and the target field's
// binding.
type linkState struct {
	anchor anchorSeq
	// key tells one link from another: where it starts in the source.
	key    int
	keyOk  bool
	target string
	rebind bool
}

func (s *linkState) hide() {
	s.anchor.hide()
	s.keyOk = false
}

var (
	atomsLinkCopy   = c.Atoms().Text("Copy").Keep()
	atomsLinkUnlink = c.Atoms().Text("Unlink").Keep()
)

// linkDrop is how far under the caret's row the popover sits.
const linkDrop = 4

func (e *Editor) renderLink(r c.TextRectsValue, l linkAt) {
	key := e.bufStart + l.start
	current := e.buf[l.tStart:l.tStop]
	if !e.link.keyOk || e.link.key != key {
		// A different link: the field takes its target, and the binding
		// is overridden so the widget drops the last link's text.
		e.link.key, e.link.keyOk = key, true
		e.link.target = current
		e.link.rebind = true
	} else if e.link.target != current {
		// The field wrote back a new target: it replaces the old one in
		// the source, and the block's buffer follows by its rebind. The
		// caret stays where it is, moved along if it sat past the target.
		caret := e.caret
		if caret > e.bufStart+l.tStop {
			caret += len(e.link.target) - len(current)
		}
		e.splice(e.bufStart+l.tStart, current, e.link.target, caret, false)
		e.pendingCaretOk = true
	}
	x, y := r.Primary.MinX, r.Primary.MaxY+linkDrop
	seq := e.link.anchor.at(x, y)
	e.popup("link", seq, x, y, func() {
		for range c.Horizontal().KeepIter() {
			te := c.TextEdit(e.ids.PrepareStr("target"), e.link.target, false).
				DesiredWidth(320).HintText("target")
			te.SendRespVal(&e.link.target)
			if e.link.rebind {
				c.CurrentApplicationState.StateManager.OverrideDatabindingSPtr(&e.link.target)
				e.link.rebind = false
			}
			if c.Button(e.ids.PrepareStr("copy"), atomsLinkCopy).Small().SendResp().HasPrimaryClicked() {
				e.copyRequest = e.link.target
			}
			if c.Button(e.ids.PrepareStr("unlink"), atomsLinkUnlink).Small().SendResp().HasPrimaryClicked() {
				e.splice(e.bufStart+l.start, e.buf[l.start:l.stop], l.label, e.bufStart+l.start+len(l.label), true)
				e.pendingCaretOk = true
				e.pendingFocus = true
			}
		}
	})
}

// --- the slash menu ---

// slashItem is one block type the menu inserts: its name, the text that
// replaces the slash and its query, and where the caret lands in it.
type slashItem struct {
	name  string
	text  string
	caret int
	atoms typed.RetainedFffiHolderTyped[c.AtomsS]
}

func item(name, text string, caret int) slashItem {
	return slashItem{name: name, text: text, caret: caret, atoms: c.Atoms().Text(name).Keep()}
}

// slashItems are the block types, in the order the menu lists them.
var slashItems = []slashItem{
	item("Heading 1", "# ", 2),
	item("Heading 2", "## ", 3),
	item("Heading 3", "### ", 4),
	item("Bulleted list", "- ", 2),
	item("Numbered list", "1. ", 3),
	item("Task list", "- [ ] ", 6),
	item("Quote", "> ", 2),
	item("Callout", "> [!note]\n> ", 12),
	item("Code block", "```\n\n```", 4),
	item("Table", "| a | b |\n| --- | --- |\n|  |  |", 2),
	item("Image", "![alt](url)", 2),
	item("Divider", "---", 3),
}

// slashQuery reads a slash command at the caret: the line up to the caret
// is blank space, a `/` and the query typed after it. at is the slash's
// byte offset in the buffer.
func slashQuery(buf string, caret int) (at int, query string, ok bool) {
	caret = clampInt(caret, 0, len(buf))
	ls := mdline.LineStart(buf, caret)
	line := buf[ls:caret]
	lead := len(line) - len(strings.TrimLeft(line, " \t"))
	rest := line[lead:]
	if !strings.HasPrefix(rest, "/") {
		return
	}
	query = rest[1:]
	for _, r := range query {
		if !(r == ' ' || r >= 'a' && r <= 'z' || r >= 'A' && r <= 'Z' || r >= '0' && r <= '9') {
			return
		}
	}
	return ls + lead, query, true
}

// slashMatches are the items whose name holds the query, case ignored.
func slashMatches(query string) (idx []int) {
	q := strings.ToLower(strings.TrimSpace(query))
	for i, it := range slashItems {
		if strings.Contains(strings.ToLower(it.name), q) {
			idx = append(idx, i)
		}
	}
	return
}

// slashState is the menu: open with a highlighted entry, or dismissed for
// one slash by Escape until the caret leaves it.
type slashState struct {
	anchor      anchorSeq
	open        bool
	sel         int
	matches     []int
	at          int
	dismissed   int
	dismissedOk bool
}

func (s *slashState) close() {
	s.anchor.hide()
	s.open = false
}

// dismissedAt says Escape closed the menu for the slash at this source
// offset; a slash anywhere else clears the dismissal.
func (s *slashState) dismissedAt(src int) bool {
	if s.dismissedOk && s.dismissed != src {
		s.dismissedOk = false
	}
	return s.dismissedOk
}

// slashKeyMask is what the open block captures while the menu is open.
var slashKeyMask = keycodes.MaskOf(keycodes.ArrowUp, keycodes.ArrowDown)

func (e *Editor) renderSlash(r c.TextRectsValue, at int, query string) {
	s := &e.slash
	s.matches = slashMatches(query)
	if len(s.matches) == 0 {
		s.close()
		return
	}
	s.open = true
	s.at = at
	s.sel = clampInt(s.sel, 0, len(s.matches)-1)
	x, y := r.Primary.MinX, r.Primary.MaxY+linkDrop
	seq := s.anchor.at(x, y)
	e.popup("slash", seq, x, y, func() {
		for range c.Vertical().KeepIter() {
			for n, i := range s.matches {
				it := slashItems[i]
				if c.Button(e.ids.PrepareSeq(uint64(i)), it.atoms).Frame(false).Selected(n == s.sel).SendResp().HasPrimaryClicked() {
					e.chooseSlash(i)
				}
			}
		}
	})
}

// applySlashKey is a captured key while the menu is open: the arrows move
// the highlight, Enter inserts, Escape dismisses. Reports whether the key
// was the menu's.
func (e *Editor) applySlashKey(k c.CapturedKey) bool {
	s := &e.slash
	if !s.open || k.Command() {
		return false
	}
	switch k.Code {
	case keycodes.ArrowUp:
		s.sel = clampInt(s.sel-1, 0, max(len(s.matches)-1, 0))
	case keycodes.ArrowDown:
		s.sel = clampInt(s.sel+1, 0, max(len(s.matches)-1, 0))
	case keycodes.Enter:
		if s.sel < len(s.matches) {
			e.chooseSlash(s.matches[s.sel])
		}
	case keycodes.Escape:
		s.dismissed, s.dismissedOk = e.bufStart+s.at, true
		s.close()
	default:
		return false
	}
	return true
}

// chooseSlash replaces the slash and its query with the item's text. A
// slash on a line after the block's first gets a blank line before the
// text, so what it inserts is a block of its own rather than a line the
// paragraph above would absorb.
func (e *Editor) chooseSlash(i int) {
	it := slashItems[i]
	s := &e.slash
	lo, _ := c.UnpackCursorRange(e.bufCursor)
	caret := mdline.CharToByte(e.buf, lo)
	if caret < s.at {
		return
	}
	text, land := it.text, it.caret
	at := s.at
	if ls := mdline.LineStart(e.buf, at); ls > 0 {
		at = ls
		text = "\n" + e.buf[ls:s.at] + text
		land += 1 + s.at - ls
	}
	e.splice(e.bufStart+at, e.buf[at:caret], text, e.bufStart+at+land, true)
	e.pendingCaretOk = true
	e.pendingFocus = true
	s.close()
}
