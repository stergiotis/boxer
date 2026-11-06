package markdown

import (
	"strconv"

	"github.com/stergiotis/boxer/public/keelson/designsystem/styletokens"
	c "github.com/stergiotis/boxer/public/thestack/imzero2/egui2/bindings"
	"github.com/stergiotis/boxer/public/thestack/imzero2/egui2/widgets/color"
	east "github.com/yuin/goldmark/extension/ast"
)

// padDefault resolves the IDS Padding.Default token at the active
// density (ADR-0032 §SD2). markdown rendering is stateless — there's
// no Renderer struct to cache the density on, so each call site reads
// the env afresh (cheap; ~once per Frame call site).
func padDefault() (v float32) {
	v = styletokens.PaddingDefault(styletokens.ActiveDensity())
	return
}

// headingGap is the vertical air set above a heading of the given level.
// Two tiers: the outer-padding step for the levels that open a major
// section (H1/H2), the default step for H3–H6.
//
// Two token tiers rather than a gradient proportional to the heading's
// font size. A gradient reads better in the abstract but reintroduces a
// tuned constant per level, and the tokens already carry the density —
// so a Tight document tightens its section breaks along with everything
// else, for free. Without any of this every block pair in a document was
// separated by exactly one item_spacing (8 px at standard density) and
// the hierarchy rode on font size alone (L1 in the rendering review).
func headingGap(level uint8) (v float32) {
	d := styletokens.ActiveDensity()
	if level <= 2 {
		v = styletokens.PaddingOuter(d)
		return
	}
	v = styletokens.PaddingDefault(d)
	return
}

// headingGapBelow is the air set under a heading beyond the item gap
// every block pair gets. The stylesheet gives a heading 0.6 em below
// against 1.4 em above; the item gap alone left the native heading
// closer to its paragraph than to the section before it, which the
// trial's `gap H↓` column read as 1.4 line heights against the IDS arm's
// 1.6. The same two tiers as [headingGap], one step down the scale.
func headingGapBelow(level uint8) (v float32) {
	d := styletokens.ActiveDensity()
	if level <= 2 {
		v = styletokens.PaddingInner(d)
		return
	}
	v = styletokens.PaddingHair(d)
	return
}

// render emits a single segment into the current egui Ui scope.
//
// rc threads the monotonic per-Render-invocation id sequence counter,
// the Doc-scoped image tracker, and the configured image fit cap. Each
// id-needing widget bumps rc.idSeq, so layout state (collapse, scroll,
// drag) keyed by id stays stable across frames as long as the lowering
// rules produce the same segment sequence.
func (inst *segment) render(rc *renderCtx) {
	atDocStart := !rc.emittedAny
	rc.emittedAny = true
	// A block id or an offset target ([renderCtx.armBlockScroll]) scrolls
	// to whatever block is about to render. A heading emits its gap first
	// (below), for the reason recorded there; every other kind has no gap
	// of its own, so the op goes out here, before the first widget.
	if rc.scrollNow && inst.kind != segKindHeading {
		c.ScrollToCursor(0)
		rc.scrollNow = false
	}
	switch inst.kind {
	case segKindHeading:
		// Air above the heading — but not above the first thing in the
		// document, where a leading gap reads as a misaligned pane
		// rather than as a section break.
		//
		// Emitted before the scroll dispatch below, not after: that op
		// targets the current cursor, and a gap placed after it would
		// push the heading below the position we just asked egui to
		// scroll to. [c.AddSpace] takes no widget id, so this does not
		// disturb the id-derivation order (EXPLANATION.md) and needs no
		// consumer scope-key bump.
		if !atDocStart {
			c.AddSpace(headingGap(inst.headingLevel))
		}
		// Scroll-to-section dispatch: scrollToSlug is consumed when it
		// matches the slug of the heading about to render. The op
		// targets the current cursor position (top of the to-be-
		// emitted heading), so the heading lands at the top of the
		// enclosing ScrollArea. Outside a ScrollArea egui drops it
		// silently — see [bindings.ScrollToCursor] doc.
		if rc.scrollToSlug != "" && rc.headingIdx < len(rc.headings) {
			if rc.headings[rc.headingIdx].Slug == rc.scrollToSlug {
				rc.scrollNow = true
				// Clear so a slug appearing twice in one doc only
				// triggers on the first occurrence — matches the
				// "scroll once per click" UX consumers expect.
				rc.scrollToSlug = ""
			}
		}
		if rc.scrollNow {
			c.ScrollToCursor(0)
			rc.scrollNow = false
		}
		rc.headingIdx++
		renderRuns(inst.runs, rc)
		c.AddSpace(headingGapBelow(inst.headingLevel))
	case segKindParagraph:
		renderRuns(inst.runs, rc)
	case segKindCodeBlock:
		// Code-block action button ([Input.ActionLabels]). Only when the
		// caller enabled actions; otherwise the block is untouched. The
		// small IDS button sits on its own line above the code; a click
		// records a CodeBlockAction reported in Result.Actions. The CodeView's own selectable text (Ctrl+C) is
		// independent of this. codeBlockIdx advances per code block so the
		// action's ordinal is stable regardless of whether buttons render.
		// The block's widgets live under its own key (assignCodeKeys), not
		// under the document-order sequence, so inserting a block above it
		// leaves its id and egui memory alone.
		for range c.IdScope(rc.ids.PrepareHighEntropy(inst.codeKey)) {
			if rc.actionsEnabled &&
				(rc.actionAccept == nil || rc.actionAccept(inst.codeText, inst.codeLang)) {
				renderCodeActionButtons(rc, inst.codeText, inst.codeLang, rc.codeBlockIdx)
			}
			c.CodeView(rc.ids.PrepareStr("code"), inst.codeJob()).Send()
		}
		rc.codeBlockIdx++
	case segKindList:
		renderList(inst, rc)
	case segKindListItem:
		for i := range inst.children {
			inst.children[i].render(rc)
		}
	case segKindBlockquote:
		renderBlockquote(inst, rc)
	case segKindHorizontalRule:
		c.Separator().Send()
	case segKindCallout:
		renderCallout(inst, rc)
	case segKindTable:
		renderTable(inst, rc)
	case segKindFootnotes:
		// The definitions after a rule, as goldmark's HTML sets them apart.
		// Separator and the list's layout containers take no widget id, so
		// the block disturbs the id-derivation order only through what its
		// items render.
		c.Separator().Send()
		for i := range inst.children {
			inst.children[i].render(rc)
		}
	}
}

