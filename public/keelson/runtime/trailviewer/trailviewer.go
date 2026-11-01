// Package trailviewer is the trail viewer (ADR-0350, proposed): a runtime
// system app that reads the trail marts on boxer.facts by case — a run, a
// window, an app, a conversation, a task, a data subject, a principal, an
// entity, an egress destination, or every row — and time window, into a
// context band and a detail strip over the window (navigation.go), a
// windowed table, a facets pane and a detail pane with the
// identity chain, the rows around the selected one, and for an audit event
// or a grant the previous-state diff. Above the table one verdict line per
// writer run says whether the run's sealed record holds. Named exception
// rules raise a reading list beside the table (rules.go), a person marks a
// row reviewed, flagged or dismissed back onto the trail (marks.go), and
// two rows can be compared field by field (compare.go). In the table's
// place the window draws a task's or conversation's tree (tree.go), the
// actor × object matrix or the dotted chart (analytics.go); it keeps saved
// views in the persist store (views.go), sends the table to play
// (sendplay.go), and opens on a case a launch config names (launch.go).
//
// The window never queries the store from a frame: a bgjob worker reads
// through the host's executor (cache.go, probe.go) and a later frame
// adopts the result. Without ClickHouse the window draws one line saying
// the trail is not persisted, and why. The source is shared (one per
// process); Register is the host's entry point.
package trailviewer

import (
	"fmt"
	"math"
	"strconv"
	"strings"
	"time"

	"github.com/apache/arrow-go/v18/arrow"
	"github.com/apache/arrow-go/v18/arrow/array"
	"github.com/apache/arrow-go/v18/arrow/memory"

	"github.com/stergiotis/boxer/public/keelson/designsystem/styletokens"
	"github.com/stergiotis/boxer/public/keelson/runtime/app"
	"github.com/stergiotis/boxer/public/keelson/runtime/appops"
	"github.com/stergiotis/boxer/public/keelson/runtime/appops/opwire"
	"github.com/stergiotis/boxer/public/keelson/runtime/trail"
	"github.com/stergiotis/boxer/public/keelson/runtime/trailviewer/verdict"
	"github.com/stergiotis/boxer/public/keelson/runtime/widgethandle"
	"github.com/stergiotis/boxer/public/storage/recordstore/evidence/audit"
	c "github.com/stergiotis/boxer/public/thestack/imzero2/egui2/bindings"
	"github.com/stergiotis/boxer/public/thestack/imzero2/egui2/widgets/badge"
	"github.com/stergiotis/boxer/public/thestack/imzero2/egui2/widgets/color"
	"github.com/stergiotis/boxer/public/thestack/imzero2/egui2/widgets/fieldview"
	"github.com/stergiotis/boxer/public/thestack/imzero2/egui2/widgets/fsmview"
	"github.com/stergiotis/boxer/public/thestack/imzero2/egui2/widgets/jobprogress"
	"github.com/stergiotis/boxer/public/thestack/imzero2/egui2/widgets/recordtable"
	"github.com/stergiotis/boxer/public/thestack/imzero2/egui2/widgets/timeline"
	"github.com/stergiotis/boxer/public/thestack/imzero2/egui2/widgets/timerangepicker"
	"github.com/stergiotis/boxer/public/thestack/imzero2/egui2/widgets/timerangepicker/presets"
	"github.com/stergiotis/boxer/public/thestack/imzero2/egui2/widgets/timescrubber"
)

const (
	detailPanelHeight = 260.0
	facetsPanelWidth  = 260.0
	// stripFallbackWidth sizes the band and the detail strip before the
	// pane probe answers.
	stripFallbackWidth = 720.0
	// minTopPanelWidth is the narrowest width the top panel's probe is
	// trusted to report (measureTopPanel).
	minTopPanelWidth = 200.0
	// stripKeys bounds the strip's series; the rest are summed as other,
	// since the qualitative cycle has seven hues.
	stripKeys = 6
)

var (
	detailMutedFg = color.Hex(styletokens.NeutralTextSecondary.AsHex())
	detailErrorFg = color.Hex(styletokens.ErrorDefault.AsHex())
	detailBgClear = color.Transparent
)

// App is one trail viewer window.
type App struct {
	manifest app.Manifest
	ids      *c.WidgetIdStack
	density  styletokens.DensityE

	frameCtx app.FrameContextI
	bus      app.BusI
	appId    string
	instance uint64
	inited   bool

	// The view: the case, the window, the filter, the strip's colouring,
	// the detail pane's tab. caseValue is the applied case; the combo and
	// the value box are its draft until Apply.
	caseValue  Case
	draftKind  CaseKindE
	draftValue string
	draftStale bool
	window     Window
	filter     Filter
	strip      StripByE
	tab        TabE
	grouped    bool

	// selected is a copy of the selected row, kept by facts id so it
	// survives a refill.
	selected    Row
	hasSelected bool

	cache    tableCache
	verdicts verdictCache
	detail   detailCache
	picker   pickerState
	// readFSM mirrors the table read's lifecycle (readstate.go); readView
	// is its chip, built at the first render since it needs the mounted
	// id stack.
	readFSM  *fsmview.Machine[readStateE]
	readView *fsmview.View[readStateE]

	valueH widgethandle.WidgetHandle

	// side is what the pane beside the table shows.
	side sideE
	// The review marks written and not read back yet, the exceptions with
	// the marks applied, and the detail pane's mark controls.
	pending     map[uint64]Review
	pendingVer  uint64
	marksMerged map[uint64]Review
	marksKey    marksKey
	marksBuilt  bool
	exc         exceptionSet
	excKey      excKey
	excBuilt    bool
	excVer      uint64
	markReason  string
	markErr     error
	markReached bool
	// compare is the second row the detail pane compares the selected one
	// with; picking says the next table click picks it.
	compare    Row
	hasCompare bool
	picking    bool

	// The grid's record and state; the record is rebuilt when the cache's
	// generation or the exceptions move.
	grid   gridState
	stripW float32

	// Time navigation (navigation.go): the context extent the reads use,
	// the windows Back returns to and the one it is returning to, the
	// context band with what it last mirrored, and the detail strip.
	ctx           contextState
	history       windowHistory
	backTarget    Window
	hasBackTarget bool
	band          *timescrubber.Scrubber
	bandSteps     []timescrubber.Step
	bandMirror    bandRange
	bandWindow    Window
	bandGen       uint64
	bandSeekId    uint64
	detailTl      *timeline.Timeline
	detailVis     timeline.Visuals
	detailGen     uint64
	// paneSeq keys the top panel's width probe (measureTopPanel), derived
	// at the window's scope once per frame.
	paneSeq struct{ top uint64 }

	// The analytic views (analytics.go, tree.go), the saved views
	// (views.go), the export to play (sendplay.go), and why a launch
	// config was ignored (launch.go).
	treeV     treeView
	an        analytics
	views     viewStore
	send      playSend
	launchErr error
	// lastDetail is the detail tab the bar's Table button returns to.
	lastDetail TabE
	fvState    fieldview.State
	drawnGen   uint64
	settleRem  int
}

var _ app.AppI = (*App)(nil)

func newInstance(m app.Manifest) (out *App) {
	out = &App{
		manifest: m,
		ids:      c.NewWidgetIdStack(),
		density:  styletokens.ActiveDensity(),
		window:   defaultWindow,
		stripW:   stripFallbackWidth,
		picker:   newPickerState(),
		readFSM:  newReadFSM(),
		// The matrix opens on the pair every row has.
		an: analytics{pair: matrixPick{Actor: ActorApp, Object: ObjectOperation}},
	}
	return
}

func (inst *App) Manifest() (m app.Manifest) { m = inst.manifest; return }

func (inst *App) Mount(ctx app.MountContextI) (err error) {
	inst.ids = ctx.Ids()
	inst.bus = ctx.Bus()
	inst.appId = string(ctx.AppId())
	inst.instance = ctx.InstanceKey()
	inst.picker.runner.bus = inst.bus
	inst.views.store = ctx.Storage()
	inst.mountLaunch(ctx)
	return
}

func (inst *App) Unmount(ctx app.MountContextI) (err error) {
	inst.cache.close()
	inst.verdicts.close()
	inst.detail.close()
	inst.picker.close()
	inst.grid.release()
	inst.an.close()
	inst.views.runner.Invalidate()
	inst.send.runner.Invalidate()
	return
}

// ensureInit opens the window on the current run once it is known, every
// row otherwise.
func (inst *App) ensureInit() {
	if inst.inited {
		return
	}
	inst.inited = true
	if run := sourceRef.Load().run(); run != "" {
		inst.applyCase(Case{Kind: CaseRun, Value: run})
		return
	}
	inst.applyCase(Case{Kind: CaseAll})
}

func (inst *App) Frame(ctx app.FrameContextI) (err error) {
	inst.frameCtx = ctx
	inst.tick(time.Now())
	return inst.render()
}

