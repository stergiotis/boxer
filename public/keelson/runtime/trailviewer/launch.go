package trailviewer

import (
	"strconv"
	"time"

	"github.com/stergiotis/boxer/public/keelson/runtime/app"
	"github.com/stergiotis/boxer/public/keelson/runtime/buscodec"
	"github.com/stergiotis/boxer/public/keelson/runtime/trailviewer/launchcfg"
	"github.com/stergiotis/boxer/public/observability/eh"
)

// AppId is the viewer's durable id, the target of a launch request.
const AppId = launchcfg.AppId

// launchSeed is what a launch config opens the window on.
type launchSeed struct {
	c         Case
	hasCase   bool
	w         Window
	hasWindow bool
	tab       TabE
	hasTab    bool
	view      string
}

// seedOf reads a launch config (ADR-0135). A config that does not read as
// a case, a window or a tab is refused whole, with why: the window then
// opens as a plain one and says so.
func seedOf(raw []byte) (s launchSeed, err error) {
	cfg, err := buscodec.Decode[launchcfg.TrailViewerLaunch](raw)
	if err != nil {
		return s, eh.Errorf("the launch config does not decode: %w", err)
	}
	s.view = cfg.View
	if cfg.Case != "" || cfg.View == "" {
		if s.c, err = parseCaseArgs(cfg.Case, cfg.Value); err != nil {
			return launchSeed{}, eh.Errorf("the launch config's case: %w", err)
		}
		s.hasCase = true
	}
	switch {
	case cfg.LastNanos < 0:
		return launchSeed{}, eh.Errorf("the launch config's span is negative")
	case cfg.LastNanos > 0:
		s.w, s.hasWindow = Window{Last: time.Duration(cfg.LastNanos)}, true
	case cfg.FromNanos != 0 || cfg.ToNanos != 0:
		s.w = Window{From: time.Unix(0, cfg.FromNanos).UTC(), To: time.Unix(0, cfg.ToNanos).UTC()}
		if cfg.FromNanos == 0 || cfg.ToNanos == 0 {
			return launchSeed{}, eh.Errorf("the launch config's window needs both from and to")
		}
		if err = s.w.validate(); err != nil {
			return launchSeed{}, eh.Errorf("the launch config's window: %w", err)
		}
		s.hasWindow = true
	}
	if cfg.Tab != "" {
		t, ok := ParseTab(cfg.Tab)
		if !ok {
			return launchSeed{}, eh.Errorf("the launch config's tab %q is not one of %s", cfg.Tab, tabNameList())
		}
		if s.hasCase && !t.availableFor(s.c.Kind) {
			return launchSeed{}, eh.Errorf("the launch config's tab %q is drawn for a task or a conversation case only", cfg.Tab)
		}
		s.tab, s.hasTab = t, true
	}
	return
}

// seed applies a launch config's seed before the first frame: the case
// replaces the current run the window opens on by default.
func (inst *App) seed(s launchSeed) {
	if s.hasCase {
		inst.applyCase(s.c)
		inst.inited = true
	}
	if s.hasWindow {
		inst.applyWindow(s.w)
		if s.w.Last > 0 {
			inst.picker.fromExpr, inst.picker.toExpr = lastExpr(s.w.Last), "anchor_now"
		}
	}
	if s.hasTab {
		inst.tab = s.tab
	}
	inst.views.wantOpen = s.view
}

// mountLaunch reads the window's launch config, if it has one.
func (inst *App) mountLaunch(ctx app.MountContextI) {
	raw := ctx.LaunchConfig()
	if len(raw) == 0 {
		return
	}
	s, err := seedOf(raw)
	if err != nil {
		inst.launchErr = eh.Errorf("%w; the window opened on its defaults", err)
		logger := ctx.Log()
		logger.Warn().Err(err).Msg("trailviewer: launch config ignored")
		return
	}
	inst.seed(s)
}

// lastExpr is a span ending now as the picker's from-expression, in the
// largest unit that divides it, which relativeSpan reads back.
func lastExpr(d time.Duration) string {
	for _, u := range []struct {
		d    time.Duration
		name string
	}{{24 * time.Hour, "DAY"}, {time.Hour, "HOUR"}, {time.Minute, "MINUTE"}} {
		if d%u.d == 0 {
			return "anchor_now - INTERVAL " + strconv.FormatInt(int64(d/u.d), 10) + " " + u.name
		}
	}
	return "anchor_now - INTERVAL " + strconv.FormatInt(max(int64(d/time.Second), 1), 10) + " SECOND"
}
