// Package tally is the browser for the lading store (ADR-0200): mounts and
// their snapshots on the left, two panes of one snapshot each as a file tree
// in the middle, and the selected file's preview, its recorded attributes,
// its history and the diff between the panes below. It reads through the
// io/fs adapter and the SQL surface and never writes.
package tally

import (
	"context"
	"fmt"
	"io/fs"
	"math"
	"path"
	"time"

	"github.com/rs/zerolog"

	playlaunch "github.com/stergiotis/boxer/apps/play/launchcfg"
	"github.com/stergiotis/boxer/apps/tally/launchcfg"
	"github.com/stergiotis/boxer/public/identity/identifier"
	"github.com/stergiotis/boxer/public/keelson/designsystem/styletokens"
	"github.com/stergiotis/boxer/public/keelson/runtime/app"
	"github.com/stergiotis/boxer/public/keelson/runtime/buscodec"
	"github.com/stergiotis/boxer/public/keelson/runtime/clipboardbroker"
	"github.com/stergiotis/boxer/public/keelson/runtime/icons"
	"github.com/stergiotis/boxer/public/keelson/runtime/task"
	"github.com/stergiotis/boxer/public/keelson/runtime/windowhost"
	"github.com/stergiotis/boxer/public/observability/eh"
	"github.com/stergiotis/boxer/public/semistructured/leeway/marshall/clickhouse/componentsql"
	c "github.com/stergiotis/boxer/public/thestack/imzero2/egui2/bindings"
	"github.com/stergiotis/boxer/public/thestack/imzero2/egui2/colwidth"
	"github.com/stergiotis/boxer/public/thestack/imzero2/egui2/widgets/color"
	"github.com/stergiotis/boxer/public/thestack/imzero2/egui2/widgets/fsbrowser"
	"github.com/stergiotis/boxer/public/thestack/imzero2/egui2/widgets/lazypane"
	"github.com/stergiotis/boxer/public/thestack/imzero2/egui2/widgets/timeline"
	"github.com/stergiotis/boxer/public/thestack/imzero2/egui2/widgets/timeline/layout"
	"github.com/stergiotis/boxer/public/thestack/imzero2/egui2/widgets/treemap"
)

const (
	dockTabBrowseA  uint64 = 1
	dockTabBrowseB  uint64 = 2
	dockTabPreview  uint64 = 3
	dockTabInfo     uint64 = 4
	dockTabHistory  uint64 = 5
	dockTabDiff     uint64 = 6
	dockTabFind     uint64 = 7
	dockTabDu       uint64 = 8
	dockTabProblems uint64 = 9

	mountsPaneWidth float32 = 300
	connectTimeout          = 2 * time.Minute
)

// paneIDE names the two browser panes.
type paneIDE uint8

const (
	paneIDA paneIDE = iota
	paneIDB
)

// App is one tally window.
type App struct {
	ids     *c.WidgetIdStack
	log     zerolog.Logger
	bus     app.BusI
	density styletokens.DensityE

	conn      lane[*storeConn]
	mounts    lane[[]mountRow]
	mountRows []mountRow

	panes       [2]pane
	target      paneIDE // which pane the Mounts clicks address
	syncBrowse  bool
	diffThisDir bool

	preview lane[previewContent]
	info    lane[[]infoRow]
	history lane[tableResult]
	diff    lane[tableResult]

	historyTable stringTable
	diffTable    stringTable
	historyTL    *timeline.Timeline
	historyTLKey string

	find          findState
	findLane      lane[tableResult]
	duLane        lane[tableResult]
	duFilesLane   lane[tableResult]
	duTable       stringTable
	duTree        *treemap.Treemap
	duTreeKey     string
	duPaneW       float32
	duPaneH       float32
	problemsLane  lane[tableResult]
	problemsTable stringTable
	auditLane     lane[tableResult]
	auditArmed    string
	components    lane[[]componentHit]

	// tasks reports a recording's peaks build as a keelson task (ADR-0038);
	// nil when the host gave no bus.
	tasks task.TaskApiI

	lazy   map[uint64]*lazypane.Pane
	status string
	imgGen uint64
	imgs   *c.ImageVersionTracker[string]

	// workingset intent tracking (ADR-0148): baseline once the mounts are
	// known, dirty on any later change.
	mountsSeen          bool
	workingsetSeen      launchcfg.TallyLaunch
	workingsetSeenTaken bool
	workingsetDirty     bool

	// column-width persistence (ADR-0151): acquired on the first frame from
	// the host, nil when the host has no store — every table then renders
	// its defaults and nothing persists.
	colWidths     *colwidth.Resolver
	colWidthsInit bool
}

var _ app.WorkingsetComposerI = (*App)(nil)

// pane is one browser pane's location and view state (ADR-0200 §SD3): a
// mount, a snapshot or "latest", and the path the browser widget keeps.
type pane struct {
	st           fsbrowser.State
	mount        identifier.TaggedId
	snap         time.Time // zero = follow latest
	followLatest bool
	mode         fsbrowser.ModeE
	showHidden   bool
	fsys         fs.FS
	fsKey        viewKey
	fsErr        error
	selected     string // the one selected file, for Preview / Info / History
	navigated    bool   // this frame
	// paneH is the last height this pane's probe answered with; the browser
	// is sized to it. Held across frames: the probe is a frame late and
	// absent on the frame the tab comes back.
	paneH float32
}