// tick advances the caches; the render goroutine's only contact with the
// workers.
func (inst *App) tick(now time.Time) {
	inst.ensureInit()
	src := sourceRef.Load()
	if w, ok := inst.picker.take(); ok {
		inst.setWindowGesture(w)
	}
	inst.updateContext(now)
	inst.cache.tick(now, src, inst.scopeValue(), readSpec{c: inst.caseValue, filter: inst.filter, buckets: stripBuckets(inst.stripW),
		ctxFrom: inst.ctx.from, ctxTo: inst.ctx.to}, inst.probeFunc(src))
	inst.syncReadFSM(src.persisted())
	inst.refreshSelected()
	inst.settlePending()
	inst.verdicts.tick(now, src, runsOf(inst.cache.rows))
	var sel *Row
	if inst.hasSelected {
		sel = &inst.selected
	}
	inst.detail.tick(now, src, sel, inst.probeFunc(src))
	inst.tickAnalytics(src)
	inst.tickViews()
	inst.drainSend()
}

func (inst *App) probeFunc(src *Source) func() (*probe, error) {
	appId, instance := inst.appIdOr(), inst.instance
	return func() (*probe, error) { return newProbe(src, appId, instance) }
}

func (inst *App) scopeValue() scope {
	return scope{Case: inst.caseValue, Window: inst.window, Filter: inst.filter.key(), Strip: inst.strip}
}

// --- state the commands set ---

func (inst *App) applyCase(cs Case) {
	inst.caseValue = cs
	inst.draftKind, inst.draftValue, inst.draftStale = cs.Kind, cs.Value, true
	if !inst.tab.availableFor(cs.Kind) {
		inst.applyTab(TabFields)
	}
}

// applyTab sets the tab, remembering a detail tab for the bar's Table.
func (inst *App) applyTab(t TabE) {
	inst.tab = t
	if t.detail() {
		inst.lastDetail = t
	}
}

// applyWindow sets the window; every face of it — the picker, the band,
// the detail strip's brush, the navigation row, set_range — lands here. The
// window it replaces goes on the history Back pops, unless this apply is
// the Back itself (ADR-0350 §SD13).
func (inst *App) applyWindow(w Window) {
	switch {
	case inst.hasBackTarget && sameWindow(w, inst.backTarget):
		inst.hasBackTarget = false
	case !sameWindow(w, inst.window):
		inst.history.push(inst.window)
	}
	inst.window = w
}

// backGesture returns to the previous window through set_range.
func (inst *App) backGesture() {
	w, ok := inst.history.pop()
	if !ok {
		return
	}
	inst.backTarget, inst.hasBackTarget = w, true
	inst.setWindowGesture(w)
}

func (inst *App) applyFilter(f Filter) {
	inst.filter = f.normalized()
}

// applyStrip sets what the strip's bars are coloured by; the Colour button
// and set_strip both land here. The strip is read with the table, so the
// cache refills under the new colouring.
func (inst *App) applyStrip(by StripByE) {
	inst.strip = by
}

// applySide sets the pane beside the table; its buttons and set_side both
// land here.
func (inst *App) applySide(side sideE) {
	inst.side = side
}

// applyCompare sets the row the detail pane compares the selected one
// with, which must be a held row; 0 clears it. A pick in the table, the
// Clear comparison button and set_compare all land here.
func (inst *App) applyCompare(id uint64) (err error) {
	if id == 0 {
		inst.compare, inst.hasCompare = Row{}, false
		return
	}
	r, ok := inst.heldRow(id)
	if !ok {
		return app.RefuseOperation(rowGoneReason)
	}
	inst.compare, inst.hasCompare = r, true
	return
}

// compareId is the compare resource's value: the compare row's facts id,
// 0 for none.
func (inst *App) compareId() (id uint64) {
	if inst.hasCompare {
		id = inst.compare.Id
	}
	return
}

func (inst *App) selectionId() (id uint64) {
	if inst.hasSelected {
		id = inst.selected.Id
	}
	return
}

func (inst *App) clearSelection() {
	inst.selected, inst.hasSelected = Row{}, false
}

// selectById selects a held row, or a row of the selected row's context.
func (inst *App) selectById(id uint64) (ok bool) {
	if id == 0 {
		inst.clearSelection()
		return true
	}
	r, ok := inst.heldRow(id)
	if ok {
		inst.selected, inst.hasSelected = r, true
	}
	return
}

// refreshSelected replaces the selected row's copy with the cache's when
// a refill holds it, so its audit columns follow; a row the refill does
// not hold stays selected as it was.
func (inst *App) refreshSelected() {
	if !inst.hasSelected {
		return
	}
	for i := range inst.cache.rows {
		if inst.cache.rows[i].Id == inst.selected.Id {
			if inst.selected.Audit == nil || inst.cache.rows[i].Audit != nil {
				inst.selected = inst.cache.rows[i]
			}
			return
		}
	}
}

// --- rendering ---

func (inst *App) render() (err error) {
	inst.density = styletokens.ActiveDensity()
	src := sourceRef.Load()
	if !src.persisted() {
		reason := notPersistedReason(src)
		c.Label(strings.ToUpper(reason[:1]) + reason[1:] + ".").Wrap().Send()
		return
	}
	if inst.draftStale {
		inst.draftStale = false
		c.CurrentApplicationState.StateManager.OverrideDatabindingSPtr(&inst.draftValue)
	}
	inst.requestRepaint()
	now := time.Now()
	inst.paneSeq.top = inst.ids.ProbeSeq("pane-top")
	for range c.PanelTopInside(inst.ids.PrepareStr("top")).Resizable(false).KeepIter() {
		inst.measureTopPanel()
		inst.renderControls()
		inst.renderNavigation(now)
		inst.renderBand(now)
		inst.renderDetailStrip()
		inst.renderVerdicts()
	}
	if inst.hasSelected {
		for range c.PanelBottomInside(inst.ids.PrepareStr("btm")).DefaultSize(detailPanelHeight).Resizable(true).KeepIter() {
			inst.renderDetail()
		}
	}
	for range c.PanelRightInside(inst.ids.PrepareStr("facets")).DefaultSize(facetsPanelWidth).Resizable(true).KeepIter() {
		inst.renderSide()
	}
	for range c.PanelCentralInside().KeepIter() {
		inst.renderViewBar()
		switch inst.tab {
		case TabTree:
			inst.renderTree()
		case TabMatrix:
			inst.renderMatrix()
		case TabDotted:
			inst.renderDotted()
		case TabDayHour:
			inst.renderDayHour()
		case TabSeries:
			inst.renderSeries()
		default:
			inst.renderTable()
		}
	}
	return
}

// measureTopPanel reads the top panel's width where it is still the
// window's: at the panel's start, before anything is placed in it. egui
// widens a Ui's max rect to include a child that overflows it, so a probe
// placed after a row that overflows the window — the controls row on a
// narrow window — reads the row's width, and a canvas sized from that
// probe overflows in turn and widens the next frame's reading. The band
// and the detail strip are therefore sized from this one measurement,
// kept in stripW, with the fallback until the probe reports.
func (inst *App) measureTopPanel() {
	c.CaptureUiAvailableRect(inst.paneSeq.top)
	if r, ok := c.CurrentApplicationState.StateManager.GetUiRect(inst.paneSeq.top); ok && r.MaxX-r.MinX >= minTopPanelWidth {
		inst.stripW = r.MaxX - r.MinX
	}
}

// renderViewBar is the bar above the central area: the table and the
// analytic views, Send to play, and the export's outcome.
func (inst *App) renderViewBar() {
	for range c.HorizontalTop().KeepIter() {
		if c.Button(inst.ids.PrepareStr("view-table"), c.Atoms().Text(TabFields.Label()).Keep()).Selected(inst.tab.detail()).
			SendResp().HasPrimaryClicked() && !inst.tab.detail() {
			inst.setTabGesture(inst.lastDetail)
		}
		for _, t := range ViewTabs {
			if !t.availableFor(inst.caseValue.Kind) {
				continue
			}
			if c.Button(inst.ids.PrepareStr("view-"+t.String()), c.Atoms().Text(t.Label()).Keep()).Selected(inst.tab == t).
				SendResp().HasPrimaryClicked() && inst.tab != t {
				inst.setTabGesture(t)
			}
		}
		c.AddSpace(styletokens.GapPanels(inst.density))
		if c.Button(inst.ids.PrepareStr("send-play"), c.Atoms().Text("Send to play").Keep()).SendResp().HasPrimaryClicked() {
			inst.sendToPlayGesture()
		}
		switch {
		case inst.send.runner.Running():
			c.Label("sending…").Send()
		case inst.send.note != "":
			c.LabelAtoms(c.Atoms().BeginRichTextColored(detailMutedFg, detailBgClear, inst.send.note).Small().End().Keep()).Send()
		}
	}
}

func (inst *App) requestRepaint() {
	running := inst.cache.runner.Running() || inst.verdicts.runner.Running() || inst.detail.runner.Running() || inst.picker.runner.runner.Running() ||
		inst.an.running() || inst.views.runner.Running() || inst.send.runner.Running()
	switch {
	case running:
		c.RequestRepaintAfter(0.1)
	case inst.drawnGen != inst.cache.gen:
		inst.drawnGen, inst.settleRem = inst.cache.gen, 3
		c.RequestRepaint()
	case inst.settleRem > 0:
		inst.settleRem--
		c.RequestRepaint()
	default:
		c.RequestRepaintAfter(refreshInterval().Seconds())
	}
}

