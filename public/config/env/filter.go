package env

import "strings"

// SpecFilter selects Specs by the fields `env list` filters on. A zero
// field matches every Spec.
type SpecFilter struct {
	Category CategoryE
	// Origin is compared with Spec.Origin.Module.
	Origin string
	// Prefix is compared with the start of Spec.Name.
	Prefix string
}

// Match reports whether s passes every non-zero field of the filter.
func (inst SpecFilter) Match(s Spec) (ok bool) {
	if inst.Category != "" && s.Category != inst.Category {
		return false
	}
	if inst.Origin != "" && s.Origin.Module != inst.Origin {
		return false
	}
	if inst.Prefix != "" && !strings.HasPrefix(s.Name, inst.Prefix) {
		return false
	}
	return true
}

// Apply returns the Specs of specs that Match, in their given order.
func (inst SpecFilter) Apply(specs []Spec) (out []Spec) {
	out = make([]Spec, 0, len(specs))
	for _, s := range specs {
		if inst.Match(s) {
			out = append(out, s)
		}
	}
	return
}
