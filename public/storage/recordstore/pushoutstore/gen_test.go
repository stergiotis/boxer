package pushoutstore

import (
	"testing"

	"github.com/stergiotis/boxer/public/storage/recordstore/gen"
	"github.com/stretchr/testify/require"
)

// TestGeneratePushoutStore emits the pushout store package through the
// recordstore generator (ADR-0100 SD6). Run it to (re)generate:
//
//	go test -tags "$(cat tags)" -run TestGeneratePushoutStore ./public/storage/recordstore/pushoutstore/
func TestGeneratePushoutStore(t *testing.T) {
	manip, err := GetPushoutSchemaInManipulator()
	require.NoError(t, err)
	td, err := manip.BuildTableDesc()
	require.NoError(t, err)
	require.NoError(t, gen.Input{
		PackageName: "pushoutstore",
		StoreName:   "Pushout",
		TableName:   "pushout",
		Table:       td,
		RowConfig:   TableRowConfig,
		ComponentPaths: []string{
			"./envelope_dto.go",
			"./logentry_dto.go",
			"./snapshot_dto.go",
			"./retention_dto.go",
		},
		OutDir:     ".",
		ImportPath: "github.com/stergiotis/boxer/public/storage/recordstore/pushoutstore",
	}.Generate())
}
