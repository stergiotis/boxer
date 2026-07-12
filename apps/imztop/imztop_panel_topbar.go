package imztop

import (
	"fmt"
	"time"

	"github.com/stergiotis/boxer/public/observability/sysmetrics/sysmsnap"
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

		// Global freeze / disable-live. Sampler.Pause drops incoming frames so
		// every panel renders the frozen snapshot; the sampler is a process
		// singleton, so one click freezes live data across all imztop panels
		// (and windows) at once.
		frozen := s.IsPaused()
		freezeLabel := "Freeze"
		if frozen {
			freezeLabel = "Go live"
		}
		if c.Button(inst.ids.PrepareStr("topbar-freeze"), c.Atoms().Text(freezeLabel).Keep()).
			Selected(frozen).
			SendResp().HasPrimaryClicked() {
			s.Pause(!frozen)
		}
		c.Separator().Vertical().Send()

		// Observed sample cadence (the scraper's rate). imztop is a pure
		// consumer and cannot change it (ADR-0090 SD5), so there is no control.
		c.Label(fmt.Sprintf("cadence: %s", s.IntervalLabel())).Send()
		c.Separator().Vertical().Send()

		ts := time.UnixMilli(snap.SampledAtUnixMs).Format("15:04:05")
		c.Label(fmt.Sprintf("last: %s", ts)).Send()

		// While frozen the "last:" timestamp stops advancing; call it out
		// explicitly so a stale-but-plausible view is never mistaken for live.
		if frozen {
			c.Separator().Vertical().Send()
			for rt := range c.RichTextLabelColored(colorWarn, colorBgClear, "FROZEN — live updates paused") {
				rt.Strong()
			}
		}

		if len(snap.Errors) > 0 {
			c.Separator().Vertical().Send()
			for rt := range c.RichTextLabelColored(colorHot, colorBgClear, fmt.Sprintf("⚠ %d collector error(s)", len(snap.Errors))) {
				rt.Strong()
			}
		}
	}
}

func containerBadge(info *sysmsnap.ContainerInfo) (out string) {
	if info == nil || info.Engine == sysmsnap.EngineNone {
		out = "bare metal"
		return
	}
	out = info.Engine.String()
	if info.Detail != "" {
		out = fmt.Sprintf("%s (%s)", out, trimTo(info.Detail, 24))
	}
	return
}