// renderControls is the case selector, the time range picker, Refresh, the
// read's state chip with its summary and, while a read runs, its progress
// row — the last thing in the row, so its coming and going moves nothing
// else (ADR-0350 §SD12).
func (inst *App) renderControls() {
	c.AddSpace(styletokens.PaddingHair(inst.density))
	for range c.Horizontal().KeepIter() {
		c.Label("Case:").Send()
		for range c.ComboBox(inst.ids.PrepareStr("case-kind"), c.WidgetText().Text("case").Keep(),
			c.WidgetText().Text(inst.draftKind.String()).Keep()).KeepIter() {
			for _, k := range AllCaseKinds {
				sel := k == inst.draftKind
				if c.Button(inst.ids.PrepareStr("case-"+k.String()), c.Atoms().Text(k.String()).Keep()).
					Selected(sel).FrameWhenInactive(!sel).Frame(true).SendResp().HasPrimaryClicked() {
					inst.draftKind = k
					if k == CaseAll {
						inst.draftValue, inst.draftStale = "", true
					}
				}
			}
		}
		inst.valueH = widgethandle.Make(inst.ids.PrepareStr("case-value").Derive())
		c.TextEdit(inst.ids.PrepareStr("case-value"), inst.draftValue, false).
			HintText(inst.draftKind.Hint()).DesiredWidth(260).SendRespVal(&inst.draftValue)
		if c.Button(inst.ids.PrepareStr("case-apply"), c.Atoms().Text("Apply").Keep()).SendResp().HasPrimaryClicked() {
			cs := Case{Kind: inst.draftKind, Value: strings.TrimSpace(inst.draftValue)}
			if err := cs.validate(); err != nil {
				inst.picker.err = err
			} else {
				inst.picker.err = nil
				inst.setCaseGesture(cs)
			}
		}
		c.AddSpace(styletokens.GapPanels(inst.density))
		inst.picker.render(inst.ids, inst.window)
		mode := "fixed"
		if inst.window.Last > 0 {
			mode = "live"
		}
		c.LabelAtoms(c.Atoms().BeginRichTextColored(detailMutedFg, detailBgClear, mode).Small().End().Keep()).Send()
		c.AddSpace(styletokens.GapPanels(inst.density))
		if c.Button(inst.ids.PrepareStr("refresh"), c.Atoms().Text("Refresh").Keep()).SendResp().HasPrimaryClicked() {
			inst.refreshGesture()
		}
		inst.renderReadState()
	}
	for _, e := range []error{inst.cache.lastErr, inst.picker.err, inst.launchErr} {
		if e != nil {
			c.LabelAtoms(c.Atoms().BeginRichTextColored(detailErrorFg, detailBgClear, e.Error()).Small().End().Keep()).Wrap().Send()
		}
	}
}

// renderReadState is the read's state chip (readstate.go) — a tethered
// fsmview summary whose popup draws the graph and the history — and, while
// a read runs, the progress row: a determinate bar by statement, the ETA,
// the step's note and the Cancel button, which routes through stop_read.
func (inst *App) renderReadState() {
	if inst.readView == nil {
		inst.readView = fsmview.New(inst.ids, "read-fsm", inst.readFSM, fsmview.Options[readStateE]{
			Title: "the window's read of the trail marts", Tethered: true, BadgeTone: readStateTone, AutoAnchor: true})
	}
	inst.readView.Opts.Summary = func() {
		if s := inst.freshness(); s != "" {
			c.LabelAtoms(c.Atoms().BeginRichTextColored(detailMutedFg, detailBgClear, s).Small().End().Keep()).Send()
		}
	}
	inst.readView.Render()
	switch inst.readFSM.Current() {
	case readReading, readRefreshing:
		snap := inst.cache.runner.Snapshot()
		res := jobprogress.Render(jobprogress.Input{Ids: inst.ids, ScopeKey: "read-progress", Inline: true, BarWidth: 160, Cancel: true,
			Fraction: snap.Fraction, EtaMs: snap.EtaMs, Note: snap.Note})
		if res.CancelClicked {
			inst.stopReadGesture()
		}
	}
}

// freshness is the chip's summary: what is held and when it was read, or
// that the previous read is still showing while a new scope is read; ""
// before the first fill, when the chip's state says it all.
func (inst *App) freshness() (label string) {
	if !inst.cache.filled {
		return
	}
	if inst.cache.scope != inst.scopeValue() {
		return "showing the previous read"
	}
	label = fmt.Sprintf("%d rows · read %s", len(inst.cache.rows), inst.picker.format(inst.cache.refreshedAt, "15:04:05"))
	if inst.cache.more {
		label += " · older rows follow"
	}
	return
}

// renderVerdicts is the verdict line per writer run (SD6), above the
// table: a run's state never hides its rows. The badge shows the state in
// bounded words and the full line as its tooltip: the row wraps, and a
// badge whose label wraps inside a wrapped row of the top panel makes the
// panel claim the window (ADR-0350 §SD12) — nothing a read puts on screen
// may move the layout outside its own row.
func (inst *App) renderVerdicts() {
	runs := runsOf(inst.cache.rows)
	if len(runs) == 0 {
		return
	}
	for range c.IdScope(inst.ids.PrepareStr("verdicts")) {
		for range c.HorizontalWrapped().KeepIter() {
			c.Label("Evidence:").Send()
			for i, run := range runs {
				l := inst.verdicts.line(run)
				badge.New(inst.ids.PrepareSeq(uint64(i)), shortRun(run)+": "+l.Short()).
					Tone(toneOf(l.Tone())).Variant(badge.VariantSoft).Size(badge.SizeSm).
					Tooltip(l.Label() + "\nwriter run " + run + "; law " + lawText(l)).Send()
			}
		}
	}
}

func lawText(l verdict.Line) string {
	if l.Law() == "" {
		return "none: unverified"
	}
	return l.Law()
}

func toneOf(t verdict.ToneE) badge.ToneE {
	switch t {
	case verdict.ToneOk:
		return badge.ToneSuccess
	case verdict.ToneWarning:
		return badge.ToneWarning
	case verdict.ToneError:
		return badge.ToneError
	}
	return badge.ToneNeutral
}

func shortRun(run string) string {
	if run == "" {
		return "(no run)"
	}
	if len(run) > 8 {
		return run[:8]
	}
	return run
}

// navButtons are the navigation row's buttons, in order, with the key each
// derives its id from.
var navButtons = []struct{ key, label string }{
	{"nav-back", "Back"}, {"nav-earlier", "Earlier"}, {"nav-later", "Later"}, {"nav-zoom-out", "Zoom out"},
	{"nav-zoom-in", "Zoom in"}, {"nav-fit", "Fit"}, {"nav-live", "Live"},
}

// renderNavigation is the navigation row (ADR-0350 §SD13): Back, Earlier,
// Later, Zoom out, Zoom in, Fit and Live, each a face of the window through
// set_range; the strip's colouring toggle; and the one text line a scene
// asserts on. A button that would do nothing is disabled.
func (inst *App) renderNavigation(now time.Time) {
	from, to := inst.window.Resolve(now)
	var anchor time.Time
	if inst.hasSelected {
		anchor = inst.selected.Ts()
	}
	_, canBack := inst.history.top()
	later, canLater := shiftWindow(from, to, now, 1)
	zoomIn, canZoomIn := zoomWindow(from, to, now, anchor, inst.hasSelected, false)
	fit, canFit := fitWindow(inst.cache.ctxBuckets, inst.cache.ctxStep)
	for range c.Horizontal().KeepIter() {
		for _, b := range navButtons {
			var target Window
			enabled := true
			switch b.label {
			case "Back":
				enabled = canBack
			case "Earlier":
				target, enabled = shiftWindow(from, to, now, -1)
			case "Later":
				target, enabled = later, canLater
			case "Zoom out":
				target, enabled = zoomWindow(from, to, now, anchor, inst.hasSelected, true)
			case "Zoom in":
				target, enabled = zoomIn, canZoomIn
			case "Fit":
				target, enabled = fit, canFit
			case "Live":
				target, enabled = Window{Last: roundLive(to.Sub(from))}, inst.window.Last == 0
			}
			var clicked bool
			for range c.EnabledUi(enabled).KeepIter() {
				clicked = c.Button(inst.ids.PrepareStr(b.key), c.Atoms().Text(b.label).Keep()).SendResp().HasPrimaryClicked()
			}
			if !clicked || !enabled {
				continue
			}
			if b.label == "Back" {
				inst.backGesture()
			} else {
				inst.setWindowGesture(target)
			}
		}
		c.AddSpace(styletokens.GapPanels(inst.density))
		label := "by kind"
		if inst.strip == StripByStatus {
			label = "by status"
		}
		if c.Button(inst.ids.PrepareStr("strip-by"), c.Atoms().Text("Colour "+label).Keep()).SendResp().HasPrimaryClicked() {
			next := StripByKind
			if inst.strip == StripByKind {
				next = StripByStatus
			}
			inst.setStripGesture(next)
		}
		c.Label(stripLineIn(inst.cache.buckets, inst.cache.step, inst.picker.loc)).Send()
	}
}

