package mdblocks

import (
	"regexp"
	"strings"

	"github.com/stergiotis/boxer/public/keelson/designsystem/styletokens"
	"github.com/stergiotis/boxer/public/keelson/runtime/widgethandle"
	c "github.com/stergiotis/boxer/public/thestack/imzero2/egui2/bindings"
	"github.com/stergiotis/boxer/public/thestack/imzero2/egui2/keycodes"
	"github.com/stergiotis/boxer/public/thestack/imzero2/egui2/widgets/color"
	"github.com/stergiotis/boxer/public/thestack/imzero2/egui2/widgets/markdown"
)

// The sub-editors (ADR-0334 §SD3): four kinds of block are not edited as
// one text field. A fenced code block is a language field over a monospace
// body with the fence lines kept out of reach; a table is a grid of cell
// fields with Tab and Enter between them and handles whose menus reshape
// it (table.go, table_grid.go); an image is its picture over
// fields for the alt text, the target and the size; a callout is a type
// menu, a fold switch and a title over a body with the `> ` prefixes
// stripped. Each parses the block's text into fields on open, composes the
// block's text from the fields after every write-back, and splices only the
// bytes that differ — the table and the callout re-emit the rows and lines
// they rewrite, the fidelity exception §SD1 states.

type subKindE uint8

const (
	subNone subKindE = iota
	subCode
	subTable
	subCallout
	subImage
)

// subState is the open sub-editor: the block it is over and its fields,
// which the TextEdits are bound to.
type subState struct {
	kind  subKindE
	start int
	// loadedText is the block's text the fields were parsed from, or the
	// text they last composed and spliced. A block whose text still equals
	// it has nothing newer than the fields; one that differs — the parse
	// re-split it, or the source moved under the editor — reloads them.
	// composed is what the fields composed to at that point: while they
	// still compose to it nothing was edited, and a spelling the composer
	// would normalise stays as written.
	loadedText string
	composed   string

	// code: the fence lines verbatim and what sits between them.
	codeOpenRun, codeClose string
	codeLang, codeBody     string
	// codeHasBody tells a fence with one empty body line from one with
	// none, which compose cannot read off an empty body string.
	codeHasBody            bool
	codeInsert             string
	codeLangId, codeBodyId uint64

	// callout
	calType, calFold, calTitle, calBody string
	calTypeId, calTitleId, calBodyId    uint64

	// image
	imgWiki                            bool
	imgAlt, imgTarget, imgSize, imgTit string
	imgIds                             [3]uint64

	// table: the rows and what reshapes them (table.go).
	tbl tableState

	rebind bool
}

var (
	calloutHeaderRe = regexp.MustCompile(`^>\s*\[!([^\]]+)\]([+-]?)[ \t]?(.*)$`)
	imageMdRe       = regexp.MustCompile(`^!\[([^\]]*)\]\(([^\s)]+)(?:\s+"([^"]*)")?\)\s*$`)
	imageWikiRe     = regexp.MustCompile(`^!\[\[([^\]|]+)(?:\|([^\]]*))?\]\]\s*$`)
	tableDelimRe    = regexp.MustCompile(`^\s*\|?\s*:?-+:?\s*(\|\s*:?-+:?\s*)*\|?\s*$`)
)

// subKindOf says which sub-editor a block opens with, or none for a plain
// text field.
func subKindOf(text string, kind blockKindE) subKindE {
	switch kind {
	case kindCode:
		if _, n := fenceRunOf(text); n > 0 {
			return subCode
		}
	case kindTable:
		lines := strings.Split(text, "\n")
		if len(lines) >= 2 && tableDelimRe.MatchString(lines[1]) {
			return subTable
		}
	case kindCallout:
		if calloutHeaderRe.MatchString(firstLine(text)) {
			return subCallout
		}
	case kindParagraph:
		if imageMdRe.MatchString(text) || imageWikiRe.MatchString(text) {
			return subImage
		}
	}
	return subNone
}

func firstLine(s string) string {
	if i := strings.IndexByte(s, '\n'); i >= 0 {
		return s[:i]
	}
	return s
}

// fenceRunOf is a fence line's run of ` or ~ after any indentation and
// quote markers: the prefix through the run, and the run length (0 for no
// fence).
func fenceRunOf(line string) (prefix string, n int) {
	i := 0
	for i < len(line) && (line[i] == ' ' || line[i] == '>' || line[i] == '\t') {
		i++
	}
	if i >= len(line) || (line[i] != '`' && line[i] != '~') {
		return "", 0
	}
	ch := line[i]
	j := i
	for j < len(line) && line[j] == ch {
		j++
	}
	if j-i < 3 {
		return "", 0
	}
	return line[:j], j - i
}

