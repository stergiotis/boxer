package trail_test

import (
	"testing"

	"github.com/stretchr/testify/require"

	"github.com/stergiotis/boxer/public/keelson/runtime/factsschema/storegen"
	"github.com/stergiotis/boxer/public/keelson/runtime/vocab"
)

//go:generate sh -c "go test -tags=\"$(cat ../../../../tags)\" -run TestGenerateTrailStore ."

// TestGenerateTrailStore (re)generates the facts-bound trail store in
// place; run it after changing a DTO or the vocabulary. The ids come from
// the runtime vocabulary, so a scan filters on the same memberships the
// rows carry. The store is externally provisioned by construction: chstore
// owns boxer.facts (ADR-0184 §SD2).
//
// The context components lead: every row carries Origin, so it leads each
// entity's archetype.
func TestGenerateTrailStore(t *testing.T) {
	ids, err := storegen.MembershipIds(vocab.NkRegistry)
	require.NoError(t, err)
	require.NoError(t, storegen.Input{
		PackageName: "trail",
		StoreName:   "Trail",
		ComponentPaths: []string{
			"./origin_dto.go", "./conversation_dto.go", "./delegation_dto.go", "./cause_dto.go",
			"./llmcall_dto.go", "./llmmessage_dto.go", "./llmmessagebody_dto.go",
			"./agentaction_dto.go", "./agentgrant_dto.go", "./httpfetch_dto.go", "./agentcapture_dto.go",
			"./agentdisclosure_dto.go", "./adhocdataset_dto.go",
		},
		OutDir:     ".",
		ImportPath: "github.com/stergiotis/boxer/public/keelson/runtime/trail",
		Ids:        ids,
	}.Generate())
}
