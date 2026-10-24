package mdblocks

import (
	"strings"

	"github.com/stergiotis/boxer/public/keelson/designsystem/styletokens"
	"github.com/stergiotis/boxer/public/keelson/runtime/widgethandle"
	"github.com/stergiotis/boxer/public/semistructured/markdown/mdline"
	"github.com/stergiotis/boxer/public/thestack/fffi2/typed"
	c "github.com/stergiotis/boxer/public/thestack/imzero2/egui2/bindings"
	"github.com/stergiotis/boxer/public/thestack/imzero2/egui2/keycodes"
	"github.com/stergiotis/boxer/public/thestack/imzero2/egui2/widgets/codeview"
	"github.com/stergiotis/boxer/public/thestack/imzero2/egui2/widgets/color"
	"github.com/stergiotis/boxer/public/thestack/imzero2/egui2/widgets/markdown"
	"github.com/stergiotis/boxer/public/thestack/imzero2/egui2/widgets/markdownhighlight"
)

// Opts configures an Editor at construction.
type Opts struct {
	// Parse are the options every reparse uses; the zero value is the
	// markdown widget's default feature set.
	Parse markdown.ParseOptions
	// LinkClaims routes link clicks in rendered blocks to Events.Links, the
	// markdown widget's own contract; nil leaves every link a hyperlink.
	LinkClaims func(url string) bool
	// HintText is what an empty block shows while nothing is typed.
	HintText string
}

// Events is what one Render reports.
type Events struct {
	// Changed says the source differs from what the previous frame held;
	// Source() has the new text.
	Changed bool
	// Caret is the caret as a byte offset into the source, or -1 while
	// nothing is being edited.
	Caret int
	// Editing says a block is open for text editing this frame.
	Editing bool
	// CopyRequest is text the person asked to copy (Ctrl+C on a block
	// selection). The widget holds no clipboard capability; the host writes it.
	CopyRequest string
	// Links are claimed link clicks in rendered blocks.
	Links []markdown.LinkClick
}

// Editor is the semi-retained block editor over one markdown source.
type Editor struct {
	ids      *c.WidgetIdStack
	scopeKey string
	opts     Opts

	// The document.
	src    string
	doc    *markdown.Doc
	docSrc string
	docOk  bool
	blocks []block

	// The caret, a byte offset into src, and whether a block is open for
	// text editing at it.
	caret   int
	editing bool

	// The focused TextEdit's binding: buf is bound to the widget and holds
	// src[bufStart:bufStop]; a gap has bufStart == bufStop and an empty
	// buf. lastEmitted is what the widget was handed last frame, so a
	// difference on write-back is what the person typed.
	buf         string
	bufStart    int
	bufStop     int
	bufCursor   uint64
	lastEmitted string
	editorId    uint64
	// rebind asks for the databinding override after the next emit; the
	// pending caret rides with it, or alone after a gesture moved the caret.
	rebind         bool
	pendingCaret   int
	pendingCaretOk bool
	pendingFocus   bool
	pendingInsert  string

	// Block selection (Escape): a range of block indexes, inclusive.
	selMode    bool
	selA, selB int
	frameId    uint64

	hist history

	// paneW is what the pane probe last reported.
	paneW float32

	// The focused block's highlight, keyed by its text.
	hlSrc      string
	hlJob      typed.RetainedFffiHolderTyped[c.CodeViewJobS]
	hlStyled   typed.RetainedFffiHolderTyped[c.StyledSectionsS]
	hlJobOk    bool
	hlStyledOk bool

	changed     bool
	copyRequest string
}

// New makes an editor under ids, scoped by scopeKey. The source is empty
// until SetSource.
func New(ids *c.WidgetIdStack, scopeKey string, opts Opts) *Editor {
	if opts.HintText == "" {
		opts.HintText = "Write here…"
	}
	return &Editor{ids: ids, scopeKey: scopeKey, opts: opts, caret: -1}
}

// SetSource replaces the document: a new text, nothing being edited, the
// history cleared. A host whose source moved under the editor — a
// replace-all, a restored document — calls it; the editor's own edits are
// read back through Source and must not be fed through here.
func (e *Editor) SetSource(src string) {
	if src == e.src && e.docOk {
		return
	}
	e.src = src
	e.caret = -1
	e.editing = false
	e.selMode = false
	e.hist.clear()
	e.pendingInsert = ""
	e.reparse()
}

