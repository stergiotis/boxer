package meshdemo_test

import (
	"testing"

	"github.com/stretchr/testify/require"

	"github.com/stergiotis/boxer/public/keelson/runtime/factsschema/meshdemo"
	"github.com/stergiotis/boxer/public/keelson/runtime/factsschema/storegen"
)

//go:generate sh -c "go test -tags=\"$(cat ../../../../../tags)\" -run TestGenerateFleetStore ."

// TestGenerateFleetStore (re)generates the fleet agent's facts-bound store in
// place:
//
//	go test -tags "$(cat tags)" -run TestGenerateFleetStore ./public/keelson/runtime/factsschema/meshdemo/
//
// Only [meshdemo.FleetSample] is generated. HostLoad — the component the other
// domain formulates later — is deliberately absent from ComponentPaths: it has
// no generated artifact at all, which is what makes its read path the late one.
//
// The ids come from the vocabulary, so what the store bakes is what any other
// reader resolves from the same names. That is the whole mesh mechanism; the
// rest of this package is a demonstration that it holds.
func TestGenerateFleetStore(t *testing.T) {
	ids, err := storegen.MembershipIds(meshdemo.NkRegistry)
	require.NoError(t, err)
	require.NoError(t, storegen.Input{
		PackageName:    "meshdemo",
		StoreName:      "Fleet",
		ComponentPaths: []string{"./fleetsample_dto.go"},
		OutDir:         ".",
		ImportPath:     "github.com/stergiotis/boxer/public/keelson/runtime/factsschema/meshdemo",
		Ids:            ids,
	}.Generate())
}