// Initial column width bounds, in points. A column starts at roughly
// the width of its widest cell, clamped into this range: below the
// minimum a short column collapses to an unreadable sliver, and above
// the maximum a single long cell would push the rest of the table past
// the pane's right edge. A clamped column truncates with an ellipsis and
// stays resizable, so the reader can widen it.
const (
	tableColumnMinWidth float32 = 48.0
	tableColumnMaxWidth float32 = 260.0
)

// tableColumnBudget is roughly the total width, in points, that a
// table's fixed-width columns may claim between them. Only the last
// column is a remainder, and egui_extras gives it whatever the fixed
// ones leave over — so on a wide table with long cells, per-column
// caps that are individually reasonable add up to more than the pane
// and starve the last column to nothing. Splitting one budget across
// the fixed columns keeps a share for the remainder no matter how many
// columns there are. It is a guess at a doc pane's usable width, not a
// measurement (the render path cannot ask), and it only sets the
// starting widths — the reader can drag any of them.
const tableColumnBudget float32 = 560.0

// tableGlyphAdvance is the fraction of the font's point size that one
// character of prose occupies on average. Used to turn a cell's rune
// count into an initial column width without measuring text — see
// [tableRowHeightFactor] for why measurement is not available here. It
// is an average over mixed-case prose in a proportional font, so a
// column of capitals comes out narrow and one of thin glyphs wide; the
// column is only *initially* this size and the reader can drag it.
const tableGlyphAdvance float32 = 0.55

// tableCellPadding pads the estimated text width so a column that fits
// its content exactly does not sit flush against the next column's
// separator.
const tableCellPadding float32 = 12.0

// tableRowHeightFactor turns a text-style point size into the fixed row
// height the table op takes. egui's real text metrics are not reachable
// from the render path — the Fetcher opcodes that could report them may
// only run from StateManager.Sync() at frame end — so the row height is
// a heuristic multiple of the IDS type scale rather than a measurement.
// It errs generous: the op clips anything taller than rowHeight, and a
// clipped row loses its descenders where a roomy one only wastes a few
// pixels.
const tableRowHeightFactor float32 = 1.6

