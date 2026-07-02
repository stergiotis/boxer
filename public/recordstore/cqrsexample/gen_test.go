package cqrsexample

import (
	"testing"

	"github.com/stergiotis/boxer/public/recordstore/gen"
	"github.com/stretchr/testify/require"
)

// TestGenerateLedgerStore emits the ledger store package through the
// recordstore generator (ADR-0100 SD6). Run it to (re)generate:
//
//	go test -tags "$(cat tags)" -run TestGenerateLedgerStore ./public/recordstore/cqrsexample/
func TestGenerateLedgerStore(t *testing.T) {
	manip, err := GetLedgerSchemaInManipulator()
	require.NoError(t, err)
	td, err := manip.BuildTableDesc()
	require.NoError(t, err)
	require.NoError(t, gen.Input{
		PackageName: "cqrsexample",
		StoreName:   "Ledger",
		TableName:   "ledger",
		Table:       td,
		RowConfig:   TableRowConfig,
		ComponentPaths: []string{
			"./opened_dto.go",
			"./deposited_dto.go",
			"./withdrawn_dto.go",
			"./closed_dto.go",
			"./accountstate_dto.go",
		},
		OutDir: ".",
	}.Generate())
}
