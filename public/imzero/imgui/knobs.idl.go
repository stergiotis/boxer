//go:build fffi_idl_code

package imgui

func Knob(label string, valueP float32, v_min float32, v_max float32) (value float32, r bool) {
	_ = `r = ImGuiKnobs::Knob(label,&valueP,v_min,v_max);
         value = valueP`
	return
}
func KnobV(label string, valueP float32, v_min float32, v_max float32,
	speed float32, format string, variant ImGuiKnobVariant, size float32, flags ImGuiKnobFlags, steps int) (value float32, r bool) {
	_ = `r = ImGuiKnobs::Knob(label,&valueP,v_min,v_max,speed,format,variant,size,flags,steps);
         value = valueP`
	return
}
func KnobInt(label string, valueP int, v_min int, v_max int) (value int, r bool) {
	_ = `r = ImGuiKnobs::KnobInt(label,&valueP,v_min,v_max);
         value = valueP`
	return
}
func KnobIntV(label string, valueP int, v_min int, v_max int,
	speed float32, format string, variant ImGuiKnobVariant, size float32, flags ImGuiKnobFlags, steps int) (value int, r bool) {
	_ = `r = ImGuiKnobs::KnobInt(label,&valueP,v_min,v_max,speed,format,variant,size,flags,steps);
         value = valueP`
	return
}
