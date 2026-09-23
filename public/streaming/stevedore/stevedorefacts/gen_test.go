package stevedorefacts_test

import (
	"testing"

	"github.com/stretchr/testify/require"

	"github.com/stergiotis/boxer/public/keelson/runtime/factsschema/storegen"
	"github.com/stergiotis/boxer/public/streaming/stevedore/stevedorevocab"
)

//go:generate sh -c "go test -tags=\"$(cat ../../../../tags)\" -run TestGenerateStevedoreStore ."

// componentPaths is the DTO set the store is generated over.
var componentPaths = []string{
	"./deadletter_dto.go",
}

// TestGenerateStevedoreStore (re)generates the facts-bound dead-letter store
// in place. Run it after changing the DTO or the vocabulary:
//
//	go test -tags "$(cat tags)" -run TestGenerateStevedoreStore ./public/streaming/stevedore/stevedorefacts/
//
// The store is externally provisioned by construction — storegen gives it no
// way to run DDL, because chstore owns boxer.facts (ADR-0184 §SD2).
func TestGenerateStevedoreStore(t *testing.T) {
	ids, err := storegen.MembershipIds(stevedorevocab.NkRegistry)
	require.NoError(t, err)
	require.NoError(t, storegen.Input{
		PackageName:    "stevedorefacts",
		StoreName:      "Stevedore",
		ComponentPaths: componentPaths,
		OutDir:         ".",
		ImportPath:     "github.com/stergiotis/boxer/public/streaming/stevedore/stevedorefacts",
		Ids:            ids,
	}.Generate())
}
