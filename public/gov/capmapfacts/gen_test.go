package capmapfacts

import (
	"testing"

	"github.com/stretchr/testify/require"

	"github.com/stergiotis/boxer/public/gov/capmapvocab"
	"github.com/stergiotis/boxer/public/semistructured/leeway/marshall/go/marshallgen"
	"github.com/stergiotis/boxer/public/semistructured/leeway/naming"
)

// TestGenerateCapmapCodecs (re)generates the codecs of the facts-row
// components in place. The membership ids come from capmapvocab by
// registered name, so the codecs write the ids the vocabulary pins.
func TestGenerateCapmapCodecs(t *testing.T) {
	ids := make(map[string]uint64, len(capmapvocab.AllMembs))
	for _, m := range capmapvocab.AllMembs {
		ids[m.GetNaturalKey().Convert(naming.LowerCamelCase).String()] = m.GetId().Value()
	}
	wrapper := marshallgen.FixedIdsWrapper{Ids: ids}
	for _, in := range []string{"competence_dto.go", "competencetags_dto.go", "competencelifecycle_dto.go", "competenceclaims_dto.go", "competenceclaimbases_dto.go", "relation_dto.go"} {
		_, err := marshallgen.Generate(in, in[:len(in)-len(".go")]+".out.go", wrapper, marshallgen.EmitOpts{})
		require.NoError(t, err, in)
	}
}
