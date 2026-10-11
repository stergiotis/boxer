package example

import (
	"testing"

	"github.com/stretchr/testify/require"

	"github.com/stergiotis/boxer/public/semistructured/leeway/common"
	"github.com/stergiotis/boxer/public/semistructured/leeway/marshall/go/marshallgen"
	"github.com/stergiotis/boxer/public/storage/recordstore/gen"
)

// evidenceInto generates a store with the chain profile and no hand-written
// component over manip, into a scratch directory.
func evidenceInto(t *testing.T, manip *common.TableManipulator, roles gen.Roles, cas, keyStream bool) error {
	t.Helper()
	td, err := manip.BuildTableDesc()
	require.NoError(t, err)
	return gen.Input{
		PackageName: "tmp",
		StoreName:   "Probe",
		TableName:   string(td.DictionaryEntry.Name),
		Table:       td,
		RowConfig:   TableRowConfig,
		OutDir:      t.TempDir(),
		ImportPath:  "example.com/tmp",
		Roles:       roles,
		Cas:         cas,
		KeyStream:   keyStream,
		Wrapper:     marshallgen.FixedIdsWrapper{Ids: ledgerGenIds},
		Evidence:    &gen.Evidence{Chain: true, WrittenBy: true},
	}.Generate()
}

// The chain profile's preconditions (ADR-0327 (proposed) §SD2): a uint64
// Key as the scope, a uint64 Order as the ordinal, and no Cas.
func TestEvidenceChainRefusesAStringKey(t *testing.T) {
	manip, err := GetRevSchemaInManipulator()
	require.NoError(t, err)
	err = evidenceInto(t, manip, gen.Roles{Order: "ord", Revision: "rev"}, false, false)
	require.ErrorContains(t, err, "uint64 Key")
}

func TestEvidenceChainRefusesATimeOrder(t *testing.T) {
	manip, err := GetDeviceSchemaInManipulator()
	require.NoError(t, err)
	err = evidenceInto(t, manip, gen.Roles{}, false, false)
	require.ErrorContains(t, err, "uint64 Order")
}

func TestEvidenceChainRefusesCas(t *testing.T) {
	manip, err := GetLedgerSchemaInManipulator()
	require.NoError(t, err)
	err = evidenceInto(t, manip, gen.Roles{Order: "ordinal"}, true, false)
	require.ErrorContains(t, err, "refused with Cas")
}

// A hand-written component of the profile's own kind is refused: the
// profile owns the shape.
func TestEvidenceRefusesAHandWrittenChainComponent(t *testing.T) {
	manip, err := GetLedgerSchemaInManipulator()
	require.NoError(t, err)
	td, err := manip.BuildTableDesc()
	require.NoError(t, err)
	err = gen.Input{
		PackageName:    "tmp",
		StoreName:      "Probe",
		TableName:      "ledger",
		Table:          td,
		RowConfig:      TableRowConfig,
		ComponentPaths: []string{"./evidencechain_dto.out.go"},
		OutDir:         t.TempDir(),
		ImportPath:     "example.com/tmp",
		Roles:          gen.Roles{Order: "ordinal"},
		Wrapper:        marshallgen.FixedIdsWrapper{Ids: ledgerGenIds},
		Evidence:       &gen.Evidence{Chain: true},
	}.Generate()
	require.ErrorContains(t, err, "emits itself")
}
