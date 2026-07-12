package widgets

import (
	"time"

	"github.com/stergiotis/boxer/public/keelson/designsystem/styletokens"
	"github.com/stergiotis/boxer/public/keelson/runtime/icons"
	c "github.com/stergiotis/boxer/public/thestack/imzero2/egui2/bindings"
	"github.com/stergiotis/boxer/public/thestack/imzero2/egui2/demo/apps/registry"
	"github.com/stergiotis/boxer/public/thestack/imzero2/egui2/widgets/canonicaltypesummary"
	"github.com/stergiotis/boxer/public/thestack/imzero2/egui2/widgets/inspector"
)

// =============================================================================
// canonicaltypesummary widget demo — two-level inspector for a leeway
// canonical type (ADR-0067).
//
// Each row renders one canonical type at level 1 (brackets icon + canonical
// string + validity dot + footprint trailer + inspector.AnchorToggle). The
// last row starts open (DefaultOpen) so the static tour capture exercises the
// level-2 window: the byte-footprint Layout strip, the bezier tether, and the
// provenance chip. The remaining rows open on click.
// =============================================================================

type ctSumDemoRow struct {
	name      string
	canonical string
	subject   string
	// idPrefix scopes the per-row Renderer so each row owns its toggle /
	// window / tether identities and pinned-state slot.
	idPrefix string
	// open starts this row's inspector pinned so the tour shows level-2.
	open bool
}

var ctSumDemoRows = []ctSumDemoRow{
	{name: "little-endian u32", canonical: "u32l", subject: "leeway.type.col.count", idPrefix: "cts-demo-0"},
	{name: "IEEE double", canonical: "f64", subject: "leeway.type.col.score", idPrefix: "cts-demo-1"},
	{name: "fixed utf8 (128b)", canonical: "sx128", subject: "leeway.type.col.code", idPrefix: "cts-demo-2"},
	{name: "variable utf8", canonical: "s", subject: "leeway.type.col.name", idPrefix: "cts-demo-3"},
	{name: "ipv4 + CIDR", canonical: "vc", subject: "leeway.type.col.cidr", idPrefix: "cts-demo-4"},
	{name: "ipv6 CIDR set", canonical: "wcm", subject: "leeway.type.col.subnets", idPrefix: "cts-demo-5"},
	{name: "signature u32-s_vc", canonical: "u32-s_vc", subject: "leeway.type.row.key", idPrefix: "cts-demo-6", open: true},
}

func init() {
	registry.Register(registry.Demo{
		Name:     "canonicaltypesummary",
		Category: "Leeway",
		Title:    icons.PhBracketsAngle + " canonicaltypesummary",
		Stage:    [2]float32{1200, 760},
		Flags:    registry.DemoFlagNeedsLargeArea,
		Kind:     registry.DemoKindMixed,
		Description: "Two-level inspector for a single leeway canonical type " +
			"(ADR-0067). Level 1 is a compact monospace row — brackets glyph, the " +
			"canonical string, a green/red validity dot, an 'N fields · K B' " +
			"footprint trailer — paired with the standard inspector.AnchorToggle " +
			"(ADR-0046). Click the accent arrow-square-out glyph on any row to open " +
			"its inspector window (the last row starts open); a bezier connector " +
			"tethers the toggle to the open window. The window body is a three-tab " +
			"surface sized to styletokens.SurfaceInspector: Layout (a byte-footprint " +
			"strip), Members (a decomposed table over IterateMembers), and Go codec " +
			"(GenerateGoCode rendered through the codeview highlighter), plus the " +
			"optional inspector.ProvenanceChip. Read-only — editing is " +
			"canonicaltypeedit's job.",
		Render:     demoCanonicalTypeSummary,
		SourceFunc: demoCanonicalTypeSummary,
	})
}

func demoCanonicalTypeSummary(ids *c.WidgetIdStack) {
	density := styletokens.DensityFromEnv()
	c.Label("Each row summarises one canonical type; click a row's arrow-square-out glyph to open its inspector (the last row starts open):").Send()
	c.Separator().Horizontal().Send()
	c.AddSpace(styletokens.GapInline(density))

	now := time.Now()
	for i, row := range ctSumDemoRows {
		// Per-row Renderer with a distinct idPrefix so each row owns its own
		// toggle / window / tether identities — the widget derives those ids
		// from the idPrefix-plus-callId scope.
		r := canonicaltypesummary.New(row.idPrefix).
			Provenance(inspector.Provenance{
				Subject:   row.subject,
				SampledAt: now,
			})
		if row.open {
			r = r.DefaultOpen(true)
		}
		for range c.Horizontal().KeepIter() {
			// Fixed-width label column keeps every summary cell at the same x.
			c.UiSetMinWidth(180)
			c.Label(row.name).Send()
			c.AddSpace(styletokens.GapItems(density))
			r.Render(ids.PrepareSeq(uint64(0xC7501000+i)), row.canonical)
		}
		c.AddSpace(styletokens.GapInline(density))
	}

	c.Separator().Horizontal().Send()
	c.AddSpace(styletokens.GapInline(density))
	c.LabelAtoms(
		c.Atoms().BeginRichText("Tip: the inspector reads any primitive or flat group; the footprint strip is type-level, not a byte-exact runtime encoding for non-network types.").
			Small().Weak().End().Keep(),
	).Send()
}
