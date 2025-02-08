//go:build fffi_idl_code

package imgui

func TableGetSortSpecs() (sort bool, dirty bool, userIds []ImGuiID, columnIndices []int16, directions []ImGuiSortDirection) {
	_ = `
		auto spec = ImGui::TableGetSortSpecs();
		size_t userIds_len = 0;
        size_t columnIndices_len = 0;
        size_t directions_len = 0;
        if(spec == nullptr) {
            sort = false;
            dirty = false;
        } else {
            sort = true;
            dirty = spec->SpecsDirty;
			userIds_len = spec->SpecsCount;
            columnIndices_len = userIds_len;
            directions_len = userIds_len;
            userIds = (ImGuiID*)arenaMalloc(userIds_len*sizeof(ImGuiID));
            columnIndices = (int16_t*)arenaMalloc(userIds_len*sizeof(int16_t));
            directions = (uint8_t*)arenaMalloc(userIds_len*sizeof(ImGuiSortDirection));
            for(size_t i=0;i<userIds_len;i++) {
               auto s = spec->Specs[i];
               userIds[i] = s.ColumnUserID;
               columnIndices[i] = s.ColumnIndex;
               directions[i] = s.SortDirection;
            }
        }
`
	return
}

func TableSetSortSpecsDirty(dirty bool) {
	_ = `auto spec = ImGui::TableGetSortSpecs();
        if(spec == nullptr){ return; }
        spec->SpecsDirty = dirty;
`
}
func TableNextColumnP() {
	_ = `ImGui::TableNextColumn()`
	return
}
