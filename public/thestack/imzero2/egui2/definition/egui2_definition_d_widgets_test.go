package definition

import (
	"regexp"
	"strings"
	"testing"
)

var statefulPushPattern = regexp.MustCompile(`\b(r10_push|r9_[a-z0-9]+_push)\b`)

// gateSubstrings are the accepted spellings of an egui response-event gate
// guarding a state push. Both consume the `resp.is_some() && resp.unwrap().<event>()`
// predicate; they differ only in how the result is used:
//   - the `if` form is what applyCodeWidgetRustOnEvent emits (checkbox, radioButton);
//   - the `let mut changed =` form is textEdit's, which folds the event check
//     into a `changed` flag also raised by a programmatic insert-at-cursor, then
//     gates the single push on that flag. A plain if-gate would drop the
//     insert path (it never sets egui's .changed()). See ADR-0063.
var gateSubstrings = []string{
	"if resp.is_some() && resp.unwrap().",
	"let mut changed = resp.is_some() && resp.unwrap().",
}

// TestStatefulWidgetsAreGated catches the historical RadioButton drift —
// a state push (r10_push / r9_*_push) emitted unconditionally instead of
// gated on an egui response event. Stateful widgets must route through
// applyCodeWidgetRustOnEvent (or fold the same predicate into a `changed`
// flag, per ADR-0063). Custom Rust helpers (e.g. apply_date_picker_button)
// are out of scope; their gates live in the helper, not the spec.
func TestStatefulWidgetsAreGated(t *testing.T) {
	widgets := definitionsWidget()
	for _, w := range widgets {
		if w.ApplyCode.CodeClientRust == nil {
			continue
		}
		code := w.ApplyCode.CodeClientRust.GetVerbatimCode()
		if !statefulPushPattern.MatchString(code) {
			continue
		}
		gated := false
		for _, sub := range gateSubstrings {
			if strings.Contains(code, sub) {
				gated = true
				break
			}
		}
		if !gated {
			t.Errorf("widget %q emits a state push without an event gate; route through applyCodeWidgetRustOnEvent", w.Name)
		}
	}
}
