package actor

import (
	"testing"

	"github.com/stergiotis/boxer/public/storage/recordstore/gen"
	"github.com/stretchr/testify/require"
)

// TestGenerateActorStore emits the actor descriptor store through the
// recordstore generator (ADR-0100 SD6). Run it to (re)generate:
//
//	go test -tags "$(cat tags)" -run TestGenerateActorStore ./public/storage/recordstore/dimension/actor/
func TestGenerateActorStore(t *testing.T) {
	manip, err := GetActorSchemaInManipulator()
	require.NoError(t, err)
	td, err := manip.BuildTableDesc()
	require.NoError(t, err)
	require.NoError(t, gen.Input{
		PackageName:    "actor",
		StoreName:      "Actor",
		TableName:      "actor",
		Table:          td,
		RowConfig:      TableRowConfig,
		ComponentPaths: []string{"./actor_dto.go"},
		OutDir:         ".",
		ImportPath:     "github.com/stergiotis/boxer/public/storage/recordstore/dimension/actor",
	}.Generate())
}
