//go:build llm_generated_opus47

package markdown

import (
	"strconv"

	"github.com/stergiotis/boxer/public/keelson/designsystem/styletokens"
	"github.com/stergiotis/boxer/public/keelson/runtime/icons"
	c "github.com/stergiotis/boxer/public/thestack/imzero2/egui2/bindings"
	"github.com/stergiotis/boxer/public/thestack/imzero2/egui2/widgets/color"
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
		seq := rc.idSeq
		rc.idSeq++
		// Copy-to-clipboard affordance (ADR-0026 Update 2026-05-30). Only
		// when a sink is wired ([WithClipboard]); otherwise the block is
		// untouched. The frameless small icon button sits on its own line
		// above the code; clicking hands inst.code to the sink, which
		// routes it through the clipboard.write capability. The CodeView's
		// own selectable text (Ctrl+C) is independent of this.
		if rc.clipboardWrite != nil {
			renderCopyButton(rc, inst.codeText)
		}
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
	}
}

// renderCopyButton emits a frameless, icon-only copy button for a code
// block, used only when [WithClipboard] wired rc.clipboardWrite. It
// consumes one id-sequence slot (so collapse/scroll state keyed by id
// stays stable across frames) and, on click, hands code to the sink —
// the caller's entry into the clipboard.write capability (ADR-0026
// Update 2026-05-30). The accent-tinted Phosphor copy glyph matches the
// icon-button styling used elsewhere (e.g. inspector/anchor); HoverText
// names the action since the button carries no label.
func renderCopyButton(rc *renderCtx, code string) {
	seq := rc.idSeq
	rc.idSeq++
	accent := color.Hex(styletokens.AccentDefault.AsHex())
	atoms := c.Atoms().
		BeginRichTextColored(accent, color.Transparent, icons.PhCopy).
		End().Keep()
	for range c.HoverText("copy to clipboard").KeepIter() {
		if c.Button(rc.ids.PrepareSeq(seq), atoms).Small().Frame(false).SendResp().HasPrimaryClicked() {
			rc.clipboardWrite(code)
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
				c.HyperlinkTo(r.label, r.url).OpenInNewTab(true).Send()
			case runKindImage:
				renderImageRun(r, rc)
			}
		}
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
