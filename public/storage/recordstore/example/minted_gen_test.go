package example

//go:generate sh -c "go test -tags=\"$(cat ../../../../tags)\" -run TestGenerateMintedStore ."

import (
	"testing"

	"github.com/stergiotis/boxer/public/storage/recordstore/gen"
	"github.com/stretchr/testify/require"
)

// TestGenerateMintedStore emits the KeyStream store package: the u64 Key is
// minted from the configured stream through MintKey. Run it to
// (re)generate:
//
//	go test -tags "$(cat tags)" -run TestGenerateMintedStore ./public/storage/recordstore/example/
func TestGenerateMintedStore(t *testing.T) {
	manip, err := GetMintedSchemaInManipulator()
	require.NoError(t, err)
	td, err := manip.BuildTableDesc()
	require.NoError(t, err)
	require.NoError(t, gen.Input{
		PackageName:    "example",
		StoreName:      "Minted",
		TableName:      "minted",
		Table:          td,
		RowConfig:      TableRowConfig,
		ComponentPaths: []string{"./mintedlabel_dto.go"},
		OutDir:         ".",
		ImportPath:     "github.com/stergiotis/boxer/public/storage/recordstore/example",
		KeyStream:      true,
	}.Generate())
}