// tableFlowMaxRows is the body-row count above which a table keeps its
// own vertical scroll instead of flowing with the document.
//
// A document's tables should flow: the reader scrolls the pane, and a
// table that opens its own scroll region inside that pane captures the
// wheel and cuts the document in two. That is the right default and the
// threshold exists only to bound the case it cannot afford — a generated
// table of thousands of rows would lay every row out on every frame,
// because a flowing table has no viewport to cull against.
//
// A heuristic, and a deliberately blunt one. It is not a knob: a knob
// would push the judgement onto every caller, and none of them knows
// more about this than the renderer does. What could replace it is a
// measurement seam — the render path cannot ask egui for the remaining
// viewport height today (the Fetcher opcodes that could may only run
// from StateManager.Sync at frame end).
const tableFlowMaxRows = 100

// tableScrollRows bounds the internal scroll of a table that exceeds
// [tableFlowMaxRows]: its viewport is this many rows tall, so a
// pathological table costs a bounded region rather than the whole frame.
const tableScrollRows = 24

// tableFlows reports whether a table of the given body-row count renders
// as part of the document flow, and — when it does not — the scroll
// height its own viewport is bounded to.
//
// Split out as a pure function because the render path itself is
// untestable (it needs a live FFFI sink), and the policy is the part
// worth pinning.
func tableFlows(rows int, hasHeader bool) (flows bool, maxScrollHeight float32) {
	if rows <= tableFlowMaxRows {
		flows = true
		return
	}
	maxScrollHeight = tableScrollRows * tableRowHeight(hasHeader)
	return
}

// tableRowHeight derives the per-row height from the IDS type scale at
// the active density. The op applies one height to the header row and
// every body row alike, and the interpreter draws header cells with
// `ui.heading()` (TextStyle::Heading, the larger step), so a table with
// a header sizes off HeadingPt and a headerless one off BodyPt.
func tableRowHeight(hasHeader bool) (h float32) {
	d := styletokens.ActiveDensity()
	pt := styletokens.ScaledPt(styletokens.BodyPt, d)
	if hasHeader {
		if hp := styletokens.ScaledPt(styletokens.HeadingPt, d); hp > pt {
			pt = hp
		}
	}
	h = pt * tableRowHeightFactor
	return
}

// tableColumnCap is the per-column ceiling for a table with cols
// columns: [tableColumnBudget] shared out among the fixed-width ones
// (every column but the trailing remainder), bounded by the absolute
// min/max. More columns therefore means a tighter ceiling on each.
func tableColumnCap(cols int) (w float32) {
	fixed := max(cols-1, 1)
	w = tableColumnBudget / float32(fixed)
	if w > tableColumnMaxWidth {
		w = tableColumnMaxWidth
	} else if w < tableColumnMinWidth {
		w = tableColumnMinWidth
	}
	return
}

// tableColumnWidth estimates the initial width of a column whose widest
// cell is runes long, clamped into [tableColumnMinWidth, colCap].
//
// An estimate rather than a measurement, and an explicit one rather than
// `Column::auto()`: auto sizing derives the width from what the previous
// frame's cells actually used, which for a clipping column is the
// *truncated* width — the column then never learns it should be wider
// and stays pinned near egui_extras' 100pt fallback suggestion. Feeding
// an absolute initial width side-steps that feedback loop.
func tableColumnWidth(runes uint32, colCap float32) (w float32) {
	pt := styletokens.ScaledPt(styletokens.BodyPt, styletokens.ActiveDensity())
	w = float32(runes)*pt*tableGlyphAdvance + tableCellPadding
	if w < tableColumnMinWidth {
		w = tableColumnMinWidth
	} else if w > colCap {
		w = colCap
	}
	return
}

// TableColumnInitialWidth is the initial width, in points, the renderer
// gives a column whose widest cell is runes long in a table of cols
// columns: the estimate [tableColumnWidth] makes under the cap
// [tableColumnCap] sets. Exported so a grid that edits a table starts its
// columns where the rendered table had them.
func TableColumnInitialWidth(runes uint32, cols int) (w float32) {
	w = tableColumnWidth(runes, tableColumnCap(cols))
	return
}

// TableRowHeight is the fixed row height the renderer draws a table's
// rows at — see [tableRowHeight].
func TableRowHeight(hasHeader bool) (h float32) {
	h = tableRowHeight(hasHeader)
	return
}

