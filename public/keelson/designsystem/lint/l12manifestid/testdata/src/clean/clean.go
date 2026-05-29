package clean

import "stub"

// Exact-match: literal equals enclosing package's import path.
var _ = stub.Manifest{
	Id:      "clean",
	Display: "Clean",
}

// Typed const, exact match.
const _ stub.AppIdT = "clean"
