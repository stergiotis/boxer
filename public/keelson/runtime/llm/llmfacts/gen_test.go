package llmfacts_test

import (
	"testing"

	"github.com/stretchr/testify/require"

	"github.com/stergiotis/boxer/public/keelson/runtime/factsschema/storegen"
	"github.com/stergiotis/boxer/public/keelson/runtime/vocab"
)

//go:generate sh -c "go test -tags=\"$(cat ../../../../../tags)\" -run TestGenerateLlmCallStore ."

// TestGenerateLlmCallStore (re)generates the facts-bound call store in
// place; run it after changing the DTO or the vocabulary. The ids come from
// the runtime vocabulary, so a scan filters on the same memberships the
// rows carry. The store is externally provisioned by construction: chstore
// owns boxer.facts (ADR-0184 §SD2).
func TestGenerateLlmCallStore(t *testing.T) {
	ids, err := storegen.MembershipIds(vocab.NkRegistry)
	require.NoError(t, err)
	require.NoError(t, storegen.Input{
		PackageName:    "llmfacts",
		StoreName:      "Call",
		ComponentPaths: []string{"./llmcall_dto.go"},
		OutDir:         ".",
		ImportPath:     "github.com/stergiotis/boxer/public/keelson/runtime/llm/llmfacts",
		Ids:            ids,
	}.Generate())
}