// renderBand is the context band (ADR-0350 §SD13): a timescrubber over the
// context extent the table read, one step per bucket with its count, the
// steps inside the wanted window loading while a read is in flight, drawn
// at the width measureTopPanel read rather than its own probe's. Its
// range band is the window and its playhead the selected row; both are
// kept one value by the commit rule in navigation.go, so a gesture lands
// through set_range once and the host never clobbers one in progress.
func (inst *App) renderBand(now time.Time) {
	if inst.band == nil {
		inst.band = timescrubber.New(inst.ids, "trail-band", timescrubber.Options{Height: bandHeight, Location: inst.picker.loc, ValueName: "rows", Now: time.Now,
			EmptyText: "The band shows the window's surroundings once the first read lands"})
	}
	inst.band.Opts.Location = inst.picker.loc
	ch := &inst.cache
	wantFrom, wantTo := inst.window.Resolve(now)
	inst.bandSteps = contextSteps(ch.ctxFrom, ch.ctxTo, ch.ctxStep, ch.ctxBuckets, ch.runner.Running(), wantFrom, wantTo, inst.bandSteps)
	steps := inst.bandSteps
	n := len(steps)
	t := &inst.band.Transport
	// The playhead follows the selection.
	switch {
	case !inst.hasSelected:
		inst.bandSeekId = 0
	case inst.selected.Id != inst.bandSeekId && n > 0:
		inst.bandSeekId = inst.selected.Id
		t.Seek(float64(stepIndexOf(steps, inst.selected.Ts())), n)
	}
	ev := inst.band.Render(inst.stripW, steps)
	stepsChanged := ch.gen != inst.bandGen
	// A settled position selects the newest held row of its bucket — a
	// click, a key, each step of playback — unless the steps were just
	// rebuilt, when the scrubber's own re-placement settles too.
	if ev.Settled && !stepsChanged && ev.Step >= 0 && ev.Step < n {
		at := steps[ev.Step].At
		if !inst.hasSelected || !inBucket(inst.selected.Ts(), at, ch.ctxStep) {
			if id, ok := newestRowIn(ch.rows, at, at.Add(ch.ctxStep)); ok {
				inst.bandSeekId = id
				inst.selectGesture(id)
			}
		}
	}
	cur := rangeOf(t, n)
	changed := stepsChanged || !sameWindow(inst.window, inst.bandWindow)
	switch decideBand(ev.Dragging, cur, inst.bandMirror, changed) {
	case bandMirror:
		t.ClearRange()
		if n > 1 {
			if lo, hi := windowToRange(steps, wantFrom, wantTo); lo < hi {
				t.SetRange(lo, hi)
			}
		}
		inst.bandMirror, inst.bandWindow, inst.bandGen = rangeOf(t, n), inst.window, ch.gen
	case bandCommitRange:
		inst.bandMirror = cur
		inst.setWindowGesture(rangeToWindow(steps, cur.lo, cur.hi, ch.ctxStep))
	case bandCommitContext:
		inst.bandMirror = cur
		to := ch.ctxTo
		if to.After(now) {
			to = now
		}
		if to.After(ch.ctxFrom) {
			inst.setWindowGesture(Window{From: ch.ctxFrom, To: to})
		}
	}
}

// renderDetailStrip is the detail strip (ADR-0350 §SD13): a timeline
// locked to the window the table read, one lane per series of the strip's
// buckets with the count as intensity, the out-of-hours bands behind. Its
// brush narrows the window through set_range and is then taken back, as
// play does; a click on a cell selects the newest held row of its bucket.
func (inst *App) renderDetailStrip() {
	ch := &inst.cache
	if inst.detailTl == nil {
		inst.detailVis = timeline.DefaultVisuals()
		inst.detailVis.LaneHeight = detailLaneHeight
		inst.detailTl = timeline.New(inst.ids, "trail-detail", nil, timeline.Options{TimeZone: inst.picker.loc, LockedView: true, Brush: true, NowLine: true,
			IntervalColors: detailColors(), BackgroundBands: outOfHoursBands(func() *time.Location { return inst.picker.loc }), Visuals: &inst.detailVis,
			ContainerWidth: stripFallbackWidth})
	}
	tl := inst.detailTl
	tl.Opts.TimeZone = inst.picker.loc
	if ch.gen != inst.detailGen {
		inst.detailGen = ch.gen
		ivs, _ := detailIntervals(ch.buckets, ch.step)
		tl.SetIntervals(ivs)
	}
	if !ch.from.IsZero() && ch.to.After(ch.from) {
		tl.SetRangeUnits(ch.from.UnixMilli(), ch.to.UnixMilli())
	}
	// A child Ui bounded to the measured width, so the strip's own width
	// probe reads it rather than a max rect widened by a row above.
	var ev timeline.Events
	for range c.Vertical().KeepIter() {
		c.UiSetMaxWidth(inst.stripW)
		ev = tl.Render()
	}
	if ev.BrushChanged && ev.BrushOk {
		tl.ClearBrush()
		inst.setWindowGesture(Window{From: time.UnixMilli(ev.Brush.FromMS).UTC(), To: time.UnixMilli(ev.Brush.ToMS).UTC()})
	}
	if ev.SelectionChanged && ev.Selection.Kind == timeline.SelectionInterval && ev.Selection.Interval != nil {
		iv := ev.Selection.Interval
		if id, ok := newestRowIn(ch.rows, time.UnixMilli(iv.FromMS), time.UnixMilli(iv.ToMS)); ok {
			inst.selectGesture(id)
		}
		tl.ClearSelection()
	}
}

// bucketStart is the start of the strip bucket holding x, in Unix seconds.
// The buckets are ClickHouse's toStartOfInterval over seconds, which
// counts from the Unix epoch; time.Truncate counts from year 1, three days
// off for a seven-day step.
func bucketStart(x float64, step time.Duration) time.Time {
	s := int64(step / time.Second)
	if s <= 0 {
		s = 1
	}
	u := int64(math.Floor(x))
	b := u / s * s
	if u < 0 && u%s != 0 {
		b -= s
	}
	return time.Unix(b, 0).UTC()
}

// seriesColor is the design system's qualitative cycle; the legend names
// every series, so colour is never the only channel.
func seriesColor(i int) uint32 {
	rgba := styletokens.QualitativeCycle(i)
	return uint32(rgba.R)<<24 | uint32(rgba.G)<<16 | uint32(rgba.B)<<8 | uint32(rgba.A)
}

func sortKeys(keys []string, totals map[string]uint64) {
	for i := 1; i < len(keys); i++ {
		for j := i; j > 0; j-- {
			a, b := keys[j-1], keys[j]
			if totals[a] > totals[b] || (totals[a] == totals[b] && a <= b) {
				break
			}
			keys[j-1], keys[j] = b, a
		}
	}
}

// stripLine is the strip in words, UTC.
func stripLine(buckets []Bucket, step time.Duration) string {
	return stripLineIn(buckets, step, time.UTC)
}

// stripLineIn is the strip in words: rows, buckets of what size, and the
// busiest bucket with its time in loc.
func stripLineIn(buckets []Bucket, step time.Duration, loc *time.Location) string {
	per := map[int64]uint64{}
	var rows uint64
	for _, b := range buckets {
		per[b.Bucket] += b.N
		rows += b.N
	}
	if len(per) == 0 {
		return "0 rows in the window"
	}
	var busiest int64
	var most uint64
	for k, n := range per {
		if n > most || (n == most && k < busiest) {
			busiest, most = k, n
		}
	}
	if loc == nil {
		loc = time.UTC
	}
	return fmt.Sprintf("%d rows in %d buckets of %s; busiest %s with %d", rows, len(per), step,
		time.Unix(busiest, 0).In(loc).Format("2006-01-02 15:04:05 MST"), most)
}

// renderFacets is the facets pane: the active filter, then per facet its
// values with counts. A click includes a value, a secondary or modified
// click (Ctrl/Cmd/Alt) excludes it, and a click on a value with a term
// removes the term.
func (inst *App) renderFacets() {
	for range c.ScrollArea().Vscroll(true).Hscroll(false).AutoShrink(false, false).KeepIter() {
		c.Label("Filter: " + inst.filter.String()).Wrap().Send()
		if len(inst.filter.Terms) > 0 {
			if c.Button(inst.ids.PrepareStr("filter-clear"), c.Atoms().Text("Clear filter").Keep()).SendResp().HasPrimaryClicked() {
				inst.setFilterGesture(Filter{})
			}
		}
		mods := c.CurrentApplicationState.StateManager.GetModifiers()
		modified := mods.Command || mods.Ctrl || mods.Alt
		for fi, f := range AllFacets {
			var vs []FacetValue
			if fi < len(inst.cache.facets) {
				vs = inst.cache.facets[fi]
			}
			for range c.IdScope(inst.ids.PrepareStr("facet-" + f.String())) {
				for range c.CollapsingHeader(inst.ids.PrepareStr("hdr"), c.WidgetText().Text(fmt.Sprintf("%s (%d)", f, len(vs))).Keep()).
					DefaultOpen(f != FacetPrincipal).KeepIter() {
					if len(vs) == 0 {
						c.Label("(none in the case and window)").Send()
					}
					for i, v := range vs {
						label := v.Value
						if label == "" {
							label = "(empty)"
						}
						excluded, has := inst.filter.term(f, v.Value)
						prefix := ""
						if has && excluded {
							prefix = "≠ "
						} else if has {
							prefix = "= "
						}
						resp := c.Button(inst.ids.PrepareSeq(uint64(i)), c.Atoms().Text(fmt.Sprintf("%s%s  %d", prefix, label, v.N)).Keep()).
							Selected(has && !excluded).SendResp()
						switch {
						case resp.HasSecondaryClicked() || (resp.HasPrimaryClicked() && modified):
							inst.setFilterGesture(inst.filter.with(f, v.Value, true))
						case resp.HasPrimaryClicked() && has:
							inst.setFilterGesture(inst.filter.without(f, v.Value))
						case resp.HasPrimaryClicked():
							inst.setFilterGesture(inst.filter.with(f, v.Value, false))
						}
					}
				}
			}
		}
	}
}

