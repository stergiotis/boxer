//go:build !bootstrap

package demo

import (
	"github.com/stergiotis/boxer/public/imzero/dto"
	"github.com/stergiotis/boxer/public/imzero/imgui"
	"github.com/stergiotis/pebble2impl/public/hmi/designsystem/spectrum/tk"
)

func RenderTextDemo() {
	texts := []string{
		"m",
		"mmmmmmmmmmmmmmmmmmmmmmmmmmmmmmmm",
		"a_BMniiiAAAAAaA=,,,O",
	}
	ava := real(imgui.GetContentRegionAvail())
	fontSize := imgui.GetFontSize()
	for i, m := range []dto.TextMeasureModeX{dto.TextMeasureModeXAdvanceWidth, dto.TextMeasureModeXBondingBox} {
		imgui.PushIDInt(i)
		skipOuter := true
		switch m {
		case dto.TextMeasureModeXAdvanceWidth:
			if imgui.TreeNode("Advance Measure") {
				skipOuter = false
			}
			break
		case dto.TextMeasureModeXBondingBox:
			if imgui.TreeNode("Bounding Box Measure") {
				skipOuter = false
			}
			break
		}
		if !skipOuter {
			imgui.PushTextMeasureMode(m, dto.TextMeasureModeYFontSize)
			for j, p := range []dto.IsParagraphText{dto.IsParagraphTextNever, dto.IsParagraphTextAlways} {
				imgui.PushIDInt(j)
				imgui.PushIsParagraphText(p)
				skipInner := true
				switch p {
				case dto.IsParagraphTextNever:
					if imgui.TreeNode("Simple Text") {
						skipInner = false
					}
					break
				case dto.IsParagraphTextAlways:
					if imgui.TreeNode("Paragraph") {
						skipInner = false
					}
					break
				}
				if !skipInner {
					for k, t := range texts {
						imgui.PushIDInt(k)
						imgui.TextUnformatted(t)
						w := imgui.CalcTextWidth(t)
						imgui.InvisibleButtonP("canvas", imgui.MakeImVec2(ava, 3*fontSize))
						p0 := imgui.GetItemRectMin()
						p1 := imgui.GetItemRectMax()
						drawList := imgui.GetWindowDrawList()
						drawList.PushClipRectV(p0, p1, true)
						drawList.AddRect(p0+(1.0+1.0i), p1-(1.0+1.0i), tk.Gray400)
						font := imgui.GetFont()
						cr := imgui.MakeImVec4(real(p0), imag(p0), real(p1), imag(p1))
						drawList.AddLineV(p0, p0+imgui.MakeImVec2(real(w), 0.0), tk.AccentColor400, 6.0)
						font.FontRenderTextV(drawList, fontSize, p0+imgui.MakeImVec2(0.0, fontSize), tk.AccentColor800, cr, t, 0.0, false)
						drawList.PopClipRect()
						imgui.Text("width=%f,height=%f", real(w), imag(w))
						imgui.PopID()
					}
					imgui.TreePop()
				}
				imgui.PopIsParagraphText()
				imgui.PopID()
			}
			imgui.PopTextMeasureMode()
			imgui.TreePop()
		}
		imgui.PopID()
	}
}
