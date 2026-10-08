package play

import (
	"strconv"
	"strings"

	"github.com/stergiotis/boxer/public/config/env"
	"github.com/stergiotis/boxer/public/observability/eh/eb"
)

// DockSplits are the fractions of the initial dock layout, one per split the
// zones make. Each is the share the existing leaf keeps when the zone's leaf
// is split off it (egui_dock's frac, DockAreaFluid.Split). Like the zones they
// are a preset: once the user drags a separator the persisted dock state wins.
type DockSplits struct {
	Editor float32 // height the editor row keeps above the body
	Tools  float32 // width the editor keeps left of the tools leaf
	Side   float32 // width the body keeps left of the side leaf
	Bottom float32 // height the body keeps above the bottom leaf
}

// DefaultDockSplits is the layout play opens with.
var DefaultDockSplits = DockSplits{Editor: 0.45, Tools: 0.55, Side: 0.70, Bottom: 0.60}

// DockSplitsOverride is the launch-time knob for the split fractions. Its use
// is a scripted capture that wants a short editor over a tall result, or a
// narrow tools leaf beside a wide editor.
var DockSplitsOverride = env.NewString(env.Spec{
	Name:        "BOXER_PLAY_DOCK_SPLITS",
	Description: "initial dock split fractions, as comma-separated zone=fraction pairs (zones: editor, tools, side, bottom; fraction in (0,1), the share the leaf split from keeps — editor=0.3 gives the editor row 30% of the height); unnamed zones keep their default; a malformed pair fails the mount",
	Category:    env.CategoryE("boxer-play"),
})

// ApplyDockSplits returns base with the fractions a BOXER_PLAY_DOCK_SPLITS
// value names replaced.
func ApplyDockSplits(base DockSplits, spec string) (out DockSplits, err error) {
	out = base
	for pair := range strings.SplitSeq(spec, ",") {
		pair = strings.TrimSpace(pair)
		if pair == "" {
			continue
		}
		zone, val, ok := strings.Cut(pair, "=")
		zone, val = strings.TrimSpace(zone), strings.TrimSpace(val)
		if !ok {
			return base, eb.Build().Str("pair", pair).Errorf("dock split pair is not zone=fraction")
		}
		f, perr := strconv.ParseFloat(val, 32)
		if perr != nil || f <= 0 || f >= 1 {
			return base, eb.Build().Str("pair", pair).Errorf("dock split fraction is not in (0,1)")
		}
		switch zone {
		case "editor":
			out.Editor = float32(f)
		case "tools":
			out.Tools = float32(f)
		case "side":
			out.Side = float32(f)
		case "bottom":
			out.Bottom = float32(f)
		default:
			return base, eb.Build().Str("pair", pair).Str("zone", zone).Errorf("unknown dock split zone")
		}
	}
	return
}

// SetDockSplits replaces the initial split fractions. Valid before the first
// Render, like the tab set: the splits are declared only when the dock state
// is first constructed.
func (inst *PlayApp) SetDockSplits(s DockSplits) { inst.dockSplits = s }
