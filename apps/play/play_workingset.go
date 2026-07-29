package play

import (
	"time"

	"github.com/stergiotis/boxer/apps/play/launchcfg"
	"github.com/stergiotis/boxer/public/keelson/runtime/app"
	"github.com/stergiotis/boxer/public/keelson/runtime/buscodec"
	"github.com/stergiotis/boxer/public/observability/eh"
)

// play_workingset.go is play's side of ADR-0148 (§SD8, the reference
// adoption): composing the window's user-authored state as a PlayLaunch,
// and tracking whether a person actually did anything in this window.
//
// The exports here are also the embedder seam. An app that clones play's
// manifest under its own id gets the contract by delegating to
// ComposeLaunch / WorkingsetDirty instead of copying the field list —
// which is how the seam stays one definition when PlayLaunch grows.

// workingsetSnapshot is the set of fields a workingset carries, sampled
// once per frame. Compared field-by-field rather than hashed so a future
// field addition is a compile-time obligation here.
type workingsetSnapshot struct {
	taken    bool
	sql      string
	bandsSql string
	live     bool
	tab      uint64
}

// syncWorkingsetDirty folds this frame's state into the intent flag. Runs
// once per frame from Render, before anything reads the flag.
//
// The first call only takes the baseline: Mount has finished seeding by
// then (launch config, env overrides, the legacy read bridge), so the
// state a window opens with is never itself an edit. Every later
// divergence is a person acting — typing in either editor, toggling Live,
// raising a pane — since nothing else writes these fields.
func (inst *PlayApp) syncWorkingsetDirty() {
	now := workingsetSnapshot{
		taken:    true,
		sql:      inst.sql,
		bandsSql: inst.timelineBandsSql,
		live:     inst.liveMain,
		tab:      inst.raisedTab,
	}
	if !inst.workingsetSeen.taken {
		inst.workingsetSeen = now
		return
	}
	if inst.workingsetSeen != now {
		inst.workingsetDirty = true
		inst.workingsetSeen = now
	}
}

// noteWorkingsetIntent marks this window as one a person acted in. Called
// from the paths that are intent by construction rather than by a changed
// field — a manual Run, whose whole meaning is "this is the query I want",
// even when it re-runs an unchanged buffer.
func (inst *PlayApp) noteWorkingsetIntent() {
	inst.workingsetDirty = true
}

// rebaseWorkingsetLive re-anchors the Live bit without marking intent, for
// the one writer of liveMain that is not a person: the circuit breaker
// unchecking it after a self-feeding query streak. The user re-checking
// the box afterwards diverges from this new baseline and reads as intent,
// which is right — that is the resume gesture.
func (inst *PlayApp) rebaseWorkingsetLive(on bool) {
	if !inst.workingsetSeen.taken {
		return
	}
	inst.workingsetSeen.live = on
}

// WorkingsetDirty reports whether user intent occurred in this window
// since Mount (ADR-0148 §SD4): an edit to either SQL buffer, a manual
// Run, a Live toggle, or a pane raised through the delivery seam. It is
// the whole save gate — a window nobody acted in leaves the stored record
// alone.
func (inst *PlayApp) WorkingsetDirty() (dirty bool) {
	dirty = inst.workingsetDirty
	return
}

// ComposeLaunch renders the window's current user-authored state as the
// launch that would reproduce it (ADR-0148 §SD2/§SD8): the editor buffer,
// the Timeline bands buffer, the Live toggle, and the active pane.
//
// AutoRun composes false unconditionally — re-executing a buffer is
// something a user asks for, not something a restoration does — and At is
// stamped now, which is also why nothing in the save path byte-compares
// two composed configs.
//
// Endpoint is deliberately left empty: which target a buffer ran against
// is a property of the run that authored it, not of the state, and a
// restored window resolves keelson reads through the auto-routing
// resolver (ADR-0141) anyway.
//
// Caches, result history, selections and pane-local view state are out of
// scope by SD1 — a workingset carries what the user authored or chose.
func (inst *PlayApp) ComposeLaunch() (cfg launchcfg.PlayLaunch) {
	tab, _ := inst.tabs.slugForDockID(inst.raisedTab)
	cfg = launchcfg.PlayLaunch{
		At:       time.Now().UTC(),
		Sql:      inst.sql,
		BandsSql: inst.timelineBandsSql,
		Live:     inst.liveMain,
		Tab:      tab,
		AutoRun:  false,
	}
	return
}

// ComposeWorkingset implements app.WorkingsetComposerI for the launcher
// (ADR-0148 §SD4). The host calls it at close, before Unmount — which is
// load-bearing: Unmount releases the inner app, so the nil guard below is
// the honest answer for anything that asks afterwards, not a fallback.
func (inst *PlayLauncher) ComposeWorkingset() (cfg []byte, dirty bool, err error) {
	if inst.inner == nil {
		return
	}
	dirty = inst.inner.WorkingsetDirty()
	if !dirty {
		// Nothing to encode: the host writes nothing when the window saw
		// no user intent.
		return
	}
	cfg, err = buscodec.Encode(inst.inner.ComposeLaunch())
	if err != nil {
		err = eh.Errorf("play: encode workingset: %w", err)
		return
	}
	return
}

var _ app.WorkingsetComposerI = (*PlayLauncher)(nil)
