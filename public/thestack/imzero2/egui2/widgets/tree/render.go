package tree

// render.go is the imzero2-facing half of the widget (ADR-0176 M2). Everything
// above it — the columnar input, its validation, the host-owned State, the
// flatten — is decided without a binding in sight; this file turns the row
// sequence into an etable and turns last frame's responses back into State
// changes.
//
// # Why an etable and not a ScrollArea
//
// A tree row is a table row with an indent and a disclosure glyph, and
// endETable already has everything else a tree needs: it prefetches the
// visible row window, keeps per-row offsets, scrolls to a row on request, and
// wraps itself in a bounded child Ui. Composing rows in a ScrollArea would be
// less code here and more cost every frame, because a ScrollArea has no
// culling — every row would build and marshal a deferred block whether or not
// it is on screen (ADR-0176 O2 vs O3).
//
// # The three-layer row
//
// Each visible row is emitted three times over, in this order:
//
//  1. the row block (et.Rows) — one full-width Frame carrying the row's fill,
//     its selection outline, and its click sense. egui_table hands this Ui the
//     WHOLE row across every column, so it reads as continuous across the
//     inter-column gutters that per-cell painting cannot cover.
//  2. the outline cell — indent, disclosure control, label.
//  3. the host's extra cells, one per [Input.Columns] entry.
//
// Because the row block runs first it sits BEHIND the cells in hit-test order,
// which is exactly the arbitration a tree wants and costs nothing to arrange
// (ADR-0176 SD7): the disclosure button wins over its own rect, every label is
// emitted non-selectable so it takes no pointer, and so clicking the arrow
// toggles while clicking anywhere else on the row selects.
//
// # Widget ids key on the node, block-map keys on the row
//
// egui_table addresses blocks by row number, so et.Rows / et.BeginCells take
// the flattened row index. The widget ids inside them do not: they key on the
// node index instead. Responses come back one frame late, and between those
// two frames an expand or a collapse renumbers every row below it — a row-keyed
// id would then hand a click to whichever node inherited that row. A node-keyed
// id survives, because a node index is stable exactly as long as the host's
// Tree ordering is, which is the identity assumption State already documents.

import (
	"errors"
	"math"

	"github.com/stergiotis/boxer/public/keelson/designsystem/styletokens"
	"github.com/stergiotis/boxer/public/keelson/runtime/icons"
	c "github.com/stergiotis/boxer/public/thestack/imzero2/egui2/bindings"
	"github.com/stergiotis/boxer/public/thestack/imzero2/egui2/widgets/color"
)

const (
	// defaultRowHeight fits one line of body text plus the outline cell's
	// padding above and below. Rows are a fixed height: etable needs one to
	// place a row before its content exists, and a tree is a list of single
	// lines by nature.
	defaultRowHeight float32 = 22
	// defaultIndent is the horizontal step per depth level. Wide enough to
	// read as a level, narrow enough that a ten-deep call stack still shows
	// its leaf labels.
	defaultIndent float32 = 14
	// defaultOutlineWidth is where the outline column starts. Resizable
	// columns keep whatever the user drags them to.
	defaultOutlineWidth float32 = 280
	// defaultColumnWidth is the fallback for a host column with no Width.
	defaultColumnWidth float32 = 120
	// discloseWidth pins the disclosure slot so a leaf's label lines up with
	// its siblings'. Both branches set the slot Ui to exactly this, rather
	// than padding a leaf by an eyeballed amount: a Button sizes to its glyph
	// plus egui's button padding, which Go cannot measure and which moves with
	// the theme.
	discloseWidth float32 = 20
	// rowOutlineInset is how much shorter than the row the chrome Frame's
	// CONTENT is asked to be, so the selection outline's bottom edge does not
	// sit on the row pitch. See rowChrome for what that fixes and for the
	// three explanations it is not.
	rowOutlineInset float32 = 1
	// scrollAlignCenter is decode_scroll_align's egui::Align::Center. A
	// revealed row lands mid-viewport with its neighbours around it, which is
	// what makes the reveal legible; TOP would put it against the edge with no
	// context above.
	scrollAlignCenter uint8 = 2
)

