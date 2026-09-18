package cardgrid

import (
	"strconv"
	"unicode/utf8"

	"github.com/stergiotis/boxer/public/keelson/designsystem/styletokens"
	"github.com/stergiotis/boxer/public/keelson/runtime/icons"
	"github.com/stergiotis/boxer/public/keelson/runtime/widgethandle"
	c "github.com/stergiotis/boxer/public/thestack/imzero2/egui2/bindings"
	"github.com/stergiotis/boxer/public/thestack/imzero2/egui2/keycodes"
	"github.com/stergiotis/boxer/public/thestack/imzero2/egui2/widgets/badge"
	"github.com/stergiotis/boxer/public/thestack/imzero2/egui2/widgets/color"
)

const (
	// minHeight floors the grid in an unbounded scroll host so its
	// ScrollArea has a bounded rect to scroll (the kanban board's idiom). A
	// bounded host (FillHost) lets it fill its rect instead.
	minHeight float32 = 420
	// fallbackPaneW stands in for the pane probe on the frame before it
	// answers.
	fallbackPaneW float32 = 880
	// scrollbarW is kept clear of the last column so the grid does not
	// reflow when the page grows a scrollbar.
	scrollbarW float32 = 14
	// toolbarGap separates the toolbar's two groups.
	toolbarGap float32 = 10
	// toneW is the accent edge's width.
	toneW float32 = 3
	// glyphPx is the hero placeholder glyph's size.
	glyphPx float32 = 28
	// scrollAlignCenter is ScrollToCursor's alignment code (0 top, 1 centre,
	// 2 bottom).
	scrollAlignCenter uint8 = 1
	// tagIdBase keeps a card's tag badges clear of its other widgets.
	tagIdBase uint64 = 0x200
)

// cullSlackRows is how many rows past the viewport are still drawn on each
// side: the viewport is last frame's, so a scroll this frame shows a row
// that is already there rather than a gap.
const cullSlackRows = 1

// gridKeyMask is what the grid eats while focused — only keys it acts on.
// Escape and Tab are absent for the tree's reasons (ADR-0177 §SD9): a
// container is one focus stop, not a trap.
var gridKeyMask = keycodes.MaskOf(
	keycodes.ArrowUp, keycodes.ArrowDown,
	keycodes.ArrowLeft, keycodes.ArrowRight,
	keycodes.Home, keycodes.End,
	keycodes.PageUp, keycodes.PageDown,
	keycodes.Space,
)

