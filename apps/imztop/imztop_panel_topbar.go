package imztop

import (
	"fmt"
	"time"

	"github.com/stergiotis/boxer/public/observability/sysmetrics/sysmsnap"
	c "github.com/stergiotis/boxer/public/thestack/imzero2/egui2/bindings"
)

func (inst *App) renderTopBar(snap *PublishedSnapshot, s SamplerI) {
	for range c.Horizontal().KeepIter() {
		for rt := range c.RichTextLabel("imztop") {
			rt.Strong()
		}
		c.Separator().Vertical().Send()

		// snap is nil before the first frame of any source, and for the whole
		// of a replay with nothing stored — the bar still draws, so every
		// field below has to tolerate it.
		//
		// In replay the container kind was never stored (ADR-0197 §SD8), so a
		// nil badge means "not recorded", not "bare metal".
		host := "—"
		if snap != nil {
			host = containerBadge(snap.LatestContainer)
			if inst.frameReplay.State == ReplayOn && snap.LatestContainer == nil {
				host = "not recorded"
			}
		}
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
		c.Label(fmt.Sprintf("cadence: %s", s.Interval().String())).Send()
		c.Separator().Vertical().Send()

		// Trend smoothing (ADR-0152); acts on the history plots only.
		inst.smooth.RenderControls(inst.ids)
		c.Separator().Vertical().Send()

		// Replay entry (ADR-0197). While a session is off this is the whole
		// control; the transport appears as its own row below. Rendered
		// unconditionally so the live layout never reflows around it.
		if inst.frameReplay.State == ReplayOff {
			inst.renderReplayEntry()
			c.Separator().Vertical().Send()
		}

		ts := "—"
		if snap != nil {
			ts = time.UnixMilli(snap.SampledAtUnixMs).Format("15:04:05")
		}
		c.Label(fmt.Sprintf("last: %s", ts)).Send()

		// While frozen the "last:" timestamp stops advancing; call it out
		// explicitly so a stale-but-plausible view is never mistaken for live.
		if frozen {
			c.Separator().Vertical().Send()
			for range c.IdScope(inst.ids.PrepareStr("topbar-frozen")) {
				for rt := range c.RichTextLabelColored(colorWarn, colorBgClear, "FROZEN — live updates paused") {
					rt.Strong()
				}
			}
		}

		// The same affordance for the third mode (ADR-0197 §SD8).
		if inst.frameReplay.State != ReplayOff {
			c.Separator().Vertical().Send()
			for range c.IdScope(inst.ids.PrepareStr("topbar-replaybanner")) {
				inst.replayBanner(inst.frameReplay)
			}
		}

		if snap != nil && len(snap.Errors) > 0 {
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