// renderTable emits one GFM table through the table op's register-drain
// protocol: columns first, then the header texts, then the body cells
// row-major, then the table node that drains all three and renders. The
// pushes carry no id and paint nothing on their own — only the closing
// [bindings.Table] does — so the four steps must stay adjacent, with no
// other table's pushes interleaved. Nothing between them here emits, and
// a nested table is impossible in GFM, so adjacency holds by
// construction.
//
// A table of ordinary length FLOWS with the document (see [tableFlows]):
// it takes its full height inline and the enclosing pane scrolls it,
// rather than opening a scroll region of its own that swallows the wheel
// halfway down a page. Only past the row threshold does it keep an
// internal scroll, bounded so the cost stays bounded.
//
// Rows are a fixed height (see [tableRowHeight]): cell text does not
// wrap and anything taller than one line is clipped. Cells are plain
// strings — a hyperlink inside a cell renders as its label text, not as
// a clickable link.
//
// The seq slot per table is load-bearing, not just an id the op happens
// to require: the table node opens an egui Ui id scope around itself, so
// that id is what keeps two tables in one document from sharing
// egui_extras' column widths and scroll offset. Do not hoist it to a
// constant.
func renderTable(s *segment, rc *renderCtx) {
	cols := int(s.tableCols)
	if cols <= 0 {
		return
	}
	rows := len(s.tableCells) / cols
	hasHeader := len(s.tableHeader) > 0
	seq := rc.idSeq
	rc.idSeq++

	// A table that flows with the document is a grid of wrapping cells
	// (renderTableGrid); one too long to lay out every frame keeps the
	// table op, whose rows are fixed-height and scroll inside a bound.
	// Either way the table takes one id, so the id-derivation order the
	// EXPLANATION invariant pins is the same on both paths.
	flows, maxScroll := tableFlows(rows, hasHeader)
	if flows {
		renderTableGrid(s, rc, seq, cols, rows)
		return
	}

	// Every column but the last starts at roughly its content width and
	// can be dragged; the last takes whatever width is left so the table
	// fills the pane instead of ending in a ragged edge. All of them
	// clip, which egui renders as an ellipsis where it can, and each
	// carries the delimiter row's alignment.
	colCap := tableColumnCap(cols)
	for i := 0; i < cols-1; i++ {
		var runes uint32
		if i < len(s.tableColRunes) {
			runes = s.tableColRunes[i]
		}
		tableColumnAligned(c.TableColumn().Initial(tableColumnWidth(runes, colCap)).
			AtLeast(tableColumnMinWidth).
			Resizable(true).ClipContents(true), s.tableAligns, i).Send()
	}
	tableColumnAligned(c.TableColumn().Remainder().ClipContents(true), s.tableAligns, cols-1).Send()

	// Zero header pushes render a headerless body — that is the op's
	// documented behaviour, and what a table whose header row lowered to
	// nothing should look like.
	for _, h := range s.tableHeader {
		c.TableHeaderText(h).Send()
	}
	for _, cell := range s.tableCells {
		c.TableCellText(cell).Send()
	}

	c.Table(rc.ids.PrepareSeq(seq), tableRowHeight(hasHeader), uint64(rows)).
		Striped(true).
		MaxScrollHeight(maxScroll).
		Send()
}

// renderTableGrid draws a table as an egui Grid: one wrapping label per
// cell, bounded to its column's width estimate, so a long cell wraps onto
// more lines instead of clipping to one with an ellipsis, and the row
// grows to hold it; header cells are strong at body size rather than the
// table op's heading step. The column bound is the same estimate the
// table op seeds its resizable columns with (tableColumnWidth), so the
// two paths agree on where a column stops; a column's actual width is the
// widest wrapped cell in it, which the grid measures. A delimiter-row
// alignment becomes the cell's cross alignment. The grid lays out every
// row each frame, which is why tableFlows bounds the rows this path takes.
func renderTableGrid(s *segment, rc *renderCtx, seq uint64, cols, rows int) {
	colCap := tableColumnCap(cols)
	widths := make([]float32, cols)
	for i := range widths {
		var runes uint32
		if i < len(s.tableColRunes) {
			runes = s.tableColRunes[i]
		}
		widths[i] = tableColumnWidth(runes, colCap)
	}
	align := func(i int) (a east.Alignment) {
		if i < len(s.tableAligns) {
			a = s.tableAligns[i]
		}
		return
	}
	// Cells carry their own side padding in a frame, with no column gap
	// between them, so the header's band is one surface across the row
	// as `th` is in the stylesheets; the row gap stays, and the stripes
	// span it.
	c.UiSetItemSpacing(0, styletokens.GapInline(styletokens.ActiveDensity()))
	for range c.Grid(rc.ids.PrepareSeq(seq)).NumColumns(uint32(cols)).Striped(true).KeepIter() {
		if len(s.tableHeader) > 0 {
			for i, h := range s.tableHeader {
				renderTableCell(h, widths[i%cols], align(i%cols), true, rc)
			}
			c.EndRow()
		}
		for r := 0; r < rows; r++ {
			for i := 0; i < cols; i++ {
				renderTableCell(s.tableCells[r*cols+i], widths[i], align(i), false, rc)
			}
			c.EndRow()
		}
	}
}

