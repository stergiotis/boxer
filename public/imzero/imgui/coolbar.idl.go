//go:build fffi_idl_code

package imgui

func MakeImCoolBarConfig() (r ImCoolBarConfigForeignPtr) {
	_ = `r = (uintptr_t)(new ImGui::ImCoolBarConfig())`
	return
}

func MakeImCoolBarConfigV(anchor ImVec2, normalSize float32, hoveredSize float32, animStep float32, effectStrength float32) (r ImCoolBarConfigForeignPtr) {
	_ = `r = (uintptr_t)(new ImGui::ImCoolBarConfig(anchor,normalSize,hoveredSize,animStep,effectStrength))`
	return
}

func DestroyImCoolBarConfig(cfg ImCoolBarConfigForeignPtr) {
	_ = `delete ((ImGui::ImCoolBarConfig*)cfg)`
}

func (foreignptr ImCoolBarConfigForeignPtr) Get() (anchor ImVec2, normalSize float32, hoveredSize float32, animStep float32, effectStrength float32) {
	_ = `
auto t = (ImGui::ImCoolBarConfig*)foreignptr;
auto anchor = t->anchor;
normalSize = t->normal_size;
hoveredSize = t->hovered_size;
animStep = t->anim_step;
effectStrength = t->effect_strength;
`
	return
}

func (foreignptr ImCoolBarConfigForeignPtr) Set(anchor ImVec2, normalSize float32, hoveredSize float32, animStep float32, effectStrength float32) {
	_ = `
auto t = (ImGui::ImCoolBarConfig*)foreignptr;
t->anchor = anchor;
t->normal_size = normalSize;
t->hovered_size = hoveredSize;
t->anim_step = animStep;
t->effect_strength = effectStrength;
`
}

func BeginCoolBar(label string) (r bool) {
	_ = `r = ImGui::BeginCoolBar(label)`
	return
}

func BeginCoolBarV(label string, flags ImCoolBarFlags, cfg ImCoolBarConfigForeignPtr, windowFlags ImGuiWindowFlags) (r bool) {
	_ = `r = ImGui::BeginCoolBar(label, flags, *((ImGui::ImCoolBarConfig*)cfg), windowFlags)`
	return
}

func EndCoolBar() {
	_ = `ImGui::EndCoolBar()`
}

func CoolBarItem() (r bool) {
	_ = `r = ImGui::CoolBarItem()`
	return
}

func CoolBarItemProperties() (width float32, scale float32) {
	_ = `
width = ImGui::GetCoolBarItemWidth();
scale = ImGui::GetCoolBarItemScale();
`
	return
}

func CoolBarButtons(fontPtr ImFontPtr, labels []string, tooltips []string) (clickedIndex int, hoveredIndex int) {
	_ = `
auto l = (int)std::min(getSliceLength(labels),getSliceLength(tooltips));
clickedIndex = -1;
hoveredIndex = -1;
ImGuiWindow* window = ImGui::GetCurrentWindow();
if (window->SkipItems) { return; } // GetCoolBarItemScale() will return 0.0f if window is non-visible
if(l > 0) {
   auto font_ptr = (ImFont*)fontPtr;
   if(font_ptr == nullptr) {
      if(ImGui::GetIO().Fonts == nullptr) {
          return;
      }
      font_ptr = ImGui::GetIO().Fonts->Fonts[0];
      if(font_ptr == nullptr) {
          return;
      }
   }
   auto saved_scale = font_ptr->Scale;
   for(int i=0;i<l;i++) {
       if(ImGui::CoolBarItem()) {
   	      float w = ImGui::GetCoolBarItemWidth();
          ImGui::PushFont(font_ptr);
   	      auto s = ImGui::GetCoolBarItemScale();
          if(s > 0.0f) {
			  font_ptr->Scale = s;
          }
          auto label = labels[i];
   	      if(ImGui::Button(label, ImVec2(w, w))) {
   	      	  clickedIndex = i;
   	      }
          ImGui::PopFont();

          if(ImGui::IsItemHovered(ImGuiHoveredFlags_ForTooltip) && ImGui::BeginTooltip()) {
              ImGui::TextUnformatted(tooltips[i],tooltips[i]+getStringLength(tooltips[i]));
              ImGui::EndTooltip();
          }
      }
   }
   font_ptr->Scale = saved_scale;
}
`
	return
}
