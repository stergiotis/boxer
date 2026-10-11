package example

//go:generate sh -c "go test -tags=\"$(cat ../../../../tags)\" -run TestGenerateRevStore ."

import (
	"testing"

	"github.com/stergiotis/boxer/public/storage/recordstore/gen"
	"github.com/stretchr/testify/require"
)

// TestGenerateRevStore emits the Revision-role store package: the Order
// bound by name to the u64 ord column and the Revision to the u32 rev
// column, so the DDL orders by (id, ord, rev) and every read collapses one
// (id, ord) to its highest rev. Run it to (re)generate:
//
//	go test -tags "$(cat tags)" -run TestGenerateRevStore ./public/storage/recordstore/example/
func TestGenerateRevStore(t *testing.T) {
	manip, err := GetRevSchemaInManipulator()
	require.NoError(t, err)
	td, err := manip.BuildTableDesc()
	require.NoError(t, err)
	require.NoError(t, gen.Input{
		PackageName:    "example",
		StoreName:      "Rev",
		TableName:      "rev",
		Table:          td,
		RowConfig:      TableRowConfig,
		ComponentPaths: []string{"./revnote_dto.go"},
		OutDir:         ".",
		ImportPath:     "github.com/stergiotis/boxer/public/storage/recordstore/example",
		Roles:          gen.Roles{Order: "ord", Revision: "rev"},
	}.Generate())
}
