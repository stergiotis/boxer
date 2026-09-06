package tally

import (
	"path"
	"reflect"
	"strconv"
	"strings"
	"time"

	"github.com/stergiotis/boxer/apps/tally/launchcfg"
	"github.com/stergiotis/boxer/public/identity/identifier"
	"github.com/stergiotis/boxer/public/keelson/runtime/buscodec"
	"github.com/stergiotis/boxer/public/observability/eh"
)

// composeLaunch is the window as a launch config (ADR-0200 §SD9, ADR-0222
// §SD2): the two panes' locations and selections, the sync flag, the target
// pane, the raised tab, and the query this window was opened with. A pane
// that follows latest records no snapshot, so a restore follows latest too.
func (inst *App) composeLaunch() (cfg launchcfg.TallyLaunch) {
	a, b := &inst.panes[paneIDA], &inst.panes[paneIDB]
	cfg = launchcfg.TallyLaunch{
		At:       time.Now().UTC(),
		MountA:   mountText(a.mount),
		SnapA:    snapText(a),
		DirA:     a.st.Dir(),
		SelA:     a.selected,
		MountB:   mountText(b.mount),
		SnapB:    snapText(b),
		DirB:     b.st.Dir(),
		SelB:     b.selected,
		Sync:     inst.syncBrowse,
		Target:   inst.target.String(),
		Tab:      tabSlug(inst.raisedTab),
		Sql:      inst.querySql,
		SqlLabel: inst.queryLabel,
	}
	return
}

// resolveTab maps a launch config's tab name to the dock tab id, 0 for a
// name this window has no tab for. The Results tab resolves only while the
// window carries a query, because that is the only time it exists.
func (inst *App) resolveTab(slug string) (id uint64) {
	if slug == launchcfg.TabResults {
		if inst.querySql == "" {
			return 0
		}
		return dockTabResults
	}
	return dockTabForSlug(slug)
}

func dockTabForSlug(slug string) (id uint64) {
	switch slug {
	case launchcfg.TabPaneA:
		return dockTabBrowseA
	case launchcfg.TabPaneB:
		return dockTabBrowseB
	case launchcfg.TabResults:
		return dockTabResults
	case launchcfg.TabPreview:
		return dockTabPreview
	case launchcfg.TabInfo:
		return dockTabInfo
	case launchcfg.TabHistory:
		return dockTabHistory
	case launchcfg.TabDiff:
		return dockTabDiff
	case launchcfg.TabFind:
		return dockTabFind
	case launchcfg.TabDu:
		return dockTabDu
	case launchcfg.TabProblems:
		return dockTabProblems
	}
	return 0
}

// tabSlug is dockTabForSlug backwards, for the workingset. It reports the
// empty string for a tab this window never raised itself — the dock's own
// focus is not readable from here (ADR-0148 §SD8's limit, as play records
// it).
func tabSlug(id uint64) (slug string) {
	for _, s := range launchcfg.TabIds() {
		if dockTabForSlug(s) == id {
			return s
		}
	}
	return ""
}

func mountText(id identifier.TaggedId) string {
	if id == 0 {
		return ""
	}
	return hexID(id)
}

func snapText(p *pane) string {
	if p.followLatest || p.snap.IsZero() {
		return ""
	}
	return p.snap.UTC().Format(time.RFC3339Nano)
}

// applyLaunch seeds the window from a launch config: mounts by hex id,
// snapshots pinned when given, directories, sync and target. A field that
// does not parse is left at its default rather than refusing the whole
// config — the window still opens, on what it can read.
func (inst *App) applyLaunch(cfg launchcfg.TallyLaunch) {
	apply := func(p *pane, mount, snap, dir, sel string) {
		if id, ok := parseMountText(mount); ok {
			p.mount = id
		}
		if snap != "" {
			if t, err := time.Parse(time.RFC3339Nano, snap); err == nil {
				p.snap, p.followLatest = t, false
			}
		}
		// A selected file implies the directory it is in, so a caller that
		// names a file need not also spell its parent; an explicit
		// directory is honoured, and a selection outside it simply will not
		// be found when the pane lists.
		if dir == "" && sel != "" {
			dir = path.Dir(sel)
		}
		if dir != "" {
			p.st.SetDir(dir)
		}
		p.pendingSel = sel
	}
	apply(&inst.panes[paneIDA], cfg.MountA, cfg.SnapA, cfg.DirA, cfg.SelA)
	apply(&inst.panes[paneIDB], cfg.MountB, cfg.SnapB, cfg.DirB, cfg.SelB)
	inst.syncBrowse = cfg.Sync
	if strings.EqualFold(cfg.Target, "B") {
		inst.target = paneIDB
	} else {
		inst.target = paneIDA
	}
	inst.focus = inst.target
	inst.querySql = strings.TrimSpace(cfg.Sql)
	inst.queryLabel = cfg.SqlLabel
	// Which tab opens: what the config asked for, or the Results tab when it
	// carried a query and said nothing — a caller that passes a query wants
	// to see what it returned.
	tab := cfg.Tab
	if tab == "" && inst.querySql != "" {
		tab = launchcfg.TabResults
	}
	if id := inst.resolveTab(tab); id != 0 {
		inst.pendingDockActivate, inst.raisedTab = id, id
		if id == dockTabResults {
			inst.focus = paneIDR
		}
	}
}

func parseMountText(s string) (id identifier.TaggedId, ok bool) {
	s = strings.TrimSpace(strings.TrimPrefix(strings.ToLower(s), "0x"))
	if s == "" {
		return
	}
	v, err := strconv.ParseUint(s, 16, 64)
	if err != nil {
		return
	}
	id = identifier.TaggedId(v)
	return id, id.IsValid()
}

// workingsetBaseline is what dirty tracking compares: the composed config
// with its timestamp zeroed, so only the reader's choices count.
func (inst *App) workingsetBaseline() (cfg launchcfg.TallyLaunch) {
	cfg = inst.composeLaunch()
	cfg.At = time.Time{}
	return
}

// syncWorkingsetDirty folds this frame's state into the intent flag: the
// first frame after Mount is the baseline, any later change is intent. The
// mount list arriving and filling an empty pane is not intent and is
// excluded by taking the baseline only once the mounts have been seen.
func (inst *App) syncWorkingsetDirty() {
	if !inst.workingsetSeenTaken {
		if !inst.mountsSeen {
			return
		}
		inst.workingsetSeen = inst.workingsetBaseline()
		inst.workingsetSeenTaken = true
		return
	}
	now := inst.workingsetBaseline()
	if !reflect.DeepEqual(inst.workingsetSeen, now) {
		inst.workingsetDirty = true
		inst.workingsetSeen = now
	}
}

// ComposeWorkingset is the host's closing-edge hook (ADR-0148 §SD4): the
// launch config that reproduces this window, written only when the reader
// did something in it.
func (inst *App) ComposeWorkingset() (cfg []byte, dirty bool, err error) {
	dirty = inst.workingsetDirty
	if !dirty {
		return
	}
	cfg, err = buscodec.Encode(inst.composeLaunch())
	if err != nil {
		err = eh.Errorf("tally: encode workingset: %w", err)
	}
	return
}