// The disclosure glyphs, from the Phosphor icon font the client loads
// explicitly — the same pair `canonicaltypeedit` uses for its own disclosure.
//
// They started as the solid triangles U+25B6 / U+25BC, on the reasoning that
// those are already the repo's collapse glyphs (helphost's nav, kanban's move
// controls). That reasoning was right about the repo and wrong about the font:
// those two codepoints are NOT in Noto Sans, so they fall through the fallback
// chain to the CJK face, which draws them at ideographic full-em and centres
// them on the ideographic box rather than the Latin baseline. Measured on one
// scene with and without `--fallbackFontTTF`, everything else equal: the ink
// grows from 8px to 12px and drops 2px below the label it sits beside. That is
// exactly what a reader sees as "too big and not centred", and it appears only
// on hosts that load a CJK fallback — which every desktop launch does.
//
// The glyphs the precedents use are fine where those precedents put them:
// inline in a label's own text run, where they inherit the run's metrics. In a
// Button they are a widget of their own, and a fallback face's idea of the em
// box becomes visible.
//
// Phosphor is bundled and loaded by the client, so this depends on no fallback
// chain at all. See also the `◈` co-section glyph in schemaview, the same
// class of defect found from the other end (ADR-0176 verification notes).
var (
	glyphCollapsed = icons.PhCaretRight
	glyphExpanded  = icons.PhCaretDown
)

// Id sequence bases, one per widget kind, so two kinds cannot land on the same
// id for the same node. The high half is the ADR number, which makes a stray
// id recognisable in a checkId warning. Everything is derived under the tree's
// own IdScope, so two trees in one frame do not collide.
const (
	seqRowBase      uint64 = 0x0176_0100_0000_0000
	seqDiscSlotBase uint64 = 0x0176_0200_0000_0000
	seqDiscBase     uint64 = 0x0176_0300_0000_0000
	seqCellBase     uint64 = 0x0176_0400_0000_0000
	seqHeaderBase   uint64 = 0x0176_0500_0000_0000
)

// Row chrome colours, from the design-system roles rather than literals
// (ADR-0031). Selection takes the accent role; the stripe takes the faintest
// neutral background so it separates rows without competing with a selection.
var (
	selectionFill   = color.Hex(styletokens.AccentSubtle.AsHex())
	selectionStroke = color.Hex(styletokens.AccentDefault.AsHex())
	stripeFill      = color.Hex(styletokens.NeutralBgFaint.AsHex())
	clearFill       = color.Transparent
)

// Column configures one etable column. The first column is always the outline
// itself and is described by [Input.Outline]; [Input.Columns] adds the rest to
// its right, and their content is entirely the host's.
type Column struct {
	// Header is the column title. A tree whose columns are all untitled shows
	// no header row at all — a one-column outline with a header bar over it
	// reads as a table that forgot its other columns.
	Header string
	// Width is the column's width in points; 0 takes a default. It is a floor
	// as well as a starting point — the column grows to fit wider content and
	// to a drag, but never shrinks below it. See pushColumns for the etable
	// sizing pass that makes the floor necessary rather than merely tidy.
	Width float32
	// Resizable lets the user drag this column's right edge.
	Resizable bool
	// Cell draws the column's content for one node, inside a padded cell Ui.
	// On [Input.Outline] it replaces the plain label and runs after the indent
	// and the disclosure control, so a host can put a badge, a count or a
	// secondary tint on the label without giving up the outline chrome.
	//
	// A nil Cell on a host column emits nothing for that column; a nil Cell on
	// the outline draws the node's [Tree.Labels] entry.
	//
	// Interactive widgets are allowed and win the pointer over the row's own
	// click sense — that is the arbitration in this file's header comment, and
	// it means a per-row control works, at the price of that control's rect no
	// longer selecting the row. Plain labels must be emitted Selectable(false)
	// or they swallow the row click; the default label does.
	Cell func(node int32)
}

