package example

import (
	"testing"

	"github.com/stergiotis/boxer/public/storage/recordstore/gen"
	"github.com/stretchr/testify/require"
)

// Input.Cas emits an in-place rewrite only for components it can rewrite
// faithfully: one plain scalar per section on a static membership. A
// collection field is refused at generation, not mis-updated at run time.
func TestCasRefusesAComponentItCannotRewriteInPlace(t *testing.T) {
	manip, err := GetDeviceSchemaInManipulator()
	require.NoError(t, err)
	td, err := manip.BuildTableDesc()
	require.NoError(t, err)
	err = gen.Input{
		PackageName: "tmp", StoreName: "Device", TableName: "device", Table: td, RowConfig: TableRowConfig,
		ComponentPaths: []string{"./tagged_dto.go"}, OutDir: t.TempDir(), ImportPath: "example.invalid/tmp",
		Cas: true,
	}.Generate()
	require.ErrorContains(t, err, "Cas: only plain scalar fields are rewritten in place")
}

// The Revision fixture's component is flat, so a Cas variant of it
// generates, with the block-position settings in the DDL.
func TestCasGeneratesForAFlatComponent(t *testing.T) {
	manip, err := GetRevSchemaInManipulator()
	require.NoError(t, err)
	td, err := manip.BuildTableDesc()
	require.NoError(t, err)
	err = gen.Input{
		PackageName: "tmp", StoreName: "Rev", TableName: "rev", Table: td, RowConfig: TableRowConfig,
		ComponentPaths: []string{"./revnote_dto.go"}, OutDir: t.TempDir(), ImportPath: "example.invalid/tmp",
		Roles: gen.Roles{Order: "ord"}, Cas: true,
	}.Generate()
	require.NoError(t, err)
}
