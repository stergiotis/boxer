package widgets

import (
	"github.com/stergiotis/boxer/public/keelson/designsystem/styletokens"
	"github.com/stergiotis/boxer/public/keelson/runtime/icons"
	c "github.com/stergiotis/boxer/public/thestack/imzero2/egui2/bindings"
	"github.com/stergiotis/boxer/public/thestack/imzero2/egui2/demo/apps/registry"
	"github.com/stergiotis/boxer/public/thestack/imzero2/egui2/widgets/color"
)

// ADR-0057 demo registrations. Each Render closure draws into the current
// Ui scope with no outer Window chrome — the consuming driver supplies it
// (Window for InteractiveDriver, stage rect for TestDriver, nothing for
// Embed). Closures still on this legacy path capture the package-level
// [ids] stack from egui2_hl_demo.go; migrated demos own their own files
// with the per-window state struct + Init + RenderStateful pattern.

func init() {
	registry.Register(registry.Demo{
		Name: "mappingplanview", Category: "Leeway", Title: icons.IconDatabase + " mappingplan playground",
		Stage:       [2]float32{1200, 980},
		Flags:       registry.DemoFlagNeedsLargeArea,
		Kind:        registry.DemoKindMixed,
		Description: "Author a leeway mappingplan spec — kind, plain columns, lw:-tagged value/const fields with membership, section, channel and flags, and dynamic-membership tuple fields (ADR-0103: a slice-of-struct maps N attributes into one section, each element carrying its own verbatim membership) — and live-preview what it compiles to: the schema-agnostic Go codec, the Plan IR, and the dql SQL read-back. Each field carries its own validity state machine (empty / incomplete / valid / rejected / conflict / blocked) as a tethered inspector chip — click it for the state graph, transition history, and rejection reason. Carrier channels are planned next.",
		Init: func(_ *c.WidgetIdStack) (state any) {
			state = newMappingPlanViewState()
			return
		},
		RenderStateful: func(ids *c.WidgetIdStack, state any) {
			st := state.(*mappingPlanViewDemoState)
			demoMappingPlanView(ids, st)
		},
		SourceFunc: demoMappingPlanView,
	})
	registry.Register(registry.Demo{
		Name: "etables", Category: "Tables", Title: icons.IconTable + " etables (deferred)",
		Stage:       [2]float32{1024, 700},
		Kind:        registry.DemoKindUX,
		Description: "Seven deferred-block etable variants — virtual scrolling, rich headers, interactive cells, variable row heights, plus 10k-row dense + sparse stress tests. Cells are captured as deferred opcode blocks and replayed on demand inside egui_table's TableDelegate.",
		Init: func(_ *c.WidgetIdStack) (state any) {
			state = &etablesDemoState{interactive: newEtableInteractiveState()}
			return
		},
		RenderStateful: func(ids *c.WidgetIdStack, state any) {
			st := state.(*etablesDemoState)
			for range c.CollapsingHeader(ids.PrepareStr("etable"), c.WidgetText().Text("etable (deferred)").Keep()).DefaultOpen(true).KeepIter() {
				demoETable(ids)
			}
			for range c.CollapsingHeader(ids.PrepareStr("etable-rich"), c.WidgetText().Text("etable (rich headers)").Keep()).KeepIter() {
				demoETableRichHeaders(ids)
			}
			for range c.CollapsingHeader(ids.PrepareStr("etable-interactive"), c.WidgetText().Text("etable (interactive)").Keep()).KeepIter() {
				demoETableInteractive(ids, st.interactive)
			}
			for range c.CollapsingHeader(ids.PrepareStr("etable-varheight"), c.WidgetText().Text("etable (variable heights)").Keep()).KeepIter() {
				demoETableVariableHeights(ids)
			}
			for range c.CollapsingHeader(ids.PrepareStr("etable-large"), c.WidgetText().Text("etable (large, deferred)").Keep()).KeepIter() {
				demoETableLarge(ids)
			}
			for range c.CollapsingHeader(ids.PrepareStr("etable-dense-10k"), c.WidgetText().Text("etable (dense, 10k × 5)").Keep()).KeepIter() {
				demoETableDense10k(ids)
			}
			for range c.CollapsingHeader(ids.PrepareStr("etable-sparse-10k"), c.WidgetText().Text("etable (sparse, 10k × 30% fill)").Keep()).KeepIter() {
				demoETableSparse10k(ids)
			}
		},
	})
	registry.Register(registry.Demo{
		Name: "tables", Category: "Tables", Title: icons.IconTable + " tables (register-drain)",
		Stage:       [2]float32{1024, 700},
		Kind:        registry.DemoKindUX,
		Description: "Five classic register-drain Table variants — mixed/auto column sizing, headless layout, large (10k rows), rich text and simple plain-text cells. Cell data is pre-collected via tableCellText / tableCellRichText registers and drained in one shot at the Table node.",
		Render: func(ids *c.WidgetIdStack) {
			for range c.CollapsingHeader(ids.PrepareStr("simple"), c.WidgetText().Text("table (simple)").Keep()).DefaultOpen(true).KeepIter() {
				demoSimpleTable(ids)
			}
			for range c.CollapsingHeader(ids.PrepareStr("mixed"), c.WidgetText().Text("table (mixed columns)").Keep()).KeepIter() {
				demoMixedColumnsTable(ids)
			}
			for range c.CollapsingHeader(ids.PrepareStr("headless"), c.WidgetText().Text("table (headless)").Keep()).KeepIter() {
				demoHeaderlessTable(ids)
			}
			for range c.CollapsingHeader(ids.PrepareStr("large"), c.WidgetText().Text("table (large)").Keep()).KeepIter() {
				demoLargeTable(ids)
			}
			for range c.CollapsingHeader(ids.PrepareStr("richtext"), c.WidgetText().Text("table (rich text)").Keep()).KeepIter() {
				demoRichTextTable(ids)
			}
		},
	})
	registry.Register(registry.Demo{
		Name: "plots", Category: "Charts & plots", Title: icons.IconChartLine + " plots",
		Stage:       [2]float32{1024, 700},
		Kind:        registry.DemoKindUX,
		Description: "Line, scatter and bar plots plus a combined view that overlays multiple series with horizontal-line and text annotations.",
		Render: func(ids *c.WidgetIdStack) {
			for range c.CollapsingHeader(ids.PrepareStr("plot-lines-demo"), c.WidgetText().Text("line chart (sin/cos)").Keep()).DefaultOpen(true).KeepIter() {
				demoPlotLines(ids)
			}
			for range c.CollapsingHeader(ids.PrepareStr("plot-scatter-demo"), c.WidgetText().Text("scatter").Keep()).KeepIter() {
				demoPlotScatter(ids)
			}
			for range c.CollapsingHeader(ids.PrepareStr("plot-bars-demo"), c.WidgetText().Text("bars").Keep()).KeepIter() {
				demoPlotBars(ids)
			}
			for range c.CollapsingHeader(ids.PrepareStr("plot-combined-demo"), c.WidgetText().Text("combined (line + scatter + hline + text)").Keep()).KeepIter() {
				demoPlotCombined(ids)
			}
		},
	})
	registry.Register(registry.Demo{
		Name: "graphs", Category: "Charts & plots", Title: icons.IconChartBar + " graphs",
		Stage: [2]float32{1024, 700}, Flags: registry.DemoFlagNeedsLargeArea | registry.DemoFlagNonDeterministic, // dynamic-tree demo grows by time.Since(start)
		Kind:        registry.DemoKindUX,
		Description: "Force-directed, hierarchical and ring graph layouts sharing one set of navigation controls and a live event log.",
		Init: func(_ *c.WidgetIdStack) (state any) {
			state = newGraphsDemoState()
			return
		},
		RenderStateful: func(ids *c.WidgetIdStack, state any) {
			st := state.(*graphsDemoState)
			for range c.CollapsingHeader(ids.PrepareStr("graph-nav-demo"), c.WidgetText().Text("navigation controls").Keep()).KeepIter() {
				demoGraphGlobalNavControls(ids, st)
			}
			for range c.CollapsingHeader(ids.PrepareStr("graph-basic-demo"), c.WidgetText().Text("ring (random layout)").Keep()).DefaultOpen(true).KeepIter() {
				demoGraphBasic(ids, st)
			}
			for range c.CollapsingHeader(ids.PrepareStr("graph-dynamic-demo"), c.WidgetText().Text("tree (force-directed)").Keep()).KeepIter() {
				demoGraphDynamic(ids, st)
			}
			for range c.CollapsingHeader(ids.PrepareStr("graph-hierarchical-demo"), c.WidgetText().Text("tree (hierarchical, 10 nodes)").Keep()).KeepIter() {
				demoGraphHierarchical(ids, st)
			}
			demoGraphEventLog(ids, st)
		},
	})
	registry.Register(registry.Demo{
		Name: "walkers", Category: "Maps & geo", Title: icons.IconGlobe + " walkers (slippy maps)",
		Stage: [2]float32{1024, 700}, Flags: registry.DemoFlagNeedsLargeArea | registry.DemoFlagNeedsNetwork,
		Kind:        registry.DemoKindUX,
		Description: "Slippy maps via the walkers crate: OSM tile viewport, H3 heatmaps and a NoTiles choropleth canvas.",
		Init: func(_ *c.WidgetIdStack) (state any) {
			state = newWalkersDemoState()
			return
		},
		RenderStateful: func(ids *c.WidgetIdStack, state any) {
			st := state.(*walkersDemoState)
			// Basic demo sits outside a CollapsingHeader so the map always renders
			// (4-frame screenshot tour hits the open-animation gotcha inside a
			// CollapsingHeader — see SKILLS.md §12 / §16.10).
			demoWalkersBasic(ids, st)
			for range c.CollapsingHeader(ids.PrepareStr("walkers-camera-demo"), c.WidgetText().Text("camera (viewport + pointer readback)").Keep()).DefaultOpen(true).KeepIter() {
				demoWalkersCamera(ids, st)
			}
			for range c.CollapsingHeader(ids.PrepareStr("walkers-heatmap-info"), c.WidgetText().Text("heatmap (H3, uniform)").Keep()).KeepIter() {
				demoWalkersHeatmapInfo(ids, st)
			}
			for range c.CollapsingHeader(ids.PrepareStr("walkers-choropleth-demo"), c.WidgetText().Text("choropleth (H3, NoTiles canvas)").Keep()).KeepIter() {
				demoWalkersChoropleth(ids, st)
			}
		},
	})
	registry.Register(registry.Demo{
		Name: "mapraster", Category: "Maps & geo", Title: icons.IconGlobe + " mapRaster (in-DB geo raster)",
		Stage:       [2]float32{760, 600},
		Flags:       registry.DemoFlagNeedsLargeArea,
		Kind:        registry.DemoKindUX,
		Description: "An RGBA framebuffer (synthetic stand-in for an in-DB-rendered tile, ADR-0096) pinned to a lat/lon bbox and composited on a NoTiles walkers map via the mapRaster overlay.",
		Init: func(_ *c.WidgetIdStack) (state any) {
			return &walkersRasterDemoState{opacity: 0.9}
		},
		RenderStateful: func(ids *c.WidgetIdStack, state any) {
			demoWalkersRaster(ids, state.(*walkersRasterDemoState))
		},
	})
	registry.Register(registry.Demo{
		Name: "sql", Category: "Text & code", Title: icons.IconDatabase + " SQL highlighter",
		Stage:       [2]float32{1024, 600},
		Kind:        registry.DemoKindUX,
		Description: "SQL syntax highlighter built on the egui2 rich-text pipeline; pasted SQL is tokenized and re-emitted styled.",
		Render:      func(ids *c.WidgetIdStack) { demoSqlView(ids) },
	})
	registry.Register(registry.Demo{
		Name: "go", Category: "Text & code", Title: "Go highlighter",
		Stage:       [2]float32{1024, 700},
		Kind:        registry.DemoKindUX,
		Description: "Go syntax highlighter driven by go/scanner + go/parser: identifiers are walked through the AST so func decls, calls, type names, struct fields, and package qualifiers each receive a distinct color. Includes line-range slicing with a numbered gutter.",
		Render:      func(ids *c.WidgetIdStack) { demoGoView(ids) },
	})
	registry.Register(registry.Demo{
		Name: "json", Category: "Text & code", Title: icons.IconBracketsCurly + " JSON highlighter",
		Stage:       [2]float32{1024, 700},
		Kind:        registry.DemoKindUX,
		Description: "JSON syntax highlighter driven by encoding/json/jsontext: object keys, string/number/bool/null values and structural delimiters each receive a distinct color. Object keys are disambiguated from value strings via the decoder's stack-position state. Malformed input degrades gracefully — the parsed prefix stays highlighted and the unparseable tail falls through to the default color.",
		Render:      func(ids *c.WidgetIdStack) { demoJsonView(ids) },
	})
	registry.Register(registry.Demo{
		Name: "i18n", Category: "Text & code", Title: icons.IconGlobe + " international text",
		Stage:       [2]float32{1024, 500},
		Kind:        registry.DemoKindUX,
		Description: "International text rendering: Latin/Greek/Cyrillic/CJK and emoji against the bundled font atlas. RTL scripts (Arabic, Hebrew) and Indic/SE-Asian (Devanagari, Thai, Tamil) are excluded — see doc/explanation/egui-arabic-bidi-status.md.",
		Render:      func(ids *c.WidgetIdStack) { demoInternationalText(ids) },
	})
	registry.Register(registry.Demo{
		Name: "badges", Category: "Design system", Title: icons.IconTag + " badges (tones × variants)",
		Stage:       [2]float32{1024, 600},
		Kind:        registry.DemoKindUX,
		Description: "Badge / chip core showcase: tones × variants matrix and the three sizes. The widget is a Frame + LabelAtoms composition — no IDL or Rust changes.",
		Render:      func(ids *c.WidgetIdStack) { demoBadgeStyle(ids) },
	})
	registry.Register(registry.Demo{
		Name: "badges-extras", Category: "Design system", Title: icons.IconTag + " badges (icons / pill / tooltip)",
		Stage:       [2]float32{1024, 500},
		Kind:        registry.DemoKindUX,
		Description: "Decoration knobs that compose with any tone × variant × size: a leading icon glyph (any `icons.IconXxx` from `keelson/runtime/icons`), the .Pill() shorthand for full rounding, and Tooltip(\"…\") for hover hints.",
		Render:      func(ids *c.WidgetIdStack) { demoBadgeExtras(ids) },
	})
	registry.Register(registry.Demo{
		Name: "debug_tools", Category: "Debug", Title: icons.IconGear + " debug tools",
		Stage:       [2]float32{1024, 600},
		Flags:       registry.DemoFlagNonDeterministic, // framerate / render-pass / io counters drift per-frame
		Kind:        registry.DemoKindDX,
		Description: "Built-in egui debug overlays plus the Puffin frame profiler (toggleable via the egui2 debug-tools API).",
		Render: func(ids *c.WidgetIdStack) {
			c.ShowDebugTools()
			c.ShowPuffinProfiler()
		},
	})
	registry.Register(registry.Demo{
		Name: "colors_styling", Category: "Design system", Title: "colors & rich-text styling",
		Stage:       [2]float32{1024, 600},
		Kind:        registry.DemoKindDX,
		Description: "Inline rich-text styling: italics, strong, code, strikethrough and arbitrary fg/bg color spans.",
		SourceFunc:  renderColorsStylingDemo,
		Render:      func(ids *c.WidgetIdStack) { renderColorsStylingDemo(ids) },
	})
}

