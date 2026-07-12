package lazypane

import (
	c "github.com/stergiotis/boxer/public/thestack/imzero2/egui2/bindings"
)

// phaseE is the pane's visibility phase, advanced once per frame by step.
type phaseE uint8

const (
	// phaseHidden: the region was not rendered last frame (or never yet).
	phaseHidden phaseE = iota
	// phaseWarming: the region renders again but the placeholder is held
	// for the remaining HoldFrames.
	phaseWarming
	// phaseLive: the heavy body is being emitted.
	phaseLive
)

// Pane gates one host-skippable region. One instance per region, persistent
// across frames (it carries the phase state machine); construct via New and
// call Skip exactly once per frame from inside the region.
type Pane struct {
	// Title names the region in the default placeholder ("loading Title…").
	// May be updated between frames (bound dock tabs rename themselves).
	Title string

	// HoldFrames keeps the placeholder up for this many extra frames after
	// the region first reports rendered. Purely cosmetic anti-flash; it
	// delays the body and any warm-up the body would trigger. Default 0.
	HoldFrames int

	// Placeholder, when non-nil, replaces the built-in spinner row. It runs
	// on every skipped frame; while the region is hidden its output is
	// discarded with the rest of the buffer, so keep it small. Widgets that
	// need ids must manage their own (the pane has no id-stack access).
	Placeholder func(title string)

	seq          uint64
	phase        phaseE
	holdLeft     int
	justRevealed bool
}

// New constructs a Pane. key seeds the captureUiRect probe seq (hashed via
// the widget-id hash, the same derivation the inspector anchors use) — it
// must be stable across frames and unique among all r21 probe users in the
// process, so namespace it ("play-dock-tab-<id>", not "tab").
func New(key string, title string) (inst *Pane) {
	inst = &Pane{
		Title: title,
		seq:   uint64(c.MakeAbsoluteIdStr(key)),
	}
	return
}

// Skip emits the visibility probe and reports whether the caller should
// skip the region's heavy body this frame. On a skipped frame it also emits
// the placeholder, so the region is never empty: the sequence a user sees on
// tab activation is one placeholder frame (plus HoldFrames), then content.
//
// Call exactly once per frame as the first thing inside the region. The
// probe must be emitted on body frames too — it is what keeps the pane
// reporting rendered — which Skip does before deciding anything.
func (inst *Pane) Skip() (skip bool) {
	c.CaptureUiRect(inst.seq)
	// Presence is the signal; the rect values are unused (the probe sits in
	// an empty Ui, where min_rect is degenerate). One-frame lag, and absent
	// on the very first frame — a cold start shows one placeholder frame.
	_, rendered := c.CurrentApplicationState.StateManager.GetUiRect(inst.seq)
	skip, inst.justRevealed = inst.step(rendered)
	if skip {
		inst.placeholder()
	}
	return
}

// JustRevealed reports whether this frame is the first body frame after a
// hidden period — the hook for eagerly re-arming send-once protocols under
// the region instead of waiting one round-trip on the starved-texture
// report. Valid after this frame's Skip; false on steady body frames.
func (inst *Pane) JustRevealed() bool {
	return inst.justRevealed
}

// step advances the phase machine on this frame's rendered signal. Pure
// (no FFFI emission) — the unit-testable core of the pane.
func (inst *Pane) step(rendered bool) (skip bool, revealed bool) {
	switch inst.phase {
	case phaseLive:
		if rendered {
			return false, false
		}
		inst.phase = phaseHidden
		return true, false
	case phaseWarming:
		if !rendered {
			inst.phase = phaseHidden
			return true, false
		}
		inst.holdLeft--
		if inst.holdLeft > 0 {
			return true, false
		}
		inst.phase = phaseLive
		return false, true
	default: // phaseHidden
		if !rendered {
			return true, false
		}
		if inst.HoldFrames > 0 {
			inst.phase = phaseWarming
			inst.holdLeft = inst.HoldFrames
			return true, false
		}
		inst.phase = phaseLive
		return false, true
	}
}

func (inst *Pane) placeholder() {
	if inst.Placeholder != nil {
		inst.Placeholder(inst.Title)
		return
	}
	label := "loading…"
	if inst.Title != "" {
		label = "loading " + inst.Title + "…"
	}
	for range c.Horizontal().KeepIter() {
		c.Spinner().Send()
		c.LabelAtoms(c.Atoms().BeginRichText(label).Weak().End().Keep()).Send()
	}
}
