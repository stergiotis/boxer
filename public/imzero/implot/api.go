//go:build !bootstrap

package implot

import "github.com/stergiotis/boxer/public/imzero/imgui"

func MakeImPlotRect(xmin float64, xmax float64, ymin float64, ymax float64) ImPlotRect {
	return ImPlotRect{xmin, xmax, ymin, ymax}
}

func MakeImPlotPoint(x float64, y float64) ImPlotPoint {
	return ImPlotPoint(complex(x, y))
}

func (inst *ImPlotStyle) Load(style ImPlotStyleForeignPtr) {
	bs, fs, vec2s, cols, markers, maps := dumpStyle(style)
	cs := make([]imgui.ImVec4, 0, len(cols)/4)
	for i := 0; i < len(cols); i += 4 {
		cs = append(cs, imgui.MakeImVec4(cols[i], cols[i+1], cols[i+2], cols[i+3]))
	}
	inst.LineWeight = fs[0]
	inst.Marker = markers[0]
	inst.MarkerSize = fs[1]
	inst.MarkerWeight = fs[2]
	inst.FillAlpha = fs[3]
	inst.ErrorBarSize = fs[4]
	inst.ErrorBarWeight = fs[5]
	inst.DigitalBitHeight = fs[6]
	inst.DigitalBitGap = fs[7]
	inst.PlotBorderSize = fs[8]
	inst.MinorAlpha = fs[9]
	inst.MajorTickLen = imgui.MakeImVec2(vec2s[0], vec2s[1])
	inst.MinorTickLen = imgui.MakeImVec2(vec2s[2], vec2s[3])
	inst.MajorTickSize = imgui.MakeImVec2(vec2s[4], vec2s[5])
	inst.MinorTickSize = imgui.MakeImVec2(vec2s[6], vec2s[7])
	inst.MajorGridSize = imgui.MakeImVec2(vec2s[8], vec2s[9])
	inst.MinorGridSize = imgui.MakeImVec2(vec2s[10], vec2s[11])
	inst.PlotPadding = imgui.MakeImVec2(vec2s[12], vec2s[13])
	inst.LabelPadding = imgui.MakeImVec2(vec2s[14], vec2s[15])
	inst.LegendPadding = imgui.MakeImVec2(vec2s[16], vec2s[17])
	inst.LegendInnerPadding = imgui.MakeImVec2(vec2s[18], vec2s[19])
	inst.LegendSpacing = imgui.MakeImVec2(vec2s[20], vec2s[21])
	inst.MousePosPadding = imgui.MakeImVec2(vec2s[22], vec2s[23])
	inst.AnnotationPadding = imgui.MakeImVec2(vec2s[24], vec2s[25])
	inst.FitPadding = imgui.MakeImVec2(vec2s[26], vec2s[27])
	inst.PlotDefaultSize = imgui.MakeImVec2(vec2s[28], vec2s[29])
	inst.PlotMinSize = imgui.MakeImVec2(vec2s[30], vec2s[31])
	inst.Colors = cs
	inst.Colormap = maps[0]
	inst.UseLocalTime = bs[0]
	inst.UseISO8601 = bs[1]
	inst.Use24HourClock = bs[2]
	return
}

func (inst *ImPlotStyle) Dump(style ImPlotStyleForeignPtr) {
	fs := make([]float32, 31, 31)
	vec2s := make([]float32, 16*2, 16*2)
	markers := make([]int, 1, 1)
	bs := make([]bool, 3, 3)
	maps := make([]ImPlotColormap, 1, 1)
	cols := make([]float32, 0, len(inst.Colors)*4)
	for _, c := range inst.Colors {
		cols = append(cols, c[0], c[1], c[2], c[3])
	}
	fs[0] = inst.LineWeight
	markers[0] = inst.Marker
	fs[1] = inst.MarkerSize
	fs[2] = inst.MarkerWeight
	fs[3] = inst.FillAlpha
	fs[4] = inst.ErrorBarSize
	fs[5] = inst.ErrorBarWeight
	fs[6] = inst.DigitalBitHeight
	fs[7] = inst.DigitalBitGap
	fs[8] = inst.PlotBorderSize
	fs[9] = inst.MinorAlpha
	vec2s[0] = real(inst.MajorTickLen)
	vec2s[1] = imag(inst.MajorTickLen)
	vec2s[2] = real(inst.MinorTickLen)
	vec2s[3] = imag(inst.MinorTickLen)
	vec2s[4] = real(inst.MajorTickSize)
	vec2s[5] = imag(inst.MajorTickSize)
	vec2s[6] = real(inst.MinorTickSize)
	vec2s[7] = imag(inst.MinorTickSize)
	vec2s[8] = real(inst.MajorGridSize)
	vec2s[9] = imag(inst.MajorGridSize)
	vec2s[10] = real(inst.MinorGridSize)
	vec2s[11] = imag(inst.MinorGridSize)
	vec2s[12] = real(inst.PlotPadding)
	vec2s[13] = imag(inst.PlotPadding)
	vec2s[14] = real(inst.LabelPadding)
	vec2s[15] = imag(inst.LabelPadding)
	vec2s[16] = real(inst.LegendPadding)
	vec2s[17] = imag(inst.LegendPadding)
	vec2s[18] = real(inst.LegendInnerPadding)
	vec2s[19] = imag(inst.LegendInnerPadding)
	vec2s[20] = real(inst.LegendSpacing)
	vec2s[21] = imag(inst.LegendSpacing)
	vec2s[22] = real(inst.MousePosPadding)
	vec2s[23] = imag(inst.MousePosPadding)
	vec2s[24] = real(inst.AnnotationPadding)
	vec2s[25] = imag(inst.AnnotationPadding)
	vec2s[26] = real(inst.FitPadding)
	vec2s[27] = imag(inst.FitPadding)
	vec2s[28] = real(inst.PlotDefaultSize)
	vec2s[29] = imag(inst.PlotDefaultSize)
	vec2s[30] = real(inst.PlotMinSize)
	vec2s[31] = imag(inst.PlotMinSize)
	maps[0] = inst.Colormap
	bs[0] = inst.UseLocalTime
	bs[1] = inst.UseISO8601
	bs[2] = inst.Use24HourClock
	loadStyle(style, bs, fs, vec2s, cols, markers, maps)
}
