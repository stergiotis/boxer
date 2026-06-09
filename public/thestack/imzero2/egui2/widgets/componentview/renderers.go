package componentview

import (
	"github.com/stergiotis/boxer/public/keelson/designsystem/styletokens"
	c "github.com/stergiotis/boxer/public/thestack/imzero2/egui2/bindings"
	"github.com/stergiotis/boxer/public/thestack/imzero2/egui2/widgets/badge"
	"github.com/stergiotis/boxer/public/thestack/imzero2/egui2/widgets/gauge"
)

// batteryMaxMAh is the gauge full-scale for drone battery telemetry (mAh).
const batteryMaxMAh = 10_000.0

var (
	_ RendererI = identityRenderer{}
	_ RendererI = batteryRenderer{}
	_ RendererI = taskedRenderer{}
)

// DefaultRegistry returns a Registry seeded with the light-version drone
// renderers in archetype order: identity, battery, tasked.
func DefaultRegistry() (inst *Registry) {
	inst = NewRegistry()
	inst.Register(identityRenderer{})
	inst.Register(batteryRenderer{})
	inst.Register(taskedRenderer{})
	return
}

// identityRenderer shows the drone status as a toned pill badge.
type identityRenderer struct{}

func (identityRenderer) Kind() ComponentKindE { return KindIdentity }
func (identityRenderer) Title() string        { return "identity" }
func (identityRenderer) Render(ids *c.WidgetIdStack, value any) {
	v, _ := value.(IdentityVal)
	tone := badge.ToneNeutral
	switch v.Status {
	case "IN_TRANSIT":
		tone = badge.ToneInfo
	case "DELIVERED":
		tone = badge.ToneSuccess
	case "WEATHER_DELAY":
		tone = badge.ToneWarning
	}
	label := v.Status
	if label == "" {
		label = "∅"
	}
	badge.New(ids.PrepareStr("status"), label).Tone(tone).Variant(badge.VariantSoft).Pill().Send()
}

// batteryRenderer shows the battery charge on a radial charge gauge.
type batteryRenderer struct{}

func (batteryRenderer) Kind() ComponentKindE { return KindBattery }
func (batteryRenderer) Title() string        { return "battery" }
func (batteryRenderer) Render(ids *c.WidgetIdStack, value any) {
	v, _ := value.(BatteryVal)
	gauge.New("battery").
		Range(0, batteryMaxMAh).
		Label("battery").
		Diameter(115).
		Suffix(" mAh").
		ZoneMode(gauge.ZonePercentage).
		Zones(batteryChargeZones()...).
		Render(ids.PrepareStr("dial"), float64(v.Charge))
}

// batteryChargeZones are charge-appropriate gauge bands. A generic
// gauge.TrafficLight reads low as "ok" (high as critical), which is backwards
// for a battery: a full pack is healthy and a flat one is the problem. So the
// zones run the other way — red below 20%, amber 20–50%, green above 50% — and
// are expressed in percentage mode so they track any Range. A near-full pack
// then reads green, a near-empty one red.
func batteryChargeZones() []gauge.Zone {
	return []gauge.Zone{
		{From: 0.0, To: 0.2, Tone: styletokens.ToneError, Label: "low"},
		{From: 0.2, To: 0.5, Tone: styletokens.ToneWarning, Label: "fair"},
		{From: 0.5, To: 1.0, Tone: styletokens.ToneSuccess, Label: "ok"},
	}
}

// taskedRenderer shows the mission tags as outline pill chips.
type taskedRenderer struct{}

func (taskedRenderer) Kind() ComponentKindE { return KindTasked }
func (taskedRenderer) Title() string        { return "tasked" }
func (taskedRenderer) Render(ids *c.WidgetIdStack, value any) {
	v, _ := value.(TaskedVal)
	if len(v.Tags) == 0 {
		for rt := range c.RichTextLabel("no tags") {
			rt.Weak().Italics().Small()
		}
		return
	}
	for range c.Horizontal().KeepIter() {
		for i, tag := range v.Tags {
			badge.New(ids.PrepareSeq(uint64(i)), tag).
				Tone(badge.ToneNeutral).
				Variant(badge.VariantOutline).
				Size(badge.SizeSm).
				Pill().
				Monospace().
				Send()
		}
	}
}