// Render draws the page where it is called and reports the frame's click and
// keys. See the package doc for the layout.
func Render(in Input) (res Result) {
	res = Result{Clicked: -1, Moved: -1, Toggled: -1}
	m, st, ids := in.Model, in.State, in.Ids
	if m == nil || st == nil || ids == nil {
		return
	}
	if err := m.Validate(); err != nil {
		for rt := range c.RichTextLabel(err.Error()) {
			rt.Small().Weak()
		}
		return
	}

	for range c.IdScope(ids.PrepareStr(in.ScopeKey)) {
		// The pane probe goes first: the rect is the room left for the next
		// widget, and it answers one frame late — hold the last good width
		// so the grid does not reflow to the fallback on a hidden→shown edge.
		paneSeq := c.ProbeSeq(in.ScopeKey, "cardgrid")
		if w, _, ok := c.CapturePaneSize(paneSeq); ok {
			st.paneW = w
		}
		// The same probe's top and height are the scroll viewport's.
		view, viewOK := c.CurrentApplicationState.StateManager.GetUiRect(paneSeq)
		if viewOK {
			st.viewTop, st.viewH = view.MinY, view.MaxY-view.MinY
		}
		paneW := st.paneW
		if paneW <= 0 {
			paneW = fallbackPaneW
		}
		lay := Plan(paneW-scrollbarW, st.density, st.aspect, m.Slots)
		applyKeys(st, lay, m.Count, &res)
		st.focused = st.keyFrameID != 0 &&
			c.CurrentApplicationState.StateManager.GetResponseByIdRaw(st.keyFrameID).HasFocus()

		// The capture Frame. CaptureKeys and NOT Focusable: a focusable rect
		// senses clicks and, registered after the body, would sit above every
		// card and eat the clicks selection is made of (the tree's finding).
		kf := c.Frame(ids.PrepareStr("keys")).CaptureKeys(uint64(gridKeyMask))
		st.keyFrameID = kf.Id()
		for range kf.KeepIter() {
			if !in.FillHost {
				c.UiSetMinHeight(minHeight)
			}
			for range c.ScrollArea().Vscroll(true).AutoShrink(false, false).KeepIter() {
				// The content's top, before anything moves the cursor: with
				// the viewport's, it gives the scroll offset.
				contentSeq := c.ProbeSeq(in.ScopeKey, "cardgrid-content")
				c.CaptureUiAvailableRect(contentSeq)
				content, contentOK := c.CurrentApplicationState.StateManager.GetUiRect(contentSeq)
				if contentOK {
					st.contentTop = content.MinY
				}
				st.haveView = viewOK && contentOK
				// Pin the content to the grid's size: the cards are placed
				// at computed rects, and an unpinned parent would size to
				// whichever of them was laid out last — and the cards the
				// cull skips would otherwise take their room with them.
				c.UiSetMinWidth(lay.GridWidth())
				c.UiSetMinHeight(lay.GridHeight(m.Count))
				reveal := st.reveal
				st.reveal = 0
				lo, hi := 0, m.Count
				if st.haveView {
					lo, hi = lay.VisibleRange(m.Count, st.viewTop-st.contentTop, st.viewH, cullSlackRows)
				}
				for i := range m.Count {
					// Off-screen cards are not emitted: a page of 96 costs
					// what the viewport shows. The card being revealed is
					// drawn wherever it is, since the scroll is asked from
					// inside it.
					if (i < lo || i >= hi) && reveal != int32(i)+1 {
						continue
					}
					for range c.IdScope(ids.PrepareSeq(uint64(i))) {
						renderCard(in, lay, i, reveal == int32(i)+1, &res)
					}
				}
			}
		}
		if res.Clicked >= 0 && st.keyFrameID != 0 {
			// Clicking a card focuses the grid, so the arrow keys work
			// straight after. The capture Frame does not sense clicks, so
			// focus is asked for here rather than arriving on its own.
			c.RequestFocus(st.keyFrameID)
		}
	}
	return
}

// applyKeys turns last frame's captured keys into selection moves. Key
// repeat delivers several per frame; each starts where the last one landed,
// and a key the page cannot hold ends the frame's keys (Result.Past).
func applyKeys(st *State, lay Layout, n int, res *Result) {
	if st.keyFrameID == 0 || n == 0 {
		return
	}
	for _, k := range c.CurrentApplicationState.StateManager.GetCapturedKeys(widgethandle.Make(st.keyFrameID)) {
		cur := int(st.Selected())
		if k.Code == keycodes.Space {
			if cur >= 0 {
				res.Toggled = int32(cur)
			}
			continue
		}
		next, past := lay.step(cur, n, k.Code)
		if past != 0 {
			res.Past = int32(past)
			return
		}
		if next >= 0 && next != cur {
			st.SetSelected(int32(next))
			st.Reveal(int32(next))
			res.Moved = int32(next)
		}
	}
}

// step is where a navigation key takes the selection from ordinal i among n
// cards: next, or — for a move past the page's edge — the ordinal delta the
// host pages by (Result.Past), next staying i. ← and → walk the reading
// order, ↑ and ↓ the rows, Home and End the page; PageUp and PageDown are
// always a page away. With nothing selected any key selects the first card.
func (inst Layout) step(i, n int, key keycodes.Code) (next int, past int) {
	if n <= 0 || inst.Cols <= 0 {
		return -1, 0
	}
	if i < 0 {
		return 0, 0
	}
	switch key {
	case keycodes.ArrowRight:
		if i == n-1 {
			return i, 1
		}
		return inst.Neighbour(i, n, 1, 0), 0
	case keycodes.ArrowLeft:
		if i == 0 {
			return i, -1
		}
		return inst.Neighbour(i, n, -1, 0), 0
	case keycodes.ArrowDown:
		if i/inst.Cols == inst.Rows(n)-1 {
			return i, inst.Cols
		}
		return inst.Neighbour(i, n, 0, 1), 0
	case keycodes.ArrowUp:
		if i < inst.Cols {
			return i, -inst.Cols
		}
		return inst.Neighbour(i, n, 0, -1), 0
	case keycodes.PageDown:
		return i, n
	case keycodes.PageUp:
		return i, -n
	case keycodes.Home:
		return 0, 0
	case keycodes.End:
		return n - 1, 0
	}
	return i, 0
}

