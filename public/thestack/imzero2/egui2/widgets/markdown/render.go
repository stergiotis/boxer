package markdown

import (
	"strconv"
	"strings"

	"github.com/stergiotis/boxer/public/keelson/designsystem/styletokens"
	c "github.com/stergiotis/boxer/public/thestack/imzero2/egui2/bindings"
)

// padDefault resolves the IDS Padding.Default token at the active
// density (ADR-0032 §SD2). markdown rendering is stateless — there's
// no Renderer struct to cache the density on, so each call site reads
// the env afresh (cheap; ~once per Frame call site).
func padDefault() (v float32) {
	v = styletokens.PaddingDefault(styletokens.DensityFromEnv())
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
	d := styletokens.DensityFromEnv()
	if level <= 2 {
		v = styletokens.PaddingOuter(d)
		return
	}
	v = styletokens.PaddingDefault(d)
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
				c.ScrollToCursor(0)
				// Clear so a slug appearing twice in one doc only
				// triggers on the first occurrence — matches the
				// "scroll once per click" UX consumers expect.
				rc.scrollToSlug = ""
			}
		}
		rc.headingIdx++
		renderRuns(inst.runs, rc)
	case segKindParagraph:
		renderRuns(inst.runs, rc)
	case segKindCodeBlock:
		// Code-block action button ([Doc.RenderActions]). Only when the
		// caller enabled actions; otherwise the block is untouched. The
		// small IDS button sits on its own line above the code; a click
		// records a CodeBlockAction the caller consumes from the returned
		// iter.Seq. The CodeView's own selectable text (Ctrl+C) is
		// independent of this. codeBlockIdx advances per code block so the
		// action's ordinal is stable regardless of whether buttons render.
		if rc.actionsEnabled &&
			(rc.actionAccept == nil || rc.actionAccept(inst.codeText, inst.codeLang)) {
			renderCodeActionButtons(rc, inst.codeText, inst.codeLang, rc.codeBlockIdx)
		}
		rc.codeBlockIdx++
		seq := rc.idSeq
		rc.idSeq++
		c.CodeView(rc.ids.PrepareSeq(seq), inst.code).Send()
	case segKindList:
		renderList(inst, rc)
	case segKindListItem:
		for i := range inst.children {
			inst.children[i].render(rc)
		}
	case segKindBlockquote:
		seq := rc.idSeq
		rc.idSeq++
		for range c.Frame(rc.ids.PrepareSeq(seq)).PresetGroup().KeepIter() {
			for range c.Vertical().KeepIter() {
				for i := range inst.children {
					inst.children[i].render(rc)
				}
			}
		}
	case segKindHorizontalRule:
		c.Separator().Send()
	case segKindCallout:
		renderCallout(inst, rc)
	case segKindTable:
		renderTable(inst, rc)
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
	d := styletokens.DensityFromEnv()
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
	pt := styletokens.ScaledPt(styletokens.BodyPt, styletokens.DensityFromEnv())
	w = float32(runes)*pt*tableGlyphAdvance + tableCellPadding
	if w < tableColumnMinWidth {
		w = tableColumnMinWidth
	} else if w > colCap {
		w = colCap
	}
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

	// Every column but the last starts at roughly its content width and
	// can be dragged; the last takes whatever width is left so the table
	// fills the pane instead of ending in a ragged edge. All of them
	// clip, which egui renders as an ellipsis where it can.
	colCap := tableColumnCap(cols)
	for i := 0; i < cols-1; i++ {
		var runes uint32
		if i < len(s.tableColRunes) {
			runes = s.tableColRunes[i]
		}
		c.TableColumn().Initial(tableColumnWidth(runes, colCap)).
			AtLeast(tableColumnMinWidth).
			Resizable(true).ClipContents(true).Send()
	}
	c.TableColumn().Remainder().ClipContents(true).Send()

	// Zero header pushes render a headerless body — that is the op's
	// documented behaviour, and what a table whose header row lowered to
	// nothing should look like.
	for _, h := range s.tableHeader {
		c.TableHeaderText(h).Send()
	}
	for _, cell := range s.tableCells {
		c.TableCellText(cell).Send()
	}

	seq := rc.idSeq
	rc.idSeq++
	hasHeader := len(s.tableHeader) > 0
	tbl := c.Table(rc.ids.PrepareSeq(seq), tableRowHeight(hasHeader), uint64(rows)).
		Striped(true)
	if flows, maxScroll := tableFlows(rows, hasHeader); flows {
		tbl = tbl.Vscroll(false)
	} else {
		tbl = tbl.MaxScrollHeight(maxScroll)
	}
	tbl.Send()
}

