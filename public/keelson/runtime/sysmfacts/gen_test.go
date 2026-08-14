package sysmfacts_test

import (
	"testing"

	"github.com/stergiotis/boxer/public/keelson/runtime/factsschema/storegen"
	"github.com/stergiotis/boxer/public/keelson/runtime/sysmvocab"
	"github.com/stretchr/testify/require"
)

//go:generate sh -c "go test -tags=\"$(cat ../../../../tags)\" -run TestGenerateSysmetricsStore ."

// TestGenerateSysmetricsStore (re)generates the facts-bound metrics store in
// place. Run it after changing a DTO or the vocabulary:
//
//	go test -tags "$(cat tags)" -run TestGenerateSysmetricsStore ./public/keelson/runtime/sysmfacts/
//
// The ids come from the vocabulary rather than from declaration order, which
// is what lets the three kinds co-reside on the shared `symbol` section; see
// the id-injectivity checks in sysmfacts_test.go. The store is externally
// provisioned by construction — storegen gives it no way to run DDL, because
// chstore owns boxer.facts (ADR-0184 §SD2).
func TestGenerateSysmetricsStore(t *testing.T) {
	ids, err := storegen.MembershipIds(sysmvocab.NkRegistry)
	require.NoError(t, err)
	require.NoError(t, storegen.Input{
		PackageName:    "sysmfacts",
		StoreName:      "Sysmetrics",
		ComponentPaths: componentPaths,
		OutDir:         ".",
		ImportPath:     "github.com/stergiotis/boxer/public/keelson/runtime/sysmfacts",
		Ids:            ids,
	}.Generate())
}
