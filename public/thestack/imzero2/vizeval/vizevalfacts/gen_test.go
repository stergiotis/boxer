package vizevalfacts_test

import (
	"testing"

	"github.com/stretchr/testify/require"

	"github.com/stergiotis/boxer/public/keelson/runtime/factsschema/storegen"
	"github.com/stergiotis/boxer/public/keelson/runtime/vocab"
)

//go:generate sh -c "go test -tags=\"$(cat ../../../../../tags)\" -run TestGenerateScoreStore ."

// TestGenerateScoreStore (re)generates the facts-bound scorecard store in
// place; run it after changing the DTO or the vocabulary. The ids come from
// the runtime vocabulary, so a scan filters on the same memberships the rows
// carry. chstore owns boxer.facts (ADR-0184 §SD2), so the store runs no DDL.
func TestGenerateScoreStore(t *testing.T) {
	ids, err := storegen.MembershipIds(vocab.NkRegistry)
	require.NoError(t, err)
	require.NoError(t, storegen.Input{
		PackageName:    "vizevalfacts",
		StoreName:      "Score",
		ComponentPaths: []string{"./score_dto.go"},
		OutDir:         ".",
		ImportPath:     "github.com/stergiotis/boxer/public/thestack/imzero2/vizeval/vizevalfacts",
		Ids:            ids,
	}.Generate())
}
