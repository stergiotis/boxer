package mdedit

// The files pane: browse lading snapshots (ADR-0198's fs snapshot store,
// through ADR-0200's fsbrowser widget — this app is the widget's third host
// after tally and play) and load a file into the buffer.
//
// The source is a SNAPSHOT, pinned and read-only: activating a file loads its
// snapshotted bytes through the same dirty guard Open uses, the loaded
// document is not file-bound (saving it means Save as, through the Powerbox),
// and there is no follow — the snapshot cannot change. The first cut browses
// each mount's LATEST snapshot only; snapshot archaeology is tally's job.
// deferred: a snapshot picker, if reading old snapshots in the editor turns
// out to be wanted.
//
// The connection is lazy — nothing connects until the pane is first shown —
// and its failure renders as a hint inside the pane while the rest of the app
// stays untouched. Directory listings read on the render thread through
// ladingview.Locked (tally's arrangement; ADR-0198 calls the surface
// batch-shaped); only file loads leave the frame, on a lane.

import (
	"context"
	"fmt"
	"io/fs"
	"time"

	"github.com/stergiotis/boxer/public/fs/lading/ladingview"
	"github.com/stergiotis/boxer/public/keelson/runtime/icons"
	"github.com/stergiotis/boxer/public/observability/eh/eb"
	c "github.com/stergiotis/boxer/public/thestack/imzero2/egui2/bindings"
	"github.com/stergiotis/boxer/public/thestack/imzero2/egui2/widgets/fsbrowser"
)

const (
	// filesSplitFrac / filesMinWindowPx are the outline's shape: a derived
	// share of the window, hidden below a floor rather than starving the
	// panes doing the work.
	filesSplitFrac   = float32(0.20)
	filesMinWindowPx = float32(900)

	// filesFallbackPaneH stands in before the pane probe reports, for the
	// same reason the outline's does: without SOME height the browser's
	// etable takes the interpreter's auto-fit cap.
	filesFallbackPaneH = float32(360)

	// maxSnapshotDocBytes bounds a load. An editor buffer is not a place for
	// a blob; past this the pane refuses with the size in the status.
	maxSnapshotDocBytes = int64(4 << 20)

	// filesConnectTimeout bounds the lazy connect and the mount listing.
	filesConnectTimeout = 15 * time.Second
)

const (
	tipFiles = "Browse the lading snapshot store (boxer fs snapshot) and load a file into the editor. Read-only: the source is a pinned snapshot, so a loaded document is not bound to any file — Save as gives it one. The column needs a window wide enough to spare it and hides itself below that."

	tipFilesMount = "Which mount to browse, at its latest snapshot. Names come from the mount policy; unnamed mounts show their id."

	tipFileSnapshot = "The snapshot entry this document was loaded from (mount:path). A snapshot is pinned and read-only — the document is not bound to any file, and the first Save asks where."
)

// filesToggleLabel is the pane's checkbox in the bar.
const filesToggleLabel = icons.PhFolders + " Files"

// filesState is the pane's own state, one field on App.
type filesState struct {
	conn   lane[*storeConn]
	mounts lane[[]mountRow]
	load   lane[string]

	// mountKey pins the selection across mount-list refreshes; "" means the
	// first mount.
	mountKey string

	st    fsbrowser.State
	paneH float32

	// One load at a time: key identifies it into the lane, label is what the
	// document will be called once it lands.
	loadKey   string
	loadLabel string
}

// filesVisible is the outlineVisible shape: the toggle AND a window wide
// enough to give the pane a share.
func (inst *App) filesVisible() (yes bool) {
	if !inst.showFiles {
		return false
	}
	winW := inst.winW
	if winW <= 0 {
		winW = windowFallbackWidthPx
	}
	return winW >= filesMinWindowPx
}

func (inst *App) filesWidth() (px float32) {
	winW := inst.winW
	if winW <= 0 {
		winW = windowFallbackWidthPx
	}
	return winW * filesSplitFrac
}

