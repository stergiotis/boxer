//go:build fffi_idl_code

package imgui

func loadStyle(ptr ImGuiStyleForeignPtr, bs []bool, fs []float32, vec2s []float32, cols []float32, dirs []ImGuiDir, hovers []ImGuiHoveredFlags) {
	_ = `
   int i;
   auto s = (ImGuiStyle *)ptr;
   
   i = 0;
   s->AntiAliasedLines = bs[i++];
   s->AntiAliasedLinesUseTex = bs[i++];
   s->AntiAliasedFill = bs[i++];

   i = 0;
   s->Alpha = fs[i++];
   s->DisabledAlpha = fs[i++];
   s->WindowRounding = fs[i++];
   s->WindowBorderSize = fs[i++];
   s->ChildRounding = fs[i++];
   s->ChildRounding = fs[i++];
   s->ChildBorderSize = fs[i++];
   s->PopupRounding = fs[i++];
   s->PopupBorderSize = fs[i++];
   s->FrameRounding = fs[i++];
   s->FrameBorderSize = fs[i++];
   s->IndentSpacing = fs[i++];
   s->ColumnsMinSpacing = fs[i++];
   s->ScrollbarSize = fs[i++];
   s->ScrollbarRounding = fs[i++];
   s->GrabMinSize = fs[i++];
   s->GrabRounding = fs[i++];
   s->LogSliderDeadzone = fs[i++];
   s->TabRounding = fs[i++];
   s->TabBorderSize = fs[i++];
   s->TabMinWidthForCloseButton = fs[i++];
   s->TabBarBorderSize = fs[i++];
   s->SeparatorTextBorderSize = fs[i++];
   s->DockingSeparatorSize = fs[i++];
   s->MouseCursorScale = fs[i++];
   s->CurveTessellationTol = fs[i++];
   s->CircleTessellationMaxError = fs[i++];
   s->HoverStationaryDelay = fs[i++];
   s->HoverDelayShort = fs[i++];
   s->HoverDelayNormal = fs[i++];
   
   i = 0;
   s->WindowPadding.x = vec2s[i++];
   s->WindowPadding.y = vec2s[i++];
   s->WindowMinSize.x = vec2s[i++];
   s->WindowMinSize.y = vec2s[i++];
   s->WindowTitleAlign.x = vec2s[i++];
   s->WindowTitleAlign.y = vec2s[i++];
   s->FramePadding.x = vec2s[i++];
   s->FramePadding.y = vec2s[i++];
   s->ItemSpacing.x = vec2s[i++];
   s->ItemSpacing.y = vec2s[i++];
   s->ItemInnerSpacing.x = vec2s[i++];
   s->ItemInnerSpacing.y = vec2s[i++];
   s->CellPadding.x = vec2s[i++];
   s->CellPadding.y = vec2s[i++];
   s->TouchExtraPadding.x = vec2s[i++];
   s->TouchExtraPadding.y = vec2s[i++];
   s->ButtonTextAlign.x = vec2s[i++];
   s->ButtonTextAlign.y = vec2s[i++];
   s->SelectableTextAlign.x = vec2s[i++];
   s->SelectableTextAlign.y = vec2s[i++];
   s->SeparatorTextAlign.x = vec2s[i++];
   s->SeparatorTextAlign.y = vec2s[i++];
   s->SeparatorTextPadding.x = vec2s[i++];
   s->SeparatorTextPadding.y = vec2s[i++];
   s->DisplayWindowPadding.x = vec2s[i++];
   s->DisplayWindowPadding.y = vec2s[i++];
   s->DisplaySafeAreaPadding.x = vec2s[i++];
   s->DisplaySafeAreaPadding.y = vec2s[i++];

   i = 0;
   for(i = 0;i<ImGuiCol_COUNT;i++) {
      s->Colors[i].x = cols[i*4+0];
      s->Colors[i].y = cols[i*4+1];
      s->Colors[i].z = cols[i*4+2];
      s->Colors[i].w = cols[i*4+3];
   }

   i = 0;
   s->WindowMenuButtonPosition = ImGuiDir(dirs[i++]);
   s->ColorButtonPosition = ImGuiDir(dirs[i++]);
   
   i = 0;
   s->HoverFlagsForTooltipMouse = hovers[i++];
   s->HoverFlagsForTooltipNav = hovers[i++];
`
	return
}

func GetStyle() (r ImGuiStyleForeignPtr) {
	_ = `r = (uintptr_t)&ImGui::GetStyle()`
	return
}

