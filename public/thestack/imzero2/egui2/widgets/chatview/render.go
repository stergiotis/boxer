package chatview

import (
	"iter"
	"strconv"
	"time"

	"github.com/stergiotis/boxer/public/functional"
	"github.com/stergiotis/boxer/public/keelson/designsystem/styletokens"
	"github.com/stergiotis/boxer/public/keelson/runtime/icons"
	c "github.com/stergiotis/boxer/public/thestack/imzero2/egui2/bindings"
	"github.com/stergiotis/boxer/public/thestack/imzero2/egui2/widgets/badge"
	"github.com/stergiotis/boxer/public/thestack/imzero2/egui2/widgets/color"
)

const (
	// defaultBubbleFraction is the bubble's maximum width as a fraction of
	// the pane's — the common messaging-client value.
	defaultBubbleFraction float32 = 0.72
	// minHeight floors the transcript in an unbounded scroll host so its
	// ScrollArea has a bounded rect to scroll (the kanban board's idiom). A
	// bounded host (FillHost) lets it fill its rect instead.
	minHeight float32 = 360
	// fallbackPaneW stands in for the pane probe on the frame before it
	// answers.
	fallbackPaneW float32 = 640
	// avatarPx is the initials disc's side; avatarRounding makes it a disc —
	// egui clamps a corner radius to half the side, the badge's Pill idiom.
	avatarPx       float32 = 28
	avatarRounding float32 = 100
	// tailRadius is the sender-side corner on a cluster's last bubble; the
	// other three corners take RoundingLg.
	tailRadius uint8 = 1
	// dayIdBase keeps day rows' ids clear of message ordinals under one
	// scope.
	dayIdBase uint64 = 1 << 40
	// reactionIdBase likewise keeps a bubble's reaction pills clear of its
	// other widgets.
	reactionIdBase uint64 = 0x100
	// scrollAlignCenter / scrollAlignBottom are ScrollToCursor's alignment
	// codes (0 top, 1 centre, 2 bottom).
	scrollAlignCenter uint8 = 1
	scrollAlignBottom uint8 = 2
)

// Render draws the transcript where it is called and reports the frame's
// clicks. See the package doc for the layout.
func Render(in Input) (res Result) {
	res = Result{Clicked: -1, JumpTo: -1}
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
	loc := in.Location
	if loc == nil {
		loc = time.UTC
	}
	dens := styletokens.ActiveDensity()

	for range c.IdScope(ids.PrepareStr(in.ScopeKey)) {
		// The pane probe goes first: the rect is the room left for the next
		// widget, and it answers one frame late — hold the last good width
		// so the bubbles do not flash to the fallback on a hidden→shown edge.
		if w, _, ok := c.CapturePaneSize(c.ProbeSeq(in.ScopeKey, "chatview")); ok {
			st.paneW = w
			st.shown = true
		} else {
			st.shown = false
		}
		paneW := st.paneW
		if paneW <= 0 {
			paneW = fallbackPaneW
		}
		frac := in.BubbleFraction
		if frac <= 0 {
			frac = defaultBubbleFraction
		}
		bubbleW := paneW * frac
		layout := chooseLayout(m, in.Viewer, in.Layout)

		// A jump is consumed at the start of the frame: a quote strip
		// requests one mid-loop, after the quoted (earlier) row has been
		// drawn, so it must survive to the next frame's loop.
		jump := st.jump
		st.jump = 0
		n := m.Len()
		first := max(0, n-st.Window())
		if jump > 0 {
			if j := int(jump - 1); j < first {
				st.window = n - j
				first = j
			}
		}
		rows := plan(m, loc, first)

		renderHeader(ids, st, n, first)
		if !in.FillHost {
			c.UiSetMinHeight(minHeight)
		}
		for range c.ScrollArea().Vscroll(true).AutoShrink(false, false).KeepIter() {
			c.UiSetMinWidthAvailable()
			if first > 0 {
				for range c.VerticalCentered().KeepIter() {
					label := icons.PhArrowUp + " older (" + strconv.Itoa(first) + " more)"
					if c.Button(ids.PrepareStr("older"), c.Atoms().Text(label).Keep()).Small().
						SendResp().HasPrimaryClicked() {
						res.OlderWanted = true
						st.window = st.Window() + olderStep
						st.unfollow = true
					}
				}
			}
			for _, r := range rows {
				if jump > 0 && r.kind != rowDay && r.msg == jump-1 {
					c.ScrollToCursor(scrollAlignCenter)
				}
				renderRow(in, m, st, r, layout, bubbleW, dens, loc, &res)
			}
			if st.Follow() {
				c.ScrollToCursor(scrollAlignBottom)
			}
		}
		// A wheel movement against the flow releases the tail. r16 is the
		// frame's scroll delta anywhere in the window, so only count it
		// while this transcript reached the screen last frame.
		if st.shown && st.Follow() {
			if d := c.CurrentApplicationState.StateManager.GetScrollDelta(); d.Y > 0 {
				st.unfollow = true
			}
		}
	}
	return
}