// load parses the block's text into the fields of its sub-editor kind.
func (s *subState) load(text string, kind subKindE, start int) {
	*s = subState{kind: kind, start: start, loadedText: text, rebind: true}
	switch kind {
	case subCode:
		lines := strings.Split(text, "\n")
		run, n := fenceRunOf(lines[0])
		s.codeOpenRun = run
		s.codeLang = strings.TrimSpace(lines[0][len(run):])
		body := lines[1:]
		if len(body) > 0 {
			if r2, n2 := fenceRunOf(body[len(body)-1]); n2 >= n && r2[len(r2)-1] == run[len(run)-1] && strings.TrimSpace(body[len(body)-1][len(r2):]) == "" {
				s.codeClose = body[len(body)-1]
				body = body[:len(body)-1]
			}
		}
		s.codeBody = strings.Join(body, "\n")
		s.codeHasBody = len(body) > 0
	case subCallout:
		lines := strings.Split(text, "\n")
		m := calloutHeaderRe.FindStringSubmatch(lines[0])
		s.calType, s.calFold, s.calTitle = m[1], m[2], m[3]
		body := make([]string, 0, len(lines)-1)
		for _, l := range lines[1:] {
			l = strings.TrimPrefix(strings.TrimLeft(l, " \t"), ">")
			l = strings.TrimPrefix(l, " ")
			body = append(body, l)
		}
		s.calBody = strings.Join(body, "\n")
	case subImage:
		if m := imageMdRe.FindStringSubmatch(text); m != nil {
			s.imgAlt, s.imgTarget, s.imgTit = m[1], m[2], m[3]
		} else if m := imageWikiRe.FindStringSubmatch(text); m != nil {
			s.imgWiki = true
			s.imgTarget, s.imgSize = m[1], m[2]
		}
	case subTable:
		s.tbl.load(text)
	}
	s.composed = s.compose()
}

// compose is the block's text as the fields now say it.
func (s *subState) compose() string {
	switch s.kind {
	case subCode:
		out := s.codeOpenRun + s.codeLang
		if s.codeHasBody || s.codeBody != "" {
			out += "\n" + s.codeBody
		}
		if s.codeClose != "" {
			out += "\n" + s.codeClose
		}
		return out
	case subCallout:
		var b strings.Builder
		b.WriteString("> [!" + s.calType + "]" + s.calFold)
		if s.calTitle != "" {
			b.WriteString(" " + s.calTitle)
		}
		for _, l := range strings.Split(s.calBody, "\n") {
			if l == "" {
				b.WriteString("\n>")
			} else {
				b.WriteString("\n> " + l)
			}
		}
		return b.String()
	case subImage:
		if s.imgWiki {
			if s.imgSize != "" {
				return "![[" + s.imgTarget + "|" + s.imgSize + "]]"
			}
			return "![[" + s.imgTarget + "]]"
		}
		out := "![" + s.imgAlt + "](" + s.imgTarget
		if s.imgTit != "" {
			out += ` "` + s.imgTit + `"`
		}
		return out + ")"
	case subTable:
		return s.tbl.compose()
	}
	return ""
}

// --- the editor's hooks ---

// openSub loads the sub-editor for the block at start, or clears it for a
// block that edits as text.
func (e *Editor) openSub(blk block) {
	text := e.src[blk.start:blk.stop]
	kind := subKindOf(text, blk.kind)
	if kind == subNone {
		e.sub = subState{}
		return
	}
	if e.sub.kind == kind && e.sub.start == blk.start {
		if text == e.sub.loadedText {
			// Nothing newer than the fields: a pending edit in them is
			// theirs to splice on the next write-back.
			return
		}
		// The block's text moved past what the fields were loaded from —
		// the parse re-split it after a splice, or the source changed under
		// the editor. Reload, keeping the cell that had focus.
		focusR, focusC, focusChar, focus := e.sub.tbl.focusR, e.sub.tbl.focusC, e.sub.tbl.focusChar, e.sub.tbl.focus
		e.sub.load(text, kind, blk.start)
		e.sub.tbl.focusR, e.sub.tbl.focusC, e.sub.tbl.focusChar, e.sub.tbl.focus = focusR, focusC, focusChar, focus
		return
	}
	e.sub.load(text, kind, blk.start)
}