// Input is the per-frame render request.
type Input struct {
	// Ids is the host's widget id stack. Render opens its own IdScope under
	// it, so two trees in one frame need only differ in ScopeKey.
	Ids *c.WidgetIdStack
	// ScopeKey names this tree within the host's id space; empty uses "tree".
	// Two trees sharing a host and a ScopeKey share widget ids, which shows up
	// as one of them never seeing a click.
	ScopeKey string
	// Tree is the hierarchy. Validated every frame — the flatten refuses a
	// broken one rather than drawing a plausible outline with arbitrary
	// subtrees missing.
	Tree Tree
	// State is the host-owned expansion, selection and cursor. Required: a
	// tree with nowhere to record what is open cannot be drawn.
	State *State

	// Outline configures the first column — the one carrying the indent, the
	// disclosure control and the label. Its Cell overrides the label.
	Outline Column
	// Columns are the host's own columns, drawn to the right of the outline.
	Columns []Column

	// RowHeight is the fixed height of every row; 0 takes defaultRowHeight.
	RowHeight float32
	// MaxHeight caps the vertical extent the table claims. Leave it 0 in a
	// bounded host. Set it in a tall or unbounded one: endETable otherwise
	// falls back to an auto-fit heuristic capped by ETABLE_AUTOFIT_CAP_PX,
	// which a long tree overruns.
	MaxHeight float32
	// Indent is the horizontal step per depth level; 0 takes defaultIndent.
	Indent float32
	// Striped tints odd rows. Off by default: an indent already gives the eye
	// a per-row landmark, and a zebra competes with the selection fill for the
	// same signal. The stripe is painted by the row block, not by etable's own
	// Striped — etable paints its zebra in cell_ui, which runs after the row
	// block and would cover it.
	Striped bool
}

// Result reports what this frame's pointer interaction did. Every node field
// is -1 for "nothing". The State changes are already applied; the fields exist
// so a host can react — open a detail pane, load a subtree, run a command.
type Result struct {
	// Rows is the row sequence drawn this frame, borrowed from the State's
	// scratch: valid until the next Render on that State, and not to be
	// retained. Useful for "how many rows are showing" and for mapping a node
	// to its row without a second flatten.
	Rows []Row
	// Clicked is the node whose row was clicked. The selection has already
	// been updated for it, honouring ctrl (toggle) and shift (extend from the
	// cursor).
	Clicked int32
	// Activated is the node whose row was double-clicked — "open this", by the
	// convention every file manager uses. An interior node is also toggled;
	// the host decides what a leaf activation means.
	Activated int32
	// Toggled is the node whose expansion changed, whether from its disclosure
	// control or from a double-click.
	Toggled int32
	// Err is the flatten's, on a structurally broken Tree. The widget draws
	// the message in place of the outline rather than drawing nothing, because
	// the failures it reports — a dangling parent index, a cycle — are
	// programming errors in the host and silence makes them look like an empty
	// result set.
	Err error
}