// Source is the document as it stands.
func (e *Editor) Source() string { return e.src }

// Caret is the caret as a byte offset, or -1.
func (e *Editor) Caret() int {
	if !e.editing {
		return -1
	}
	return e.caret
}

// FocusAt opens the block holding the byte offset for editing, with the
// caret there, and asks for keyboard focus.
func (e *Editor) FocusAt(off int) {
	e.caret = clampInt(off, 0, len(e.src))
	e.editing = true
	e.selMode = false
	e.pendingFocus = true
	e.pendingCaretOk = true
}

// CanUndo and CanRedo report the history's state.
func (e *Editor) CanUndo() bool { return len(e.hist.undo) > 0 }
func (e *Editor) CanRedo() bool { return len(e.hist.redo) > 0 }

// Undo reverts the most recent entry.
func (e *Editor) Undo() bool {
	ent, ok := e.hist.pop()
	if !ok {
		return false
	}
	inv := invert(ent)
	e.src = apply(e.src, inv.at, inv.old, inv.new)
	e.caret = clampInt(inv.caretAfter, 0, len(e.src))
	e.editing = true
	e.selMode = false
	e.changed = true
	e.pendingCaretOk = true
	e.pendingFocus = true
	e.reparse()
	return true
}

// Redo re-applies the most recently undone entry.
func (e *Editor) Redo() bool {
	ent, ok := e.hist.unpop()
	if !ok {
		return false
	}
	e.src = apply(e.src, ent.at, ent.old, ent.new)
	e.caret = clampInt(ent.caretAfter, 0, len(e.src))
	e.editing = true
	e.selMode = false
	e.changed = true
	e.pendingCaretOk = true
	e.pendingFocus = true
	e.reparse()
	return true
}

func (e *Editor) reparse() {
	if e.docOk && e.docSrc == e.src {
		return
	}
	e.doc = markdown.ParseWith([]byte(e.src), e.opts.Parse)
	e.docSrc = e.src
	e.docOk = true
	e.blocks = blocksOf(e.src, e.doc)
}

// splice replaces [at, at+len(old)) with new, records it, and moves the
// caret. sealed marks a structural gesture that must not coalesce with
// typing around it.
func (e *Editor) splice(at int, old, new string, caretAfter int, sealed bool) {
	before := e.caret
	e.src = apply(e.src, at, old, new)
	e.caret = clampInt(caretAfter, 0, len(e.src))
	if sealed {
		e.hist.seal()
	}
	e.hist.record(entry{at: at, old: old, new: new, caretBefore: before, caretAfter: e.caret})
	if sealed {
		e.hist.seal()
	}
	e.changed = true
	e.reparse()
}

// editorKeyMask is what the focused TextEdit always captures; the
// conditional keys (Backspace at a block start, the arrows at a block's
// ends) join it per frame.
var editorKeyMask = keycodes.MaskOf(keycodes.Enter, keycodes.Escape)

// editorChordMask is what the focused TextEdit captures under the command
// modifier: undo, redo, and the formatting wraps.
var editorChordMask = keycodes.MaskOf(keycodes.KeyZ, keycodes.KeyY, keycodes.KeyB, keycodes.KeyI, keycodes.KeyK, keycodes.KeyE)

// frameKeyMask is what the container captures while a block selection has
// focus; frameChordMask its chords.
var frameKeyMask = keycodes.MaskOf(keycodes.Escape, keycodes.Enter, keycodes.ArrowUp, keycodes.ArrowDown, keycodes.Delete, keycodes.Backspace)
var frameChordMask = keycodes.MaskOf(keycodes.KeyZ, keycodes.KeyY, keycodes.KeyC)

// wraps are the formatting chords: the bar's wraps from the keyboard.
var wraps = map[keycodes.Code]struct{ open, close, placeholder string }{
	keycodes.KeyB: {"**", "**", "bold"},
	keycodes.KeyI: {"*", "*", "italic"},
	keycodes.KeyK: {"[", "](url)", "label"},
	keycodes.KeyE: {"`", "`", "code"},
}

