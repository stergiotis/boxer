package mddocfacts_test

import (
	"testing"

	"github.com/stretchr/testify/require"

	"github.com/stergiotis/boxer/public/keelson/runtime/factsschema/storegen"
	"github.com/stergiotis/boxer/public/semistructured/markdown/mddocvocab"
)

//go:generate sh -c "go test -tags=\"$(cat ../../../tags)\" -run TestGenerateMddocStore ."

// componentPaths is the DTO set the store is generated over.
var componentPaths = []string{
	"./mddoc_dto.go",
	"./mdheading_dto.go",
	"./mdcode_dto.go",
	"./mdlink_dto.go",
	"./mdemphasis_dto.go",
	"./mdtag_dto.go",
}

// TestGenerateMddocStore (re)generates the facts-bound document store in
// place. Run it after changing the DTO or the vocabulary:
//
//	go test -tags "$(cat tags)" -run TestGenerateMddocStore ./public/semistructured/markdown/mddocfacts/
//
// The store is externally provisioned by construction — storegen gives it no
// way to run DDL, because chstore owns boxer.facts (ADR-0184 §SD2).
func TestGenerateMddocStore(t *testing.T) {
	ids, err := storegen.MembershipIds(mddocvocab.NkRegistry)
	require.NoError(t, err)
	require.NoError(t, storegen.Input{
		PackageName:    "mddocfacts",
		StoreName:      "Mddoc",
		ComponentPaths: componentPaths,
		OutDir:         ".",
		ImportPath:     "github.com/stergiotis/boxer/public/semistructured/markdown/mddocfacts",
		Ids:            ids,
	}.Generate())
}