func dumpStyle(ptr ImGuiStyleForeignPtr) (bs []bool, fs []float32, vec2s []float32, cols []float32, dirs []ImGuiDir, hovers []ImGuiHoveredFlags) {
	_ = `
   auto s = (ImGuiStyle*)ptr;
   size_t bs_len = 3;
   bs = (decltype(bs))arenaCalloc(bs_len,sizeof(*bs));
   size_t fs_len = 30;
   fs = (decltype(fs))arenaCalloc(fs_len,sizeof(*fs));
   size_t vec2s_len = 14*2;
   vec2s = (decltype(vec2s))arenaCalloc(vec2s_len,sizeof(*vec2s));
   size_t cols_len = 4*ImGuiCol_COUNT;
   cols = (decltype(cols))arenaCalloc(cols_len,sizeof(*cols));
   size_t dirs_len = 2;
   dirs = (decltype(dirs))arenaCalloc(dirs_len,sizeof(*dirs));
   size_t hovers_len = 2;
   hovers = (decltype(hovers))arenaCalloc(hovers_len,sizeof(*hovers));

   int i;
   
   i = 0;
   bs[i++] = s->AntiAliasedLines;
   bs[i++] = s->AntiAliasedLinesUseTex;
   bs[i++] = s->AntiAliasedFill;

   i = 0;
   fs[i++] = s->Alpha;
   fs[i++] = s->DisabledAlpha;
   fs[i++] = s->WindowRounding;
   fs[i++] = s->WindowBorderSize;
   fs[i++] = s->ChildRounding;
   fs[i++] = s->ChildRounding;
   fs[i++] = s->ChildBorderSize;
   fs[i++] = s->PopupRounding;
   fs[i++] = s->PopupBorderSize;
   fs[i++] = s->FrameRounding;
   fs[i++] = s->FrameBorderSize;
   fs[i++] = s->IndentSpacing;
   fs[i++] = s->ColumnsMinSpacing;
   fs[i++] = s->ScrollbarSize;
   fs[i++] = s->ScrollbarRounding;
   fs[i++] = s->GrabMinSize;
   fs[i++] = s->GrabRounding;
   fs[i++] = s->LogSliderDeadzone;
   fs[i++] = s->TabRounding;
   fs[i++] = s->TabBorderSize;
   fs[i++] = s->TabMinWidthForCloseButton;
   fs[i++] = s->TabBarBorderSize;
   fs[i++] = s->SeparatorTextBorderSize;
   fs[i++] = s->DockingSeparatorSize;
   fs[i++] = s->MouseCursorScale;
   fs[i++] = s->CurveTessellationTol;
   fs[i++] = s->CircleTessellationMaxError;
   fs[i++] = s->HoverStationaryDelay;
   fs[i++] = s->HoverDelayShort;
   fs[i++] = s->HoverDelayNormal;
   
   i = 0;
   vec2s[i++] = s->WindowPadding.x;
   vec2s[i++] = s->WindowPadding.y;
   vec2s[i++] = s->WindowMinSize.x;
   vec2s[i++] = s->WindowMinSize.y;
   vec2s[i++] = s->WindowTitleAlign.x;
   vec2s[i++] = s->WindowTitleAlign.y;
   vec2s[i++] = s->FramePadding.x;
   vec2s[i++] = s->FramePadding.y;
   vec2s[i++] = s->ItemSpacing.x;
   vec2s[i++] = s->ItemSpacing.y;
   vec2s[i++] = s->ItemInnerSpacing.x;
   vec2s[i++] = s->ItemInnerSpacing.y;
   vec2s[i++] = s->CellPadding.x;
   vec2s[i++] = s->CellPadding.y;
   vec2s[i++] = s->TouchExtraPadding.x;
   vec2s[i++] = s->TouchExtraPadding.y;
   vec2s[i++] = s->ButtonTextAlign.x;
   vec2s[i++] = s->ButtonTextAlign.y;
   vec2s[i++] = s->SelectableTextAlign.x;
   vec2s[i++] = s->SelectableTextAlign.y;
   vec2s[i++] = s->SeparatorTextAlign.x;
   vec2s[i++] = s->SeparatorTextAlign.y;
   vec2s[i++] = s->SeparatorTextPadding.x;
   vec2s[i++] = s->SeparatorTextPadding.y;
   vec2s[i++] = s->DisplayWindowPadding.x;
   vec2s[i++] = s->DisplayWindowPadding.y;
   vec2s[i++] = s->DisplaySafeAreaPadding.x;
   vec2s[i++] = s->DisplaySafeAreaPadding.y;

   i = 0;
   for(i = 0;i<ImGuiCol_COUNT;i++) {
      cols[i*4+0] = s->Colors[i].x;
      cols[i*4+1] = s->Colors[i].y;
      cols[i*4+2] = s->Colors[i].z;
      cols[i*4+3] = s->Colors[i].w;
   }

   i = 0;
   dirs[i++] = s->WindowMenuButtonPosition;
   dirs[i++] = s->ColorButtonPosition;
   
   i = 0;
   hovers[i++] = s->HoverFlagsForTooltipMouse;
   hovers[i++] = s->HoverFlagsForTooltipNav;
`
	return
}
