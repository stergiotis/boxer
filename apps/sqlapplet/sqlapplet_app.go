package sqlapplet

import (
	"github.com/rs/zerolog"
	"github.com/stergiotis/boxer/apps/play"
	"github.com/stergiotis/boxer/public/keelson/runtime/app"
	"github.com/stergiotis/boxer/public/observability/eh"
)

// appletMaxHistory bounds each lane's result-history ring. Applets hide the
// History tab, so the ring only serves last-good-while-loading; keep it
// small next to the launcher's 100.
const appletMaxHistory = 25

// chromeTabIDs are the exploration-chrome tabs an applet removes pre-mount
// (ADR-0132 §SD3). The result panels and the status bar stay; the dock
// handles an emptied editor zone (ADR-0097 slice 6a).
var chromeTabIDs = []string{"editor", "history", "preview", "snippets", "map", "graph", "diagnostics", "passes"}

// orderedResultTabIDs is resultTabIDs in play's registration order, for
// deterministic removal when an explicit `tabs:` list prunes the set.
var orderedResultTabIDs = []string{"table", "projection", "timeline", "world", "kanban", "network", "schema", "detail"}

// appletApp is the minted AppI: a fresh attenuated PlayApp per open window
// (factory dispatch), built in Mount so env-configured connection details
// bind late — the PlayLauncher precedent.
type appletApp struct {
	def *AppletDef
	m   app.Manifest

	inner *play.PlayApp
}

var _ app.AppI = (*appletApp)(nil)

func (inst *appletApp) Manifest() (m app.Manifest) {
	m = inst.m
	return
}

func (inst *appletApp) Mount(ctx app.MountContextI) (err error) {
	// The minted per-applet id rides the log_comment stamp, so captured
	// query runs attribute to the applet, not to a shared host (ADR-0132
	// §SD9 over ADR-0115). A standalone applet has no ad-hoc dataset
	// bindings; those arrive only through an embedder (ADR-0134 §SD7).
	inner, err := NewEmbedded(inst.def, EmbedConfig{
		StampAppId: string(inst.m.Id),
		RunId:      ctx.RunId(),
		Bus:        ctx.Bus(),
		Log:        ctx.Log(),
	})
	if err != nil {
		return
	}
	inst.inner = inner
	return
}

func (inst *appletApp) Frame(ctx app.FrameContextI) (err error) {
	if inst.inner == nil {
		err = eh.Errorf("sqlapplet %s: Frame called before Mount", inst.def.Slug)
		return
	}
	err = inst.inner.Render()
	return
}

func (inst *appletApp) Unmount(ctx app.MountContextI) (err error) {
	if inst.inner != nil {
		inst.inner.Close()
	}
	inst.inner = nil
	return
}

// attenuateTabs applies the ADR-0132 §SD3/§SD4 tab surface between
// construction and mount: chrome removed wholesale; with an explicit `tabs:`
// list, unlisted result panels removed and node bindings applied. A failed
// chrome removal (a renamed built-in) degrades to a warning — an applet with
// a stray tab beats one that fails to mount — while a failed binding is an
// error: the author asked for a view the instance cannot provide.
func attenuateTabs(inner *play.PlayApp, def *AppletDef, logger zerolog.Logger) (err error) {
	for _, id := range chromeTabIDs {
		if rerr := inner.Tabs().Remove(id); rerr != nil {
			logger.Warn().Err(rerr).Str("tab", id).Msg("sqlapplet: chrome tab removal failed")
		}
	}
	if len(def.Tabs) > 0 {
		keep := make(map[string]struct{}, len(def.Tabs))
		for _, sel := range def.Tabs {
			keep[sel.ID] = struct{}{}
		}
		for _, id := range orderedResultTabIDs {
			if _, keepIt := keep[id]; keepIt {
				continue
			}
			if rerr := inner.Tabs().Remove(id); rerr != nil {
				logger.Warn().Err(rerr).Str("tab", id).Msg("sqlapplet: result tab removal failed")
			}
		}
		for _, sel := range def.Tabs {
			if sel.Node == "" {
				continue
			}
			if err = inner.BindTab(sel.ID, sel.Node); err != nil {
				err = eh.Errorf("sqlapplet %s: %w", def.Slug, err)
				return
			}
		}
	}
	return
}
