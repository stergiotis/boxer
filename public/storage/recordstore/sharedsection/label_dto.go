package sharedsection

// Label and State (state_dto.go) are the two asset components. Unlike
// recordstore/example — where each kind owns a distinct section because
// per-plan declaration-order ids would collide — both bind the SAME
// `symbol` section under distinct memberships; the caller-assigned ids
// (AssetMembershipIdAssignment) keep their attributes apart on read.
type Label struct {
	_    struct{} `kind:"label"`
	ID   uint64   `lw:",id"`
	Name string   `lw:"assetName,symbol"`
}