// renderHeader is the one-line strip above the transcript: the count on the
// left, the "newest" affordance on the right — always drawn, disabled while
// following, so the scroll area below never shifts.
func renderHeader(ids *c.WidgetIdStack, st *State, n, first int) {
	for range c.HorizontalTop().KeepIter() {
		shown := n - first
		text := strconv.Itoa(shown) + " of " + strconv.Itoa(n) + " messages"
		for rt := range c.RichTextLabel(text) {
			rt.Small().Weak()
		}
		for range c.UiWithLayout().MainDirRightToLeft().KeepIter() {
			for range c.EnabledUi(!st.Follow()).KeepIter() {
				if c.Button(ids.PrepareStr("newest"), c.Atoms().Text(icons.PhArrowDown+" newest").Keep()).Small().
					SendResp().HasPrimaryClicked() {
					st.unfollow = false
				}
			}
		}
	}
}

func renderRow(in Input, m *Model, st *State, r row, layout LayoutE, bubbleW float32, dens styletokens.DensityE, loc *time.Location, res *Result) {
	ids := in.Ids
	switch r.kind {
	case rowDay:
		for range c.IdScope(ids.PrepareSeq(dayIdBase + uint64(r.msg))) {
			c.AddSpace(styletokens.GapItems(dens))
			for range c.VerticalCentered().KeepIter() {
				for rt := range c.RichTextLabel(time.UnixMilli(r.dayMS).In(loc).Format("Monday, 2 January 2006")) {
					rt.Small().Weak()
				}
			}
			c.AddSpace(styletokens.GapInline(dens))
		}
	case rowSystem:
		i := int(r.msg)
		for range c.IdScope(ids.PrepareSeq(uint64(r.msg))) {
			c.AddSpace(styletokens.GapInline(dens))
			for range c.VerticalCentered().KeepIter() {
				c.LabelAtoms(c.Atoms().BeginRichText(m.Body[i]).Small().Weak().Italics().End().Keep()).
					Wrap().Selectable(false).Send()
			}
		}
	case rowMessage:
		i := int(r.msg)
		mine := in.Viewer >= 0 && m.Sender[i] == in.Viewer
		if r.first {
			c.AddSpace(styletokens.GapItems(dens))
		} else {
			c.AddSpace(styletokens.GapInline(dens))
		}
		for range c.IdScope(ids.PrepareSeq(uint64(r.msg))) {
			switch {
			case mine:
				renderBubbleColumn(in, m, st, i, r, layout, true, bubbleW, dens, loc, res)
			case layout == LayoutGroup:
				for range c.HorizontalTop().KeepIter() {
					if r.first {
						renderAvatar(ids, m, m.Sender[i], dens)
					} else {
						c.AddSpace(avatarPx)
					}
					renderBubbleColumn(in, m, st, i, r, layout, false, bubbleW, dens, loc, res)
				}
			default:
				for range c.HorizontalTop().KeepIter() {
					renderBubbleColumn(in, m, st, i, r, layout, false, bubbleW, dens, loc, res)
				}
			}
		}
	}
}

// renderBubbleColumn stacks the quote strip, the bubble and its reactions.
//
// The viewer's column is a top-down layout aligned to the cross axis's end,
// which is how egui right-aligns content of unknown width: every child is
// placed at the right edge, a max width shrinks the column from the left,
// and a frame wraps its content's min rect. Nothing inside may reopen a
// Vertical — that resets the alignment and the content lands at the left
// while the frame stays where the column put it — so the rows inside are
// right-to-left ones, drawn in reverse (hrow).
func renderBubbleColumn(in Input, m *Model, st *State, i int, r row, layout LayoutE, mine bool, bubbleW float32, dens styletokens.DensityE, loc *time.Location, res *Result) {
	ids := in.Ids
	for range column(mine) {
		if q := m.ReplyTo[i]; q >= 0 {
			renderQuote(ids, m, st, int(q), mine, bubbleW, dens, res)
		}
		renderBubble(in, m, st, i, r, layout, mine, bubbleW, dens, loc, res)
		keys, counts, who := m.Reactions(i)
		if len(keys) > 0 {
			for range hrow(mine) {
				for j := range ordered(len(keys), mine) {
					b := badge.New(ids.PrepareSeq(reactionIdBase+uint64(j)), keys[j]+" "+strconv.Itoa(int(counts[j]))).
						Tone(badge.ToneNeutral).Variant(badge.VariantSoft).Size(badge.SizeSm).Pill()
					if who[j] != "" {
						b = b.Tooltip(who[j])
					}
					b.Send()
				}
			}
		}
	}
}

