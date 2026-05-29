package sub

import "stub"

// Nested-path package: import path is "nested/sub". Both exact-match
// and synthetic-suffix must work for multi-segment paths.
var _ = stub.Manifest{
	Id: "nested/sub",
}

var _ = stub.Manifest{
	Id: "nested/sub/widget",
}

// Bare top-level prefix is not enough: "nested" alone is *not* a valid
// substring-of-pkgPath match.
var _ = stub.Manifest{
	Id: "nested", // want `L12: AppIdT literal "nested" does not match package import path "nested/sub"`
}