// panePaneProbeSalt namespaces a browser pane's probe inside the shared r21
// slot map; threading it through the instance's id stack makes the slot
// window-unique, and ProbeSeq over the pane id separates A from B.
const panePaneProbeSalt uint64 = 0x7a11179a4e000001

var _ app.AppI = (*App)(nil)

func newApp() (inst *App) {
	inst = &App{
		ids:         c.NewWidgetIdStack(),
		density:     styletokens.ActiveDensity(),
		lazy:        map[uint64]*lazypane.Pane{},
		imgs:        c.NewImageVersionTracker[string](),
		diffThisDir: true,
	}
	inst.panes[paneIDA].followLatest = true
	inst.panes[paneIDB].followLatest = true
	// The preview lane is the only owner of an open recording: it closes the
	// one it replaces, so browsing away from a track releases its staged
	// bytes, its decoders and the output device.
	inst.preview.dispose = func(content previewContent) {
		if content.audio == nil {
			return
		}
		if err := content.audio.closeE(); err != nil {
			inst.log.Warn().Err(err).Msg("tally: closing a recording")
		}
	}
	inst.historyTable = stringTable{scopeKey: "history-table", selected: -1}
	inst.diffTable = stringTable{scopeKey: "diff-table", selected: -1}
	inst.find.table = stringTable{scopeKey: "find-table", selected: -1}
	inst.duTable = stringTable{scopeKey: "du-table", selected: -1}
	inst.problemsTable = stringTable{scopeKey: "problems-table", selected: -1}
	return
}

func (inst *App) Manifest() (m app.Manifest) { m = manifest; return }

func (inst *App) Mount(ctx app.MountContextI) (err error) {
	inst.ids = ctx.Ids()
	inst.log = ctx.Log()
	inst.bus = ctx.Bus()
	if inst.bus != nil {
		inst.tasks = task.NewBusApi(task.ApiConfig{Bus: inst.bus})
	}
	inst.status = "connecting…"
	if raw := ctx.LaunchConfig(); len(raw) > 0 {
		cfg, dErr := buscodec.Decode[launchcfg.TallyLaunch](raw)
		if dErr != nil {
			err = eh.Errorf("tally: decode launch config: %w", dErr)
			return
		}
		inst.applyLaunch(cfg)
	}
	inst.conn.demand("connect", func(cctx context.Context) (*storeConn, error) {
		cctx, cancel := context.WithTimeout(cctx, connectTimeout)
		defer cancel()
		return connect(cctx)
	})
	return
}

func (inst *App) Unmount(ctx app.MountContextI) (err error) {
	inst.preview.close()
	inst.info.close()
	inst.history.close()
	inst.diff.close()
	inst.findLane.close()
	inst.duLane.close()
	inst.duFilesLane.close()
	inst.problemsLane.close()
	inst.auditLane.close()
	inst.components.close()
	inst.mounts.close()
	if sc, done, _, _ := inst.conn.demand("connect", nil); done && sc != nil {
		sc.close()
	}
	inst.conn.close()
	return
}

func (inst *App) Frame(ctx app.FrameContextI) (err error) {
	// Re-resolve: the density preset is runtime-switchable (Layout ▸ Density).
	inst.density = styletokens.ActiveDensity()
	inst.ensureColWidths(ctx)
	inst.renderBody()
	inst.flushColWidths()
	return
}

// ensureColWidths acquires the column-width store once, on a frame — it is a
// frame-context capability (ADR-0155 §SD1), not a mount-time one — and
// builds the resolver with the bounds the widget drags against, so a stored
// width and a dragged one agree (ADR-0151, its 2026-08-16 updates).
func (inst *App) ensureColWidths(ctx app.FrameContextI) {
	if inst.colWidthsInit {
		return
	}
	inst.colWidthsInit = true
	h, ok := ctx.(colwidth.HostI)
	if !ok {
		return
	}
	store := h.ColumnWidthStore()
	if store == nil {
		return
	}
	res, err := colwidth.New(store, colwidth.Opts{
		AppId:       ctx.AppId(),
		InstanceKey: ctx.InstanceKey(),
		MinPoints:   float64(fsbrowser.MinColumnWidth(inst.density)),
		MaxPoints:   float64(fsbrowser.MaxColumnWidth),
	})
	if err != nil {
		inst.log.Warn().Err(err).Msg("tally: column-width resolver unavailable")
		return
	}
	if lerr := res.Load(); lerr != nil {
		inst.log.Warn().Err(lerr).Msg("tally: stored column widths could not be loaded")
	}
	inst.colWidths = res
	for _, t := range inst.tables() {
		t.res = res
	}
}

// tables is every result table the app owns, for wiring the resolver.
func (inst *App) tables() []*stringTable {
	return []*stringTable{&inst.historyTable, &inst.diffTable, &inst.find.table, &inst.duTable, &inst.problemsTable}
}

// flushColWidths writes captured widths once their debounce has passed; a
// failed write stays dirty and retries next frame.
func (inst *App) flushColWidths() {
	if inst.colWidths == nil {
		return
	}
	if _, err := inst.colWidths.Flush(time.Now()); err != nil {
		inst.log.Warn().Err(err).Msg("tally: storing column widths failed; will retry")
	}
}

// store is the connection once it is there; nil while connecting or failed.
func (inst *App) store() *storeConn {
	sc, done, cerr, busy := inst.conn.demand("connect", nil)
	switch {
	case busy:
		inst.status = "connecting…"
		c.RequestRepaint()
		return nil
	case !done:
		return nil
	case cerr != nil:
		inst.status = "not connected: " + cerr.Error()
		return nil
	}
	return sc
}

