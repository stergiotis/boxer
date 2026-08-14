package persiststore

// State is one persisted app-state value: the whole payload as a single
// row, latest-wins through the state view.
//
// ID is the composite "<appId>/<key>". AppId and Key repeat it in split
// form — the composite is what the store keys on, the split is what SQL
// filters on without parsing strings.
type State struct {
	_     struct{} `kind:"persistState"`
	ID    string   `lw:",id"`
	AppId string   `lw:"persistAppId,stateAppId"`
	Key   string   `lw:"persistKey,stateKey"`
	Value []byte   `lw:"persistValue,stateBlob"`
}
