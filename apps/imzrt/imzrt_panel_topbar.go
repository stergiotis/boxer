//go:build llm_generated_opus48

package imzrt

import (
	"fmt"
	"runtime"
	"time"

	c "github.com/stergiotis/boxer/public/thestack/imzero2/egui2/bindings"
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

		// Sampler controls.
		paused := s.IsPaused()
		pauseLabel := "Pause"
		if paused {
			pauseLabel = "Resume"
		}
		if c.Button(inst.ids.PrepareStr("topbar-pause"), c.Atoms().Text(pauseLabel).Keep()).
			Selected(paused).
			SendResp().HasPrimaryClicked() {
			s.Pause(!paused)
		}
		c.Label(fmt.Sprintf("every %s", s.IntervalLabel())).Send()
		if c.Button(inst.ids.PrepareStr("topbar-int-down"), c.Atoms().Text("−").Keep()).
			SendResp().HasPrimaryClicked() {
			s.SetInterval(s.Interval() - 500*time.Millisecond)
		}
		if c.Button(inst.ids.PrepareStr("topbar-int-up"), c.Atoms().Text("+").Keep()).
			SendResp().HasPrimaryClicked() {
			s.SetInterval(s.Interval() + 500*time.Millisecond)
		}
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
