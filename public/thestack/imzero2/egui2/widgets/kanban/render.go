package kanban

import (
	"strconv"

	"github.com/stergiotis/boxer/public/keelson/designsystem/styletokens"
	c "github.com/stergiotis/boxer/public/thestack/imzero2/egui2/bindings"
	"github.com/stergiotis/boxer/public/thestack/imzero2/egui2/widgets/badge"
	"github.com/stergiotis/boxer/public/thestack/imzero2/egui2/widgets/color"
)

// moveKind is the one card action a frame can carry (a user can click at most
// one control per frame). Collected during the render pass and applied after it,
// so Cards is never mutated mid-iteration.
type moveKind uint8

const (
	mvNone moveKind = iota
	mvLeft
	mvRight
	mvUp
	mvDown
)

const (
	defaultColumnWidth float32 = 240
	// boardMinHeight floors the board in an unbounded scroll host so its
	// ScrollArea has a bounded rect to scroll (schemaview's dockMinHeight
	// idiom). A bounded host (FillHost) lets the board fill its rect instead.
	boardMinHeight float32 = 520
	// emptyColumnHeaderClear offsets the drop insertion line below a column
	// header when the target column has no other cards, so it lands in the card
	// area rather than over the title.
	emptyColumnHeaderClear float32 = 34
)

// showMoveButtons toggles the per-card ◀▶▲▼ footer. Off by default — the buttons
// are not yet styled well enough, and drag-and-drop already moves cards; flip to
// true to bring them back (they still work).
const showMoveButtons = false

// CaptureUiRect seq bases for drag hit-testing. Distinctive high bases keep them
// clear of other widgets' seqs; the per-index offset is the card's slice index
// or the column index. A single active drag is assumed — two boards dragging at
// the same instant would share these seqs.
const (
	seqCardBase uint64 = 0xCA0B_0000_0000_0000
	seqLaneBase uint64 = 0xCA0B_1000_0000_0000
)

func cardRectSeq(sliceIdx int) uint64 { return seqCardBase + uint64(sliceIdx) }
func laneRectSeq(colIdx int) uint64   { return seqLaneBase + uint64(colIdx) }

// Input is the per-frame render request.
type Input struct {
	// Ids is the widget id stack supplied by the host (the tour / window scopes
	// each instance so two boards never collide).
	Ids *c.WidgetIdStack
	// ScopeKey disambiguates instances that share one unscoped parent; Render
	// opens IdScope(Ids.PrepareStr(ScopeKey)) around the whole board.
	ScopeKey string
	// Model is the board state, mutated in place on a move.
	Model *Model
	// FillHost tells Render its host already gives it a bounded height, so it
	// fills that rect rather than flooring to boardMinHeight. Dock-tab leaves
	// set this true; a vertically-unbounded gallery ScrollArea leaves it false.
	FillHost bool
	// ReadOnly hides the move controls and suppresses moves; selection still
	// works.
	ReadOnly bool
	// ColumnWidth overrides the per-lane width; 0 uses defaultColumnWidth.
	ColumnWidth float32
}

// Render draws the board: columns left-to-right inside one board ScrollArea,
// each a fixed-width lane of card frames with per-card move controls. A move
// clicked this frame is applied to the Model after the pass (so the card
// relocates next frame) and recorded for [Model.DrainMoves].
func Render(in Input) {
	m := in.Model
	if m == nil || len(m.Columns) == 0 {
		return
	}
	colW := in.ColumnWidth
	if colW <= 0 {
		colW = defaultColumnWidth
	}
	density := styletokens.DensityFromEnv()

	var actCard uint64
	var act moveKind

	for range c.IdScope(in.Ids.PrepareStr(in.ScopeKey)) {
		if !in.FillHost {
			c.UiSetMinHeight(boardMinHeight)
		}
		for range c.ScrollArea().Vscroll(true).Hscroll(true).AutoShrink(false, false).KeepIter() {
			for range c.Horizontal().KeepIter() {
				for i := range m.Columns {
					aCard, aKind := renderColumn(in, m, i, colW, density)
					if aKind != mvNone {
						actCard, act = aCard, aKind
					}
				}
			}
		}
	}

	if act != mvNone && !in.ReadOnly {
		idx := m.cardIndex(actCard)
		switch act {
		case mvLeft:
			m.shiftColumn(idx, -1)
		case mvRight:
			m.shiftColumn(idx, +1)
		case mvUp:
			m.reorderWithin(idx, -1)
		case mvDown:
			m.reorderWithin(idx, +1)
		}
	}

	// Drag-and-drop: while a card is held, recompute the drop target from the
	// pointer + last frame's captured rects and paint the ghost + insertion
	// line over everything; apply on release. Uses the previous frame's rects
	// (one-frame lag), which is exact here because the layout is frozen for the
	// duration of a drag — nothing moves until the drop lands.
	if m.drag != nil {
		updateAndPaintDrag(m, density)
		if m.dragStop {
			if m.drag.dropOK && !in.ReadOnly {
				m.moveTo(m.drag.cardID, m.drag.dropColumn, m.drag.dropIndex)
			}
			m.drag = nil
			m.dragStop = false
		}
	}
}