// column is the bubble column's layout: left-aligned top-down for the
// other parties, right-aligned for the viewer.
func column(mine bool) iter.Seq[functional.NilIteratorValueType] {
	if mine {
		return c.UiWithLayout().MainDirTopDown().CrossAlignMax().KeepIter()
	}
	return c.Vertical().KeepIter()
}

// hrow is a row inside the column: left-to-right for the other parties,
// right-to-left for the viewer, so the row hugs the column's aligned edge.
// In a right-to-left row the first child drawn sits rightmost; iterate
// the items with ordered so they read the same either way.
func hrow(mine bool) iter.Seq[functional.NilIteratorValueType] {
	if mine {
		return c.UiWithLayout().MainDirRightToLeft().KeepIter()
	}
	return c.HorizontalTop().KeepIter()
}

// ordered yields 0..n-1, reversed for a right-to-left row.
func ordered(n int, reverse bool) iter.Seq[int] {
	return func(yield func(int) bool) {
		if reverse {
			for j := n - 1; j >= 0; j-- {
				if !yield(j) {
					return
				}
			}
			return
		}
		for j := range n {
			if !yield(j) {
				return
			}
		}
	}
}

// renderQuote is the quoted message's excerpt above a reply — its own
// click-sensed frame, outside the bubble's, because a click-sensed Frame
// wins the pointer over anything drawn inside it (the kanban card's
// finding). A click asks for a jump to the quoted message.
func renderQuote(ids *c.WidgetIdStack, m *Model, st *State, q int, mine bool, bubbleW float32, dens styletokens.DensityE, res *Result) {
	who := "system"
	if s := m.Sender[q]; s >= 0 {
		who = m.Participants[s].Name
	}
	frame := c.Frame(ids.PrepareStr("quote")).
		Fill(color.Hex(styletokens.NeutralBgFaint.AsHex())).
		CornerRadius(styletokens.RoundingMd).
		Stroke(styletokens.StrokeHair, color.Hex(styletokens.NeutralBorderFaint.AsHex())).
		InnerMargin(styletokens.PaddingTight(dens)).
		SenseClick().HoverCursorPointer()
	fid := frame.Id()
	for range frame.KeepIter() {
		c.UiSetMaxWidth(bubbleW)
		for range hrow(mine) {
			for rt := range c.RichTextLabel(icons.PhArrowBendUpLeft + " " + who) {
				rt.Small().Weak().Strong()
			}
		}
		excerpt := firstLine(m.Body[q])
		if m.Flags[q]&FlagDeleted != 0 {
			excerpt = "message deleted"
		}
		c.LabelAtoms(c.Atoms().BeginRichText(excerpt).Small().Weak().End().Keep()).
			Wrap().Selectable(false).Send()
	}
	if c.CurrentApplicationState.StateManager.GetResponseByIdRaw(fid).HasPrimaryClicked() {
		st.JumpTo(int32(q))
		res.JumpTo = int32(q)
	}
}

func renderBubble(in Input, m *Model, st *State, i int, r row, layout LayoutE, mine bool, bubbleW float32, dens styletokens.DensityE, loc *time.Location, res *Result) {
	ids := in.Ids
	selected := st.Selected() == int32(i)
	fill := color.Hex(styletokens.NeutralBgSurface.AsHex())
	if mine {
		fill = color.Hex(styletokens.AccentSubtle.AsHex())
	}
	stroke := color.Hex(styletokens.NeutralBorderFaint.AsHex())
	strokeW := styletokens.StrokeHair
	if selected {
		stroke = color.Hex(styletokens.AccentDefault.AsHex())
		strokeW = styletokens.StrokeStrong
	}
	nw, ne, sw, se := uint8(styletokens.RoundingLg), uint8(styletokens.RoundingLg), uint8(styletokens.RoundingLg), uint8(styletokens.RoundingLg)
	if r.last {
		if mine {
			se = tailRadius
		} else {
			sw = tailRadius
		}
	}
	frame := c.Frame(ids.PrepareStr("bubble")).
		Fill(fill).
		CornerRadiusSides(nw, ne, sw, se).
		Stroke(strokeW, stroke).
		InnerMargin(styletokens.PaddingDefault(dens)).
		SenseClick()
	fid := frame.Id()
	for range frame.KeepIter() {
		// The frame's content inherits the column's layout (see
		// renderBubbleColumn); the max width shrinks it from the column's
		// far side.
		c.UiSetMaxWidth(bubbleW)
		if layout == LayoutGroup && r.first && !mine {
			p := m.Sender[i]
			for rt := range c.RichTextLabelColored(participantColor(m, p), color.Transparent, m.Participants[p].Name) {
				rt.Small().Strong()
			}
		}
		switch {
		case m.Flags[i]&FlagDeleted != 0:
			c.LabelAtoms(c.Atoms().BeginRichText("message deleted").Italics().Weak().End().Keep()).
				Wrap().Selectable(false).Send()
		default:
			drawn := false
			if in.Block != nil {
				if b, ok := in.Block(i); ok && b.Render != nil {
					for range c.PushId(ids.PrepareStr("block")).KeepIter() {
						b.Render()
					}
					drawn = true
				}
			}
			if !drawn {
				c.Label(m.Body[i]).Wrap().Selectable(false).Send()
			}
		}
		renderFooter(m, i, mine, loc)
	}
	if c.CurrentApplicationState.StateManager.GetResponseByIdRaw(fid).HasPrimaryClicked() {
		st.SetSelected(int32(i))
		res.Clicked = int32(i)
	}
}

