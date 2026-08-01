package sqlapplet

import (
	"github.com/rs/zerolog"
	"github.com/stergiotis/boxer/apps/play"
	"github.com/stergiotis/boxer/public/keelson/runtime/adhocdata"
	"github.com/stergiotis/boxer/public/keelson/runtime/app"
	"github.com/stergiotis/boxer/public/observability/eh"
)

// appletMaxHistory bounds each lane's result-history ring. Applets hide the
// History tab, so the ring only serves last-good-while-loading; keep it
// small next to the launcher's 100.
const appletMaxHistory = 25

// chromeTabIDs are the exploration-chrome tabs an applet removes pre-mount
// (ADR-0132 §SD3). The result panels and the status bar stay; the dock
// handles an emptied editor zone (ADR-0097 slice 6a). Docs is chrome too:
// its follow-caret half reads the editor the applet just removed. So is
// Flow: it inspects how the buffer executes, not what it returned.
var chromeTabIDs = []string{"editor", "history", "preview", "snippets", "map", "graph", "diagnostics", "passes", "docs", "flow"}

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
	// rebind retries the declared dataset aliases that missed at open, and
	// carries the notice the window shows meanwhile (sqlapplet_datasets.go).
	// nil once every alias is bound — which for most applets is from the
	// start, and for all of them eventually.
	rebind *datasetRebinder
}

var _ app.AppI = (*appletApp)(nil)

func (inst *appletApp) Manifest() (m app.Manifest) {
	m = inst.m
	return
}

func (inst *appletApp) Mount(ctx app.MountContextI) (err error) {
	// The minted per-applet id rides the log_comment stamp, so captured
	// query runs attribute to the applet, not to a shared host (ADR-0132
	// §SD9 over ADR-0115). Declared `datasets:` aliases resolve here first,
	// at open, to the newest live dataset published under each (ADR-0134
	// §SD4, update 2026-08-01); whatever misses is retried from Frame rather
	// than stranding the window (sqlapplet_datasets.go). An embedder still
	// overrides both by binding explicit handles instead (§SD7).
	bindings, unresolved := resolveDatasetAliases(ctx.Bus(), ctx.Log(), inst.def.Datasets)
	inner, err := NewEmbedded(inst.def, EmbedConfig{
		StampAppId: string(inst.m.Id),
		RunId:      ctx.RunId(),
		Bus:        ctx.Bus(),
		Log:        ctx.Log(),
		Bindings:   bindings,
	})
	if err != nil {
		return
	}
	inst.inner = inner
	// What missed at open is retried from Frame until it lands, and the
	// window says what it is waiting for until then (sqlapplet_datasets.go).
	inst.rebind = newDatasetRebinder(ctx.Bus(), ctx.Log(), inst.def.DatasetsHint, unresolved)
	return
}

// resolveDatasetAliases maps each declared alias to the newest live
// dataset published under it, and returns the ones that missed. A miss binds
// nothing rather than failing the mount: the applet still opens, and the
// caller retries the misses after open rather than stranding the window on
// the wrong side of a capture-then-open ordering. Blocking bus round-trips in
// Mount follow the adhocdemo precedent — Mount is not the frame loop; the
// loop is empty for the common dataset-less applet.
func resolveDatasetAliases(bus app.BusI, logger zerolog.Logger, aliases []string) (bindings map[string]string, unresolved []string) {
	if len(aliases) == 0 || bus == nil {
		return nil, nil
	}
	bindings = make(map[string]string, len(aliases))
	for _, alias := range aliases {
		res, err := adhocdata.ResolveRequest(bus, alias)
		if err != nil {
			logger.Warn().Err(err).Str("alias", alias).Msg("sqlapplet: dataset alias unresolved at open")
			unresolved = append(unresolved, alias)
			continue
		}
		bindings[alias] = res.Handle
	}
	return
}

func (inst *appletApp) Frame(ctx app.FrameContextI) (err error) {
	if inst.inner == nil {
		err = eh.Errorf("sqlapplet %s: Frame called before Mount", inst.def.Slug)
		return
	}
	if inst.rebind != nil {
		bound, notice, noticeChanged, done := inst.rebind.sync(inst.inner.BindDataset)
		if noticeChanged {
			inst.inner.SetDatasetNotice(notice)
		}
		if bound {
			// AutoRun already fired against the unbound buffer at open, so
			// the newly bound alias needs its own run to become visible.
			inst.inner.RequestRun()
		}
		if done {
			inst.rebind = nil
		}
	}
	err = inst.inner.Render()
	return
}

func (inst *appletApp) Unmount(ctx app.MountContextI) (err error) {
	if inst.inner != nil {
		inst.inner.Close()
	}
	inst.inner = nil
	// Dropping the rebinder starts no further attempts. One may still be in
	// flight; it finishes against a closed window, parks a result nobody
	// reads, and is collected with the struct.
	inst.rebind = nil
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