var (
	selectionFill   = color.Hex(styletokens.AccentSubtle.AsHex())
	selectionStroke = color.Hex(styletokens.AccentDefault.AsHex())
	clearFill       = color.Transparent
)

// Render draws the editor into the current Ui scope and reports what
// happened. The order inside is the one-frame-lag contract: what the
// TextEdit wrote back and what it captured last frame are applied first,
// the focus is derived from the caret, and only then is this frame emitted.
func (e *Editor) Render() (ev Events) {
	e.changed = false
	e.copyRequest = ""
	e.reparse()
	e.applyWriteBack()
	e.applyEditorKeys()
	e.applyFrameKeys()
	e.reparse()
	e.deriveBuffer()

	for range c.IdScope(e.ids.PrepareStr(e.scopeKey)) {
		if w, _, ok := c.CapturePaneSize(e.ids.ProbeSeq("pane")); ok && w > 0 {
			e.paneW = w
		}
		kf := c.Frame(e.ids.PrepareStr("keys")).
			CaptureKeys(uint64(frameKeyMask)).
			CaptureKeyChords(uint64(frameChordMask), uint8(keycodes.ModCommand))
		e.frameId = kf.Id()
		for range kf.KeepIter() {
			ev.Links = e.renderBlocks()
		}
	}
	ev.Changed = e.changed
	ev.Caret = e.Caret()
	ev.Editing = e.editing
	ev.CopyRequest = e.copyRequest
	return
}

// applyWriteBack turns what the TextEdit wrote back last frame — its text
// and its caret — into a splice and a caret move.
func (e *Editor) applyWriteBack() {
	if !e.editing || e.editorId == 0 {
		return
	}
	// What came back describes the end of last frame, after that frame's
	// insert, cursor and rebind were applied — so nothing here is stale
	// with respect to what this widget asked for. A caret the host or a
	// gesture queued since (pendingCaretOk) is newer than the report and
	// wins over it; the text is taken regardless.
	cursorStart, _ := c.UnpackCursorRange(e.bufCursor)
	if e.buf != e.lastEmitted {
		old := e.lastEmitted
		at, oldMid, newMid := editRegion(old, e.buf)
		caretAfter := e.bufStart + charToByte(e.buf, cursorStart)
		if e.pendingCaretOk {
			caretAfter = e.caret
		}
		if e.bufStart == e.bufStop && newMid != "" {
			// The first text in a gap becomes a block of its own.
			before, after := e.gapPadding(e.bufStart)
			e.splice(e.bufStart, "", before+e.buf+after, e.bufStart+len(before)+charToByte(e.buf, cursorStart), false)
			e.hist.seal()
			return
		}
		e.splice(e.bufStart+at, oldMid, newMid, caretAfter, false)
		return
	}
	// No text change: the caret may still have moved inside the block.
	if !e.pendingCaretOk {
		e.caret = clampInt(e.bufStart+charToByte(e.buf, cursorStart), 0, len(e.src))
	}
}

// gapPadding wraps gapPadding with the block list, so text typed at the very
// start or end of the document is padded only on the side a block is.
func (e *Editor) gapPadding(pos int) (before, after string) {
	before, after = gapPadding(e.src, pos)
	if blockBefore(e.blocks, pos) < 0 {
		before = ""
	}
	if blockAfter(e.blocks, pos) < 0 {
		after = ""
	}
	return
}

// applyEditorKeys consumes the focused TextEdit's captures.
func (e *Editor) applyEditorKeys() {
	if !e.editing || e.editorId == 0 {
		return
	}
	keys := c.CurrentApplicationState.StateManager.GetCapturedKeys(widgethandle.Make(e.editorId))
	for _, k := range keys {
		cb := clampInt(e.caret-e.bufStart, 0, len(e.buf))
		switch {
		case k.Command():
			e.applyChord(k.Code)
		case k.Code == keycodes.Enter:
			e.applyEnter(cb, k.Shift())
		case k.Code == keycodes.Tab:
			e.applyLine(mdline.Tab(e.buf, cb, k.Shift()))
		case k.Code == keycodes.Backspace:
			e.applyBackspaceAtStart()
		case k.Code == keycodes.ArrowUp:
			e.moveToNeighbour(-1)
		case k.Code == keycodes.ArrowDown:
			e.moveToNeighbour(+1)
		case k.Code == keycodes.Escape:
			e.selectCurrent()
		}
		// One gesture per frame: the next acts on a moved caret.
		return
	}
}