func (inst *App) renderBody() {
	sc := inst.store()
	if sc != nil {
		inst.pollMounts(sc)
	}
	inst.syncWorkingsetDirty()
	inst.panes[paneIDA].navigated, inst.panes[paneIDB].navigated = false, false
	for range c.PanelTopInside(inst.ids.PrepareStr("tally-top")).Resizable(false).KeepIter() {
		inst.renderToolbar(sc)
	}
	for range c.PanelLeftInside(inst.ids.PrepareStr("tally-mounts")).ExactSize(mountsPaneWidth).KeepIter() {
		for range c.ScrollArea().Vscroll(true).AutoShrink(false, false).KeepIter() {
			inst.renderMounts(sc)
		}
	}
	for range c.PanelCentralInside().KeepIter() {
		if sc == nil {
			c.Label(inst.status).Send()
			continue
		}
		for dock := range c.DockArea(inst.ids.PrepareStr("tally-dock")) {
			// Bottom leaf first so it spans the window, then pane B to the
			// right of pane A in what is left above it (the imztop shape).
			root := dock.InitRoot(dockTabBrowseA)
			_ = dock.Split(root, c.DockBelow, 0.6, dockTabPreview, dockTabInfo, dockTabHistory, dockTabDiff, dockTabFind, dockTabDu, dockTabProblems)
			_ = dock.Split(root, c.DockRight, 0.5, dockTabBrowseB)
			for range dock.Tab(dockTabBrowseA, "Pane A") {
				inst.renderPane(sc, paneIDA)
			}
			for range dock.Tab(dockTabBrowseB, "Pane B") {
				inst.renderPane(sc, paneIDB)
			}
			for range dock.Tab(dockTabPreview, "Preview") {
				if inst.lazyBody(dockTabPreview, "Preview") {
					continue
				}
				inst.renderPreview(sc)
			}
			for range dock.Tab(dockTabInfo, "Info") {
				if inst.lazyBody(dockTabInfo, "Info") {
					continue
				}
				inst.renderInfo(sc)
			}
			for range dock.Tab(dockTabHistory, "History") {
				if inst.lazyBody(dockTabHistory, "History") {
					continue
				}
				inst.renderHistory(sc)
			}
			for range dock.Tab(dockTabDiff, "Diff") {
				if inst.lazyBody(dockTabDiff, "Diff") {
					continue
				}
				inst.renderDiff(sc)
			}
			for range dock.Tab(dockTabFind, "Find") {
				if inst.lazyBody(dockTabFind, "Find") {
					continue
				}
				inst.renderFind(sc)
			}
			for range dock.Tab(dockTabDu, "Du") {
				if inst.lazyBody(dockTabDu, "Du") {
					continue
				}
				inst.renderDu(sc)
			}
			for range dock.Tab(dockTabProblems, "Problems") {
				if inst.lazyBody(dockTabProblems, "Problems") {
					continue
				}
				inst.renderProblems(sc)
			}
		}
	}
	inst.applySyncBrowsing()
}

// applySyncBrowsing keeps the two panes on one path when asked: whichever
// pane navigated this frame leads, the other follows (ADR-0200 §SD3).
func (inst *App) applySyncBrowsing() {
	if !inst.syncBrowse {
		return
	}
	a, b := &inst.panes[paneIDA], &inst.panes[paneIDB]
	switch {
	case a.navigated:
		b.st.SetDir(a.st.Dir())
	case b.navigated:
		a.st.SetDir(b.st.Dir())
	}
}

func (inst *App) lazyBody(dockID uint64, title string) (skip bool) {
	pane := inst.lazy[dockID]
	if pane == nil {
		pane = lazypane.New("tally-dock-tab-"+title, title)
		inst.lazy[dockID] = pane
	}
	return pane.Skip()
}

// pollMounts keeps the mount list current: it runs once on connect and again
// on Refresh.
func (inst *App) pollMounts(sc *storeConn) {
	rows, done, merr, busy := inst.mounts.demand("mounts", func(ctx context.Context) ([]mountRow, error) {
		return sc.listMounts(ctx)
	})
	if busy {
		c.RequestRepaint()
		return
	}
	if done {
		if merr != nil {
			inst.status = "mounts: " + merr.Error()
			return
		}
		inst.mountRows = rows
		inst.mountsSeen = true
		if len(rows) > 0 {
			if inst.panes[paneIDA].mount == 0 {
				inst.panes[paneIDA].mount = rows[0].id
			}
			if inst.panes[paneIDB].mount == 0 {
				inst.panes[paneIDB].mount = rows[0].id
			}
		}
		inst.status = fmt.Sprintf("%d mounts", len(rows))
	}
}

func (inst *App) activePane() *pane { return &inst.panes[inst.target] }