// renderCodeActionButtons emits a horizontal row of small IDS buttons above
// a code block, one per rc.actionLabels entry — used only on the
// [Doc.RenderActions]/[Doc.RenderActionsN] path. Each button consumes one
// id-sequence slot (so layout state keyed by id stays stable across frames)
// and, on click, records a [CodeBlockAction] carrying the block's verbatim
// text, fence language, and ordinal, plus the 0-based index of the clicked
// button — the caller consumes these from the returned iter.Seq and decides
// what each click means. text/lang/idx are passed rather than read off rc so
// the call site stays adjacent to the CodeView it labels.
func renderCodeActionButtons(rc *renderCtx, text, lang string, idx int) {
	for range c.Horizontal().KeepIter() {
		for btn, label := range rc.actionLabels {
			seq := rc.idSeq
			rc.idSeq++
			if c.Button(rc.ids.PrepareSeq(seq), c.Atoms().Text(label).Keep()).
				Small().SendResp().HasPrimaryClicked() {
				rc.codeActions = append(rc.codeActions,
					CodeBlockAction{Text: text, Lang: lang, Index: idx, Button: btn})
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
				rt.Strong()
			}
			c.LabelAtoms(titleAtoms.Keep()).Wrap().Send()
			for i := range s.children {
				s.children[i].render(rc)
			}
		}
	}
}

// renderRuns emits a paragraph or heading. A run sequence containing
// only a single Atoms run becomes one wrapping LabelAtoms (so egui's
// text shaper can do glyph-level wrapping). A mixed sequence becomes a
// HorizontalWrapped flow so links and images can sit inline with text.
//
// The mixed path zeroes the row's horizontal item spacing. Each run
// boundary otherwise inserts item_spacing.x IN ADDITION to whatever
// space characters the text run already carries, so a link floats in a
// double-wide gap and punctuation written flush against it detaches
// ("…even a link ." — L3 in the rendering review). With the style gap at
// zero the text's own spaces carry the word gap, which is what the
// author wrote and what the single-run path already does. The vertical
// component keeps a real gap: it is what separates the rows a wrapped
// paragraph breaks into.
//
// [c.UiSetItemSpacing] applies to the Ui it lands in and to children
// opened after it, never to siblings — so this affects one paragraph's
// row and nothing around it. It carries no widget id.
func renderRuns(runs []paragraphRun, rc *renderCtx) {
	if len(runs) == 0 {
		return
	}
	if len(runs) == 1 && runs[0].kind == runKindAtoms {
		c.LabelAtoms(runs[0].atoms).Wrap().Send()
		return
	}
	for range c.HorizontalWrapped().KeepIter() {
		c.UiSetItemSpacing(0, styletokens.GapItems(styletokens.DensityFromEnv()))
		for i := range runs {
			r := &runs[i]
			switch r.kind {
			case runKindAtoms:
				c.LabelAtoms(r.atoms).Wrap().Send()
			case runKindLink:
				renderLinkRun(r, rc)
			case runKindImage:
				renderImageRun(r, rc)
			}
		}
	}
}

