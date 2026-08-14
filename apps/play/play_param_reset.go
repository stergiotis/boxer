package play

import (
	"strings"

	c "github.com/stergiotis/boxer/public/thestack/imzero2/egui2/bindings"
)

// Reset: put the knobs back where the buffer had them (ADR-0124 Update
// 2026-08-14).
//
// A published query is a set of knobs at their author's defaults, and a reader
// who has moved four of them has no way back short of remembering what they
// were. The gesture is small and the semantics are the whole of it: **the
// defaults are the values the buffer was LOADED with**, captured when a buffer
// is installed and never recomputed. They cannot be read from the prelude on
// demand, because a widget's drift rewrites that prelude — "restore the
// defaults" would then mean "restore what you last did", which is nothing.
//
// Only prelude-bound names have a default. A live name's value comes from a
// panel (ADR-0124's 2026-07-22 §SD4 amendment), and taking it back to a value
// the buffer never stated would be the pane overruling the panel that owns it.

// captureParamDefaults records the prelude values of a newly installed buffer.
//
// A buffer that does not parse leaves no defaults rather than stale ones: the
// Reset control is then simply absent, which is honest — nothing here knows
// what the knobs were supposed to be.
func (inst *PlayApp) captureParamDefaults(sql string) {
	inst.paramDefaults = nil
	if strings.TrimSpace(sql) == "" {
		return
	}
	_, params, err := ExtractParams(sql)
	if err != nil || len(params) == 0 {
		return
	}
	defaults := make(map[string]string, len(params))
	for key, value := range params {
		name, isParam := strings.CutPrefix(key, "param_")
		if !isParam || name == "" {
			continue
		}
		defaults[name] = value
	}
	if len(defaults) == 0 {
		return
	}
	inst.paramDefaults = defaults
}

// paramsMovedFromDefaults reports whether any knob is off the value the buffer
// was loaded with — which is exactly when a Reset has something to do.
func (inst *PlayApp) paramsMovedFromDefaults() (moved bool) {
	if len(inst.paramDefaults) == 0 {
		return false
	}
	for _, s := range inst.paramSlots {
		want, has := inst.paramDefaults[s.Name]
		if !has {
			continue
		}
		ptr, drafted := inst.paramDrafts[s.Name]
		if !drafted {
			continue
		}
		if *ptr != want {
			return true
		}
	}
	return false
}

// resetParamsToDefaults puts every knob back and asks for a run.
//
// The drafts are written directly and the databinding is overridden for each,
// the way syncLiveParamDrafts does when a value changes behind an interactive
// widget's back: without it the frontend keeps the buffer it cached for the
// TextEdit's id, and the field would go on showing what the reader typed while
// the query ran with something else.
//
// The prelude rewrite is left to the ordinary drift path at the end of this
// same frame, so a reset is not a second way to author a prelude — it moves
// drafts, and everything downstream treats it as any other move.
func (inst *PlayApp) resetParamsToDefaults() {
	if len(inst.paramDefaults) == 0 {
		return
	}
	changed := false
	for _, s := range inst.paramSlots {
		want, has := inst.paramDefaults[s.Name]
		if !has {
			continue
		}
		ptr, drafted := inst.paramDrafts[s.Name]
		if !drafted || *ptr == want {
			continue
		}
		*ptr = want
		c.CurrentApplicationState.StateManager.OverrideDatabindingSPtr(ptr)
		changed = true
	}
	if !changed {
		return
	}
	// Re-run: the knobs are what the query reads, so leaving the result showing
	// the old ones would make the button look like it had not worked.
	inst.RequestRun()
}

// renderParamResetControl draws the gesture beside the PARAMETERS caption, and
// only when it would do something.
//
// Absent rather than disabled: a permanently greyed control in a strip that is
// mostly empty of chrome reads as a broken feature, and its appearing is itself
// the signal that something has been moved.
func (inst *PlayApp) renderParamResetControl() {
	if !inst.paramsMovedFromDefaults() {
		return
	}
	if c.Button(inst.ids.PrepareStr("paramReset"), c.Atoms().Text("Reset").Keep()).
		Small().
		SendResp().HasPrimaryClicked() {
		inst.resetParamsToDefaults()
	}
}