func (inst *App) renderToolbar(sc *storeConn) {
	p := inst.activePane()
	for range c.HorizontalTop().KeepIter() {
		c.LabelAtoms(c.Atoms().BeginRichText(icons.PhFolderOpen + " tally").Strong().End().Keep()).Selectable(false).Send()
		c.AddSpace(styletokens.GapInline(inst.density) * 3)
		c.Label("Target").Selectable(false).Send()
		if c.Button(inst.ids.PrepareStr("tb-target-a"), c.Atoms().Text("A").Keep()).
			Selected(inst.target == paneIDA).SendResp().HasPrimaryClicked() {
			inst.target = paneIDA
		}
		if c.Button(inst.ids.PrepareStr("tb-target-b"), c.Atoms().Text("B").Keep()).
			Selected(inst.target == paneIDB).SendResp().HasPrimaryClicked() {
			inst.target = paneIDB
		}
		c.AddSpace(styletokens.GapInline(inst.density) * 2)
		if c.Button(inst.ids.PrepareStr("tb-list"), c.Atoms().Text(icons.PhListBullets+" List").Keep()).
			Selected(p.mode == fsbrowser.ModeList).SendResp().HasPrimaryClicked() {
			p.mode = fsbrowser.ModeList
		}
		if c.Button(inst.ids.PrepareStr("tb-outline"), c.Atoms().Text(icons.PhTreeStructure+" Outline").Keep()).
			Selected(p.mode == fsbrowser.ModeOutline).SendResp().HasPrimaryClicked() {
			p.mode = fsbrowser.ModeOutline
		}
		c.AddSpace(styletokens.GapInline(inst.density))
		c.Checkbox(inst.ids.PrepareStr("tb-hidden"), p.showHidden, "Hidden files").SendRespVal(&p.showHidden)
		c.AddSpace(styletokens.GapInline(inst.density))
		if c.Checkbox(inst.ids.PrepareStr("tb-sync"), inst.syncBrowse, "Sync browsing").SendRespVal(&inst.syncBrowse).HasChanged() && inst.syncBrowse {
			inst.panes[paneIDB].st.SetDir(inst.panes[paneIDA].st.Dir())
		}
		c.AddSpace(styletokens.GapInline(inst.density))
		if c.Button(inst.ids.PrepareStr("tb-refresh"), c.Atoms().Text(icons.PhArrowsClockwise+" Refresh").Keep()).
			SendResp().HasPrimaryClicked() && sc != nil {
			inst.mounts.invalidate()
		}
		c.AddSpace(styletokens.GapInline(inst.density))
		if c.Button(inst.ids.PrepareStr("tb-open-play"), c.Atoms().Text(icons.PhArrowSquareOut+" Open in play").Keep()).
			SendResp().HasPrimaryClicked() {
			inst.openInPlay(p)
		}
		if c.Button(inst.ids.PrepareStr("tb-copy-path"), c.Atoms().Text(icons.PhLinkSimple+" Copy SFTP path").Keep()).
			SendResp().HasPrimaryClicked() {
			inst.copyToClipboard(inst.sftpPath(p))
		}
		if c.Button(inst.ids.PrepareStr("tb-copy-rclone"), c.Atoms().Text(icons.PhTerminal+" Copy rclone mount").Keep()).
			SendResp().HasPrimaryClicked() {
			if loc, ok := inst.locationOf(p); ok {
				inst.copyToClipboard(rcloneMountCommand(loc, p.followLatest))
			}
		}
		c.AddSpace(styletokens.GapInline(inst.density) * 3)
		c.Label(inst.status).Selectable(false).Send()
	}
}

// locationOf resolves a pane's place: its mount and the snapshot it shows.
func (inst *App) locationOf(p *pane) (loc location, ok bool) {
	snap, has := inst.paneSnapshot(p)
	if p.mount == 0 || !has {
		return
	}
	return location{mount: p.mount, snap: snap}, true
}

// sftpPath is the pane's location as the SFTP head spells it:
// /<mount>/<snapshot>/<path>, the selected file when there is one.
func (inst *App) sftpPath(p *pane) string {
	if p.mount == 0 {
		return "/"
	}
	snap := "latest"
	if s, ok := inst.paneSnapshot(p); ok && !p.followLatest {
		snap = s.UTC().Format("20060102T150405.000000000Z")
	}
	loc := "/" + hexID(p.mount) + "/" + snap
	switch {
	case p.selected != "":
		loc += "/" + p.selected
	case p.st.Dir() != ".":
		loc += "/" + p.st.Dir()
	}
	return loc
}

func (inst *App) copyToClipboard(text string) {
	if inst.bus == nil {
		return
	}
	bus := inst.bus
	go func() { _, _ = bus.Request(clipboardbroker.SubjectWrite, []byte(text)) }()
	inst.status = "copied"
}

// openInPlay hands the pane's directory to play as a query (ADR-0135): the
// buffer stands on its own, mount and snapshot pinned as literals.
func (inst *App) openInPlay(p *pane) {
	if inst.bus == nil {
		return
	}
	loc, ok := inst.locationOf(p)
	if !ok {
		inst.status = "nothing to open: the pane has no snapshot"
		return
	}
	cfg := playlaunch.PlayLaunch{Sql: openInPlaySQL(loc, p.st.Dir()), AutoRun: true}
	cfgBytes, err := buscodec.Encode(cfg)
	if err != nil {
		inst.status = "open in play: " + err.Error()
		return
	}
	bus := inst.bus
	go func() { _, _ = windowhost.RequestOpen(bus, playlaunch.AppId, playlaunch.Kind, cfgBytes) }()
	inst.status = "opened in play"
}

