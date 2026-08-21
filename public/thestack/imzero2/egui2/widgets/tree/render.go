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
// id survives, because a node index holds still across those two frames as
// long as the host does not rebuild between them — a weaker assumption than
// the one State makes about identity across rebuilds, and the reason widget
// ids need no Tree.Keys where State does.

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
	//
	// 16 rather than the 20 this started at. UiSetWidth pins the Ui's max as
	// well as its min, so the slack shows up as a gap between the disclosure
	// control and whatever the label starts with, on top of the row's own item
	// spacing — 19 points of it at 20, which reads as the two belonging to
	// different columns. Measured on `34_fsbrowser`'s outline capture at a
	// point per pixel, the caret's ink is unchanged between the two, so 16 is
	// still wider than the control it holds; below that the width would start
	// clipping a glyph rather than trimming a gap.
	discloseWidth float32 = 16
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
// chain at all. See also schemaview's co-section glyph, the same class of
// defect found from the other end (ADR-0176 verification notes).
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
	// Cell draws the column's content for one row, inside a padded cell Ui.
	// On [Input.Outline] it replaces the plain label and runs after the indent
	// and the disclosure control, so a host can put a badge, a count or a
	// secondary tint on the label without giving up the outline chrome.
	//
	// A nil Cell on a host column emits nothing for that column; a nil Cell on
	// the outline draws the node's [Tree.Labels] entry.
	//
	// It takes the whole [Row] rather than the node, because a cell that
	// varies on expansion — a count of what a closed section hides, an indent
	// guide, a different glyph on a leaf — otherwise has to reach back into
	// the [State] it passed in, during the widget's own render pass, to ask
	// something the renderer is already holding.
	//
	// # Three things about this that bite
	//
	// Interactive widgets are allowed and win the pointer over the row's own
	// click sense — that is the arbitration in this file's header comment, and
	// it means a per-row control works, at the price of that control's rect no
	// longer selecting the row. Plain labels must be emitted Selectable(false)
	// or they swallow the row click; the default label does.
	//
	// And a TRUNCATING label takes the whole width it is offered, so anything
	// emitted after it in the same cell is pushed out of view. A count, a
	// chip or a glyph that has to survive a long label belongs in a column of
	// its own, whose width is reserved before the label is laid out — not
	// after the label in this one.
	//
	// The third is a HEIGHT BUDGET, and it is the one with no diagnostic.
	// Content is centred on the row by egui_table's own cell layout, but only
	// while it fits [Input.RowHeight]: something taller is pushed down to the
	// row's top edge and hangs off the bottom, under the next row and through
	// the selection outline, silently (see paddedCell for the egui clamp that
	// does it). At the 22-point default a line of body text fits and a DEFAULT
	// Button does not — a button is its text plus button_padding.y twice. A
	// per-row control therefore wants `.Small()`, which drops that padding and
	// the interact_size floor with it, the way disclose draws the disclosure
	// control. A host that wants full-size controls in its rows should raise
	// [Input.RowHeight] instead.
	Cell func(r Row)
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
	// tree with nowhere to record what is open cannot be drawn. Render binds
	// it to Tree before reading anything out of it, so it survives a rebuild
	// exactly as far as [Tree.Keys] lets it.
	State *State

	// Outline configures the first column — the one carrying the indent, the
	// disclosure control and the label. Its Cell overrides the label.
	Outline Column
	// Columns are the host's own columns, drawn to the right of the outline.
	Columns []Column

	// RowHeight is the fixed height of every row; 0 takes defaultRowHeight.
	RowHeight float32
	// MaxHeight caps the vertical extent the table claims. Feed it the pane's
	// measured height, from c.CapturePaneSize, with a constant to fall back on
	// for the first frame — the probe answers one frame late and not at all on
	// the first. That is what every in-repo adopter does.
	//
	// Left at 0 the table falls back to endETable's auto-fit heuristic, capped
	// by ETABLE_AUTOFIT_CAP_PX, which a tree of any length overruns. That is
	// only the right answer in a host that already bounds the tree tightly and
	// knows it stays short.
	MaxHeight float32
	// Indent is the horizontal step per depth level; 0 takes defaultIndent.
	Indent float32
	// Striped tints odd rows. Off by default: an indent already gives the eye
	// a per-row landmark, and a zebra competes with the selection fill for the
	// same signal. The stripe is painted by the row block, not by etable's own
	// Striped — etable paints its zebra in cell_ui, which runs after the row
	// block and would cover it.
	Striped bool

	// WidthEpoch, when non-zero, is handed to the table as its apply
	// generation (ADR-0151): the binding writes the column widths into the
	// crate's state only when it changes, so a host that resolves widths from
	// stored overrides bumps it when they change and leaves the reader's live
	// drag alone in between. Zero keeps the crate's own state, as before.
	WidthEpoch uint32
	// MinColumnWidth, when positive, is the drag floor for every column in
	// place of each column's seed width; MaxColumnWidth, when positive, the
	// ceiling. A host persisting widths passes the bounds it stores, so a
	// column cannot be dragged below what will come back on the next load.
	MinColumnWidth float32
	MaxColumnWidth float32
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
	// Widths is what the table reported its columns to be, outline column
	// first, when last frame's report was available; nil otherwise. A host
	// persisting widths feeds it to its resolver (ADR-0151).
	Widths []float32
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

	// Bound before anything is read out of the State, because until it is, a
	// node index means whatever the PREVIOUS frame's Tree called it — and the
	// reveal below is resolved through exactly that binding.
	st.Bind(in.Tree)

	// Keys are applied before the reveal, and this is the whole ordering: a
	// key sets a cursor and asks for a reveal, the reveal opens the ancestors,
	// and the flatten below then emits the row the scroll lands on. Running it
	// after the flatten would put the scroll one frame behind every keypress.
	//
	// It needs a row sequence to resolve "the next visible row" against, so it
	// gets a provisional flatten — the one keys were pressed against, since
	// captures are one frame late anyway. Expansion changes make it stale,
	// which is why the real flatten below is unconditional rather than reused.
	keyActivated := int32(-1)
	if st.keyFrameID != 0 {
		if pre, perr := Flatten(in.Tree, st, st.rows[:0]); perr == nil {
			st.rows = pre
			keyActivated = applyKeys(st, pre, st.keyFrameID, st.lastVisibleRows)
		}
	}

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
		// The capture Frame. CaptureKeys and NOT Focusable: see keys.go — a
		// focusable rect senses clicks and, registered after the body, would
		// sit above every row and eat the clicks selection is made of.
		kf := c.Frame(in.Ids.PrepareStr("keys")).CaptureKeys(uint64(treeKeyMask))
		st.keyFrameID = kf.Id()
		for range kf.KeepIter() {
			in.pushColumns()
			et := c.EndETable(in.Ids.PrepareStr("t"), uint64(len(rows)), rowH,
				in.numStickyHeaders(), 0)
			if in.MaxHeight > 0 {
				et = et.MaxHeight(in.MaxHeight)
			}
			if in.WidthEpoch != 0 {
				et = et.ApplyWidths(in.WidthEpoch)
			}
			if ws, ok := et.ColumnWidths(); ok {
				res.Widths = ws
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
			// What PageUp / PageDown mean, kept for the next frame's key pass.
			st.lastVisibleRows = rowEnd - rowBegin
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
					in.paddedCell(r, ci+1, density, in.Columns[ci].Cell)
					et.EndCells()
				}
			}
			et.Send()
		}
	}

	// Applied only now that the pass is over and nothing else reads the rows.
	if clickedNode >= 0 {
		applySelection(st, rows, clickedRow, mode)
		res.Clicked = clickedNode
		// Clicking a row focuses the tree, so the arrow keys work straight
		// after without a separate Tab. The capture Frame does not sense
		// clicks (it must not — see keys.go), so focus is asked for here
		// rather than arriving on its own.
		if st.keyFrameID != 0 {
			c.RequestFocus(st.keyFrameID)
		}
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
	if activated < 0 {
		activated = keyActivated
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
		lo, hi := w, float32(math.Inf(1))
		if in.MinColumnWidth > 0 {
			lo = in.MinColumnWidth
		}
		if in.MaxColumnWidth > 0 {
			hi = in.MaxColumnWidth
		}
		c.EtColumn(w).
			RangeMinMax(lo, hi).
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
//
// "The same inset" is horizontal only, for the reason paddedCell gives: a
// symmetric one spends the header row's height on margin and pushes the title
// off its own midline, so a header no longer lines up with the column under it.
func (in Input) renderHeaders(et c.EndETableFluid, density styletokens.DensityE) {
	if in.numStickyHeaders() == 0 {
		return
	}
	pad := cellInset(density)
	header := func(col uint32, text string) {
		for range et.Headers(0, col) {
			for range c.Frame(in.Ids.PrepareSeq(seqHeaderBase+uint64(col))).
				OuterMargin(0).
				InnerMarginSides(pad, pad, 0, 0).
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
// # The content is asked for the row MINUS both strokes, so the painted rect is the row
//
// egui does not paint a Frame at its content rect. `Frame::widget_rect` is
// `content_rect + inner_margin + stroke.width`, so a stroked Frame paints a
// point wider than its content on every side and the stroke lands inside
// THAT. Asking for the full rowH therefore paints 24 points of chrome on a
// 22-point pitch: the bottom edge falls in the next row (the defect ADR-0176
// M4 was reported with) and the top edge falls in the previous one, where it
// crosses whatever that row's outline column has hanging below its baseline.
//
// Taking a single point off — what this did until 2026-08-21 — closes the
// bottom edge and leaves the top one: 23 points still do not fit in 22, and
// the capture that prompted this showed the outline's top edge cutting the
// descenders of the row above it.
//
// Subtracting both strokes makes the painted rect exactly the row, on either
// branch and by construction rather than by a measured constant:
//
//   - selected: content rowH-2, painted rowH, all four edges inside the row.
//   - not selected: stroke width 0, content rowH, painted rowH — so adjacent
//     stripes tile with no seam, where the old constant left a point of the
//     backdrop showing between every pair.
//
// Pinned by `scripts/dev/play-screenshot-tour.sh 34_fsbrowser`: the
// reproduction is select a row, expand its parent, look at both edges.
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
			c.UiSetMinHeight(rowH - 2*strokeWidth)
		}
	}
	return c.CurrentApplicationState.StateManager.GetResponseByIdRaw(fr.Id())
}

// outlineCell draws the indent, the disclosure control and the label, and
// reports whether the disclosure was clicked.
//
// # There is no Horizontal here, and that is what keeps the column on the row's midline
//
// The obvious spelling wraps the three in a `c.Horizontal()`. It is not needed
// — egui_table already builds every cell Ui as `left_to_right(Align::Center)`,
// and a Frame's content Ui inherits its parent's layout — and it costs the
// whole column its vertical placement.
//
// `ui.horizontal()` allocates a child Ui whose cross-axis extent is
// `spacing.interact_size.y` rather than the cell's, and what centres in that
// band is not what centres in the row. Measured on one capture at a point per
// pixel, 22-point rows: the size and modified columns (Frame → Label) put
// their ink 1 point ABOVE the row's midline; the outline column, one
// Horizontal deep, put it 2 points below, and a host cell that opened a second
// Horizontal of its own put it 5 below. Six points of drift between the first
// column and the rest, on rows with 2 points of headroom — so the descenders
// of the outline column's own labels fell outside the cell rect, which
// egui_table clips (`cell_ui.shrink_clip_rect`), and "canonicaltypes" lost the
// tails of its y and its p.
//
// The same applies to a host's [Column.Cell]: it runs inside this cell's Ui,
// so it should emit its labels straight into it. One that wraps itself in a
// Horizontal seats its content ~3 points low and gets that back by dropping
// the wrapper.
func (in Input) outlineCell(r Row, indent float32, density styletokens.DensityE) (toggled bool) {
	in.paddedCell(r, 0, density, func(row Row) {
		if d := float32(r.Depth) * indent; d > 0 {
			c.AddSpace(d)
		}
		toggled = in.disclose(r)
		if in.Outline.Cell != nil {
			in.Outline.Cell(row)
			return
		}
		// Selectable(false) is load-bearing, not tidiness: egui makes labels
		// selectable by default and a selectable label senses click_and_drag,
		// so it would sit over the row block behind it and eat every click on
		// the label's own rect. Truncate keeps a long label on one line — the
		// row height is fixed, so a wrapped label is a clipped one.
		c.Label(in.Tree.Labels[row.Node]).Selectable(false).Truncate().Send()
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
//
// # The inset is horizontal only, and that is what stops a control hanging out of its row
//
// egui_table builds every cell Ui as `left_to_right(Align::Center)` over the
// cell rect, so cell content is ALREADY centred on the row's midline — this
// widget adds no centring of its own and needs none. What it can do is take
// away the room the centring needs.
//
// A symmetric inset spent Px[1] top and bottom: at Standard density that is 8
// of a 22-point row, leaving 14 for content. A default Button is text plus
// button_padding.y twice — about 25 points at BODY_PT 13 — so it did not fit,
// and egui does not centre what does not fit. `Layout::next_frame_ignore_wrap`
// has a clamp for exactly this ("for horizontal layouts we always want to
// expand down, or we will overlap the row above"): an item taller than its
// frame is pushed back down to the cursor, so the whole overflow lands BELOW
// the row instead of straddling it. That is a control sitting visibly low
// against its own row, and against the selection outline, which is drawn to
// the row pitch and clips it.
//
// Dropping the vertical half is free for content that fits — the centre of a
// 22-point row and the centre of a 14-point box inset 4 from its top are the
// same line — and it hands the overflowing case 8 points back before the clamp
// fires. It does not make the row unbounded: see [Column.Cell] for the budget
// a cell still has to live inside.
func (in Input) paddedCell(r Row, col int, density styletokens.DensityE, body func(r Row)) {
	ncols := uint64(1 + len(in.Columns))
	pad := cellInset(density)
	for range c.Frame(in.Ids.PrepareSeq(seqCellBase+uint64(r.Node)*ncols+uint64(col))).
		OuterMargin(0).
		InnerMarginSides(pad, pad, 0, 0).
		KeepIter() {
		body(r)
	}
}

// cellInset is the horizontal gap between a cell's content and its column
// gridline, both sides — Px[2] rather than the Px[1] this used until
// 2026-08-21. Four points read as text hugging the rule at the densities the
// widget is used at, and a column's own gridline is a harder edge than the
// widget interiors Px[1] is scaled for; six separates the two without costing
// a column enough width to matter. A host sizing its columns should count it
// twice, the way fsbrowser's MinColumnWidth does.
func cellInset(density styletokens.DensityE) float32 {
	return styletokens.PaddingTight(density)
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
