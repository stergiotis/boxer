//go:build fffi_idl_code

package imgui

//import "github.com/stergiotis/boxer/public/imzero/utils"

func PushFont(font ImFontPtr) {
	_ = `ImGui::PushFont((ImFont*)font);`
}
func GetFont() (font ImFontPtr) {
	_ = `font = (uintptr_t)ImGui::GetFont();`
	return
}
func GetFontTexID() (tex ImTextureID) {
	_ = `tex = (uintptr_t)ImGui::GetIO().Fonts->TexID`
	return
}
func addFontFromMemoryTrueTypeFontV(name string, fontData []byte, sizeInPixels float32,
	glyphRanges []ImWchar,
	oversampleH int, oversampleV int,
	pixelSnapH bool,
	glyphExtraSpacing ImVec2,
	glyphOffset ImVec2,
	glyphMinAdvanceX float32, glyphMaxAdvanceX float32,
	mergeMode bool,
	fontBuilderFlags uint,
	rasterizerMultiply float32,
	ellipsisChar ImWchar,
) (font ImFontPtr) {
	//name = utils.TruncateDescriptiveNameLeft(name, 40-1, "…")
	_ = `
  static_assert(sizeof(ImWchar) == 4, "code assumes IMGUI_USE_WCHAR32");
  auto cfg = ImFontConfig();

  // copy ttf font memory
  {
	  auto fontDataSize = getSliceLength(fontData);
	  cfg.FontData = ImGui::MemAlloc(fontDataSize);
	  memcpy(cfg.FontData,fontData,fontDataSize);

	  cfg.FontDataSize = fontDataSize;
	  cfg.FontDataOwnedByAtlas = true;
  }

  cfg.FontNo = 0;
  cfg.SizePixels = sizeInPixels;
  cfg.OversampleH = oversampleH;
  cfg.OversampleV = oversampleV;
  cfg.PixelSnapH = pixelSnapH;
  cfg.GlyphExtraSpacing = ImVec2(glyphExtraSpacing);
  cfg.GlyphOffset = ImVec2(glyphOffset);
  cfg.GlyphMinAdvanceX = glyphMinAdvanceX;
  cfg.GlyphMaxAdvanceX = glyphMaxAdvanceX;
  cfg.MergeMode = mergeMode;
  cfg.FontBuilderFlags = fontBuilderFlags;
  cfg.RasterizerMultiply = rasterizerMultiply;
  cfg.EllipsisChar = ellipsisChar;

  // copy name (truncate if too long)
  {
    auto l = getStringLength(name);
    if(l >= sizeof(cfg.Name)) {
       l = sizeof(cfg.Name)-1;
    }
    memcpy(cfg.Name,name,l);
    cfg.Name[l] = '\0';
  }

  // copy glyph range
  {
	auto l = getSliceLength(glyphRanges);
	auto tmp = (ImWchar*)ImGui::MemAlloc(l*sizeof(ImWchar));
    memcpy(tmp,glyphRanges,l*sizeof(ImWchar));
	cfg.GlyphRanges = tmp;
  }

  ImGuiIO& io = ImGui::GetIO();
  font = (uintptr_t)io.Fonts->AddFont(&cfg);

  if(font != 0 && !mergeMode){
     io.Fonts->Build();
  }
`
	return
}

func (foreignptr ImFontPtr) RenderChar(drawList ImDrawListPtr, size float32, pos ImVec2, color uint32, charP rune) {
	_ = `
auto dl = (ImDrawList*)drawList;
if(dl == nullptr) {
   dl = ImGui::GetForegroundDrawList();
}
((ImFont*)foreignptr)->RenderChar(dl,size,pos,color,(ImWchar)charP)`
}
func (foreignptr ImFontPtr) FontRenderText(drawList ImDrawListPtr, size float32, pos ImVec2, color uint32, clipRect ImVec4, text string) {
	_ = `
auto dl = (ImDrawList*)drawList;
if(dl == nullptr) {
   dl = ImGui::GetForegroundDrawList();
}
((ImFont*)foreignptr)->RenderText(dl,size,pos,(ImU32)color,clipRect,text,text+getStringLength(text))`
}
func (foreignptr ImFontPtr) FontRenderTextV(drawList ImDrawListPtr, size float32, pos ImVec2, color uint32, clipRect ImVec4, text string, wrapWidth float32, cpuFineClip bool) {
	_ = `
auto dl = (ImDrawList*)drawList;
if(dl == nullptr) {
   dl = ImGui::GetForegroundDrawList();
}
((ImFont*)foreignptr)->RenderText(dl,size,pos,(ImU32)color,clipRect,text,text+getStringLength(text),wrapWidth,cpuFineClip)`
}

// CalcTextSizeA
// 'max_width' stops rendering after a certain width (could be turned into a 2d size). FLT_MAX to disable.
// 'wrap_width' enable automatic word-wrapping across multiple lines to fit into given width. 0.0f to disable.
func (foreignptr ImFontPtr) CalcTextSizeA(size float32, max_width float32, wrap_width float32, text string, pixel_perfect bool) (r ImVec2, remainingBytes Size_t) {
	_ = `
         ImVec2 r;
         const char **remaining = nullptr;
         auto end = text+getStringLength(text);
         r = ((ImFont*)foreignptr)->CalcTextSizeA(size,max_width,wrap_width,text,end,remaining);
         // see https://github.com/ocornut/imgui/pull/3437 https://github.com/ocornut/imgui/issues/3776 https://github.com/ocornut/imgui/issues/791
         if(pixel_perfect) {
            // original
            //r.x = IM_TRUNC(r.x + 0.99999f);
            // improved, see https://github.com/ocornut/imgui/issues/791
            r.x = ceilf(r.x);
         }
         remainingBytes = (uintptr_t)end-(uintptr_t)remaining;
`
	return
}