// applyFrameKeys consumes the container's captures, which arrive while a
// block selection holds focus.
func (e *Editor) applyFrameKeys() {
	if e.frameId == 0 {
		return
	}
	keys := c.CurrentApplicationState.StateManager.GetCapturedKeys(widgethandle.Make(e.frameId))
	for _, k := range keys {
		switch {
		case k.Command() && k.Code == keycodes.KeyZ:
			e.Undo()
		case k.Command() && k.Code == keycodes.KeyY:
			e.Redo()
		case k.Command() && k.Code == keycodes.KeyC:
			if e.selMode {
				a, b := e.selRange()
				e.copyRequest = e.src[e.blocks[a].start:e.blocks[b].stop]
			}
		case !e.selMode:
			// Nothing selected: the frame holds focus only between modes.
		case k.Code == keycodes.Escape || k.Code == keycodes.Enter:
			_, b := e.selRange()
			e.openBlock(b, e.blocks[b].stop)
		case k.Code == keycodes.ArrowUp || k.Code == keycodes.ArrowDown:
			d := -1
			if k.Code == keycodes.ArrowDown {
				d = 1
			}
			e.selB = clampInt(e.selB+d, 0, len(e.blocks)-1)
			if !k.Shift() {
				e.selA = e.selB
			}
		case k.Code == keycodes.Delete || k.Code == keycodes.Backspace:
			e.deleteSelection()
		}
		return
	}
}

func (e *Editor) selRange() (a, b int) {
	a, b = e.selA, e.selB
	if a > b {
		a, b = b, a
	}
	a = clampInt(a, 0, len(e.blocks)-1)
	b = clampInt(b, 0, len(e.blocks)-1)
	return
}

// applyChord runs an undo, a redo or a wrap from the focused TextEdit.
func (e *Editor) applyChord(code keycodes.Code) {
	switch code {
	case keycodes.KeyZ:
		e.Undo()
	case keycodes.KeyY:
		e.Redo()
	default:
		w, ok := wraps[code]
		if !ok {
			return
		}
		lo, hi := c.UnpackCursorRange(e.bufCursor)
		if lo > hi {
			lo, hi = hi, lo
		}
		inner := w.placeholder
		if lo != hi {
			if sel := e.buf[charToByte(e.buf, lo):charToByte(e.buf, hi)]; sel != "" {
				inner = sel
			}
		}
		e.pendingInsert = w.open + inner + w.close
	}
}

// applyEnter is Enter in the focused block at cb, a byte offset into buf.
func (e *Editor) applyEnter(cb int, shift bool) {
	f := blockAt(e.blocks, e.caret)
	kind := kindParagraph
	if f >= 0 {
		kind = e.blocks[f].kind
	}
	switch {
	case shift, kind == kindCode, kind == kindTable, kind == kindFrontmatter:
		e.pendingInsert = "\n"
	case kind == kindList, kind == kindQuote, kind == kindCallout:
		e.applyLine(mdline.Enter(e.buf, cb, false))
	case f < 0:
		// In a gap: nothing to split, the caret stays.
	case cb >= len(e.buf):
		e.leaveBlockBelow(f)
	default:
		// Split: a blank line at the caret makes two blocks.
		e.splice(e.caret, "", "\n\n", e.caret+2, true)
		e.pendingCaretOk = true
	}
}

// leaveBlockBelow puts the caret in the gap after block f, making the gap
// if the source has none there, so the next text typed starts a new block.
func (e *Editor) leaveBlockBelow(f int) {
	stop := e.blocks[f].stop
	switch {
	case stop >= len(e.src):
		e.splice(stop, "", "\n", stop+1, true)
	case stop+1 < len(e.src) && e.src[stop+1] == '\n':
		e.caret = stop + 1
	default:
		e.splice(stop+1, "", "\n", stop+1, true)
	}
	e.pendingCaretOk = true
}

// applyLine lands an mdline action: a snippet through the widget, a rewrite
// as a splice of the whole block.
func (e *Editor) applyLine(act mdline.Action) {
	switch {
	case act.Rebind:
		e.splice(e.bufStart, e.buf, act.Next, e.bufStart+act.Caret, true)
		e.pendingCaretOk = true
	case act.Insert != "":
		e.pendingInsert = act.Insert
	}
}

