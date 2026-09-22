package persiststore

// State is one persisted app-state value (ADR-0026 §SD3's runtime.persist):
// the whole payload as a single row, latest-wins through the state view,
// keyed StateKey(appId, key). The owning app and the writer's provenance
// ride on the row's Owner component.
//
// The memberships are the runtime vocabulary's (`runtime/vocab`), resolved
// into the store at generation time through storegen.MembershipIds — the
// same regime every facts-bound store uses. They are not declaration-order
// ids: under those, inserting a field here would renumber what every row on
// disk carries, silently. Adding a field means minting its membership in the
// vocabulary first (ADR-0183 D0's explicit ordinals), then regenerating.
type State struct {
	_     struct{} `kind:"persistState"`
	ID    string   `lw:",id"`
	Key   string   `lw:"runtimePersistKey,string"`
	Value []byte   `lw:"runtimePersistValue,blob"`
}