// renderFooter is the bubble's last line: the time (the full instant on
// hover), an "edited" mark, and on the viewer's own bubbles the status.
func renderFooter(m *Model, i int, mine bool, loc *time.Location) {
	t := time.UnixMilli(m.TimeMS[i]).In(loc)
	items := make([]func(), 0, 3)
	items = append(items, func() {
		for range c.HoverText(t.Format(time.RFC3339)).KeepIter() {
			for rt := range c.RichTextLabel(t.Format("15:04")) {
				rt.Small().Weak()
			}
		}
	})
	if m.Flags[i]&FlagEdited != 0 {
		tip := "edited"
		if len(m.EditedMS) == m.Len() && m.EditedMS[i] != 0 {
			tip = "edited " + time.UnixMilli(m.EditedMS[i]).In(loc).Format(time.RFC3339)
		}
		items = append(items, func() {
			for range c.HoverText(tip).KeepIter() {
				for rt := range c.RichTextLabel("edited") {
					rt.Small().Weak().Italics()
				}
			}
		})
	}
	if mine {
		if glyph, tone, tip, ok := statusMark(m.Status[i]); ok {
			items = append(items, func() {
				for range c.HoverText(tip).KeepIter() {
					for rt := range c.RichTextLabelColored(tone, color.Transparent, glyph) {
						rt.Small()
					}
				}
			})
		}
	}
	for range hrow(mine) {
		for j := range ordered(len(items), mine) {
			items[j]()
		}
	}
}

// statusMark is the delivery glyph: one check sent, two delivered, two in
// the accent read, a warning failed.
func statusMark(s StatusE) (glyph string, tone color.Color, tip string, ok bool) {
	switch s {
	case StatusSent:
		return icons.PhCheck, color.Hex(styletokens.NeutralTextSecondary.AsHex()), "sent", true
	case StatusDelivered:
		return icons.PhChecks, color.Hex(styletokens.NeutralTextSecondary.AsHex()), "delivered", true
	case StatusRead:
		return icons.PhChecks, color.Hex(styletokens.AccentDefault.AsHex()), "read", true
	case StatusFailed:
		return icons.PhWarning, color.Hex(styletokens.ErrorDefault.AsHex()), "failed", true
	}
	return "", color.Transparent, "", false
}

// renderAvatar is the initials disc beside a group cluster's first bubble.
func renderAvatar(ids *c.WidgetIdStack, m *Model, p int32, dens styletokens.DensityE) {
	name := m.Participants[p].Name
	for range c.HoverText(name).KeepIter() {
		for range c.Frame(ids.PrepareStr("avatar")).
			Fill(participantColor(m, p)).
			CornerRadius(avatarRounding). // designlint:ignore=L4 (a disc: egui clamps the radius to half the side, the badge's Pill idiom)
			InnerMargin(styletokens.PaddingTight(dens)).
			KeepIter() {
			// A fixed box, then a top-down layout centred on the cross axis
			// for the glyph. Not VerticalCentered: that one takes the whole
			// width it is offered, and the disc became a bar across the pane.
			inner := avatarPx - 2*styletokens.PaddingTight(dens)
			c.UiSetMinWidth(inner)
			c.UiSetMaxWidth(inner)
			c.UiSetMinHeight(inner)
			c.UiSetMaxHeight(inner)
			for range c.UiWithLayout().MainDirTopDown().CrossAlignCenter().KeepIter() {
				for rt := range c.RichTextLabelColored(color.Hex(styletokens.NeutralBgPanel.AsHex()), color.Transparent, initials(name)) {
					rt.Small().Strong()
				}
			}
		}
	}
}

// participantColor is the declared colour, else the qualitative cycle by
// index.
func participantColor(m *Model, p int32) color.Color {
	if p < 0 || int(p) >= len(m.Participants) {
		return color.Hex(styletokens.NeutralTextSecondary.AsHex())
	}
	if col := m.Participants[p].Color; col.Kind() != color.ColorKindNone {
		return col
	}
	return color.Hex(styletokens.QualitativeCycle(int(p)).AsHex())
}
