package ignored

import "stub"

// Preceding-line ignore. Per ignoreann, a `// designlint:ignore=L<N>`
// comment on line K suppresses diagnostics whose Pos() lands on line K
// or K+1 — so the comment must sit directly above the `Id:` keyvalue,
// not above the enclosing `var _ = stub.Manifest{` opener.
var _ = stub.Manifest{
	// designlint:ignore=L12 (intentional alias; broker exposes legacy id)
	Id: "github.com/example/legacy-alias",
}

// Trailing ignore on the literal's own line.
const ManifestId stub.AppIdT = "github.com/example/another-legacy" // designlint:ignore=L12 (vendor compatibility)

// Multi-id ignore parses commas correctly.
var _ = stub.Manifest{
	// designlint:ignore=L5,L12 (composite suppression)
	Id: "github.com/example/multi-rule-suppressed",
}
