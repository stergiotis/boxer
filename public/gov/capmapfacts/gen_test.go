package capmapfacts

import (
	"testing"

	"github.com/stretchr/testify/require"

	"github.com/stergiotis/boxer/public/gov/capmapvocab"
	"github.com/stergiotis/boxer/public/keelson/runtime/factsschema/storegen"
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
	// The origin's memberships are the runtime vocabulary's (ADR-0330 §SD3).
	require.NoError(t, storegen.AddOriginIds(ids))
	wrapper := marshallgen.FixedIdsWrapper{Ids: ids}
	for _, in := range []string{"competence_dto.go", "competencetags_dto.go", "competencelifecycle_dto.go", "competenceclaims_dto.go", "competenceclaimbases_dto.go", "relation_dto.go"} {
		_, err := marshallgen.Generate(in, in[:len(in)-len(".go")]+".out.go", wrapper, marshallgen.EmitOpts{})
		require.NoError(t, err, in)
	}
	// The origin is the audit package's component, its codec emitted into
	// this package beside the alias in origin.go.
	_, err := marshallgen.Generate("../../storage/recordstore/evidence/audit/origin_dto.go", "origin_dto.out.go", wrapper, marshallgen.EmitOpts{Package: "capmapfacts"})
	require.NoError(t, err, "origin_dto.go")
}
