package sharedsection

// State is the second resident of the shared `symbol` section — see
// label_dto.go.
type State struct {
	_     struct{} `kind:"state"`
	ID    uint64   `lw:",id"`
	Phase string   `lw:"assetPhase,symbol"`
}