// Render draws the tree and applies this frame's pointer interaction to
// [Input.State].
//
// Responses arrive one frame late, as everywhere in imzero2: the click handled
// here is the click the user made on the previous frame's geometry. State
// changes are collected during the pass and applied after it — mutating
// expansion mid-pass would leave the row slice being iterated describing a
// tree that no longer exists.
//
// An empty Tree draws nothing at all. A host that wants an empty-state message
// owns it, since what to say there ("no matches", "not connected", "loading")
// is the host's fact and not the widget's.
func Render(in Input) (res Result) {
	res.Clicked, res.Activated, res.Toggled = -1, -1, -1
	if in.Ids == nil || in.State == nil {
		res.Err = errors.New("tree: Render needs a non-nil Ids and State")
		return
	}
	st := in.State

	// The reveal is consumed before the flatten because its two halves
	// straddle it: opening the ancestors changes which rows exist, and
	// scrolling to the row needs the index the flatten then assigns.
	reveal := st.takeReveal()
	if reveal >= 0 {
		st.ExpandAncestors(in.Tree, reveal)
	}

	rows, err := Flatten(in.Tree, st, st.rows[:0])
	st.rows = rows
	res.Rows, res.Err = rows, err
	if err != nil {
		c.Label("tree: " + err.Error()).Send()
		return
	}
	if len(rows) == 0 {
		return
	}

	rowH := in.RowHeight
	if rowH <= 0 {
		rowH = defaultRowHeight
	}
	indent := in.Indent
	if indent <= 0 {
		indent = defaultIndent
	}
	scopeKey := in.ScopeKey
	if scopeKey == "" {
		scopeKey = "tree"
	}
	density := styletokens.DensityFromEnv()

	// One action per frame: a user can click at most one control, so a single
	// slot each is enough and there is no ordering to arbitrate.
	clickedNode, clickedRow, activated, toggled := int32(-1), -1, int32(-1), int32(-1)
	mode := clickMode()

	for range c.IdScope(in.Ids.PrepareStr(scopeKey)) {
		in.pushColumns()
		et := c.EndETable(in.Ids.PrepareStr("t"), uint64(len(rows)), rowH,
			in.numStickyHeaders(), 0)
		if in.MaxHeight > 0 {
			et = et.MaxHeight(in.MaxHeight)
		}
		if reveal >= 0 {
			if ri := RowOf(rows, reveal); ri >= 0 {
				et = et.ScrollToRow(uint64(ri), scrollAlignCenter)
			}
		}
		in.renderHeaders(et, density)

		// Emit only the rows egui_table will draw. Without this gate every row
		// of the whole outline builds deferred blocks and widget ids each
		// frame, most of which are culled on arrival — the allocation
		// pathology this widget chose an etable to avoid. VisibleRange reports
		// the PREVIOUS frame's window, so it needs clamping here in a way a
		// fixed-length table does not: a collapse can shorten the outline
		// between the two frames.
		rowBegin, rowEnd := 0, len(rows)
		if rb, re, _, _, _, ok := et.VisibleRange(); ok {
			rowBegin = min(int(rb), len(rows))
			rowEnd = min(int(re), rowEnd)
		}
		for i := rowBegin; i < rowEnd; i++ {
			r := rows[i]
			flags := in.rowChrome(et, i, r, rowH, st.IsSelected(r.Node))
			if flags.HasPrimaryClicked() {
				clickedNode, clickedRow = r.Node, i
			}
			if flags.HasDoubleClicked() {
				activated = r.Node
			}

			et.BeginCells(uint64(i), 0)
			if in.outlineCell(r, indent, density) {
				toggled = r.Node
			}
			et.EndCells()

			for ci := range in.Columns {
				if in.Columns[ci].Cell == nil {
					continue
				}
				et.BeginCells(uint64(i), uint32(ci+1))
				in.paddedCell(r.Node, ci+1, density, in.Columns[ci].Cell)
				et.EndCells()
			}
		}
		et.Send()
	}

	// Applied only now that the pass is over and nothing else reads the rows.
	if clickedNode >= 0 {
		applySelection(st, rows, clickedRow, mode)
		res.Clicked = clickedNode
	}
	if toggled < 0 && activated >= 0 {
		// Double-clicking an interior row opens or closes it, the way every
		// file manager does. A leaf has nothing to toggle and is reported as
		// an activation only.
		if ri := RowOf(rows, activated); ri >= 0 && rows[ri].HasChildren {
			toggled = activated
		}
	}
	if toggled >= 0 {
		st.ToggleExpanded(toggled)
		res.Toggled = toggled
	}
	res.Activated = activated
	return
}

// pushColumns registers the etable's columns. They are drained in registration
// order at apply time, so they must all be sent before EndETable.
//
// Each width is declared as the column's range minimum as well as its starting
// width, which is what makes it hold. egui_table runs a full sizing pass the
// first frame a table id exists and, for RESIZABLE columns only, replaces the
// declared width with the widest cell actually laid out (table.rs:845 skips
// non-resizable ones, :861 does the shrink) — then stores that and reuses it
// forever. A truncating label makes this a collapse rather than a fit: it lays
// out to whatever width it is given and reports that back, so the column
// converges on a stub. The range floor stops the shrink at the declared width;
// growing past it, by content or by a drag, still works.
func (in Input) pushColumns() {
	push := func(col Column, deflt float32) {
		w := col.Width
		if w <= 0 {
			w = deflt
		}
		c.EtColumn(w).
			RangeMinMax(w, float32(math.Inf(1))).
			Resizable(col.Resizable).
			Send()
	}
	push(in.Outline, defaultOutlineWidth)
	for i := range in.Columns {
		push(in.Columns[i], defaultColumnWidth)
	}
}

// numStickyHeaders is 1 when any column carries a title and 0 otherwise, which
// is what keeps a plain one-column outline from growing a header bar it has no
// use for.
func (in Input) numStickyHeaders() uint32 {
	if in.Outline.Header != "" {
		return 1
	}
	for i := range in.Columns {
		if in.Columns[i].Header != "" {
			return 1
		}
	}
	return 0
}

