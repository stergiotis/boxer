package example

import (
	"testing"

	"github.com/stergiotis/boxer/public/storage/recordstore/gen"
	"github.com/stretchr/testify/require"
)

// KeyStream mints tagged uint64 ids, so a string Key is refused at
// generation.
func TestKeyStreamRefusesAStringKey(t *testing.T) {
	manip, err := GetRevSchemaInManipulator()
	require.NoError(t, err)
	td, err := manip.BuildTableDesc()
	require.NoError(t, err)
	err = gen.Input{
		PackageName: "tmp", StoreName: "Rev", TableName: "rev", Table: td, RowConfig: TableRowConfig,
		ComponentPaths: []string{"./revnote_dto.go"}, OutDir: t.TempDir(), ImportPath: "example.invalid/tmp",
		Roles: gen.Roles{Order: "ord"}, KeyStream: true,
	}.Generate()
	require.ErrorContains(t, err, "KeyStream mints a tagged uint64 id")
}