// applySubWriteBack composes the block from the fields and splices what
// differs.
func (e *Editor) applySubWriteBack() {
	f := blockAt(e.blocks, e.caret)
	if f < 0 || e.sub.kind == subNone {
		return
	}
	blk := e.blocks[f]
	if blk.start != e.sub.start {
		// The caret moved to another block (a click) before this frame's
		// derivation: the fields describe the block they were loaded from,
		// never the one the caret is on now.
		return
	}
	sealed := e.sealNext
	e.sealNext = false
	if e.sub.kind == subTable && e.sub.tbl.expandPasted() {
		// A pasted block of cells is its own undo entry and reshaped the
		// rows, so every cell rebinds.
		sealed = true
		e.sub.rebind = true
	}
	cur := e.src[blk.start:blk.stop]
	next := e.sub.compose()
	if next == e.sub.composed || next == cur {
		return
	}
	at, oldMid, newMid := editRegion(cur, next)
	e.splice(blk.start+at, oldMid, newMid, blk.start, sealed)
	e.sub.loadedText = next
	e.sub.composed = next
}

// subFieldIds lists the sub-editor's bound fields, for reading captures.
func (s *subState) fieldIds() (ids []uint64) {
	switch s.kind {
	case subCode:
		ids = append(ids, s.codeLangId, s.codeBodyId)
	case subCallout:
		ids = append(ids, s.calTypeId, s.calTitleId, s.calBodyId)
	case subImage:
		ids = append(ids, s.imgIds[:]...)
	case subTable:
		for _, row := range s.tbl.ids {
			ids = append(ids, row...)
		}
	}
	return
}

// applySubKeys consumes the captures on the sub-editor's fields: Escape
// leaves for a block selection everywhere; Tab in a code body inserts a
// tab; a table's cells have the keys of table_grid.go, one gesture per
// frame, and a menu's operation lands first.
func (e *Editor) applySubKeys() {
	if e.sub.kind == subNone {
		return
	}
	if e.sub.kind == subTable && e.applyTableOp() {
		return
	}
	sm := c.CurrentApplicationState.StateManager
	for _, id := range e.sub.fieldIds() {
		if id == 0 {
			continue
		}
		for _, k := range sm.GetCapturedKeys(widgethandle.Make(id)) {
			switch {
			case k.Code == keycodes.Escape:
				e.selectCurrent()
				return
			case e.sub.kind == subCode && k.Code == keycodes.Tab && id == e.sub.codeBodyId:
				e.sub.codeInsert = "\t"
			case e.sub.kind == subTable:
				e.applyTableKey(id, k)
				return
			}
		}
	}
}

var (
	subFill   = color.Hex(styletokens.NeutralBgFaint.AsHex())
	subStroke = color.Hex(styletokens.AccentDefault.AsHex())
)

// renderSub draws the open sub-editor for block f.
func (e *Editor) renderSub(f int) {
	blk := e.blocks[f]
	e.editorId = 0
	fr := c.Frame(e.ids.PrepareStr("sub")).Fill(subFill).Stroke(1, subStroke).InnerMargin(6).OuterMargin(0)
	for range fr.KeepIter() {
		c.UiSetMinWidthAvailable()
		switch e.sub.kind {
		case subCode:
			e.renderCodeSub()
		case subCallout:
			e.renderCalloutSub(blk)
		case subImage:
			e.renderImageSub(blk)
		case subTable:
			e.renderTableSub()
		}
	}
	e.sub.rebind = false
}

// field is one bound single-line text field with Escape captured.
func (e *Editor) field(key string, ptr *string, width float32, hint string) (id uint64) {
	te := c.TextEdit(e.ids.PrepareStr(key), *ptr, false).
		DesiredWidth(width).HintText(hint).
		CaptureKeys(uint64(keycodes.MaskOf(keycodes.Escape)))
	id = te.Id()
	te.SendRespVal(ptr)
	if e.sub.rebind {
		c.CurrentApplicationState.StateManager.OverrideDatabindingSPtr(ptr)
	}
	return
}

func (e *Editor) width() float32 {
	if e.paneW > 0 {
		return max(e.paneW-40, 1)
	}
	return 480
}

