package test

import (
	"math/rand/v2"
	"testing"

	"github.com/stergiotis/boxer/public/semistructured/leeway/canonicalTypes"
	"github.com/stergiotis/boxer/public/semistructured/leeway/canonicalTypes/sample"
	"github.com/stretchr/testify/require"
)

func TestCanonicalTypes_Roundtrip(t *testing.T) {
	rnd := rand.New(rand.NewPCG(rand.Uint64(), rand.Uint64()))
	p := canonicalTypes.NewParser()
	for i := 0; i < 1000; i++ {
		ct := sample.GenerateSamplePrimitiveType(rnd, nil)
		ct2, err := p.ParsePrimitiveTypeAst(ct.String())
		require.NoError(t, err)
		require.Equal(t, ct.String(), ct2.String())
	}

	for i := 0; i < 1000; i++ {
		ct := sample.GenerateSampleGroup(rnd.IntN(8)+1, rnd, nil)
		ct2, err := p.ParsePrimitiveTypeOrGroupAst(ct.String())
		require.NoError(t, err)
		require.Equal(t, ct.String(), ct2.String())
	}
}