// --- the table ---

// gridState is the grid's record, built from the cache's rows.
type gridState struct {
	rec    arrow.RecordBatch
	rows   []Row
	gen    uint64
	excVer uint64
	built  bool
	state  recordtable.State
	sort   recordtable.SortState
	groups recordtable.GroupState
	order  []int64
	// maxPos is the display position of the last row drawn this frame.
	maxPos int64
	// endGen and endFrames count the consecutive frames of one fill that
	// drew the end row: a fill's first frame may draw every row while the
	// grid fits itself, so the end is reached on the second.
	endGen    uint64
	endFrames int
}

func (inst *gridState) release() {
	if inst.rec != nil {
		inst.rec.Release()
		inst.rec = nil
	}
}

// gridCol is one column of the table: its header, its sort key and its
// cell.
type gridCol struct {
	header  string
	width   float32
	agentic bool
	// exc is the exceptions column, whose text the window computes.
	exc bool
	str func(r *Row) string
	num func(r *Row) int64
}

// tableColumns are the table's columns: the verdict mark first, the
// exceptions or the review mark second, then time,
// kind, headline, status, subject, app, instance, and the conversation and
// task columns when the case is agentic. The time is kept as nanoseconds
// for the sort; its cell is drawn in the picker's zone.
var tableColumns = []gridCol{
	{header: "verdict", width: 80, str: func(r *Row) string { return r.Run }},
	{header: "exceptions", width: 140, exc: true},
	{header: "time", width: 150, num: func(r *Row) int64 { return r.TsNs }},
	{header: "kind", width: 100, str: func(r *Row) string { return r.Kind }},
	{header: "headline", width: 520, str: func(r *Row) string { return r.Headline }},
	{header: "status", width: 90, str: func(r *Row) string { return r.Status }},
	{header: "subject", width: 160, str: func(r *Row) string { return r.Subject }},
	{header: "app", width: 120, str: func(r *Row) string { return r.App }},
	{header: "instance", width: 70, num: func(r *Row) int64 { return int64(r.Instance) }},
	{header: "conversation", width: 140, agentic: true, str: func(r *Row) string { return r.Conversation }},
	{header: "task", width: 140, agentic: true, str: func(r *Row) string { return r.Task }},
}

// build rebuilds the record from rows; excText is the exceptions column.
func (inst *gridState) build(rows []Row, gen uint64, excVer uint64, excText func(r *Row) string) {
	if inst.built && inst.gen == gen && inst.excVer == excVer {
		return
	}
	inst.release()
	inst.rows, inst.gen, inst.excVer, inst.built = rows, gen, excVer, true
	mem := memory.DefaultAllocator
	fields := make([]arrow.Field, 0, len(tableColumns))
	arrs := make([]arrow.Array, 0, len(tableColumns))
	for _, col := range tableColumns {
		if col.num != nil {
			b := array.NewInt64Builder(mem)
			for i := range rows {
				b.Append(col.num(&rows[i]))
			}
			arrs = append(arrs, b.NewArray())
			b.Release()
			fields = append(fields, arrow.Field{Name: col.header, Type: arrow.PrimitiveTypes.Int64})
			continue
		}
		str := col.str
		if col.exc {
			str = excText
		}
		b := array.NewStringBuilder(mem)
		for i := range rows {
			b.Append(str(&rows[i]))
		}
		arrs = append(arrs, b.NewArray())
		b.Release()
		fields = append(fields, arrow.Field{Name: col.header, Type: arrow.BinaryTypes.String})
	}
	inst.rec = array.NewRecordBatch(arrow.NewSchema(fields, nil), arrs, int64(len(rows)))
	for _, a := range arrs {
		a.Release()
	}
}

func (inst *App) renderTable() {
	g := &inst.grid
	exc := inst.exceptions()
	marks := inst.marks()
	g.build(inst.cache.rows, inst.cache.gen, inst.excVer, func(r *Row) string {
		text, _, _ := exceptionCell(exc.byRow[r.Id], marks, r.Id)
		return text
	})
	for range c.Horizontal().KeepIter() {
		groupLabel := "Group by parent: off"
		if inst.grouped {
			groupLabel = "Group by parent: on"
		}
		if c.Button(inst.ids.PrepareStr("group"), c.Atoms().Text(groupLabel).Keep()).Selected(inst.grouped).SendResp().HasPrimaryClicked() {
			inst.setGroupedGesture(!inst.grouped)
		}
		c.Label("Case: " + inst.caseValue.String() + " · " + inst.window.String()).Send()
		if inst.picking {
			c.LabelAtoms(c.Atoms().BeginRichTextColored(detailMutedFg, detailBgClear, "click a row to compare it with the selected one").Small().End().Keep()).Send()
		}
	}
	if line := inst.asOfLine(); line != "" {
		c.Label(line).Wrap().Send()
	}
	if len(g.rows) == 0 {
		switch {
		case inst.cache.filled:
			c.Label("(no rows in the case, window and filter)").Send()
		default:
			c.Label("(reading the trail marts…)").Send()
		}
		return
	}
	agentic := inst.caseValue.Kind.agentic()
	cols := make([]recordtable.Column, 0, len(tableColumns))
	rows := g.rows
	g.maxPos = -1
	g.order = g.sort.OrderFor(g.rec, g.rec.NumRows())
	for i, col := range tableColumns {
		if col.agentic && !agentic {
			continue
		}
		cl := recordtable.Column{Arrow: i, Header: col.header, Width: col.width, WidthType: "text"}
		if col.num != nil {
			cl.WidthType, cl.DescFirst = "number", true
		}
		switch col.header {
		case "verdict":
			cl.Cell = func(row int64) recordtable.Cell {
				inst.noteDrawn(row)
				l := inst.verdicts.line(rows[row].Run)
				return recordtable.Cell{Text: l.Mark(), Hover: l.Label(), Tone: color.Hex(toneHex(l.Tone())), Toned: true}
			}
		case "exceptions":
			cl.Cell = func(row int64) recordtable.Cell {
				text, tone, hover := exceptionCell(exc.byRow[rows[row].Id], marks, rows[row].Id)
				return recordtable.Cell{Text: text, Hover: hover, Tone: color.Hex(tone), Toned: text != "", LeftAlign: true}
			}
		case "time":
			cl.Cell = func(row int64) recordtable.Cell {
				return recordtable.Cell{Text: inst.picker.format(rows[row].Ts(), "2006-01-02 15:04:05.000"), LeftAlign: true}
			}
		default:
			str, num := col.str, col.num
			cl.Cell = func(row int64) recordtable.Cell {
				if num != nil {
					return recordtable.Cell{Text: strconv.FormatInt(num(&rows[row]), 10)}
				}
				return recordtable.Cell{Text: str(&rows[row]), LeftAlign: true}
			}
		}
		cols = append(cols, cl)
	}
	sel, has := int64(-1), false
	if inst.hasSelected {
		for i := range rows {
			if rows[i].Id == inst.selected.Id {
				sel, has = int64(i), true
				break
			}
		}
	}
	in := recordtable.Input{Ids: inst.ids, ScopeKey: "trail-rows", Record: g.rec, Columns: cols, Sort: &g.sort, Order: g.order,
		Selected: sel, HasSelection: has, State: &g.state, RowHeight: 22}
	if inst.grouped {
		in.Groups = &g.groups
	}
	res := recordtable.RenderGrid(in)
	g.sort.Apply(res)
	if inst.grouped {
		g.groups.Apply(res)
	}
	if res.Clicked >= 0 && res.Clicked < int64(len(rows)) {
		id := rows[res.Clicked].Id
		mods := c.CurrentApplicationState.StateManager.GetModifiers()
		switch {
		case inst.hasSelected && inst.selected.Id != id && (inst.picking || mods.Command || mods.Ctrl):
			// A second selection: the detail pane compares the two.
			inst.picking = false
			inst.setCompareGesture(id)
			if inst.tab != TabCompare {
				inst.setTabGesture(TabCompare)
			}
		case inst.hasSelected && inst.selected.Id == id:
			inst.selectGesture(0)
		default:
			inst.selectGesture(id)
		}
	}
	// The end of what is held is reached when the last row by display
	// position was drawn on two consecutive frames of one fill; the cursor
	// then reads the rows after it.
	if g.reachedEnd(!inst.grouped && g.maxPos >= int64(len(rows))-1, inst.cache.gen) && inst.cache.more && !inst.cache.runner.Running() {
		inst.cache.extendNow = true
	}
}

// reachedEnd records whether this frame drew the end row of fill gen and
// reports whether the end is reached: drawn on this frame and the one
// before it. The first frame after a fill can draw every row of a short
// table before the grid has fitted itself, which is no scroll of the
// person's.
func (inst *gridState) reachedEnd(drawn bool, gen uint64) bool {
	switch {
	case !drawn:
		inst.endFrames = 0
	case inst.endGen != gen:
		inst.endGen, inst.endFrames = gen, 1
	default:
		inst.endFrames++
	}
	return inst.endFrames >= 2
}

