package ladingdata

//go:generate sh -c "go test -tags=\"$(cat ../../../../tags)\" -run TestGenerateLadingDataStore ."

import (
	"testing"

	"github.com/stergiotis/boxer/public/fs/lading/ladingschema"
	"github.com/stergiotis/boxer/public/fs/lading/ladingvocab"
	"github.com/stergiotis/boxer/public/keelson/runtime/factsschema/storegen"
	"github.com/stergiotis/boxer/public/semistructured/leeway/marshall/go/marshallgen"
	"github.com/stergiotis/boxer/public/storage/recordstore/gen"
	"github.com/stretchr/testify/require"
)

// TestGenerateLadingDataStore emits the block store over `boxer.fsdata`. Run
// it after changing the DTO or the vocabulary:
//
//	go test -tags "$(cat tags)" -run TestGenerateLadingDataStore ./public/fs/lading/ladingdata/
//
// The setup is the entry store's; what differs is the granularity, which the
// corpus profile puts at 1 so that one block is one mark and one compressed
// block — the property a block read's cost depends on (M0 check 1).
//
// This table is the one whose shape was measured against a bespoke
// seven-column table before being committed to: facts-shaped inserts cost
// roughly twice as much wall-clock at 1 MiB blocks and essentially nothing
// extra on disk — 39 KiB compressed for the 184 columns beside the block data
// against 7.7 MiB for the data itself (ADR-0198 `## Updates` 2026-08-19,
// SD11).
func TestGenerateLadingDataStore(t *testing.T) {
	td, err := ladingschema.TableDesc(ladingschema.TableNameData)
	require.NoError(t, err)
	ids, err := storegen.MembershipIds(ladingvocab.NkRegistry)
	require.NoError(t, err)
	require.NoError(t, gen.Input{
		PackageName:    "ladingdata",
		StoreName:      "Data",
		TableName:      ladingschema.TableNameData,
		Database:       ladingschema.DatabaseName,
		Table:          td,
		RowConfig:      ladingschema.TableRowConfig,
		ComponentPaths: []string{"./ladingblock_dto.go"},
		OutDir:         ".",
		ImportPath:     "github.com/stergiotis/boxer/public/fs/lading/ladingdata",
		Wrapper:        marshallgen.FixedIdsWrapper{Ids: ids},
		SharedRA:       &ladingschema.SharedRA,
		DDL:            ladingschema.DataTableOptions(ladingschema.ProfileCorpus),
	}.Generate())
}