// applyBackspaceAtStart is Backspace with the caret at a block's start: a
// marker is removed first, then the block merges into the one above; a
// gap closes onto the block above it; a block above that is not text is
// selected rather than merged into.
func (e *Editor) applyBackspaceAtStart() {
	f := blockAt(e.blocks, e.caret)
	if f < 0 {
		prev := blockBefore(e.blocks, e.caret)
		if prev < 0 {
			if e.caret > 0 {
				e.splice(0, e.src[:e.caret], "", 0, true)
				e.pendingCaretOk = true
			}
			return
		}
		stop := e.blocks[prev].stop
		e.splice(stop, e.src[stop:e.caret], "", stop, true)
		e.pendingCaretOk = true
		return
	}
	b := e.blocks[f]
	if n := markerLen(e.src[b.start:b.stop], b.kind); n > 0 {
		e.splice(b.start, e.src[b.start:b.start+n], "", b.start, true)
		e.pendingCaretOk = true
		return
	}
	if f == 0 {
		return
	}
	prev := e.blocks[f-1]
	switch prev.kind {
	case kindCode, kindTable, kindRule, kindFrontmatter:
		e.selMode, e.selA, e.selB = true, f-1, f-1
		e.editing = false
		c.SurrenderFocus(e.editorId)
		c.RequestFocus(e.frameId)
		return
	}
	// Join: the gap becomes one newline, a soft break inside the block above.
	e.splice(prev.stop, e.src[prev.stop:b.start], "\n", prev.stop+1, true)
	e.pendingCaretOk = true
}

// markerLen is the length of the marker a Backspace at the block's start
// removes first: a heading's `#` run and space, a list item's or quote's
// first-line prefix.
func markerLen(text string, kind blockKindE) int {
	switch kind {
	case kindHeading:
		i := 0
		for i < len(text) && text[i] == '#' {
			i++
		}
		if i > 0 && i < len(text) && text[i] == ' ' {
			return i + 1
		}
		return i
	case kindList, kindQuote, kindCallout:
		return len(mdline.ParsePrefix(text[:mdline.LineEnd(text, 0)]).Text)
	}
	return 0
}

// moveToNeighbour moves the caret into the block above (d < 0, at its end)
// or below (d > 0, at its start), or into the gap at the document's edge.
func (e *Editor) moveToNeighbour(d int) {
	f := blockAt(e.blocks, e.caret)
	var target int
	if d < 0 {
		prev := blockBefore(e.blocks, e.caret)
		if f >= 0 {
			prev = f - 1
		}
		if prev < 0 {
			return
		}
		target = e.blocks[prev].stop
	} else {
		next := blockAfter(e.blocks, e.caret)
		if f >= 0 {
			next = f + 1
		}
		if next >= len(e.blocks) {
			return
		}
		target = e.blocks[next].start
	}
	e.caret = target
	e.pendingCaretOk = true
	e.pendingFocus = true
}

// selectCurrent leaves text editing for a block selection on the current
// block (Escape).
func (e *Editor) selectCurrent() {
	f := blockAt(e.blocks, e.caret)
	if f < 0 {
		f = blockBefore(e.blocks, e.caret)
	}
	if f < 0 {
		return
	}
	e.selMode, e.selA, e.selB = true, f, f
	e.editing = false
	c.SurrenderFocus(e.editorId)
	c.RequestFocus(e.frameId)
}

// openBlock opens block i for editing with the caret at off.
func (e *Editor) openBlock(i int, off int) {
	e.selMode = false
	e.editing = true
	e.caret = clampInt(off, e.blocks[i].start, e.blocks[i].stop)
	e.pendingCaretOk = true
	e.pendingFocus = true
}

// deleteSelection removes the selected blocks and the gap after them.
func (e *Editor) deleteSelection() {
	a, b := e.selRange()
	if len(e.blocks) == 0 {
		return
	}
	start := e.blocks[a].start
	end := e.blocks[b].stop
	if next := b + 1; next < len(e.blocks) {
		end = e.blocks[next].start
	} else {
		end = len(e.src)
	}
	e.hist.seal()
	e.splice(start, e.src[start:end], "", start, true)
	e.selMode = false
	e.editing = true
	e.pendingCaretOk = true
	e.pendingFocus = true
}

