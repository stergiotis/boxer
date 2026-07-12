package schemaview

import (
	"github.com/stergiotis/boxer/public/keelson/designsystem/styletokens"
	"github.com/stergiotis/boxer/public/keelson/runtime/icons"
	c "github.com/stergiotis/boxer/public/thestack/imzero2/egui2/bindings"
	"github.com/stergiotis/boxer/public/thestack/imzero2/egui2/widgets/badge"
	"github.com/stergiotis/boxer/public/thestack/imzero2/egui2/widgets/color"
	"github.com/stergiotis/boxer/public/thestack/imzero2/egui2/widgets/inspector"
)

// The navigator borrows TopologySpark's glyph vocabulary (rebound from data to
// schema). The legend popup is the in-widget key for it, so the same vocabulary
// reads without consulting the demo description or doc.go. Keep these entries in
// step with card.TopologySpark's legend and renderSections' glyph choices.
type legendEntry struct {
	glyph   string
	tone    badge.ToneE
	meaning string
}

var legendEntries = []legendEntry{
	{"◆", badge.ToneInfo, "plain item-type section"},
	{"◇", badge.TonePrimary, "tagged section"},
	{"◈", badge.TonePrimary, "co-section group"},
	{"·", badge.ToneNeutral, "column / property separator"},
	{"ˡ", badge.ToneNeutral, "low-card membership spec"},
	{"ʰ", badge.ToneNeutral, "high-card membership spec"},
	{"ᵐ", badge.ToneNeutral, "mixed-card membership spec"},
	{"·∅", badge.ToneNeutral, "value-less (membership-only) section"},
}

// legendScope derives the tether / window / toggle id scope from the host's
// ScopeKey so two inspector instances in one app keep independent legend
// windows.
func legendScope(scopeKey string) string {
	return scopeKey + "-legend"
}

// renderLegendToggle draws the "?" affordance that pins the glyph-legend window,
// then captures its rect so the bezier tether anchors at the toggle's right
// edge. Call inside the navigator's header Horizontal, after the title. Mirrors
// inspector.AnchorToggle's click-capture grammar but carries a help glyph
// (PhQuestion) rather than the pop-out arrow, since this opens a key, not a
// value inspector.
func renderLegendToggle(m *Model, scope string) {
	accent := color.Hex(styletokens.AccentDefault.AsHex())
	transparent := color.Transparent
	fill := transparent
	if m.legendOpen {
		fill = color.Hex(styletokens.AccentSubtle.AsHex())
	}
	atoms := c.Atoms().BeginRichTextColored(accent, transparent, icons.PhQuestion).End().Keep()

	toggleId := c.MakeAbsoluteIdStr(scope + "-toggle")
	f := c.Frame(toggleId).
		Fill(fill).
		CornerRadius(styletokens.RoundingSm).
		InnerMarginSides(4, 4, 1, 1).
		SenseClick().
		HoverCursorPointer()
	frameId := f.Id()
	tip := "show glyph legend"
	if m.legendOpen {
		tip = "hide glyph legend"
	}
	for range c.HoverText(tip).KeepIter() {
		for range f.KeepIter() {
			c.LabelAtoms(atoms).Send()
		}
	}
	if c.CurrentApplicationState.StateManager.GetResponseByIdRaw(frameId).HasPrimaryClicked() {
		m.legendOpen = !m.legendOpen
	}

	inspector.NewAnchorTether(scope).CaptureToggle()
}

// renderLegendWindow draws the tethered glyph-legend window when pinned. It is
// rendered OUTSIDE the dock area (after the DockArea block): there is no
// precedent for spawning a floating window from inside a dock tab body, and the
// tether links toggle ↔ window purely by scope, so the two need not be nested.
// The native title-bar X is wired back to m.legendOpen via OpenBound + an R10
// databinding (the canonicaltypesummary / distsummary pattern).
func renderLegendWindow(ids *c.WidgetIdStack, m *Model, scope string) {
	if !m.legendOpen {
		return
	}
	tether := inspector.NewAnchorTether(scope)
	winId := c.MakeAbsoluteIdStr(scope + "-window")
	win := c.Window(winId, c.WidgetText().Text("glyph legend").Keep()).
		DefaultOpen(true).
		Resizable(true).
		Collapsible(false).
		AlwaysOnTop(true).
		DefaultSize(320, 280)
	bindId := win.Id()
	win = win.OpenBound(bindId)
	c.CurrentApplicationState.StateManager.AddR10Databinding(bindId, &m.legendOpen)
	for range win.KeepIter() {
		tether.CaptureWindow()
		renderLegendBody(ids)
	}
	tether.Paint()
}

// renderLegendBody lays the glyphs out as a two-column grid: a toned chip
// carrying the glyph, then its meaning. The chip tone echoes the detail-pane
// category accent (◆ info, ◇/◈ accent, annotations neutral) so glyph colour
// reads the same wherever it appears.
func renderLegendBody(ids *c.WidgetIdStack) {
	for rt := range c.RichTextLabel("navigator glyphs") {
		rt.Weak().Small()
	}
	c.AddSpace(styletokens.PaddingInner(styletokens.DensityFromEnv()))
	for range c.Grid(ids.PrepareStr("legend-grid")).NumColumns(2).KeepIter() {
		for i, e := range legendEntries {
			badge.New(ids.PrepareSeq(uint64(0x1e6e_0000+i)), e.glyph).
				Tone(e.tone).
				Variant(badge.VariantSoft).
				Size(badge.SizeSm).
				Monospace().
				Send()
			for rt := range c.RichTextLabel(e.meaning) {
				rt.Weak()
			}
			c.EndRow()
		}
	}
}