// renderMounts is the Mounts pane: every mount, and the target pane's mount
// with its snapshots and the follow-latest toggle.
func (inst *App) renderMounts(sc *storeConn) {
	c.LabelAtoms(c.Atoms().BeginRichText("Mounts").Strong().End().Keep()).Selectable(false).Send()
	if sc == nil {
		c.Label(inst.status).Selectable(false).Send()
		return
	}
	if len(inst.mountRows) == 0 {
		c.Label("No mounts in the store yet — take one with `boxer fs snapshot`.").Send()
		return
	}
	p := inst.activePane()
	c.Label(fmt.Sprintf("Clicks set pane %s", inst.target.String())).Selectable(false).Send()
	for i := range inst.mountRows {
		m := inst.mountRows[i]
		sel := m.id == p.mount
		label := m.label()
		if n := len(m.snapshots); n > 0 {
			label += fmt.Sprintf("  ·  %d", n)
		}
		if c.Button(inst.ids.PrepareSeq(0x1000+uint64(i)), c.Atoms().Text(icons.PhFolder+" "+label).Keep()).
			Selected(sel).SendResp().HasPrimaryClicked() && !sel {
			inst.selectMount(p, m.id)
		}
		if !sel {
			continue
		}
		for range c.IdScope(inst.ids.PrepareStr("snaps")) {
			c.AddSpace(styletokens.GapInline(inst.density))
			c.Label("id " + hexID(m.id)).Selectable(false).Send()
			if m.store != "" {
				c.Label("store " + m.store).Selectable(false).Send()
			}
			c.Checkbox(inst.ids.PrepareStr("follow"), p.followLatest, "Follow latest").SendRespVal(&p.followLatest)
			for j, s := range m.snapshots {
				pinned := !p.followLatest && p.snap.Equal(s.Snap)
				isLatest := j == 0
				text := s.Snap.UTC().Format("2006-01-02 15:04:05")
				if isLatest {
					text += "  (latest)"
				}
				text += fmt.Sprintf("\n%d entries · %s · expires %s", s.Entries, humanSize(int64(s.Bytes)), s.ExpiresAt.UTC().Format("2006-01-02"))
				if c.Button(inst.ids.PrepareSeq(0x2000+uint64(j)), c.Atoms().Text(text).Keep()).
					Selected(pinned || (p.followLatest && isLatest)).
					SendResp().HasPrimaryClicked() {
					inst.pinSnapshot(p, s.Snap)
				}
			}
		}
	}
}

func (id paneIDE) String() string {
	if id == paneIDB {
		return "B"
	}
	return "A"
}

func (inst *App) selectMount(p *pane, id identifier.TaggedId) {
	p.mount = id
	p.snap = time.Time{}
	p.followLatest = true
	p.selected = ""
	p.st.SetDir(".")
	c.CurrentApplicationState.StateManager.OverrideDatabindingBPtr(&p.followLatest)
}

// pinSnapshot points a pane at one snapshot and stops it following latest.
// The path stays: that is what makes it time travel rather than a reload.
func (inst *App) pinSnapshot(p *pane, snap time.Time) {
	p.followLatest = false
	p.snap = snap
	c.CurrentApplicationState.StateManager.OverrideDatabindingBPtr(&p.followLatest)
}

// paneSnapshot resolves the pane's snapshot: the pinned one, or the mount's
// newest complete one when following.
func (inst *App) paneSnapshot(p *pane) (snap time.Time, ok bool) {
	if !p.followLatest && !p.snap.IsZero() {
		return p.snap, true
	}
	for _, m := range inst.mountRows {
		if m.id == p.mount {
			if s, has := m.latest(); has {
				return s.Snap, true
			}
			return time.Time{}, false
		}
	}
	return time.Time{}, false
}

// paneFS is the adapter view behind the pane, opened when the location
// changes and kept otherwise.
func (inst *App) paneFS(sc *storeConn, p *pane) (fsys fs.FS, key viewKey, ok bool) {
	snap, has := inst.paneSnapshot(p)
	if p.mount == 0 || !has {
		return nil, viewKey{}, false
	}
	key = viewKey{mount: p.mount, snap: snap.UnixNano()}
	if p.fsys != nil && p.fsKey == key {
		return p.fsys, key, true
	}
	fsys, err := sc.view(p.mount, snap)
	if err != nil {
		p.fsErr = err
		return nil, key, false
	}
	p.fsys, p.fsKey, p.fsErr = fsys, key, nil
	return fsys, key, true
}

func (inst *App) mountLabel(id identifier.TaggedId) string {
	for _, m := range inst.mountRows {
		if m.id == id {
			return m.label()
		}
	}
	return hexID(id)
}