// renderCard draws card i at its computed rect: the click-sensing surface
// first, then the slots over it (see the package doc for why that order).
func renderCard(in Input, lay Layout, i int, reveal bool, res *Result) {
	m, st, ids := in.Model, in.State, in.Ids
	x, y := lay.CardOrigin(i)
	selected := st.Selected() == int32(i)

	stroke, strokeW := tok(styletokens.NeutralBorderFaint), styletokens.StrokeHair
	if selected {
		stroke, strokeW = tok(styletokens.AccentDefault), styletokens.StrokeStrong
	}
	var fid uint64
	for range c.AllocateUiAtRect(x, y, x+lay.CardW, y+lay.CardH).KeepIter() {
		c.UiClipToMaxRect()
		if reveal {
			c.ScrollToCursor(scrollAlignCenter)
		}
		frame := c.Frame(ids.PrepareStr("card")).
			Fill(tok(styletokens.NeutralBgSurface)).
			CornerRadius(styletokens.RoundingLg).
			Stroke(strokeW, stroke).
			InnerMargin(0).
			SenseClick()
		fid = frame.Id()
		for range frame.KeepIter() {
			// The stroke is part of the frame's size; the content makes up
			// the rest of the rect whichever stroke is drawn.
			c.UiSetMinWidth(lay.CardW - 2*strokeW)
			c.UiSetMinHeight(lay.CardH - 2*strokeW)
		}
	}
	if c.CurrentApplicationState.StateManager.GetResponseByIdRaw(fid).HasPrimaryClicked() {
		st.SetSelected(int32(i))
		res.Clicked = int32(i)
	}

	if lay.Hero.H > 0 {
		renderHero(in, lay, i, x, y, res)
	}
	if m.Tone != nil && m.Tone[i].Kind() != color.ColorKindNone {
		top := y + lay.Hero.H + styletokens.RoundingLg
		if lay.Hero.H == 0 {
			top = y + styletokens.RoundingLg
		}
		fillRect(ids.PrepareStr("tone"), x+1, top, x+1+toneW, y+lay.CardH-styletokens.RoundingLg, m.Tone[i], styletokens.RoundingSm)
	}

	tx0, tx1 := x+cardPad, x+lay.CardW-cardPad
	if s := lay.Head; s.H > 0 {
		renderHead(m, lay, i, tx0, y+s.Y, tx1, y+s.Y+s.H)
	}
	if s := lay.Body; s.H > 0 {
		renderBody(in, lay, i, tx0, y+s.Y, tx1, y+s.Y+s.H, res)
	}
	if s := lay.Facts; s.H > 0 {
		renderFacts(in, lay, i, tx0, y+s.Y, tx1)
	}
	if s := lay.Tags; s.H > 0 {
		renderTags(in, lay, i, tx0, y+s.Y, tx1, y+s.Y+s.H)
	}
	if s := lay.Footer; s.H > 0 && m.Footer[i] != "" {
		text, cut := displayCut(m.Footer[i], lay.smallRunes())
		for range slot(tx0, y+s.Y, tx1, y+s.Y+s.H) {
			hover(cut, m.Footer[i], func() {
				c.LabelAtoms(c.Atoms().BeginRichText(text).Small().Weak().End().Keep()).
					Truncate().Selectable(false).Send()
			})
		}
	}
}