// renderHeaders emits the header row as deferred blocks rather than as
// etHeaderText, so the titles get the same inset the cells get — the text
// fallback is a bare ui.heading() sitting flush against the column gridline.
func (in Input) renderHeaders(et c.EndETableFluid, density styletokens.DensityE) {
	if in.numStickyHeaders() == 0 {
		return
	}
	header := func(col uint32, text string) {
		for range et.Headers(0, col) {
			for range c.Frame(in.Ids.PrepareSeq(seqHeaderBase + uint64(col))).
				OuterMargin(0).
				InnerMargin(styletokens.PaddingInner(density)).
				KeepIter() {
				atoms := c.Atoms().BeginRichText(text).Strong().End().Keep()
				c.LabelAtoms(atoms).Selectable(false).Send()
			}
		}
	}
	header(0, in.Outline.Header)
	for i := range in.Columns {
		header(uint32(i+1), in.Columns[i].Header)
	}
}

// rowChrome paints one row's backdrop and outline across the full row width
// and returns its response flags. The Frame spans every column, gutters
// included, because UiSetMinWidthAvailable stretches it to the row Ui's
// available width; left alone a Frame sizes to its content, which for an empty
// one is nothing at all.
//
// Selection beats the stripe, and the stroke is width 0 and transparent when
// the row is not selected so nothing picks up an accidental border.
//
// # The content is a point shorter than the row, and that is what closes the outline
//
// Asking for the full rowH loses the selection outline's BOTTOM edge on every
// row that has another row after it: top, left and right paint, the bottom
// does not. It is the defect ADR-0176 M4 was reported with, and it is worth
// the paragraph because the shape of it misleads.
//
// What it is not, each ruled out by measuring a headless capture at one pixel
// per point:
//
//   - not the next row painting over it. An unselected, unstriped row's fill
//     is RGBA(0,0,0,0) and its stroke is width 0, so it paints nothing at all;
//     the row below the affected one is usually exactly that.
//   - not the row's own clip. egui_table builds a row Ui with `max_rect` and,
//     unlike the cell path, never calls `shrink_clip_rect` — and with the
//     point taken off here the bottom edge lands one pixel BELOW the row and
//     is drawn, which a clip would have removed.
//   - not rasterisation losing a hairline. At stroke width 2 the bottom edge
//     is absent entirely rather than thinned.
//
// What is left is inside epaint's `StrokeKind::Inside` tessellation of a rect
// whose height is exactly the row pitch: the fill wins the bottom band and the
// three other sides survive. Taking a point off the content height moves the
// stroke off that boundary and all four sides paint — measured on both row
// parities, and a single row with nothing below it draws correctly either way.
// The fill still covers the row, since the frame paints its own rect rather
// than the content's, so this leaves no seam between adjacent stripes.
//
// It is a workaround for a renderer detail, not a layout decision, and it is
// pinned by `scripts/dev/tree-widget-scene.sh`'s capture: the reproduction is
// select a row, expand its parent, look at the bottom edge.
func (in Input) rowChrome(et c.EndETableFluid, rowIdx int, r Row, rowH float32, selected bool) c.ResponseFlagsE {
	fill := clearFill
	switch {
	case selected:
		fill = selectionFill
	case in.Striped && rowIdx%2 == 1:
		fill = stripeFill
	}
	strokeWidth, stroke := float32(0), clearFill
	if selected {
		strokeWidth, stroke = 1.0, selectionStroke
	}
	var fr c.FrameFluid
	for range et.Rows(uint64(rowIdx)) {
		fr = c.Frame(in.Ids.PrepareSeq(seqRowBase+uint64(r.Node))).
			Fill(fill).
			Stroke(strokeWidth, stroke).
			OuterMargin(0).
			InnerMargin(0).
			SenseClick().
			HoverCursorPointer()
		for range fr.KeepIter() {
			c.UiSetMinWidthAvailable()
			c.UiSetMinHeight(rowH - rowOutlineInset)
		}
	}
	return c.CurrentApplicationState.StateManager.GetResponseByIdRaw(fr.Id())
}

