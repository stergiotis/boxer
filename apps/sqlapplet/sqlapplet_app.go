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
// handles an emptied editor zone (ADR-0097 slice 6a). Docs is chrome too:
// its follow-caret half reads the editor the applet just removed. So is
// Flow: it inspects how the buffer executes, not what it returned. And so is
// Experiments: a sink playground whose default subject is a built-in fixture
// rather than the applet's own result. And so is Vocabulary (ADR-0174): it
// lists what a buffer could call, which an applet — a published query with no
// editor — has no buffer for. Glosses (ADR-0186) is chrome for the same
// reason: it explains how a buffer's rules resolved, an authoring view; the
// glosses themselves still render in an applet's Table and Detail.
var chromeTabIDs = []string{"editor", "history", "preview", "snippets", "map", "graph", "diagnostics", "passes", "docs", "flow", "experiments", "vocabulary", "glosses"}

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
	// binder keeps the declared dataset aliases bound to live datasets for
	// the life of the window and carries the notice the window shows while
	// one is missing (sqlapplet_datasets.go). nil for the dataset-less
	// applet, which is most of them.
	binder *datasetBinder
}

var _ app.AppI = (*appletApp)(nil)

func (inst *appletApp) Manifest() (m app.Manifest) {
	m = inst.m
	return
}

func (inst *appletApp) Mount(ctx app.MountContextI) (err error) {
	// The minted per-applet id rides the log_comment stamp, so captured
	// query runs attribute to the applet, not to a shared host (ADR-0132
	// §SD9 over ADR-0115). Declared `datasets:` aliases are kept bound to
	// live datasets for the life of the window by the binder
	// (sqlapplet_datasets.go): it subscribes to the service's events, then
	// resolves each alias at open to the newest live dataset (ADR-0134
	// §SD4); a miss stays pending, a later withdrawal unbinds (ADR-0188
	// §SD3), and the window says what it is waiting for in either case. An
	// embedder still overrides all of that by binding explicit handles
	// instead (§SD7).
	binder, bindings := newDatasetBinder(ctx.Bus(), ctx.Log(), inst.def.DatasetsHint, inst.def.Datasets)
	inner, err := NewEmbedded(inst.def, EmbedConfig{
		StampAppId: string(inst.m.Id),
		RunId:      ctx.RunId(),
		Bus:        ctx.Bus(),
		Log:        ctx.Log(),
		Bindings:   bindings,
		Rules:      bookRepository,
	})
	if err != nil {
		if binder != nil {
			binder.close()
		}
		return
	}
	inst.inner = inner
	inst.binder = binder
	return
}

func (inst *appletApp) Frame(ctx app.FrameContextI) (err error) {
	if inst.inner == nil {
		err = eh.Errorf("sqlapplet %s: Frame called before Mount", inst.def.Slug)
		return
	}
	if inst.binder != nil {
		bound, notice, noticeChanged := inst.binder.sync(inst.inner)
		if noticeChanged {
			inst.inner.SetDatasetNotice(notice)
		}
		if bound {
			// AutoRun already fired against the unbound buffer at open, so
			// the newly bound alias needs its own run to become visible.
			inst.inner.RequestRun()
		}
	}
	// Frame, not the bare render pass: the engine reads the window-focus gate
	// and the column-width store off this context, and an applet window is
	// entitled to both. Its own manifest id keys the widths, so one applet's
	// columns never reach another's.
	err = inst.inner.Frame(ctx)
	return
}

func (inst *appletApp) Unmount(ctx app.MountContextI) (err error) {
	if inst.inner != nil {
		inst.inner.Close()
	}
	inst.inner = nil
	// Releasing the binder drops its events subscription; a poll that is
	// still in flight finishes against a closed window, parks a result
	// nobody reads, and is collected with the struct.
	if inst.binder != nil {
		inst.binder.close()
		inst.binder = nil
	}
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
