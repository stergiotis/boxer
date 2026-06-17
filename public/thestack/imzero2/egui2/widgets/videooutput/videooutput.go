// Package videooutput is the ADR-0088 "video output" control widget: a codec
// picker for the imzero2 remote-stream pipeline. It refreshes a
// [videopipeline.Model] from the headless host's published capabilities
// (fetchVideoCapabilities — the browser-decode ∩ host-encode set), renders the
// offered codecs annotated by the viewer's decode standing, and on a change
// drives the runtime switch via bindings.SetVideoPipeline.
//
// The control is Go-owned state (ADR-0088 SD1/SD10): the caller holds the
// [videopipeline.Model] across frames (it carries the active selection). In the
// desktop host there is no remote sink, so the capabilities are empty and the
// control renders a quiet placeholder.
package videooutput

import (
	c "github.com/stergiotis/boxer/public/thestack/imzero2/egui2/bindings"
	"github.com/stergiotis/boxer/public/thestack/imzero2/videopipeline"
)

// Show refreshes the model from the host and renders the codec picker. `ids`
// scopes the widget ids (e.g. ids.PrepareStr("videoout")); `model` is owned by
// the caller.
func Show(ids *c.WidgetIdStack, model *videopipeline.Model) {
	model.Update(videopipeline.Decode(c.NewFetcher().FetchVideoCapabilities()))
	offered := model.Offered()
	if len(offered) == 0 {
		return // no remote sink / no capabilities yet — render nothing
	}
	for range c.Horizontal().KeepIter() {
		c.Label("Codec:").Send()
		for _, cc := range offered {
			id := ids.PrepareStr("codec-" + cc.Codec.String())
			checked := model.Active == cc.Codec
			if c.SelectableLabel(id, checked, codecLabel(cc)).SendResp().HasPrimaryClicked() {
				if cc.Offerable() && model.Active != cc.Codec {
					model.Active = cc.Codec
					c.SetVideoPipeline(uint32(cc.Codec))
				}
			}
		}
	}
}

// codecLabel annotates a codec with its browser decode standing so the picker
// communicates why a codec might be a poor choice without hiding it (ADR-0088
// SD8: surface the AV1-is-software case rather than letting the user pick blind).
func codecLabel(cc videopipeline.CodecCaps) string {
	name := cc.Codec.String()
	switch {
	case !cc.DecodeSupported:
		return name + " (no decode)"
	case cc.PowerEfficient:
		return name + " ⚡" // hardware-accelerated decode
	case !cc.Smooth:
		return name + " (sw)" // software decode — may be janky
	default:
		return name
	}
}
