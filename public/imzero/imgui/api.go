//go:build !bootstrap

package imgui

import (
	"fmt"
	"math"

	"github.com/stergiotis/boxer/public/observability/eh/eb"
)

/*
	func Begin(name string) (r bool) {
		r, _ = inst.BeginV(name, 0)
		return
	}

	func Button(label string) (r bool) {
		r = inst.ButtonV(label, ImVec2(complex(0.0, 0.0)))
		return
	}
*/
func Text(format string, a ...any) {
	TextUnformatted(fmt.Sprintf(format, a...))
	return
}

/*func BeginChild(str_id string) bool {
	return inst.BeginChildV(str_id, 0, false, 0)
}*/

type FontConfig struct {
	FontData           []byte    // TTF/OTF data
	FontNo             int       // Index of font within TTF/OTF file
	GlyphRanges        []ImWchar // List of unicode range (2 value per range, values are inclusive, zero-terminated list)
	OversampleH        int       // Rasterize at higher quality for sub-pixel positioning. Note the difference between 2 and 3 is minimal. You can reduce this to 1 for large glyphs save memory. Read https://github.com/nothings/stb/blob/master/tests/oversample/README.md for details.
	OversampleV        int       // Rasterize at higher quality for sub-pixel positioning. This is not really useful as we don't use sub-pixel positions on the Y axis.
	PixelSnapH         bool      // Align every glyph to pixel boundary. Useful e.g. if you are merging a non-pixel aligned font with the default font. If enabled, you can set OversampleH/V to 1.
	GlyphExtraSpacing  ImVec2    // Extra spacing (in pixels) between glyphs. Only X axis is supported for now
	GlyphOffset        ImVec2    // Offset all glyphs from this font input
	GlyphMinAdvanceX   float32   // Minimum AdvanceX for glyphs, set Min to align font icons, set both Min/Max to enforce mono-space font
	GlyphMaxAdvanceX   float32   // Maximum AdvanceX for glyphs
	MergeMode          bool      // Merge into previous ImFont, so you can combine multiple inputs font into one ImFont (e.g. ASCII font + icons + Japanese glyphs). You may want to use GlyphOffset.y when merge font of different heights.\endverbatim
	FontBuilderFlags   uint      // Settings for custom font builder. THIS IS BUILDER IMPLEMENTATION DEPENDENT. Leave as zero if unsure.
	RasterizerMultiply float32   // Brighten (>1.0f) or darken (<1.0f) font output. Brightening small fonts may be a good workaround to make them more readable.\endverbatim
	EllipsisChar       ImWchar   // Explicitly specify unicode codepoint of ellipsis character. When fonts are being merged first specified ellipsis will be used.\endverbatim
	Name               string
}

func NewFontConfig() *FontConfig {
	return &FontConfig{
		FontData:           nil,
		FontNo:             0,
		GlyphRanges:        nil,
		OversampleH:        2,
		OversampleV:        1,
		PixelSnapH:         false,
		GlyphExtraSpacing:  complex(0.0, 0.0),
		GlyphOffset:        complex(0.0, 0.0),
		GlyphMinAdvanceX:   0,
		GlyphMaxAdvanceX:   math.MaxFloat32,
		MergeMode:          false,
		FontBuilderFlags:   0,
		RasterizerMultiply: 1.0,
		EllipsisChar:       ImWchar(-1),
		Name:               "",
	}
}

type ConfiguredFont struct {
	Config       *FontConfig
	SizeInPixels float32
	Ptr          ImFontPtr
}

var ConfiguredFonts = make([]*ConfiguredFont, 0, 16)

func AddFont(cfg *FontConfig, sizeInPixels float32) (font ImFontPtr, err error) {
	font = addFontFromMemoryTrueTypeFontV(cfg.Name, cfg.FontData, sizeInPixels, cfg.GlyphRanges, cfg.OversampleH, cfg.OversampleV, cfg.PixelSnapH,
		cfg.GlyphExtraSpacing, cfg.GlyphOffset, cfg.GlyphMinAdvanceX, cfg.GlyphMaxAdvanceX, cfg.MergeMode, cfg.FontBuilderFlags, cfg.RasterizerMultiply, cfg.EllipsisChar)
	if font == 0 {
		err = eb.Build().Str("name", cfg.Name).Errorf("unable to add font, imgui returned null pointer")
		return
	}
	t := &ConfiguredFont{
		Config:       cfg,
		SizeInPixels: sizeInPixels,
		Ptr:          font,
	}
	ConfiguredFonts = append(ConfiguredFonts, t)
	return
}