// deriveBuffer binds the TextEdit to the block the caret is in, or to an
// empty buffer at the gap, rebinding when the binding changes.
func (e *Editor) deriveBuffer() {
	if !e.editing {
		return
	}
	e.caret = clampInt(e.caret, 0, len(e.src))
	start, stop := e.caret, e.caret
	if f := blockAt(e.blocks, e.caret); f >= 0 {
		start, stop = e.blocks[f].start, e.blocks[f].stop
	}
	text := e.src[start:stop]
	if start != e.bufStart || stop != e.bufStop || text != e.buf {
		e.buf = text
		e.bufStart, e.bufStop = start, stop
		e.rebind = true
		e.pendingCaretOk = true
	}
	if e.pendingCaretOk {
		e.pendingCaret = clampInt(e.caret-e.bufStart, 0, len(e.buf))
	}
}

// renderBlocks draws every block, the focused one as a TextEdit, with a
// trailing affordance to write after the last block.
func (e *Editor) renderBlocks() (links []markdown.LinkClick) {
	gap := styletokens.PaddingDefault(styletokens.ActiveDensity())
	a, b := -1, -1
	if e.selMode {
		a, b = e.selRange()
	}
	// The editing state is read once here. A click inside the loop below
	// opens a block for the NEXT frame — its buffer is derived at the top of
	// Render — and must not make this frame emit an editor over a buffer
	// nothing has derived yet.
	editing, edStart, edStop := e.editing, e.bufStart, e.bufStop
	inGap := editing && edStart == edStop
	drawnEditor := false
	for i := range e.blocks {
		blk := e.blocks[i]
		if inGap && !drawnEditor && edStart <= blk.start {
			if i > 0 {
				c.AddSpace(gap)
			}
			e.renderEditor()
			drawnEditor = true
		}
		if i > 0 {
			c.AddSpace(gap)
		}
		if editing && !inGap && blk.start == edStart && blk.stop == edStop {
			e.renderEditor()
			drawnEditor = true
			continue
		}
		selected := e.selMode && i >= a && i <= b
		links = append(links, e.renderBlock(i, selected)...)
	}
	if editing && !drawnEditor {
		if len(e.blocks) > 0 {
			c.AddSpace(gap)
		}
		e.renderEditor()
		drawnEditor = true
	}
	if !inGap || edStart < len(e.src) {
		e.renderTail(gap, drawnEditor)
	}
	return
}

// renderBlock draws one unfocused block inside a hit frame; a click opens
// it with the caret at its end.
func (e *Editor) renderBlock(i int, selected bool) (links []markdown.LinkClick) {
	blk := e.blocks[i]
	fill, strokeW, stroke := clearFill, float32(0), clearFill
	if selected {
		fill, strokeW, stroke = selectionFill, 1, selectionStroke
	}
	var fr c.FrameFluid
	for range c.IdScope(e.ids.PrepareHighEntropy(blk.key)) {
		// Focusable, not merely click-sensing: a focusable Frame registers
		// its rect above the body, so the click lands here rather than on the
		// paragraph's own selectable text — which in an editor is what a click
		// on a block means. The click is read as the frame gaining focus; the
		// TextEdit takes that focus over on the next frame.
		fr = c.Frame(e.ids.PrepareStr("hit")).
			Fill(fill).Stroke(strokeW, stroke).
			InnerMargin(2).OuterMargin(0).
			SenseClick().Focusable().HoverCursorPointer()
		for range fr.KeepIter() {
			c.UiSetMinWidthAvailable()
			if blk.docIndex < 0 {
				markdown.RenderFrontmatter(e.doc)
			} else {
				res := markdown.RenderBlock(markdown.Input{Ids: e.ids, ScopeKey: "block", Doc: e.doc, LinkClaims: e.opts.LinkClaims}, blk.docIndex)
				links = res.Links
			}
		}
	}
	if flags := c.CurrentApplicationState.StateManager.GetResponseByIdRaw(fr.Id()); flags.HasPrimaryClicked() || flags.HasGainedFocus() {
		e.openBlock(i, blk.stop)
	}
	return
}