// renderColorsStylingDemo is the body of the "Colors & Styling" window in
// egui2_hl_demo.go. Kept here during migration so the registration file
// owns what it registers; the call-site in egui2_hl_demo.go collapses at
// C19 cutover.
func renderColorsStylingDemo(ids *c.WidgetIdStack) {
	atoms := c.Atoms()
	for rt := range atoms.StyledText("italics") {
		rt.Italics()
	}
	for rt := range atoms.StyledText(" strong") {
		rt.Strong()
	}
	for rt := range atoms.StyledText(" code") {
		rt.Code()
	}
	for rt := range atoms.StyledText(" strikethrough") {
		rt.Strikethrough()
	}
	// designlint:ignore=L2 (intentional jarring cyan-on-magenta contrast — the "awfully-colored" demo's whole point is showing what garish StyledTextColored looks like)
	for rt := range atoms.StyledTextColored(color.RGB(0, 255, 255).Keep(), color.RGB(255, 0, 255).Keep(), " awfully-colored") {
		_ = rt
	}
	c.Button(ids.PrepareStr("richtext"), atoms.Keep()).Send()
	for rt := range c.RichTextLabel("hello") {
		rt.Strikethrough()
	}
	for rt := range c.RichTextLabelColored(color.Hex(styletokens.InfoDefault.AsHex()).Keep(), color.Transparent.Keep(), "colored label") {
		_ = rt
	}
}