func ToggleFlags[T ~uint | ~uint8 | ~uint16 | ~uint32 | ~uint64 | ~int | ~int8 | ~int16 | ~int32 | ~int64](label string, flagsP T, val T) (flags T, changed bool) {
	var v bool
	v, changed = Toggle(label, flagsP&val != 0)
	if v {
		flags = flagsP | val
	} else {
		flags = flagsP & ^val
	}
	return
}

func RadioFlags[T ~uint | ~uint8 | ~uint16 | ~uint32 | ~uint64 | ~int | ~int8 | ~int16 | ~int32 | ~int64](label string, flagsP T, val T) (flags T) {
	var active bool
	active = RadioButton(label, flagsP&val != 0)
	if active {
		flags = flagsP | val
	} else {
		flags = flagsP & ^val
	}
	return
}

func MakeImVec2(x float32, y float32) ImVec2 {
	return ImVec2(complex(x, y))
}

func MakeImVec4(x float32, y float32, z float32, w float32) ImVec4 {
	return ImVec4([4]float32{x, y, z, w})
}

func MakeImVec4ImVec2(a ImVec2, b ImVec2) ImVec4 {
	return ImVec4([4]float32{real(a), imag(a), real(b), imag(b)})
}

func (inst *ImGuiStyle) Load(style ImGuiStyleForeignPtr) {
	bs, fs, vec2s, cols, dirs, hovers := dumpStyle(style)
	cs := make([]ImVec4, 0, len(cols)/4)
	for i := 0; i < len(cols); i += 4 {
		cs = append(cs, MakeImVec4(cols[i], cols[i+1], cols[i+2], cols[i+3]))
	}
	inst.Alpha = fs[0]
	inst.DisabledAlpha = fs[1]
	inst.WindowPadding = MakeImVec2(vec2s[0], vec2s[1])
	inst.WindowRounding = fs[2]
	inst.WindowBorderSize = fs[3]
	inst.WindowMinSize = MakeImVec2(vec2s[2], vec2s[3])
	inst.WindowTitleAlign = MakeImVec2(vec2s[4], vec2s[5])
	inst.WindowMenuButtonPosition = dirs[0]
	inst.ChildRounding = fs[4]
	inst.ChildBorderSize = fs[5]
	inst.PopupRounding = fs[6]
	inst.PopupBorderSize = fs[7]
	inst.FramePadding = MakeImVec2(vec2s[6], vec2s[7])
	inst.FrameRounding = fs[8]
	inst.FrameBorderSize = fs[9]
	inst.ItemSpacing = MakeImVec2(vec2s[8], vec2s[9])
	inst.ItemInnerSpacing = MakeImVec2(vec2s[10], vec2s[11])
	inst.CellPadding = MakeImVec2(vec2s[12], vec2s[13])
	inst.TouchExtraPadding = MakeImVec2(vec2s[14], vec2s[15])
	inst.IndentSpacing = fs[10]
	inst.ColumnsMinSpacing = fs[11]
	inst.ScrollbarSize = fs[12]
	inst.ScrollbarRounding = fs[13]
	inst.GrabMinSize = fs[14]
	inst.GrabRounding = fs[15]
	inst.LogSliderDeadzone = fs[16]
	inst.TabRounding = fs[17]
	inst.TabBorderSize = fs[18]
	inst.TabMinWidthForCloseButton = fs[19]
	inst.TabBarBorderSize = fs[20]
	inst.ColorButtonPosition = dirs[1]
	inst.ButtonTextAlign = MakeImVec2(vec2s[16], vec2s[17])
	inst.SelectableTextAlign = MakeImVec2(vec2s[18], vec2s[19])
	inst.SeparatorTextBorderSize = fs[21]
	inst.SeparatorTextAlign = MakeImVec2(vec2s[20], vec2s[21])
	inst.SeparatorTextPadding = MakeImVec2(vec2s[22], vec2s[23])
	inst.DisplayWindowPadding = MakeImVec2(vec2s[24], vec2s[25])
	inst.DisplaySafeAreaPadding = MakeImVec2(vec2s[26], vec2s[27])
	inst.DockingSeparatorSize = fs[22]
	inst.MouseCursorScale = fs[23]
	inst.AntiAliasedLines = bs[0]
	inst.AntiAliasedLinesUseTex = bs[1]
	inst.AntiAliasedFill = bs[2]
	inst.CurveTessellationTol = fs[24]
	inst.CircleTessellationMaxError = fs[25]
	inst.Colors = cs
	inst.HoverStationaryDelay = fs[26]
	inst.HoverDelayShort = fs[27]
	inst.HoverDelayNormal = fs[28]
	inst.HoverFlagsForTooltipMouse = hovers[0]
	inst.HoverFlagsForTooltipNav = hovers[1]
	return
}

