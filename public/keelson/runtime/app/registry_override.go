package app

import (
	"slices"

	"github.com/stergiotis/boxer/public/observability/eh/eb"
)

// ManifestOverride replaces the descriptive fields of a registered manifest
// for this process (ADR-0376). A nil field keeps the value in force. The
// fields are the ones a deployment may restate without changing what the
// app is or may do: its capabilities, surface, kind and origin stay
// declared.
type ManifestOverride struct {
	Display  *string
	Summary  *string
	Icon     *string
	Topics   *[]TopicT
	Keywords *[]string
	Maturity *MaturityE
	// SurfaceHints restates the initial window size; an app whose content
	// decides its shape at run time (a splash screen sized to its artwork)
	// sets it for itself. Not a keelson.apps column a SQL override writes.
	SurfaceHints *SurfaceHints
}

// Overridden column names, as keelson.apps names them.
const (
	OverriddenDisplay  = "display"
	OverriddenSummary  = "summary"
	OverriddenIcon     = "icon"
	OverriddenTopics   = "topics"
	OverriddenKeywords = "keywords"
	OverriddenMaturity = "maturity"
	// OverriddenSurfaceHints names a restated initial window size.
	OverriddenSurfaceHints = "surface_hints"
)

// apply returns m with the override's fields set, and the column names of
// the fields that differ from m.
func (inst ManifestOverride) apply(m Manifest) (out Manifest, changed []string) {
	out = m
	if inst.Display != nil && *inst.Display != m.Display {
		out.Display = *inst.Display
		changed = append(changed, OverriddenDisplay)
	}
	if inst.Summary != nil && *inst.Summary != m.Summary {
		out.Summary = *inst.Summary
		changed = append(changed, OverriddenSummary)
	}
	if inst.Icon != nil && *inst.Icon != m.Icon {
		out.Icon = *inst.Icon
		changed = append(changed, OverriddenIcon)
	}
	if inst.Topics != nil && !slices.Equal(*inst.Topics, m.Topics) {
		out.Topics = slices.Clone(*inst.Topics)
		changed = append(changed, OverriddenTopics)
	}
	if inst.Keywords != nil && !slices.Equal(*inst.Keywords, m.Keywords) {
		out.Keywords = slices.Clone(*inst.Keywords)
		changed = append(changed, OverriddenKeywords)
	}
	if inst.Maturity != nil && *inst.Maturity != m.Maturity {
		out.Maturity = *inst.Maturity
		changed = append(changed, OverriddenMaturity)
	}
	if inst.SurfaceHints != nil && *inst.SurfaceHints != m.SurfaceHints {
		out.SurfaceHints = *inst.SurfaceHints
		changed = append(changed, OverriddenSurfaceHints)
	}
	return
}

// OverrideManifests applies overrides to the registered manifests
// (ADR-0376). Every overridden manifest is validated as a registration
// would be, and every id must be registered; on any failure nothing is
// changed. The manifests handed out afterwards — Manifests,
// LaunchableManifests, LookupManifest, Registrations — carry the
// overridden values; slices handed out before keep theirs. An app's own
// AppI.Manifest is not reached. Overriding the same field twice keeps the
// later value, and the field stays listed as overridden even when the later
// value restores the declared one.
func (inst *Registry) OverrideManifests(overrides map[AppIdT]ManifestOverride) (err error) {
	if len(overrides) == 0 {
		return
	}
	inst.mu.Lock()
	defer inst.mu.Unlock()
	type pending struct {
		idx     int
		m       Manifest
		changed []string
	}
	staged := make([]pending, 0, len(overrides))
	for id, o := range overrides {
		idx, ok := inst.byId[id]
		if !ok {
			err = eb.Build().Str("id", string(id)).Errorf("registry: override names an app that is not registered")
			return
		}
		m, changed := o.apply(inst.entries[idx].manifest)
		if len(changed) == 0 {
			continue
		}
		if vErr := m.Validate(); vErr != nil {
			err = eb.Build().Str("id", string(id)).Strs("columns", changed).Errorf("registry: overridden manifest is invalid: %w", vErr)
			return
		}
		staged = append(staged, pending{idx: idx, m: m, changed: changed})
	}
	if len(staged) == 0 {
		return
	}
	for _, p := range staged {
		e := &inst.entries[p.idx]
		e.manifest = p.m
		for _, c := range p.changed {
			if !slices.Contains(e.overridden, c) {
				e.overridden = append(e.overridden, c)
			}
		}
		slices.Sort(e.overridden)
	}
	manifests := make([]Manifest, len(inst.entries))
	for i, e := range inst.entries {
		manifests[i] = e.manifest
	}
	inst.manifests = manifests
	inst.rebuildLaunchable()
	return
}

// Overridden returns the keelson.apps column names whose values for id an
// override replaced (ADR-0376), sorted; nil when none was, or id is not
// registered.
func (inst *Registry) Overridden(id AppIdT) (columns []string) {
	inst.mu.RLock()
	defer inst.mu.RUnlock()
	if idx, ok := inst.byId[id]; ok {
		columns = slices.Clone(inst.entries[idx].overridden)
	}
	return
}
