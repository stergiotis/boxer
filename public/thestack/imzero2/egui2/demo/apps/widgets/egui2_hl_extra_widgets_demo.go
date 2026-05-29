//go:build llm_generated_opus47

package widgets

import (
	"fmt"
	"math"
	"time"

	c "github.com/stergiotis/boxer/public/thestack/imzero2/egui2/bindings"
	"github.com/stergiotis/boxer/public/thestack/imzero2/egui2/demo/apps/registry"
)

// extraWidgetsDemoState is the per-window state for the extra-widgets
// demo: three independent SelectableLabel toggles and a progress-bar
// origin used by the pulsing progress demo's sin() phase.
type extraWidgetsDemoState struct {
	selectable1 bool
	selectable2 bool
	selectable3 bool
	progressT   time.Time
}

func init() {
	registry.Register(registry.Demo{
		Name:        "extra-widgets",
		Category:    "Layout & widgets",
		Title:       "extra widgets",
		Stage:       [2]float32{1024, 600},
		Flags:       registry.DemoFlagNonDeterministic, // animated progress bar phase = time.Since(startup)
		Kind:        registry.DemoKindDX,
		Description: "Hyperlinks, selectable labels and the docking primitive — small standalone widgets without a category home.",
		Init: func(_ *c.WidgetIdStack) (state any) {
			state = &extraWidgetsDemoState{progressT: time.Now()}
			return
		},
		RenderStateful: func(ids *c.WidgetIdStack, state any) {
			demoExtraWidgets(ids, state.(*extraWidgetsDemoState))
		},
		SourceFunc: demoExtraWidgets,
	})
}