// renderColumn draws one lane: a panel Frame around a width-pinned Vertical
// carrying the header and the lane's cards.
func renderColumn(in Input, m *Model, colIdx int, colW float32, density styletokens.DensityE) (actCard uint64, act moveKind) {
	ids := in.Ids
	col := m.Columns[colIdx]
	atFirst := colIdx == 0
	atLast := colIdx == len(m.Columns)-1
	idxs := m.cardIndicesIn(col.ID)

	for range c.IdScope(ids.PrepareStr("col:" + strconv.FormatUint(col.ID, 10))) {
		for range c.Frame(ids.PrepareStr("lane")).
			Fill(color.Hex(styletokens.NeutralBgPanel.AsHex())).
			CornerRadius(styletokens.RoundingMd).
			InnerMargin(styletokens.PaddingDefault(density)).
			KeepIter() {
			for range c.Vertical().KeepIter() {
				c.UiSetMinWidth(colW)
				c.UiSetMaxWidth(colW)
				renderColumnHeader(ids, col, len(idxs), density)
				c.AddSpace(styletokens.GapInline(density))
				for _, ci := range idxs {
					aCard, aKind := renderCard(in, m, ci, colW, atFirst, atLast, density)
					if aKind != mvNone {
						actCard, act = aCard, aKind
					}
				}
				if len(idxs) == 0 {
					for rt := range c.RichTextLabel("— empty —") {
						rt.Weak().Small().Italics()
					}
				}
				// Snapshot the lane's rect (viewport-absolute, one-frame lag)
				// for drag hit-testing: which column is the pointer over.
				c.CaptureUiRect(laneRectSeq(colIdx))
			}
		}
		c.AddSpace(styletokens.GapItems(density)) // gap to the next lane
	}
	return
}

// renderColumnHeader draws the lane title and a pill count of its cards.
func renderColumnHeader(ids *c.WidgetIdStack, col Column, count int, density styletokens.DensityE) {
	for range c.Horizontal().KeepIter() {
		for rt := range c.RichTextLabel(col.Title) {
			rt.Strong().Heading()
		}
		c.AddSpace(styletokens.GapInline(density))
		badge.New(ids.PrepareStr("count"), strconv.Itoa(count)).
			Tone(badge.ToneNeutral).
			Variant(badge.VariantSoft).
			Size(badge.SizeSm).
			Pill().
			Send()
	}
}

