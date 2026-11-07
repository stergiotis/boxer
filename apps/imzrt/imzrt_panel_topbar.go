package imzrt

import (
	"fmt"
	"runtime"
	"time"

	c "github.com/stergiotis/boxer/public/thestack/imzero2/egui2/bindings"
	"github.com/stergiotis/boxer/public/thestack/imzero2/egui2/widgets/trendsmooth"
)

// The top bar's texts, as constants so a scene can read them (A5).
const (
	// labelFreeze and labelGoLive stop and restart the shared sampler for
	// every imzrt window — imztop's verbs for the same act (L3); Pause is the
	// host's act on one window (W6).
	labelFreeze = "Freeze"
	labelGoLive = "Go live"
	// tipFreeze is the hover of that button.
	tipFreeze = "Stop the sampler for every imzrt window; the plots hold their last sample. Go live restarts it."
	// intervalStep is what one click on − or + moves the sampling interval by.
	intervalStep = 500 * time.Millisecond
	// tipIntervalDown and tipIntervalUp are the hovers of the icon-only
	// interval buttons (A5). The sampler clamps to 100 ms and 60 s.
	tipIntervalDown = "Sample 500 ms more often, down to every 100 ms. Sampling runs inside the process it measures, so a shorter interval costs more of what the plots show."
	tipIntervalUp   = "Sample 500 ms less often, up to every 60 s. A longer interval smooths the plots and misses short spikes."
)

func (inst *App) renderTopBar(snap *PublishedSnapshot, s *Sampler) {
	for range c.Horizontal().KeepIter() {
		for rt := range c.RichTextLabel("imzrt") {
			rt.Strong()
		}
		c.Separator().Vertical().Send()

		// Static build facts.
		c.Label(fmt.Sprintf("%s %s/%s", runtime.Version(), runtime.GOOS, runtime.GOARCH)).Send()
		c.Separator().Vertical().Send()

		// Knobs (read-only).
		c.Label(fmt.Sprintf("GOMAXPROCS %d/%d", runtime.GOMAXPROCS(0), runtime.NumCPU())).Send()
		c.Label(fmt.Sprintf("GOGC %s", gogcLabel(snap.GOGCPercent))).Send()
		c.Label(fmt.Sprintf("limit %s", memLimitLabel(snap))).Send()
		c.Separator().Vertical().Send()

		// Live gauges.
		c.Label(fmt.Sprintf("goroutines %s", humanCount(snap.Goroutines))).Send()
		c.Label(fmt.Sprintf("live %s", humanBytes(snap.HeapLiveBytes))).Send()
		c.Label(fmt.Sprintf("GC %s", humanCount(snap.GCCyclesTotal))).Send()
		c.Separator().Vertical().Send()

		// Sampler controls. Freeze is the top bar's one action (L2).
		frozen := s.IsPaused()
		freezeLabel := labelFreeze
		if frozen {
			freezeLabel = labelGoLive
		}
		for range c.HoverText(tipFreeze).KeepIter() {
			if c.Button(inst.ids.PrepareStr("topbar-freeze"), c.Atoms().Text(freezeLabel).Keep()).
				Kind(c.ButtonKindPrimary).
				Selected(frozen).
				SendResp().HasPrimaryClicked() {
				s.Pause(!frozen)
			}
		}
		c.Label(fmt.Sprintf("every %s", s.IntervalLabel())).Send()
		for range c.HoverText(tipIntervalDown).KeepIter() {
			if c.Button(inst.ids.PrepareStr("topbar-int-down"), c.Atoms().Text("−").Keep()).
				SendResp().HasPrimaryClicked() {
				s.SetInterval(s.Interval() - intervalStep)
			}
		}
		for range c.HoverText(tipIntervalUp).KeepIter() {
			if c.Button(inst.ids.PrepareStr("topbar-int-up"), c.Atoms().Text("+").Keep()).
				SendResp().HasPrimaryClicked() {
				s.SetInterval(s.Interval() + intervalStep)
			}
		}
		c.Separator().Vertical().Send()

		// Trend smoothing (ADR-0152); acts on the rate/latency plots only.
		trendsmooth.Render(trendsmooth.Input{Ids: inst.ids, ScopeKey: "trendsmooth", State: inst.smooth})
		c.Separator().Vertical().Send()

		ts := time.UnixMilli(snap.SampledAtUnixMs).Format("15:04:05")
		c.Label(fmt.Sprintf("last %s", ts)).Send()

		// Observer-effect disclosure: imzrt runs inside the process it measures,
		// so its own sampling/rendering perturbs these very numbers (ADR-0061 SD7).
		c.Separator().Vertical().Send()
		for rt := range c.RichTextLabelColored(colorWarn, colorBgClear, "⊙ self-measured") {
			rt.Weak()
		}

		if snap.MissingMetrics > 0 {
			c.Separator().Vertical().Send()
			c.Label(fmt.Sprintf("%d metric(s) n/a", snap.MissingMetrics)).Send()
		}
	}
}

// gogcLabel renders /gc/gogc:percent, mapping the disabled state (GOGC=off, which
// surfaces as a sentinel) and an absent metric to "off".
func gogcLabel(v uint64) (s string) {
	if v == 0 || v >= 1<<63 {
		s = "off"
		return
	}
	s = fmt.Sprintf("%d%%", v)
	return
}

func memLimitLabel(snap *PublishedSnapshot) (s string) {
	if snap.MemLimitSet() {
		s = humanBytes(snap.GOMemLimitBytes)
		return
	}
	s = "off"
	return
}
