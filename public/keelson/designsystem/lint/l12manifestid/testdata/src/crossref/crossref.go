package crossref

import "stub"

// Cross-reference table: the carousel and other dispatch surfaces hold
// maps from arbitrary keys to other packages' AppIds. The literal values
// here belong to OTHER packages by design — the L12 rule must not fire
// on these (no expectation markers below).
var legacyCodeToId = map[uint64]stub.AppIdT{
	1: "github.com/stergiotis/boxer/public/thestack/imzero2/egui2/demo/apps/widgets",
	5: "github.com/stergiotis/pebble2impl/apps/play",
	7: "github.com/stergiotis/pebble2impl/apps/imztop",
}

// Slice of AppIdTs — same logic; this isn't "my Id" assertion.
var preferredOrder = []stub.AppIdT{
	"github.com/example/a",
	"github.com/example/b",
}

// Untyped-but-string-keyed map with explicit AppIdT key — same.
var fromAlias = map[stub.AppIdT]int{
	"github.com/example/upstream": 1,
}

// Sanity: a real Manifest in the same file is still checked.
var _ = stub.Manifest{
	Id: "github.com/example/wrong", // want `L12: AppIdT literal "github.com/example/wrong" does not match package import path "crossref"`
}

// Used so the package compiles.
var _ = legacyCodeToId
var _ = preferredOrder
var _ = fromAlias
