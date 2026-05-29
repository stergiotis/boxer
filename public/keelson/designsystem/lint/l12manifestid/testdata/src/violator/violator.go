package violator

import "stub"

// Plain mismatched literal in Manifest{Id: ...}.
var _ = stub.Manifest{
	Id:      "github.com/example/wrong", // want `L12: AppIdT literal "github.com/example/wrong" does not match package import path "violator"`
	Display: "Wrong",
}

// AppIdT conversion still resolves to a string constant — must be flagged.
var _ = stub.Manifest{
	Id: stub.AppIdT("totally-not-this-package"), // want `L12: AppIdT literal "totally-not-this-package" does not match package import path "violator"`
}

// Typed const ValueSpec — caught via the second AST walk arm.
const ManifestId stub.AppIdT = "github.com/example/some-other-place" // want `L12: AppIdT literal "github.com/example/some-other-place" does not match package import path "violator"`

// Subprefix-but-wrong-separator: "violatorx" must not slip through the
// strings.HasPrefix check; we require the trailing "/".
var _ = stub.Manifest{
	Id: "violatorx", // want `L12: AppIdT literal "violatorx" does not match package import path "violator"`
}
