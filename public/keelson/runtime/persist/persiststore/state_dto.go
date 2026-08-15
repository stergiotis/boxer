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
type State struct {
	_     struct{} `kind:"persistState"`
	ID    string   `lw:",id"`
	AppId string   `lw:"runtimeApp,stateAppId"`
	Key   string   `lw:"runtimePersistKey,stateKey"`
	Value []byte   `lw:"runtimePersistValue,stateBlob"`
}