// renderTableCell is one grid cell: a top-down scope aligned per the
// delimiter row, bounded to the column's width so the label inside wraps
// there. Bounding the scope shrinks only its maximum, so a short cell
// stays as narrow as its text and the column does not grow to the bound.
func renderTableCell(text string, width float32, a east.Alignment, header bool, rc *renderCtx) {
	d := styletokens.ActiveDensity()
	frame := c.Frame(rc.ids.PrepareSeq(rc.idSeq)).
		InnerMarginSides(styletokens.PaddingDefault(d), styletokens.PaddingDefault(d),
			styletokens.PaddingInner(d), styletokens.PaddingInner(d))
	rc.idSeq++
	if header {
		frame = frame.Fill(tableHeaderBg)
	}
	for range frame.KeepIter() {
		renderTableCellText(text, width, a, header)
	}
}

// renderTableCellText lays the cell's text out at the column's width and
// alignment, a header in the bold face.
func renderTableCellText(text string, width float32, a east.Alignment, header bool) {
	layout := c.UiWithLayout().MainDirTopDown()
	switch a {
	case east.AlignCenter:
		layout = layout.CrossAlignCenter()
	case east.AlignRight:
		layout = layout.CrossAlignMax()
	default:
		layout = layout.CrossAlignMin()
	}
	for range layout.KeepIter() {
		// Min and max at the column's width, so a cell's frame — and the
		// header's band — spans the column rather than the text in it.
		c.UiSetMinWidth(width)
		c.UiSetMaxWidth(width)
		if header {
			c.LabelAtoms(c.Atoms().BeginRichText(text).Strong().Bold().End().Keep()).Wrap().Send()
		} else {
			c.Label(text).Wrap().Send()
		}
	}
}

// tableColumnAligned gives column i the delimiter row's alignment; a
// column without one, or past the aligns the lowering kept, stays left.
func tableColumnAligned(col c.TableColumnFluid, aligns []east.Alignment, i int) c.TableColumnFluid {
	if i >= len(aligns) {
		return col
	}
	switch aligns[i] {
	case east.AlignCenter:
		return col.AlignCenter()
	case east.AlignRight:
		return col.AlignRight()
	}
	return col
}

// renderCodeActionButtons emits a horizontal row of small IDS buttons above
// a code block, one per rc.actionLabels entry — used only when
// [Input.ActionLabels] is set. It runs inside the block's own id scope, so
// each button's id is its label index. A click records a [CodeBlockAction]
// carrying the block's verbatim text, fence language, and ordinal, plus the
// 0-based index of the clicked button, reported in [Result.Actions]. text/lang/idx are passed rather than read off rc so
// the call site stays adjacent to the CodeView it labels.
func renderCodeActionButtons(rc *renderCtx, text, lang string, idx int) {
	mask := ^uint64(0)
	if rc.actionMask != nil {
		if mask = rc.actionMask(text, lang); mask == 0 {
			return
		}
	}
	for range c.Horizontal().KeepIter() {
		for btn, label := range rc.actionLabels {
			if btn < 64 && mask&(1<<uint(btn)) == 0 {
				continue
			}
			if c.Button(rc.ids.PrepareSeq(uint64(btn)), c.Atoms().Text(label).Keep()).
				Small().SendResp().HasPrimaryClicked() {
				rc.codeActions = append(rc.codeActions,
					CodeBlockAction{Text: text, Lang: lang, Index: idx, Button: btn})
			}
		}
	}
}

// renderBlockquote draws a quote as the reference stylesheets do: a
// rule along the left edge and the body set in from it, no box. The
// Frame has no stroke and no fill; the rule is its left edge bar at the
// strong stroke width in the neutral border colour (ids.css: a
// `border-left` of `--ids-border-strong` in `--ids-border-default`), and
// the inner margin clears it by the default padding on the left with a
// hairline of vertical padding, since the quote's own line height
// carries the rest. The group frame the quote wore before is withdrawn
// in ADR-0180's Updates: a boxed quote read as a callout without a
// title, where every reference stylesheet draws a rule.
func renderBlockquote(s *segment, rc *renderCtx) {
	seq := rc.idSeq
	rc.idSeq++
	bar := styletokens.StrokeStrong
	for range c.Frame(rc.ids.PrepareSeq(seq)).
		EdgeBarLeft(bar, color.Hex(styletokens.NeutralBorderDefault.AsHex())).
		InnerMarginSides(bar+padDefault(), 0, styletokens.StrokeHair, styletokens.StrokeHair).
		KeepIter() {
		for range c.Vertical().KeepIter() {
			for i := range s.children {
				s.children[i].render(rc)
			}
		}
	}
}