// renderCard draws one card Frame (click-sensed for selection) and its controls.
func renderCard(in Input, m *Model, ci int, colW float32, atFirst, atLast bool, density styletokens.DensityE) (actCard uint64, act moveKind) {
	ids := in.Ids
	card := m.Cards[ci]
	selected := m.sel == card.ID
	dragging := m.drag != nil && m.drag.cardID == card.ID

	fill := color.Hex(styletokens.NeutralBgSurface.AsHex())
	stroke := color.Hex(styletokens.NeutralBorderFaint.AsHex())
	strokeW := styletokens.StrokeHair
	if selected || dragging {
		strokeW = styletokens.StrokeStrong
		if card.Accent.Kind() != color.ColorKindNone {
			stroke = card.Accent
		} else {
			stroke = color.Hex(styletokens.AccentDefault.AsHex())
		}
	}

	for range c.IdScope(ids.PrepareStr("card:" + strconv.FormatUint(card.ID, 10))) {
		// Wrap the frame + footer in one Vertical so the CaptureUiRect below
		// snapshots the whole card *unit*, not just the frame's inner content.
		// The drag insertion line is placed in the gaps between these unit
		// rects — capturing only the content rect (as an earlier cut did) put
		// the line inside the card, above its footer, instead of in the gap.
		for range c.Vertical().KeepIter() {
			frame := c.Frame(ids.PrepareStr("frame")).
				Fill(fill).
				CornerRadius(styletokens.RoundingMd).
				Stroke(strokeW, stroke).
				InnerMargin(styletokens.PaddingTight(density)).
				SenseClick()
			if !in.ReadOnly {
				frame = frame.SenseDrag()
			}
			fid := frame.Id()
			for range frame.KeepIter() {
				bodyW := colW - 2*styletokens.PaddingTight(density)
				c.UiSetMinWidth(bodyW)
				c.UiSetMaxWidth(bodyW)

				for range c.Horizontal().KeepIter() {
					switch {
					case card.ParentID != 0:
						for rt := range c.RichTextLabel("↳") {
							rt.Weak()
						}
					case card.Accent.Kind() != color.ColorKindNone:
						for rt := range c.RichTextLabelColored(card.Accent, color.Transparent, "●") {
							rt.Small()
						}
					}
					for rt := range c.RichTextLabel(card.Title) {
						rt.Strong()
					}
				}
				if card.Subtitle != "" {
					for rt := range c.RichTextLabel(card.Subtitle) {
						rt.Weak().Small()
					}
				}
				renderRelations(ids, m, card)
			}
			resp := c.CurrentApplicationState.StateManager.GetResponseByIdRaw(fid)
			if resp.HasPrimaryClicked() {
				m.sel = card.ID
			}
			if !in.ReadOnly {
				if resp.HasDragStarted() {
					beginDrag(m, card, ci)
				}
				if m.drag != nil && m.drag.cardID == card.ID && resp.HasDragStopped() {
					m.dragStop = true
				}
			}
			// The controls are a footer row OUTSIDE the card Frame (a click-
			// sensed Frame wins the pointer over buttons drawn inside it). They
			// are gated behind showMoveButtons — off until they are styled;
			// drag-and-drop is the move mechanism meanwhile.
			if showMoveButtons && !in.ReadOnly {
				aCard, aKind := renderControls(ids, card, atFirst, atLast, density)
				if aKind != mvNone {
					actCard, act = aCard, aKind
				}
			}
			// Snapshot the whole card unit (frame + footer) for drag hit-testing,
			// the insertion line, and the ghost's grab offset + size.
			c.CaptureUiRect(cardRectSeq(ci))
		}
		c.AddSpace(styletokens.GapInline(density))
	}
	return
}

// renderRelations surfaces the one-level parent link: a "sub-item of …" trailer
// on a child, a "◱ N sub" chip on a parent. Deliberately minimal — see the
// deferred sub-item presentation note in the package doc.
func renderRelations(ids *c.WidgetIdStack, m *Model, card Card) {
	if card.ParentID != 0 {
		if p := m.cardByID(card.ParentID); p != nil {
			for rt := range c.RichTextLabel("sub-item of " + p.Title) {
				rt.Weak().Small().Italics()
			}
		}
		return
	}
	if n := m.childCount(card.ID); n > 0 {
		badge.New(ids.PrepareStr("kids"), "◱ "+strconv.Itoa(n)+" sub").
			Tone(badge.ToneNeutral).
			Variant(badge.VariantSoft).
			Size(badge.SizeSm).
			Send()
	}
}

// renderControls draws the compact move row. Edge buttons are omitted (no ◀ in
// the first column, no ▶ in the last) rather than shown disabled.
func renderControls(ids *c.WidgetIdStack, card Card, atFirst, atLast bool, density styletokens.DensityE) (actCard uint64, act moveKind) {
	c.AddSpace(styletokens.PaddingHair(density))
	for range c.Horizontal().KeepIter() {
		if !atFirst {
			if c.Button(ids.PrepareStr("mL"), c.Atoms().Text("◀").Keep()).Small().SendResp().HasPrimaryClicked() {
				actCard, act = card.ID, mvLeft
			}
		}
		if !atLast {
			if c.Button(ids.PrepareStr("mR"), c.Atoms().Text("▶").Keep()).Small().SendResp().HasPrimaryClicked() {
				actCard, act = card.ID, mvRight
			}
		}
		c.AddSpace(styletokens.GapInline(density))
		if c.Button(ids.PrepareStr("mU"), c.Atoms().Text("▲").Keep()).Small().SendResp().HasPrimaryClicked() {
			actCard, act = card.ID, mvUp
		}
		if c.Button(ids.PrepareStr("mD"), c.Atoms().Text("▼").Keep()).Small().SendResp().HasPrimaryClicked() {
			actCard, act = card.ID, mvDown
		}
	}
	return
}

