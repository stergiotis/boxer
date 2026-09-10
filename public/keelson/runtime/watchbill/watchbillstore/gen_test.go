package watchbillstore

//go:generate sh -c "go test -tags=\"$(cat ../../../../../tags)\" -run TestGenerateWatchbillStores ."

import (
	"testing"

	"github.com/stergiotis/boxer/public/keelson/runtime/factsschema/storegen"
	"github.com/stergiotis/boxer/public/keelson/runtime/vocab"
	"github.com/stergiotis/boxer/public/semistructured/leeway/marshall/go/marshallgen"
	"github.com/stergiotis/boxer/public/storage/recordstore/gen"
	"github.com/stretchr/testify/require"
)

// TestGenerateWatchbillStores emits both stores through the recordstore
// generator (ADR-0100 SD6, ADR-0105 D3a). Run it to (re)generate:
//
//	go test -tags "$(cat tags)" -run TestGenerateWatchbillStores ./public/keelson/runtime/watchbill/watchbillstore/
//
// The membership ids are the runtime vocabulary's, snapshotted through
// storegen.MembershipIds exactly as persiststore's are, so a field reorder
// in a DTO cannot renumber what is on disk.
func TestGenerateWatchbillStores(t *testing.T) {
	ids, err := storegen.MembershipIds(vocab.NkRegistry)
	require.NoError(t, err)
	const importPath = "github.com/stergiotis/boxer/public/keelson/runtime/watchbill/watchbillstore"

	jobManip, err := GetJobSchemaInManipulator()
	require.NoError(t, err)
	jobTd, err := jobManip.BuildTableDesc()
	require.NoError(t, err)
	require.NoError(t, gen.Input{
		PackageName:    "watchbillstore",
		StoreName:      "Job",
		TableName:      TableNameJob,
		Database:       DatabaseName,
		Table:          jobTd,
		RowConfig:      TableRowConfig,
		ComponentPaths: []string{"./job_dto.go"},
		OutDir:         ".",
		ImportPath:     importPath,
		Wrapper:        marshallgen.FixedIdsWrapper{Ids: ids},
	}.Generate())

	eventManip, err := GetEventSchemaInManipulator()
	require.NoError(t, err)
	eventTd, err := eventManip.BuildTableDesc()
	require.NoError(t, err)
	require.NoError(t, gen.Input{
		PackageName:    "watchbillstore",
		StoreName:      "Event",
		TableName:      TableNameEvent,
		Database:       DatabaseName,
		Table:          eventTd,
		RowConfig:      TableRowConfig,
		ComponentPaths: []string{"./event_dto.go"},
		OutDir:         ".",
		ImportPath:     importPath,
		Wrapper:        marshallgen.FixedIdsWrapper{Ids: ids},
	}.Generate())
}
