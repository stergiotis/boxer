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
// Flow: it inspects how the buffer executes, not what it returned. And so is
// Experiments: a sink playground whose default subject is a built-in fixture
// rather than the applet's own result. And so is Vocabulary (ADR-0174): it
// lists what a buffer could call, which an applet — a published query with no
// editor — has no buffer for.
var chromeTabIDs = []string{"editor", "history", "preview", "snippets", "map", "graph", "diagnostics", "passes", "docs", "flow", "experiments", "vocabulary"}

// orderedResultTabIDs is resultTabIDs in play's registration order, for
// deterministic removal when an explicit `tabs:` list prunes the set.
//
// Every tab play registers must appear here or in chromeTabIDs — a tab in
// neither survives attenuation unconditionally, so an explicit `tabs:` list
// cannot prune it and it rides along on every applet. TestTabPolicyCoversEveryRegisteredTab
// pins that, because the failure is silent: a new panel in play just quietly
// appears in every applet window.
var orderedResultTabIDs = []string{"table", "projection", "timeline", "world", "kanban", "network", "sankey", "dist", "icicle", "series", "treemap", "chart", "schema", "detail"}

// autoOffResultTabIDs are result panels `tabs: auto` does NOT show. They are
// still listable — an applet that names one in `tabs:` gets it — so this is a
// default, not an attenuation.
//
// SD4's auto rule is "the panels whose channel negotiation accepts the executed
// shapes", and the accept/reject contract answers that at render time. It
// answers it per-frame, though, not per-applet: a panel that no applet's shape
// will ever satisfy still occupies a tab and still draws its reject. Sankey is
// that case today — it binds two convention-named CTEs (`flows`, `nodes`)
// carrying a conserved quantity, which no applet in the corpus has — so under
// auto it would be a permanently rejecting tab on every applet window.
// Distribution joins it for the same reason: its columns (`series`, `n`, `ps`,
// `qs`, `hist_*`) come out of the `distsql` macro vocabulary, so a query either
// asked for a distribution or carries none of them.
//
// Chart joins them, though the reason inverted under ADR-0172 §SD2 and is now
// the stronger one. It used to be that its lanes reading needed a column
// literally NAMED `x`, which no corpus query produces by accident. That reading
// now numbers the rows when there is no `x`, so it claims ANY result carrying a
// number — which is most of them, and would put a chart tab on applet windows
// that never asked for one. An applet that wants a chart names the tab in
// `tabs:`.
//
// Icicle is deliberately NOT here. Its node contract is `id` + `parent` +
// `value` — an ordinary hierarchy shape a recursive CTE can land on without
// meaning to, like kanban's `lane` + `title` — so it earns the same per-frame
// accept/reject the other shape panels take. Its folded contract is what a
// pprof capture already is; `bookpprof/profile-flame.md` names it explicitly.
// Series is not here either, and for the stronger version of that reason: its
// claim is TYPED (a temporal column plus any number, ADR-0163 §SD1), which many
// applet results satisfy without being written for it.
var autoOffResultTabIDs = []string{"sankey", "dist", "chart"}

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
// construction and mount: chrome removed wholesale; under `tabs: auto` the
// default-off panels removed; with an explicit `tabs:` list, unlisted result
// panels removed and node bindings applied. A failed removal (a renamed
// built-in) degrades to a warning — an applet with a stray tab beats one that
// fails to mount — while a failed binding is an error: the author asked for a
// view the instance cannot provide.
func attenuateTabs(inner *play.PlayApp, def *AppletDef, logger zerolog.Logger) (err error) {
	for _, id := range chromeTabIDs {
		if rerr := inner.Tabs().Remove(id); rerr != nil {
			logger.Warn().Err(rerr).Str("tab", id).Msg("sqlapplet: chrome tab removal failed")
		}
	}
	if len(def.Tabs) == 0 {
		// `tabs: auto` — the default-off panels are the only ones removed;
		// everything else negotiates per frame (SD4).
		for _, id := range autoOffResultTabIDs {
			if rerr := inner.Tabs().Remove(id); rerr != nil {
				logger.Warn().Err(rerr).Str("tab", id).Msg("sqlapplet: default-off tab removal failed")
			}
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
			// Placement before binding, and both before the landing tab: a
			// zone a document names is part of what it declares, so a failure
			// to apply it is an error like a failed binding rather than a
			// warning like a stray tab. The author asked for a layout the
			// instance cannot give.
			if sel.Zone != "" {
				zone, known := tabZones[sel.Zone]
				if !known {
					err = eh.Errorf("sqlapplet %s: tab %q names unknown zone %q", def.Slug, sel.ID, sel.Zone)
					return
				}
				if zErr := inner.Tabs().SetZone(sel.ID, zone); zErr != nil {
					err = eh.Errorf("sqlapplet %s: %w", def.Slug, zErr)
					return
				}
			}
			if sel.Node == "" {
				continue
			}
			if err = inner.BindTab(sel.ID, sel.Node); err != nil {
				err = eh.Errorf("sqlapplet %s: %w", def.Slug, err)
				return
			}
		}
		// The first tab a document lists is the one the document is ABOUT, so
		// that is the one that opens (ADR-0132 Update 2026-08-05). Without
		// this a fresh dock leaf activates its own first tab, which is play's
		// REGISTRATION order and starts at `table` — so a document whose whole
		// point is a treemap or a flamegraph opened on a grid of the very
		// columns that feed the picture.
		//
		// Only for an explicit list. Under `tabs: auto` there is no declared
		// order to honour, and picking one would be inventing an opinion the
		// document did not express.
		//
		// A failure is logged, not returned: every id here is a result-panel
		// slug the parser already validated and attenuation just kept, so this
		// cannot fail for a corpus document — and if it somehow does, opening
		// on the wrong tab is not worth refusing to mount over.
		if aerr := inner.ActivateTab(def.Tabs[0].ID); aerr != nil {
			logger.Warn().Err(aerr).Str("tab", def.Tabs[0].ID).
				Str("applet", def.Slug).Msg("sqlapplet: landing tab activation failed")
		}
	}
	return
}