func (inst *App) renderPane(sc *storeConn, id paneIDE) {
	p := &inst.panes[id]
	fsys, key, ok := inst.paneFS(sc, p)
	if !ok {
		switch {
		case p.mount == 0:
			c.Label("Pick a mount on the left.").Send()
		case p.fsErr != nil:
			c.Label("Cannot open the snapshot: " + p.fsErr.Error()).Send()
		default:
			c.Label("This mount has no complete snapshot.").Send()
		}
		return
	}
	snapText := "latest"
	if !p.followLatest {
		snapText = time.Unix(0, key.snap).UTC().Format("2006-01-02 15:04:05")
	}
	// The browser is the whole tab, so the tab's height is its ceiling: a long
	// listing fills the pane, a short one stays short. Probed before it,
	// because the rect a probe reports is the room left for the NEXT widget;
	// held on the pane because the answer is a frame late and absent on the
	// frame the tab comes back. Without a height the browser's etable takes
	// the interpreter's 400 px auto-fit cap and leaves the bottom of the pane
	// empty.
	seq := c.ProbeSeq("tally-pane", id.String()) ^ inst.ids.PrepareHighEntropy(panePaneProbeSalt).Derive()
	if _, h, ok := c.CapturePaneSize(seq); ok && h > 0 && !math.IsNaN(float64(h)) {
		p.paneH = h
	}
	res := fsbrowser.Render(fsbrowser.Input{
		Ids:        inst.ids,
		ScopeKey:   "pane-" + id.String(),
		FS:         fsys,
		RootLabel:  inst.mountLabel(p.mount) + " @ " + snapText,
		CacheKey:   fmt.Sprintf("%x@%d", key.mount.Value(), key.snap),
		State:      &p.st,
		Mode:       p.mode,
		ShowHidden: p.showHidden,
		Striped:    true,
		Widths:     inst.colWidths,
		WidthTag:   "pane-" + id.String(),
		MaxHeight:  p.paneH,
	})
	if res.Err != nil {
		inst.status = res.Err.Error()
	}
	p.navigated = res.Navigated
	if res.Clicked >= 0 || res.Activated >= 0 || res.Navigated {
		inst.target = id
	}
	// The preview follows the single selected file; a directory selection or
	// a multi-selection shows nothing.
	sel := p.st.Selection()
	if len(sel) == 1 {
		if e, found := entryOf(res.Rows, sel[0]); found && !e.IsDir {
			p.selected = sel[0]
		} else {
			p.selected = ""
		}
	} else {
		p.selected = ""
	}
	if res.Activated >= 0 && res.Activated < len(res.Rows) {
		p.selected = res.Rows[res.Activated].Path
	}
}

func entryOf(rows []fsbrowser.Entry, p string) (e fsbrowser.Entry, ok bool) {
	for i := range rows {
		if rows[i].Path == p {
			return rows[i], true
		}
	}
	return
}

// focusPane is the pane the lower tabs describe: the target pane.
func (inst *App) focusPane() *pane { return inst.activePane() }

func (inst *App) renderPreview(sc *storeConn) {
	p := inst.focusPane()
	if p.selected == "" {
		c.Label("Select a file to preview it.").Send()
		return
	}
	fsys, key, ok := inst.paneFS(sc, p)
	if !ok {
		return
	}
	laneKey := fmt.Sprintf("%x@%d:%s", key.mount.Value(), key.snap, p.selected)
	path := p.selected
	content, done, perr, busy := inst.preview.demand(laneKey, func(ctx context.Context) (previewContent, error) {
		return loadPreview(ctx, fsys, path)
	})
	if busy {
		c.RequestRepaint()
		for range c.HorizontalTop().KeepIter() {
			c.Spinner().Send()
			c.Label("Loading " + path + "…").Send()
		}
		return
	}
	if !done {
		return
	}
	if perr != nil {
		c.Label("Cannot preview " + path + ": " + perr.Error()).Send()
		return
	}
	header := fmt.Sprintf("%s  ·  %s", path, humanSize(content.size))
	c.LabelAtoms(c.Atoms().BeginRichText(header).Strong().End().Keep()).Selectable(false).Send()
	if content.note != "" {
		c.Label(content.note).Send()
	}
	if content.kind == previewKindAudio {
		// The player captures the wheel and owns its own drag, so it draws
		// outside the scroll area rather than fighting it for both.
		for range c.IdScope(inst.ids.PrepareStr("preview-audio")) {
			inst.renderAudioPreview(content.audio)
		}
		return
	}
	for range c.ScrollArea().Vscroll(true).Hscroll(true).AutoShrink(false, false).KeepIter() {
		for range c.IdScope(inst.ids.PrepareStr("preview-body")) {
			switch content.kind {
			case previewKindMarkdown:
				if content.doc != nil {
					content.doc.Render(inst.ids)
				}
			case previewKindImage:
				inst.renderImage(laneKey, content)
			case previewKindNone, previewKindError:
			default:
				c.CodeView(inst.ids.PrepareStr("preview-code"), content.job).Send()
			}
		}
	}
}

func (inst *App) renderImage(key string, content previewContent) {
	if len(content.pixels) == 0 {
		return
	}
	imgID := inst.ids.PrepareStr("preview-img").Derive()
	pixels := inst.imgs.PixelsToSendFor(key, imgID, inst.imgGen, content.pixels)
	boxW := min(content.imgW, uint32(previewImageMaxW))
	boxH := min(content.imgH, uint32(previewImageMaxH))
	c.Image(inst.ids.PrepareStr("preview-img"),
		content.imgW, content.imgH, inst.imgGen,
		uint8(c.FitAspectMaxE), boxW, boxH,
		uint8(c.FilterLinearE), c.TintNoneRgba, pixels).
		Send()
	c.Label(fmt.Sprintf("%d × %d px", content.imgW, content.imgH)).Selectable(false).Send()
}

