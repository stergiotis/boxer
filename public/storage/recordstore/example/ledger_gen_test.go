package example

//go:generate sh -c "go test -tags=\"$(cat ../../../../tags)\" -run TestGenerateLedgerStore ."

import (
	"testing"

	"github.com/stretchr/testify/require"

	"github.com/stergiotis/boxer/public/semistructured/leeway/marshall/go/marshallgen"
	"github.com/stergiotis/boxer/public/storage/recordstore/gen"
)

// TestGenerateLedgerStore emits the evidence-profile store package
// (ADR-0327 (proposed) §SD2): the id as the Key (the scope), the ordinal
// as the Order, the chain and the written-by components emitted by the generator
// beside the fixture's note, under a registry-style id snapshot (the
// profile's components share sections, which declaration-order ids cannot
// serve). Run it to (re)generate:
//
//	go test -tags "$(cat tags)" -run TestGenerateLedgerStore ./public/storage/recordstore/example/
func TestGenerateLedgerStore(t *testing.T) {
	manip, err := GetLedgerSchemaInManipulator()
	require.NoError(t, err)
	td, err := manip.BuildTableDesc()
	require.NoError(t, err)
	require.NoError(t, gen.Input{
		PackageName:    "example",
		StoreName:      "Ledger",
		TableName:      "ledger",
		Table:          td,
		RowConfig:      TableRowConfig,
		ComponentPaths: []string{"./ledgernote_dto.go"},
		OutDir:         ".",
		ImportPath:     "github.com/stergiotis/boxer/public/storage/recordstore/example",
		Roles:          gen.Roles{Order: "ordinal"},
		Wrapper:        marshallgen.FixedIdsWrapper{Ids: ledgerGenIds},
		Evidence:       &gen.Evidence{Chain: true, WrittenBy: true},
	}.Generate())
}