// outlineCell draws the indent, the disclosure control and the label, and
// reports whether the disclosure was clicked.
func (in Input) outlineCell(r Row, indent float32, density styletokens.DensityE) (toggled bool) {
	in.paddedCell(r.Node, 0, density, func(node int32) {
		for range c.Horizontal().KeepIter() {
			if d := float32(r.Depth) * indent; d > 0 {
				c.AddSpace(d)
			}
			toggled = in.disclose(r)
			if in.Outline.Cell != nil {
				in.Outline.Cell(node)
				return
			}
			// Selectable(false) is load-bearing, not tidiness: egui makes
			// labels selectable by default and a selectable label senses
			// click_and_drag, so it would sit over the row block behind it and
			// eat every click on the label's own rect. Truncate keeps a long
			// label on one line — the row height is fixed, so a wrapped label
			// is a clipped one.
			c.Label(in.Tree.Labels[node]).Selectable(false).Truncate().Send()
		}
	})
	return
}

// disclose emits the row's disclosure control into a fixed-width slot and
// reports a click on it. Both branches pin the slot to discloseWidth so a
// leaf's label starts where an interior sibling's does.
func (in Input) disclose(r Row) (clicked bool) {
	for range c.Frame(in.Ids.PrepareSeq(seqDiscSlotBase + uint64(r.Node))).
		OuterMargin(0).
		InnerMargin(0).
		KeepIter() {
		c.UiSetWidth(discloseWidth)
		if !r.HasChildren {
			break
		}
		glyph := glyphCollapsed
		if r.Expanded {
			glyph = glyphExpanded
		}
		// Frame(false) drops the button's own background so the row's fill
		// shows through; it stays a Button, and therefore still wins the
		// pointer over the row sense behind it.
		clicked = c.Button(in.Ids.PrepareSeq(seqDiscBase+uint64(r.Node)),
			c.Atoms().Text(glyph).Keep()).
			Frame(false).
			Small().
			SendResp().
			HasPrimaryClicked()
	}
	return
}

// paddedCell insets a cell's content so it does not sit flush against the
// column gridlines. The row block behind it owns the fill, the outline and the
// click sense, so the inset is all a cell Frame has left to do.
func (in Input) paddedCell(node int32, col int, density styletokens.DensityE, body func(node int32)) {
	ncols := uint64(1 + len(in.Columns))
	for range c.Frame(in.Ids.PrepareSeq(seqCellBase + uint64(node)*ncols + uint64(col))).
		OuterMargin(0).
		InnerMargin(styletokens.PaddingInner(density)).
		KeepIter() {
		body(node)
	}
}

// selectMode is how a click combines with the selection already there — the
// three-way split every list control has.
type selectMode uint8

const (
	selectReplace selectMode = iota // plain click
	selectToggle                    // ctrl / command click
	selectExtend                    // shift click
)

// clickMode reads last frame's modifier keys, which is the right frame: the
// click being handled is also last frame's, so the two describe the same
// moment. Command rather than Ctrl alone so the shortcut follows the OS
// convention on macOS.
func clickMode() selectMode {
	mods := c.CurrentApplicationState.StateManager.GetModifiers()
	switch {
	case mods.Command || mods.Ctrl:
		return selectToggle
	case mods.Shift:
		return selectExtend
	}
	return selectReplace
}

// applySelection resolves a click on rows[rowIdx] under mode.
//
// Replace and toggle move the cursor; extend does not. A shift click leaves
// the cursor on the anchor it extended from, so a second shift click
// re-extends from that same row rather than walking the range along behind it.
func applySelection(st *State, rows []Row, rowIdx int, mode selectMode) {
	node := rows[rowIdx].Node
	switch mode {
	case selectToggle:
		st.SetSelected(node, !st.IsSelected(node))
	case selectExtend:
		// A cursor that is no longer visible — its row collapsed away — has no
		// anchor to extend from, so the click degrades to a plain one.
		anchor := RowOf(rows, st.Cursor())
		if anchor < 0 {
			st.SelectOnly(node)
			break
		}
		lo, hi := anchor, rowIdx
		if lo > hi {
			lo, hi = hi, lo
		}
		st.ClearSelection()
		for i := lo; i <= hi; i++ {
			st.SetSelected(rows[i].Node, true)
		}
		return
	default:
		st.SelectOnly(node)
	}
	st.SetCursor(node)
}