// renderHead draws the overline, title and subtitle as one flowing block. A
// slot the page declares and this card leaves empty takes no room in it, so
// what the card does carry stays together at the top.
func renderHead(m *Model, lay Layout, i int, x0, y0, x1, y1 float32) {
	for range slot(x0, y0, x1, y1) {
		c.UiSetItemSpacing(0, headGap)
		if m.Slots.Has(SlotsOverline) && m.Overline[i] != "" {
			text, cut := displayCut(m.Overline[i], lay.smallRunes())
			hover(cut, m.Overline[i], func() {
				c.LabelAtoms(c.Atoms().BeginRichText(text).Small().Weak().End().Keep()).
					Truncate().Selectable(false).Send()
			})
		}
		if m.Slots.Has(SlotsTitle) && m.Title[i] != "" {
			text, cut := displayCut(m.Title[i], titleLines*lay.lineRunes())
			hover(cut || utf8.RuneCountInString(text) > lay.lineRunes(), m.Title[i], func() {
				c.LabelAtoms(c.Atoms().BeginRichText(text).Strong().End().Keep()).
					Wrap().Selectable(false).Send()
			})
		}
		if m.Slots.Has(SlotsSubtitle) && m.Subtitle[i] != "" {
			text, cut := displayCut(m.Subtitle[i], lay.lineRunes())
			hover(cut, m.Subtitle[i], func() {
				c.LabelAtoms(c.Atoms().BeginRichTextColored(tok(styletokens.NeutralTextSecondary), color.Transparent, text).
					End().Keep()).Truncate().Selectable(false).Send()
			})
		}
	}
}

// renderHero fills the hero box and draws the host's block centred in it,
// or what stands in for one: a skeleton while the host builds it, the
// reason when it cannot be shown, a muted glyph when the card has none —
// the box stays either way, so the row keeps its line.
func renderHero(in Input, lay Layout, i int, x, y float32, res *Result) {
	ids := in.Ids
	w, h := lay.CardW-2, lay.Hero.H-1
	x0, y0 := x+1, y+1
	for range c.AllocateUiAtRect(x0, y0, x0+w, y0+h).KeepIter() {
		c.UiClipToMaxRect()
		r := uint8(styletokens.RoundingLg)
		sw, se := r, r
		if lay.CardH > lay.Hero.H {
			sw, se = 0, 0 // a text column follows: only the top is rounded
		}
		for range c.Frame(ids.PrepareStr("hero-bg")).
			Fill(tok(styletokens.NeutralBgFaint)).
			CornerRadiusSides(r, r, sw, se).
			InnerMargin(0).
			KeepIter() {
			c.UiSetMinWidth(w)
			c.UiSetMinHeight(h)
		}
	}
	var b Block
	ok := false
	if in.Hero != nil {
		b, ok = in.Hero(i, Box{W: w, H: h})
	}
	switch {
	case !ok:
		centred(x0, y0, w, h, glyphPx, glyphPx, func() {
			c.LabelAtoms(c.Atoms().BeginRichTextColored(tok(styletokens.NeutralTextDisabled), color.Transparent, icons.PhImage).
				Size(glyphPx).End().Keep()).Selectable(false).Send()
		})
	case b.Pending:
		centred(x0, y0, w, h, glyphPx, glyphPx, func() { c.Spinner().Size(glyphPx).Send() })
	case b.Reason != "" || b.Render == nil:
		renderReason(b.Reason, x0+cardPad, y0+cardPad, x0+w-cardPad, y0+h-cardPad)
	default:
		bw, bh := b.W, b.H
		if bw <= 0 || bw > w {
			bw = w
		}
		if bh <= 0 || bh > h {
			bh = h
		}
		centred(x0, y0, w, h, bw, bh, func() {
			for range c.PushId(ids.PrepareStr("hero")).KeepIter() {
				if b.Render() {
					in.State.SetSelected(int32(i))
					res.Clicked = int32(i)
				}
			}
		})
	}
}

