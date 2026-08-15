package persiststore

//go:generate sh -c "go test -tags=\"$(cat ../../../../../tags)\" -run TestGeneratePersistStore ."

import (
	"testing"

	"github.com/stergiotis/boxer/public/keelson/runtime/factsschema/storegen"
	"github.com/stergiotis/boxer/public/keelson/runtime/vocab"
	"github.com/stergiotis/boxer/public/semistructured/leeway/marshall/go/marshallgen"
	"github.com/stergiotis/boxer/public/storage/recordstore/gen"
	"github.com/stretchr/testify/require"
)

// TestGeneratePersistStore emits the persist-state store through the
// recordstore generator (ADR-0100 SD6, ADR-0105 D3a). Run it to
// (re)generate:
//
//	go test -tags "$(cat tags)" -run TestGeneratePersistStore ./public/keelson/runtime/persist/persiststore/
//
// The membership ids are the runtime vocabulary's, snapshotted through
// storegen.MembershipIds exactly as a facts-bound store does (ADR-0105,
// Update 2026-08-15) — not recordstore/gen's default declaration-order
// numbering, under which a field reorder in State would silently renumber
// every row on disk. persiststore_test.go pins the baked ids to the
// registry.
func TestGeneratePersistStore(t *testing.T) {
	manip, err := GetPersistSchemaInManipulator()
	require.NoError(t, err)
	td, err := manip.BuildTableDesc()
	require.NoError(t, err)
	ids, err := storegen.MembershipIds(vocab.NkRegistry)
	require.NoError(t, err)
	require.NoError(t, gen.Input{
		PackageName:    "persiststore",
		StoreName:      "Persist",
		TableName:      TableName,
		Database:       DatabaseName,
		Table:          td,
		RowConfig:      TableRowConfig,
		ComponentPaths: []string{"./state_dto.go"},
		OutDir:         ".",
		ImportPath:     "github.com/stergiotis/boxer/public/keelson/runtime/persist/persiststore",
		Wrapper:        marshallgen.FixedIdsWrapper{Ids: ids},
	}.Generate())
}