// exceptionCell is the exceptions column for a row: its mark when it has
// one, else the rules its open exceptions name; the tone, and the reasons
// for the hover.
func exceptionCell(es []Exception, marks map[uint64]Review, id uint64) (text string, tone uint32, hover string) {
	if m, ok := marks[id]; ok {
		text, hover = m.Mark, "marked "+m.Mark
		if m.Reason != "" {
			hover += ": " + m.Reason
		}
		if len(es) > 0 {
			hover += "; clears " + ruleNames(es)
		}
		switch m.Mark {
		case trail.ReviewActionReviewed:
			tone = styletokens.SuccessDefault.AsHex()
		case trail.ReviewActionFlagged:
			tone = styletokens.ErrorDefault.AsHex()
		default:
			tone = styletokens.NeutralTextSecondary.AsHex()
		}
		return
	}
	if len(es) == 0 {
		return
	}
	text = strings.ReplaceAll(ruleNames(es), ",", ", ")
	reasons := make([]string, 0, len(es))
	for _, e := range es {
		reasons = append(reasons, e.Rule+": "+e.Reason)
	}
	hover, tone = strings.Join(reasons, "\n"), styletokens.WarningDefault.AsHex()
	return
}

// noteDrawn records the display position of a row the grid drew.
func (inst *App) noteDrawn(row int64) {
	g := &inst.grid
	pos := g.sort.DisplayPos(g.order, row)
	if pos > g.maxPos {
		g.maxPos = pos
	}
}

// setGrouped folds the table's rows under their parent, or unfolds them;
// the Group by parent button and set_grouping both land here.
func (inst *App) setGrouped(on bool) {
	inst.grouped = on
	if !on {
		inst.grid.groups.SetLevels(nil)
		return
	}
	rows := func() []Row { return inst.grid.rows }
	inst.grid.groups.SetLevels([]recordtable.GroupLevel{{Arrow: -1, Key: func(row int64) recordtable.Key {
		rs := rows()
		if row < 0 || row >= int64(len(rs)) || rs[row].ParentRef == "" {
			return recordtable.Key{Value: "", Label: "(no parent)", Null: true}
		}
		return recordtable.Key{Value: rs[row].ParentRef, Label: rs[row].ParentRef}
	}}})
}

func toneHex(t verdict.ToneE) uint32 {
	switch t {
	case verdict.ToneOk:
		return styletokens.SuccessDefault.AsHex()
	case verdict.ToneWarning:
		return styletokens.WarningDefault.AsHex()
	case verdict.ToneError:
		return styletokens.ErrorDefault.AsHex()
	}
	return styletokens.NeutralTextSecondary.AsHex()
}

// --- the detail pane ---

// breadcrumb is the identity chain as one line — person › window (app,
// instance) › task › model call › tool call — from the row's context
// columns, absent links omitted.
func breadcrumb(r *Row) string {
	var parts []string
	if r.Audit != nil && r.Audit.Principal != "" {
		parts = append(parts, "person "+r.Audit.Principal)
	}
	switch {
	case r.App != "" && r.Instance != 0:
		parts = append(parts, "window "+shortApp(r.App)+"#"+strconv.FormatUint(r.Instance, 10))
	case r.App != "":
		parts = append(parts, "window "+shortApp(r.App))
	case r.Instance != 0:
		parts = append(parts, "window #"+strconv.FormatUint(r.Instance, 10))
	}
	if r.Task != "" {
		parts = append(parts, "task "+r.Task)
	}
	modelCall := r.ParentRef
	if r.Kind == "llmCall" {
		modelCall = r.Ref
	}
	if r.Kind != kindAuditEvent && r.Kind != "httpFetch" && modelCall != "" {
		parts = append(parts, "model call "+modelCall)
	}
	if r.TaskCall != "" {
		parts = append(parts, "tool call "+r.TaskCall)
	}
	return strings.Join(parts, " › ")
}

func shortApp(id string) string {
	if i := strings.LastIndexByte(id, '/'); i >= 0 && i < len(id)-1 {
		return id[i+1:]
	}
	return id
}

func (inst *App) renderDetail() {
	r := &inst.selected
	l := inst.verdicts.line(r.Run)
	for range c.Horizontal().KeepIter() {
		badge.New(inst.ids.PrepareStr("d-verdict"), l.Mark()).Tone(toneOf(l.Tone())).Variant(badge.VariantSolid).Size(badge.SizeSm).
			Tooltip(l.Label()).Send()
		badge.New(inst.ids.PrepareStr("d-kind"), appops.CutText(r.Kind, 40)).Tone(badge.TonePrimary).Variant(badge.VariantOutline).Size(badge.SizeSm).Send()
		c.LabelAtoms(c.Atoms().BeginRichTextColored(detailMutedFg, detailBgClear,
			inst.picker.format(r.Ts(), time.RFC3339Nano)+" · id "+strconv.FormatUint(r.Id, 10)).Small().Monospace().End().Keep()).Send()
		for _, t := range DetailTabs {
			if c.Button(inst.ids.PrepareStr("d-tab-"+t.String()), c.Atoms().Text(t.String()).Keep()).Selected(inst.tab == t).
				SendResp().HasPrimaryClicked() && inst.tab != t {
				inst.setTabGesture(t)
			}
		}
		pickLabel := "Compare with…"
		if inst.picking {
			pickLabel = "Picking: click a row"
		}
		if c.Button(inst.ids.PrepareStr("d-compare"), c.Atoms().Text(pickLabel).Keep()).Selected(inst.picking).SendResp().HasPrimaryClicked() {
			inst.picking = !inst.picking
		}
		if c.Button(inst.ids.PrepareStr("d-clear"), c.Atoms().Text("Clear selection").Keep()).SendResp().HasPrimaryClicked() {
			inst.selectGesture(0)
		}
	}
	if bc := breadcrumb(r); bc != "" {
		c.Label(bc).Wrap().Send()
	}
	inst.renderReview()
	c.Separator().Horizontal().Send()
	for range c.ScrollArea().Vscroll(true).Hscroll(false).AutoShrink(false, false).KeepIter() {
		switch inst.tab {
		case TabPrevious:
			inst.renderPrevious()
		case TabContext:
			inst.renderContext()
		case TabCompare:
			inst.renderCompare()
		default:
			inst.renderFields()
		}
	}
}

func (inst *App) renderFields() {
	r := &inst.selected
	inst.renderAsOf()
	c.Label(r.Headline).Wrap().Send()
	if r.Text != "" {
		c.AddSpace(styletokens.PaddingHair(inst.density))
		c.Label("Text:").Send()
		c.Label(r.Text).Wrap().Selectable(true).Send()
	}
	fieldview.Render(fieldview.Input{Ids: inst.ids, ScopeKey: "tv-fields", State: &inst.fvState, Fields: rowFields(r)})
}

// rowFields is the row's columns for the field viewer: the timeline's,
// then an audit event's own, its attributes and its references.
func rowFields(r *Row) (fs []fieldview.Field) {
	str := func(n, v string) {
		if v != "" {
			fs = append(fs, fieldview.Field{Name: n, Kind: fieldview.KindString, Str: v})
		}
	}
	num := func(n string, v uint64) {
		if v != 0 {
			fs = append(fs, fieldview.Field{Name: n, Kind: fieldview.KindUint, Uint: v})
		}
	}
	str("kind", r.Kind)
	str("status", r.Status)
	str("subject", r.Subject)
	str("run", r.Run)
	str("app", r.App)
	num("instance", r.Instance)
	str("conversation", r.Conversation)
	str("turn", r.Turn)
	num("round", uint64(r.Round))
	str("task", r.Task)
	num("task-epoch", r.TaskEpoch)
	str("task-call", r.TaskCall)
	str("ref", r.Ref)
	str("parent-ref", r.ParentRef)
	num("tokens-in", r.TokensIn)
	num("tokens-out", r.TokensOut)
	num("elapsed-ms", r.ElapsedMs)
	if r.Confined {
		fs = append(fs, fieldview.Field{Name: "confined", Kind: fieldview.KindBool, Bool: true})
	}
	if r.Tainted {
		fs = append(fs, fieldview.Field{Name: "tainted", Kind: fieldview.KindBool, Bool: true})
	}
	if a := r.Audit; a != nil {
		str("domain", a.Domain)
		str("action", a.Action)
		str("outcome", a.Outcome)
		str("principal", a.Principal)
		str("principal-by", a.PrincipalBy)
		num("data subject", a.Subject)
		str("entity", a.Entity)
		str("from", a.From)
		str("from-by", a.FromBy)
		attrs := fieldview.Field{Name: "attributes", Kind: fieldview.KindObject}
		for i, k := range a.AttrKeys {
			if i < len(a.AttrValues) {
				attrs.Children = append(attrs.Children, fieldview.Field{Name: k, Kind: fieldview.KindString, Str: a.AttrValues[i]})
			}
		}
		if len(attrs.Children) > 0 {
			fs = append(fs, attrs)
		}
		refs := fieldview.Field{Name: "references", Kind: fieldview.KindObject}
		for i, t := range a.RefTypes {
			if i < len(a.RefValues) {
				refs.Children = append(refs.Children, fieldview.Field{Name: t, Kind: fieldview.KindString, Str: a.RefValues[i]})
			}
		}
		if len(refs.Children) > 0 {
			fs = append(fs, refs)
		}
	}
	return
}