// renderCallout emits an Obsidian callout as either a CollapsingHeader
// (when Foldable) or a themed Frame with a strong-styled title row
// above the body. The frame's stroke + fill come from [calloutColors];
// the title row uses the type-derived glyph so the family is visible
// even before the user reads the title text.
func renderCallout(s *segment, rc *renderCtx) {
	theme, glyph := calloutTheme(s.calloutType)
	border, fill := calloutColors(theme)
	titleText := calloutTitleText(s.calloutType, s.calloutTitle, glyph)

	if s.calloutFoldable {
		seq := rc.idSeq
		rc.idSeq++
		ch := c.CollapsingHeader(rc.ids.PrepareSeq(seq),
			c.WidgetText().Text(titleText).Keep())
		if s.calloutDefaultOpen {
			ch = ch.DefaultOpen(true)
		}
		for range ch.KeepIter() {
			for range c.Frame(rc.ids.PrepareSeq(rc.idSeq)).
				Stroke(styletokens.StrokeStrong, border).
				Fill(fill).
				CornerRadius(styletokens.RoundingMd).
				InnerMargin(padDefault()).
				KeepIter() {
				rc.idSeq++
				for range c.Vertical().KeepIter() {
					for i := range s.children {
						s.children[i].render(rc)
					}
				}
			}
		}
		return
	}

	seq := rc.idSeq
	rc.idSeq++
	for range c.Frame(rc.ids.PrepareSeq(seq)).
		Stroke(styletokens.StrokeStrong, border).
		Fill(fill).
		CornerRadius(styletokens.RoundingMd).
		InnerMargin(padDefault()).
		KeepIter() {
		for range c.Vertical().KeepIter() {
			titleAtoms := c.Atoms()
			for rt := range titleAtoms.StyledText(titleText) {
				rt.Strong().Bold()
			}
			c.LabelAtoms(titleAtoms.Keep()).Wrap().Send()
			for i := range s.children {
				s.children[i].render(rc)
			}
		}
	}
}

// renderRuns emits a paragraph or heading. A run sequence containing
// only a single Atoms run becomes one wrapping label (so egui's text
// shaper can do glyph-level wrapping — through the run's links too, which
// are atoms of it). A mixed sequence becomes a HorizontalWrapped flow so
// images, task boxes and footnote marks can sit inline with text.
//
// The mixed path zeroes the row's horizontal item spacing. Each run
// boundary otherwise inserts item_spacing.x IN ADDITION to whatever
// space characters the text run already carries, so a link floats in a
// double-wide gap and punctuation written flush against it detaches
// ("…even a link ." — L3 in the rendering review). With the style gap at
// zero the text's own spaces carry the word gap, which is what the
// author wrote and what the single-run path already does. The vertical
// component is zero as well: the rows a wrapped paragraph breaks into
// are spaced by the text's own line height, which carries the leading
// ([bodyLineHeight]), so a mixed paragraph's rows step like a
// single-run paragraph's lines (L7 in the review). A row holding only a
// link or an image is as tall as that widget.
//
// [c.UiSetItemSpacing] applies to the Ui it lands in and to children
// opened after it, never to siblings — so this affects one paragraph's
// row and nothing around it. It carries no widget id.
func renderRuns(runs []paragraphRun, rc *renderCtx) {
	if len(runs) == 0 {
		return
	}
	if len(runs) == 1 && runs[0].kind == runKindAtoms {
		renderAtomsRun(&runs[0], rc)
		return
	}
	for range c.HorizontalWrapped().KeepIter() {
		c.UiSetItemSpacing(0, 0)
		for i := range runs {
			r := &runs[i]
			switch r.kind {
			case runKindAtoms:
				renderAtomsRun(r, rc)
			case runKindImage:
				renderImageRun(r, rc)
			case runKindFootnote:
				renderFootnoteRun(r)
			case runKindTask:
				renderTaskRun(r, rc)
			}
		}
	}
}