// renderFiles draws the pane: the mount picker, the browser, and whatever a
// pending load has to say. Callers own the enclosing panel.
func (inst *App) renderFiles() {
	f := &inst.files

	// The lazy connect. The lane caches success forever (dispose closes it if
	// superseded) and failure until retried.
	sc, done, cerr, busy := f.conn.demand("connect", func(ctx context.Context) (*storeConn, error) {
		cctx, cancel := context.WithTimeout(ctx, filesConnectTimeout)
		defer cancel()
		return connectLading(cctx)
	})
	switch {
	case busy:
		c.RequestRepaint()
		c.Label("connecting…").Selectable(false).Send()
		return
	case !done:
		return
	case cerr != nil:
		// The hint, not a modal: the pane explains itself and the app is
		// otherwise unaffected.
		c.Label(cerr.Error()).Send()
		c.Label("Take a snapshot with `boxer fs snapshot <dir> --mount …` and set CLICKHOUSE_ENDPOINT.").Send()
		if c.Button(inst.ids.PrepareStr("files-retry"), c.Atoms().Text("Retry").Keep()).
			Small().SendResp().HasPrimaryClicked() {
			f.conn.invalidate()
		}
		return
	}

	rows, done, merr, busy := f.mounts.demand("mounts", func(ctx context.Context) ([]mountRow, error) {
		cctx, cancel := context.WithTimeout(ctx, filesConnectTimeout)
		defer cancel()
		return sc.listMounts(cctx)
	})
	switch {
	case busy:
		c.RequestRepaint()
		c.Label("listing mounts…").Selectable(false).Send()
		return
	case !done:
		return
	case merr != nil:
		c.Label(merr.Error()).Send()
		if c.Button(inst.ids.PrepareStr("files-mounts-retry"), c.Atoms().Text("Retry").Keep()).
			Small().SendResp().HasPrimaryClicked() {
			f.mounts.invalidate()
		}
		return
	}
	if len(rows) == 0 {
		c.Label("No snapshots recorded yet.").Send()
		c.Label("Take one with `boxer fs snapshot <dir> --mount …`.").Send()
		return
	}

	row := inst.pickMount(rows)
	snap, ok := row.latest()
	if !ok {
		c.Label("The mount has no complete snapshot yet.").Send()
		return
	}
	fsys, verr := sc.view(row.id, snap.Snap)
	if verr != nil {
		c.Label(verr.Error()).Send()
		return
	}

	// A pending load: poll it here (the pane is where the gesture lives) and
	// keep frames coming while it runs. The APPLY happens in renderBody's
	// drain, before the source pane renders — a rebind issued from this pane
	// would miss the frame's databinding override.
	if f.loadKey != "" {
		c.RequestRepaint()
	}

	if _, h, ok := c.CapturePaneSize(inst.paneProbeSeq("files-pane")); ok && h > 0 {
		f.paneH = h
	}
	paneH := f.paneH
	if paneH <= 0 {
		paneH = filesFallbackPaneH
	}

	res := fsbrowser.Render(fsbrowser.Input{
		Ids:       inst.ids,
		ScopeKey:  "files",
		FS:        fsys,
		RootLabel: row.label() + " @ latest",
		CacheKey:  fmt.Sprintf("%x@%d", row.id.Value(), snap.Snap.UnixNano()),
		State:     &f.st,
		Mode:      fsbrowser.ModeList,
		Striped:   true,
		MaxHeight: paneH,
	})
	if res.Err != nil {
		inst.status = res.Err.Error()
	}
	if res.Activated >= 0 && res.Activated < len(res.Rows) {
		e := res.Rows[res.Activated]
		if !e.IsDir {
			inst.requestSnapshotLoad(fsys, row.label(), fmt.Sprintf("%x@%d", row.id.Value(), snap.Snap.UnixNano()), e.Path)
		}
	}
}

// pickMount renders the mount picker and resolves the pinned selection
// against the current rows: the picked mount when it still exists, else the
// first.
func (inst *App) pickMount(rows []mountRow) (row mountRow) {
	f := &inst.files
	sel := 0
	for i := range rows {
		if fmt.Sprintf("%016x", rows[i].id.Value()) == f.mountKey {
			sel = i
			break
		}
	}
	row = rows[sel]
	for range c.HoverText(tipFilesMount).KeepIter() {
		for range c.ComboBox(inst.ids.PrepareStr("files-mount"),
			c.WidgetText().Text("Mount").Keep(),
			c.WidgetText().Text(row.label()).Keep()).
			KeepIter() {
			for i := range rows {
				clicked := c.Button(inst.ids.PrepareStr("fm-"+fmt.Sprintf("%016x", rows[i].id.Value())),
					c.Atoms().Text(rows[i].label()).Keep()).
					Frame(false).
					Selected(i == sel).
					SendResp().HasPrimaryClicked()
				if clicked {
					f.mountKey = fmt.Sprintf("%016x", rows[i].id.Value())
				}
			}
		}
	}
	return
}

// requestSnapshotLoad routes an activation through the shared replace guard
// (the two-click arming Open uses — a snapshot load replaces the buffer the
// same way) and starts the read on a lane. fsys is a Locked view, safe from
// the lane's goroutine.
func (inst *App) requestSnapshotLoad(fsys fs.FS, label, cacheKey, path string) {
	if !inst.confirmReplace("activate again to discard them") {
		return
	}
	f := &inst.files
	f.loadKey = cacheKey + ":" + path
	f.loadLabel = label + ":" + path + " @ latest"
	inst.status = "loading " + path + "…"
	f.load.demand(f.loadKey, func(ctx context.Context) (string, error) {
		h, err := ladingview.ReadHead(fsys, path, maxSnapshotDocBytes, 0)
		if err != nil {
			return "", err
		}
		if h.IsDir {
			return "", eb.Build().Str("path", path).Errorf("mdedit: a directory cannot be loaded")
		}
		if h.Truncated {
			return "", eb.Build().Str("path", path).Int64("size", h.Size).Int64("cap", maxSnapshotDocBytes).
				Errorf("mdedit: file exceeds the editor's size cap")
		}
		return string(h.Data), nil
	})
}

// drainSnapshotLoad lands a finished load, called from renderBody BEFORE the
// panes — the rebind's databinding override needs the editor's emit after it
// in the same frame. Free of c.* calls, so the flow is drivable from plain
// tests; the pane keeps frames coming while a load runs.
func (inst *App) drainSnapshotLoad() {
	f := &inst.files
	if f.loadKey == "" {
		return
	}
	text, done, err, busy := f.load.demand(f.loadKey, nil)
	if busy || !done {
		return
	}
	f.load.invalidate()
	f.loadKey = ""
	label := f.loadLabel
	f.loadLabel = ""
	if err != nil {
		inst.status = "load failed: " + err.Error()
		inst.logger.Warn().Err(err).Msg("mdedit: snapshot load failed")
		return
	}
	// The same trio an Open lands, plus ending any follow — the snapshot is
	// pinned, and the read handle being followed belongs to a different
	// document now.
	inst.stopFollow()
	inst.rebindBuffer(text)
	inst.saved = text
	inst.persistedSrc = ""
	inst.requestCaret(0, 0, false)
	inst.readName = label
	inst.readFromSnapshot = true
	inst.status = "loaded " + label
}
