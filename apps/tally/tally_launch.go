package tally

import (
	"reflect"
	"strconv"
	"strings"
	"time"

	"github.com/stergiotis/boxer/apps/tally/launchcfg"
	"github.com/stergiotis/boxer/public/identity/identifier"
	"github.com/stergiotis/boxer/public/keelson/runtime/buscodec"
	"github.com/stergiotis/boxer/public/observability/eh"
)

// composeLaunch is the window as a launch config (ADR-0200 §SD9): the two
// panes' locations, the sync flag and the target pane. A pane that follows
// latest records no snapshot, so a restore follows latest too.
func (inst *App) composeLaunch() (cfg launchcfg.TallyLaunch) {
	a, b := &inst.panes[paneIDA], &inst.panes[paneIDB]
	cfg = launchcfg.TallyLaunch{
		At:     time.Now().UTC(),
		MountA: mountText(a.mount),
		SnapA:  snapText(a),
		DirA:   a.st.Dir(),
		MountB: mountText(b.mount),
		SnapB:  snapText(b),
		DirB:   b.st.Dir(),
		Sync:   inst.syncBrowse,
		Target: inst.target.String(),
	}
	return
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
	apply := func(p *pane, mount, snap, dir string) {
		if id, ok := parseMountText(mount); ok {
			p.mount = id
		}
		if snap != "" {
			if t, err := time.Parse(time.RFC3339Nano, snap); err == nil {
				p.snap, p.followLatest = t, false
			}
		}
		if dir != "" {
			p.st.SetDir(dir)
		}
	}
	apply(&inst.panes[paneIDA], cfg.MountA, cfg.SnapA, cfg.DirA)
	apply(&inst.panes[paneIDB], cfg.MountB, cfg.SnapB, cfg.DirB)
	inst.syncBrowse = cfg.Sync
	if strings.EqualFold(cfg.Target, "B") {
		inst.target = paneIDB
	} else {
		inst.target = paneIDA
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
