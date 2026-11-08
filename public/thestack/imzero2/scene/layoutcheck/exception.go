package layoutcheck

import (
	"slices"
	"strings"

	"github.com/stergiotis/boxer/public/observability/eh/eb"
	"github.com/stergiotis/boxer/public/thestack/imzero2/carrierclient"
)

// Exception excuses findings a scene means to produce: one invariant, at the
// nodes an anchor selects. The anchor fields are a trace step's (`id`,
// `name`, `contains`, `value`, `valueContains`, `role`) and match the same
// way, except that several matches are not an error — an exception may cover
// every row of a list. An overlap is excused when either of its nodes
// matches.
type Exception struct {
	Invariant InvariantE `yaml:"invariant"`
	// Capture limits the exception to one capture, named as the trace's
	// `capture` step names it. Empty means every capture of the scene.
	Capture       string `yaml:"capture"`
	ID            uint64 `yaml:"id"`
	Name          string `yaml:"name"`
	Contains      string `yaml:"contains"`
	Value         string `yaml:"value"`
	ValueContains string `yaml:"valueContains"`
	Role          string `yaml:"role"`
	// Reason says why the layout is meant to be this way. Required: an
	// exception nobody can explain is a finding with the alarm turned off.
	Reason string `yaml:"reason"`
}

func (inst Exception) locator() carrierclient.Locator {
	return carrierclient.Locator{ID: inst.ID, Name: inst.Name, NameContains: inst.Contains,
		Value: inst.Value, ValueContains: inst.ValueContains, Role: inst.Role}
}

// Validate rejects an exception that names no known invariant, has no anchor
// or gives no reason.
func (inst Exception) Validate() (err error) {
	if !slices.Contains(AllInvariants, inst.Invariant) {
		names := make([]string, 0, len(AllInvariants))
		for _, v := range AllInvariants {
			names = append(names, string(v))
		}
		return eb.Build().Str("invariant", string(inst.Invariant)).Str("known", strings.Join(names, ", ")).
			Errorf("unknown layout invariant in a layout exception")
	}
	loc := inst.locator()
	if loc.ID == 0 && loc.Name == "" && loc.NameContains == "" && loc.Value == "" && loc.ValueContains == "" && loc.Role == "" {
		return eb.Build().Str("invariant", string(inst.Invariant)).
			Errorf("a layout exception needs an anchor: id, name, contains, value, valueContains or role")
	}
	if strings.TrimSpace(inst.Reason) == "" {
		return eb.Build().Str("invariant", string(inst.Invariant)).Errorf("a layout exception needs a reason")
	}
	return nil
}

// Excuses reports whether the exception covers finding f in the capture
// named capture.
func (inst Exception) Excuses(capture string, f Finding) bool {
	if inst.Invariant != f.Invariant || (inst.Capture != "" && inst.Capture != capture) {
		return false
	}
	loc := inst.locator()
	if loc.Matches(f.Node.treeNode()) {
		return true
	}
	return f.Other != nil && loc.Matches(f.Other.treeNode())
}

// Apply splits findings into those no exception covers and those one does.
func Apply(capture string, findings []Finding, exceptions []Exception) (kept []Finding, excused []Finding) {
	for _, f := range findings {
		if slices.ContainsFunc(exceptions, func(e Exception) bool { return e.Excuses(capture, f) }) {
			excused = append(excused, f)
		} else {
			kept = append(kept, f)
		}
	}
	return kept, excused
}
