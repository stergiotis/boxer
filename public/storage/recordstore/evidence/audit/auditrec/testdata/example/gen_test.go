package example

//go:generate sh -c "go test -tags=\"$(cat ../../../../../../../../tags)\" -run TestGenerateExampleStore ."

import (
	"testing"

	"github.com/stretchr/testify/require"

	"github.com/stergiotis/boxer/public/semistructured/leeway/marshall/go/marshallgen"
	"github.com/stergiotis/boxer/public/storage/recordstore/gen"
)

// TestGenerateExampleStore (re)generates the store in place: boxer's
// origin and audit event as its components, the seal component the
// profile emits, and the canonical wire the seals' row digests are over.
func TestGenerateExampleStore(t *testing.T) {
	manip, err := SchemaInManipulator()
	require.NoError(t, err)
	td, err := manip.BuildTableDesc()
	require.NoError(t, err)
	require.NoError(t, gen.Input{
		PackageName: "example",
		StoreName:   "Example",
		TableName:   TableName,
		Database:    Database,
		Table:       td,
		RowConfig:   TableRowConfig,
		ComponentPaths: []string{
			"../../../origin_dto.go",
			"../../../auditevent_dto.go",
		},
		OutDir:     ".",
		ImportPath: "github.com/stergiotis/boxer/public/storage/recordstore/evidence/audit/auditrec/testdata/example",
		Roles:      gen.Roles{Key: "id", Order: "ts"},
		Wrapper:    marshallgen.FixedIdsWrapper{Ids: genIds},
		CanonWire:  true,
		Evidence:   &gen.Evidence{Seals: true},
	}.Generate())
}
