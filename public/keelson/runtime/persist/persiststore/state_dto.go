package persiststore

// State is one persisted app-state value: the whole payload as a single
// row, latest-wins through the state view.
//
// ID is the composite "<appId>/<key>". AppId and Key repeat it in split
// form — the composite is what the store keys on, the split is what SQL
// filters on without parsing strings.
//
// The memberships are the runtime vocabulary's (`runtime/vocab`), resolved
// into the store at generation time through storegen.MembershipIds — the
// same regime every facts-bound store uses. They are not declaration-order
// ids: under those, inserting a field here would renumber what every row
// on disk carries, silently. Adding a field means minting its membership
// in the vocabulary first (ADR-0183 D0's explicit ordinals), then
// regenerating.
// RunId and InstanceKey are provenance, not identity (ADR-0191 §SD5): which
// process and which window wrote this value. The key stays "<appId>/<key>",
// so a second window writing the same key still overwrites rather than
// forking — the same relationship WorkingsetRow.TileKey has to its own
// identity. They exist because the row trail is the history of a key, and
// "who wrote this" was the one question it could not answer; before them,
// attributing a persist write to a run meant intersecting timestamps with a
// run's span, which two overlapping processes make wrong rather than absent.
//
// Both reuse memberships the runtime vocabulary already has, so nothing is
// minted: `runtimeRun`, and `runtimeLifecycleTileKey` for the window (the
// name is the lifecycle cohort's for the reason §SD1 gives — one column to
// join on).
type State struct {
	_           struct{} `kind:"persistState"`
	ID          string   `lw:",id"`
	AppId       string   `lw:"runtimeApp,stateAppId"`
	Key         string   `lw:"runtimePersistKey,stateKey"`
	Value       []byte   `lw:"runtimePersistValue,stateBlob"`
	RunId       string   `lw:"runtimeRun,stateRunId"`
	InstanceKey uint64   `lw:"runtimeLifecycleTileKey,stateInstanceKey"`
}