// renderBody draws the host's body block, or Model.Body wrapped and clipped
// at the slot's line budget.
func renderBody(in Input, lay Layout, i int, x0, y0, x1, y1 float32, res *Result) {
	m, ids := in.Model, in.Ids
	if in.Body != nil {
		if b, ok := in.Body(i, Box{W: x1 - x0, H: y1 - y0}); ok {
			switch {
			case b.Pending:
				centred(x0, y0, x1-x0, y1-y0, lineBody, lineBody, func() { c.Spinner().Size(lineBody).Send() })
			case b.Reason != "" || b.Render == nil:
				renderReason(b.Reason, x0, y0, x1, y1)
			default:
				for range slot(x0, y0, x1, y1) {
					for range c.PushId(ids.PrepareStr("body")).KeepIter() {
						if b.Render() {
							in.State.SetSelected(int32(i))
							res.Clicked = int32(i)
						}
					}
				}
			}
			return
		}
	}
	if m.Body[i] == "" {
		return
	}
	// Cut by lines as well as by runes: a body of short lines runs out of
	// rows long before it runs out of runes.
	lines := int((y1 - y0) / lineBody)
	text, cut := clampCut(m.Body[i], lines*lay.lineRunes(), max(1, lines))
	for range slot(x0, y0, x1, y1) {
		hover(cut, m.Body[i], func() {
			c.LabelAtoms(c.Atoms().BeginRichTextColored(tok(styletokens.NeutralTextSecondary), color.Transparent, text).
				End().Keep()).Wrap().Selectable(false).Send()
		})
	}
}

// renderFacts draws card i's facts, one row each, label left and value
// right, and spends the last row on "+k more" when they do not all fit.
func renderFacts(in Input, lay Layout, i int, x0, y0, x1 float32) {
	m := in.Model
	lo, hi, more := m.facts(i)
	n := int(hi - lo)
	rows := lay.FactRows
	shown := n
	if n > rows || (more > 0 && n == rows) {
		shown = rows - 1
	}
	more += int32(n - shown)
	split := x0 + (x1-x0)*factLabelShare
	labelRunes := max(4, int((split-x0)/runeSmallPx))
	valueRunes := max(4, int((x1-split)/runeSmallPx))
	for j := range shown {
		k := int(lo) + j
		ry := y0 + float32(j)*lineFact
		label, _ := displayCut(m.FactLabel[k], labelRunes)
		value, cut := displayCut(m.FactValue[k], valueRunes)
		for range slot(x0, ry, split-slotGap, ry+lineFact) {
			c.LabelAtoms(c.Atoms().BeginRichText(label).Small().Weak().End().Keep()).
				Truncate().Selectable(false).Send()
		}
		for range slot(split, ry, x1, ry+lineFact) {
			hover(cut, m.FactLabel[k]+": "+m.FactValue[k], func() {
				if m.FactColor != nil && m.FactColor[k].Kind() != color.ColorKindNone {
					c.LabelAtoms(c.Atoms().BeginRichTextColored(m.FactColor[k], color.Transparent, value).
						Small().End().Keep()).Truncate().Selectable(false).Send()
					return
				}
				c.LabelAtoms(c.Atoms().BeginRichText(value).Small().End().Keep()).
					Truncate().Selectable(false).Send()
			})
		}
	}
	if more > 0 {
		ry := y0 + float32(shown)*lineFact
		for range slot(x0, ry, x1, ry+lineFact) {
			c.LabelAtoms(c.Atoms().BeginRichText("+" + strconv.Itoa(int(more)) + " more").Small().Weak().Italics().End().Keep()).
				Truncate().Selectable(false).Send()
		}
	}
}

// renderTags draws as many of card i's tags as the row is estimated to hold
// and counts the rest. A badge's width is its label's plus its padding; the
// estimate errs wide, and the slot's clip is the backstop.
func renderTags(in Input, lay Layout, i int, x0, y0, x1, y1 float32) {
	tags := in.Model.tags(i)
	if len(tags) == 0 {
		return
	}
	const badgePad, moreW float32 = 16, 30
	room := x1 - x0
	shown := 0
	for shown < len(tags) {
		w := float32(utf8.RuneCountInString(tags[shown]))*runeSmallPx + badgePad
		reserve := float32(0)
		if shown+1 < len(tags) {
			reserve = moreW
		}
		if w+reserve > room && shown > 0 {
			break
		}
		room -= w
		shown++
	}
	for range slot(x0, y0, x1, y1) {
		for range c.HorizontalTop().KeepIter() {
			for j := range shown {
				badge.New(in.Ids.PrepareSeq(tagIdBase+uint64(j)), tags[j]).
					Tone(badge.ToneNeutral).Variant(badge.VariantSoft).Size(badge.SizeSm).Send()
			}
			if rest := len(tags) - shown; rest > 0 {
				c.LabelAtoms(c.Atoms().BeginRichText("+" + strconv.Itoa(rest)).Small().Weak().End().Keep()).
					Selectable(false).Send()
			}
		}
	}
}