func (inst *ImGuiStyle) Dump(style ImGuiStyleForeignPtr) {
	bs := make([]bool, 3, 3)
	fs := make([]float32, 29, 29)
	vec2s := make([]float32, 14*2, 14*2)
	cols := make([]float32, 0, len(inst.Colors)*4)
	dirs := make([]ImGuiDir, 2, 2)
	hovers := make([]ImGuiHoveredFlags, 2, 2)
	for _, c := range inst.Colors {
		cols = append(cols, c[0], c[1], c[2], c[3])
	}
	fs[0] = inst.Alpha
	fs[1] = inst.DisabledAlpha
	vec2s[0] = real(inst.WindowPadding)
	vec2s[1] = imag(inst.WindowPadding)
	fs[2] = inst.WindowRounding
	fs[3] = inst.WindowBorderSize
	vec2s[2] = real(inst.WindowMinSize)
	vec2s[3] = imag(inst.WindowMinSize)
	vec2s[4] = real(inst.WindowTitleAlign)
	vec2s[5] = imag(inst.WindowTitleAlign)
	dirs[0] = inst.WindowMenuButtonPosition
	fs[4] = inst.ChildRounding
	fs[5] = inst.ChildBorderSize
	fs[6] = inst.PopupRounding
	fs[7] = inst.PopupBorderSize
	vec2s[6] = real(inst.FramePadding)
	vec2s[7] = real(inst.FramePadding)
	fs[8] = inst.FrameRounding
	fs[9] = inst.FrameBorderSize
	vec2s[8] = real(inst.ItemSpacing)
	vec2s[9] = imag(inst.ItemSpacing)
	vec2s[10] = real(inst.ItemInnerSpacing)
	vec2s[11] = imag(inst.ItemInnerSpacing)
	vec2s[12] = real(inst.CellPadding)
	vec2s[13] = imag(inst.CellPadding)
	vec2s[14] = real(inst.TouchExtraPadding)
	vec2s[15] = imag(inst.TouchExtraPadding)
	fs[10] = inst.IndentSpacing
	fs[11] = inst.ColumnsMinSpacing
	fs[12] = inst.ScrollbarSize
	fs[13] = inst.ScrollbarRounding
	fs[14] = inst.GrabMinSize
	fs[15] = inst.GrabRounding
	fs[16] = inst.LogSliderDeadzone
	fs[17] = inst.TabRounding
	fs[18] = inst.TabBorderSize
	fs[19] = inst.TabMinWidthForCloseButton
	fs[20] = inst.TabBarBorderSize
	dirs[1] = inst.ColorButtonPosition
	vec2s[16] = real(inst.ButtonTextAlign)
	vec2s[17] = imag(inst.ButtonTextAlign)
	vec2s[18] = real(inst.SelectableTextAlign)
	vec2s[19] = imag(inst.SelectableTextAlign)
	fs[21] = inst.SeparatorTextBorderSize
	vec2s[20] = real(inst.SeparatorTextAlign)
	vec2s[21] = imag(inst.SeparatorTextAlign)
	vec2s[22] = real(inst.SeparatorTextPadding)
	vec2s[23] = imag(inst.SeparatorTextPadding)
	vec2s[24] = real(inst.DisplayWindowPadding)
	vec2s[25] = imag(inst.DisplayWindowPadding)
	vec2s[26] = real(inst.DisplaySafeAreaPadding)
	vec2s[27] = imag(inst.DisplaySafeAreaPadding)
	fs[22] = inst.DockingSeparatorSize
	fs[23] = inst.MouseCursorScale
	bs[0] = inst.AntiAliasedLines
	bs[1] = inst.AntiAliasedLinesUseTex
	bs[2] = inst.AntiAliasedFill
	fs[24] = inst.CurveTessellationTol
	fs[25] = inst.CircleTessellationMaxError
	fs[26] = inst.HoverStationaryDelay
	fs[27] = inst.HoverDelayShort
	fs[28] = inst.HoverDelayNormal
	hovers[0] = inst.HoverFlagsForTooltipMouse
	hovers[1] = inst.HoverFlagsForTooltipNav
	loadStyle(style, bs, fs, vec2s, cols, dirs, hovers)
	return
}

func ScaleImVec2[T ~float32](v ImVec2, f T) (vOut ImVec2) {
	vOut = ImVec2(complex(real(v)*float32(f), imag(v)*float32(f)))
	return
}