// renderAtomsRun emits one text run: a plain wrapping label when it
// carries no link, else the linked label — one galley with the links as
// sections, which reports a click as the link's index. The host opens a
// link's url itself unless the caller claimed it ([Input.LinkClaims]);
// a claimed link's click is reported as a [LinkClick] instead. The
// readout is the run's, written when the frame that registered it
// completes, so the click is heard one frame on and cleared once heard.
func renderAtomsRun(r *paragraphRun, rc *renderCtx) {
	if len(r.links) == 0 {
		c.LabelAtoms(r.atoms).Wrap().Send()
		return
	}
	seq := rc.idSeq
	rc.idSeq++
	open := make([]uint8, len(r.links))
	for i := range r.links {
		if rc.linkClaims == nil || !rc.linkClaims(r.links[i].url) {
			open[i] = 1
		}
	}
	c.LabelAtomsLinked(rc.ids.PrepareSeq(seq), r.atoms, r.linkAtoms, r.linkURLs, open).
		Wrap().
		SendRespVal(&r.clicked)
	if k := r.clicked; k != noLink {
		r.clicked = noLink
		if k < uint64(len(r.links)) && open[k] == 0 {
			rc.links = append(rc.links, LinkClick{Label: r.links[k].label, URL: r.links[k].url})
		}
	}
}

// renderTaskRun emits one task box as a frameless button whose click is
// reported with the box's source offset; a box whose offset is unknown is
// the glyph alone.
func renderTaskRun(r *paragraphRun, rc *renderCtx) {
	if r.taskOffset < 0 {
		c.LabelAtoms(r.atoms).Send()
		return
	}
	seq := rc.idSeq
	rc.idSeq++
	if c.Button(rc.ids.PrepareSeq(seq), r.atoms).Frame(false).SendResp().HasPrimaryClicked() {
		rc.tasks = append(rc.tasks, TaskClick{Offset: r.taskOffset, Checked: r.taskChecked})
	}
}

// renderFootnoteRun emits one footnote reference: the superscript marker
// inside a [c.HoverText] block carrying the definition's plain text
// (ADR-0255). The block opens its own `ui.scope` and senses hover on it, so
// the marker needs no id of its own and consumes no id-sequence slot. A
// reference whose tip is empty renders bare rather than with an empty
// tooltip.
func renderFootnoteRun(r *paragraphRun) {
	if r.tip == "" {
		c.LabelAtoms(r.atoms).Send()
		return
	}
	for range c.HoverText(r.tip).KeepIter() {
		c.LabelAtoms(r.atoms).Send()
	}
}

// renderImageRun emits one image-pixel-data widget.
//
// The fit box is min(cap, native) per axis, not the cap: FitAspectMaxE
// computes s = min(fw/nw, fh/nh) with no s ≤ 1 clamp, so handing it the
// raw cap upscaled every image smaller than the box — the demo's 128×80
// assets rendered stretched to the 200×140 cap (L4 in the rendering
// review). Passing the native size where it is smaller pins s at 1. A
// zero cap axis means "no cap", which is why it resolves to native here
// rather than to egui's fill-available (that reads ~0 inside a vertical
// ScrollArea, which is where every markdown document lives, and
// collapsed the image to invisible).
//
// Pixels are sent once per widget id: later frames send the empty slice
// and the host draws its cached texture, so a frame of a document full of
// figures costs one image command per figure rather than its pixels. A
// widget the host reports starved — its texture evicted, or its upload
// lost in a region the host skipped — is sent the pixels again.
func renderImageRun(r *paragraphRun, rc *renderCtx) {
	seq := rc.idSeq
	rc.idSeq++
	pixels := r.imgPixels
	if rc.images != nil {
		pixels = rc.images.pixelsFor(rc.ids.PrepareSeq(seq).Derive(), rc.imageVersion, pixels)
	}
	c.Image(
		rc.ids.PrepareSeq(seq),
		r.imgWidthPx, r.imgHeightPx,
		rc.imageVersion,
		uint8(c.FitAspectMaxE),
		imageFitAxis(rc.imageMaxW, r.imgWidthPx),
		imageFitAxis(rc.imageMaxH, r.imgHeightPx),
		uint8(c.FilterLinearE),
		c.TintNoneRgba,
		pixels,
	).Send()
}

// imageFitAxis resolves one axis of the fit box: the smaller of the
// configured cap and the image's own size, with a zero cap meaning
// "uncapped" and therefore native.
func imageFitAxis(capPx uint32, native uint32) (box uint32) {
	if capPx == 0 || capPx > native {
		box = native
		return
	}
	box = capPx
	return
}