func (inst *App) renderPrevious() {
	r := &inst.selected
	d := inst.detail.current(r.Id)
	switch {
	case d == nil:
		c.Label("(reading the previous state…)").Send()
		return
	case d.err != nil:
		c.LabelAtoms(c.Atoms().BeginRichTextColored(detailErrorFg, detailBgClear, d.err.Error()).End().Keep()).Wrap().Send()
		return
	case d.previous == nil:
		c.Label("This row has no previous state: it is neither an audit event naming an entity nor an agent grant of a task.").Wrap().Send()
		return
	}
	p := d.previous
	base := "No previous event; the field table shows this row's state."
	if p.Has {
		base = "Before is the previous event, id " + strconv.FormatUint(p.PrevId, 10) + " at " + inst.picker.format(p.PrevTs, time.RFC3339) +
			"; after is this row."
	}
	c.Label(base).Wrap().Send()
	tone := badge.ToneNeutral
	switch p.Claim {
	case ClaimMatches:
		tone = badge.ToneSuccess
	case ClaimDiffers:
		tone = badge.ToneError
	case ClaimGapBetween, ClaimNoPrevious:
		tone = badge.ToneWarning
	}
	badge.New(inst.ids.PrepareStr("p-verdict"), "from: "+appops.CutText(p.Verdict, 120)).Tone(tone).Variant(badge.VariantSoft).Size(badge.SizeSm).
		Tooltip(p.Verdict).Send()
	inst.renderFieldTable("p-table", []string{"field", "before", "after", "change"}, p.Fields)
}

func (inst *App) renderContext() {
	r := &inst.selected
	d := inst.detail.current(r.Id)
	switch {
	case d == nil:
		c.Label("(reading the rows around this one…)").Send()
		return
	case d.err != nil:
		c.LabelAtoms(c.Atoms().BeginRichTextColored(detailErrorFg, detailBgClear, d.err.Error()).End().Keep()).Wrap().Send()
		return
	}
	c.Label(fmt.Sprintf("The rows before and after this one in writer run %s, regardless of the filter; a click selects one.", r.Run)).Wrap().Send()
	for range c.IdScope(inst.ids.PrepareStr("ctx")) {
		n := uint64(0)
		line := func(x *Row, self bool) {
			text := inst.picker.format(x.Ts(), "15:04:05.000") + "  " + x.Kind + "  " + x.Headline
			if self {
				c.LabelAtoms(c.Atoms().BeginRichText("▸ " + text).Strong().End().Keep()).Send()
				return
			}
			if c.Button(inst.ids.PrepareSeq(n), c.Atoms().Text(text).Keep()).Frame(false).SendResp().HasPrimaryClicked() {
				inst.selectGesture(x.Id)
			}
			n++
		}
		for i := range d.before {
			line(&d.before[i], false)
		}
		line(r, true)
		for i := range d.after {
			line(&d.after[i], false)
		}
	}
}

// --- the time range picker ---

// pickerState is the picker's expressions, its zone, and the evaluation
// of an expression the viewer does not read itself.
type pickerState struct {
	fromExpr, toExpr string
	packed           string
	tzName           string
	loc              *time.Location
	presets          *presets.Registry
	runner           rangeRunner
	err              error
	pending          *Window
}

func newPickerState() pickerState {
	return pickerState{fromExpr: "anchor_now - INTERVAL 1 HOUR", toExpr: "anchor_now", tzName: "UTC", loc: time.UTC,
		presets: presets.DefaultGrafana75()}
}

// format renders t in the picker's zone.
func (inst *pickerState) format(t time.Time, layout string) string {
	loc := inst.loc
	if loc == nil {
		loc = time.UTC
	}
	return t.In(loc).Format(layout)
}

// render draws the picker; a change is read here when it is a span ending
// now, and otherwise evaluated off the frame.
func (inst *pickerState) render(ids *c.WidgetIdStack, w Window) {
	f := c.TimeRangePicker(ids.PrepareStr("range"), inst.fromExpr, inst.toExpr).Tz(inst.tzName)
	if w.Last == 0 && !w.From.IsZero() {
		f = f.EvaluatedBounds(w.From.UnixMilli(), w.To.UnixMilli())
	}
	for _, p := range inst.presets.All() {
		f = f.AddPreset(p.Label(), p.FromSQL(), p.ToSQL())
	}
	if !f.SendRespVal(&inst.packed).HasChanged() || inst.packed == "" {
		return
	}
	tz, from, to := timerangepicker.UnpackRange(inst.packed)
	inst.fromExpr, inst.toExpr = from, to
	if tz != "" {
		if id, err := timerangepicker.LookupTz(tz); err == nil {
			if loc, lerr := timerangepicker.LoadTzLocation(id); lerr == nil {
				inst.tzName, inst.loc = tz, loc
			}
		}
	}
	if d, ok := relativeSpan(from, to); ok {
		inst.err = nil
		inst.pending = &Window{Last: d}
		return
	}
	inst.err = inst.runner.start(from, to, inst.tzName)
}

// take returns a window the picker resolved since the last frame.
func (inst *pickerState) take() (w Window, ok bool) {
	if inst.pending != nil {
		w, ok = *inst.pending, true
		inst.pending = nil
		return
	}
	if res, rerr, done := inst.runner.take(); done {
		if rerr != nil {
			inst.err = rerr
			return
		}
		inst.err = nil
		return res, true
	}
	return
}

func (inst *pickerState) close() { inst.runner.close() }

// relativeSpan reads "anchor_now - INTERVAL n UNIT" to "anchor_now" as a
// span ending now, which the window re-resolves on every refresh. Other
// expressions are evaluated once into a fixed window.
func relativeSpan(from, to string) (d time.Duration, ok bool) {
	if strings.TrimSpace(to) != "anchor_now" {
		return
	}
	f := strings.Fields(from)
	if len(f) != 5 || f[0] != "anchor_now" || f[1] != "-" || !strings.EqualFold(f[2], "INTERVAL") {
		return
	}
	n, err := strconv.Atoi(f[3])
	if err != nil || n <= 0 {
		return
	}
	units := map[string]time.Duration{"SECOND": time.Second, "MINUTE": time.Minute, "HOUR": time.Hour, "DAY": 24 * time.Hour,
		"WEEK": 7 * 24 * time.Hour, "MONTH": 30 * 24 * time.Hour, "YEAR": 365 * 24 * time.Hour}
	// Upper-cased before the plural is trimmed, so "days" is a DAY.
	u, found := units[strings.TrimSuffix(strings.ToUpper(f[4]), "S")]
	if !found {
		return
	}
	return time.Duration(n) * u, true
}

// --- review: exceptions, marks, compare, as of ---

// sideE is what the pane beside the table shows.
type sideE uint8

const (
	sideFacets sideE = iota
	sideExceptions
	sideRules
	sideViews
)

var sideNames = [...]string{"facets", "exceptions", "rules", "views"}

func (inst sideE) String() string {
	if int(inst) < len(sideNames) {
		return sideNames[inst]
	}
	return "unknown"
}

// parseSide reads a pane's name.
func parseSide(s string) (side sideE, ok bool) {
	s = strings.ToLower(strings.TrimSpace(s))
	for i, n := range sideNames {
		if n == s {
			return sideE(i), true
		}
	}
	return
}

func sideNameList() string { return strings.Join(sideNames[:], ", ") }

// exceptionsShown bounds the exception list's drawn lines.
const exceptionsShown = 500

// renderSide is the pane beside the table: the facets, the exception list
// or the rules.
func (inst *App) renderSide() {
	exc := inst.exceptions()
	for range c.HorizontalWrapped().KeepIter() {
		for _, x := range []struct {
			side  sideE
			label string
		}{{sideFacets, "Facets"}, {sideExceptions, fmt.Sprintf("Exceptions (%d)", len(exc.open))}, {sideRules, "Rules"}, {sideViews, "Views"}} {
			if c.Button(inst.ids.PrepareStr(fmt.Sprintf("side-%d", x.side)), c.Atoms().Text(x.label).Keep()).Selected(inst.side == x.side).
				SendResp().HasPrimaryClicked() && inst.side != x.side {
				inst.setSideGesture(x.side)
			}
		}
	}
	c.Separator().Horizontal().Send()
	switch inst.side {
	case sideExceptions:
		inst.renderExceptions(exc)
	case sideRules:
		inst.renderRules(exc)
	case sideViews:
		inst.renderViews()
	default:
		inst.renderFacets()
	}
}

// renderExceptions is the exception list: each open exception with its
// rule and reason; a click selects the row in the table.
func (inst *App) renderExceptions(exc exceptionSet) {
	for range c.ScrollArea().Vscroll(true).Hscroll(false).AutoShrink(false, false).KeepIter() {
		c.Label(fmt.Sprintf("%d open among the %d rows held; %d cleared by a review mark. A click selects the row.",
			len(exc.open), len(inst.cache.rows), exc.cleared)).Wrap().Send()
		byId := make(map[uint64]*Row, len(inst.cache.rows))
		for i := range inst.cache.rows {
			byId[inst.cache.rows[i].Id] = &inst.cache.rows[i]
		}
		for range c.IdScope(inst.ids.PrepareStr("exc")) {
			for i, e := range exc.open {
				if i == exceptionsShown {
					c.Label(fmt.Sprintf("… and %d more", len(exc.open)-exceptionsShown)).Send()
					break
				}
				when := ""
				if r, ok := byId[e.Id]; ok {
					when = inst.picker.format(r.Ts(), "15:04:05") + "  "
				}
				sel := inst.hasSelected && inst.selected.Id == e.Id
				if c.Button(inst.ids.PrepareSeq(uint64(i)), c.Atoms().Text(when+e.Rule+": "+appops.CutText(e.Reason, 120)).Keep()).
					Frame(false).Selected(sel).SendResp().HasPrimaryClicked() && !sel {
					inst.selectGesture(e.Id)
				}
			}
		}
	}
}