func (inst *App) renderInfo(sc *storeConn) {
	p := inst.focusPane()
	if p.selected == "" {
		c.Label("Select a file to see its recorded attributes.").Send()
		return
	}
	snap, has := inst.paneSnapshot(p)
	if !has {
		return
	}
	laneKey := fmt.Sprintf("%x@%d:%s", p.mount.Value(), snap.UnixNano(), p.selected)
	mount, path := p.mount, p.selected
	rows, done, ierr, busy := inst.info.demand(laneKey, func(ctx context.Context) ([]infoRow, error) {
		return loadInfo(ctx, sc.exec, mount, snap, path)
	})
	if busy {
		c.RequestRepaint()
		for range c.HorizontalTop().KeepIter() {
			c.Spinner().Send()
			c.Label("Reading " + path + "…").Send()
		}
		return
	}
	if !done {
		return
	}
	if ierr != nil {
		c.Label("Cannot read the entry: " + ierr.Error()).Send()
		return
	}
	c.LabelAtoms(c.Atoms().BeginRichText(path).Strong().End().Keep()).Selectable(false).Send()
	for range c.ScrollArea().Vscroll(true).AutoShrink(false, false).KeepIter() {
		for range c.Grid(inst.ids.PrepareStr("info-grid")).NumColumns(2).KeepIter() {
			c.Label("mount").Selectable(false).Send() // designlint:ignore=L1 (a field caption, like the column names beside it)
			c.Label(hexID(p.mount)).Send()
			c.EndRow()
			for _, r := range rows {
				c.Label(r.name).Selectable(false).Send()
				c.Label(r.value).Send()
				c.EndRow()
			}
		}
		inst.renderComponents(sc, mount, snap, path, laneKey)
	}
}

// renderComponents is the Info pane's "Components" section (ADR-0200 M5):
// which registered kinds carry this entry — the store's own on the entry row
// (a root row is an entry and a snapshot), and any kind another domain
// formulated over the same key in its own table.
func (inst *App) renderComponents(sc *storeConn, mount identifier.TaggedId, snap time.Time, path, laneKey string) {
	c.AddSpace(styletokens.GapInline(inst.density) * 2)
	c.LabelAtoms(c.Atoms().BeginRichText("Components").Strong().End().Keep()).Selectable(false).Send()
	hits, done, herr, busy := inst.components.demand(laneKey, func(ctx context.Context) ([]componentHit, error) {
		return loadComponents(ctx, sc.exec, componentsql.Default, mount, snap, path)
	})
	switch {
	case busy:
		c.RequestRepaint()
		c.Label("Looking up registered kinds…").Selectable(false).Send()
	case done && herr != nil:
		c.Label("Cannot look up components: " + herr.Error()).Send()
	case done && len(hits) == 0:
		c.Label("No registered kind names this entry.").Selectable(false).Send()
	case done:
		for _, h := range hits {
			c.Label(fmt.Sprintf("%s  ·  %s  ·  %d row(s)", h.kind, h.table, h.rows)).Selectable(false).Send()
		}
		c.Label(fmt.Sprintf("%d of %d registered kinds", len(hits), len(componentProbes(componentsql.Default, mount, snap, path)))).Selectable(false).Send()
	}
}

// renderHistory is the selected path across every complete snapshot of the
// target pane's mount (ADR-0198 §7 history): a timeline of its versions and
// a table; a click on a row pins the pane to that snapshot.
func (inst *App) renderHistory(sc *storeConn) {
	p := inst.focusPane()
	if p.mount == 0 {
		c.Label("Pick a mount on the left.").Send()
		return
	}
	target := p.selected
	if target == "" {
		target = p.st.Dir()
	}
	laneKey := fmt.Sprintf("%x:%s", p.mount.Value(), target)
	mount := p.mount
	res, done, herr, busy := inst.history.demand(laneKey, func(ctx context.Context) (tableResult, error) {
		return runTable(ctx, sc.exec, historySQL(mount, target))
	})
	if busy {
		c.RequestRepaint()
		for range c.HorizontalTop().KeepIter() {
			c.Spinner().Send()
			c.Label("Reading the history of " + target + "…").Send()
		}
		return
	}
	if !done {
		return
	}
	if herr != nil {
		c.Label("Cannot read the history: " + herr.Error()).Send()
		return
	}
	header := fmt.Sprintf("%s across %d snapshot(s) of %s", target, len(res.rows), inst.mountLabel(mount))
	c.LabelAtoms(c.Atoms().BeginRichText(header).Strong().End().Keep()).Selectable(false).Send()
	if len(res.rows) == 0 {
		c.Label("No snapshot of this mount carries that path.").Send()
		return
	}
	inst.renderHistoryTimeline(laneKey, res)
	inst.historyTable.resetFor(laneKey)
	inst.historyTable.headers = res.headers
	inst.historyTable.rows = res.rows
	inst.historyTable.widths = []float32{200, 80, 110, 170, 300, 90, 170}
	inst.historyTable.tone = nil
	if clicked := inst.historyTable.render(inst.ids, inst.density); clicked >= 0 {
		if snap, ok := snapOfRow(res, clicked); ok {
			inst.pinSnapshot(p, snap)
		}
	}
}

