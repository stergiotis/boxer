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
)

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

	var actCard uint64
	var act moveKind

	for range c.IdScope(in.Ids.PrepareStr(in.ScopeKey)) {
		if !in.FillHost {
			c.UiSetMinHeight(boardMinHeight)
		}
		for range c.ScrollArea().Vscroll(true).Hscroll(true).AutoShrink(false, false).KeepIter() {
			for range c.Horizontal().KeepIter() {
				for i := range m.Columns {
					aCard, aKind := renderColumn(in, m, i, colW)
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
}

// renderColumn draws one lane: a panel Frame around a width-pinned Vertical
// carrying the header and the lane's cards.
func renderColumn(in Input, m *Model, colIdx int, colW float32) (actCard uint64, act moveKind) {
	ids := in.Ids
	col := m.Columns[colIdx]
	atFirst := colIdx == 0
	atLast := colIdx == len(m.Columns)-1
	idxs := m.cardIndicesIn(col.ID)

	for range c.IdScope(ids.PrepareStr("col:" + strconv.FormatUint(col.ID, 10))) {
		for range c.Frame(ids.PrepareStr("lane")).
			Fill(color.Hex(styletokens.NeutralBgPanel.AsHex())).
			CornerRadius(8).
			InnerMarginSides(8, 8, 8, 8).
			KeepIter() {
			for range c.Vertical().KeepIter() {
				c.UiSetMinWidth(colW)
				c.UiSetMaxWidth(colW)
				renderColumnHeader(ids, col, len(idxs))
				c.AddSpace(6)
				for _, ci := range idxs {
					aCard, aKind := renderCard(in, m, ci, colW, atFirst, atLast)
					if aKind != mvNone {
						actCard, act = aCard, aKind
					}
				}
				if len(idxs) == 0 {
					for rt := range c.RichTextLabel("— empty —") {
						rt.Weak().Small().Italics()
					}
				}
			}
		}
		c.AddSpace(6) // gap to the next lane (horizontal flow)
	}
	return
}

// renderColumnHeader draws the lane title and a pill count of its cards.
func renderColumnHeader(ids *c.WidgetIdStack, col Column, count int) {
	for range c.Horizontal().KeepIter() {
		for rt := range c.RichTextLabel(col.Title) {
			rt.Strong().Size(15)
		}
		c.AddSpace(6)
		badge.New(ids.PrepareStr("count"), strconv.Itoa(count)).
			Tone(badge.ToneNeutral).
			Variant(badge.VariantSoft).
			Size(badge.SizeSm).
			Pill().
			Send()
	}
}

// renderCard draws one card Frame (click-sensed for selection) and its controls.
func renderCard(in Input, m *Model, ci int, colW float32, atFirst, atLast bool) (actCard uint64, act moveKind) {
	ids := in.Ids
	card := m.Cards[ci]
	selected := m.sel == card.ID

	fill := color.Hex(styletokens.NeutralBgSurface.AsHex())
	stroke := color.Hex(styletokens.NeutralBorderFaint.AsHex())
	strokeW := float32(1)
	if selected {
		strokeW = 2
		if card.Accent.Kind() != color.ColorKindNone {
			stroke = card.Accent
		} else {
			stroke = color.Hex(styletokens.AccentDefault.AsHex())
		}
	}

	for range c.IdScope(ids.PrepareStr("card:" + strconv.FormatUint(card.ID, 10))) {
		frame := c.Frame(ids.PrepareStr("frame")).
			Fill(fill).
			CornerRadius(6).
			Stroke(strokeW, stroke).
			InnerMarginSides(8, 8, 6, 6).
			SenseClick()
		fid := frame.Id()
		for range frame.KeepIter() {
			bodyW := colW - 20
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
		if c.CurrentApplicationState.StateManager.GetResponseByIdRaw(fid).HasPrimaryClicked() {
			m.sel = card.ID
		}
		// The controls live OUTSIDE the card Frame, as a footer row. An imzero2
		// Frame with SenseClick interacts its whole rect and wins the pointer
		// over buttons drawn inside it — the card would select but the ◀▶▲▼
		// would never fire. Rendered after the frame, they receive their own
		// clicks; the frame still senses body clicks for selection.
		if !in.ReadOnly {
			aCard, aKind := renderControls(ids, card, atFirst, atLast)
			if aKind != mvNone {
				actCard, act = aCard, aKind
			}
		}
		c.AddSpace(6)
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
func renderControls(ids *c.WidgetIdStack, card Card, atFirst, atLast bool) (actCard uint64, act moveKind) {
	c.AddSpace(2)
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
		c.AddSpace(8)
		if c.Button(ids.PrepareStr("mU"), c.Atoms().Text("▲").Keep()).Small().SendResp().HasPrimaryClicked() {
			actCard, act = card.ID, mvUp
		}
		if c.Button(ids.PrepareStr("mD"), c.Atoms().Text("▼").Keep()).Small().SendResp().HasPrimaryClicked() {
			actCard, act = card.ID, mvDown
		}
	}
	return
}