// renderLinkRun emits one link: a browser hyperlink by default, or — when
// the host claimed it via [WithLinkRouter] — a frameless button that reads
// as a link and reports its click instead of navigating away.
//
// A frameless Button rather than a Hyperlink because the two things a link
// needs here are a click the host hears and text it does not own: Hyperlink
// consumes the click itself (it opens a URL), and Label has no click sense at
// all. Frame(false) plus the link tone leaves it looking like the hyperlink
// beside it, which matters — a reader should not have to learn which links
// stay in the app.
func renderLinkRun(r *paragraphRun, rc *renderCtx) {
	if rc.linkClaims == nil || rc.linkClicked == nil || !rc.linkClaims(r.url) {
		c.HyperlinkTo(r.label, r.url).OpenInNewTab(true).Send()
		return
	}
	seq := rc.idSeq
	rc.idSeq++
	// Id prepared before the atoms, both inside the one call expression —
	// the form every other call site in the tree uses.
	if c.Button(rc.ids.PrepareSeq(seq),
		c.Atoms().BeginRichTextColored(linkFg, linkBg, r.label).End().Keep()).
		Frame(false).
		SendResp().HasPrimaryClicked() {
		rc.linkClicked(r.label, r.url)
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
// Pixels are re-sent every frame and contentVersion is pinned at 1. The
// Rust ImageCache keys on (id, version, w, h) and skips the GPU upload
// when they match, so this costs wire bandwidth — one memcpy each way
// per frame — and NOT a per-frame texture upload. Fine for icons and
// diagrams; the wrong cost model for a book of full-page screenshots,
// which is when the tracker becomes worth its complexity. Skipping it
// today follows the bindings doc-comment at [c.ImageVersionTracker]
// ("the per-widget-id one-shot upload cost is negligible") and keeps the
// package-level retain-once / render-many Doc usable under multiple id
// scopes — keyed-by-seq trackers silently drop pixels on the second
// scope.
func renderImageRun(r *paragraphRun, rc *renderCtx) {
	seq := rc.idSeq
	rc.idSeq++
	c.Image(
		rc.ids.PrepareSeq(seq),
		r.imgWidthPx, r.imgHeightPx,
		1, // contentVersion: pinned; pixels ride every frame.
		uint8(c.FitAspectMaxE),
		imageFitAxis(rc.imageMaxW, r.imgWidthPx),
		imageFitAxis(rc.imageMaxH, r.imgHeightPx),
		uint8(c.FilterLinearE),
		c.TintNoneRgba,
		r.imgPixels,
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

// renderList emits a Vertical of list items. Each item is a
// Horizontal{ marker , Vertical{ children } } so multi-line item
// content stays aligned to the marker's right edge.
func renderList(s *segment, rc *renderCtx) {
	for range c.Vertical().KeepIter() {
		for i := range s.children {
			item := &s.children[i]
			for range c.Horizontal().KeepIter() {
				renderItemMarker(s, uint32(i))
				for range c.Vertical().KeepIter() {
					item.render(rc)
				}
			}
		}
	}
}

// renderItemMarker draws the bullet or the number for list item i
// (0-based).
//
// A bullet is a plain label. A number is a MONOSPACE label, padded on the
// left to the list's widest marker: proportional digits are not
// equal-width, so `9.` and `10.` put their item bodies at different x
// (measured at 7.4 pt apart — L2 in the rendering review) and a numbered
// list crossing ten items develops a visible step. Monospace plus
// padding lines the periods up exactly at every digit width.
//
// The trade is deliberate: the numerals sit in a different face from the
// prose beside them. Exact alignment reads better than matched faces in
// a column of items, and the marker is chrome rather than content.
//
// The numbered markers are retained holders built once at lowering time,
// not Atoms rebuilt per frame — monospace needs the RichText scope, and
// interning a fresh blob per item per frame would put an allocation back
// into the steady state the package is built to keep empty. A missing
// holder falls back to a plain label rather than panicking: the segment
// tree is the renderer's own, but a nil holder is not worth a crash in a
// reading surface.
func renderItemMarker(s *segment, i uint32) {
	if !s.listOrdered || int(i) >= len(s.listMarkers) {
		c.Label(itemMarker(s, i)).Send()
		return
	}
	c.LabelAtoms(s.listMarkers[i]).Send()
}

// itemMarker returns the bullet glyph or a numbered marker for list
// item index i (0-based). Ordered markers are left-padded with spaces to
// s.listMarkerDigits, the digit count of the list's LAST number, settled
// once at lowering time.
func itemMarker(s *segment, i uint32) (m string) {
	if !s.listOrdered {
		m = "• "
		return
	}
	n := strconv.FormatUint(uint64(s.listStart+i), 10)
	if pad := int(s.listMarkerDigits) - len(n); pad > 0 {
		m = strings.Repeat(" ", pad)
	}
	m += n + ". "
	return
}