func (e *Editor) renderCodeSub() {
	for range c.Horizontal().KeepIter() {
		c.Label(e.sub.codeOpenRun).Selectable(false).Send()
		e.sub.codeLangId = e.field("lang", &e.sub.codeLang, 140, "language")
	}
	te := c.TextEdit(e.ids.PrepareStr("body"), e.sub.codeBody, true).
		CodeEditor().
		DesiredWidth(e.width()).
		DesiredRows(uint32(countLines(e.sub.codeBody))).
		CaptureKeys(uint64(keycodes.MaskOf(keycodes.Escape))).
		CaptureTab()
	if e.sub.codeInsert != "" {
		te = te.InsertAtCursor(e.sub.codeInsert)
		e.sub.codeInsert = ""
	}
	e.sub.codeBodyId = te.Id()
	te.SendRespVal(&e.sub.codeBody)
	if e.sub.rebind {
		c.CurrentApplicationState.StateManager.OverrideDatabindingSPtr(&e.sub.codeBody)
	}
}

// The fold switch's labels, built once.
var (
	atomsFoldFixed  = c.Atoms().Text("fixed").Keep()
	atomsFoldOpen   = c.Atoms().Text("open").Keep()
	atomsFoldClosed = c.Atoms().Text("closed").Keep()
)

// calloutTypes are Obsidian's callout types, the first the default.
var calloutTypes = []string{"note", "abstract", "info", "todo", "tip", "success", "question", "warning", "failure", "danger", "bug", "example", "quote"}

func (e *Editor) renderCalloutSub(blk block) {
	for range c.Horizontal().KeepIter() {
		for range c.ComboBox(e.ids.PrepareStr("type"), c.WidgetText().Text("").Keep(), c.WidgetText().Text(e.sub.calType).Keep()).Width(130).KeepIter() {
			for i, typ := range calloutTypes {
				if c.Button(e.ids.PrepareSeq(uint64(i)), c.Atoms().Text(typ).Keep()).Frame(false).Selected(typ == e.sub.calType).SendResp().HasPrimaryClicked() {
					e.sub.calType = typ
				}
			}
		}
		if c.Button(e.ids.PrepareStr("fold-fixed"), atomsFoldFixed).Small().Selected(e.sub.calFold == "").SendResp().HasPrimaryClicked() {
			e.sub.calFold = ""
		}
		if c.Button(e.ids.PrepareStr("fold-open"), atomsFoldOpen).Small().Selected(e.sub.calFold == "+").SendResp().HasPrimaryClicked() {
			e.sub.calFold = "+"
		}
		if c.Button(e.ids.PrepareStr("fold-closed"), atomsFoldClosed).Small().Selected(e.sub.calFold == "-").SendResp().HasPrimaryClicked() {
			e.sub.calFold = "-"
		}
		e.sub.calTitleId = e.field("title", &e.sub.calTitle, 260, "title")
	}
	te := c.TextEdit(e.ids.PrepareStr("body"), e.sub.calBody, true).
		DesiredWidth(e.width()).
		DesiredRows(uint32(countLines(e.sub.calBody))).
		HintText("body").
		CaptureKeys(uint64(keycodes.MaskOf(keycodes.Escape)))
	e.sub.calBodyId = te.Id()
	te.SendRespVal(&e.sub.calBody)
	if e.sub.rebind {
		c.CurrentApplicationState.StateManager.OverrideDatabindingSPtr(&e.sub.calBody)
	}
}

func (e *Editor) renderImageSub(blk block) {
	if blk.docIndex >= 0 {
		for range c.IdScope(e.ids.PrepareStr("pic")) {
			_ = markdown.RenderBlock(markdown.Input{Ids: e.ids, ScopeKey: "block", Doc: e.doc}, blk.docIndex)
		}
	}
	for range c.Horizontal().KeepIter() {
		if e.sub.imgWiki {
			c.Label("![[").Selectable(false).Send()
			e.sub.imgIds[0] = e.field("target", &e.sub.imgTarget, 260, "file")
			c.Label("|").Selectable(false).Send()
			e.sub.imgIds[1] = e.field("size", &e.sub.imgSize, 90, "WxH")
			c.Label("]]").Selectable(false).Send()
		} else {
			c.Label("![").Selectable(false).Send()
			e.sub.imgIds[0] = e.field("alt", &e.sub.imgAlt, 200, "alt text")
			c.Label("](").Selectable(false).Send()
			e.sub.imgIds[1] = e.field("target", &e.sub.imgTarget, 260, "url")
			e.sub.imgIds[2] = e.field("title", &e.sub.imgTit, 140, "title")
			c.Label(")").Selectable(false).Send()
		}
	}
}