// renderRules is every rule with its description, its predicate and how
// many open exceptions it raised: a highlight whose rule is hidden is
// distrusted (ADR-0350 §SD7).
func (inst *App) renderRules(exc exceptionSet) {
	counts := map[string]int{}
	for _, e := range exc.open {
		counts[e.Rule]++
	}
	for range c.ScrollArea().Vscroll(true).Hscroll(false).AutoShrink(false, false).KeepIter() {
		c.Label("The rules raise a reading list, not a verdict and not an action.").Wrap().Send()
		for _, r := range Rules {
			c.AddSpace(styletokens.PaddingHair(inst.density))
			c.LabelAtoms(c.Atoms().BeginRichText(fmt.Sprintf("%s (%d open)", r.Name, counts[r.Name])).Strong().End().Keep()).Send()
			c.Label(r.Description).Wrap().Send()
			c.LabelAtoms(c.Atoms().BeginRichTextColored(detailMutedFg, detailBgClear, r.PredicateText()).Small().Monospace().End().Keep()).Wrap().Send()
		}
	}
}

// renderReview is the selected row's exceptions, its mark, and the mark
// controls: a reason box and one button per mark, each routed through
// mark_row as the person's gesture.
func (inst *App) renderReview() {
	r := &inst.selected
	es := inst.exceptions().byRow[r.Id]
	m, marked := inst.marks()[r.Id]
	for range c.IdScope(inst.ids.PrepareStr("review")) {
		if len(es) > 0 {
			reasons := make([]string, 0, len(es))
			for _, e := range es {
				reasons = append(reasons, e.Rule+": "+e.Reason)
			}
			fg := color.Hex(styletokens.WarningDefault.AsHex())
			if marked {
				fg = detailMutedFg
			}
			c.LabelAtoms(c.Atoms().BeginRichTextColored(fg, detailBgClear, "Exceptions — "+strings.Join(reasons, "; ")).End().Keep()).Wrap().Send()
		}
		if marked {
			line := "Marked " + m.Mark + " at " + inst.picker.format(m.Ts(), time.RFC3339) + " by " + m.Principal
			if m.Reason != "" {
				line += ": " + m.Reason
			}
			if m.Marks > 1 {
				line += fmt.Sprintf(" (the latest of %d marks)", m.Marks)
			}
			c.Label(line).Wrap().Send()
		}
		for range c.HorizontalTop().KeepIter() {
			c.Label("Mark:").Send()
			c.TextEdit(inst.ids.PrepareStr("reason"), inst.markReason, false).HintText("reason (required to dismiss)").DesiredWidth(260).
				SendRespVal(&inst.markReason)
			for _, mk := range AllMarks {
				if c.Button(inst.ids.PrepareStr("mark-"+mk.String()), c.Atoms().Text(mk.String()).Keep()).SendResp().HasPrimaryClicked() {
					inst.markGesture(r.Id, mk)
				}
			}
		}
		if inst.markErr != nil {
			c.LabelAtoms(c.Atoms().BeginRichTextColored(detailErrorFg, detailBgClear, inst.markErr.Error()).Small().End().Keep()).Wrap().Send()
		}
	}
}

// markGesture routes the person's mark through the catalog when the host
// serves one, and to the handler directly when it does not reach it.
func (inst *App) markGesture(id uint64, mk MarkE) {
	in := MarkRowArgs{Id: id, Mark: mk.String(), Reason: inst.markReason}
	inst.markReached = false
	var err error
	if inst.frameCtx != nil {
		_, err = appops.Gesture[MarkRowArgs, MarkRowResult](inst.frameCtx, opMarkRow, in)
	}
	if !inst.markReached {
		_, err = inst.markRow(app.OperationCall{Writer: opwire.WriterPerson}, in)
	}
	inst.markErr = err
	if err == nil {
		inst.markReason = ""
		c.CurrentApplicationState.StateManager.OverrideDatabindingSPtr(&inst.markReason)
	}
}

// renderCompare is the selected row against the second row the person
// picked, older left, with the base in words above the table.
func (inst *App) renderCompare() {
	if !inst.hasCompare || inst.compare.Id == inst.selected.Id {
		c.Label("Pick a second row: press Compare with… and click a row, or Ctrl/Cmd-click a row in the table.").Wrap().Send()
		return
	}
	cmp := compareRows(inst.selected, inst.compare, func(t time.Time) string { return inst.picker.format(t, time.RFC3339Nano) })
	for range c.HorizontalTop().KeepIter() {
		c.Label(cmp.Base).Wrap().Send()
	}
	if c.Button(inst.ids.PrepareStr("cmp-clear"), c.Atoms().Text("Clear comparison").Keep()).SendResp().HasPrimaryClicked() {
		inst.setCompareGesture(0)
	}
	inst.renderFieldTable("cmp-table", []string{"field", "older", "newer", "change"}, cmp.Fields)
}

// renderFieldTable is a field table: field, before, after, change.
func (inst *App) renderFieldTable(key string, headers []string, fields []FieldDiff) {
	for range c.IdScope(inst.ids.PrepareStr(key)) {
		for range c.Grid(inst.ids.PrepareStr("grid")).NumColumns(4).Striped(true).KeepIter() {
			for _, h := range headers {
				c.LabelAtoms(c.Atoms().BeginRichText(h).Strong().End().Keep()).Send()
			}
			c.EndRow()
			for _, f := range fields {
				c.Label(f.Field).Send()
				c.Label(appops.CutText(f.Before, 400)).Send()
				c.Label(appops.CutText(f.After, 400)).Send()
				fg := detailMutedFg
				switch f.Change {
				case ChangeAdded.String():
					fg = color.Hex(styletokens.SuccessDefault.AsHex())
				case ChangeRemoved.String():
					fg = color.Hex(styletokens.ErrorDefault.AsHex())
				case ChangeChanged.String():
					fg = color.Hex(styletokens.WarningDefault.AsHex())
				}
				c.LabelAtoms(c.Atoms().BeginRichTextColored(fg, detailBgClear, f.Change).End().Keep()).Send()
				c.EndRow()
			}
		}
	}
}

// asOfText is the entity's state as of the window's end in one line, per
// domain, and the event each comes from; "" outside the entity case.
func asOfText(a *AsOf, format func(t time.Time) string) string {
	if a == nil {
		return ""
	}
	if len(a.Events) == 0 {
		return "As of " + format(a.Bound) + ": no event of " + a.Entity + " at or before it within the look-back."
	}
	parts := make([]string, 0, len(a.Events))
	for i := range a.Events {
		s := asOfHead(&a.Events[i], format)
		if kv := asOfAttrs(&a.Events[i]); len(kv) > 0 {
			s += ": " + strings.Join(kv, ", ")
		}
		parts = append(parts, s)
	}
	return "As of " + format(a.Bound) + ", " + a.Entity + "'s state " + strings.Join(parts, "; ")
}

// asOfHead names the domain a state is in and the event it comes from.
func asOfHead(e *AuditDetail, format func(t time.Time) string) string {
	return "in domain " + e.Domain + " comes from event id " + strconv.FormatUint(e.Id, 10) +
		" (" + e.Domain + "." + e.Action + ") at " + format(e.Ts())
}

// asOfAttrs is an event's attributes as key=value, the from claim left out.
func asOfAttrs(e *AuditDetail) (kv []string) {
	for i, k := range e.AttrKeys {
		if i < len(e.AttrValues) && k != audit.AttrFrom && k != audit.AttrFromBy {
			kv = append(kv, k+"="+e.AttrValues[i])
		}
	}
	return
}

func (inst *App) asOfLine() string {
	if inst.caseValue.Kind != CaseEntity || inst.cache.scope.Case != inst.caseValue {
		return ""
	}
	return appops.CutText(asOfText(inst.cache.asOf, func(t time.Time) string { return inst.picker.format(t, time.RFC3339) }), 400)
}

// renderAsOf is the as-of state in the detail pane for the entity case:
// the event it comes from, when, and its attributes as a table.
func (inst *App) renderAsOf() {
	a := inst.cache.asOf
	if inst.caseValue.Kind != CaseEntity || a == nil || inst.cache.scope.Case != inst.caseValue {
		return
	}
	format := func(t time.Time) string { return inst.picker.format(t, time.RFC3339) }
	if len(a.Events) == 0 {
		c.Label(asOfText(a, format)).Wrap().Send()
		c.Separator().Horizontal().Send()
		return
	}
	for range c.IdScope(inst.ids.PrepareStr("asof")) {
		for i := range a.Events {
			e := &a.Events[i]
			c.Label("As of " + format(a.Bound) + ", " + a.Entity + "'s state " + asOfHead(e, format) + ":").Wrap().Send()
			for range c.Grid(inst.ids.PrepareStr("grid-" + e.Domain)).NumColumns(2).Striped(true).KeepIter() {
				for j, k := range e.AttrKeys {
					if j < len(e.AttrValues) {
						c.Label(k).Send()
						c.Label(appops.CutText(e.AttrValues[j], 400)).Send()
						c.EndRow()
					}
				}
			}
		}
	}
	c.Separator().Horizontal().Send()
}
