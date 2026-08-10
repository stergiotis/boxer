package sharedsection

//go:generate sh -c "go test -tags=\"$(cat ../../../../tags)\" -run TestGenerateAssetStore ."

import (
	"testing"

	"github.com/stergiotis/boxer/public/semistructured/leeway/marshall/go/marshallgen"
	"github.com/stergiotis/boxer/public/storage/recordstore/gen"
	"github.com/stretchr/testify/require"
)

// TestGenerateAssetStore (re)generates the shared-section store. Both
// components bind the `symbol` section; generation succeeds only because
// the FixedIdsWrapper states globally-unique ids — under the default
// NoOpWrapper the disjoint-sections gate refuses exactly this layout
// (TestGenerateRejectsSharedSection in recordstore/example). Run it to
// (re)generate:
//
//	go test -tags "$(cat tags)" -run TestGenerateAssetStore ./public/storage/recordstore/sharedsection/
func TestGenerateAssetStore(t *testing.T) {
	manip, err := GetAssetSchemaInManipulator()
	require.NoError(t, err)
	td, err := manip.BuildTableDesc()
	require.NoError(t, err)
	require.NoError(t, gen.Input{
		PackageName: "sharedsection",
		StoreName:   "Asset",
		TableName:   "asset",
		Table:       td,
		RowConfig:   TableRowConfig,
		ComponentPaths: []string{
			"./label_dto.go",
			"./state_dto.go",
		},
		OutDir:     ".",
		ImportPath: "github.com/stergiotis/boxer/public/storage/recordstore/sharedsection",
		Wrapper:    marshallgen.FixedIdsWrapper{Ids: AssetMembershipIdAssignment},
	}.Generate())
}