// renderTail is the affordance after the last block: a click puts the caret
// in a gap after it, ready to write a new block.
func (e *Editor) renderTail(gap float32, drawnEditor bool) {
	if len(e.blocks) == 0 && e.editing {
		return
	}
	c.AddSpace(gap)
	fr := c.Frame(e.ids.PrepareStr("tail")).
		Fill(clearFill).InnerMargin(4).OuterMargin(0).
		SenseClick().Focusable().HoverCursorPointer()
	for range fr.KeepIter() {
		c.UiSetMinWidthAvailable()
		c.Label("+").Selectable(false).Send()
	}
	if flags := c.CurrentApplicationState.StateManager.GetResponseByIdRaw(fr.Id()); flags.HasPrimaryClicked() || flags.HasGainedFocus() {
		e.selMode = false
		e.editing = true
		if n := len(e.blocks); n > 0 {
			e.caret = e.blocks[n-1].stop
			e.leaveBlockBelow(n - 1)
		} else {
			e.caret = len(e.src)
			e.pendingCaretOk = true
		}
		e.pendingFocus = true
	}
}

// renderEditor emits the focused TextEdit over buf and applies what was
// queued for it: the snippet, the caret, the rebind.
func (e *Editor) renderEditor() {
	e.ensureHighlight()
	mask := editorKeyMask
	cb := clampInt(e.caret-e.bufStart, 0, len(e.buf))
	lo, hi := c.UnpackCursorRange(e.bufCursor)
	if cb == 0 && lo == hi {
		mask |= keycodes.MaskOf(keycodes.Backspace, keycodes.ArrowUp)
	}
	if cb >= len(e.buf) && lo == hi {
		mask |= keycodes.MaskOf(keycodes.ArrowDown)
	}
	width := float32(480)
	if e.paneW > 0 {
		width = max(e.paneW-16, 1)
	}
	rows := uint32(countLines(e.buf))
	b := c.TextEdit(e.ids.PrepareStr("editor"), e.buf, true).
		CodeEditor().
		DesiredWidth(width).
		DesiredRows(rows).
		HintText(e.opts.HintText).
		ReportCursor().
		CaptureKeys(uint64(mask)).
		CaptureTab().
		CaptureKeyChords(uint64(editorChordMask), uint8(keycodes.ModCommand))
	if e.hlJobOk {
		b = b.HighlightJob(e.hlJob)
		if e.hlStyledOk {
			b = b.SectionStyled(e.hlStyled)
		}
	}
	if e.pendingInsert != "" {
		b = b.InsertAtCursor(e.pendingInsert)
		e.pendingInsert = ""
	}
	if e.pendingCaretOk {
		ch := byteToChar(e.buf, e.pendingCaret)
		b = b.SetCursor(c.PackCursorRange(ch, ch), e.pendingFocus)
		// The report the widget writes back carries this caret; keep the
		// field in step so the next frame's derivation sees no stale value.
		e.bufCursor = c.PackCursorRange(ch, ch)
		e.pendingCaretOk = false
		e.pendingFocus = false
	}
	e.editorId = b.Id()
	_ = b.SendRespValCursor(&e.buf, &e.bufCursor)
	e.lastEmitted = e.buf
	if e.rebind {
		c.CurrentApplicationState.StateManager.OverrideDatabindingSPtr(&e.buf)
		e.rebind = false
	}
}

// ensureHighlight builds the focused block's colours and fonts, keyed by
// its text.
func (e *Editor) ensureHighlight() {
	if e.hlJobOk && e.hlSrc == e.buf {
		return
	}
	e.hlSrc = e.buf
	if e.buf == "" {
		e.hlJobOk, e.hlStyledOk = false, false
		return
	}
	spans := markdownhighlight.HighlightLex([]byte(e.buf))
	e.hlJob = codeview.BuildMarkdownFromSpans(e.buf, spans)
	e.hlJobOk = true
	e.hlStyled, e.hlStyledOk = codeview.BuildStyledSections(codeview.MarkdownFontSections(e.buf, spans, nil))
}

// blockText is the source of block i.
func (e *Editor) blockText(i int) string {
	b := e.blocks[i]
	return strings.Clone(e.src[b.start:b.stop])
}
