package subpath

import "stub"

// Synthetic-subpath case: widgets carousel dual-registers demos under
// "<pkg>/<demo>" suffixes. Must pass the prefix check.
var _ = stub.Manifest{
	Id:      "subpath/table",
	Display: "Sub-demo: Table",
}

var _ = stub.Manifest{
	Id:      "subpath/chart",
	Display: "Sub-demo: Chart",
}

// Deep suffix still passes.
const _ stub.AppIdT = "subpath/group/inner"
