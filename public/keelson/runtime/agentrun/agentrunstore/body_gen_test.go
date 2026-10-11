package agentrunstore

//go:generate sh -c "go test -tags=\"$(cat ../../../../../tags)\" -run TestGenerateAgentrunBodyStore ."

import (
	"testing"

	"github.com/stretchr/testify/require"

	"github.com/stergiotis/boxer/public/keelson/runtime/factsschema/storegen"
	"github.com/stergiotis/boxer/public/keelson/runtime/vocab"
	"github.com/stergiotis/boxer/public/semistructured/leeway/marshall/go/marshallgen"
	"github.com/stergiotis/boxer/public/storage/recordstore/gen"
)

// TestGenerateAgentrunBodyStore emits the vault store (ADR-0320 §SD1)
// through the recordstore generator, as TestGenerateAgentrunStores emits
// the ledger's two. Run it to (re)generate:
//
//	go test -tags "$(cat tags)" -run TestGenerateAgentrunBodyStore ./public/keelson/runtime/agentrun/agentrunstore/
func TestGenerateAgentrunBodyStore(t *testing.T) {
	ids, err := storegen.MembershipIds(vocab.NkRegistry)
	require.NoError(t, err)
	const importPath = "github.com/stergiotis/boxer/public/keelson/runtime/agentrun/agentrunstore"

	manip, err := GetBodySchemaInManipulator()
	require.NoError(t, err)
	td, err := manip.BuildTableDesc()
	require.NoError(t, err)
	require.NoError(t, gen.Input{
		PackageName: "agentrunstore", StoreName: "BodyRow", TableName: TableNameBody, Database: DatabaseName,
		Table: td, RowConfig: TableRowConfig, ComponentPaths: []string{"./body_dto.go"}, OutDir: ".",
		ImportPath: importPath, Wrapper: marshallgen.FixedIdsWrapper{Ids: ids},
	}.Generate())
}
