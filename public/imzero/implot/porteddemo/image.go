package porteddemo

import (
	"github.com/stergiotis/boxer/public/imzero/imgui"
	"github.com/stergiotis/boxer/public/imzero/implot"
)

func MakeImageDemo() (r Demofunc) {
	bmin := []float64{0, 0}
	bmax := []float64{1, 1}
	uv0 := []float32{0, 0}
	uv1 := []float32{1, 1}
	tint := imgui.MakeImVec4(1, 1, 1, 1)
	texid := imgui.ImTextureID(0)
	r = func() {
		texid = imgui.GetFontTexID()
		imgui.BulletText("Below we are displaying the font texture, which is the only texture we have\naccess to in this demo.")
		imgui.BulletText("Use the 'ImTextureID' type as storage to pass pointers or identifiers to your\nown texture data.")
		imgui.BulletText("See ImGui Wiki page 'Image Loading and Displaying Examples'.")
		bmin, _ = imgui.SliderFloat64NV("Min", bmin, -2, 2, "%.1f", 0)
		bmax, _ = imgui.SliderFloat64NV("Max", bmax, -2, 2, "%.1f", 0)
		uv0, _ = imgui.SliderFloat32NV("UV0", uv0, -2, 2, "%.1f", 0)
		uv1, _ = imgui.SliderFloat32NV("UV1", uv1, -2, 2, "%.1f", 0)
		tint, _ = imgui.ColorEdit4("Tint", tint, 0)
		if implot.BeginPlot("##image") {
			implot.PlotImageV("my image", texid,
				implot.MakeImPlotPoint(bmin[0], bmin[1]), implot.MakeImPlotPoint(bmax[0], bmax[1]),
				imgui.MakeImVec2(uv0[0], uv0[1]), imgui.MakeImVec2(uv1[0], uv1[1]),
				tint, 0)
			implot.EndPlot()
		}
	}
	return
}