// beginDrag starts a drag for the card at slice index ci, seeding the ghost's
// grab offset and size from the previous frame's captured rect so it tracks the
// pointer without jumping to a corner.
func beginDrag(m *Model, card Card, ci int) {
	sm := c.CurrentApplicationState.StateManager
	d := &dragState{cardID: card.ID, title: card.Title, accent: card.Accent}
	p := sm.GetPointer()
	if r, ok := sm.GetUiRect(cardRectSeq(ci)); ok && p.Valid {
		d.grabDX = p.X - r.MinX
		d.grabDY = p.Y - r.MinY
		d.w = r.MaxX - r.MinX
		d.h = r.MaxY - r.MinY
	}
	m.drag = d
}

// updateAndPaintDrag recomputes the drop target for the held card and paints the
// insertion line + floating ghost into the foreground overlay. Called once per
// frame while a drag is active, after the board rendered (so it reads the rects
// captured for the previous frame). The drop column is the lane whose x-range
// contains the pointer; the drop index counts that column's other cards whose
// vertical midpoint sits above the pointer.
func updateAndPaintDrag(m *Model, density styletokens.DensityE) {
	sm := c.CurrentApplicationState.StateManager
	d := m.drag
	p := sm.GetPointer()
	d.dropOK = false

	accent := d.accent
	if accent.Kind() == color.ColorKindNone {
		accent = color.Hex(styletokens.AccentDefault.AsHex())
	}

	if p.Valid {
		for i := range m.Columns {
			lr, ok := sm.GetUiRect(laneRectSeq(i))
			if !ok || p.X < lr.MinX || p.X > lr.MaxX {
				continue
			}
			col := m.Columns[i].ID
			// The target column's other cards' unit rects, in render order.
			var rects []c.UiRectValue
			for _, ci := range m.cardIndicesIn(col) {
				if m.Cards[ci].ID == d.cardID {
					continue // a card never counts toward its own drop
				}
				if cr, ok := sm.GetUiRect(cardRectSeq(ci)); ok {
					rects = append(rects, cr)
				}
			}
			// Drop index = cards whose vertical midpoint sits above the pointer.
			idx := 0
			for _, cr := range rects {
				if p.Y > (cr.MinY+cr.MaxY)*0.5 {
					idx++
				}
			}
			// Place the insertion line in the gap for that index: above the
			// first card, below the last, or midway between the two it splits.
			var lineY float32
			switch {
			case len(rects) == 0:
				lineY = lr.MinY + emptyColumnHeaderClear
			case idx == 0:
				lineY = rects[0].MinY - 3
			case idx >= len(rects):
				lineY = rects[len(rects)-1].MaxY + 3
			default:
				lineY = (rects[idx-1].MaxY + rects[idx].MinY) * 0.5
			}
			d.dropColumn, d.dropIndex, d.dropOK = col, idx, true
			c.PaintLine(lr.MinX, lineY, lr.MaxX, lineY, color.Hex(styletokens.AccentStrong.AsHex()), styletokens.StrokeStrong).Send()
			break
		}
	}

	if p.Valid {
		w, h := d.w, d.h
		if w < 60 {
			w = 180
		}
		if h < 24 {
			h = 44
		}
		gx, gy := p.X-d.grabDX, p.Y-d.grabDY
		pad := styletokens.PaddingTight(density)
		c.PaintRectFilled(gx, gy, gx+w, gy+h, styletokens.RoundingMd, color.Hex(styletokens.NeutralBgSurface.AsHex())).Send()
		c.PaintRectStroke(gx, gy, gx+w, gy+h, styletokens.RoundingMd, accent, styletokens.StrokeStrong).Send()
		c.PaintText(gx+pad, gy+pad, 0, 0, d.title, styletokens.ScaledPt(styletokens.BodyPt, density), color.Hex(styletokens.NeutralTextPrimary.AsHex())).Send()
	}
	c.PaintAbsoluteOverlay()
}
