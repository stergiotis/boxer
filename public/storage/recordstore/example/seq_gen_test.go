package example

//go:generate sh -c "go test -tags=\"$(cat ../../../../tags)\" -run TestGenerateSeqStore ."

import (
	"testing"

	"github.com/stergiotis/boxer/public/storage/recordstore/gen"
	"github.com/stretchr/testify/require"
)

// TestGenerateSeqStore emits the u64-Order, predicate-state-view store
// package (ADR-0100 Updates 2026-08-30): the Order role bound by name to
// the eid EntityId column — uint64 verb signatures, plain integer range
// SQL — and the state view generated with TombstoneView over a schema
// without a u8 Lifecycle role, so the tombstone pair is required at
// construction. Run it to (re)generate:
//
//	go test -tags "$(cat tags)" -run TestGenerateSeqStore ./public/storage/recordstore/example/
func TestGenerateSeqStore(t *testing.T) {
	manip, err := GetSeqSchemaInManipulator()
	require.NoError(t, err)
	td, err := manip.BuildTableDesc()
	require.NoError(t, err)
	require.NoError(t, gen.Input{
		PackageName: "example",
		StoreName:   "Seq",
		TableName:   "seq",
		Table:       td,
		RowConfig:   TableRowConfig,
		ComponentPaths: []string{
			"./seqreading_dto.go",
		},
		OutDir:        ".",
		ImportPath:    "github.com/stergiotis/boxer/public/storage/recordstore/example",
		Roles:         gen.Roles{Order: "eid"},
		TombstoneView: true,
	}.Generate())
}
