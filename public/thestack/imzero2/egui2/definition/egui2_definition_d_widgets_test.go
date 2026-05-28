//go:build llm_generated_opus47

package definition

import (
	"regexp"
	"strings"
	"testing"
)

var statefulPushPattern = regexp.MustCompile(`\b(r10_push|r9_[a-z0-9]+_push)\b`)

const gateSubstring = "if resp.is_some() && resp.unwrap()."

// TestStatefulWidgetsAreGated catches the historical RadioButton drift —
// a state push (r10_push / r9_*_push) emitted unconditionally instead of
// gated on an egui response event. Stateful widgets must route through
// applyCodeWidgetRustOnEvent. Custom Rust helpers (e.g. apply_date_picker_button)
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
		if !strings.Contains(code, gateSubstring) {
			t.Errorf("widget %q emits a state push without an event gate; route through applyCodeWidgetRustOnEvent", w.Name)
		}
	}
}