// renderHistoryTimeline paints one flag per snapshot carrying the path.
func (inst *App) renderHistoryTimeline(key string, res tableResult) {
	if inst.historyTL == nil {
		inst.historyTL = timeline.New(inst.ids, "tally-history-timeline", nil, timeline.WithInteractive(false))
	}
	if inst.historyTLKey != key {
		inst.historyTLKey = key
		anns := make([]*layout.Annotation, 0, len(res.rows))
		lo, hi := time.Time{}, time.Time{}
		sizeCol := columnIndex(res.headers, "size")
		for i := range res.rows {
			snap, ok := snapOfRow(res, i)
			if !ok {
				continue
			}
			label := snap.UTC().Format("2006-01-02 15:04:05")
			if sizeCol >= 0 {
				label += " · " + res.rows[i][sizeCol] + " B"
			}
			anns = append(anns, &layout.Annotation{TMS: snap.UnixMilli(), Number: int32(i + 1), Label: label})
			if lo.IsZero() || snap.Before(lo) {
				lo = snap
			}
			if hi.IsZero() || snap.After(hi) {
				hi = snap
			}
		}
		inst.historyTL.SetAnnotations(anns)
		if !lo.IsZero() {
			span := hi.Sub(lo)
			if span < 24*time.Hour {
				span = 24 * time.Hour
			}
			margin := span / 10
			inst.historyTL.SetRange(lo.Add(-margin), hi.Add(margin))
		}
	}
	inst.historyTL.Render()
}

// snapOfRow parses the `snap` column of a history row.
func snapOfRow(res tableResult, row int) (snap time.Time, ok bool) {
	col := columnIndex(res.headers, "snap")
	if col < 0 || row < 0 || row >= len(res.rows) || col >= len(res.rows[row]) {
		return
	}
	return parseSnapText(res.rows[row][col])
}

// parseSnapText reads the formats gloss renders a DateTime64 in.
func parseSnapText(s string) (t time.Time, ok bool) {
	for _, f := range []string{time.RFC3339Nano, "2006-01-02 15:04:05.999999999", "2006-01-02T15:04:05.999999999Z07:00", "2006-01-02 15:04:05"} {
		if v, err := time.Parse(f, s); err == nil {
			return v, true
		}
	}
	return
}

// renderDiff compares pane A (older side) with pane B (newer side): the
// ADR-0198 §7 diff as a coloured checklist; a click travels pane B to the
// path.
func (inst *App) renderDiff(sc *storeConn) {
	a, b := &inst.panes[paneIDA], &inst.panes[paneIDB]
	locA, okA := inst.locationOf(a)
	locB, okB := inst.locationOf(b)
	if !okA || !okB {
		c.Label("Both panes need a snapshot to compare.").Send()
		return
	}
	for range c.HorizontalTop().KeepIter() {
		c.Checkbox(inst.ids.PrepareStr("diff-this-dir"), inst.diffThisDir, "Pane A's directory only").SendRespVal(&inst.diffThisDir)
		c.AddSpace(styletokens.GapInline(inst.density))
		c.Label(fmt.Sprintf("A = %s @ %s  →  B = %s @ %s", inst.mountLabel(locA.mount), locA.snap.UTC().Format("2006-01-02 15:04:05"),
			inst.mountLabel(locB.mount), locB.snap.UTC().Format("2006-01-02 15:04:05"))).Selectable(false).Send()
	}
	if locA == locB {
		c.Label("The panes show the same snapshot — pin another snapshot, or another mount, in one of them.").Send()
		return
	}
	dir := "."
	if inst.diffThisDir {
		dir = a.st.Dir()
	}
	laneKey := locB.key() + "|" + locA.key() + "|" + dir
	res, done, derr, busy := inst.diff.demand(laneKey, func(ctx context.Context) (tableResult, error) {
		return runTable(ctx, sc.exec, diffSQL(locB, locA, dir))
	})
	if busy {
		c.RequestRepaint()
		for range c.HorizontalTop().KeepIter() {
			c.Spinner().Send()
			c.Label("Comparing…").Send()
		}
		return
	}
	if !done {
		return
	}
	if derr != nil {
		c.Label("Cannot compare: " + derr.Error()).Send()
		return
	}
	if len(res.rows) == 0 {
		c.Label("No difference" + scopeNote(dir)).Send()
		return
	}
	added, removed, modified := 0, 0, 0
	changeCol := columnIndex(res.headers, "change")
	tone := make([]color.Color, len(res.rows))
	for i, row := range res.rows {
		ch := ""
		if changeCol >= 0 && changeCol < len(row) {
			ch = row[changeCol]
		}
		switch ch {
		case "added":
			added++
			tone[i] = color.Hex(styletokens.SuccessDefault.AsHex())
		case "removed":
			removed++
			tone[i] = color.Hex(styletokens.ErrorDefault.AsHex())
		case "modified":
			modified++
			tone[i] = color.Hex(styletokens.WarningDefault.AsHex())
		default:
			tone[i] = color.Transparent
		}
	}
	c.Label(fmt.Sprintf("%d added · %d removed · %d modified%s", added, removed, modified, scopeNote(dir))).Selectable(false).Send()
	inst.diffTable.resetFor(laneKey)
	inst.diffTable.headers = res.headers
	inst.diffTable.rows = res.rows
	inst.diffTable.tone = tone
	inst.diffTable.widths = []float32{360, 90, 100, 100, 170, 170, 200, 200}
	if clicked := inst.diffTable.render(inst.ids, inst.density); clicked >= 0 {
		if pc := columnIndex(res.headers, "path"); pc >= 0 && pc < len(res.rows[clicked]) {
			inst.travelTo(b, res.rows[clicked][pc])
		}
	}
}

func scopeNote(dir string) string {
	if dir == "" || dir == "." {
		return " (whole snapshot)"
	}
	return " under " + dir + "/"
}

// travelTo puts a pane on a path: its directory opened, the entry selected.
func (inst *App) travelTo(p *pane, target string) {
	p.st.SetDir(path.Dir(target))
	p.st.SelectOnly(target)
	p.selected = target
	p.navigated = true
}