func demoExtraWidgets(ids *c.WidgetIdStack, st *extraWidgetsDemoState) {
	for range c.CollapsingHeader(ids.PrepareStr("hyperlinks"),
		c.WidgetText().Text("hyperlinks").Keep()).KeepIter() {
		c.Hyperlink("https://github.com/emilk/egui").Send()
		c.HyperlinkTo("egui (GitHub)", "https://github.com/emilk/egui").OpenInNewTab(true).Send()
		c.HyperlinkTo("egui docs", "https://docs.rs/egui").Send()
	}

	for range c.CollapsingHeader(ids.PrepareStr("selectable-labels"),
		c.WidgetText().Text("selectable labels").Keep()).KeepIter() {
		if c.SelectableLabel(ids.PrepareStr("sel-1"), st.selectable1, "option one").
			SendResp().HasPrimaryClicked() {
			st.selectable1 = !st.selectable1
		}
		if c.SelectableLabel(ids.PrepareStr("sel-2"), st.selectable2, "option two").
			SendResp().HasPrimaryClicked() {
			st.selectable2 = !st.selectable2
		}
		if c.SelectableLabel(ids.PrepareStr("sel-3"), st.selectable3, "option three").
			SendResp().HasPrimaryClicked() {
			st.selectable3 = !st.selectable3
		}
	}

	for range c.CollapsingHeader(ids.PrepareStr("progress-bars"),
		c.WidgetText().Text("progress bars").Keep()).KeepIter() {
		// Pulse 0..1 over a 3s period.
		phase := math.Mod(time.Since(st.progressT).Seconds(), 3.0) / 3.0
		c.ProgressBar(float32(phase)).Text("loading…").ShowPercentage().Send()
		c.ProgressBar(0.33).Text("one third").DesiredWidth(250).Send()
		c.ProgressBar(1.0).Text("complete").Animate(false).Send()
		// Keep the widget loop repainting so the animated bar moves.
		c.RequestRepaintAfter(0.05)
	}

	for range c.CollapsingHeader(ids.PrepareStr("group-scope-indent"),
		c.WidgetText().Text("group / scope / indent / pushId / enabledUi").Keep()).KeepIter() {
		for range c.Group().KeepIter() {
			c.Label("Inside a Group block").Send()
			for range c.Indent(ids.PrepareStr("ind")).KeepIter() {
				c.Label("Indented once").Send()
				for range c.Indent(ids.PrepareStr("ind2")).KeepIter() {
					c.Label("Indented twice").Send()
				}
			}
		}
		for range c.Scope().KeepIter() {
			c.Label("Inside Scope (isolated style)").Send()
		}
		// pushId lets two identical buttons coexist.
		for range c.PushId(ids.PrepareStr("copy-a")).KeepIter() {
			c.Button(ids.PrepareStr("btn"), c.Atoms().Text("click me (a)").Keep()).Send()
		}
		for range c.PushId(ids.PrepareStr("copy-b")).KeepIter() {
			c.Button(ids.PrepareStr("btn"), c.Atoms().Text("click me (b)").Keep()).Send()
		}
		// enabledUi with a live toggle.
		for range c.EnabledUi(st.selectable1).KeepIter() {
			c.Label("Follows 'option one'").Send()
			c.Button(ids.PrepareStr("enabled-demo"),
				c.Atoms().Text("enabled if option one is on").Keep()).Send()
		}
	}

	for range c.CollapsingHeader(ids.PrepareStr("hover-tooltips"),
		c.WidgetText().Text("hover tooltips").Keep()).KeepIter() {
		// Plain text tooltip — scoped block wrapping the target.
		for range c.HoverText("plain tooltip via HoverText block").KeepIter() {
			c.Button(ids.PrepareStr("tt-plain"),
				c.Atoms().Text("hover me (plain)").Keep()).Send()
		}

		// Rich UI tooltip — tip body + target body as two closures.
		c.HoverUi().Render(
			func() {
				for range c.Horizontal().KeepIter() {
					c.Spinner().Size(16).Send()
					c.Label("Rich tooltip").Send()
				}
				c.Separator().Send()
				c.Label("With multiple widgets").Send()
				c.Label("Rendered via on_hover_ui").Send()
			},
			func() {
				c.Button(ids.PrepareStr("tt-rich"),
					c.Atoms().Text("hover me (rich)").Keep()).Send()
			},
		)
	}

	// Docking (egui_dock) — three tabs proving composition:
	//   - "widgets": kept atoms + block iterator
	//   - "data":    a full etable (deferred-block widget) inside a tab body
	//   - "log":     scrollable content
	// Layout state (split ratios, active tab, drag-to-reorder) persists on
	// the Rust side, keyed by the dock area's id.
	for range c.CollapsingHeader(ids.PrepareStr("docking"),
		c.WidgetText().Text("docking (egui_dock)").Keep()).DefaultOpen(true).KeepIter() {
		// Constrain the dock area to a fixed min height so it's visible
		// inside the collapsing scope without fighting for space. Upper
		// bound (clip rect) is enforced inside the DockAreaRaw apply.
		c.UiSetMinHeight(240)
		for dock := range c.DockArea(ids.PrepareStr("main-dock")) {
			for range dock.Tab(1, "widgets") {
				c.Label("Kept widgets inside a tab:").Send()
				c.Button(ids.PrepareStr("dock-btn-1"),
					c.Atoms().Text("button A").Keep()).Send()
				c.Button(ids.PrepareStr("dock-btn-2"),
					c.Atoms().Text("button B").Keep()).Send()
				for range c.Horizontal().KeepIter() {
					c.Label("Block iterator inside a tab:").Send()
					c.Spinner().Size(14).Send()
				}
			}
			for range dock.Tab(2, "data") {
				employees := sampleEmployees()
				c.EtColumn(140.0).Resizable(true).Send()
				c.EtColumn(120.0).Resizable(true).Send()
				c.EtColumn(90.0).Resizable(true).Send()
				c.EtHeaderText("Name").Send()
				c.EtHeaderText("Department").Send()
				c.EtHeaderText("Salary").Send()
				et := c.EndETable(ids.PrepareStr("dock-etable"),
					uint64(len(employees)), 20.0, 1, 0)
				for row, emp := range employees {
					et.BeginCells(uint64(row), 0)
					c.Label(emp.Name).Send()
					et.EndCells()
					et.BeginCells(uint64(row), 1)
					c.Label(emp.Department).Send()
					et.EndCells()
					et.BeginCells(uint64(row), 2)
					c.Label(fmt.Sprintf("$%d", emp.Salary)).Send()
					et.EndCells()
				}
				et.Send()
			}
			for range dock.Tab(3, "log") {
				for range c.ScrollArea().Vscroll(true).KeepIter() {
					for i := 0; i < 40; i++ {
						c.Label(fmt.Sprintf("log line %d", i)).Send()
					}
				}
			}
		}
	}
}
