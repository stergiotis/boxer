package agentfacts_test

import (
	"testing"

	"github.com/stretchr/testify/require"

	"github.com/stergiotis/boxer/public/keelson/runtime/factsschema/storegen"
	"github.com/stergiotis/boxer/public/keelson/runtime/vocab"
)

//go:generate sh -c "go test -tags=\"$(cat ../../../../../tags)\" -run TestGenerateActionStore ."

// TestGenerateActionStore (re)generates the facts-bound action store in
// place; run it after changing the DTO or the vocabulary. The store is
// externally provisioned by construction: chstore owns boxer.facts
// (ADR-0184 §SD2).
func TestGenerateActionStore(t *testing.T) {
	ids, err := storegen.MembershipIds(vocab.NkRegistry)
	require.NoError(t, err)
	require.NoError(t, storegen.Input{
		PackageName:    "agentfacts",
		StoreName:      "Action",
		ComponentPaths: []string{"./agentaction_dto.go"},
		OutDir:         ".",
		ImportPath:     "github.com/stergiotis/boxer/public/keelson/runtime/agent/agentfacts",
		Ids:            ids,
	}.Generate())
}
