package agentrunstore

//go:generate sh -c "go test -tags=\"$(cat ../../../../../tags)\" -run TestGenerateAgentrunStores ."

import (
	"testing"

	"github.com/stretchr/testify/require"

	"github.com/stergiotis/boxer/public/keelson/runtime/factsschema/storegen"
	"github.com/stergiotis/boxer/public/keelson/runtime/vocab"
	"github.com/stergiotis/boxer/public/semistructured/leeway/marshall/go/marshallgen"
	"github.com/stergiotis/boxer/public/storage/recordstore/gen"
)

// TestGenerateAgentrunStores emits both stores through the recordstore
// generator (ADR-0100 SD6, ADR-0105 D3a), with the runtime vocabulary's
// membership ids snapshotted through storegen.MembershipIds so a field
// reorder cannot renumber what is on disk. Run it to (re)generate:
//
//	go test -tags "$(cat tags)" -run TestGenerateAgentrunStores ./public/keelson/runtime/agentrun/agentrunstore/
func TestGenerateAgentrunStores(t *testing.T) {
	ids, err := storegen.MembershipIds(vocab.NkRegistry)
	require.NoError(t, err)
	const importPath = "github.com/stergiotis/boxer/public/keelson/runtime/agentrun/agentrunstore"

	runManip, err := GetRunSchemaInManipulator()
	require.NoError(t, err)
	runTd, err := runManip.BuildTableDesc()
	require.NoError(t, err)
	require.NoError(t, gen.Input{
		PackageName: "agentrunstore", StoreName: "Run", TableName: TableNameRun, Database: DatabaseName,
		Table: runTd, RowConfig: TableRowConfig, ComponentPaths: []string{"./run_dto.go"}, OutDir: ".",
		ImportPath: importPath, Wrapper: marshallgen.FixedIdsWrapper{Ids: ids},
	}.Generate())

	entryManip, err := GetEntrySchemaInManipulator()
	require.NoError(t, err)
	entryTd, err := entryManip.BuildTableDesc()
	require.NoError(t, err)
	require.NoError(t, gen.Input{
		PackageName: "agentrunstore", StoreName: "Entry", TableName: TableNameEntry, Database: DatabaseName,
		Table: entryTd, RowConfig: TableRowConfig, ComponentPaths: []string{"./entry_dto.go"}, OutDir: ".",
		ImportPath: importPath, Wrapper: marshallgen.FixedIdsWrapper{Ids: ids},
	}.Generate())
}
