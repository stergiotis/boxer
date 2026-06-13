package leewaywidgets

import _ "embed"

// FixtureSource is the literal Go source of fixture_schema.go embedded at
// build time. The leewaywidgets demo renders this through codeview.PrepareGo
// so readers can see the declarative TableDesc that backs the fixture.
//
//go:embed fixture_schema.go
var FixtureSource string

// FixtureBuilderSource is the literal Go source of fixture.go embedded at
// build time — RunFixture, BuildFixtureBatches, and the once-cached
// driver/IR/batch state machine. The demo's Source/fixture.go leaf renders
// this side-by-side with FixtureSource so readers can read the schema and
// the data-loader as the two halves of one fixture.
//
//go:embed fixture.go
var FixtureBuilderSource string
