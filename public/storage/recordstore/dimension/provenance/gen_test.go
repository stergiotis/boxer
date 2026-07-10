package provenance

import (
	"testing"

	"github.com/stergiotis/boxer/public/storage/recordstore/gen"
	"github.com/stretchr/testify/require"
)

// TestGenerateProvenanceStore emits the provenance descriptor store — DML, the
// composed CREATE TABLE, RA classes, the Provenance codec and the store glue —
// through the recordstore generator (ADR-0100 SD6). Run it to (re)generate:
//
//	go test -tags "$(cat tags)" -run TestGenerateProvenanceStore ./public/storage/recordstore/dimension/provenance/
func TestGenerateProvenanceStore(t *testing.T) {
	manip, err := GetProvenanceSchemaInManipulator()
	require.NoError(t, err)
	td, err := manip.BuildTableDesc()
	require.NoError(t, err)
	require.NoError(t, gen.Input{
		PackageName:    "provenance",
		StoreName:      "Provenance",
		TableName:      "provenance",
		Table:          td,
		RowConfig:      TableRowConfig,
		ComponentPaths: []string{"./provenance_dto.go"},
		OutDir:         ".",
		ImportPath:     "github.com/stergiotis/boxer/public/storage/recordstore/dimension/provenance",
	}.Generate())
}
