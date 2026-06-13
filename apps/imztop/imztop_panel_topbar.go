package imztop

import (
	"fmt"
	"time"

	"github.com/stergiotis/boxer/public/observability/sysmetrics/container"
	c "github.com/stergiotis/boxer/public/thestack/imzero2/egui2/bindings"
)

func (inst *App) renderTopBar(snap *PublishedSnapshot, s *Sampler) {
	for range c.Horizontal().KeepIter() {
		for rt := range c.RichTextLabel("imztop") {
			rt.Strong()
		}
		c.Separator().Vertical().Send()

		host := containerBadge(snap.LatestContainer)
		c.Label(fmt.Sprintf("host: %s", host)).Send()
		c.Separator().Vertical().Send()

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
		c.Separator().Vertical().Send()

		c.Label(fmt.Sprintf("interval: %s", s.IntervalLabel())).Send()
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
		c.Label(fmt.Sprintf("last: %s", ts)).Send()

		if len(snap.Errors) > 0 {
			c.Separator().Vertical().Send()
			for rt := range c.RichTextLabelColored(colorHot, colorBgClear, fmt.Sprintf("⚠ %d collector error(s)", len(snap.Errors))) {
				rt.Strong()
			}
		}
	}
}

func containerBadge(info *container.Info) (out string) {
	if info == nil || info.Engine == container.EngineNone {
		out = "bare metal"
		return
	}
	out = info.Engine.String()
	if info.Detail != "" {
		out = fmt.Sprintf("%s (%s)", out, trimTo(info.Detail, 24))
	}
	return
}
