package markdown

import (
	"fmt"
	"strings"

	"github.com/stergiotis/boxer/public/containers"
	c "github.com/stergiotis/boxer/public/thestack/imzero2/egui2/bindings"
)

// RenderFrontmatter emits the parsed YAML frontmatter as a labeled
// list in the current Ui scope: a Separator, a "Frontmatter (parsed):"
// header label, then one strong-key + value-text label per top-level
// entry in [containers.BinarySearchGrowingKV.IteratePairs] order
// (sorted, deterministic across frames).
//
// Nested values are stringified with [stringifyFrontmatterValue] —
// slices as "[a, b, c]", nested KVs as "{k: v, ...}", scalars via
// fmt.Sprintf("%v", …). No-op when the doc has no frontmatter (whether
// because [obsidian.FeatureFrontmatter] was disabled or the source had
// none) so it is safe to call unconditionally after [Doc.Render].
//
// Callers who want a richer layout (table, side panel, pill chips,
// per-key custom widgets) should iterate
// [Doc.Frontmatter].IteratePairs directly instead of using this
// helper.
func (inst *Doc) RenderFrontmatter() {
	fm := inst.frontmatter
	if fm == nil || fm.IsEmpty() {
		return
	}
	c.Separator().Send()
	c.Label("Frontmatter (parsed):").Send()
	for k, v := range fm.IteratePairs() {
		atoms := c.Atoms()
		for rt := range atoms.StyledText(k + ": ") {
			rt.Strong()
		}
		atoms = atoms.Text(stringifyFrontmatterValue(v))
		c.LabelAtoms(atoms.Keep()).Send()
	}
}

// stringifyFrontmatterValue is the default value formatter used by
// [Doc.RenderFrontmatter]. Recognised shapes:
//
//   - string                                        — passed through.
//   - []any                                         — "[v1, v2, ...]" with each element recursed.
//   - *BinarySearchGrowingKV[string, any] (nested)  — "{k: v, ...}".
//   - nil                                           — "(nil)".
//   - any other (numeric, bool, time.Time, …)        — fmt.Sprintf("%v", …).
//
// Decoded YAML produced by goldmark-meta + the recursive converter in
// [containers.NewBinarySearchGrowingKVFromAnyMap] is fully covered by
// these cases.
func stringifyFrontmatterValue(v interface{}) (s string) {
	switch t := v.(type) {
	case string:
		s = t
	case []interface{}:
		parts := make([]string, len(t))
		for i, item := range t {
			parts[i] = stringifyFrontmatterValue(item)
		}
		s = "[" + strings.Join(parts, ", ") + "]"
	case *containers.BinarySearchGrowingKV[string, interface{}]:
		var parts []string
		for k, val := range t.IteratePairs() {
			parts = append(parts, k+": "+stringifyFrontmatterValue(val))
		}
		s = "{" + strings.Join(parts, ", ") + "}"
	case nil:
		s = "(nil)"
	default:
		s = fmt.Sprintf("%v", v)
	}
	return
}
