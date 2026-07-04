package example

import (
	"testing"

	"github.com/stergiotis/boxer/public/storage/recordstore/gen"
	"github.com/stretchr/testify/require"
)

// TestGenerateDeviceStore emits the whole device store package — DML, DDL
// column body, RA classes, the three component codecs and the store glue —
// through the recordstore generator (ADR-0100 SD6). Run it to
// (re)generate:
//
//	go test -tags "$(cat tags)" -run TestGenerateDeviceStore ./public/storage/recordstore/example/
func TestGenerateDeviceStore(t *testing.T) {
	manip, err := GetDeviceSchemaInManipulator()
	require.NoError(t, err)
	td, err := manip.BuildTableDesc()
	require.NoError(t, err)
	require.NoError(t, gen.Input{
		PackageName: "example",
		StoreName:   "Device",
		TableName:   "device",
		Table:       td,
		RowConfig:   TableRowConfig,
		ComponentPaths: []string{
			"./identity_dto.go",
			"./battery_dto.go",
			"./tagged_dto.go",
			"./located_dto.go",
		},
		OutDir: ".",
	}.Generate())
}