// renderReason says why a block cannot be shown, in the block's own room.
func renderReason(reason string, x0, y0, x1, y1 float32) {
	if reason == "" {
		reason = "nothing to show"
	}
	for range slot(x0, y0, x1, y1) {
		c.LabelAtoms(c.Atoms().BeginRichTextColored(tok(styletokens.WarningDefault), color.Transparent, icons.PhWarning).
			Small().End().Keep()).Selectable(false).Send()
		c.LabelAtoms(c.Atoms().BeginRichText(reason).Small().Weak().End().Keep()).
			Wrap().Selectable(false).Send()
	}
}

// slot is one clipped rect of the card: whatever is drawn inside cannot
// paint past it.
func slot(x0, y0, x1, y1 float32) func(func(struct{}) bool) {
	return func(yield func(struct{}) bool) {
		for range c.AllocateUiAtRect(x0, y0, x1, y1).KeepIter() {
			c.UiClipToMaxRect()
			yield(struct{}{})
		}
	}
}

// centred runs body in a clipped rect of (cw, ch) centred in the box at
// (x, y) of (w, h).
func centred(x, y, w, h, cw, ch float32, body func()) {
	cx, cy := x+(w-cw)/2, y+(h-ch)/2
	for range slot(cx, cy, cx+cw, cy+ch) {
		body()
	}
}

// hover wraps body in a tooltip carrying the whole text when the drawn text
// was cut; otherwise it just runs body.
func hover(on bool, full string, body func()) {
	if !on {
		body()
		return
	}
	for range c.HoverText(full).KeepIter() {
		body()
	}
}

// fillRect draws a filled, rounded rect at a computed position.
func fillRect(id c.WidgetIdCreatorI, x0, y0, x1, y1 float32, fill color.Color, rounding float32) {
	if x1 <= x0 || y1 <= y0 {
		return
	}
	for range c.AllocateUiAtRect(x0, y0, x1, y1).KeepIter() {
		c.UiClipToMaxRect()
		for range c.Frame(id).Fill(fill).CornerRadius(rounding).InnerMargin(0).KeepIter() {
			c.UiSetMinWidth(x1 - x0)
			c.UiSetMinHeight(y1 - y0)
		}
	}
}

func tok(t styletokens.RGBA8) color.Color { return color.Hex(t.AsHex()) }

// Toolbar draws the density and hero-aspect controls inline — the host
// places it in its own row, beside a pager. The aspect control is drawn only
// for a page that has heroes.
func Toolbar(ids *c.WidgetIdStack, scopeKey string, st *State, slots SlotsE) {
	if st == nil || ids == nil {
		return
	}
	for range c.IdScope(ids.PrepareStr(scopeKey + "-toolbar")) {
		pick := func(key, label, tip string, on bool) (clicked bool) {
			for range c.HoverText(tip).KeepIter() {
				clicked = c.Button(ids.PrepareStr(key), c.Atoms().Text(label).Keep()).Small().
					Selected(on).SendResp().HasPrimaryClicked()
			}
			return
		}
		if pick("d-s", "S", "small cards", st.density == DensitySmall) {
			st.density = DensitySmall
		}
		if pick("d-m", "M", "medium cards", st.density == DensityMedium) {
			st.density = DensityMedium
		}
		if pick("d-l", "L", "large cards", st.density == DensityLarge) {
			st.density = DensityLarge
		}
		if !slots.Has(SlotsHero) {
			return
		}
		// A gap, not a vertical Separator: that one grows to the height the
		// row is offered, and a toolbar row is offered the whole pane.
		c.AddSpace(toolbarGap)
		if pick("a-169", "16:9", "hero aspect 16:9", st.aspect == Aspect16x9) {
			st.aspect = Aspect16x9
		}
		if pick("a-43", "4:3", "hero aspect 4:3", st.aspect == Aspect4x3) {
			st.aspect = Aspect4x3
		}
		if pick("a-11", "1:1", "hero aspect 1:1", st.aspect == Aspect1x1) {
			st.aspect = Aspect1x1
		}
	}
}
