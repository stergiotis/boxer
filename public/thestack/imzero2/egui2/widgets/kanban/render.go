package kanban

import (
	"strconv"
	"strings"

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
	// Group selects the arrangement: GroupNone (flat, the default),
	// GroupByParent (a swimlane per parent), or GroupByField (a swimlane per
	// GroupField value). Pointer-drag is available in flat mode; grouped modes
	// are a read/select view for now.
	Group GroupModeE
	// GroupField maps a card to its swimlane key + display label, used when
	// Group == GroupByField (e.g. `return ownerByID[c.ID], ownerByID[c.ID]` to
	// group by owner). The caller keeps the attribute; Card stays lean. Ignored
	// in other modes; a nil func with GroupByField falls back to flat.
	GroupField func(c *Card) (key, label string)
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

	if in.Group == GroupByParent || (in.Group == GroupByField && in.GroupField != nil) {
		renderGrouped(in, m, colW, density)
		return
	}

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

// RenderLegend draws the board's dot legend: an always-visible row of one
// coloured "●" + Label pair per entry, each carrying dk.Tooltip as a hover
// tooltip when set. It is a separate call from Render — not drawn
// automatically — so the host places it wherever it fits (above the board, a
// toolbar, a footer); a no-op when legend is empty, so it is safe to call
// unconditionally every frame.
func RenderLegend(legend []DotKind) {
	if len(legend) == 0 {
		return
	}
	density := styletokens.DensityFromEnv()
	for range c.Horizontal().KeepIter() {
		for i, dk := range legend {
			renderLegendEntry(dk)
			if i < len(legend)-1 {
				c.AddSpace(styletokens.GapItems(density))
			}
		}
	}
}

// renderLegendEntry draws one legend swatch + label. The tooltip (when set)
// wraps only the label RichTextLabel call — a single widget, matching every
// other HoverText call site in this codebase (badge.Tooltip, the schemaview
// legend toggle, labeledField's HoverText-wrapped text field): wrapping a
// whole multi-widget Horizontal in HoverText was tried first and silently
// dropped its content, so the swatch dot stays outside the wrap.
func renderLegendEntry(dk DotKind) {
	for rt := range c.RichTextLabelColored(dk.Color, color.Transparent, "●") {
		rt.Small()
	}
	if dk.Tooltip == "" {
		for rt := range c.RichTextLabel(dk.Label) {
			rt.Weak().Small()
		}
		return
	}
	for range c.HoverText(dk.Tooltip).KeepIter() {
		for rt := range c.RichTextLabel(dk.Label) {
			rt.Weak().Small()
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
					aCard, aKind := renderCard(in, m, ci, colW, atFirst, atLast, density, !in.ReadOnly)
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
func renderCard(in Input, m *Model, ci int, colW float32, atFirst, atLast bool, density styletokens.DensityE, dragEnabled bool) (actCard uint64, act moveKind) {
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
			if dragEnabled {
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
					// Wrapped, not plain: an unwrapped label is sized by its
					// text, and a title longer than the lane pushes straight
					// through the UiSetMaxWidth pin above — widening the card,
					// the lane, and every lane after it. Wrapping keeps the
					// card at colW and lets it grow downwards instead.
					titleAtoms := c.Atoms()
					titleAtoms = titleAtoms.BeginRichText(card.Title).Strong().End()
					c.LabelAtoms(titleAtoms.Keep()).Wrap().Send()
				}
				if card.Subtitle != "" {
					for rt := range c.RichTextLabel(card.Subtitle) {
						rt.Weak().Small()
					}
				}
				renderRelations(ids, m, card)
				renderDots(m, card.Dots, density)
			}
			resp := c.CurrentApplicationState.StateManager.GetResponseByIdRaw(fid)
			if resp.HasPrimaryClicked() {
				m.sel = card.ID
			}
			if dragEnabled {
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

// renderDots paints a packed tally of small "•" dots along the card's bottom
// edge: up to 3 [DotTally] entries, each Count copies of "•" in its
// [DotKind]'s colour, explained board-wide by [RenderLegend] rather than a
// per-dot tooltip. Composed as a single multi-run LabelAtoms call rather than
// one widget per dot, so runs sit with zero gap — same colour or different —
// reading as one continuous tally rather than spaced badges. Entries past the
// third, non-positive counts, and ids absent from the board's DotLegend are
// silently skipped; a no-op when nothing resolves.
func renderDots(m *Model, dots []DotTally, density styletokens.DensityE) {
	if len(dots) > 3 {
		dots = dots[:3]
	}
	pt := styletokens.ScaledPt(styletokens.MicroPt, density)
	a := c.Atoms()
	any := false
	for _, dt := range dots {
		if dt.Count <= 0 {
			continue
		}
		dk, ok := m.dotKind(dt.ID)
		if !ok {
			continue
		}
		a = a.BeginRichTextColored(dk.Color, color.Transparent, strings.Repeat("•", dt.Count)).Size(pt).End()
		any = true
	}
	if !any {
		return
	}
	c.LabelAtoms(a.Keep()).Send()
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
	if done, total := m.rollup(card.ID); total > 0 {
		badge.New(ids.PrepareStr("kids"), "◱ "+strconv.Itoa(done)+"/"+strconv.Itoa(total)).
			Tone(rollupTone(done, total)).
			Variant(badge.VariantSoft).
			Size(badge.SizeSm).
			Send()
	}
}

// rollupTone greens the pill once every child is done, else stays neutral.
func rollupTone(done, total int) badge.ToneE {
	if total > 0 && done == total {
		return badge.ToneSuccess
	}
	return badge.ToneNeutral
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

// --- GroupByParent (swimlanes) ---

// swimlaneSpec describes one swimlane. parent is set for a GroupByParent lane
// (the lane represents that card, drives the status chip); label is the header
// text for a non-parent lane (a field value, or "Standalone"). cardIdxs are the
// slice indices of the cards that fill the lane's columns.
type swimlaneSpec struct {
	key      string
	label    string
	parent   *Card
	cardIdxs []int
}

// groupedSwimlanes builds the ordered swimlane list for the active grouped mode:
// one lane per field value (GroupByField), or one lane per parent plus a
// Standalone lane (GroupByParent).
func groupedSwimlanes(in Input, m *Model) (lanes []swimlaneSpec) {
	if in.Group == GroupByField {
		for _, fl := range m.fieldLanes(in.GroupField) {
			label := fl.label
			if label == "" {
				label = "Unassigned"
			}
			lanes = append(lanes, swimlaneSpec{key: "f:" + fl.key, label: label, cardIdxs: fl.idxs})
		}
		return
	}
	for _, pi := range m.topLevelParentIndices() {
		parent := m.Cards[pi]
		lanes = append(lanes, swimlaneSpec{
			key:      "p" + strconv.FormatUint(parent.ID, 10),
			parent:   &parent,
			cardIdxs: m.childIndicesOf(parent.ID),
		})
	}
	if sa := m.standaloneIndices(); len(sa) > 0 {
		lanes = append(lanes, swimlaneSpec{key: "standalone", label: "Standalone", cardIdxs: sa})
	}
	return
}

// renderGrouped lays the board out as a vertical stack of swimlanes: the shared
// column titles on top, then one band per lane. Grouped modes are a read/select
// view — pointer-drag is disabled (flat mode owns moves), so any in-flight drag
// is cancelled here.
func renderGrouped(in Input, m *Model, colW float32, density styletokens.DensityE) {
	m.drag = nil
	m.dragStop = false
	ids := in.Ids
	lanes := groupedSwimlanes(in, m)
	for range c.IdScope(ids.PrepareStr(in.ScopeKey)) {
		if !in.FillHost {
			c.UiSetMinHeight(boardMinHeight)
		}
		for range c.ScrollArea().Vscroll(true).Hscroll(true).AutoShrink(false, false).KeepIter() {
			for range c.Vertical().KeepIter() {
				renderColumnTitleRow(ids, m, colW, density)
				for i := range lanes {
					renderSwimlane(in, m, colW, density, lanes[i])
				}
			}
		}
	}
}

// renderColumnTitleRow draws the shared column headers above the swimlanes — one
// colW-wide cell per column so they line up with every lane's columns below.
func renderColumnTitleRow(ids *c.WidgetIdStack, m *Model, colW float32, density styletokens.DensityE) {
	for range c.Horizontal().KeepIter() {
		for i := range m.Columns {
			for range c.IdScope(ids.PrepareStr("cth:" + strconv.FormatUint(m.Columns[i].ID, 10))) {
				for range c.Vertical().KeepIter() {
					c.UiSetMinWidth(colW)
					c.UiSetMaxWidth(colW)
					for rt := range c.RichTextLabel(m.Columns[i].Title) {
						rt.Strong().Heading()
					}
				}
			}
			c.AddSpace(styletokens.GapItems(density))
		}
	}
	c.AddSpace(styletokens.GapInline(density))
}

// renderSwimlane draws one lane: a faint full-width band with a header row
// (parent + rollup + own-status chip, or a "Standalone" label) over a row of the
// same columns, each showing this lane's cards.
func renderSwimlane(in Input, m *Model, colW float32, density styletokens.DensityE, s swimlaneSpec) {
	ids := in.Ids
	pad := styletokens.PaddingTight(density)
	for range c.IdScope(ids.PrepareStr("swim:" + s.key)) {
		for range c.Frame(ids.PrepareStr("band")).
			Fill(color.Hex(styletokens.NeutralBgFaint.AsHex())).
			CornerRadius(styletokens.RoundingMd).
			InnerMarginSides(0, 0, pad, pad).
			KeepIter() {
			for range c.Vertical().KeepIter() {
				renderSwimlaneHeader(ids, m, density, s)
				c.AddSpace(styletokens.GapInline(density))
				for range c.Horizontal().KeepIter() {
					for i := range m.Columns {
						col := m.Columns[i]
						for range c.IdScope(ids.PrepareStr("sc:" + strconv.FormatUint(col.ID, 10))) {
							for range c.Vertical().KeepIter() {
								c.UiSetMinWidth(colW)
								c.UiSetMaxWidth(colW)
								any := false
								for _, ci := range s.cardIdxs {
									if m.Cards[ci].ColumnID == col.ID {
										renderCard(in, m, ci, colW, false, false, density, false)
										any = true
									}
								}
								if !any {
									c.AddSpace(pad) // keep the empty cell colW wide
								}
							}
						}
						c.AddSpace(styletokens.GapItems(density))
					}
				}
			}
		}
		c.AddSpace(styletokens.GapInline(density))
	}
}

// renderSwimlaneHeader draws the lane label: for a parent lane, its accent +
// title + rollup pill + its own column chip; for the Standalone lane, a plain
// label.
func renderSwimlaneHeader(ids *c.WidgetIdStack, m *Model, density styletokens.DensityE, s swimlaneSpec) {
	for range c.Horizontal().KeepIter() {
		c.AddSpace(styletokens.PaddingTight(density))
		// Title: a parent lane shows the parent's accent bullet + title; a field
		// or Standalone lane shows its label.
		title := s.label
		if s.parent != nil {
			if s.parent.Accent.Kind() != color.ColorKindNone {
				for rt := range c.RichTextLabelColored(s.parent.Accent, color.Transparent, "●") {
					rt.Small()
				}
			}
			title = s.parent.Title
		}
		for rt := range c.RichTextLabel(title) {
			rt.Strong()
		}
		// Rollup over the lane's cards (children for a parent lane, the lane's
		// own cards for a field/Standalone lane).
		c.AddSpace(styletokens.GapInline(density))
		done, total := m.rollupOfIdxs(s.cardIdxs)
		badge.New(ids.PrepareStr("roll"), "◱ "+strconv.Itoa(done)+"/"+strconv.Itoa(total)).
			Tone(rollupTone(done, total)).
			Variant(badge.VariantSoft).
			Size(badge.SizeSm).
			Send()
		// A parent lane also shows the parent's own column, since here it is a
		// lane header rather than a card in a column.
		if s.parent != nil {
			c.AddSpace(styletokens.GapInline(density))
			badge.New(ids.PrepareStr("st"), "in "+m.columnTitle(s.parent.ColumnID)).
				Tone(badge.ToneNeutral).
				Variant(badge.VariantOutline).
				Size(badge.SizeSm).
				Send()
		}
	}
}
