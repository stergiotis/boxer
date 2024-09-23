//go:build !bootstrap

package demo

import (
	"github.com/stergiotis/boxer/public/imzero/application"
	"github.com/stergiotis/boxer/public/imzero/imgui"
	"github.com/stergiotis/boxer/public/imzero/nerdfont"
)

var render func()

var coolbar func()

func menu(app *application.Application, beginMenu func(name string) bool, endMenu func(), menuItem func(name string) bool) {
	if beginMenu("Widgets") {
		if menuItem(nerdfont.FaPieChart + " PieMenu") {
			render = RenderPieMenuDemo
		}
		if menuItem(nerdfont.DevDropbox + " Basic Widgets") {
			render = MakeRenderBasicWidgets()
		}
		if menuItem(nerdfont.FaTable + " Interactive Table") {
			render = MakeRenderInteractiveTable()
		}
		if menuItem(nerdfont.MdArrowLeftRight + " Splitter") {
			render = RenderSplitterDemo
		}
		if menuItem(nerdfont.FaSpinner + " Spinner") {
			render = MakeRenderSpinnerDemo()
		}
		if menuItem(nerdfont.CodFlame + " FlameGraph") {
			render = MakeFlameGraphDemo()
		}
		if menuItem(nerdfont.FaFontAwesome + " Nerdfont") {
			render = MakeNerdfontDemo(app)
		}
		if menuItem(nerdfont.FaParagraph + " Paragraph") {
			render = MakeParagraphDemo()
		}
		endMenu()
	}
	if beginMenu("Editor") {
		if menuItem(nerdfont.CodEdit + " Text") {
			render = MakeRenderImColorTextDemo()
		}
		if menuItem(nerdfont.CodEdit + " Hex") {
			render = MakeHexEditorDemo()
		}
		endMenu()
	}
	if beginMenu("ImPlot") {
		if menuItem(nerdfont.MdLanguageCpp + " Native Demo") {
			render = RenderImPlotDemo
		}
		if menuItem(nerdfont.MdScatterPlot + " Ported Demo") {
			render = RenderImPlotPortedDemo
		}
		if menuItem(nerdfont.CodGraphLine + " Line Plot Demo") {
			render = RenderLinePlotDemo
		}
		if menuItem(nerdfont.DevStylus + " ImPlot Style Struct") {
			render = MakeRenderImPlotStyleDemo()
		}
		endMenu()
	}
	if beginMenu("Development") {
		if menuItem(nerdfont.FaBomb + " Assertions") {
			render = RenderAssertDemo
		}
		endMenu()
	}

	if beginMenu("FFFI") {
		if beginMenu("Sub sub\nmenu") {
			if menuItem("SubSub") {

			}
			if menuItem("SubSub2") {

			}
			endMenu()
		}
		if menuItem("Simple") {
			render = RenderSimpleDemo
		}
		if menuItem(nerdfont.FaSmileO + " Best-Case") {
			render = func() {
				RenderFffiBestCaseDemo(1000)
			}
		}
		if menuItem(nerdfont.FaFrownO + " Worst-Case") {
			render = func() {
				RenderFffiWorstCaseDemo(1000)
			}
		}
		if menuItem(nerdfont.DevStylus + " ImGui Style Struct") {
			render = MakeRenderImGuiStyleDemo()
		}
		endMenu()
	}

}

func RenderDemo(app *application.Application) {
	if coolbar == nil {
		coolbar = MakeCoolbarDemo(app)
	}

	r, _ := imgui.BeginV("demo window", imgui.ImGuiWindowFlags_MenuBar)
	if r {
		//if imgui.IsWindowHovered() && imgui.IsMouseClickedV(1, false) {
		//	imgui.OpenPopupV("PieMenu", 0)
		//}

		//if imgui.BeginPiePopupV("PieMenu", 1) {
		//	menu(app, imgui.BeginPieMenu, imgui.EndPieMenu, imgui.PieMenuItem)
		//	imgui.EndPiePopup()
		//}

		if imgui.BeginMenuBar() {
			menu(app, imgui.BeginMenu, imgui.EndMenu, imgui.MenuItem)
			imgui.EndMenuBar()
		}
	}

	if render != nil {
		render()
	}

	coolbar()
	imgui.End()
}
