package markdown

import (
	"strconv"

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

// render emits a single segment into the current egui Ui scope.
//
// rc threads the monotonic per-Render-invocation id sequence counter,
// the Doc-scoped image tracker, and the configured image fit cap. Each
// id-needing widget bumps rc.idSeq, so layout state (collapse, scroll,
// drag) keyed by id stays stable across frames as long as the lowering
// rules produce the same segment sequence.
func (inst *segment) render(rc *renderCtx) {
	switch inst.kind {
	case segKindHeading:
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
		if rc.actionsEnabled {
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
	c.Table(rc.ids.PrepareSeq(seq), tableRowHeight(len(s.tableHeader) > 0), uint64(rows)).
		Striped(true).
		Send()
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
func renderRuns(runs []paragraphRun, rc *renderCtx) {
	if len(runs) == 0 {
		return
	}
	if len(runs) == 1 && runs[0].kind == runKindAtoms {
		c.LabelAtoms(runs[0].atoms).Wrap().Send()
		return
	}
	for range c.HorizontalWrapped().KeepIter() {
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
	atoms := c.Atoms()
	for rt := range atoms.StyledTextColored(linkFg, linkBg, r.label) {
		_ = rt
	}
	if c.Button(rc.ids.PrepareSeq(seq), atoms.Keep()).
		Frame(false).
		SendResp().HasPrimaryClicked() {
		rc.linkClicked(r.label, r.url)
	}
}

// renderImageRun emits one image-pixel-data widget. Pixels are re-sent
// every frame and contentVersion is pinned at 1: per the wire contract
// in egui2_definition_d_image.go, a non-empty pixel buffer triggers a
// Rust-side re-upload regardless of version, and per the bindings
// doc-comment at [c.ImageVersionTracker] skipping the tracker is the
// recommended pattern for static assets ("the per-widget-id one-shot
// upload cost is negligible"). Avoiding the tracker also keeps the
// package-level retain-once / render-many Doc usable under multiple
// id scopes — keyed-by-seq trackers silently drop pixels on the
// second scope.
func renderImageRun(r *paragraphRun, rc *renderCtx) {
	seq := rc.idSeq
	rc.idSeq++
	c.Image(
		rc.ids.PrepareSeq(seq),
		r.imgWidthPx, r.imgHeightPx,
		1, // contentVersion: any value works (non-empty pixels re-upload).
		uint8(c.FitAspectMaxE),
		rc.imageMaxW, rc.imageMaxH,
		uint8(c.FilterLinearE),
		c.TintNoneRgba,
		r.imgPixels,
	).Send()
}

// renderList emits a Vertical of list items. Each item is a
// Horizontal{ glyph-Label , Vertical{ children } } so multi-line item
// content stays aligned to the glyph's right edge.
func renderList(s *segment, rc *renderCtx) {
	for range c.Vertical().KeepIter() {
		for i := range s.children {
			item := &s.children[i]
			for range c.Horizontal().KeepIter() {
				c.Label(itemMarker(s, uint32(i))).Send()
				for range c.Vertical().KeepIter() {
					item.render(rc)
				}
			}
		}
	}
}

// itemMarker returns the bullet glyph or a numbered marker for list
// item index i (0-based).
func itemMarker(s *segment, i uint32) (m string) {
	if !s.listOrdered {
		m = "• "
		return
	}
	m = strconv.FormatUint(uint64(s.listStart+i), 10) + ". "
	return
}