// renderList emits a list as a two-column egui Grid: the marker cell,
// then the item's children in a Vertical, one row per item. The grid
// measures the marker column at the widest marker and the marker cell
// is right-aligned inside it, so the periods of `9.` and `10.` sit on
// one vertical line and every body starts at the same x, with the
// numerals in the body face (L2 in the rendering review: left-aligned
// markers put the bodies a digit apart across the ten boundary). The
// body cell is the grid's last column, which egui widens to the
// available width, so a wrapped item wraps at the reading column and
// its rows stay under the first one, clear of the marker. Nested lists
// are grids inside the body cell.
//
// The list's vertical step is the inline gap, not the item gap the host
// style sets for every Ui: items of one list are lines of one block, and
// the reference stylesheets space them at a fraction of a line (0.2 em
// in ids.css) where the item gap is nearly half of one. The horizontal
// component stays the item gap, which is the whole space between a
// marker and its body — the marker carries no trailing space of its own
// ([itemMarker]), so the gap is the same for a bullet and for a number.
// The grid takes its spacing from the Ui it opens in, which is why the
// spacing is set on an enclosing Vertical; both components reach the
// items' own Verticals, so paragraphs inside one item and nested lists
// step the same way as the list around them.
//
// The grid id is the renderer's monotonic sequence, like a blockquote's
// frame; its per-frame state is the column width and the row heights.
func renderList(s *segment, rc *renderCtx) {
	d := styletokens.ActiveDensity()
	seq := rc.idSeq
	rc.idSeq++
	for range c.Vertical().KeepIter() {
		c.UiSetItemSpacing(styletokens.GapItems(d), styletokens.GapInline(d))
		// The marker column is at least the outer padding wide, so a
		// nested list's text starts about 1.5 em in from its parent's, as
		// the stylesheets indent a list; egui's default minimum is the
		// interact size, which set a nested item nearly twice as far in.
		for range c.Grid(rc.ids.PrepareSeq(seq)).NumColumns(2).MinColWidth(styletokens.PaddingOuter(d)).KeepIter() {
			for i := range s.children {
				item := &s.children[i]
				for range c.UiWithLayout().MainDirTopDown().CrossAlignMax().KeepIter() {
					renderItemMarker(s, uint32(i))
				}
				for range c.Vertical().KeepIter() {
					item.render(rc)
				}
				c.EndRow()
			}
		}
	}
}

// renderItemMarker draws the bullet or the number for list item i
// (0-based). A bullet is a plain label; a number is the retained holder
// built at lowering time ([buildListMarkers]), which carries the body's
// line height so the marker row is as tall as the body's first row. A
// missing holder falls back to a plain label rather than panicking: the
// segment tree is the renderer's own, but a nil holder is not worth a
// crash in a reading surface.
func renderItemMarker(s *segment, i uint32) {
	// A marker never wraps: the marker column is only as wide as its
	// widest marker, and a wrapping label would report its wrapped width
	// back to the grid and stay wrapped.
	if s.listOrdered && int(i) < len(s.listMarkers) {
		c.LabelAtoms(s.listMarkers[i]).Extend().Send()
		return
	}
	// A bullet carries the body's line height so it shares the item's
	// baseline; the nested glyphs come from the mono face, which carries
	// the geometric shapes the proportional face lacks.
	glyph, mono := bulletGlyph(s.listDepth)
	atoms := c.Atoms()
	for rt := range atoms.StyledText(glyph) {
		rt = rt.LineHeight(bodyLineHeight())
		if mono {
			rt = rt.Monospace()
		}
		_ = rt
	}
	c.LabelAtoms(atoms.Keep()).Extend().Send()
}

// bulletGlyph is the marker of an unordered item at the given nesting
// depth: a disc, a circle, then a square, as browsers step `ul` markers,
// and `mono` says whether the glyph is to be set in the mono face. The
// proportional face (Noto Sans on a scene launch) has no white bullet or
// small square; the mono faces do.
func bulletGlyph(depth uint8) (glyph string, mono bool) {
	switch depth {
	case 0:
		glyph = "•"
	case 1:
		glyph, mono = "◦", true
	default:
		glyph, mono = "▪", true
	}
	return
}

// itemMarker returns the bullet glyph or the number-and-period marker
// for list item index i (0-based). Neither kind carries padding or a
// trailing space: the grid in [renderList] right-aligns the markers and
// the row's item spacing is the whole gap to the body.
func itemMarker(s *segment, i uint32) (m string) {
	if !s.listOrdered {
		m = "•"
		return
	}
	m = strconv.FormatUint(uint64(s.listStart+i), 10) + "."
	return
}
