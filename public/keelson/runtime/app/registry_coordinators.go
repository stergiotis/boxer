package app

import "slices"

// SetCoordinators records the app ids or subject aliases the host resolved
// as coordinators at boot (ADR-0364, proposed). The dispatcher holds the
// same list and decides by it; the registry's copy is what keelson.apps and
// an app's own default read.
func (inst *Registry) SetCoordinators(names []string) {
	inst.mu.Lock()
	defer inst.mu.Unlock()
	inst.coordinators = slices.Clone(names)
}

// Coordinator reports whether id was resolved as a coordinator.
func (inst *Registry) Coordinator(id AppIdT) (ok bool) {
	inst.mu.RLock()
	defer inst.mu.RUnlock()
	return slices.ContainsFunc(inst.coordinators, func(n string) bool {
		return n == string(id) || n == id.SubjectAlias()
	})
}
