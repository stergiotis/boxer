package widgets

import (
	"time"

	runtimeapp "github.com/stergiotis/boxer/public/keelson/runtime/app"
	"github.com/stergiotis/boxer/public/keelson/runtime/icons"
	c "github.com/stergiotis/boxer/public/thestack/imzero2/egui2/bindings"
	"github.com/stergiotis/boxer/public/thestack/imzero2/egui2/demo/apps/registry"
	"github.com/stergiotis/boxer/public/thestack/imzero2/egui2/widgets/inspector"
	"github.com/stergiotis/boxer/public/thestack/imzero2/egui2/widgets/regexsummary"
)

// =============================================================================
// regexsummary widget demo — two-level summary of a regex value
//
// Four rows demonstrate the level-1 inline summary (icon + truncated
// pattern + compile-status dot) paired with the standard
// inspector.AnchorToggle (ADR-0046). Clicking the toggle on any row
// opens its inspector window — a draggable c.Window hosting the full
// regex_explorer body (cheatsheet, pattern/haystack/multi inputs,
// Test/List/Replace tabs, status bar). The bezier tether mirrors
// distsummary's visual language so the link between source row and
// floating inspector reads at a glance.
//
// Bus wiring: the gallery host hands a BusI in via BusInit; the demo
// forwards it to every Renderer so the embedded explorer's CH-backed
// tabs work end-to-end (extractAll, replaceRegexpAll, multiMatchAllIndices
// route through ch.local.exec.regex_explorer the same way the standalone
// app does).
// =============================================================================

// regexsumDemoRow seeds one row's pattern, label, and inspector
// metadata, and holds the row's summary State — each row is its own
// widget instance, drawn under its own id scope.
type regexsumDemoRow struct {
	name    string
	pattern string
	state   regexsummary.State
	// provenance, when non-zero, surfaces as the inspector.ProvenanceChip
	// at the top of the inspector window. Only one row in the demo
	// carries one so the chip's optional-by-default behaviour is
	// visible in the carousel.
	provenance inspector.Provenance
}

// regexsumDemoState is the per-app-instance state for the demo. The
// gallery host's BusInit captures the BusI from MountContextI and
// stores it here so the per-frame Render hook can pass it on every
// Input.
type regexsumDemoState struct {
	bus  runtimeapp.BusI
	rows []*regexsumDemoRow
}

func init() {
	registry.Register(registry.Demo{
		Name:     "regexsummary",
		Category: "Inspectors",
		Title:    icons.PhMagnifyingGlass + " regexsummary",
		// Stage is intentionally compact: the level-1 row is one line
		// per pattern, so 720×420 fits four rows + a tip without the
		// inspector windows (which open over the top of the tile when
		// the user clicks the toggle). The inspector itself opens at
		// its default 1100×720 envelope and floats above the tile.
		Stage: [2]float32{720, 420},
		Kind:  registry.DemoKindUX,
		Description: "Two-level summarisation of a regex value. Level 1 is a " +
			"compact icon + truncated pattern + compile-status dot row paired " +
			"with the standard inspector.AnchorToggle. Click the accent-coloured " +
			"arrow-square-out glyph on any row to open its inspector window — a " +
			"bezier connector tethers the toggle to the open window, which hosts " +
			"the full regex_explorer body. Composes regex_explorer + inspector.",
		BusInit: func(_ *c.WidgetIdStack, bus runtimeapp.BusI) (state any) {
			now := time.Now()
			st := &regexsumDemoState{
				bus: bus,
				rows: []*regexsumDemoRow{
					{
						name:    "simple word",
						pattern: `\w+`,
					},
					{
						name:    "invalid (open group)",
						pattern: `(unclosed`,
					},
					{
						name:    "long — truncated",
						pattern: `^([a-zA-Z0-9._%+-]+)@([a-zA-Z0-9.-]+)\.([a-zA-Z]{2,})$`,
					},
					{
						name:    "with provenance",
						pattern: `\d{4}-\d{2}-\d{2}T\d{2}:\d{2}:\d{2}(?:\.\d+)?(?:Z|[+-]\d{2}:?\d{2})`,
						provenance: inspector.Provenance{
							Subject:   "app.spinnaker.event.rules.iso8601",
							SourceApp: "spinnaker",
							SampledAt: now,
						},
					},
				},
			}
			state = st
			return
		},
		RenderStateful: func(ids *c.WidgetIdStack, state any) {
			demoRegexsummary(ids, state.(*regexsumDemoState))
		},
		SourceFunc: demoRegexsummary,
	})
}

func demoRegexsummary(ids *c.WidgetIdStack, st *regexsumDemoState) {
	c.Label("Click any row's arrow-square-out glyph to open its regex inspector:").Send()
	c.Separator().Horizontal().Send()
	c.AddSpace(padInner())

	for i, row := range st.rows {
		// Each row is its own summary under a per-row id scope, so the
		// rows' toggles, windows and tethers stay apart. Bus is passed
		// every frame from the BusInit-captured handle — the embedded
		// explorer re-reads it on each open so CH-backed tabs work.
		for range c.IdScope(ids.PrepareSeq(uint64(i))) {
			for range c.Horizontal().KeepIter() {
				// Fixed-width label column keeps every regexsummary cell
				// at the same x — visually aligned across rows.
				c.UiSetMinWidth(180)
				c.Label(row.name).Send()
				c.AddSpace(gapSections())
				regexsummary.Render(regexsummary.Input{
					Ids:        ids,
					ScopeKey:   "row",
					Pattern:    row.pattern,
					State:      &row.state,
					Bus:        st.bus,
					Provenance: row.provenance,
					Title:      "regex: " + row.name,
				})
			}
		}
		c.AddSpace(padInner())
	}

	c.Separator().Horizontal().Send()
	c.AddSpace(padInner())
	c.LabelAtoms(
		c.Atoms().BeginRichText("Tip: the level-1 row mirrors what you'd see beside any " +
			"regex-valued field in a future field inspector. Green dot = compiles " +
			"under Go's regexp; red = compile error. The inspector embeds the full " +
			"regex_explorer — pattern edits inside stay local (bidirectional " +
			"propagation lands when bidirectional inspectors do).").
			Small().Weak().End().Keep(),
	).Send()
}
