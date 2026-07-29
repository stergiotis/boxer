package windowhost

import (
	"github.com/stergiotis/boxer/public/keelson/runtime/app"
	"github.com/stergiotis/boxer/public/keelson/runtime/codec/kindcheck"
	"github.com/stergiotis/boxer/public/keelson/runtime/factsstore"
)

// windowhost_workingset.go is the host half of ADR-0148: pulling a
// closing window's workingset and writing it as a fact beside the launch
// facts, and (M4) feeding a stored record back through the host's own
// OpenWithConfig at a later plain open.

// WorkingsetDefaultName is the single workingset name v1 wires (ADR-0148
// §SD3). The store, the row, and the host paths carry a name from day one
// so a named-set UX needs no migration; nothing mints another value yet.
const WorkingsetDefaultName = "default"

// saveWorkingset pulls the closing window's workingset and writes it as a
// boxer.facts row (ADR-0148 §SD4). Best-effort throughout: every failure
// path logs and returns, because persisting a record must never disturb a
// close.
//
// MUST be called before Unmount. Unmount tears the app instance down —
// play's launcher nils its inner app there — so a compose afterwards
// would read a corpse.
//
// The gate is the composer's dirty flag alone: user intent occurred in
// this window. A launch-seeded window closed untouched writes nothing, so
// an ephemeral seed cannot poison what the next plain open inherits.
// There is deliberately no byte comparison against the stored record — a
// composed config carrying its own timestamp is never byte-equal anyway.
func (inst *Inst) saveWorkingset(w *window, reason string) {
	if !w.manifest.Workingset {
		return
	}
	inst.mu.Lock()
	runId := inst.runId
	facts := inst.facts
	inst.mu.Unlock()
	if facts == nil {
		// No store attached: workingsets degrade with the audit trail
		// (§SD5), and composing would be work with nowhere to land.
		return
	}
	composer, ok := w.appInst.(app.WorkingsetComposerI)
	if !ok {
		// Registration cannot catch this — the manifest is known at
		// Register time, the instance only after the ctor runs at Open —
		// so the diagnosis lands here, once per app id.
		inst.warnMissingComposer(w.manifest.Id)
		return
	}
	cfg, dirty, err := composer.ComposeWorkingset()
	if err != nil {
		inst.logger.Warn().Err(err).
			Str("id", string(w.manifest.Id)).
			Uint64("windowKey", uint64(w.key)).
			Msg("windowhost: compose workingset failed; skipping the save")
		return
	}
	if !dirty || len(cfg) == 0 {
		return
	}
	// The same boundary rules an open enforces (§SD4): a record that
	// OpenWithConfig would refuse is a record no restore could ever
	// deliver, so it is not worth storing. LaunchKind is non-empty here —
	// Manifest.Validate refuses Workingset without one.
	if len(cfg) > maxLaunchConfigBytes {
		inst.logger.Warn().
			Str("id", string(w.manifest.Id)).
			Int("len", len(cfg)).Int("max", maxLaunchConfigBytes).
			Msg("windowhost: composed workingset exceeds the size cap; skipping the save")
		return
	}
	if cErr := kindcheck.Check(w.manifest.LaunchKind, cfg); cErr != nil {
		inst.logger.Warn().Err(cErr).
			Str("id", string(w.manifest.Id)).
			Str("kind", w.manifest.LaunchKind).
			Msg("windowhost: composed workingset refused by kindcheck; skipping the save")
		return
	}
	_, wErr := facts.WriteWorkingset(factsstore.WorkingsetRow{
		RunId:   runId,
		AppId:   w.manifest.Id,
		Name:    WorkingsetDefaultName,
		Kind:    w.manifest.LaunchKind,
		Config:  cfg,
		TileKey: uint64(w.key),
		Reason:  reason,
	})
	if wErr != nil {
		inst.logger.Warn().Err(wErr).
			Str("id", string(w.manifest.Id)).
			Uint64("windowKey", uint64(w.key)).
			Msg("windowhost: write workingset failed")
	}
}

// warnMissingComposer logs the manifest-says-yes / instance-says-no
// mismatch once per app id, so a misdeclared app is visible without
// filling the log at every close.
func (inst *Inst) warnMissingComposer(id app.AppIdT) {
	inst.mu.Lock()
	if inst.warnedNoComposer == nil {
		inst.warnedNoComposer = make(map[app.AppIdT]struct{}, 1)
	}
	_, seen := inst.warnedNoComposer[id]
	inst.warnedNoComposer[id] = struct{}{}
	inst.mu.Unlock()
	if seen {
		return
	}
	inst.logger.Warn().Str("id", string(id)).
		Msg("windowhost: manifest declares Workingset but the app does not implement WorkingsetComposerI; no record will ever be saved")
}

// warnWorkingsetSharedInstance explains a skipped save: another window
// still points at this AppI instance, which only happens for a
// singleton-registered app shown more than once. The surviving window
// owns that state — writing it from the closing one would be a
// last-writer-wins race on state neither window has finished with. The
// mirror image is OpenWithConfig's refusal to deliver a config to an
// instance that already has a window (ADR-0135).
func (inst *Inst) warnWorkingsetSharedInstance(w *window) {
	if !w.manifest.Workingset {
		return
	}
	inst.logger.Warn().
		Str("id", string(w.manifest.Id)).
		Uint64("windowKey", uint64(w.key)).
		Msg("windowhost: skipping workingset save; another window still holds this app instance (singleton registration)")
}
